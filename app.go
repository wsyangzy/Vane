// app.go - Wails 应用核心结构
// 封装所有业务组件，提供生命周期管理

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	appcache "ai-relay-forwarder/internal/cache"
	"ai-relay-forwarder/internal/events"
	"ai-relay-forwarder/internal/logging"
	"ai-relay-forwarder/internal/middleware"
	"ai-relay-forwarder/internal/privacy"
	"ai-relay-forwarder/internal/providers/claude/proxy"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/proxy/subscription"
	claudeservice "ai-relay-forwarder/internal/providers/claude/service"
	claudestore "ai-relay-forwarder/internal/providers/claude/store"
	codexproxy "ai-relay-forwarder/internal/providers/codex/proxy"
	codexsubscription "ai-relay-forwarder/internal/providers/codex/proxy/subscription"
	codexservice "ai-relay-forwarder/internal/providers/codex/service"
	codexstore "ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/service"
	"ai-relay-forwarder/internal/store"
	"ai-relay-forwarder/internal/subscriptions/accounts"
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
	modelPricingStore   store.ModelPricingStore      // 模型定价数据持久化
	modelPricingService *service.ModelPricingService // 模型定价业务服务

	// 模型路由规则 (SQLite, Claude/Codex 共用一张表按 provider 隔离)
	modelRouteService *service.ModelRouteService

	// 系统设置存储 (SQLite)
	settingsStore   store.SettingsStore      // 设置数据持久化
	settingsService *service.SettingsService // 设置业务服务
	portManager     *utils.PortManager       // 端口管理器

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

// NewApp 创建新的应用实例
func NewApp() *App {
	a := &App{
		startTime: time.Now(),
		stopChan:  make(chan struct{}),
	}
	a.setupCacheRegistry()
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

	// 1.5. 在日志系统初始化前应用 SQLite 中的日志设置，避免先按代码默认值写入启动日志
	a.applyPersistedLoggingConfigBeforeLogger()

	// 2. 初始化日志
	a.setupLogger()

	// 3. 显示启动信息
	a.logger.Info("🚀 AI Relay Forwarder 桌面版启动中...",
		"version", Version,
		"config_source", "sqlite+defaults")

	// 4. 初始化事件总线
	a.setupEventBus()

	// 5. 初始化使用追踪（SQLite 存储需要依赖数据库）
	a.setupUsageTracker()

	// 5.2 初始化管理/配置 DB（channels/endpoints/settings/codex_providers 等）
	a.setupStoreDB()

	// 5.5 初始化设置服务 (SQLite)
	a.setupSettingsStore()

	// 5.6 初始化隐私防护服务 (SQLite)
	a.setupPrivacyService()

	// 6. 创建端点管理器（但不启动健康检查）
	a.endpointManager = endpoint.NewManager(a.config)
	a.endpointManager.SetEventBus(a.eventBus)
	// Wails 桌面应用：设置健康检查完成回调，推送事件到前端
	a.endpointManager.SetOnHealthCheckComplete(func() {
		a.emitEndpointUpdate()
	})

	// 设置故障转移回调：以“渠道(channel)”为单位同步数据库状态
	a.endpointManager.SetOnFailoverTriggered(func(failedChannel, newChannel string, activationRevision uint64) (bool, error) {
		if a.endpointService != nil {
			ctx := context.Background()
			if newChannel != "" {
				applied, err := a.endpointService.ActivateChannelForFailover(ctx, newChannel, failedChannel, activationRevision)
				if err != nil {
					slog.Warn(fmt.Sprintf("⚠️ [故障转移回调] 激活新渠道失败: %s - %v", newChannel, err))
					return false, err
				} else if !applied {
					slog.Info(fmt.Sprintf("⏭️ [故障转移回调] 活跃渠道已由其他操作更新，跳过过期同步: %s → %s", failedChannel, newChannel))
					return false, nil
				} else {
					slog.Info(fmt.Sprintf("✅ [故障转移回调] 渠道已切换并同步数据库: %s → %s", failedChannel, newChannel))
				}
			}
		}

		// 推送事件到前端
		a.emitEndpointUpdate()
		return true, nil
	})

	// 7. 初始化端点存储 (SQLite, 需要在创建 Manager 之后)
	// 从数据库同步端点到 Manager
	a.setupEndpointStore()

	// 7.5 初始化模型定价存储 (SQLite)
	a.setupModelPricingStore()

	// 7.55 初始化 Codex Provider 存储 (SQLite)
	a.setupCodexProviderStore()

	// 7.56 发布 Claude 请求级路由配置（模型路由规则 + 渠道非流改道）
	a.syncClaudeRouteConfig(context.Background())

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

	// 9.5 初始化 Codex 代理组件，须在 startProxyServer 前完成，以便注册路由
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

	// 11. 设置事件桥接
	a.setupEventBridges()

	// 12. 启动历史数据收集器
	a.startHistoryCollector()

	a.isRunning = proxyStarted
	a.logger.Info("✅ AI Relay Forwarder 启动完成",
		"proxy_port", a.config.Server.Port,
		"app_state", AppState(a.appState.Load()).String())
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
	a.stopDevWatcherAfterQuit()

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

const devWatcherPIDFile = ".wails-dev-watcher.pid"

func parseDevWatcherPID(data []byte) (int, error) {
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		return 0, fmt.Errorf("无效的 Wails dev watcher PID")
	}
	return pid, nil
}

func devWatcherStopScript(pid int) string {
	return fmt.Sprintf(`
Start-Sleep -Milliseconds 1000
$process = Get-CimInstance Win32_Process -Filter "ProcessId = %d" -ErrorAction SilentlyContinue
if ($null -eq $process) { exit 0 }
$commandLine = [string]$process.CommandLine
if ($process.Name -ne 'wails3.exe' -or $commandLine -notmatch '(^|\s)dev(\s|$)') { exit 0 }
& taskkill.exe /T /F /PID %d *> $null
`, pid, pid)
}

func (a *App) stopDevWatcherAfterQuit() {
	if BuildMode != "dev" || goruntime.GOOS != "windows" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	pidFile := filepath.Join(filepath.Dir(executable), devWatcherPIDFile)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := parseDevWatcherPID(data)
	if err != nil {
		return
	}
	_ = os.Remove(pidFile)

	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-Command", devWatcherStopScript(pid),
	)
	if err := cmd.Start(); err != nil && a.logger != nil {
		a.logger.Warn("无法通知 Wails dev watcher 退出", "error", err)
	}
}

func (a *App) openDirectory(path, label string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("创建%s失败: %w", label, err)
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("当前操作系统不支持打开目录: %s", goruntime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开%s失败: %w", label, err)
	}
	return nil
}

// OpenLogDir 打开日志目录（托盘菜单使用）
func (a *App) OpenLogDir() error {
	return a.openDirectory(utils.GetLogDir(), "日志目录")
}

// OpenDataDir 打开数据库与历史归档所在目录。
func (a *App) OpenDataDir() error {
	return a.openDirectory(utils.GetDataDir(), "数据目录")
}

// OpenClaudeConfigDir 打开 Claude 配置目录（托盘菜单使用）
// Claude 配置目录通常位于用户主目录下的 .claude 文件夹
func (a *App) OpenClaudeConfigDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return a.openDirectory(filepath.Join(homeDir, ".claude"), "Claude 配置目录")
}

// OpenCodexConfigDir 打开 Codex 配置目录
// Codex 配置目录通常位于用户主目录下的 .codex 文件夹（或 CODEX_HOME 环境变量指定）
func (a *App) OpenCodexConfigDir() error {
	codexHome, err := a.getCodexHomeDir()
	if err != nil {
		return fmt.Errorf("获取 Codex 配置目录失败: %w", err)
	}
	return a.openDirectory(codexHome, "Codex 配置目录")
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

	// 配置基线来自代码，端点、渠道和可调设置随后从 SQLite 加载。
	cfg := config.NewDefaultConfig()
	cfg.Logging.FilePath = filepath.Join(utils.GetLogDir(), "app.log")
	cfg.UsageTracking.DatabasePath = filepath.Join(utils.GetDataDir(), "usage.db")

	a.config = cfg

	tempLogger.Info("✅ 代码默认配置已加载",
		"log_path", a.config.Logging.FilePath,
		"db_path", a.config.UsageTracking.DatabasePath,
		"appdir", utils.GetAppDataDir())
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

func (a *App) applyPersistedLoggingConfigBeforeLogger() {
	if a.config == nil {
		return
	}
	dbPath := strings.TrimSpace(a.config.UsageTracking.DatabasePath)
	if a.config.UsageTracking.Database != nil && strings.TrimSpace(a.config.UsageTracking.Database.Path) != "" {
		dbPath = strings.TrimSpace(a.config.UsageTracking.Database.Path)
	}
	if dbPath == "" || dbPath == ":memory:" {
		return
	}
	if _, err := os.Stat(dbPath); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", sqliteReadOnlyStartupDSN(dbPath))
	if err != nil {
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	rows, err := db.QueryContext(ctx, `
		SELECT key, value
		FROM settings
		WHERE category = 'logging'
		  AND key IN ('level', 'format')
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return
		}
		value = strings.TrimSpace(value)
		switch key {
		case "level":
			if isValidLoggingLevel(value) {
				a.config.Logging.Level = strings.ToLower(value)
			}
		case "format":
			if isValidLoggingFormat(value) {
				a.config.Logging.Format = strings.ToLower(value)
			}
		}
	}
}

func sqliteReadOnlyStartupDSN(dbPath string) string {
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	return dbPath + separator + "mode=ro&_busy_timeout=2000"
}

func isValidLoggingLevel(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

func isValidLoggingFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text", "json":
		return true
	default:
		return false
	}
}

func (a *App) applyRuntimeLoggingConfig() {
	if a.config == nil {
		return
	}
	if a.logEmitter != nil {
		a.logEmitter.Stop()
	}

	logger, broadcastHandler := setupLogger(a.config.Logging)
	slog.SetDefault(logger)
	a.logger = logger
	a.logHandler = broadcastHandler
	a.logEmitter = broadcastHandler.Emitter

	utils.SetDebugConfig(a.config)
	if a.codexHTTPHandler != nil {
		a.codexHTTPHandler.UpdateLogger(logger)
	}
	a.logger.Info("✅ 日志设置已应用",
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

	a.setupSubscriptionAccounts(db)

	// 创建 EndpointStore
	a.endpointStore = claudestore.NewSQLiteEndpointStore(db)

	// 创建 ChannelStore / ChannelService
	a.channelStore = claudestore.NewSQLiteChannelStore(db)
	a.channelService = claudeservice.NewChannelService(a.channelStore)

	// 创建 Claude CredentialGroupStore / Service
	a.claudeCredentialGroupStore = claudestore.NewSQLiteClaudeCredentialGroupStore(db)
	a.claudeSubscriptions = subscription.NewController(claudestore.NewSQLiteClaudeSubscriptionStore(db))
	a.claudeSubscriptions.SetOnChange(a.endpointManager.NotifyEndpointStatusChange)
	a.claudeCredentialGroupService = claudeservice.NewClaudeCredentialGroupService(a.claudeCredentialGroupStore, a.channelStore, a.endpointStore)
	if a.subscriptionAccounts != nil {
		validate := func(ctx context.Context, id string) error { return a.subscriptionAccounts.Validate(ctx, "claude", id) }
		a.claudeCredentialGroupService.SetAccountValidator(validate)
		a.claudeSubscriptions.SetCredentialRejector(func(ctx context.Context, id, token string) {
			a.subscriptionAccounts.RejectAccess(ctx, "claude", id, token)
		})
		a.claudeSubscriptions.SetAccountResolver(func(ctx context.Context, id string) (string, string, error) {
			c, e := a.subscriptionAccounts.Resolve(ctx, "claude", id)
			return c.AccessToken, c.AccountID, e
		}, validate)
	}

	// 创建 EndpointService
	a.endpointService = claudeservice.NewEndpointService(a.endpointStore, a.endpointManager, a.config, a.claudeCredentialGroupStore)

	// 从数据库同步端点到内存
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 回填渠道表，保证“端点删光后渠道仍存在”
	if a.channelService != nil {
		if added, err := a.channelService.BackfillChannelsFromEndpoints(ctx, a.endpointStore); err != nil {
			a.logger.Warn("⚠️ 渠道回填失败", "error", err)
		} else if added > 0 {
			a.logger.Info("✅ 渠道回填完成", "count", added)
		}
	}
	if a.claudeCredentialGroupService != nil {
		if updated, err := a.claudeCredentialGroupService.EnsureDefaultGroupsFromEndpoints(ctx); err != nil {
			a.logger.Warn("⚠️ Claude 凭证分组回填失败", "error", err)
		} else if updated > 0 {
			a.logger.Info("✅ Claude 凭证分组回填完成", "endpoint_count", updated)
		}
	}

	if err := a.endpointService.SyncFromDatabase(ctx); err != nil {
		a.logger.Warn("⚠️ 从数据库同步端点失败，端点管理不可用", "error", err)
		if a.config != nil && a.config.Failover.Enabled {
			if rollbackErr := a.rollbackFailoverSetting(service.ProviderClaude, "Claude 渠道间转移已关闭：启动时从数据库同步端点失败", err); rollbackErr != nil {
				a.logger.Error("启动时回滚 Claude 渠道间转移失败", "error", rollbackErr)
			}
		}
	} else {
		// 同步渠道优先级到运行时，用于渠道间故障转移顺序
		if err := a.syncChannelPrioritiesToEndpointManager(ctx); err != nil {
			a.logger.Warn("⚠️ 启动时同步渠道优先级失败", "error", err)
		}
		// 同步渠道“参与故障转移”开关（暂停/恢复持久化）。开启全局
		// failover 前必须拿到完整映射，避免读取失败时以空映射放行。
		participationReady := true
		if err := a.syncChannelFailoverEnabledToEndpointManager(ctx); err != nil {
			participationReady = false
			a.logger.Warn("⚠️ 启动时同步渠道故障转移开关失败", "error", err)
			if a.config != nil && a.config.Failover.Enabled {
				if rollbackErr := a.rollbackFailoverSetting(service.ProviderClaude, "Claude 渠道间转移已关闭：启动时无法同步渠道参与状态", err); rollbackErr != nil {
					a.logger.Error("启动时回滚 Claude 渠道间转移失败", "error", rollbackErr)
				}
			}
		}
		// 同步渠道模型映射到运行时转发层
		a.syncChannelModelMappingsToEndpointManager(ctx)
		if participationReady && a.config != nil && a.config.Failover.Enabled {
			resumed, resumeErr := a.endpointService.EnsureActiveChannelParticipates(ctx, a.channelService)
			if resumeErr != nil {
				if rollbackErr := a.rollbackFailoverSetting(service.ProviderClaude, "Claude 渠道间转移未启用：启动时无法恢复当前接管渠道参与", resumeErr); rollbackErr != nil {
					a.logger.Error("启动时回滚 Claude 渠道间转移失败", "error", rollbackErr)
				}
			} else if resumed {
				a.logger.Info("✅ Claude 当前接管渠道已自动恢复参与渠道间转移")
			}
		}
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
	if a.codexChannelService == nil {
		if a.logger != nil {
			a.logger.Debug("Codex Provider 存储跳过初始化 (数据库未就绪)")
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.ensureCodexProviderSchema(ctx); err != nil {
		a.logger.Warn("⚠️ 初始化 Codex Provider 数据表结构失败", "error", err)
		if a.config != nil && a.config.CodexFailover.Enabled {
			if rollbackErr := a.rollbackFailoverSetting(service.ProviderCodex, "Codex 渠道间转移已关闭：启动时初始化数据表失败", err); rollbackErr != nil {
				a.logger.Error("启动时回滚 Codex 渠道间转移失败", "error", rollbackErr)
			}
		}
		return
	}

	channels, err := a.codexChannelService.ListChannels(ctx)
	if err != nil {
		a.logger.Warn("⚠️ 加载 Codex 渠道列表失败", "error", err)
		if a.config != nil && a.config.CodexFailover.Enabled {
			if rollbackErr := a.rollbackFailoverSetting(service.ProviderCodex, "Codex 渠道间转移已关闭：启动时加载渠道失败", err); rollbackErr != nil {
				a.logger.Error("启动时回滚 Codex 渠道间转移失败", "error", rollbackErr)
			}
		}
		return
	}

	a.logger.Info("✅ Codex Provider 存储已启用 (SQLite)", "count", len(channels))
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

	codexProviderStore := codexstore.NewSQLiteCodexProviderStore(db)
	codexProviderService := codexservice.NewCodexProviderService(codexProviderStore)
	codexChannelStore := codexstore.NewSQLiteCodexChannelStore(db)
	codexEndpointStore := codexstore.NewSQLiteCodexEndpointStore(db)
	codexCredentialGroupStore := codexstore.NewSQLiteCodexCredentialGroupStore(db)
	codexChannelService := codexservice.NewCodexChannelService(codexChannelStore, codexEndpointStore)
	codexEndpointService := codexservice.NewCodexEndpointService(codexEndpointStore, codexChannelStore, codexCredentialGroupStore)
	codexCredentialGroupService := codexservice.NewCodexCredentialGroupService(codexCredentialGroupStore, codexChannelStore, codexEndpointStore)
	codexSubscriptions := codexsubscription.NewController(codexstore.NewSQLiteCodexSubscriptionStore(db))
	if a.subscriptionAccounts != nil {
		validate := func(ctx context.Context, id string) error { return a.subscriptionAccounts.Validate(ctx, "codex", id) }
		codexCredentialGroupService.SetAccountValidator(validate)
		codexSubscriptions.SetCredentialRejector(func(ctx context.Context, id, token string) {
			a.subscriptionAccounts.RejectAccess(ctx, "codex", id, token)
		})
		codexSubscriptions.SetAccountResolver(func(ctx context.Context, id string) (string, string, error) {
			c, e := a.subscriptionAccounts.Resolve(ctx, "codex", id)
			return c.AccessToken, c.AccountID, e
		}, validate)
	}

	a.mu.Lock()
	if a.codexProviderService == nil {
		a.codexProviderStore = codexProviderStore
		a.codexProviderService = codexProviderService
	}
	if a.codexChannelService == nil {
		a.codexChannelStore = codexChannelStore
		a.codexEndpointStore = codexEndpointStore
		a.codexCredentialGroupStore = codexCredentialGroupStore
		a.codexChannelService = codexChannelService
		a.codexEndpointService = codexEndpointService
		a.codexCredentialGroupService = codexCredentialGroupService
		a.codexSubscriptions = codexSubscriptions
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
	// key 必须带 provider 与档位：ListPricings 返回两个 provider 的全部记录，
	// 若仅以模型名为 key，同名模型会静默互相覆盖（详见 tracking.ModelPricingKey）。
	pricing := make(map[string]tracking.ModelPricing)
	for _, r := range records {
		pricing[tracking.ModelPricingKey(r.Provider, r.ModelName, r.ServiceTier)] = a.modelPricingService.ToTrackingPricing(r)
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
			legacyKeyCounts := make(map[string]int, len(endpoints))
			for _, ep := range endpoints {
				if ep == nil {
					continue
				}
				legacyKeyCounts[tracking.EndpointMultiplierKey("claude", ep.Channel, ep.Name)]++
			}
			claudeGroupsByID := make(map[int64]*claudestore.ClaudeCredentialGroupRecord)
			claudeDefaultGroupByChannel := make(map[string]*claudestore.ClaudeCredentialGroupRecord)
			if a.claudeCredentialGroupStore != nil {
				for _, ep := range endpoints {
					if ep == nil {
						continue
					}
					if ep.CredentialGroupID > 0 {
						if _, ok := claudeGroupsByID[ep.CredentialGroupID]; !ok {
							if group, err := a.claudeCredentialGroupStore.GetByID(ctx, ep.CredentialGroupID); err == nil && group != nil {
								claudeGroupsByID[ep.CredentialGroupID] = group
							}
						}
					}
					if ep.Channel != "" {
						if _, ok := claudeDefaultGroupByChannel[ep.Channel]; !ok {
							if group, err := a.claudeCredentialGroupStore.GetDefaultByChannel(ctx, ep.Channel); err == nil && group != nil {
								claudeDefaultGroupByChannel[ep.Channel] = group
							}
						}
					}
				}
			}
			for _, ep := range endpoints {
				if ep == nil {
					continue
				}
				group := claudeGroupsByID[ep.CredentialGroupID]
				if group == nil {
					group = claudeDefaultGroupByChannel[ep.Channel]
				}
				groupName := ep.CredentialGroupName
				groupCostMultiplier := 1.0
				groupInputMultiplier := 1.0
				groupOutputMultiplier := 1.0
				groupCacheCreationMultiplier := 1.0
				groupCacheCreation1hMultiplier := 1.0
				groupCacheReadMultiplier := 1.0
				groupLongContextMultiplier := 1.0
				groupLongContextInputMultiplier := 1.0
				groupLongContextOutputMultiplier := 1.0
				groupLongContextCacheCreationMultiplier := 1.0
				groupLongContextCacheReadMultiplier := 1.0
				groupDisableLongContextPricing := false
				groupModelMultipliers := []tracking.ModelMultiplier(nil)
				if group != nil {
					groupName = group.Name
					groupCostMultiplier = defaultPositiveMultiplier(group.CostMultiplier)
					groupInputMultiplier = defaultPositiveMultiplier(group.InputCostMultiplier)
					groupOutputMultiplier = defaultPositiveMultiplier(group.OutputCostMultiplier)
					groupCacheCreationMultiplier = defaultPositiveMultiplier(group.CacheCreationCostMultiplier)
					groupCacheCreation1hMultiplier = defaultPositiveMultiplier(group.CacheCreationCostMultiplier1h)
					groupCacheReadMultiplier = defaultPositiveMultiplier(group.CacheReadCostMultiplier)
					groupLongContextMultiplier = defaultPositiveMultiplier(group.LongContextCostMultiplier)
					groupLongContextInputMultiplier = defaultPositiveMultiplier(group.LongContextInputCostMultiplier)
					groupLongContextOutputMultiplier = defaultPositiveMultiplier(group.LongContextOutputCostMultiplier)
					groupLongContextCacheCreationMultiplier = defaultPositiveMultiplier(group.LongContextCacheCreationCostMultiplier)
					groupLongContextCacheReadMultiplier = defaultPositiveMultiplier(group.LongContextCacheReadCostMultiplier)
					groupDisableLongContextPricing = group.DisableLongContextPricing
					groupModelMultipliers = endpointModelMultipliersToTracking(group.ModelMultipliers)
				}
				if groupName == "" && ep.CredentialGroupID > 0 {
					groupName = fmt.Sprintf("g%d", ep.CredentialGroupID)
				}
				// Claude 的 API Key 与计费配置归属凭证分组；端点只表示线路配置。
				// 历史端点倍率字段保留为兼容存储，但不再参与运行时计费（与 Codex 一致）。
				multiplier := tracking.EndpointMultiplier{
					CostMultiplier:                         groupCostMultiplier,
					InputCostMultiplier:                    groupInputMultiplier,
					OutputCostMultiplier:                   groupOutputMultiplier,
					CacheCreationCostMultiplier:            groupCacheCreationMultiplier,
					CacheCreationCostMultiplier1h:          groupCacheCreation1hMultiplier,
					CacheReadCostMultiplier:                groupCacheReadMultiplier,
					LongContextCostMultiplier:              groupLongContextMultiplier,
					LongContextInputCostMultiplier:         groupLongContextInputMultiplier,
					LongContextOutputCostMultiplier:        groupLongContextOutputMultiplier,
					LongContextCacheCreationCostMultiplier: groupLongContextCacheCreationMultiplier,
					LongContextCacheReadCostMultiplier:     groupLongContextCacheReadMultiplier,
					DisableLongContextPricing:              groupDisableLongContextPricing,
					ModelMultipliers:                       mergeTrackingModelMultipliers(endpointModelMultipliersToTracking(ep.ModelMultipliers), groupModelMultipliers),
				}
				keys := []string{
					tracking.EndpointMultiplierGroupKey("claude", ep.Channel, groupName, ep.Name),
					tracking.ScopeEndpointMultiplierKey("claude", claudeservice.EndpointRuntimeKey(ep)),
				}
				legacyKey := tracking.EndpointMultiplierKey("claude", ep.Channel, ep.Name)
				if legacyKeyCounts[legacyKey] <= 1 {
					keys = append(keys, legacyKey)
				}
				seenKeys := make(map[string]struct{}, len(keys))
				for _, key := range keys {
					if key == "" {
						continue
					}
					if _, ok := seenKeys[key]; ok {
						continue
					}
					seenKeys[key] = struct{}{}
					multipliers[key] = multiplier
				}
			}
		}
	}

	// 2. Codex 端点倍率（每个端点的 key = channel.name::credential_group.name::endpoint.name）
	if a.codexChannelStore != nil && a.codexEndpointStore != nil {
		channels, err := a.codexChannelStore.List(ctx)
		if err != nil {
			a.logger.Warn("⚠️ 获取 Codex 渠道列表失败", "error", err)
		} else {
			for _, ch := range channels {
				var defaultGroup *codexstore.CodexCredentialGroupRecord
				if a.codexCredentialGroupStore != nil {
					defaultGroup, _ = a.codexCredentialGroupStore.GetDefaultByChannelID(ctx, ch.ID)
				}
				codexEndpoints, err := a.codexEndpointStore.ListByChannelID(ctx, ch.ID)
				if err != nil {
					a.logger.Warn("⚠️ 获取 Codex 端点列表失败", "channel", ch.Name, "error", err)
					continue
				}
				legacyKeyCounts := make(map[string]int, len(codexEndpoints))
				for _, ep := range codexEndpoints {
					if ep == nil {
						continue
					}
					legacyKeyCounts[tracking.EndpointMultiplierKey("codex", ch.Name, ep.Name)]++
				}
				for _, ep := range codexEndpoints {
					if ep == nil {
						continue
					}
					group := defaultGroup
					if a.codexCredentialGroupStore != nil && ep.CredentialGroupID > 0 {
						if resolved, err := a.codexCredentialGroupStore.GetByID(ctx, ep.CredentialGroupID); err == nil && resolved != nil {
							group = resolved
						}
					}
					groupName := ""
					groupCostMultiplier := 1.0
					groupInputMultiplier := 1.0
					groupOutputMultiplier := 1.0
					groupCacheCreationMultiplier := 1.0
					groupCacheReadMultiplier := 1.0
					groupLongContextMultiplier := 1.0
					groupLongContextInputMultiplier := 1.0
					groupLongContextOutputMultiplier := 1.0
					groupLongContextCacheCreationMultiplier := 1.0
					groupLongContextCacheReadMultiplier := 1.0
					groupDisableLongContextPricing := false
					groupModelMultipliers := []tracking.ModelMultiplier(nil)
					groupBillingRules := []tracking.BillingRule(nil)
					billingOutputTokenSource := "output_tokens"
					if group != nil {
						groupName = group.Name
						groupCostMultiplier = defaultPositiveMultiplier(group.CostMultiplier)
						groupInputMultiplier = defaultPositiveMultiplier(group.InputCostMultiplier)
						groupOutputMultiplier = defaultPositiveMultiplier(group.OutputCostMultiplier)
						groupCacheCreationMultiplier = defaultPositiveMultiplier(group.CacheCreationCostMultiplier)
						groupCacheReadMultiplier = defaultPositiveMultiplier(group.CacheReadCostMultiplier)
						groupLongContextMultiplier = defaultPositiveMultiplier(group.LongContextCostMultiplier)
						groupLongContextInputMultiplier = defaultPositiveMultiplier(group.LongContextInputCostMultiplier)
						groupLongContextOutputMultiplier = defaultPositiveMultiplier(group.LongContextOutputCostMultiplier)
						groupLongContextCacheCreationMultiplier = defaultPositiveMultiplier(group.LongContextCacheCreationCostMultiplier)
						groupLongContextCacheReadMultiplier = defaultPositiveMultiplier(group.LongContextCacheReadCostMultiplier)
						groupDisableLongContextPricing = group.DisableLongContextPricing
						groupModelMultipliers = codexModelMultipliersToTracking(group.ModelMultipliers)
						groupBillingRules = codexBillingRulesToTracking(group.BillingRules)
						billingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(group.BillingOutputTokenSource)
					}
					if groupName == "" && ep.CredentialGroupID > 0 {
						groupName = fmt.Sprintf("g%d", ep.CredentialGroupID)
					}
					// Codex 的 API Key 与计费配置归属凭证分组；端点只表示 base_url 线路配置。
					// 历史端点倍率字段保留为兼容存储，但不再参与运行时计费。
					multiplier := tracking.EndpointMultiplier{
						CostMultiplier:                         groupCostMultiplier,
						InputCostMultiplier:                    groupInputMultiplier,
						OutputCostMultiplier:                   groupOutputMultiplier,
						CacheCreationCostMultiplier:            groupCacheCreationMultiplier,
						CacheCreationCostMultiplier1h:          1.0,
						CacheReadCostMultiplier:                groupCacheReadMultiplier,
						LongContextCostMultiplier:              groupLongContextMultiplier,
						LongContextInputCostMultiplier:         groupLongContextInputMultiplier,
						LongContextOutputCostMultiplier:        groupLongContextOutputMultiplier,
						LongContextCacheCreationCostMultiplier: groupLongContextCacheCreationMultiplier,
						LongContextCacheReadCostMultiplier:     groupLongContextCacheReadMultiplier,
						DisableLongContextPricing:              groupDisableLongContextPricing,
						BillingOutputTokenSource:               billingOutputTokenSource,
						ModelMultipliers:                       mergeTrackingModelMultipliers(codexModelMultipliersToTracking(ep.ModelMultipliers), groupModelMultipliers),
						BillingRules:                           groupBillingRules,
					}
					legacyKey := tracking.EndpointMultiplierKey("codex", ch.Name, ep.Name)
					groupKey := tracking.EndpointMultiplierGroupKey("codex", ch.Name, groupName, ep.Name)
					keys := make([]string, 0, 2)
					if groupKey != "" && (groupKey != legacyKey || legacyKeyCounts[legacyKey] <= 1) {
						keys = append(keys, groupKey)
					}
					if legacyKeyCounts[legacyKey] <= 1 {
						keys = append(keys, legacyKey)
					}
					seenKeys := make(map[string]struct{}, len(keys))
					for _, key := range keys {
						if key == "" {
							continue
						}
						if _, ok := seenKeys[key]; ok {
							continue
						}
						seenKeys[key] = struct{}{}
						multipliers[key] = multiplier
					}
				}
			}
		}
	}

	// 更新 UsageTracker 的端点倍率缓存
	a.usageTracker.UpdateEndpointMultipliers(multipliers)
	a.logger.Debug("已同步端点倍率到 UsageTracker", "count", len(multipliers))
}

func endpointModelMultipliersToTracking(rules []claudestore.EndpointModelMultiplierRecord) []tracking.ModelMultiplier {
	if len(rules) == 0 {
		return nil
	}
	result := make([]tracking.ModelMultiplier, 0, len(rules))
	for _, rule := range rules {
		result = append(result, tracking.ModelMultiplier{
			Model:                   rule.Model,
			CostMultiplier:          rule.CostMultiplier,
			InputCostMultiplier:     rule.InputCostMultiplier,
			OutputCostMultiplier:    rule.OutputCostMultiplier,
			CacheReadCostMultiplier: rule.CacheReadCostMultiplier,
		})
	}
	return result
}

func codexModelMultipliersToTracking(rules []codexstore.CodexModelMultiplierRecord) []tracking.ModelMultiplier {
	if len(rules) == 0 {
		return nil
	}
	result := make([]tracking.ModelMultiplier, 0, len(rules))
	for _, rule := range rules {
		result = append(result, tracking.ModelMultiplier{
			Model:                   rule.Model,
			CostMultiplier:          rule.CostMultiplier,
			InputCostMultiplier:     rule.InputCostMultiplier,
			OutputCostMultiplier:    rule.OutputCostMultiplier,
			CacheReadCostMultiplier: rule.CacheReadCostMultiplier,
		})
	}
	return result
}

func codexBillingRulesToTracking(records []codexstore.CodexBillingRuleRecord) []tracking.BillingRule {
	if len(records) == 0 {
		return nil
	}
	result := make([]tracking.BillingRule, 0, len(records))
	for _, record := range records {
		result = append(result, tracking.BillingRule{
			Model:                       record.Model,
			ServiceTier:                 record.ServiceTier,
			Context:                     record.Context,
			Mode:                        record.Mode,
			CostMultiplier:              record.CostMultiplier,
			InputCostMultiplier:         record.InputCostMultiplier,
			OutputCostMultiplier:        record.OutputCostMultiplier,
			CacheCreationCostMultiplier: record.CacheCreationCostMultiplier,
			CacheReadCostMultiplier:     record.CacheReadCostMultiplier,
			InputPrice:                  record.InputPrice,
			OutputPrice:                 record.OutputPrice,
			CacheCreationPrice:          record.CacheCreationPrice,
			CacheReadPrice:              record.CacheReadPrice,
		})
	}
	return result
}

func mergeTrackingModelMultipliers(primary, fallback []tracking.ModelMultiplier) []tracking.ModelMultiplier {
	if len(primary) == 0 {
		return fallback
	}
	if len(fallback) == 0 {
		return primary
	}

	result := make([]tracking.ModelMultiplier, 0, len(primary)+len(fallback))
	seen := make(map[string]struct{}, len(primary)+len(fallback))
	appendRule := func(rule tracking.ModelMultiplier) {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		rule.Model = model
		result = append(result, rule)
	}
	for _, rule := range primary {
		appendRule(rule)
	}
	for _, rule := range fallback {
		appendRule(rule)
	}
	return result
}

func defaultPositiveMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

// setupProxyHandler 设置代理处理器
func (a *App) setupProxyHandler() {
	// 创建代理处理器
	a.proxyHandler = proxy.NewHandler(a.endpointManager, a.config)
	a.proxyHandler.SetEventBus(a.eventBus)
	a.proxyHandler.SetPrivacyService(a.privacyService)
	a.proxyHandler.SetSubscriptionController(a.claudeSubscriptions)

	// 创建中间件（Claude 代理日志附加 provider=claude，用于文件分流）
	a.loggingMiddleware = middleware.NewLoggingMiddleware(a.logger.With("provider", "claude"))
	a.monitoringMiddleware = middleware.NewMonitoringMiddleware(a.endpointManager)
	a.authMiddleware = middleware.NewAuthMiddleware(a.config.Auth)
	if a.claudeSubscriptions != nil {
		a.authMiddleware.SetRequestAuthenticator(func(r *http.Request) (context.Context, bool, error) {
			if strings.HasPrefix(r.URL.Path, "/v1/messages") {
				return a.claudeSubscriptions.Authenticate(r)
			}
			if a.codexSubscriptions != nil {
				return a.codexSubscriptions.Authenticate(r)
			}
			return a.claudeSubscriptions.Authenticate(r)
		})
	}

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
	a.config.CodexServer.Enabled = true

	// 检查依赖
	if a.codexChannelStore == nil || a.codexEndpointStore == nil {
		a.logger.Warn("Codex 代理初始化跳过：渠道/端点存储未就绪")
		return
	}

	// 创建 EndpointManager
	mgr := codexproxy.NewEndpointManager(a.codexChannelStore, a.codexEndpointStore, a.codexCredentialGroupStore, a.logger)
	mgr.SetActivationMutex(&a.codexActivationMu)
	mgr.SetConfig(a.config)

	// 启动时恢复活跃渠道参与状态与首次 Reload 必须处于同一补偿边界。
	// Reload 成功前不发布 manager，避免请求观察到未验证的运行时状态。
	a.codexActivationMu.Lock()
	var (
		activeSnapshot *codexstore.CodexChannelRecord
		resumed        bool
		prepareErr     error
	)
	if a.config.CodexFailover.Enabled {
		if a.codexChannelService == nil {
			prepareErr = fmt.Errorf("Codex 渠道服务未初始化")
		} else {
			prepareCtx, prepareCancel := context.WithTimeout(context.Background(), 10*time.Second)
			activeSnapshot, prepareErr = a.codexChannelService.GetActiveChannel(prepareCtx)
			if prepareErr == nil {
				resumed, prepareErr = a.codexChannelService.EnsureActiveChannelParticipates(prepareCtx)
			}
			prepareCancel()
		}
	}
	if prepareErr == nil {
		prepareErr = mgr.Reload()
	}
	if prepareErr != nil && activeSnapshot != nil {
		if restoreErr := a.restoreCodexActiveParticipation(activeSnapshot); restoreErr != nil {
			prepareErr = fmt.Errorf("%w；恢复原活跃渠道参与状态失败: %v", prepareErr, restoreErr)
		}
	}
	a.codexActivationMu.Unlock()

	if prepareErr != nil {
		if a.config.CodexFailover.Enabled {
			if rollbackErr := a.rollbackFailoverSetting(service.ProviderCodex, "Codex 渠道间转移已关闭：启动时运行时加载失败", prepareErr); rollbackErr != nil {
				a.logger.Error("启动时回滚 Codex 渠道间转移失败", "error", rollbackErr)
			}
		} else {
			a.logger.Error("Codex 端点管理器加载失败", "error", prepareErr)
		}
		return
	}
	if resumed {
		a.logger.Info("✅ Codex 当前接管渠道已自动恢复参与渠道间转移")
	}
	// 注册活跃端点变更回调：故障转移成功提升端点后，刷新前端 UI
	mgr.SetOnActiveChanged(func() {
		a.emitCodexProviderStateUpdate()
	})
	a.codexEndpointMgr = mgr
	// 发布 Codex 模型路由规则（渠道非流改道已随 Reload 加载）；此时代理端口尚未开始接收请求
	a.syncCodexModelRules(context.Background())

	// 创建 HealthChecker（参数动态从 a.config.Health.* 读取，设置页改动下一轮即生效）
	hc := codexproxy.NewHealthChecker(mgr, a.config, a.logger)
	// 按设置项决定是否启动定时探测；保留实例以供 GetLatencies 等接口使用
	// 与 Claude 端对齐：仅当 Enabled + LoopRetry + SkipHealthCheck 三者都为 true 时才跳过
	if !a.config.CodexFailover.ShouldSkipHealthCheck() {
		hc.Start()
	} else {
		a.logger.Info("Codex 健康检查器已创建但未启动（ShouldSkipHealthCheck=true）")
	}
	a.codexHealthCheck = hc

	// 创建 SuspensionManager（请求挂起队列）
	// 当 loop_retry 全部耗尽后将请求挂起，等待运维通过 UI 重新激活渠道/端点
	sm := codexproxy.NewSuspensionManager(
		a.config.CodexFailover.MaxSuspendedRequests,
		a.config.CodexFailover.LoopRetrySuspendTimeout,
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
	if a.codexEndpointMgr == nil {
		return
	}

	cfg := codexproxy.ServerConfig{
		Timeout:       a.config.CodexServer.Timeout,
		Config:        a.config,
		Retry:         a.config.Retry,         // 同端点重试配置（设置页 retry.*）
		Failover:      a.config.CodexFailover, // Codex 独立故障转移配置
		SuspensionMgr: a.codexSuspensionMgr,   // 请求挂起队列
	}
	baseHandler := codexproxy.NewHTTPHandler(a.codexEndpointMgr, cfg, a.logger, a.usageTracker, a.privacyService)
	baseHandler.SetSubscriptionController(a.codexSubscriptions)
	a.codexHTTPHandler = baseHandler

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
		"/backend-api/codex",
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
	a.codexHTTPHandler = nil
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
	a.settingsService.SetOnChangeCallback(func() error {
		return a.applySettingsToConfig(true)
	})

	// 初始化默认设置（如果表为空）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.settingsService.InitDefaults(ctx); err != nil {
		a.logger.Error("❌ 初始化默认设置失败", "error", err)
		return
	}

	// 从数据库加载设置并应用到配置
	if err := a.applySettingsToConfig(false); err != nil {
		var failoverErr *failoverApplyError
		if errors.As(err, &failoverErr) {
			if rollbackErr := a.rollbackFailoverSetting(failoverErr.provider, "启动时无法启用渠道间转移", failoverErr.err); rollbackErr != nil {
				a.logger.Error("启动时回滚渠道间转移失败", "error", rollbackErr)
			}
		} else {
			a.logger.Error("从数据库应用设置失败", "error", err)
		}
	}

	// 初始化端口管理器
	preferredPort := a.settingsService.GetInt(ctx, service.CategoryServer, preferredPortSettingKey(), a.config.Server.Port)

	// Dev 模式：端口偏移 +10，允许与 release 版本同时运行
	if BuildMode == "dev" {
		preferredPort += 10
		a.logger.Info("🔧 Dev 模式端口偏移", "original", preferredPort-10, "dev_port", preferredPort)
	}

	a.portManager = utils.NewPortManager(preferredPort)

	a.logger.Info("✅ 系统设置存储已启用 (SQLite)")
}

func (a *App) setupPrivacyService() {
	// Keep a non-nil faulted service so proxy handlers keep working if privacy
	// initialization breaks. A ready service with zero rules still passes through.
	a.privacyService = privacy.NewService(nil, a.logger)
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	if db == nil {
		a.logger.Warn("隐私防护服务未启用：数据库未就绪")
		return
	}

	a.privacyStore = privacy.NewStore(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.privacyStore.EnsureSchema(ctx); err != nil {
		a.logger.Warn("隐私防护规则表兼容升级失败", "error", err)
		return
	}
	service := privacy.NewService(a.privacyStore, a.logger)
	if err := service.Reload(ctx); err != nil {
		// 规则编译失败时服务保持空快照（放行），由日志暴露问题，
		// 不因隐私模块自身故障中断全部转发。
		a.logger.Warn("隐私防护规则加载失败，本次启动将不执行任何隐私规则", "error", err)
	}
	a.privacyService = service
	a.logger.Info("✅ 隐私防护服务已启用 (SQLite)")
}

type failoverApplyError struct {
	provider service.SettingsProvider
	err      error
}

func (e *failoverApplyError) Error() string { return e.err.Error() }
func (e *failoverApplyError) Unwrap() error { return e.err }

const failoverRollbackTimeout = 5 * time.Second

// rollbackFailoverSetting disables a setting directly when the corresponding
// active-channel repair cannot be completed. Calling the store directly avoids
// recursively triggering the hot-reload callback with a state that is known to
// be invalid.
func (a *App) rollbackFailoverSetting(provider service.SettingsProvider, title string, cause error) error {
	if a.config != nil {
		switch provider {
		case service.ProviderClaude:
			a.config.Failover.Enabled = false
			if a.endpointManager != nil {
				a.endpointManager.SetFailoverEnabled(false)
			}
		case service.ProviderCodex:
			a.config.CodexFailover.Enabled = false
			if a.codexHTTPHandler != nil {
				a.codexHTTPHandler.UpdateConfig(a.config.Retry, a.config.CodexFailover)
			}
		}
	}
	var rollbackErr error
	if a.settingsStore != nil {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), failoverRollbackTimeout)
		defer rollbackCancel()
		if err := a.settingsStore.SetScoped(rollbackCtx, string(provider), service.CategoryFailover, "enabled", "false"); err != nil {
			rollbackErr = err
			if a.logger != nil {
				a.logger.Error("回滚渠道间转移设置失败", "provider", provider, "error", err)
			}
		}
	}
	if a.logger != nil {
		a.logger.Error(title, "error", cause)
	}
	a.emitNotification("error", title, cause.Error())
	if rollbackErr != nil {
		return fmt.Errorf("%w；持久化回滚失败: %v", cause, rollbackErr)
	}
	return nil
}

func (a *App) restoreCodexActiveParticipation(snapshot *codexstore.CodexChannelRecord) error {
	if snapshot == nil {
		return nil
	}
	if a.codexChannelService == nil {
		return fmt.Errorf("Codex 渠道服务未初始化")
	}
	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), failoverRollbackTimeout)
	defer restoreCancel()
	return a.codexChannelService.SetActiveChannelParticipation(restoreCtx, snapshot.ID, snapshot.FailoverEnabled)
}

// applySettingsToConfig 从数据库加载设置并应用到运行时配置。
func (a *App) applySettingsToConfig(runRetentionMaintenance bool) error {
	if a.settingsService == nil {
		return nil
	}
	if a.config == nil {
		return fmt.Errorf("运行时配置未初始化")
	}
	previousConfig := *a.config
	restoreRuntimeConfig := func() {
		*a.config = previousConfig
		if a.endpointManager != nil {
			a.endpointManager.UpdateConfig(a.config)
			a.endpointManager.SetFailoverEnabled(a.config.Failover.Enabled)
		}
		if a.codexEndpointMgr != nil {
			a.codexEndpointMgr.SetConfig(a.config)
		}
		if a.proxyHandler != nil {
			a.proxyHandler.UpdateConfig(a.config)
		}
		if a.authMiddleware != nil {
			a.authMiddleware.UpdateConfig(a.config.Auth)
		}
		if a.codexHTTPHandler != nil {
			a.codexHTTPHandler.UpdateConfig(a.config.Retry, a.config.CodexFailover)
		}
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
	requestedFailoverEnabled, err := a.settingsService.GetBoolStrict(ctx, service.CategoryFailover, "enabled", a.config.Failover.Enabled)
	if err != nil {
		restoreRuntimeConfig()
		return &failoverApplyError{provider: service.ProviderClaude, err: fmt.Errorf("读取 Claude 渠道间转移设置失败: %w", err)}
	}
	if requestedFailoverEnabled && a.endpointService != nil && a.channelService != nil {
		if err := a.syncChannelFailoverEnabledToEndpointManager(ctx); err != nil {
			restoreRuntimeConfig()
			return &failoverApplyError{provider: service.ProviderClaude, err: fmt.Errorf("Claude 渠道间转移未启用：无法同步渠道参与状态: %w", err)}
		}
		resumed, err := a.endpointService.EnsureActiveChannelParticipates(ctx, a.channelService)
		if err != nil {
			restoreRuntimeConfig()
			return &failoverApplyError{provider: service.ProviderClaude, err: fmt.Errorf("Claude 渠道间转移未启用：当前接管渠道无法恢复参与: %w", err)}
		} else {
			a.config.Failover.Enabled = true
			if a.endpointManager != nil {
				a.endpointManager.SetFailoverEnabled(true)
			}
			if resumed {
				a.logger.Info("✅ Claude 渠道间转移已启用，当前接管渠道已自动恢复参与")
				a.emitEndpointUpdate()
			}
		}
	} else {
		a.config.Failover.Enabled = requestedFailoverEnabled
		if a.endpointManager != nil {
			a.endpointManager.SetFailoverEnabled(requestedFailoverEnabled)
		}
	}
	// 从数据库加载 loop_retry、max_loops、skip_health_check 到 Failover
	a.config.Failover.LoopRetry = a.settingsService.GetBool(ctx, service.CategoryFailover, "loop_retry", a.config.Failover.LoopRetry)
	a.config.Failover.MaxLoops = a.settingsService.GetInt(ctx, service.CategoryFailover, "max_loops", a.config.Failover.MaxLoops)
	a.config.Failover.SkipHealthCheck = a.settingsService.GetBool(ctx, service.CategoryFailover, "skip_health_check", a.config.Failover.SkipHealthCheck)
	// 达到最大循环轮数后的手动等待超时
	a.config.Failover.LoopRetrySuspendTimeout = a.settingsService.GetDuration(ctx, service.CategoryFailover, "loop_retry_wait_timeout", a.config.Failover.LoopRetrySuspendTimeout)

	// Codex 独立故障转移配置
	requestedCodexFailoverEnabled, err := a.settingsService.GetBoolScopedStrict(ctx, service.ProviderCodex, service.CategoryFailover, "enabled", false)
	if err != nil {
		restoreRuntimeConfig()
		return &failoverApplyError{provider: service.ProviderCodex, err: fmt.Errorf("读取 Codex 渠道间转移设置失败: %w", err)}
	}
	a.config.CodexFailover.LoopRetry = a.settingsService.GetBoolScoped(ctx, service.ProviderCodex, service.CategoryFailover, "loop_retry", true)
	a.config.CodexFailover.MaxLoops = a.settingsService.GetIntScoped(ctx, service.ProviderCodex, service.CategoryFailover, "max_loops", 3)
	a.config.CodexFailover.SkipHealthCheck = a.settingsService.GetBoolScoped(ctx, service.ProviderCodex, service.CategoryFailover, "skip_health_check", false)
	a.config.CodexFailover.LoopRetrySuspendTimeout = a.settingsService.GetDurationScoped(ctx, service.ProviderCodex, service.CategoryFailover, "loop_retry_wait_timeout", 120*time.Second)
	if requestedCodexFailoverEnabled && a.codexChannelService != nil {
		a.codexActivationMu.Lock()
		activeSnapshot, err := a.codexChannelService.GetActiveChannel(ctx)
		resumed := false
		if err == nil {
			resumed, err = a.codexChannelService.EnsureActiveChannelParticipates(ctx)
		}
		if err == nil {
			if a.codexEndpointMgr == nil {
				err = fmt.Errorf("Codex 端点管理器未初始化")
			} else if reloadErr := a.reloadCodexEndpointManager(); reloadErr != nil {
				err = fmt.Errorf("重新加载 Codex 运行时失败: %w", reloadErr)
			}
		}
		if err != nil && activeSnapshot != nil && !activeSnapshot.FailoverEnabled {
			if rollbackErr := a.restoreCodexActiveParticipation(activeSnapshot); rollbackErr != nil {
				err = fmt.Errorf("%w；恢复原活跃渠道参与状态失败: %v", err, rollbackErr)
			}
		}
		a.codexActivationMu.Unlock()
		if err != nil {
			restoreRuntimeConfig()
			return &failoverApplyError{provider: service.ProviderCodex, err: fmt.Errorf("Codex 渠道间转移未启用：当前接管渠道无法恢复参与: %w", err)}
		} else {
			a.config.CodexFailover.Enabled = true
			if a.codexHTTPHandler != nil {
				a.codexHTTPHandler.UpdateConfig(a.config.Retry, a.config.CodexFailover)
			}
			if resumed {
				a.logger.Info("✅ Codex 渠道间转移已启用，当前接管渠道已自动恢复参与")
			}
			// Runtime reload above already refreshed the manager; only publish the
			// resulting state here to avoid a second reload with a swallowed error.
			a.emitCodexProviderStateUpdate()
		}
	} else {
		a.config.CodexFailover.Enabled = requestedCodexFailoverEnabled
		if a.codexHTTPHandler != nil {
			a.codexHTTPHandler.UpdateConfig(a.config.Retry, a.config.CodexFailover)
		}
	}

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
	a.config.Streaming.ResponseHeaderTimeout = a.settingsService.GetDuration(ctx, service.CategoryRequest, "response_header_timeout", a.config.Streaming.ResponseHeaderTimeout)

	// 访问控制配置
	a.config.Auth.Enabled = a.settingsService.GetBool(ctx, service.CategoryAuth, "enabled", a.config.Auth.Enabled)
	a.config.Auth.Token = a.getSettingString(ctx, service.CategoryAuth, "token", a.config.Auth.Token)

	// Token 计数配置
	a.config.TokenCounting.Enabled = a.settingsService.GetBool(ctx, service.CategoryTokenCounting, "enabled", a.config.TokenCounting.Enabled)
	a.config.TokenCounting.EstimationRatio = a.settingsService.GetFloat(ctx, service.CategoryTokenCounting, "estimation_ratio", a.config.TokenCounting.EstimationRatio)

	// 数据保留配置
	a.config.UsageTracking.RetentionDays = a.settingsService.GetInt(ctx, service.CategoryRetention, "retention_days", a.config.UsageTracking.RetentionDays)
	if a.usageTracker != nil {
		applyPolicy := a.usageTracker.ConfigureRetentionPolicy
		if runRetentionMaintenance {
			applyPolicy = a.usageTracker.UpdateRetentionPolicy
		}
		if err := applyPolicy(a.config.UsageTracking.RetentionDays); err != nil {
			a.logger.Error("应用数据保留策略失败", "error", err)
			a.emitNotification("error", "数据保留策略更新失败", err.Error())
		}
	}

	// 🔧 [P5.5.4 新增] Logging 日志配置（从 SQLite 同步）
	oldLoggingLevel := a.config.Logging.Level
	oldLoggingFormat := a.config.Logging.Format
	a.config.Logging.Level = a.getSettingString(ctx, service.CategoryLogging, "level", a.config.Logging.Level)
	a.config.Logging.Format = a.getSettingString(ctx, service.CategoryLogging, "format", a.config.Logging.Format)
	if a.config.Logging.Level != oldLoggingLevel || a.config.Logging.Format != oldLoggingFormat {
		a.applyRuntimeLoggingConfig()
	}

	// 🔧 [P5.5.4 新增] Timezone 时区配置（从 SQLite 同步）
	// 注意：时区变更标记为 RequiresRestart=true，运行时变更只影响后续新生成的时间戳
	a.config.Timezone = a.getSettingString(ctx, service.CategoryTimezone, "global", a.config.Timezone)

	// Codex 代理与 Claude 代理共用主端口，始终启用路径路由。
	a.config.CodexServer.Enabled = true
	a.config.CodexServer.Timeout = a.config.GlobalTimeout

	a.logger.Debug("已从数据库加载设置")

	// 更新各组件配置
	if a.endpointManager != nil {
		a.endpointManager.UpdateConfig(a.config)
	}
	if a.codexEndpointMgr != nil {
		a.codexEndpointMgr.SetConfig(a.config)
	}
	if a.proxyHandler != nil {
		a.proxyHandler.UpdateConfig(a.config)
	}
	if a.authMiddleware != nil {
		a.authMiddleware.UpdateConfig(a.config.Auth)
	}

	a.applyCacheSettings(ctx)
	return nil
}

// getSettingString 获取字符串设置值（带默认值）
func (a *App) getSettingString(ctx context.Context, category, key, defaultVal string) string {
	val, err := a.settingsService.GetValue(ctx, category, key)
	if err != nil || val == "" {
		return defaultVal
	}
	return val
}
