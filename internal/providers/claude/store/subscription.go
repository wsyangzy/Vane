package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
)

// EnsureClaudeSubscriptionSchema is provider-owned and idempotent. It is
// called by the existing application schema upgrade, never against client files.
func EnsureClaudeSubscriptionSchema(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(claude_credential_groups)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var id, notNull, pk int
		var name, kind string
		var def sql.NullString
		if err := rows.Scan(&id, &name, &kind, &notNull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, column := range []struct{ name, ddl string }{
		{"auth_mode", `ALTER TABLE claude_credential_groups ADD COLUMN auth_mode TEXT NOT NULL DEFAULT 'group_credential'`},
		{"subscription_config", `ALTER TABLE claude_credential_groups ADD COLUMN subscription_config TEXT NOT NULL DEFAULT '{}'`},
	} {
		if !columns[column.name] {
			if _, err := db.ExecContext(ctx, column.ddl); err != nil {
				return fmt.Errorf("upgrade Claude subscription: %w", err)
			}
		}
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS claude_subscription_pauses (
		group_id INTEGER NOT NULL, binding_version INTEGER NOT NULL, model TEXT NOT NULL,
		payload TEXT NOT NULL, PRIMARY KEY(group_id,binding_version,model));`)
	return err
}

type SQLiteClaudeSubscriptionStore struct{ db *sql.DB }

func NewSQLiteClaudeSubscriptionStore(db *sql.DB) *SQLiteClaudeSubscriptionStore {
	return &SQLiteClaudeSubscriptionStore{db: db}
}
func (s *SQLiteClaudeSubscriptionStore) ListBindings(ctx context.Context) ([]subscription.Binding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,channel,subscription_config FROM claude_credential_groups WHERE auth_mode='client_passthrough'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []subscription.Binding{}
	for rows.Next() {
		var id int64
		var channel, raw string
		if err := rows.Scan(&id, &channel, &raw); err != nil {
			return nil, err
		}
		b := decodeSubscription(raw)
		if b == nil {
			continue
		}
		b.GroupID = id
		b.Channel = channel
		result = append(result, *b)
	}
	return result, rows.Err()
}
func (s *SQLiteClaudeSubscriptionStore) ListPauses(ctx context.Context) ([]subscription.Pause, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.payload FROM claude_subscription_pauses p JOIN claude_credential_groups g ON g.id=p.group_id WHERE g.auth_mode='client_passthrough'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []subscription.Pause{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p subscription.Pause
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, fmt.Errorf("invalid subscription pause")
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func (s *SQLiteClaudeSubscriptionStore) PutPause(ctx context.Context, p subscription.Pause) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO claude_subscription_pauses(group_id,binding_version,model,payload)
	 SELECT ?,?,?,? WHERE EXISTS (SELECT 1 FROM claude_credential_groups WHERE id=? AND auth_mode='client_passthrough' AND json_extract(subscription_config,'$.version')=?)
	 ON CONFLICT(group_id,binding_version,model) DO UPDATE SET payload=excluded.payload
	 WHERE json_extract(excluded.payload,'$.revision') > json_extract(claude_subscription_pauses.payload,'$.revision')`, p.GroupID, p.Version, p.Model, string(data), p.GroupID, p.Version)
	return err
}
func (s *SQLiteClaudeSubscriptionStore) DeletePause(ctx context.Context, p subscription.Pause) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM claude_subscription_pauses WHERE group_id=? AND binding_version=? AND model=? AND json_extract(payload,'$.revision')=?`, p.GroupID, p.Version, p.Model, p.Revision)
	return err
}

// Configuration and pause revisions commit together. Rotating a local key,
// disabling a caller or editing backup permission never erases known quota.
// Only explicit rebind denotes a different subscription identity.
func (s *SQLiteClaudeCredentialGroupStore) reviseSubscriptionPauses(ctx context.Context, old, next *ClaudeCredentialGroupRecord) error {
	if old == nil || old.Subscription == nil || next.Subscription == nil || old.Subscription.Version == next.Subscription.Version {
		return nil
	}
	b := next.Subscription
	if b.Action == "rebind" || old.Subscription.AccountID != next.Subscription.AccountID {
		return nil
	}
	rows, err := s.getQuerier().QueryContext(ctx, `SELECT payload FROM claude_subscription_pauses WHERE group_id=? AND binding_version=?`, old.ID, old.Subscription.Version)
	if err != nil {
		return err
	}
	var pauses []subscription.Pause
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var p subscription.Pause
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			rows.Close()
			return err
		}
		pauses = append(pauses, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range pauses {
		p.Version = b.Version
		p.Revision++
		if b.Action == "verify" {
			p.ResumeNotBefore = time.Now()
			p.ResetKnown = false
			p.Reason = "manual_verification_requested"
			p.Source = "manual"
		}
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err = s.getQuerier().ExecContext(ctx, `INSERT INTO claude_subscription_pauses(group_id,binding_version,model,payload) VALUES(?,?,?,?)`, p.GroupID, p.Version, p.Model, string(data)); err != nil {
			return err
		}
	}
	return nil
}
func encodeSubscription(b *subscription.Binding) string {
	if b == nil {
		return "{}"
	}
	copy := *b
	copy.Action = ""
	data, _ := json.Marshal(copy)
	return string(data)
}
func decodeSubscription(raw string) *subscription.Binding {
	var b subscription.Binding
	if json.Unmarshal([]byte(raw), &b) != nil || b.Version <= 0 {
		return nil
	}
	return &b
}
