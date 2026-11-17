package tracking

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking/storage"
)

func TestCompletionWindowMergesHotPoolAndDatabaseInStableOrder(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema, err := storage.Schema()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("HKT", 8*60*60)
	since := time.Date(2026, 11, 17, 8, 46, 12, 0, location)
	before := since.Add(2 * time.Second)
	pool := NewHotPool(HotPoolConfig{CleanupInterval: time.Hour})
	t.Cleanup(func() { pool.Close() })
	tracker := &UsageTracker{readDB: db, location: location, hotPool: pool}
	for _, row := range []struct{ id, start, end, environment string }{
		{"old-start", "2026-11-16 23:00:00", "2026-11-17 08:46:12", "release"},
		{"too-old", "2026-11-17 08:46:00", "2026-11-17 08:46:11", "release"},
		{"exclusive-end", "2026-11-17 08:46:00", "2026-11-17 08:46:14", "release"},
		{"other-environment", "2026-11-17 08:46:00", "2026-11-17 08:46:12", "dev"},
	} {
		if _, err := db.Exec(`INSERT INTO request_logs(request_id,start_time,end_time,status,provider,environment) VALUES(?,?,?,'failed','codex',?)`, row.id, row.start, row.end, row.environment); err != nil {
			t.Fatal(err)
		}
	}
	end := since.Add(700 * time.Millisecond)
	hot := &ActiveRequest{RequestID: "zz-hot", StartTime: since.Add(-24 * time.Hour), EndTime: &end, Status: "failed", Provider: "codex"}
	if err := pool.Add(hot); err != nil {
		t.Fatal(err)
	}
	opts := &QueryOptions{CompletedSince: &since, CompletedBefore: &before, Status: "failed", Provider: "codex", Limit: 1}
	first, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].RequestID != "old-start" {
		t.Fatalf("first page = %#v", first)
	}
	// Archive the hot row between pages. Its timestamp loses sub-second
	// precision, but the second page must retain the same position.
	if _, err := db.Exec(`INSERT INTO request_logs(request_id,start_time,end_time,status,provider,environment) VALUES('zz-hot','2026-11-16 08:46:12','2026-11-17 08:46:12','failed','codex','release')`); err != nil {
		t.Fatal(err)
	}
	overlapOpts := *opts
	overlapOpts.Limit = 100
	overlap, overlapTotal, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &overlapOpts)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlap) != 2 || overlapTotal != 2 || overlap[0].RequestID == overlap[1].RequestID {
		t.Fatalf("overlapping hot/database request was counted twice: rows=%#v total=%d", overlap, overlapTotal)
	}
	pool.mu.Lock()
	delete(pool.requests, "zz-hot")
	pool.mu.Unlock()
	opts.Offset = 1
	second, total, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].RequestID != "zz-hot" || total != 2 {
		t.Fatalf("second page=%#v total=%d", second, total)
	}
	count, err := tracker.CountRequestDetails(context.Background(), opts)
	if err != nil || count != 2 {
		t.Fatalf("completion count=%d err=%v", count, err)
	}
}

func TestCompletionWindowHotPoolBoundsMatchArchivedSecondPrecision(t *testing.T) {
	since := time.Date(2026, 11, 17, 0, 0, 1, 0, time.UTC)
	before := since.Add(time.Second)
	opts := &QueryOptions{CompletedSince: &since, CompletedBefore: &before}
	for _, tc := range []struct {
		offset time.Duration
		want   bool
	}{{-time.Nanosecond, false}, {0, true}, {500 * time.Millisecond, true}, {time.Second, false}} {
		end := since.Add(tc.offset)
		if got := activeRequestMatchesFilters(&ActiveRequest{EndTime: &end}, opts); got != tc.want {
			t.Fatalf("offset=%v got=%v want=%v", tc.offset, got, tc.want)
		}
	}
	if activeRequestMatchesFilters(&ActiveRequest{}, opts) {
		t.Fatal("unfinished request matched a completion window")
	}
}

func TestCompletionWindowIndexesAreAddedToExistingHistoryArchives(t *testing.T) {
	tracker, archiveDir := newHistoryArchiveTestTracker(t)
	archivePath := filepath.Join(archiveDir, "2020-01.db")
	ctx := context.Background()
	err := tracker.withAttachedHistoryArchive(ctx, archivePath, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `INSERT INTO history_archive.request_logs(request_id,start_time,end_time,status,provider,environment) VALUES('long-archived','2020-01-01 00:00:00','2026-11-17 08:46:12','failed','codex','release')`); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `DROP INDEX history_archive.idx_history_request_completion`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// The startup reconciliation reopens existing archives through the same
	// schema path, so databases created before this change gain the new index.
	if err := tracker.reconcileHistoryArchives(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", archivePath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, parent, unused int
	var plan string
	if err := db.QueryRow(`EXPLAIN QUERY PLAN SELECT request_id FROM request_logs WHERE end_time >= ? AND end_time < ?`, "2026-11-17 08:46:12", "2026-11-17 08:46:14").Scan(&id, &parent, &unused, &plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "idx_history_request_completion") {
		t.Fatalf("completion query did not use archive index: %s", plan)
	}
	since := time.Date(2026, 11, 17, 8, 46, 12, 0, tracker.location)
	before := since.Add(2 * time.Second)
	rows, total, err := tracker.QueryRequestDetailsWithHotPool(ctx, &QueryOptions{Status: "failed", CompletedSince: &since, CompletedBefore: &before, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RequestID != "long-archived" || total != 1 {
		t.Fatalf("archived completion rows=%#v total=%d", rows, total)
	}
}
