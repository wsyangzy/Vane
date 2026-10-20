package tracking

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// startArchiver 批量归档协程
func (am *ArchiveManager) startArchiver() {
	defer am.wg.Done()

	batch := make([]*ArchiveEvent, 0, am.config.BatchSize)
	ticker := time.NewTicker(am.config.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		// 提取请求 ID 列表
		requestIDs := make([]string, len(batch))
		for i, event := range batch {
			requestIDs[i] = event.Request.RequestID
		}

		start := time.Now()
		err := am.flushBatch(batch)
		latency := time.Since(start)

		am.stats.mu.Lock()
		am.stats.LastFlushTime = time.Now()
		am.stats.LastFlushLatency = latency
		am.stats.TotalBatches++
		if err == nil {
			am.stats.TotalArchived += int64(len(batch))
			// 更新平均批次大小
			am.stats.AvgBatchSize = float64(am.stats.TotalArchived) / float64(am.stats.TotalBatches)
		} else {
			am.stats.TotalFailed += int64(len(batch))
		}
		am.stats.mu.Unlock()

		if err != nil {
			slog.Error("📦 批量归档失败",
				"batch_size", len(batch),
				"request_ids", strings.Join(requestIDs, ", "),
				"error", err,
				"latency", latency)
		} else {
			slog.Debug("📦 批量归档成功",
				"batch_size", len(batch),
				"request_ids", strings.Join(requestIDs, ", "),
				"latency", latency)
		}

		// 清空批次
		batch = batch[:0]
	}

	for {
		select {
		case event := <-am.archiveChan:
			batch = append(batch, event)

			// 达到批次大小，立即刷新
			if len(batch) >= am.config.BatchSize {
				flush()
			}

		case <-ticker.C:
			// 定时刷新（即使未满批次）
			flush()
			if err := am.ReplayFailedArchives(); err != nil {
				slog.Warn("📦 定时回放归档失败文件失败", "error", err)
			}

		// 🔧 [P1.7 新增] 同步刷新请求：先排空 channel 再 flush，确保所有"已 Archive 但未持久化"事件都被刷掉
		case respChan := <-am.flushReqChan:
			// 先把 archiveChan 当前所有待处理事件吸收到 batch
			drainArchiveChan := true
			for drainArchiveChan {
				select {
				case event := <-am.archiveChan:
					batch = append(batch, event)
					if len(batch) >= am.config.BatchSize {
						flush()
					}
				default:
					drainArchiveChan = false
				}
			}
			// 刷新当前批次
			start := time.Now()
			var flushErr error
			if len(batch) > 0 {
				flushErr = am.flushBatch(batch)
				am.stats.mu.Lock()
				am.stats.LastFlushTime = time.Now()
				am.stats.LastFlushLatency = time.Since(start)
				am.stats.TotalBatches++
				if flushErr == nil {
					am.stats.TotalArchived += int64(len(batch))
					am.stats.AvgBatchSize = float64(am.stats.TotalArchived) / float64(am.stats.TotalBatches)
				} else {
					am.stats.TotalFailed += int64(len(batch))
				}
				am.stats.mu.Unlock()
				batch = batch[:0]
			}
			// 通知调用方
			select {
			case respChan <- flushErr:
			default:
			}

		case <-am.ctx.Done():
			// 优雅关闭，处理剩余事件
			slog.Info("📦 ArchiveManager 正在关闭，处理剩余事件...")

			// 先刷新当前批次
			flush()

			// 处理通道中剩余的事件
			for {
				select {
				case event := <-am.archiveChan:
					batch = append(batch, event)
					if len(batch) >= am.config.BatchSize {
						flush()
					}
				default:
					// 通道已空，最后刷新
					flush()
					slog.Info("📦 ArchiveManager 已关闭",
						"total_archived", am.stats.TotalArchived,
						"total_failed", am.stats.TotalFailed)
					return
				}
			}
		}
	}
}

// flushBatch 批量写入数据库
func (am *ArchiveManager) flushBatch(events []*ArchiveEvent) error {
	if len(events) == 0 {
		return nil
	}

	var lastErr error
	for retry := 0; retry < am.config.MaxRetry; retry++ {
		err := am.batchInsert(events)
		if err == nil {
			// 归档成功，从热池的归档缓存中移除
			if am.hotPool != nil {
				requestIDs := make([]string, len(events))
				for i, event := range events {
					requestIDs[i] = event.Request.RequestID
				}
				am.hotPool.ConfirmArchived(requestIDs)
			}
			return nil
		}

		lastErr = err
		slog.Warn("📦 批量插入失败，重试中",
			"retry", retry+1,
			"max_retry", am.config.MaxRetry,
			"error", err)

		// 指数退避
		time.Sleep(time.Duration(retry+1) * time.Second)
	}

	// 🔧 [P1.3 新增] 所有重试失败后，降级写入本地 JSONL 文件以防数据丢失
	// 旧实现：仅记录错误后丢弃整个 batch，请求 Token 已被上游消费但本地无任何记录
	// 新实现：序列化为 JSONL 写入本地"事故文件"，运维可后续手动恢复
	if dumpErr := am.dumpFailedBatchToFile(events, lastErr); dumpErr != nil {
		slog.Error("📦 批量归档完全失败：连本地降级文件都写不进去",
			"batch_size", len(events),
			"original_error", lastErr,
			"dump_error", dumpErr)
	}
	return fmt.Errorf("batch insert failed after %d retries: %w", am.config.MaxRetry, lastErr)
}
