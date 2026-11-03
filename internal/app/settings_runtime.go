package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/netutil"
	"github.com/wsyangzy/Vane/internal/privacy"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

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
	a.settingsStore = settingsstore.NewSQLiteSettingsStore(db)

	// 创建 SettingsService
	a.settingsService = settingsservice.NewSettingsService(a.settingsStore)

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
	preferredPort := a.settingsService.GetInt(ctx, settingsservice.CategoryServer, preferredPortSettingKey(), a.config.Server.Port)

	// Dev 模式：端口偏移 +10，允许与 release 版本同时运行
	if buildinfo.BuildMode == "dev" {
		preferredPort += 10
		a.logger.Info("🔧 Dev 模式端口偏移", "original", preferredPort-10, "dev_port", preferredPort)
	}

	a.portManager = netutil.NewPortManager(preferredPort)

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
	provider settingsservice.SettingsProvider
	err      error
}

func (e *failoverApplyError) Error() string { return e.err.Error() }

func (e *failoverApplyError) Unwrap() error { return e.err }

const failoverRollbackTimeout = 5 * time.Second

func (a *App) rollbackFailoverSetting(provider settingsservice.SettingsProvider, title string, cause error) error {
	if a.config != nil {
		switch provider {
		case settingsservice.ProviderClaude:
			a.config.Failover.Enabled = false
			if a.endpointManager != nil {
				a.endpointManager.SetFailoverEnabled(false)
			}
		case settingsservice.ProviderCodex:
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
		if err := a.settingsStore.SetScoped(rollbackCtx, string(provider), settingsservice.CategoryFailover, "enabled", "false"); err != nil {
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
	a.config.Strategy.Type = a.getSettingString(ctx, settingsservice.CategoryStrategy, "type", a.config.Strategy.Type)
	a.config.Strategy.FastTestEnabled = a.settingsService.GetBool(ctx, settingsservice.CategoryStrategy, "fast_test_enabled", a.config.Strategy.FastTestEnabled)
	a.config.Strategy.FastTestCacheTTL = a.settingsService.GetDuration(ctx, settingsservice.CategoryStrategy, "fast_test_cache_ttl", a.config.Strategy.FastTestCacheTTL)
	a.config.Strategy.FastTestTimeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryStrategy, "fast_test_timeout", a.config.Strategy.FastTestTimeout)
	a.config.Strategy.FastTestPath = a.getSettingString(ctx, settingsservice.CategoryStrategy, "fast_test_path", a.config.Strategy.FastTestPath)

	// 重试配置
	a.config.Retry.MaxAttempts = a.settingsService.GetInt(ctx, settingsservice.CategoryRetry, "max_attempts", a.config.Retry.MaxAttempts)
	a.config.Retry.BaseDelay = a.settingsService.GetDuration(ctx, settingsservice.CategoryRetry, "base_delay", a.config.Retry.BaseDelay)
	a.config.Retry.MaxDelay = a.settingsService.GetDuration(ctx, settingsservice.CategoryRetry, "max_delay", a.config.Retry.MaxDelay)
	a.config.Retry.Multiplier = a.settingsService.GetFloat(ctx, settingsservice.CategoryRetry, "multiplier", a.config.Retry.Multiplier)
	// Current profile execution consumes these shared ceilings in both adapters.
	// Legacy per-endpoint retry values above remain isolated compatibility data.
	a.config.Retry.MaxTotalAttempts = a.settingsService.GetInt(ctx, settingsservice.CategoryRetry, "max_total_attempts", a.config.Retry.MaxTotalAttempts)
	a.config.Retry.MaxTotalDuration = a.settingsService.GetDuration(ctx, settingsservice.CategoryRetry, "max_total_duration", a.config.Retry.MaxTotalDuration)
	a.config.Retry.DisableRecoveryWait = !a.settingsService.GetBool(ctx, settingsservice.CategoryRetry, "wait_for_recovery", true)

	// 健康检查配置
	a.config.Health.PauseActiveProbes = !a.settingsService.GetBool(ctx, settingsservice.CategoryHealth, "active_probes_enabled", !a.config.Health.PauseActiveProbes)
	a.config.Health.CheckInterval = a.settingsService.GetDuration(ctx, settingsservice.CategoryHealth, "check_interval", a.config.Health.CheckInterval)
	a.config.Health.Timeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryHealth, "timeout", a.config.Health.Timeout)
	a.config.Health.HealthPath = a.getSettingString(ctx, settingsservice.CategoryHealth, "health_path", a.config.Health.HealthPath)

	// 故障转移配置
	requestedFailoverEnabled, err := a.settingsService.GetBoolStrict(ctx, settingsservice.CategoryFailover, "enabled", a.config.Failover.Enabled)
	if err != nil {
		restoreRuntimeConfig()
		return &failoverApplyError{provider: settingsservice.ProviderClaude, err: fmt.Errorf("读取 Claude 渠道间转移设置失败: %w", err)}
	}
	if requestedFailoverEnabled && a.endpointService != nil && a.channelService != nil {
		if err := a.syncChannelFailoverEnabledToEndpointManager(ctx); err != nil {
			restoreRuntimeConfig()
			return &failoverApplyError{provider: settingsservice.ProviderClaude, err: fmt.Errorf("Claude 渠道间转移未启用：无法同步渠道参与状态: %w", err)}
		}
		resumed, err := a.endpointService.EnsureActiveChannelParticipates(ctx, a.channelService)
		if err != nil {
			restoreRuntimeConfig()
			return &failoverApplyError{provider: settingsservice.ProviderClaude, err: fmt.Errorf("Claude 渠道间转移未启用：当前接管渠道无法恢复参与: %w", err)}
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
	a.config.Failover.LoopRetry = a.settingsService.GetBool(ctx, settingsservice.CategoryFailover, "loop_retry", a.config.Failover.LoopRetry)
	a.config.Failover.MaxLoops = a.settingsService.GetInt(ctx, settingsservice.CategoryFailover, "max_loops", a.config.Failover.MaxLoops)
	a.config.Failover.SkipHealthCheck = a.settingsService.GetBool(ctx, settingsservice.CategoryFailover, "skip_health_check", a.config.Failover.SkipHealthCheck)
	// 达到最大循环轮数后的手动等待超时
	a.config.Failover.LoopRetrySuspendTimeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryFailover, "loop_retry_wait_timeout", a.config.Failover.LoopRetrySuspendTimeout)

	// Codex 独立故障转移配置
	requestedCodexFailoverEnabled, err := a.settingsService.GetBoolScopedStrict(ctx, settingsservice.ProviderCodex, settingsservice.CategoryFailover, "enabled", false)
	if err != nil {
		restoreRuntimeConfig()
		return &failoverApplyError{provider: settingsservice.ProviderCodex, err: fmt.Errorf("读取 Codex 渠道间转移设置失败: %w", err)}
	}
	a.config.CodexFailover.LoopRetry = a.settingsService.GetBoolScoped(ctx, settingsservice.ProviderCodex, settingsservice.CategoryFailover, "loop_retry", true)
	a.config.CodexFailover.MaxLoops = a.settingsService.GetIntScoped(ctx, settingsservice.ProviderCodex, settingsservice.CategoryFailover, "max_loops", 3)
	a.config.CodexFailover.SkipHealthCheck = a.settingsService.GetBoolScoped(ctx, settingsservice.ProviderCodex, settingsservice.CategoryFailover, "skip_health_check", false)
	a.config.CodexFailover.LoopRetrySuspendTimeout = a.settingsService.GetDurationScoped(ctx, settingsservice.ProviderCodex, settingsservice.CategoryFailover, "loop_retry_wait_timeout", 120*time.Second)
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
			return &failoverApplyError{provider: settingsservice.ProviderCodex, err: fmt.Errorf("Codex 渠道间转移未启用：当前接管渠道无法恢复参与: %w", err)}
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
	a.config.GlobalTimeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryRequest, "global_timeout", a.config.GlobalTimeout)
	// 普通挂起等待超时
	a.config.Failover.SuspendTimeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryRequest, "suspend_wait_timeout", a.config.Failover.SuspendTimeout)
	a.config.Failover.SuspendOnExhausted = a.settingsService.GetBool(ctx, settingsservice.CategoryRequest, "suspend_enabled", a.config.Failover.SuspendOnExhausted)
	a.config.Failover.MaxSuspendedRequests = a.settingsService.GetInt(ctx, settingsservice.CategoryRequest, "max_suspended", a.config.Failover.MaxSuspendedRequests)
	a.config.Failover.EOFRetryHint = a.settingsService.GetBool(ctx, settingsservice.CategoryRequest, "eof_retry_hint", a.config.Failover.EOFRetryHint)
	// allow_retry_on_exhausted 配置已废弃，始终保持连接挂起
	// 注释保留用于向后兼容文档记录

	// 流式传输配置
	a.config.Streaming.HeartbeatInterval = a.settingsService.GetDuration(ctx, settingsservice.CategoryStreaming, "heartbeat_interval", a.config.Streaming.HeartbeatInterval)
	a.config.Streaming.ReadTimeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryStreaming, "read_timeout", a.config.Streaming.ReadTimeout)
	a.config.Streaming.MaxIdleTime = a.settingsService.GetDuration(ctx, settingsservice.CategoryStreaming, "max_idle_time", a.config.Streaming.MaxIdleTime)
	a.config.Streaming.ResponseHeaderTimeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryRequest, "response_header_timeout", a.config.Streaming.ResponseHeaderTimeout)
	a.config.Streaming.DefaultResponseHeaderTimeout = a.settingsService.GetDuration(ctx, settingsservice.CategoryRequest, "default_response_header_timeout", a.config.Streaming.DefaultResponseHeaderTimeout)

	// 访问控制配置
	a.config.Auth.Enabled = a.settingsService.GetBool(ctx, settingsservice.CategoryAuth, "enabled", a.config.Auth.Enabled)
	if token, err := a.settingsService.GetValue(ctx, settingsservice.CategoryAuth, "token"); err == nil {
		a.config.Auth.Token = token
	}

	// Token 计数配置
	a.config.TokenCounting.Enabled = a.settingsService.GetBool(ctx, settingsservice.CategoryTokenCounting, "enabled", a.config.TokenCounting.Enabled)
	a.config.TokenCounting.EstimationRatio = a.settingsService.GetFloat(ctx, settingsservice.CategoryTokenCounting, "estimation_ratio", a.config.TokenCounting.EstimationRatio)

	// 数据保留配置
	a.config.UsageTracking.RetentionDays = a.settingsService.GetInt(ctx, settingsservice.CategoryRetention, "retention_days", a.config.UsageTracking.RetentionDays)
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

	oldLogging := a.config.Logging
	a.config.Logging.Level = a.getSettingString(ctx, settingsservice.CategoryLogging, "level", a.config.Logging.Level)
	a.config.Logging.Format = a.getSettingString(ctx, settingsservice.CategoryLogging, "format", a.config.Logging.Format)
	a.config.Logging.FileEnabled = a.settingsService.GetBool(ctx, settingsservice.CategoryLogging, "file_enabled", a.config.Logging.FileEnabled)
	a.config.Logging.MaxFileSize = a.getSettingString(ctx, settingsservice.CategoryLogging, "max_file_size", a.config.Logging.MaxFileSize)
	a.config.Logging.MaxFiles = a.settingsService.GetInt(ctx, settingsservice.CategoryLogging, "max_files", a.config.Logging.MaxFiles)
	a.config.Logging.CompressRotated = a.settingsService.GetBool(ctx, settingsservice.CategoryLogging, "compress_rotated", a.config.Logging.CompressRotated)
	if a.config.Logging != oldLogging {
		a.applyRuntimeLoggingConfig()
	}

	// 固定使用北京时间，忽略旧数据库中的可选时区设置。
	a.config.Timezone = config.BeijingTimezone

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
	cooldown, ruleDefaults := a.settingsService.RoutingPolicy(ctx)
	a.poolMu.Lock()
	for _, runtime := range a.poolRuntimes {
		runtime.SetCooldownPolicy(cooldown)
	}
	for _, runtime := range a.profileRuntimes {
		runtime.SetRuleDefaults(ruleDefaults)
	}
	a.poolMu.Unlock()
	if a.config.Health.PauseActiveProbes != previousConfig.Health.PauseActiveProbes {
		a.emitEndpointUpdate()
		a.emitCodexProviderStateUpdate()
	}
	return nil
}

func (a *App) getSettingString(ctx context.Context, category, key, defaultVal string) string {
	val, err := a.settingsService.GetValue(ctx, category, key)
	if err != nil || val == "" {
		return defaultVal
	}
	return val
}
