package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wsyangzy/Vane/internal/platform/sqlite"
)

// scanModelPricing 从单行扫描模型定价记录
func (s *SQLiteModelPricingStore) scanModelPricing(row *sql.Row) (*ModelPricingRecord, error) {
	var record ModelPricingRecord
	var provider string
	var displayName, description sql.NullString
	var isDefault int
	var createdAt, updatedAt string

	err := row.Scan(
		&record.ID, &record.ModelName, &provider, &record.ServiceTier,
		&record.InputPrice, &record.OutputPrice,
		&record.CacheCreationPrice5m, &record.CacheCreationPrice1h, &record.CacheReadPrice,
		&record.LongContextThreshold, &record.LongContextInputPrice,
		&record.LongContextCacheReadPrice, &record.LongContextCacheWritePrice,
		&record.LongContextOutputPrice,
		&displayName, &description, &isDefault,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("扫描模型定价记录失败: %w", err)
	}

	// 解析 provider
	record.Provider = provider
	if record.Provider == "" {
		record.Provider = "claude"
	}

	// 解析可空字段
	if displayName.Valid {
		record.DisplayName = displayName.String
	}
	if description.Valid {
		record.Description = description.String
	}

	// 转换布尔值
	record.IsDefault = isDefault == 1

	// 解析时间
	record.CreatedAt = sqlite.ParseDateTime(createdAt)
	record.UpdatedAt = sqlite.ParseDateTime(updatedAt)

	return &record, nil
}

// scanModelPricings 扫描多个模型定价记录
func (s *SQLiteModelPricingStore) scanModelPricings(ctx context.Context, query string, args ...interface{}) ([]*ModelPricingRecord, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if s.tx != nil {
		rows, err = s.tx.QueryContext(ctx, query, args...)
	} else {
		rows, err = sqlite.QueryRowsWithBusyRetry(ctx, func() (*sql.Rows, error) {
			return s.db.QueryContext(ctx, query, args...)
		})
	}
	if err != nil {
		return nil, fmt.Errorf("查询模型定价失败: %w", err)
	}
	defer rows.Close()

	var records []*ModelPricingRecord
	for rows.Next() {
		var record ModelPricingRecord
		var provider string
		var displayName, description sql.NullString
		var isDefault int
		var createdAt, updatedAt string

		err := rows.Scan(
			&record.ID, &record.ModelName, &provider, &record.ServiceTier,
			&record.InputPrice, &record.OutputPrice,
			&record.CacheCreationPrice5m, &record.CacheCreationPrice1h, &record.CacheReadPrice,
			&record.LongContextThreshold, &record.LongContextInputPrice,
			&record.LongContextCacheReadPrice, &record.LongContextCacheWritePrice,
			&record.LongContextOutputPrice,
			&displayName, &description, &isDefault,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描模型定价记录失败: %w", err)
		}

		// 解析 provider
		record.Provider = provider
		if record.Provider == "" {
			record.Provider = "claude"
		}

		// 解析可空字段
		if displayName.Valid {
			record.DisplayName = displayName.String
		}
		if description.Valid {
			record.Description = description.String
		}

		// 转换布尔值
		record.IsDefault = isDefault == 1

		// 解析时间
		record.CreatedAt = sqlite.ParseDateTime(createdAt)
		record.UpdatedAt = sqlite.ParseDateTime(updatedAt)

		records = append(records, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历模型定价记录失败: %w", err)
	}

	return records, nil
}
