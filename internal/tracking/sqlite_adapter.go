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

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var sqliteSchemaFS embed.FS

// SQLiteAdapter SQLite数据库适配器实现（保持原有逻辑）
type SQLiteAdapter struct {
	config   DatabaseConfig
	db       *sql.DB
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
		// Windows: %APPDATA%\CC-Forwarder\data\cc-forwarder.db
		// macOS: ~/Library/Application Support/CC-Forwarder/data/cc-forwarder.db
		// Linux: ~/.local/share/cc-forwarder/data/cc-forwarder.db
		dbPath = filepath.Join(getSQLiteAppDataDir(), "data", "cc-forwarder.db")
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

	// 构建SQLite连接字符串
	dsn := dbPath + "?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_foreign_keys=1&_busy_timeout=60000"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// 设置连接池参数（SQLite建议少量连接）
	db.SetMaxOpenConns(1) // SQLite写操作需要单一连接
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	s.db = db

	// 诊断时区设置
	s.diagnoseTimezoneSettings()

	s.logger.Info("✅ SQLite数据库连接成功")

	return nil
}

// Close 关闭数据库连接
func (s *SQLiteAdapter) Close() error {
	if s.db != nil {
		s.logger.Info("正在关闭SQLite数据库连接")
		return s.db.Close()
	}
	return nil
}

// Ping 测试数据库连接
func (s *SQLiteAdapter) Ping(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database not connected")
	}
	return s.db.PingContext(ctx)
}

// GetDB 获取数据库连接
func (s *SQLiteAdapter) GetDB() *sql.DB {
	return s.db
}

// GetReadDB 获取读数据库连接
func (s *SQLiteAdapter) GetReadDB() *sql.DB {
	return s.db
}

// GetWriteDB 获取写数据库连接
func (s *SQLiteAdapter) GetWriteDB() *sql.DB {
	return s.db
}

// BeginTx 开始事务
func (s *SQLiteAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	return s.db.BeginTx(ctx, opts)
}

// InitSchema 初始化SQLite数据库Schema
func (s *SQLiteAdapter) InitSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.logger.Info("正在初始化SQLite数据库Schema")

	// 读取并执行SQLite schema
	schema, err := sqliteSchemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema.sql: %w", err)
	}

	// SQLite可以直接执行整个schema。
	// 但：旧库可能缺少后续新增的列，schema.sql 中的索引/触发器可能会引用这些列，
	// 导致 Exec 直接失败并中断启动。为保证向后兼容：
	// - 先尝试执行 schema.sql
	// - 若因“no such column”失败，则先跑 migrateSchema 补列，再重试执行 schema.sql
	if _, err := s.db.ExecContext(ctx, string(schema)); err != nil {
		if strings.Contains(err.Error(), "no such column:") {
			s.logger.Warn("schema.sql 执行失败（缺少列），将先执行迁移后重试", "error", err)
			if err := s.migrateSchema(ctx); err != nil {
				return fmt.Errorf("failed to migrate schema (pre-schema retry): %w", err)
			}
			if _, err := s.db.ExecContext(ctx, string(schema)); err != nil {
				return fmt.Errorf("failed to execute schema (after migrate): %w", err)
			}
		} else {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	// v5.0.1+: 执行迁移添加新字段
	if err := s.migrateSchema(ctx); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	s.logger.Info("✅ SQLite数据库Schema初始化完成")
	return nil
}

// migrateSchema 执行数据库迁移（v5.0.1+: 添加 5m/1h 缓存字段）
func (s *SQLiteAdapter) migrateSchema(ctx context.Context) error {
	// request_logs 迁移：历史上先上线 usage tracking，后续补充缓存字段
	requestLogMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
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
	}

	// endpoints 迁移：端点存储表迭代新增字段时，需要兼容旧 db（CREATE TABLE IF NOT EXISTS 不会补列）
	endpointsMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
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
	}

	// channels 迁移：早期可能只有 name，后续新增 website
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
			if _, err := s.db.ExecContext(ctx, m.alterSQL); err != nil {
				return fmt.Errorf("failed to add column %s.%s: %w", table, m.checkColumn, err)
			}
			s.logger.Info(fmt.Sprintf("✅ [数据库迁移] %s：%s 添加成功", table, m.description))
		}
		return nil
	}

	if err := runMigrations("request_logs", requestLogMigrations); err != nil {
		return err
	}
	if err := runMigrations("endpoints", endpointsMigrations); err != nil {
		return err
	}
	// v6.2+: 允许不同渠道端点同名（约束从 name 全局唯一调整为 (channel,name) 渠道内唯一）。
	// SQLite 无法直接移除旧 UNIQUE(name) 约束，需要在发现旧约束时重建表。
	if err := s.ensureEndpointsUniqueByChannelAndName(ctx); err != nil {
		return err
	}
	if err := runMigrations("channels", channelMigrations); err != nil {
		return err
	}

	return nil
}

func (s *SQLiteAdapter) ensureEndpointsUniqueByChannelAndName(ctx context.Context) error {
	tableExists, err := s.tableExists(ctx, "endpoints")
	if err != nil {
		return fmt.Errorf("failed to check table endpoints: %w", err)
	}
	if !tableExists {
		return nil
	}

	hasLegacyUniqueOnName, hasDesiredUnique, err := s.detectEndpointsUniqueIndexes(ctx)
	if err != nil {
		return err
	}
	if hasDesiredUnique && !hasLegacyUniqueOnName {
		return nil
	}

	// 若存在旧 UNIQUE(name)，必须重建表，否则即使补一个新索引也仍会被旧约束拦住。
	if hasLegacyUniqueOnName {
		s.logger.Info("🔧 [数据库迁移] endpoints：检测到旧 UNIQUE(name)，将重建为 UNIQUE(channel,name)")
		return s.rebuildEndpointsTableForChannelScopedUniq(ctx)
	}

	// 没有旧约束但也没有新约束：补一个唯一索引即可。
	_, err = s.db.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_channel_name_unique ON endpoints(channel, name)")
	if err != nil {
		return fmt.Errorf("failed to create unique index endpoints(channel,name): %w", err)
	}
	return nil
}

func (s *SQLiteAdapter) detectEndpointsUniqueIndexes(ctx context.Context) (hasLegacyUniqueOnName bool, hasDesiredUnique bool, _ error) {
	rows, err := s.db.QueryContext(ctx, "PRAGMA index_list(endpoints)")
	if err != nil {
		return false, false, fmt.Errorf("failed to query endpoints indexes: %w", err)
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
			return false, false, fmt.Errorf("failed to scan endpoints index: %w", err)
		}
		if unique != 1 {
			continue
		}
		uniqueIndexes = append(uniqueIndexes, idx{name: name})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, false, fmt.Errorf("failed to iterate endpoints index_list: %w", err)
	}
	rows.Close()

	for _, it := range uniqueIndexes {
		// index 名来自 sqlite_master/pragma 输出，但仍做简单转义避免拼接问题
		escaped := strings.ReplaceAll(it.name, "'", "''")
		colRows, err := s.db.QueryContext(ctx, "PRAGMA index_info('"+escaped+"')")
		if err != nil {
			return false, false, fmt.Errorf("failed to query endpoints index_info(%s): %w", it.name, err)
		}

		cols := make([]string, 0, 2)
		for colRows.Next() {
			var seqno, cid int
			var colName string
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				colRows.Close()
				return false, false, fmt.Errorf("failed to scan endpoints index_info(%s): %w", it.name, err)
			}
			cols = append(cols, colName)
		}
		colRows.Close()

		if len(cols) == 1 && cols[0] == "name" {
			hasLegacyUniqueOnName = true
		}
		if len(cols) == 2 && cols[0] == "channel" && cols[1] == "name" {
			hasDesiredUnique = true
		}
	}

	return hasLegacyUniqueOnName, hasDesiredUnique, nil
}

func (s *SQLiteAdapter) rebuildEndpointsTableForChannelScopedUniq(ctx context.Context) error {
	// 注意：该迁移会锁表并重建 endpoints，但表体量通常较小（配置表），可接受。
	tx, err := s.db.BeginTx(ctx, nil)
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

    cost_multiplier REAL DEFAULT 1.0,
    input_cost_multiplier REAL DEFAULT 1.0,
    output_cost_multiplier REAL DEFAULT 1.0,
    cache_creation_cost_multiplier REAL DEFAULT 1.0,
    cache_creation_cost_multiplier_1h REAL DEFAULT 1.0,
    cache_read_cost_multiplier REAL DEFAULT 1.0,

    enabled INTEGER DEFAULT 1,

    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),

    UNIQUE(channel, name)
);`
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create new endpoints table: %w", err)
	}

	// 复制数据（保留原 id）
	copySQL := `
INSERT INTO endpoints (
    id, channel, name, url, token, api_key, headers,
    priority, failover_enabled, cooldown_seconds, timeout_seconds,
    supports_count_tokens,
    cost_multiplier, input_cost_multiplier, output_cost_multiplier,
    cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h, cache_read_cost_multiplier,
    enabled, created_at, updated_at
)
SELECT
    id, channel, name, url, token, api_key, headers,
    priority, failover_enabled, cooldown_seconds, timeout_seconds,
    supports_count_tokens,
    cost_multiplier, input_cost_multiplier, output_cost_multiplier,
    cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h, cache_read_cost_multiplier,
    enabled, created_at, updated_at
FROM endpoints_old;`
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("failed to copy endpoints data: %w", err)
	}

	// 重建索引
	indexSQL := []string{
		"CREATE INDEX IF NOT EXISTS idx_endpoints_channel ON endpoints(channel)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_priority ON endpoints(priority)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_enabled ON endpoints(enabled)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_failover ON endpoints(failover_enabled)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_channel_name_unique ON endpoints(channel, name)",
	}
	for _, stmt := range indexSQL {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to create endpoints index: %w", err)
		}
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
	s.logger.Info("✅ [数据库迁移] endpoints：已重建为 UNIQUE(channel,name)")
	return nil
}

func (s *SQLiteAdapter) tableExists(ctx context.Context, table string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// columnExists 检查表中是否存在指定列
func (s *SQLiteAdapter) columnExists(ctx context.Context, table, column string) (bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", table)
	rows, err := s.db.QueryContext(ctx, query)
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

	_, err := s.db.ExecContext(ctx, "VACUUM")
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
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs").Scan(&stats.TotalRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to get total requests count: %w", err)
	}

	// 获取汇总记录总数
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_summary").Scan(&stats.TotalSummaries)
	if err != nil {
		return nil, fmt.Errorf("failed to get total summaries count: %w", err)
	}

	// 获取最早和最新的记录时间
	var earliestStr, latestStr sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT MIN(start_time), MAX(start_time) FROM request_logs").Scan(&earliestStr, &latestStr)
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
	err = s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)
	if err == nil {
		err = s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize)
		if err == nil {
			stats.DatabaseSize = pageCount * pageSize
		}
	}

	// 获取总成本
	err = s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_cost_usd), 0) FROM request_logs WHERE total_cost_usd > 0").Scan(&stats.TotalCostUSD)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get total cost: %w", err)
	}

	return stats, nil
}

// GetConnectionStats 获取连接池统计信息
func (s *SQLiteAdapter) GetConnectionStats() ConnectionStats {
	if s.db == nil {
		return ConnectionStats{}
	}

	dbStats := s.db.Stats()
	return ConnectionStats{
		OpenConnections:  dbStats.OpenConnections,
		IdleConnections:  dbStats.Idle,
		InUseConnections: dbStats.InUse,
		WaitCount:        dbStats.WaitCount,
		WaitDuration:     dbStats.WaitDuration,
		MaxLifetime:      0, // SQLite不限制连接生命周期
	}
}

// GetDatabaseType 返回数据库类型标识
func (s *SQLiteAdapter) GetDatabaseType() string {
	return "sqlite"
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
// Windows: %APPDATA%\CC-Forwarder
// macOS: ~/Library/Application Support/CC-Forwarder
// Linux: ~/.local/share/cc-forwarder
func getSQLiteAppDataDir() string {
	var baseDir string

	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			baseDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(baseDir, "CC-Forwarder")

	case "darwin":
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "Library", "Application Support", "CC-Forwarder")

	case "linux":
		homeDir, _ := os.UserHomeDir()
		xdgDataHome := os.Getenv("XDG_DATA_HOME")
		if xdgDataHome != "" {
			return filepath.Join(xdgDataHome, "cc-forwarder")
		}
		return filepath.Join(homeDir, ".local", "share", "cc-forwarder")

	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".cc-forwarder")
	}
}
