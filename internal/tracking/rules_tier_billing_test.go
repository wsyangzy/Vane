package tracking

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCombinationBillingAutomaticallyMatchesTierAcrossAccountingPaths(t *testing.T) {
	for _, configured := range []string{"response", "request"} {
		for _, tc := range []struct {
			name, requested, response, model, tier, origin, pricingTier, fallback string
			want                                                                  float64
			missingFastPrice                                                      bool
		}{
			{name: "requested priority", requested: "priority", response: "default", tier: "priority", origin: "request", pricingTier: "fast", want: 7.5},
			{name: "requested fast alias", requested: "fast", response: "standard", tier: "fast", origin: "request", pricingTier: "fast", want: 7.5},
			{name: "requested standard", requested: "standard", response: "priority", tier: "standard", origin: "request", want: 3},
			{name: "requested flex", requested: "flex", response: "default", tier: "flex", origin: "request", pricingTier: "flex", want: 1.2},
			{name: "requested batch fixed price", requested: "batch", response: "default", tier: "batch", origin: "request", pricingTier: "batch", want: .6},
			{name: "requested ultrafast", requested: "ultrafast", response: "default", tier: "ultrafast", origin: "request", pricingTier: "ultrafast", want: 18},
			{name: "auto uses response flex", requested: "auto", response: "flex", tier: "flex", origin: "response", pricingTier: "flex", want: 1.2},
			{name: "omitted request uses response ultrafast", response: "ultrafast", tier: "ultrafast", origin: "response", pricingTier: "ultrafast", want: 18},
			{name: "unknown request uses response fast", requested: "future-tier", response: "priority", tier: "priority", origin: "response", pricingTier: "fast", want: 7.5},
			{name: "no tier uses standard", origin: "default", want: 3},
			{name: "unresolved auto keeps evidence", requested: "auto", tier: "auto", origin: "request", fallback: "unresolved_tier", want: 3},
			{name: "fast rule does not affect other model", requested: "priority", response: "default", model: "other", tier: "priority", origin: "request", pricingTier: "fast", want: 12},
			{name: "missing fast price keeps fast rule", requested: "priority", response: "default", tier: "priority", origin: "request", fallback: "standard_price", want: 3.75, missingFastPrice: true},
		} {
			t.Run(configured+"/"+tc.name, func(t *testing.T) {
				model := tc.model
				if model == "" {
					model = "test"
				}
				prices := map[string]ModelPricing{}
				for tier, price := range map[string]float64{"": 2, "fast": 4, "flex": 1, "batch": 1, "ultrafast": 6} {
					prices[ModelPricingKey("codex", "test", tier)] = ModelPricing{Input: price}
				}
				prices[ModelPricingKey("codex", "other", "fast")] = ModelPricing{Input: 8}
				if tc.missingFastPrice {
					delete(prices, ModelPricingKey("codex", "test", "fast"))
				}
				mult := DefaultEndpointMultiplier()
				mult.CostMultiplier, mult.ServiceTierSource = 1.5, configured
				mult.BillingRules = []BillingRule{
					{Mode: BillingRuleModeMultiplier, CostMultiplier: 1.5},
					{Model: "test", ServiceTier: ServiceTierStandard, Mode: BillingRuleModeMultiplier, CostMultiplier: 1},
					{Model: "test", ServiceTier: ServiceTierFast, Mode: BillingRuleModeMultiplier, CostMultiplier: 1.25},
					{Model: "test", ServiceTier: ServiceTierFlex, Mode: BillingRuleModeMultiplier, CostMultiplier: .8},
					{Model: "test", ServiceTier: ServiceTierBatch, Mode: BillingRuleModeFixed, InputPrice: .4},
					{Model: "test", ServiceTier: ServiceTierUltrafast, Mode: BillingRuleModeMultiplier, CostMultiplier: 2},
				}
				key := EndpointMultiplierGroupKey("codex", "c", "g", "e")
				multipliers := map[string]EndpointMultiplier{key: mult}
				tracker := &UsageTracker{pricing: prices, endpointMu: multipliers}
				req := &ActiveRequest{
					Provider: "codex", Channel: "c", GroupName: "g", EndpointName: "e", ModelName: model,
					RequestedServiceTier: tc.requested, ResponseServiceTier: tc.response,
					ServiceTier: tc.response, ServiceTierSource: configured, InputTokens: 1000000,
					BillingRulesCaptured: true, BillingRules: append([]BillingRule(nil), mult.BillingRules...), BillingGroupMultiplier: &mult,
				}
				detail := tracker.ActiveRequestToDetail(req)
				require.InDelta(t, tc.want, detail.TotalCostUSD, 1e-12)
				require.Equal(t, tc.tier, detail.ServiceTier)
				var snapshot BillingDecisionSnapshot
				require.NoError(t, json.Unmarshal([]byte(detail.BillingRuleSnapshot), &snapshot))
				require.Equal(t, ServiceTierSourceRules, snapshot.ServiceTierPolicy)
				require.Equal(t, tc.origin, snapshot.ServiceTierSource)
				require.Equal(t, tc.pricingTier, snapshot.PricingServiceTier)
				require.Equal(t, tc.fallback, snapshot.PricingFallback)
				attempt, priced := tracker.CalculateAttemptCost("codex", "c", "g", "e", model, tc.response, &TokenUsage{InputTokens: 1000000}, tc.requested)
				require.True(t, priced)
				require.InDelta(t, tc.want, attempt, 1e-12)
				archive := &ArchiveManager{pricing: prices, endpointMu: multipliers}
				require.InDelta(t, tc.want, archive.calculateCost(req).TotalCost, 1e-12)
				require.Equal(t, detail.BillingRuleSnapshot, req.BillingRuleSnapshot)
				require.Equal(t, tc.tier, req.ServiceTier)
				require.Equal(t, configured, multipliers[key].ServiceTierSource, "automatic rule matching must preserve the legacy preference")
			})
		}
	}
}

func TestCombinationBillingUnpricedTierEvidenceMatchesActiveAndArchive(t *testing.T) {
	rule := BillingRule{Mode: BillingRuleModeMultiplier, CostMultiplier: 1}
	req := &ActiveRequest{Provider: "codex", ModelName: "unpriced", RequestedServiceTier: "priority", ResponseServiceTier: "default",
		ServiceTier: "default", ServiceTierSource: "response", BillingRulesCaptured: true, BillingRules: []BillingRule{rule}}
	detail := (&UsageTracker{}).ActiveRequestToDetail(req)
	var snapshot BillingDecisionSnapshot
	require.NoError(t, json.Unmarshal([]byte(detail.BillingRuleSnapshot), &snapshot))
	require.Equal(t, "priority", snapshot.BillingServiceTier)
	require.Equal(t, "request", snapshot.ServiceTierSource)
	require.Equal(t, "rules", snapshot.ServiceTierPolicy)
	require.Equal(t, "missing_price", snapshot.PricingFallback)
	archive := &ArchiveManager{}
	require.Zero(t, archive.calculateCost(req).TotalCost)
	require.Equal(t, detail.BillingRuleSnapshot, req.BillingRuleSnapshot)
}

func TestTraditionalBillingAlwaysUsesStandardAcrossAccountingPaths(t *testing.T) {
	for _, configured := range []string{"response", "request"} {
		for _, tier := range []string{"priority", "flex", "ultrafast", "auto", ""} {
			t.Run(configured+"/"+tier, func(t *testing.T) {
				prices := map[string]ModelPricing{}
				for name, price := range map[string]float64{"": 2, "fast": 4, "flex": 1, "ultrafast": 6} {
					prices[ModelPricingKey("codex", "test", name)] = ModelPricing{Input: price}
				}
				mult := DefaultEndpointMultiplier()
				mult.CostMultiplier, mult.ServiceTierSource = 1.5, configured
				key := EndpointMultiplierGroupKey("codex", "c", "g", "e")
				multipliers := map[string]EndpointMultiplier{key: mult}
				tracker := &UsageTracker{pricing: prices, endpointMu: multipliers}
				req := &ActiveRequest{Provider: "codex", Channel: "c", GroupName: "g", EndpointName: "e", ModelName: "test",
					RequestedServiceTier: tier, ResponseServiceTier: "priority", ServiceTier: "priority", ServiceTierSource: configured,
					InputTokens: 1000000}
				detail := tracker.ActiveRequestToDetail(req)
				require.InDelta(t, 3, detail.TotalCostUSD, 1e-12)
				require.Equal(t, ServiceTierStandard, detail.ServiceTier)
				for _, requested := range [][]string{nil, {tier}} {
					attempt, known := tracker.CalculateAttemptCost("codex", "c", "g", "e", "test", "priority", &TokenUsage{InputTokens: 1000000}, requested...)
					require.True(t, known)
					require.InDelta(t, 3, attempt, 1e-12)
				}
				archive := &ArchiveManager{pricing: prices, endpointMu: multipliers}
				require.InDelta(t, 3, archive.calculateCost(req).TotalCost, 1e-12)
				require.Equal(t, detail.BillingRuleSnapshot, req.BillingRuleSnapshot)
				var snapshot BillingDecisionSnapshot
				require.NoError(t, json.Unmarshal([]byte(req.BillingRuleSnapshot), &snapshot))
				require.Equal(t, tier, snapshot.RequestedServiceTier)
				require.Equal(t, "priority", snapshot.ResponseServiceTier)
				require.Equal(t, ServiceTierStandard, snapshot.BillingServiceTier)
				require.Equal(t, ServiceTierSourceStandard, snapshot.ServiceTierPolicy)
				require.Empty(t, snapshot.PricingServiceTier)
				require.Equal(t, configured, multipliers[key].ServiceTierSource)
			})
		}
	}
}
