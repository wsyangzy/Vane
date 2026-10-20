package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

func (a *App) GetCodexChannels() ([]CodexChannelInfo, error) {
	a.ensureCodexProviderService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	credentialGroupService := a.codexCredentialGroupService
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return nil, fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	channels, err := channelService.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 渠道列表失败: %w", err)
	}

	result := make([]CodexChannelInfo, 0, len(channels))
	// 只有存在非流改道配置时才需要目标目录来解析目标名称
	var routeCatalog routing.Catalog
	for _, ch := range channels {
		if ch != nil && ch.NonStreamRoute != nil {
			routeCatalog, _ = a.routeCatalog(ctx, "codex")
			break
		}
	}
	for _, ch := range channels {
		endpoints, err := endpointService.ListByChannelID(ctx, ch.ID)
		if err != nil {
			return nil, fmt.Errorf("获取渠道 '%s' 端点列表失败: %w", ch.Name, err)
		}
		var credentialGroups []*codexstore.CodexCredentialGroupRecord
		if credentialGroupService != nil {
			credentialGroups, err = credentialGroupService.ListByChannelID(ctx, ch.ID)
			if err != nil {
				return nil, fmt.Errorf("获取渠道 '%s' 凭证分组列表失败: %w", ch.Name, err)
			}
		}
		info := a.buildCodexChannelInfo(ch, endpoints, credentialGroups)
		info.NonStreamRoute = nonStreamRouteInfo(ch.NonStreamRoute, routeCatalog)
		info.PrivacyLevel = ch.PrivacyLevel
		result = append(result, info)
	}
	return result, nil
}

func (a *App) GetNextAvailableCodexChannel(excludeID int64) (CodexChannelCandidateInfo, error) {
	a.ensureCodexProviderService()
	a.mu.RLock()
	channelService := a.codexChannelService
	a.mu.RUnlock()
	if channelService == nil {
		return CodexChannelCandidateInfo{}, fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	next, err := channelService.GetNextFailoverChannel(ctx, excludeID)
	if err != nil {
		return CodexChannelCandidateInfo{}, err
	}
	return CodexChannelCandidateInfo{ID: next.ID, Name: next.Name}, nil
}

func (a *App) CreateCodexChannel(input CreateCodexChannelInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	credentialGroupService := a.codexCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}
	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	record := codexChannelInputToRecord(input.Name, input.ProviderType, input.ApiKey, input.Model, input.ModelMapping,
		input.ReasoningEffort, input.Website, input.Enable1MContext)
	record.Priority = input.Priority
	record.FailoverEnabled = input.FailoverEnabled == nil || *input.FailoverEnabled

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	previousChannels, err := channelService.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("创建 Codex 渠道失败: 读取原渠道状态失败: %w", err)
	}
	channelSnapshots := make([]codexstore.CodexChannelRecord, 0, len(previousChannels))
	for _, channel := range previousChannels {
		if channel != nil {
			channelSnapshots = append(channelSnapshots, *channel)
		}
	}

	created, err := channelService.CreateChannel(ctx, record)
	if err != nil {
		return fmt.Errorf("创建 Codex 渠道失败: %w", err)
	}
	rollbackCreated := func(cause error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		if err := channelService.DeleteChannelByID(rollbackCtx, created.ID); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("删除已创建渠道失败: %v", err))
		} else {
			for index := range channelSnapshots {
				if err := channelService.RestoreChannel(rollbackCtx, &channelSnapshots[index]); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原渠道状态失败: %v", err))
				}
			}
		}
		if err := a.reloadCodexEndpointManager(); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 运行时失败: %v", err))
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}
	if credentialGroupService != nil {
		if _, err := credentialGroupService.EnsureDefaultGroup(ctx, created); err != nil {
			return rollbackCreated(fmt.Errorf("创建 Codex 默认凭证分组失败: %w", err))
		}
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return rollbackCreated(fmt.Errorf("创建 Codex 渠道失败: 运行时未生效: %w", err))
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已创建", "name", created.Name, "is_active", created.IsActive)
	}
	a.emitCodexProviderUpdate()
	return nil
}

func (a *App) UpdateCodexChannelByID(id int64, input UpdateCodexChannelInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	credentialGroupService := a.codexCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}
	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	record := codexChannelInputToRecord(input.Name, input.ProviderType, input.ApiKey, input.Model, input.ModelMapping,
		input.ReasoningEffort, input.Website, input.Enable1MContext)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previous, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取 Codex 渠道失败: %w", err)
	}
	if previous == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	record.Priority = input.Priority
	if input.FailoverEnabled == nil {
		record.FailoverEnabled = previous.FailoverEnabled
	} else {
		record.FailoverEnabled = *input.FailoverEnabled
	}
	var previousDefaultGroup *codexstore.CodexCredentialGroupRecord
	var previousPriorityConflict *codexstore.CodexChannelRecord
	if record.Priority != previous.Priority {
		channels, listErr := channelService.ListChannels(ctx)
		if listErr != nil {
			return fmt.Errorf("更新 Codex 渠道失败: 读取原渠道优先级失败: %w", listErr)
		}
		for _, channel := range channels {
			if channel != nil && channel.ID != id && channel.Priority == record.Priority {
				copy := *channel
				previousPriorityConflict = &copy
				break
			}
		}
	}

	rollbackChannel := func(cause error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		if previousDefaultGroup != nil && credentialGroupService != nil {
			if err := credentialGroupService.UpdateGroup(rollbackCtx, previousDefaultGroup.ID, previousDefaultGroup); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复默认凭证分组失败: %v", err))
			}
		}
		if err := channelService.RestoreChannel(rollbackCtx, previous); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复渠道记录失败: %v", err))
		}
		if previousPriorityConflict != nil {
			if err := channelService.RestoreChannel(rollbackCtx, previousPriorityConflict); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复冲突渠道优先级失败: %v", err))
			}
		}
		if err := a.reloadCodexEndpointManager(); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 运行时失败: %v", err))
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}

	if err := channelService.UpdateChannelByID(ctx, id, record); err != nil {
		return fmt.Errorf("更新 Codex 渠道失败: %w", err)
	}
	if credentialGroupService != nil && record.ApiKey != "" {
		defaultGroup, groupErr := credentialGroupService.GetDefaultByChannelID(ctx, id)
		if groupErr != nil {
			return rollbackChannel(fmt.Errorf("读取 Codex 默认凭证分组失败: %w", groupErr))
		}
		if defaultGroup != nil && defaultGroup.AuthMode != subscription.ClientPassthrough && defaultGroup.ApiKey == previous.ApiKey {
			groupCopy := *defaultGroup
			previousDefaultGroup = &groupCopy
			defaultGroup.ApiKey = record.ApiKey
			if err := credentialGroupService.UpdateGroup(ctx, defaultGroup.ID, defaultGroup); err != nil {
				return rollbackChannel(fmt.Errorf("同步默认凭证分组失败: %w", err))
			}
		}
	}

	updated, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return rollbackChannel(fmt.Errorf("读取更新后的 Codex 渠道失败: %w", err))
	}
	if updated == nil {
		return rollbackChannel(fmt.Errorf("读取更新后的 Codex 渠道失败: 渠道不存在: %d", id))
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return rollbackChannel(fmt.Errorf("更新 Codex 渠道失败: 运行时未生效: %w", err))
	}

	if updated.IsActive {
		// 配置同步：把更新后的渠道+激活端点写回 config.toml
		activeEndpoint, endpointErr := endpointService.GetActiveByChannelID(ctx, updated.ID)
		if endpointErr != nil {
			return rollbackChannel(fmt.Errorf("读取激活端点失败: %w", endpointErr))
		}
		if activeEndpoint != nil {
			if syncErr := a.syncActiveCodexChannelToConfig(updated, activeEndpoint, previous.Name); syncErr != nil {
				return rollbackChannel(fmt.Errorf("更新 Codex 渠道失败: 配置接管未生效: %w", syncErr))
			}
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已更新", "id", id, "name", record.Name)
	}
	a.emitCodexProviderUpdate()
	return nil
}

func (a *App) SetCodexChannelFailoverEnabled(id int64, enabled bool) error {
	a.ensureCodexProviderService()
	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}
	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取 Codex 渠道失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	previousEndpoint, err := endpointService.GetActiveByChannelID(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("读取当前 Codex 激活端点失败: %w", err)
	}
	if enabled || !target.IsActive {
		if err := channelService.SetFailoverEnabled(ctx, id, enabled); err != nil {
			return fmt.Errorf("更新 Codex 渠道故障转移状态失败: %w", err)
		}
		a.emitCodexProviderUpdate()
		return nil
	}

	next, err := channelService.GetNextFailoverChannel(ctx, id)
	if err != nil {
		return fmt.Errorf("暂停当前接管渠道失败: %w", err)
	}
	nextEndpoint, err := channelService.EnsureActiveEndpoint(ctx, next.ID, true)
	if err != nil {
		return fmt.Errorf("准备后续 Codex 渠道端点失败: %w", err)
	}
	if nextEndpoint == nil {
		return fmt.Errorf("暂停当前接管渠道失败: 候选渠道 '%s' 没有参与故障转移的端点", next.Name)
	}
	rollback := func(cause error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		if err := channelService.SetFailoverEnabled(rollbackCtx, target.ID, true); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原渠道参与状态失败: %v", err))
		}
		if err := channelService.ActivateChannel(rollbackCtx, target.ID, false); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原激活渠道失败: %v", err))
		}
		if err := a.reloadCodexEndpointManager(); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 运行时失败: %v", err))
		}
		if previousEndpoint != nil {
			if err := a.syncActiveCodexChannelToConfig(target, previousEndpoint, next.Name); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原 Codex 配置失败: %v", err))
			}
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}

	if err := channelService.ActivateChannel(ctx, next.ID, true); err != nil {
		return fmt.Errorf("切换到后续 Codex 渠道失败: %w", err)
	}
	if err := channelService.SetFailoverEnabled(ctx, target.ID, false); err != nil {
		return rollback(fmt.Errorf("暂停原 Codex 渠道失败: %w", err))
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return rollback(fmt.Errorf("切换 Codex 运行时渠道失败: %w", err))
	}
	if err := a.syncActiveCodexChannelToConfig(next, nextEndpoint, target.Name); err != nil {
		return rollback(fmt.Errorf("切换 Codex 配置接管失败: %w", err))
	}
	if logger != nil {
		logger.Info("Codex 活跃渠道已暂停并自动切换", "paused_channel", target.Name, "active_channel", next.Name)
	}
	a.emitCodexProviderUpdate()
	a.broadcastCodexRecovery()
	return nil
}

func (a *App) DeleteCodexChannelByID(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}
	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	target, err := channelService.GetChannelByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取待删除的 Codex 渠道失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}

	var previousEndpoint *codexstore.CodexEndpointRecord
	var nextChannel *codexstore.CodexChannelRecord
	var nextEndpoint *codexstore.CodexEndpointRecord
	var nextPreviousEndpoint *codexstore.CodexEndpointRecord
	configSynced := false
	prepared := false
	rollbackPrepared := func(cause error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		var rollbackErrs []string
		routeRestored := !prepared
		if prepared {
			routeRestored = true
			if err := channelService.SetFailoverEnabled(rollbackCtx, target.ID, target.FailoverEnabled); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原渠道参与状态失败: %v", err))
				routeRestored = false
			}
			if routeRestored {
				if err := channelService.ActivateChannel(rollbackCtx, target.ID, false); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原激活渠道失败: %v", err))
					routeRestored = false
				}
			}
			if routeRestored && nextChannel != nil {
				if nextPreviousEndpoint != nil {
					if err := endpointService.RestoreEndpoint(rollbackCtx, nextPreviousEndpoint); err != nil {
						rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复后续渠道原激活端点失败: %v", err))
					}
				} else if err := endpointService.ClearActiveByChannel(rollbackCtx, nextChannel.ID); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Sprintf("清空后续渠道临时激活端点失败: %v", err))
				}
			}
			if err := a.reloadCodexEndpointManager(); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 运行时失败: %v", err))
				routeRestored = false
			}
		}
		if configSynced && previousEndpoint != nil && routeRestored {
			if err := a.syncActiveCodexChannelToConfig(target, previousEndpoint, ""); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原 Codex 配置失败: %v", err))
			}
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
		}
		return cause
	}
	if target.IsActive {
		previousEndpoint, err = endpointService.GetActiveByChannelID(ctx, target.ID)
		if err != nil {
			return fmt.Errorf("读取当前 Codex 激活端点失败: %w", err)
		}
		nextChannel, nextEndpoint, err = channelService.GetChannelDeletionSuccessor(ctx, id)
		if err != nil {
			return fmt.Errorf("读取删除后的候选 Codex 渠道失败: %w", err)
		}
		if nextChannel != nil {
			nextPreviousEndpoint, err = endpointService.GetActiveByChannelID(ctx, nextChannel.ID)
			if err != nil {
				return fmt.Errorf("读取后续 Codex 原激活端点失败: %w", err)
			}
		}
		if err := a.removeCodexChannelConfig(target.Name, nextChannel, nextEndpoint); err != nil {
			return fmt.Errorf("删除 Codex 渠道失败: 配置同步未生效: %w", err)
		}
		configSynced = true
		nextChannelID := int64(0)
		nextEndpointID := int64(0)
		if nextChannel != nil {
			nextChannelID = nextChannel.ID
		}
		if nextEndpoint != nil {
			nextEndpointID = nextEndpoint.ID
		}
		if err := channelService.PrepareChannelDeletion(ctx, id, nextChannelID, nextEndpointID); err != nil {
			return rollbackPrepared(fmt.Errorf("删除 Codex 渠道失败: 准备后续接管状态失败: %w", err))
		}
		prepared = true
		if err := a.reloadCodexEndpointManager(); err != nil {
			return rollbackPrepared(fmt.Errorf("删除 Codex 渠道失败: 后续运行时未生效: %w", err))
		}
	}

	if err := channelService.DeleteChannelByID(ctx, id); err != nil {
		return rollbackPrepared(fmt.Errorf("删除 Codex 渠道失败: %w", err))
	}
	a.deleteChannelModelRoutes(ctx, "codex", id)
	a.syncCodexModelRules(ctx)
	if err := a.reloadCodexEndpointManager(); err != nil && logger != nil {
		logger.Warn("⚠️ Codex 渠道删除后重新加载运行时失败", "id", id, "error", err)
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已删除", "id", id)
	}
	a.emitCodexProviderUpdate()
	return nil
}

func (a *App) ActivateCodexChannel(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	endpointService := a.codexEndpointService
	logger := a.logger
	requireFailoverEnabled := a.config != nil && a.config.CodexFailover.Enabled
	a.mu.RUnlock()
	if channelService == nil || endpointService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previousActive, err := channelService.GetActiveChannel(ctx)
	if err != nil {
		return fmt.Errorf("读取当前激活渠道失败: %w", err)
	}
	var previousEndpoint *codexstore.CodexEndpointRecord
	if previousActive != nil && previousActive.ID != id {
		previousEndpoint, err = endpointService.GetActiveByChannelID(ctx, previousActive.ID)
		if err != nil {
			return fmt.Errorf("读取当前 Codex 激活端点失败: %w", err)
		}
	}

	activated := previousActive
	if previousActive == nil || previousActive.ID != id {
		if err := channelService.ActivateChannel(ctx, id, requireFailoverEnabled); err != nil {
			return fmt.Errorf("切换 Codex 渠道失败: %w", err)
		}

		activated, err = channelService.GetChannelByID(ctx, id)
		if err != nil {
			return fmt.Errorf("读取激活后的渠道失败: %w", err)
		}
	}
	if activated == nil {
		return fmt.Errorf("激活后的渠道不存在: %d", id)
	}
	if requireFailoverEnabled && !activated.FailoverEnabled {
		return fmt.Errorf("切换 Codex 渠道失败: 渠道 '%s' 已暂停，请先恢复渠道间故障转移", activated.Name)
	}

	endpoint, err := channelService.EnsureActiveEndpoint(ctx, activated.ID, requireFailoverEnabled)
	if err != nil {
		return fmt.Errorf("确保激活端点失败: %w", err)
	}
	if endpoint == nil && requireFailoverEnabled {
		if previousActive != nil && previousActive.ID != id {
			if restoreErr := channelService.ActivateChannel(ctx, previousActive.ID, false); restoreErr != nil {
				return fmt.Errorf("切换 Codex 渠道失败: 渠道 '%s' 没有参与故障转移的端点；恢复原激活渠道失败: %v", activated.Name, restoreErr)
			}
		}
		return fmt.Errorf("切换 Codex 渠道失败: 渠道 '%s' 没有参与故障转移的端点", activated.Name)
	}
	if endpoint == nil {
		// 渠道下没有任何端点，仍允许激活但提示用户
		if logger != nil {
			logger.Warn("⚠️ 渠道已激活但无可用端点，config.toml 暂未更新", "channel", activated.Name)
		}
		a.emitCodexProviderUpdate()
		return nil
	}

	previousName := ""
	if previousActive != nil && previousActive.ID != id {
		previousName = previousActive.Name
	}

	rollback := func(cause error) error {
		if previousActive != nil && previousActive.ID != id {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer rollbackCancel()
			var rollbackErrs []string
			if err := channelService.ActivateChannel(rollbackCtx, previousActive.ID, false); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原激活渠道失败: %v", err))
			}
			if err := a.reloadCodexEndpointManager(); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Sprintf("重新加载 Codex 运行时失败: %v", err))
			}
			if previousEndpoint != nil {
				if err := a.syncActiveCodexChannelToConfig(previousActive, previousEndpoint, activated.Name); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Sprintf("恢复原 Codex 配置失败: %v", err))
				}
			}
			if len(rollbackErrs) > 0 {
				return fmt.Errorf("%w；回滚失败: %s", cause, strings.Join(rollbackErrs, "; "))
			}
		}
		return cause
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return rollback(fmt.Errorf("切换 Codex 渠道失败: 运行时未生效: %w", err))
	}
	if err := a.syncActiveCodexChannelToConfig(activated, endpoint, previousName); err != nil {
		return rollback(fmt.Errorf("切换 Codex 渠道失败: 配置接管未生效: %w", err))
	}

	if logger != nil {
		logger.Info("✅ Codex 渠道已激活", "id", id, "channel", activated.Name, "base_url", endpoint.BaseURL)
	}
	a.emitCodexProviderUpdate()
	// 唤醒所有挂起请求（运维通过 UI 切换渠道 = 恢复信号）
	a.broadcastCodexRecovery()
	return nil
}
