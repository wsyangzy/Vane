package storage

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var sqliteSchemaFS embed.FS

// InitSchema 初始化SQLite数据库Schema
func (s *SQLiteAdapter) InitSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.logger.Info("正在初始化SQLite数据库Schema")
	for _, table := range []string{"claude_credential_groups", "codex_credential_groups"} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		enabled, err := s.columnExists(ctx, table, "enabled")
		if err != nil {
			return err
		}
		if !enabled {
			if _, err = s.backupDatabaseFile(ctx, "pre-account-pools"); err != nil {
				return fmt.Errorf("账号池迁移前备份失败: %w", err)
			}
			break
		}
	}

	// 🔧 [P1.2 新增] 首先创建/检查 schema_versions 表
	// 用于追踪迁移执行状态，让迁移可观察、可回滚、可幂等
	if err := s.ensureSchemaVersionsTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure schema_versions table: %w", err)
	}

	// 读取并执行SQLite schema
	schema, err := sqliteSchemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema.sql: %w", err)
	}

	// SQLite可以直接执行整个schema。
	// 但：旧库可能缺少后续新增的列，schema.sql 中的索引/触发器可能会引用这些列，
	// 导致 Exec 直接失败并中断启动。为保证向后兼容：
	// - 先尝试执行 schema.sql
	// - 若因"no such column"失败，则先跑 migrateSchema 补列，再重试执行 schema.sql
	if _, err := s.GetWriteDB().ExecContext(ctx, string(schema)); err != nil {
		if strings.Contains(err.Error(), "no such column:") {
			s.logger.Warn("schema.sql 执行失败（缺少列），将先执行迁移后重试", "error", err)
			if err := s.migrateSchema(ctx); err != nil {
				return fmt.Errorf("failed to migrate schema (pre-schema retry): %w", err)
			}
			if _, err := s.GetWriteDB().ExecContext(ctx, string(schema)); err != nil {
				return fmt.Errorf("failed to execute schema (after migrate): %w", err)
			}
		} else {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	// 执行迁移添加新字段
	if err := s.migrateSchema(ctx); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	s.logger.Info("✅ SQLite数据库Schema初始化完成")
	return nil
}

// ensureSchemaVersionsTable 创建 schema_versions 表（如不存在）
//
// 🔧 [P1.2 新增] 引入 schema 版本管理：
//   - 每个迁移有唯一的 version 标识（如 "20251205_add_endpoint_chain"）
//   - 已成功执行的迁移记录在 schema_versions 表中
//   - 后续启动跳过已执行的迁移，提供幂等性
//   - 迁移失败时记录失败状态，便于排查
//
// 表结构：
//
//	version       TEXT PRIMARY KEY  -- 迁移唯一标识
//	description   TEXT              -- 人类可读描述
//	applied_at    DATETIME          -- 成功应用时间
//	checksum      TEXT              -- 迁移 SQL 的简单校验和（未来扩展）
func (s *SQLiteAdapter) ensureSchemaVersionsTable(ctx context.Context) error {
	if s.GetWriteDB() == nil {
		return fmt.Errorf("write DB not available")
	}
	const ddl = `
		CREATE TABLE IF NOT EXISTS schema_versions (
			version     TEXT PRIMARY KEY NOT NULL,
			description TEXT,
			applied_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			checksum    TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_schema_versions_applied_at ON schema_versions(applied_at);
	`
	if _, err := s.GetWriteDB().ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create schema_versions table: %w", err)
	}
	return nil
}

// isSchemaVersionApplied 检查指定 version 的迁移是否已成功应用
// 🔧 [P1.2 新增]
func (s *SQLiteAdapter) isSchemaVersionApplied(ctx context.Context, version string) (bool, error) {
	if s.GetReadDB() == nil {
		return false, fmt.Errorf("read DB not available")
	}
	var count int
	err := s.GetReadDB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_versions WHERE version = ?", version).Scan(&count)
	if err != nil {
		// 如果表本身不存在，视为未应用（首次启动场景）
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, fmt.Errorf("check schema version %s: %w", version, err)
	}
	return count > 0, nil
}

// recordSchemaVersion 记录迁移已成功应用
// 🔧 [P1.2 新增]
func (s *SQLiteAdapter) recordSchemaVersion(ctx context.Context, version, description string) error {
	if s.GetWriteDB() == nil {
		return fmt.Errorf("write DB not available")
	}
	_, err := s.GetWriteDB().ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_versions (version, description) VALUES (?, ?)`,
		version, description)
	if err != nil {
		return fmt.Errorf("record schema version %s: %w", version, err)
	}
	return nil
}

// Schema returns a fresh copy of the schema used by the primary and recovery databases.
func Schema() ([]byte, error) { return sqliteSchemaFS.ReadFile("schema.sql") }
