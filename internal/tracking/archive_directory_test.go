package tracking

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveDirectorySwitchPreservesWALHistoryAndRejectsOccupiedTargets(t *testing.T) {
	tracker, original := newHistoryArchiveTestTracker(t)
	month := previousHistoryMonthStart(tracker.now()).Format("2006-01")
	insertHistoryRequest(t, tracker, "move-history", month+"-15 10:00:00", "completed", "codex", 0)
	if _, moved, err := tracker.archiveOldestClosedMonth(context.Background()); err != nil || !moved {
		t.Fatalf("archive: %t %v", moved, err)
	}
	// Keep an independent WAL connection open: copying just the .db would lose
	// this committed row, while VACUUM INTO includes it.
	db, err := sql.Open("sqlite", filepath.Join(original, month+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE switch_probe(value TEXT); INSERT INTO switch_probe VALUES('committed-wal')`); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "history", "dev")
	if err := tracker.SwitchArchiveDirectory(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	if tracker.ArchiveDirectory() != destination {
		t.Fatal("readers did not switch")
	}
	copyDB, err := sql.Open("sqlite", filepath.Join(destination, month+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var value string
	if err := copyDB.QueryRow(`SELECT value FROM switch_probe`).Scan(&value); err != nil || value != "committed-wal" {
		t.Fatalf("WAL snapshot lost: %q %v", value, err)
	}
	var count int
	if err := copyDB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id='move-history'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("history lost: %d %v", count, err)
	}
	if _, err := os.Stat(filepath.Join(original, month+".db")); err != nil {
		t.Fatal("source rollback copy removed", err)
	}
	if err := tracker.SwitchArchiveDirectory(context.Background(), original); err == nil {
		t.Fatal("occupied destination accepted")
	}
	if tracker.ArchiveDirectory() != destination {
		t.Fatal("failed switch changed runtime path")
	}
}

func TestArchiveDirectoryCanceledCopyLeavesRuntimeAndTargetIntact(t *testing.T) {
	tracker, original := newHistoryArchiveTestTracker(t)
	month := previousHistoryMonthStart(tracker.now()).Format("2006-01")
	insertHistoryRequest(t, tracker, "cancel-copy", month+"-12 10:00:00", "completed", "codex", 0)
	if _, _, err := tracker.archiveOldestClosedMonth(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination := t.TempDir()
	if err := tracker.SwitchArchiveDirectory(ctx, destination); err == nil {
		t.Fatal("canceled copy succeeded")
	}
	files, err := os.ReadDir(destination)
	if err != nil || len(files) != 0 || tracker.ArchiveDirectory() != original {
		t.Fatalf("rollback failed: %v %v", files, err)
	}
}
