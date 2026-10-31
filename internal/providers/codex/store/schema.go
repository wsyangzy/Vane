package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

func (m *schemaMigrator) ensureSchema(ctx context.Context, db *sql.DB) error {
	if err := m.normalizeCodexProviderArchiveTables(ctx, db); err != nil {
		return err
	}

	tableExists, err := sqliteTableExists(ctx, db, codexProvidersTableName)
	if err != nil {
		return err
	}

	if tableExists {
		hasReasoning, err := sqliteColumnExists(ctx, db, codexProvidersTableName, "model_reasoning_effort")
		if err != nil {
			return err
		}
		if !hasReasoning {
			if _, err := db.ExecContext(ctx, "ALTER TABLE "+codexProvidersTableName+" ADD COLUMN model_reasoning_effort TEXT NOT NULL DEFAULT 'xhigh'"); err != nil {
				return fmt.Errorf("为 %s 添加 model_reasoning_effort 字段失败: %w", codexProvidersTableName, err)
			}
		}

		hasEnable1MContext, err := sqliteColumnExists(ctx, db, codexProvidersTableName, "enable_1m_context")
		if err != nil {
			return err
		}
		if !hasEnable1MContext {
			if _, err := db.ExecContext(ctx, "ALTER TABLE "+codexProvidersTableName+" ADD COLUMN enable_1m_context INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("为 %s 添加 enable_1m_context 字段失败: %w", codexProvidersTableName, err)
			}
		}
	}

	if err := m.ensureCodexChannelsSchema(ctx, db); err != nil {
		return err
	}

	return m.migrateCodexProvidersToChannels(ctx, db, tableExists)
}

func (m *schemaMigrator) ensureCodexChannelsSchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS codex_channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			provider_type TEXT NOT NULL DEFAULT 'openai-compatible',
			api_key TEXT NOT NULL,
			model TEXT NOT NULL,
			model_mapping TEXT NOT NULL DEFAULT '{}',
			model_reasoning_effort TEXT NOT NULL DEFAULT 'xhigh',
			enable_1m_context INTEGER NOT NULL DEFAULT 0,
			website TEXT,
			is_active INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 0,
			failover_enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
		)`,
		`CREATE INDEX IF NOT EXISTS idx_codex_channels_name ON codex_channels(name)`,
		`CREATE INDEX IF NOT EXISTS idx_codex_channels_is_active ON codex_channels(is_active)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_channels_single_active ON codex_channels(is_active) WHERE is_active = 1`,
		`CREATE TRIGGER IF NOT EXISTS update_codex_channels_timestamp
				AFTER UPDATE ON codex_channels
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
			BEGIN
				UPDATE codex_channels SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
			END`,
		`CREATE TABLE IF NOT EXISTS codex_credential_groups (
    service_tier_source TEXT NOT NULL DEFAULT 'response',
    cache_write_ttl_pricing INTEGER NOT NULL DEFAULT 0,
				enabled INTEGER NOT NULL DEFAULT 1,
id INTEGER PRIMARY KEY AUTOINCREMENT,
				channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
				name TEXT NOT NULL,
				api_key TEXT NOT NULL DEFAULT '',
				is_default INTEGER NOT NULL DEFAULT 0,
				daily_cost_limit_usd REAL NOT NULL DEFAULT 0,
				cost_multiplier REAL NOT NULL DEFAULT 1.0,
				input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
				output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
				cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
					cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
					long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0,
					long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
					long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
					long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
					long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
					disable_long_context_pricing INTEGER NOT NULL DEFAULT 1,
					disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1,
					service_tier_multipliers TEXT NOT NULL DEFAULT '[]',
					billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
					model_multipliers TEXT NOT NULL DEFAULT '[]',
					billing_rules TEXT NOT NULL DEFAULT '[]',
					created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
				updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
				UNIQUE(channel_id, name)
			)`,
		`CREATE INDEX IF NOT EXISTS idx_codex_credential_groups_channel ON codex_credential_groups(channel_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_credential_groups_single_default
				ON codex_credential_groups(channel_id, is_default) WHERE is_default = 1`,
		`CREATE TRIGGER IF NOT EXISTS update_codex_credential_groups_timestamp
				AFTER UPDATE ON codex_credential_groups
				FOR EACH ROW
				WHEN NEW.updated_at = OLD.updated_at
			BEGIN
				UPDATE codex_credential_groups SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
			END`,
		`CREATE TABLE IF NOT EXISTS codex_endpoints (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
					credential_group_id INTEGER REFERENCES codex_credential_groups(id) ON DELETE SET NULL,
				api_key TEXT NOT NULL DEFAULT '',
				name TEXT NOT NULL,
				base_url TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0,
			skip_health_check INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
				billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
				created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
				updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
			)`,
		`CREATE INDEX IF NOT EXISTS idx_codex_endpoints_channel ON codex_endpoints(channel_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_single_active_per_channel
			ON codex_endpoints(channel_id, is_active) WHERE is_active = 1`,
		`CREATE TRIGGER IF NOT EXISTS update_codex_endpoints_timestamp
			AFTER UPDATE ON codex_endpoints
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
		BEGIN
			UPDATE codex_endpoints SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
		END`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("创建 Codex 渠道-端点 schema 失败: %w", err)
		}
	}
	for _, column := range []struct{ name, ddl string }{
		{"priority", `ALTER TABLE codex_channels ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`},
		{"failover_enabled", `ALTER TABLE codex_channels ADD COLUMN failover_enabled INTEGER NOT NULL DEFAULT 1`},
		{"non_stream_route", `ALTER TABLE codex_channels ADD COLUMN non_stream_route TEXT`},
		{"privacy_level", `ALTER TABLE codex_channels ADD COLUMN privacy_level TEXT`},
	} {
		exists, err := sqliteColumnExists(ctx, db, "codex_channels", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, column.ddl); err != nil {
				return fmt.Errorf("为 codex_channels 添加 %s 失败: %w", column.name, err)
			}
		}
	}

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("启用 SQLite foreign_keys 失败: %w", err)
	}

	// 为已升级到 v7.0 但尚未含 name 列的 codex_endpoints 表补字段（向后兼容）。
	// 默认值用 base_url 自身（同渠道唯一约束保证不冲突）；用户可在 UI 中重命名。
	if err := m.ensureCodexEndpointNameColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_channels 补充模型映射字段
	if err := m.ensureCodexChannelModelMappingColumn(ctx, db); err != nil {
		return err
	}

	// 为 Codex 凭证分组补充 schema、默认分组和端点引用
	if err := m.ensureCodexCredentialGroupSchema(ctx, db); err != nil {
		return err
	}
	if err := EnsureCodexSubscriptionSchema(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充故障转移字段（v7.2+）
	if err := m.ensureCodexEndpointFailoverColumns(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充端点级请求超时字段（0 = 使用全局超时）
	if err := m.ensureCodexEndpointTimeoutColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充成本倍率字段（v7.2+）
	if err := m.ensureCodexEndpointCostMultiplierColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充细分成本倍率字段（v7.2+）
	if err := m.ensureCodexEndpointDetailedCostColumns(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充输出计费口径字段
	if err := m.ensureCodexEndpointBillingOutputTokenSourceColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充模型级倍率字段
	if err := m.ensureCodexEndpointModelMultipliersColumn(ctx, db); err != nil {
		return err
	}

	if err := m.backfillCodexEndpointCredentialGroups(ctx, db); err != nil {
		return err
	}

	if err := m.backfillCodexCredentialGroupModelMultipliers(ctx, db); err != nil {
		return err
	}

	if err := m.ensureCodexEndpointCredentialGroupUniqueConstraints(ctx, db); err != nil {
		return err
	}

	return nil
}

func (m *schemaMigrator) ensureCodexCredentialGroupSchema(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		table string
		name  string
		ddl   string
	}{
		{"codex_endpoints", "credential_group_id", `ALTER TABLE codex_endpoints ADD COLUMN credential_group_id INTEGER`},
		{"codex_endpoints", "api_key", `ALTER TABLE codex_endpoints ADD COLUMN api_key TEXT NOT NULL DEFAULT ''`},
		{"codex_credential_groups", "service_tier_source", `ALTER TABLE codex_credential_groups ADD COLUMN service_tier_source TEXT NOT NULL DEFAULT 'response'`},
		{"codex_credential_groups", "cache_write_ttl_pricing", `ALTER TABLE codex_credential_groups ADD COLUMN cache_write_ttl_pricing INTEGER NOT NULL DEFAULT 0`},
		{"codex_credential_groups", "enabled", `ALTER TABLE codex_credential_groups ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`},
		{"codex_credential_groups", "daily_cost_limit_usd", `ALTER TABLE codex_credential_groups ADD COLUMN daily_cost_limit_usd REAL NOT NULL DEFAULT 0`},
		{"codex_credential_groups", "model_policy", `ALTER TABLE codex_credential_groups ADD COLUMN model_policy TEXT NOT NULL DEFAULT 'inherit'`},
		{"codex_credential_groups", "model_mapping", `ALTER TABLE codex_credential_groups ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'`},
		{"codex_credential_groups", "supported_models", `ALTER TABLE codex_credential_groups ADD COLUMN supported_models TEXT NOT NULL DEFAULT '[]'`},
		{"codex_credential_groups", "cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "input_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN input_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "output_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN output_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "cache_creation_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "cache_read_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "long_context_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "long_context_input_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "long_context_output_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "long_context_cache_creation_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "long_context_cache_read_cost_multiplier", `ALTER TABLE codex_credential_groups ADD COLUMN long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"codex_credential_groups", "disable_long_context_pricing", `ALTER TABLE codex_credential_groups ADD COLUMN disable_long_context_pricing INTEGER NOT NULL DEFAULT 1`},
		{"codex_credential_groups", "disable_service_tier_pricing", `ALTER TABLE codex_credential_groups ADD COLUMN disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1`},
		{"codex_credential_groups", "service_tier_multipliers", `ALTER TABLE codex_credential_groups ADD COLUMN service_tier_multipliers TEXT NOT NULL DEFAULT '[]'`},
		{"codex_credential_groups", "billing_output_token_source", `ALTER TABLE codex_credential_groups ADD COLUMN billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens'`},
		{"codex_credential_groups", "model_multipliers", `ALTER TABLE codex_credential_groups ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'`},
		{"codex_credential_groups", "billing_rules", `ALTER TABLE codex_credential_groups ADD COLUMN billing_rules TEXT NOT NULL DEFAULT '[]'`},
	}
	for _, col := range columns {
		exists, err := sqliteColumnExists(ctx, db, col.table, col.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, col.ddl); err != nil {
				return fmt.Errorf("为 %s 添加 %s 字段失败: %w", col.table, col.name, err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_codex_credential_groups_channel ON codex_credential_groups(channel_id)`); err != nil {
		return fmt.Errorf("创建 Codex 凭证分组渠道索引失败: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_credential_groups_single_default ON codex_credential_groups(channel_id, is_default) WHERE is_default = 1`); err != nil {
		return fmt.Errorf("创建 Codex 默认凭证分组唯一索引失败: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO codex_credential_groups (
			channel_id, name, api_key, is_default, cost_multiplier,
			input_cost_multiplier, output_cost_multiplier, cache_creation_cost_multiplier,
			cache_read_cost_multiplier, long_context_cost_multiplier,
			long_context_input_cost_multiplier, long_context_output_cost_multiplier,
			long_context_cache_creation_cost_multiplier, long_context_cache_read_cost_multiplier,
			disable_long_context_pricing,
			billing_output_token_source, model_multipliers
		)
		SELECT id, '默认分组', api_key, 1, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1, 'output_tokens', '[]'
		FROM codex_channels ch
		WHERE NOT EXISTS (
			SELECT 1 FROM codex_credential_groups g WHERE g.channel_id = ch.id
		)
	`); err != nil {
		return fmt.Errorf("回填 Codex 默认凭证分组失败: %w", err)
	}
	return nil
}

func (m *schemaMigrator) backfillCodexEndpointCredentialGroups(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		UPDATE codex_endpoints
		SET credential_group_id = (
			SELECT id
			FROM codex_credential_groups
			WHERE channel_id = codex_endpoints.channel_id AND is_default = 1
			LIMIT 1
		)
		WHERE credential_group_id IS NULL OR credential_group_id = 0
	`); err != nil {
		return fmt.Errorf("回填 Codex 端点凭证分组失败: %w", err)
	}
	return nil
}

func (m *schemaMigrator) backfillCodexCredentialGroupModelMultipliers(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		UPDATE codex_credential_groups
		SET model_multipliers = (
			SELECT COALESCE(ep.model_multipliers, '[]')
			FROM codex_endpoints ep
			WHERE ep.channel_id = codex_credential_groups.channel_id
				AND ep.credential_group_id = codex_credential_groups.id
				AND COALESCE(ep.model_multipliers, '[]') NOT IN ('', '[]')
			ORDER BY COALESCE(ep.priority, 0) ASC, COALESCE(ep.created_at, '') ASC, ep.id ASC
			LIMIT 1
		)
		WHERE COALESCE(model_multipliers, '[]') IN ('', '[]')
			AND EXISTS (
				SELECT 1
				FROM codex_endpoints ep
				WHERE ep.channel_id = codex_credential_groups.channel_id
					AND ep.credential_group_id = codex_credential_groups.id
					AND COALESCE(ep.model_multipliers, '[]') NOT IN ('', '[]')
			)
	`); err != nil {
		return fmt.Errorf("回填 Codex 凭证分组模型附加倍率失败: %w", err)
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointNameColumn(ctx context.Context, db *sql.DB) error {
	hasName, err := sqliteColumnExists(ctx, db, "codex_endpoints", "name")
	if err != nil {
		return err
	}
	if hasName {
		return nil
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN name TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("为 codex_endpoints 添加 name 字段失败: %w", err)
	}
	// 把空 name 兜底成 base_url；唯一约束由凭证分组级索引统一维护。
	if _, err := db.ExecContext(ctx, `UPDATE codex_endpoints SET name = base_url WHERE name = '' OR name IS NULL`); err != nil {
		return fmt.Errorf("回填 codex_endpoints.name 失败: %w", err)
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointCredentialGroupUniqueConstraints(ctx context.Context, db *sql.DB) error {
	needsRebuild, err := codexEndpointHasChannelScopedUniqueConstraints(ctx, db)
	if err != nil {
		return err
	}
	if needsRebuild {
		if err := m.rebuildCodexEndpointsForCredentialGroupUniqueness(ctx, db); err != nil {
			return err
		}
	}

	stmts := []string{
		`DROP INDEX IF EXISTS idx_codex_endpoints_unique_name`,
		`CREATE INDEX IF NOT EXISTS idx_codex_endpoints_channel ON codex_endpoints(channel_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_single_active_per_channel
			ON codex_endpoints(channel_id, is_active) WHERE is_active = 1`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_group_base_url
			ON codex_endpoints(channel_id, credential_group_id, base_url) WHERE credential_group_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_legacy_base_url
			ON codex_endpoints(channel_id, base_url) WHERE credential_group_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_group_name
			ON codex_endpoints(channel_id, credential_group_id, name) WHERE credential_group_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_legacy_name
			ON codex_endpoints(channel_id, name) WHERE credential_group_id IS NULL`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("维护 Codex 端点凭证分组唯一约束失败: %w", err)
		}
	}
	return nil
}

func codexEndpointHasChannelScopedUniqueConstraints(ctx context.Context, db *sql.DB) (bool, error) {
	var tableSQL string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'table' AND name = 'codex_endpoints'`).Scan(&tableSQL); err != nil {
		return false, fmt.Errorf("读取 codex_endpoints 表结构失败: %w", err)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(tableSQL)), "")
	return strings.Contains(normalized, "unique(channel_id,base_url)") ||
		strings.Contains(normalized, "unique(channel_id,name)"), nil
}

func (m *schemaMigrator) rebuildCodexEndpointsForCredentialGroupUniqueness(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启 Codex 端点唯一约束迁移事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmts := []string{
		`DROP TRIGGER IF EXISTS update_codex_endpoints_timestamp`,
		`CREATE TABLE codex_endpoints_group_scoped_unique (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
			credential_group_id INTEGER REFERENCES codex_credential_groups(id) ON DELETE SET NULL,
			api_key TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0,
			failover_enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
			cost_multiplier REAL NOT NULL DEFAULT 1.0,
			input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
			output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
			cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
			billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
			model_multipliers TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
		)`,
		`INSERT INTO codex_endpoints_group_scoped_unique (
			id, channel_id, credential_group_id, api_key, name, base_url, is_active,
			failover_enabled, priority, timeout_seconds, cost_multiplier,
			input_cost_multiplier, output_cost_multiplier, cache_read_cost_multiplier,
			billing_output_token_source, model_multipliers, created_at, updated_at
		)
		SELECT
			id, channel_id, credential_group_id, api_key, name, base_url, is_active,
			failover_enabled, priority, timeout_seconds, cost_multiplier,
			input_cost_multiplier, output_cost_multiplier, cache_read_cost_multiplier,
			billing_output_token_source, model_multipliers, created_at, updated_at
		FROM codex_endpoints`,
		`DROP TABLE codex_endpoints`,
		`ALTER TABLE codex_endpoints_group_scoped_unique RENAME TO codex_endpoints`,
		`CREATE TRIGGER IF NOT EXISTS update_codex_endpoints_timestamp
			AFTER UPDATE ON codex_endpoints
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
		BEGIN
			UPDATE codex_endpoints SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
		END`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("迁移 Codex 端点唯一约束失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Codex 端点唯一约束迁移失败: %w", err)
	}
	if m.logger != nil {
		m.logger.Info("✅ Codex 端点唯一约束已迁移到凭证分组维度")
	}
	return nil
}

func (m *schemaMigrator) ensureCodexChannelModelMappingColumn(ctx context.Context, db *sql.DB) error {
	exists, err := sqliteColumnExists(ctx, db, "codex_channels", "model_mapping")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE codex_channels ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return fmt.Errorf("为 codex_channels 添加 model_mapping 字段失败: %w", err)
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointFailoverColumns(ctx context.Context, db *sql.DB) error {
	hasFailover, err := sqliteColumnExists(ctx, db, "codex_endpoints", "failover_enabled")
	if err != nil {
		return err
	}
	if !hasFailover {
		if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN failover_enabled INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("为 codex_endpoints 添加 failover_enabled 字段失败: %w", err)
		}
	}
	hasSkipHealthCheck, err := sqliteColumnExists(ctx, db, "codex_endpoints", "skip_health_check")
	if err != nil {
		return err
	}
	if !hasSkipHealthCheck {
		if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN skip_health_check INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("为 codex_endpoints 添加 skip_health_check 字段失败: %w", err)
		}
	}
	hasPriority, err := sqliteColumnExists(ctx, db, "codex_endpoints", "priority")
	if err != nil {
		return err
	}
	if !hasPriority {
		if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("为 codex_endpoints 添加 priority 字段失败: %w", err)
		}
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointTimeoutColumn(ctx context.Context, db *sql.DB) error {
	exists, err := sqliteColumnExists(ctx, db, "codex_endpoints", "timeout_seconds")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN timeout_seconds INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("为 codex_endpoints 添加 timeout_seconds 字段失败: %w", err)
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointCostMultiplierColumn(ctx context.Context, db *sql.DB) error {
	hasCostMultiplier, err := sqliteColumnExists(ctx, db, "codex_endpoints", "cost_multiplier")
	if err != nil {
		return err
	}
	if !hasCostMultiplier {
		if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN cost_multiplier REAL NOT NULL DEFAULT 1.0`); err != nil {
			return fmt.Errorf("为 codex_endpoints 添加 cost_multiplier 字段失败: %w", err)
		}
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointDetailedCostColumns(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name string
		ddl  string
	}{
		{"input_cost_multiplier", `ALTER TABLE codex_endpoints ADD COLUMN input_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"output_cost_multiplier", `ALTER TABLE codex_endpoints ADD COLUMN output_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"cache_read_cost_multiplier", `ALTER TABLE codex_endpoints ADD COLUMN cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
	}
	for _, col := range columns {
		exists, err := sqliteColumnExists(ctx, db, "codex_endpoints", col.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, col.ddl); err != nil {
				return fmt.Errorf("为 codex_endpoints 添加 %s 字段失败: %w", col.name, err)
			}
		}
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointBillingOutputTokenSourceColumn(ctx context.Context, db *sql.DB) error {
	exists, err := sqliteColumnExists(ctx, db, "codex_endpoints", "billing_output_token_source")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens'`); err != nil {
		return fmt.Errorf("为 codex_endpoints 添加 billing_output_token_source 字段失败: %w", err)
	}
	return nil
}

func (m *schemaMigrator) ensureCodexEndpointModelMultipliersColumn(ctx context.Context, db *sql.DB) error {
	exists, err := sqliteColumnExists(ctx, db, "codex_endpoints", "model_multipliers")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE codex_endpoints ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return fmt.Errorf("为 codex_endpoints 添加 model_multipliers 字段失败: %w", err)
	}
	return nil
}

func sqliteTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func sqliteColumnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", table)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue interface{}
		var pk int

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}

// EnsureSchema migrates Codex-owned tables using the application logger.
func EnsureSchema(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	return (&schemaMigrator{logger: logger}).ensureSchema(ctx, db)
}

type schemaMigrator struct{ logger *slog.Logger }
