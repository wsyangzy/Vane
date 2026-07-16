package tracking

import (
	"path/filepath"
	"time"
)

// DatabaseConfig 数据库配置结构
// 简化配置，仅保留 SQLite 相关字段
type DatabaseConfig struct {
	// SQLite配置
	DatabasePath string `yaml:"database_path,omitempty"`

	// 时区配置（用于时间格式化）
	Timezone string `yaml:"timezone,omitempty"`

	// ===== 以下 MySQL 相关字段已废弃，保留用于配置文件向后兼容 =====
	// 注意：这些字段不再生效，配置这些字段会打印警告日志
	// Host     string `yaml:"host,omitempty"`     // DEPRECATED: MySQL not supported
	// Port     int    `yaml:"port,omitempty"`     // DEPRECATED: MySQL not supported
	// Database string `yaml:"database,omitempty"` // DEPRECATED: MySQL not supported
	// Username string `yaml:"username,omitempty"` // DEPRECATED: MySQL not supported
	// Password string `yaml:"password,omitempty"` // DEPRECATED: MySQL not supported
	// MaxOpenConns    int           `yaml:"max_open_conns,omitempty"`    // DEPRECATED
	// MaxIdleConns    int           `yaml:"max_idle_conns,omitempty"`    // DEPRECATED
	// ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime,omitempty"` // DEPRECATED
	// ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time,omitempty"` // DEPRECATED
	// Charset  string `yaml:"charset,omitempty"`  // DEPRECATED: MySQL not supported
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
