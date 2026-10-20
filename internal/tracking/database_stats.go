package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GetDatabaseStats 获取数据库统计信息（使用读连接）
func (ut *UsageTracker) getDatabaseStatsInternal(ctx context.Context) (*DatabaseStats, error) {
	if ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	stats := &DatabaseStats{}

	// 获取请求记录总数（使用读连接）
	err := ut.readDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM request_logs WHERE COALESCE(environment, 'release') = ?", ut.environment).Scan(&stats.TotalRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to get total requests count: %w", err)
	}

	// 获取汇总记录总数（使用读连接）
	err = ut.readDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM usage_summary WHERE COALESCE(environment, 'release') = ?", ut.environment).Scan(&stats.TotalSummaries)
	if err != nil {
		return nil, fmt.Errorf("failed to get total summaries count: %w", err)
	}

	// 获取最早和最新的记录时间（使用读连接）
	var earliestStr, latestStr sql.NullString
	err = ut.readDB.QueryRowContext(ctx,
		"SELECT MIN(start_time), MAX(start_time) FROM request_logs WHERE COALESCE(environment, 'release') = ?",
		ut.environment).Scan(&earliestStr, &latestStr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get record time range: %w", err)
	}

	if earliestStr.Valid {
		if t, err := time.Parse(time.RFC3339, earliestStr.String); err == nil {
			stats.EarliestRecord = &t
		}
	}
	if latestStr.Valid {
		if t, err := time.Parse(time.RFC3339, latestStr.String); err == nil {
			stats.LatestRecord = &t
		}
	}

	// 获取 SQLite 数据库文件大小（使用读连接）
	var pageCount, pageSize int64
	err = ut.readDB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)
	if err == nil {
		err = ut.readDB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize)
		if err == nil {
			stats.DatabaseSize = pageCount * pageSize
		}
	}

	// 获取总成本（使用读连接）
	err = ut.readDB.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(total_cost_usd), 0) FROM request_logs WHERE total_cost_usd > 0 AND COALESCE(environment, 'release') = ?",
		ut.environment).Scan(&stats.TotalCostUSD)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get total cost: %w", err)
	}

	return stats, nil
}
