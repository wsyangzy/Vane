package tracking

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

// periodicCleanup 定期清理历史数据
//
// 🔧 [P2.4 增强] 每次清理后同时执行 ANALYZE + 增量 VACUUM
//   - ANALYZE：更新查询规划器统计信息，让查询继续走最优索引
//   - VACUUM (incremental)：回收清理释放的空间，控制数据库文件大小
func (ut *UsageTracker) periodicCleanup() {
	defer ut.wg.Done()

	policy := ut.currentRetentionPolicy()
	timer := time.NewTimer(policy.cleanupInterval)
	defer timer.Stop()

	slog.Debug("Periodic cleanup task started", "interval", policy.cleanupInterval)

	for {
		select {
		case <-timer.C:
		case <-ut.retentionChanged:
		case <-ut.ctx.Done():
			slog.Debug("Periodic cleanup task stopped")
			return
		}

		if err := ut.runRetentionMaintenance(); err != nil {
			slog.Error("Failed to apply retention policy", "error", err)
		}

		policy = ut.currentRetentionPolicy()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(policy.cleanupInterval)
	}
}

func (ut *UsageTracker) runRetentionMaintenance() error {
	if err := ut.cleanupOldRecords(); err != nil {
		return err
	}
	if err := ut.archiveClosedMonths(context.Background()); err != nil {
		return err
	}
	ut.postCleanupMaintenance()
	return nil
}

// postCleanupMaintenance 清理后的数据库维护任务
// 🔧 [P2.4 新增] 包括 ANALYZE 和增量 VACUUM
func (ut *UsageTracker) postCleanupMaintenance() {
	if ut.adapter == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. ANALYZE 更新统计信息
	if err := ut.adapter.PerformAnalyze(ctx); err != nil {
		slog.Warn("⚠️ [P2.4] ANALYZE 失败（不影响业务）", "error", err)
	} else {
		slog.Debug("✅ [P2.4] ANALYZE 完成")
	}

	// 2. 增量 VACUUM 回收空间（仅在 auto_vacuum=INCREMENTAL 时有效）
	if err := ut.adapter.PerformIncrementalVacuum(ctx, 0); err != nil {
		// 这是正常情况：旧数据库可能不是 INCREMENTAL 模式
		slog.Debug("incremental_vacuum 跳过（auto_vacuum 未启用或无可回收空间）", "error", err)
	} else {
		slog.Debug("✅ [P2.4] incremental_vacuum 完成")
	}
}

// cleanupOldRecords 清理过期记录（使用写队列）
//
// 🔧 [P2.5 修复] 改为分批删除，避免单次大 DELETE 长时间锁表
//   - 旧实现：一次性 DELETE 所有过期记录，大数据量下锁表数秒甚至更久
//   - 新实现：每批 1000 行，每批之间释放锁，让其他读写操作有机会执行
//   - 这对长时间运行的大数据库尤其重要（数十万行待删除场景）
func (ut *UsageTracker) cleanupOldRecords() error {
	policy := ut.currentRetentionPolicy()
	if policy.retentionDays <= 0 {
		return nil // 永久保留
	}
	ut.historyMu.Lock()
	defer ut.historyMu.Unlock()

	cutoffTime := ut.now().AddDate(0, 0, -policy.retentionDays)

	// 🔧 [P2.5] 分批删除 request_logs，每批 1000 行
	const batchSize = 1000
	const maxBatches = 1000 // 最多删 100 万行，避免无限循环（防御性上限）

	totalDeleted := int64(0)
	for batch := 0; batch < maxBatches; batch++ {
		// 检查 ctx 取消
		select {
		case <-ut.ctx.Done():
			return ut.ctx.Err()
		default:
		}

		// 用子查询 LIMIT 实现分批删除
		// 注意：SQLite 的 DELETE...LIMIT 需要编译时启用 SQLITE_ENABLE_UPDATE_DELETE_LIMIT
		// 这里用 WHERE id IN (SELECT id ... LIMIT N) 兼容标准 SQLite
		requestQuery := `DELETE FROM request_logs WHERE id IN (
			SELECT id FROM request_logs
			WHERE start_time < ? AND COALESCE(environment, 'release') = ?
			LIMIT ?
		)`
		requestWriteReq := WriteRequest{
			Query:     requestQuery,
			Args:      []interface{}{cutoffTime, ut.environment, batchSize},
			Response:  make(chan error, 1),
			Context:   context.Background(),
			EventType: "cleanup_requests_batch",
			// 通过 callback 拿 RowsAffected
		}

		select {
		case ut.writeQueue <- requestWriteReq:
			if err := <-requestWriteReq.Response; err != nil {
				return fmt.Errorf("failed to delete old request logs (batch %d): %w", batch, err)
			}
		case <-ut.ctx.Done():
			return ut.ctx.Err()
		}

		// 查询本批是否还有数据可删（用 SELECT COUNT 而非依赖 RowsAffected）
		var remaining int
		err := ut.readDB.QueryRow(
			"SELECT COUNT(*) FROM request_logs WHERE start_time < ? AND COALESCE(environment, 'release') = ? LIMIT 1",
			cutoffTime, ut.environment).Scan(&remaining)
		if err != nil {
			slog.Warn("cleanup: failed to check remaining rows, stopping early", "error", err)
			break
		}
		if remaining == 0 {
			// 本批之后已无剩余，退出
			totalDeleted += int64(batchSize) // 粗略估计
			break
		}
		totalDeleted += int64(batchSize)

		// 批之间短暂让出，让其他读写有机会
		time.Sleep(10 * time.Millisecond)
	}

	if totalDeleted > 0 {
		slog.Info("🧹 [P2.5] 分批清理过期请求记录完成",
			"approx_deleted", totalDeleted,
			"batch_size", batchSize,
			"cutoff_time", cutoffTime.Format(time.RFC3339))

		// 🔧 [P6 新增] 清理后从 DB 重新同步内存计数器（避免漂移）
		// 删除批次涉及多 provider 难以精确扣减，且清理频率低（默认每日一次），
		// 直接重新 Init 是 KISS 的合理选择。
		if ut.dbCounter != nil && ut.readDB != nil {
			resyncCtx, resyncCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := ut.dbCounter.Init(resyncCtx, ut.readDB, ut.environment); err != nil {
				slog.Warn("清理后重新同步内存计数器失败（计数可能存在漂移）", "error", err)
			}
			resyncCancel()
		}
	}

	// 清理过期的汇总数据（通过写队列，汇总表小得多，单次足够）
	summaryQuery := "DELETE FROM usage_summary WHERE date < ? AND COALESCE(environment, 'release') = ?"
	summaryWriteReq := WriteRequest{
		Query:     summaryQuery,
		Args:      []interface{}{cutoffTime.Format("2006-01-02"), ut.environment},
		Response:  make(chan error, 1),
		Context:   context.Background(),
		EventType: "cleanup_summaries",
	}

	select {
	case ut.writeQueue <- summaryWriteReq:
		err := <-summaryWriteReq.Response
		if err != nil {
			return fmt.Errorf("failed to delete old usage summary: %w", err)
		}
	case <-ut.ctx.Done():
		return ut.ctx.Err()
	}
	if err := ut.cleanupHistoryArchivesBefore(context.Background(), cutoffTime); err != nil {
		return err
	}

	// 运行VACUUM以回收空间（通过写队列，仅对SQLite有效）
	// 说明：
	// VACUUM 会获取较强的锁（Windows 上尤甚），会导致管理/UI 侧读写出现 SQLITE_BUSY。
	// 对于桌面端运行时，定期 cleanup 已足够，自动 VACUUM 属于“收益小但风险大”的操作（YAGNI）。
	// 如需回收空间，请后续考虑在“手动维护/诊断”入口中按需触发。

	// 记录清理结果
	slog.Info("Cleaned up old records",
		"cutoff_date", cutoffTime.Format("2006-01-02"),
		"retention_days", policy.retentionDays)

	// 删除了 cutoff 之前的活动记录与归档记录：归档聚合在 historyMu 写锁内清空，
	// 其余统计缓存只清依赖 cutoff 之前数据的条目
	ut.invalidateArchiveTotalsLocked()
	ut.notifyUsageDataChanged(appcache.Change{End: cutoffTime})

	// 更新汇总统计（异步）
	go ut.updateUsageSummary()

	return nil
}

// vacuumDatabase 运行VACUUM命令回收数据库空间
func (ut *UsageTracker) vacuumDatabase(ctx context.Context) error {
	slog.Debug("Running database VACUUM to reclaim space...")

	// VACUUM不能在事务中运行
	_, err := ut.db.ExecContext(ctx, "VACUUM")
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}

	slog.Debug("Database VACUUM completed successfully")
	return nil
}

// periodicBackup 定期备份数据库
func (ut *UsageTracker) periodicBackup() {
	defer ut.wg.Done()

	ticker := time.NewTicker(6 * time.Hour) // 每6小时备份一次
	defer ticker.Stop()

	slog.Debug("Periodic backup task started")

	for {
		select {
		case <-ticker.C:
			if ut.errorHandler != nil {
				if err := ut.errorHandler.CreateBackup(); err != nil {
					slog.Error("Scheduled backup failed", "error", err)
				} else {
					slog.Debug("Scheduled backup completed successfully")
				}
			}

		case <-ut.ctx.Done():
			slog.Debug("Periodic backup task stopped")
			return
		}
	}
}
