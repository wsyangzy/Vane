package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
)

func (a *App) CreateCodexCredentialGroup(input CreateCodexCredentialGroupInput) (CodexCredentialGroupInfo, error) {
	a.ensureCodexProviderService()

	a.mu.RLock()
	credentialGroupService := a.codexCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()
	if credentialGroupService == nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("Codex 凭证分组服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record := codexCredentialGroupInputToRecord(input.ChannelID, input.Name, input.ApiKey, input.IsDefault, input.DailyCostLimitUSD, input.ModelPolicy, input.ModelMapping, input.SupportedModels, input.CostMultiplier, input.InputCostMultiplier, input.OutputCostMultiplier, input.CacheCreationCostMultiplier, input.CacheReadCostMultiplier, input.LongContextCostMultiplier, input.LongContextInputCostMultiplier, input.LongContextOutputCostMultiplier, input.LongContextCacheCreationCostMultiplier, input.LongContextCacheReadCostMultiplier, input.DisableLongContextPricing, input.BillingOutputTokenSource, input.ModelMultipliers, input.BillingRules)
	record.ServiceTierSource = input.ServiceTierSource
	record.CacheWriteTTLPricing = input.CacheWriteTTLPricing
	record.AuthMode = input.AuthMode
	record.Subscription = input.Subscription
	created, err := credentialGroupService.CreateGroup(ctx, record)
	if err != nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("创建 Codex 凭证分组失败: %w", err)
	}
	if logger != nil {
		logger.Info("✅ Codex 凭证分组已创建", "channel_id", created.ChannelID, "name", created.Name, "is_default", created.IsDefault)
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return CodexCredentialGroupInfo{}, err
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return buildCodexCredentialGroupInfo(created), nil
}

func (a *App) UpdateCodexCredentialGroupByID(id int64, input UpdateCodexCredentialGroupInput) (CodexCredentialGroupInfo, error) {
	a.ensureCodexProviderService()

	a.mu.RLock()
	credentialGroupService := a.codexCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()
	if credentialGroupService == nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("Codex 凭证分组服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	existing, err := credentialGroupService.GetByID(ctx, id)
	if err != nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("读取 Codex 凭证分组失败: %w", err)
	}
	if existing == nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("Codex 凭证分组不存在: %d", id)
	}
	record := codexCredentialGroupInputToRecord(existing.ChannelID, input.Name, input.ApiKey, input.IsDefault, input.DailyCostLimitUSD, input.ModelPolicy, input.ModelMapping, input.SupportedModels, input.CostMultiplier, input.InputCostMultiplier, input.OutputCostMultiplier, input.CacheCreationCostMultiplier, input.CacheReadCostMultiplier, input.LongContextCostMultiplier, input.LongContextInputCostMultiplier, input.LongContextOutputCostMultiplier, input.LongContextCacheCreationCostMultiplier, input.LongContextCacheReadCostMultiplier, input.DisableLongContextPricing, input.BillingOutputTokenSource, input.ModelMultipliers, input.BillingRules)
	record.ServiceTierSource = input.ServiceTierSource
	record.CacheWriteTTLPricing = input.CacheWriteTTLPricing
	record.AuthMode = input.AuthMode
	record.Subscription = input.Subscription
	if record.ApiKey == "" && input.AuthMode != subscription.ClientPassthrough {
		record.ApiKey = existing.ApiKey
	}
	if input.ModelMultipliers == nil {
		record.ModelMultipliers = existing.ModelMultipliers
	}
	if input.BillingRules == nil {
		record.BillingRules = existing.BillingRules
	}
	if input.ModelPolicy == "" {
		record.ModelPolicy = existing.ModelPolicy
	}
	if input.ModelMapping == nil {
		record.ModelMapping = existing.ModelMapping
	}
	if input.SupportedModels == nil {
		record.SupportedModels = existing.SupportedModels
	}
	if err := credentialGroupService.UpdateGroup(ctx, id, record); err != nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("更新 Codex 凭证分组失败: %w", err)
	}
	if input.ApiKey != "" && input.ApiKey != existing.ApiKey {
		a.repairPoolGroup("codex", id)
	}
	updated, err := credentialGroupService.GetByID(ctx, id)
	if err != nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("读取更新后的 Codex 凭证分组失败: %w", err)
	}
	if logger != nil {
		logger.Info("✅ Codex 凭证分组已更新", "id", id, "name", updated.Name, "is_default", updated.IsDefault)
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return CodexCredentialGroupInfo{}, err
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return buildCodexCredentialGroupInfo(updated), nil
}

func (a *App) DeleteCodexCredentialGroupByID(id int64) error {
	a.routingWriteMu.Lock()
	defer a.routingWriteMu.Unlock()
	a.ensureCodexProviderService()

	a.mu.RLock()
	credentialGroupService := a.codexCredentialGroupService
	groupStore := a.codexCredentialGroupStore
	channelStore := a.codexChannelStore
	endpointStore := a.codexEndpointStore
	logger := a.logger
	a.mu.RUnlock()
	if credentialGroupService == nil {
		return fmt.Errorf("Codex 凭证分组服务未就绪，请稍后重试")
	}

	// Serialize with endpoint creation/edit/activation through the runtime
	// reload, so no endpoint can be attached to a group being removed.
	a.codexActivationMu.Lock()
	defer a.codexActivationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.deleteCredentialGroup(ctx, "codex", id, func(tx *sql.Tx) error {
		service := codexservice.NewCodexCredentialGroupService(groupStore.WithTx(tx), channelStore.WithTx(tx), endpointStore.WithTx(tx))
		return service.DeleteGroup(ctx, id)
	}); err != nil {
		return fmt.Errorf("删除 Codex 凭证分组失败: %w", err)
	}
	if logger != nil {
		logger.Info("✅ Codex 凭证分组已删除", "id", id)
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return err
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return a.refreshRoutingAfterCredentialGroupDeletion(ctx)
}

func (a *App) SetDefaultCodexCredentialGroup(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	credentialGroupService := a.codexCredentialGroupService
	a.mu.RUnlock()
	if credentialGroupService == nil {
		return fmt.Errorf("Codex 凭证分组服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := credentialGroupService.SetDefault(ctx, id); err != nil {
		return fmt.Errorf("设置默认 Codex 凭证分组失败: %w", err)
	}
	if err := a.reloadCodexEndpointManager(); err != nil {
		return err
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return nil
}
