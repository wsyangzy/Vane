package app

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/appdir"
	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
	"github.com/wsyangzy/Vane/internal/platform/events"
	"github.com/wsyangzy/Vane/internal/platform/logging"
	"github.com/wsyangzy/Vane/internal/platform/middleware"
	"github.com/wsyangzy/Vane/internal/platform/netutil"
	pricingservice "github.com/wsyangzy/Vane/internal/pricing/service"
	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexproxy "github.com/wsyangzy/Vane/internal/providers/codex/proxy"
	codexsubscription "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
	routingservice "github.com/wsyangzy/Vane/internal/routing/service"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
	"github.com/wsyangzy/Vane/internal/tracking"
	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

type App struct {
	routingWriteMu        sync.Mutex
	routingAffinityMu     sync.Mutex
	poolMu                sync.Mutex
	poolRuntimes          map[string]*routing.PoolRuntime
	profileRuntimes       map[string]*routing.ProfileRuntime
	quotaRefresher        *routing.QuotaRefresher
	quotaRefreshCancel    context.CancelFunc
	quotaRefreshDone      chan struct{}
	quotaActivity         map[string]time.Time
	routingQuotaTransport *http.Transport
	routingCLIProxy       string
	// Wails v3 应用与窗口引用
	wailsApp *application.App
	window   *application.WebviewWindow

	// 核心组件
	config               *config.Config
	logger               *slog.Logger
	endpointManager      *endpoint.Manager
	eventBus             events.EventBus // 接口类型，不是指针
	usageTracker         *tracking.UsageTracker
	storeDB              *sql.DB // 管理/配置专用 DB 连接，避免被 tracking 写入阻塞
	proxyHandler         *proxy.Handler
	loggingMiddleware    *middleware.LoggingMiddleware
	monitoringMiddleware *middleware.MonitoringMiddleware
	authMiddleware       *middleware.AuthMiddleware

	// 读路径缓存治理
	cacheRegistry          *appcache.Registry
	usageSummaryCache      *appcache.Cache[usageSummaryDBSnapshot]
	tokenUsageCache        *appcache.Cache[TokenUsageData]
	endpointCostTodayCache *appcache.Cache[[]EndpointCostItem]
	localUsageReader       *sessionusage.Reader

	// 端点存储 (SQLite)
	endpointStore   claudestore.EndpointStore      // 端点数据持久化
	endpointService *claudeservice.EndpointService // 端点业务服务

	// 渠道存储 (SQLite)
	channelStore   claudestore.ChannelStore      // 渠道数据持久化
	channelService *claudeservice.ChannelService // 渠道业务服务

	// Claude 凭证分组 (SQLite)
	claudeCredentialGroupStore   claudestore.ClaudeCredentialGroupStore      // Claude Token/API Key 分组
	claudeCredentialGroupService *claudeservice.ClaudeCredentialGroupService // Claude 凭证分组业务
	claudeSubscriptions          *subscription.Controller
	codexSubscriptions           *codexsubscription.Controller
	subscriptionAccounts         *accounts.Manager

	// 模型定价存储 (SQLite)
	modelPricingStore   pricingstore.ModelPricingStore      // 模型定价数据持久化
	modelPricingService *pricingservice.ModelPricingService // 模型定价业务服务

	// 模型路由规则 (SQLite, Claude/Codex 共用一张表按 provider 隔离)
	modelRouteService *routingservice.ModelRouteService

	// 系统设置存储 (SQLite)
	settingsStore   settingsstore.SettingsStore      // 设置数据持久化
	settingsService *settingsservice.SettingsService // 设置业务服务
	portManager     *netutil.PortManager             // 端口管理器

	// 隐私防护 (SQLite)
	privacyStore   *privacy.Store
	privacyService *privacy.Service

	// Codex Provider 存储 (SQLite)
	codexProviderStore   codexstore.CodexProviderStore      // Codex Provider 数据持久化（旧接口转调用）
	codexProviderService *codexservice.CodexProviderService // Codex Provider 业务服务（旧接口转调用）

	// Codex 渠道-端点 两层存储 (SQLite, 2026-05-13 引入)
	codexChannelStore           codexstore.CodexChannelStore              // 渠道存储（共用配置）
	codexEndpointStore          codexstore.CodexEndpointStore             // 端点存储（仅 base_url）
	codexCredentialGroupStore   codexstore.CodexCredentialGroupStore      // Codex 凭证分组存储
	codexChannelService         *codexservice.CodexChannelService         // 渠道业务
	codexEndpointService        *codexservice.CodexEndpointService        // 端点业务
	codexCredentialGroupService *codexservice.CodexCredentialGroupService // 凭证分组业务

	// HTTP 代理服务器 (保留，监听配置的端口)
	proxyServer *http.Server

	// Codex 代理组件（合并到主代理端口，通过路径路由）
	codexEndpointMgr   *codexproxy.EndpointManager
	codexHealthCheck   *codexproxy.HealthChecker
	codexSuspensionMgr *codexproxy.SuspensionManager
	codexHTTPHandler   *codexproxy.HTTPHandler
	codexActivationMu  sync.Mutex // 串行化 Codex 激活写库/写配置，避免快捷切换并发

	// 应用状态
	startTime time.Time

	// 端口冲突状态
	portConflict     bool   // 是否存在端口冲突
	portConflictPort int    // 冲突的端口号
	portConflictErr  string // 冲突错误信息

	// 🔧 [P5.7 新增] 启动状态机
	// 旧实现：端口冲突时代理未启动但后台任务都在跑（空耗资源）
	//        数据库失败时部分组件 nil 但应用看起来正常运行
	// 新实现：明确的状态机区分 Starting/Running/Degraded/Failed，
	//        Failed 状态下停止所有不必要的后台任务，前端展示诊断信息
	appState       atomic.Int32 // AppState 枚举值
	appStateReason string       // 进入当前状态的原因（如失败时的具体错误）
	appStateMu     sync.RWMutex // 保护 appStateReason

	// 并发控制
	mu        sync.RWMutex
	isRunning bool
	quitting  int32
	stopChan  chan struct{} // goroutine 退出信号

	// 日志处理器（用于查询和广播）
	logHandler *logging.BroadcastHandler
	logEmitter *logging.EventEmitter
}

func NewApp() *App {
	a := &App{
		startTime:        time.Now(),
		stopChan:         make(chan struct{}),
		localUsageReader: sessionusage.NewPersistentReader(filepath.Join(appdir.Dir(), "cache", "local-usage-v1.gob")),
	}
	a.setupCacheRegistry()
	a.appState.Store(int32(AppStateStarting))
	return a
}
