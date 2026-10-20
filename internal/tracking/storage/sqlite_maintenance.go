package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

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
