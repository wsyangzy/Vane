package tracking

import (
	appcache "github.com/wsyangzy/Vane/internal/platform/cache"

	_ "modernc.org/sqlite"
)

// SetUsageCacheInvalidationCallback registers an external invalidator for app-level usage caches.
// 每次 request_logs 落库变更后回调，参数描述受影响的 provider 与时间范围。
func (ut *UsageTracker) SetUsageCacheInvalidationCallback(callback func(appcache.Change)) {
	if ut == nil {
		return
	}
	ut.usageCacheInvalidatorMu.Lock()
	ut.usageCacheInvalidator = callback
	ut.usageCacheInvalidatorMu.Unlock()
}

// NotifyUsageDataChanged 供 tracking 包外直接改写 request_logs 的调用点（如端点改名）使缓存失效。
func (ut *UsageTracker) NotifyUsageDataChanged(change appcache.Change) {
	ut.notifyUsageDataChanged(change)
}

// notifyUsageDataChanged 是 request_logs 变更后的唯一失效入口：所有写路径都必须经过这里，
// 否则统计缓存只能等兜底 TTL 才刷新。
// requests:count-fallback 是「上次成功的计数」兜底值，本就允许陈旧，不随数据变更清除。
func (ut *UsageTracker) notifyUsageDataChanged(change appcache.Change) {
	if ut == nil {
		return
	}
	if ut.usageStatsCache != nil {
		ut.usageStatsCache.Invalidate(appcache.InvalidateOptions{Change: &change})
	}
	ut.usageCacheInvalidatorMu.RLock()
	callback := ut.usageCacheInvalidator
	ut.usageCacheInvalidatorMu.RUnlock()
	if callback != nil {
		callback(change)
	}
}

// invalidateArchiveTotalsLocked 必须在持有 historyMu 写锁时调用：归档文件内容已变，
// 持有读锁的查询都已结束，之后的查询会重新读取归档文件。
func (ut *UsageTracker) invalidateArchiveTotalsLocked() {
	if ut.archiveTotalsCache != nil {
		ut.archiveTotalsCache.Invalidate(appcache.InvalidateOptions{All: true})
	}
}
