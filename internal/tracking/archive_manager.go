package tracking

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

// ArchiveEvent 归档事件（请求完成时发送）
type ArchiveEvent struct {
	Request     *ActiveRequest
	Timestamp   time.Time
	Dumped      bool
	Environment string
}

func eventEnvironment(event *ArchiveEvent, fallback string) string {
	if event != nil && strings.TrimSpace(event.Environment) != "" {
		return normalizeTrackingEnvironment(event.Environment)
	}
	return normalizeTrackingEnvironment(fallback)
}

// ArchiveManagerConfig 归档管理器配置
type ArchiveManagerConfig struct {
	ChannelSize   int           // 归档通道大小（默认1000）
	BatchSize     int           // 批次大小（默认100）
	FlushInterval time.Duration // 刷新间隔（默认5秒）
	MaxRetry      int           // 最大重试次数（默认3）
	Environment   string        // dev/release 请求追踪作用域
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
	adapter     *SQLiteAdapter
	config      ArchiveManagerConfig

	// 🔧 [P3.3 新增] pricingMu 保护 pricing 和 endpointMu 两个 map 的引用替换
	// 旧实现：UpdatePricing / UpdateEndpointMultipliers 直接替换引用，
	// 与 calculateCost 读这两个 map 形成 race（极端情况下可触发 map 并发读写 panic）
	pricingMu  sync.RWMutex
	pricing    map[string]ModelPricing       // 模型定价缓存
	endpointMu map[string]EndpointMultiplier // 端点倍率缓存

	location    *time.Location
	writeLocker sync.Locker // 与 UsageTracker 写队列复用，避免同连接并发开启事务

	// 热池引用（用于归档成功后清理）
	hotPool *HotPool

	// 🔧 [P6 新增] 数据库请求计数器：归档成功后更新内存计数
	// 用于避免高频 COUNT 查询触发 SQLITE_BUSY，可选注入（nil 时退化为无计数）。
	dbCounter *DBRequestCounter

	archiveSuccessMu       sync.RWMutex
	archiveSuccessCallback func(appcache.Change)

	// 🔧 [P1.7 新增] 同步刷新请求通道
	// Flush() 调用时向此 channel 发送一个回复 channel，
	// archiver 收到后立即刷新当前批次并通过回复 channel 报告完成
	flushReqChan chan chan error
	dumpMu       sync.Mutex

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
func NewArchiveManager(adapter *SQLiteAdapter, config ArchiveManagerConfig, pricing map[string]ModelPricing, location *time.Location) *ArchiveManager {
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

// SetDBCounter 注入数据库请求计数器（用于归档成功后增量维护）
// 🔧 [P6 新增] 与 SetHotPool 同源，避免在构造函数添加新参数破坏调用方
func (am *ArchiveManager) SetDBCounter(counter *DBRequestCounter) {
	am.dbCounter = counter
}

// SetArchiveSuccessCallback registers a callback that runs only after a batch
// has been successfully inserted into request_logs.
func (am *ArchiveManager) SetArchiveSuccessCallback(callback func(appcache.Change)) {
	am.archiveSuccessMu.Lock()
	am.archiveSuccessCallback = callback
	am.archiveSuccessMu.Unlock()
}

func (am *ArchiveManager) notifyArchiveSuccess(events []*ArchiveEvent) {
	am.archiveSuccessMu.RLock()
	callback := am.archiveSuccessCallback
	am.archiveSuccessMu.RUnlock()
	if callback != nil {
		callback(archiveBatchChange(events))
	}
}

// archiveBatchChange 汇总一批归档事件影响的 provider 与 start_time 范围。
// 只看 start_time：所有统计查询都按 start_time 过滤，与写入行的时间维度一致。
func archiveBatchChange(events []*ArchiveEvent) appcache.Change {
	change := appcache.Change{}
	seen := make(map[string]struct{}, 2)
	unbounded := false
	for _, event := range events {
		if event == nil || event.Request == nil {
			continue
		}
		event.Request.mu.RLock()
		provider := defaultProvider(event.Request.Provider)
		startTime := event.Request.StartTime
		event.Request.mu.RUnlock()

		if _, ok := seen[provider]; !ok {
			seen[provider] = struct{}{}
			change.Providers = append(change.Providers, provider)
		}
		if startTime.IsZero() {
			unbounded = true
			continue
		}
		if change.Start.IsZero() || startTime.Before(change.Start) {
			change.Start = startTime
		}
		if change.End.IsZero() || startTime.After(change.End) {
			change.End = startTime
		}
	}
	if unbounded {
		// 无法定位时间的记录按全时间范围处理
		change.Start, change.End = time.Time{}, time.Time{}
	}
	return change
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
// 🔧 [P3.3 新增] 供 calculateCost 等读路径调用，保证读取的 map 引用在使用期间不变
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
		Request:     req,
		Timestamp:   time.Now(),
		Environment: normalizeTrackingEnvironment(am.config.Environment),
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

	// 通道拥堵时施加有界背压；超时则立即转储，绝不静默丢弃。
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case am.archiveChan <- event:
		am.stats.mu.Lock()
		am.stats.TotalReceived++
		am.stats.mu.Unlock()
	case <-timer.C:
		if err := am.dumpFailedBatchToFile([]*ArchiveEvent{event}, fmt.Errorf("archive channel full")); err != nil {
			return fmt.Errorf("archive channel full and dump failed: %w", err)
		}
		slog.Warn("📦 归档通道持续拥堵，事件已持久化转储",
			"request_id", req.RequestID,
			"channel_length", len(am.archiveChan))
	case <-am.ctx.Done():
		if err := am.dumpFailedBatchToFile([]*ArchiveEvent{event}, am.ctx.Err()); err != nil {
			return err
		}
	}
	return nil
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
