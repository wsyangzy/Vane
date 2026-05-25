package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrorHandler handles errors and provides recovery mechanisms
type ErrorHandler struct {
	tracker *UsageTracker
	logger  *slog.Logger
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(tracker *UsageTracker, logger *slog.Logger) *ErrorHandler {
	return &ErrorHandler{
		tracker: tracker,
		logger:  logger,
	}
}

// HandleDatabaseError handles database-related errors with recovery attempts
func (eh *ErrorHandler) HandleDatabaseError(err error, operation string) bool {
	if err == nil {
		return true
	}

	eh.logger.Error("Database operation failed",
		"operation", operation,
		"error", err.Error())

	// 尝试诊断错误类型
	switch {
	case isDiskSpaceError(err):
		return eh.handleDiskSpaceError()
	case isDatabaseCorruptionError(err):
		return eh.handleCorruptionError()
	case isDatabaseLockedError(err):
		return eh.handleLockError()
	case isConnectionError(err):
		return eh.handleConnectionError()
	default:
		eh.logger.Warn("Unknown database error type", "error", err)
		return false
	}
}

// handleDiskSpaceError handles disk space issues
func (eh *ErrorHandler) handleDiskSpaceError() bool {
	eh.logger.Warn("Disk space error detected, attempting cleanup...")

	// 尝试清理旧数据
	if eh.tracker != nil {
		if err := eh.tracker.cleanupOldRecords(); err != nil {
			eh.logger.Error("Emergency cleanup failed", "error", err)
			return false
		}
		eh.logger.Info("Emergency cleanup completed")
		return true
	}

	return false
}

// handleCorruptionError handles database corruption
func (eh *ErrorHandler) handleCorruptionError() bool {
	eh.logger.Error("Database corruption detected, attempting recovery...")

	if eh.tracker == nil || eh.tracker.db == nil {
		return false
	}

	// 尝试数据库完整性检查
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var integrityResult string
	err := eh.tracker.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrityResult)
	if err != nil {
		eh.logger.Error("Integrity check failed", "error", err)
		return false
	}

	if integrityResult != "ok" {
		eh.logger.Error("Database integrity compromised", "result", integrityResult)

		// 尝试备份和重建
		return eh.attemptDatabaseRestore()
	}

	eh.logger.Info("Database integrity check passed")
	return true
}

// handleLockError handles database lock issues
func (eh *ErrorHandler) handleLockError() bool {
	eh.logger.Warn("Database lock detected, waiting for release...")

	// 等待锁释放
	for i := 0; i < 10; i++ {
		time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)

		// 尝试简单查询测试连接
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		var count int
		err := eh.tracker.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master").Scan(&count)
		cancel()

		if err == nil {
			eh.logger.Info("Database lock released", "attempts", i+1)
			return true
		}
	}

	eh.logger.Error("Database lock timeout exceeded")
	return false
}

// handleConnectionError handles connection issues
func (eh *ErrorHandler) handleConnectionError() bool {
	eh.logger.Warn("Database connection error, attempting reconnection...")

	if eh.tracker == nil || eh.tracker.config == nil {
		return false
	}

	// 尝试重新连接
	return eh.attemptReconnection()
}

// attemptReconnection attempts to reconnect to the database
func (eh *ErrorHandler) attemptReconnection() bool {
	for attempt := 1; attempt <= 3; attempt++ {
		eh.logger.Info("Attempting database reconnection", "attempt", attempt)

		if eh.tracker == nil {
			return false
		}

		// 数据库连接由 adapter 统一管理，避免产生“孤儿连接”导致 Windows 无法删除/替换数据库文件。
		// 这里用 writeMu 阻止写线程并发访问 writeDB，再在 mu 保护下重建 adapter 连接。
		eh.tracker.writeMu.Lock()
		err := func() error {
			eh.tracker.mu.Lock()
			defer eh.tracker.mu.Unlock()

			if eh.tracker.adapter == nil {
				return fmt.Errorf("database adapter not initialized")
			}

			_ = eh.tracker.adapter.Close()
			if err := eh.tracker.adapter.Open(); err != nil {
				return err
			}

			// 重新绑定读写连接引用
			eh.tracker.readDB = eh.tracker.adapter.GetReadDB()
			eh.tracker.writeDB = eh.tracker.adapter.GetWriteDB()
			eh.tracker.db = eh.tracker.readDB // 兼容字段
			return nil
		}()
		eh.tracker.writeMu.Unlock()

		if err != nil {
			eh.logger.Error("Reconnection failed", "attempt", attempt, "error", err)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		eh.logger.Info("Database reconnection successful")
		return true
	}

	eh.logger.Error("All reconnection attempts failed")
	return false
}

// attemptDatabaseRestore attempts to restore the database from backup
func (eh *ErrorHandler) attemptDatabaseRestore() bool {
	if eh.tracker == nil || eh.tracker.config == nil {
		return false
	}

	dbPath := eh.tracker.config.DatabasePath
	backupPath := dbPath + ".backup"
	corruptedPath := dbPath + ".corrupted." + time.Now().Format("20060102-150405")

	// 检查是否有备份文件
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		eh.logger.Error("No backup file found for restoration", "backup_path", backupPath)
		return false
	}

	eh.logger.Info("Attempting database restoration from backup",
		"backup_path", backupPath,
		"corrupted_path", corruptedPath)

	// 移动损坏的数据库文件
	if err := os.Rename(dbPath, corruptedPath); err != nil {
		eh.logger.Error("Failed to move corrupted database", "error", err)
		return false
	}

	// 复制备份文件
	if err := copyFile(backupPath, dbPath); err != nil {
		eh.logger.Error("Failed to restore from backup", "error", err)
		// 尝试恢复原文件
		os.Rename(corruptedPath, dbPath)
		return false
	}

	// 尝试重新连接到恢复的数据库
	if !eh.attemptReconnection() {
		eh.logger.Error("Failed to connect to restored database")
		return false
	}

	eh.logger.Info("Database successfully restored from backup")
	return true
}

// CreateBackup creates a backup of the current database
func (eh *ErrorHandler) CreateBackup() error {
	if eh.tracker == nil || eh.tracker.db == nil {
		return fmt.Errorf("tracker or database not initialized")
	}

	dbPath := eh.tracker.config.DatabasePath
	backupPath := dbPath + ".backup"
	tempBackupPath := backupPath + ".tmp"

	// 创建备份目录
	if err := os.MkdirAll(filepath.Dir(tempBackupPath), 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// 使用SQLite的备份API
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 创建临时备份
	backupDB, err := sql.Open("sqlite", tempBackupPath)
	if err != nil {
		return fmt.Errorf("failed to create backup database: %w", err)
	}

	// 简化的备份方案：导出和导入数据
	if err := eh.performSimpleBackup(ctx, backupDB); err != nil {
		_ = backupDB.Close()
		os.Remove(tempBackupPath)
		return fmt.Errorf("backup operation failed: %w", err)
	}

	// Windows 下文件 rename 对打开句柄非常敏感：必须先关闭连接，避免 “file is being used by another process”
	if err := backupDB.Close(); err != nil {
		os.Remove(tempBackupPath)
		return fmt.Errorf("failed to close backup database: %w", err)
	}

	// 原子性地移动备份文件
	if err := os.Rename(tempBackupPath, backupPath); err != nil {
		os.Remove(tempBackupPath)
		return fmt.Errorf("failed to finalize backup: %w", err)
	}

	eh.logger.Info("Database backup created successfully", "backup_path", backupPath)
	return nil
}

// performSimpleBackup performs a simple backup by recreating schema and copying data
//
// 🔧 [P1.1 修复] 复用嵌入的 schema.sql 创建表，避免硬编码 schema 与主表漂移
//   - 旧实现：硬编码 CREATE TABLE 语句，缺少 10+ 新增字段（cache_creation_5m/1h, failure_reason, last_failure_reason, cancel_reason, is_streaming 等）
//   - 风险：从备份恢复时这些字段全部丢失数据
//   - 新实现：直接 ExecContext(schemaFS) 创建所有表，schema 变更自动同步
//
// 同时使用 PRAGMA table_info 动态获取列列表，避免 SELECT/INSERT 硬编码列名
func (eh *ErrorHandler) performSimpleBackup(ctx context.Context, backupDB *sql.DB) error {
	// 🔧 [P1.1] 使用嵌入的 schema.sql 创建完整 schema（包含所有表与索引）
	schemaData, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read embedded schema: %w", err)
	}
	if _, err := backupDB.ExecContext(ctx, string(schemaData)); err != nil {
		return fmt.Errorf("failed to create backup schema: %w", err)
	}

	// 复制请求日志数据：使用独立只读连接，避免占用主连接池导致前端长时间等待/卡顿
	srcDB, err := sql.Open("sqlite", eh.tracker.config.DatabasePath+"?_query_only=1&_busy_timeout=30000")
	if err != nil {
		return fmt.Errorf("failed to open source database for backup: %w", err)
	}
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)
	srcDB.SetConnMaxLifetime(0)
	defer srcDB.Close()

	// 🔧 [P1.1] 动态获取 request_logs 列列表，避免硬编码与 schema 漂移
	columns, err := getTableColumns(ctx, srcDB, "request_logs")
	if err != nil {
		return fmt.Errorf("failed to get request_logs columns: %w", err)
	}
	if len(columns) == 0 {
		eh.logger.Warn("source request_logs has no columns, skipping backup")
		return nil
	}

	// 排除 id（自增），让备份表自己生成
	dataColumns := make([]string, 0, len(columns))
	for _, col := range columns {
		if col != "id" {
			dataColumns = append(dataColumns, col)
		}
	}

	selectSQL := fmt.Sprintf("SELECT %s FROM request_logs ORDER BY id",
		joinIdentifiers(dataColumns))
	rows, err := srcDB.QueryContext(ctx, selectSQL)
	if err != nil {
		return fmt.Errorf("failed to read request logs: %w", err)
	}
	defer rows.Close()

	placeholders := make([]string, len(dataColumns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO request_logs (%s) VALUES (%s)",
		joinIdentifiers(dataColumns), strings.Join(placeholders, ", "))

	stmt, err := backupDB.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	recordCount := 0
	for rows.Next() {
		// 通用扫描：用 []any 接所有列，避免依赖 RequestDetail 结构
		values := make([]any, len(dataColumns))
		scanArgs := make([]any, len(dataColumns))
		for i := range values {
			scanArgs[i] = &values[i]
		}
		if err := rows.Scan(scanArgs...); err != nil {
			return fmt.Errorf("failed to scan record: %w", err)
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("failed to insert record: %w", err)
		}
		recordCount++
	}

	eh.logger.Info("Backup completed", "records_copied", recordCount, "columns", len(dataColumns))
	return nil
}

// getTableColumns 通过 PRAGMA table_info 获取表的列名列表（按 cid 升序）
// 🔧 [P1.1 新增] 用于动态获取表 schema，避免硬编码列列表与 schema 漂移
func getTableColumns(ctx context.Context, db *sql.DB, tableName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

// joinIdentifiers 将列名用反引号包裹后用逗号拼接，避免与 SQL 关键字冲突
// 🔧 [P1.1 新增] SQLite 支持双引号或反引号，这里统一用双引号以兼容
func joinIdentifiers(names []string) string {
	if len(names) == 0 {
		return ""
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + n + `"`
	}
	return strings.Join(quoted, ", ")
}

// Error type detection helper functions
func isDiskSpaceError(err error) bool {
	errStr := err.Error()
	return contains(errStr, "no space left") ||
		contains(errStr, "disk full") ||
		contains(errStr, "SQLITE_FULL")
}

func isDatabaseCorruptionError(err error) bool {
	errStr := err.Error()
	return contains(errStr, "SQLITE_CORRUPT") ||
		contains(errStr, "database disk image is malformed") ||
		contains(errStr, "file is not a database")
}

func isDatabaseLockedError(err error) bool {
	errStr := err.Error()
	return contains(errStr, "SQLITE_BUSY") ||
		contains(errStr, "SQLITE_LOCKED") ||
		contains(errStr, "database is locked")
}

func isConnectionError(err error) bool {
	errStr := err.Error()
	return contains(errStr, "connection") ||
		contains(errStr, "no such file") ||
		contains(errStr, "unable to open database")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// RestoreFromBackup 从备份恢复数据库
func (eh *ErrorHandler) RestoreFromBackup() error {
	if eh.tracker == nil || eh.tracker.config == nil {
		return fmt.Errorf("tracker not initialized")
	}

	dbPath := eh.tracker.config.DatabasePath
	backupPath := dbPath + ".backup"

	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	eh.logger.Info("Restoring database from backup", "backup_path", backupPath)

	// 关闭当前数据库连接
	if eh.tracker.db != nil {
		eh.tracker.db.Close()
	}

	// 复制备份文件到原位置
	if err := copyFile(backupPath, dbPath); err != nil {
		return fmt.Errorf("failed to restore from backup: %w", err)
	}

	// 重新建立数据库连接
	if !eh.attemptReconnection() {
		return fmt.Errorf("failed to reconnect after restore")
	}

	eh.logger.Info("Database successfully restored from backup")
	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := sourceFile.Read(buf)
		if n > 0 {
			if _, writeErr := destFile.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
	}

	return destFile.Sync()
}
