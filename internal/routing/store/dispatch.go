package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

func ensureDispatchTraceEnvironment(ctx context.Context, tx *sql.Tx) error {
	var hasEnvironment int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('routing_dispatch_traces') WHERE name='environment'`).Scan(&hasEnvironment); err != nil {
		return err
	}
	if hasEnvironment == 0 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE routing_dispatch_traces ADD COLUMN environment TEXT NOT NULL DEFAULT 'unknown'`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS routing_dispatch_traces_environment ON routing_dispatch_traces(environment,provider,id);
CREATE INDEX IF NOT EXISTS routing_dispatch_traces_request ON routing_dispatch_traces(environment,provider,request_id);`); err != nil {
		return err
	}
	var hasRequestLogs int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='request_logs'`).Scan(&hasRequestLogs); err != nil {
		return err
	}
	if hasRequestLogs == 0 {
		return nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name='environment'`).Scan(&hasEnvironment); err != nil {
		return err
	}
	environmentExpr := `'release'`
	if hasEnvironment > 0 {
		environmentExpr = `CASE WHEN COALESCE(logs.environment,'release') IN ('dev','release') THEN COALESCE(logs.environment,'release') ELSE 'unknown' END`
	}
	// Recover provenance only from the matching request and provider. Traces
	// without a request record stay unknown, rather than inheriting whichever
	// build happened to open this database first.
	_, err := tx.ExecContext(ctx, `UPDATE routing_dispatch_traces AS traces
SET environment=(SELECT `+environmentExpr+` FROM request_logs AS logs
 WHERE logs.request_id=traces.request_id AND COALESCE(logs.provider,'claude')=traces.provider LIMIT 1)
WHERE traces.environment='unknown' AND EXISTS (SELECT 1 FROM request_logs AS logs
 WHERE logs.request_id=traces.request_id AND COALESCE(logs.provider,'claude')=traces.provider)`)
	return err
}

func (s *ProfileStore) SaveTrace(ctx context.Context, trace routing.DispatchTrace) error {
	if !validProfileProvider(trace.Provider) {
		return errors.New("provider_invalid")
	}
	payload, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_dispatch_traces WHERE environment=? AND provider=? AND request_id=?`, s.environment, trace.Provider, trace.RequestID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO routing_dispatch_traces(environment,provider,profile_id,request_id,payload) VALUES(?,?,?,?,?)`, s.environment, trace.Provider, trace.ProfileID, trace.RequestID, string(payload)); err != nil {
		return err
	}
	// Bound local diagnostics independently of the request archive.
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_dispatch_traces WHERE environment=? AND provider=? AND id NOT IN (SELECT id FROM routing_dispatch_traces WHERE environment=? AND provider=? ORDER BY id DESC LIMIT 5000)`, s.environment, trace.Provider, s.environment, trace.Provider); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ProfileStore) ListTraces(ctx context.Context, provider string, limit int) ([]routing.DispatchTrace, error) {
	if !validProfileProvider(provider) {
		return nil, errors.New("provider_invalid")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM routing_dispatch_traces WHERE environment=? AND provider=? ORDER BY id DESC LIMIT ?`, s.environment, provider, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []routing.DispatchTrace{}
	for rows.Next() {
		var payload string
		var trace routing.DispatchTrace
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &trace); err != nil {
			return nil, err
		}
		result = append(result, trace)
	}
	return result, rows.Err()
}

type MigrationRecord struct {
	State      string    `json:"state"`
	BackupPath string    `json:"backup_path"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (s *ProfileStore) Migration(ctx context.Context, provider string) (MigrationRecord, error) {
	var result MigrationRecord
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT details FROM routing_profile_migrations WHERE provider=?`, provider).Scan(&raw)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(raw), &result)
	return result, err
}

func (s *ProfileStore) SaveMigration(ctx context.Context, provider string, record MigrationRecord) error {
	if !validProfileProvider(provider) {
		return errors.New("provider_invalid")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO routing_profile_migrations(provider,state,started_at,completed_at,details) VALUES(?,?,?,?,?) ON CONFLICT(provider) DO UPDATE SET state=excluded.state,completed_at=excluded.completed_at,details=excluded.details`, provider, record.State, record.UpdatedAt.Format(time.RFC3339Nano), record.UpdatedAt.Format(time.RFC3339Nano), string(data))
	return err
}
