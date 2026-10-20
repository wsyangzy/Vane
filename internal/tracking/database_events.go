package tracking

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

// processEvents 异步事件处理循环
func (ut *UsageTracker) processEvents() {
	defer ut.wg.Done()

	ticker := time.NewTicker(ut.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]RequestEvent, 0, ut.config.BatchSize)

	slog.Debug("Usage tracking event processor started")

	for {
		select {
		case event := <-ut.eventChan:
			batch = append(batch, event)
			if len(batch) >= ut.config.BatchSize {
				ut.flushBatch(batch)
				batch = batch[:0] // 重置切片但保留容量
			}

		case <-ticker.C:
			if len(batch) > 0 {
				ut.flushBatch(batch)
				batch = batch[:0]
			}

		case <-ut.ctx.Done():
			// 优雅关闭，处理剩余事件
			slog.Debug("Processing remaining events before shutdown", "count", len(batch))
			if len(batch) > 0 {
				ut.flushBatch(batch)
			}

			// 处理通道中剩余的事件
			for {
				select {
				case event := <-ut.eventChan:
					batch = append(batch, event)
					if len(batch) >= ut.config.BatchSize {
						ut.flushBatch(batch)
						batch = batch[:0]
					}
				default:
					if len(batch) > 0 {
						ut.flushBatch(batch)
					}
					slog.Debug("Usage tracking event processor stopped")
					return
				}
			}
		}
	}
}

// flushBatch 批量写入事件到数据库
func (ut *UsageTracker) flushBatch(events []RequestEvent) {
	if len(events) == 0 {
		return
	}
	// 事件队列路径按 request_id UPDATE 已落库的行（如已归档请求的 token 补录），
	// 拿不到原行的 start_time，影响范围按未知处理。processBatch 逐条执行，
	// 即使整批报错也可能已有部分写入，因此无论成败都通知。
	defer ut.notifyUsageDataChanged(appcache.Change{All: true})

	var retryCount int
	for retryCount < ut.config.MaxRetry {
		if err := ut.processBatch(events); err != nil {
			// 使用错误处理器处理错误
			if ut.errorHandler != nil && ut.errorHandler.HandleDatabaseError(err, "flushBatch") {
				// 错误已处理，重试操作
				retryCount++
				slog.Info("Database error handled successfully, retrying",
					"retry", retryCount,
					"batch_size", len(events))
				time.Sleep(time.Duration(retryCount) * time.Second)
				continue
			}

			retryCount++
			slog.Warn("Failed to flush batch, retrying",
				"error", err,
				"retry", retryCount,
				"max_retry", ut.config.MaxRetry,
				"batch_size", len(events))

			if retryCount < ut.config.MaxRetry {
				time.Sleep(time.Duration(retryCount) * time.Second)
			}
			continue
		}

		// 成功处理
		if retryCount > 0 {
			slog.Info("Batch processed successfully after retry",
				"retry_count", retryCount,
				"batch_size", len(events))
		}
		return
	}

	// 所有重试都失败
	slog.Error("Failed to process batch after all retries",
		"batch_size", len(events),
		"max_retry", ut.config.MaxRetry)
}

// processBatch 处理一批事件（重构为使用写队列）
func (ut *UsageTracker) processBatch(events []RequestEvent) error {
	successCount := 0
	failedCount := 0
	var firstErr error

	for _, event := range events {
		// 特殊处理flush事件
		if event.Type == "flush" {
			// flush事件不需要处理，只是用来触发批处理
			successCount++
			continue
		}

		// 构建写操作请求
		query, args, err := ut.buildWriteQuery(event)
		if err != nil {
			failedCount++
			if firstErr == nil {
				firstErr = err
			}
			slog.Error("Failed to build write query",
				"error", err,
				"event_type", event.Type,
				"request_id", event.RequestID)
			continue
		}

		writeReq := WriteRequest{
			Query:     query,
			Args:      args,
			Response:  make(chan error, 1),
			Context:   context.Background(),
			EventType: event.Type,
		}

		// 通过队列发送写操作
		select {
		case ut.writeQueue <- writeReq:
			select {
			case err := <-writeReq.Response:
				if err != nil {
					failedCount++
					if firstErr == nil {
						firstErr = err
					}
					slog.Error("Write operation failed",
						"error", err,
						"event_type", event.Type,
						"request_id", event.RequestID)
					continue
				}
				successCount++
			case <-ut.ctx.Done():
				return ut.ctx.Err()
			}

		case <-ut.ctx.Done():
			return ut.ctx.Err()
		}
	}

	if failedCount > 0 {
		slog.Warn("Some events failed to process",
			"success", successCount,
			"failed", failedCount,
			"total", len(events))
	}

	if firstErr != nil {
		return fmt.Errorf("processBatch: %d/%d events failed: %w", failedCount, len(events), firstErr)
	}
	return nil
}
