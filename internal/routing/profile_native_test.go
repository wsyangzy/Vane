package routing

import (
	"context"
	"testing"
)

func TestNativePlanPreservesDefaultMembersAndModelThroughRecovery(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.PrepareCandidate = func(c DispatchCandidate, requested, _ string) DispatchCandidate {
		c.Model, c.UpstreamModel = requested, "text-model"
		return c
	}
	cfg.Profiles[0].WaitForRecovery = true
	cfg.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: "model", MatchModel: "*", Enabled: true, Mode: "strict", Targets: []RouteTarget{{ChannelID: 99, UpstreamModel: "wrong-model"}}}}
	runtime := NewProfileRuntime(cfg)
	plan := runtime.PlanNative(context.Background(), 1, "image-model", "images")
	check := func() {
		t.Helper()
		if plan.Error != "" || plan.Decision.Kind != "default" || len(plan.EligibleCandidates) == 0 {
			t.Fatalf("native plan did not use default orchestration: %+v", plan)
		}
		for _, c := range plan.EligibleCandidates {
			if c.ChannelID != 1 || c.Model != "image-model" || c.UpstreamModel != "" || c.Protocol != "images" {
				t.Fatalf("text policy leaked into native plan: %+v", c)
			}
		}
	}
	check()
	if !runtime.Recover(context.Background(), &plan) {
		t.Fatal("native plan did not recover")
	}
	check()
	text := runtime.Plan(context.Background(), 1, "text-model", false, TurnInput{}, true)
	if text.Decision.Kind == "default" {
		t.Fatal("native request mutated normal text routing rules")
	}
}

func TestNativeRequestNeverReadsOrOverwritesTextAffinity(t *testing.T) {
	ctx := context.Background()
	config := runtimeTestConfig()
	config.Profiles[0].Members = append(config.Profiles[0].Members, RoutingMember{ChannelID: 2, GroupID: 3, Enabled: true, Weight: 1})
	config.Profiles[0].AffinityMode = AffinityTurn
	strict := false
	config.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: "model", MatchModel: "gpt-test", Enabled: true, Targets: []RouteTarget{{ChannelID: 2, GroupID: 3}}, FallbackToDefault: &strict}}
	r := NewProfileRuntime(config)
	turn := runtimeTestTurn("same-turn")
	text := r.Plan(ctx, 0, "gpt-test", true, turn, false)
	selected := runtimeTestSend(t, r, &text, 0)
	r.RememberResponseStates(&text, selected, []string{"text-state"})
	textOnly := len(r.affinity)
	withState := WithProfileStateKeys(ctx, []string{"text-state"})
	native := r.PlanNative(withState, 0, "native-model", "images")
	if native.Decision.Kind != "default" || native.Decision.ReusedTurn || native.Model != "native-model" || native.EligibleCandidates[0].ChannelID != 1 {
		t.Fatalf("native request reused text policy/model: %+v", native)
	}
	image := runtimeTestSend(t, r, &native, 0)
	r.RememberResponseStates(&native, image, []string{"native-state"})
	if len(r.affinity) != textOnly {
		t.Fatal("native request added protocol affinity")
	}
	after := r.Plan(ctx, 0, "gpt-test", true, turn, false)
	if after.Decision.RuleID != 1 || !after.Decision.ReusedTurn || after.Model != "gpt-test" || after.EligibleCandidates[0].ChannelID != 2 {
		t.Fatalf("native completion overwrote text routing: %+v", after)
	}
}
