package tracking

import (
	"context"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"
)

// GetDatabaseStats 获取数据库统计信息（包装方法）
func (ut *UsageTracker) GetDatabaseStats(ctx context.Context) (*DatabaseStats, error) {
	if ut.config == nil || !ut.config.Enabled {
		return nil, fmt.Errorf("usage tracking not enabled")
	}
	if ut.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return ut.getDatabaseStatsInternal(ctx)
}

// HealthCheck 检查数据库连接状态和基本功能（使用读连接）
func (ut *UsageTracker) HealthCheck(ctx context.Context) error {
	if ut.config == nil || !ut.config.Enabled {
		return nil // 如果未启用，认为是健康的
	}

	if ut.readDB == nil {
		return fmt.Errorf("read database not initialized")
	}

	// 测试读数据库连接
	if err := ut.readDB.PingContext(ctx); err != nil {
		return fmt.Errorf("read database ping failed: %w", err)
	}

	// 测试写数据库连接
	if ut.writeDB != nil {
		if err := ut.writeDB.PingContext(ctx); err != nil {
			return fmt.Errorf("write database ping failed: %w", err)
		}
	}

	// 测试基本查询（使用读连接）
	var count int
	err := ut.readDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	if err != nil {
		return fmt.Errorf("database query test failed: %w", err)
	}

	// 检查表是否存在
	if count < 2 { // 至少应该有 request_logs 和 usage_summary 两个表
		return fmt.Errorf("database schema incomplete: expected at least 2 tables, found %d", count)
	}

	// 检查事件处理通道是否正常
	select {
	case <-ut.ctx.Done():
		return fmt.Errorf("usage tracker context cancelled")
	default:
		// 上下文正常
	}

	// 检查事件通道容量
	if ut.eventChan != nil {
		channelLoad := float64(len(ut.eventChan)) / float64(cap(ut.eventChan)) * 100
		if channelLoad > 90 {
			return fmt.Errorf("event channel overloaded: %.1f%% capacity used", channelLoad)
		}
	}

	// 检查写队列容量
	if ut.writeQueue != nil {
		writeQueueLoad := float64(len(ut.writeQueue)) / float64(cap(ut.writeQueue)) * 100
		if writeQueueLoad > 90 {
			return fmt.Errorf("write queue overloaded: %.1f%% capacity used", writeQueueLoad)
		}
	}

	return nil
}

// ForceFlush 强制刷新所有待处理事件
//
// 🔧 [P1.7 改进] 同时刷新 HotPool 的 ArchiveManager（真正等待落盘），
// 不再只发送一个 traditional eventChan 的 flush 事件（旧实现只是触发批处理，不等待落盘）
func (ut *UsageTracker) ForceFlush() error {
	if ut.config == nil || !ut.config.Enabled {
		return nil
	}

	// 1. 触发 traditional eventChan 的 flush 事件（保持向后兼容）
	flushEvent := RequestEvent{
		Type:      "flush",
		RequestID: "force-flush-" + ut.now().Format("20060102150405"),
		Timestamp: ut.now(),
		Data:      nil,
	}

	select {
	case ut.eventChan <- flushEvent:
		slog.Info("Force flush event sent to traditional event channel")
	default:
		slog.Warn("Event channel full, traditional flush skipped")
	}

	// 2. 🔧 [P1.7] 同步刷新 ArchiveManager，真正等待数据落盘
	if ut.archiveManager != nil {
		if err := ut.archiveManager.Flush(); err != nil {
			return fmt.Errorf("archive manager flush failed: %w", err)
		}
	}

	return nil
}
