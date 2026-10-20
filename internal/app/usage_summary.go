package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
	"github.com/wsyangzy/Vane/internal/platform/display"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func (a *App) GetCredentialGroupDailyUsage(provider, channel, groupName string, limitUSD float64) (CredentialGroupDailyUsage, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	channel = strings.TrimSpace(channel)
	groupName = strings.TrimSpace(groupName)
	if provider != "claude" && provider != "codex" {
		return CredentialGroupDailyUsage{}, fmt.Errorf("不支持的 provider: %s", provider)
	}
	if channel == "" || groupName == "" {
		return CredentialGroupDailyUsage{}, fmt.Errorf("渠道和凭证分组不能为空")
	}
	if limitUSD < 0 {
		return CredentialGroupDailyUsage{}, fmt.Errorf("每日成本限额不能小于 0")
	}

	a.mu.RLock()
	usageTracker := a.usageTracker
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	usedUSD, resetAt, err := usageTracker.CredentialGroupDailyCost(ctx, provider, channel, groupName)
	if err != nil {
		return CredentialGroupDailyUsage{}, fmt.Errorf("查询凭证分组当日成本失败: %w", err)
	}
	remainingUSD := 0.0
	if limitUSD > 0 {
		remainingUSD = math.Max(limitUSD-usedUSD, 0)
	}
	return CredentialGroupDailyUsage{
		Provider:     provider,
		Channel:      channel,
		GroupName:    groupName,
		UsedUSD:      usedUSD,
		LimitUSD:     limitUSD,
		RemainingUSD: remainingUSD,
		Reached:      limitUSD > 0 && usedUSD >= limitUSD,
		ResetAt:      resetAt.Format(time.RFC3339),
	}, nil
}

func (a *App) GetUsageSummary(startTimeStr, endTimeStr, provider string) (UsageSummary, error) {
	a.mu.RLock()
	monitoringMiddleware := a.monitoringMiddleware
	usageTracker := a.usageTracker
	cfg := a.config
	logger := a.logger
	a.mu.RUnlock()

	// 如果没有传递时间参数，返回运行时统计（与 Web 版本 /api/v1/connections 一致）
	if startTimeStr == "" && endTimeStr == "" {
		a.ensureCacheRegistry()
		if monitoringMiddleware == nil {
			return UsageSummary{}, nil
		}

		// 从内存中获取运行时统计
		metrics := monitoringMiddleware.GetMetrics()
		stats := metrics.GetMetrics()

		// 计算总 Token
		totalInputTokens := stats.TotalTokenUsage.InputTokens
		totalOutputTokens := stats.TotalTokenUsage.OutputTokens
		totalTokens := stats.TotalTokenUsage.InputTokens + stats.TotalTokenUsage.OutputTokens +
			stats.TotalTokenUsage.CacheCreationTokens + stats.TotalTokenUsage.CacheReadTokens
		activeConnections := int64(len(stats.ActiveConnections))
		avgResponseTime := display.FormatResponseTime(stats.GetAverageResponseTime())

		// 查询数据库获取全部历史和今日统计
		var (
			allTimeTotalCost   float64
			allTimeTotalTokens int64
			allTimeTotal       int64
			todayCost          float64
			todayTokens        int64
			todayRequests      int64
		)

		if usageTracker != nil {
			loc := time.Local
			if cfg != nil && cfg.Timezone != "" {
				if parsedLoc, err := time.LoadLocation(cfg.Timezone); err == nil {
					loc = parsedLoc
				}
			}
			now := time.Now().In(loc)
			todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			todayEnd := todayStart.Add(24 * time.Hour)
			cacheKey := fmt.Sprintf("runtime|provider=%s|today=%s|tz=%s", provider, todayStart.Format("2006-01-02"), loc.String())

			// 含全部历史聚合，任意时间的写入都会影响，只按 provider 区分失效
			snapshot, err := a.usageSummaryCache.GetOrLoadScoped(cacheKey, appcache.Scope{Provider: provider}, func() (usageSummaryDBSnapshot, error) {
				ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
				allTimeCost, allTimeTokens, allTimeRequests, err := queryStatsFromDBStrict(ctx, logger, usageTracker, time.Time{}, time.Now(), provider)
				cancel()
				if err != nil {
					return usageSummaryDBSnapshot{}, err
				}

				ctx, cancel = context.WithTimeout(context.Background(), usageDBQueryTimeout)
				dayCost, dayTokens, dayRequests, err := queryStatsFromDBStrict(ctx, logger, usageTracker, todayStart, todayEnd, provider)
				cancel()
				if err != nil {
					return usageSummaryDBSnapshot{}, err
				}
				return usageSummaryDBSnapshot{
					allTimeTotalCost:   allTimeCost,
					allTimeTotalTokens: allTimeTokens,
					allTimeTotal:       allTimeRequests,
					todayCost:          dayCost,
					todayTokens:        dayTokens,
					todayRequests:      dayRequests,
				}, nil
			})
			if err != nil {
				return UsageSummary{}, fmt.Errorf("查询历史使用统计失败: %w", err)
			}
			allTimeTotalCost = snapshot.allTimeTotalCost
			allTimeTotalTokens = snapshot.allTimeTotalTokens
			allTimeTotal = snapshot.allTimeTotal
			todayCost = snapshot.todayCost
			todayTokens = snapshot.todayTokens
			todayRequests = snapshot.todayRequests
		}

		return UsageSummary{
			TotalRequests:        stats.TotalRequests,
			AllTimeTotalRequests: allTimeTotal,
			TodayRequests:        todayRequests,
			SuccessRequests:      stats.SuccessfulRequests,
			FailedRequests:       stats.FailedRequests,
			ActiveConnections:    activeConnections,
			AverageResponseTime:  avgResponseTime,
			TotalTokens:          totalTokens,
			TotalInputTokens:     totalInputTokens,
			TotalOutputTokens:    totalOutputTokens,
			TotalCost:            0, // 运行时统计不计算成本
			TodayCost:            todayCost,
			AllTimeTotalCost:     allTimeTotalCost,
			TodayTokens:          todayTokens,
			AllTimeTotalTokens:   allTimeTotalTokens,
		}, nil
	}

	// 传递了时间参数，从数据库查询历史数据
	if usageTracker == nil {
		return UsageSummary{}, nil
	}

	// 解析时间范围
	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			startTime = time.Now().AddDate(0, 0, -7) // 默认最近7天
		}
	} else {
		startTime = time.Now().AddDate(0, 0, -7)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			endTime = time.Now()
		}
	} else {
		endTime = time.Now()
	}

	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	summaries, err := usageTracker.QueryUsageSummary(ctx, &tracking.QueryOptions{
		StartDate: &startTime,
		EndDate:   &endTime,
		Provider:  provider,
	})
	if err != nil {
		return UsageSummary{}, err
	}

	// 聚合所有摘要
	result := UsageSummary{}
	for _, s := range summaries {
		result.TotalRequests += int64(s.RequestCount)
		result.SuccessRequests += int64(s.SuccessCount)
		result.FailedRequests += int64(s.ErrorCount)
		result.TotalInputTokens += s.TotalInputTokens
		result.TotalOutputTokens += s.TotalOutputTokens
		result.TotalCost += s.TotalCostUSD
	}
	return result, nil
}

func queryStatsFromDB(ctx context.Context, logger *slog.Logger, usageTracker *tracking.UsageTracker, startTime, endTime time.Time, provider string) (cost float64, tokens int64, requests int64) {
	cost, tokens, requests, _ = queryStatsFromDBStrict(ctx, logger, usageTracker, startTime, endTime, provider)
	return cost, tokens, requests
}

func queryStatsFromDBStrict(ctx context.Context, logger *slog.Logger, usageTracker *tracking.UsageTracker, startTime, endTime time.Time, provider string) (cost float64, tokens int64, requests int64, err error) {
	if usageTracker == nil {
		return 0, 0, 0, fmt.Errorf("usage tracker is nil")
	}
	opts := &tracking.QueryOptions{Provider: provider}
	if !startTime.IsZero() {
		opts.StartDate = &startTime
		opts.EndDate = &endTime
	}
	totals, err := usageTracker.QueryUsageStatsTotals(ctx, opts)
	if err != nil {
		if logger != nil {
			logger.Debug("查询统计数据失败", "error", err)
		}
		return 0, 0, 0, err
	}
	return totals.TotalCostUSD, totals.TotalTokens, totals.TotalRequests, nil
}
