package app

import (
	"os"
	"runtime"
	"time"
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

	return metrics, nil
}
