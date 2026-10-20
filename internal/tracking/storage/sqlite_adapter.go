package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/appdir"

	_ "modernc.org/sqlite"
)

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
		// Windows: %APPDATA%\Vane\data\usage.db
		// macOS: ~/Library/Application Support/Vane/data/usage.db
		// Linux: ~/.local/share/Vane/data/usage.db
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

// getSQLiteAppDataDir uses the shared Vane directory and legacy fallback.
func getSQLiteAppDataDir() string { return appdir.Dir() }

// DatabasePath returns the configured path used by archive recovery files.
func (s *SQLiteAdapter) DatabasePath() string { return s.config.DatabasePath }
