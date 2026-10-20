package app

import (
	"context"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking"
)

func (a *App) GetUsageStats(params UsageStatsQueryParams) (UsageStatsData, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	a.mu.RUnlock()

	period := params.Period
	if period == "" {
		period = "30d"
	}

	result := UsageStatsData{
		Period: period,
	}

	// 解析时间参数（使用配置的时区）
	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}

	var startTime, endTime time.Time
	var useCustomRange bool

	// 优先使用自定义时间范围
	if params.StartDate != "" {
		if t, err := parseTimeWithLocation(params.StartDate, loc); err == nil {
			startTime = t
			useCustomRange = true
		}
	}
	if params.EndDate != "" {
		if t, err := parseTimeWithLocation(params.EndDate, loc); err == nil {
			endTime = t
			useCustomRange = true
		}
	}

	// 如果没有自定义时间，使用 period 计算
	if !useCustomRange {
		endTime = time.Now()
		switch period {
		case "1h":
			startTime = endTime.Add(-1 * time.Hour)
		case "1d":
			startTime = endTime.AddDate(0, 0, -1)
		case "7d":
			startTime = endTime.AddDate(0, 0, -7)
		case "30d":
			startTime = endTime.AddDate(0, 0, -30)
		case "90d":
			startTime = endTime.AddDate(0, 0, -90)
		default:
			startTime = endTime.AddDate(0, 0, -30)
			result.Period = "30d"
		}
	}

	// 如果有 usageTracker，从热池+数据库组合查询（与 HTTP API 一致）
	if usageTracker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 使用聚合查询：热池 + 数据库双源统计（避免拉取海量明细导致 UI 卡顿）
		opts := &tracking.QueryOptions{
			StartDate:    &startTime,
			EndDate:      &endTime,
			ModelName:    params.Model,
			Channel:      params.Channel, // 渠道筛选
			EndpointName: params.Endpoint,
			GroupName:    params.Group,
			Status:       params.Status,
			Provider:     params.Provider, // 提供商筛选
			Limit:        0,
			Offset:       0,
		}

		totals, err := usageTracker.QueryUsageStatsTotalsWithHotPool(ctx, opts)
		if err != nil {
			return result, err
		}
		if totals != nil {
			result.TotalRequests = int(totals.TotalRequests)
			result.FailedCount = int(totals.FailedRequests)
			result.TotalTokens = totals.TotalTokens
			result.TotalCostUSD = totals.TotalCostUSD
			result.AvgDurationMs = totals.AvgDurationMs()
			result.CacheHitRate = totals.CacheHitRate()

			if totals.TotalRequests > 0 {
				result.SuccessRate = float64(totals.SuccessRequests) / float64(totals.TotalRequests) * 100
			}

			return result, nil
		}
	}

	// 从运行时 Metrics 获取统计（降级方案，或无 usageTracker 时）
	return a.getUsageStatsFromRuntime(period), nil
}

func (a *App) getUsageStatsFromRuntime(period string) UsageStatsData {
	result := UsageStatsData{
		Period: period,
	}

	if a.monitoringMiddleware == nil {
		return result
	}

	metrics := a.monitoringMiddleware.GetMetrics()
	if metrics == nil {
		return result
	}

	stats := metrics.GetMetrics()

	// 从运行时统计计算
	result.TotalRequests = int(stats.TotalRequests)
	result.FailedCount = int(stats.FailedRequests)

	// 计算成功率
	if stats.TotalRequests > 0 {
		result.SuccessRate = float64(stats.SuccessfulRequests) / float64(stats.TotalRequests) * 100
	}

	// 计算总 Token
	result.TotalTokens = stats.TotalTokenUsage.InputTokens + stats.TotalTokenUsage.OutputTokens +
		stats.TotalTokenUsage.CacheCreationTokens + stats.TotalTokenUsage.CacheReadTokens

	// 计算平均耗时（使用 Metrics 提供的方法）
	avgResponseTime := metrics.GetAverageResponseTime()
	result.AvgDurationMs = float64(avgResponseTime.Milliseconds())

	// 运行时不计算成本
	result.TotalCostUSD = 0

	return result
}
