package routing

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func poolFixture() PoolConfig {
	return PoolConfig{Pools: []Pool{{ID: 1, Provider: "codex", Name: "main", Default: true, Strategy: PoolRoundRobin, Members: []PoolMember{{1, 11, TierPrimary}, {2, 22, TierPrimary}, {3, 33, TierBackup}}}, {ID: 2, Provider: "codex", Name: "other", Strategy: PoolSequential, Members: []PoolMember{{4, 44, TierPrimary}}}}, Accounts: []AccountState{{1, 11, "one", true, false}, {2, 22, "two", true, false}, {3, 33, "three", true, false}, {4, 44, "four", true, false}}}
}

func TestProfileHealthIgnoresLegacySwitchButPreservesRecovery(t *testing.T) {
	now := time.Now()
	r := NewPoolRuntime(PoolConfig{Accounts: []AccountState{{ChannelID: 1, GroupID: 11, Identity: "shared", Enabled: false}}}, nil, nil)
	r.now = func() time.Time { return now }
	if allowed, _ := r.Allowed(1, 11, "ep", "m"); allowed {
		t.Fatal("legacy account switch was ignored")
	}
	if _, err := r.Acquire(1, 11, "ep", "m"); err == nil {
		t.Fatal("legacy sends must stay disabled")
	}
	if allowed, reason := r.HealthAllowed(1, 11, "ep", "m"); !allowed {
		t.Fatal(reason)
	}
	if _, err := r.AcquireHealth(9, 99, "ep", "m"); err == nil {
		t.Fatal("missing resources must not be admitted")
	}
	if err := r.Fail(1, 11, "ep", "m", Failure{Scope: ScopeEndpoint, Reason: "unavailable"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if allowed, reason := r.HealthAllowed(1, 11, "ep", "m"); allowed || reason != "account_cooling_down" {
		t.Fatalf("cooldown bypassed: %v %s", allowed, reason)
	}
	if _, err := r.AcquireHealth(1, 11, "ep", "m"); err == nil {
		t.Fatal("cooldown must prevent a unified send")
	}
	ready, valid := r.HealthReadyAt(1, 11, "ep", "m")
	if !valid || !ready.After(now) {
		t.Fatal("unified recovery must ignore legacy participation")
	}
	now = ready.Add(time.Second)
	release, err := r.AcquireHealth(1, 11, "ep", "m")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AcquireHealth(1, 11, "ep", "m"); err == nil {
		t.Fatal("only one recovery probe may run")
	}
	release(true)
	if err := r.Fail(1, 11, "ep", "m", Failure{Scope: ScopeCredential, Reason: "rejected"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if allowed, reason := r.HealthAllowed(1, 11, "ep", "m"); allowed || reason != "account_repair_required" {
		t.Fatalf("credential repair bypassed: %v %s", allowed, reason)
	}
	if _, valid := r.HealthReadyAt(1, 11, "ep", "m"); valid {
		t.Fatal("invalid credentials have no automatic recovery time")
	}
}

func TestPoolSchedulesChannelsBeforeTheirGroups(t *testing.T) {
	cfg := poolFixture()
	p := &cfg.Pools[0]
	p.Channels = []PoolChannel{{1, TierPrimary}, {2, TierPrimary}, {3, TierBackup}, {5, TierPrimary}}
	p.Members = []PoolMember{{1, 11, TierPrimary}, {2, 22, TierPrimary}, {1, 12, TierPrimary}, {4, 44, TierPrimary}, {3, 33, TierBackup}}
	r := NewPoolRuntime(cfg, nil, nil)
	groups := func() []int64 {
		var result []int64
		for _, stage := range r.Plan(context.Background(), "codex", "test", true).Stages {
			result = append(result, stage.GroupID)
		}
		return result
	}
	if got := groups(); !reflect.DeepEqual(got, []int64{11, 12, 22, 33}) {
		t.Fatalf("channel groups were interleaved or an excluded channel was used: %v", got)
	}
	if got := groups(); !reflect.DeepEqual(got, []int64{22, 11, 12, 33}) {
		t.Fatalf("round robin must rotate channels, independent of group count: %v", got)
	}
	p.Strategy = PoolSequential
	p.Members = []PoolMember{{2, 22, TierPrimary}, {3, 33, TierBackup}}
	r.Replace(cfg)
	if got := groups(); !reflect.DeepEqual(got, []int64{22, 33}) {
		t.Fatalf("deselected groups and backup tier were ignored: %v", got)
	}
	if len(r.Config().Pools[0].Channels) != 4 {
		t.Fatal("turning off groups must keep their channels in the pool")
	}
}

func TestPoolRejectsGroupsFromChannelsOutsideThePool(t *testing.T) {
	catalog := Catalog{{ID: 1, Groups: []CatalogGroup{{ID: 11, Name: "one"}}}, {ID: 2, Groups: []CatalogGroup{{ID: 22, Name: "two"}}}}
	p := Pool{Name: "work", Strategy: PoolSequential, Channels: []PoolChannel{{1, TierPrimary}}, Members: []PoolMember{{2, 22, TierPrimary}}}
	if err := ValidatePool(&p, catalog, nil); err == nil {
		t.Fatal("group from a channel outside the pool was accepted")
	}
	p.Channels = append(p.Channels, PoolChannel{2, TierBackup})
	if err := ValidatePool(&p, catalog, nil); err != nil || p.Members[0].Tier != TierBackup {
		t.Fatalf("explicit backup channel was rejected or group tier was not normalized: %+v %v", p, err)
	}
	p.Channels = append(p.Channels, p.Channels[0])
	if err := ValidatePool(&p, catalog, nil); err == nil {
		t.Fatal("duplicate channel accepted")
	}
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
	if ok, _ := r.Allowed(1, 11, "ep2", "gpt-5"); ok {
		t.Fatal("same account and model escaped through another endpoint")
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

func TestStalePoolCallerCannotUseDefault(t *testing.T) {
	cfg := poolFixture()
	cfg.Pools[0].Revision = 1
	cfg.Pools[0].KeyHash = "retained-historical-hash"
	r := NewPoolRuntime(cfg, nil, nil)
	c := PoolCaller{Provider: "codex", PoolID: 1, Revision: 1}
	if !r.ValidCaller(c) {
		t.Fatal("historical caller fixture rejected")
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
