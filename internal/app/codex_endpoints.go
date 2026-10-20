package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func (a *App) reloadCodexEndpointManager() error {
	a.mu.RLock()
	manager := a.codexEndpointMgr
	a.mu.RUnlock()
	if manager == nil {
		return nil
	}
	return manager.Reload()
}

func (a *App) CreateCodexEndpoint(input CreateCodexEndpointInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	priority := -1
	if input.Priority != nil {
		priority = *input.Priority
	}

	record := &codexstore.CodexEndpointRecord{
		ChannelID:                input.ChannelID,
		CredentialGroupID:        input.CredentialGroupID,
		ApiKey:                   input.ApiKey,
		Name:                     input.Name,
		BaseURL:                  input.BaseURL,
		FailoverEnabled:          input.FailoverEnabled == nil || *input.FailoverEnabled,
		SkipHealthCheck:          input.SkipHealthCheck,
		Priority:                 priority,
		TimeoutSeconds:           input.TimeoutSeconds,
		CostMultiplier:           input.CostMultiplier,
		InputCostMultiplier:      input.InputCostMultiplier,
		OutputCostMultiplier:     input.OutputCostMultiplier,
		CacheReadCostMultiplier:  input.CacheReadCostMultiplier,
		BillingOutputTokenSource: tracking.NormalizeBillingOutputTokenSource(input.BillingOutputTokenSource),
		ModelMultipliers:         input.ModelMultipliers,
	}

	// 默认值：costMultiplier 为 0 时视为 1.0
	if record.CostMultiplier == 0 {
		record.CostMultiplier = 1.0
	}
	if record.InputCostMultiplier == 0 {
		record.InputCostMultiplier = 1.0
	}
	if record.OutputCostMultiplier == 0 {
		record.OutputCostMultiplier = 1.0
	}
	if record.CacheReadCostMultiplier == 0 {
		record.CacheReadCostMultiplier = 1.0
	}

	// Keep the pre-create priority occupant so a later config/runtime failure
	// can restore the exact ordering after the service has moved it aside.
	var prioritySnapshots []*codexstore.CodexEndpointRecord
	if input.Priority != nil && *input.Priority >= 0 {
		snapshots, snapshotErr := endpointService.ListByChannelID(ctx, input.ChannelID)
		if snapshotErr != nil {
			return fmt.Errorf("创建 Codex 端点失败: 读取原端点优先级失败: %w", snapshotErr)
		}
		for _, snapshot := range snapshots {
			if snapshot == nil {
				continue
			}
			copy := *snapshot
			prioritySnapshots = append(prioritySnapshots, &copy)
		}
	}

	created, err := endpointService.CreateEndpoint(ctx, record)
	if err != nil {
		return fmt.Errorf("创建 Codex 端点失败: %w", err)
	}

	rollbackCreated := func(cause error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		createdDeleted := false
		if created != nil {
			if err := endpointService.DeleteEndpointByID(rollbackCtx, created.ID); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("删除已创建端点失败: %v", err))
			} else {
				createdDeleted = true
			}
		}
		if createdDeleted {
			for _, snapshot := range prioritySnapshots {
				if snapshot.ChannelID != created.ChannelID || snapshot.CredentialGroupID != created.CredentialGroupID || snapshot.Priority != created.Priority {
					continue
				}
				if err := endpointService.RestoreEndpoint(rollbackCtx, snapshot); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原端点优先级失败: %v", err))
				}
				break
			}
		}
		if reloadErr := a.reloadCodexEndpointManager(); reloadErr != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 端点失败: %v", reloadErr))
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}

	if err := a.reloadCodexEndpointManager(); err != nil {
		return rollbackCreated(fmt.Errorf("创建 Codex 端点失败: 运行时未生效: %w", err))
	}

	// 若该渠道当前激活 + 新端点被自动激活（首端点） → 同步 config.toml
	channel, channelErr := channelService.GetChannelByID(ctx, input.ChannelID)
	if created != nil && created.IsActive && channel != nil && channel.IsActive {
		if syncErr := a.syncActiveCodexChannelToConfig(channel, created, ""); syncErr != nil {
			return rollbackCreated(fmt.Errorf("创建 Codex 端点失败: 配置同步未生效: %w", syncErr))
		}
	} else if created != nil && created.IsActive && channelErr != nil {
		return rollbackCreated(fmt.Errorf("创建 Codex 端点失败: 读取渠道失败: %w", channelErr))
	}

	if logger != nil {
		logger.Info("✅ Codex 端点已创建", "channel_id", input.ChannelID, "base_url", input.BaseURL)
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return nil
}

func (a *App) UpdateCodexEndpointByID(id int64, input UpdateCodexEndpointInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}
	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previous, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取 Codex 端点失败: %w", err)
	}
	if previous == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	failoverEnabled := previous.FailoverEnabled
	if input.FailoverEnabled != nil {
		failoverEnabled = *input.FailoverEnabled
	}
	skipHealthCheck := previous.SkipHealthCheck
	if input.SkipHealthCheck != nil {
		skipHealthCheck = *input.SkipHealthCheck
	}
	priority := previous.Priority
	if input.Priority != nil {
		priority = *input.Priority
	}
	timeoutSeconds := previous.TimeoutSeconds
	if input.TimeoutSeconds != nil {
		timeoutSeconds = *input.TimeoutSeconds
	}
	costMultiplier := previous.CostMultiplier
	if input.CostMultiplier != nil {
		costMultiplier = *input.CostMultiplier
	}
	inputCostMultiplier := previous.InputCostMultiplier
	if input.InputCostMultiplier != nil {
		inputCostMultiplier = *input.InputCostMultiplier
	}
	outputCostMultiplier := previous.OutputCostMultiplier
	if input.OutputCostMultiplier != nil {
		outputCostMultiplier = *input.OutputCostMultiplier
	}
	cacheReadCostMultiplier := previous.CacheReadCostMultiplier
	if input.CacheReadCostMultiplier != nil {
		cacheReadCostMultiplier = *input.CacheReadCostMultiplier
	}
	billingOutputTokenSource := previous.BillingOutputTokenSource
	if input.BillingOutputTokenSource != nil {
		billingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(*input.BillingOutputTokenSource)
	}
	modelMultipliers := previous.ModelMultipliers
	if input.ModelMultipliers != nil {
		modelMultipliers = input.ModelMultipliers
	}
	credentialGroupID := previous.CredentialGroupID
	if input.CredentialGroupID != nil {
		credentialGroupID = *input.CredentialGroupID
	}
	apiKey := previous.ApiKey
	if input.ApiKey != nil {
		apiKey = *input.ApiKey
	}

	// Snapshot a same-group priority occupant before the service performs its
	// swap. If config/runtime synchronization fails, both records can be put
	// back exactly as they were.
	var priorityConflict *codexstore.CodexEndpointRecord
	if input.Priority != nil && *input.Priority >= 0 && *input.Priority != previous.Priority && credentialGroupID == previous.CredentialGroupID {
		siblings, listErr := endpointService.ListByChannelID(ctx, previous.ChannelID)
		if listErr != nil {
			return fmt.Errorf("更新 Codex 端点失败: 读取原端点优先级失败: %w", listErr)
		}
		for _, sibling := range siblings {
			if sibling != nil && sibling.ID != id && sibling.CredentialGroupID == credentialGroupID && sibling.Priority == *input.Priority {
				copy := *sibling
				priorityConflict = &copy
				break
			}
		}
	}

	restoreUpdated := func(cause error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		if err := endpointService.RestoreEndpoint(rollbackCtx, previous); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原端点记录失败: %v", err))
		}
		if priorityConflict != nil {
			if err := endpointService.RestoreEndpoint(rollbackCtx, priorityConflict); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复冲突端点优先级失败: %v", err))
			}
		}
		if err := a.reloadCodexEndpointManager(); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 端点失败: %v", err))
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}

	if err := endpointService.UpdateEndpointByID(ctx, id, input.Name, input.BaseURL, credentialGroupID, apiKey, failoverEnabled, skipHealthCheck, priority, timeoutSeconds, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier, billingOutputTokenSource, modelMultipliers); err != nil {
		return restoreUpdated(fmt.Errorf("更新 Codex 端点失败: %w", err))
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return restoreUpdated(fmt.Errorf("更新 Codex 端点失败: 运行时未生效: %w", err))
	}

	// 配置同步策略（2026-05-14 扩大同步范围）：
	// 只要被编辑的端点所属渠道是「当前激活渠道」，就重新同步 config.toml。
	// - 编辑激活端点的 base_url → toml 中的 base_url 行被改写
	// - 编辑非激活端点 → 用当前激活端点的 base_url 重写（toml 内容通常不变，但保证 DB↔toml 一致）
	// - 编辑端点 name → 不影响 toml（name 不出现在 toml 中），但同步动作幂等无害
	channel, err := channelService.GetChannelByID(ctx, previous.ChannelID)
	if err != nil {
		return restoreUpdated(fmt.Errorf("更新 Codex 端点失败: 读取所属渠道失败: %w", err))
	}
	if channel != nil && channel.IsActive {
		// 取该渠道下的当前激活端点（可能就是刚编辑的，也可能是另一个）
		activeEndpoint, epErr := endpointService.GetActiveByChannelID(ctx, channel.ID)
		if epErr != nil {
			return restoreUpdated(fmt.Errorf("更新 Codex 端点失败: 读取激活端点失败: %w", epErr))
		}
		if activeEndpoint != nil {
			if syncErr := a.syncActiveCodexChannelToConfig(channel, activeEndpoint, ""); syncErr != nil {
				return restoreUpdated(fmt.Errorf("更新 Codex 端点失败: 配置同步未生效: %w", syncErr))
			}
			if logger != nil {
				logger.Info("✅ Codex 端点已更新并同步 config.toml",
					"id", id, "name", input.Name, "base_url", input.BaseURL,
					"channel", channel.Name, "active_endpoint_url", activeEndpoint.BaseURL)
			}
		} else if logger != nil {
			logger.Warn("⚠️ Codex 渠道无激活端点，config.toml 未更新", "channel", channel.Name)
		}
	} else if logger != nil {
		logger.Info("ℹ️ Codex 端点已更新（所属渠道未激活，config.toml 未变化）",
			"id", id, "name", input.Name, "base_url", input.BaseURL)
	}

	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return nil
}

func (a *App) DeleteCodexEndpointByID(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}
	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	target, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取待删除的 Codex 端点失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	wasActive := target.IsActive
	channelID := target.ChannelID
	channel, err := channelService.GetChannelByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("读取所属 Codex 渠道失败: %w", err)
	}
	if channel == nil {
		return fmt.Errorf("所属 Codex 渠道不存在: %d", channelID)
	}
	runtimeAffected := channel.IsActive
	var nextEndpoint *codexstore.CodexEndpointRecord
	if wasActive {
		nextEndpoint, err = endpointService.GetEndpointDeletionSuccessor(ctx, id)
		if err != nil {
			return fmt.Errorf("读取删除后的候选 Codex 端点失败: %w", err)
		}
	}
	configSynced := false
	prepared := false
	rollbackState := func(cause error, restoreRecord bool, deleted bool) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		routeRestored := !restoreRecord
		if restoreRecord {
			var restoreErr error
			if deleted {
				restoreErr = endpointService.RestoreDeletedEndpoint(rollbackCtx, target)
			} else {
				restoreErr = endpointService.RestoreEndpoint(rollbackCtx, target)
			}
			if restoreErr != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原端点记录失败: %v", restoreErr))
			} else {
				routeRestored = true
			}
		}
		if prepared {
			if err := a.reloadCodexEndpointManager(); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 运行时失败: %v", err))
				routeRestored = false
			}
		}
		if configSynced && routeRestored {
			if err := a.syncActiveCodexChannelToConfig(channel, target, ""); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原 Codex 配置失败: %v", err))
			}
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}

	if runtimeAffected && wasActive {
		var syncErr error
		if nextEndpoint != nil {
			syncErr = a.syncActiveCodexChannelToConfig(channel, nextEndpoint, "")
		} else {
			syncErr = a.removeCodexChannelConfig(channel.Name, nil, nil)
		}
		if syncErr != nil {
			return fmt.Errorf("删除 Codex 端点失败: 配置同步未生效: %w", syncErr)
		}
		configSynced = true
	}

	if runtimeAffected {
		nextEndpointID := int64(0)
		if nextEndpoint != nil {
			nextEndpointID = nextEndpoint.ID
		}
		if err := endpointService.PrepareEndpointDeletion(ctx, id, nextEndpointID); err != nil {
			return rollbackState(fmt.Errorf("删除 Codex 端点失败: 准备运行时状态失败: %w", err), false, false)
		}
		prepared = true
		if err := a.reloadCodexEndpointManager(); err != nil {
			return rollbackState(fmt.Errorf("删除 Codex 端点失败: 排除待删除端点未生效: %w", err), true, false)
		}
	}

	if err := endpointService.DeleteEndpointByID(ctx, id); err != nil {
		return rollbackState(fmt.Errorf("删除 Codex 端点失败: %w", err), prepared, false)
	}
	if runtimeAffected {
		if err := a.reloadCodexEndpointManager(); err != nil {
			return rollbackState(fmt.Errorf("删除 Codex 端点失败: 运行时未生效: %w", err), true, true)
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 端点已删除", "id", id)
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return nil
}

func (a *App) ActivateCodexEndpoint(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	target, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取目标端点失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	wasActive := target.IsActive
	previousActive, err := endpointService.GetActiveByChannelID(ctx, target.ChannelID)
	if err != nil {
		return fmt.Errorf("读取当前激活端点失败: %w", err)
	}
	restoreEndpoint := func(cause error) error {
		if wasActive || previousActive == nil {
			return cause
		}
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		if err := endpointService.ActivateEndpoint(rollbackCtx, previousActive.ID); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原激活端点失败: %v", err))
		}
		if err := a.reloadCodexEndpointManager(); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 运行时失败: %v", err))
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}
	if !target.IsActive {
		if err := endpointService.ActivateEndpoint(ctx, id); err != nil {
			return fmt.Errorf("激活 Codex 端点失败: %w", err)
		}
		if err := a.reloadCodexEndpointManager(); err != nil {
			return restoreEndpoint(fmt.Errorf("激活 Codex 端点失败: 运行时未生效: %w", err))
		}
	}

	// 仅当端点所属渠道为当前激活渠道时才改写 config.toml（KISS：核心规则之一）
	channel, err := channelService.GetChannelByID(ctx, target.ChannelID)
	if err != nil {
		return restoreEndpoint(fmt.Errorf("激活 Codex 端点失败: 读取所属渠道失败: %w", err))
	}
	if channel != nil && channel.IsActive {
		if syncErr := a.syncActiveCodexChannelToConfig(channel, target, ""); syncErr != nil {
			return restoreEndpoint(fmt.Errorf("激活 Codex 端点失败: 配置同步未生效: %w", syncErr))
		}
		if logger != nil {
			logger.Info("✅ Codex 端点已激活并同步", "id", id, "base_url", target.BaseURL)
		}
	} else if logger != nil {
		logger.Info("ℹ️ Codex 端点已激活（渠道未激活，config.toml 暂未变化）", "id", id, "base_url", target.BaseURL)
	}

	a.emitCodexProviderUpdate()
	// 唤醒所有挂起请求（运维通过 UI 切换端点 = 恢复信号，已激活端点的重复点击同样有效）
	a.broadcastCodexRecovery()
	return nil
}
