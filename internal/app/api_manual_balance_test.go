package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestManualBalanceAPIRuntimeAndCostParity(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			p := routing.DefaultProfile(provider)
			p.Name, p.IsDefault = "manual-balance", true
			for _, m := range members[:2] {
				p.Members = append(p.Members, routing.RoutingMember{ChannelID: m.ChannelID, GroupID: m.GroupID, Enabled: true})
			}
			_, err := a.SaveRoutingProfile(p)
			require.NoError(t, err)
			a.usageTracker = &tracking.UsageTracker{}
			a.usageTracker.UpdatePricing(map[string]tracking.ModelPricing{tracking.ModelPricingKey(provider, "test", ""): {Input: 10, Output: 20, CacheRead: 1}})
			m := members[0]
			input := routing.ManualBalanceUpdate{Provider: provider, ChannelID: m.ChannelID, GroupID: m.GroupID, Action: "calibrate", AmountUSD: "0.0048", StopWhenExhausted: true, OperationID: "initial-operation"}
			b, err := a.UpdateManualBalance(input)
			require.NoError(t, err)
			require.Equal(t, "0.0048", b.BalanceUSD)
			ctx := context.Background()
			runtime := a.profileRuntimes[provider]
			plan := runtime.Plan(ctx, 0, "test", false, routing.TurnInput{}, false)
			var chosen routing.DispatchCandidate
			for _, c := range plan.EligibleCandidates {
				if c.GroupID == m.GroupID && c.ChannelID == m.ChannelID {
					chosen = c
					break
				}
			}
			require.NotEmpty(t, chosen.EndpointID)
			attempt, err := runtime.Reserve(ctx, &plan, chosen)
			require.NoError(t, err)
			require.NoError(t, attempt.Begin(ctx))
			// Claude input excludes caches; Codex raw input includes them.
			inputTokens := int64(200)
			if provider == "codex" {
				inputTokens = 1000
			}
			attempt.SetUsage(routing.AttemptUsage{Known: true, Complete: true, InputTokens: inputTokens, OutputTokens: 100, CacheReadTokens: 800, InputIncludesCacheRead: provider == "codex"})
			attempt.Finish(1100, true, routing.Failure{})
			balances, err := a.ListManualBalances(provider)
			require.NoError(t, err)
			require.Len(t, balances, 1)
			require.Equal(t, "0", balances[0].BalanceUSD)
			require.Equal(t, "0.0048", balances[0].SpentUSD)
			preview := runtime.Preview(ctx, 0, "test", false, routing.TurnInput{})
			blocked := false
			for _, evaluation := range preview.Evaluations {
				if evaluation.Candidate.GroupID == m.GroupID && evaluation.Candidate.ChannelID == m.ChannelID {
					require.False(t, evaluation.Eligible)
					require.Equal(t, "manual_balance_exhausted", evaluation.Reason)
					blocked = true
				}
			}
			require.True(t, blocked)
			entries, err := a.ListManualBalanceEntries(provider, m.ChannelID, m.GroupID)
			require.NoError(t, err)
			require.Len(t, entries, 2)
			state, err := a.ListRoutingProfiles(provider)
			require.NoError(t, err)
			require.Len(t, state.Balances, 1)
			input.Action, input.AmountUSD, input.Revision, input.OperationID = "credit", "2", balances[0].Revision, "credit-operation"
			_, err = a.UpdateManualBalance(input)
			require.NoError(t, err)
			preview = runtime.Preview(ctx, 0, "test", false, routing.TurnInput{})
			for _, e := range preview.Evaluations {
				if e.Candidate.GroupID == m.GroupID && e.Candidate.ChannelID == m.ChannelID {
					require.True(t, e.Eligible)
				}
			}
			managed := members[3]
			input.ChannelID, input.GroupID, input.Revision, input.OperationID, input.Action = managed.ChannelID, managed.GroupID, 0, "managed-operation", "calibrate"
			_, err = a.UpdateManualBalance(input)
			require.ErrorContains(t, err, "official_quota")
			_, err = a.RefreshRoutingQuota(provider, m.ChannelID, m.GroupID, "")
			require.ErrorContains(t, err, "quota_query_unsupported")
			// A settlement that began before the baseline cannot charge it again.
			s, err := a.profileStore(ctx)
			require.NoError(t, err)
			require.NoError(t, s.SettleManualBalance(ctx, routing.AttemptSettlement{Candidate: chosen, ID: "historical", StartedAt: b.CalibratedAt.Add(-time.Second)}, 999, true, time.Now()))
			balances, err = a.ListManualBalances(provider)
			require.NoError(t, err)
			require.Equal(t, "2", balances[0].BalanceUSD)
		})
	}
}
