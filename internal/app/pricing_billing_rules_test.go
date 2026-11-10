package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestCodexBillingRuleMappingPreservesGroupRelationship(t *testing.T) {
	rules := codexBillingRulesToTracking([]codexstore.CodexBillingRuleRecord{
		{Mode: "multiplier", GroupMultiplierMode: "inherit", CostMultiplier: 1.25},
		{Mode: "fixed", GroupMultiplierMode: "override", InputPrice: 4},
	})
	require.Equal(t, "inherit", rules[0].GroupMultiplierMode)
	require.Equal(t, 1.25, rules[0].CostMultiplier)
	require.Equal(t, "override", rules[1].GroupMultiplierMode)
	require.Equal(t, 4.0, rules[1].InputPrice)
}

func TestCredentialGroupZeroRatesPersistAndReachRuntime(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, member, _ := credentialGroupDeletionFixture(t, provider)
			a.usageTracker = newAppUsageTestTracker(t)
			defer a.usageTracker.Close()
			ctx := context.Background()
			for _, rate := range []float64{0, .5, 1} {
				if provider == "claude" {
					group, err := a.claudeCredentialGroupStore.GetByID(ctx, member.GroupID)
					require.NoError(t, err)
					group.CostMultiplier = rate
					group.InputCostMultiplier, group.OutputCostMultiplier = 1, 1
					group.CacheCreationCostMultiplier, group.CacheCreationCostMultiplier1h, group.CacheReadCostMultiplier = 1, 1, 0
					group.LongContextCostMultiplier, group.LongContextInputCostMultiplier, group.LongContextOutputCostMultiplier = 1, 1, 1
					group.LongContextCacheCreationCostMultiplier, group.LongContextCacheReadCostMultiplier = 1, 1
					require.NoError(t, a.claudeCredentialGroupService.UpdateGroup(ctx, group.ID, group))
					stored, err := a.claudeCredentialGroupStore.GetByID(ctx, group.ID)
					require.NoError(t, err)
					require.Equal(t, rate, stored.CostMultiplier)
					require.Zero(t, stored.CacheReadCostMultiplier)
				} else {
					group, err := a.codexCredentialGroupStore.GetByID(ctx, member.GroupID)
					require.NoError(t, err)
					group.CostMultiplier = rate
					group.InputCostMultiplier, group.OutputCostMultiplier = 1, 1
					group.CacheCreationCostMultiplier, group.CacheReadCostMultiplier = 1, 0
					group.LongContextCostMultiplier, group.LongContextInputCostMultiplier, group.LongContextOutputCostMultiplier = 1, 1, 1
					group.LongContextCacheCreationCostMultiplier, group.LongContextCacheReadCostMultiplier = 1, 1
					require.NoError(t, a.codexCredentialGroupService.UpdateGroup(ctx, group.ID, group))
					stored, err := a.codexCredentialGroupStore.GetByID(ctx, group.ID)
					require.NoError(t, err)
					require.Equal(t, rate, stored.CostMultiplier)
					require.Zero(t, stored.CacheReadCostMultiplier)
				}
				a.syncEndpointMultipliersToTracker(ctx)
				mult := a.usageTracker.GetEndpointMultiplierForEndpointWithGroup(provider, "first", "delete-me", "delete-child")
				require.True(t, mult.Configured)
				require.Equal(t, rate, mult.CostMultiplier)
				cost := tracking.CalculateCost(&tracking.TokenUsage{InputTokens: 1000000, CacheReadTokens: 1000000}, &tracking.ModelPricing{Input: 4, CacheRead: .2}, &mult)
				require.Equal(t, 4*rate, cost.TotalCost)
				require.Zero(t, cost.CacheReadCost)
			}
		})
	}
}
