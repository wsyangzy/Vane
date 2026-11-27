package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/appdir"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
	_ "modernc.org/sqlite"
)

// RecoveryRecord binds an exact completed item to its issuing scope. Both the
// value and its provenance are protected; SQL indexes contain only hashes.
type RecoveryRecord struct {
	Scope, ItemID, PartialHash, CipherHash, Fingerprint string
	Item                                                json.RawMessage `json:",omitempty"`
	CreatedAt                                           time.Time
}

type RecoveryStore struct {
	Path      string
	Retention time.Duration
	MaxBytes  int64
	Vault     accounts.Vault
}

func DefaultRecoveryDirectory() string { return filepath.Join(appdir.GetDataDir(), "codex-recovery") }

func (s *RecoveryStore) open(ctx context.Context, create ...bool) (*sql.DB, error) {
	path := filepath.ToSlash(s.Path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_pragma", "busy_timeout(200)")
	q.Set("mode", "rw")
	if len(create) > 0 && create[0] {
		q.Set("mode", "rwc")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if len(create) == 0 || !create[0] {
		var id int
		if err = db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&id); err != nil || id != 0x56414E52 {
			db.Close()
			return nil, errors.New("recovery_database_identity_mismatch")
		}
	}
	return db, nil
}

func (s *RecoveryStore) Initialize(ctx context.Context) error {
	if !filepath.IsAbs(s.Path) || s.Retention <= 0 || s.MaxBytes < 1<<20 || s.MaxBytes > 1<<30 || s.Vault == nil {
		return errors.New("invalid_recovery_storage_configuration")
	}
	// Fail closed on hosts without a user-secret store; never store plaintext.
	sealed, err := s.Vault.Seal([]byte("vane-recovery-v1"))
	if err != nil {
		return errors.New("recovery_storage_protection_unavailable")
	}
	if _, err = s.Vault.Open(sealed); err != nil {
		return errors.New("recovery_storage_protection_unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return errors.New("recovery_directory_unavailable")
	}
	if info, err := os.Lstat(s.Path); err == nil && !info.Mode().IsRegular() {
		return errors.New("invalid_recovery_database_file")
	}
	db, err := s.open(ctx, true)
	if err != nil {
		return errors.New("recovery_database_unavailable")
	}
	defer db.Close()
	var applicationID, tables int
	if err := db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&applicationID); err != nil {
		return errors.New("recovery_database_unavailable")
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&tables); err != nil {
		return errors.New("recovery_database_unavailable")
	}
	if applicationID != 0x56414E52 && (applicationID != 0 || tables != 0) {
		return errors.New("recovery_directory_contains_unrelated_database")
	}
	_, err = db.ExecContext(ctx, `PRAGMA auto_vacuum=FULL;
PRAGMA application_id=1447120466;
CREATE TABLE IF NOT EXISTS completed_reasoning (
 scope TEXT NOT NULL, item_id TEXT NOT NULL, created_at INTEGER NOT NULL,
 payload BLOB NOT NULL, PRIMARY KEY(scope,item_id));
CREATE INDEX IF NOT EXISTS completed_reasoning_age ON completed_reasoning(created_at);`)
	if err != nil {
		return errors.New("recovery_schema_unavailable")
	}
	return s.maintain(ctx, db)
}

func (s *RecoveryStore) maintain(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DELETE FROM completed_reasoning WHERE created_at < ?`, time.Now().Add(-s.Retention).UnixNano())
	if err != nil {
		return err
	}
	// Reserve headroom for indexes, SQLite pages and a single transaction. Newest
	// records survive the cap; expired evidence cannot be revived by lookup.
	_, err = db.ExecContext(ctx, `DELETE FROM completed_reasoning WHERE rowid IN (
 SELECT rowid FROM (SELECT rowid, SUM(length(payload)+256) OVER (ORDER BY created_at DESC,rowid DESC) AS total,
 ROW_NUMBER() OVER (ORDER BY created_at DESC,rowid DESC) AS n FROM completed_reasoning)
 WHERE total > ? OR n > 4096)`, s.MaxBytes/2)
	return err
}

func (s *RecoveryStore) Save(ctx context.Context, records []RecoveryRecord) error {
	if len(records) == 0 {
		return nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range records {
		if record.Scope == "" || record.ItemID == "" || record.CipherHash == "" || record.CreatedAt.IsZero() {
			continue
		}
		var existing []byte
		lookupErr := tx.QueryRowContext(ctx, `SELECT payload FROM completed_reasoning WHERE scope=? AND item_id=?`, record.Scope, record.ItemID).Scan(&existing)
		if lookupErr == nil {
			plain, err := s.Vault.Open(existing)
			if err != nil {
				continue
			}
			var old RecoveryRecord
			if json.Unmarshal(plain, &old) != nil || old.CipherHash == "" || old.CipherHash == record.CipherHash {
				continue
			}
			// Conflicting completions disable the item until the original expiry.
			old.CipherHash, old.PartialHash, old.Fingerprint, old.Item = "", "", "", nil
			plain, _ = json.Marshal(old)
			sealed, err := s.Vault.Seal(plain)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE completed_reasoning SET payload=? WHERE scope=? AND item_id=?`, sealed, record.Scope, record.ItemID); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}
		plain, err := json.Marshal(record)
		if err != nil {
			return err
		}
		sealed, err := s.Vault.Seal(plain)
		if err != nil {
			return errors.New("recovery_encryption_failed")
		}
		// Never overwrite the first observed issuer/value, including conflicts.
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO completed_reasoning(scope,item_id,created_at,payload) VALUES(?,?,?,?)`, record.Scope, record.ItemID, record.CreatedAt.UnixNano(), sealed); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.maintain(ctx, db)
}

func (s *RecoveryStore) Load(ctx context.Context, scope string, ids []string) (map[string]RecoveryRecord, error) {
	result := make(map[string]RecoveryRecord)
	db, err := s.open(ctx)
	if err != nil {
		return result, err
	}
	defer db.Close()
	if err = s.maintain(ctx, db); err != nil {
		return result, err
	}
	for _, id := range ids {
		var sealed []byte
		err := db.QueryRowContext(ctx, `SELECT payload FROM completed_reasoning WHERE scope=? AND item_id=? AND created_at>=?`, scope, id, time.Now().Add(-s.Retention).UnixNano()).Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, err
		}
		plain, err := s.Vault.Open(sealed)
		if err != nil {
			continue
		}
		var record RecoveryRecord
		if json.Unmarshal(plain, &record) != nil || record.Scope != scope || record.ItemID != id || time.Since(record.CreatedAt) < 0 || time.Since(record.CreatedAt) > s.Retention {
			continue
		}
		result[id] = record
	}
	return result, nil
}
