package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

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
