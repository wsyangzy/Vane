package tracking

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnknownModelsUseSelectedProviderDefaultAcrossBillingPaths(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			model := "claude-fable-5-2"
			if provider == "codex" {
				model = "unconfigured-codex-model"
			}
			defaultModel := provider + "-selected-default"
			prices := map[string]ModelPricing{
				ModelPricingKey(provider, defaultModel, ""): {IsDefault: true, Input: 2, Output: 10, CacheRead: .2, CodexCachePricing: provider == "codex"},
				// An older fixed-name row must not override the user's current choice.
				defaultModelPricingKey(provider): {Input: 99, Output: 99},
			}
			mult := DefaultEndpointMultiplier()
			mult.CostMultiplier = .5
			mult.ModelMultipliers = []ModelMultiplier{{Model: model, CostMultiplier: 2}}
			key := EndpointMultiplierGroupKey(provider, "channel", "group", "endpoint")
			tracker := &UsageTracker{pricing: prices, endpointMu: map[string]EndpointMultiplier{key: mult}}
			req := NewActiveRequest("default-billing", "", "", "POST", "/", true)
			req.Provider, req.ModelName = provider, model
			req.Channel, req.GroupName, req.EndpointName = "channel", "group", "endpoint"
			req.InputTokens, req.OutputTokens, req.CacheReadTokens = 1000000, 100000, 100000
			req.InputIncludesCacheRead = provider == "codex"
			want := 3.02
			if provider == "codex" {
				want = 2.82
			}
			detail := tracker.ActiveRequestToDetail(req)
			require.InDelta(t, want, detail.TotalCostUSD, 1e-12)
			var snapshot BillingDecisionSnapshot
			require.NoError(t, json.Unmarshal([]byte(detail.BillingRuleSnapshot), &snapshot))
			require.Equal(t, "provider_default", snapshot.PricingFallback)
			require.Equal(t, provider, snapshot.PricingProvider)
			require.Equal(t, defaultModel, snapshot.PricingModel)
			require.Equal(t, 2.0, snapshot.InputPrice)
			require.Equal(t, 10.0, snapshot.OutputPrice)
			require.Equal(t, .2, snapshot.CacheReadPrice)
			require.Equal(t, 1.0, snapshot.TotalCostMultiplier)

			attempt, known := tracker.CalculateAttemptCost(provider, "channel", "group", "endpoint", model, "", &TokenUsage{
				InputTokens: req.InputTokens, OutputTokens: req.OutputTokens, CacheReadTokens: req.CacheReadTokens, InputIncludesCacheRead: req.InputIncludesCacheRead,
			})
			require.True(t, known)
			require.InDelta(t, want, attempt, 1e-12)
			archive := &ArchiveManager{pricing: prices, endpointMu: tracker.endpointMu}
			require.InDelta(t, want, archive.calculateCost(req).TotalCost, 1e-12)
			require.Equal(t, detail.BillingRuleSnapshot, req.BillingRuleSnapshot)

			// Defaults are selected by configuration, not by positive-cost heuristics.
			mult.CostMultiplier = 0
			tracker.endpointMu[key] = mult
			require.Zero(t, tracker.ActiveRequestToDetail(req).TotalCostUSD)
			require.Zero(t, archive.calculateCost(req).TotalCost)
		})
	}
}

func TestDefaultPricingLookupKeepsExactPricesAndProviderIsolation(t *testing.T) {
	prices := map[string]ModelPricing{
		ModelPricingKey("claude", "sonnet-default", ""): {IsDefault: true, Input: 2},
		ModelPricingKey("claude", "free-model", ""):     {},
		ModelPricingKey("codex", "tier-only", "fast"):   {IsDefault: true, Input: 99},
	}
	pricing, model, _, found := lookupModelPricing(prices, "claude", "free-model", "")
	require.True(t, found)
	require.Equal(t, "free-model", model)
	require.Zero(t, pricing.Input, "an explicitly configured zero price must not be replaced")
	_, _, _, found = lookupModelPricing(prices, "codex", "unknown", "fast")
	require.False(t, found, "neither another provider's default nor a default flag on a nonstandard tier can be used")
	_, model, tier, found := lookupModelPricing(prices, "", "unknown", "fast")
	require.True(t, found)
	require.Equal(t, "sonnet-default", model)
	require.Empty(t, tier)
}

func TestLegacyPricingUsesSelectedClaudeDefault(t *testing.T) {
	tracker := &UsageTracker{
		config: &Config{},
		pricing: map[string]ModelPricing{
			ModelPricingKey("claude", "selected-default", ""): {IsDefault: true, Input: 2, Output: 10},
			ModelPricingKey("codex", "known-codex", ""):       {Input: 5},
		},
	}
	require.Equal(t, 2.0, tracker.GetPricing("unconfigured").Input)
	require.Equal(t, 5.0, tracker.GetPricing("known-codex").Input, "exact model prices must precede defaults")
	_, _, _, _, total := tracker.calculateCost("unconfigured", &TokenUsage{InputTokens: 1000000, OutputTokens: 100000})
	require.Equal(t, 3.0, total)
}
