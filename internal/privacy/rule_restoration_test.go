package privacy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAllOrdinaryRulesMaskAndRestoreIncludingLegacyWarnings(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	if err := svc.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []Rule{
		{Name: "可逆业务值", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "restorable-value", Action: ActionRedact, ReplacementMode: ReplacementReversible},
		{Name: "旧占位符业务值", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "masked-value", Action: ActionRedact, ReplacementMode: ReplacementPlaceholder, Placeholder: "[MASKED]"},
		{Name: "仅检测业务值", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "detected-value", Action: ActionDetect},
	} {
		if _, err := svc.CreateRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "mixed"}, []byte(`{"input":"restorable-value masked-value detected-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, redacted := result.SelectBody(nil)
	var upstream map[string]string
	if json.Unmarshal(body, &upstream) != nil || !redacted || strings.Contains(upstream["input"], "restorable-value") || strings.Contains(upstream["input"], "masked-value") || strings.Contains(upstream["input"], "detected-value") {
		t.Fatalf("mixed outbound actions changed: %s", body)
	}
	if result.Restoration == nil || len(result.Restoration.values) != 3 {
		t.Fatal("ordinary and legacy warning values were not all mapped")
	}
	rec := httptest.NewRecorder()
	w, finish := result.WrapResponse(rec)
	w.Header().Set("Content-Type", "application/json")
	response, _ := json.Marshal(map[string]string{"output_text": upstream["input"]})
	_, _ = w.Write(response)
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	var downstream map[string]string
	if json.Unmarshal(rec.Body.Bytes(), &downstream) != nil || downstream["output_text"] != "restorable-value masked-value detected-value" {
		t.Fatalf("irreversible output was restored or reversible output was lost: %s", rec.Body)
	}
}

func TestRuleRestorationRespectsGlobalMode(t *testing.T) {
	for _, mode := range []string{ModeDisabled, ModeDetect, ModeRedact} {
		t.Run(mode, func(t *testing.T) {
			svc := protectionService(t)
			ctx := context.Background()
			_, err := svc.CreateRule(ctx, Rule{Name: "可逆", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "original-value", Action: ActionRedact})
			if err != nil {
				t.Fatal(err)
			}
			settings := DefaultSettings()
			settings.Mode = mode
			if err := svc.UpdateSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			original := []byte(`{"input":"original-value"}`)
			result, err := svc.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "mode"}, original)
			if err != nil {
				t.Fatal(err)
			}
			body, changed := result.SelectBody(original)
			want := mode == ModeRedact
			if changed != want || strings.Contains(string(body), "__REDACTED_") != want {
				t.Fatalf("outbound=%s changed=%v want=%v", body, changed, want)
			}
			if (result.Restoration != nil) != want {
				t.Fatal("inactive mode allocated restoration state")
			}
			if result.Restoration != nil && !result.Restoration.active {
				t.Fatal("restoration inactive after sending tokens")
			}
		})
	}
}

func TestDetectModeDoesNotCreateRestorationSessions(t *testing.T) {
	for _, action := range []string{ActionDetect, ActionRedact} {
		svc := protectionService(t)
		ctx := context.Background()
		_, err := svc.CreateRule(ctx, Rule{Name: "不还原", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "private-value", Action: action, ReplacementMode: ReplacementPlaceholder, Placeholder: "[MASKED]"})
		if err != nil {
			t.Fatal(err)
		}
		result, err := svc.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "no-restoration"}, []byte(`{"input":"private-value"}`))
		if err != nil || result.Restoration != nil || len(svc.vault.sessions) != 0 {
			t.Fatalf("%s allocated a restoration session: %v", action, err)
		}
	}
}

func TestResponseRestorationTogglePreservesOutboundMaskingAndStableTokens(t *testing.T) {
	ctx := context.Background()
	svc := protectionService(t)
	if _, err := svc.CreateRule(ctx, Rule{Name: "private term", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "private-value", Action: ActionRedact}); err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	var firstToken string
	for _, restore := range []bool{false, true, false} {
		settings.RestoreResponses = restore
		if err := svc.UpdateSettings(ctx, settings); err != nil {
			t.Fatal(err)
		}
		if err := svc.store.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		if err := svc.Reload(ctx); err != nil {
			t.Fatal(err)
		}
		stored, err := svc.GetSettings(ctx)
		if err != nil || stored.RestoreResponses != restore {
			t.Fatal("restoration toggle did not survive restart")
		}
		result, err := svc.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "toggle-session"}, []byte(`{"input":"private-value"}`))
		if err != nil {
			t.Fatal(err)
		}
		sent, changed := result.SelectBody(nil)
		var upstream map[string]string
		if json.Unmarshal(sent, &upstream) != nil || !changed || !strings.HasPrefix(upstream["input"], "__REDACTED_") {
			t.Fatal("restoration toggle changed outbound masking")
		}
		if firstToken == "" {
			firstToken = upstream["input"]
		} else if firstToken != upstream["input"] {
			t.Fatal("restoration toggle changed the same conversation's token")
		}
		rec := httptest.NewRecorder()
		writer, finish := result.WrapResponse(rec)
		writer.Header().Set("Content-Type", "application/json")
		response, _ := json.Marshal(map[string]string{"output_text": upstream["input"]})
		if _, err := writer.Write(response); err != nil {
			t.Fatal(err)
		}
		if err := finish(); err != nil {
			t.Fatal(err)
		}
		var output map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		want := firstToken
		if restore {
			want = "private-value"
		}
		if output["output_text"] != want {
			t.Fatal("response did not follow restoration toggle")
		}
		entries, err := svc.store.listActivity(ctx)
		if err != nil || entries[0].State != "redacted" {
			t.Fatal("outbound masking was not recorded")
		}
		if !restore && (entries[0].RestoreSkipped != "restoration_disabled" || entries[0].RestoredCount != 0) {
			t.Fatal("disabled response restoration was not recorded")
		}
	}
}

func TestGlobalRestorationMigrationPreservesRulesAndLaterEdits(t *testing.T) {
	for _, oldRestore := range []bool{false, true} {
		for _, oldFormat := range []bool{false, true} {
			t.Run(fmt.Sprintf("restore=%v/format=%v", oldRestore, oldFormat), func(t *testing.T) {
				svc := protectionService(t)
				ctx := context.Background()
				rules := []StoredRule{}
				for _, action := range []string{ActionRedact, ActionDetect, ActionBlock, ActionAllow} {
					for _, enabled := range []bool{false, true} {
						rule, err := svc.store.CreateRule(ctx, Rule{Name: fmt.Sprintf("%s/%v", action, enabled), Enabled: enabled, MatchType: MatchTypeLiteral, Pattern: "private-value", Action: action, ReplacementMode: ReplacementPlaceholder, Placeholder: "[MASKED]"})
						if err != nil {
							t.Fatal(err)
						}
						rules = append(rules, rule)
					}
				}
				settings := DefaultSettings()
				settings.Mode = ModeRedact
				settings.RestoreResponses, settings.KeepKeyFormat = oldRestore, oldFormat
				settings.OnError = OnErrorFailClosed
				settings.OverLimitAction = OverLimitFailClosed
				settings.UnsupportedPathAction = UnsupportedPathFailClosed
				migrateProtectionSettings(t, svc, settings)
				for _, original := range rules {
					got, err := svc.store.GetRule(ctx, original.ID)
					want := ReplacementPlaceholder
					action := original.Action
					if action != ActionAllow {
						action = ActionRedact
						want = ReplacementReversible
						if oldRestore && oldFormat {
							want = ReplacementReversibleKey
						}
					}
					if err != nil || got.ReplacementMode != want || got.Action != action || got.Enabled != original.Enabled || got.Pattern != original.Pattern || got.Placeholder != original.Placeholder {
						t.Fatalf("migration changed an existing rule: %+v, %v", got, err)
					}
				}
				got, err := svc.GetSettings(ctx)
				if err != nil || got.RestoreResponses != oldRestore || got.KeepKeyFormat || got.Mode != settings.Mode {
					t.Fatal("restoration toggle or mode was changed, or retired key format option retained")
				}
				if got.OnError != OnErrorFailOpen || got.OverLimitAction != OverLimitScanPrefix || got.UnsupportedPathAction != UnsupportedPathWarn {
					t.Fatal("legacy rejection policies survived migration")
				}
				changed := rules[1]
				changed.ReplacementMode = ReplacementReversibleKey
				changed.PseudonymKind = "custom"
				if _, err := svc.UpdateRule(ctx, changed.ID, changed.Rule); err != nil {
					t.Fatal(err)
				}
				before, _ := svc.ListRules(ctx)
				if err := svc.store.EnsureSchema(ctx); err != nil {
					t.Fatal(err)
				}
				after, _ := svc.ListRules(ctx)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("restart overwrote a later rule edit")
				}
			})
		}
	}
}

func TestGlobalRestorationMigrationRollsBackAtomically(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.RestoreResponses = true
	oldOptions := writeLegacyMatchingOptions(t, svc.store, settings)
	before, _ := svc.ListRules(ctx)
	_, err := svc.store.db.Exec(`CREATE TRIGGER reject_restore_migration BEFORE UPDATE OF options_json ON privacy_settings BEGIN SELECT RAISE(ABORT, 'interrupted'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.EnsureSchema(ctx); err == nil {
		t.Fatal("expected migration to fail")
	}
	after, _ := svc.ListRules(ctx)
	var options string
	if err := svc.store.db.QueryRow(`SELECT options_json FROM privacy_settings WHERE id = 1`).Scan(&options); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || options != oldOptions {
		t.Fatal("failed migration changed rules or consumed the old setting")
	}
}

func TestGlobalModeControlsOrdinaryAndLegacyRulesWithAllowance(t *testing.T) {
	for _, mode := range []string{ModeDisabled, ModeDetect, ModeRedact} {
		t.Run(mode, func(t *testing.T) {
			svc := protectionService(t)
			ctx := context.Background()
			settings := DefaultSettings()
			settings.Mode = mode
			if err := svc.UpdateSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			for _, rule := range []Rule{
				{Name: "legacy block", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "blocked-value", Action: ActionBlock},
				{Name: "legacy warning", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "warned-value", Action: ActionDetect},
				{Name: "mask", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "masked-value", Action: ActionRedact},
				{Name: "allow", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "allowed-value", Action: ActionAllow},
				{Name: "overlapping block", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "allowed-value", Action: ActionBlock},
			} {
				if _, err := svc.CreateRule(ctx, rule); err != nil {
					t.Fatal(err)
				}
			}
			original := []byte(`{"input":"blocked-value warned-value allowed-value masked-value"}`)
			result, err := svc.Apply(ctx, Request{Path: "/v1/responses", SessionKey: mode}, original)
			if err != nil {
				t.Fatal(err)
			}
			sent, changed := result.SelectBody(original)
			if !strings.Contains(string(sent), "allowed-value") || changed != (mode == ModeRedact) {
				t.Fatalf("mode=%s body=%s", mode, sent)
			}
			if mode == ModeRedact {
				for _, value := range []string{"blocked-value", "warned-value", "masked-value"} {
					if strings.Contains(string(sent), value) {
						t.Fatalf("ordinary rule left %s unchanged", value)
					}
				}
			} else if string(sent) != string(original) {
				t.Fatal("inactive redaction changed request bytes")
			}
			wantHits, wantState := 3, "redacted"
			if mode == ModeDetect {
				wantState = "detected"
			}
			if mode == ModeDisabled {
				wantHits, wantState = 0, "disabled"
			}
			if result.HitCount != wantHits {
				t.Fatalf("mode=%s hits=%d", mode, result.HitCount)
			}
			for _, hit := range result.RuleHits {
				if hit.Action != mode {
					t.Fatalf("mode=%s hit action=%s", mode, hit.Action)
				}
			}
			preview := svc.PreviewText("blocked-value warned-value allowed-value masked-value")
			if preview.Blocked || !preview.Changed || preview.HitCount != 3 || !strings.Contains(preview.Output, "allowed-value") {
				t.Fatalf("preview disagrees with unified rule behavior: %+v", preview)
			}
			entries, err := svc.store.listActivity(ctx)
			if err != nil || len(entries) != 1 || entries[0].State != wantState {
				t.Fatalf("mode=%s activity=%+v err=%v", mode, entries, err)
			}
		})
	}
}

func TestDetectModeObservesStrictScanPoliciesWithoutRejecting(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.ScanMaxBytes = 4
	settings.OnError = OnErrorFailClosed
	settings.OverLimitAction = OverLimitFailClosed
	settings.UnsupportedPathAction = UnsupportedPathFailClosed
	if err := svc.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ path, body string }{
		{"/v1/responses", `{"input":"long text to scan"}`},
		{"/v1/responses", `{"input":`},
		{"/uncovered", `{"input":"raw"}`},
	} {
		result, err := svc.Apply(ctx, Request{Path: item.path}, []byte(item.body))
		if err != nil || result.Changed || string(result.Body) != item.body || result.Restoration != nil {
			t.Fatalf("detect mutated or rejected request: %+v %v", result, err)
		}
		if result.SkippedReason == "" {
			t.Fatal("scan exception was not recorded")
		}
	}
}

func TestRedactionMappingCapacityPassesOriginalWithoutRejecting(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	if err := svc.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRule(ctx, Rule{Name: "large secret", Enabled: true, MatchType: MatchTypePrefix, Pattern: "secret-", Action: ActionRedact}); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"input":"secret-shortvalue secret-` + strings.Repeat("a", 17*1024) + `"}`)
	result, err := svc.Apply(ctx, Request{Path: "/v1/responses"}, body)
	if err != nil {
		t.Fatalf("mapping capacity rejected request: %v", err)
	}
	sent, changed := result.SelectBody(body)
	if changed || result.RedactedBody != nil || string(sent) != string(body) || result.SkippedReason != "mapping_capacity" {
		t.Fatal("mapping capacity sent a partially masked or irreversible body")
	}
	entries, err := svc.store.listActivity(ctx)
	if err != nil || len(entries) != 1 || entries[0].State != "skipped" || entries[0].RestoreSkipped != "mapping_capacity" || entries[0].SkippedReason != "mapping_capacity" {
		t.Fatalf("missing capacity pass-through record: %+v %v", entries, err)
	}
}

func TestLegacyReplacementModesMigrateWithoutChangingRuleIdentity(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{ReplacementPlaceholder, ReplacementPseudonym, ReplacementReversible, ReplacementReversibleKey, ReplacementMaskOnly, ReplacementMaskOnlyKey} {
		t.Run(mode, func(t *testing.T) {
			svc := protectionService(t)
			old, err := svc.store.CreateRule(ctx, Rule{Name: "legacy", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "legacy-private-value", Action: ActionRedact, ReplacementMode: mode, Placeholder: "[CUSTOM]", PseudonymKind: "name", Source: SourceCustom, Priority: 17})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.store.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := svc.store.GetRule(ctx, old.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := ReplacementReversible
			if mode == ReplacementReversibleKey || mode == ReplacementMaskOnly || mode == ReplacementMaskOnlyKey {
				want = mode
			}
			expected := old.Rule
			expected.ReplacementMode = want
			if got.Rule != expected {
				t.Fatalf("migration changed rule identity: %+v", got)
			}
			settings := DefaultSettings()
			settings.Mode = ModeRedact
			if err := svc.UpdateSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			result, err := svc.Apply(ctx, Request{Path: "/v1/responses"}, []byte(`{"input":"legacy-private-value"}`))
			if err != nil {
				t.Fatal(err)
			}
			sent, changed := result.SelectBody(nil)
			if !changed || strings.Contains(string(sent), "legacy-private-value") || len(result.Restoration.values) != 1 {
				t.Fatal("migrated value did not get a restoration mapping")
			}
			if err := svc.store.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			after, _ := svc.store.GetRule(ctx, old.ID)
			if !reflect.DeepEqual(got, after) {
				t.Fatal("restart rewrote a migrated rule")
			}
		})
	}
}
