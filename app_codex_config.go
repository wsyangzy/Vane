package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	codexstore "ai-relay-forwarder/internal/providers/codex/store"
)

const codexProviderWireAPI = "responses"
const codexProvider1MContextWindow = "1000000"
const codexProvidersTableName = "codex_providers"
const codexProvidersArchiveTableName = "codex_providers_legacy"

var deprecatedCodexProviderArchiveTableNames = []string{"codex_providers_legacy_v7"}

type codexTopLevelValue struct {
	Value  string
	Quoted bool
}

func (a *App) ensureCodexProviderSchema(ctx context.Context) error {
	a.mu.RLock()
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()

	if db == nil {
		return fmt.Errorf("Codex 提供商数据库未就绪")
	}

	if err := a.normalizeCodexProviderArchiveTables(ctx, db); err != nil {
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

	if err := a.ensureCodexChannelsSchema(ctx, db); err != nil {
		return err
	}

	return a.migrateCodexProvidersToChannels(ctx, db, tableExists)
}

// ensureCodexChannelsSchema 创建 codex_channels / codex_endpoints 两张表及索引/触发器（幂等）。
func (a *App) ensureCodexChannelsSchema(ctx context.Context, db *sql.DB) error {
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
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
				name TEXT NOT NULL,
				api_key TEXT NOT NULL DEFAULT '',
				is_default INTEGER NOT NULL DEFAULT 0,
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
					disable_long_context_pricing INTEGER NOT NULL DEFAULT 0,
					billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
					model_multipliers TEXT NOT NULL DEFAULT '[]',
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

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("启用 SQLite foreign_keys 失败: %w", err)
	}

	// 为已升级到 v7.0 但尚未含 name 列的 codex_endpoints 表补字段（向后兼容）。
	// 默认值用 base_url 自身（同渠道唯一约束保证不冲突）；用户可在 UI 中重命名。
	if err := a.ensureCodexEndpointNameColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_channels 补充模型映射字段
	if err := a.ensureCodexChannelModelMappingColumn(ctx, db); err != nil {
		return err
	}

	// 为 Codex 凭证分组补充 schema、默认分组和端点引用
	if err := a.ensureCodexCredentialGroupSchema(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充故障转移字段（v7.2+）
	if err := a.ensureCodexEndpointFailoverColumns(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充端点级请求超时字段（0 = 使用全局超时）
	if err := a.ensureCodexEndpointTimeoutColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充成本倍率字段（v7.2+）
	if err := a.ensureCodexEndpointCostMultiplierColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充细分成本倍率字段（v7.2+）
	if err := a.ensureCodexEndpointDetailedCostColumns(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充输出计费口径字段
	if err := a.ensureCodexEndpointBillingOutputTokenSourceColumn(ctx, db); err != nil {
		return err
	}

	// 为 codex_endpoints 补充模型级倍率字段
	if err := a.ensureCodexEndpointModelMultipliersColumn(ctx, db); err != nil {
		return err
	}

	if err := a.backfillCodexEndpointCredentialGroups(ctx, db); err != nil {
		return err
	}

	if err := a.backfillCodexCredentialGroupModelMultipliers(ctx, db); err != nil {
		return err
	}

	if err := a.ensureCodexEndpointCredentialGroupUniqueConstraints(ctx, db); err != nil {
		return err
	}

	return nil
}

func (a *App) ensureCodexCredentialGroupSchema(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		table string
		name  string
		ddl   string
	}{
		{"codex_endpoints", "credential_group_id", `ALTER TABLE codex_endpoints ADD COLUMN credential_group_id INTEGER`},
		{"codex_endpoints", "api_key", `ALTER TABLE codex_endpoints ADD COLUMN api_key TEXT NOT NULL DEFAULT ''`},
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
		{"codex_credential_groups", "disable_long_context_pricing", `ALTER TABLE codex_credential_groups ADD COLUMN disable_long_context_pricing INTEGER NOT NULL DEFAULT 0`},
		{"codex_credential_groups", "billing_output_token_source", `ALTER TABLE codex_credential_groups ADD COLUMN billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens'`},
		{"codex_credential_groups", "model_multipliers", `ALTER TABLE codex_credential_groups ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'`},
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
		SELECT id, '默认分组', api_key, 1, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 0, 'output_tokens', '[]'
		FROM codex_channels ch
		WHERE NOT EXISTS (
			SELECT 1 FROM codex_credential_groups g WHERE g.channel_id = ch.id
		)
	`); err != nil {
		return fmt.Errorf("回填 Codex 默认凭证分组失败: %w", err)
	}
	return nil
}

func (a *App) backfillCodexEndpointCredentialGroups(ctx context.Context, db *sql.DB) error {
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

func (a *App) backfillCodexCredentialGroupModelMultipliers(ctx context.Context, db *sql.DB) error {
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

func (a *App) ensureCodexEndpointNameColumn(ctx context.Context, db *sql.DB) error {
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

func (a *App) ensureCodexEndpointCredentialGroupUniqueConstraints(ctx context.Context, db *sql.DB) error {
	needsRebuild, err := codexEndpointHasChannelScopedUniqueConstraints(ctx, db)
	if err != nil {
		return err
	}
	if needsRebuild {
		if err := a.rebuildCodexEndpointsForCredentialGroupUniqueness(ctx, db); err != nil {
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

func (a *App) rebuildCodexEndpointsForCredentialGroupUniqueness(ctx context.Context, db *sql.DB) error {
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
	if a.logger != nil {
		a.logger.Info("✅ Codex 端点唯一约束已迁移到凭证分组维度")
	}
	return nil
}

func (a *App) ensureCodexChannelModelMappingColumn(ctx context.Context, db *sql.DB) error {
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

// ensureCodexEndpointFailoverColumns 为 codex_endpoints 补充 failover_enabled、skip_health_check 和 priority 字段（幂等）。
func (a *App) ensureCodexEndpointFailoverColumns(ctx context.Context, db *sql.DB) error {
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

func (a *App) ensureCodexEndpointTimeoutColumn(ctx context.Context, db *sql.DB) error {
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

// ensureCodexEndpointCostMultiplierColumn 为 codex_endpoints 补充 cost_multiplier 字段（幂等）。
func (a *App) ensureCodexEndpointCostMultiplierColumn(ctx context.Context, db *sql.DB) error {
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

// ensureCodexEndpointDetailedCostColumns 为 codex_endpoints 补充细分成本倍率字段（幂等）。
func (a *App) ensureCodexEndpointDetailedCostColumns(ctx context.Context, db *sql.DB) error {
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

func (a *App) ensureCodexEndpointBillingOutputTokenSourceColumn(ctx context.Context, db *sql.DB) error {
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

func (a *App) ensureCodexEndpointModelMultipliersColumn(ctx context.Context, db *sql.DB) error {
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

// migrateCodexProvidersToChannels 在首次启动时把旧 codex_providers 数据迁移到新表。
// 幂等：仅当旧表存在 && 新 codex_channels 表为空时执行；迁移完成后把旧表重命名为 codex_providers_legacy。
func (a *App) migrateCodexProvidersToChannels(ctx context.Context, db *sql.DB, oldTableExists bool) error {
	if !oldTableExists {
		return nil
	}

	var channelCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM codex_channels").Scan(&channelCount); err != nil {
		return fmt.Errorf("读取 codex_channels 计数失败: %w", err)
	}
	if channelCount > 0 {
		// 已迁移过，仍归档旧表，避免最终库继续保留旧主表。
		return a.archiveCodexProvidersTable(ctx, db)
	}

	var oldCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+codexProvidersTableName).Scan(&oldCount); err != nil {
		return fmt.Errorf("读取 %s 计数失败: %w", codexProvidersTableName, err)
	}
	if oldCount == 0 {
		// 旧表无数据，直接重命名作为归档
		return a.archiveCodexProvidersTable(ctx, db)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启迁移事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT name, provider_type, base_url, api_key, model, model_reasoning_effort,
			COALESCE(enable_1m_context, 0), COALESCE(website, ''), COALESCE(is_active, 0)
		FROM codex_providers
	`)
	if err != nil {
		return fmt.Errorf("读取旧 %s 数据失败: %w", codexProvidersTableName, err)
	}

	type legacyRow struct {
		name, providerType, baseURL, apiKey, model, reasoningEffort, website string
		enable1M, isActive                                                   int
	}
	var rowsBuf []legacyRow

	for rows.Next() {
		var r legacyRow
		if err := rows.Scan(&r.name, &r.providerType, &r.baseURL, &r.apiKey, &r.model,
			&r.reasoningEffort, &r.enable1M, &r.website, &r.isActive); err != nil {
			rows.Close()
			return fmt.Errorf("扫描旧 codex_providers 数据失败: %w", err)
		}
		rowsBuf = append(rowsBuf, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("迭代旧 codex_providers 数据失败: %w", err)
	}

	for _, r := range rowsBuf {
		websiteArg := nullableTomlString(r.website)
		channelResult, err := tx.ExecContext(ctx, `
			INSERT INTO codex_channels (name, provider_type, api_key, model, model_reasoning_effort,
				enable_1m_context, website, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, r.name, r.providerType, r.apiKey, r.model, r.reasoningEffort, r.enable1M, websiteArg, r.isActive)
		if err != nil {
			return fmt.Errorf("迁移渠道 %s 失败: %w", r.name, err)
		}
		channelID, err := channelResult.LastInsertId()
		if err != nil {
			return fmt.Errorf("获取迁移渠道 ID 失败: %w", err)
		}
		groupResult, err := tx.ExecContext(ctx, `
			INSERT INTO codex_credential_groups (channel_id, name, api_key, is_default)
			VALUES (?, ?, ?, 1)
		`, channelID, "默认分组", r.apiKey)
		if err != nil {
			return fmt.Errorf("迁移默认凭证分组 %s 失败: %w", r.name, err)
		}
		groupID, err := groupResult.LastInsertId()
		if err != nil {
			return fmt.Errorf("获取迁移默认凭证分组 ID 失败: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO codex_endpoints (channel_id, credential_group_id, name, base_url, is_active) VALUES (?, ?, ?, ?, 1)
		`, channelID, groupID, "默认端点", r.baseURL); err != nil {
			return fmt.Errorf("迁移端点 %s 失败: %w", r.baseURL, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交迁移事务失败: %w", err)
	}

	if a.logger != nil {
		a.logger.Info("✅ Codex 旧数据已迁移到渠道-端点模型",
			"migrated_channels", len(rowsBuf),
		)
	}

	return a.archiveCodexProvidersTable(ctx, db)
}

// archiveCodexProvidersTable 把旧表重命名为 codex_providers_legacy 作为安全归档（非破坏性）。
// 幂等：若旧表已不存在则跳过；若归档表已存在则合并后删除旧表。
func (a *App) archiveCodexProvidersTable(ctx context.Context, db *sql.DB) error {
	if err := a.normalizeCodexProviderArchiveTables(ctx, db); err != nil {
		return err
	}
	stillExists, err := sqliteTableExists(ctx, db, codexProvidersTableName)
	if err != nil {
		return err
	}
	if !stillExists {
		return nil
	}
	archiveExists, err := sqliteTableExists(ctx, db, codexProvidersArchiveTableName)
	if err != nil {
		return err
	}
	if archiveExists {
		if err := mergeSQLiteTablesByCommonColumns(ctx, db, codexProvidersTableName, codexProvidersArchiveTableName); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "DROP TABLE "+codexProvidersTableName); err != nil {
			return fmt.Errorf("删除已归档的旧 %s 失败: %w", codexProvidersTableName, err)
		}
		return nil
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE "+codexProvidersTableName+" RENAME TO "+codexProvidersArchiveTableName); err != nil {
		return fmt.Errorf("归档旧 %s 失败: %w", codexProvidersTableName, err)
	}
	if a.logger != nil {
		a.logger.Info("📦 旧 codex_providers 已归档为 codex_providers_legacy")
	}
	return nil
}

func (a *App) normalizeCodexProviderArchiveTables(ctx context.Context, db *sql.DB) error {
	for _, tableName := range deprecatedCodexProviderArchiveTableNames {
		exists, err := sqliteTableExists(ctx, db, tableName)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		archiveExists, err := sqliteTableExists(ctx, db, codexProvidersArchiveTableName)
		if err != nil {
			return err
		}
		if !archiveExists {
			if _, err := db.ExecContext(ctx, "ALTER TABLE "+tableName+" RENAME TO "+codexProvidersArchiveTableName); err != nil {
				return fmt.Errorf("归一化 Codex 旧归档表失败: %w", err)
			}
			continue
		}
		if err := mergeSQLiteTablesByCommonColumns(ctx, db, tableName, codexProvidersArchiveTableName); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "DROP TABLE "+tableName); err != nil {
			return fmt.Errorf("删除 Codex 旧归档表失败: %w", err)
		}
	}
	return nil
}

func mergeSQLiteTablesByCommonColumns(ctx context.Context, db *sql.DB, sourceTable, targetTable string) error {
	sourceColumns, err := sqliteTableColumnNames(ctx, db, sourceTable)
	if err != nil {
		return err
	}
	targetColumns, err := sqliteTableColumnNames(ctx, db, targetTable)
	if err != nil {
		return err
	}
	targetSet := make(map[string]struct{}, len(targetColumns))
	for _, column := range targetColumns {
		targetSet[column] = struct{}{}
	}
	commonColumns := make([]string, 0, len(sourceColumns))
	for _, column := range sourceColumns {
		if _, ok := targetSet[column]; ok {
			commonColumns = append(commonColumns, column)
		}
	}
	if len(commonColumns) == 0 {
		return fmt.Errorf("合并 SQLite 表失败：%s 与 %s 没有共同字段", sourceTable, targetTable)
	}
	quotedColumns := make([]string, 0, len(commonColumns))
	for _, column := range commonColumns {
		quotedColumns = append(quotedColumns, quoteSQLiteIdentifier(column))
	}
	columnList := strings.Join(quotedColumns, ", ")
	query := fmt.Sprintf(
		"INSERT OR IGNORE INTO %s (%s) SELECT %s FROM %s",
		quoteSQLiteIdentifier(targetTable),
		columnList,
		columnList,
		quoteSQLiteIdentifier(sourceTable),
	)
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("合并 SQLite 表 %s -> %s 失败: %w", sourceTable, targetTable, err)
	}
	return nil
}

func sqliteTableColumnNames(ctx context.Context, db *sql.DB, tableName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteSQLiteIdentifier(tableName)+")")
	if err != nil {
		return nil, fmt.Errorf("读取 SQLite 表字段失败: %w", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return nil, fmt.Errorf("扫描 SQLite 表字段失败: %w", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代 SQLite 表字段失败: %w", err)
	}
	return columns, nil
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func nullableTomlString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *App) getCodexHomeDir() (string, error) {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return codexHome, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}

	return filepath.Join(homeDir, ".codex"), nil
}

func (a *App) getCodexConfigPath() (string, error) {
	codexHome, err := a.getCodexHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(codexHome, "config.toml"), nil
}

func (a *App) getCodexAuthPath() (string, error) {
	codexHome, err := a.getCodexHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(codexHome, "auth.json"), nil
}

func (a *App) syncActiveCodexProviderToConfig(record *codexstore.CodexProviderRecord, previousName string) error {
	if record == nil {
		return fmt.Errorf("Codex 提供商记录为空")
	}

	configPath, originalConfig, configExisted, err := a.readCodexFileAllowMissing(a.getCodexConfigPath)
	if err != nil {
		return err
	}
	authPath, originalAuth, authExisted, err := a.readCodexFileAllowMissing(a.getCodexAuthPath)
	if err != nil {
		return err
	}

	updatedConfig := string(originalConfig)
	if previousName != "" && previousName != record.Name {
		updatedConfig = deleteCodexProviderSection(updatedConfig, previousName)
	}

	updatedConfig, err = upsertCodexProviderSection(updatedConfig, record)
	if err != nil {
		return err
	}
	if !record.Enable1MContext {
		updatedConfig = removeCodexTopLevelConfig(updatedConfig, []string{"model_context_window"})
	}
	updatedConfig, err = upsertCodexTopLevelConfig(updatedConfig, buildCodexTopLevelConfigValues(record))
	if err != nil {
		return err
	}

	updatedAuth, err := upsertCodexAuthConfig(string(originalAuth), record.ApiKey)
	if err != nil {
		return err
	}

	if err := writeCodexLiveFiles(
		authPath,
		originalAuth,
		authExisted,
		updatedAuth,
		configPath,
		originalConfig,
		configExisted,
		[]byte(updatedConfig),
	); err != nil {
		return err
	}

	if a.logger != nil {
		a.logger.Info(
			"✅ Codex 配置已接管",
			"config_path", configPath,
			"auth_path", authPath,
			"model_provider", record.Name,
			"model", record.Model,
			"enable_1m_context", record.Enable1MContext,
			"reasoning_effort", record.ReasoningEffort,
		)
	}

	return nil
}

func (a *App) removeCodexProviderConfig(name string, nextActive *codexstore.CodexProviderRecord) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("Codex 提供商标识不能为空")
	}

	configPath, originalConfig, configExisted, err := a.readCodexFileAllowMissing(a.getCodexConfigPath)
	if err != nil {
		return err
	}

	updatedConfig := deleteCodexProviderSection(string(originalConfig), name)
	if nextActive == nil {
		updatedConfig = removeCodexTopLevelConfig(updatedConfig, []string{"model", "model_context_window", "model_reasoning_effort", "model_provider"})
		if err := writeCodexFile(configPath, []byte(updatedConfig)); err != nil {
			if rollbackErr := restoreCodexFile(configPath, originalConfig, configExisted); rollbackErr != nil {
				return fmt.Errorf("写入 Codex 配置文件失败 (%s): %w；回滚也失败: %v", configPath, err, rollbackErr)
			}
			return fmt.Errorf("写入 Codex 配置文件失败 (%s): %w", configPath, err)
		}
		return nil
	}

	authPath, originalAuth, authExisted, err := a.readCodexFileAllowMissing(a.getCodexAuthPath)
	if err != nil {
		return err
	}

	updatedConfig, err = upsertCodexProviderSection(updatedConfig, nextActive)
	if err != nil {
		return err
	}
	if !nextActive.Enable1MContext {
		updatedConfig = removeCodexTopLevelConfig(updatedConfig, []string{"model_context_window"})
	}
	updatedConfig, err = upsertCodexTopLevelConfig(updatedConfig, buildCodexTopLevelConfigValues(nextActive))
	if err != nil {
		return err
	}

	updatedAuth, err := upsertCodexAuthConfig(string(originalAuth), nextActive.ApiKey)
	if err != nil {
		return err
	}

	return writeCodexLiveFiles(
		authPath,
		originalAuth,
		authExisted,
		updatedAuth,
		configPath,
		originalConfig,
		configExisted,
		[]byte(updatedConfig),
	)
}

func (a *App) readCodexFileAllowMissing(pathGetter func() (string, error)) (string, []byte, bool, error) {
	path, err := pathGetter()
	if err != nil {
		return "", nil, false, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, nil, false, nil
		}
		return "", nil, false, fmt.Errorf("读取 Codex 文件失败 (%s): %w", path, err)
	}

	return path, content, true, nil
}

func writeCodexLiveFiles(
	authPath string,
	originalAuth []byte,
	authExisted bool,
	updatedAuth []byte,
	configPath string,
	originalConfig []byte,
	configExisted bool,
	updatedConfig []byte,
) error {
	if err := writeCodexFile(authPath, updatedAuth); err != nil {
		if rollbackErr := restoreCodexFile(authPath, originalAuth, authExisted); rollbackErr != nil {
			return fmt.Errorf("写入 Codex auth.json 失败 (%s): %w；回滚也失败: %v", authPath, err, rollbackErr)
		}
		return fmt.Errorf("写入 Codex auth.json 失败 (%s): %w", authPath, err)
	}

	if err := writeCodexFile(configPath, updatedConfig); err != nil {
		configRollbackErr := restoreCodexFile(configPath, originalConfig, configExisted)
		authRollbackErr := restoreCodexFile(authPath, originalAuth, authExisted)

		if configRollbackErr != nil || authRollbackErr != nil {
			return fmt.Errorf(
				"写入 Codex 配置文件失败 (%s): %w；config 回滚失败: %v；auth 回滚失败: %v",
				configPath,
				err,
				configRollbackErr,
				authRollbackErr,
			)
		}

		return fmt.Errorf("写入 Codex 配置文件失败 (%s): %w", configPath, err)
	}

	return nil
}

func writeCodexFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

func restoreCodexFile(path string, original []byte, existed bool) error {
	if existed {
		return writeCodexFile(path, original)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func upsertCodexAuthConfig(content string, apiKey string) ([]byte, error) {
	authConfig := make(map[string]any)

	if trimmed := strings.TrimSpace(content); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &authConfig); err != nil {
			return nil, fmt.Errorf("解析 Codex auth.json 失败: %w", err)
		}
	}

	authConfig["OPENAI_API_KEY"] = apiKey

	updated, err := json.MarshalIndent(authConfig, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成 Codex auth.json 失败: %w", err)
	}

	return append(updated, '\n'), nil
}

func buildCodexTopLevelConfigValues(record *codexstore.CodexProviderRecord) map[string]codexTopLevelValue {
	values := map[string]codexTopLevelValue{
		"model": {
			Value:  record.Model,
			Quoted: true,
		},
		"model_reasoning_effort": {
			Value:  record.ReasoningEffort,
			Quoted: true,
		},
		"model_provider": {
			Value:  record.Name,
			Quoted: true,
		},
	}

	if record.Enable1MContext {
		values["model_context_window"] = codexTopLevelValue{
			Value:  codexProvider1MContextWindow,
			Quoted: false,
		}
	}

	return values
}

func upsertCodexTopLevelConfig(content string, values map[string]codexTopLevelValue) (string, error) {
	order := []string{"model", "model_context_window", "model_reasoning_effort", "model_provider"}
	lines := splitCodexLines(content)
	found := make(map[string]bool, len(values))
	firstSectionIndex := -1
	inSection := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if firstSectionIndex == -1 {
				firstSectionIndex = i
			}
			inSection = true
			continue
		}

		if inSection {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		for _, key := range order {
			value, ok := values[key]
			if !ok {
				continue
			}
			if !found[key] && isTopLevelTomlAssignment(trimmed, key) {
				lines[i] = fmt.Sprintf("%s = %s", key, renderCodexTopLevelValue(value))
				found[key] = true
			}
		}
	}

	missing := make([]string, 0, len(order))
	for _, key := range order {
		value, ok := values[key]
		if !ok {
			continue
		}
		if !found[key] {
			missing = append(missing, fmt.Sprintf("%s = %s", key, renderCodexTopLevelValue(value)))
		}
	}

	if len(missing) > 0 {
		insertAt := firstSectionIndex
		if insertAt < 0 {
			insertAt = len(lines)
		}
		insertion := append(missing, "")
		lines = append(lines[:insertAt], append(insertion, lines[insertAt:]...)...)
	}

	return joinCodexLines(lines), nil
}

func renderCodexTopLevelValue(value codexTopLevelValue) string {
	if value.Quoted {
		return fmt.Sprintf("\"%s\"", escapeTomlString(value.Value))
	}
	return value.Value
}

func removeCodexTopLevelConfig(content string, keys []string) string {
	lines := splitCodexLines(content)
	inSection := false
	filtered := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = true
			filtered = append(filtered, line)
			continue
		}

		if inSection {
			filtered = append(filtered, line)
			continue
		}

		shouldSkip := false
		for _, key := range keys {
			if isTopLevelTomlAssignment(trimmed, key) {
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			continue
		}

		filtered = append(filtered, line)
	}

	return joinCodexLines(filtered)
}

func upsertCodexProviderSection(content string, record *codexstore.CodexProviderRecord) (string, error) {
	if record == nil {
		return "", fmt.Errorf("Codex 提供商记录为空")
	}

	updated := deleteCodexProviderSection(content, record.Name)
	lines := splitCodexLines(updated)

	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}

	lines = append(lines, renderCodexProviderSection(record)...)
	lines = append(lines, "")
	return joinCodexLines(lines), nil
}

func deleteCodexProviderSection(content, name string) string {
	lines := splitCodexLines(content)
	headers := codexProviderSectionHeaders(name)

	start := -1
	end := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start == -1 {
			if containsString(headers, trimmed) {
				start = i
			}
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			end = i
			break
		}
	}

	if start == -1 {
		return joinCodexLines(lines)
	}
	if end == -1 {
		end = len(lines)
	}

	lines = append(lines[:start], lines[end:]...)
	return joinCodexLines(lines)
}

func renderCodexProviderSection(record *codexstore.CodexProviderRecord) []string {
	return []string{
		codexProviderSectionHeader(record.Name),
		fmt.Sprintf("name = \"%s\"", escapeTomlString(record.Name)),
		fmt.Sprintf("base_url = \"%s\"", escapeTomlString(record.BaseURL)),
		fmt.Sprintf("wire_api = \"%s\"", codexProviderWireAPI),
		"requires_openai_auth = true",
	}
}

func codexProviderSectionHeader(name string) string {
	if isTomlBareKey(name) {
		return fmt.Sprintf("[model_providers.%s]", name)
	}
	return fmt.Sprintf("[model_providers.\"%s\"]", escapeTomlString(name))
}

func codexProviderSectionHeaders(name string) []string {
	formatted := codexProviderSectionHeader(name)
	legacy := fmt.Sprintf("[model_providers.%s]", name)
	if formatted == legacy {
		return []string{formatted}
	}
	return []string{formatted, legacy}
}

func isTomlBareKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func splitCodexLines(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if normalized == "" {
		return []string{}
	}
	return strings.Split(normalized, "\n")
}

func joinCodexLines(lines []string) string {
	updated := strings.Join(lines, "\n")
	updated = strings.TrimRight(updated, "\r\n")
	if updated == "" {
		return ""
	}
	return updated + "\n"
}

func isTopLevelTomlAssignment(trimmedLine, key string) bool {
	parts := strings.SplitN(trimmedLine, "=", 2)
	if len(parts) != 2 {
		return false
	}
	return strings.TrimSpace(parts[0]) == key
}

func escapeTomlString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", `"`, `\"`)
	return replacer.Replace(value)
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

// codexChannelEndpointAsProviderRecord 把渠道+端点适配为旧 CodexProviderRecord 形态，
// 以便复用 upsertCodexProviderSection / buildCodexTopLevelConfigValues / upsertCodexAuthConfig 等已有工具。
// 这是一个纯本地适配器（DRY），不会被外部使用。
//
// model 字段遵循「渠道级覆盖优先于全局默认」的规则：
//   - channel.Model 非空 → 写入 toml 时使用该值
//   - channel.Model 为空 → 读取 ~/.codex/config.toml 当前顶层 model 作为 fallback
//   - 全局也为空 → 写入空字符串（保持现有行为，由 Codex CLI 自己报错）
//
// 改为方法以便访问 App 的 codex 配置读取能力。
func (a *App) codexChannelEndpointAsProviderRecord(channel *codexstore.CodexChannelRecord, endpoint *codexstore.CodexEndpointRecord) *codexstore.CodexProviderRecord {
	if channel == nil || endpoint == nil {
		return nil
	}

	effectiveModel := channel.Model
	if effectiveModel == "" {
		// 读取 ~/.codex/config.toml 当前的全局 model 作为 fallback
		if _, content, existed, err := a.readCodexFileAllowMissing(a.getCodexConfigPath); err == nil && existed {
			effectiveModel = readCodexTopLevelString(string(content), "model")
		}
	}
	effectiveApiKey := strings.TrimSpace(endpoint.ApiKey)
	if effectiveApiKey == "" && a.codexCredentialGroupStore != nil && endpoint.CredentialGroupID > 0 {
		if group, err := a.codexCredentialGroupStore.GetByID(context.Background(), endpoint.CredentialGroupID); err == nil && group != nil {
			effectiveApiKey = strings.TrimSpace(group.ApiKey)
		}
	}
	if effectiveApiKey == "" && a.codexCredentialGroupStore != nil {
		if group, err := a.codexCredentialGroupStore.GetDefaultByChannelID(context.Background(), channel.ID); err == nil && group != nil {
			effectiveApiKey = strings.TrimSpace(group.ApiKey)
		}
	}
	if effectiveApiKey == "" {
		effectiveApiKey = channel.ApiKey
	}

	return &codexstore.CodexProviderRecord{
		Name:            channel.Name,
		ProviderType:    channel.ProviderType,
		BaseURL:         endpoint.BaseURL,
		ApiKey:          effectiveApiKey,
		Model:           effectiveModel,
		ReasoningEffort: channel.ReasoningEffort,
		Enable1MContext: channel.Enable1MContext,
		Website:         channel.Website,
	}
}

// syncActiveCodexChannelToConfig 将渠道+激活端点同步到 ~/.codex/config.toml 与 auth.json。
// previousChannelName 不为空且与当前不同时，先移除旧 section（用于渠道改名）。
func (a *App) syncActiveCodexChannelToConfig(channel *codexstore.CodexChannelRecord, endpoint *codexstore.CodexEndpointRecord, previousChannelName string) error {
	if channel == nil {
		return fmt.Errorf("Codex 渠道为空")
	}
	if endpoint == nil {
		return fmt.Errorf("Codex 渠道 '%s' 没有可用端点", channel.Name)
	}
	// 当 Codex 代理启用时，不再修改 ~/.codex/config.toml
	// 用户手动管理 model_providers，代理通过内部路由根据数据库中的活跃渠道转发请求
	if a.isCodexProxyEnabled() {
		if a.logger != nil {
			a.logger.Info("Codex 代理已启用，跳过 config.toml 写入（仅切换内部路由）",
				"channel", channel.Name,
				"endpoint", endpoint.Name,
			)
		}
		return nil
	}
	record := a.codexChannelEndpointAsProviderRecord(channel, endpoint)
	return a.syncActiveCodexProviderToConfig(record, previousChannelName)
}

// isCodexProxyEnabled 判断 Codex 代理是否启用
func (a *App) isCodexProxyEnabled() bool {
	return true
}

// applyCodexProxyOverride 已废弃 - 代理启用时不再修改 config.toml
// 保留函数以避免影响其他可能的调用者，但实际是 no-op
func (a *App) applyCodexProxyOverride(_ *codexstore.CodexProviderRecord) {
	// no-op: Codex 代理启用时跳过 config.toml 写入
}

// removeCodexChannelConfig 删除指定渠道的 config.toml section；若 nextChannel/nextEndpoint 不为空，则切换到下一个渠道。
func (a *App) removeCodexChannelConfig(name string, nextChannel *codexstore.CodexChannelRecord, nextEndpoint *codexstore.CodexEndpointRecord) error {
	// 当 Codex 代理启用时，不再修改 config.toml
	if a.isCodexProxyEnabled() {
		if a.logger != nil {
			a.logger.Info("Codex 代理已启用，跳过 config.toml 删除（仅清理数据库记录）",
				"channel", name,
			)
		}
		return nil
	}
	var nextRecord *codexstore.CodexProviderRecord
	if nextChannel != nil && nextEndpoint != nil {
		nextRecord = a.codexChannelEndpointAsProviderRecord(nextChannel, nextEndpoint)
	}
	return a.removeCodexProviderConfig(name, nextRecord)
}
