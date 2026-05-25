package tracking

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ArchiveEvent 归档事件（请求完成时发送）
type ArchiveEvent struct {
	Request   *ActiveRequest
	Timestamp time.Time
}

// ArchiveManagerConfig 归档管理器配置
type ArchiveManagerConfig struct {
	ChannelSize   int           // 归档通道大小（默认1000）
	BatchSize     int           // 批次大小（默认100）
	FlushInterval time.Duration // 刷新间隔（默认5秒）
	MaxRetry      int           // 最大重试次数（默认3）
}

// DefaultArchiveManagerConfig 返回默认配置
func DefaultArchiveManagerConfig() ArchiveManagerConfig {
	return ArchiveManagerConfig{
		ChannelSize:   1000,
		BatchSize:     100,
		FlushInterval: 5 * time.Second,
		MaxRetry:      3,
	}
}

// ArchiveManager 管理归档写入
type ArchiveManager struct {
	archiveChan chan *ArchiveEvent
	adapter     DatabaseAdapter
	config      ArchiveManagerConfig

	// 🔧 [P3.3 新增] pricingMu 保护 pricing 和 endpointMu 两个 map 的引用替换
	// 旧实现：UpdatePricing / UpdateEndpointMultipliers 直接替换引用，
	// 与 calculateCostV2 读这两个 map 形成 race（极端情况下可触发 map 并发读写 panic）
	pricingMu  sync.RWMutex
	pricing    map[string]ModelPricing       // 模型定价缓存
	endpointMu map[string]EndpointMultiplier // 端点倍率缓存

	location    *time.Location
	writeLocker sync.Locker // 与 UsageTracker 写队列复用，避免同连接并发开启事务

	// 热池引用（用于归档成功后清理）
	hotPool *HotPool

	// 🔧 [P1.7 新增] 同步刷新请求通道
	// Flush() 调用时向此 channel 发送一个回复 channel，
	// archiver 收到后立即刷新当前批次并通过回复 channel 报告完成
	flushReqChan chan chan error

	// 统计信息
	stats ArchiveStats

	// 生命周期控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ArchiveStats 归档统计信息
type ArchiveStats struct {
	mu               sync.RWMutex
	TotalReceived    int64         `json:"total_received"`     // 累计接收事件数
	TotalArchived    int64         `json:"total_archived"`     // 累计归档成功数
	TotalFailed      int64         `json:"total_failed"`       // 累计归档失败数
	TotalDropped     int64         `json:"total_dropped"`      // 累计丢弃数（通道满）
	TotalBatches     int64         `json:"total_batches"`      // 累计批次数
	AvgBatchSize     float64       `json:"avg_batch_size"`     // 平均批次大小
	LastFlushTime    time.Time     `json:"last_flush_time"`    // 最后刷新时间
	LastFlushLatency time.Duration `json:"last_flush_latency"` // 最后刷新延迟
	ChannelLength    int           `json:"channel_length"`     // 当前通道长度
}

// NewArchiveManager 创建归档管理器
func NewArchiveManager(adapter DatabaseAdapter, config ArchiveManagerConfig, pricing map[string]ModelPricing, location *time.Location) *ArchiveManager {
	if config.ChannelSize <= 0 {
		config.ChannelSize = 1000
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.MaxRetry <= 0 {
		config.MaxRetry = 3
	}

	ctx, cancel := context.WithCancel(context.Background())

	am := &ArchiveManager{
		archiveChan:  make(chan *ArchiveEvent, config.ChannelSize),
		adapter:      adapter,
		config:       config,
		pricing:      pricing,
		location:     location,
		flushReqChan: make(chan chan error, 4), // 🔧 [P1.7] 同步刷新请求通道（小缓冲避免阻塞）
		ctx:          ctx,
		cancel:       cancel,
	}

	// 启动归档协程
	am.wg.Add(1)
	go am.startArchiver()

	slog.Info("📦 ArchiveManager 初始化完成",
		"channel_size", config.ChannelSize,
		"batch_size", config.BatchSize,
		"flush_interval", config.FlushInterval)

	return am
}

// SetHotPool 设置热池引用（用于归档成功后清理）
func (am *ArchiveManager) SetHotPool(hp *HotPool) {
	am.hotPool = hp
}

// SetWriteLocker 设置写锁（用于与其他写路径串行化事务）
func (am *ArchiveManager) SetWriteLocker(locker sync.Locker) {
	am.writeLocker = locker
}

// UpdateEndpointMultipliers 更新端点成本倍率
//
// 🔧 [P3.3 修复] 加锁保护 map 引用替换，避免与读路径形成 race
func (am *ArchiveManager) UpdateEndpointMultipliers(multipliers map[string]EndpointMultiplier) {
	am.pricingMu.Lock()
	am.endpointMu = multipliers
	am.pricingMu.Unlock()
}

// UpdatePricing 更新模型定价（运行时动态更新）
//
// 🔧 [P3.3 修复] 加锁保护 map 引用替换
func (am *ArchiveManager) UpdatePricing(pricing map[string]ModelPricing) {
	am.pricingMu.Lock()
	am.pricing = pricing
	am.pricingMu.Unlock()
}

// getPricingSnapshot 读取定价快照（受 pricingMu 保护）
// 🔧 [P3.3 新增] 供 calculateCostV2 等读路径调用，保证读取的 map 引用在使用期间不变
func (am *ArchiveManager) getPricingSnapshot() (map[string]ModelPricing, map[string]EndpointMultiplier) {
	am.pricingMu.RLock()
	pricing := am.pricing
	endpointMu := am.endpointMu
	am.pricingMu.RUnlock()
	return pricing, endpointMu
}

// Archive 发送请求到归档通道
func (am *ArchiveManager) Archive(req *ActiveRequest) error {
	event := &ArchiveEvent{
		Request:   req,
		Timestamp: time.Now(),
	}

	// 优先非阻塞发送；通道满时短暂等待，降低“完成后丢归档”的概率。
	select {
	case am.archiveChan <- event:
		am.stats.mu.Lock()
		am.stats.TotalReceived++
		am.stats.mu.Unlock()
		return nil
	default:
	}

	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()

	select {
	case am.archiveChan <- event:
		am.stats.mu.Lock()
		am.stats.TotalReceived++
		am.stats.mu.Unlock()
		slog.Debug("📦 归档通道短暂拥堵，延迟入队成功",
			"request_id", req.RequestID,
			"channel_length", len(am.archiveChan))
		return nil
	case <-timer.C:
		am.stats.mu.Lock()
		am.stats.TotalDropped++
		am.stats.mu.Unlock()
		slog.Warn("📦 归档通道已满，丢弃事件",
			"request_id", req.RequestID,
			"channel_length", len(am.archiveChan))
		return fmt.Errorf("archive channel full, event dropped")
	}
}

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
	} else {
		// 降级文件写入成功后，仍要从热池清理（避免热池堆积已转储数据）
		if am.hotPool != nil {
			requestIDs := make([]string, len(events))
			for i, event := range events {
				requestIDs[i] = event.Request.RequestID
			}
			am.hotPool.ConfirmArchived(requestIDs)
		}
	}

	return fmt.Errorf("batch insert failed after %d retries: %w", am.config.MaxRetry, lastErr)
}

// dumpFailedBatchToFile 将归档失败的事件序列化为 JSONL 写入本地降级文件
//
// 🔧 [P1.3 新增] 防止归档失败导致已完成请求数据丢失
//   - 文件位置：与数据库同目录下的 archive_failures.jsonl
//   - 格式：每行一个 JSON 对象（含完整 ArchiveEvent.Request）
//   - 滚动策略：单文件超过 10 MB 时自动重命名为 .{timestamp} 备份并新建
//   - 运维方式：用户/管理员可后续用脚本读取并通过 SQL 导入到数据库
func (am *ArchiveManager) dumpFailedBatchToFile(events []*ArchiveEvent, lastErr error) error {
	if len(events) == 0 {
		return nil
	}

	// 推断降级文件路径：用 adapter 的数据库路径同目录
	dumpPath := am.getDumpFilePath()
	if dumpPath == "" {
		return fmt.Errorf("cannot determine dump file path")
	}

	// 滚动策略：单文件超过 10 MB 时备份
	const maxFileSize = 10 * 1024 * 1024
	if info, err := os.Stat(dumpPath); err == nil && info.Size() > maxFileSize {
		backupPath := dumpPath + "." + time.Now().Format("20060102-150405")
		if renameErr := os.Rename(dumpPath, backupPath); renameErr != nil {
			slog.Warn("📦 降级文件备份失败（继续追加到原文件）",
				"path", dumpPath, "error", renameErr)
		}
	}

	f, err := os.OpenFile(dumpPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open dump file: %w", err)
	}
	defer f.Close()

	dumpedCount := 0
	for _, event := range events {
		if event == nil || event.Request == nil {
			continue
		}
		// 构造降级记录：包含失败原因和原始事件，便于排查
		record := map[string]any{
			"dumped_at":  time.Now().Format(time.RFC3339Nano),
			"reason":     "archive_failed_after_retries",
			"last_error": lastErr.Error(),
			"request":    event.Request,
		}
		data, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			slog.Warn("📦 降级记录序列化失败", "request_id", event.Request.RequestID, "error", marshalErr)
			continue
		}
		if _, writeErr := f.Write(append(data, '\n')); writeErr != nil {
			return fmt.Errorf("write dump record: %w", writeErr)
		}
		dumpedCount++
	}

	if dumpedCount > 0 {
		slog.Warn("📦 批量归档失败：已降级到本地文件，等待手动恢复",
			"dump_file", dumpPath,
			"dumped_count", dumpedCount,
			"original_error", lastErr)
	}
	return nil
}

// getDumpFilePath 推断降级文件路径（与数据库同目录）
func (am *ArchiveManager) getDumpFilePath() string {
	if am.adapter == nil {
		return ""
	}
	dbType := am.adapter.GetDatabaseType()
	if dbType != "sqlite" {
		// 非 SQLite 情况下不主动决定路径，避免写入意外位置
		return ""
	}
	// 从 SQLite adapter 拿到 DB 文件路径
	// 简化做法：直接放在用户 home 下的子目录（与 tracker DatabasePath 同源）
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(homeDir, ".claude-request-forwarder")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}
	return filepath.Join(dir, "archive_failures.jsonl")
}

// batchInsert 执行批量插入
//
// 错误处理重点（2026-05-14）：
// SQLite 单连接写入池（MaxOpenConns=1）+ modernc.org/sqlite 驱动场景下，
// 当 tx.Commit() 因 SQLITE_BUSY 失败时，连接虽被归还到 Go pool，
// 但底层 SQLite C handle 仍处于 BEGIN 事务状态。
// 下次 BeginTx 命中同一污染连接，会立即报 "cannot start a transaction within a transaction (1)"，
// 导致写入彻底瘫痪、所有读请求也被锁阻塞。
//
// 修复：用 sql.Conn 显式管理连接生命周期，commit/写入出错时调用 evictConn 强制驱逐该连接，
// 让 sql.DB 在下次取连接时重建一个干净的 SQLite handle。
func (am *ArchiveManager) batchInsert(events []*ArchiveEvent) error {
	if am.adapter == nil {
		return fmt.Errorf("database adapter not initialized")
	}

	if am.writeLocker != nil {
		am.writeLocker.Lock()
		defer am.writeLocker.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writeDB := am.adapter.GetWriteDB()
	if writeDB == nil {
		return fmt.Errorf("write DB not available")
	}

	// 关键：用 *sql.Conn 显式持有连接，便于在事务出错时驱逐它
	conn, err := writeDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get write conn: %w", err)
	}
	connEvicted := false
	defer func() {
		if connEvicted {
			// 已通过 evictConn 销毁，conn.Close() 会返回错误也无所谓
			_ = conn.Close()
			return
		}
		_ = conn.Close()
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		// BeginTx 失败极可能是连接已被污染，强制驱逐
		connEvicted = true
		_ = evictConn(conn)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// defer 中的 rollback 是兜底，正常路径走 commit
	defer func() {
		// 只有未 commit 时才需要 rollback
		_ = tx.Rollback()
	}()

	// 🔧 [P1.6 修复] 使用 INSERT ... ON CONFLICT(request_id) DO UPDATE，
	// 避免单条 request_id 唯一约束冲突导致整批回滚。
	// 冲突来源：HotPool 满时降级到 traditional eventChan 路径，可能先写入；
	//          然后 HotPool 归档时再次写入相同 request_id → 旧实现整批失败。
	// 新实现：冲突时用归档数据（含完整 token 与成本）覆盖，因为归档总是后到达的"权威"版本。
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO request_logs (
			request_id, client_ip, user_agent, method, path,
			start_time, end_time, duration_ms,
			channel, endpoint_name, endpoint_chain, group_name, model_name,
			status, http_status_code, retry_count,
			failure_reason, cancel_reason,
			is_streaming,
			input_tokens, output_tokens,
			cache_creation_tokens, cache_creation_5m_tokens, cache_creation_1h_tokens,
			cache_read_tokens,
			input_cost_usd, output_cost_usd,
			cache_creation_cost_usd, cache_creation_5m_cost_usd, cache_creation_1h_cost_usd,
			cache_read_cost_usd, total_cost_usd
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(request_id) DO UPDATE SET
			end_time = excluded.end_time,
			duration_ms = excluded.duration_ms,
			endpoint_name = excluded.endpoint_name,
			endpoint_chain = excluded.endpoint_chain,
			model_name = excluded.model_name,
			status = excluded.status,
			http_status_code = excluded.http_status_code,
			retry_count = excluded.retry_count,
			failure_reason = excluded.failure_reason,
			cancel_reason = excluded.cancel_reason,
			is_streaming = excluded.is_streaming,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cache_creation_tokens = excluded.cache_creation_tokens,
			cache_creation_5m_tokens = excluded.cache_creation_5m_tokens,
			cache_creation_1h_tokens = excluded.cache_creation_1h_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			input_cost_usd = excluded.input_cost_usd,
			output_cost_usd = excluded.output_cost_usd,
			cache_creation_cost_usd = excluded.cache_creation_cost_usd,
			cache_creation_5m_cost_usd = excluded.cache_creation_5m_cost_usd,
			cache_creation_1h_cost_usd = excluded.cache_creation_1h_cost_usd,
			cache_read_cost_usd = excluded.cache_read_cost_usd,
			total_cost_usd = excluded.total_cost_usd
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, event := range events {
		req := event.Request

		// 计算分开的成本
		costBreakdown := am.calculateCostV2(req)

		// 格式化时间
		startTime := am.formatTime(req.StartTime)
		var endTime interface{}
		if req.EndTime != nil {
			endTime = am.formatTime(*req.EndTime)
		} else {
			endTime = nil
		}

		_, err := stmt.ExecContext(ctx,
			req.RequestID,
			req.ClientIP,
			req.UserAgent,
			req.Method,
			req.Path,
			startTime,
			endTime,
			req.DurationMs,
			req.Channel,
			req.EndpointName,
			req.EndpointChain,
			req.GroupName,
			req.ModelName,
			req.Status,
			req.HTTPStatus,
			req.RetryCount,
			nullString(req.FailureReason),
			nullString(req.CancelReason),
			req.IsStreaming,
			req.InputTokens,
			req.OutputTokens,
			req.CacheCreationTokens,
			req.CacheCreation5mTokens,
			req.CacheCreation1hTokens,
			req.CacheReadTokens,
			costBreakdown.InputCost,
			costBreakdown.OutputCost,
			costBreakdown.CacheCreationCost,   // 总成本（向后兼容）
			costBreakdown.CacheCreation5mCost, // 5分钟缓存成本
			costBreakdown.CacheCreation1hCost, // 1小时缓存成本
			costBreakdown.CacheReadCost,
			costBreakdown.TotalCost,
		)
		if err != nil {
			return fmt.Errorf("failed to insert request %s: %w", req.RequestID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		// Commit 失败（通常是 SQLITE_BUSY），驱逐这条连接避免后续 BeginTx 在污染连接上嵌套报错
		connEvicted = true
		_ = evictConn(conn)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// calculateCostV2 计算请求成本（支持分开的 5m/1h 缓存）
func (am *ArchiveManager) calculateCostV2(req *ActiveRequest) CostBreakdown {
	// 🔧 [P3.3 修复] 通过 snapshot 取得 map 引用，保证使用期间引用稳定
	// 旧实现：直接读 am.pricing/am.endpointMu，与 UpdatePricing/UpdateEndpointMultipliers 写操作 race
	pricingMap, endpointMuMap := am.getPricingSnapshot()
	if pricingMap == nil {
		return CostBreakdown{}
	}

	// 查找模型定价，不存在则回退到 _default 定价
	pricing, exists := pricingMap[req.ModelName]
	if !exists {
		pricing, exists = pricingMap["_default"]
		if !exists {
			return CostBreakdown{}
		}
	}

	// 获取端点倍率
	var multiplier *EndpointMultiplier
	if endpointMuMap != nil {
		if key := endpointMultiplierKey(req.Channel, req.GroupName, req.EndpointName); key != "" {
			if m, ok := endpointMuMap[key]; ok {
				multiplier = &m
			}
		} else if m, ok := endpointMuMap[req.EndpointName]; ok {
			// 兼容旧 key：endpointName 作为唯一键
			multiplier = &m
		}
	}

	// 构建 TokenUsage 对象
	usage := &TokenUsage{
		InputTokens:           req.InputTokens,
		OutputTokens:          req.OutputTokens,
		CacheCreationTokens:   req.CacheCreationTokens,
		CacheCreation5mTokens: req.CacheCreation5mTokens,
		CacheCreation1hTokens: req.CacheCreation1hTokens,
		CacheReadTokens:       req.CacheReadTokens,
	}

	// 调用公共成本计算函数
	return CalculateCostV2(usage, &pricing, multiplier)
}

// formatTime 格式化时间为易读格式（使用配置的时区）
// 格式：2025-12-04 17:18:48（北京时间，无时区后缀）
func (am *ArchiveManager) formatTime(t time.Time) string {
	if am.location != nil {
		t = t.In(am.location)
	}
	return t.Format("2006-01-02 15:04:05")
}

// nullString 处理空字符串为SQL NULL
func nullString(s string) interface{} {
	if s == "" {
		return sql.NullString{}
	}
	return s
}

// GetStats 获取统计信息（返回快照，不含锁）
func (am *ArchiveManager) GetStats() ArchiveStats {
	am.stats.mu.RLock()
	defer am.stats.mu.RUnlock()

	// 返回不含锁的快照副本
	return ArchiveStats{
		TotalReceived:    am.stats.TotalReceived,
		TotalArchived:    am.stats.TotalArchived,
		TotalFailed:      am.stats.TotalFailed,
		TotalDropped:     am.stats.TotalDropped,
		TotalBatches:     am.stats.TotalBatches,
		AvgBatchSize:     am.stats.AvgBatchSize,
		LastFlushTime:    am.stats.LastFlushTime,
		LastFlushLatency: am.stats.LastFlushLatency,
		ChannelLength:    len(am.archiveChan),
		// 注意：mu 字段是零值，不会复制原始锁
	}
}

// GetChannelLength 获取当前通道长度
func (am *ArchiveManager) GetChannelLength() int {
	return len(am.archiveChan)
}

// Close 关闭归档管理器
func (am *ArchiveManager) Close() error {
	slog.Info("📦 正在关闭 ArchiveManager...")
	am.cancel()
	am.wg.Wait()
	return nil
}

// Flush 强制刷新（用于测试或优雅关闭）
// Flush 触发立即刷新当前批次并等待落盘完成
//
// 🔧 [P1.7 修复] 旧实现是 time.Sleep(100ms)，纯猜测性等待，不可靠：
//   - 测试场景下时序不确定
//   - 优雅关闭场景下可能未刷完就退出
//   - 实际落盘时间可能远超 100ms（大批次 + SQLITE_BUSY 时）
//
// 新实现：通过 flushReqChan 与 archiver 同步通信，真正等待数据写入数据库
func (am *ArchiveManager) Flush() error {
	// 检查 ctx 是否已取消（关闭后 Flush 无意义）
	select {
	case <-am.ctx.Done():
		return nil
	default:
	}

	respChan := make(chan error, 1)
	flushTimeout := 30 * time.Second

	// 提交刷新请求
	select {
	case am.flushReqChan <- respChan:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("flush request timeout: archiver too busy to accept flush request")
	case <-am.ctx.Done():
		return nil
	}

	// 等待刷新完成
	select {
	case err := <-respChan:
		return err
	case <-time.After(flushTimeout):
		return fmt.Errorf("flush timeout after %v: batch may still be processing", flushTimeout)
	case <-am.ctx.Done():
		return nil
	}
}

// evictConn 强制驱逐一个 *sql.Conn，让 sql.DB 在下次 Get 时重建。
// 通过 conn.Raw 内部回调返回 driver.ErrBadConn，标准库会将此 conn 标记为坏，
// 关闭底层 driver.Conn 并不再放回 pool。
//
// 用途：modernc.org/sqlite 在 tx.Commit() 失败时，连接对应的 SQLite handle
// 仍处于 BEGIN 事务状态。若不驱逐，下次 BeginTx 拿到该污染连接会立即报
// "cannot start a transaction within a transaction (1)"，导致写入彻底瘫痪。
func evictConn(conn *sql.Conn) error {
	if conn == nil {
		return nil
	}
	rawErr := conn.Raw(func(driverConn interface{}) error {
		// 主动 Close 底层连接以释放 SQLite C 资源
		if c, ok := driverConn.(io.Closer); ok {
			_ = c.Close()
		}
		// 关键：返回 ErrBadConn 让 sql.DB 标记此 conn 为坏，不再放回 pool
		return driver.ErrBadConn
	})
	// Raw 在我们故意返回 ErrBadConn 时会回传该错误，过滤掉它即可
	if rawErr != nil && !errors.Is(rawErr, driver.ErrBadConn) {
		return rawErr
	}
	return nil
}
