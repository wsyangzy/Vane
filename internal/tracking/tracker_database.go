package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/appdir"
	"github.com/wsyangzy/Vane/internal/tracking/storage"

	_ "modernc.org/sqlite"
)

// buildDatabaseConfig 从Config构建DatabaseConfig
// 简化为仅支持 SQLite
func buildDatabaseConfig(config *Config, globalTimezone string) (DatabaseConfig, error) {
	var dbConfig DatabaseConfig

	// 优先使用新的Database配置
	if config.Database != nil {
		// Use the normalized SQLite path when no backend path is supplied.
		if config.Database.Path != "" {
			dbConfig.DatabasePath = config.Database.Path
		} else {
			dbConfig.DatabasePath = config.DatabasePath
		}
		if config.Database.Timezone != "" {
			dbConfig.Timezone = config.Database.Timezone
		} else {
			dbConfig.Timezone = globalTimezone
		}
	} else {
		// Use the normalized SQLite database path.
		dbConfig.DatabasePath = config.DatabasePath
	}

	// 设置默认数据库路径 - 使用跨平台用户目录
	if dbConfig.DatabasePath == "" {
		// 使用应用统一的跨平台数据目录
		// Windows: %APPDATA%\Vane\data\usage.db
		// macOS: ~/Library/Application Support/Vane/data/usage.db
		// Linux: ~/.local/share/Vane/data/usage.db
		dbConfig.DatabasePath = filepath.Join(getAppDataDir(), "data", "usage.db")
	}

	// 时区级联逻辑：优先级 database.timezone > global.timezone > 默认值
	if dbConfig.Timezone == "" {
		if globalTimezone != "" {
			dbConfig.Timezone = globalTimezone
		}
		// setDefaultConfig 会设置默认值 Asia/Shanghai
	}

	return dbConfig, nil
}

// getAppDataDir uses the shared Vane directory and legacy fallback.
func getAppDataDir() string { return appdir.Dir() }

// initDatabaseWithAdapter 使用适配器初始化数据库
func (ut *UsageTracker) initDatabaseWithAdapter() error {
	if ut.adapter == nil {
		return fmt.Errorf("database adapter not initialized")
	}

	// 使用适配器初始化Schema
	if err := ut.adapter.InitSchema(); err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	slog.Info("数据库Schema初始化完成", "database_type", "sqlite")

	return nil
}

// cleanupCrashedRequests 启动时清理上次崩溃遗留的"未完成"请求
//
// 🔧 [P1.5 新增] 进程崩溃恢复后的数据一致性保护：
//   - 扫描所有处于活跃状态（forwarding/streaming/processing/retry/suspended/pending）
//     且 updated_at 超过 staleThreshold（默认 10 分钟）的请求
//   - 标记为 status=failed, failure_reason=crashed_during_processing
//   - 设置 end_time = 当前时间，用于统计聚合
//
// 🔧 [P5.9 增强] 区分"已发送 token"和"未发送 token"的崩溃场景：
//   - 已发送 token（output_sent_at IS NOT NULL）→ crashed_after_output_sent
//     表示客户端可能已收到部分响应，本地缺失最终 token 数据
//   - 未发送 token → crashed_during_processing
//     表示请求处理过程中崩溃，客户端未收到响应
//
// 这样可以避免：
//  1. 前端展示永久"运行中"误导用户
//  2. usage_summary 聚合时把这些异常请求计入活跃统计
//  3. 双源查询时被误认为"活跃请求"
func (ut *UsageTracker) cleanupCrashedRequests() error {
	if ut.writeDB == nil {
		return fmt.Errorf("write DB not initialized")
	}

	const staleThreshold = 10 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 计算阈值时间：updated_at < (now - staleThreshold) 的活跃请求即为僵尸
	// 使用 SQLite 的 datetime 函数，保持与 schema 中 updated_at 格式一致
	cutoffTime := time.Now().In(ut.location).Add(-staleThreshold).Format("2006-01-02 15:04:05")
	now := time.Now().In(ut.location).Format("2006-01-02 15:04:05")

	// 🔧 [P5.9] 第一步：处理 output_sent_at IS NOT NULL 的崩溃请求
	// 这些请求已向客户端发送了部分 token，客户端可能已计费
	query1 := `
		UPDATE request_logs
		SET status = 'failed',
		    failure_reason = COALESCE(failure_reason, 'crashed_after_output_sent'),
		    last_failure_reason = COALESCE(last_failure_reason, '进程在已发送内容后崩溃，token 数据可能不完整'),
		    end_time = COALESCE(end_time, ?)
		WHERE status IN ('pending', 'forwarding', 'streaming', 'processing', 'retry', 'suspended')
		  AND updated_at < ?
		  AND COALESCE(environment, 'release') = ?
		  AND output_sent_at IS NOT NULL
	`
	result1, err1 := ut.writeDB.ExecContext(ctx, query1, now, cutoffTime, ut.environment)
	if err1 != nil {
		// output_sent_at 字段可能在旧库不存在，降级到原始 query
		slog.Warn("output_sent_at 列可能不存在，降级到统一清理", "error", err1)
	} else {
		rows1, _ := result1.RowsAffected()
		if rows1 > 0 {
			slog.Warn("🧹 [P5.9] 启动时清理已发送内容后崩溃的请求",
				"count", rows1,
				"reason", "crashed_after_output_sent，建议手动检查这些请求的 token 计费一致性")
		}
	}

	// 第二步：处理剩余的崩溃请求（output_sent_at IS NULL 或字段不存在）
	query2 := `
		UPDATE request_logs
		SET status = 'failed',
		    failure_reason = COALESCE(failure_reason, 'crashed_during_processing'),
		    last_failure_reason = COALESCE(last_failure_reason, '进程在处理期间崩溃，启动时自动标记为失败'),
		    end_time = COALESCE(end_time, ?)
		WHERE status IN ('pending', 'forwarding', 'streaming', 'processing', 'retry', 'suspended')
		  AND updated_at < ?
		  AND COALESCE(environment, 'release') = ?
	`
	result, err := ut.writeDB.ExecContext(ctx, query2, now, cutoffTime, ut.environment)
	if err != nil {
		return fmt.Errorf("failed to cleanup crashed requests: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		slog.Warn("🧹 启动时清理僵尸请求",
			"count", rowsAffected,
			"stale_threshold", staleThreshold,
			"reason", "上次进程未正常结束，将这些请求标记为 crashed_during_processing")
	} else {
		slog.Debug("启动时未发现僵尸请求")
	}

	return nil
}

// initDatabaseWithWriteDB 使用写连接初始化数据库（同步等待完成）
func (ut *UsageTracker) initDatabaseWithWriteDB() error {
	// 读取并执行 schema SQL
	schemaSQL, err := storage.Schema()
	if err != nil {
		return fmt.Errorf("failed to read schema.sql: %w", err)
	}

	// 使用写连接直接执行 schema（同步方式，确保表创建完成）
	if _, err := ut.writeDB.Exec(string(schemaSQL)); err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	slog.Debug("Database schema initialized successfully with write connection")
	return nil
}

// ============================================================================
// 🔥 热池相关方法
// ============================================================================

// initDatabase 初始化数据库
func (ut *UsageTracker) initDatabase() error {
	// 使用适配器初始化数据库schema
	if err := ut.adapter.InitSchema(); err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	slog.Debug("Database schema initialized successfully")
	return nil
}

// GetDB returns the read database connection for external queries (读写分离：返回读连接)
func (ut *UsageTracker) GetDB() *sql.DB {
	return ut.readDB
}

// GetReadDB returns the read database connection
func (ut *UsageTracker) GetReadDB() *sql.DB {
	return ut.readDB
}

// GetWriteDB returns the write database connection (仅用于特殊情况)
func (ut *UsageTracker) GetWriteDB() *sql.DB {
	return ut.writeDB
}

// These aliases preserve the tracking API while storage owns SQLite lifecycle and schema.
type DatabaseConfig = storage.DatabaseConfig
type ConnectionStats = storage.ConnectionStats
type DatabaseStats = storage.DatabaseStats
type SQLiteAdapter = storage.SQLiteAdapter

func NewSQLiteAdapter(config DatabaseConfig) (*SQLiteAdapter, error) {
	return storage.NewSQLiteAdapter(config)
}
