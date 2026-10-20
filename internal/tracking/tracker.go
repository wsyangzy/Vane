package tracking

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"

	_ "modernc.org/sqlite"
)

// UsageTracker 使用跟踪器
type UsageTracker struct {
	// 原有字段（兼容性）
	db           *sql.DB
	eventChan    chan RequestEvent
	config       *Config
	environment  string                        // dev/release scope for request logs and archives
	pricing      map[string]ModelPricing       // 模型定价缓存
	endpointMu   map[string]EndpointMultiplier // 端点倍率缓存
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.RWMutex
	errorHandler *ErrorHandler

	// 🔧 [P3.5 新增] eventChan 关闭保护：
	//   - eventChanClosed: 标记 channel 是否已关闭
	//   - eventChanClose:  保证 close 只执行一次
	eventChanClosed atomic.Bool
	eventChanClose  sync.Once

	// 🔧 [P5.8 新增] 热池满告警相关
	// hotPoolAlertCb: 告警回调（由 app.go 注册以推送前端 Toast）
	// hotPoolAlertLast: 上次告警时间（防抖）
	// hotPoolDroppedTotal: 累计因热池/事件队列满而丢弃的请求数
	hotPoolAlertCb      atomic.Pointer[HotPoolAlertCallback]
	hotPoolAlertLast    atomic.Int64
	hotPoolDroppedTotal atomic.Int64

	// 时区支持
	location *time.Location // 配置的时区

	// SQLite 数据库适配器
	adapter *SQLiteAdapter

	// 新增：读写分离组件（从适配器获取）
	readDB     *sql.DB           // 读连接池 (多连接)
	writeDB    *sql.DB           // 写连接 (单连接)
	writeQueue chan WriteRequest // 写操作队列
	writeMu    sync.Mutex        // 写操作保护锁
	writeWg    sync.WaitGroup    // 写处理器等待组

	// 🔥 新增：内存热池 + 归档架构
	hotPool        *HotPool        // 内存热池（活跃请求）
	archiveManager *ArchiveManager // 归档管理器（批量写入）
	hotPoolEnabled bool            // 是否启用热池模式

	// 🔧 [P6] 内存计数器：替代高频 count 查询，避免 SQLITE_BUSY
	// 仅支持 total / by-provider 维度的过滤；其他过滤条件回退到 DB 查询
	dbCounter *DBRequestCounter

	// usageStatsCache caches only DB aggregation totals. HotPool data is still
	// added live on every QueryUsageStatsTotalsWithHotPool call.
	usageStatsCache      *appcache.Cache[UsageStatsTotals]
	dbCountFallbackCache *appcache.Cache[int]
	// archiveTotalsCache 缓存月度归档文件的聚合结果。归档文件只在持有 historyMu 写锁时变更，
	// 变更时整表清空，因此条目无需 TTL；活动库部分每次实时查询。
	archiveTotalsCache *appcache.Cache[UsageStatsTotals]

	usageCacheInvalidatorMu sync.RWMutex
	usageCacheInvalidator   func(appcache.Change)

	retentionMu      sync.RWMutex
	retentionChanged chan struct{}
	historyMu        sync.RWMutex
}
