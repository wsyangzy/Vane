package routing

import (
	"context"
	"strings"
	"testing"
)

func TestRegexpModelPatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern, model string
		want           bool
	}{
		{`regex:^gpt-(4|5).*$`, "GPT-5.4", true},
		{` ReGeX: gpt-(4|5).* `, " gpt-4.1 ", true},
		{`regex:gpt-(4|5).*`, "prefix-gpt-5", false},
		{`regex:gpt-(4|5)`, "gpt-5-suffix", false},
		{`regex:gpt-\D+`, "gpt-test", true},
		{`regex:gpt-\D+`, "gpt-123", false},
		{`regex:gpt-\S+`, "gpt-test", true},
		{`regex:gpt-\S+`, "gpt-two words", false},
		{`regex:claude-(sonnet|opus)-[0-9]+`, "claude-opus-5", true},
		{`regex:claude-(sonnet|opus)-[0-9]+`, "claude-haiku-5", false},
		{`regex:gpt-[`, "gpt-test", false},
		{`regex:`, "gpt-test", false},
		{`regex:.*`, "", false},
		{`gpt-(4|5).*`, "gpt-5.4", false},
		{`gpt-5.4`, "gpt-5x4", false},
	} {
		if got := MatchModel(tc.pattern, tc.model); got != tc.want {
			t.Errorf("MatchModel(%q, %q) = %v, want %v", tc.pattern, tc.model, got, tc.want)
		}
	}
	if MatchSupportedModel(`regex:gpt-.*`, "gpt-test") {
		t.Fatal("route regex syntax changed supported_models glob semantics")
	}
}

func TestProfileRejectsInvalidRegexpBeforeSaving(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		for _, kind := range []string{"model", "mapping", "composite"} {
			for _, pattern := range []string{`regex:`, `regex:([`, `regex:gpt-(?=test)`, `regex:(gpt)\1`} {
				p := DefaultProfile(provider)
				p.Name = "regexp validation"
				p.Rules = []RoutingRule{{Kind: kind, MatchModel: pattern}}
				if err := ValidateProfile(&p); err == nil || !strings.Contains(err.Error(), "正则表达式") {
					t.Errorf("%s/%s accepted invalid pattern %q: %v", provider, kind, pattern, err)
				}
			}
		}
	}
	p := DefaultProfile("codex")
	p.Name = "valid regexp"
	p.Rules = []RoutingRule{{Kind: "model", MatchModel: `regex:gpt-(4|5).*`}}
	if err := ValidateProfile(&p); err != nil {
		t.Fatal(err)
	}
}

func TestRegexpModelRulePriority(t *testing.T) {
	targets := []RouteTarget{{ChannelID: 2, GroupID: 3}}
	p := DefaultProfile("codex")
	p.Rules = []RoutingRule{
		{ID: 1, Kind: "model", Enabled: true, MatchModel: "*", Priority: -100, Targets: targets},
		{ID: 2, Kind: "model", Enabled: true, MatchModel: "regex:gpt-.+", Priority: 10, Targets: targets},
		{ID: 3, Kind: "model", Enabled: true, MatchModel: "regex:gpt-(test|other)", Priority: 1, Targets: targets},
		{ID: 4, Kind: "model", Enabled: true, MatchModel: "gpt-*", Priority: 100, Targets: targets},
		{ID: 5, Kind: "model", Enabled: true, MatchModel: "gpt-test", Priority: 1000, Targets: targets},
	}
	for _, want := range []int64{5, 4, 3, 2, 1} {
		if got := ResolveDecision(p, "gpt-test", false); got.RuleID != want {
			t.Fatalf("selected rule %d, want %d", got.RuleID, want)
		}
		p.Rules = p.Rules[:len(p.Rules)-1]
	}
}

func TestRegexpRuleDispatchesForBothProviders(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			cfg := runtimeTestConfig()
			cfg.Provider, cfg.Profiles[0].Provider = provider, provider
			for i := range cfg.Candidates {
				cfg.Candidates[i].Provider = provider
			}
			cfg.Profiles[0].Rules = []RoutingRule{{ID: 9, Kind: "model", Enabled: true, MatchModel: `regex:gpt-(test|other)`, Targets: []RouteTarget{{ChannelID: 2, GroupID: 3}}}}
			runtime := NewProfileRuntime(cfg)
			plan := runtime.Plan(context.Background(), cfg.Profiles[0].ID, "gpt-test", false, TurnInput{}, false)
			if plan.Decision.RuleID != 9 {
				t.Fatalf("regexp rule not selected: %+v", plan.Decision)
			}
			candidate := runtimeTestSend(t, runtime, &plan, 0)
			if candidate.ChannelID != 2 || candidate.GroupID != 3 {
				t.Fatalf("regexp target not used: candidate=%+v", candidate)
			}
		})
	}
}
