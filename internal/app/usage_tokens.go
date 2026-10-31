package app

import (
	"context"
	"fmt"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func (a *App) GetTokenUsage(provider string) (TokenUsageData, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	a.mu.RUnlock()

	if usageTracker == nil {
		return TokenUsageData{}, nil
	}

	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		if parsedLoc, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = parsedLoc
		}
	}
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tomorrowStart := todayStart.Add(24 * time.Hour)
	a.ensureCacheRegistry()
	cacheKey := fmt.Sprintf("provider=%s|date=%s|tz=%s", provider, todayStart.Format("2006-01-02"), loc.String())
	scope := appcache.Scope{Provider: provider, Start: todayStart, End: tomorrowStart}
	data, err := a.tokenUsageCache.GetOrLoadScoped(cacheKey, scope, func() (TokenUsageData, error) {
		return queryTokenUsageForRange(usageTracker, todayStart, tomorrowStart, provider)
	})
	if err != nil {
		return TokenUsageData{}, err
	}
	return data, nil
}

func (a *App) GetTokenUsageHeatmap(params RequestQueryParams) ([]TokenUsageHeatmapDay, error) {
	return a.getLocalTokenUsageHeatmap(params.Provider, params.Model)
}

func queryTokenUsageForRange(usageTracker *tracking.UsageTracker, startTime, endTime time.Time, provider string) (TokenUsageData, error) {
	if usageTracker == nil {
		return TokenUsageData{}, fmt.Errorf("usage tracker is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	totals, err := usageTracker.QueryTokenUsageTotals(ctx, startTime, endTime, provider)
	if err != nil {
		return TokenUsageData{}, err
	}
	return TokenUsageData{
		InputTokens:         totals.InputTokens,
		OutputTokens:        totals.OutputTokens,
		CacheCreationTokens: totals.CacheCreationTokens,
		CacheReadTokens:     totals.CacheReadTokens,
		TotalTokens:         totals.TotalTokens,
	}, nil
}
