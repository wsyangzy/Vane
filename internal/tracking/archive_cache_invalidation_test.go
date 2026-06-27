package tracking

import (
	"testing"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
)

func TestArchiveSuccessCallbackInvalidatesUsageCache(t *testing.T) {
	manager := &ArchiveManager{}
	called := 0

	manager.SetArchiveSuccessCallback(func() {
		called++
	})
	manager.notifyArchiveSuccess()

	if called != 1 {
		t.Fatalf("expected archive success callback to run once, got %d", called)
	}
}

func TestCleanupOldRecordsInvalidatesUsageCachesAfterSuccessfulCleanup(t *testing.T) {
	tracker := newUsageStatsCacheTestTracker(t)
	defer tracker.Close()
	tracker.config.RetentionDays = 1
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
	tracker.SetUsageCacheInvalidationCallback(func() {
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
