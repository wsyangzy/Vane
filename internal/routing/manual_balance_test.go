package routing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManualBalanceMoneyAndEligibility(t *testing.T) {
	for _, value := range []string{"0", "0.000000001", "20.123456789", "2000000000.25"} {
		n, err := ParseUSD(value)
		require.NoError(t, err)
		require.Equal(t, value, FormatUSD(n))
	}
	for _, value := range []string{"-1", "NaN", "1e2", "0.0000000001", "1.2.3", "99999999999999999"} {
		_, err := ParseUSD(value)
		require.Error(t, err)
	}
	now := time.Now()
	c := DispatchCandidate{Enabled: true, Balance: &ManualBalance{Enabled: true, StopWhenExhausted: true, BalanceUSD: "0"}}
	require.Equal(t, "manual_balance_exhausted", candidateEligibility(c, LoadView{}, dispatchQuota{}, now))
	c.Balance.StopWhenExhausted = false
	require.Empty(t, candidateEligibility(c, LoadView{}, dispatchQuota{}, now))
	c.Balance = nil
	require.Empty(t, candidateEligibility(c, LoadView{}, dispatchQuota{}, now))
}

func TestManualBalanceAdmissionRecheckAndExactlyOneSettlement(t *testing.T) {
	p := DefaultProfile("codex")
	p.ID = 1
	p.IsDefault = true
	p.Members = []RoutingMember{{ChannelID: 1, GroupID: 2, Enabled: true}}
	c := DispatchCandidate{Provider: "codex", ChannelID: 1, GroupID: 2, EndpointID: "ep", Protocol: "responses", Enabled: true}
	blocked := false
	settlements := []AttemptSettlement{}
	r := NewProfileRuntime(RuntimeConfig{Provider: "codex", Profiles: []RoutingProfile{p}, Candidates: []DispatchCandidate{c}, CheckAdmission: func(DispatchCandidate) string {
		if blocked {
			return "manual_balance_exhausted"
		}
		return ""
	}, OnSettlement: func(s AttemptSettlement) { settlements = append(settlements, s) }})
	ctx := context.Background()
	plan := r.Plan(ctx, 0, "model", false, TurnInput{}, false)
	require.NotEmpty(t, plan.EligibleCandidates)
	a, err := r.Reserve(ctx, &plan, plan.EligibleCandidates[0])
	require.NoError(t, err)
	blocked = true
	require.ErrorContains(t, a.Begin(ctx), "manual_balance_exhausted")
	attempts, _ := plan.Budget.Stats()
	require.Zero(t, attempts, "balance skips must not consume the send budget")
	a.Finish(0, false, Failure{})
	require.Empty(t, settlements)
	blocked = false
	plan = r.Plan(ctx, 0, "model", false, TurnInput{}, false)
	a, err = r.Reserve(ctx, &plan, plan.EligibleCandidates[0])
	require.NoError(t, err)
	require.NoError(t, a.Begin(ctx))
	a.SetUsage(AttemptUsage{Known: true, Complete: false, InputTokens: 10, OutputTokens: 2})
	a.Finish(12, false, Failure{Scope: ScopeCandidate, Reason: "incomplete"})
	a.Finish(12, false, Failure{})
	require.Len(t, settlements, 1)
	require.EqualValues(t, 10, settlements[0].Usage.InputTokens)
	require.NotEmpty(t, settlements[0].ID)
	require.False(t, settlements[0].Usage.Complete)
	preview := r.Preview(ctx, 0, "model", false, TurnInput{})
	require.True(t, preview.Preview)
	require.Len(t, settlements, 1)
}
