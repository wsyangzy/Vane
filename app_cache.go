package main

import (
	"context"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
	"ai-relay-forwarder/internal/service"
	"ai-relay-forwarder/internal/tracking"
)

const (
	cacheNameUsageStats        = "usage:stats"
	cacheNameUsageSummary      = "usage:summary"
	cacheNameUsageArchive      = "usage:archive-months"
	cacheNameTokenUsage        = "usage:token"
	cacheNameEndpointCostToday = "endpoint-cost:today"
	cacheNameRequestCount      = "requests:count-fallback"
	cacheNameRuntimePortCheck  = "runtime:port-check"
	cacheNameRuntimeClaudeCLI  = "runtime:claude-cli-version"
	cacheNameRuntimeCodexCLI   = "runtime:codex-cli-version"
)

type usageSummaryDBSnapshot struct {
	allTimeTotalCost   float64
	allTimeTotalTokens int64
	allTimeTotal       int64
	todayCost          float64
	todayTokens        int64
	todayRequests      int64
}

// cacheSettings 的 TTL 是兜底时长：统计缓存靠 request_logs 写入时的失效保持新鲜，
// TTL 只防御遗漏的写入路径，因此所有统计缓存共用一个值。
type cacheSettings struct {
	Enabled      bool
	FallbackTTL  time.Duration
	MaxEntries   int
	StaleOnError bool
}

// CacheStatus is returned to the frontend for cache observability.
type CacheStatus struct {
	Enabled bool              `json:"enabled"`
	Caches  []CacheStatusItem `json:"caches"`
}

type CacheStatusItem struct {
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	Fixed             bool   `json:"fixed"`
	TTL               string `json:"ttl"`
	MaxEntries        int    `json:"max_entries"`
	Size              int    `json:"size"`
	Hits              uint64 `json:"hits"`
	Misses            uint64 `json:"misses"`
	Expired           uint64 `json:"expired"`
	Evicted           uint64 `json:"evicted"`
	Invalidations     uint64 `json:"invalidations"`
	StaleServed       uint64 `json:"stale_served"`
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

// externalCacheAdapter 把运行时状态缓存（端口检测、CLI 版本）接入注册表以便观测与清理。
// 这类缓存保护的是外部进程和网络探测而非数据库，不受「启用缓存」开关控制，状态里标记为常驻。
type externalCacheAdapter struct {
	name       string
	stats      func() appcache.Stats
	invalidate func() int
}

func (a externalCacheAdapter) Name() string {
	return a.name
}

func (a externalCacheAdapter) Stats() appcache.Stats {
	stats := appcache.Stats{Name: a.name, Enabled: true}
	if a.stats != nil {
		stats = a.stats()
	}
	stats.Fixed = true
	return stats
}

func (a externalCacheAdapter) Invalidate(options appcache.InvalidateOptions) int {
	// 运行时状态与 request_logs 无关，按数据范围的失效不作用于它们
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

	a.usageSummaryCache = appcache.New[usageSummaryDBSnapshot](cacheOptions(cacheNameUsageSummary, settings))
	a.tokenUsageCache = appcache.New[TokenUsageData](cacheOptions(cacheNameTokenUsage, settings))
	a.endpointCostTodayCache = appcache.New[[]EndpointCostItem](cacheOptions(cacheNameEndpointCostToday, settings))

	a.cacheRegistry.Register(a.usageSummaryCache)
	a.cacheRegistry.Register(a.tokenUsageCache)
	a.cacheRegistry.Register(a.endpointCostTodayCache)
	a.registerRuntimeStatusCaches()
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
	for _, managed := range []appcache.ManagedCache{
		a.usageTracker.UsageStatsCache(),
		a.usageTracker.ArchiveTotalsCache(),
		a.usageTracker.DBCountFallbackCache(),
	} {
		if managed != nil {
			a.cacheRegistry.Register(managed)
		}
	}
}

func (a *App) ensureCacheRegistry() {
	if a.cacheRegistry == nil || a.usageSummaryCache == nil || a.tokenUsageCache == nil {
		a.setupCacheRegistry()
	}
}

func defaultCacheSettings() cacheSettings {
	return cacheSettings{
		Enabled:      true,
		FallbackTTL:  5 * time.Minute,
		MaxEntries:   1000,
		StaleOnError: true,
	}
}

func cacheOptions(name string, settings cacheSettings) appcache.Options {
	return appcache.Options{
		Name:         name,
		TTL:          settings.FallbackTTL,
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
		settings.FallbackTTL = a.settingsService.GetDuration(ctx, service.CategoryCache, "fallback_ttl", settings.FallbackTTL)
		settings.MaxEntries = a.settingsService.GetInt(ctx, service.CategoryCache, "max_entries", settings.MaxEntries)
		settings.StaleOnError = a.settingsService.GetBool(ctx, service.CategoryCache, "stale_on_error", settings.StaleOnError)
	}
	if settings.MaxEntries <= 0 {
		settings.MaxEntries = 1000
	}

	a.usageSummaryCache.SetOptions(cacheOptions(cacheNameUsageSummary, settings))
	a.tokenUsageCache.SetOptions(cacheOptions(cacheNameTokenUsage, settings))
	a.endpointCostTodayCache.SetOptions(cacheOptions(cacheNameEndpointCostToday, settings))

	if a.usageTracker != nil {
		a.usageTracker.ConfigureUsageStatsCache(cacheOptions(cacheNameUsageStats, settings))
		a.usageTracker.ConfigureArchiveTotalsCache(cacheOptions(cacheNameUsageArchive, settings))
		// 计数兜底值只在查库失败时使用，本就是「上次成功的值」，不设 TTL
		countOptions := cacheOptions(cacheNameRequestCount, settings)
		countOptions.TTL = 0
		a.usageTracker.ConfigureDBCountFallbackCache(countOptions)
		a.registerUsageTrackerCaches()
	}
}

// invalidateUsageQueryCaches 是 UsageTracker 在 request_logs 变更后的回调，只清依赖受影响范围的条目。
func (a *App) invalidateUsageQueryCaches(change appcache.Change) {
	if a == nil {
		return
	}
	options := appcache.InvalidateOptions{Change: &change}
	if a.usageSummaryCache != nil {
		a.usageSummaryCache.Invalidate(options)
	}
	if a.tokenUsageCache != nil {
		a.tokenUsageCache.Invalidate(options)
	}
	if a.endpointCostTodayCache != nil {
		a.endpointCostTodayCache.Invalidate(options)
	}
}

// notifyUsageDataChanged 供 tracking 包外直接改写 request_logs 的调用点使用。
func (a *App) notifyUsageDataChanged(usageTracker *tracking.UsageTracker, change appcache.Change) {
	if usageTracker != nil {
		usageTracker.NotifyUsageDataChanged(change)
		return
	}
	a.invalidateUsageQueryCaches(change)
}

func (a *App) GetCacheStatus() (CacheStatus, error) {
	if a.cacheRegistry == nil {
		a.setupCacheRegistry()
	}
	a.registerUsageTrackerCaches()

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
	a.registerUsageTrackerCaches()
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
		Fixed:             stat.Fixed,
		TTL:               stat.TTL.String(),
		MaxEntries:        stat.MaxEntries,
		Size:              stat.Size,
		Hits:              stat.Hits,
		Misses:            stat.Misses,
		Expired:           stat.Expired,
		Evicted:           stat.Evicted,
		Invalidations:     stat.Invalidations,
		StaleServed:       stat.StaleServed,
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
