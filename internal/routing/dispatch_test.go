package routing

import (
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func dispatchTestProfile(strategy string) RoutingProfile {
	p := DefaultProfile("codex")
	p.ID = 1
	p.CandidateMode = UnifiedCandidates
	p.Strategy = strategy
	return p
}

func dispatchTestCandidates() []DispatchCandidate {
	return []DispatchCandidate{
		{Provider: "codex", ChannelID: 1, GroupID: 1, EndpointID: "a", Model: "gpt-test", Protocol: "responses", Enabled: true, Position: 0, Weight: 1},
		{Provider: "codex", ChannelID: 1, GroupID: 2, EndpointID: "b", Model: "gpt-test", Protocol: "responses", Enabled: true, Position: 1, Weight: 1},
		{Provider: "codex", ChannelID: 2, GroupID: 3, EndpointID: "c", Model: "gpt-test", Protocol: "responses", Enabled: true, Position: 2, Weight: 1},
	}
}

func dispatchTestWindow(now time.Time, used float64, reset time.Duration) QuotaSnapshot {
	r := now.Add(reset)
	return QuotaSnapshot{UsedPercent: &used, ResetAt: &r, ObservedAt: now, ExpiresAt: now.Add(time.Hour), Confidence: "exact"}
}

func dispatchFirst(t *testing.T, evaluations []CandidateEvaluation) DispatchCandidate {
	t.Helper()
	for _, e := range evaluations {
		if e.Eligible {
			return e.Candidate
		}
	}
	t.Fatal("no eligible candidate")
	return DispatchCandidate{}
}

func dispatchSend(t *testing.T, s *Scheduler, p RoutingProfile, cs []DispatchCandidate, now time.Time, newSelection bool) DispatchCandidate {
	t.Helper()
	c := dispatchFirst(t, s.Order(p, cs, nil, now))
	if !s.TryReserve(c, now) {
		t.Fatal("reservation rejected")
	}
	s.CommitSelection(p, cs, c, newSelection, now)
	s.Release(c, 0, true, now)
	return c
}

func TestDispatchSmartOrdersSoonestResetAndKeepsStableTies(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	cs[0].Quota = QuotaView{Learns: true, Windows: []QuotaSnapshot{dispatchTestWindow(now, 20, 8*time.Hour)}}
	cs[1].Quota = QuotaView{Learns: true, Windows: []QuotaSnapshot{dispatchTestWindow(now, 30, time.Hour)}}
	cs[2].Quota = QuotaView{Learns: true, Windows: []QuotaSnapshot{dispatchTestWindow(now, 40, time.Hour)}}
	e := NewScheduler().Order(dispatchTestProfile(StrategySmart), cs, nil, now)
	if e[0].Candidate.GroupID != 2 || e[1].Candidate.GroupID != 3 || e[2].Candidate.GroupID != 1 {
		t.Fatalf("unexpected reset order: %+v", e)
	}
	cs[1].Quota.Windows[0] = dispatchTestWindow(now, 95, time.Hour)
	cs[2].Quota.Windows[0] = dispatchTestWindow(now, 99, time.Hour)
	e = NewScheduler().Order(dispatchTestProfile(StrategySmart), cs, nil, now)
	if e[0].Candidate.GroupID != 1 || e[1].Candidate.GroupID != 2 || e[2].Candidate.GroupID != 3 {
		t.Fatalf("nearly spent quota did not fall behind: %+v", e)
	}
}

func TestDispatchSmartNeverPrefersNearlySpentOverBusyAvailableQuota(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()[:2]
	cs[0].Load.Concurrent, cs[0].Load.MaxConcurrent = 1, 4
	cs[0].Quota.Windows = []QuotaSnapshot{dispatchTestWindow(now, 20, time.Hour)}
	cs[1].Quota.Windows = []QuotaSnapshot{dispatchTestWindow(now, 99, time.Hour)}
	if c := dispatchFirst(t, NewScheduler().Order(dispatchTestProfile(StrategySmart), cs, nil, now)); c.GroupID != 1 {
		t.Fatalf("idle nearly spent account beat available busy account: %+v", c)
	}
	cs[0].Load.Concurrent = cs[0].Load.MaxConcurrent
	if c := dispatchFirst(t, NewScheduler().Order(dispatchTestProfile(StrategySmart), cs, nil, now)); c.GroupID != 2 {
		t.Fatalf("full account did not allow nearly spent fallback: %+v", c)
	}
}

func TestDispatchSkipsExhaustedRepairCooldownAndDuplicate(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	cs[0].Quota.Windows = []QuotaSnapshot{dispatchTestWindow(now, 100, time.Hour)}
	cs[1].Health.RepairRequired = true
	cs[2].Health = HealthState{CoolingDown: true, Until: now.Add(time.Minute)}
	cs = append(cs, cs[0])
	e := NewScheduler().Order(dispatchTestProfile(StrategySmart), cs, nil, now)
	for i, reason := range []string{"quota_exhausted", "credential_repair_required", "cooldown", "candidate_duplicate"} {
		if e[i].Eligible || e[i].Reason != reason {
			t.Fatalf("candidate %d = %+v, expected %s", i, e[i], reason)
		}
	}
}

func TestDispatchOrderedChannelsAndAffinity(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	cs[0].Load.DecayedTokens = 200
	cs[1].Load.DecayedTokens = 100
	p := dispatchTestProfile(StrategyUsage)
	p.CandidateMode = OrderedChannels
	p.Members = []RoutingMember{{ChannelID: 1}, {ChannelID: 2}}
	s := NewScheduler()
	if c := dispatchFirst(t, s.Order(p, cs, nil, now)); c.GroupID != 2 {
		t.Fatalf("ordered channel precedence lost: %+v", c)
	}
	preferred := cs[2].Identity()
	if c := dispatchFirst(t, s.Order(p, cs, &preferred, now)); c.GroupID != 3 {
		t.Fatalf("affinity should precede channel order: %+v", c)
	}
	p.AffinityMode = AffinityNone
	if c := dispatchFirst(t, s.Order(p, cs, &preferred, now)); c.GroupID != 2 {
		t.Fatalf("disabled affinity still affected order: %+v", c)
	}
}

func TestDispatchRotateOnlyAdvancesOnActualNewSelection(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	p, s := dispatchTestProfile(StrategyRotate), NewScheduler()
	for i := 0; i < 3; i++ {
		if c := dispatchFirst(t, s.Order(p, cs, nil, now)); c.GroupID != 1 {
			t.Fatalf("planning advanced rotation: %+v", c)
		}
	}
	if !s.TryReserve(cs[0], now) {
		t.Fatal("reserve")
	}
	s.Release(cs[0], 0, false, now)
	if c := dispatchFirst(t, s.Order(p, cs, nil, now)); c.GroupID != 1 {
		t.Fatal("unsent reservation advanced rotation")
	}
	dispatchSend(t, s, p, cs, now, true)
	preferred := cs[0].Identity()
	tool := dispatchFirst(t, s.Order(p, cs, &preferred, now))
	if tool.GroupID != 1 || !s.TryReserve(tool, now) {
		t.Fatal("tool continuation lost affinity")
	}
	s.CommitSelection(p, cs, tool, false, now)
	s.Release(tool, 0, true, now)
	if next := dispatchSend(t, s, p, cs, now, true); next.GroupID != 2 {
		t.Fatalf("tool continuation advanced cursor: %+v", next)
	}
	cs[1].Health.RepairRequired = true
	if next := dispatchSend(t, s, p, cs, now, true); next.GroupID != 3 {
		t.Fatalf("temporarily ineligible last account reset cursor: %+v", next)
	}
	cs[2].Health.RepairRequired = true
	if next := dispatchSend(t, s, p, cs, now, true); next.GroupID != 1 {
		t.Fatalf("qualification skip advanced cursor: %+v", next)
	}
}

func TestDispatchPreviewHasNoStateSideEffects(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	for _, strategy := range []string{StrategySmart, StrategyOrder, StrategyRotate, StrategyUsage, StrategyPace, StrategyWeighted} {
		t.Run(strategy, func(t *testing.T) {
			p, s := dispatchTestProfile(strategy), NewScheduler()
			dispatchSend(t, s, p, cs, now, true)
			before := s.Order(p, cs, nil, now)
			weights := map[dispatchBucket]map[dispatchAccount]int64{}
			for key, state := range s.rotation {
				weights[key] = map[dispatchAccount]int64{}
				for account, weight := range state.weights {
					weights[key][account] = weight
				}
			}
			for i := 0; i < 10; i++ {
				if got := s.Preview(p, cs, nil, now); !reflect.DeepEqual(before, got) {
					t.Fatal("preview changed decision")
				}
			}
			for key, state := range s.rotation {
				if len(state.weights) != len(weights[key]) {
					t.Fatal("preview mutated weights")
				}
				for account, weight := range state.weights {
					if weight != weights[key][account] {
						t.Fatal("preview advanced smooth weight")
					}
				}
			}
		})
	}
}

func TestDispatchWeightedDistributionCountsAccountsOnce(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()[:2]
	cs[0].Weight, cs[1].Weight = 5, 1
	secondEndpoint := cs[0]
	secondEndpoint.EndpointID = "a2"
	cs = append(cs, secondEndpoint)
	p, s := dispatchTestProfile(StrategyWeighted), NewScheduler()
	counts := map[int64]int{}
	for i := 0; i < 600; i++ {
		selected := dispatchSend(t, s, p, cs, now, true)
		counts[selected.GroupID]++
	}
	if counts[1] != 500 || counts[2] != 100 {
		t.Fatalf("duplicate endpoint changed weighted votes: %v", counts)
	}
	// A continuation must not alter the distribution of subsequent turns.
	baseline := s.Order(p, cs, nil, now)
	dispatchSend(t, s, p, cs, now, false)
	if after := s.Order(p, cs, nil, now); after[0].Candidate.GroupID != baseline[0].Candidate.GroupID {
		t.Fatal("tool continuation advanced smooth weights")
	}
}

func TestDispatchUsageHasOneHourHalfLifeAndFailedRequestsCount(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	s, p := NewScheduler(), dispatchTestProfile(StrategyUsage)
	if !s.TryReserve(cs[0], now) {
		t.Fatal("reserve")
	}
	s.CommitSelection(p, cs, cs[0], true, now)
	s.Release(cs[0], 200, false, now)
	load := s.Load(cs[0], now.Add(time.Hour))
	if math.Abs(load.DecayedTokens-100) > 0.001 || math.Abs(load.DecayedRequests-0.5) > 0.001 || load.Concurrent != 0 {
		t.Fatalf("wrong load decay: %+v", load)
	}
	if c := dispatchFirst(t, s.Order(p, cs, nil, now)); c.GroupID != 2 {
		t.Fatalf("usage did not avoid recently used account: %+v", c)
	}
	if got := s.Order(p, cs, nil, now)[0]; got.UsedPercent != nil || !IsQuotaUnknownReason(got.Reason) {
		t.Fatalf("local load misrepresented provider quota: %+v", got)
	}
}

func TestDispatchSuccessfulResponseWithoutUsageHasMinimalLocalLoad(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	s, p := NewScheduler(), dispatchTestProfile(StrategyUsage)
	dispatchSend(t, s, p, cs[:1], now, true)
	if load := s.Load(cs[0], now); load.DecayedTokens != 1 || load.DecayedRequests != 1 {
		t.Fatalf("success without usage lost its local signal: %+v", load)
	}
	if !s.TryReserve(cs[1], now) {
		t.Fatal("reserve")
	}
	s.CommitSelection(p, cs, cs[1], true, now)
	s.Release(cs[1], 0, false, now)
	if load := s.Load(cs[1], now); load.DecayedTokens != 0 || load.DecayedRequests != 1 {
		t.Fatalf("failed zero-usage request manufactured tokens: %+v", load)
	}
}

func TestDispatchRetainedStateIsBoundedWithoutLosingInflightReservations(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	s, p := NewScheduler(), dispatchTestProfile(StrategyRotate)
	for i := 0; i < maxDispatchRotations+10; i++ {
		p.Revision = int64(i)
		s.CommitSelection(p, cs, cs[0], true, now.Add(time.Duration(i)*time.Second))
	}
	if len(s.rotation) != maxDispatchRotations {
		t.Fatalf("profile revision state is unbounded: %d", len(s.rotation))
	}
	cs[0].Load.MaxConcurrent = 1
	if !s.TryReserve(cs[0], now) {
		t.Fatal("reserve active account")
	}
	for i := 0; i < maxDispatchAccounts+10; i++ {
		c := cs[1]
		c.GroupID = int64(i + 100)
		if !s.TryReserve(c, now.Add(time.Duration(i)*time.Second)) {
			t.Fatal("reserve idle test account")
		}
		s.Release(c, 0, false, now.Add(time.Duration(i)*time.Second))
	}
	if len(s.loads) > maxDispatchAccounts {
		t.Fatalf("idle account state is unbounded: %d", len(s.loads))
	}
	if s.TryReserve(cs[0], now.Add(2*time.Hour)) {
		t.Fatal("eviction lost inflight reservation")
	}
	s.Release(cs[0], 0, false, now.Add(2*time.Hour))
	if !s.TryReserve(cs[0], now.Add(2*time.Hour)) {
		t.Fatal("eviction damaged release")
	}
	s.Release(cs[0], 0, false, now.Add(2*time.Hour))
}

func TestDispatchConcurrencyReservationsAreSharedAcrossEndpoints(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	cs[0].Load.MaxConcurrent = 1
	otherEndpoint := cs[0]
	otherEndpoint.EndpointID = "a2"
	s := NewScheduler()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := cs[0]
			if i%2 == 0 {
				c = otherEndpoint
			}
			if s.TryReserve(c, now) {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("account reserved %d times through multiple endpoints", accepted.Load())
	}
	if c := dispatchFirst(t, s.Order(dispatchTestProfile(StrategySmart), cs, nil, now)); c.GroupID != 2 {
		t.Fatalf("concurrent request did not spread to another account: %+v", c)
	}
	s.Release(cs[0], 0, false, now)
	if !s.TryReserve(otherEndpoint, now) {
		t.Fatal("release did not free account")
	}
	s.Release(otherEndpoint, 0, false, now)
}

func TestDispatchQuotaModelIsolationExpiryAndReset(t *testing.T) {
	now := time.Now()
	c := dispatchTestCandidates()[0]
	q := dispatchTestWindow(now, 100, time.Hour)
	q.ModelScope = "other-model"
	c.Quota.Windows = []QuotaSnapshot{q}
	s, p := NewScheduler(), dispatchTestProfile(StrategySmart)
	check := func(eligible, known bool) {
		t.Helper()
		e := s.Order(p, []DispatchCandidate{c}, nil, now)[0]
		if e.Eligible != eligible || e.QuotaKnown != known {
			t.Fatalf("wrong model/stale quota eligibility: %+v", e)
		}
	}
	check(true, false)
	c.UpstreamModel = "other-model"
	check(false, true)
	c.Quota.Windows[0].ExpiresAt = now
	check(true, false)
	c.Quota.Windows[0].ExpiresAt = now.Add(time.Hour)
	c.Quota.Windows[0].Confidence = "stale"
	check(true, false)
	c.Quota.Windows[0].Confidence = "exact"
	pastReset := now.Add(-time.Second)
	c.Quota.Windows[0].ResetAt = &pastReset
	check(true, false)
}

func TestDispatchManagedAccountAcrossBindingsSharesLoadVotesAndFailureScope(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	cs[0].AccountIDHash, cs[1].AccountIDHash = "safe-account", "safe-account"
	cs[0].Load.MaxConcurrent, cs[1].Load.MaxConcurrent = 1, 1
	s := NewScheduler()
	if !s.TryReserve(cs[0], now) || s.TryReserve(cs[1], now) {
		t.Fatal("same managed account reserved independently through two bindings")
	}
	s.Release(cs[0], 20, true, now)
	if load := s.Load(cs[1], now); load.DecayedTokens != 20 {
		t.Fatalf("managed account load was not shared: %+v", load)
	}
	ledger := &Ledger{}
	ledger.Fail(cs[0].Identity(), Failure{Scope: ScopeModel, Reason: "model_access_denied"})
	if allowed, _ := ledger.Allowed(cs[1].Identity()); allowed {
		t.Fatal("same-account model failure did not cross group binding")
	}
	if allowed, _ := ledger.Allowed(cs[2].Identity()); !allowed {
		t.Fatal("model failure crossed distinct account boundary")
	}
	p := dispatchTestProfile(StrategyWeighted)
	s = NewScheduler()
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		c := dispatchSend(t, s, p, cs, now, true)
		counts[c.AccountIDHash]++
	}
	if counts["safe-account"] != 100 || counts[""] != 100 {
		t.Fatalf("duplicate bindings gave the same account extra votes: %v", counts)
	}
	cs[1].ChannelID = 2
	cs[2].ChannelID = 3
	p.CandidateMode = OrderedChannels
	p.Members = []RoutingMember{{ChannelID: 1}, {ChannelID: 2}, {ChannelID: 3}}
	evaluations := NewScheduler().Order(p, cs, nil, now)
	for i, e := range evaluations {
		if e.Candidate.ChannelID != int64(i+1) {
			t.Fatal("account aggregation collapsed ordered channel domains")
		}
	}
}

func TestDispatchPaceUsesBottleneckInsteadOfAddingWindows(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()[:2]
	// Account A has 80% short-term room but only 1% per hour in its
	// restrictive weekly window. B can spend 10% per hour in both windows.
	cs[0].Quota.Windows = []QuotaSnapshot{dispatchTestWindow(now, 20, time.Hour), dispatchTestWindow(now, 90, 10*time.Hour)}
	cs[1].Quota.Windows = []QuotaSnapshot{dispatchTestWindow(now, 90, time.Hour), dispatchTestWindow(now, 80, 2*time.Hour)}
	e := NewScheduler().Order(dispatchTestProfile(StrategyPace), cs, nil, now)
	if e[0].Candidate.GroupID != 2 || e[0].PaceScore == nil || *e[0].PaceScore != 10 || *e[1].PaceScore != 1 {
		t.Fatalf("pace summed incompatible windows: %+v", e)
	}
	cs[0].Quota.Windows[1].ResetAt = nil
	e = NewScheduler().Order(dispatchTestProfile(StrategyPace), cs[:1], nil, now)
	if e[0].PaceScore != nil || e[0].Reason != "quota_reset_unknown_recent_load" {
		t.Fatalf("missing bottleneck reset did not fall back: %+v", e[0])
	}
}

func TestDispatchOrderUsesFirstUntilUnavailable(t *testing.T) {
	now := time.Now()
	cs := dispatchTestCandidates()
	p, s := dispatchTestProfile(StrategyOrder), NewScheduler()
	for i := 0; i < 10; i++ {
		if c := dispatchSend(t, s, p, cs, now, true); c.GroupID != 1 {
			t.Fatalf("order changed with use: %+v", c)
		}
	}
	cs[0].Health = HealthState{CoolingDown: true, Until: now.Add(time.Minute)}
	if c := dispatchSend(t, s, p, cs, now, true); c.GroupID != 2 {
		t.Fatalf("order failed to skip unavailable account: %+v", c)
	}
}
