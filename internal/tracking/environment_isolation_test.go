package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newEnvironmentTestTracker(t *testing.T, environment string) *UsageTracker {
	t.Helper()
	tracker, err := NewUsageTracker(&Config{
		Enabled:         true,
		Environment:     environment,
		DatabasePath:    filepath.Join(t.TempDir(), "usage.db"),
		BufferSize:      32,
		BatchSize:       8,
		FlushInterval:   time.Hour,
		MaxRetry:        1,
		CleanupInterval: time.Hour,
		RetentionDays:   30,
		HotPool:         &HotPoolSettings{Enabled: false},
	})
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	t.Cleanup(func() { _ = tracker.Close() })
	return tracker
}

func TestEnvironmentScopedQueries(t *testing.T) {
	tracker := newEnvironmentTestTracker(t, "dev")
	ctx := context.Background()
	_, err := tracker.GetWriteDB().ExecContext(ctx, `
		INSERT INTO request_logs (request_id, start_time, status, provider, environment)
		VALUES
			('dev-request', '2026-08-26 10:00:00', 'completed', 'claude', 'dev'),
			('release-request', '2026-08-26 10:01:00', 'completed', 'claude', 'release')`)
	if err != nil {
		t.Fatalf("seed requests: %v", err)
	}

	details, err := tracker.QueryRequestDetails(ctx, &QueryOptions{})
	if err != nil {
		t.Fatalf("query request details: %v", err)
	}
	if len(details) != 1 || details[0].RequestID != "dev-request" {
		t.Fatalf("expected only dev request, got %#v", details)
	}

	count, err := tracker.CountRequestDetails(ctx, &QueryOptions{})
	if err != nil {
		t.Fatalf("count request details: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected dev count 1, got %d", count)
	}

	totals, err := tracker.QueryUsageStatsTotals(ctx, &QueryOptions{})
	if err != nil {
		t.Fatalf("query usage totals: %v", err)
	}
	if totals.TotalRequests != 1 {
		t.Fatalf("expected dev totals 1, got %#v", totals)
	}
}

func TestCleanupCrashedRequestsScopesEnvironment(t *testing.T) {
	tracker := newEnvironmentTestTracker(t, "dev")
	ctx := context.Background()
	old := time.Now().Add(-time.Hour).In(tracker.location).Format("2006-01-02 15:04:05")
	_, err := tracker.GetWriteDB().ExecContext(ctx, `
		INSERT INTO request_logs (request_id, start_time, status, updated_at, environment)
		VALUES (?, ?, 'processing', ?, 'dev'), (?, ?, 'processing', ?, 'release')`,
		"stale-dev", old, old, "stale-release", old, old)
	if err != nil {
		t.Fatalf("seed stale requests: %v", err)
	}

	if err := tracker.cleanupCrashedRequests(); err != nil {
		t.Fatalf("cleanup crashed requests: %v", err)
	}

	statuses := map[string]string{}
	rows, err := tracker.GetReadDB().QueryContext(ctx,
		"SELECT request_id, status FROM request_logs WHERE request_id IN ('stale-dev', 'stale-release')")
	if err != nil {
		t.Fatalf("query stale requests: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan stale request: %v", err)
		}
		statuses[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stale requests: %v", err)
	}
	if statuses["stale-dev"] != "failed" || statuses["stale-release"] != "processing" {
		t.Fatalf("unexpected scoped cleanup result: %#v", statuses)
	}
}

func TestSQLiteAdapterRebuildsUsageSummaryEnvironmentUnique(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-summary.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE usage_summary (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL,
			model_name TEXT NOT NULL,
			endpoint_name TEXT NOT NULL,
			group_name TEXT,
			provider TEXT NOT NULL DEFAULT 'claude',
			request_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			error_count INTEGER DEFAULT 0,
			total_input_tokens INTEGER DEFAULT 0,
			total_output_tokens INTEGER DEFAULT 0,
			total_cache_creation_tokens INTEGER DEFAULT 0,
			total_cache_read_tokens INTEGER DEFAULT 0,
			total_cost_usd REAL DEFAULT 0,
			avg_duration_ms REAL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(date, model_name, endpoint_name, group_name, provider)
		);
		INSERT INTO usage_summary (date, model_name, endpoint_name, group_name, provider)
		VALUES ('2026-08-26', 'model', 'endpoint', '', 'claude');
		CREATE INDEX idx_usage_summary_date ON usage_summary(date);
		CREATE INDEX idx_usage_summary_provider ON usage_summary(provider);`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy summary: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter: %v", err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatalf("adapter.Open: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("adapter.InitSchema: %v", err)
	}

	var columns string
	rows, err := adapter.GetReadDB().Query("PRAGMA index_list(usage_summary)")
	if err != nil {
		t.Fatalf("list summary indexes: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan summary index: %v", err)
		}
		if unique == 0 {
			continue
		}
		indexRows, err := adapter.GetReadDB().Query(fmt.Sprintf("PRAGMA index_info('%s')", name))
		if err != nil {
			t.Fatalf("inspect summary index %s: %v", name, err)
		}
		var names []string
		for indexRows.Next() {
			var seqno, cid int
			var column string
			if err := indexRows.Scan(&seqno, &cid, &column); err != nil {
				_ = indexRows.Close()
				t.Fatalf("scan summary index %s: %v", name, err)
			}
			names = append(names, column)
		}
		_ = indexRows.Close()
		if len(names) == 6 {
			columns = fmt.Sprint(names)
		}
	}
	if columns == "" {
		t.Fatalf("expected six-column usage_summary unique index")
	}

	_, err = adapter.GetWriteDB().Exec(`
		INSERT INTO usage_summary (date, model_name, endpoint_name, group_name, provider, environment)
		VALUES ('2026-08-26', 'model', 'endpoint', '', 'claude', 'dev')
		ON CONFLICT(date, model_name, endpoint_name, group_name, provider, environment)
		DO UPDATE SET request_count = usage_summary.request_count + 1`)
	if err != nil {
		t.Fatalf("environment-scoped usage_summary upsert: %v", err)
	}

	var count int
	if err := adapter.GetReadDB().QueryRow(
		"SELECT COUNT(*) FROM usage_summary WHERE date = '2026-08-26'").Scan(&count); err != nil {
		t.Fatalf("count migrated summaries: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected release and dev summary rows, got %d", count)
	}
}
