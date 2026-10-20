package tracking

import (
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/config"

	_ "modernc.org/sqlite"
)

// Config 使用跟踪配置
type Config struct {
	Enabled bool
	// Environment scopes request tracking when dev and release share usage.db.
	// Empty values are normalized to release for backwards compatibility.
	Environment string

	// SQLite database path.
	DatabasePath string

	// 新增：数据库配置（优先级高于 DatabasePath）
	Database *config.DatabaseBackendConfig

	BufferSize       int
	BatchSize        int
	FlushInterval    time.Duration
	MaxRetry         int
	RetentionDays    int
	CleanupInterval  time.Duration
	ArchiveDirectory string
	ModelPricing     map[string]ModelPricing
	DefaultPricing   ModelPricing

	// 🔥 新增：热池配置
	HotPool *HotPoolSettings
}

func normalizeTrackingEnvironment(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "dev") {
		return "dev"
	}
	return "release"
}

// HotPoolSettings 热池配置
type HotPoolSettings struct {
	Enabled          bool          // 是否启用热池模式（默认true）
	MaxAge           time.Duration // 最大存活时间（默认30分钟）
	MaxSize          int           // 最大容量（默认10000）
	CleanupInterval  time.Duration // 清理间隔（默认1分钟）
	ArchiveOnCleanup bool          // 清理时是否归档（默认true）
}
