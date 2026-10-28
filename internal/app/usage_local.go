package app

import (
	"context"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

func (a *App) usageReader() *sessionusage.Reader {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.localUsageReader == nil {
		a.localUsageReader = sessionusage.NewReader()
	}
	return a.localUsageReader
}

// The heatmap reads native session metadata without importing local calls into
// the proxy's request ledger. Its public Wails contract stays unchanged.
func (a *App) getLocalTokenUsageHeatmap(provider, model string) ([]TokenUsageHeatmapDay, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()
	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		if parsed, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = parsed
		}
	}
	roots, err := sessionusage.NativeRoots()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return queryLocalHeatmap(ctx, a.usageReader(), roots, time.Now().In(loc), provider, model)
}

func queryLocalHeatmap(ctx context.Context, reader *sessionusage.Reader, roots sessionusage.Roots, now time.Time, provider, model string) ([]TokenUsageHeatmapDay, error) {
	start, end := heatmapCalendarRange(now)
	calls, err := reader.Query(ctx, roots, start, end, provider, model)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]TokenUsageHeatmapDay)
	var peak int64
	for _, call := range calls {
		date := call.At.In(now.Location()).Format(time.DateOnly)
		day := byDate[date]
		day.RequestCount++
		day.InputTokens += call.Input
		day.OutputTokens += call.Output
		day.CacheReadTokens += call.CacheRead
		day.CacheCreationTokens += call.CacheWrite
		day.TotalTokens += call.Total()
		byDate[date] = day
		peak = max(peak, day.TotalTokens)
	}
	result := make([]TokenUsageHeatmapDay, 0, 371)
	for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 0, 1) {
		date := cursor.Format(time.DateOnly)
		day := byDate[date]
		day.Date, day.Bucket = date, "day"
		if peak > 0 {
			day.Intensity = float64(day.TotalTokens) / float64(peak)
		}
		result = append(result, day)
	}
	return result, nil
}

func heatmapCalendarRange(now time.Time) (time.Time, time.Time) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return today.AddDate(0, 0, -(int(today.Weekday())+6)%7-52*7), today.AddDate(0, 0, 1)
}
