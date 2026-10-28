package tracking

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAttemptCostReusesCacheAndBillingOutputSemantics(t *testing.T) {
	ut := &UsageTracker{pricing: map[string]ModelPricing{}, endpointMu: map[string]EndpointMultiplier{}}
	_, known := ut.CalculateAttemptCost("codex", "c", "g", "e", "missing", "", &TokenUsage{InputTokens: 10})
	require.False(t, known)
	// The exact cache keys and fallbacks are shared with the ordinary tracker.
	ut.pricing[ModelPricingKey("codex", "test", "")] = ModelPricing{Input: 10, Output: 20, CacheRead: 1}
	pricing, _, _, found := lookupModelPricing(ut.pricing, "codex", "test", "")
	if !found {
		t.Fatal("test pricing key does not match provider pricing contract")
	}
	u := &TokenUsage{InputTokens: 1000, OutputTokens: 100, ReasoningTokens: 80, CacheReadTokens: 800, InputIncludesCacheRead: true}
	got, known := ut.CalculateAttemptCost("codex", "c", "g", "e", "test", "", u)
	require.True(t, known)
	want := CalculateCost(u, &pricing, nil)
	require.InDelta(t, want.TotalCost, got, 1e-12)
}
