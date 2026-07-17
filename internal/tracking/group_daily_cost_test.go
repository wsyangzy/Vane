package tracking

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"
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
