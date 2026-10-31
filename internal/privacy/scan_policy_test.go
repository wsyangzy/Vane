package privacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestScanPolicyChoicesSurviveSaveAndSchemaReload(t *testing.T) {
	ctx := context.Background()
	for _, onError := range []string{OnErrorFailOpen, OnErrorPassthrough} {
		for _, unsupported := range []string{UnsupportedPathWarn, UnsupportedPathPassthrough} {
			for _, overLimit := range []string{OverLimitScanPrefix, OverLimitSkipScan} {
				t.Run(onError+"/"+unsupported+"/"+overLimit, func(t *testing.T) {
					svc := protectionService(t)
					settings := DefaultSettings()
					settings.OnError, settings.UnsupportedPathAction, settings.OverLimitAction = onError, unsupported, overLimit
					settings.ScanMaxBytes, settings.RestoreResponses = 1572864, false
					if err := svc.UpdateSettings(ctx, settings); err != nil {
						t.Fatal(err)
					}
					for range 2 {
						if err := svc.store.EnsureSchema(ctx); err != nil {
							t.Fatal(err)
						}
						restarted := NewService(svc.store)
						if err := restarted.Reload(ctx); err != nil {
							t.Fatal(err)
						}
						got, err := restarted.GetSettings(ctx)
						if err != nil || got.OnError != onError || got.UnsupportedPathAction != unsupported || got.OverLimitAction != overLimit || got.ScanMaxBytes != settings.ScanMaxBytes || got.RestoreResponses {
							t.Fatalf("saved scanning choices changed after restart: %+v %v", got, err)
						}
					}
				})
			}
		}
	}
}

func TestScanPolicyMigrationOnlyReplacesLegacyRejection(t *testing.T) {
	svc := protectionService(t)
	settings := DefaultSettings()
	settings.OnError = OnErrorPassthrough
	settings.UnsupportedPathAction = UnsupportedPathFailClosed
	settings.OverLimitAction = OverLimitSkipScan
	migrateProtectionSettings(t, svc, settings)
	got, err := svc.GetSettings(context.Background())
	if err != nil || got.OnError != OnErrorPassthrough || got.UnsupportedPathAction != UnsupportedPathWarn || got.OverLimitAction != OverLimitSkipScan {
		t.Fatalf("legacy policy migration overwrote other choices: %+v %v", got, err)
	}
}

func TestScanExceptionRecordingChoicesPreserveRequestBytes(t *testing.T) {
	for _, mode := range []string{ModeDetect, ModeRedact} {
		for _, record := range []bool{false, true} {
			for _, failure := range []string{"scan_error", SkippedUnsupportedPath} {
				t.Run(fmt.Sprintf("%s/record=%v/%s", mode, record, failure), func(t *testing.T) {
					svc := protectionService(t)
					ctx := context.Background()
					settings := DefaultSettings()
					settings.Mode = mode
					if !record {
						settings.OnError, settings.UnsupportedPathAction = OnErrorPassthrough, UnsupportedPathPassthrough
					}
					if err := svc.UpdateSettings(ctx, settings); err != nil {
						t.Fatal(err)
					}
					path, body := "/v1/responses", []byte(`{"input":`)
					if failure == SkippedUnsupportedPath {
						path, body = "/v1/uncovered", []byte(`{ "input": "private-value" }`)
					}
					result, err := svc.Apply(ctx, Request{Path: path, SessionKey: "exceptions"}, body)
					if err != nil || result.SkippedReason != failure {
						t.Fatalf("exception changed: %+v %v", result, err)
					}
					sent, changed := result.SelectBody(body)
					if changed || !bytes.Equal(sent, body) {
						t.Fatal("scan exception changed the original request")
					}
					entries, err := svc.store.listActivity(ctx)
					if err != nil || (len(entries) == 1) != record || len(entries) > 1 {
						t.Fatalf("recording choice not applied: %+v %v", entries, err)
					}
					if record && (entries[0].State != "skipped" || entries[0].SkippedReason != failure) {
						t.Fatalf("missing exception metadata: %+v", entries[0])
					}
					overview, err := svc.GetOverview(ctx)
					wantUnscanned := record && failure == SkippedUnsupportedPath
					if err != nil || (len(overview.UnscannedPaths) > 0) != wantUnscanned {
						t.Fatalf("uncovered-path recording ignored policy: %+v %v", overview.UnscannedPaths, err)
					}
					if _, err := svc.Apply(ctx, Request{Path: "/v1/responses"}, []byte(`{"input":"ordinary text"}`)); err != nil {
						t.Fatal(err)
					}
					after, err := svc.store.listActivity(ctx)
					if err != nil || len(after) != len(entries)+1 {
						t.Fatal("exception policy suppressed an unrelated request record")
					}
				})
			}
		}
	}
}

func TestScanLimitChoicesApplyToWholeCumulativeText(t *testing.T) {
	for _, mode := range []string{ModeDetect, ModeRedact} {
		for _, path := range []string{"/v1/messages", "/v1/responses"} {
			for _, tc := range []struct {
				name, policy, first, second, reason string
				hits                                int
			}{
				{"skip cumulative overflow", OverLimitSkipScan, "SECRET", "other-SECRET", SkippedScanLimit, 0},
				{"skip first-segment overflow", OverLimitSkipScan, "1234567SECRET", "SECRET", SkippedScanLimit, 0},
				{"skip counts UTF-8 bytes", OverLimitSkipScan, "SECRET", "中文中", SkippedScanLimit, 0},
				{"skip exact budget still scans", OverLimitSkipScan, "SECRET", "SECRET", "", 2},
				{"prefix scans within budget", OverLimitScanPrefix, "SECRET", "other-SECRET", SkippedScanTruncated, 1},
			} {
				t.Run(mode+"/"+path+"/"+tc.name, func(t *testing.T) {
					svc := protectionService(t)
					ctx := context.Background()
					settings := DefaultSettings()
					settings.Mode, settings.ScanMaxBytes, settings.OverLimitAction = mode, 12, tc.policy
					if err := svc.UpdateSettings(ctx, settings); err != nil {
						t.Fatal(err)
					}
					if _, err := svc.CreateRule(ctx, Rule{Name: "test secret", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "SECRET", Action: ActionRedact}); err != nil {
						t.Fatal(err)
					}
					body, err := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": tc.first}, {"role": "user", "content": tc.second}}})
					if err != nil {
						t.Fatal(err)
					}
					// Both API formats accept message content, but Responses calls it input.
					if path == "/v1/responses" {
						body = bytes.Replace(body, []byte(`"messages"`), []byte(`"input"`), 1)
					}
					result, err := svc.Apply(ctx, Request{Path: path, SessionKey: "scan-limit"}, body)
					if err != nil || result.HitCount != tc.hits || result.SkippedReason != tc.reason {
						t.Fatalf("wrong cumulative limit behavior: %+v %v", result, err)
					}
					sent, changed := result.SelectBody(body)
					wantChanged := mode == ModeRedact && tc.hits > 0
					if changed != wantChanged || bytes.Equal(sent, body) == wantChanged {
						t.Fatal("scan limit policy changed outbound masking")
					}
					if tc.reason == SkippedScanLimit && (result.Truncated || len(result.RuleHits) != 0 || result.RedactedBody != nil || result.Restoration != nil && len(result.Restoration.values) != 0) {
						t.Fatal("skipping the whole scan allocated mappings or partially masked content")
					}
					entries, err := svc.store.listActivity(ctx)
					if err != nil || len(entries) != 1 || entries[0].SkippedReason != tc.reason {
						t.Fatalf("missing scan limit metadata: %+v %v", entries, err)
					}
					if tc.reason == SkippedScanLimit {
						preview := svc.PreviewRequest(path, body)
						if preview.Changed || preview.HitCount != 0 || preview.Output != string(body) || !strings.Contains(preview.Error, "跳过扫描") {
							t.Fatalf("preview differs from runtime skip: %+v", preview)
						}
					}
				})
			}
		}
	}
}
