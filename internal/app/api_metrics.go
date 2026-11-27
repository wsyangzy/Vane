package app

import (
	"os"
	"runtime"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/monitor"
)

// SystemMetrics 系统运行指标快照
// 用于前端监控面板实时展示
type SystemMetrics struct {
	HotPoolSize     int   `json:"hot_pool_size"`
	HotPoolCapacity int   `json:"hot_pool_capacity"`
	HotPoolDropped  int64 `json:"hot_pool_dropped"`

	TotalRequests  int64 `json:"total_requests"`
	ActiveRequests int   `json:"active_requests"`

	HealthyEndpoints int `json:"healthy_endpoints"`
	TotalEndpoints   int `json:"total_endpoints"`

	Uptime        string  `json:"uptime"`
	GoRoutines    int     `json:"go_routines"`
	MemoryUsageMB float64 `json:"memory_usage_mb"`

	DBSizeMB float64 `json:"db_size_mb"`

	RetryBudgetExhausted int64 `json:"retry_budget_exhausted"`
}

// GetSystemMetrics 返回系统运行指标快照
func (a *App) GetSystemMetrics() (*SystemMetrics, error) {
	metrics := &SystemMetrics{
		GoRoutines: runtime.NumGoroutine(),
	}

	// 内存使用
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	metrics.MemoryUsageMB = float64(memStats.Alloc) / 1024 / 1024
	if process, err := monitor.CurrentProcessMemory(); err == nil {
		metrics.MemoryUsageMB = float64(process.WorkingSetBytes) / 1024 / 1024
	}

	// Uptime
	a.mu.RLock()
	startTime := a.startTime
	cfg := a.config
	a.mu.RUnlock()

	if !startTime.IsZero() {
		metrics.Uptime = formatDuration(time.Since(startTime))
	}

	// HotPool 指标
	if a.usageTracker != nil {
		metrics.HotPoolDropped = a.usageTracker.GetHotPoolDroppedCount()
		metrics.ActiveRequests = a.usageTracker.GetActiveRequestCount()
	}

	// 端点健康
	if a.endpointManager != nil {
		allEndpoints := a.endpointManager.GetAllEndpoints()
		metrics.TotalEndpoints = len(allEndpoints)
		for _, ep := range allEndpoints {
			if ep.Status.Healthy {
				metrics.HealthyEndpoints++
			}
		}
	}

	// 数据库文件大小
	if cfg != nil && cfg.UsageTracking.DatabasePath != "" {
		if info, err := os.Stat(cfg.UsageTracking.DatabasePath); err == nil {
			metrics.DBSizeMB = float64(info.Size()) / 1024 / 1024
		}
	}

	a.logMemoryDiagnostics()
	return metrics, nil
}

// Keep diagnostics out of request payloads and throttle them independently of
// UI polling. The existing Wails DTO remains usable by older frontend builds.
func (a *App) logMemoryDiagnostics() {
	now := time.Now().UnixNano()
	last := a.memoryLogAt.Load()
	if last != 0 && now-last < int64(time.Minute) {
		return
	}
	a.mu.RLock()
	logger, reader, tracker := a.logger, a.localUsageReader, a.usageTracker
	a.mu.RUnlock()
	if logger == nil || !a.memoryLogAt.CompareAndSwap(last, now) {
		return
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	process, err := monitor.CurrentProcessMemory()
	fields := []any{
		"go_heap_bytes", mem.Alloc,
		"go_managed_bytes", mem.Sys - mem.HeapReleased,
		"go_heap_idle_bytes", mem.HeapIdle,
		"go_heap_released_bytes", mem.HeapReleased,
		"process_memory_available", err == nil,
		"process_working_set_bytes", process.WorkingSetBytes,
		"process_private_bytes", process.PrivateBytes,
		"process_private_available", process.PrivateAvailable,
	}
	if reader != nil {
		cache := reader.MemoryStats()
		fields = append(fields,
			"local_index_files", cache.IndexedFiles,
			"local_resident_parsers", cache.ResidentParsers,
			"local_parser_estimated_bytes", cache.ParserBytes,
			"local_title_estimated_bytes", cache.TitleBytes,
			"local_metadata_estimated_bytes", cache.MetadataBytes,
			"local_cache_budget_bytes", cache.BudgetBytes,
			"local_cache_storage_available", cache.LastStorageError == "",
		)
	}
	if tracker != nil {
		if db := tracker.GetReadDB(); db != nil {
			pool := db.Stats()
			fields = append(fields, "sqlite_read_open", pool.OpenConnections, "sqlite_read_in_use", pool.InUse, "sqlite_read_idle", pool.Idle, "sqlite_read_idle_closed", pool.MaxIdleClosed+pool.MaxIdleTimeClosed)
		}
		if pool := tracker.GetHotPoolStats(); pool != nil {
			fields = append(fields, "request_hot_pool_size", pool.CurrentSize, "request_hot_pool_peak", pool.PeakSize)
		}
	}
	logger.Info("内存诊断", fields...)
}
