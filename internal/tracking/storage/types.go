package storage

import (
	"path/filepath"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/appdir"
)

// DatabaseConfig 数据库配置结构
// 简化配置，仅保留 SQLite 相关字段
type DatabaseConfig struct {
	// SQLite配置
	DatabasePath string

	// 时区配置（用于时间格式化）
	Timezone string
}

// ConnectionStats 连接池统计信息
type ConnectionStats struct {
	OpenConnections  int           `json:"open_connections"`
	IdleConnections  int           `json:"idle_connections"`
	InUseConnections int           `json:"in_use_connections"`
	WaitCount        int64         `json:"wait_count"`
	WaitDuration     time.Duration `json:"wait_duration"`
	MaxLifetime      time.Duration `json:"max_lifetime"`
}

// setDefaultConfig 设置数据库配置默认值
// 简化为仅处理 SQLite 配置
func setDefaultConfig(config *DatabaseConfig) {
	// SQLite 默认配置
	if config.DatabasePath == "" {
		// 默认使用跨平台用户目录，避免在桌面应用/打包环境下写入到工作目录导致“数据丢失/分裂”。
		config.DatabasePath = filepath.Join(appdir.Dir(), "data", "usage.db")
	}
	if config.Timezone == "" {
		config.Timezone = "Asia/Shanghai"
	}
}

// DatabaseStats 数据库统计信息
type DatabaseStats struct {
	TotalRequests  int64      `json:"total_requests"`
	TotalSummaries int64      `json:"total_summaries"`
	EarliestRecord *time.Time `json:"earliest_record,omitempty"`
	LatestRecord   *time.Time `json:"latest_record,omitempty"`
	DatabaseSize   int64      `json:"database_size_bytes"`
	TotalCostUSD   float64    `json:"total_cost_usd"`
}
