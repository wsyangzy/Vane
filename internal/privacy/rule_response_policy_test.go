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

func TestPerRuleRestorationRespectsModeAndGlobalSwitch(t *testing.T) {
	for _, mode := range []string{ModeDisabled, ModeDetect, ModeRedact} {
		for _, global := range []bool{false, true} {
			for _, restore := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/global=%v/rule=%v", mode, global, restore), func(t *testing.T) {
					svc := protectionService(t)
					ctx := context.Background()
					settings := DefaultSettings()
					settings.Mode, settings.RestoreResponses = mode, global
					if err := svc.UpdateSettings(ctx, settings); err != nil {
						t.Fatal(err)
					}
					matching := ReplacementMaskOnly
					if restore {
						matching = ReplacementReversible
					}
					if _, err := svc.CreateRule(ctx, Rule{Name: "private term", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "private-value", Action: ActionRedact, ReplacementMode: matching}); err != nil {
						t.Fatal(err)
					}
					original := []byte(`{"input":"private-value"}`)
					result, err := svc.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "policy"}, original)
					if err != nil {
						t.Fatal(err)
					}
					sent, changed := result.SelectBody(original)
					var upstream map[string]string
					if err := json.Unmarshal(sent, &upstream); err != nil {
						t.Fatal(err)
					}
					if changed != (mode == ModeRedact) {
						t.Fatal("rule restoration changed outbound mode")
					}
					if mode != ModeRedact && string(sent) != string(original) {
						t.Fatal("inactive redaction modified request bytes")
					}
					if mode == ModeRedact && (!strings.HasPrefix(upstream["input"], "__REDACTED_") || upstream["input"] == "private-value") {
						t.Fatal("disabled restoration bypassed masking")
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
					var downstream map[string]string
					if err := json.Unmarshal(rec.Body.Bytes(), &downstream); err != nil {
						t.Fatal(err)
					}
					want := upstream["input"]
					if global && restore {
						want = "private-value"
					}
					if downstream["output_text"] != want {
						t.Fatal("response ignored global or per-rule restoration")
					}
				})
			}
		}
	}
}

func TestGlobalRestorationSwitchPreservesRulePoliciesAndExistingTokens(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	for _, rule := range []Rule{
		{Name: "restore", Pattern: "restore-value", Action: ActionRedact, ReplacementMode: ReplacementReversible},
		{Name: "mask", Pattern: "mask-value", Action: ActionRedact, ReplacementMode: ReplacementMaskOnly},
		{Name: "public value matcher", Pattern: "public-value", Action: ActionRedact, ReplacementMode: ReplacementReversible},
		{Name: "allow", Pattern: "public-value", Action: ActionAllow},
	} {
		rule.Enabled, rule.MatchType = true, MatchTypeLiteral
		if _, err := svc.CreateRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}
	before, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Path: "/v1/responses", SessionKey: "global-switch"}
	var previous string
	for _, global := range []bool{false, true, false, true} {
		settings.RestoreResponses = global
		if err := svc.UpdateSettings(ctx, settings); err != nil {
			t.Fatal(err)
		}
		after, err := svc.ListRules(ctx)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("global setting overwrote a rule's saved preference: %v", err)
		}
		result, err := svc.Apply(ctx, request, []byte(`{"input":"restore-value mask-value public-value"}`))
		if err != nil {
			t.Fatal(err)
		}
		sent, changed := result.SelectBody(nil)
		var upstream map[string]string
		if err := json.Unmarshal(sent, &upstream); err != nil || !changed {
			t.Fatalf("global restoration switch disabled outbound masking: %v", err)
		}
		parts := strings.Fields(upstream["input"])
		if len(parts) != 3 || !strings.HasPrefix(parts[0], "__REDACTED_") || !strings.HasPrefix(parts[1], "__REDACTED_") || parts[2] != "public-value" {
			t.Fatal("global setting bypassed a mask or allowance")
		}
		if previous != "" && previous != upstream["input"] {
			t.Fatal("global restoration switch changed existing tokens")
		}
		previous = upstream["input"]
		// Later turns can contain only historical tokens; both gates still apply.
		historyBody, _ := json.Marshal(map[string]string{"input": upstream["input"]})
		history, err := svc.Apply(ctx, request, historyBody)
		if err != nil {
			t.Fatal(err)
		}
		history.SelectBody(historyBody)
		for _, current := range []ApplyResult{result, history} {
			rec := httptest.NewRecorder()
			writer, finish := current.WrapResponse(rec)
			writer.Header().Set("Content-Type", "application/json")
			arguments, _ := json.Marshal(map[string]string{"value": upstream["input"]})
			body, _ := json.Marshal(map[string]string{"output_text": upstream["input"], "arguments": string(arguments)})
			if _, err := writer.Write(body); err != nil {
				t.Fatal(err)
			}
			if err := finish(); err != nil {
				t.Fatal(err)
			}
			var response, tool map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(response["arguments"]), &tool); err != nil {
				t.Fatal(err)
			}
			want := upstream["input"]
			if global {
				want = "restore-value " + parts[1] + " public-value"
			}
			if response["output_text"] != want || tool["value"] != want {
				t.Fatalf("global=%v: response ignored the global or rule gate", global)
			}
		}
	}
}

func TestSameValueUnderDifferentRulesDoesNotBypassMaskOnlyPolicy(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	if err := svc.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	for i, prefix := range []string{"A=", "B="} {
		mode := ReplacementReversible
		if i == 1 {
			mode = ReplacementMaskOnly
		}
		if _, err := svc.CreateRule(ctx, Rule{Name: prefix, Enabled: true, MatchType: MatchTypeRegex, Pattern: prefix + `(?P<redact>shared-secret)`, Action: ActionRedact, ReplacementMode: mode}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "mixed"}, []byte(`{"input":"A=shared-secret B=shared-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	sent, changed := result.SelectBody(nil)
	var upstream map[string]string
	if err := json.Unmarshal(sent, &upstream); err != nil || !changed {
		t.Fatal("request was not masked")
	}
	parts := strings.Fields(upstream["input"])
	a, b := strings.TrimPrefix(parts[0], "A="), strings.TrimPrefix(parts[1], "B=")
	if a == b || !strings.HasPrefix(a, "__REDACTED_") || !strings.HasPrefix(b, "__REDACTED_") {
		t.Fatal("distinct rule policies shared one token")
	}
	rec := httptest.NewRecorder()
	writer, finish := result.WrapResponse(rec)
	writer.Header().Set("Content-Type", "application/json")
	args, _ := json.Marshal(map[string]string{"a": a, "b": b})
	response, _ := json.Marshal(map[string]string{"output_text": a + " " + b, "arguments": string(args)})
	if _, err := writer.Write(response); err != nil {
		t.Fatal(err)
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	var downstream map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &downstream); err != nil {
		t.Fatal(err)
	}
	var tool map[string]string
	if err := json.Unmarshal([]byte(downstream["arguments"]), &tool); err != nil {
		t.Fatal(err)
	}
	if downstream["output_text"] != "shared-secret "+b || tool["a"] != "shared-secret" || tool["b"] != b {
		t.Fatal("text or tool restoration bypassed a rule's mask-only setting")
	}
	entries, err := svc.store.listActivity(ctx)
	if err != nil || entries[0].RestoreSkipped != "rule_restoration_disabled" || entries[0].RestoredCount != 2 {
		t.Fatalf("mixed restoration metadata: %+v %v", entries, err)
	}
}

func TestAllowanceCannotRestoreAnEarlierMaskedValue(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	if err := svc.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	rule, err := svc.CreateRule(ctx, Rule{Name: "private term", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "private-value", Action: ActionRedact})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRule(ctx, Rule{Name: "another rule", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "another-value", Action: ActionRedact}); err != nil {
		t.Fatal(err)
	}
	request := Request{Path: "/v1/responses", SessionKey: "allow-history"}
	result, err := svc.Apply(ctx, request, []byte(`{"input":"private-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := result.SelectBody(nil)
	var upstream map[string]string
	if err := json.Unmarshal(sent, &upstream); err != nil {
		t.Fatal(err)
	}
	rule.Action = ActionAllow
	updated, err := svc.UpdateRule(ctx, rule.ID, rule.Rule)
	if err != nil || updated.ReplacementMode != ReplacementMaskOnly {
		t.Fatalf("allowance retained restoration: %+v %v", updated, err)
	}
	body, _ := json.Marshal(map[string]string{"input": upstream["input"]})
	history, err := svc.Apply(ctx, request, body)
	if err != nil {
		t.Fatal(err)
	}
	history.SelectBody(body)
	text, tail := history.Restoration.restoreText(upstream["input"], false, true)
	if text != upstream["input"] || tail != "" {
		t.Fatal("allowance restored an earlier token")
	}
	allowed, err := svc.Apply(ctx, request, []byte(`{"input":"private-value"}`))
	if err != nil || allowed.Changed || allowed.HitCount != 0 {
		t.Fatal("allowance still participated in masking")
	}
}

func TestRuleRestorationEditsPersistAndApplyToHistoricalTokens(t *testing.T) {
	svc := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	if err := svc.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	rule, err := svc.CreateRule(ctx, Rule{Name: "context sensitive", Enabled: true, MatchType: MatchTypeRegex, Pattern: `secret=(?P<redact>private-value)`, Action: ActionRedact})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Path: "/v1/responses", SessionKey: "history"}
	var token string
	for _, mode := range []string{ReplacementReversible, ReplacementMaskOnly, ReplacementReversible} {
		rule.ReplacementMode = mode
		if _, err := svc.UpdateRule(ctx, rule.ID, rule.Rule); err != nil {
			t.Fatal(err)
		}
		if err := svc.store.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		if err := svc.Reload(ctx); err != nil {
			t.Fatal(err)
		}
		stored, err := svc.store.GetRule(ctx, rule.ID)
		if err != nil || stored.ReplacementMode != mode {
			t.Fatal("restart lost per-rule restoration policy")
		}
		result, err := svc.Apply(ctx, request, []byte(`{"input":"secret=private-value"}`))
		if err != nil {
			t.Fatal(err)
		}
		sent, _ := result.SelectBody(nil)
		var upstream map[string]string
		if err := json.Unmarshal(sent, &upstream); err != nil {
			t.Fatal(err)
		}
		next := strings.TrimPrefix(upstream["input"], "secret=")
		if token == "" {
			token = next
		} else if token != next {
			t.Fatal("changing restoration policy changed an existing token")
		}
		body, _ := json.Marshal(map[string]string{"input": token})
		history, err := svc.Apply(ctx, request, body)
		if err != nil {
			t.Fatal(err)
		}
		historySent, changed := history.SelectBody(body)
		if changed || string(historySent) != string(body) {
			t.Fatal("historical token was masked a second time")
		}
		rec := httptest.NewRecorder()
		writer, finish := history.WrapResponse(rec)
		writer.Header().Set("Content-Type", "application/json")
		response, _ := json.Marshal(map[string]string{"output_text": token})
		if _, err := writer.Write(response); err != nil {
			t.Fatal(err)
		}
		if err := finish(); err != nil {
			t.Fatal(err)
		}
		var downstream map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &downstream); err != nil {
			t.Fatal(err)
		}
		want := token
		if mode == ReplacementReversible {
			want = "private-value"
		}
		if downstream["output_text"] != want {
			t.Fatal("current rule policy was not applied to an existing contextual token")
		}
	}
}
