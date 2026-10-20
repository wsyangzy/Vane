package tracking

import (
	"context"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

// ConfigureUsageStatsCache applies runtime cache options for DB usage-stat totals.
func (ut *UsageTracker) ConfigureUsageStatsCache(options appcache.Options) {
	if ut == nil {
		return
	}
	if options.Name == "" {
		options.Name = "usage:stats"
	}
	if ut.usageStatsCache == nil {
		ut.usageStatsCache = appcache.New[UsageStatsTotals](options)
		return
	}
	ut.usageStatsCache.SetOptions(options)
}

// UsageStatsCacheStats returns the cache status snapshot for DB usage-stat totals.
func (ut *UsageTracker) UsageStatsCacheStats() appcache.Stats {
	if ut == nil || ut.usageStatsCache == nil {
		return appcache.Stats{Name: "usage:stats"}
	}
	return ut.usageStatsCache.Stats()
}

// UsageStatsCache returns the managed cache instance for app-level registry wiring.
func (ut *UsageTracker) UsageStatsCache() appcache.ManagedCache {
	if ut == nil || ut.usageStatsCache == nil {
		return nil
	}
	return ut.usageStatsCache
}

// InvalidateUsageStatsCache clears cached DB usage-stat totals.
func (ut *UsageTracker) InvalidateUsageStatsCache() int {
	if ut == nil || ut.usageStatsCache == nil {
		return 0
	}
	return ut.usageStatsCache.Invalidate(appcache.InvalidateOptions{All: true})
}

// ConfigureDBCountFallbackCache applies runtime cache options for request count fallback.
func (ut *UsageTracker) ConfigureDBCountFallbackCache(options appcache.Options) {
	if ut == nil {
		return
	}
	if options.Name == "" {
		options.Name = "requests:count-fallback"
	}
	if ut.dbCountFallbackCache == nil {
		ut.dbCountFallbackCache = appcache.New[int](options)
		return
	}
	ut.dbCountFallbackCache.SetOptions(options)
}

// QueryScope 把查询条件映射成缓存依赖范围：provider 过滤与 start_time 区间决定哪些写入会影响结果。
func QueryScope(opts *QueryOptions) appcache.Scope {
	if opts == nil {
		return appcache.Scope{}
	}
	scope := appcache.Scope{Provider: opts.Provider}
	if opts.StartDate != nil {
		scope.Start = *opts.StartDate
	}
	if opts.EndDate != nil {
		scope.End = *opts.EndDate
	}
	return scope
}

// ArchiveTotalsCache returns the archive-month aggregation cache for app-level registry wiring.
func (ut *UsageTracker) ArchiveTotalsCache() appcache.ManagedCache {
	if ut == nil || ut.archiveTotalsCache == nil {
		return nil
	}
	return ut.archiveTotalsCache
}

// ConfigureArchiveTotalsCache 应用统一开关等运行时选项；归档聚合由失效驱动，不设 TTL。
func (ut *UsageTracker) ConfigureArchiveTotalsCache(options appcache.Options) {
	if ut == nil || ut.archiveTotalsCache == nil {
		return
	}
	options.TTL = 0
	ut.archiveTotalsCache.SetOptions(options)
}

// DBCountFallbackCache returns the managed cache instance for app-level registry wiring.
func (ut *UsageTracker) DBCountFallbackCache() appcache.ManagedCache {
	if ut == nil || ut.dbCountFallbackCache == nil {
		return nil
	}
	return ut.dbCountFallbackCache
}

// InvalidateDBCountFallbackCache clears cached DB count fallback values.
func (ut *UsageTracker) InvalidateDBCountFallbackCache() int {
	if ut == nil || ut.dbCountFallbackCache == nil {
		return 0
	}
	return ut.dbCountFallbackCache.Invalidate(appcache.InvalidateOptions{All: true})
}

func (ut *UsageTracker) queryUsageStatsTotalsCached(ctx context.Context, opts *QueryOptions) (*UsageStatsTotals, error) {
	if ut == nil || ut.usageStatsCache == nil {
		return ut.QueryUsageStatsTotals(ctx, opts)
	}

	key := "usage:stats:" + queryOptsCacheKey(opts)
	value, err := ut.usageStatsCache.GetOrLoadScoped(key, QueryScope(opts), func() (UsageStatsTotals, error) {
		totals, err := ut.QueryUsageStatsTotals(ctx, opts)
		if err != nil {
			return UsageStatsTotals{}, err
		}
		if totals == nil {
			return UsageStatsTotals{}, nil
		}
		return *totals, nil
	})
	if err != nil {
		return nil, err
	}
	totals := value
	return &totals, nil
}
