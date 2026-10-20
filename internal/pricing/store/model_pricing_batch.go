package store

import (
	"context"
	"fmt"

	"github.com/wsyangzy/Vane/internal/platform/sqlite"
)

// BatchCreate 批量创建模型定价
func (s *SQLiteModelPricingStore) BatchCreate(ctx context.Context, records []*ModelPricingRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(records) == 0 {
		return nil
	}

	// 使用事务
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO model_pricing (
			model_name, provider, service_tier, input_price, output_price,
			cache_creation_price_5m, cache_creation_price_1h, cache_read_price,
			long_context_threshold, long_context_input_price,
			long_context_cache_read_price, long_context_cache_write_price,
			long_context_output_price,
			display_name, description, is_default,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			strftime('%Y-%m-%d %H:%M:%S', 'now', 'localtime'),
			strftime('%Y-%m-%d %H:%M:%S', 'now', 'localtime'))
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	for _, record := range records {
		// 设置默认值
		if record.Provider == "" {
			record.Provider = "claude"
		}
		if record.InputPrice == 0 {
			record.InputPrice = 3.0
		}
		if record.OutputPrice == 0 {
			record.OutputPrice = 15.0
		}
		if record.CacheCreationPrice5m == 0 && record.Provider == "claude" {
			record.CacheCreationPrice5m = record.InputPrice * 1.25
		}
		if record.CacheCreationPrice1h == 0 && record.Provider == "claude" {
			record.CacheCreationPrice1h = record.InputPrice * 2.0
		}
		if record.CacheReadPrice == 0 && record.Provider == "claude" {
			record.CacheReadPrice = record.InputPrice * 0.1
		}

		_, err = stmt.ExecContext(ctx,
			record.ModelName, record.Provider, record.ServiceTier, record.InputPrice, record.OutputPrice,
			record.CacheCreationPrice5m, record.CacheCreationPrice1h, record.CacheReadPrice,
			record.LongContextThreshold, record.LongContextInputPrice,
			record.LongContextCacheReadPrice, record.LongContextCacheWritePrice,
			record.LongContextOutputPrice,
			record.DisplayName, record.Description, sqlite.BoolToInt(record.IsDefault),
		)
		if err != nil {
			return fmt.Errorf("插入模型定价 %s 失败: %w", record.ModelName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// BatchUpsert 批量创建或更新模型定价
func (s *SQLiteModelPricingStore) BatchUpsert(ctx context.Context, records []*ModelPricingRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(records) == 0 {
		return nil
	}

	// 使用事务
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO model_pricing (
			model_name, provider, service_tier, input_price, output_price,
			cache_creation_price_5m, cache_creation_price_1h, cache_read_price,
			long_context_threshold, long_context_input_price,
			long_context_cache_read_price, long_context_cache_write_price,
			long_context_output_price,
			display_name, description, is_default
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, model_name, service_tier) DO UPDATE SET
			provider = excluded.provider,
			input_price = excluded.input_price,
			output_price = excluded.output_price,
			cache_creation_price_5m = excluded.cache_creation_price_5m,
			cache_creation_price_1h = excluded.cache_creation_price_1h,
			cache_read_price = excluded.cache_read_price,
			long_context_threshold = excluded.long_context_threshold,
			long_context_input_price = excluded.long_context_input_price,
			long_context_cache_read_price = excluded.long_context_cache_read_price,
			long_context_cache_write_price = excluded.long_context_cache_write_price,
			long_context_output_price = excluded.long_context_output_price,
			display_name = excluded.display_name,
			description = excluded.description,
			is_default = excluded.is_default
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	for _, record := range records {
		// 设置默认值
		if record.Provider == "" {
			record.Provider = "claude"
		}
		if record.InputPrice == 0 {
			record.InputPrice = 3.0
		}
		if record.OutputPrice == 0 {
			record.OutputPrice = 15.0
		}
		if record.CacheCreationPrice5m == 0 && record.Provider == "claude" {
			record.CacheCreationPrice5m = record.InputPrice * 1.25
		}
		if record.CacheCreationPrice1h == 0 && record.Provider == "claude" {
			record.CacheCreationPrice1h = record.InputPrice * 2.0
		}
		if record.CacheReadPrice == 0 && record.Provider == "claude" {
			record.CacheReadPrice = record.InputPrice * 0.1
		}

		_, err = stmt.ExecContext(ctx,
			record.ModelName, record.Provider, record.ServiceTier, record.InputPrice, record.OutputPrice,
			record.CacheCreationPrice5m, record.CacheCreationPrice1h, record.CacheReadPrice,
			record.LongContextThreshold, record.LongContextInputPrice,
			record.LongContextCacheReadPrice, record.LongContextCacheWritePrice,
			record.LongContextOutputPrice,
			record.DisplayName, record.Description, sqlite.BoolToInt(record.IsDefault),
		)
		if err != nil {
			return fmt.Errorf("upsert 模型定价 %s 失败: %w", record.ModelName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}
