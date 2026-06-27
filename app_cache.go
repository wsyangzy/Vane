package main

import (
	"context"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
	"ai-relay-forwarder/internal/service"
)

const (
	cacheNameUsageStats          = "usage:stats"
	cacheNameUsageSummary        = "usage:summary"
	cacheNameUsageHistory        = "usage:history"
	cacheNameTokenUsage          = "usage:token"
	cacheNameEndpointCostToday   = "endpoint-cost:today"
	cacheNameEndpointCostHistory = "endpoint-cost:history"
	cacheNameRequestCount        = "requests:count-fallback"
	cacheNameRuntimePortCheck    = "runtime:port-check"
	cacheNameRuntimeClaudeCLI    = "runtime:claude-cli-version"
	cacheNameRuntimeCodexCLI     = "runtime:codex-cli-version"
)

type usageSummaryDBSnapshot struct {
	allTimeTotalCost   float64
	allTimeTotalTokens int64
	allTimeTotal       int64
	todayCost          float64
	todayTokens        int64
	todayRequests      int64
}

type cacheSettings struct {
	Enabled                bool
	UsageStatsTTL          time.Duration
	UsageSummaryTTL        time.Duration
	TokenUsageTTL          time.Duration
	EndpointCostTodayTTL   time.Duration
	EndpointCostHistoryTTL time.Duration
	MaxEntries             int
	StaleOnError           bool
}

// CacheStatus is returned to the frontend for cache observability.
type CacheStatus struct {
	Enabled bool              `json:"enabled"`
	Caches  []CacheStatusItem `json:"caches"`
}

type CacheStatusItem struct {
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	TTL               string `json:"ttl"`
	MaxEntries        int    `json:"max_entries"`
	Size              int    `json:"size"`
	Hits              uint64 `json:"hits"`
	Misses            uint64 `json:"misses"`
	Expired           uint64 `json:"expired"`
	Evicted           uint64 `json:"evicted"`
	Invalidations     uint64 `json:"invalidations"`
	LastUpdatedAt     string `json:"last_updated_at"`
	LastInvalidatedAt string `json:"last_invalidated_at"`
}

type ClearCacheInput struct {
	Names []string `json:"names"`
}

type CacheClearResult struct {
	ClearedCaches  int      `json:"cleared_caches"`
	ClearedEntries int      `json:"cleared_entries"`
	UnknownNames   []string `json:"unknown_names,omitempty"`
}

type externalCacheAdapter struct {
	name       string
	stats      func() appcache.Stats
	invalidate func() int
}

func (a externalCacheAdapter) Name() string {
	return a.name
}

func (a externalCacheAdapter) Stats() appcache.Stats {
	if a.stats == nil {
		return appcache.Stats{Name: a.name, Enabled: true}
	}
	return a.stats()
}

func (a externalCacheAdapter) Invalidate(options appcache.InvalidateOptions) int {
	if a.invalidate == nil || (!options.All && options.Key == "" && options.Prefix == "") {
		return 0
	}
	return a.invalidate()
}

func (a externalCacheAdapter) SetOptions(appcache.Options) {}

func (a externalCacheAdapter) Options() appcache.Options {
	return appcache.Options{Name: a.name, Enabled: true}
}

func (a *App) setupCacheRegistry() {
	a.cacheRegistry = appcache.NewRegistry()
	settings := defaultCacheSettings()

	a.usageSummaryCache = appcache.New[usageSummaryDBSnapshot](cacheOptions(cacheNameUsageSummary, settings.UsageSummaryTTL, settings))
	a.usageHistoryCache = appcache.New[UsageSummary](cacheOptions(cacheNameUsageHistory, settings.UsageSummaryTTL, settings))
	a.tokenUsageCache = appcache.New[TokenUsageData](cacheOptions(cacheNameTokenUsage, settings.TokenUsageTTL, settings))
	a.endpointCostTodayCache = appcache.New[[]EndpointCostItem](cacheOptions(cacheNameEndpointCostToday, settings.EndpointCostTodayTTL, settings))
	a.endpointCostHistoryCache = appcache.New[[]EndpointCostItem](cacheOptions(cacheNameEndpointCostHistory, settings.EndpointCostHistoryTTL, settings))

	a.cacheRegistry.Register(a.usageSummaryCache)
	a.cacheRegistry.Register(a.usageHistoryCache)
	a.cacheRegistry.Register(a.tokenUsageCache)
	a.cacheRegistry.Register(a.endpointCostTodayCache)
	a.cacheRegistry.Register(a.endpointCostHistoryCache)
	a.registerRuntimeStatusCaches()
}

func (a *App) registerModelPricingCache() {
	if a == nil || a.cacheRegistry == nil || a.modelPricingService == nil {
		return
	}
	service := a.modelPricingService
	a.cacheRegistry.Register(externalCacheAdapter{
		name:       "model-pricing",
		stats:      service.CacheStats,
		invalidate: service.InvalidateCache,
	})
}

func (a *App) registerRuntimeStatusCaches() {
	if a == nil || a.cacheRegistry == nil {
		return
	}
	a.cacheRegistry.Register(externalCacheAdapter{
		name:       cacheNameRuntimePortCheck,
		stats:      portCheckCacheStats,
		invalidate: invalidatePortCheckCache,
	})
	a.cacheRegistry.Register(externalCacheAdapter{
		name:       cacheNameRuntimeClaudeCLI,
		stats:      claudeCodeVersionCacheStats,
		invalidate: invalidateClaudeCodeVersionCache,
	})
	a.cacheRegistry.Register(externalCacheAdapter{
		name:       cacheNameRuntimeCodexCLI,
		stats:      codexVersionCacheStats,
		invalidate: invalidateCodexVersionCache,
	})
}

func (a *App) registerUsageTrackerCaches() {
	if a == nil || a.cacheRegistry == nil || a.usageTracker == nil {
		return
	}
	if usageStatsCache := a.usageTracker.UsageStatsCache(); usageStatsCache != nil {
		a.cacheRegistry.Register(usageStatsCache)
	}
	if countCache := a.usageTracker.DBCountFallbackCache(); countCache != nil {
		a.cacheRegistry.Register(countCache)
	}
}

func (a *App) ensureCacheRegistry() {
	if a.cacheRegistry == nil || a.usageSummaryCache == nil || a.tokenUsageCache == nil {
		a.setupCacheRegistry()
	}
}

func defaultCacheSettings() cacheSettings {
	return cacheSettings{
		Enabled:                true,
		UsageStatsTTL:          5 * time.Second,
		UsageSummaryTTL:        30 * time.Second,
		TokenUsageTTL:          5 * time.Second,
		EndpointCostTodayTTL:   10 * time.Second,
		EndpointCostHistoryTTL: 5 * time.Minute,
		MaxEntries:             1000,
		StaleOnError:           true,
	}
}

func cacheOptions(name string, ttl time.Duration, settings cacheSettings) appcache.Options {
	return appcache.Options{
		Name:         name,
		TTL:          ttl,
		MaxEntries:   settings.MaxEntries,
		StaleOnError: settings.StaleOnError,
		Enabled:      settings.Enabled,
	}
}

func (a *App) applyCacheSettings(ctx context.Context) {
	if a.cacheRegistry == nil {
		a.setupCacheRegistry()
	}

	settings := defaultCacheSettings()
	if a.settingsService != nil {
		settings.Enabled = a.settingsService.GetBool(ctx, service.CategoryCache, "enabled", settings.Enabled)
		settings.UsageStatsTTL = a.settingsService.GetDuration(ctx, service.CategoryCache, "usage_stats_ttl", settings.UsageStatsTTL)
		settings.UsageSummaryTTL = a.settingsService.GetDuration(ctx, service.CategoryCache, "usage_summary_ttl", settings.UsageSummaryTTL)
		settings.TokenUsageTTL = a.settingsService.GetDuration(ctx, service.CategoryCache, "token_usage_ttl", settings.TokenUsageTTL)
		settings.EndpointCostTodayTTL = a.settingsService.GetDuration(ctx, service.CategoryCache, "endpoint_cost_today_ttl", settings.EndpointCostTodayTTL)
		settings.EndpointCostHistoryTTL = a.settingsService.GetDuration(ctx, service.CategoryCache, "endpoint_cost_history_ttl", settings.EndpointCostHistoryTTL)
		settings.MaxEntries = a.settingsService.GetInt(ctx, service.CategoryCache, "max_entries", settings.MaxEntries)
		settings.StaleOnError = a.settingsService.GetBool(ctx, service.CategoryCache, "stale_on_error", settings.StaleOnError)
	}
	if settings.MaxEntries <= 0 {
		settings.MaxEntries = 1000
	}

	a.usageSummaryCache.SetOptions(cacheOptions(cacheNameUsageSummary, settings.UsageSummaryTTL, settings))
	a.usageHistoryCache.SetOptions(cacheOptions(cacheNameUsageHistory, settings.UsageSummaryTTL, settings))
	a.tokenUsageCache.SetOptions(cacheOptions(cacheNameTokenUsage, settings.TokenUsageTTL, settings))
	a.endpointCostTodayCache.SetOptions(cacheOptions(cacheNameEndpointCostToday, settings.EndpointCostTodayTTL, settings))
	a.endpointCostHistoryCache.SetOptions(cacheOptions(cacheNameEndpointCostHistory, settings.EndpointCostHistoryTTL, settings))

	if a.usageTracker != nil {
		a.usageTracker.ConfigureUsageStatsCache(cacheOptions(cacheNameUsageStats, settings.UsageStatsTTL, settings))
		a.usageTracker.ConfigureDBCountFallbackCache(cacheOptions(cacheNameRequestCount, 0, settings))
		a.registerUsageTrackerCaches()
	}
}

func (a *App) invalidateUsageQueryCaches() {
	if a == nil {
		return
	}
	if a.usageSummaryCache != nil {
		a.usageSummaryCache.Invalidate(appcache.InvalidateOptions{All: true})
	}
	if a.usageHistoryCache != nil {
		a.usageHistoryCache.Invalidate(appcache.InvalidateOptions{All: true})
	}
	if a.tokenUsageCache != nil {
		a.tokenUsageCache.Invalidate(appcache.InvalidateOptions{All: true})
	}
	if a.endpointCostTodayCache != nil {
		a.endpointCostTodayCache.Invalidate(appcache.InvalidateOptions{All: true})
	}
	if a.endpointCostHistoryCache != nil {
		a.endpointCostHistoryCache.Invalidate(appcache.InvalidateOptions{All: true})
	}
	if a.usageTracker != nil {
		a.usageTracker.InvalidateUsageStatsCache()
		a.usageTracker.InvalidateDBCountFallbackCache()
	}
}

func (a *App) GetCacheStatus() (CacheStatus, error) {
	if a.cacheRegistry == nil {
		a.setupCacheRegistry()
	}
	a.registerRuntimeStatusCaches()
	a.registerUsageTrackerCaches()
	a.registerModelPricingCache()

	status := CacheStatus{Enabled: true}
	if a.settingsService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		status.Enabled = a.settingsService.GetBool(ctx, service.CategoryCache, "enabled", true)
		cancel()
	}

	stats := a.cacheRegistry.Status()
	status.Caches = make([]CacheStatusItem, 0, len(stats))
	for _, stat := range stats {
		status.Caches = append(status.Caches, cacheStatusItem(stat))
	}
	return status, nil
}

func (a *App) ClearCache(input ClearCacheInput) (CacheClearResult, error) {
	if a.cacheRegistry == nil {
		a.setupCacheRegistry()
	}
	a.registerRuntimeStatusCaches()
	a.registerUsageTrackerCaches()
	a.registerModelPricingCache()
	result := a.cacheRegistry.Clear(appcache.ClearOptions{Names: input.Names})
	return CacheClearResult{
		ClearedCaches:  result.ClearedCaches,
		ClearedEntries: result.ClearedEntries,
		UnknownNames:   result.UnknownNames,
	}, nil
}

func cacheStatusItem(stat appcache.Stats) CacheStatusItem {
	return CacheStatusItem{
		Name:              stat.Name,
		Enabled:           stat.Enabled,
		TTL:               stat.TTL.String(),
		MaxEntries:        stat.MaxEntries,
		Size:              stat.Size,
		Hits:              stat.Hits,
		Misses:            stat.Misses,
		Expired:           stat.Expired,
		Evicted:           stat.Evicted,
		Invalidations:     stat.Invalidations,
		LastUpdatedAt:     formatCacheTime(stat.LastUpdatedAt),
		LastInvalidatedAt: formatCacheTime(stat.LastInvalidatedAt),
	}
}

func formatCacheTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
