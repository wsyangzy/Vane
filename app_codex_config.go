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
	if !tableExists {
		return nil
	}

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

	return nil
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
