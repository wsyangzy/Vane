package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/platform/appdir"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// getEffectiveUsageDBPath returns the single SQLite database path used by:
// - usage tracker (request_logs / usage_summary / ...)
// - management stores (channels/endpoints/settings/model_pricing/codex_providers)
//
// 统一数据库路径：内部只使用 UsageTracking.DatabasePath（由 config.SetDefaults 将 database.path 归一到该字段）。
func (a *App) getEffectiveUsageDBPath() string {
	if a == nil || a.config == nil {
		return ""
	}
	if a.config.UsageTracking.DatabasePath != "" {
		return a.config.UsageTracking.DatabasePath
	}
	return filepath.Join(appdir.GetDataDir(), "usage.db")
}

// setupUsageTracker 设置使用追踪
func (a *App) setupUsageTracker() {
	if !a.config.UsageTracking.Enabled {
		a.logger.Info("📊 使用追踪已禁用")
		return
	}

	dbPath := a.getEffectiveUsageDBPath()
	a.config.UsageTracking.DatabasePath = dbPath

	a.logger.Info("📊 初始化使用追踪器", "db_path", dbPath)

	// 定价配置完全从 SQLite 加载。
	// 初始化时使用空定价，后续由 syncPricingToTracker() 从数据库加载
	archiveEnvironment := "dev"
	if strings.EqualFold(buildinfo.BuildMode, "release") {
		archiveEnvironment = "release"
	}
	trackingConfig := &tracking.Config{
		Enabled:          a.config.UsageTracking.Enabled,
		Environment:      archiveEnvironment,
		DatabasePath:     dbPath,
		Database:         a.config.UsageTracking.Database,
		BufferSize:       a.config.UsageTracking.BufferSize,
		BatchSize:        a.config.UsageTracking.BatchSize,
		FlushInterval:    a.config.UsageTracking.FlushInterval,
		MaxRetry:         a.config.UsageTracking.MaxRetry,
		RetentionDays:    a.config.UsageTracking.RetentionDays,
		ArchiveDirectory: filepath.Join(appdir.GetDataDir(), "archive", archiveEnvironment),
		ModelPricing:     nil,                     // 定价从 SQLite model_pricing 表加载
		DefaultPricing:   tracking.ModelPricing{}, // 默认定价从 SQLite 加载
	}

	var err error
	a.usageTracker, err = tracking.NewUsageTracker(trackingConfig, a.config.Timezone)
	if err != nil {
		a.logger.Error("使用追踪器初始化失败", "error", err)
		return
	}
	a.usageTracker.SetUsageCacheInvalidationCallback(a.invalidateUsageQueryCaches)
	if a.cacheRegistry != nil {
		a.registerUsageTrackerCaches()
	}

	a.logger.Info("📊 使用追踪已启用", "database", dbPath)
}

// setupStoreDB 初始化一个专用的 SQLite 连接用于管理写操作（channels/endpoints/settings/codex_providers）。
func (a *App) setupStoreDB() {
	if a.config == nil {
		return
	}
	if a.storeDB != nil {
		return
	}
	dbPath := a.getEffectiveUsageDBPath()
	// 运行时回填，确保后续组件拿到统一路径（即使 usage_tracking.enabled=false）。
	a.config.UsageTracking.DatabasePath = dbPath

	// 确保数据库目录存在
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			a.logger.Warn("⚠️ 创建数据库目录失败", "error", err)
			return
		}
	}

	// 使用较长 busy_timeout：管理侧读写应尽量避免 SQLITE_BUSY；上层仍用 ctx 控制总体等待时间。
	// 注意：写冲突时 SQLite 仍只有一个 writer，该超时只是让管理侧在短暂争用时更耐心等待。
	dsn := dbPath + "?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_foreign_keys=1&_busy_timeout=60000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		a.logger.Warn("⚠️ 初始化管理数据库连接失败", "error", err)
		return
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// 这里不做过短超时：如果用户正运行旧版本/另一实例占用数据库，短超时会导致 storeDB 为 nil，
	// 进而前端出现“设置服务未启用/数据为空”的连锁问题。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dbReady := true
	if err := db.PingContext(ctx); err != nil {
		if isLikelySQLiteBusyOrLocked(err) || strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded") {
			dbReady = false
			a.logger.Warn("⚠️ 管理数据库连接暂不可用（可能被占用/初始化耗时较长），将继续启动并允许稍后重试", "error", err)
			a.emitNotification("warning", "数据库暂不可用", "检测到数据库可能被占用或初始化耗时较长；管理功能（设置/渠道/定价）可能暂不可用。请确保只运行一个 Vane 实例，稍等后点击刷新，必要时重启。")
		} else {
			if closeErr := db.Close(); closeErr != nil {
				a.logger.Warn("⚠️ 关闭不可用管理数据库连接失败", "error", closeErr)
			}
			a.logger.Warn("⚠️ 管理数据库连接不可用", "error", err)
			return
		}
	}

	// 当 usageTracker 未启用/初始化失败时，仍需要确保管理表（settings/channels/endpoints/model_pricing/codex_providers 等）存在。
	if a.usageTracker == nil && dbReady {
		adapter, err := tracking.NewSQLiteAdapter(tracking.DatabaseConfig{
			DatabasePath: dbPath,
			Timezone:     a.config.Timezone,
		})
		if err == nil {
			if err := adapter.Open(); err == nil {
				if err := adapter.InitSchema(); err != nil {
					a.logger.Warn("⚠️ 初始化数据库 Schema 失败（管理存储）", "error", err)
				}
				if closeErr := adapter.Close(); closeErr != nil {
					a.logger.Warn("⚠️ 关闭数据库适配器失败（管理存储）", "error", closeErr)
				}
			} else {
				a.logger.Warn("⚠️ 打开数据库失败（管理存储）", "error", err)
			}
		} else {
			a.logger.Warn("⚠️ 创建数据库适配器失败（管理存储）", "error", err)
		}
	}

	a.storeDB = db
	a.logger.Info("✅ 管理数据库连接已就绪", "db", dbPath)
}

func isLikelySQLiteBusyOrLocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "locked")
}
