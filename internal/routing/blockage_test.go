package routing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDailyLimitCanRecoverWithoutSwitchingGroup(t *testing.T) {
	config := runtimeTestConfig()
	config.Candidates = config.Candidates[:1]
	config.Profiles[0].WaitForRecovery = true
	reset := time.Now().Add(time.Hour)
	blocked := true
	config.CheckAdmission = func(DispatchCandidate) string {
		if blocked {
			return "daily_cost_limit_exceeded"
		}
		return ""
	}
	config.DescribeBlockage = func(c DispatchCandidate, reason string) *Blockage {
		return &Blockage{Reason: reason, Recovery: "automatic", RecoverAt: &reset, ChannelID: c.ChannelID, GroupID: c.GroupID}
	}
	runtime := NewProfileRuntime(config)
	plan := runtime.Plan(context.Background(), 0, "test", false, TurnInput{}, false)
	if len(plan.EligibleCandidates) != 0 {
		t.Fatal("daily cost limit admitted a candidate")
	}
	ready, ok := runtime.NextRecovery(&plan, nil)
	if !ok || !ready.Equal(reset) {
		t.Fatalf("daily reset missing from recovery: %v %v", ready, ok)
	}
	blocked = false
	after := runtime.Plan(context.Background(), 0, "test", false, TurnInput{}, false)
	if len(after.EligibleCandidates) != 1 || after.EligibleCandidates[0].GroupID != config.Candidates[0].GroupID {
		t.Fatal("recovered group required a profile/account switch")
	}
	if plan.Trace().Candidates[0].Reason != "daily_cost_limit_exceeded" {
		t.Fatal("new availability rewrote the historical failure")
	}
}

func TestExplicitRetryPreservesBackoffAndScopesAndUsesSingleLease(t *testing.T) {
	now := time.Now()
	runtime := NewPoolRuntime(PoolConfig{Accounts: []AccountState{{ChannelID: 1, GroupID: 2, Identity: "group/2", Enabled: true}}}, nil, nil)
	runtime.now = func() time.Time { return now }
	for _, endpoint := range []string{"one", "other"} {
		if err := runtime.Fail(1, 2, endpoint, "model", Failure{Scope: ScopeCandidate, Reason: "network_error"}, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.RequestRetry(1, 2, "one", "model"); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := runtime.HealthAllowed(1, 2, "other", "model"); allowed {
		t.Fatal("retry released a different endpoint")
	}
	release, err := runtime.AcquireHealth(1, 2, "one", "model")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.RequestRetry(1, 2, "one", "model"); err == nil {
		t.Fatal("retry interrupted an active half-open lease")
	}
	if _, err := runtime.AcquireHealth(1, 2, "one", "model"); err == nil {
		t.Fatal("multiple retries acquired the same gate")
	}
	release(false)
	if err := runtime.Fail(1, 2, "one", "model", Failure{Scope: ScopeCandidate, Reason: "network_error"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	for _, state := range runtime.Cooldowns() {
		if state.Endpoint == "one" && state.Failures != 2 {
			t.Fatalf("retry reset failure history: %+v", state)
		}
	}
}

func TestExplicitRetryRollsBackFailedPersistence(t *testing.T) {
	failure := errors.New("storage failed")
	runtime := NewPoolRuntime(PoolConfig{Accounts: []AccountState{{ChannelID: 1, GroupID: 2, Identity: "group/2"}}}, []Cooldown{{Identity: "group/2", Repair: true, Until: time.Now().Add(time.Hour)}}, func([]Cooldown) error { return failure })
	if err := runtime.RequestRetry(1, 2, "one", "model"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if allowed, reason := runtime.HealthAllowed(1, 2, "one", "model"); allowed || reason != "account_repair_required" {
		t.Fatalf("failed save changed health: %v %s", allowed, reason)
	}
}

func TestQuotaRecoveryOnlyUsesApplicableExhaustedWindows(t *testing.T) {
	now := time.Now()
	soon, later := now.Add(time.Minute), now.Add(time.Hour)
	used, unused := 100.0, 20.0
	candidate := DispatchCandidate{Provider: "codex", Model: "model", Quota: QuotaView{Windows: []QuotaSnapshot{
		{ModelScope: "different", UsedPercent: &used, Confidence: "exact", ExpiresAt: later},
		{UsedPercent: &unused, Confidence: "exact", ExpiresAt: later},
		{UsedPercent: &used, ResetAt: &soon, Confidence: "exact", ExpiresAt: later},
	}}}
	if at, ok := QuotaRecoveryAt(candidate, now); !ok || !at.Equal(soon) {
		t.Fatalf("irrelevant windows prevented known recovery: %v %v", at, ok)
	}
	candidate.Quota.Windows = append(candidate.Quota.Windows, QuotaSnapshot{UsedPercent: &used, Confidence: "exact", ExpiresAt: later})
	if _, ok := QuotaRecoveryAt(candidate, now); ok {
		t.Fatal("an exhausted window without a reset manufactured automatic recovery")
	}
}
