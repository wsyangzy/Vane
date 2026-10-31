package privacy

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Write the old storage format, rather than exercising the retired settings API.
func writeLegacyMatchingOptions(t *testing.T, store *Store, settings Settings) string {
	t.Helper()
	options, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`UPDATE privacy_settings SET mode = ?, scan_max_bytes = ?, over_limit_action = ?,
		on_error = ?, unsupported_path_action = ?, options_json = ? WHERE id = 1`, settings.Mode,
		settings.ScanMaxBytes, settings.OverLimitAction, settings.OnError, settings.UnsupportedPathAction, string(options))
	if err != nil {
		t.Fatal(err)
	}
	return string(options)
}

func migrateProtectionSettings(t *testing.T, svc *Service, settings Settings) {
	t.Helper()
	writeLegacyMatchingOptions(t, svc.store, settings)
	if err := svc.store.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMatchingOptionsMigrationPreservesEffectiveRulesAndUserEdits(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	custom, err := svc.CreateRule(ctx, Rule{Name: "内部文本", Enabled: true, Priority: -100, MatchType: MatchTypeLiteral,
		Pattern: "Acme", Action: ActionRedact, ReplacementMode: ReplacementPlaceholder, Placeholder: "[内部]"})
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	settings.Categories = map[string]bool{"personal": false, "secrets": false, "terms": true}
	settings.Actions = map[string]string{"phone": ActionBlock, "custom": ActionDetect}
	settings.Words = []string{"Acme", "ACME"}
	settings.Allow = []string{"public@example.com"}
	settings.RestoreResponses, settings.KeepKeyFormat = true, true
	migrateProtectionSettings(t, svc, settings)

	got, err := svc.GetSettings(ctx)
	if err != nil || hasLegacyMatchingOptions(got) || !got.RestoreResponses || got.KeepKeyFormat || got.Mode != ModeRedact {
		t.Fatalf("runtime settings lost or legacy controls retained: %+v %v", got, err)
	}
	rules, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var words, allows int
	for _, rule := range rules {
		switch {
		case rule.ID < 0:
			words++
			if !rule.Enabled || rule.Action != ActionRedact || rule.ReplacementMode != ReplacementReversibleKey || rule.Pattern != "Acme" && rule.Pattern != "ACME" {
				t.Fatalf("term configuration lost: %+v", rule)
			}
		case rule.Action == ActionAllow:
			allows++
			if rule.Name == rule.Pattern || !rule.Enabled {
				t.Fatal("allow value was placed in its loggable name or disabled")
			}
		case rule.ID == custom.ID:
			if rule.Enabled || rule.Action != ActionRedact {
				t.Fatal("custom category was lost or legacy warning was not migrated")
			}
		case rule.Source == SourceBuiltin:
			if rule.Enabled || rule.Action != ActionRedact {
				t.Fatalf("builtin category/action was not merged: %+v", rule)
			}
		}
	}
	if words != 2 || allows != 1 {
		t.Fatalf("missing migrated rules: words=%d allows=%d", words, allows)
	}
	preview := svc.PreviewText("Acme ACME acme person@example.com")
	if preview.HitCount != 2 || !strings.HasSuffix(preview.Output, "acme person@example.com") {
		t.Fatalf("case-sensitive terms or disabled PII changed: %+v", preview)
	}
	// Rules are the only control now: enabling a previously excluded rule must work.
	custom.Enabled, custom.Action = true, ActionBlock
	if _, err := svc.UpdateRule(ctx, custom.ID, custom.Rule); err != nil {
		t.Fatal(err)
	}
	if preview := svc.PreviewText("Acme"); preview.Blocked || !preview.Changed {
		t.Fatal("the enabled legacy rule did not follow redaction mode")
	}
	// Persisted former synthetic rules support normal edit, toggle and delete.
	if _, err := svc.SetRulesEnabled(ctx, []int64{-1}, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteRule(ctx, -2); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.ListRules(ctx)
	if err := svc.store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.ListRules(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart duplicated migrated rules or undid user edits")
	}
	got.Mode = ModeDetect
	if err := svc.UpdateSettings(ctx, got); err != nil {
		t.Fatal(err)
	}
	after, _ = svc.ListRules(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("saving runtime settings changed rules")
	}
}

func TestMatchingOptionsMigrationPreservesOverlappingTermOrder(t *testing.T) {
	svc := protectionService(t)
	settings := DefaultSettings()
	settings.Words = []string{"Acme", "Acme Labs", "a.cme"}
	settings.WordsAnyCase = true
	settings.Actions = map[string]string{"term": ActionRedact}
	_, err := svc.CreateRule(context.Background(), Rule{Name: "已有重叠规则", Enabled: true, Priority: -100,
		MatchType: MatchTypeLiteral, Pattern: "Acme", Action: ActionDetect})
	if err != nil {
		t.Fatal(err)
	}
	migrateProtectionSettings(t, svc, settings)
	result := svc.PreviewText("ACME LABS A.CME aXcme")
	if strings.Count(result.Output, "__REDACTED_") != 2 || !strings.HasSuffix(result.Output, " aXcme") || result.HitCount != 2 {
		t.Fatalf("term precedence, quoting, or case changed: %+v", result)
	}
}

func TestMatchingOptionsMigrationRollsBackAndCanRetry(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	settings := DefaultSettings()
	settings.Categories = map[string]bool{"personal": false}
	settings.Actions = map[string]string{"email": ActionBlock}
	settings.Words, settings.Allow = []string{"private-project"}, []string{"public@example.com"}
	options := writeLegacyMatchingOptions(t, store, settings)
	before, _ := store.ListRules(ctx)
	if _, err := store.db.Exec(`CREATE TRIGGER reject_privacy_migration BEFORE UPDATE OF options_json ON privacy_settings
		BEGIN SELECT RAISE(ABORT, 'migration interrupted'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSchema(ctx); err == nil {
		t.Fatal("expected migration failure")
	}
	after, _ := store.ListRules(ctx)
	var retained string
	if err := store.db.QueryRow(`SELECT options_json FROM privacy_settings WHERE id = 1`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != options || !reflect.DeepEqual(before, after) {
		t.Fatal("failed migration partially changed rules or lost options")
	}
	if _, err := store.db.Exec(`DROP TRIGGER reject_privacy_migration`); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	got, err := store.LoadSettings(ctx)
	if err != nil || hasLegacyMatchingOptions(got) {
		t.Fatalf("retry left old matching controls: %+v %v", got, err)
	}
}

func TestRuntimeSettingsRejectLegacyMatchingControls(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	before, _ := svc.ListRules(ctx)
	version := svc.SnapshotVersion()
	for _, legacy := range []Settings{
		{KeepKeyFormat: true},
		{Categories: map[string]bool{"personal": false}}, {Words: []string{"private-project"}},
		{WordsAnyCase: true}, {Allow: []string{"public@example.com"}}, {Actions: map[string]string{"email": ActionBlock}},
	} {
		settings := DefaultSettings()
		settings.Categories, settings.Words, settings.WordsAnyCase, settings.Allow, settings.Actions = legacy.Categories, legacy.Words, legacy.WordsAnyCase, legacy.Allow, legacy.Actions
		settings.RestoreResponses, settings.KeepKeyFormat = legacy.RestoreResponses, legacy.KeepKeyFormat
		if err := svc.UpdateSettings(ctx, settings); err == nil || !strings.Contains(err.Error(), "防护规则") {
			t.Fatalf("retired matching settings were accepted: %v", err)
		}
	}
	after, _ := svc.ListRules(ctx)
	if !reflect.DeepEqual(before, after) || svc.SnapshotVersion() != version {
		t.Fatal("rejected settings changed rules or the snapshot")
	}
}

func TestAllowRulesOnlyExemptWholeMatchesAndHonorToggle(t *testing.T) {
	publicValue := "Public" + "@" + "example.com"
	for _, mode := range []string{ModeDetect, ModeRedact} {
		for _, matching := range []struct{ matchType, pattern string }{
			{MatchTypeLiteral, strings.ToUpper(publicValue)},
			{MatchTypeRegex, `(?i)public@example\.com`},
		} {
			t.Run(mode+"/"+matching.matchType, func(t *testing.T) {
				svc := protectionService(t)
				ctx := context.Background()
				settings := DefaultSettings()
				settings.Mode = mode
				if err := svc.UpdateSettings(ctx, settings); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.CreateRule(ctx, Rule{Name: "邮箱匹配", Enabled: true, MatchType: MatchTypeBuiltin, Pattern: "builtin:email", Action: ActionRedact}); err != nil {
					t.Fatal(err)
				}
				allow, err := svc.CreateRule(ctx, Rule{Name: "公开示例", Enabled: true, MatchType: matching.matchType, Pattern: matching.pattern, Action: ActionAllow})
				if err != nil {
					t.Fatal(err)
				}
				body, _ := json.Marshal(map[string]string{"input": publicValue, "model": "unchanged"})
				result, err := svc.Apply(ctx, Request{Path: "/v1/responses"}, body)
				if err != nil || result.HitCount != 0 || string(result.Body) != string(body) || len(result.RuleHits) != 0 {
					t.Fatalf("whole-match allowance changed bytes or was counted as a hit: %+v %v", result, err)
				}
				for _, value := range []string{"x" + publicValue, publicValue + "x", "Private" + "@" + "example.com"} {
					preview := svc.PreviewText(value)
					if preview.Blocked || !preview.Changed || preview.HitCount != 1 || preview.Output == value {
						t.Fatal("allowed a different or partial value")
					}
					request, _ := json.Marshal(map[string]string{"input": value})
					actual, err := svc.Apply(ctx, Request{Path: "/v1/responses"}, request)
					if err != nil || actual.Changed != (mode == ModeRedact) || actual.HitCount != 1 {
						t.Fatal("request path disagrees with preview")
					}
				}
				allow.Enabled = false
				if _, err := svc.UpdateRule(ctx, allow.ID, allow.Rule); err != nil {
					t.Fatal(err)
				}
				preview := svc.PreviewText(publicValue)
				if preview.Blocked || !preview.Changed || preview.HitCount != 1 {
					t.Fatal("disabled allow rule remained active")
				}
			})
		}
	}
}
