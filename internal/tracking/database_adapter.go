package tracking

import (
	"path/filepath"
	"time"
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
		config.DatabasePath = filepath.Join(getAppDataDir(), "data", "usage.db")
	}
	if config.Timezone == "" {
		config.Timezone = "Asia/Shanghai"
	}
}
