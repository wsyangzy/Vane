package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"

	_ "modernc.org/sqlite"
)

func (s *SQLiteAdapter) ensureClaudeCredentialGroupsSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS claude_credential_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel TEXT NOT NULL,
    name TEXT NOT NULL,
    token TEXT NOT NULL DEFAULT '',
    api_key TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0,
    daily_cost_limit_usd REAL NOT NULL DEFAULT 0,
    model_policy TEXT NOT NULL DEFAULT 'inherit',
    model_mapping TEXT NOT NULL DEFAULT '{}',
    supported_models TEXT NOT NULL DEFAULT '[]',
    cost_multiplier REAL NOT NULL DEFAULT 1.0,
    input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_creation_cost_multiplier_1h REAL NOT NULL DEFAULT 1.0,
    cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    disable_long_context_pricing INTEGER NOT NULL DEFAULT 1,
    model_multipliers TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    UNIQUE(channel, name)
);
CREATE INDEX IF NOT EXISTS idx_claude_credential_groups_channel ON claude_credential_groups(channel);
CREATE UNIQUE INDEX IF NOT EXISTS idx_claude_credential_groups_single_default
    ON claude_credential_groups(channel, is_default) WHERE is_default = 1;
CREATE TRIGGER IF NOT EXISTS update_claude_credential_groups_timestamp
    AFTER UPDATE ON claude_credential_groups
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE claude_credential_groups SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;`
	if _, err := s.GetWriteDB().ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("failed to ensure claude_credential_groups schema: %w", err)
	}
	hasModelMultipliers, err := s.columnExists(ctx, "claude_credential_groups", "model_multipliers")
	if err != nil {
		return fmt.Errorf("failed to check claude_credential_groups.model_multipliers: %w", err)
	}
	if !hasModelMultipliers {
		if _, err := s.GetWriteDB().ExecContext(ctx, `ALTER TABLE claude_credential_groups ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("failed to add claude_credential_groups.model_multipliers: %w", err)
		}
	}
	for _, migration := range []struct {
		column string
		ddl    string
	}{
		{"daily_cost_limit_usd", `ALTER TABLE claude_credential_groups ADD COLUMN daily_cost_limit_usd REAL NOT NULL DEFAULT 0`},
		{"model_policy", `ALTER TABLE claude_credential_groups ADD COLUMN model_policy TEXT NOT NULL DEFAULT 'inherit'`},
		{"model_mapping", `ALTER TABLE claude_credential_groups ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'`},
		{"supported_models", `ALTER TABLE claude_credential_groups ADD COLUMN supported_models TEXT NOT NULL DEFAULT '[]'`},
		{"long_context_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_input_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_output_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_cache_creation_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_cache_read_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"disable_long_context_pricing", `ALTER TABLE claude_credential_groups ADD COLUMN disable_long_context_pricing INTEGER NOT NULL DEFAULT 1`},
		{"disable_service_tier_pricing", `ALTER TABLE claude_credential_groups ADD COLUMN disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1`},
		{"service_tier_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"service_tier_input_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_input_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"service_tier_output_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_output_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"service_tier_cache_read_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
	} {
		exists, err := s.columnExists(ctx, "claude_credential_groups", migration.column)
		if err != nil {
			return fmt.Errorf("failed to check claude_credential_groups.%s: %w", migration.column, err)
		}
		if !exists {
			if _, err := s.GetWriteDB().ExecContext(ctx, migration.ddl); err != nil {
				return fmt.Errorf("failed to add claude_credential_groups.%s: %w", migration.column, err)
			}
		}
	}
	return claudestore.EnsureClaudeSubscriptionSchema(ctx, s.GetWriteDB())
}

func (s *SQLiteAdapter) backfillClaudeCredentialGroups(ctx context.Context) error {
	endpointsExists, err := s.tableExists(ctx, "endpoints")
	if err != nil {
		return err
	}
	if !endpointsExists {
		return nil
	}

	rows, err := s.GetWriteDB().QueryContext(ctx, `SELECT DISTINCT channel FROM endpoints WHERE channel <> '' ORDER BY channel ASC`)
	if err != nil {
		return fmt.Errorf("failed to query endpoint channels for credential group backfill: %w", err)
	}
	channels := make([]string, 0)
	for rows.Next() {
		var channel string
		if err := rows.Scan(&channel); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan endpoint channel: %w", err)
		}
		channel = strings.TrimSpace(channel)
		if channel != "" {
			channels = append(channels, channel)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to iterate endpoint channels: %w", err)
	}
	rows.Close()

	for _, channel := range channels {
		groupID, err := s.ensureDefaultClaudeCredentialGroup(ctx, channel)
		if err != nil {
			return err
		}
		if groupID <= 0 {
			continue
		}
		if _, err := s.GetWriteDB().ExecContext(ctx, `
			UPDATE claude_credential_groups
			SET model_multipliers = (
				SELECT COALESCE(ep.model_multipliers, '[]')
				FROM endpoints ep
				WHERE ep.channel = claude_credential_groups.channel
					AND (ep.credential_group_id = claude_credential_groups.id OR ep.credential_group_id IS NULL OR ep.credential_group_id = 0)
					AND COALESCE(ep.model_multipliers, '[]') NOT IN ('', '[]')
				ORDER BY COALESCE(ep.priority, 1) ASC, COALESCE(ep.created_at, '') ASC, ep.id ASC
				LIMIT 1
			)
			WHERE id = ?
				AND COALESCE(model_multipliers, '[]') IN ('', '[]')
				AND EXISTS (
					SELECT 1
					FROM endpoints ep
					WHERE ep.channel = claude_credential_groups.channel
						AND (ep.credential_group_id = claude_credential_groups.id OR ep.credential_group_id IS NULL OR ep.credential_group_id = 0)
						AND COALESCE(ep.model_multipliers, '[]') NOT IN ('', '[]')
				)
		`, groupID); err != nil {
			return fmt.Errorf("failed to backfill Claude credential group model multipliers for channel %s: %w", channel, err)
		}
		if _, err := s.GetWriteDB().ExecContext(ctx, `
			UPDATE endpoints
			SET credential_group_id = ?
			WHERE channel = ? AND (credential_group_id IS NULL OR credential_group_id = 0)
		`, groupID, channel); err != nil {
			return fmt.Errorf("failed to backfill endpoints credential_group_id for channel %s: %w", channel, err)
		}
	}
	return nil
}

func (s *SQLiteAdapter) ensureDefaultClaudeCredentialGroup(ctx context.Context, channel string) (int64, error) {
	var groupID int64
	err := s.GetWriteDB().QueryRowContext(ctx, `
		SELECT id FROM claude_credential_groups
		WHERE channel = ? AND is_default = 1
		LIMIT 1
	`, channel).Scan(&groupID)
	if err == nil {
		return groupID, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query default Claude credential group: %w", err)
	}

	var (
		token                       string
		apiKey                      string
		costMultiplier              float64
		inputCostMultiplier         float64
		outputCostMultiplier        float64
		cacheCreationCostMultiplier float64
		cacheCreationMultiplier1h   float64
		cacheReadCostMultiplier     float64
		modelMultipliers            string
	)
	err = s.GetWriteDB().QueryRowContext(ctx, `
		SELECT COALESCE(token, ''), COALESCE(api_key, ''),
			COALESCE(cost_multiplier, 1.0), COALESCE(input_cost_multiplier, 1.0),
			COALESCE(output_cost_multiplier, 1.0), COALESCE(cache_creation_cost_multiplier, 1.0),
			COALESCE(cache_creation_cost_multiplier_1h, 1.0), COALESCE(cache_read_cost_multiplier, 1.0),
			COALESCE(model_multipliers, '[]')
		FROM endpoints
		WHERE channel = ?
		ORDER BY COALESCE(priority, 1) ASC, COALESCE(created_at, '') ASC, id ASC
		LIMIT 1
	`, channel).Scan(
		&token,
		&apiKey,
		&costMultiplier,
		&inputCostMultiplier,
		&outputCostMultiplier,
		&cacheCreationCostMultiplier,
		&cacheCreationMultiplier1h,
		&cacheReadCostMultiplier,
		&modelMultipliers,
	)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query endpoint seed for Claude credential group: %w", err)
	}

	result, err := s.GetWriteDB().ExecContext(ctx, `
		INSERT OR IGNORE INTO claude_credential_groups (
			channel, name, token, api_key, is_default, cost_multiplier,
			input_cost_multiplier, output_cost_multiplier,
			cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h,
			cache_read_cost_multiplier, disable_long_context_pricing, model_multipliers
		) VALUES (?, '默认分组', ?, ?, 1, ?, ?, ?, ?, ?, ?, 1, ?)
	`, channel, token, apiKey,
		defaultSQLiteMigrationMultiplier(costMultiplier),
		defaultSQLiteMigrationMultiplier(inputCostMultiplier),
		defaultSQLiteMigrationMultiplier(outputCostMultiplier),
		defaultSQLiteMigrationMultiplier(cacheCreationCostMultiplier),
		defaultSQLiteMigrationMultiplier(cacheCreationMultiplier1h),
		defaultSQLiteMigrationMultiplier(cacheReadCostMultiplier),
		modelMultipliers)
	if err != nil {
		return 0, fmt.Errorf("failed to create default Claude credential group: %w", err)
	}
	if id, err := result.LastInsertId(); err == nil && id > 0 {
		return id, nil
	}

	if err := s.GetWriteDB().QueryRowContext(ctx, `
		SELECT id FROM claude_credential_groups
		WHERE channel = ? AND is_default = 1
		LIMIT 1
	`, channel).Scan(&groupID); err != nil {
		return 0, fmt.Errorf("failed to load default Claude credential group after insert: %w", err)
	}
	return groupID, nil
}

func defaultSQLiteMigrationMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

const schemaVersionRepairClaudeCredentialGroupOwnership = "20260825_repair_claude_credential_group_ownership"

// repairClaudeCredentialGroupOwnership 修复历史迁移残留：endpoints.credential_group_id 指向了其它渠道的凭证分组。
//
// 这种归属是非法状态（EndpointService 的创建/更新路径都会拒绝跨渠道分组），后果是端点游离在所有分组之外：
// UI 按 ListByChannel 渲染分组卡片时碰不到它，「关闭本分组所有端点参与转移」的批量操作漏掉它；
// credentialGroupOrder 又把它排到全部分组之后，于是它总在渠道内端点耗尽后被当作最后一批重试，
// 表现为「参与转移开关关不掉」。
//
// 处理策略：孤儿端点归回本渠道默认分组；若默认分组内已存在同名或同 URL 的端点，说明它只是历史重影，直接删除。
// 每次启动检测，但只在命中非法数据时写入 —— 合法行永远不被触碰，因此不违反「不得覆盖用户显式保存值」的约定。
func (s *SQLiteAdapter) repairClaudeCredentialGroupOwnership(ctx context.Context) error {
	for _, table := range []string{"endpoints", "claude_credential_groups"} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check %s for Claude credential group ownership repair: %w", table, err)
		}
		if !exists {
			return nil
		}
	}

	type orphanEndpoint struct {
		id           int64
		channel      string
		name         string
		url          string
		groupID      int64
		groupChannel string
	}

	rows, err := s.GetReadDB().QueryContext(ctx, `
		SELECT e.id, e.channel, e.name, e.url, e.credential_group_id, g.channel
		FROM endpoints e
		JOIN claude_credential_groups g ON g.id = e.credential_group_id
		WHERE g.channel <> e.channel
		ORDER BY e.id ASC
	`)
	if err != nil {
		return fmt.Errorf("failed to query Claude endpoints with foreign credential groups: %w", err)
	}
	orphans := make([]orphanEndpoint, 0)
	for rows.Next() {
		var orphan orphanEndpoint
		if err := rows.Scan(&orphan.id, &orphan.channel, &orphan.name, &orphan.url, &orphan.groupID, &orphan.groupChannel); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan Claude endpoint with foreign credential group: %w", err)
		}
		orphans = append(orphans, orphan)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to iterate Claude endpoints with foreign credential groups: %w", err)
	}
	rows.Close()

	if len(orphans) == 0 {
		return nil
	}

	s.logger.Warn(fmt.Sprintf("🔧 [数据库迁移] 检测到 %d 个 Claude 端点的凭证分组归属跨渠道错配，开始修复", len(orphans)))
	backupPath, err := s.backupDatabaseFile(ctx, "pre-credential-group-repair")
	if err != nil {
		return fmt.Errorf("failed to back up database before Claude credential group ownership repair: %w", err)
	}
	if backupPath != "" {
		s.logger.Info(fmt.Sprintf("💾 [数据库迁移] 修复前已备份数据库: %s", backupPath))
	}

	var repaired, removed, skipped int
	for _, orphan := range orphans {
		var defaultGroupID int64
		err := s.GetReadDB().QueryRowContext(ctx, `
			SELECT id FROM claude_credential_groups
			WHERE channel = ? AND is_default = 1
			LIMIT 1
		`, orphan.channel).Scan(&defaultGroupID)
		if err == sql.ErrNoRows {
			skipped++
			s.logger.Warn(fmt.Sprintf("⚠️ [数据库迁移] 渠道 %s 没有默认凭证分组，端点 %s (id=%d) 的错配归属保持原样",
				orphan.channel, orphan.name, orphan.id))
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to load default Claude credential group for channel %s: %w", orphan.channel, err)
		}

		var conflictID int64
		var conflictName string
		err = s.GetReadDB().QueryRowContext(ctx, `
			SELECT id, name FROM endpoints
			WHERE channel = ? AND credential_group_id = ? AND (name = ? OR url = ?)
			LIMIT 1
		`, orphan.channel, defaultGroupID, orphan.name, orphan.url).Scan(&conflictID, &conflictName)
		switch {
		case err == nil:
			// 默认分组里已经有同名或同上游地址的端点，孤儿只是历史重影，删除它保留正主。
			if _, execErr := s.GetWriteDB().ExecContext(ctx, `DELETE FROM endpoints WHERE id = ?`, orphan.id); execErr != nil {
				return fmt.Errorf("failed to delete duplicated Claude endpoint %s (id=%d): %w", orphan.name, orphan.id, execErr)
			}
			removed++
			s.logger.Info(fmt.Sprintf("🗑️ [数据库迁移] 删除重影端点: 渠道=%s 端点=%s (id=%d, url=%s, 原分组=%d 属于 %s)，默认分组内已有 %s (id=%d)",
				orphan.channel, orphan.name, orphan.id, orphan.url, orphan.groupID, orphan.groupChannel, conflictName, conflictID))
		case err == sql.ErrNoRows:
			if _, execErr := s.GetWriteDB().ExecContext(ctx,
				`UPDATE endpoints SET credential_group_id = ? WHERE id = ?`, defaultGroupID, orphan.id); execErr != nil {
				return fmt.Errorf("failed to reassign Claude endpoint %s (id=%d) to its default credential group: %w", orphan.name, orphan.id, execErr)
			}
			repaired++
			s.logger.Info(fmt.Sprintf("🔁 [数据库迁移] 端点归位默认分组: 渠道=%s 端点=%s (id=%d)，分组 %d (属于 %s) → %d",
				orphan.channel, orphan.name, orphan.id, orphan.groupID, orphan.groupChannel, defaultGroupID))
		default:
			return fmt.Errorf("failed to check duplicated Claude endpoint %s in channel %s: %w", orphan.name, orphan.channel, err)
		}
	}

	s.logger.Info(fmt.Sprintf("✅ [数据库迁移] Claude 凭证分组归属修复完成：归位 %d 个，删除重影 %d 个，跳过 %d 个",
		repaired, removed, skipped))

	if err := s.recordSchemaVersion(ctx, schemaVersionRepairClaudeCredentialGroupOwnership,
		"repair Claude endpoints whose credential group belongs to another channel"); err != nil {
		return err
	}
	return nil
}
