package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

func TestDispatchRecoveryChecksCurrentStateAndAllowsSameGroupRetry(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			profile := routing.DefaultProfile(provider)
			profile.Name, profile.IsDefault = "same group recovery", true
			profile.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
			_, err := a.SaveRoutingProfile(profile)
			require.NoError(t, err)
			plan := a.profileRuntimes[provider].Plan(context.Background(), 0, "test", false, routing.TurnInput{}, true)
			require.NotEmpty(t, plan.EligibleCandidates)
			candidate := plan.EligibleCandidates[0]
			input := DispatchRecoveryInput{Provider: provider, ChannelID: candidate.ChannelID, GroupID: candidate.GroupID, EndpointID: candidate.EndpointID, Model: candidate.Identity().Model}
			pool := a.poolRuntimes[provider]
			require.NoError(t, pool.Fail(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, input.Model, routing.Failure{Scope: routing.ScopeCandidate, Reason: "network_error"}, time.Now().Add(time.Hour)))
			before := pool.Cooldowns()
			states, err := a.GetDispatchRecovery(input)
			require.NoError(t, err)
			require.Len(t, states, 1)
			require.True(t, states[0].CanRetry)
			require.Equal(t, "automatic", states[0].Blockage.Recovery)
			require.Equal(t, "network_error", states[0].Blockage.Cause)
			require.Equal(t, before, pool.Cooldowns(), "reading diagnostics must not change health")
			states, err = a.RetryDispatchCandidate(input)
			require.NoError(t, err)
			require.Equal(t, "ready", states[0].Blockage.Recovery)
			require.Equal(t, 1, pool.Cooldowns()[0].Failures, "opening a gate does not erase failures")
			lease, err := pool.AcquireHealth(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, input.Model)
			require.NoError(t, err)
			states, err = a.GetDispatchRecovery(input)
			require.NoError(t, err)
			require.Equal(t, "probing", states[0].Blockage.Recovery, "an expired timer does not complete an active probe")
			require.False(t, states[0].CanRetry)
			_, err = a.RetryDispatchCandidate(input)
			require.Error(t, err)
			lease(true)
			states, err = a.GetDispatchRecovery(input)
			require.NoError(t, err)
			require.Equal(t, "ready", states[0].Blockage.Recovery)
			after := a.profileRuntimes[provider].Plan(context.Background(), 0, "test", false, routing.TurnInput{}, true)
			require.Equal(t, candidate.GroupID, after.EligibleCandidates[0].GroupID)
			require.Equal(t, profile.Name, after.Profile.Name)
		})
	}
}

func TestDispatchRecoveryReadsManagedAccountMetadataWithoutRefreshingCredentials(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			member := members[3]
			input := DispatchRecoveryInput{Provider: provider, ChannelID: member.ChannelID, GroupID: member.GroupID, Model: "test"}
			states, err := a.GetDispatchRecovery(input)
			require.NoError(t, err)
			require.NotEmpty(t, states)
			require.Equal(t, "subscription_state_unavailable", states[0].Blockage.Reason)
			pool := a.poolRuntimes[provider]
			account, ok := pool.Account(member.ChannelID, member.GroupID)
			require.True(t, ok)
			require.True(t, account.Managed)
			// List needs metadata only. A nil vault and adapter make any attempt
			// to resolve or refresh credentials fail this test.
			a.subscriptionAccounts, err = accounts.New(a.storeDB, nil, map[string]accounts.Adapter{provider: nil})
			require.NoError(t, err)
			info := accounts.Info{ID: strings.TrimPrefix(account.Identity, "account/"), Provider: provider, Enabled: true, Status: "sign_in_required"}
			for _, status := range []string{"sign_in_required", "disabled", "ready"} {
				info.Status, info.Enabled = status, status != "disabled"
				raw, err := json.Marshal(info)
				require.NoError(t, err)
				_, err = a.storeDB.Exec(`INSERT OR REPLACE INTO subscription_accounts(id,provider,identity_hash,info,credential,revision) VALUES(?,?,?,?,?,1)`, info.ID, provider, "test-identity", string(raw), []byte{})
				require.NoError(t, err)
				states, err = a.GetDispatchRecovery(input)
				require.NoError(t, err)
				want := map[string]string{"sign_in_required": "subscription_authentication_required", "disabled": "subscription_account_disabled", "ready": ""}[status]
				require.Equal(t, want, states[0].Blockage.Reason)
				require.False(t, states[0].CanRetry)
			}
		})
	}
}

func TestRecoveryRetryDoesNotBypassOtherAdmissionConditions(t *testing.T) {
	used := 100.0
	for _, reason := range []string{"disabled", "quota", "balance", "concurrent", "probing"} {
		t.Run(reason, func(t *testing.T) {
			c := routing.DispatchCandidate{Provider: "codex", Enabled: true, Health: routing.HealthState{RepairRequired: true}}
			evaluation := routing.CandidateEvaluation{Candidate: c, Reason: "credential_repair_required", Blockage: &routing.Blockage{Recovery: "manual"}}
			switch reason {
			case "disabled":
				evaluation.Candidate.Enabled = false
			case "quota":
				evaluation.Candidate.Quota.Windows = []routing.QuotaSnapshot{{UsedPercent: &used, Confidence: "exact", ExpiresAt: time.Now().Add(time.Hour)}}
			case "balance":
				evaluation.Candidate.Balance = &routing.ManualBalance{Enabled: true, StopWhenExhausted: true, BalanceUSD: "0"}
			case "concurrent":
				evaluation.Load = routing.LoadView{Concurrent: 1, MaxConcurrent: 1}
			case "probing":
				evaluation.Blockage.Recovery = "probing"
			}
			require.False(t, recoveryRetryAllowed(evaluation))
		})
	}
}
