package tracking

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newHistoryArchiveTestTracker(t *testing.T) (*UsageTracker, string) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "usage.db")
	archiveDir := filepath.Join(root, "archive", "dev")
	tracker, err := NewUsageTracker(&Config{
		Enabled:          true,
		DatabasePath:     dbPath,
		BufferSize:       100,
		BatchSize:        10,
		FlushInterval:    time.Hour,
		MaxRetry:         1,
		RetentionDays:    0,
		CleanupInterval:  24 * time.Hour,
		ArchiveDirectory: archiveDir,
		HotPool:          &HotPoolSettings{Enabled: false},
	}, "Asia/Tokyo")
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	t.Cleanup(func() { _ = tracker.Close() })
	return tracker, archiveDir
}

func insertHistoryRequest(t *testing.T, tracker *UsageTracker, requestID, startTime, status, provider string, payloadSize int) {
	t.Helper()
	userAgent := "test"
	if payloadSize > 0 {
		userAgent = strings.Repeat("x", payloadSize)
	}
	_, err := tracker.GetWriteDB().Exec(`
		INSERT INTO request_logs (
			request_id, user_agent, start_time, end_time, status, provider,
			model_name, endpoint_name, input_tokens, output_tokens, total_cost_usd
		) VALUES (?, ?, ?, ?, ?, ?, 'model-test', 'endpoint-test', 10, 5, 0.01)
	`, requestID, userAgent, startTime, startTime, status, provider)
	if err != nil {
		t.Fatalf("insert request %s: %v", requestID, err)
	}
}

func TestNewUsageTrackerArchivesClosedMonthsOnStartup(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "usage.db")
	archiveDir := filepath.Join(root, "archive", "dev")
	config := &Config{
		Enabled:          true,
		DatabasePath:     dbPath,
		BufferSize:       100,
		BatchSize:        10,
		FlushInterval:    time.Hour,
		MaxRetry:         1,
		RetentionDays:    0,
		CleanupInterval:  24 * time.Hour,
		ArchiveDirectory: archiveDir,
		HotPool:          &HotPoolSettings{Enabled: false},
	}

	first, err := NewUsageTracker(config, "Asia/Tokyo")
	if err != nil {
		t.Fatalf("create first tracker: %v", err)
	}
	oldMonth := first.now().AddDate(0, -1, 0).Format("2006-01")
	insertHistoryRequest(t, first, "startup-archive", oldMonth+"-15 10:00:00", "completed", "claude", 0)
	if err := first.Close(); err != nil {
		t.Fatalf("close first tracker: %v", err)
	}

	second, err := NewUsageTracker(config, "Asia/Tokyo")
	if err != nil {
		t.Fatalf("create second tracker: %v", err)
	}
	defer second.Close()
	var activeCount int
	if err := second.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'startup-archive'`).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 0 {
		t.Fatalf("closed-month request remains active after startup: %d", activeCount)
	}
	archiveDB, err := sql.Open("sqlite", filepath.Join(archiveDir, oldMonth+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer archiveDB.Close()
	var archivedCount int
	if err := archiveDB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'startup-archive'`).Scan(&archivedCount); err != nil {
		t.Fatal(err)
	}
	if archivedCount != 1 {
		t.Fatalf("startup archive count = %d", archivedCount)
	}
}

func TestArchiveOldestClosedMonthMovesOnlyTrackingData(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	oldMonth := tracker.now().AddDate(0, -1, 0).Format("2006-01")
	oldDay := oldMonth + "-15"
	today := tracker.now().Format("2006-01-02")
	insertHistoryRequest(t, tracker, "archive-old", oldDay+" 10:00:00", "completed", "codex", 0)
	insertHistoryRequest(t, tracker, "archive-current", today+" 10:00:00", "completed", "claude", 0)

	month, moved, err := tracker.archiveOldestClosedMonth(context.Background())
	if err != nil {
		t.Fatalf("archive oldest month: %v", err)
	}
	if !moved || month != oldMonth {
		t.Fatalf("unexpected archive result: month=%q moved=%v", month, moved)
	}

	var activeOld, activeCurrent, settingsTable int
	if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-old'`).Scan(&activeOld); err != nil {
		t.Fatal(err)
	}
	if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-current'`).Scan(&activeCurrent); err != nil {
		t.Fatal(err)
	}
	if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'settings'`).Scan(&settingsTable); err != nil {
		t.Fatal(err)
	}
	if activeOld != 0 || activeCurrent != 1 || settingsTable != 1 {
		t.Fatalf("unexpected active database state: old=%d current=%d settings=%d", activeOld, activeCurrent, settingsTable)
	}

	archivePath := filepath.Join(archiveDir, oldMonth+".db")
	archiveDB, err := sql.Open("sqlite", archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archiveDB.Close()
	var archived, summaries, providerCount int
	if err := archiveDB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-old'`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if err := archiveDB.QueryRow(`SELECT COUNT(*) FROM usage_summary WHERE date = ?`, oldDay).Scan(&summaries); err != nil {
		t.Fatal(err)
	}
	if err := archiveDB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE provider = 'codex'`).Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if archived != 1 || summaries != 1 || providerCount != 1 {
		t.Fatalf("unexpected archive state: requests=%d summaries=%d codex=%d", archived, summaries, providerCount)
	}
}

func TestArchiveClosedMonthLeavesNonTerminalRequestActive(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	oldMonth := tracker.now().AddDate(0, -1, 0).Format("2006-01")
	oldDay := oldMonth + "-15"
	insertHistoryRequest(t, tracker, "archive-complete", oldDay+" 10:00:00", "completed", "claude", 0)
	insertHistoryRequest(t, tracker, "archive-pending", oldDay+" 11:00:00", "pending", "claude", 0)

	month, moved, err := tracker.archiveOldestClosedMonth(context.Background())
	if err != nil {
		t.Fatalf("archive oldest month: %v", err)
	}
	if !moved || month != oldMonth {
		t.Fatalf("completed history should move: month=%q moved=%v", month, moved)
	}
	var completedActive, pendingActive int
	if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-complete'`).Scan(&completedActive); err != nil {
		t.Fatal(err)
	}
	if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-pending'`).Scan(&pendingActive); err != nil {
		t.Fatal(err)
	}
	if completedActive != 0 || pendingActive != 1 {
		t.Fatalf("unexpected active rows: completed=%d pending=%d", completedActive, pendingActive)
	}
	archiveDB, err := sql.Open("sqlite", filepath.Join(archiveDir, oldMonth+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer archiveDB.Close()
	var archived int
	if err := archiveDB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-complete'`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatalf("completed request was not archived: %d", archived)
	}
}

func TestHistoryRecoveryKeepsActiveDuplicate(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	oldDay := tracker.now().AddDate(0, -1, 0).Format("2006-01") + "-15"
	insertHistoryRequest(t, tracker, "archive-duplicate", oldDay+" 10:00:00", "completed", "claude", 0)
	if _, moved, err := tracker.archiveOldestClosedMonth(context.Background()); err != nil || !moved {
		t.Fatalf("archive request: moved=%v err=%v", moved, err)
	}

	archivePath := filepath.Join(archiveDir, oldDay[:7]+".db")
	if _, err := tracker.GetWriteDB().Exec(`ATTACH DATABASE ? AS recovery_archive`, archivePath); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.GetWriteDB().Exec(`INSERT INTO main.request_logs SELECT * FROM recovery_archive.request_logs WHERE request_id = 'archive-duplicate'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.GetWriteDB().Exec(`DETACH DATABASE recovery_archive`); err != nil {
		t.Fatal(err)
	}

	if err := tracker.reconcileHistoryArchives(); err != nil {
		t.Fatalf("reconcile history: %v", err)
	}
	var activeCount int
	if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-duplicate'`).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 {
		t.Fatalf("active duplicate should win, got %d rows", activeCount)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("empty duplicate archive should be removed, stat err=%v", err)
	}
}

func TestPositiveRetentionDeletesExpiredArchive(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	oldDay := tracker.now().AddDate(0, -2, 0).Format("2006-01") + "-15"
	insertHistoryRequest(t, tracker, "archive-expired", oldDay+" 10:00:00", "completed", "claude", 0)
	if _, moved, err := tracker.archiveOldestClosedMonth(context.Background()); err != nil || !moved {
		t.Fatalf("archive request: moved=%v err=%v", moved, err)
	}

	tracker.retentionMu.Lock()
	tracker.config.RetentionDays = 3
	tracker.retentionMu.Unlock()
	if err := tracker.cleanupOldRecords(); err != nil {
		t.Fatalf("cleanup expired history: %v", err)
	}
	archivePath := filepath.Join(archiveDir, oldDay[:7]+".db")
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("expired empty archive should be removed, stat err=%v", err)
	}
}

func TestMonthlyArchiveIncludesLegacyTerminalStatuses(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	oldMonth := tracker.now().AddDate(0, -1, 0).Format("2006-01")
	oldDay := oldMonth + "-15"
	insertHistoryRequest(t, tracker, "archive-legacy", oldDay+" 10:00:00", "partial_recovery", "codex", 0)

	if err := tracker.archiveClosedMonths(context.Background()); err != nil {
		t.Fatalf("archive closed months: %v", err)
	}
	archivePath := filepath.Join(archiveDir, oldMonth+".db")
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("monthly archive not created: %v", err)
	}
	var activeCount int
	if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = 'archive-legacy'`).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 0 {
		t.Fatalf("legacy terminal request remains active: %d", activeCount)
	}
}

func TestHistoryQueriesMergeActiveAndArchive(t *testing.T) {
	tracker, _ := newHistoryArchiveTestTracker(t)
	oldTime := tracker.now().AddDate(0, -1, 0)
	oldDay := oldTime.Format("2006-01-02")
	currentTime := tracker.now()
	insertHistoryRequest(t, tracker, "history-query-old", oldDay+" 10:00:00", "completed", "codex", 0)
	insertHistoryRequest(t, tracker, "history-query-current", currentTime.Format("2006-01-02 15:04:05"), "completed", "claude", 0)
	if _, moved, err := tracker.archiveOldestClosedMonth(context.Background()); err != nil || !moved {
		t.Fatalf("archive request: moved=%v err=%v", moved, err)
	}

	location := currentTime.Location()
	startTime, _ := time.ParseInLocation("2006-01-02 15:04:05", oldDay+" 00:00:00", location)
	endTime := currentTime.Add(24 * time.Hour)
	opts := &QueryOptions{StartDate: &startTime, EndDate: &endTime}
	details, err := tracker.QueryRequestDetails(context.Background(), opts)
	if err != nil {
		t.Fatalf("query merged details: %v", err)
	}
	if len(details) != 2 || details[0].RequestID != "history-query-current" || details[1].RequestID != "history-query-old" {
		t.Fatalf("unexpected merged details: %+v", details)
	}
	page, err := tracker.QueryRequestDetails(context.Background(), &QueryOptions{StartDate: &startTime, EndDate: &endTime, Limit: 1, Offset: 1})
	if err != nil || len(page) != 1 || page[0].RequestID != "history-query-old" {
		t.Fatalf("unexpected merged page: %+v err=%v", page, err)
	}
	count, err := tracker.CountRequestDetails(context.Background(), opts)
	if err != nil || count != 2 {
		t.Fatalf("unexpected merged count: %d err=%v", count, err)
	}

	totals, err := tracker.QueryUsageStatsTotals(context.Background(), opts)
	if err != nil || totals.TotalRequests != 2 || totals.TotalTokens != 30 {
		t.Fatalf("unexpected merged totals: %+v err=%v", totals, err)
	}
	tokenTotals, err := tracker.QueryTokenUsageTotals(context.Background(), startTime, endTime, "")
	if err != nil || tokenTotals.TotalTokens != 30 {
		t.Fatalf("unexpected token totals: %+v err=%v", tokenTotals, err)
	}
	buckets, err := tracker.QueryTokenUsageBuckets(context.Background(), opts, "day")
	if err != nil || len(buckets) < 2 {
		t.Fatalf("unexpected token buckets: %+v err=%v", buckets, err)
	}

	summaries, err := tracker.QueryUsageSummary(context.Background(), &QueryOptions{StartDate: &startTime, EndDate: &endTime})
	if err != nil || len(summaries) != 1 || summaries[0].Date != oldDay {
		t.Fatalf("unexpected archived summaries: %+v err=%v", summaries, err)
	}
	costs, err := tracker.GetEndpointCostsForDate(context.Background(), oldDay)
	if err != nil || len(costs) != 1 || costs[0].RequestCount != 1 || costs[0].Provider != "codex" {
		t.Fatalf("unexpected endpoint costs: %+v err=%v", costs, err)
	}
	detailed, err := tracker.GetUsageStats(context.Background(), startTime, endTime)
	if err != nil || detailed.TotalRequests != 2 || detailed.ModelStats["model-test"].RequestCount != 2 {
		t.Fatalf("unexpected detailed stats: %+v err=%v", detailed, err)
	}

	exported, err := tracker.ExportToJSON(context.Background(), startTime, endTime, "", "", "")
	if err != nil {
		t.Fatalf("export merged history: %v", err)
	}
	var exportedDetails []RequestDetail
	if err := json.Unmarshal(exported, &exportedDetails); err != nil || len(exportedDetails) != 2 {
		t.Fatalf("unexpected export: count=%d err=%v", len(exportedDetails), err)
	}
}

func TestHistoryQueryReportsCorruptArchiveOnlyWhenRangeIntersects(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	corruptPath := filepath.Join(archiveDir, "2020-01.db")
	if err := os.WriteFile(corruptPath, []byte("not a sqlite database"), 0644); err != nil {
		t.Fatal(err)
	}

	recentStart := tracker.now().AddDate(0, 0, -1)
	recentEnd := tracker.now()
	if _, err := tracker.QueryUsageStatsTotals(context.Background(), &QueryOptions{StartDate: &recentStart, EndDate: &recentEnd}); err != nil {
		t.Fatalf("out-of-range corrupt archive affected query: %v", err)
	}

	archiveStart := time.Date(2020, time.January, 1, 0, 0, 0, 0, tracker.now().Location())
	archiveEnd := archiveStart.AddDate(0, 1, 0)
	_, err := tracker.QueryUsageStatsTotals(context.Background(), &QueryOptions{StartDate: &archiveStart, EndDate: &archiveEnd})
	if err == nil || !strings.Contains(err.Error(), "2020-01") {
		t.Fatalf("expected month-specific corrupt archive error, got %v", err)
	}
}

// 归档是独立文件，活动库的止血迁移够不到它；历史归档中的隐私明文必须在启动时一并清除。
func TestReconcileHistoryArchivesPurgesLegacyPrivacyPlaintext(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	archivePath := filepath.Join(archiveDir, "2020-01.db")

	archiveDB, err := sql.Open("sqlite", archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	if _, err := archiveDB.Exec(`
		CREATE TABLE request_logs (
			request_id TEXT,
			start_time TEXT,
			status TEXT,
			environment TEXT,
			privacy_redaction_matches TEXT
		);
		INSERT INTO request_logs VALUES ('legacy-1', '2020-01-05 00:00:00+08:00', 'completed', 'dev', '[{"raw":"13800138000"}]');
	`); err != nil {
		t.Fatalf("seed legacy archive: %v", err)
	}
	_ = archiveDB.Close()

	if err := tracker.reconcileHistoryArchives(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	archiveDB, err = sql.Open("sqlite", archivePath)
	if err != nil {
		t.Fatalf("reopen archive: %v", err)
	}
	defer archiveDB.Close()

	var leaked int
	err = archiveDB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE privacy_redaction_matches IS NOT NULL`).Scan(&leaked)
	if err == nil && leaked != 0 {
		t.Fatalf("归档仍残留 %d 行隐私明文", leaked)
	}
	if err != nil && !strings.Contains(err.Error(), "no such column") {
		t.Fatalf("query archive: %v", err)
	}

	var rows int
	if err := archiveDB.QueryRow(`SELECT COUNT(*) FROM request_logs`).Scan(&rows); err != nil {
		t.Fatalf("count archive rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("归档行数应保持 1，实际 %d", rows)
	}
}
