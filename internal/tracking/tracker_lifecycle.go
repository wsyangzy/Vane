package tracking

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"

	_ "modernc.org/sqlite"
)

// NewUsageTracker 创建新的使用跟踪器
func NewUsageTracker(cfg *Config, globalTimezone ...string) (*UsageTracker, error) {
	if cfg == nil || !cfg.Enabled {
		return &UsageTracker{config: cfg}, nil
	}

	// 设置默认值
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 1000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 30 * time.Second
	}
	if cfg.MaxRetry <= 0 {
		cfg.MaxRetry = 3
	}
	cfg.Environment = normalizeTrackingEnvironment(cfg.Environment)
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 24 * time.Hour // 默认24小时清理一次
	}
	// 构建数据库配置
	tz := ""
	if len(globalTimezone) > 0 {
		tz = globalTimezone[0]
	}
	dbConfig, err := buildDatabaseConfig(cfg, tz)
	if err != nil {
		return nil, fmt.Errorf("failed to build database config: %w", err)
	}

	// 创建 SQLite 数据库适配器
	adapter, err := NewSQLiteAdapter(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database adapter: %w", err)
	}

	// 打开数据库连接
	if err := adapter.Open(); err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 获取读写连接（从适配器）
	readDB := adapter.GetReadDB()
	writeDB := adapter.GetWriteDB()

	// 保持原有db字段兼容性（指向readDB）
	db := readDB

	ctx, cancel := context.WithCancel(context.Background())

	// 初始化时区
	timezone := dbConfig.Timezone
	if timezone == "" {
		timezone = "Asia/Shanghai" // 默认时区
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		slog.Warn("加载时区失败，使用Asia/Shanghai", "timezone", timezone, "error", err)
		fallbackLocation, fallbackErr := time.LoadLocation("Asia/Shanghai")
		if fallbackErr != nil {
			slog.Warn("加载默认时区失败，使用固定+8时区", "error", fallbackErr)
			location = time.FixedZone("CST", 8*3600) // 后备方案：固定+8时区
		} else {
			location = fallbackLocation
		}
	}

	ut := &UsageTracker{
		// 原有字段（兼容性）
		db:               db, // 兼容性：指向readDB
		eventChan:        make(chan RequestEvent, cfg.BufferSize),
		config:           cfg,
		environment:      cfg.Environment,
		pricing:          cfg.ModelPricing,
		ctx:              ctx,
		cancel:           cancel,
		retentionChanged: make(chan struct{}, 1),

		// 时区支持
		location: location,

		// 新增：数据库适配器
		adapter: adapter,

		// 读写分离组件（从适配器获取）
		readDB:     readDB,
		writeDB:    writeDB,
		writeQueue: make(chan WriteRequest, cfg.BufferSize), // 与事件队列容量一致

		usageStatsCache: appcache.New[UsageStatsTotals](appcache.Options{
			Name:         "usage:stats",
			TTL:          5 * time.Second,
			MaxEntries:   1000,
			StaleOnError: true,
		}),
		dbCountFallbackCache: appcache.New[int](appcache.Options{
			Name:       "requests:count-fallback",
			MaxEntries: 1000,
			Enabled:    true,
		}),
		archiveTotalsCache: appcache.New[UsageStatsTotals](appcache.Options{
			Name:       "usage:archive-months",
			MaxEntries: 1000,
			Enabled:    true,
		}),
	}

	// 初始化错误处理器
	ut.errorHandler = NewErrorHandler(ut, slog.Default())

	// 初始化数据库Schema（使用适配器）
	if err := ut.initDatabaseWithAdapter(); err != nil {
		cancel()
		if closeErr := adapter.Close(); closeErr != nil {
			slog.Warn("初始化失败后关闭数据库适配器失败", "error", closeErr)
		}
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// 🔧 [P1.5 新增] 启动时扫描僵尸请求
	// 上次进程崩溃时遗留的"未完成"请求（forwarding/streaming/processing/retry/suspended）
	// 需要标记为 crashed_during_processing 失败状态，避免：
	//   1. 前端展示永久"运行中"误导用户
	//   2. 统计聚合时计入异常状态
	//   3. 内存查询时被当成"活跃请求"
	if err := ut.cleanupCrashedRequests(); err != nil {
		// 清理失败不应该阻塞启动，仅记录警告
		slog.Warn("启动时清理僵尸请求失败（不影响启动）", "error", err)
	}
	if err := ut.reconcileHistoryArchives(); err != nil {
		slog.Error("启动时恢复月度历史归档失败", "error", err)
	}
	if err := ut.archiveClosedMonths(context.Background()); err != nil {
		slog.Error("启动时执行请求历史月度归档失败", "error", err)
	}

	// 🔧 [P6 新增] 初始化内存请求计数器：替代 QueryRequestDetailsWithHotPool 中的高频 COUNT 查询
	// 初始化失败不阻塞启动，未初始化时 Get 会返回 (0, false)，
	// 调用方会自动回退到原有 DB count 路径 + 缓存降级，行为兼容。
	ut.dbCounter = NewDBRequestCounter()
	{
		initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := ut.dbCounter.Init(initCtx, ut.readDB, ut.environment); err != nil {
			slog.Warn("初始化数据库请求计数器失败（将降级到 DB count 查询）", "error", err)
		}
		initCancel()
	}

	// 启动写操作处理器
	// 🔧 [P3.4 修复] writeWg.Add 在启动 goroutine 前调用，避免与 Close 的 Wait 形成 race
	// 旧实现：writeWg.Add(1) 在 processWriteQueue 函数体内调用，如果 Close 在 goroutine 调度前先 Wait，
	// 会立即返回（计数为 0），但 goroutine 仍可能在后续启动并访问已关闭资源
	ut.writeWg.Add(1)
	go ut.processWriteQueue()

	// 启动异步事件处理器
	ut.wg.Add(1)
	go ut.processEvents()

	// 启动定期清理任务
	ut.wg.Add(1)
	go ut.periodicCleanup()

	// 启动定期备份任务
	ut.wg.Add(1)
	go ut.periodicBackup()

	// 🔥 初始化热池架构
	ut.initHotPool()

	slog.Info("✅ 使用跟踪器初始化完成",
		"database_type", "sqlite",
		"buffer_size", cfg.BufferSize,
		"batch_size", cfg.BatchSize,
		"hot_pool_enabled", ut.hotPoolEnabled)

	return ut, nil
}

// now 返回当前配置时区的时间
func (ut *UsageTracker) now() time.Time {
	if ut.location == nil {
		return time.Now() // 后备方案
	}
	return time.Now().In(ut.location)
}

// Close 关闭使用跟踪器
func (ut *UsageTracker) Close() error {
	if ut.config == nil || !ut.config.Enabled {
		return nil
	}

	// 先检查是否已经关闭
	ut.mu.RLock()
	if ut.cancel == nil {
		ut.mu.RUnlock()
		return nil // 已经关闭过
	}
	ut.mu.RUnlock()

	slog.Info("Shutting down usage tracker...")

	// 取消上下文（不需要持有锁）
	ut.cancel()

	// 等待所有协程完成（包括写处理器）
	ut.wg.Wait()
	ut.writeWg.Wait() // 等待写处理器完成

	// 现在可以安全地持有写锁进行清理
	ut.mu.Lock()
	defer ut.mu.Unlock()

	ut.cancel = nil // 标记为已关闭

	// 关闭事件通道
	// 🔧 [P3.5 修复] 用 atomic + sync.Once 保证 close 只执行一次且对并发可见
	if ut.eventChan != nil {
		ut.eventChanClose.Do(func() {
			ut.eventChanClosed.Store(true) // 先标记，让 safePublishEvent 提前返回
			close(ut.eventChan)
		})
		ut.eventChan = nil
	}

	// 关闭写操作队列
	if ut.writeQueue != nil {
		close(ut.writeQueue)
		ut.writeQueue = nil
	}

	// 🔥 关闭热池和归档管理器
	if ut.hotPool != nil {
		if err := ut.hotPool.Close(); err != nil {
			slog.Error("Failed to close hot pool", "error", err)
		}
		ut.hotPool = nil
	}
	if ut.archiveManager != nil {
		if err := ut.archiveManager.Close(); err != nil {
			slog.Error("Failed to close archive manager", "error", err)
		}
		ut.archiveManager = nil
	}

	// 关闭数据库适配器（会自动处理所有连接）
	if ut.adapter != nil {
		if err := ut.adapter.Close(); err != nil {
			slog.Error("Failed to close database adapter", "error", err)
			return fmt.Errorf("failed to close database adapter: %w", err)
		}
		ut.adapter = nil
	}

	// 清理连接引用（这些现在由adapter管理）
	ut.readDB = nil
	ut.writeDB = nil
	ut.db = nil

	slog.Info("✅ 使用跟踪器关闭完成")
	return nil
}
