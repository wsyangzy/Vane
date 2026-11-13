package tracking

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCredentialGroupDailyCostUsesConfiguredDayAndExactRoute(t *testing.T) {
	tracker, err := NewUsageTracker(&Config{
		Enabled:         true,
		DatabasePath:    filepath.Join(t.TempDir(), "usage.db"),
		BufferSize:      100,
		BatchSize:       10,
		FlushInterval:   time.Hour,
		CleanupInterval: 24 * time.Hour,
		RetentionDays:   30,
	}, "Asia/Tokyo")
	if err != nil {
		t.Fatalf("NewUsageTracker failed: %v", err)
	}
	defer tracker.Close()

	now := tracker.now()
	insert := func(id, provider, channel, group string, at time.Time, cost float64) {
		t.Helper()
		if _, err := tracker.db.Exec(`
			INSERT INTO request_logs (request_id, start_time, status, provider, channel, group_name, total_cost_usd)
			VALUES (?, ?, 'failed', ?, ?, ?, ?)
		`, id, at.Format("2006-01-02 15:04:05"), provider, channel, group, cost); err != nil {
			t.Fatalf("insert request log: %v", err)
		}
	}
	insert("match-1", "claude", "channel-a", "group-a", now.Add(-time.Hour), 1.25)
	insert("match-2", "claude", "channel-a", "group-a", now.Add(-2*time.Hour), 0.75)
	insert("other-provider", "codex", "channel-a", "group-a", now.Add(-time.Hour), 9)
	insert("other-channel", "claude", "channel-b", "group-a", now.Add(-time.Hour), 9)
	insert("other-group", "claude", "channel-a", "group-b", now.Add(-time.Hour), 9)
	insert("previous-day", "claude", "channel-a", "group-a", now.AddDate(0, 0, -1), 9)

	usedUSD, resetAt, err := tracker.CredentialGroupDailyCost(context.Background(), "claude", "channel-a", "group-a")
	if err != nil {
		t.Fatalf("CredentialGroupDailyCost failed: %v", err)
	}
	if math.Abs(usedUSD-2.0) > 1e-9 {
		t.Fatalf("usedUSD = %v, want 2", usedUSD)
	}
	wantReset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	if !resetAt.Equal(wantReset) {
		t.Fatalf("resetAt = %v, want %v", resetAt, wantReset)
	}
}

func TestCredentialGroupDailyCostUsesCalculatedCostAndFrozenMultiplier(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			tracker, err := NewUsageTracker(&Config{
				Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"),
				BufferSize: 8, BatchSize: 8, FlushInterval: time.Hour,
				CleanupInterval: time.Hour, RetentionDays: 30,
			}, "Asia/Hong_Kong")
			require.NoError(t, err)
			t.Cleanup(func() { tracker.Close() })
			tracker.UpdatePricing(map[string]ModelPricing{ModelPricingKey(provider, "test-model", ""): {
				Input: 10, Output: 50, CacheRead: 1,
				LongContextThreshold: 272000, LongContextInputPrice: 20,
				LongContextOutputPrice: 75, LongContextCacheReadPrice: 2,
			}})
			key := EndpointMultiplierGroupKey(provider, "channel", "group", "main")
			mult := DefaultEndpointMultiplier()
			mult.CostMultiplier = .09
			tracker.UpdateEndpointMultipliers(map[string]EndpointMultiplier{key: mult})
			for _, requestID := range []string{"normal", "long"} {
				tracker.RecordRequestStart(requestID, "", "", "POST", "/", false)
				require.NoError(t, tracker.hotPool.Update(requestID, func(req *ActiveRequest) {
					req.Provider, req.Channel, req.GroupName, req.EndpointName = provider, "channel", "group", "main"
				}))
				require.NoError(t, tracker.UpdateActiveRequestMultiplier(requestID, mult))
				usage := &TokenUsage{InputTokens: 100000, OutputTokens: 1000, CacheReadTokens: 10000}
				if requestID == "long" {
					usage.InputTokens = 300000
				}
				// Codex input includes cached input; Claude input is already uncached.
				usage.InputIncludesCacheRead = provider == "codex"
				tracker.RecordRequestSuccess(requestID, "test-model", usage, time.Second)
			}
			require.NoError(t, tracker.ForceFlush())
			want := .64395 // (1.06 normal + 6.095 long) * .09, Claude input excludes cache.
			if provider == "codex" {
				want = .61695 // (.96 normal + 5.895 long) * .09.
			}
			ctx := context.Background()
			used, _, err := tracker.CredentialGroupDailyCost(ctx, provider, "channel", "group")
			require.NoError(t, err)
			require.InDelta(t, want, used, 1e-12)
			totals, err := tracker.QueryUsageStatsTotals(ctx, &QueryOptions{Provider: provider, Channel: "channel", GroupName: "group"})
			require.NoError(t, err)
			require.InDelta(t, want, totals.TotalCostUSD, 1e-12)
			for _, requestID := range []string{"normal", "long"} {
				var snapshotJSON string
				require.NoError(t, tracker.db.QueryRow("SELECT billing_rule_snapshot FROM request_logs WHERE request_id = ?", requestID).Scan(&snapshotJSON))
				var snapshot BillingDecisionSnapshot
				require.NoError(t, json.Unmarshal([]byte(snapshotJSON), &snapshot))
				require.Equal(t, .09, snapshot.GroupCostMultiplier)
				require.Equal(t, .09, snapshot.TotalCostMultiplier)
				require.Equal(t, requestID, snapshot.ContextMode)
			}
			// Current configuration changes apply to new requests and never multiply
			// the already settled cost a second time during daily aggregation.
			mult.CostMultiplier = 1
			tracker.UpdateEndpointMultipliers(map[string]EndpointMultiplier{key: mult})
			used, _, err = tracker.CredentialGroupDailyCost(ctx, provider, "channel", "group")
			require.NoError(t, err)
			require.InDelta(t, want, used, 1e-12)
		})
	}
}
