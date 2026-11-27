package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

func testRecoveryStore(t *testing.T) *RecoveryStore {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows user vault")
	}
	s := &RecoveryStore{Path: filepath.Join(t.TempDir(), "恢复 & state", "recovery.db"), Retention: 7 * 24 * time.Hour, MaxBytes: 8 << 20, Vault: accounts.UserVault{}}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRecoveryStoreProtectedRestartExpiryAndConflict(t *testing.T) {
	s := testRecoveryStore(t)
	ctx := context.Background()
	record := RecoveryRecord{Scope: "scope", ItemID: "item", PartialHash: "partial", CipherHash: "complete", Fingerprint: "fingerprint", Item: json.RawMessage(`{"encrypted_content":"TEST_SEALED_STATE","summary":[{"text":"TEST_PRIVATE_SUMMARY"}]}`), CreatedAt: time.Now()}
	if err := s.Save(ctx, []RecoveryRecord{record}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"TEST_SEALED_STATE", "TEST_PRIVATE_SUMMARY", "fingerprint"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("plaintext persisted")
		}
	}
	reopened := *s
	rows, err := reopened.Load(ctx, "scope", []string{"item"})
	if err != nil || string(rows["item"].Item) != string(record.Item) {
		t.Fatalf("restart restore failed: %v", err)
	}
	rows, err = s.Load(ctx, "other", []string{"item"})
	if err != nil || len(rows) != 0 {
		t.Fatal("cross-source lookup")
	}
	conflict := record
	conflict.CipherHash = "other"
	if err = s.Save(ctx, []RecoveryRecord{conflict, record}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.Load(ctx, "scope", []string{"item"})
	if rows["item"].CipherHash != "" {
		t.Fatal("conflict was not disabled")
	}
	expired := record
	expired.ItemID = "expired"
	expired.CreatedAt = time.Now().Add(-8 * 24 * time.Hour)
	if err = s.Save(ctx, []RecoveryRecord{expired}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.Load(ctx, "scope", []string{"expired"})
	if len(rows) != 0 {
		t.Fatal("expired entry restored")
	}
	if err = s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryStoreCapacityRemovesOldestAndShrinksFile(t *testing.T) {
	s := testRecoveryStore(t)
	s.MaxBytes = 1 << 20
	ctx := context.Background()
	records := make([]RecoveryRecord, 0, 12)
	for i := 0; i < 12; i++ {
		records = append(records, RecoveryRecord{Scope: "scope", ItemID: fmt.Sprint(i), CipherHash: fmt.Sprint(i), Item: json.RawMessage(`{"value":"` + strings.Repeat("x", 100<<10) + `"}`), CreatedAt: time.Now().Add(time.Duration(i-20) * time.Second)})
	}
	if err := s.Save(ctx, records); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Load(ctx, "scope", []string{"0", "11"})
	if err != nil || rows["11"].CipherHash != "11" || rows["0"].CipherHash != "" {
		t.Fatalf("capacity policy failed: %v", err)
	}
	info, err := os.Stat(s.Path)
	if err != nil || info.Size() > s.MaxBytes {
		t.Fatalf("database did not shrink: %v", err)
	}
}

func TestRecoveryStoreUnreadableProtectedRecordIsNotUsed(t *testing.T) {
	s := testRecoveryStore(t)
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO completed_reasoning VALUES(?,?,?,?)`, "scope", "item", time.Now().UnixNano(), []byte("corrupt"))
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Load(ctx, "scope", []string{"item"})
	if err != nil || len(rows) != 0 {
		t.Fatal("corrupt protected data used")
	}
}

func TestRecoveryStoreRejectsUnrelatedDatabaseAndDoesNotRecreateDeletedFile(t *testing.T) {
	s := testRecoveryStore(t)
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA application_id=42`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(ctx); err == nil {
		t.Fatal("unrelated database accepted")
	}
	db, err = s.open(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var id int
	err = db.QueryRow(`PRAGMA application_id`).Scan(&id)
	db.Close()
	if err != nil || id != 42 {
		t.Fatal("unrelated database modified")
	}
	if err = os.Remove(s.Path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(ctx, "scope", []string{"item"}); err == nil {
		t.Fatal("deleted database recreated")
	}
	if _, err = os.Stat(s.Path); !os.IsNotExist(err) {
		t.Fatal("lookup created a database")
	}
}
