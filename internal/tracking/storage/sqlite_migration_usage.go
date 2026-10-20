package storage

import (
	"context"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// ensureUsageSummaryEnvironmentScope upgrades legacy usage_summary tables whose
// UNIQUE constraint predates provider/environment scoping. SQLite cannot alter
// a table constraint in place, so rebuild only when the desired unique key is
// missing; clean databases take the cheap metadata-check path.
func (s *SQLiteAdapter) ensureUsageSummaryEnvironmentScope(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "usage_summary")
	if err != nil || !exists {
		return err
	}
	hasDesiredUnique, err := s.usageSummaryHasEnvironmentUnique(ctx)
	if err != nil {
		return err
	}
	if hasDesiredUnique {
		return nil
	}
	s.logger.Info("🔧 [数据库迁移] usage_summary：重建包含 provider/environment 的唯一约束")
	return s.rebuildUsageSummaryForEnvironment(ctx)
}

func (s *SQLiteAdapter) usageSummaryHasEnvironmentUnique(ctx context.Context) (bool, error) {
	rows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_list(usage_summary)")
	if err != nil {
		return false, fmt.Errorf("failed to inspect usage_summary indexes: %w", err)
	}
	type indexInfo struct {
		name   string
		unique int
	}
	var indexes []indexInfo
	for rows.Next() {
		var seq, unique, partial int
		var origin string
		var name string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("failed to scan usage_summary indexes: %w", err)
		}
		if unique != 0 {
			indexes = append(indexes, indexInfo{name: name, unique: unique})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, fmt.Errorf("failed to iterate usage_summary indexes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	want := []string{"date", "model_name", "endpoint_name", "group_name", "provider", "environment"}
	for _, index := range indexes {
		escaped := strings.ReplaceAll(index.name, "'", "''")
		columnRows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_info('"+escaped+"')")
		if err != nil {
			return false, fmt.Errorf("failed to inspect usage_summary index %s: %w", index.name, err)
		}
		columns := make([]string, 0, len(want))
		for columnRows.Next() {
			var seqno, cid int
			var name string
			if err := columnRows.Scan(&seqno, &cid, &name); err != nil {
				_ = columnRows.Close()
				return false, fmt.Errorf("failed to scan usage_summary index %s: %w", index.name, err)
			}
			columns = append(columns, name)
		}
		if err := columnRows.Close(); err != nil {
			return false, err
		}
		if len(columns) == len(want) {
			match := true
			for i := range want {
				if columns[i] != want[i] {
					match = false
					break
				}
			}
			if match {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *SQLiteAdapter) rebuildUsageSummaryForEnvironment(ctx context.Context) error {
	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin usage_summary rebuild: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`DROP INDEX IF EXISTS idx_usage_summary_date`,
		`DROP INDEX IF EXISTS idx_usage_summary_model`,
		`DROP INDEX IF EXISTS idx_usage_summary_endpoint`,
		`DROP INDEX IF EXISTS idx_usage_summary_group`,
		`DROP INDEX IF EXISTS idx_usage_summary_provider`,
		`DROP INDEX IF EXISTS idx_usage_summary_environment_date`,
		`DROP TRIGGER IF EXISTS update_usage_summary_timestamp`,
		`ALTER TABLE usage_summary RENAME TO usage_summary_legacy_environment`,
		`CREATE TABLE usage_summary (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL,
			model_name TEXT NOT NULL,
			endpoint_name TEXT NOT NULL,
			group_name TEXT,
			provider TEXT NOT NULL DEFAULT 'claude',
			environment TEXT NOT NULL DEFAULT 'release',
			request_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			error_count INTEGER DEFAULT 0,
			total_input_tokens INTEGER DEFAULT 0,
			total_output_tokens INTEGER DEFAULT 0,
			total_cache_creation_tokens INTEGER DEFAULT 0,
			total_cache_read_tokens INTEGER DEFAULT 0,
			total_cost_usd REAL DEFAULT 0,
			avg_duration_ms REAL DEFAULT 0,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			UNIQUE(date, model_name, endpoint_name, group_name, provider, environment)
		)`,
		`INSERT INTO usage_summary (
			id, date, model_name, endpoint_name, group_name, provider, environment,
			request_count, success_count, error_count,
			total_input_tokens, total_output_tokens,
			total_cache_creation_tokens, total_cache_read_tokens,
			total_cost_usd, avg_duration_ms, created_at, updated_at
		)
		SELECT id, date, model_name, endpoint_name, group_name,
			COALESCE(provider, 'claude'), COALESCE(environment, 'release'),
			request_count, success_count, error_count,
			total_input_tokens, total_output_tokens,
			total_cache_creation_tokens, total_cache_read_tokens,
			total_cost_usd, avg_duration_ms, created_at, updated_at
		FROM usage_summary_legacy_environment`,
		`DROP TABLE usage_summary_legacy_environment`,
		`CREATE INDEX idx_usage_summary_date ON usage_summary(date)`,
		`CREATE INDEX idx_usage_summary_model ON usage_summary(model_name)`,
		`CREATE INDEX idx_usage_summary_endpoint ON usage_summary(endpoint_name)`,
		`CREATE INDEX idx_usage_summary_group ON usage_summary(group_name)`,
		`CREATE INDEX idx_usage_summary_provider ON usage_summary(provider)`,
		`CREATE INDEX idx_usage_summary_environment_date ON usage_summary(environment, date)`,
		`CREATE TRIGGER update_usage_summary_timestamp
			AFTER UPDATE ON usage_summary
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
		BEGIN
				UPDATE usage_summary SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
			END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("rebuild usage_summary: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit usage_summary rebuild: %w", err)
	}
	return nil
}
