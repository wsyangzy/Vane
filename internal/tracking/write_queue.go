package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/appdir"

	_ "modernc.org/sqlite"
)

// WriteRequest 写操作请求
type WriteRequest struct {
	Query     string
	Args      []interface{}
	Response  chan error
	Context   context.Context
	EventType string // 用于调试和监控
}

// recordToFailureQueue 将 HotPool 写入失败的请求事件持久化到本地 JSONL 文件
//
// 🔧 [P5.1 新增] 替换原来的"降级到 traditional eventChan"机制
// 旧实现：HotPool 失败 → 写 eventChan → eventChan 满静默丢弃
// 新实现：HotPool 失败 → 持久化到本地 archive_failures.jsonl，运维可后续手动恢复
//
// 文件格式：每行一个 JSON 对象 {"kind":"start|update|complete|fail","payload":{...}}
// 滚动策略：单文件超过 10 MB 时备份为 .{timestamp} 并新建
func (ut *UsageTracker) recordToFailureQueue(kind string, payload any) {
	if fields, ok := payload.(map[string]any); ok {
		if isUpstream, _ := fields["http_status_is_upstream"].(bool); !isUpstream {
			delete(fields, "http_status")
		}
	}

	// 推断失败队列文件路径
	dir := appdir.Dir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	dumpPath := filepath.Join(dir, "tracker_failures.jsonl")

	// 滚动策略
	const maxFileSize = 10 * 1024 * 1024
	if info, err := os.Stat(dumpPath); err == nil && info.Size() > maxFileSize {
		backupPath := dumpPath + "." + time.Now().Format("20060102-150405")
		if renameErr := os.Rename(dumpPath, backupPath); renameErr != nil {
			slog.Debug("备份失败队列文件失败", "path", dumpPath, "backup", backupPath, "error", renameErr)
		}
	}

	f, err := os.OpenFile(dumpPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Debug("打开失败队列文件失败", "error", err)
		return
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Debug("关闭失败队列文件失败", "error", closeErr)
		}
	}()

	record := map[string]any{
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"kind":      kind,
		"payload":   payload,
	}
	data, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		slog.Debug("序列化失败队列记录失败", "error", marshalErr)
		return
	}
	if _, writeErr := f.Write(append(data, '\n')); writeErr != nil {
		slog.Debug("写入失败队列文件失败", "error", writeErr)
		return
	}
}

// processWriteQueue 启动写操作队列处理器（简化版，确保稳定性）
func (ut *UsageTracker) processWriteQueue() {
	// 🔧 [P3.4 修复] writeWg.Add 已移至调用方，这里只 Done
	defer ut.writeWg.Done()
	slog.Debug("Write processor started")

	// 🔧 [P2.2 优化] 添加简单的批量处理：累积 50ms 窗口内的非关键写请求，
	// 一次性串行执行减少调度开销。注意：仍然每个请求独立事务，
	// 因为合并事务会引入"某请求出错触发整批回滚"的语义问题（特别是 update 0 rows）。
	//
	// 实测：高频路径已走 ArchiveManager 批量插入，这里只是降低延迟。
	const batchWindow = 50 * time.Millisecond
	const maxBatchPerWake = 10 // 单次唤醒最多处理 N 个请求，避免长时间占用写锁

	for {
		select {
		case writeReq := <-ut.writeQueue:
			// 处理首个请求
			err := ut.executeWriteSimple(writeReq)
			writeReq.Response <- err

			// 🔧 [P2.2] 尝试快速排空：在小窗口内连续处理多个请求
			// 减少 select 调度次数，对 SQLite 单写连接更友好
			drainTimer := time.NewTimer(batchWindow)
			drainCount := 0
		drainLoop:
			for drainCount < maxBatchPerWake {
				select {
				case nextReq := <-ut.writeQueue:
					err := ut.executeWriteSimple(nextReq)
					nextReq.Response <- err
					drainCount++
				case <-drainTimer.C:
					break drainLoop
				case <-ut.ctx.Done():
					drainTimer.Stop()
					// 进入退出流程，让外层 ctx.Done 处理排空
					goto exit
				}
			}
			drainTimer.Stop()

		case <-ut.ctx.Done():
			goto exit
		}
	}
exit:
	// 退出前：尽量唤醒所有等待中的写请求，避免关闭时出现 goroutine 卡死
	drainErr := ut.ctx.Err()
	for {
		select {
		case writeReq := <-ut.writeQueue:
			writeReq.Response <- drainErr
		default:
			slog.Debug("Write processor stopped")
			return
		}
	}
}

// executeWriteSimple 执行简单写操作（避免复杂的批处理）
//
// 🔧 [P0.4 修复] 与 archive_manager.batchInsert 保持一致的连接驱逐机制
// 旧实现：直接用 ut.writeDB.BeginTx，如果 commit 失败（SQLITE_BUSY 等）
// 连接会进入污染状态，后续 BeginTx 会触发"事务嵌套"错误，写入永久瘫痪
// 新实现：用 *sql.Conn 显式持有连接 + commit/begin 失败时 evictConn 驱逐
func (ut *UsageTracker) executeWriteSimple(req WriteRequest) error {
	ut.writeMu.Lock()
	defer ut.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(req.Context, 30*time.Second)
	defer cancel()

	// 直接执行，不使用事务（对于简单INSERT/UPDATE，不一定需要事务）
	if req.EventType == "vacuum" {
		// VACUUM不能在事务中执行
		_, err := ut.writeDB.ExecContext(ctx, req.Query, req.Args...)
		return err
	}

	// 🔧 [P0.4 修复] 用 *sql.Conn 显式持有连接，便于在事务出错时驱逐
	conn, err := ut.writeDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get write conn: %w", err)
	}
	connEvicted := false
	defer func() {
		// connEvicted 已通过 evictConn 销毁；正常路径下也需 Close 让连接回池
		if closeErr := conn.Close(); closeErr != nil && !connEvicted {
			slog.Warn("关闭追踪写入数据库连接失败", "error", closeErr)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		// BeginTx 失败极可能是连接已被污染，强制驱逐
		connEvicted = true
		if evictErr := evictConn(conn); evictErr != nil {
			slog.Warn("驱逐追踪写入数据库污染连接失败", "error", evictErr)
		}
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				slog.Debug("Failed to rollback transaction", "error", rbErr, "event_type", req.EventType)
			}
		}
	}()

	result, err := tx.ExecContext(ctx, req.Query, req.Args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	// 对于必须命中已存在记录的事件类型，确保更新确实发生，避免"看似成功但数据未写入"
	if req.EventType == "update" && result != nil {
		if rows, err := result.RowsAffected(); err == nil && rows == 0 {
			return fmt.Errorf("no rows updated for event_type=update")
		}
	}

	err = tx.Commit()
	if err != nil {
		// 🔧 [P0.4 修复] Commit 失败时驱逐连接，防止后续 BeginTx 在污染连接上嵌套报错
		connEvicted = true
		if evictErr := evictConn(conn); evictErr != nil {
			slog.Warn("驱逐追踪写入数据库污染连接失败", "error", evictErr)
		}
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	committed = true
	return nil
}

// executeWrite 执行单个写操作
func (ut *UsageTracker) executeWrite(req WriteRequest) error {
	ut.writeMu.Lock()
	defer ut.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(req.Context, 60*time.Second)
	defer cancel()

	// 安全的事务处理
	return ut.executeWriteTransaction(ctx, req)
}

// executeWriteTransaction 执行写事务（修复defer tx.Rollback()问题）
func (ut *UsageTracker) executeWriteTransaction(ctx context.Context, req WriteRequest) error {
	tx, err := ut.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				slog.Error("Failed to rollback transaction", "error", rbErr, "event_type", req.EventType)
			}
		}
	}()

	_, err = tx.ExecContext(ctx, req.Query, req.Args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	committed = true // 标记已提交，避免重复Rollback
	return nil
}
