// app.go - Wails 应用核心结构
// 封装所有业务组件，提供生命周期管理

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/codexproxy"
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/events"
	"ai-relay-forwarder/internal/logging"
	"ai-relay-forwarder/internal/middleware"
	"ai-relay-forwarder/internal/proxy"
	"ai-relay-forwarder/internal/service"
	"ai-relay-forwarder/internal/store"
	"ai-relay-forwarder/internal/tracking"
	"ai-relay-forwarder/internal/transport"
	"ai-relay-forwarder/internal/utils"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// App 是 Wails 应用的核心结构
// 它封装了所有业务组件，并暴露方法给前端调用
type App struct {
	// Wails v3 应用与窗口引用
	wailsApp *application.App
	window   *application.WebviewWindow

	// 核心组件
	config               *config.Config
	configWatcher        *config.ConfigWatcher
	logger               *slog.Logger
	endpointManager      *endpoint.Manager
	eventBus             events.EventBus // 接口类型，不是指针
	usageTracker         *tracking.UsageTracker
	storeDB              *sql.DB // 管理/配置专用 DB 连接，避免被 tracking 写入阻塞
	proxyHandler         *proxy.Handler
	loggingMiddleware    *middleware.LoggingMiddleware
	monitoringMiddleware *middleware.MonitoringMiddleware
	authMiddleware       *middleware.AuthMiddleware

	// 端点存储 (SQLite)
	endpointStore   store.EndpointStore      // 端点数据持久化
	endpointService *service.EndpointService // 端点业务服务

	// 渠道存储 (SQLite)
	channelStore   store.ChannelStore      // 渠道数据持久化
	channelService *service.ChannelService // 渠道业务服务

	// 模型定价存储 (SQLite)
	modelPricingStore   store.ModelPricingStore      // 模型定价数据持久化
	modelPricingService *service.ModelPricingService // 模型定价业务服务

	// 系统设置存储 (SQLite)
	settingsStore   store.SettingsStore      // 设置数据持久化
	settingsService *service.SettingsService // 设置业务服务
	portManager     *utils.PortManager       // 端口管理器

	// Codex Provider 存储 (SQLite)
	codexProviderStore   store.CodexProviderStore      // Codex Provider 数据持久化（旧接口转调用）
	codexProviderService *service.CodexProviderService // Codex Provider 业务服务（旧接口转调用）

	// Codex 渠道-端点 两层存储 (SQLite, 2026-05-13 引入)
	codexChannelStore    store.CodexChannelStore       // 渠道存储（共用配置）
	codexEndpointStore   store.CodexEndpointStore      // 端点存储（仅 base_url）
	codexChannelService  *service.CodexChannelService  // 渠道业务
	codexEndpointService *service.CodexEndpointService // 端点业务

	// HTTP 代理服务器 (保留，监听配置的端口)
	proxyServer *http.Server

	// Codex 代理组件（合并到主代理端口，通过路径路由）
	codexEndpointMgr  *codexproxy.EndpointManager
	codexHealthCheck  *codexproxy.HealthChecker
	codexSuspensionMgr *codexproxy.SuspensionManager

	// 应用状态
	startTime  time.Time
	configPath string

	// 端口冲突状态
	portConflict     bool   // 是否存在端口冲突
	portConflictPort int    // 冲突的端口号
	portConflictErr  string // 冲突错误信息

	// 🔧 [P5.7 新增] 启动状态机
	// 旧实现：端口冲突时代理未启动但后台任务都在跑（空耗资源）
	//        数据库失败时部分组件 nil 但应用看起来正常运行
	// 新实现：明确的状态机区分 Starting/Running/Degraded/Failed，
	//        Failed 状态下停止所有不必要的后台任务，前端展示诊断信息
	appState        atomic.Int32 // AppState 枚举值
	appStateReason  string       // 进入当前状态的原因（如失败时的具体错误）
	appStateMu      sync.RWMutex // 保护 appStateReason

	// 并发控制
	mu        sync.RWMutex
	isRunning bool
	quitting  int32
	stopChan  chan struct{} // goroutine 退出信号

	// 日志处理器（用于查询和广播）
	logHandler *logging.BroadcastHandler
	logEmitter *logging.EventEmitter
}

// NewApp 创建新的应用实例
func NewApp() *App {
	a := &App{
		startTime: time.Now(),
		stopChan:  make(chan struct{}),
	}
	a.appState.Store(int32(AppStateStarting))
	return a
}

// AppState 启动状态枚举
//
// 🔧 [P5.7 新增] 明确启动过程的状态
type AppState int32

const (
	// AppStateStarting 启动中
	AppStateStarting AppState = iota
	// AppStateRunning 正常运行（所有核心组件就绪）
	AppStateRunning
	// AppStateDegraded 降级运行（部分组件失败，但仍能提供主要功能）
	AppStateDegraded
	// AppStateFailed 启动失败（核心组件失败，应用无法正常服务）
	AppStateFailed
)

// String 返回 AppState 的字符串表示
func (s AppState) String() string {
	switch s {
	case AppStateStarting:
		return "starting"
	case AppStateRunning:
		return "running"
	case AppStateDegraded:
		return "degraded"
	case AppStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// setAppState 设置应用状态（线程安全）
// 🔧 [P5.7 新增]
func (a *App) setAppState(state AppState, reason string) {
	a.appState.Store(int32(state))
	a.appStateMu.Lock()
	a.appStateReason = reason
	a.appStateMu.Unlock()
	if a.logger != nil {
		a.logger.Info("📍 应用状态变更",
			"state", state.String(),
			"reason", reason)
	}
}

// GetAppState 返回当前应用状态（线程安全，可被前端调用）
// 🔧 [P5.7 新增 - 暴露给前端]
func (a *App) GetAppState() map[string]string {
	state := AppState(a.appState.Load())
	a.appStateMu.RLock()
	reason := a.appStateReason
	a.appStateMu.RUnlock()
	return map[string]string{
		"state":  state.String(),
		"reason": reason,
	}
}

// startup 在 Wails 应用启动时调用
// 这里初始化所有组件并启动代理服务器
func (a *App) startup() {

	// 1. 加载配置
	a.loadConfig()

	// 2. 初始化日志
	a.setupLogger()

	// 3. 显示启动信息
	a.logger.Info("🚀 AI Relay Forwarder 桌面版启动中...",
		"version", Version,
		"config_file", a.configPath)

	// 4. 初始化事件总线
	a.setupEventBus()

	// 5. 初始化使用追踪（SQLite 存储需要依赖数据库）
	a.setupUsageTracker()

	// 5.2 初始化管理/配置 DB（channels/endpoints/settings/codex_providers 等）
	a.setupStoreDB()

	// 5.5 初始化设置服务 (SQLite)
	a.setupSettingsStore()

	// 6. 创建端点管理器（但不启动健康检查）
	a.endpointManager = endpoint.NewManager(a.config)
	a.endpointManager.SetEventBus(a.eventBus)
	// Wails 桌面应用：设置健康检查完成回调，推送事件到前端
	a.endpointManager.SetOnHealthCheckComplete(func() {
		a.emitEndpointUpdate()
	})

	// 设置故障转移回调：以“渠道(channel)”为单位同步数据库状态
	a.endpointManager.SetOnFailoverTriggered(func(failedChannel, newChannel string) {
		if a.endpointService != nil {
			ctx := context.Background()
			if newChannel != "" {
				if err := a.endpointService.ActivateChannel(ctx, newChannel); err != nil {
					slog.Warn(fmt.Sprintf("⚠️ [故障转移回调] 激活新渠道失败: %s - %v", newChannel, err))
				}
			}
			slog.Info(fmt.Sprintf("✅ [故障转移回调] 渠道已切换并同步数据库: %s → %s", failedChannel, newChannel))
		}

		// 推送事件到前端
		a.emitEndpointUpdate()
	})

	// 7. 初始化端点存储 (SQLite, 需要在创建 Manager 之后)
	// 从数据库同步端点到 Manager
	if a.config.EndpointsStorage.Type == "sqlite" {
		a.setupEndpointStore()
	}

	// 7.5 初始化模型定价存储 (SQLite)
	a.setupModelPricingStore()

	// 7.55 初始化 Codex Provider 存储 (SQLite)
	a.setupCodexProviderStore()

	// 7.6 同步端点倍率到 UsageTracker（用于成本计算）
	a.syncEndpointMultipliersToTracker(context.Background())

	// 8. 启动端点管理器（此时端点已从数据库加载完成）
	a.endpointManager.Start()

	// 显示代理配置
	if a.config.Proxy.Enabled {
		proxyInfo := transport.GetProxyInfo(a.config)
		a.logger.Info("🔗 " + proxyInfo)
	}

	// 9. 初始化代理处理器
	a.setupProxyHandler()

	// 9.5 初始化 Codex 代理组件（如果配置启用）— 须在 startProxyServer 前完成，以便注册路由
	a.initCodexProxy()

	// 10. 启动 HTTP 代理服务器
	proxyStarted := a.startProxyServer()

	// 🔧 [P5.7] 启动状态决策：根据核心组件状态确定 AppState
	// 必须在后台任务启动前决定状态，以便 Failed 状态下跳过不必要的后台任务
	if !proxyStarted {
		// 核心组件失败：代理服务器未启动 = 应用无法提供服务
		a.setAppState(AppStateFailed, "代理服务器未启动（端口冲突或绑定失败）")
		// Failed 状态：不启动后台空耗任务，仅保留 UI 让用户重试
		a.isRunning = false
		a.logger.Error("❌ AI Relay Forwarder 启动未完成：代理服务器未启动",
			"configured_port", a.config.Server.Port,
			"hint", "请在前端检查端口冲突诊断信息，修改端口后重启")
		return
	}

	// 核心组件就绪：检查降级条件
	if a.storeDB == nil || a.settingsStore == nil {
		// 数据库不可用：可以转发请求，但无法保存配置/查看历史
		a.setAppState(AppStateDegraded, "配置存储不可用（数据库初始化失败），无法保存设置变更")
	} else {
		a.setAppState(AppStateRunning, "")
	}

	// 11. 设置配置热重载（仅 Running/Degraded 状态启动）
	a.setupConfigReload()

	// 12. 设置事件桥接
	a.setupEventBridges()

	// 13. 启动历史数据收集器
	a.startHistoryCollector()

	a.isRunning = proxyStarted
	a.logger.Info("✅ AI Relay Forwarder 启动完成",
		"proxy_port", a.config.Server.Port,
		"app_state", AppState(a.appState.Load()).String())
}

// shutdown 在 Wails 应用关闭时调用
func (a *App) shutdown() {
	a.mu.Lock()
	logger := a.logger
	proxyServer := a.proxyServer
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	endpointManager := a.endpointManager
	eventBus := a.eventBus
	configWatcher := a.configWatcher
	logEmitter := a.logEmitter
	a.isRunning = false
	a.mu.Unlock()

	// 0. 关闭 goroutine 退出信号
	close(a.stopChan)

	if logger != nil {
		logger.Info("🛑 正在关闭 AI Relay Forwarder...")
	}

	// 1. 停止接收新请求
	if proxyServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := proxyServer.Shutdown(shutdownCtx); err != nil {
			_ = proxyServer.Close()
			if logger != nil {
				logger.Error("代理服务器关闭失败", "error", err)
			}
		}
	}

	// 1.1 停止 Codex 健康检查
	a.stopCodexProxy()

	// 2. 关闭使用追踪 (flush 数据库)
	if usageTracker != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := usageTracker.Close(); err != nil {
				if logger != nil {
					logger.Error("使用追踪器关闭失败", "error", err)
				}
			}
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			if logger != nil {
				logger.Warn("使用追踪器关闭超时，强制继续退出")
			}
		}
	}

	// 2.1 关闭管理/配置 DB
	if storeDB != nil {
		if err := storeDB.Close(); err != nil {
			if logger != nil {
				logger.Error("管理数据库关闭失败", "error", err)
			}
		}
	}

	// 3. 关闭端点管理器
	if endpointManager != nil {
		endpointManager.Stop()
	}

	// 4. 关闭事件总线
	if eventBus != nil {
		if err := eventBus.Stop(); err != nil {
			if logger != nil {
				logger.Error("事件总线关闭失败", "error", err)
			}
		}
	}

	// 5. 关闭配置监听
	if configWatcher != nil {
		_ = configWatcher.Close()
	}

	// 6. 停止日志事件发射器
	if logEmitter != nil {
		logEmitter.Stop()
	}

	a.mu.Lock()
	a.proxyServer = nil
	a.usageTracker = nil
	a.storeDB = nil
	a.mu.Unlock()

	if logger != nil {
		logger.Info("✅ AI Relay Forwarder 已关闭")
	}
}

// onWindowReady 在窗口首次准备就绪时调用（替代 v2 的 domReady）
func (a *App) onWindowReady() {
	// 发送初始状态给前端
	a.emitSystemStatus()
}

// isQuitting 检查是否正在退出
func (a *App) isQuitting() bool {
	return atomic.LoadInt32(&a.quitting) == 1
}

// markQuitting 标记应用进入退出流程（用于放行窗口关闭钩子）
func (a *App) markQuitting() {
	atomic.StoreInt32(&a.quitting, 1)
}

// ShowMainWindow 显示主窗口（用于托盘菜单/单实例唤起）
func (a *App) ShowMainWindow() {
	if a.window == nil {
		return
	}
	a.window.Show()
	a.window.Focus()
}

// HideMainWindow 隐藏主窗口（用于托盘菜单）
func (a *App) HideMainWindow() {
	if a.window == nil {
		return
	}
	a.window.Hide()
}

// RequestQuit 请求退出应用（托盘菜单"退出"使用）
// 优雅退出：先让 Wails 处理关闭，超时后强制退出
func (a *App) RequestQuit() {
	a.markQuitting()

	// 在 goroutine 中执行退出，避免阻塞 UI
	go func() {
		// 调用 Wails Quit
		if a.wailsApp != nil {
			a.wailsApp.Quit()
		}

		// 等待 500ms 让 Wails 完成清理
		time.Sleep(500 * time.Millisecond)

		// 保底退出
		os.Exit(0)
	}()
}

// OpenLogDir 打开日志目录（托盘菜单使用）
func (a *App) OpenLogDir() {
	logDir := utils.GetLogDir()

	// 确保目录存在
	if err := os.MkdirAll(logDir, 0755); err != nil {
		if a.logger != nil {
			a.logger.Warn("创建日志目录失败", "error", err)
		}
		return
	}

	// 跨平台打开目录
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", logDir)
	case "darwin":
		cmd = exec.Command("open", logDir)
	case "linux":
		cmd = exec.Command("xdg-open", logDir)
	default:
		if a.logger != nil {
			a.logger.Warn("不支持的操作系统", "os", goruntime.GOOS)
		}
		return
	}

	if err := cmd.Start(); err != nil {
		if a.logger != nil {
			a.logger.Warn("打开日志目录失败", "error", err, "path", logDir)
		}
	}
}

// OpenClaudeConfigDir 打开 Claude 配置目录（托盘菜单使用）
// Claude 配置目录通常位于用户主目录下的 .claude 文件夹
func (a *App) OpenClaudeConfigDir() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("获取用户主目录失败", "error", err)
		}
		return
	}

	claudeConfigDir := filepath.Join(homeDir, ".claude")

	// 检查目录是否存在
	if _, err := os.Stat(claudeConfigDir); os.IsNotExist(err) {
		if a.logger != nil {
			a.logger.Warn("Claude 配置目录不存在", "path", claudeConfigDir)
		}
		// 仍然尝试打开，让文件管理器显示错误
	}

	// 跨平台打开目录
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", claudeConfigDir)
	case "darwin":
		cmd = exec.Command("open", claudeConfigDir)
	case "linux":
		cmd = exec.Command("xdg-open", claudeConfigDir)
	default:
		if a.logger != nil {
			a.logger.Warn("不支持的操作系统", "os", goruntime.GOOS)
		}
		return
	}

	if err := cmd.Start(); err != nil {
		if a.logger != nil {
			a.logger.Warn("打开 Claude 配置目录失败", "error", err, "path", claudeConfigDir)
		}
	}
}

// OpenCodexConfigDir 打开 Codex 配置目录
// Codex 配置目录通常位于用户主目录下的 .codex 文件夹（或 CODEX_HOME 环境变量指定）
func (a *App) OpenCodexConfigDir() {
	codexHome, err := a.getCodexHomeDir()
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("获取 Codex 配置目录失败", "error", err)
		}
		return
	}

	// 检查目录是否存在
	if _, err := os.Stat(codexHome); os.IsNotExist(err) {
		if a.logger != nil {
			a.logger.Warn("Codex 配置目录不存在", "path", codexHome)
		}
		// 仍然尝试打开，让文件管理器显示错误
	}

	// 跨平台打开目录
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", codexHome)
	case "darwin":
		cmd = exec.Command("open", codexHome)
	case "linux":
		cmd = exec.Command("xdg-open", codexHome)
	default:
		if a.logger != nil {
			a.logger.Warn("不支持的操作系统", "os", goruntime.GOOS)
		}
		return
	}

	if err := cmd.Start(); err != nil {
		if a.logger != nil {
			a.logger.Warn("打开 Codex 配置目录失败", "error", err, "path", codexHome)
		}
	}
}

// GetProxyAddress 获取当前代理地址（托盘菜单"复制代理地址"使用）
func (a *App) GetProxyAddress() string {
	host := "127.0.0.1"
	port := 8087 // 默认端口

	if a.config != nil && a.config.Server.Host != "" && a.config.Server.Host != "localhost" {
		host = a.config.Server.Host
	}
	if a.portManager != nil {
		port = a.portManager.GetActualPort()
	} else if a.config != nil {
		port = a.config.Server.Port
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}

// loadConfig 加载配置
func (a *App) loadConfig() {
	// 创建临时 logger 用于初始化
	tempLogger := slog.Default()

	// 确保应用目录存在
	if err := utils.EnsureAppDirs(); err != nil {
		tempLogger.Warn("⚠️ 无法创建应用目录", "error", err)
	} else {
		tempLogger.Info("📁 应用目录已就绪",
			"appdir", utils.GetAppDataDir(),
			"data", utils.GetDataDir(),
			"logs", utils.GetLogDir())
	}

	// 直接从嵌入的配置加载（不写文件）
	tempLogger.Info("📝 从嵌入配置加载")

	// 将嵌入的配置写入临时文件进行解析
	tmpConfigPath := filepath.Join(os.TempDir(), "ai-relay-forwarder-config.yaml")

	// 先修改配置内容，替换路径为用户目录
	configContent := string(defaultConfigContent)
	// 注意：不修改配置内容，而是加载后再覆盖路径

	if err := os.WriteFile(tmpConfigPath, []byte(configContent), 0644); err != nil {
		panic(fmt.Sprintf("无法创建临时配置文件: %v", err))
	}
	defer os.Remove(tmpConfigPath)

	// 创建配置监听器（此时会调用 SetDefaults 设置默认路径）
	configWatcher, err := config.NewConfigWatcher(tmpConfigPath, tempLogger)
	if err != nil {
		panic(fmt.Sprintf("无法加载配置: %v", err))
	}

	a.configWatcher = configWatcher
	cfg := configWatcher.GetConfig()

	// ⚠️ 关键：立即覆盖所有路径为用户目录（在任何组件初始化之前）
	// Dev 模式使用独立的数据库和日志文件，避免与 release 版本冲突
	logFileName := "app.log"
	dbFileName := "usage.db"
	if BuildMode == "dev" {
		logFileName = "app-dev.log"
		dbFileName = "usage-dev.db"
	}
	cfg.Logging.FilePath = filepath.Join(utils.GetLogDir(), logFileName)
	// 为避免与旧版本共享同一 usage.db 产生锁冲突，默认切换到新文件名，并在启动时自动迁移旧数据。
	cfg.UsageTracking.DatabasePath = filepath.Join(utils.GetDataDir(), dbFileName)

	// 同时设置 Database 配置（如果存在）
	if cfg.UsageTracking.Database != nil {
		// Database 配置中没有 DatabasePath 字段，UsageTracking.DatabasePath 是统一路径
	}

	a.config = cfg

	tempLogger.Info("✅ 配置加载完成",
		"log_path", a.config.Logging.FilePath,
		"db_path", a.config.UsageTracking.DatabasePath,
		"appdir", utils.GetAppDataDir())

	a.configPath = tmpConfigPath
}

// setupLogger 设置日志
func (a *App) setupLogger() {
	logger, broadcastHandler := setupLogger(a.config.Logging)
	a.logger = logger
	slog.SetDefault(logger)

	// 存储日志处理器和发射器引用
	a.logHandler = broadcastHandler
	a.logEmitter = broadcastHandler.Emitter

	// 初始化调试配置
	utils.SetDebugConfig(a.config)

	a.logger.Info("✅ 日志系统初始化完成",
		"level", a.config.Logging.Level,
		"file_enabled", a.config.Logging.FileEnabled)
}

// setupEventBus 设置事件总线
func (a *App) setupEventBus() {
	a.eventBus = events.NewEventBus(a.logger)
	if err := a.eventBus.Start(); err != nil {
		a.logger.Error("事件总线启动失败", "error", err)
	}
}

// setupEndpointStore 设置端点存储 (SQLite)
func (a *App) setupEndpointStore() {
	// 获取数据库连接：优先使用 storeDB（避免 tracking 写入阻塞管理写操作）。
	// 注意：端点/渠道管理属于“管理存储”，不应强耦合到 usage tracking 是否启用。
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	if db == nil {
		a.logger.Error("❌ 无法获取数据库连接")
		return
	}

	// 创建 EndpointStore
	a.endpointStore = store.NewSQLiteEndpointStore(db)

	// 创建 EndpointService
	a.endpointService = service.NewEndpointService(a.endpointStore, a.endpointManager, a.config)

	// 创建 ChannelStore / ChannelService
	a.channelStore = store.NewSQLiteChannelStore(db)
	a.channelService = service.NewChannelService(a.channelStore)

	// 从数据库同步端点到内存
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.endpointService.SyncFromDatabase(ctx); err != nil {
		a.logger.Warn("⚠️ 从数据库同步端点失败，使用 YAML 配置", "error", err)
	} else {
		// 回填渠道表，保证“端点删光后渠道仍存在”
		if a.channelService != nil {
			if added, err := a.channelService.BackfillChannelsFromEndpoints(ctx, a.endpointStore); err != nil {
				a.logger.Warn("⚠️ 渠道回填失败", "error", err)
			} else if added > 0 {
				a.logger.Info("✅ 渠道回填完成", "count", added)
			}
		}

		// 同步渠道优先级到运行时，用于渠道间故障转移顺序
		a.syncChannelPrioritiesToEndpointManager(ctx)
		// 同步渠道“参与故障转移”开关（暂停/恢复持久化）
		a.syncChannelFailoverEnabledToEndpointManager(ctx)
		a.logger.Info("✅ 端点存储已启用 (SQLite)")
	}
}

// setupModelPricingStore 设置模型定价存储 (SQLite)
func (a *App) setupModelPricingStore() {
	a.ensureModelPricingService()
	if a.modelPricingService == nil {
		if a.logger != nil {
			a.logger.Debug("模型定价存储跳过初始化 (数据库未就绪)")
		}
		return
	}

	// 检查是否需要初始化默认数据
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	count, err := a.modelPricingService.GetPricingCount(ctx)
	if err != nil {
		a.logger.Warn("⚠️ 检查模型定价数据失败", "error", err)
		return
	}

	// 如果表为空，初始化默认数据
	if count == 0 {
		a.initDefaultModelPricing(ctx)
	}

	// 加载缓存
	if err := a.modelPricingService.LoadCache(ctx); err != nil {
		a.logger.Warn("⚠️ 加载模型定价缓存失败", "error", err)
	}

	// 同步定价到 UsageTracker（用于成本计算）
	a.syncPricingToTracker(ctx)

	a.logger.Info("✅ 模型定价存储已启用 (SQLite)", "count", count)
}

// ensureModelPricingService 确保模型定价服务已初始化。
// 目的：避免因启动顺序/初始化失败导致前端提示“未启用”，同时不把模型定价强耦合到 UsageTracker 的生命周期。
func (a *App) ensureModelPricingService() {
	a.mu.RLock()
	if a.modelPricingService != nil {
		a.mu.RUnlock()
		return
	}
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()

	if db == nil {
		return
	}

	modelPricingStore := store.NewSQLiteModelPricingStore(db)
	modelPricingService := service.NewModelPricingService(modelPricingStore)

	a.mu.Lock()
	if a.modelPricingService == nil {
		a.modelPricingStore = modelPricingStore
		a.modelPricingService = modelPricingService
	}
	a.mu.Unlock()
}

func (a *App) setupCodexProviderStore() {
	a.ensureCodexProviderService()
	if a.codexProviderService == nil {
		if a.logger != nil {
			a.logger.Debug("Codex Provider 存储跳过初始化 (数据库未就绪)")
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.ensureCodexProviderSchema(ctx); err != nil {
		a.logger.Warn("⚠️ 初始化 Codex Provider 数据表结构失败", "error", err)
		return
	}

	providers, err := a.codexProviderService.ListProviders(ctx)
	if err != nil {
		a.logger.Warn("⚠️ 加载 Codex Provider 列表失败", "error", err)
		return
	}

	a.logger.Info("✅ Codex Provider 存储已启用 (SQLite)", "count", len(providers))
}

func (a *App) ensureCodexProviderService() {
	a.mu.RLock()
	if a.codexProviderService != nil {
		a.mu.RUnlock()
		return
	}
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()

	if db == nil {
		return
	}

	codexProviderStore := store.NewSQLiteCodexProviderStore(db)
	codexProviderService := service.NewCodexProviderService(codexProviderStore)
	codexChannelStore := store.NewSQLiteCodexChannelStore(db)
	codexEndpointStore := store.NewSQLiteCodexEndpointStore(db)
	codexChannelService := service.NewCodexChannelService(codexChannelStore, codexEndpointStore)
	codexEndpointService := service.NewCodexEndpointService(codexEndpointStore, codexChannelStore)

	a.mu.Lock()
	if a.codexProviderService == nil {
		a.codexProviderStore = codexProviderStore
		a.codexProviderService = codexProviderService
	}
	if a.codexChannelService == nil {
		a.codexChannelStore = codexChannelStore
		a.codexEndpointStore = codexEndpointStore
		a.codexChannelService = codexChannelService
		a.codexEndpointService = codexEndpointService
	}
	a.mu.Unlock()
}

// initDefaultModelPricing 初始化默认模型定价数据
func (a *App) initDefaultModelPricing(ctx context.Context) {
	// Claude 官方定价 (2025年最新)
	// 5分钟缓存: input * 1.25, 1小时缓存: input * 2.0, 读取: input * 0.1
	defaultPricings := []*store.ModelPricingRecord{
		// 默认定价
		{
			ModelName:            "_default",
			DisplayName:          "默认定价",
			Description:          "未知模型使用的默认定价",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75, // 3.0 * 1.25
			CacheCreationPrice1h: 6.0,  // 3.0 * 2.0
			CacheReadPrice:       0.30, // 3.0 * 0.1
			IsDefault:            true,
		},
		// Claude Sonnet 4
		{
			ModelName:            "claude-sonnet-4-20250514",
			DisplayName:          "Claude Sonnet 4",
			Description:          "Claude Sonnet 4 (2025-05-14)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75,
			CacheCreationPrice1h: 6.0,
			CacheReadPrice:       0.30,
		},
		// Claude 3.5 Sonnet
		{
			ModelName:            "claude-3-5-sonnet-20241022",
			DisplayName:          "Claude 3.5 Sonnet",
			Description:          "Claude 3.5 Sonnet (2024-10-22)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75,
			CacheCreationPrice1h: 6.0,
			CacheReadPrice:       0.30,
		},
		// Claude 3.5 Haiku
		{
			ModelName:            "claude-3-5-haiku-20241022",
			DisplayName:          "Claude 3.5 Haiku",
			Description:          "Claude 3.5 Haiku (2024-10-22)",
			InputPrice:           0.80,
			OutputPrice:          4.0,
			CacheCreationPrice5m: 1.0,  // 0.80 * 1.25
			CacheCreationPrice1h: 1.6,  // 0.80 * 2.0
			CacheReadPrice:       0.08, // 0.80 * 0.1
		},
		// Claude Opus 4
		{
			ModelName:            "claude-opus-4-20250514",
			DisplayName:          "Claude Opus 4",
			Description:          "Claude Opus 4 (2025-05-14)",
			InputPrice:           15.0,
			OutputPrice:          75.0,
			CacheCreationPrice5m: 18.75, // 15.0 * 1.25
			CacheCreationPrice1h: 30.0,  // 15.0 * 2.0
			CacheReadPrice:       1.50,  // 15.0 * 0.1
		},
		// ========== Claude 4.5 系列 (2025年最新) ==========
		// Claude Sonnet 4.5
		{
			ModelName:            "claude-sonnet-4-5-20250929",
			DisplayName:          "Claude Sonnet 4.5",
			Description:          "Claude Sonnet 4.5 (2025-09-29)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75, // 3.0 * 1.25
			CacheCreationPrice1h: 6.0,  // 3.0 * 2.0
			CacheReadPrice:       0.30, // 3.0 * 0.1
		},
		// Claude Haiku 4.5
		{
			ModelName:            "claude-haiku-4-5-20251001",
			DisplayName:          "Claude Haiku 4.5",
			Description:          "Claude Haiku 4.5 (2025-10-01)",
			InputPrice:           1.0,
			OutputPrice:          5.0,
			CacheCreationPrice5m: 1.25, // 1.0 * 1.25
			CacheCreationPrice1h: 2.0,  // 1.0 * 2.0
			CacheReadPrice:       0.10, // 1.0 * 0.1
		},
		// Claude Opus 4.5
		{
			ModelName:            "claude-opus-4-5-20251101",
			DisplayName:          "Claude Opus 4.5",
			Description:          "Claude Opus 4.5 (2025-11-01)",
			InputPrice:           5.0,
			OutputPrice:          25.0,
			CacheCreationPrice5m: 6.25, // 5.0 * 1.25
			CacheCreationPrice1h: 10.0, // 5.0 * 2.0
			CacheReadPrice:       0.50, // 5.0 * 0.1
		},
		// ========== 旧版本兼容 ==========
		{
			ModelName:            "claude-3-opus-20240229",
			DisplayName:          "Claude 3 Opus",
			Description:          "Claude 3 Opus (2024-02-29)",
			InputPrice:           15.0,
			OutputPrice:          75.0,
			CacheCreationPrice5m: 18.75,
			CacheCreationPrice1h: 30.0,
			CacheReadPrice:       1.50,
		},
		{
			ModelName:            "claude-3-sonnet-20240229",
			DisplayName:          "Claude 3 Sonnet",
			Description:          "Claude 3 Sonnet (2024-02-29)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75,
			CacheCreationPrice1h: 6.0,
			CacheReadPrice:       0.30,
		},
		{
			ModelName:            "claude-3-haiku-20240307",
			DisplayName:          "Claude 3 Haiku",
			Description:          "Claude 3 Haiku (2024-03-07)",
			InputPrice:           0.25,
			OutputPrice:          1.25,
			CacheCreationPrice5m: 0.31,  // 0.25 * 1.25
			CacheCreationPrice1h: 0.50,  // 0.25 * 2.0
			CacheReadPrice:       0.025, // 0.25 * 0.1
		},
		// ========== Codex 模型定价（OpenAI 格式，无 cache 概念）==========
		{
			ModelName:   "o3",
			DisplayName: "O3",
			Provider:    "codex",
			InputPrice:  2.0,
			OutputPrice: 8.0,
			Description: "O3 标准模型",
		},
		{
			ModelName:   "o4-mini",
			DisplayName: "O4 Mini",
			Provider:    "codex",
			InputPrice:  1.10,
			OutputPrice: 4.40,
			Description: "O4 Mini 轻量模型",
		},
		{
			ModelName:   "codex-mini-latest",
			DisplayName: "Codex Mini Latest",
			Provider:    "codex",
			InputPrice:  1.50,
			OutputPrice: 6.0,
			Description: "Codex Mini 最新版本",
		},
		{
			ModelName:   "_codex_default",
			DisplayName: "Codex 默认定价",
			Provider:    "codex",
			InputPrice:  2.0,
			OutputPrice: 8.0,
			IsDefault:   true,
			Description: "Codex 默认定价方案",
		},
	}

	if err := a.modelPricingStore.BatchUpsert(ctx, defaultPricings); err != nil {
		a.logger.Error("❌ 初始化默认模型定价失败", "error", err)
		return
	}

	a.logger.Info("✅ 已初始化默认模型定价", "count", len(defaultPricings))
}

// syncPricingToTracker 同步模型定价到 UsageTracker
func (a *App) syncPricingToTracker(ctx context.Context) {
	if a.usageTracker == nil || a.modelPricingService == nil {
		return
	}

	records, err := a.modelPricingService.ListPricings(ctx)
	if err != nil {
		a.logger.Warn("⚠️ 获取模型定价列表失败", "error", err)
		return
	}

	// 转换为 tracking.ModelPricing 格式
	pricing := make(map[string]tracking.ModelPricing)
	for _, r := range records {
		pricing[r.ModelName] = a.modelPricingService.ToTrackingPricing(r)
	}

	// 更新 UsageTracker 的定价缓存
	a.usageTracker.UpdatePricing(pricing)
	a.logger.Debug("已同步模型定价到 UsageTracker", "count", len(pricing))
}

// syncEndpointMultipliersToTracker 同步端点倍率到 UsageTracker
// 成本计算公式：模型基础定价 * 细分倍率 * 总倍率
// 同时同步 Claude 端点（endpoints 表）与 Codex 端点（codex_endpoints + codex_channels）
func (a *App) syncEndpointMultipliersToTracker(ctx context.Context) {
	if a.usageTracker == nil {
		return
	}

	multipliers := make(map[string]tracking.EndpointMultiplier)

	// 1. Claude 端点倍率
	if a.endpointStore != nil {
		endpoints, err := a.endpointStore.List(ctx)
		if err != nil {
			a.logger.Warn("⚠️ 获取端点列表失败", "error", err)
		} else {
			for _, ep := range endpoints {
				key := tracking.EndpointMultiplierKey(ep.Channel, ep.Name)
				multipliers[key] = tracking.EndpointMultiplier{
					CostMultiplier:                ep.CostMultiplier,
					InputCostMultiplier:           ep.InputCostMultiplier,
					OutputCostMultiplier:          ep.OutputCostMultiplier,
					CacheCreationCostMultiplier:   ep.CacheCreationCostMultiplier,
					CacheCreationCostMultiplier1h: ep.CacheCreationCostMultiplier1h,
					CacheReadCostMultiplier:       ep.CacheReadCostMultiplier,
				}
			}
		}
	}

	// 2. Codex 端点倍率（每个端点的 key = channel.name::endpoint.name）
	if a.codexChannelStore != nil && a.codexEndpointStore != nil {
		channels, err := a.codexChannelStore.List(ctx)
		if err != nil {
			a.logger.Warn("⚠️ 获取 Codex 渠道列表失败", "error", err)
		} else {
			for _, ch := range channels {
				codexEndpoints, err := a.codexEndpointStore.ListByChannelID(ctx, ch.ID)
				if err != nil {
					a.logger.Warn("⚠️ 获取 Codex 端点列表失败", "channel", ch.Name, "error", err)
					continue
				}
				for _, ep := range codexEndpoints {
					key := tracking.EndpointMultiplierKey(ch.Name, ep.Name)
					// Codex 端点目前没有 5m/1h 拆分倍率，统一应用 CacheReadCostMultiplier；
					// 缓存创建倍率默认 1.0（不影响计算）
					multipliers[key] = tracking.EndpointMultiplier{
						CostMultiplier:                ep.CostMultiplier,
						InputCostMultiplier:           ep.InputCostMultiplier,
						OutputCostMultiplier:          ep.OutputCostMultiplier,
						CacheCreationCostMultiplier:   1.0,
						CacheCreationCostMultiplier1h: 1.0,
						CacheReadCostMultiplier:       ep.CacheReadCostMultiplier,
					}
				}
			}
		}
	}

	// 更新 UsageTracker 的端点倍率缓存
	a.usageTracker.UpdateEndpointMultipliers(multipliers)
	a.logger.Debug("已同步端点倍率到 UsageTracker", "count", len(multipliers))
}

// getEffectiveUsageDBPath returns the single SQLite database path used by:
// - usage tracker (request_logs / usage_summary / ...)
// - management stores (channels/endpoints/settings/model_pricing/codex_providers)
//
// 统一数据库路径：内部只使用 UsageTracking.DatabasePath（由 config.SetDefaults 将 database.path 归一到该字段）。
func (a *App) getEffectiveUsageDBPath() string {
	if a == nil || a.config == nil {
		return ""
	}
	if a.config.UsageTracking.DatabasePath != "" {
		return a.config.UsageTracking.DatabasePath
	}
	return filepath.Join(utils.GetDataDir(), "usage.db")
}

// setupUsageTracker 设置使用追踪
func (a *App) setupUsageTracker() {
	if !a.config.UsageTracking.Enabled {
		a.logger.Info("📊 使用追踪已禁用")
		return
	}

	// 统一数据库路径（避免 Database.Path 为空时回退到工作目录，导致历史数据“看不到/分裂”）
	dbPath := a.getEffectiveUsageDBPath()
	a.config.UsageTracking.DatabasePath = dbPath

	// 自动迁移：如果新库不存在但旧库存在，则复制旧库到新路径。
	a.migrateLegacyDatabaseIfNeeded()

	a.logger.Info("📊 初始化使用追踪器", "db_path", dbPath)

	// 重构：定价配置完全从 SQLite 加载，不再依赖 config.yaml
	// 初始化时使用空定价，后续由 syncPricingToTracker() 从数据库加载
	trackingConfig := &tracking.Config{
		Enabled:         a.config.UsageTracking.Enabled,
		DatabasePath:    dbPath,
		Database:        a.config.UsageTracking.Database,
		BufferSize:      a.config.UsageTracking.BufferSize,
		BatchSize:       a.config.UsageTracking.BatchSize,
		FlushInterval:   a.config.UsageTracking.FlushInterval,
		MaxRetry:        a.config.UsageTracking.MaxRetry,
		RetentionDays:   a.config.UsageTracking.RetentionDays,
		CleanupInterval: a.config.UsageTracking.CleanupInterval,
		ModelPricing:    nil,                     // 定价从 SQLite model_pricing 表加载
		DefaultPricing:  tracking.ModelPricing{}, // 默认定价从 SQLite 加载
	}

	var err error
	a.usageTracker, err = tracking.NewUsageTracker(trackingConfig, a.config.Timezone)
	if err != nil {
		a.logger.Error("使用追踪器初始化失败", "error", err)
		return
	}

	a.logger.Info("📊 使用追踪已启用", "database", dbPath)
}

// setupStoreDB 初始化一个专用的 SQLite 连接用于管理写操作（channels/endpoints/settings/codex_providers）。
// 目的：避免 UsageTracker 的后台写入/归档在同一连接上阻塞 UI 管理操作，造成“前端超时但最终成功”的错觉。
func (a *App) setupStoreDB() {
	if a.config == nil {
		return
	}
	if a.storeDB != nil {
		return
	}
	dbPath := a.getEffectiveUsageDBPath()
	// 运行时回填，确保后续组件拿到统一路径（即使 usage_tracking.enabled=false）。
	a.config.UsageTracking.DatabasePath = dbPath

	// 即使未启用 usage tracking，也执行旧库迁移，避免共享 usage.db 导致锁冲突。
	a.migrateLegacyDatabaseIfNeeded()

	// 确保数据库目录存在
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			a.logger.Warn("⚠️ 创建数据库目录失败", "error", err)
			return
		}
	}

	// 使用较长 busy_timeout：管理侧读写应尽量避免 SQLITE_BUSY；上层仍用 ctx 控制总体等待时间。
	// 注意：写冲突时 SQLite 仍只有一个 writer，该超时只是让管理侧在短暂争用时更耐心等待。
	dsn := dbPath + "?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_foreign_keys=1&_busy_timeout=60000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		a.logger.Warn("⚠️ 初始化管理数据库连接失败", "error", err)
		return
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// 这里不做过短超时：如果用户正运行旧版本/另一实例占用数据库，短超时会导致 storeDB 为 nil，
	// 进而前端出现“设置服务未启用/数据为空”的连锁问题。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dbReady := true
	if err := db.PingContext(ctx); err != nil {
		if isLikelySQLiteBusyOrLocked(err) || strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded") {
			dbReady = false
			a.logger.Warn("⚠️ 管理数据库连接暂不可用（可能被占用/初始化耗时较长），将继续启动并允许稍后重试", "error", err)
			a.emitNotification("warning", "数据库暂不可用", "检测到数据库可能被占用或初始化耗时较长；管理功能（设置/渠道/定价）可能暂不可用。请确保只运行一个 AI Relay Forwarder 实例，稍等后点击刷新，必要时重启。")
		} else {
			_ = db.Close()
			a.logger.Warn("⚠️ 管理数据库连接不可用", "error", err)
			return
		}
	}

	// 当 usageTracker 未启用/初始化失败时，仍需要确保管理表（settings/channels/endpoints/model_pricing/codex_providers 等）存在。
	if a.usageTracker == nil && dbReady {
		adapter, err := tracking.NewDatabaseAdapter(tracking.DatabaseConfig{
			Type:         "sqlite",
			DatabasePath: dbPath,
			Timezone:     a.config.Timezone,
		})
		if err == nil {
			if err := adapter.Open(); err == nil {
				if err := adapter.InitSchema(); err != nil {
					a.logger.Warn("⚠️ 初始化数据库 Schema 失败（管理存储）", "error", err)
				}
				_ = adapter.Close()
			} else {
				a.logger.Warn("⚠️ 打开数据库失败（管理存储）", "error", err)
			}
		} else {
			a.logger.Warn("⚠️ 创建数据库适配器失败（管理存储）", "error", err)
		}
	}

	a.storeDB = db
	a.logger.Info("✅ 管理数据库连接已就绪", "db", dbPath)
}

func isLikelySQLiteBusyOrLocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "locked")
}

// migrateLegacyDatabaseIfNeeded 将旧版本默认库 usage.db 迁移到新版本默认库 usage.db。
// 目的：避免新旧版本同时运行时共享同一 SQLite 文件导致锁冲突（创建渠道/写入配置“无响应”）。
func (a *App) migrateLegacyDatabaseIfNeeded() {
	newPath := a.getEffectiveUsageDBPath()
	if newPath == "" {
		return
	}

	// 仅对默认路径场景启用迁移，避免覆盖用户显式配置
	defaultNew := filepath.Join(utils.GetDataDir(), "usage.db")
	defaultOld := filepath.Join(utils.GetDataDir(), "usage.db")
	if filepath.Clean(newPath) != filepath.Clean(defaultNew) {
		return
	}

	legacyCandidates := []string{}

	// 兼容旧版本/调试环境可能使用的相对路径（例如安装目录/data/*.db 或运行目录/data/*.db）
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		legacyCandidates = append(legacyCandidates,
			filepath.Join(exeDir, "data", "usage.db"),
			filepath.Join(exeDir, "data", "usage.db"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		legacyCandidates = append(legacyCandidates,
			filepath.Join(cwd, "data", "usage.db"),
			filepath.Join(cwd, "data", "usage.db"),
		)
	}
	// 兼容更老版本：用户目录下的 usage.db
	legacyCandidates = append(legacyCandidates, defaultOld)

	existingLegacy := make([]string, 0, len(legacyCandidates))
	seen := make(map[string]struct{}, len(legacyCandidates))
	for _, p := range legacyCandidates {
		if p == "" {
			continue
		}
		cp := filepath.Clean(p)
		if cp == filepath.Clean(defaultNew) {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		if _, err := os.Stat(cp); err != nil {
			continue
		}
		seen[cp] = struct{}{}
		existingLegacy = append(existingLegacy, cp)
	}
	if len(existingLegacy) == 0 {
		return
	}

	// 场景1：新库不存在 -> 直接复制（最简单、最稳定）
	if _, err := os.Stat(defaultNew); err != nil {
		// 选择一个“最佳”的来源文件：优先 request_logs 数量更多的库（更可能包含用户的历史数据）。
		bestLegacy := existingLegacy[0]
		bestReq := int64(-1)
		bestCh := int64(-1)
		for _, p := range existingLegacy {
			reqCount, _ := countTableRows(p, "request_logs")
			chCount, _ := countTableRows(p, "channels")
			if reqCount > bestReq || (reqCount == bestReq && chCount > bestCh) {
				bestLegacy = p
				bestReq = reqCount
				bestCh = chCount
			}
		}

		if err := copyFile(bestLegacy, defaultNew); err != nil {
			a.logger.Warn("⚠️ 旧数据迁移失败，将使用新的空数据库（可在关闭旧版本后重启再迁移）",
				"old", bestLegacy, "new", defaultNew, "error", err)
			a.emitNotification("warning", "旧数据迁移失败", "无法复制旧数据库（可能被旧版本占用），将使用新的空数据库；关闭旧版本后重启可再次尝试迁移。")
			return
		}

		// 拷贝后确保 schema 完整（补表/补列/迁移），并从其他来源合并缺失数据。
		_ = ensureSchemaForDB(defaultNew, a.config.Timezone)
		for _, p := range existingLegacy {
			if filepath.Clean(p) == filepath.Clean(bestLegacy) {
				continue
			}
			_ = importLegacyTables(defaultNew, p)
		}

		a.logger.Info("✅ 旧数据迁移完成", "old", bestLegacy, "new", defaultNew)
		a.emitNotification("success", "旧数据已迁移", "已将旧版本数据库迁移到新数据库文件（避免新旧版本冲突）。")
		return
	}

	// 场景2：新库已存在，但用户反馈“历史数据丢失”（多见于新库先被创建为空库，导致复制迁移跳过）。
	// 这里按“缺什么补什么”的思路合并导入：INSERT OR IGNORE，不覆盖现有配置。
	_ = ensureSchemaForDB(defaultNew, a.config.Timezone)

	newReqCount, _ := countTableRows(defaultNew, "request_logs")
	newChCount, _ := countTableRows(defaultNew, "channels")
	newEpCount, _ := countTableRows(defaultNew, "endpoints")

	needImport := newReqCount == 0 || newChCount == 0 || newEpCount == 0
	if !needImport {
		return
	}

	importedAny := false
	for _, p := range existingLegacy {
		// 仅在“新库缺数据”时尝试导入，避免每次启动重复扫描大库。
		oldReqCount, _ := countTableRows(p, "request_logs")
		oldChCount, _ := countTableRows(p, "channels")
		oldEpCount, _ := countTableRows(p, "endpoints")
		if (newReqCount == 0 && oldReqCount > 0) || (newChCount == 0 && oldChCount > 0) || (newEpCount == 0 && oldEpCount > 0) {
			if err := importLegacyTables(defaultNew, p); err != nil {
				a.logger.Warn("⚠️ 旧数据导入失败", "old", p, "new", defaultNew, "error", err)
				continue
			}
			importedAny = true
		}
	}
	if importedAny {
		a.logger.Info("✅ 已导入旧数据", "new", defaultNew)
		a.emitNotification("success", "旧数据已导入", "已从旧数据库合并导入历史数据到当前数据库（不会覆盖现有配置）。")
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func countTableRows(dbPath, table string) (int64, error) {
	if dbPath == "" || table == "" {
		return 0, nil
	}
	dsn := dbPath + "?_busy_timeout=2000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// table 名仅允许字母数字下划线，避免拼接 SQL 注入
	for _, r := range table {
		if !(r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return 0, fmt.Errorf("invalid table name: %s", table)
		}
	}

	var count int64
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	if err := row.Scan(&count); err != nil {
		// 例如表不存在：当作 0
		return 0, nil
	}
	return count, nil
}

func ensureSchemaForDB(dbPath, timezone string) error {
	if dbPath == "" {
		return nil
	}
	adapter, err := tracking.NewDatabaseAdapter(tracking.DatabaseConfig{
		Type:         "sqlite",
		DatabasePath: dbPath,
		Timezone:     timezone,
	})
	if err != nil {
		return err
	}
	if err := adapter.Open(); err != nil {
		return err
	}
	defer adapter.Close()
	return adapter.InitSchema()
}

func importLegacyTables(dstDBPath, legacyDBPath string) error {
	if dstDBPath == "" || legacyDBPath == "" {
		return nil
	}

	dsn := dstDBPath + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=1&_busy_timeout=10000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	legacyEscaped := strings.ReplaceAll(legacyDBPath, "'", "''")
	if _, err := db.ExecContext(ctx, "ATTACH DATABASE '"+legacyEscaped+"' AS legacy"); err != nil {
		return fmt.Errorf("attach legacy database failed: %w", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DETACH DATABASE legacy")
	}()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 导入历史数据（INSERT OR IGNORE），不覆盖新库中已存在的记录
	for _, table := range []string{"request_logs", "usage_summary", "endpoints", "channels", "model_pricing", "settings", "codex_providers"} {
		cols, err := commonTableColumns(ctx, tx, table)
		if err != nil {
			continue
		}
		cols = filterOut(cols, "id")
		if len(cols) == 0 {
			continue
		}

		colList := make([]string, 0, len(cols))
		for _, c := range cols {
			colList = append(colList, `"`+c+`"`)
		}
		colCSV := strings.Join(colList, ", ")

		sqlStmt := fmt.Sprintf(
			"INSERT OR IGNORE INTO %s (%s) SELECT %s FROM legacy.%s",
			table, colCSV, colCSV, table,
		)
		if _, err := tx.ExecContext(ctx, sqlStmt); err != nil {
			continue
		}
	}

	return tx.Commit()
}

func commonTableColumns(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table string) ([]string, error) {
	mainCols, err := tableColumns(ctx, q, "main", table)
	if err != nil {
		return nil, err
	}
	legacyCols, err := tableColumns(ctx, q, "legacy", table)
	if err != nil {
		return nil, err
	}

	legacySet := make(map[string]struct{}, len(legacyCols))
	for _, c := range legacyCols {
		legacySet[c] = struct{}{}
	}
	common := make([]string, 0, len(mainCols))
	for _, c := range mainCols {
		if _, ok := legacySet[c]; ok {
			common = append(common, c)
		}
	}
	return common, nil
}

func tableColumns(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, schema, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA "+schema+".table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name != "" {
			cols = append(cols, name)
		}
	}
	return cols, nil
}

func filterOut(cols []string, disallow string) []string {
	if disallow == "" {
		return cols
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c == disallow {
			continue
		}
		out = append(out, c)
	}
	return out
}

// setupProxyHandler 设置代理处理器
func (a *App) setupProxyHandler() {
	// 创建代理处理器
	a.proxyHandler = proxy.NewHandler(a.endpointManager, a.config)
	a.proxyHandler.SetEventBus(a.eventBus)

	// 创建中间件（Claude 代理日志附加 provider=claude，用于文件分流）
	a.loggingMiddleware = middleware.NewLoggingMiddleware(a.logger.With("provider", "claude"))
	a.monitoringMiddleware = middleware.NewMonitoringMiddleware(a.endpointManager)
	a.authMiddleware = middleware.NewAuthMiddleware(a.config.Auth)

	// 连接组件
	a.monitoringMiddleware.SetEventBus(a.eventBus)
	a.loggingMiddleware.SetUsageTracker(a.usageTracker)
	a.loggingMiddleware.SetMonitoringMiddleware(a.monitoringMiddleware)
	a.proxyHandler.SetMonitoringMiddleware(a.monitoringMiddleware)

	if a.usageTracker != nil {
		a.proxyHandler.SetUsageTracker(a.usageTracker)
		if retryHandler := a.proxyHandler.GetRetryHandler(); retryHandler != nil {
			retryHandler.SetUsageTracker(a.usageTracker)
		}
	}
}

// startProxyServer 启动 HTTP 代理服务器
// 返回 true 表示启动成功，false 表示启动失败
func (a *App) startProxyServer() bool {
	mux := http.NewServeMux()

	// 注册监控端点
	a.monitoringMiddleware.RegisterHealthEndpoint(mux)

	// 注册使用追踪健康检查端点
	if a.usageTracker != nil {
		mux.HandleFunc("/health/usage-tracker", func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			if err := a.usageTracker.HealthCheck(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(fmt.Sprintf("Usage Tracker unhealthy: %v", err)))
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Usage Tracker healthy"))
		})
	}

	// 注册 Codex 代理路由（路径精确匹配，优先于 Claude catch-all）
	a.registerCodexRoutes(mux)

	// 注册代理处理器（Claude catch-all）
	mux.Handle("/", a.loggingMiddleware.Wrap(a.authMiddleware.Wrap(a.proxyHandler)))

	// 端口绑定逻辑
	var actualPort int
	var listener net.Listener
	var err error

	preferredPort := a.config.Server.Port
	if a.portManager != nil {
		preferredPort = a.portManager.GetPreferredPort()
	}

	// 首先尝试首选端口
	addr := fmt.Sprintf("%s:%d", a.config.Server.Host, preferredPort)
	listener, err = net.Listen("tcp", addr)
	if err != nil {
		// 端口冲突：记录状态，允许应用继续启动
		a.portConflict = true
		a.portConflictPort = preferredPort
		a.portConflictErr = err.Error()
		a.logger.Warn("⚠️ 首选端口被占用，等待用户决定", "port", preferredPort, "error", err)
		// 不立即返回 false，让应用继续启动，前端会显示冲突对话框
		return false
	}
	actualPort = preferredPort

	if a.portManager != nil {
		a.portManager.SetActualPort(actualPort)
	}

	// 更新配置中的实际端口
	a.config.Server.Port = actualPort

	a.proxyServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0, // 流式请求禁用写入超时
		IdleTimeout:  120 * time.Second,
	}

	// 在 goroutine 中启动服务器
	go func() {
		a.logger.Info("🌐 HTTP 代理服务器启动中...",
			"address", listener.Addr().String())

		if err := a.proxyServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			a.logger.Error("代理服务器启动失败", "error", err)
			// 通知前端
			a.emitError("代理服务器启动失败", err.Error())
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	baseURL := fmt.Sprintf("http://%s:%d", a.config.Server.Host, actualPort)
	a.logger.Info("✅ 代理服务器启动成功",
		"url", baseURL)

	// 端口冲突提示
	if a.portManager != nil {
		portInfo := a.portManager.GetPortInfo()
		if portInfo.WasOccupied {
			a.logger.Warn(fmt.Sprintf("⚠️ 首选端口 %d 被占用，已自动切换到端口 %d",
				portInfo.PreferredPort, portInfo.ActualPort))
		}
	}

	// 安全警告
	if a.config.Server.Host != "127.0.0.1" && a.config.Server.Host != "localhost" && a.config.Server.Host != "::1" {
		if !a.config.Auth.Enabled {
			a.logger.Warn("⚠️  安全警告：服务器绑定到非本地地址但未启用鉴权！")
		}
	}

	return true
}

// startProxyServerWithListener 使用已绑定的监听器启动代理服务器
// 用于端口冲突解决后，使用 ResolvePortConflict API 返回的监听器启动服务器
func (a *App) startProxyServerWithListener(listener net.Listener, actualPort int) {
	mux := http.NewServeMux()

	// 注册监控端点
	a.monitoringMiddleware.RegisterHealthEndpoint(mux)

	// 注册使用追踪健康检查端点
	if a.usageTracker != nil {
		mux.HandleFunc("/health/usage-tracker", func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			if err := a.usageTracker.HealthCheck(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(fmt.Sprintf("Usage Tracker unhealthy: %v", err)))
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Usage Tracker healthy"))
		})
	}

	// 注册 Codex 代理路由（路径精确匹配，优先于 Claude catch-all）
	a.registerCodexRoutes(mux)

	// 注册代理处理器（Claude catch-all）
	mux.Handle("/", a.loggingMiddleware.Wrap(a.authMiddleware.Wrap(a.proxyHandler)))

	a.proxyServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0, // 流式请求禁用写入超时
		IdleTimeout:  120 * time.Second,
	}

	// 标记服务器运行状态
	a.isRunning = true

	// 在 goroutine 中启动服务器
	go func() {
		a.logger.Info("🌐 HTTP 代理服务器启动中...",
			"address", listener.Addr().String())

		if err := a.proxyServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			a.logger.Error("代理服务器启动失败", "error", err)
			// 通知前端
			a.emitError("代理服务器启动失败", err.Error())
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	baseURL := fmt.Sprintf("http://%s:%d", a.config.Server.Host, actualPort)
	a.logger.Info("✅ 代理服务器启动成功", "url", baseURL)

	// 安全警告
	if a.config.Server.Host != "127.0.0.1" && a.config.Server.Host != "localhost" && a.config.Server.Host != "::1" {
		if !a.config.Auth.Enabled {
			a.logger.Warn("⚠️  安全警告：服务器绑定到非本地地址但未启用鉴权！")
		}
	}
}

// initCodexProxy 初始化 Codex 代理组件（EndpointManager + HealthChecker）。
// 不再启动独立 HTTP server，路由注册由 startProxyServer 中的 registerCodexRoutes 完成。
func (a *App) initCodexProxy() {
	if !a.config.CodexServer.Enabled {
		// 即使 codex_enabled 未显式开启，如果存在活跃 Codex 渠道也自动启用代理路由
		// 这避免了用户配置了 Codex 渠道但忘记开启 codex_enabled 导致路由不生效的问题
		if a.codexChannelStore != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			active, err := a.codexChannelStore.GetActive(ctx)
			cancel()
			if err == nil && active != nil {
				a.config.CodexServer.Enabled = true
				a.logger.Info("检测到活跃 Codex 渠道，自动启用 Codex 代理路由", "channel", active.Name)
			} else {
				a.logger.Debug("Codex 代理未启用，跳过")
				return
			}
		} else {
			a.logger.Debug("Codex 代理未启用，跳过")
			return
		}
	}

	// 检查依赖
	if a.codexChannelStore == nil || a.codexEndpointStore == nil {
		a.logger.Warn("Codex 代理初始化跳过：渠道/端点存储未就绪")
		return
	}

	// 创建 EndpointManager
	mgr := codexproxy.NewEndpointManager(a.codexChannelStore, a.codexEndpointStore, a.logger)
	if err := mgr.Reload(); err != nil {
		a.logger.Error("Codex 端点管理器加载失败", "error", err)
		return
	}
	// 注册活跃端点变更回调：故障转移成功提升端点后，刷新前端 UI
	mgr.SetOnActiveChanged(func() {
		a.emitCodexProviderUpdate()
	})
	a.codexEndpointMgr = mgr

	// 创建 HealthChecker（参数动态从 a.config.Health.* 读取，设置页改动下一轮即生效）
	hc := codexproxy.NewHealthChecker(mgr, a.config, a.logger)
	// 按设置项决定是否启动定时探测；保留实例以供 GetLatencies 等接口使用
	// 与 Claude 端对齐：仅当 Enabled + LoopRetry + SkipHealthCheck 三者都为 true 时才跳过
	if !a.config.Failover.ShouldSkipHealthCheck() {
		hc.Start()
	} else {
		a.logger.Info("Codex 健康检查器已创建但未启动（ShouldSkipHealthCheck=true）")
	}
	a.codexHealthCheck = hc

	// 创建 SuspensionManager（请求挂起队列）
	// 当 loop_retry 全部耗尽后将请求挂起，等待运维通过 UI 重新激活渠道/端点
	sm := codexproxy.NewSuspensionManager(
		a.config.Failover.MaxSuspendedRequests,
		a.config.Failover.LoopRetrySuspendTimeout,
		a.logger,
	)
	// 注册挂起数变化回调，推送前端事件
	sm.SetOnCountChange(func(count int) {
		a.emitCodexSuspensionUpdate(count)
	})
	a.codexSuspensionMgr = sm

	a.logger.Info("✅ Codex 代理组件初始化完成（路由将注册到主代理端口）")
}

// registerCodexRoutes 将 Codex handler 注册到主 mux 的特定路径上。
// 路由规则：/v1/chat/completions, /v1/completions, /v1/models, /v1/embeddings → Codex handler
// 其他路径 → Claude handler（catch-all）
func (a *App) registerCodexRoutes(mux *http.ServeMux) {
	if !a.config.CodexServer.Enabled || a.codexEndpointMgr == nil {
		return
	}

	cfg := codexproxy.ServerConfig{
		Timeout:       a.config.CodexServer.Timeout,
		Retry:         a.config.Retry,         // 同端点重试配置（设置页 retry.*）
		Failover:      a.config.Failover,      // 故障转移配置（设置页 failover.*）
		SuspensionMgr: a.codexSuspensionMgr,   // 请求挂起队列
	}
	baseHandler := codexproxy.NewHTTPHandler(a.codexEndpointMgr, cfg, a.logger, a.usageTracker)

	// 公用 Claude 代理的鉴权中间件 + 日志中间件
	var handler http.Handler = baseHandler
	if a.authMiddleware != nil {
		handler = a.authMiddleware.Wrap(baseHandler)
	}
	if a.loggingMiddleware != nil {
		handler = a.loggingMiddleware.Wrap(handler)
	}

	// 注册 Codex 路径
	// 同时注册精确路径和子树路径（带尾斜杠），以匹配子路径请求（如 /v1/responses/{id}）。
	// 同时注册不带 /v1 前缀的路径，因为 Codex CLI 的 base_url 可能不含 /v1。
	codexBasePaths := []string{
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/models",
		"/v1/embeddings",
		"/v1/responses",
		"/responses",
		"/chat/completions",
		"/completions",
		"/models",
		"/embeddings",
	}
	var registeredPaths []string
	for _, path := range codexBasePaths {
		mux.Handle(path, handler)
		registeredPaths = append(registeredPaths, path)
		// 注册子树路径（带尾斜杠）以匹配子路径请求
		subtree := path + "/"
		mux.Handle(subtree, handler)
		registeredPaths = append(registeredPaths, subtree)
	}

	a.logger.Info("✅ Codex 代理路由已注册到主端口",
		"paths", registeredPaths,
	)
}

// stopCodexProxy 停止 Codex 代理组件（HealthChecker）
func (a *App) stopCodexProxy() {
	if a.codexHealthCheck != nil {
		a.codexHealthCheck.Stop()
		a.codexHealthCheck = nil
	}
	a.codexEndpointMgr = nil
}

// setupConfigReload 设置配置热重载
func (a *App) setupConfigReload() {
	a.configWatcher.AddReloadCallback(func(newCfg *config.Config) {
		a.mu.Lock()
		defer a.mu.Unlock()

		// 更新配置引用
		a.config = newCfg

		// 停止旧的日志 Emitter，避免多个 Emitter 同时广播导致日志重复
		if a.logEmitter != nil {
			a.logEmitter.Stop()
		}

		// 更新日志
		newLogger, newBroadcastHandler := setupLogger(newCfg.Logging)
		slog.SetDefault(newLogger)
		a.logger = newLogger
		a.logHandler = newBroadcastHandler
		a.logEmitter = newBroadcastHandler.Emitter

		// 更新各组件
		a.configWatcher.UpdateLogger(newLogger)
		a.endpointManager.UpdateConfig(newCfg)
		a.proxyHandler.UpdateConfig(newCfg)
		a.authMiddleware.UpdateConfig(newCfg.Auth)

		// 注意：模型定价不再从 config.yaml 热重载
		// 定价配置通过前端「定价」页面管理，存储在 SQLite model_pricing 表中

		a.logger.Info("🔄 配置已重新加载")

		// 通知前端配置已更新
		a.emitConfigReloaded()
	})

	a.logger.Info("🔄 配置热重载已启用")
}

// setupEventBridges 设置事件桥接
// 将内部 EventBus 事件转发到 Wails 前端
//
// 🔧 [P0.3 修复] 通过 runResilientGoroutine 启动，panic 后自动重启
// 旧实现：panic recover 后 goroutine 直接退出，事件桥接永久失效，前端状态不再更新
// 新实现：panic 后自动重启（带 backoff + 重启次数上限），保持服务可恢复
func (a *App) setupEventBridges() {
	// 注意：当前 EventBus 实现不支持订阅回调
	// 我们使用定时轮询来更新前端状态
	a.runResilientGoroutine("event-bridge", func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-a.stopChan:
				return
			case <-ticker.C:
				// 使用读锁检查运行状态，避免竞态条件
				a.mu.RLock()
				running := a.isRunning
				window := a.window
				a.mu.RUnlock()

				if running && window != nil {
					a.emitSystemStatus()
					a.emitUsageUpdate()
				}
			}
		}
	})

	a.logger.Info("📡 事件桥接已启用")
}

// startHistoryCollector 启动历史数据收集器
// 定期收集 Metrics 历史数据点，用于图表显示
func (a *App) startHistoryCollector() {
	if a.monitoringMiddleware == nil {
		a.logger.Warn("⚠️  监控中间件未初始化，跳过历史数据收集器启动")
		return
	}

	// 立即收集一次初始数据点
	// 注意：必须直接在原始 *Metrics 上调用 AddHistoryDataPoints()
	// 不能调用 GetMetrics() 获取副本，因为那样修改的是副本而不是原始数据
	metrics := a.monitoringMiddleware.GetMetrics()
	if metrics != nil {
		metrics.AddHistoryDataPoints()
		a.logger.Info("📊 初始历史数据点已收集")
	}

	// 🔧 [P0.3 修复] 改用 runResilientGoroutine，panic 后自动重启
	a.runResilientGoroutine("history-collector", func() {
		ticker := time.NewTicker(30 * time.Second) // 每30秒收集一次
		defer ticker.Stop()

		a.logger.Info("📊 历史数据收集器已启动 (30秒间隔)")

		for {
			select {
			case <-a.stopChan:
				a.logger.Info("📊 历史数据收集器已停止")
				return
			case <-ticker.C:
				// 收集历史数据点
				// 直接在原始 *Metrics 上调用，而不是获取副本
				metrics := a.monitoringMiddleware.GetMetrics()
				if metrics != nil {
					metrics.AddHistoryDataPoints()
				}
			}
		}
	})
}

// runResilientGoroutine 启动一个具备自重启能力的后台 goroutine
//
// 🔧 [P0.3 新增] 解决后台关键 goroutine（事件桥接、历史收集器等）
// panic 后永久退出导致前端状态停滞的问题。
//
// 行为：
//  1. panic 发生时记录详细日志（含 stack trace）
//  2. 按指数退避策略重启（1s → 2s → 4s → 8s → 16s 上限）
//  3. 短时间内连续 panic 超过 5 次时停止重启，避免无限崩溃
//  4. a.stopChan 关闭时正常退出，不重启
//
// 参数：
//   - name: goroutine 名称，用于日志
//   - work: 实际工作函数，正常 return 表示退出（不重启）
func (a *App) runResilientGoroutine(name string, work func()) {
	go func() {
		const maxRestarts = 5
		const restartWindow = 60 * time.Second // 60s 内累计超过 maxRestarts 则放弃

		var restartCount int
		var restartWindowStart time.Time

		for {
			// 检查 stopChan，正常退出不重启
			select {
			case <-a.stopChan:
				if a.logger != nil {
					a.logger.Info(fmt.Sprintf("🛑 %s goroutine 收到停止信号，退出", name))
				}
				return
			default:
			}

			func() {
				defer func() {
					if r := recover(); r != nil {
						if a.logger != nil {
							a.logger.Error(fmt.Sprintf("💥 %s goroutine panic", name),
								"recover", r,
								"stack", string(debug.Stack()))
						}
					}
				}()

				work()
			}()

			// work() 正常 return 不重启
			// 只有 panic 才会到这里继续循环

			// 滑动窗口计数
			now := time.Now()
			if now.Sub(restartWindowStart) > restartWindow {
				restartWindowStart = now
				restartCount = 0
			}
			restartCount++

			if restartCount > maxRestarts {
				if a.logger != nil {
					a.logger.Error(fmt.Sprintf("❌ %s goroutine 在 %v 内累计 panic %d 次，停止重启",
						name, restartWindow, restartCount))
				}
				return
			}

			// 指数退避（最长 16s）
			backoff := time.Duration(1<<uint(restartCount-1)) * time.Second
			if backoff > 16*time.Second {
				backoff = 16 * time.Second
			}

			if a.logger != nil {
				a.logger.Warn(fmt.Sprintf("🔄 %s goroutine 将在 %v 后重启 (第 %d 次)",
					name, backoff, restartCount))
			}

			select {
			case <-a.stopChan:
				return
			case <-time.After(backoff):
			}
		}
	}()
}

// emitError 发送错误通知到前端
func (a *App) emitError(title, message string) {
	a.safeEmitEvent("error", map[string]string{
		"title":   title,
		"message": message,
	})
}

// emitConfigReloaded 通知前端配置已重载
func (a *App) emitConfigReloaded() {
	a.safeEmitEvent("config:reloaded", nil)
}

// setupSettingsStore 设置系统设置存储 (SQLite)
func (a *App) setupSettingsStore() {
	// 获取数据库连接：优先使用 storeDB
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	if db == nil {
		a.logger.Error("❌ 无法获取数据库连接 (设置存储)")
		return
	}

	// 创建 SettingsStore
	a.settingsStore = store.NewSQLiteSettingsStore(db)

	// 创建 SettingsService
	a.settingsService = service.NewSettingsService(a.settingsStore)

	// 设置配置变更回调 - 热更新
	a.settingsService.SetOnChangeCallback(func() {
		a.applySettingsToConfig()
	})

	// 初始化默认设置（如果表为空）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.settingsService.InitDefaults(ctx); err != nil {
		a.logger.Error("❌ 初始化默认设置失败", "error", err)
		return
	}

	// 从数据库加载设置并应用到配置
	a.applySettingsToConfig()

	// 初始化端口管理器
	preferredPort := a.settingsService.GetInt(ctx, service.CategoryServer, "preferred_port", a.config.Server.Port)

	// Dev 模式：端口偏移 +10，允许与 release 版本同时运行
	if BuildMode == "dev" {
		preferredPort += 10
		a.logger.Info("🔧 Dev 模式端口偏移", "original", preferredPort-10, "dev_port", preferredPort)
	}

	a.portManager = utils.NewPortManager(preferredPort)

	a.logger.Info("✅ 系统设置存储已启用 (SQLite)")
}

// applySettingsToConfig 从数据库加载设置并应用到运行时配置
func (a *App) applySettingsToConfig() {
	if a.settingsService == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 策略配置
	a.config.Strategy.Type = a.getSettingString(ctx, service.CategoryStrategy, "type", a.config.Strategy.Type)
	a.config.Strategy.FastTestEnabled = a.settingsService.GetBool(ctx, service.CategoryStrategy, "fast_test_enabled", a.config.Strategy.FastTestEnabled)
	a.config.Strategy.FastTestCacheTTL = a.settingsService.GetDuration(ctx, service.CategoryStrategy, "fast_test_cache_ttl", a.config.Strategy.FastTestCacheTTL)
	a.config.Strategy.FastTestTimeout = a.settingsService.GetDuration(ctx, service.CategoryStrategy, "fast_test_timeout", a.config.Strategy.FastTestTimeout)
	a.config.Strategy.FastTestPath = a.getSettingString(ctx, service.CategoryStrategy, "fast_test_path", a.config.Strategy.FastTestPath)

	// 重试配置
	a.config.Retry.MaxAttempts = a.settingsService.GetInt(ctx, service.CategoryRetry, "max_attempts", a.config.Retry.MaxAttempts)
	a.config.Retry.BaseDelay = a.settingsService.GetDuration(ctx, service.CategoryRetry, "base_delay", a.config.Retry.BaseDelay)
	a.config.Retry.MaxDelay = a.settingsService.GetDuration(ctx, service.CategoryRetry, "max_delay", a.config.Retry.MaxDelay)
	a.config.Retry.Multiplier = a.settingsService.GetFloat(ctx, service.CategoryRetry, "multiplier", a.config.Retry.Multiplier)

	// 健康检查配置
	a.config.Health.CheckInterval = a.settingsService.GetDuration(ctx, service.CategoryHealth, "check_interval", a.config.Health.CheckInterval)
	a.config.Health.Timeout = a.settingsService.GetDuration(ctx, service.CategoryHealth, "timeout", a.config.Health.Timeout)
	a.config.Health.HealthPath = a.getSettingString(ctx, service.CategoryHealth, "health_path", a.config.Health.HealthPath)

	// 故障转移配置
	// 记录旧值用于检测状态变化
	oldFailoverEnabled := a.config.Failover.Enabled
	a.config.Failover.Enabled = a.settingsService.GetBool(ctx, service.CategoryFailover, "enabled", a.config.Failover.Enabled)
	// 当渠道间转移从禁用变为启用时，重置所有渠道的暂停状态
	// 同时更新数据库，避免后续从数据库同步时恢复旧的暂停状态
	if !oldFailoverEnabled && a.config.Failover.Enabled && a.endpointManager != nil {
		gm := a.endpointManager.GetGroupManager()
		if gm != nil {
			gm.ResetAllPausedGroups()
		}
		// 同步更新数据库中的 failover_enabled 字段
		if a.channelService != nil {
			if resetCount, err := a.channelService.ResetAllFailoverEnabled(ctx); err != nil {
				a.logger.Warn("⚠️ 重置数据库渠道状态失败", "error", err)
			} else if resetCount > 0 {
				a.logger.Info(fmt.Sprintf("✅ 渠道间转移已启用，已重置 %d 个渠道的暂停状态（内存+数据库）", resetCount))
			} else {
				a.logger.Info("✅ 渠道间转移已启用，所有渠道暂停状态已重置")
			}
		} else {
			a.logger.Info("✅ 渠道间转移已启用，所有渠道暂停状态已重置（仅内存）")
		}
	}
	// 从数据库加载 loop_retry、max_loops、skip_health_check 到 Failover
	a.config.Failover.LoopRetry = a.settingsService.GetBool(ctx, service.CategoryFailover, "loop_retry", a.config.Failover.LoopRetry)
	a.config.Failover.MaxLoops = a.settingsService.GetInt(ctx, service.CategoryFailover, "max_loops", a.config.Failover.MaxLoops)
	a.config.Failover.SkipHealthCheck = a.settingsService.GetBool(ctx, service.CategoryFailover, "skip_health_check", a.config.Failover.SkipHealthCheck)
	// 达到最大循环轮数后的手动等待超时
	a.config.Failover.LoopRetrySuspendTimeout = a.settingsService.GetDuration(ctx, service.CategoryFailover, "loop_retry_wait_timeout", a.config.Failover.LoopRetrySuspendTimeout)

	// 请求控制配置
	a.config.GlobalTimeout = a.settingsService.GetDuration(ctx, service.CategoryRequest, "global_timeout", a.config.GlobalTimeout)
	// 普通挂起等待超时
	a.config.Failover.SuspendTimeout = a.settingsService.GetDuration(ctx, service.CategoryRequest, "suspend_wait_timeout", a.config.Failover.SuspendTimeout)
	a.config.Failover.SuspendOnExhausted = a.settingsService.GetBool(ctx, service.CategoryRequest, "suspend_enabled", a.config.Failover.SuspendOnExhausted)
	a.config.Failover.MaxSuspendedRequests = a.settingsService.GetInt(ctx, service.CategoryRequest, "max_suspended", a.config.Failover.MaxSuspendedRequests)
	a.config.Failover.EOFRetryHint = a.settingsService.GetBool(ctx, service.CategoryRequest, "eof_retry_hint", a.config.Failover.EOFRetryHint)
	// allow_retry_on_exhausted 配置已废弃，始终保持连接挂起
	// 注释保留用于向后兼容文档记录

	// 流式传输配置
	a.config.Streaming.HeartbeatInterval = a.settingsService.GetDuration(ctx, service.CategoryStreaming, "heartbeat_interval", a.config.Streaming.HeartbeatInterval)
	a.config.Streaming.ReadTimeout = a.settingsService.GetDuration(ctx, service.CategoryStreaming, "read_timeout", a.config.Streaming.ReadTimeout)
	a.config.Streaming.MaxIdleTime = a.settingsService.GetDuration(ctx, service.CategoryStreaming, "max_idle_time", a.config.Streaming.MaxIdleTime)
	a.config.Streaming.ResponseHeaderTimeout = a.settingsService.GetDuration(ctx, service.CategoryStreaming, "response_header_timeout", a.config.Streaming.ResponseHeaderTimeout)

	// 访问控制配置
	a.config.Auth.Enabled = a.settingsService.GetBool(ctx, service.CategoryAuth, "enabled", a.config.Auth.Enabled)
	a.config.Auth.Token = a.getSettingString(ctx, service.CategoryAuth, "token", a.config.Auth.Token)

	// Token 计数配置
	a.config.TokenCounting.Enabled = a.settingsService.GetBool(ctx, service.CategoryTokenCounting, "enabled", a.config.TokenCounting.Enabled)
	a.config.TokenCounting.EstimationRatio = a.settingsService.GetFloat(ctx, service.CategoryTokenCounting, "estimation_ratio", a.config.TokenCounting.EstimationRatio)

	// 数据保留配置
	a.config.UsageTracking.RetentionDays = a.settingsService.GetInt(ctx, service.CategoryRetention, "retention_days", a.config.UsageTracking.RetentionDays)
	a.config.UsageTracking.CleanupInterval = a.settingsService.GetDuration(ctx, service.CategoryRetention, "cleanup_interval", a.config.UsageTracking.CleanupInterval)

	// 🔧 [P5.5.4 新增] Logging 日志配置（从 SQLite 同步）
	a.config.Logging.Level = a.getSettingString(ctx, service.CategoryLogging, "level", a.config.Logging.Level)
	a.config.Logging.Format = a.getSettingString(ctx, service.CategoryLogging, "format", a.config.Logging.Format)

	// 🔧 [P5.5.4 新增] Timezone 时区配置（从 SQLite 同步）
	// 注意：时区变更标记为 RequiresRestart=true，运行时变更只影响后续新生成的时间戳
	a.config.Timezone = a.getSettingString(ctx, service.CategoryTimezone, "global", a.config.Timezone)

	// Codex 代理配置（从 SQLite 同步，统一在 server 分类下）
	// 注意：enabled 标记为 RequiresRestart=true，运行时变更需重启生效
	// v7.2+ Codex 合并到主代理端口，不再有独立 host/port
	a.config.CodexServer.Enabled = a.settingsService.GetBool(ctx, service.CategoryServer, "codex_enabled", a.config.CodexServer.Enabled)
	a.config.CodexServer.Timeout = a.config.GlobalTimeout

	a.logger.Debug("已从数据库加载设置")

	// 更新各组件配置
	if a.endpointManager != nil {
		a.endpointManager.UpdateConfig(a.config)
	}
	if a.proxyHandler != nil {
		a.proxyHandler.UpdateConfig(a.config)
	}
	if a.authMiddleware != nil {
		a.authMiddleware.UpdateConfig(a.config.Auth)
	}
}

// getSettingString 获取字符串设置值（带默认值）
func (a *App) getSettingString(ctx context.Context, category, key, defaultVal string) string {
	val, err := a.settingsService.GetValue(ctx, category, key)
	if err != nil || val == "" {
		return defaultVal
	}
	return val
}

