package storage

import (
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

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
