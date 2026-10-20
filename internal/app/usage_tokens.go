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
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	a.mu.RUnlock()

	if usageTracker == nil {
		return []TokenUsageHeatmapDay{}, nil
	}

	timezone := ""
	if cfg != nil {
		timezone = cfg.Timezone
	}
	data, err := queryTokenUsageHeatmap(usageTracker, buildRequestQueryOptions(params, timezone, 0, 0))
	if err != nil {
		return nil, err
	}
	return data, nil
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

func queryTokenUsageHeatmap(usageTracker *tracking.UsageTracker, opts *tracking.QueryOptions) ([]TokenUsageHeatmapDay, error) {
	if usageTracker == nil {
		return nil, fmt.Errorf("usage tracker is nil")
	}
	if opts == nil || opts.StartDate == nil || opts.EndDate == nil {
		return []TokenUsageHeatmapDay{}, nil
	}
	startTime := *opts.StartDate
	endTime := *opts.EndDate
	if endTime.Before(startTime) {
		return []TokenUsageHeatmapDay{}, nil
	}

	bucket := "day"
	if endTime.Sub(startTime) <= tokenUsageHeatmapHourlyMaxRange {
		bucket = "hour"
	}

	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	buckets, err := usageTracker.QueryTokenUsageBuckets(ctx, opts, bucket)
	if err != nil {
		return nil, err
	}

	byBucket := make(map[string]TokenUsageHeatmapDay)
	var maxTokens int64
	for _, value := range buckets {
		item := TokenUsageHeatmapDay{
			Date:                value.Key,
			RequestCount:        value.RequestCount,
			InputTokens:         value.InputTokens,
			OutputTokens:        value.OutputTokens,
			CacheCreationTokens: value.CacheCreationTokens,
			CacheReadTokens:     value.CacheReadTokens,
			TotalTokens:         value.TotalTokens,
		}
		item.Bucket = bucket
		if item.TotalTokens > maxTokens {
			maxTokens = item.TotalTokens
		}
		byBucket[item.Date] = item
	}

	if bucket == "hour" {
		rangeStart := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), startTime.Hour(), 0, 0, 0, startTime.Location())
		rangeEnd := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), endTime.Hour(), 0, 0, 0, endTime.Location()).Add(time.Hour)
		hours := make([]TokenUsageHeatmapDay, 0, int(rangeEnd.Sub(rangeStart).Hours())+1)
		for cursor := rangeStart; cursor.Before(rangeEnd); cursor = cursor.Add(time.Hour) {
			key := cursor.Format("2006-01-02 15")
			item := byBucket[key]
			item.Date = key
			item.Bucket = bucket
			if maxTokens > 0 && item.TotalTokens > 0 {
				item.Intensity = float64(item.TotalTokens) / float64(maxTokens)
			}
			hours = append(hours, item)
		}
		return hours, nil
	}

	rangeStart := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, startTime.Location())
	rangeEnd := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 0, 0, 0, 0, endTime.Location()).AddDate(0, 0, 1)
	days := make([]TokenUsageHeatmapDay, 0, int(rangeEnd.Sub(rangeStart).Hours()/24)+1)
	for cursor := rangeStart; cursor.Before(rangeEnd); cursor = cursor.AddDate(0, 0, 1) {
		date := cursor.Format("2006-01-02")
		day := byBucket[date]
		day.Date = date
		day.Bucket = bucket
		if maxTokens > 0 && day.TotalTokens > 0 {
			day.Intensity = float64(day.TotalTokens) / float64(maxTokens)
		}
		days = append(days, day)
	}
	return days, nil
}
