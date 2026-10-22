package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/middleware"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy"
	codexproxy "github.com/wsyangzy/Vane/internal/providers/codex/proxy"
)

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
			provider := "codex"
			if strings.HasPrefix(r.URL.Path, "/v1/messages") {
				provider = "claude"
			}
			if ctx, handled, err := a.authenticatePool(r, provider); handled {
				return ctx, handled, err
			}
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

	// Pool scheduling never changes persisted global channel participation.
	if err := mgr.Reload(); err != nil {
		a.logger.Error("Codex 端点管理器加载失败", "error", err)
		return
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
	if mgr.PoolRuntime() != nil || !a.config.CodexFailover.ShouldSkipHealthCheck() {
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

func (a *App) stopCodexProxy() {
	if a.codexHealthCheck != nil {
		a.codexHealthCheck.Stop()
		a.codexHealthCheck = nil
	}
	a.codexEndpointMgr = nil
	a.codexHTTPHandler = nil
}
