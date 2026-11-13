package app

import (
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestRequestListAndStatsShareMultipleModelFilter(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	t.Cleanup(func() { _ = tracker.Close() })
	for _, model := range []string{"model-a", "model-b", "other"} {
		tracker.RecordRequestStart(model, "127.0.0.1", "test", "POST", "/v1/responses", false)
		provider := "codex"
		tracker.RecordRequestUpdate(model, tracking.UpdateOptions{Provider: &provider, ModelName: &model})
	}
	a := &App{
		usageTracker:               tracker,
		endpointManager:            endpoint.NewManager(config.NewDefaultConfig()),
		endpointStore:              claudestore.NewSQLiteEndpointStore(tracker.GetDB()),
		claudeCredentialGroupStore: claudestore.NewSQLiteClaudeCredentialGroupStore(tracker.GetDB()),
	}
	models := []string{"model-a", "model-b"}
	requests, err := a.GetRequests(RequestQueryParams{Models: models, Provider: "codex", Page: 1, PageSize: 1})
	if err != nil || requests.Total != 2 || len(requests.Requests) != 1 {
		t.Fatalf("requests=%+v err=%v", requests, err)
	}
	stats, err := a.GetUsageStats(UsageStatsQueryParams{Models: models, Provider: "codex", StartDate: time.Now().Add(-time.Hour).Format(time.RFC3339), EndDate: time.Now().Add(time.Hour).Format(time.RFC3339)})
	if err != nil || stats.TotalRequests != 2 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}
