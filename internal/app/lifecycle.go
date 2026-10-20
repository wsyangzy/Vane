package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/platform/transport"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
)

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

func (a *App) startup() {

	// 1. 加载配置
	a.loadConfig()

	// 1.5. 在日志系统初始化前应用 SQLite 中的日志设置，避免先按代码默认值写入启动日志
	a.applyPersistedLoggingConfigBeforeLogger()

	// 2. 初始化日志
	a.setupLogger()

	// 3. 显示启动信息
	a.logger.Info("🚀 Vane 桌面版启动中...",
		"version", buildinfo.Version,
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
		a.logger.Error("❌ Vane 启动未完成：代理服务器未启动",
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
	a.logger.Info("✅ Vane 启动完成",
		"proxy_port", a.config.Server.Port,
		"app_state", AppState(a.appState.Load()).String())
}

func (a *App) onWindowReady() {
	// 发送初始状态给前端
	a.emitSystemStatus()
}

func (a *App) isQuitting() bool {
	return atomic.LoadInt32(&a.quitting) == 1
}

func (a *App) markQuitting() {
	atomic.StoreInt32(&a.quitting, 1)
}
