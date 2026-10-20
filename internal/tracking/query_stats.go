package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// QueryUsageStats queries aggregated usage statistics
func (ut *UsageTracker) QueryUsageStats(ctx context.Context, period string) (*UsageStats, error) {
	// Calculate date range based on period
	endDate := ut.now()
	var startDate time.Time

	switch period {
	case "1d":
		startDate = endDate.AddDate(0, 0, -1)
	case "7d":
		startDate = endDate.AddDate(0, 0, -7)
	case "30d":
		startDate = endDate.AddDate(0, 0, -30)
	case "90d":
		startDate = endDate.AddDate(0, 0, -90)
	default:
		startDate = endDate.AddDate(0, 0, -7) // default to 7 days
	}

	totals, err := ut.QueryUsageStatsTotals(ctx, &QueryOptions{StartDate: &startDate, EndDate: &endDate})
	if err != nil {
		return nil, fmt.Errorf("failed to query usage stats: %w", err)
	}
	stats := UsageStats{
		Period:        period,
		TotalRequests: int(totals.TotalRequests),
		AvgDuration:   totals.AvgDurationMs(),
		TotalCost:     totals.TotalCostUSD,
	}
	if totals.TotalRequests > 0 {
		stats.SuccessRate = float64(totals.SuccessRequests) / float64(totals.TotalRequests) * 100
	}

	// 添加调试日志
	slog.Debug("Usage stats query result",
		"total_requests", stats.TotalRequests,
		"success_rate", stats.SuccessRate,
		"period", period,
		"start_date", startDate,
		"end_date", endDate)

	return &stats, nil
}

// QueryUsageStatsTotals 查询聚合统计（数据库部分）。
// 该方法只做聚合，不返回明细，适用于前端统计卡片/图表的快速刷新。
func (ut *UsageTracker) QueryUsageStatsTotals(ctx context.Context, opts *QueryOptions) (*UsageStatsTotals, error) {
	if ut == nil || ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}
	opts = ut.scopedQueryOptions(opts)

	ut.historyMu.RLock()
	defer ut.historyMu.RUnlock()

	var combined UsageStatsTotals
	active, err := ut.queryUsageStatsTotalsFromSource(ctx, ut.readDB, opts)
	if err != nil {
		return nil, fmt.Errorf("活动数据库查询失败: %w", err)
	}
	combined.add(*active)

	files, err := ut.intersectingArchiveFilesLocked(opts.StartDate, opts.EndDate)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		load := func() (UsageStatsTotals, error) {
			var totals UsageStatsTotals
			err := ut.queryHistoryArchiveFile(ctx, file, func(source historyQuerySource) error {
				result, err := ut.queryUsageStatsTotalsFromSource(ctx, source.db, opts)
				if err != nil {
					return err
				}
				totals = *result
				return nil
			})
			return totals, err
		}
		var totals UsageStatsTotals
		if ut.archiveTotalsCache != nil {
			// 归档文件只在 historyMu 写锁下变更，变更时整表清空；这里持有读锁，
			// 读到的缓存与磁盘上的归档文件一致。命中时不必打开归档文件。
			totals, err = ut.archiveTotalsCache.GetOrLoad(file.month+"|"+ut.archiveScopedCacheKey(file.month, opts), load)
		} else {
			totals, err = load()
		}
		if err != nil {
			return nil, err
		}
		combined.add(totals)
	}
	return &combined, nil
}

// archiveScopedCacheKey 生成归档月份聚合的缓存 key。
// 时间范围完整覆盖该月时不同查询的结果相同，用 "full" 归一，
// 避免 end=time.Now() 这类每次都变的上界让归档月份永远无法命中。
func (ut *UsageTracker) archiveScopedCacheKey(month string, opts *QueryOptions) string {
	monthStart, monthEnd, err := ut.archiveMonthBounds(month)
	covers := err == nil &&
		(opts.StartDate == nil || !opts.StartDate.After(monthStart)) &&
		(opts.EndDate == nil || !opts.EndDate.Before(monthEnd))
	if !covers {
		return queryOptsCacheKey(opts)
	}
	return fmt.Sprintf("full|p=%s|c=%s|m=%s|e=%s|g=%s|s=%s|env=%s",
		opts.Provider, opts.Channel, opts.ModelName, opts.EndpointName,
		opts.GroupName, opts.Status, opts.Environment)
}

func (ut *UsageTracker) queryUsageStatsTotalsFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) (*UsageStatsTotals, error) {
	if db == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	// 历史月度归档可能早于 stream_outcome / cache 列：列缺失时退回旧口径与 0
	hasOutcome, err := requestLogsHasColumn(ctx, db, "stream_outcome")
	if err != nil {
		return nil, err
	}
	successExpr := `CASE WHEN status IN ('completed', 'processing') THEN 1 ELSE 0 END`
	if hasOutcome {
		successExpr = `CASE WHEN ` + successRequestSQLCondition + ` THEN 1 ELSE 0 END`
	}

	query := `SELECT
		COUNT(*) as total_requests,
		COALESCE(SUM(` + successExpr + `), 0) as success_requests,
		COALESCE(SUM(CASE WHEN status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout') THEN 1 ELSE 0 END), 0) as failed_requests,
		COALESCE(SUM(` + totalTokensSQLExpression + `), 0) as total_tokens,
		COALESCE(SUM(total_cost_usd), 0.0) as total_cost_usd,
		COALESCE(SUM(CASE WHEN duration_ms IS NOT NULL AND duration_ms > 0 THEN duration_ms ELSE 0 END), 0) as duration_sum_ms,
		COALESCE(SUM(CASE WHEN duration_ms IS NOT NULL AND duration_ms > 0 THEN 1 ELSE 0 END), 0) as duration_count,
		COALESCE(SUM(` + cacheHitNumeratorSQLExpression + `), 0) as cache_hit_numerator,
		COALESCE(SUM(` + cacheHitDenominatorSQLExpression + `), 0) as cache_hit_denominator
		FROM request_logs WHERE 1=1
			AND COALESCE(environment, 'release') = ?`

	args := []interface{}{queryEnvironment(opts)}

	if opts != nil {
		if opts.StartDate != nil {
			query += " AND start_time >= ?"
			args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
		}
		if opts.EndDate != nil {
			query += " AND start_time <= ?"
			args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
		}
		if opts.ModelName != "" {
			query += " AND model_name = ?"
			args = append(args, opts.ModelName)
		}
		if opts.Channel != "" {
			query += " AND channel = ?"
			args = append(args, opts.Channel)
		}
		if opts.EndpointName != "" {
			query += " AND endpoint_name = ?"
			args = append(args, opts.EndpointName)
		}
		if opts.GroupName != "" {
			query += " AND group_name = ?"
			args = append(args, opts.GroupName)
		}
		if opts.Status != "" {
			// 与 QueryRequestDetails 一致：failed 为兼容集合查询，其余精确匹配。
			switch opts.Status {
			case "failed":
				query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
			default:
				query += " AND status = ?"
				args = append(args, opts.Status)
			}
		}
		if opts.Provider != "" {
			query += " AND provider = ?"
			args = append(args, opts.Provider)
		}
	}

	var totals UsageStatsTotals
	if err := db.QueryRowContext(ctx, query, args...).Scan(
		&totals.TotalRequests,
		&totals.SuccessRequests,
		&totals.FailedRequests,
		&totals.TotalTokens,
		&totals.TotalCostUSD,
		&totals.DurationSumMs,
		&totals.DurationCount,
		&totals.CacheHitNumerator,
		&totals.CacheHitDenominator,
	); err != nil {
		return nil, fmt.Errorf("failed to query usage stats totals: %w", err)
	}

	return &totals, nil
}

// QueryUsageStatsTotalsWithHotPool 双源聚合：热池 + 数据库。
// 相比 QueryRequestDetailsWithHotPool：不拉取大量明细，避免大数据量时拖慢 UI。
func (ut *UsageTracker) QueryUsageStatsTotalsWithHotPool(ctx context.Context, opts *QueryOptions) (*UsageStatsTotals, error) {
	opts = ut.scopedQueryOptions(opts)
	dbTotals, err := ut.queryUsageStatsTotalsCached(ctx, opts)
	if err != nil {
		return nil, err
	}

	// 热池部分：仅遍历活跃请求（通常数量有限），成本可控。
	hotRequests := ut.getFilteredHotPoolRequests(opts)
	if len(hotRequests) == 0 {
		return dbTotals, nil
	}

	var hotTotals UsageStatsTotals
	for _, req := range hotRequests {
		hotTotals.TotalRequests++
		httpStatus := 0
		if req.HTTPStatusCode != nil {
			httpStatus = *req.HTTPStatusCode
		}
		if isSuccessfulOutcome(req.StreamOutcome, req.Status, httpStatus, req.HTTPStatusIsUpstream) {
			hotTotals.SuccessRequests++
		}
		switch req.Status {
		case "failed", "partial_failed", "error", "auth_error", "rate_limited", "server_error", "network_error", "stream_error", "timeout":
			hotTotals.FailedRequests++
		}

		numerator, denominator := cacheHitParts(req.Provider, req.InputTokens, req.CacheCreationTokens, req.CacheReadTokens)
		hotTotals.CacheHitNumerator += numerator
		hotTotals.CacheHitDenominator += denominator
		hotTotals.TotalTokens += DisplayTotalTokens(req.Provider, req.InputTokens, req.OutputTokens, req.CacheCreationTokens, req.CacheReadTokens)
		hotTotals.TotalCostUSD += req.TotalCostUSD

		if req.DurationMs != nil && *req.DurationMs > 0 {
			hotTotals.DurationSumMs += *req.DurationMs
			hotTotals.DurationCount++
		}
	}

	dbTotals.add(hotTotals)
	return dbTotals, nil
}
