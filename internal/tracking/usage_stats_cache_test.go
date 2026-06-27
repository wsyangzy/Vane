package tracking

import (
	"context"
	"fmt"
	"testing"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
)

func TestUsageStatsCacheCachesDBTotalsButAddsHotPoolEveryCall(t *testing.T) {
	tracker := newUsageStatsCacheTestTracker(t)
	defer tracker.Close()

	tracker.ConfigureUsageStatsCache(appcache.Options{
		Name:         "usage-stats-test",
		TTL:          time.Minute,
		MaxEntries:   20,
		StaleOnError: true,
		Enabled:      true,
	})

	persistCompletedRequest(t, tracker, "db-claude-1", "claude", "model-a", 100, 50)

	start := time.Now().Add(-time.Hour)
	end := time.Now().Add(time.Hour)
	opts := &QueryOptions{StartDate: &start, EndDate: &end, Provider: "claude"}

	first, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), opts)
	if err != nil {
		t.Fatalf("first query failed: %v", err)
	}
	if first.TotalRequests != 1 {
		t.Fatalf("expected 1 DB request, got %d", first.TotalRequests)
	}
	if stats := tracker.UsageStatsCacheStats(); stats.Misses != 1 || stats.Hits != 0 {
		t.Fatalf("expected first query to miss cache, got hits=%d misses=%d", stats.Hits, stats.Misses)
	}

	startActiveRequest(t, tracker, "hot-claude-1", "claude", "model-hot", "processing")

	second, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), opts)
	if err != nil {
		t.Fatalf("second query failed: %v", err)
	}
	if second.TotalRequests != 2 {
		t.Fatalf("expected cached DB total plus live HotPool request, got %d", second.TotalRequests)
	}
	if second.TotalTokens <= first.TotalTokens {
		t.Fatalf("expected live HotPool tokens to be added, first=%d second=%d", first.TotalTokens, second.TotalTokens)
	}
	if stats := tracker.UsageStatsCacheStats(); stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("expected second query to hit cache, got hits=%d misses=%d", stats.Hits, stats.Misses)
	}
}

func TestUsageStatsCacheKeyIncludesProviderAndDateRange(t *testing.T) {
	tracker := newUsageStatsCacheTestTracker(t)
	defer tracker.Close()

	tracker.ConfigureUsageStatsCache(appcache.Options{
		Name:       "usage-stats-test",
		TTL:        time.Minute,
		MaxEntries: 20,
		Enabled:    true,
	})

	persistCompletedRequest(t, tracker, "db-claude-1", "claude", "model-a", 100, 50)
	persistCompletedRequest(t, tracker, "db-codex-1", "codex", "model-b", 200, 75)

	start := time.Now().Add(-time.Hour)
	end := time.Now().Add(time.Hour)
	claudeOpts := &QueryOptions{StartDate: &start, EndDate: &end, Provider: "claude"}
	codexOpts := &QueryOptions{StartDate: &start, EndDate: &end, Provider: "codex"}
	laterStart := start.Add(2 * time.Hour)
	laterEnd := end.Add(2 * time.Hour)
	laterClaudeOpts := &QueryOptions{StartDate: &laterStart, EndDate: &laterEnd, Provider: "claude"}

	if totals, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), claudeOpts); err != nil {
		t.Fatalf("claude query failed: %v", err)
	} else if totals.TotalRequests != 1 {
		t.Fatalf("expected 1 claude request, got %d", totals.TotalRequests)
	}
	if totals, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), codexOpts); err != nil {
		t.Fatalf("codex query failed: %v", err)
	} else if totals.TotalRequests != 1 {
		t.Fatalf("expected 1 codex request, got %d", totals.TotalRequests)
	}
	if totals, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), laterClaudeOpts); err != nil {
		t.Fatalf("later claude query failed: %v", err)
	} else if totals.TotalRequests != 0 {
		t.Fatalf("expected later date range to be isolated from cached claude total, got %d", totals.TotalRequests)
	}
	if _, err := tracker.QueryUsageStatsTotalsWithHotPool(context.Background(), claudeOpts); err != nil {
		t.Fatalf("repeated claude query failed: %v", err)
	}

	stats := tracker.UsageStatsCacheStats()
	if stats.Misses != 3 {
		t.Fatalf("expected 3 distinct key misses, got %d", stats.Misses)
	}
	if stats.Hits != 1 {
		t.Fatalf("expected repeated claude query to hit cache, got %d hits", stats.Hits)
	}
}

func newUsageStatsCacheTestTracker(t *testing.T) *UsageTracker {
	t.Helper()

	tracker, err := NewUsageTracker(&Config{
		Enabled:         true,
		DatabasePath:    ":memory:",
		BufferSize:      100,
		BatchSize:       10,
		FlushInterval:   time.Hour,
		MaxRetry:        1,
		CleanupInterval: 24 * time.Hour,
		RetentionDays:   30,
		ModelPricing: map[string]ModelPricing{
			"model-a":   {Input: 1, Output: 2, CacheCreation: 1, CacheRead: 0.1},
			"model-b":   {Input: 1, Output: 2, CacheCreation: 1, CacheRead: 0.1},
			"model-hot": {Input: 1, Output: 2, CacheCreation: 1, CacheRead: 0.1},
		},
		HotPool: &HotPoolSettings{
			Enabled:          true,
			MaxAge:           time.Hour,
			MaxSize:          100,
			CleanupInterval:  time.Hour,
			ArchiveOnCleanup: true,
		},
	})
	if err != nil {
		t.Fatalf("NewUsageTracker failed: %v", err)
	}
	return tracker
}

func persistCompletedRequest(t *testing.T, tracker *UsageTracker, requestID, provider, model string, input, output int64) {
	t.Helper()

	tracker.RecordRequestStart(requestID, "127.0.0.1", "test-agent", "POST", "/v1/messages", false)
	status := "processing"
	tracker.RecordRequestUpdate(requestID, UpdateOptions{
		Provider:  &provider,
		Status:    &status,
		ModelName: &model,
	})
	tracker.RecordRequestSuccess(requestID, model, &TokenUsage{
		InputTokens:  input,
		OutputTokens: output,
	}, 100*time.Millisecond)
	if err := tracker.ForceFlush(); err != nil {
		t.Fatalf("ForceFlush failed: %v", err)
	}
}

func startActiveRequest(t *testing.T, tracker *UsageTracker, requestID, provider, model, status string) {
	t.Helper()

	tracker.RecordRequestStart(requestID, "127.0.0.1", "test-agent", "POST", "/v1/messages", false)
	endpointName := fmt.Sprintf("%s-endpoint", provider)
	tracker.RecordRequestUpdate(requestID, UpdateOptions{
		Provider:     &provider,
		Status:       &status,
		ModelName:    &model,
		EndpointName: &endpointName,
	})
	tracker.UpdateActiveRequestTokens(requestID, 25, 5, 0, 0)
}
