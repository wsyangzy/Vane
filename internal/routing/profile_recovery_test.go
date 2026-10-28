package routing

import (
	"context"
	"testing"
	"time"
)

func TestProfileRecoveryWaitsForAllExhaustedWindowsAndKeepsRequest(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].WaitForRecovery = true
	config.Profiles[0].Members = config.Profiles[0].Members[:1]
	now := time.Now()
	first, last := now.Add(time.Minute), now.Add(2*time.Minute)
	full := 100.0
	config.Candidates[0].Quota.Windows = []QuotaSnapshot{
		{ModelScope: "*", UsedPercent: &full, ResetAt: &first, ExpiresAt: now.Add(time.Hour)},
		{ModelScope: "*", UsedPercent: &full, ResetAt: &last, ExpiresAt: now.Add(time.Hour)},
	}
	runtime := NewProfileRuntime(config)
	runtime.now = func() time.Time { return now }
	plan := runtime.Plan(context.Background(), 0, "test", true, runtimeTestTurn("one"), false)
	if next, ok := runtime.NextRecovery(&plan, nil); !ok || !next.Equal(last) {
		t.Fatalf("recovery=%v %v want %v", next, ok, last)
	}
	budget, state, decision := plan.Budget, plan.state, plan.Decision
	now = last.Add(time.Millisecond)
	if !runtime.Recover(context.Background(), &plan) || plan.Budget != budget || plan.state != state || plan.Decision.Kind != decision.Kind {
		t.Fatal("recovery reset request or failed to restore eligibility")
	}
	if next, ok := runtime.NextRecovery(&plan, nil); ok {
		t.Fatalf("ready candidate created another wait: %v", next)
	}
}

func TestProfileRecoveryDoesNotRetryHardFailuresOrStartedResponse(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].WaitForRecovery = true
	config.Profiles[0].Members = config.Profiles[0].Members[:1]
	runtime := NewProfileRuntime(config)
	plan := runtime.Plan(context.Background(), 0, "test", true, runtimeTestTurn("one"), false)
	candidate := plan.EligibleCandidates[0]
	reservation, err := runtime.Reserve(context.Background(), &plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err = reservation.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	reservation.Finish(0, false, Failure{Scope: ScopeModel, Reason: "model_not_found"})
	if _, ok := runtime.NextRecovery(&plan, func(DispatchCandidate) (time.Time, bool) { return time.Now().Add(time.Second), true }); ok {
		t.Fatal("deterministic model failure scheduled recovery")
	}
	if runtime.Recover(context.Background(), &plan) {
		if _, err = runtime.Reserve(context.Background(), &plan, candidate); err == nil {
			t.Fatal("hard failure lost across recovery")
		}
	}
	plan = runtime.Plan(context.Background(), 0, "test", true, runtimeTestTurn("two"), false)
	reservation, err = runtime.Reserve(context.Background(), &plan, plan.EligibleCandidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = reservation.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	reservation.ResponseStarted()
	reservation.Finish(1, false, Failure{Scope: ScopeCandidate, Reason: "response_incomplete"})
	if _, ok := runtime.NextRecovery(&plan, func(DispatchCandidate) (time.Time, bool) { return time.Now().Add(time.Second), true }); ok || runtime.Recover(context.Background(), &plan) {
		t.Fatal("response started allowed recovery")
	}
}

func TestProfileRecoveryDisabledUnknownQuotaAndBudgetDoNotWait(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].WaitForRecovery = true
	config.Profiles[0].Members = config.Profiles[0].Members[:1]
	full := 100.0
	config.Candidates[0].Quota.Windows = []QuotaSnapshot{{ModelScope: "*", UsedPercent: &full, ExpiresAt: time.Now().Add(time.Hour)}}
	runtime := NewProfileRuntime(config)
	plan := runtime.Plan(context.Background(), 0, "test", false, runtimeTestTurn("one"), false)
	if _, ok := runtime.NextRecovery(&plan, nil); ok {
		t.Fatal("unknown reset generated wait")
	}
	plan.Budget = NewBudget(1, time.Second)
	_ = plan.Budget.Begin(context.Background())
	if _, ok := runtime.NextRecovery(&plan, func(DispatchCandidate) (time.Time, bool) { return time.Now().Add(time.Second), true }); ok {
		t.Fatal("exhausted budget generated wait")
	}
}

func TestProfileRecoveryRetainsAdapterStateRejections(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].WaitForRecovery = true
	config.Profiles[0].Members = config.Profiles[0].Members[:1]
	runtime := NewProfileRuntime(config)
	plan := runtime.Plan(context.Background(), 0, "test", true, runtimeTestTurn("one"), false)
	candidate := plan.EligibleCandidates[0]
	plan.Skip(candidate, "state_backend_incompatible")
	if _, ok := runtime.NextRecovery(&plan, func(DispatchCandidate) (time.Time, bool) { return time.Now().Add(time.Second), true }); ok {
		t.Fatal("state-incompatible candidate caused recovery waiting")
	}
	if _, err := runtime.Reserve(context.Background(), &plan, candidate); err == nil || err.Error() != "state_backend_incompatible" {
		t.Fatalf("adapter state rejection was lost at reservation: %v", err)
	}
	if runtime.Recover(context.Background(), &plan) {
		t.Fatal("state-incompatible candidate returned after recovery")
	}
}
