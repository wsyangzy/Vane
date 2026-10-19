package tracking

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	claudestore "ai-relay-forwarder/internal/providers/claude/store"
	codexstore "ai-relay-forwarder/internal/providers/codex/store"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var sqliteSchemaFS embed.FS

// SQLiteAdapter SQLite数据库适配器实现（保持原有逻辑）
type SQLiteAdapter struct {
	config   DatabaseConfig
	readDB   *sql.DB
	writeDB  *sql.DB
	logger   *slog.Logger
	location *time.Location // 配置的时区
}

// NewSQLiteAdapter 创建SQLite适配器实例
func NewSQLiteAdapter(config DatabaseConfig) (*SQLiteAdapter, error) {
	// 设置默认配置
	setDefaultConfig(&config)

	// 解析时区配置
	timezone := strings.TrimSpace(config.Timezone)
	if timezone == "" {
		timezone = "Asia/Shanghai" // 默认时区
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		// 如果时区解析失败，记录错误但不终止，使用系统本地时区
		location = time.Local
		slog.Warn("SQLite时区解析失败，使用系统本地时区",
			"configured_timezone", timezone,
			"error", err,
			"fallback_timezone", location.String())
	} else {
		slog.Info("SQLite时区配置成功", "timezone", timezone)
	}

	adapter := &SQLiteAdapter{
		config:   config,
		logger:   slog.Default(),
		location: location,
	}

	return adapter, nil
}

// Open 建立SQLite数据库连接
func (s *SQLiteAdapter) Open() error {
	dbPath := s.config.DatabasePath
	if dbPath == "" {
		// 使用跨平台用户目录作为默认路径
		// Windows: %APPDATA%\Claude-Request-Forwarder\data\usage.db
		// macOS: ~/Library/Application Support/Claude-Request-Forwarder/data/usage.db
		// Linux: ~/.local/share/ai-relay-forwarder/data/usage.db
		dbPath = filepath.Join(getSQLiteAppDataDir(), "data", "usage.db")
		s.logger.Info("使用默认数据库路径", "path", dbPath)
	}

	s.logger.Info("正在连接SQLite数据库", "path", dbPath)

	// 确保数据库目录存在
	if dbPath != ":memory:" {
		dbDir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	writeDSN := s.buildSQLiteDSN(dbPath)

	writeDB, err := sql.Open(sqliteDriverName, writeDSN)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// 写句柄保持单连接，避免多事务并发写入带来的锁争用。
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := writeDB.PingContext(ctx); err != nil {
		writeDB.Close()
		return fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	readDB, err := sql.Open(sqliteDriverName, writeDSN)
	if err != nil {
		writeDB.Close()
		return fmt.Errorf("failed to open SQLite read database: %w", err)
	}

	// 读句柄允许少量并发连接，让查询不要和写事务共用同一连接。
	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(4)
	readDB.SetConnMaxLifetime(0)

	if err := readDB.PingContext(ctx); err != nil {
		readDB.Close()
		writeDB.Close()
		return fmt.Errorf("failed to ping SQLite read database: %w", err)
	}

	s.writeDB = writeDB
	s.readDB = readDB

	// 🔧 [P2.1 新增] PRAGMA 调优，缓解长时间运行后数据库压力
	if err := s.applyPragmaTuning(ctx); err != nil {
		s.logger.Warn("PRAGMA tuning failed (continuing with defaults)", "error", err)
	}

	// 诊断时区设置
	s.diagnoseTimezoneSettings()

	s.logger.Info("✅ SQLite数据库连接成功")

	return nil
}

// applyPragmaTuning 应用 SQLite PRAGMA 调优
//
// 🔧 [P2.1 新增] 针对"长时间运行后数据库压力大"的核心优化：
//
//  1. wal_autocheckpoint=500（默认 1000 页）
//     - 更频繁地把 WAL 内容合并到主数据库，避免 WAL 文件无界膨胀
//     - 500 页 ≈ 2 MB，触发 checkpoint 比 4 MB（默认）更主动
//
//  2. journal_size_limit=67108864（64 MB）
//     - 限制 WAL 文件最大大小，超过后会被截断
//     - 防止极端场景下 WAL 占用过多磁盘
//
//  3. auto_vacuum=INCREMENTAL（仅对新数据库生效；旧库需要 VACUUM 一次）
//     - 启用增量真空，DELETE 后空间可通过 incremental_vacuum 回收
//     - 旧数据库的 auto_vacuum 模式已固化，需重建数据库才能改变
//
//  4. mmap_size=268435456（256 MB）
//     - 启用内存映射 I/O，加速读取
//
// 这些设置每次连接都需要执行（PRAGMA 是 session 级别），但 wal_autocheckpoint
// 和 journal_size_limit 会影响整个数据库，所以只需在 writeDB 上执行
func (s *SQLiteAdapter) applyPragmaTuning(ctx context.Context) error {
	if s.writeDB == nil {
		return fmt.Errorf("write DB not available")
	}

	// 检查 auto_vacuum 当前模式，仅在 NONE (0) 时尝试设为 INCREMENTAL
	// SQLite 文档：auto_vacuum 只能在数据库为空或执行 VACUUM 时改变
	var currentAutoVacuum int
	_ = s.writeDB.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&currentAutoVacuum)

	tunings := []struct {
		pragma string
		desc   string
	}{
		// 更频繁 checkpoint，控制 WAL 大小
		{"PRAGMA wal_autocheckpoint=500", "WAL 自动 checkpoint 阈值"},
		// 限制 WAL 文件大小上限（64 MB）
		{"PRAGMA journal_size_limit=67108864", "WAL 文件大小上限"},
		// 临时表存内存，加速排序/聚合
		{"PRAGMA temp_store=MEMORY", "临时表内存模式"},
		// 🔧 [P2.1 调整] 不启用 mmap_size：Windows 上 mmap 会阻止 RestoreFromBackup 的文件重命名操作
		// 实测数据库 < 1GB 场景下 mmap 收益有限，关闭以确保跨平台稳定性
	}

	for _, t := range tunings {
		if _, err := s.writeDB.ExecContext(ctx, t.pragma); err != nil {
			s.logger.Warn("PRAGMA failed", "pragma", t.pragma, "desc", t.desc, "error", err)
		} else {
			s.logger.Debug("PRAGMA applied", "pragma", t.pragma, "desc", t.desc)
		}
	}

	// 仅在 auto_vacuum=0（NONE）时尝试启用 INCREMENTAL
	// 注：新数据库默认就是 0，但已有数据时无法切换，需要 VACUUM 重建
	if currentAutoVacuum == 0 {
		// 仅记录提示，不实际启用（启用需要先 VACUUM 整库，对大数据库影响大）
		s.logger.Info("提示：当前 auto_vacuum=NONE，删除数据后空间不会自动回收。" +
			"如需启用增量 VACUUM，可手动执行：PRAGMA auto_vacuum=INCREMENTAL; VACUUM;")
	} else {
		s.logger.Debug("auto_vacuum 已启用", "mode", currentAutoVacuum)
	}

	return nil
}

// PerformIncrementalVacuum 执行增量 VACUUM 回收空间
//
// 🔧 [P2.1 新增] 仅在 auto_vacuum=INCREMENTAL 时有效
// 调用时机建议：周期性后台任务、用户主动触发、清理大量旧数据后
//
// 参数：
//   - pages: 回收的页数（0 = 回收所有空闲页）
//
// 注意：incremental_vacuum 不会锁全表，可在运行时调用
func (s *SQLiteAdapter) PerformIncrementalVacuum(ctx context.Context, pages int) error {
	if s.writeDB == nil {
		return fmt.Errorf("write DB not available")
	}
	pragma := "PRAGMA incremental_vacuum"
	if pages > 0 {
		pragma = fmt.Sprintf("PRAGMA incremental_vacuum(%d)", pages)
	}
	if _, err := s.writeDB.ExecContext(ctx, pragma); err != nil {
		return fmt.Errorf("incremental_vacuum: %w", err)
	}
	return nil
}

// PerformAnalyze 更新 SQLite 查询规划器的统计信息
//
// 🔧 [P2.4 新增] 定期执行 ANALYZE 让查询规划器选择更优执行计划
//   - 数据分布变化（如大量删除/插入后）会让旧统计信息失效
//   - 失效的统计信息可能导致规划器选错索引或走全表扫描
//   - ANALYZE 不会锁全表，可在运行时调用
//
// 建议调用时机：
//   - 启动后首次调用（更新到最新统计）
//   - 大批量数据变更后（如 cleanupOldRecords 之后）
//   - 周期性后台任务（如每天一次）
func (s *SQLiteAdapter) PerformAnalyze(ctx context.Context) error {
	if s.writeDB == nil {
		return fmt.Errorf("write DB not available")
	}
	if _, err := s.writeDB.ExecContext(ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	return nil
}

// AuditIndexUsage 审计索引使用情况
//
// 🔧 [P2.4 新增] 通过 EXPLAIN QUERY PLAN 检查关键查询的索引使用
// 返回 map[query_name]plan_text，运维可观察哪些查询走全表扫描
//
// 注：这是诊断工具，不在常规路径调用，仅供前端"诊断"按钮或调试日志使用
func (s *SQLiteAdapter) AuditIndexUsage(ctx context.Context) (map[string]string, error) {
	if s.readDB == nil {
		return nil, fmt.Errorf("read DB not available")
	}

	// 项目核心查询清单：与 tracker.go / database.go 中的关键查询保持同步
	criticalQueries := map[string]string{
		"by_start_time":     "SELECT * FROM request_logs WHERE start_time >= ? AND start_time < ?",
		"by_status":         "SELECT * FROM request_logs WHERE status = ?",
		"by_request_id":     "SELECT * FROM request_logs WHERE request_id = ?",
		"by_endpoint_name":  "SELECT * FROM request_logs WHERE endpoint_name = ?",
		"by_channel":        "SELECT * FROM request_logs WHERE channel = ?",
		"by_failure_reason": "SELECT * FROM request_logs WHERE failure_reason = ?",
		"daily_aggregation": "SELECT model_name, endpoint_name, group_name, DATE(start_time) FROM request_logs WHERE start_time >= ? GROUP BY DATE(start_time), model_name, endpoint_name, group_name",
		"order_by_start":    "SELECT * FROM request_logs ORDER BY start_time DESC LIMIT 100",
	}

	results := make(map[string]string, len(criticalQueries))
	for name, query := range criticalQueries {
		rows, err := s.readDB.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, makeDummyArgs(query)...)
		if err != nil {
			results[name] = fmt.Sprintf("error: %v", err)
			continue
		}
		var planLines []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err == nil {
				planLines = append(planLines, detail)
			}
		}
		rows.Close()
		results[name] = strings.Join(planLines, " | ")
	}
	return results, nil
}

// makeDummyArgs 为 EXPLAIN QUERY PLAN 构造占位参数
// 简单计算 query 中 ? 的数量，返回相应数量的 nil 参数
func makeDummyArgs(query string) []any {
	count := strings.Count(query, "?")
	args := make([]any, count)
	for i := range args {
		args[i] = nil
	}
	return args
}

// Close 关闭数据库连接
func (s *SQLiteAdapter) Close() error {
	var firstErr error
	if s.readDB != nil {
		s.logger.Info("正在关闭SQLite读数据库连接")
		if err := s.readDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		s.readDB = nil
	}
	if s.writeDB != nil {
		s.logger.Info("正在关闭SQLite写数据库连接")
		if err := s.writeDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		s.writeDB = nil
	}
	return firstErr
}

// Ping 测试数据库连接
func (s *SQLiteAdapter) Ping(ctx context.Context) error {
	if s.writeDB == nil {
		return fmt.Errorf("database not connected")
	}
	if err := s.writeDB.PingContext(ctx); err != nil {
		return err
	}
	if s.readDB != nil {
		return s.readDB.PingContext(ctx)
	}
	return nil
}

// GetDB 获取数据库连接
func (s *SQLiteAdapter) GetDB() *sql.DB {
	if s.readDB != nil {
		return s.readDB
	}
	return s.writeDB
}

// GetReadDB 获取读数据库连接
func (s *SQLiteAdapter) GetReadDB() *sql.DB {
	return s.GetDB()
}

// GetWriteDB 获取写数据库连接
func (s *SQLiteAdapter) GetWriteDB() *sql.DB {
	if s.writeDB != nil {
		return s.writeDB
	}
	return s.readDB
}

// BeginTx 开始事务
func (s *SQLiteAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if s.GetWriteDB() == nil {
		return nil, fmt.Errorf("database not connected")
	}
	return s.GetWriteDB().BeginTx(ctx, opts)
}

// InitSchema 初始化SQLite数据库Schema
func (s *SQLiteAdapter) InitSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.logger.Info("正在初始化SQLite数据库Schema")

	// 🔧 [P1.2 新增] 首先创建/检查 schema_versions 表
	// 用于追踪迁移执行状态，让迁移可观察、可回滚、可幂等
	if err := s.ensureSchemaVersionsTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure schema_versions table: %w", err)
	}

	// 读取并执行SQLite schema
	schema, err := sqliteSchemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema.sql: %w", err)
	}

	// SQLite可以直接执行整个schema。
	// 但：旧库可能缺少后续新增的列，schema.sql 中的索引/触发器可能会引用这些列，
	// 导致 Exec 直接失败并中断启动。为保证向后兼容：
	// - 先尝试执行 schema.sql
	// - 若因"no such column"失败，则先跑 migrateSchema 补列，再重试执行 schema.sql
	if _, err := s.GetWriteDB().ExecContext(ctx, string(schema)); err != nil {
		if strings.Contains(err.Error(), "no such column:") {
			s.logger.Warn("schema.sql 执行失败（缺少列），将先执行迁移后重试", "error", err)
			if err := s.migrateSchema(ctx); err != nil {
				return fmt.Errorf("failed to migrate schema (pre-schema retry): %w", err)
			}
			if _, err := s.GetWriteDB().ExecContext(ctx, string(schema)); err != nil {
				return fmt.Errorf("failed to execute schema (after migrate): %w", err)
			}
		} else {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	// 执行迁移添加新字段
	if err := s.migrateSchema(ctx); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	s.logger.Info("✅ SQLite数据库Schema初始化完成")
	return nil
}

// ensureSchemaVersionsTable 创建 schema_versions 表（如不存在）
//
// 🔧 [P1.2 新增] 引入 schema 版本管理：
//   - 每个迁移有唯一的 version 标识（如 "20251205_add_endpoint_chain"）
//   - 已成功执行的迁移记录在 schema_versions 表中
//   - 后续启动跳过已执行的迁移，提供幂等性
//   - 迁移失败时记录失败状态，便于排查
//
// 表结构：
//
//	version       TEXT PRIMARY KEY  -- 迁移唯一标识
//	description   TEXT              -- 人类可读描述
//	applied_at    DATETIME          -- 成功应用时间
//	checksum      TEXT              -- 迁移 SQL 的简单校验和（未来扩展）
func (s *SQLiteAdapter) ensureSchemaVersionsTable(ctx context.Context) error {
	if s.GetWriteDB() == nil {
		return fmt.Errorf("write DB not available")
	}
	const ddl = `
		CREATE TABLE IF NOT EXISTS schema_versions (
			version     TEXT PRIMARY KEY NOT NULL,
			description TEXT,
			applied_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			checksum    TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_schema_versions_applied_at ON schema_versions(applied_at);
	`
	if _, err := s.GetWriteDB().ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create schema_versions table: %w", err)
	}
	return nil
}

// isSchemaVersionApplied 检查指定 version 的迁移是否已成功应用
// 🔧 [P1.2 新增]
func (s *SQLiteAdapter) isSchemaVersionApplied(ctx context.Context, version string) (bool, error) {
	if s.GetReadDB() == nil {
		return false, fmt.Errorf("read DB not available")
	}
	var count int
	err := s.GetReadDB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_versions WHERE version = ?", version).Scan(&count)
	if err != nil {
		// 如果表本身不存在，视为未应用（首次启动场景）
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, fmt.Errorf("check schema version %s: %w", version, err)
	}
	return count > 0, nil
}

// recordSchemaVersion 记录迁移已成功应用
// 🔧 [P1.2 新增]
func (s *SQLiteAdapter) recordSchemaVersion(ctx context.Context, version, description string) error {
	if s.GetWriteDB() == nil {
		return fmt.Errorf("write DB not available")
	}
	_, err := s.GetWriteDB().ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_versions (version, description) VALUES (?, ?)`,
		version, description)
	if err != nil {
		return fmt.Errorf("record schema version %s: %w", version, err)
	}
	return nil
}

// migrateSchema 执行数据库迁移（添加 5m/1h 缓存字段）
func (s *SQLiteAdapter) migrateSchema(ctx context.Context) error {
	if err := s.ensureSettingsProviderScope(ctx); err != nil {
		return err
	}

	// request_logs 迁移：历史上先上线 usage tracking，后续补充缓存字段
	requestLogMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "endpoint_chain",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN endpoint_chain TEXT",
			description: "端点链路字段",
		},
		{
			checkColumn: "cache_creation_5m_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_5m_tokens INTEGER DEFAULT 0",
			description: "5分钟缓存创建tokens字段",
		},
		{
			checkColumn: "cache_creation_1h_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_1h_tokens INTEGER DEFAULT 0",
			description: "1小时缓存创建tokens字段",
		},
		{
			checkColumn: "cache_creation_5m_cost_usd",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_5m_cost_usd REAL DEFAULT 0",
			description: "5分钟缓存创建成本字段",
		},
		{
			checkColumn: "cache_creation_1h_cost_usd",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_1h_cost_usd REAL DEFAULT 0",
			description: "1小时缓存创建成本字段",
		},
		// 🔧 [P5.9 新增] output_sent_at 字段，用于崩溃恢复时区分"已计费"场景
		{
			checkColumn: "output_sent_at",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN output_sent_at DATETIME",
			description: "已发送内容时间戳（P5.9）",
		},
		{
			checkColumn: "provider",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude'",
			description: "提供商标识字段（claude/codex）",
		},
		{
			checkColumn: "environment",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN environment TEXT NOT NULL DEFAULT 'release'",
			description: "运行环境字段（dev/release）",
		},
		{
			checkColumn: "privacy_redacted",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN privacy_redacted BOOLEAN DEFAULT 0",
			description: "隐私脱敏命中标记字段",
		},
		{
			checkColumn: "privacy_redaction_summary",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN privacy_redaction_summary TEXT",
			description: "隐私脱敏摘要字段",
		},
		// 端点倍率快照字段（用于详情页计费过程展示）
		{
			checkColumn: "cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点总成本倍率快照",
		},
		{
			checkColumn: "input_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN input_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点输入成本倍率快照",
		},
		{
			checkColumn: "output_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN output_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点输出成本倍率快照",
		},
		{
			checkColumn: "cache_creation_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点 5m 缓存创建成本倍率快照",
		},
		{
			checkColumn: "cache_creation_cost_multiplier_1h",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_cost_multiplier_1h REAL DEFAULT 1.0",
			description: "请求时端点 1h 缓存创建成本倍率快照",
		},
		{
			checkColumn: "cache_read_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_read_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点缓存读取成本倍率快照",
		},
		{
			checkColumn: "model_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加总倍率快照",
		},
		{
			checkColumn: "model_input_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_input_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加输入倍率快照",
		},
		{
			checkColumn: "model_output_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_output_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加输出倍率快照",
		},
		{
			checkColumn: "model_cache_read_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_cache_read_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加缓存读取倍率快照",
		},
		{
			checkColumn: "reasoning_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN reasoning_tokens INTEGER DEFAULT 0",
			description: "输出推理 tokens 字段",
		},
		{
			checkColumn: "billable_output_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN billable_output_tokens INTEGER DEFAULT 0",
			description: "输出计费 tokens 字段",
		},
		{
			checkColumn: "billing_output_token_source",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens'",
			description: "输出计费口径字段",
		},
		{
			checkColumn: "billing_rule_snapshot",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN billing_rule_snapshot TEXT",
			description: "组合计费规则命中快照",
		},
		{
			checkColumn: "original_model_name",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN original_model_name TEXT",
			description: "模型改写前原始模型字段",
		},
		{
			checkColumn: "model_rewrite_source",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_rewrite_source TEXT",
			description: "模型改写来源字段",
		},
		{
			checkColumn: "model_rewrite_target",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_rewrite_target TEXT",
			description: "模型改写目标模型字段",
		},
		{
			checkColumn: "model_reasoning_effort",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_reasoning_effort TEXT",
			description: "推理强度字段 (Codex reasoning.effort)",
		},
		{
			checkColumn: "service_tier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN service_tier TEXT",
			description: "服务等级字段 (Codex fast mode)",
		},
		{
			checkColumn: "http_status_is_upstream",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN http_status_is_upstream BOOLEAN NOT NULL DEFAULT 0",
			description: "HTTP状态码来源标记（真实上游响应）",
		},
		{
			checkColumn: "route_snapshot",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN route_snapshot TEXT",
			description: "请求级路由快照字段",
		},
		{
			// 不回填历史行：旧行没有流完整性判定的原始信号，成功率 SQL 对 NULL 回退到旧口径
			checkColumn: "stream_outcome",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN stream_outcome TEXT",
			description: "请求终态枚举字段",
		},
	}

	// endpoints 迁移：端点存储表迭代新增字段时，需要兼容旧 db（CREATE TABLE IF NOT EXISTS 不会补列）
	endpointsMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "credential_group_id",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN credential_group_id INTEGER REFERENCES claude_credential_groups(id) ON DELETE SET NULL",
			description: "Claude 凭证分组引用字段",
		},
		{
			checkColumn: "timeout_seconds",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN timeout_seconds INTEGER DEFAULT 300",
			description: "端点超时字段",
		},
		{
			checkColumn: "supports_count_tokens",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN supports_count_tokens INTEGER DEFAULT 0",
			description: "端点 supports_count_tokens 字段",
		},
		{
			checkColumn: "cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cost_multiplier REAL DEFAULT 1.0",
			description: "端点总成本倍率字段",
		},
		{
			checkColumn: "input_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN input_cost_multiplier REAL DEFAULT 1.0",
			description: "端点输入成本倍率字段",
		},
		{
			checkColumn: "output_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN output_cost_multiplier REAL DEFAULT 1.0",
			description: "端点输出成本倍率字段",
		},
		{
			checkColumn: "cache_creation_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cache_creation_cost_multiplier REAL DEFAULT 1.0",
			description: "端点 5m 缓存创建成本倍率字段",
		},
		{
			checkColumn: "cache_creation_cost_multiplier_1h",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cache_creation_cost_multiplier_1h REAL DEFAULT 1.0",
			description: "端点 1h 缓存创建成本倍率字段",
		},
		{
			checkColumn: "cache_read_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cache_read_cost_multiplier REAL DEFAULT 1.0",
			description: "端点缓存读取成本倍率字段",
		},
		{
			checkColumn: "skip_health_check",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN skip_health_check INTEGER DEFAULT 0",
			description: "端点跳过健康检查字段 (v6.1)",
		},
		{
			checkColumn: "model_multipliers",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'",
			description: "端点模型附加倍率字段",
		},
	}

	// channels 迁移：早期可能只有 name，后续新增 website
	codexEndpointMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "skip_health_check",
			alterSQL:    "ALTER TABLE codex_endpoints ADD COLUMN skip_health_check INTEGER NOT NULL DEFAULT 0",
			description: "Codex 端点跳过健康检查字段",
		},
		{
			checkColumn: "timeout_seconds",
			alterSQL:    "ALTER TABLE codex_endpoints ADD COLUMN timeout_seconds INTEGER NOT NULL DEFAULT 0",
			description: "Codex 端点请求超时字段",
		},
		{
			checkColumn: "model_multipliers",
			alterSQL:    "ALTER TABLE codex_endpoints ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 端点模型附加倍率字段",
		},
	}
	codexChannelMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{"priority", "ALTER TABLE codex_channels ADD COLUMN priority INTEGER NOT NULL DEFAULT 0", "Codex 渠道优先级字段"},
		{"failover_enabled", "ALTER TABLE codex_channels ADD COLUMN failover_enabled INTEGER NOT NULL DEFAULT 1", "Codex 渠道故障转移参与字段"},
		{"non_stream_route", "ALTER TABLE codex_channels ADD COLUMN non_stream_route TEXT", "Codex 渠道非流改道字段"},
		{"privacy_level", "ALTER TABLE codex_channels ADD COLUMN privacy_level TEXT", "Codex 渠道隐私级别字段"},
	}
	modelRouteMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		// 旧行补 0：它们原本是全局规则，改为渠道级后不再参与匹配，需用户在渠道下重建
		{"source_channel_id", "ALTER TABLE model_routes ADD COLUMN source_channel_id INTEGER NOT NULL DEFAULT 0", "模型路由规则来源渠道字段"},
	}

	channelMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "website",
			alterSQL:    "ALTER TABLE channels ADD COLUMN website TEXT",
			description: "渠道官网字段",
		},
		{
			checkColumn: "priority",
			alterSQL:    "ALTER TABLE channels ADD COLUMN priority INTEGER DEFAULT 1",
			description: "渠道优先级字段",
		},
		{
			checkColumn: "failover_enabled",
			alterSQL:    "ALTER TABLE channels ADD COLUMN failover_enabled INTEGER DEFAULT 1",
			description: "渠道故障转移开关字段",
		},
		{
			checkColumn: "model_mapping",
			alterSQL:    "ALTER TABLE channels ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'",
			description: "渠道模型映射字段",
		},
		{
			checkColumn: "non_stream_route",
			alterSQL:    "ALTER TABLE channels ADD COLUMN non_stream_route TEXT",
			description: "渠道非流改道字段",
		},
		{
			checkColumn: "privacy_level",
			alterSQL:    "ALTER TABLE channels ADD COLUMN privacy_level TEXT",
			description: "渠道隐私级别字段",
		},
	}

	runMigrations := func(table string, migrations []struct {
		checkColumn string
		alterSQL    string
		description string
	}) error {
		tableExists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}
		if !tableExists {
			return nil
		}

		for _, m := range migrations {
			exists, err := s.columnExists(ctx, table, m.checkColumn)
			if err != nil {
				return fmt.Errorf("failed to check column %s.%s: %w", table, m.checkColumn, err)
			}
			if exists {
				continue
			}

			s.logger.Info(fmt.Sprintf("🔧 [数据库迁移] %s：添加 %s", table, m.description))
			if _, err := s.GetWriteDB().ExecContext(ctx, m.alterSQL); err != nil {
				return fmt.Errorf("failed to add column %s.%s: %w", table, m.checkColumn, err)
			}
			s.logger.Info(fmt.Sprintf("✅ [数据库迁移] %s：%s 添加成功", table, m.description))
		}
		return nil
	}

	if err := runMigrations("request_logs", requestLogMigrations); err != nil {
		return err
	}
	if err := runMigrations("model_routes", modelRouteMigrations); err != nil {
		return err
	}
	// 历史行的 http_status_code 可能是旧版本 Relay 自造的兜底码，但读取侧已按
	// http_status_is_upstream 隐藏；不做不可逆的批量清空，真实上游码也在其中。
	if err := s.purgePrivacyPlaintextMatches(ctx); err != nil {
		return err
	}
	if err := runMigrations("endpoints", endpointsMigrations); err != nil {
		return err
	}
	if err := s.ensureClaudeCredentialGroupsSchema(ctx); err != nil {
		return err
	}
	if err := s.backfillClaudeCredentialGroups(ctx); err != nil {
		return err
	}
	if err := s.repairClaudeCredentialGroupOwnership(ctx); err != nil {
		return err
	}
	if err := s.repairCrossProviderMultiplierCost(ctx); err != nil {
		return err
	}
	if err := s.migrateLongContextPricingDefault(ctx); err != nil {
		return err
	}
	if err := s.migrateClaudeLongContextRemoval(ctx); err != nil {
		return err
	}
	if err := runMigrations("codex_credential_groups", []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "daily_cost_limit_usd",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN daily_cost_limit_usd REAL NOT NULL DEFAULT 0",
			description: "Codex 凭证分组每日成本限额字段",
		},
		{
			checkColumn: "model_policy",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN model_policy TEXT NOT NULL DEFAULT 'inherit'",
			description: "Codex 凭证分组模型策略字段",
		},
		{
			checkColumn: "model_mapping",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'",
			description: "Codex 凭证分组模型映射字段",
		},
		{
			checkColumn: "supported_models",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN supported_models TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 凭证分组支持模型字段",
		},
		{
			checkColumn: "disable_service_tier_pricing",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1",
			description: "Codex 凭证分组服务等级计费开关",
		},
		{
			checkColumn: "service_tier_multipliers",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN service_tier_multipliers TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 凭证分组四档服务等级倍率",
		},
		{
			checkColumn: "billing_rules",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN billing_rules TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 凭证分组组合计费规则",
		},
	}); err != nil {
		return err
	}
	if err := codexstore.EnsureCodexSubscriptionSchema(ctx, s.GetWriteDB()); err != nil {
		return err
	}
	if err := runMigrations("codex_endpoints", codexEndpointMigrations); err != nil {
		return err
	}
	if err := runMigrations("codex_channels", codexChannelMigrations); err != nil {
		return err
	}
	if err := s.normalizeCodexChannelPriorities(ctx); err != nil {
		return err
	}
	// 允许不同凭证分组端点同名（约束从 name/channel 级调整为 credential_group 级）。
	// SQLite 无法直接移除旧 UNIQUE 约束，需要在发现旧约束时重建表。
	if err := s.ensureEndpointsUniqueByCredentialGroup(ctx); err != nil {
		return err
	}
	if err := runMigrations("channels", channelMigrations); err != nil {
		return err
	}

	// usage_summary 迁移
	usageSummaryMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "provider",
			alterSQL:    "ALTER TABLE usage_summary ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude'",
			description: "使用统计提供商标识字段（claude/codex）",
		},
		{
			checkColumn: "environment",
			alterSQL:    "ALTER TABLE usage_summary ADD COLUMN environment TEXT NOT NULL DEFAULT 'release'",
			description: "使用统计运行环境字段（dev/release）",
		},
	}
	if err := runMigrations("usage_summary", usageSummaryMigrations); err != nil {
		return err
	}
	if err := s.ensureUsageSummaryEnvironmentScope(ctx); err != nil {
		return err
	}

	// model_pricing 迁移
	modelPricingMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "provider",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude'",
			description: "模型定价提供商标识字段（claude/codex）",
		},
		{
			checkColumn: "long_context_threshold",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_threshold INTEGER NOT NULL DEFAULT 0",
			description: "Long context 原始输入 token 阈值",
		},
		{
			checkColumn: "long_context_input_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_input_price REAL NOT NULL DEFAULT 0",
			description: "Long context 输入价格",
		},
		{
			checkColumn: "long_context_cache_read_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_cache_read_price REAL NOT NULL DEFAULT 0",
			description: "Long context 缓存读取价格",
		},
		{
			checkColumn: "long_context_cache_write_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_cache_write_price REAL NOT NULL DEFAULT 0",
			description: "Long context 缓存写入价格",
		},
		{
			checkColumn: "long_context_output_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_output_price REAL NOT NULL DEFAULT 0",
			description: "Long context 输出价格",
		},
	}
	if err := runMigrations("model_pricing", modelPricingMigrations); err != nil {
		return err
	}
	if err := s.rebuildModelPricingForProviderTierUnique(ctx); err != nil {
		return err
	}

	return nil
}

// ensureUsageSummaryEnvironmentScope upgrades legacy usage_summary tables whose
// UNIQUE constraint predates provider/environment scoping. SQLite cannot alter
// a table constraint in place, so rebuild only when the desired unique key is
// missing; clean databases take the cheap metadata-check path.
func (s *SQLiteAdapter) ensureUsageSummaryEnvironmentScope(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "usage_summary")
	if err != nil || !exists {
		return err
	}
	hasDesiredUnique, err := s.usageSummaryHasEnvironmentUnique(ctx)
	if err != nil {
		return err
	}
	if hasDesiredUnique {
		return nil
	}
	s.logger.Info("🔧 [数据库迁移] usage_summary：重建包含 provider/environment 的唯一约束")
	return s.rebuildUsageSummaryForEnvironment(ctx)
}

func (s *SQLiteAdapter) usageSummaryHasEnvironmentUnique(ctx context.Context) (bool, error) {
	rows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_list(usage_summary)")
	if err != nil {
		return false, fmt.Errorf("failed to inspect usage_summary indexes: %w", err)
	}
	type indexInfo struct {
		name   string
		unique int
	}
	var indexes []indexInfo
	for rows.Next() {
		var seq, unique, partial int
		var origin string
		var name string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("failed to scan usage_summary indexes: %w", err)
		}
		if unique != 0 {
			indexes = append(indexes, indexInfo{name: name, unique: unique})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, fmt.Errorf("failed to iterate usage_summary indexes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	want := []string{"date", "model_name", "endpoint_name", "group_name", "provider", "environment"}
	for _, index := range indexes {
		escaped := strings.ReplaceAll(index.name, "'", "''")
		columnRows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_info('"+escaped+"')")
		if err != nil {
			return false, fmt.Errorf("failed to inspect usage_summary index %s: %w", index.name, err)
		}
		columns := make([]string, 0, len(want))
		for columnRows.Next() {
			var seqno, cid int
			var name string
			if err := columnRows.Scan(&seqno, &cid, &name); err != nil {
				_ = columnRows.Close()
				return false, fmt.Errorf("failed to scan usage_summary index %s: %w", index.name, err)
			}
			columns = append(columns, name)
		}
		if err := columnRows.Close(); err != nil {
			return false, err
		}
		if len(columns) == len(want) {
			match := true
			for i := range want {
				if columns[i] != want[i] {
					match = false
					break
				}
			}
			if match {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *SQLiteAdapter) rebuildUsageSummaryForEnvironment(ctx context.Context) error {
	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin usage_summary rebuild: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`DROP INDEX IF EXISTS idx_usage_summary_date`,
		`DROP INDEX IF EXISTS idx_usage_summary_model`,
		`DROP INDEX IF EXISTS idx_usage_summary_endpoint`,
		`DROP INDEX IF EXISTS idx_usage_summary_group`,
		`DROP INDEX IF EXISTS idx_usage_summary_provider`,
		`DROP INDEX IF EXISTS idx_usage_summary_environment_date`,
		`DROP TRIGGER IF EXISTS update_usage_summary_timestamp`,
		`ALTER TABLE usage_summary RENAME TO usage_summary_legacy_environment`,
		`CREATE TABLE usage_summary (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL,
			model_name TEXT NOT NULL,
			endpoint_name TEXT NOT NULL,
			group_name TEXT,
			provider TEXT NOT NULL DEFAULT 'claude',
			environment TEXT NOT NULL DEFAULT 'release',
			request_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			error_count INTEGER DEFAULT 0,
			total_input_tokens INTEGER DEFAULT 0,
			total_output_tokens INTEGER DEFAULT 0,
			total_cache_creation_tokens INTEGER DEFAULT 0,
			total_cache_read_tokens INTEGER DEFAULT 0,
			total_cost_usd REAL DEFAULT 0,
			avg_duration_ms REAL DEFAULT 0,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			UNIQUE(date, model_name, endpoint_name, group_name, provider, environment)
		)`,
		`INSERT INTO usage_summary (
			id, date, model_name, endpoint_name, group_name, provider, environment,
			request_count, success_count, error_count,
			total_input_tokens, total_output_tokens,
			total_cache_creation_tokens, total_cache_read_tokens,
			total_cost_usd, avg_duration_ms, created_at, updated_at
		)
		SELECT id, date, model_name, endpoint_name, group_name,
			COALESCE(provider, 'claude'), COALESCE(environment, 'release'),
			request_count, success_count, error_count,
			total_input_tokens, total_output_tokens,
			total_cache_creation_tokens, total_cache_read_tokens,
			total_cost_usd, avg_duration_ms, created_at, updated_at
		FROM usage_summary_legacy_environment`,
		`DROP TABLE usage_summary_legacy_environment`,
		`CREATE INDEX idx_usage_summary_date ON usage_summary(date)`,
		`CREATE INDEX idx_usage_summary_model ON usage_summary(model_name)`,
		`CREATE INDEX idx_usage_summary_endpoint ON usage_summary(endpoint_name)`,
		`CREATE INDEX idx_usage_summary_group ON usage_summary(group_name)`,
		`CREATE INDEX idx_usage_summary_provider ON usage_summary(provider)`,
		`CREATE INDEX idx_usage_summary_environment_date ON usage_summary(environment, date)`,
		`CREATE TRIGGER update_usage_summary_timestamp
			AFTER UPDATE ON usage_summary
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
		BEGIN
				UPDATE usage_summary SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
			END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("rebuild usage_summary: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit usage_summary rebuild: %w", err)
	}
	return nil
}

func (s *SQLiteAdapter) normalizeCodexChannelPriorities(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "codex_channels")
	if err != nil || !exists {
		return err
	}
	exists, err = s.columnExists(ctx, "codex_channels", "priority")
	if err != nil || !exists {
		return err
	}
	rows, err := s.writeDB.QueryContext(ctx, `
		SELECT id, priority
		FROM codex_channels
		ORDER BY priority ASC, id ASC`)
	if err != nil {
		return fmt.Errorf("查询 Codex 渠道优先级失败: %w", err)
	}
	type channelPriority struct {
		id       int64
		priority int
	}
	var channels []channelPriority
	maxPriority := -1
	for rows.Next() {
		var channel channelPriority
		if err := rows.Scan(&channel.id, &channel.priority); err != nil {
			rows.Close()
			return fmt.Errorf("读取 Codex 渠道优先级失败: %w", err)
		}
		channels = append(channels, channel)
		if channel.priority > maxPriority {
			maxPriority = channel.priority
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	used := make(map[int]struct{}, len(channels))
	for _, channel := range channels {
		if _, exists := used[channel.priority]; !exists {
			used[channel.priority] = struct{}{}
			continue
		}
		maxPriority++
		if _, err := s.writeDB.ExecContext(ctx, `UPDATE codex_channels SET priority = ? WHERE id = ?`, maxPriority, channel.id); err != nil {
			return fmt.Errorf("修复 Codex 渠道重复优先级失败: %w", err)
		}
		used[maxPriority] = struct{}{}
	}
	return nil
}

func (s *SQLiteAdapter) ensureSettingsProviderScope(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "settings")
	if err != nil || !exists {
		return err
	}
	hasProvider, err := s.columnExists(ctx, "settings", "provider")
	if err != nil || hasProvider {
		return err
	}

	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings provider migration: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`DROP INDEX IF EXISTS idx_settings_category`,
		`DROP INDEX IF EXISTS idx_settings_category_key`,
		`DROP TRIGGER IF EXISTS update_settings_timestamp`,
		`ALTER TABLE settings RENAME TO settings_legacy_provider_scope`,
		`CREATE TABLE settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL CHECK (provider IN ('global', 'claude', 'codex')),
			category TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			value_type TEXT DEFAULT 'string',
			label TEXT,
			description TEXT,
			display_order INTEGER DEFAULT 0,
			requires_restart INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			UNIQUE(provider, category, key)
		)`,
		`INSERT INTO settings (
			id, provider, category, key, value, value_type, label, description,
			display_order, requires_restart, created_at, updated_at
		) SELECT
			id, CASE WHEN category = 'failover' THEN 'claude' ELSE 'global' END,
			category, key, value, value_type, label, description,
			display_order, requires_restart, created_at, updated_at
		FROM settings_legacy_provider_scope`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate settings provider scope: %w", err)
		}
	}

	var oldCount, newCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings_legacy_provider_scope`).Scan(&oldCount); err != nil {
		return fmt.Errorf("count legacy settings: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings`).Scan(&newCount); err != nil {
		return fmt.Errorf("count migrated settings: %w", err)
	}
	if oldCount != newCount {
		return fmt.Errorf("settings provider migration row mismatch: old=%d new=%d", oldCount, newCount)
	}

	for _, statement := range []string{
		`DROP TABLE settings_legacy_provider_scope`,
		`CREATE INDEX idx_settings_provider_category ON settings(provider, category)`,
		`CREATE INDEX idx_settings_provider_category_key ON settings(provider, category, key)`,
		`CREATE TRIGGER update_settings_timestamp
			AFTER UPDATE ON settings
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
			BEGIN
				UPDATE settings SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
			END`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("finalize settings provider migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings provider migration: %w", err)
	}
	return nil
}

// purgePrivacyPlaintextMatches 清除历史遗留的 privacy_redaction_matches 列。
//
// 该列曾存放隐私命中的原文与替换值——隐私防护功能反而把客户明文集中落进了
// 本地库。停止写入不足以止血，已存在的明文必须主动清除：
// 先 UPDATE 置空（即便后续 DROP 不被支持，明文也已不可读），再尝试 DROP COLUMN
// （SQLite 3.35+ 才支持，失败不阻断启动）。
func (s *SQLiteAdapter) purgePrivacyPlaintextMatches(ctx context.Context) error {
	exists, err := s.columnExists(ctx, "request_logs", "privacy_redaction_matches")
	if err != nil {
		return fmt.Errorf("failed to check column request_logs.privacy_redaction_matches: %w", err)
	}
	if !exists {
		return nil
	}

	s.logger.Info("🔒 [隐私止血] 清除 request_logs.privacy_redaction_matches 中的历史明文")
	if _, err := s.GetWriteDB().ExecContext(ctx,
		`UPDATE request_logs SET privacy_redaction_matches = NULL WHERE privacy_redaction_matches IS NOT NULL`); err != nil {
		return fmt.Errorf("failed to clear privacy_redaction_matches: %w", err)
	}
	if _, err := s.GetWriteDB().ExecContext(ctx,
		`ALTER TABLE request_logs DROP COLUMN privacy_redaction_matches`); err != nil {
		// 旧版 SQLite 不支持 DROP COLUMN。内容已置空，留列不影响隐私。
		s.logger.Warn("隐私明文列内容已清空，但当前 SQLite 不支持删除该列", "error", err)
		return nil
	}
	s.logger.Info("✅ [隐私止血] privacy_redaction_matches 列已删除")
	return nil
}

const longContextPricingDefaultMigrationVersion = "20260712_default_disable_long_context_pricing"

func (s *SQLiteAdapter) migrateLongContextPricingDefault(ctx context.Context) error {
	applied, err := s.isSchemaVersionApplied(ctx, longContextPricingDefaultMigrationVersion)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	for _, table := range []string{"claude_credential_groups", "codex_credential_groups"} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check %s for Long context default migration: %w", table, err)
		}
		if !exists {
			continue
		}
		hasColumn, err := s.columnExists(ctx, table, "disable_long_context_pricing")
		if err != nil {
			return fmt.Errorf("failed to check %s.disable_long_context_pricing: %w", table, err)
		}
		if !hasColumn {
			continue
		}
		if _, err := s.GetWriteDB().ExecContext(ctx, "UPDATE "+table+" SET disable_long_context_pricing = 1"); err != nil {
			return fmt.Errorf("failed to disable Long context pricing for legacy %s rows: %w", table, err)
		}
	}

	return s.recordSchemaVersion(ctx, longContextPricingDefaultMigrationVersion, "凭证分组 Long context 计费默认关闭")
}

const claudeLongContextRemovalMigrationVersion = "20260821_remove_claude_long_context_pricing"

// migrateClaudeLongContextRemoval 强制关闭全部 Claude 凭证分组的 Long context 计费。
//
// Claude 侧已移除该功能的配置入口，但 20260712 那次「默认关闭」迁移是一次性的，
// 在它之后被用户手动开启过的分组仍为 0。若不补这一刀，前端看不到开关、
// tracker.go:557 的闸门却仍然打开，会形成看不见却在生效的隐蔽计费。
// 仅处理 claude_credential_groups；Codex 仍保留该能力，不得波及。
func (s *SQLiteAdapter) migrateClaudeLongContextRemoval(ctx context.Context) error {
	applied, err := s.isSchemaVersionApplied(ctx, claudeLongContextRemovalMigrationVersion)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	exists, err := s.tableExists(ctx, "claude_credential_groups")
	if err != nil {
		return fmt.Errorf("failed to check claude_credential_groups for Long context removal: %w", err)
	}
	if exists {
		hasColumn, err := s.columnExists(ctx, "claude_credential_groups", "disable_long_context_pricing")
		if err != nil {
			return fmt.Errorf("failed to check claude_credential_groups.disable_long_context_pricing: %w", err)
		}
		if hasColumn {
			if _, err := s.GetWriteDB().ExecContext(ctx,
				"UPDATE claude_credential_groups SET disable_long_context_pricing = 1 WHERE disable_long_context_pricing != 1"); err != nil {
				return fmt.Errorf("failed to disable Long context pricing for Claude credential groups: %w", err)
			}
		}
	}

	return s.recordSchemaVersion(ctx, claudeLongContextRemovalMigrationVersion, "Claude 凭证分组移除 Long context 计费")
}

func (s *SQLiteAdapter) ensureEndpointsUniqueByCredentialGroup(ctx context.Context) error {
	tableExists, err := s.tableExists(ctx, "endpoints")
	if err != nil {
		return fmt.Errorf("failed to check table endpoints: %w", err)
	}
	if !tableExists {
		return nil
	}

	hasLegacyUniqueOnName, hasLegacyUniqueOnChannelName, hasDesiredUnique, err := s.detectEndpointsUniqueIndexes(ctx)
	if err != nil {
		return err
	}
	if hasDesiredUnique && !hasLegacyUniqueOnName && !hasLegacyUniqueOnChannelName {
		return nil
	}

	// 若存在旧 UNIQUE(name)/(channel,name)，必须重建表，否则不同分组同名仍会被旧约束拦住。
	if hasLegacyUniqueOnName || hasLegacyUniqueOnChannelName {
		s.logger.Info("🔧 [数据库迁移] endpoints：检测到旧唯一约束，将重建为凭证分组作用域唯一")
		return s.rebuildEndpointsTableForCredentialGroupScopedUniq(ctx)
	}

	return s.createEndpointCredentialGroupIndexes(ctx, s.GetWriteDB())
}

func (s *SQLiteAdapter) detectEndpointsUniqueIndexes(ctx context.Context) (hasLegacyUniqueOnName bool, hasLegacyUniqueOnChannelName bool, hasDesiredUnique bool, _ error) {
	rows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_list(endpoints)")
	if err != nil {
		return false, false, false, fmt.Errorf("failed to query endpoints indexes: %w", err)
	}
	// 注意：database/sql 在 *sql.Rows 未 Close 前会占用连接。
	// SQLite 连接池通常限制为 1（见 Open: SetMaxOpenConns(1)），如果在遍历 rows 时再发起嵌套 Query，
	// 会因拿不到新连接而阻塞，最终触发 ctx 超时（表现为 context deadline exceeded）。
	// 因此这里先把需要的索引名读出来，再逐个查询 index_info。

	type idx struct {
		name string
	}
	uniqueIndexes := make([]idx, 0, 4)
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return false, false, false, fmt.Errorf("failed to scan endpoints index: %w", err)
		}
		if unique != 1 {
			continue
		}
		uniqueIndexes = append(uniqueIndexes, idx{name: name})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, false, false, fmt.Errorf("failed to iterate endpoints index_list: %w", err)
	}
	rows.Close()

	for _, it := range uniqueIndexes {
		// index 名来自 sqlite_master/pragma 输出，但仍做简单转义避免拼接问题
		escaped := strings.ReplaceAll(it.name, "'", "''")
		colRows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_info('"+escaped+"')")
		if err != nil {
			return false, false, false, fmt.Errorf("failed to query endpoints index_info(%s): %w", it.name, err)
		}

		cols := make([]string, 0, 2)
		for colRows.Next() {
			var seqno, cid int
			var colName string
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				colRows.Close()
				return false, false, false, fmt.Errorf("failed to scan endpoints index_info(%s): %w", it.name, err)
			}
			cols = append(cols, colName)
		}
		colRows.Close()

		if len(cols) == 1 && cols[0] == "name" {
			hasLegacyUniqueOnName = true
		}
		if len(cols) == 2 && cols[0] == "channel" && cols[1] == "name" {
			if it.name != "idx_endpoints_unique_legacy_name" {
				hasLegacyUniqueOnChannelName = true
			}
		}
		if len(cols) == 3 && cols[0] == "channel" && cols[1] == "credential_group_id" && cols[2] == "name" {
			hasDesiredUnique = true
		}
	}

	return hasLegacyUniqueOnName, hasLegacyUniqueOnChannelName, hasDesiredUnique, nil
}

func (s *SQLiteAdapter) rebuildEndpointsTableForCredentialGroupScopedUniq(ctx context.Context) error {
	// 注意：该迁移会锁表并重建 endpoints，但表体量通常较小（配置表），可接受。
	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin endpoints rebuild tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 清理旧触发器（表重建前先删，避免名字冲突）
	_, _ = tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS update_endpoints_timestamp")

	// 重命名旧表
	if _, err := tx.ExecContext(ctx, "ALTER TABLE endpoints RENAME TO endpoints_old"); err != nil {
		return fmt.Errorf("failed to rename endpoints to endpoints_old: %w", err)
	}

	// 创建新表（与 schema.sql 保持一致）
	createSQL := `
CREATE TABLE endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    channel TEXT NOT NULL,
    credential_group_id INTEGER REFERENCES claude_credential_groups(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL,

    token TEXT,
    api_key TEXT,
    headers TEXT,

    priority INTEGER DEFAULT 1,
    failover_enabled INTEGER DEFAULT 1,
    cooldown_seconds INTEGER,
    timeout_seconds INTEGER DEFAULT 300,

    supports_count_tokens INTEGER DEFAULT 0,
    skip_health_check INTEGER DEFAULT 0,

    cost_multiplier REAL DEFAULT 1.0,
    input_cost_multiplier REAL DEFAULT 1.0,
    output_cost_multiplier REAL DEFAULT 1.0,
    cache_creation_cost_multiplier REAL DEFAULT 1.0,
    cache_creation_cost_multiplier_1h REAL DEFAULT 1.0,
    cache_read_cost_multiplier REAL DEFAULT 1.0,
    model_multipliers TEXT NOT NULL DEFAULT '[]',

    enabled INTEGER DEFAULT 1,

    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);`
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create new endpoints table: %w", err)
	}

	// 复制数据（保留原 id）
	copySQL := `
INSERT INTO endpoints (
    id, channel, credential_group_id, name, url, token, api_key, headers,
    priority, failover_enabled, cooldown_seconds, timeout_seconds,
    supports_count_tokens, skip_health_check,
    cost_multiplier, input_cost_multiplier, output_cost_multiplier,
    cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h, cache_read_cost_multiplier,
    model_multipliers,
    enabled, created_at, updated_at
)
SELECT
    id, channel, COALESCE(credential_group_id, NULL), name, url, token, api_key, headers,
    priority, failover_enabled, cooldown_seconds, timeout_seconds,
    supports_count_tokens, skip_health_check,
    cost_multiplier, input_cost_multiplier, output_cost_multiplier,
    cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h, cache_read_cost_multiplier,
    COALESCE(model_multipliers, '[]'),
    enabled, created_at, updated_at
FROM endpoints_old;`
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("failed to copy endpoints data: %w", err)
	}

	// 重建索引
	if err := s.createEndpointCredentialGroupIndexes(ctx, tx); err != nil {
		return err
	}

	// 重建触发器
	triggerSQL := `
CREATE TRIGGER IF NOT EXISTS update_endpoints_timestamp
    AFTER UPDATE ON endpoints
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE endpoints SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;`
	if _, err := tx.ExecContext(ctx, triggerSQL); err != nil {
		return fmt.Errorf("failed to recreate endpoints trigger: %w", err)
	}

	// 删除旧表
	if _, err := tx.ExecContext(ctx, "DROP TABLE endpoints_old"); err != nil {
		return fmt.Errorf("failed to drop endpoints_old: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit endpoints rebuild: %w", err)
	}
	s.logger.Info("✅ [数据库迁移] endpoints：已重建为凭证分组作用域唯一")
	return nil
}

// modelPricingNeedsProviderTierRebuild 检测 model_pricing 是否仍是「model_name 单列唯一」
// 的老结构。老结构有两处硬伤：
//   - Claude 与 Codex 无法配置同名模型的不同定价，而两侧模型名空间天然会重叠
//     （同一中转商常同时提供两种协议；Claude provider 下已存在 MiniMax-M2 这类非 Anthropic 模型）
//   - 无法为同一模型配置多个服务档位的定价行
func (s *SQLiteAdapter) modelPricingNeedsProviderTierRebuild(ctx context.Context) (bool, error) {
	// 见 detectEndpointsUniqueIndexes 的说明：SQLite 连接池限制为 1 条连接，
	// 必须先把索引名读完再逐个查 index_info，不能在遍历 rows 时嵌套查询。
	rows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_list(model_pricing)")
	if err != nil {
		return false, fmt.Errorf("failed to query model_pricing indexes: %w", err)
	}
	uniqueIndexes := make([]string, 0, 4)
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return false, fmt.Errorf("failed to scan model_pricing index: %w", err)
		}
		if unique == 1 {
			uniqueIndexes = append(uniqueIndexes, name)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("failed to iterate model_pricing index_list: %w", err)
	}
	rows.Close()

	for _, name := range uniqueIndexes {
		escaped := strings.ReplaceAll(name, "'", "''")
		colRows, err := s.GetReadDB().QueryContext(ctx, "PRAGMA index_info('"+escaped+"')")
		if err != nil {
			return false, fmt.Errorf("failed to query model_pricing index_info(%s): %w", name, err)
		}
		cols := make([]string, 0, 3)
		for colRows.Next() {
			var (
				seqno   int
				cid     int
				colName sql.NullString
			)
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				colRows.Close()
				return false, fmt.Errorf("failed to scan model_pricing index_info(%s): %w", name, err)
			}
			cols = append(cols, colName.String)
		}
		colRows.Close()

		if len(cols) == 3 && cols[0] == "provider" && cols[1] == "model_name" && cols[2] == "service_tier" {
			return false, nil
		}
	}
	return true, nil
}

// rebuildModelPricingForProviderTierUnique 把 model_pricing 的唯一约束从 model_name
// 单列重建为 (provider, model_name, service_tier) 三元组。
//
// service_tier 为空串的行是标准档/兜底行，现有数据全部落入该行，因此是零数据迁移。
// SQLite 无法直接删除列上的 UNIQUE 约束，只能重建表。
func (s *SQLiteAdapter) rebuildModelPricingForProviderTierUnique(ctx context.Context) error {
	exists, err := s.tableExists(ctx, "model_pricing")
	if err != nil {
		return fmt.Errorf("failed to check model_pricing before rebuild: %w", err)
	}
	if !exists {
		return nil
	}

	needs, err := s.modelPricingNeedsProviderTierRebuild(ctx)
	if err != nil {
		return err
	}
	if !needs {
		return nil
	}

	// service_tier 列由早期迁移引入，之后从 schema.sql 的建表语句里被移除，
	// 因此老库可能有该列（值全为空）也可能没有，复制数据时要分别处理。
	hasServiceTier, err := s.columnExists(ctx, "model_pricing", "service_tier")
	if err != nil {
		return fmt.Errorf("failed to check model_pricing.service_tier: %w", err)
	}
	serviceTierExpr := "''"
	if hasServiceTier {
		serviceTierExpr = "COALESCE(service_tier, '')"
	}

	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin model_pricing rebuild tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`DROP TRIGGER IF EXISTS update_model_pricing_timestamp`,
		`DROP INDEX IF EXISTS idx_model_pricing_model_name`,
		`DROP INDEX IF EXISTS idx_model_pricing_is_default`,
		`DROP INDEX IF EXISTS idx_model_pricing_provider`,
		`ALTER TABLE model_pricing RENAME TO model_pricing_old`,
		// 与 schema.sql 保持一致
		`CREATE TABLE model_pricing (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_name TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT 'claude',
			service_tier TEXT NOT NULL DEFAULT '',
			input_price REAL NOT NULL DEFAULT 3.0,
			output_price REAL NOT NULL DEFAULT 15.0,
			cache_creation_price_5m REAL DEFAULT 3.75,
			cache_creation_price_1h REAL DEFAULT 6.0,
			cache_read_price REAL DEFAULT 0.30,
			long_context_threshold INTEGER NOT NULL DEFAULT 0,
			long_context_input_price REAL NOT NULL DEFAULT 0,
			long_context_cache_read_price REAL NOT NULL DEFAULT 0,
			long_context_cache_write_price REAL NOT NULL DEFAULT 0,
			long_context_output_price REAL NOT NULL DEFAULT 0,
			display_name TEXT,
			description TEXT,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
			UNIQUE(provider, model_name, service_tier)
		)`,
		`INSERT INTO model_pricing (
			id, model_name, provider, service_tier,
			input_price, output_price, cache_creation_price_5m, cache_creation_price_1h, cache_read_price,
			long_context_threshold, long_context_input_price, long_context_cache_read_price,
			long_context_cache_write_price, long_context_output_price,
			display_name, description, is_default, created_at, updated_at
		)
		SELECT
			id, model_name, COALESCE(provider, 'claude'), ` + serviceTierExpr + `,
			input_price, output_price, cache_creation_price_5m, cache_creation_price_1h, cache_read_price,
			long_context_threshold, long_context_input_price, long_context_cache_read_price,
			long_context_cache_write_price, long_context_output_price,
			display_name, description, is_default, created_at, updated_at
		FROM model_pricing_old`,
		`DROP TABLE model_pricing_old`,
		`CREATE INDEX IF NOT EXISTS idx_model_pricing_model_name ON model_pricing(model_name)`,
		`CREATE INDEX IF NOT EXISTS idx_model_pricing_is_default ON model_pricing(is_default)`,
		`CREATE INDEX IF NOT EXISTS idx_model_pricing_provider ON model_pricing(provider)`,
		`CREATE TRIGGER IF NOT EXISTS update_model_pricing_timestamp
			AFTER UPDATE ON model_pricing
			FOR EACH ROW
			WHEN NEW.updated_at = OLD.updated_at
		BEGIN
			UPDATE model_pricing SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
		END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("failed to rebuild model_pricing: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit model_pricing rebuild: %w", err)
	}

	s.logger.Info("✅ [数据库迁移] model_pricing：唯一约束已重建为 (provider, model_name, service_tier)")
	return nil
}

func (s *SQLiteAdapter) createEndpointCredentialGroupIndexes(ctx context.Context, executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}) error {
	indexSQL := []string{
		"CREATE INDEX IF NOT EXISTS idx_endpoints_channel ON endpoints(channel)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_credential_group ON endpoints(credential_group_id)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_priority ON endpoints(priority)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_enabled ON endpoints(enabled)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_failover ON endpoints(failover_enabled)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_unique_group_name ON endpoints(channel, credential_group_id, name) WHERE credential_group_id IS NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_unique_legacy_name ON endpoints(channel, name) WHERE credential_group_id IS NULL",
	}
	for _, stmt := range indexSQL {
		if _, err := executor.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to create endpoints credential group index: %w", err)
		}
	}
	return nil
}

func (s *SQLiteAdapter) ensureClaudeCredentialGroupsSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS claude_credential_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel TEXT NOT NULL,
    name TEXT NOT NULL,
    token TEXT NOT NULL DEFAULT '',
    api_key TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0,
    daily_cost_limit_usd REAL NOT NULL DEFAULT 0,
    model_policy TEXT NOT NULL DEFAULT 'inherit',
    model_mapping TEXT NOT NULL DEFAULT '{}',
    supported_models TEXT NOT NULL DEFAULT '[]',
    cost_multiplier REAL NOT NULL DEFAULT 1.0,
    input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_creation_cost_multiplier_1h REAL NOT NULL DEFAULT 1.0,
    cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    disable_long_context_pricing INTEGER NOT NULL DEFAULT 1,
    model_multipliers TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    UNIQUE(channel, name)
);
CREATE INDEX IF NOT EXISTS idx_claude_credential_groups_channel ON claude_credential_groups(channel);
CREATE UNIQUE INDEX IF NOT EXISTS idx_claude_credential_groups_single_default
    ON claude_credential_groups(channel, is_default) WHERE is_default = 1;
CREATE TRIGGER IF NOT EXISTS update_claude_credential_groups_timestamp
    AFTER UPDATE ON claude_credential_groups
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE claude_credential_groups SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;`
	if _, err := s.GetWriteDB().ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("failed to ensure claude_credential_groups schema: %w", err)
	}
	hasModelMultipliers, err := s.columnExists(ctx, "claude_credential_groups", "model_multipliers")
	if err != nil {
		return fmt.Errorf("failed to check claude_credential_groups.model_multipliers: %w", err)
	}
	if !hasModelMultipliers {
		if _, err := s.GetWriteDB().ExecContext(ctx, `ALTER TABLE claude_credential_groups ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("failed to add claude_credential_groups.model_multipliers: %w", err)
		}
	}
	for _, migration := range []struct {
		column string
		ddl    string
	}{
		{"daily_cost_limit_usd", `ALTER TABLE claude_credential_groups ADD COLUMN daily_cost_limit_usd REAL NOT NULL DEFAULT 0`},
		{"model_policy", `ALTER TABLE claude_credential_groups ADD COLUMN model_policy TEXT NOT NULL DEFAULT 'inherit'`},
		{"model_mapping", `ALTER TABLE claude_credential_groups ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'`},
		{"supported_models", `ALTER TABLE claude_credential_groups ADD COLUMN supported_models TEXT NOT NULL DEFAULT '[]'`},
		{"long_context_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_input_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_output_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_cache_creation_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"long_context_cache_read_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"disable_long_context_pricing", `ALTER TABLE claude_credential_groups ADD COLUMN disable_long_context_pricing INTEGER NOT NULL DEFAULT 1`},
		{"disable_service_tier_pricing", `ALTER TABLE claude_credential_groups ADD COLUMN disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1`},
		{"service_tier_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"service_tier_input_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_input_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"service_tier_output_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_output_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
		{"service_tier_cache_read_cost_multiplier", `ALTER TABLE claude_credential_groups ADD COLUMN service_tier_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0`},
	} {
		exists, err := s.columnExists(ctx, "claude_credential_groups", migration.column)
		if err != nil {
			return fmt.Errorf("failed to check claude_credential_groups.%s: %w", migration.column, err)
		}
		if !exists {
			if _, err := s.GetWriteDB().ExecContext(ctx, migration.ddl); err != nil {
				return fmt.Errorf("failed to add claude_credential_groups.%s: %w", migration.column, err)
			}
		}
	}
	return claudestore.EnsureClaudeSubscriptionSchema(ctx, s.GetWriteDB())
}

func (s *SQLiteAdapter) backfillClaudeCredentialGroups(ctx context.Context) error {
	endpointsExists, err := s.tableExists(ctx, "endpoints")
	if err != nil {
		return err
	}
	if !endpointsExists {
		return nil
	}

	rows, err := s.GetWriteDB().QueryContext(ctx, `SELECT DISTINCT channel FROM endpoints WHERE channel <> '' ORDER BY channel ASC`)
	if err != nil {
		return fmt.Errorf("failed to query endpoint channels for credential group backfill: %w", err)
	}
	channels := make([]string, 0)
	for rows.Next() {
		var channel string
		if err := rows.Scan(&channel); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan endpoint channel: %w", err)
		}
		channel = strings.TrimSpace(channel)
		if channel != "" {
			channels = append(channels, channel)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to iterate endpoint channels: %w", err)
	}
	rows.Close()

	for _, channel := range channels {
		groupID, err := s.ensureDefaultClaudeCredentialGroup(ctx, channel)
		if err != nil {
			return err
		}
		if groupID <= 0 {
			continue
		}
		if _, err := s.GetWriteDB().ExecContext(ctx, `
			UPDATE claude_credential_groups
			SET model_multipliers = (
				SELECT COALESCE(ep.model_multipliers, '[]')
				FROM endpoints ep
				WHERE ep.channel = claude_credential_groups.channel
					AND (ep.credential_group_id = claude_credential_groups.id OR ep.credential_group_id IS NULL OR ep.credential_group_id = 0)
					AND COALESCE(ep.model_multipliers, '[]') NOT IN ('', '[]')
				ORDER BY COALESCE(ep.priority, 1) ASC, COALESCE(ep.created_at, '') ASC, ep.id ASC
				LIMIT 1
			)
			WHERE id = ?
				AND COALESCE(model_multipliers, '[]') IN ('', '[]')
				AND EXISTS (
					SELECT 1
					FROM endpoints ep
					WHERE ep.channel = claude_credential_groups.channel
						AND (ep.credential_group_id = claude_credential_groups.id OR ep.credential_group_id IS NULL OR ep.credential_group_id = 0)
						AND COALESCE(ep.model_multipliers, '[]') NOT IN ('', '[]')
				)
		`, groupID); err != nil {
			return fmt.Errorf("failed to backfill Claude credential group model multipliers for channel %s: %w", channel, err)
		}
		if _, err := s.GetWriteDB().ExecContext(ctx, `
			UPDATE endpoints
			SET credential_group_id = ?
			WHERE channel = ? AND (credential_group_id IS NULL OR credential_group_id = 0)
		`, groupID, channel); err != nil {
			return fmt.Errorf("failed to backfill endpoints credential_group_id for channel %s: %w", channel, err)
		}
	}
	return nil
}

func (s *SQLiteAdapter) ensureDefaultClaudeCredentialGroup(ctx context.Context, channel string) (int64, error) {
	var groupID int64
	err := s.GetWriteDB().QueryRowContext(ctx, `
		SELECT id FROM claude_credential_groups
		WHERE channel = ? AND is_default = 1
		LIMIT 1
	`, channel).Scan(&groupID)
	if err == nil {
		return groupID, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query default Claude credential group: %w", err)
	}

	var (
		token                       string
		apiKey                      string
		costMultiplier              float64
		inputCostMultiplier         float64
		outputCostMultiplier        float64
		cacheCreationCostMultiplier float64
		cacheCreationMultiplier1h   float64
		cacheReadCostMultiplier     float64
		modelMultipliers            string
	)
	err = s.GetWriteDB().QueryRowContext(ctx, `
		SELECT COALESCE(token, ''), COALESCE(api_key, ''),
			COALESCE(cost_multiplier, 1.0), COALESCE(input_cost_multiplier, 1.0),
			COALESCE(output_cost_multiplier, 1.0), COALESCE(cache_creation_cost_multiplier, 1.0),
			COALESCE(cache_creation_cost_multiplier_1h, 1.0), COALESCE(cache_read_cost_multiplier, 1.0),
			COALESCE(model_multipliers, '[]')
		FROM endpoints
		WHERE channel = ?
		ORDER BY COALESCE(priority, 1) ASC, COALESCE(created_at, '') ASC, id ASC
		LIMIT 1
	`, channel).Scan(
		&token,
		&apiKey,
		&costMultiplier,
		&inputCostMultiplier,
		&outputCostMultiplier,
		&cacheCreationCostMultiplier,
		&cacheCreationMultiplier1h,
		&cacheReadCostMultiplier,
		&modelMultipliers,
	)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query endpoint seed for Claude credential group: %w", err)
	}

	result, err := s.GetWriteDB().ExecContext(ctx, `
		INSERT OR IGNORE INTO claude_credential_groups (
			channel, name, token, api_key, is_default, cost_multiplier,
			input_cost_multiplier, output_cost_multiplier,
			cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h,
			cache_read_cost_multiplier, disable_long_context_pricing, model_multipliers
		) VALUES (?, '默认分组', ?, ?, 1, ?, ?, ?, ?, ?, ?, 1, ?)
	`, channel, token, apiKey,
		defaultSQLiteMigrationMultiplier(costMultiplier),
		defaultSQLiteMigrationMultiplier(inputCostMultiplier),
		defaultSQLiteMigrationMultiplier(outputCostMultiplier),
		defaultSQLiteMigrationMultiplier(cacheCreationCostMultiplier),
		defaultSQLiteMigrationMultiplier(cacheCreationMultiplier1h),
		defaultSQLiteMigrationMultiplier(cacheReadCostMultiplier),
		modelMultipliers)
	if err != nil {
		return 0, fmt.Errorf("failed to create default Claude credential group: %w", err)
	}
	if id, err := result.LastInsertId(); err == nil && id > 0 {
		return id, nil
	}

	if err := s.GetWriteDB().QueryRowContext(ctx, `
		SELECT id FROM claude_credential_groups
		WHERE channel = ? AND is_default = 1
		LIMIT 1
	`, channel).Scan(&groupID); err != nil {
		return 0, fmt.Errorf("failed to load default Claude credential group after insert: %w", err)
	}
	return groupID, nil
}

func defaultSQLiteMigrationMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

const schemaVersionRepairClaudeCredentialGroupOwnership = "20260825_repair_claude_credential_group_ownership"

// repairClaudeCredentialGroupOwnership 修复历史迁移残留：endpoints.credential_group_id 指向了其它渠道的凭证分组。
//
// 这种归属是非法状态（EndpointService 的创建/更新路径都会拒绝跨渠道分组），后果是端点游离在所有分组之外：
// UI 按 ListByChannel 渲染分组卡片时碰不到它，「关闭本分组所有端点参与转移」的批量操作漏掉它；
// credentialGroupOrder 又把它排到全部分组之后，于是它总在渠道内端点耗尽后被当作最后一批重试，
// 表现为「参与转移开关关不掉」。
//
// 处理策略：孤儿端点归回本渠道默认分组；若默认分组内已存在同名或同 URL 的端点，说明它只是历史重影，直接删除。
// 每次启动检测，但只在命中非法数据时写入 —— 合法行永远不被触碰，因此不违反「不得覆盖用户显式保存值」的约定。
func (s *SQLiteAdapter) repairClaudeCredentialGroupOwnership(ctx context.Context) error {
	for _, table := range []string{"endpoints", "claude_credential_groups"} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check %s for Claude credential group ownership repair: %w", table, err)
		}
		if !exists {
			return nil
		}
	}

	type orphanEndpoint struct {
		id           int64
		channel      string
		name         string
		url          string
		groupID      int64
		groupChannel string
	}

	rows, err := s.GetReadDB().QueryContext(ctx, `
		SELECT e.id, e.channel, e.name, e.url, e.credential_group_id, g.channel
		FROM endpoints e
		JOIN claude_credential_groups g ON g.id = e.credential_group_id
		WHERE g.channel <> e.channel
		ORDER BY e.id ASC
	`)
	if err != nil {
		return fmt.Errorf("failed to query Claude endpoints with foreign credential groups: %w", err)
	}
	orphans := make([]orphanEndpoint, 0)
	for rows.Next() {
		var orphan orphanEndpoint
		if err := rows.Scan(&orphan.id, &orphan.channel, &orphan.name, &orphan.url, &orphan.groupID, &orphan.groupChannel); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan Claude endpoint with foreign credential group: %w", err)
		}
		orphans = append(orphans, orphan)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to iterate Claude endpoints with foreign credential groups: %w", err)
	}
	rows.Close()

	if len(orphans) == 0 {
		return nil
	}

	s.logger.Warn(fmt.Sprintf("🔧 [数据库迁移] 检测到 %d 个 Claude 端点的凭证分组归属跨渠道错配，开始修复", len(orphans)))
	backupPath, err := s.backupDatabaseFile(ctx, "pre-credential-group-repair")
	if err != nil {
		return fmt.Errorf("failed to back up database before Claude credential group ownership repair: %w", err)
	}
	if backupPath != "" {
		s.logger.Info(fmt.Sprintf("💾 [数据库迁移] 修复前已备份数据库: %s", backupPath))
	}

	var repaired, removed, skipped int
	for _, orphan := range orphans {
		var defaultGroupID int64
		err := s.GetReadDB().QueryRowContext(ctx, `
			SELECT id FROM claude_credential_groups
			WHERE channel = ? AND is_default = 1
			LIMIT 1
		`, orphan.channel).Scan(&defaultGroupID)
		if err == sql.ErrNoRows {
			skipped++
			s.logger.Warn(fmt.Sprintf("⚠️ [数据库迁移] 渠道 %s 没有默认凭证分组，端点 %s (id=%d) 的错配归属保持原样",
				orphan.channel, orphan.name, orphan.id))
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to load default Claude credential group for channel %s: %w", orphan.channel, err)
		}

		var conflictID int64
		var conflictName string
		err = s.GetReadDB().QueryRowContext(ctx, `
			SELECT id, name FROM endpoints
			WHERE channel = ? AND credential_group_id = ? AND (name = ? OR url = ?)
			LIMIT 1
		`, orphan.channel, defaultGroupID, orphan.name, orphan.url).Scan(&conflictID, &conflictName)
		switch {
		case err == nil:
			// 默认分组里已经有同名或同上游地址的端点，孤儿只是历史重影，删除它保留正主。
			if _, execErr := s.GetWriteDB().ExecContext(ctx, `DELETE FROM endpoints WHERE id = ?`, orphan.id); execErr != nil {
				return fmt.Errorf("failed to delete duplicated Claude endpoint %s (id=%d): %w", orphan.name, orphan.id, execErr)
			}
			removed++
			s.logger.Info(fmt.Sprintf("🗑️ [数据库迁移] 删除重影端点: 渠道=%s 端点=%s (id=%d, url=%s, 原分组=%d 属于 %s)，默认分组内已有 %s (id=%d)",
				orphan.channel, orphan.name, orphan.id, orphan.url, orphan.groupID, orphan.groupChannel, conflictName, conflictID))
		case err == sql.ErrNoRows:
			if _, execErr := s.GetWriteDB().ExecContext(ctx,
				`UPDATE endpoints SET credential_group_id = ? WHERE id = ?`, defaultGroupID, orphan.id); execErr != nil {
				return fmt.Errorf("failed to reassign Claude endpoint %s (id=%d) to its default credential group: %w", orphan.name, orphan.id, execErr)
			}
			repaired++
			s.logger.Info(fmt.Sprintf("🔁 [数据库迁移] 端点归位默认分组: 渠道=%s 端点=%s (id=%d)，分组 %d (属于 %s) → %d",
				orphan.channel, orphan.name, orphan.id, orphan.groupID, orphan.groupChannel, defaultGroupID))
		default:
			return fmt.Errorf("failed to check duplicated Claude endpoint %s in channel %s: %w", orphan.name, orphan.channel, err)
		}
	}

	s.logger.Info(fmt.Sprintf("✅ [数据库迁移] Claude 凭证分组归属修复完成：归位 %d 个，删除重影 %d 个，跳过 %d 个",
		repaired, removed, skipped))

	if err := s.recordSchemaVersion(ctx, schemaVersionRepairClaudeCredentialGroupOwnership,
		"repair Claude endpoints whose credential group belongs to another channel"); err != nil {
		return err
	}
	return nil
}

const schemaVersionRepairCrossProviderMultiplierCost = "20260902_repair_cross_provider_multiplier_cost"

// repairCrossProviderMultiplierCost 重算被跨 provider 倍率污染的 Claude 请求成本。
//
// 倍率缓存曾以 "channel::group::endpoint" 为 key，由 Claude 与 Codex 共用一张 map。
// 两侧允许配置同名的渠道 / 凭证分组 / 端点（实际存在 anyrouter::默认分组::主站直连
// 在两个 provider 下同时成立），而 app.go 先写 Claude 段再写 Codex 段，后者静默覆盖
// 前者，于是 Claude 请求按 Codex 的倍率计费。实测 685 条 Claude 请求按 0.4 计价，
// 成本少算 $551.96。key 加 provider 维度后新请求已正确，此处只清理历史数据。
//
// 修复口径：按当前 Claude 凭证分组配置算出每条记录应有的总倍率，与落库值不符时按比例
// 还原各成本列并回写正确倍率。成本 = 基础单价 × 分项倍率 × 总倍率，本缺陷只污染总倍率
// （受影响记录的六项分项倍率与模型倍率实测均为 1.0），因此比例还原是精确的，无需重查定价。
//
// 只执行一次（schema_versions 幂等）：历史记录必须反映请求发生时的倍率。若每次启动都
// 比对当前配置，用户日后调整分组倍率会把历史账单一并改写。
//
// 汇总表无需同步：成本统计直接聚合 request_logs（queries.go 的
// queryUsageStatsTotalsFromSource），凭证分组日成本也是实时计算的。
func (s *SQLiteAdapter) repairCrossProviderMultiplierCost(ctx context.Context) error {
	applied, err := s.isSchemaVersionApplied(ctx, schemaVersionRepairCrossProviderMultiplierCost)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	for _, table := range []string{"request_logs", "claude_credential_groups", "codex_channels", "codex_credential_groups"} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check %s for cross-provider multiplier repair: %w", table, err)
		}
		if !exists {
			// 表尚未建全（首次启动），不写标记，留待下次启动重判。
			return nil
		}
	}

	type pollutedRequest struct {
		id       int64
		recorded float64
		correct  float64
		cost     float64
	}

	// 判定由两个条件合成，缺一不可：
	//
	//  1. 落库倍率 ≠ 当前 Claude 分组配置 —— 必要但不充分。倍率是请求发生时的快照，
	//     用户新建分组或调整倍率后，早于该变更的记录本就与当前配置不同。实测存在这种
	//     记录（Right Code/aws 分组建立前的 7 条请求落库 1.0，建立后配置为 0.3），
	//     它们的 1.0 是当时的正确值，不能按今天的配置追溯改写。
	//
	//  2. 落库倍率 == 同名 Codex 渠道 + 分组的倍率 —— 这才是污染的指纹。旧 key 不含
	//     provider，Codex 段后写覆盖 Claude 段，因此被污染记录的倍率必然等于肇事方
	//     Codex 分组的倍率。反之，Claude 的 "Right Code" 在 Codex 侧叫 "RightCode"
	//     （无空格），渠道名对不上，那 7 条记录因此被自然排除。
	//
	// 该条件同时保证幂等：修正后落库倍率不再等于 Codex 侧的值。
	//
	// group_name 匹配不到 Claude 分组时（分组已删除），应有倍率取 1.0 —— 当时的真实
	// 倍率已无从考证，而 1.0 是配置缺失时计费链路本身的回退值。
	rows, err := s.GetReadDB().QueryContext(ctx, `
		SELECT r.id, r.cost_multiplier, COALESCE(g.cost_multiplier, 1.0), COALESCE(r.total_cost_usd, 0)
		FROM request_logs r
		LEFT JOIN claude_credential_groups g
			ON g.channel = r.channel AND g.name = r.group_name
		WHERE COALESCE(r.provider, 'claude') = 'claude'
			AND r.cost_multiplier > 0
			AND ABS(r.cost_multiplier - COALESCE(g.cost_multiplier, 1.0)) > 0.000000001
			AND EXISTS (
				SELECT 1
				FROM codex_credential_groups cg
				JOIN codex_channels cc ON cc.id = cg.channel_id
				WHERE cc.name = r.channel
					AND cg.name = r.group_name
					AND ABS(cg.cost_multiplier - r.cost_multiplier) < 0.000000001
			)
		ORDER BY r.id ASC
	`)
	if err != nil {
		return fmt.Errorf("failed to query Claude requests with foreign multipliers: %w", err)
	}
	polluted := make([]pollutedRequest, 0)
	for rows.Next() {
		var row pollutedRequest
		if err := rows.Scan(&row.id, &row.recorded, &row.correct, &row.cost); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan Claude request with foreign multiplier: %w", err)
		}
		polluted = append(polluted, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to iterate Claude requests with foreign multipliers: %w", err)
	}
	rows.Close()

	if len(polluted) == 0 {
		return s.recordSchemaVersion(ctx, schemaVersionRepairCrossProviderMultiplierCost,
			"recompute Claude request costs polluted by provider-less multiplier keys")
	}

	s.logger.Warn(fmt.Sprintf("🔧 [数据库迁移] 检测到 %d 条 Claude 请求套用了其它 provider 的倍率，开始重算成本", len(polluted)))
	backupPath, err := s.backupDatabaseFile(ctx, "pre-multiplier-repair")
	if err != nil {
		return fmt.Errorf("failed to back up database before cross-provider multiplier repair: %w", err)
	}
	if backupPath != "" {
		s.logger.Info(fmt.Sprintf("💾 [数据库迁移] 重算前已备份数据库: %s", backupPath))
	}

	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cross-provider multiplier repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// cost_multiplier 同时出现在赋值右侧与左侧：SQLite 对同一条 UPDATE 的所有右侧
	// 表达式统一取旧值求值，因此这里的比例基于污染值计算，是安全的。
	const updateSQL = `
		UPDATE request_logs SET
			input_cost_usd = COALESCE(input_cost_usd, 0) * ?,
			output_cost_usd = COALESCE(output_cost_usd, 0) * ?,
			cache_creation_cost_usd = COALESCE(cache_creation_cost_usd, 0) * ?,
			cache_creation_5m_cost_usd = COALESCE(cache_creation_5m_cost_usd, 0) * ?,
			cache_creation_1h_cost_usd = COALESCE(cache_creation_1h_cost_usd, 0) * ?,
			cache_read_cost_usd = COALESCE(cache_read_cost_usd, 0) * ?,
			total_cost_usd = COALESCE(total_cost_usd, 0) * ?,
			cost_multiplier = ?
		WHERE id = ?`

	var costBefore, costAfter float64
	for _, row := range polluted {
		ratio := row.correct / row.recorded
		if _, err := tx.ExecContext(ctx, updateSQL,
			ratio, ratio, ratio, ratio, ratio, ratio, ratio, row.correct, row.id); err != nil {
			return fmt.Errorf("failed to recompute cost for request %d: %w", row.id, err)
		}
		costBefore += row.cost
		costAfter += row.cost * ratio
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cross-provider multiplier repair: %w", err)
	}

	s.logger.Info(fmt.Sprintf("✅ [数据库迁移] 跨 provider 倍率污染重算完成：修正 %d 条，成本合计 $%.4f → $%.4f",
		len(polluted), costBefore, costAfter))

	return s.recordSchemaVersion(ctx, schemaVersionRepairCrossProviderMultiplierCost,
		"recompute Claude request costs polluted by provider-less multiplier keys")
}

// backupDatabaseFile 用 VACUUM INTO 生成当前库的一致快照，返回备份路径。
// 内存库或路径未配置时返回空字符串，表示无需备份。
func (s *SQLiteAdapter) backupDatabaseFile(ctx context.Context, tag string) (string, error) {
	dbPath := strings.TrimSpace(s.config.DatabasePath)
	if dbPath == "" || dbPath == ":memory:" {
		return "", nil
	}
	backupPath := fmt.Sprintf("%s.%s-%s.db", dbPath, tag, time.Now().Format("20060102-150405"))
	// VACUUM INTO 不支持占位符，路径需按 SQL 字符串字面量转义。
	quoted := strings.ReplaceAll(backupPath, "'", "''")
	if _, err := s.GetWriteDB().ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", quoted)); err != nil {
		return "", err
	}
	return backupPath, nil
}

func (s *SQLiteAdapter) tableExists(ctx context.Context, table string) (bool, error) {
	var count int
	if err := s.GetReadDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// columnExists 检查表中是否存在指定列
func (s *SQLiteAdapter) columnExists(ctx context.Context, table, column string) (bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", table)
	rows, err := s.GetReadDB().QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var dfltValue interface{}
		var pk int

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}

		if name == column {
			return true, nil
		}
	}

	return false, nil
}

// BuildInsertOrReplaceQuery 构建插入或更新查询（SQLite语法）
// 使用 INSERT ... ON CONFLICT DO UPDATE 来避免数据丢失
func (s *SQLiteAdapter) BuildInsertOrReplaceQuery(table string, columns []string, values []string) string {
	columnsStr := strings.Join(columns, ", ")
	valuesStr := strings.Join(values, ", ")

	// 构建INSERT部分
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, columnsStr, valuesStr)

	// 构建ON CONFLICT DO UPDATE部分，对start_time字段进行特殊处理
	// 对于request_logs表，主键冲突时更新提供的字段（除了request_id主键）
	var updatePairs []string
	for _, col := range columns {
		if col != "request_id" { // 跳过主键字段
			if col == "start_time" {
				// 对start_time使用COALESCE，只在原值为NULL时才更新
				updatePairs = append(updatePairs, fmt.Sprintf("%s = COALESCE(request_logs.%s, EXCLUDED.%s)", col, col, col))
			} else {
				updatePairs = append(updatePairs, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
			}
		}
	}

	if len(updatePairs) > 0 {
		query += " ON CONFLICT(request_id) DO UPDATE SET " + strings.Join(updatePairs, ", ")
	} else {
		// 如果只有request_id字段，则使用IGNORE避免重复插入
		query = fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES (%s)", table, columnsStr, valuesStr)
	}

	return query
}

// BuildDateTimeNow 返回当前时间函数（支持微秒精度）
// SQLite没有时区支持，我们在Go层面生成正确时区的时间字符串
func (s *SQLiteAdapter) BuildDateTimeNow() string {
	// 获取当前配置时区的时间
	now := time.Now().In(s.location)

	// 格式化为SQLite兼容的datetime格式（微秒精度）
	return fmt.Sprintf("'%s'", now.Format("2006-01-02 15:04:05.000000"))
}

// BuildLimitOffset 构建分页查询
func (s *SQLiteAdapter) BuildLimitOffset(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	if offset <= 0 {
		return fmt.Sprintf(" LIMIT %d", limit)
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}

// VacuumDatabase SQLite执行VACUUM操作
func (s *SQLiteAdapter) VacuumDatabase(ctx context.Context) error {
	s.logger.Info("正在执行SQLite VACUUM操作")

	_, err := s.GetWriteDB().ExecContext(ctx, "VACUUM")
	if err != nil {
		return fmt.Errorf("failed to vacuum SQLite database: %w", err)
	}

	s.logger.Info("✅ SQLite VACUUM操作完成")
	return nil
}

// GetDatabaseStats 获取SQLite数据库统计信息
func (s *SQLiteAdapter) GetDatabaseStats(ctx context.Context) (*DatabaseStats, error) {
	stats := &DatabaseStats{}

	// 获取请求记录总数
	err := s.GetReadDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs").Scan(&stats.TotalRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to get total requests count: %w", err)
	}

	// 获取汇总记录总数
	err = s.GetReadDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_summary").Scan(&stats.TotalSummaries)
	if err != nil {
		return nil, fmt.Errorf("failed to get total summaries count: %w", err)
	}

	// 获取最早和最新的记录时间
	var earliestStr, latestStr sql.NullString
	err = s.GetReadDB().QueryRowContext(ctx, "SELECT MIN(start_time), MAX(start_time) FROM request_logs").Scan(&earliestStr, &latestStr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get record time range: %w", err)
	}

	if earliestStr.Valid {
		if t, err := time.Parse(time.RFC3339, earliestStr.String); err == nil {
			stats.EarliestRecord = &t
		}
	}
	if latestStr.Valid {
		if t, err := time.Parse(time.RFC3339, latestStr.String); err == nil {
			stats.LatestRecord = &t
		}
	}

	// 获取数据库文件大小（SQLite特有）
	var pageCount, pageSize int64
	err = s.GetReadDB().QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)
	if err == nil {
		err = s.GetReadDB().QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize)
		if err == nil {
			stats.DatabaseSize = pageCount * pageSize
		}
	}

	// 获取总成本
	err = s.GetReadDB().QueryRowContext(ctx, "SELECT COALESCE(SUM(total_cost_usd), 0) FROM request_logs WHERE total_cost_usd > 0").Scan(&stats.TotalCostUSD)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get total cost: %w", err)
	}

	return stats, nil
}

// GetConnectionStats 获取连接池统计信息
func (s *SQLiteAdapter) GetConnectionStats() ConnectionStats {
	if s.readDB == nil && s.writeDB == nil {
		return ConnectionStats{}
	}

	merge := func(dst *ConnectionStats, stats sql.DBStats) {
		dst.OpenConnections += stats.OpenConnections
		dst.IdleConnections += stats.Idle
		dst.InUseConnections += stats.InUse
		dst.WaitCount += stats.WaitCount
		dst.WaitDuration += stats.WaitDuration
	}

	var result ConnectionStats
	if s.readDB != nil {
		merge(&result, s.readDB.Stats())
	}
	if s.writeDB != nil {
		merge(&result, s.writeDB.Stats())
	}
	return result
}

func (s *SQLiteAdapter) buildSQLiteDSN(dbPath string) string {
	basePath := dbPath
	if dbPath == ":memory:" {
		basePath = fmt.Sprintf("file:tracking-%p?mode=memory&cache=shared", s)
	}

	separator := "?"
	if strings.Contains(basePath, "?") {
		separator = "&"
	}

	// modernc accepts repeated _pragma parameters; the sqlite3-style options
	// previously used here were ignored, leaving DELETE journaling and no wait.
	// Keep the lock wait below the management APIs' 10-second request budget.
	// Foreign-key enforcement stays unchanged; enabling it needs a legacy-data
	// compatibility review independent of this connection-lifecycle fix.
	return basePath + separator + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(10000)"
}

// diagnoseTimezoneSettings 诊断SQLite时区设置，帮助调试时区不一致问题
func (s *SQLiteAdapter) diagnoseTimezoneSettings() {
	// SQLite时区诊断相对简单，因为我们在应用层处理时区
	goNow := time.Now()
	goInConfigTZ := time.Now().In(s.location)

	_, goOffset := goInConfigTZ.Zone()
	goOffsetHours := float64(goOffset) / 3600

	s.logger.Info("🔍 SQLite时区诊断信息",
		"configured_timezone", s.location.String(),
		"system_now", goNow.Format("2006-01-02 15:04:05 -07:00"),
		"configured_tz_now", goInConfigTZ.Format("2006-01-02 15:04:05 -07:00"),
		"configured_offset_hours", goOffsetHours,
		"builddatetimenow_output", s.BuildDateTimeNow())

	// 验证时区偏移是否符合预期
	if s.location.String() == "Asia/Shanghai" && goOffsetHours == 8.0 {
		s.logger.Info("✅ SQLite时区设置正确: 使用Asia/Shanghai时区 (+8小时)")
	} else if s.location == time.UTC {
		s.logger.Info("ℹ️  SQLite使用UTC时区")
	} else {
		s.logger.Info("ℹ️  SQLite使用自定义时区", "timezone", s.location.String(), "offset_hours", goOffsetHours)
	}
}

// getSQLiteAppDataDir 获取应用数据目录（跨平台）
// 复制自 internal/utils/appdir.go，避免循环依赖
// Windows: %APPDATA%\Claude-Request-Forwarder
// macOS: ~/Library/Application Support/Claude-Request-Forwarder
// Linux: ~/.local/share/ai-relay-forwarder
func getSQLiteAppDataDir() string {
	var baseDir string

	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			baseDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(baseDir, "Claude-Request-Forwarder")

	case "darwin":
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "Library", "Application Support", "Claude-Request-Forwarder")

	case "linux":
		homeDir, _ := os.UserHomeDir()
		xdgDataHome := os.Getenv("XDG_DATA_HOME")
		if xdgDataHome != "" {
			return filepath.Join(xdgDataHome, "ai-relay-forwarder")
		}
		return filepath.Join(homeDir, ".local", "share", "ai-relay-forwarder")

	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".ai-relay-forwarder")
	}
}
