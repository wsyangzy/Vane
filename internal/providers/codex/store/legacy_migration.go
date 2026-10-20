package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (m *schemaMigrator) migrateCodexProvidersToChannels(ctx context.Context, db *sql.DB, oldTableExists bool) error {
	if !oldTableExists {
		return nil
	}

	var channelCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM codex_channels").Scan(&channelCount); err != nil {
		return fmt.Errorf("读取 codex_channels 计数失败: %w", err)
	}
	if channelCount > 0 {
		// 已迁移过，仍归档旧表，避免最终库继续保留旧主表。
		return m.archiveCodexProvidersTable(ctx, db)
	}

	var oldCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+codexProvidersTableName).Scan(&oldCount); err != nil {
		return fmt.Errorf("读取 %s 计数失败: %w", codexProvidersTableName, err)
	}
	if oldCount == 0 {
		// 旧表无数据，直接重命名作为归档
		return m.archiveCodexProvidersTable(ctx, db)
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
		ORDER BY id ASC
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

	for priority, r := range rowsBuf {
		websiteArg := nullableTomlString(r.website)
		channelResult, err := tx.ExecContext(ctx, `
			INSERT INTO codex_channels (name, provider_type, api_key, model, model_reasoning_effort,
				enable_1m_context, website, is_active, priority)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, r.name, r.providerType, r.apiKey, r.model, r.reasoningEffort, r.enable1M, websiteArg, r.isActive, priority)
		if err != nil {
			return fmt.Errorf("迁移渠道 %s 失败: %w", r.name, err)
		}
		channelID, err := channelResult.LastInsertId()
		if err != nil {
			return fmt.Errorf("获取迁移渠道 ID 失败: %w", err)
		}
		groupResult, err := tx.ExecContext(ctx, `
			INSERT INTO codex_credential_groups (channel_id, name, api_key, is_default, disable_long_context_pricing)
			VALUES (?, ?, ?, 1, 1)
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

	if m.logger != nil {
		m.logger.Info("✅ Codex 旧数据已迁移到渠道-端点模型",
			"migrated_channels", len(rowsBuf),
		)
	}

	return m.archiveCodexProvidersTable(ctx, db)
}

func (m *schemaMigrator) archiveCodexProvidersTable(ctx context.Context, db *sql.DB) error {
	if err := m.normalizeCodexProviderArchiveTables(ctx, db); err != nil {
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
	if m.logger != nil {
		m.logger.Info("📦 旧 codex_providers 已归档为 codex_providers_legacy")
	}
	return nil
}

func (m *schemaMigrator) normalizeCodexProviderArchiveTables(ctx context.Context, db *sql.DB) error {
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

const codexProvidersTableName = "codex_providers"

const codexProvidersArchiveTableName = "codex_providers_legacy"

var deprecatedCodexProviderArchiveTableNames = []string{"codex_providers_legacy_v7"}
