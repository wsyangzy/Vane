package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

// A fixed-width UTC timestamp preserves chronological ordering in SQLite TEXT
// comparisons regardless of the OS time zone or fractional-second precision.
const affinityTimestamp = "2006-01-02T15:04:05.000000000Z"

func (s *ProfileStore) SaveAffinity(ctx context.Context, provider string, entries []routing.SessionAffinity) error {
	if !validProfileProvider(provider) {
		return errors.New("provider_invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_affinity WHERE provider=?`, provider); err != nil {
		return err
	}
	for i, entry := range entries {
		if i >= 4096 {
			break
		}
		if entry.Provider != provider || len(entry.Key) != 64 || !time.Now().Before(entry.ExpiresAt) {
			continue
		}
		payload, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_affinity(provider,profile_id,affinity_key,payload,expires_at) SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM routing_profiles WHERE id=? AND provider=?)`, provider, entry.ProfileID, entry.Key, string(payload), entry.ExpiresAt.UTC().Format(affinityTimestamp), entry.ProfileID, provider); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *ProfileStore) LoadAffinity(ctx context.Context, provider string) ([]routing.SessionAffinity, error) {
	if !validProfileProvider(provider) {
		return nil, errors.New("provider_invalid")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM routing_affinity WHERE provider=? AND expires_at>? ORDER BY expires_at DESC LIMIT 4096`, provider, time.Now().UTC().Format(affinityTimestamp))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []routing.SessionAffinity{}
	for rows.Next() {
		var raw string
		var entry routing.SessionAffinity
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
