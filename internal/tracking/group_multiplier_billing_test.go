package tracking

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingRuleGroupInheritanceRequestE765f80c(t *testing.T) {
	usage := &TokenUsage{InputTokens: 128747, OutputTokens: 1800, CacheReadTokens: 128384, InputIncludesCacheRead: true}
	pricing := ModelPricing{CodexCachePricing: true, Input: 4, Output: 20, CacheRead: .2, LongContextThreshold: 272000}
	id := billingIdentity{provider: "codex", requestModel: "gpt-6.1-sol", pricingModel: "gpt-6.1-sol", serviceTier: "priority", pricingTier: "fast"}
	for _, tc := range []struct {
		name, relation, mode string
		want, group, rule    float64
	}{
		{"stacked fast", BillingGroupMultiplierInherit, BillingRuleModeMultiplier, .1183665, 1.5, 1.25},
		{"old override fast now inherits group", BillingGroupMultiplierOverride, BillingRuleModeMultiplier, .1183665, 1.5, 1.25},
		{"unspecified fast defaults to group inheritance", "", BillingRuleModeMultiplier, .1183665, 1.5, 1.25},
		{"stacked fixed prices", BillingGroupMultiplierInherit, BillingRuleModeFixed, .0946932, 1.5, 1},
		{"old override fixed prices now inherit group", BillingGroupMultiplierOverride, BillingRuleModeFixed, .0946932, 1.5, 1},
		{"unspecified fixed rule defaults to group inheritance", "", BillingRuleModeFixed, .0946932, 1.5, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mult := DefaultEndpointMultiplier()
			mult.CostMultiplier = 1.5
			mult.BillingRules = []BillingRule{
				{Mode: BillingRuleModeMultiplier, CostMultiplier: 1.5},
				{Model: id.requestModel, ServiceTier: ServiceTierFast, Context: BillingContextNormal, Mode: tc.mode, GroupMultiplierMode: tc.relation,
					CostMultiplier: 1.25, InputPrice: 4, OutputPrice: 20, CacheReadPrice: .2},
			}
			cost, effective, snapshot := calculateRequestCost(usage, pricing, mult, id)
			require.InDelta(t, tc.want, cost.TotalCost, 1e-12)
			require.Equal(t, tc.group, snapshot.GroupCostMultiplier)
			require.Equal(t, tc.rule, snapshot.RuleCostMultiplier)
			require.Equal(t, tc.group*tc.rule, snapshot.TotalCostMultiplier)
			require.Equal(t, snapshot.TotalCostMultiplier, effective.CostMultiplier)
			require.Equal(t, "model=gpt-6.1-sol;tier=fast;context=normal", snapshot.RuleKey)
			require.InDelta(t, cost.InputCost+cost.OutputCost+cost.CacheReadCost, cost.TotalCost, 1e-12)
			var persisted BillingDecisionSnapshot
			require.NoError(t, json.Unmarshal([]byte(billingDecisionSnapshotJSON(snapshot)), &persisted))
			require.Equal(t, *snapshot, persisted)
			if tc.name == "stacked fast" {
				require.InDelta(t, .0027225, cost.InputCost, 1e-12)
				require.InDelta(t, .0675, cost.OutputCost, 1e-12)
				require.InDelta(t, .048144, cost.CacheReadCost, 1e-12)
			}
		})
	}
}

func TestBillingRuleInheritanceIncludesCategoryFactorsAndCacheTTL(t *testing.T) {
	usage := &TokenUsage{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 30, CacheCreationTokens: 20,
		CacheWrite30mTokens: 8, CacheWrite12hTokens: 12, InputIncludesCacheRead: true}
	pricing := ModelPricing{CodexCachePricing: true, Input: 1, Output: 2, CacheRead: .1, CacheWritePrice30m: 3, CacheWritePrice12h: 6,
		LongContextThreshold: 90, LongContextInputPrice: 2, LongContextOutputPrice: 4, LongContextCacheReadPrice: .2,
		LongContextCacheWritePrice: 6, LongContextCacheWritePrice12h: 12}
	mult := DefaultEndpointMultiplier()
	mult.CostMultiplier, mult.InputCostMultiplier, mult.OutputCostMultiplier = 1.5, 4.0/3, 1.2
	mult.CacheCreationCostMultiplier, mult.CacheReadCostMultiplier, mult.CacheWriteTTLPricing = 1.4, .8, true
	// Legacy model/long modifiers do not join the explicit rule hierarchy.
	mult.LongContextCostMultiplier = 99
	mult.ModelMultipliers = []ModelMultiplier{{Model: "test", CostMultiplier: 99}}
	mult.BillingRules = []BillingRule{{Mode: BillingRuleModeMultiplier, GroupMultiplierMode: BillingGroupMultiplierInherit,
		Context: BillingContextLong, CostMultiplier: 1.25, InputCostMultiplier: .5, OutputCostMultiplier: 2,
		CacheCreationCostMultiplier: 2.5, CacheReadCostMultiplier: 1.25}}
	cost, _, snapshot := calculateRequestCost(usage, pricing, mult, billingIdentity{provider: "codex", requestModel: "test", pricingModel: "test"})
	require.Equal(t, BillingContextLong, snapshot.ContextMode)
	require.Equal(t, "reported_ttl", snapshot.CacheWritePricing)
	require.InDelta(t, 50*2*(4.0/3)*.5*1.875/1e6, cost.InputCost, 1e-12)
	require.InDelta(t, 10*4*1.2*2*1.875/1e6, cost.OutputCost, 1e-12)
	require.InDelta(t, (8*6+12*12)*1.4*2.5*1.875/1e6, cost.CacheCreationCost, 1e-12)
	require.InDelta(t, 30*.2*.8*1.25*1.875/1e6, cost.CacheReadCost, 1e-12)
	require.InDelta(t, snapshot.CacheWrite30mCost+snapshot.CacheWrite12hCost, cost.CacheCreationCost, 1e-12)
	rebuilt := (50*snapshot.InputPrice*snapshot.InputCostMultiplier + 10*snapshot.OutputPrice*snapshot.OutputCostMultiplier +
		30*snapshot.CacheReadPrice*snapshot.CacheReadCostMultiplier) * snapshot.TotalCostMultiplier / 1e6
	require.InDelta(t, cost.TotalCost, rebuilt+snapshot.CacheWrite30mCost+snapshot.CacheWrite12hCost, 1e-12)
}

func TestBillingRuleDefaultInheritanceAppliesGroupOnce(t *testing.T) {
	mult := DefaultEndpointMultiplier()
	mult.CostMultiplier = 1.5
	mult.BillingRules = []BillingRule{{Mode: BillingRuleModeMultiplier, GroupMultiplierMode: BillingGroupMultiplierInherit, CostMultiplier: 1}}
	for _, tier := range []string{"standard", "fast", "flex", "batch", "ultrafast"} {
		t.Run(tier, func(t *testing.T) {
			cost, _, snapshot := calculateRequestCost(&TokenUsage{InputTokens: 1000000}, ModelPricing{Input: 2}, mult, billingIdentity{serviceTier: tier})
			require.Equal(t, 3.0, cost.TotalCost)
			require.Equal(t, 1.5, snapshot.TotalCostMultiplier)
		})
	}
	_, _, snapshot := calculateRequestCost(nil, ModelPricing{}, mult, billingIdentity{})
	require.Nil(t, snapshot)
}

func TestUnspecifiedRuleRelationshipUsesGroupWithoutDoublingLegacyFallback(t *testing.T) {
	mult := DefaultEndpointMultiplier()
	mult.CostMultiplier = 1.5
	mult.BillingRules = []BillingRule{
		{Mode: BillingRuleModeMultiplier, CostMultiplier: 1.5},
		{Model: "test", ServiceTier: "fast", Context: "normal", Mode: BillingRuleModeMultiplier, CostMultiplier: 1.25},
		{Model: "test", ServiceTier: "fast", Context: "long", Mode: BillingRuleModeMultiplier, CostMultiplier: 2},
	}
	for _, tc := range []struct {
		name, tier string
		input      int64
		want       float64
	}{
		{"ordinary tier uses old fallback once", "standard", 100, .0003},
		{"missing relationship fast inherits", "priority", 100, .000375},
		{"missing relationship long inherits", "priority", 101, .000606},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost, _, _ := calculateRequestCost(&TokenUsage{InputTokens: tc.input}, ModelPricing{Input: 2, LongContextThreshold: 100}, mult,
				billingIdentity{requestModel: "test", serviceTier: tc.tier})
			require.InDelta(t, tc.want, cost.TotalCost, 1e-12)
		})
	}
}

func TestUnmatchedBillingRuleKeepsGroupFactors(t *testing.T) {
	mult := DefaultEndpointMultiplier()
	mult.CostMultiplier, mult.InputCostMultiplier = 1.5, 4.0/3
	mult.ModelMultipliers = []ModelMultiplier{{Model: "other", CostMultiplier: 99}}
	mult.LongContextCostMultiplier = 99
	mult.BillingRules = []BillingRule{{Model: "test", ServiceTier: ServiceTierFast, Mode: BillingRuleModeMultiplier, CostMultiplier: 1.25}}
	cost, effective, snapshot := calculateRequestCost(&TokenUsage{InputTokens: 1000000}, ModelPricing{Input: 2}, mult,
		billingIdentity{requestModel: "other", serviceTier: ServiceTierStandard})
	require.InDelta(t, 4, cost.TotalCost, 1e-12)
	require.Equal(t, 1.5, effective.CostMultiplier)
	require.Equal(t, 4.0/3, effective.InputCostMultiplier)
	require.Empty(t, snapshot.RuleKey)
}

func TestBillingRuleGroupSnapshotSurvivesLiveConfigChangesAndRepeatedArchive(t *testing.T) {
	pool := NewHotPool(DefaultHotPoolConfig())
	defer pool.Close()
	mult := DefaultEndpointMultiplier()
	mult.CostMultiplier, mult.ServiceTierSource = 1.5, "response"
	mult.BillingRules = []BillingRule{{Mode: BillingRuleModeMultiplier, GroupMultiplierMode: BillingGroupMultiplierInherit, CostMultiplier: 1.25}}
	key := EndpointMultiplierGroupKey("codex", "relay", "group", "main")
	prices := map[string]ModelPricing{ModelPricingKey("codex", "gpt-6.1-sol", "fast"): {Input: 4, Output: 20, CacheRead: .2}}
	tracker := &UsageTracker{hotPool: pool, pricing: prices, endpointMu: map[string]EndpointMultiplier{key: mult}}
	req := NewActiveRequest("group-snapshot", "", "", "POST", "/v1/responses", true)
	req.Provider, req.Channel, req.GroupName, req.EndpointName, req.ModelName = "codex", "relay", "group", "main", "gpt-6.1-sol"
	req.RequestedServiceTier, req.ResponseServiceTier = "priority", "default"
	req.InputTokens, req.OutputTokens, req.CacheReadTokens, req.InputIncludesCacheRead = 128747, 1800, 128384, true
	require.NoError(t, pool.Add(req))
	require.NoError(t, tracker.UpdateActiveRequestMultiplier(req.RequestID, mult))
	require.NotNil(t, req.BillingGroupMultiplier)
	require.Equal(t, ServiceTierSourceRules, req.ServiceTierSource)
	require.Equal(t, "priority", req.ServiceTier)
	replayData, err := json.Marshal(req)
	require.NoError(t, err)
	// Changing the live group must affect subsequent requests only.
	mult.CostMultiplier = 9
	tracker.endpointMu[key] = mult
	active := tracker.ActiveRequestToDetail(req)
	require.InDelta(t, .1183665, active.TotalCostUSD, 1e-12)
	attempt, known := tracker.CalculateAttemptCost("codex", "relay", "group", "main", "gpt-6.1-sol", "default", &TokenUsage{
		InputTokens: 128747, OutputTokens: 1800, CacheReadTokens: 128384, InputIncludesCacheRead: true,
	}, "priority")
	require.True(t, known)
	require.InDelta(t, .0631288*9*1.25, attempt, 1e-12)
	archive := &ArchiveManager{pricing: prices, endpointMu: tracker.endpointMu}
	for range 2 {
		cost := archive.calculateCost(req)
		require.InDelta(t, .1183665, cost.TotalCost, 1e-12, "repeated calculations must not reapply the group factor")
		require.InDelta(t, 1.875, req.CostMultiplier, 1e-12)
	}
	var snapshot BillingDecisionSnapshot
	require.NoError(t, json.Unmarshal([]byte(req.BillingRuleSnapshot), &snapshot))
	require.Equal(t, 1.5, snapshot.GroupCostMultiplier)
	require.Equal(t, 1.25, snapshot.RuleCostMultiplier)
	require.Equal(t, ServiceTierSourceRules, snapshot.ServiceTierPolicy)
	// Failed archive files use the same JSON round-trip before batchInsert
	// recomputes costs. Both the captured rule and its base factors must survive.
	var replayed ActiveRequest
	require.NoError(t, json.Unmarshal(replayData, &replayed))
	require.True(t, replayed.BillingRulesCaptured)
	// Older failed archives captured rules alongside the legacy response policy.
	// Resolve the automatic tier before choosing a price during detail/replay.
	replayed.ServiceTierSource, replayed.ServiceTier = "response", "default"
	require.InDelta(t, .1183665, tracker.ActiveRequestToDetail(&replayed).TotalCostUSD, 1e-12)
	require.InDelta(t, .1183665, archive.calculateCost(&replayed).TotalCost, 1e-12)
	// A removed endpoint still uses the captured group factors.
	delete(tracker.endpointMu, key)
	require.InDelta(t, .1183665, tracker.ActiveRequestToDetail(req).TotalCostUSD, 1e-12)
}

func TestCombinationLongContextSwitchIsCapturedAcrossAccountingPaths(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name, context, want := "enabled", BillingContextLong, 18.0
		if disabled {
			name, context, want = "disabled", BillingContextNormal, 7.5
		}
		t.Run(name, func(t *testing.T) {
			pool := NewHotPool(DefaultHotPoolConfig())
			defer pool.Close()
			mult := DefaultEndpointMultiplier()
			mult.DisableLongContextPricing, mult.CostMultiplier = disabled, 1.5
			mult.BillingRules = []BillingRule{
				{Mode: BillingRuleModeMultiplier, GroupMultiplierMode: BillingGroupMultiplierInherit, CostMultiplier: 1},
				{Model: "test", Context: BillingContextNormal, Mode: BillingRuleModeMultiplier, CostMultiplier: 1.25},
				{Model: "test", Context: BillingContextLong, Mode: BillingRuleModeMultiplier, CostMultiplier: 2},
			}
			key := EndpointMultiplierGroupKey("codex", "c", "g", "e")
			prices := map[string]ModelPricing{ModelPricingKey("codex", "test", ""): {
				Input: 4, Output: 2, LongContextThreshold: 100, LongContextInputPrice: 6, LongContextOutputPrice: 3,
			}}
			tracker := &UsageTracker{hotPool: pool, pricing: prices, endpointMu: map[string]EndpointMultiplier{key: mult}}
			req := NewActiveRequest("long-switch", "", "", "POST", "/v1/responses", true)
			req.Provider, req.Channel, req.GroupName, req.EndpointName, req.ModelName = "codex", "c", "g", "e", "test"
			req.InputTokens = 1000000
			require.NoError(t, pool.Add(req))
			require.NoError(t, tracker.UpdateActiveRequestMultiplier(req.RequestID, mult))
			require.Equal(t, disabled, req.BillingGroupMultiplier.DisableLongContextPricing)
			attempt, known := tracker.CalculateAttemptCost("codex", "c", "g", "e", "test", "default", &TokenUsage{InputTokens: 1000000})
			require.True(t, known)
			require.InDelta(t, want, attempt, 1e-12)
			// Later changes apply to new requests; an in-flight request keeps its switch.
			mult.DisableLongContextPricing = !disabled
			tracker.endpointMu[key] = mult
			detail := tracker.ActiveRequestToDetail(req)
			require.InDelta(t, want, detail.TotalCostUSD, 1e-12)
			archive := &ArchiveManager{pricing: prices, endpointMu: tracker.endpointMu}
			require.InDelta(t, want, archive.calculateCost(req).TotalCost, 1e-12)
			require.Equal(t, detail.BillingRuleSnapshot, req.BillingRuleSnapshot)
			var snapshot BillingDecisionSnapshot
			require.NoError(t, json.Unmarshal([]byte(req.BillingRuleSnapshot), &snapshot))
			require.Equal(t, context, snapshot.ContextMode)
			require.Equal(t, "model=test;tier=*;context="+context, snapshot.RuleKey)
			data, err := json.Marshal(req)
			require.NoError(t, err)
			var replayed ActiveRequest
			require.NoError(t, json.Unmarshal(data, &replayed))
			require.InDelta(t, want, archive.calculateCost(&replayed).TotalCost, 1e-12)
		})
	}
}
