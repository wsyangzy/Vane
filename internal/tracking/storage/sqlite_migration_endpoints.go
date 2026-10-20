package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

func (s *SQLiteAdapter) ensureEndpointsUniqueByCredentialGroup(ctx context.Context) error {
	tableExists, err := s.tableExists(ctx, "endpoints")
	if err != nil {
		return fmt.Errorf("failed to check table endpoints: %w", err)
	}
	if !tableExists {
		return nil
	}

	hasLegacyUniqueOnName, hasLegacyUniqueOnChannelName, hasDesiredUnique, err := s.detectEndpointsUniqueIndexes(ctx)
	if err != nil {
		return err
	}
	if hasDesiredUnique && !hasLegacyUniqueOnName && !hasLegacyUniqueOnChannelName {
		return nil
	}

	// 若存在旧 UNIQUE(name)/(channel,name)，必须重建表，否则不同分组同名仍会被旧约束拦住。
	if hasLegacyUniqueOnName || hasLegacyUniqueOnChannelName {
		s.logger.Info("🔧 [数据库迁移] endpoints：检测到旧唯一约束，将重建为凭证分组作用域唯一")
		return s.rebuildEndpointsTableForCredentialGroupScopedUniq(ctx)
	}

	return s.createEndpointCredentialGroupIndexes(ctx, s.GetWriteDB())
}

func (s *SQLiteAdapter) detectEndpointsUniqueIndexes(ctx context.Context) (hasLegacyUniqueOnName bool, hasLegacyUniqueOnChannelName bool, hasDesiredUnique bool, _ error) {
	rows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_list(endpoints)")
	if err != nil {
		return false, false, false, fmt.Errorf("failed to query endpoints indexes: %w", err)
	}
	// 注意：database/sql 在 *sql.Rows 未 Close 前会占用连接。
	// SQLite 连接池通常限制为 1（见 Open: SetMaxOpenConns(1)），如果在遍历 rows 时再发起嵌套 Query，
	// 会因拿不到新连接而阻塞，最终触发 ctx 超时（表现为 context deadline exceeded）。
	// 因此这里先把需要的索引名读出来，再逐个查询 index_info。

	type idx struct {
		name string
	}
	uniqueIndexes := make([]idx, 0, 4)
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return false, false, false, fmt.Errorf("failed to scan endpoints index: %w", err)
		}
		if unique != 1 {
			continue
		}
		uniqueIndexes = append(uniqueIndexes, idx{name: name})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, false, false, fmt.Errorf("failed to iterate endpoints index_list: %w", err)
	}
	rows.Close()

	for _, it := range uniqueIndexes {
		// index 名来自 sqlite_master/pragma 输出，但仍做简单转义避免拼接问题
		escaped := strings.ReplaceAll(it.name, "'", "''")
		colRows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_info('"+escaped+"')")
		if err != nil {
			return false, false, false, fmt.Errorf("failed to query endpoints index_info(%s): %w", it.name, err)
		}

		cols := make([]string, 0, 2)
		for colRows.Next() {
			var seqno, cid int
			var colName string
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				colRows.Close()
				return false, false, false, fmt.Errorf("failed to scan endpoints index_info(%s): %w", it.name, err)
			}
			cols = append(cols, colName)
		}
		colRows.Close()

		if len(cols) == 1 && cols[0] == "name" {
			hasLegacyUniqueOnName = true
		}
		if len(cols) == 2 && cols[0] == "channel" && cols[1] == "name" {
			if it.name != "idx_endpoints_unique_legacy_name" {
				hasLegacyUniqueOnChannelName = true
			}
		}
		if len(cols) == 3 && cols[0] == "channel" && cols[1] == "credential_group_id" && cols[2] == "name" {
			hasDesiredUnique = true
		}
	}

	return hasLegacyUniqueOnName, hasLegacyUniqueOnChannelName, hasDesiredUnique, nil
}

func (s *SQLiteAdapter) rebuildEndpointsTableForCredentialGroupScopedUniq(ctx context.Context) error {
	// 注意：该迁移会锁表并重建 endpoints，但表体量通常较小（配置表），可接受。
	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin endpoints rebuild tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 清理旧触发器（表重建前先删，避免名字冲突）
	_, _ = tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS update_endpoints_timestamp")

	// 重命名旧表
	if _, err := tx.ExecContext(ctx, "ALTER TABLE endpoints RENAME TO endpoints_old"); err != nil {
		return fmt.Errorf("failed to rename endpoints to endpoints_old: %w", err)
	}

	// 创建新表（与 schema.sql 保持一致）
	createSQL := `
CREATE TABLE endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    channel TEXT NOT NULL,
    credential_group_id INTEGER REFERENCES claude_credential_groups(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL,

    token TEXT,
    api_key TEXT,
    headers TEXT,

    priority INTEGER DEFAULT 1,
    failover_enabled INTEGER DEFAULT 1,
    cooldown_seconds INTEGER,
    timeout_seconds INTEGER DEFAULT 300,

    supports_count_tokens INTEGER DEFAULT 0,
    skip_health_check INTEGER DEFAULT 0,

    cost_multiplier REAL DEFAULT 1.0,
    input_cost_multiplier REAL DEFAULT 1.0,
    output_cost_multiplier REAL DEFAULT 1.0,
    cache_creation_cost_multiplier REAL DEFAULT 1.0,
    cache_creation_cost_multiplier_1h REAL DEFAULT 1.0,
    cache_read_cost_multiplier REAL DEFAULT 1.0,
    model_multipliers TEXT NOT NULL DEFAULT '[]',

    enabled INTEGER DEFAULT 1,

    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);`
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create new endpoints table: %w", err)
	}

	// 复制数据（保留原 id）
	copySQL := `
INSERT INTO endpoints (
    id, channel, credential_group_id, name, url, token, api_key, headers,
    priority, failover_enabled, cooldown_seconds, timeout_seconds,
    supports_count_tokens, skip_health_check,
    cost_multiplier, input_cost_multiplier, output_cost_multiplier,
    cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h, cache_read_cost_multiplier,
    model_multipliers,
    enabled, created_at, updated_at
)
SELECT
    id, channel, COALESCE(credential_group_id, NULL), name, url, token, api_key, headers,
    priority, failover_enabled, cooldown_seconds, timeout_seconds,
    supports_count_tokens, skip_health_check,
    cost_multiplier, input_cost_multiplier, output_cost_multiplier,
    cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h, cache_read_cost_multiplier,
    COALESCE(model_multipliers, '[]'),
    enabled, created_at, updated_at
FROM endpoints_old;`
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("failed to copy endpoints data: %w", err)
	}

	// 重建索引
	if err := s.createEndpointCredentialGroupIndexes(ctx, tx); err != nil {
		return err
	}

	// 重建触发器
	triggerSQL := `
CREATE TRIGGER IF NOT EXISTS update_endpoints_timestamp
    AFTER UPDATE ON endpoints
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE endpoints SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;`
	if _, err := tx.ExecContext(ctx, triggerSQL); err != nil {
		return fmt.Errorf("failed to recreate endpoints trigger: %w", err)
	}

	// 删除旧表
	if _, err := tx.ExecContext(ctx, "DROP TABLE endpoints_old"); err != nil {
		return fmt.Errorf("failed to drop endpoints_old: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit endpoints rebuild: %w", err)
	}
	s.logger.Info("✅ [数据库迁移] endpoints：已重建为凭证分组作用域唯一")
	return nil
}

func (s *SQLiteAdapter) createEndpointCredentialGroupIndexes(ctx context.Context, executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}) error {
	indexSQL := []string{
		"CREATE INDEX IF NOT EXISTS idx_endpoints_channel ON endpoints(channel)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_credential_group ON endpoints(credential_group_id)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_priority ON endpoints(priority)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_enabled ON endpoints(enabled)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_failover ON endpoints(failover_enabled)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_unique_group_name ON endpoints(channel, credential_group_id, name) WHERE credential_group_id IS NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_unique_legacy_name ON endpoints(channel, name) WHERE credential_group_id IS NULL",
	}
	for _, stmt := range indexSQL {
		if _, err := executor.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to create endpoints credential group index: %w", err)
		}
	}
	return nil
}
