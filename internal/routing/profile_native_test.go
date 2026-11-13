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
