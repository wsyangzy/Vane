package tracking

import (
	"context"
	"testing"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
)

func TestArchiveBatchChangeCoversProvidersAndStartTimes(t *testing.T) {
	early := time.Date(2026, 10, 7, 23, 59, 0, 0, time.Local)
	late := time.Date(2026, 10, 8, 9, 0, 0, 0, time.Local)
	change := archiveBatchChange([]*ArchiveEvent{
		{Request: &ActiveRequest{Provider: "codex", StartTime: late}},
		{Request: &ActiveRequest{Provider: "", StartTime: early}}, // 空 provider 归一为 claude
		nil,
	})
	if change.All {
		t.Fatal("bounded batch must not invalidate everything")
	}
	if len(change.Providers) != 2 || change.Providers[0] != "codex" || change.Providers[1] != "claude" {
		t.Fatalf("providers = %v, want [codex claude]", change.Providers)
	}
	if !change.Start.Equal(early) || !change.End.Equal(late) {
		t.Fatalf("range = [%v, %v], want [%v, %v]", change.Start, change.End, early, late)
	}

	unbounded := archiveBatchChange([]*ArchiveEvent{
		{Request: &ActiveRequest{Provider: "claude", StartTime: late}},
		{Request: &ActiveRequest{Provider: "claude"}},
	})
	if !unbounded.Start.IsZero() || !unbounded.End.IsZero() {
		t.Fatalf("record without start_time must widen to all time, got [%v, %v]", unbounded.Start, unbounded.End)
	}
}

// 归档写入后只清依赖受影响 provider 与日期的统计缓存条目。
func TestArchiveWriteInvalidatesOnlyAffectedUsageStats(t *testing.T) {
	tracker := newUsageStatsCacheTestTracker(t)
	defer tracker.Close()
	tracker.ConfigureUsageStatsCache(appcache.Options{Name: "usage-stats-test", TTL: time.Hour, MaxEntries: 10, Enabled: true})

	var changes []appcache.Change
	tracker.SetUsageCacheInvalidationCallback(func(change appcache.Change) {
		changes = append(changes, change)
	})

	start := time.Now().Add(-time.Hour)
	end := time.Now().Add(time.Hour)
	claudeOpts := &QueryOptions{StartDate: &start, EndDate: &end, Provider: "claude"}
	lastWeekStart := start.AddDate(0, 0, -7)
	lastWeekEnd := end.AddDate(0, 0, -7)
	lastWeekOpts := &QueryOptions{StartDate: &lastWeekStart, EndDate: &lastWeekEnd, Provider: "codex"}
	for _, opts := range []*QueryOptions{claudeOpts, lastWeekOpts} {
		if _, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), opts); err != nil {
			t.Fatalf("prime cache: %v", err)
		}
	}

	persistCompletedRequest(t, tracker, "archive-invalidation-1", "claude", "model-a", 10, 5)
	if len(changes) == 0 {
		t.Fatal("expected archive write to notify app-level caches")
	}

	totals, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), claudeOpts)
	if err != nil {
		t.Fatal(err)
	}
	if totals.TotalRequests != 1 {
		t.Fatalf("affected entry must be reloaded, got %d requests", totals.TotalRequests)
	}
	if _, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), lastWeekOpts); err != nil {
		t.Fatal(err)
	}
	if stats := tracker.UsageStatsCacheStats(); stats.Misses != 3 || stats.Hits != 1 {
		t.Fatalf("expected only the affected entry to reload, got hits=%d misses=%d", stats.Hits, stats.Misses)
	}
}

func TestCleanupOldRecordsInvalidatesUsageCachesAfterSuccessfulCleanup(t *testing.T) {
	tracker := newUsageStatsCacheTestTracker(t)
	defer tracker.Close()
	tracker.retentionMu.Lock()
	tracker.config.RetentionDays = 1
	tracker.retentionMu.Unlock()
	tracker.ConfigureUsageStatsCache(appcache.Options{
		Name:         "usage-stats-test",
		TTL:          time.Minute,
		MaxEntries:   10,
		StaleOnError: true,
		Enabled:      true,
	})
	tracker.usageStatsCache.Set("sentinel", UsageStatsTotals{TotalRequests: 1})

	oldStartTime := time.Now().AddDate(0, 0, -3).Format("2006-01-02 15:04:05")
	if _, err := tracker.GetWriteDB().Exec(
		`INSERT INTO request_logs (request_id, start_time, status, provider) VALUES (?, ?, ?, ?)`,
		"cleanup-old-cache-invalidation", oldStartTime, "completed", "claude",
	); err != nil {
		t.Fatalf("insert old request log: %v", err)
	}

	called := 0
	tracker.SetUsageCacheInvalidationCallback(func(appcache.Change) {
		called++
	})

	if err := tracker.cleanupOldRecords(); err != nil {
		t.Fatalf("cleanupOldRecords failed: %v", err)
	}

	if called != 1 {
		t.Fatalf("expected cleanup success to invalidate app-level usage caches once, got %d", called)
	}
	if stats := tracker.UsageStatsCacheStats(); stats.Size != 0 {
		t.Fatalf("expected usage stats cache to be cleared, got size %d", stats.Size)
	}
}
