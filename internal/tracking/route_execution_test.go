package tracking

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"ai-relay-forwarder/internal/routing"
)

func TestRouteExecutionSurvivesHotPoolSQLiteReplayAndMonthlyArchive(t *testing.T) {
	root := t.TempDir()
	archiveDir := filepath.Join(root, "history")
	tracker, err := NewUsageTracker(&Config{Enabled: true, DatabasePath: filepath.Join(root, "usage.db"), ArchiveDirectory: archiveDir, BufferSize: 10, BatchSize: 10, FlushInterval: time.Hour, MaxRetry: 1, CleanupInterval: time.Hour, RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tracker.Close() })
	ledger := &routing.Ledger{}
	candidate := routing.Candidate{Provider: "codex", Protocol: "responses", Channel: "B", Credential: "group/7", Endpoint: "20", Model: "y"}
	ledger.Skip(candidate, "state_backend_incompatible")
	ledger.Begin(candidate)
	snapshot := (routing.Snapshot{Kind: routing.KindModelRule, Execution: ledger, Attempts: 1, WaitMs: 12}).JSON(2)
	for _, id := range []string{"new-trace", "legacy-trace"} {
		tracker.RecordRequestStart(id, "127.0.0.1", "test", "POST", "/v1/responses", false)
		if id == "new-trace" {
			tracker.RecordRequestUpdate(id, UpdateOptions{RouteSnapshot: &snapshot})
		}
		req, ok := tracker.hotPool.Get(id)
		if !ok {
			t.Fatal("missing hot request")
		}
		if id == "new-trace" && tracker.ActiveRequestToDetail(req).RouteSnapshot != snapshot {
			t.Fatal("hot detail dropped execution")
		}
		tracker.RecordRequestSuccessWithHTTPStatus(id, "y", &TokenUsage{InputTokens: 2, OutputTokens: 1}, time.Millisecond, 200)
	}
	if err := tracker.ForceFlush(); err != nil {
		t.Fatal(err)
	}
	details, err := tracker.QueryRequestDetails(context.Background(), &QueryOptions{Limit: 10})
	if err != nil || len(details) != 2 {
		t.Fatalf("details=%d err=%v", len(details), err)
	}
	for _, detail := range details {
		if detail.RequestID == "new-trace" && detail.RouteSnapshot != snapshot {
			t.Fatal("sqlite detail dropped execution")
		}
		if detail.RequestID == "legacy-trace" && detail.RouteSnapshot != "" {
			t.Fatal("legacy trace changed")
		}
	}
	// Exercise failed-archive serialization/replay using the same optional JSON.
	manager, _ := newReplayTestManager(t, filepath.Join(root, "replay.db"))
	req := replayTestRequest("replayed-trace")
	req.RouteSnapshot = snapshot
	writeReplayRecord(t, manager.getDumpFilePath(), req, "release")
	if err := manager.ReplayFailedArchives(); err != nil {
		t.Fatal(err)
	}
	var replayed string
	if err := manager.adapter.GetReadDB().QueryRow(`SELECT route_snapshot FROM request_logs WHERE request_id=?`, req.RequestID).Scan(&replayed); err != nil {
		t.Fatal(err)
	}
	if replayed != snapshot {
		t.Fatal("failed archive replay dropped execution")
	}
	oldMonth := tracker.now().AddDate(0, -1, 0).Format("2006-01")
	if _, err := tracker.GetWriteDB().Exec(`UPDATE request_logs SET start_time=?, end_time=?`, oldMonth+"-15 10:00:00", oldMonth+"-15 10:00:01"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.archiveClosedMonths(context.Background()); err != nil {
		t.Fatal(err)
	}
	archive, err := sql.Open("sqlite", filepath.Join(archiveDir, oldMonth+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var archived string
	if err := archive.QueryRow(`SELECT route_snapshot FROM request_logs WHERE request_id='new-trace'`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	var decoded routing.Snapshot
	if err := json.Unmarshal([]byte(archived), &decoded); err != nil {
		t.Fatal(err)
	}
	if archived != snapshot || decoded.Execution == nil || len(decoded.Execution.Events) != 2 {
		t.Fatal("monthly archive dropped execution")
	}
}
