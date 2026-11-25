package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

func EnsureSearchSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS codex_profile_search_credentials (
 profile_id INTEGER NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
 vendor TEXT NOT NULL, secret BLOB NOT NULL, PRIMARY KEY(profile_id,vendor));
CREATE TRIGGER IF NOT EXISTS codex_profile_search_cleanup AFTER DELETE ON routing_profiles
BEGIN DELETE FROM codex_profile_search_credentials WHERE profile_id=OLD.id; END;`)
	return err
}

func PutSearchCredential(ctx context.Context, tx *sql.Tx, profileID int64, vendor, key string) error {
	if key == "" {
		_, err := tx.ExecContext(ctx, `DELETE FROM codex_profile_search_credentials WHERE profile_id=? AND vendor=?`, profileID, vendor)
		return err
	}
	cipher, err := (accounts.UserVault{}).Seal([]byte(key))
	if err != nil {
		return errors.New("无法加密搜索服务凭证")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO codex_profile_search_credentials(profile_id,vendor,secret) VALUES(?,?,?)
 ON CONFLICT(profile_id,vendor) DO UPDATE SET secret=excluded.secret`, profileID, vendor, cipher)
	return err
}

func SearchCredential(ctx context.Context, db *sql.DB, profileID int64, vendor string) (string, error) {
	if db == nil {
		return "", errors.New("搜索凭证存储未就绪")
	}
	var cipher []byte
	err := db.QueryRowContext(ctx, `SELECT secret FROM codex_profile_search_credentials WHERE profile_id=? AND vendor=?`, profileID, vendor).Scan(&cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	plain, err := (accounts.UserVault{}).Open(cipher)
	if err != nil {
		return "", errors.New("无法解密搜索服务凭证，请重新填写")
	}
	return string(plain), nil
}

func HasSearchCredential(ctx context.Context, db *sql.DB, profileID int64, vendor string) (bool, error) {
	if db == nil {
		return false, errors.New("搜索凭证存储未就绪")
	}
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM codex_profile_search_credentials WHERE profile_id=? AND vendor=?`, profileID, vendor).Scan(&count)
	return count > 0, err
}

func SearchCredentialVendors(ctx context.Context, db *sql.DB, profileID int64) ([]string, error) {
	if db == nil {
		return nil, errors.New("搜索凭证存储未就绪")
	}
	rows, err := db.QueryContext(ctx, `SELECT vendor FROM codex_profile_search_credentials WHERE profile_id=? ORDER BY vendor`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vendors []string
	for rows.Next() {
		var vendor string
		if err := rows.Scan(&vendor); err != nil {
			return nil, err
		}
		vendors = append(vendors, vendor)
	}
	return vendors, rows.Err()
}

// MigrateLegacySearch preserves an existing global choice once, on the Codex
// profiles that existed at migration time. New profiles use automatic selection.
// Metadata, encrypted credentials and removal of plaintext commit together.
func MigrateLegacySearch(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='settings'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT key,value FROM settings WHERE provider='global' AND category='search'`)
	if err != nil {
		return err
	}
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return err
		}
		values[key] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	p := routing.DefaultSearchPolicy()
	if v := values["mode"]; v != "" {
		p.Mode = v
	}
	if v := values["endpoint"]; v != "" {
		p.Endpoint = v
	}
	if v := values["timeout"]; v != "" {
		p.Timeout, err = time.ParseDuration(v)
		if err != nil {
			return err
		}
	}
	if v := values["max_results"]; v != "" {
		p.MaxResults, err = strconv.Atoi(v)
		if err != nil {
			return err
		}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id FROM routing_profiles WHERE provider='codex' AND id NOT IN (SELECT profile_id FROM routing_profile_search_config)`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_profile_search_config(profile_id,payload) VALUES(?,?)`, id, string(data)); err != nil {
			return err
		}
		if key := values["api_key"]; key != "" {
			if err = PutSearchCredential(ctx, tx, id, p.Vendor, key); err != nil {
				return err
			}
		}
	}
	// Keep settings until a profile exists, so an installation configured before
	// its first scheme does not silently lose the user's search credentials.
	if len(ids) == 0 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM settings WHERE provider='global' AND category='search'`); err != nil {
		return err
	}
	return tx.Commit()
}
