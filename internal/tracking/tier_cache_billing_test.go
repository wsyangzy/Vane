package tracking

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBillingTierKeepsRequestAndResponseSemantics(t *testing.T) {
	for _, tc := range []struct{ requested, response, source, tier, origin string }{
		{"priority", "default", "", "default", "response"},
		{"priority", "default", "request", "priority", "request"},
		{"auto", "ultrafast", "request", "ultrafast", "response"},
		{"future-tier", "flex", "request", "flex", "response"},
		{"priority", "", "response", "priority", "request"},
		{"", "batch", "request", "batch", "response"},
		{" Standard ", "priority", "request", "standard", "request"},
		{"", "", "response", "", "default"},
		{"priority", "default", "rules", "priority", "request"},
		{"fast", "standard", "rules", "fast", "request"},
		{"standard", "priority", "rules", "standard", "request"},
		{"flex", "default", "rules", "flex", "request"},
		{"batch", "default", "rules", "batch", "request"},
		{"ultrafast", "default", "rules", "ultrafast", "request"},
		{"auto", "flex", "rules", "flex", "response"},
		{"future-tier", "ultrafast", "rules", "ultrafast", "response"},
		{"", "priority", "rules", "priority", "response"},
		{"priority", "", "rules", "priority", "request"},
		{"auto", "", "rules", "auto", "request"},
		{"", "", "rules", "", "default"},
	} {
		t.Run(tc.requested+"/"+tc.response+"/"+tc.source, func(t *testing.T) {
			tier, origin := ResolveBillingServiceTier(tc.requested, tc.response, tc.source)
			require.Equal(t, tc.tier, tier)
			require.Equal(t, tc.origin, origin)
		})
	}
	require.Equal(t, ServiceTierUltrafast, NormalizeServiceTierForBilling(" Ultrafast "))
}

func TestUnpricedCodexRequestKeepsTierEvidence(t *testing.T) {
	req := &ActiveRequest{Provider: "codex", ModelName: "unpriced", RequestedServiceTier: "priority", ResponseServiceTier: "default", ServiceTierSource: "request", ServiceTier: "priority"}
	tracker := &UsageTracker{}
	detail := tracker.ActiveRequestToDetail(req)
	archive := &ArchiveManager{}
	require.Zero(t, archive.calculateCost(req).TotalCost)
	require.Equal(t, detail.BillingRuleSnapshot, req.BillingRuleSnapshot)
	var snapshot BillingDecisionSnapshot
	require.NoError(t, json.Unmarshal([]byte(req.BillingRuleSnapshot), &snapshot))
	require.Equal(t, "missing_price", snapshot.PricingFallback)
	require.Equal(t, "priority", snapshot.RequestedServiceTier)
	require.Equal(t, "default", snapshot.ResponseServiceTier)
	require.Equal(t, ServiceTierStandard, snapshot.BillingServiceTier)
	require.Equal(t, ServiceTierSourceStandard, snapshot.ServiceTierSource)
}

func TestCodexCacheWriteAllocationAndSnapshotAgree(t *testing.T) {
	base := ModelPricing{CodexCachePricing: true, Input: 1, CacheRead: .1, CacheWritePrice30m: 1.25, CacheWritePrice12h: 2.5}
	u := TokenUsage{InputTokens: 12000, CacheReadTokens: 4000, CacheCreationTokens: 7000, CacheWrite30mTokens: 3000, CacheWrite12hTokens: 4000, InputIncludesCacheRead: true}
	for _, tc := range []struct {
		name, reason       string
		ttl                bool
		price12h           float64
		write30m, write12h int64
		want               float64
	}{
		{"default ignores TTL", "30m_default", false, 2.5, 3000, 4000, .01015},
		{"opt in", "reported_ttl", true, 2.5, 3000, 4000, .01515},
		{"missing price", "30m_missing_12h_price", true, 0, 3000, 4000, .01015},
		{"missing counters", "30m_missing_ttl_usage", true, 2.5, 0, 0, .01015},
		{"incomplete counters", "30m_missing_ttl_usage", true, 2.5, 3000, 3999, .01015},
		{"negative counters", "30m_missing_ttl_usage", true, 2.5, -1, 7001, .01015},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, usage := base, u
			p.CacheWritePrice12h = tc.price12h
			usage.CacheWrite30mTokens, usage.CacheWrite12hTokens = tc.write30m, tc.write12h
			m := DefaultEndpointMultiplier()
			m.CacheWriteTTLPricing = tc.ttl
			cost, _, snap := calculateRequestCost(&usage, p, m, billingIdentity{provider: "codex", requestModel: "test", pricingModel: "test"})
			require.InDelta(t, tc.want, cost.TotalCost, 1e-12)
			require.InDelta(t, .001, cost.InputCost, 1e-12, "cache reads and writes must be removed from ordinary input")
			require.Equal(t, tc.reason, snap.CacheWritePricing)
			require.Equal(t, int64(7000), snap.CacheWrite30mTokens+snap.CacheWrite12hTokens)
			require.InDelta(t, cost.CacheCreationCost, snap.CacheWrite30mCost+snap.CacheWrite12hCost, 1e-12)
			require.Zero(t, cost.CacheCreation5mCost)
			require.Zero(t, cost.CacheCreation1hCost)
			var persisted BillingDecisionSnapshot
			require.NoError(t, json.Unmarshal([]byte(billingDecisionSnapshotJSON(snap)), &persisted))
			require.Equal(t, *snap, persisted)
		})
	}
	// A cache hit alone is not evidence of a cache write.
	u.CacheCreationTokens, u.CacheWrite30mTokens, u.CacheWrite12hTokens = 0, 0, 0
	require.Zero(t, CalculateCost(&u, &base, nil).CacheCreationCost)
}

func TestCodexCacheWriteUsesLongContextAndFixedRule(t *testing.T) {
	p := ModelPricing{CodexCachePricing: true, Input: 1, Output: 2, CacheWritePrice30m: 1.25, CacheWritePrice12h: 2.5,
		LongContextThreshold: 100, LongContextInputPrice: 2, LongContextOutputPrice: 3, LongContextCacheWritePrice: 2.5, LongContextCacheWritePrice12h: 5}
	u := &TokenUsage{InputTokens: 101, CacheCreationTokens: 80, CacheWrite30mTokens: 30, CacheWrite12hTokens: 50, InputIncludesCacheRead: true}
	m := DefaultEndpointMultiplier()
	m.CacheWriteTTLPricing, m.ServiceTierSource, m.CostMultiplier, m.LongContextCostMultiplier = true, "request", 1.5, 2
	id := billingIdentity{provider: "codex", requestModel: "test", pricingModel: "test", serviceTier: "ultrafast", pricingTier: "ultrafast"}
	cost, _, snap := calculateRequestCost(u, p, m, id)
	require.Equal(t, "long", snap.ContextMode)
	require.Equal(t, 5.0, snap.CacheWrite12hPrice)
	require.InDelta(t, (21*2+30*2.5+50*5)*3/1e6, cost.TotalCost, 1e-12)
	require.InDelta(t, cost.CacheCreationCost, snap.CacheWrite30mCost+snap.CacheWrite12hCost, 1e-12)
	p.LongContextCacheWritePrice12h = 0
	_, _, snap = calculateRequestCost(u, p, m, id)
	require.Equal(t, "30m_missing_12h_price", snap.CacheWritePricing)
	m.BillingRules = []BillingRule{{ServiceTier: "ultrafast", Mode: BillingRuleModeFixed, InputPrice: 3, CacheCreationPrice: 4}}
	cost, effective, snap := calculateRequestCost(u, p, m, id)
	require.True(t, effective.CacheWriteTTLPricing)
	require.Equal(t, "request", effective.ServiceTierSource)
	require.Equal(t, "normal", snap.ContextMode)
	require.Equal(t, "reported_ttl", snap.CacheWritePricing)
	require.InDelta(t, (21*3+80*4)*1.5/1e6, cost.TotalCost, 1e-12)
}

func TestAttemptCostUsesSameTierAndTTLPolicyAsRequestAccounting(t *testing.T) {
	u := &TokenUsage{InputTokens: 12000, CacheReadTokens: 4000, CacheCreationTokens: 7000, CacheWrite30mTokens: 3000, CacheWrite12hTokens: 4000, InputIncludesCacheRead: true}
	m := DefaultEndpointMultiplier()
	m.CacheWriteTTLPricing = true
	key := EndpointMultiplierGroupKey("codex", "c", "g", "e")
	tracker := &UsageTracker{pricing: map[string]ModelPricing{
		ModelPricingKey("codex", "test", ""):     {CodexCachePricing: true, Input: 1, CacheRead: .1, CacheWritePrice30m: 1.25, CacheWritePrice12h: 2.5},
		ModelPricingKey("codex", "test", "fast"): {CodexCachePricing: true, Input: 2, CacheRead: .2, CacheWritePrice30m: 2.5, CacheWritePrice12h: 5},
	}, endpointMu: map[string]EndpointMultiplier{key: m}}
	standard, priced := tracker.CalculateAttemptCost("codex", "c", "g", "e", "test", "default", u, "priority")
	require.True(t, priced)
	require.InDelta(t, .01515, standard, 1e-12)
	m.ServiceTierSource = "request"
	tracker.endpointMu[key] = m
	legacy, priced := tracker.CalculateAttemptCost("codex", "c", "g", "e", "test", "default", u, "priority")
	require.True(t, priced)
	require.InDelta(t, standard, legacy, 1e-12, "traditional multipliers ignore the retired tier preference")
	m.BillingRules = []BillingRule{{Mode: BillingRuleModeMultiplier, GroupMultiplierMode: BillingGroupMultiplierInherit, CostMultiplier: 1}}
	tracker.endpointMu[key] = m
	fast, priced := tracker.CalculateAttemptCost("codex", "c", "g", "e", "test", "default", u, "priority")
	require.True(t, priced)
	require.InDelta(t, .0303, fast, 1e-12)
}

func TestRequestedFastTierMatchesRelayInvoiceWithSpecificRule(t *testing.T) {
	usage := &TokenUsage{InputTokens: 196402, CacheReadTokens: 13568, OutputTokens: 1865, InputIncludesCacheRead: true}
	prices := map[string]ModelPricing{
		ModelPricingKey("codex", "gpt-6.1-sol", ""):     {CodexCachePricing: true, Input: 2, Output: 10, CacheRead: .1, LongContextThreshold: 272000},
		ModelPricingKey("codex", "gpt-6.1-sol", "fast"): {CodexCachePricing: true, Input: 4, Output: 20, CacheRead: .2, LongContextThreshold: 272000},
	}
	for _, tc := range []struct {
		source, pricingTier, ruleKey string
		input, output, cache, total  float64
	}{
		{"response", "fast", "model=gpt-6.1-sol;tier=fast;context=normal", .91417, .046625, .003392, .964187},
		{"request", "fast", "model=gpt-6.1-sol;tier=fast;context=normal", .91417, .046625, .003392, .964187},
	} {
		t.Run(tc.source, func(t *testing.T) {
			mult := DefaultEndpointMultiplier()
			mult.ServiceTierSource = tc.source
			mult.BillingRules = []BillingRule{
				{Mode: BillingRuleModeMultiplier, CostMultiplier: 1.5},
				{Model: "gpt-6.1-sol", ServiceTier: ServiceTierFast, Context: BillingContextNormal, Mode: BillingRuleModeMultiplier, CostMultiplier: 1.25},
			}
			policy := billingServiceTierSource(mult)
			tier, _ := ResolveBillingServiceTier("priority", "default", policy)
			pricing, model, pricingTier, found := lookupModelPricing(prices, "codex", "gpt-6.1-sol", tier)
			require.True(t, found)
			cost, _, snapshot := calculateRequestCost(usage, pricing, mult, billingIdentity{
				provider: "codex", requestModel: "gpt-6.1-sol", pricingModel: model, pricingTier: pricingTier,
				requestedTier: "priority", responseTier: "default", tierSource: policy, serviceTier: tier,
			})
			require.InDelta(t, tc.input, cost.InputCost, 1e-12)
			require.InDelta(t, tc.output, cost.OutputCost, 1e-12)
			require.InDelta(t, tc.cache, cost.CacheReadCost, 1e-12)
			require.InDelta(t, tc.total, cost.TotalCost, 1e-12)
			require.Equal(t, tc.pricingTier, snapshot.PricingServiceTier)
			require.Equal(t, tc.ruleKey, snapshot.RuleKey)
			require.Equal(t, ServiceTierSourceRequest, snapshot.ServiceTierSource)
			require.Equal(t, ServiceTierSourceRules, snapshot.ServiceTierPolicy)
			tracker := &UsageTracker{pricing: prices, endpointMu: map[string]EndpointMultiplier{
				EndpointMultiplierGroupKey("codex", "relay", "github-pro", "main"): mult,
			}}
			attemptCost, priced := tracker.CalculateAttemptCost("codex", "relay", "github-pro", "main", "gpt-6.1-sol", "default", usage, "priority")
			require.True(t, priced)
			require.InDelta(t, tc.total, attemptCost, 1e-12)
		})
	}
}
