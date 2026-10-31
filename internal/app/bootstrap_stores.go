package app

import (
	"context"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/events"
	pricingservice "github.com/wsyangzy/Vane/internal/pricing/service"
	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexsubscription "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
)

func (a *App) setupEventBus() {
	a.eventBus = events.NewEventBus(a.logger)
	if err := a.eventBus.Start(); err != nil {
		a.logger.Error("事件总线启动失败", "error", err)
	}
}

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
			if rollbackErr := a.rollbackFailoverSetting(settingsservice.ProviderClaude, "Claude 渠道间转移已关闭：启动时从数据库同步端点失败", err); rollbackErr != nil {
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
				if rollbackErr := a.rollbackFailoverSetting(settingsservice.ProviderClaude, "Claude 渠道间转移已关闭：启动时无法同步渠道参与状态", err); rollbackErr != nil {
					a.logger.Error("启动时回滚 Claude 渠道间转移失败", "error", rollbackErr)
				}
			}
		}
		// 同步渠道模型映射到运行时转发层
		a.syncChannelModelMappingsToEndpointManager(ctx)
		if participationReady && a.config != nil && a.config.Failover.Enabled {
			resumed, resumeErr := a.endpointService.EnsureActiveChannelParticipates(ctx, a.channelService)
			if resumeErr != nil {
				if rollbackErr := a.rollbackFailoverSetting(settingsservice.ProviderClaude, "Claude 渠道间转移未启用：启动时无法恢复当前接管渠道参与", resumeErr); rollbackErr != nil {
					a.logger.Error("启动时回滚 Claude 渠道间转移失败", "error", rollbackErr)
				}
			} else if resumed {
				a.logger.Info("✅ Claude 当前接管渠道已自动恢复参与渠道间转移")
			}
		}
		a.logger.Info("✅ 端点存储已启用 (SQLite)")
	}
}

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
	if changed, err := a.modelPricingService.BackfillOfficialPrices(ctx); err != nil {
		a.logger.Error("补齐 OpenAI 官方价格失败", "error", err)
	} else if changed > 0 {
		a.logger.Info("已补齐 OpenAI 官方价格", "rows", changed)
	}

	// 同步定价到 UsageTracker（用于成本计算）
	a.syncPricingToTracker(ctx)

	a.logger.Info("✅ 模型定价存储已启用 (SQLite)", "count", count)
}

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

	modelPricingStore := pricingstore.NewSQLiteModelPricingStore(db)
	modelPricingService := pricingservice.NewModelPricingService(modelPricingStore)

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
			if rollbackErr := a.rollbackFailoverSetting(settingsservice.ProviderCodex, "Codex 渠道间转移已关闭：启动时初始化数据表失败", err); rollbackErr != nil {
				a.logger.Error("启动时回滚 Codex 渠道间转移失败", "error", rollbackErr)
			}
		}
		return
	}

	channels, err := a.codexChannelService.ListChannels(ctx)
	if err != nil {
		a.logger.Warn("⚠️ 加载 Codex 渠道列表失败", "error", err)
		if a.config != nil && a.config.CodexFailover.Enabled {
			if rollbackErr := a.rollbackFailoverSetting(settingsservice.ProviderCodex, "Codex 渠道间转移已关闭：启动时加载渠道失败", err); rollbackErr != nil {
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
