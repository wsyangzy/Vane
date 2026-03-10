package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-request-forwarder/internal/store"
)

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

	return nil
}

func (a *App) getCodexConfigPath() (string, error) {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return filepath.Join(codexHome, "config.toml"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}

	return filepath.Join(homeDir, ".codex", "config.toml"), nil
}

func (a *App) syncActiveCodexProviderToConfig(record *store.CodexProviderRecord) error {
	if record == nil {
		return fmt.Errorf("Codex 提供商记录为空")
	}

	configPath, err := a.getCodexConfigPath()
	if err != nil {
		return err
	}

	contentBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取 Codex 配置文件失败 (%s): %w", configPath, err)
	}

	updated, err := upsertCodexTopLevelConfig(string(contentBytes), map[string]string{
		"model":                  record.Model,
		"model_reasoning_effort": record.ReasoningEffort,
		"model_provider":         record.Name,
	})
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("写入 Codex 配置文件失败 (%s): %w", configPath, err)
	}

	if a.logger != nil {
		a.logger.Info("✅ Codex 配置已接管", "config_path", configPath, "model_provider", record.Name, "model", record.Model, "reasoning_effort", record.ReasoningEffort)
	}

	return nil
}

func upsertCodexTopLevelConfig(content string, values map[string]string) (string, error) {
	order := []string{"model", "model_reasoning_effort", "model_provider"}
	lines := strings.Split(content, "\n")
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
			if !found[key] && isTopLevelTomlAssignment(trimmed, key) {
				lines[i] = fmt.Sprintf("%s = \"%s\"", key, escapeTomlString(values[key]))
				found[key] = true
			}
		}
	}

	missing := make([]string, 0, len(order))
	for _, key := range order {
		if !found[key] {
			missing = append(missing, fmt.Sprintf("%s = \"%s\"", key, escapeTomlString(values[key])))
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

	updated := strings.Join(lines, "\n")
	updated = strings.TrimRight(updated, "\r\n") + "\n"
	return updated, nil
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

