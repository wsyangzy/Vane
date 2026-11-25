package routing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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

func TestProfileRecoveryCanRecheckAfterProbeBlocksFirstWake(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].WaitForRecovery = true
	config.Profiles[0].Members = config.Profiles[0].Members[:1]
	health := HealthState{CoolingDown: true, Reason: "account_probe_in_progress"}
	config.PrepareCandidate = func(candidate DispatchCandidate, model, override string) DispatchCandidate {
		candidate = CopyCandidateForModel(candidate, model, override)
		candidate.Health = health
		return candidate
	}
	runtime := NewProfileRuntime(config)
	plan := runtime.Plan(context.Background(), 0, "test", false, runtimeTestTurn("probe-wake"), false)
	budget, state, decision := plan.Budget, plan.state, plan.Decision
	if runtime.Recover(context.Background(), &plan) {
		t.Fatal("an in-flight probe admitted another request")
	}
	health = HealthState{}
	if !runtime.Recover(context.Background(), &plan) {
		t.Fatal("a blocked wake permanently consumed recovery before any request was sent")
	}
	if plan.Budget != budget || plan.state != state || plan.Decision.Kind != decision.Kind || plan.Trace().Attempts != 0 {
		t.Fatal("rechecking a probe reset the request or fabricated an attempt")
	}
	reservation, err := runtime.Reserve(context.Background(), &plan, plan.EligibleCandidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	reservation.Finish(1, true, Failure{})
	if trace := plan.Trace(); trace.Attempts != 1 || !trace.Successful {
		t.Fatalf("recovered request did not settle once: attempts=%d success=%t", trace.Attempts, trace.Successful)
	}
}

func TestProfileRecoveryWaitersResumeWhenProbeOpensBeforeWait(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].WaitForRecovery = true
	config.Profiles[0].Members = config.Profiles[0].Members[:1]
	config.Candidates = config.Candidates[:1]
	pool := NewPoolRuntime(PoolConfig{Accounts: []AccountState{{ChannelID: 1, GroupID: 1, Identity: "account"}}}, []Cooldown{{Identity: "account", Endpoint: "a", Reason: "network", Until: time.Now().Add(-time.Second), Failures: 1}}, nil)
	release, err := pool.AcquireHealth(1, 1, "a", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer release(false)
	config.PrepareCandidate = func(candidate DispatchCandidate, model, override string) DispatchCandidate {
		candidate = CopyCandidateForModel(candidate, model, override)
		if allowed, reason := pool.HealthAllowed(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model); !allowed {
			candidate.Health = HealthState{CoolingDown: true, Reason: reason}
		}
		return candidate
	}
	runtime := NewProfileRuntime(config)
	plans := make([]DispatchPlan, 4)
	for i := range plans {
		plans[i] = runtime.Plan(context.Background(), 0, "test", false, runtimeTestTurn(fmt.Sprint(i)), false)
		if len(plans[i].EligibleCandidates) != 0 {
			t.Fatal("a waiting request bypassed the single recovery probe")
		}
	}
	release(true)
	var waiters sync.WaitGroup
	for i := range plans {
		waiters.Go(func() {
			plan := &plans[i]
			recovered, err := runtime.WaitForRecovery(context.Background(), plan, func(candidate DispatchCandidate) (time.Time, bool) {
				return pool.HealthReadyAt(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model)
			})
			if err != nil || !recovered {
				t.Errorf("probe completion between planning and waiting was lost: recovered=%t err=%v", recovered, err)
				return
			}
			candidate := plan.EligibleCandidates[0]
			reservation, err := runtime.Reserve(context.Background(), plan, candidate)
			if err != nil {
				t.Error(err)
				return
			}
			healthRelease, err := pool.AcquireHealth(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model)
			if err != nil {
				reservation.Finish(0, false, Failure{})
				t.Error(err)
				return
			}
			if err := reservation.Begin(context.Background()); err != nil {
				healthRelease(false)
				reservation.Finish(0, false, Failure{})
				t.Error(err)
				return
			}
			healthRelease(true)
			reservation.Finish(1, true, Failure{})
		})
	}
	waiters.Wait()
	for i := range plans {
		if trace := plans[i].Trace(); trace.Attempts != 1 || !trace.Successful || trace.WaitMS != 0 || trace.NextRecoveryAt != nil {
			t.Errorf("waiter %d did not settle once without another cooldown: attempts=%d success=%t wait=%d recovery=%v", i, trace.Attempts, trace.Successful, trace.WaitMS, trace.NextRecoveryAt)
		}
	}
}

func TestProfileRecoveryWaitKeepsQuotaResetAndCancellation(t *testing.T) {
	for _, scenario := range []string{"quota_reset", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			config := runtimeTestConfig()
			config.Profiles[0].WaitForRecovery = true
			config.Profiles[0].Members = config.Profiles[0].Members[:1]
			config.Candidates = config.Candidates[:1]
			var probing atomic.Bool
			probing.Store(true)
			config.PrepareCandidate = func(candidate DispatchCandidate, model, override string) DispatchCandidate {
				candidate = CopyCandidateForModel(candidate, model, override)
				if probing.Load() {
					candidate.Health = HealthState{CoolingDown: true, Reason: "account_probe_in_progress"}
				}
				return candidate
			}
			reset := time.Now().Add(60 * time.Millisecond)
			if scenario == "quota_reset" {
				full := 100.0
				config.Candidates[0].Quota.Windows = []QuotaSnapshot{{ModelScope: "*", UsedPercent: &full, ResetAt: &reset, ExpiresAt: reset.Add(time.Hour)}}
			}
			runtime := NewProfileRuntime(config)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			plan := runtime.Plan(ctx, 0, "test", false, TurnInput{}, false)
			plan.Budget = NewBudget(2, time.Second)
			timer := time.AfterFunc(20*time.Millisecond, func() {
				if scenario == "quota_reset" {
					probing.Store(false)
				} else {
					cancel()
				}
			})
			defer timer.Stop()
			recovered, err := runtime.WaitForRecovery(ctx, &plan, func(DispatchCandidate) (time.Time, bool) {
				return time.Time{}, !probing.Load()
			})
			trace := plan.Trace()
			if trace.Attempts != 0 || trace.WaitMS <= 0 {
				t.Fatalf("waiting fabricated an attempt or lost wait time: attempts=%d wait=%d", trace.Attempts, trace.WaitMS)
			}
			if scenario == "quota_reset" {
				// The budget converts a delay back to an instant using a later clock read.
				if err != nil || !recovered || trace.NextRecoveryAt == nil || trace.NextRecoveryAt.Before(reset) || trace.NextRecoveryAt.After(reset.Add(10*time.Millisecond)) || time.Now().Before(reset) {
					t.Fatalf("probe completion bypassed the quota reset: recovered=%t err=%v reset=%v", recovered, err, trace.NextRecoveryAt)
				}
			} else if recovered || !errors.Is(err, context.Canceled) || trace.NextRecoveryAt != nil {
				t.Fatalf("probe wait ignored cancellation: recovered=%t err=%v reset=%v", recovered, err, trace.NextRecoveryAt)
			}
		})
	}
}
