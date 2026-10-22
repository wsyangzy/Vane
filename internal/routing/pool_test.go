package routing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func poolFixture() PoolConfig {
	return PoolConfig{Pools: []Pool{{ID: 1, Provider: "codex", Name: "main", Default: true, Strategy: PoolRoundRobin, Members: []PoolMember{{1, 11, TierPrimary}, {2, 22, TierPrimary}, {3, 33, TierBackup}}}, {ID: 2, Provider: "codex", Name: "other", Strategy: PoolSequential, Members: []PoolMember{{4, 44, TierPrimary}}}}, Accounts: []AccountState{{1, 11, "one", true, false}, {2, 22, "two", true, false}, {3, 33, "three", true, false}, {4, 44, "four", true, false}}}
}

func TestPoolOldRecoveryCannotReleaseNewProbe(t *testing.T) {
	r := NewPoolRuntime(poolFixture(), nil, nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	fail := Failure{Scope: ScopeEndpoint, Reason: "network_error"}
	_ = r.Fail(1, 11, "ep", "m", fail, time.Time{})
	now = now.Add(time.Minute)
	old, err := r.Acquire(1, 11, "ep", "m")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Fail(1, 11, "ep", "m", fail, time.Time{})
	now = now.Add(2 * time.Minute)
	current, err := r.Acquire(1, 11, "ep", "m")
	if err != nil {
		t.Fatal(err)
	}
	old(true)
	if len(r.Cooldowns()) != 1 || !r.Cooldowns()[0].Probing {
		t.Fatal("old lease cleared the new probe")
	}
	if _, err = r.Acquire(1, 11, "ep", "m"); err == nil {
		t.Fatal("concurrent probe admitted")
	}
	current(true)
	if len(r.Cooldowns()) != 0 {
		t.Fatal("current protocol success failed to recover")
	}
}

func TestPoolHelpersSkipGenerationRulesAndQuotaWaitUsesProviderState(t *testing.T) {
	cfg := poolFixture()
	cfg.Pools[0].Rules = []ModelRule{{MatchModel: "*", Mode: ModePrefer, Targets: []Target{{PoolID: 2}}}}
	cfg.Pools[0].NonStream = &NonStreamRoute{Mode: ModePrefer, Targets: []Target{{PoolID: 2}}}
	r := NewPoolRuntime(cfg, nil, nil)
	if plan := r.PlanDefault(context.Background(), "codex"); plan.Stages[0].GroupID == 44 || plan.Decision.Routed() {
		t.Fatal("helper ran generation rules")
	}
	stages := []PoolStage{{PoolMember: PoolMember{ChannelID: 1, GroupID: 11, Tier: TierPrimary}, Endpoint: "ep", UpstreamModel: "m"}}
	at := time.Now().Add(time.Second)
	next, ok := r.NextRecoveryWith(stages, "m", func(PoolStage) (time.Time, bool) { return at, true })
	if !ok || !next.Equal(at) {
		t.Fatal("provider quota recovery was not reused")
	}
	if len(r.Cooldowns()) != 0 {
		t.Fatal("provider quota copied into generic cooldown")
	}
	cfg.Accounts[0].Enabled = false
	r.Replace(cfg)
	if _, ok = r.NextRecoveryWith(stages, "m", func(PoolStage) (time.Time, bool) { return at, true }); ok {
		t.Fatal("waited for disabled account")
	}
}

func TestPoolOrderingAndNonRecursiveRules(t *testing.T) {
	cfg := poolFixture()
	cfg.Pools[0].Rules = []ModelRule{{Name: "route", MatchModel: "gpt-*", Mode: ModePrefer, Targets: []Target{{PoolID: 2}}}}
	cfg.Pools[1].Rules = []ModelRule{{Name: "do not recurse", MatchModel: "*", Mode: ModePrefer, Targets: []Target{{PoolID: 1}}}}
	r := NewPoolRuntime(cfg, nil, nil)
	plan := r.Plan(context.Background(), "codex", "gpt-5", true)
	if len(plan.Stages) != 4 || plan.Stages[0].GroupID != 44 || plan.Stages[1].GroupID != 11 || plan.Stages[3].Tier != TierBackup {
		t.Fatalf("unexpected stages: %+v", plan.Stages)
	}
	plan = r.Plan(context.Background(), "codex", "gpt-5", true)
	if plan.Stages[1].GroupID != 22 || plan.Stages[3].GroupID != 33 {
		t.Fatalf("round robin crossed tiers: %+v", plan.Stages)
	}
}

func TestPoolConcurrentRoundRobin(t *testing.T) {
	r := NewPoolRuntime(poolFixture(), nil, nil)
	var counts [2]atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := r.Plan(context.Background(), "codex", "other", true)
			if p.Stages[0].GroupID == 11 {
				counts[0].Add(1)
			} else {
				counts[1].Add(1)
			}
		}()
	}
	wg.Wait()
	if counts[0].Load() != 50 || counts[1].Load() != 50 {
		t.Fatalf("unbalanced rotation: %d/%d", counts[0].Load(), counts[1].Load())
	}
}

func TestPoolCooldownScopeAndSingleProbe(t *testing.T) {
	r := NewPoolRuntime(poolFixture(), nil, nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	if err := r.Fail(1, 11, "ep1", "gpt-5", Failure{Scope: ScopeModel, Reason: "model_not_found"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.Allowed(1, 11, "ep1", "gpt-5"); ok {
		t.Fatal("cooling candidate allowed")
	}
	if ok, _ := r.Allowed(1, 11, "ep1", "other"); !ok {
		t.Fatal("model failure disabled other model")
	}
	if ok, _ := r.Allowed(1, 11, "ep2", "gpt-5"); !ok {
		t.Fatal("model failure disabled another endpoint")
	}
	now = now.Add(31 * time.Second)
	finish, err := r.Acquire(1, 11, "ep1", "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Acquire(1, 11, "ep1", "gpt-5"); err == nil {
		t.Fatal("multiple recovery probes admitted")
	}
	finish(true)
	if len(r.Cooldowns()) != 0 {
		t.Fatal("successful probe did not recover")
	}
}

func TestPoolDisabledRepairAndSharedIdentity(t *testing.T) {
	cfg := poolFixture()
	cfg.Accounts[1].Identity = cfg.Accounts[0].Identity
	r := NewPoolRuntime(cfg, nil, nil)
	_ = r.Fail(1, 11, "ep", "gpt", Failure{Scope: ScopeCredential, Reason: "credential_rejected"}, time.Time{})
	if ok, _ := r.Allowed(2, 22, "other", "other"); ok {
		t.Fatal("shared account bypassed repair")
	}
	if _, ok := r.NextRecovery([]PoolStage{{PoolMember: PoolMember{2, 22, TierPrimary}}}, "gpt"); ok {
		t.Fatal("auth repair must not wait")
	}
	cfg.Accounts[2].Enabled = false
	r.Replace(cfg)
	if _, err := r.Acquire(3, 33, "ep", "gpt"); err == nil {
		t.Fatal("disabled account admitted")
	}
}

func TestPoolKeyAndStaleCaller(t *testing.T) {
	cfg := poolFixture()
	hash := sha256.Sum256([]byte("secret"))
	cfg.Pools[0].KeyHash = hex.EncodeToString(hash[:])
	cfg.Pools[0].Revision = 1
	r := NewPoolRuntime(cfg, nil, nil)
	c, err := r.Authenticate("codex", "secret")
	if err != nil || !r.ValidCaller(c) {
		t.Fatal("valid caller rejected")
	}
	if _, err = r.Authenticate("codex", "wrong"); err == nil {
		t.Fatal("wrong key accepted")
	}
	cfg.Pools[0].Revision++
	r.Replace(cfg)
	if r.ValidCaller(c) {
		t.Fatal("stale pool authorization accepted")
	}
	if p := r.Plan(WithPoolCaller(context.Background(), c), "codex", "gpt", true); p.Pool.ID != 0 {
		t.Fatal("stale caller fell through to default pool")
	}
}

func TestPoolRecoveryWaitUsesBudget(t *testing.T) {
	r := NewPoolRuntime(poolFixture(), nil, nil)
	_ = r.Fail(1, 11, "ep", "gpt", Failure{Scope: ScopeEndpoint, Reason: "network_error"}, time.Now().Add(time.Hour))
	next, ok := r.NextRecovery([]PoolStage{{PoolMember: PoolMember{1, 11, TierPrimary}, Endpoint: "ep"}}, "gpt")
	if !ok {
		t.Fatal("missing recovery")
	}
	budget := NewBudget(10, 10*time.Millisecond)
	if err := budget.Wait(context.Background(), time.Until(next)); err != ErrBudgetExhausted {
		t.Fatalf("unbounded wait: %v", err)
	}
}

func TestPoolIncompleteResponseDoesNotRecover(t *testing.T) {
	r := NewPoolRuntime(poolFixture(), nil, nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	_ = r.Fail(1, 11, "ep", "gpt", Failure{Scope: ScopeEndpoint, Reason: "network"}, time.Time{})
	now = now.Add(time.Minute)
	_, attempt, err := AdmitPool(context.Background(), r, 1, 11, "ep", "gpt")
	if err != nil {
		t.Fatal(err)
	}
	body := ObservePoolBody(io.NopCloser(strings.NewReader("data: done\n")), attempt, true, func(data []byte) (bool, Failure) { return strings.Contains(string(data), "done"), Failure{} }, time.Time{})
	_ = body.Close()
	if len(r.Cooldowns()) == 0 {
		t.Fatal("closed incomplete response recovered")
	}
}
