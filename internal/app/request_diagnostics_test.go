package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

func TestRequestDiagnosticsRecoverHistoryBeyondRecentWindow(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			setBuildModeForTest(t, "release")
			a, members := dispatchTestApp(t, provider)
			ctx := context.Background()
			_, err := a.storeDB.Exec(`INSERT INTO request_logs(request_id,start_time,status,provider,environment) VALUES('request-0',?,'completed',?,'release')`, time.Now().Format("2006-01-02 15:04:05"), provider)
			require.NoError(t, err)
			store := routingstore.NewProfileStore(a.storeDB, "release")
			require.NoError(t, store.EnsureSchema(ctx))
			require.NoError(t, privacy.NewStore(a.storeDB).EnsureSchema(ctx))
			candidate := routing.Candidate{Provider: provider, Channel: fmt.Sprint(members[1].ChannelID), Credential: fmt.Sprintf("%d/%d", members[1].ChannelID, members[1].GroupID), Endpoint: "backup::backup", Protocol: "messages", Model: "test"}
			if provider == "codex" {
				endpoints, err := a.codexEndpointStore.ListByChannelID(ctx, members[1].ChannelID)
				require.NoError(t, err)
				candidate.Endpoint, candidate.Protocol = fmt.Sprint(endpoints[0].ID), "responses"
			}
			finished := time.Now()
			for index := 0; index < 110; index++ {
				id := fmt.Sprintf("request-%d", index)
				require.NoError(t, store.SaveTrace(ctx, routing.DispatchTrace{RequestID: id, Provider: provider, ProfileID: 7, ProfileName: "historical-profile", Decision: routing.DispatchDecision{Kind: "default"}, Attempts: 2, Successful: true, FinishedAt: &finished, Candidates: []routing.DispatchTraceEvent{{Candidate: candidate, Action: "attempt"}, {Candidate: candidate, Action: "attempt"}, {Candidate: candidate, Action: "success"}}}))
				activity := privacy.Activity{RequestID: id, Provider: provider, State: "clean", ScanDurationMS: 1.25, Path: "/v1/responses", CreatedAt: finished}
				payload, err := json.Marshal(activity)
				require.NoError(t, err)
				_, err = a.storeDB.Exec(`INSERT INTO privacy_activity(created_at,payload) VALUES(?,?)`, finished.Unix(), string(payload))
				require.NoError(t, err)
			}
			// Same request ID in a different provider/build must never replace it.
			other := "claude"
			if provider == "claude" {
				other = "codex"
			}
			require.NoError(t, store.SaveTrace(ctx, routing.DispatchTrace{RequestID: "request-0", Provider: other, ProfileID: 99}))
			require.NoError(t, routingstore.NewProfileStore(a.storeDB, "dev").SaveTrace(ctx, routing.DispatchTrace{RequestID: "request-0", Provider: provider, ProfileID: 88}))
			_, err = a.storeDB.Exec(`INSERT INTO privacy_activity(created_at,payload) VALUES(?,?)`, finished.Unix(), fmt.Sprintf(`{"request_id":"request-0","provider":%q,"state":"blocked"}`, other))
			require.NoError(t, err)
			records := []RequestRecord{{RequestID: "request-0", Provider: provider, Channel: "stale-initial-channel"}, {RequestID: "request-0", Provider: provider, Channel: "captured-channel", Endpoint: "captured-endpoint", Group: "captured-group"}, {RequestID: "missing", Provider: provider}}
			progress, err := json.Marshal(routing.DispatchTrace{RequestID: "request-0", Provider: provider, ProfileID: 7, Attempts: 3, Candidates: []routing.DispatchTraceEvent{{Candidate: candidate, Action: "attempt"}}})
			require.NoError(t, err)
			records = append(records, RequestRecord{RequestID: "request-0", Provider: provider, RouteSnapshot: string(progress)}, RequestRecord{RequestID: "request-0", Provider: provider, Channel: "historical-channel", Endpoint: "historical-endpoint"})
			a.enrichRequestDiagnostics(ctx, records)
			require.Equal(t, "backup", records[0].Channel)
			require.Equal(t, "backup", records[0].Endpoint)
			require.Equal(t, "backup", records[0].Group)
			require.Equal(t, 1, records[0].RetryCount)
			var snapshot routing.DispatchTrace
			require.NoError(t, json.Unmarshal([]byte(records[0].RouteSnapshot), &snapshot))
			require.Equal(t, int64(7), snapshot.ProfileID)
			require.True(t, snapshot.Successful)
			var summary struct {
				Activity privacy.Activity `json:"activity"`
			}
			require.NoError(t, json.Unmarshal([]byte(records[0].PrivacyRedactionSummary), &summary))
			require.Equal(t, "clean", summary.Activity.State)
			require.Equal(t, 1.25, summary.Activity.ScanDurationMS)
			require.False(t, records[0].PrivacyRedacted, "a clean scan is not redaction")
			require.Equal(t, "captured-endpoint", records[1].Endpoint)
			require.Equal(t, "captured-group", records[1].Group)
			require.Empty(t, records[2].RouteSnapshot)
			require.Empty(t, records[2].PrivacyRedactionSummary)
			require.NoError(t, json.Unmarshal([]byte(records[3].RouteSnapshot), &snapshot))
			require.Equal(t, 3, snapshot.Attempts, "active progress must not be replaced by an earlier settlement")
			require.Equal(t, "historical-channel", records[4].Channel)
			var endpoint string
			// Enrichment is read-only and never backfills guessed configuration
			// names into historical usage records.
			err = a.storeDB.QueryRow(`SELECT COALESCE(endpoint_name,'') FROM request_logs WHERE request_id='request-0'`).Scan(&endpoint)
			require.NoError(t, err)
			require.Empty(t, endpoint)
		})
	}
}
