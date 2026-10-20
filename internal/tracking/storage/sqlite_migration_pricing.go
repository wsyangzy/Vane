package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

const longContextPricingDefaultMigrationVersion = "20260712_default_disable_long_context_pricing"

func (s *SQLiteAdapter) migrateLongContextPricingDefault(ctx context.Context) error {
	applied, err := s.isSchemaVersionApplied(ctx, longContextPricingDefaultMigrationVersion)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	for _, table := range []string{"claude_credential_groups", "codex_credential_groups"} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check %s for Long context default migration: %w", table, err)
		}
		if !exists {
			continue
		}
		hasColumn, err := s.columnExists(ctx, table, "disable_long_context_pricing")
		if err != nil {
			return fmt.Errorf("failed to check %s.disable_long_context_pricing: %w", table, err)
		}
		if !hasColumn {
			continue
		}
		if _, err := s.GetWriteDB().ExecContext(ctx, "UPDATE "+table+" SET disable_long_context_pricing = 1"); err != nil {
			return fmt.Errorf("failed to disable Long context pricing for legacy %s rows: %w", table, err)
		}
	}

	return s.recordSchemaVersion(ctx, longContextPricingDefaultMigrationVersion, "凭证分组 Long context 计费默认关闭")
}

const claudeLongContextRemovalMigrationVersion = "20260821_remove_claude_long_context_pricing"

// migrateClaudeLongContextRemoval 强制关闭全部 Claude 凭证分组的 Long context 计费。
//
// Claude 侧已移除该功能的配置入口，但 20260712 那次「默认关闭」迁移是一次性的，
// 在它之后被用户手动开启过的分组仍为 0。若不补这一刀，前端看不到开关、
// tracker.go:557 的闸门却仍然打开，会形成看不见却在生效的隐蔽计费。
// 仅处理 claude_credential_groups；Codex 仍保留该能力，不得波及。
func (s *SQLiteAdapter) migrateClaudeLongContextRemoval(ctx context.Context) error {
	applied, err := s.isSchemaVersionApplied(ctx, claudeLongContextRemovalMigrationVersion)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	exists, err := s.tableExists(ctx, "claude_credential_groups")
	if err != nil {
		return fmt.Errorf("failed to check claude_credential_groups for Long context removal: %w", err)
	}
	if exists {
		hasColumn, err := s.columnExists(ctx, "claude_credential_groups", "disable_long_context_pricing")
		if err != nil {
			return fmt.Errorf("failed to check claude_credential_groups.disable_long_context_pricing: %w", err)
		}
		if hasColumn {
			if _, err := s.GetWriteDB().ExecContext(ctx,
				"UPDATE claude_credential_groups SET disable_long_context_pricing = 1 WHERE disable_long_context_pricing != 1"); err != nil {
				return fmt.Errorf("failed to disable Long context pricing for Claude credential groups: %w", err)
			}
		}
	}

	return s.recordSchemaVersion(ctx, claudeLongContextRemovalMigrationVersion, "Claude 凭证分组移除 Long context 计费")
}

// modelPricingNeedsProviderTierRebuild 检测 model_pricing 是否仍是「model_name 单列唯一」
// 的老结构。老结构有两处硬伤：
//   - Claude 与 Codex 无法配置同名模型的不同定价，而两侧模型名空间天然会重叠
//     （同一中转商常同时提供两种协议；Claude provider 下已存在 MiniMax-M2 这类非 Anthropic 模型）
//   - 无法为同一模型配置多个服务档位的定价行
func (s *SQLiteAdapter) modelPricingNeedsProviderTierRebuild(ctx context.Context) (bool, error) {
	// 见 detectEndpointsUniqueIndexes 的说明：SQLite 连接池限制为 1 条连接，
	// 必须先把索引名读完再逐个查 index_info，不能在遍历 rows 时嵌套查询。
	rows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_list(model_pricing)")
	if err != nil {
		return false, fmt.Errorf("failed to query model_pricing indexes: %w", err)
	}
	uniqueIndexes := make([]string, 0, 4)
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
			return false, fmt.Errorf("failed to scan model_pricing index: %w", err)
		}
		if unique == 1 {
			uniqueIndexes = append(uniqueIndexes, name)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("failed to iterate model_pricing index_list: %w", err)
	}
	rows.Close()

	for _, name := range uniqueIndexes {
		escaped := strings.ReplaceAll(name, "'", "''")
		colRows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_info('"+escaped+"')")
		if err != nil {
			return false, fmt.Errorf("failed to query model_pricing index_info(%s): %w", name, err)
		}
		cols := make([]string, 0, 3)
		for colRows.Next() {
			var (
				seqno   int
				cid     int
				colName sql.NullString
			)
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				colRows.Close()
				return false, fmt.Errorf("failed to scan model_pricing index_info(%s): %w", name, err)
			}
			cols = append(cols, colName.String)
		}
		colRows.Close()

		if len(cols) == 3 && cols[0] == "provider" && cols[1] == "model_name" && cols[2] == "service_tier" {
			return false, nil
		}
	}
	return true, nil
}

// rebuildModelPricingForProviderTierUnique 把 model_pricing 的唯一约束从 model_name
// 单列重建为 (provider, model_name, service_tier) 三元组。
//
// service_tier 为空串的行是标准档/兜底行，现有数据全部落入该行，因此是零数据迁移。
// SQLite 无法直接删除列上的 UNIQUE 约束，只能重建表。
func (s *SQLiteAdapter) rebuildModelPricingForProviderTierUnique(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "model_pricing")
	if err != nil {
		return fmt.Errorf("failed to check model_pricing before rebuild: %w", err)
	}
	if !exists {
		return nil
	}

	needs, err := s.modelPricingNeedsProviderTierRebuild(ctx)
	if err != nil {
		return err
	}
	if !needs {
		return nil
	}

	// service_tier 列由早期迁移引入，之后从 schema.sql 的建表语句里被移除，
	// 因此老库可能有该列（值全为空）也可能没有，复制数据时要分别处理。
	hasServiceTier, err := s.columnExists(ctx, "model_pricing", "service_tier")
	if err != nil {
		return fmt.Errorf("failed to check model_pricing.service_tier: %w", err)
	}
	serviceTierExpr := "''"
	if hasServiceTier {
		serviceTierExpr = "COALESCE(service_tier, '')"
	}

	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin model_pricing rebuild tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`DROP TRIGGER IF EXISTS update_model_pricing_timestamp`,
		`DROP INDEX IF EXISTS idx_model_pricing_model_name`,
		`DROP INDEX IF EXISTS idx_model_pricing_is_default`,
		`DROP INDEX IF EXISTS idx_model_pricing_provider`,
		`ALTER TABLE model_pricing RENAME TO model_pricing_old`,
		// 与 schema.sql 保持一致
		`CREATE TABLE model_pricing (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_name TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT 'claude',
			service_tier TEXT NOT NULL DEFAULT '',
			input_price REAL NOT NULL DEFAULT 3.0,
			output_price REAL NOT NULL DEFAULT 15.0,
			cache_creation_price_5m REAL DEFAULT 3.75,
			cache_creation_price_1h REAL DEFAULT 6.0,
			cache_read_price REAL DEFAULT 0.30,
			long_context_threshold INTEGER NOT NULL DEFAULT 0,
			long_context_input_price REAL NOT NULL DEFAULT 0,
			long_context_cache_read_price REAL NOT NULL DEFAULT 0,
			long_context_cache_write_price REAL NOT NULL DEFAULT 0,
			long_context_output_price REAL NOT NULL DEFAULT 0,
			display_name TEXT,
			description TEXT,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			UNIQUE(provider, model_name, service_tier)
		)`,
		`INSERT INTO model_pricing (
			id, model_name, provider, service_tier,
			input_price, output_price, cache_creation_price_5m, cache_creation_price_1h, cache_read_price,
			long_context_threshold, long_context_input_price, long_context_cache_read_price,
			long_context_cache_write_price, long_context_output_price,
			display_name, description, is_default, created_at, updated_at
		)
		SELECT
			id, model_name, COALESCE(provider, 'claude'), ` + serviceTierExpr + `,
			input_price, output_price, cache_creation_price_5m, cache_creation_price_1h, cache_read_price,
			long_context_threshold, long_context_input_price, long_context_cache_read_price,
			long_context_cache_write_price, long_context_output_price,
			display_name, description, is_default, created_at, updated_at
		FROM model_pricing_old`,
		`DROP TABLE model_pricing_old`,
		`CREATE INDEX IF NOT EXISTS idx_model_pricing_model_name ON model_pricing(model_name)`,
		`CREATE INDEX IF NOT EXISTS idx_model_pricing_is_default ON model_pricing(is_default)`,
		`CREATE INDEX IF NOT EXISTS idx_model_pricing_provider ON model_pricing(provider)`,
		`CREATE TRIGGER IF NOT EXISTS update_model_pricing_timestamp
			AFTER UPDATE ON model_pricing
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
		BEGIN
			UPDATE model_pricing SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
		END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("failed to rebuild model_pricing: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit model_pricing rebuild: %w", err)
	}

	s.logger.Info("✅ [数据库迁移] model_pricing：唯一约束已重建为 (provider, model_name, service_tier)")
	return nil
}
