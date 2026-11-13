package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestDispatchDailyCostAdmissionPreservesLimitReasonAndRechecksSends(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			member := members[0]
			if provider == "claude" {
				for _, ep := range a.endpointManager.GetAllEndpoints() {
					if ep.Config.CredentialGroupID == member.GroupID {
						ep.Config.CredentialGroupName, ep.Config.DailyCostLimitUSD = "first", 5
					}
				}
			} else {
				for _, ep := range a.codexEndpointMgr.PoolEndpoints(member.ChannelID, member.GroupID) {
					ep.DailyCostLimitUSD = 5
				}
			}
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 10, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour}, "Asia/Hong_Kong")
			require.NoError(t, err)
			t.Cleanup(func() { tracker.Close() })
			a.usageTracker = tracker
			profile := routing.DefaultProfile(provider)
			profile.Name, profile.IsDefault = "daily limit", true
			for _, m := range members[:2] {
				profile.Members = append(profile.Members, routing.RoutingMember{ChannelID: m.ChannelID, GroupID: m.GroupID, Enabled: true})
			}
			_, err = a.SaveRoutingProfile(profile)
			require.NoError(t, err)
			ctx := context.Background()
			runtime := a.profileRuntimes[provider]
			before := runtime.Plan(ctx, 0, "test", false, routing.TurnInput{}, false)
			var candidate routing.DispatchCandidate
			for _, c := range before.EligibleCandidates {
				if c.ChannelID == member.ChannelID && c.GroupID == member.GroupID {
					candidate = c
				}
			}
			require.NotEmpty(t, candidate.EndpointID)
			attempt, err := runtime.Reserve(ctx, &before, candidate)
			require.NoError(t, err)
			_, err = tracker.GetDB().Exec(`INSERT INTO request_logs(request_id,start_time,status,provider,channel,group_name,total_cost_usd) VALUES('at-limit',?,'completed',?,'first','first',5)`, time.Now().In(time.FixedZone("HKT", 8*60*60)).Format("2006-01-02 15:04:05"), provider)
			require.NoError(t, err)
			// A second request may finish after this one planned/reserved a seat.
			require.EqualError(t, attempt.Begin(ctx), "daily_cost_limit_exceeded")
			attempt.Finish(0, false, routing.Failure{})
			_, err = runtime.Reserve(ctx, &before, candidate)
			require.EqualError(t, err, "daily_cost_limit_exceeded")
			for _, preview := range []bool{true, false} {
				plan := runtime.Plan(ctx, 0, "test", false, routing.TurnInput{}, preview)
				require.NotEmpty(t, plan.EligibleCandidates, "the backup remains available")
				for _, c := range plan.EligibleCandidates {
					require.NotEqual(t, candidate.Identity(), c.Identity())
				}
				trace := plan.Trace()
				require.Zero(t, trace.Attempts)
				found := false
				for _, event := range trace.Candidates {
					if event.Candidate == candidate.Identity() {
						require.Equal(t, "skipped", event.Action)
						require.Equal(t, "daily_cost_limit_exceeded", event.Reason)
						found = true
					}
				}
				require.True(t, found, "a skipped group must keep its daily-limit diagnosis")
			}
			a.usageTracker = nil
			require.Equal(t, "daily_cost_state_unavailable", a.dispatchDailyCostAdmission(candidate))
		})
	}
}
