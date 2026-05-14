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

	"claude-request-forwarder/internal/store"
)

const codexProviderWireAPI = "responses"
const codexProvider1MContextWindow = "1000000"

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

	tableExists, err := sqliteTableExists(ctx, db, "codex_providers")
	if err != nil {
		return err
	}

	if tableExists {
		hasReasoning, err := sqliteColumnExists(ctx, db, "codex_providers", "model_reasoning_effort")
		if err != nil {
			return err
		}
		if !hasReasoning {
			if _, err := db.ExecContext(ctx, "ALTER TABLE codex_providers ADD COLUMN model_reasoning_effort TEXT NOT NULL DEFAULT 'xhigh'"); err != nil {
				return fmt.Errorf("为 codex_providers 添加 model_reasoning_effort 字段失败: %w", err)
			}
		}

		hasEnable1MContext, err := sqliteColumnExists(ctx, db, "codex_providers", "enable_1m_context")
		if err != nil {
			return err
		}
		if !hasEnable1MContext {
			if _, err := db.ExecContext(ctx, "ALTER TABLE codex_providers ADD COLUMN enable_1m_context INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("为 codex_providers 添加 enable_1m_context 字段失败: %w", err)
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
		`CREATE TABLE IF NOT EXISTS codex_endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			UNIQUE(channel_id, base_url),
			UNIQUE(channel_id, name)
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
	// 把空 name 兜底成 base_url（保留 UNIQUE 约束语义）
	if _, err := db.ExecContext(ctx, `UPDATE codex_endpoints SET name = base_url WHERE name = '' OR name IS NULL`); err != nil {
		return fmt.Errorf("回填 codex_endpoints.name 失败: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_name ON codex_endpoints(channel_id, name)`); err != nil {
		return fmt.Errorf("创建 codex_endpoints name 唯一索引失败: %w", err)
	}
	return nil
}

// migrateCodexProvidersToChannels 在首次启动时把旧 codex_providers 数据迁移到新表。
// 幂等：仅当旧表存在 && 新 codex_channels 表为空时执行；迁移完成后把旧表重命名为 codex_providers_legacy_v7。
func (a *App) migrateCodexProvidersToChannels(ctx context.Context, db *sql.DB, oldTableExists bool) error {
	if !oldTableExists {
		return nil
	}

	var channelCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM codex_channels").Scan(&channelCount); err != nil {
		return fmt.Errorf("读取 codex_channels 计数失败: %w", err)
	}
	if channelCount > 0 {
		// 已迁移过，幂等返回
		return nil
	}

	var oldCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM codex_providers").Scan(&oldCount); err != nil {
		return fmt.Errorf("读取 codex_providers 计数失败: %w", err)
	}
	if oldCount == 0 {
		// 旧表无数据，直接重命名作为归档
		return a.archiveLegacyCodexProvidersTable(ctx, db)
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
		return fmt.Errorf("读取旧 codex_providers 数据失败: %w", err)
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
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO codex_endpoints (channel_id, name, base_url, is_active) VALUES (?, ?, ?, 1)
		`, channelID, "默认端点", r.baseURL); err != nil {
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

	return a.archiveLegacyCodexProvidersTable(ctx, db)
}

// archiveLegacyCodexProvidersTable 把旧表重命名为 *_legacy_v7 作为安全归档（非破坏性）。
// 幂等：若旧表已不存在或归档表已存在则跳过。
func (a *App) archiveLegacyCodexProvidersTable(ctx context.Context, db *sql.DB) error {
	stillExists, err := sqliteTableExists(ctx, db, "codex_providers")
	if err != nil {
		return err
	}
	if !stillExists {
		return nil
	}
	legacyExists, err := sqliteTableExists(ctx, db, "codex_providers_legacy_v7")
	if err != nil {
		return err
	}
	if legacyExists {
		// 历史归档已存在，删除旧表（避免重复触发）
		if _, err := db.ExecContext(ctx, "DROP TABLE codex_providers"); err != nil {
			return fmt.Errorf("删除已归档的旧 codex_providers 失败: %w", err)
		}
		return nil
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE codex_providers RENAME TO codex_providers_legacy_v7"); err != nil {
		return fmt.Errorf("归档旧 codex_providers 失败: %w", err)
	}
	if a.logger != nil {
		a.logger.Info("📦 旧 codex_providers 已归档为 codex_providers_legacy_v7")
	}
	return nil
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

func (a *App) syncActiveCodexProviderToConfig(record *store.CodexProviderRecord, previousName string) error {
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

func (a *App) removeCodexProviderConfig(name string, nextActive *store.CodexProviderRecord) error {
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

func buildCodexTopLevelConfigValues(record *store.CodexProviderRecord) map[string]codexTopLevelValue {
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

func upsertCodexProviderSection(content string, record *store.CodexProviderRecord) (string, error) {
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
	header := fmt.Sprintf("[model_providers.%s]", name)

	start := -1
	end := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start == -1 {
			if trimmed == header {
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

func renderCodexProviderSection(record *store.CodexProviderRecord) []string {
	return []string{
		fmt.Sprintf("[model_providers.%s]", record.Name),
		fmt.Sprintf("name = \"%s\"", escapeTomlString(record.Name)),
		fmt.Sprintf("base_url = \"%s\"", escapeTomlString(record.BaseURL)),
		fmt.Sprintf("wire_api = \"%s\"", codexProviderWireAPI),
		"requires_openai_auth = true",
	}
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
func codexChannelEndpointAsProviderRecord(channel *store.CodexChannelRecord, endpoint *store.CodexEndpointRecord) *store.CodexProviderRecord {
	if channel == nil || endpoint == nil {
		return nil
	}
	return &store.CodexProviderRecord{
		Name:            channel.Name,
		ProviderType:    channel.ProviderType,
		BaseURL:         endpoint.BaseURL,
		ApiKey:          channel.ApiKey,
		Model:           channel.Model,
		ReasoningEffort: channel.ReasoningEffort,
		Enable1MContext: channel.Enable1MContext,
		Website:         channel.Website,
	}
}

// syncActiveCodexChannelToConfig 将渠道+激活端点同步到 ~/.codex/config.toml 与 auth.json。
// previousChannelName 不为空且与当前不同时，先移除旧 section（用于渠道改名）。
func (a *App) syncActiveCodexChannelToConfig(channel *store.CodexChannelRecord, endpoint *store.CodexEndpointRecord, previousChannelName string) error {
	if channel == nil {
		return fmt.Errorf("Codex 渠道为空")
	}
	if endpoint == nil {
		return fmt.Errorf("Codex 渠道 '%s' 没有可用端点", channel.Name)
	}
	record := codexChannelEndpointAsProviderRecord(channel, endpoint)
	return a.syncActiveCodexProviderToConfig(record, previousChannelName)
}

// removeCodexChannelConfig 删除指定渠道的 config.toml section；若 nextChannel/nextEndpoint 不为空，则切换到下一个渠道。
func (a *App) removeCodexChannelConfig(name string, nextChannel *store.CodexChannelRecord, nextEndpoint *store.CodexEndpointRecord) error {
	var nextRecord *store.CodexProviderRecord
	if nextChannel != nil && nextEndpoint != nil {
		nextRecord = codexChannelEndpointAsProviderRecord(nextChannel, nextEndpoint)
	}
	return a.removeCodexProviderConfig(name, nextRecord)
}
