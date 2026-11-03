package routing

import (
	"context"
	"testing"
	"time"
)

func TestConfiguredCooldownSurvivesPolicyChangesAndHonorsRetryAfter(t *testing.T) {
	runtime := NewPoolRuntime(poolFixture(), nil, nil)
	now := time.Now()
	runtime.now = func() time.Time { return now }
	runtime.SetCooldownPolicy(CooldownPolicy{Initial: 2 * time.Second, Maximum: 5 * time.Second, Multiplier: 1.5})
	failure := Failure{Scope: ScopeEndpoint, Reason: "network_error"}
	for _, delay := range []time.Duration{2 * time.Second, 3 * time.Second, 4500 * time.Millisecond, 5 * time.Second} {
		if err := runtime.Fail(1, 11, "ep", "model", failure, time.Time{}); err != nil {
			t.Fatal(err)
		}
		if got := runtime.Cooldowns()[0].Until.Sub(now); got != delay {
			t.Fatalf("configured cooldown=%v want %v", got, delay)
		}
	}
	until := runtime.Cooldowns()[0].Until
	runtime.SetCooldownPolicy(CooldownPolicy{Initial: time.Second, Maximum: time.Second, Multiplier: 1})
	if !runtime.Cooldowns()[0].Until.Equal(until) {
		t.Fatal("policy update rewrote existing cooldown")
	}
	until = now.Add(time.Hour)
	_ = runtime.Fail(1, 11, "ep", "model", failure, until)
	if !runtime.Cooldowns()[0].Until.Equal(until) {
		t.Fatal("local maximum overrode explicit upstream Retry-After")
	}
	now = until
	release, err := runtime.AcquireHealth(1, 11, "ep", "model")
	if err != nil {
		t.Fatal(err)
	}
	release(false)
	if got := runtime.Cooldowns()[0].Until.Sub(now); got != time.Second {
		t.Fatalf("failed probe ignored configured cooldown: %v", got)
	}
	now = now.Add(time.Second)
	release, err = runtime.AcquireHealth(1, 11, "ep", "model")
	if err != nil {
		t.Fatal(err)
	}
	release(true)
	if len(runtime.Cooldowns()) != 0 {
		t.Fatal("successful recovery retained failed cooldown")
	}
}

func TestRuleDefaultsUpdateInheritedRulesAndReusedTurnsWithoutOverridingExplicitChoices(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].AffinityMode = AffinityTurn
	config.Profiles[0].Rules = []RoutingRule{{ID: 1, Enabled: true, Kind: "model", MatchModel: "*", Mode: ModePrefer, Targets: []RouteTarget{{ChannelID: 2}}}}
	runtime := NewProfileRuntime(config)
	ctx, turn := context.Background(), runtimeTestTurn("one")
	first := runtime.Plan(ctx, 1, "gpt-test", true, turn, false)
	if !first.Decision.FallbackToDefault || len(first.Stages) != 2 {
		t.Fatal("initial model default lost compatibility")
	}
	runtimeTestSend(t, runtime, &first, 0)
	runtime.SetRuleDefaults(RuleDefaults{})
	turn.NewTurn = false
	second := runtime.Plan(ctx, 1, "gpt-test", true, turn, false)
	if !second.Decision.ReusedTurn || second.Decision.FallbackToDefault || len(second.Stages) != 1 {
		t.Fatalf("reused turn ignored updated inherited policy: %+v", second.Decision)
	}
	if !first.Decision.FallbackToDefault || len(first.Stages) != 2 {
		t.Fatal("settings update mutated an already admitted request")
	}
	for _, explicit := range []bool{false, true} {
		config.Profiles[0].Rules[0].FallbackToDefault = &explicit
		config.RuleDefaults = &RuleDefaults{FallbackToDefault: !explicit}
		runtime.Replace(config)
		plan := runtime.Plan(ctx, 1, "gpt-test", true, runtimeTestTurn("new"), true)
		if plan.Decision.FallbackToDefault != explicit {
			t.Fatal("global setting overrode an explicit rule choice")
		}
	}
}

func TestIntentDefaultsReachFallbackChainAndPreserveExplicitOverrides(t *testing.T) {
	deny := false
	profile := RoutingProfile{Intents: []IntentRule{{ID: "first"}, {ID: "second", FallbackToDefault: &deny}}}
	decision := DispatchDecision{Kind: "intent", IntentID: "first", FallbackIntents: []IntentRoute{{IntentID: "second"}}}
	got := applyRuleDefaults(profile, decision, &RuleDefaults{FallbackToDefault: true})
	if !got.FallbackToDefault || got.FallbackIntents[0].FallbackToDefault {
		t.Fatal("intent defaults or explicit fallback override lost")
	}
	got.FallbackIntents[0].Mode = ModeFallback
	if decision.FallbackIntents[0].Mode != "" {
		t.Fatal("resolving defaults changed a cached decision")
	}
}

func TestEachRuleKindResolvesItsSavedDefaultInActualAndPreviewPlans(t *testing.T) {
	for _, kind := range []string{"model", "composite", "mapping", "non_stream", "intent"} {
		for _, enabled := range []bool{false, true} {
			config := runtimeTestConfig()
			config.RuleDefaults = &RuleDefaults{FallbackToDefault: enabled}
			profile := &config.Profiles[0]
			profile.Classifier = &ClassifierConfig{Enabled: true}
			profile.Intents = []IntentRule{{ID: "coding", Enabled: true}}
			profile.Rules = []RoutingRule{{ID: 1, Enabled: true, Kind: kind, MatchModel: "*", Targets: []RouteTarget{{ChannelID: 1, GroupID: 1, UpstreamModel: "mapped"}}}}
			if kind == "intent" {
				profile.Rules[0].IntentID = "coding"
				config.Classify = func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
					return IntentResult{IntentID: "coding", Confidence: 1}, nil
				}
			}
			runtime := NewProfileRuntime(config)
			for _, preview := range []bool{false, true} {
				if preview && kind == "intent" {
					continue // Preview deliberately does not invoke the classifier.
				}
				plan := runtime.Plan(context.Background(), 1, "gpt-test", false, runtimeTestTurn("rule"), preview)
				if plan.Decision.Kind != kind || plan.Decision.FallbackToDefault != enabled || len(plan.Stages) != 1+btoi(enabled) {
					t.Fatalf("kind=%s enabled=%t preview=%t decision=%+v stages=%d", kind, enabled, preview, plan.Decision, len(plan.Stages))
				}
			}
			// Both explicit choices must override either global state for every kind.
			for _, explicit := range []bool{false, true} {
				profile.Rules[0].FallbackToDefault = &explicit
				runtime.Replace(config)
				plan := runtime.Plan(context.Background(), 1, "gpt-test", false, runtimeTestTurn("override"), false)
				if plan.Decision.FallbackToDefault != explicit || len(plan.Stages) != 1+btoi(explicit) {
					t.Fatalf("kind=%s global=%t explicit=%t decision=%+v stages=%d", kind, enabled, explicit, plan.Decision, len(plan.Stages))
				}
			}
		}
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}
