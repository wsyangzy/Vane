package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
)

type ClaudeCredentialGroupInfo struct {
	Enabled                                bool                                        `json:"enabled"`
	AuthMode                               string                                      `json:"auth_mode"`
	Subscription                           *subscription.Binding                       `json:"subscription,omitempty"`
	LocalKeyConfigured                     bool                                        `json:"local_key_configured"`
	SubscriptionStatus                     []subscription.Pause                        `json:"subscription_status"`
	OfficialVerification                   string                                      `json:"official_verification"`
	ID                                     int64                                       `json:"id"`
	Channel                                string                                      `json:"channel"`
	Name                                   string                                      `json:"name"`
	Token                                  string                                      `json:"token"`
	ApiKey                                 string                                      `json:"api_key"`
	TokenMasked                            string                                      `json:"token_masked"`
	ApiKeyMasked                           string                                      `json:"api_key_masked"`
	IsDefault                              bool                                        `json:"is_default"`
	DailyCostLimitUSD                      float64                                     `json:"daily_cost_limit_usd"`
	ModelPolicy                            string                                      `json:"model_policy"`
	ModelMapping                           map[string]string                           `json:"model_mapping"`
	SupportedModels                        []string                                    `json:"supported_models"`
	CostMultiplier                         float64                                     `json:"cost_multiplier"`
	InputCostMultiplier                    float64                                     `json:"input_cost_multiplier"`
	OutputCostMultiplier                   float64                                     `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier            float64                                     `json:"cache_creation_cost_multiplier"`
	CacheCreationCostMultiplier1h          float64                                     `json:"cache_creation_cost_multiplier_1h"`
	CacheReadCostMultiplier                float64                                     `json:"cache_read_cost_multiplier"`
	LongContextCostMultiplier              float64                                     `json:"long_context_cost_multiplier"`
	LongContextInputCostMultiplier         float64                                     `json:"long_context_input_cost_multiplier"`
	LongContextOutputCostMultiplier        float64                                     `json:"long_context_output_cost_multiplier"`
	LongContextCacheCreationCostMultiplier float64                                     `json:"long_context_cache_creation_cost_multiplier"`
	LongContextCacheReadCostMultiplier     float64                                     `json:"long_context_cache_read_cost_multiplier"`
	DisableLongContextPricing              bool                                        `json:"disable_long_context_pricing"`
	ModelMultipliers                       []claudestore.EndpointModelMultiplierRecord `json:"model_multipliers"`
	EndpointCount                          int                                         `json:"endpoint_count"`
	CreatedAt                              string                                      `json:"created_at"`
	UpdatedAt                              string                                      `json:"updated_at"`
}

type CreateClaudeCredentialGroupInput struct {
	AuthMode                               string                                      `json:"auth_mode"`
	Subscription                           *subscription.Binding                       `json:"subscription,omitempty"`
	Channel                                string                                      `json:"channel"`
	Name                                   string                                      `json:"name"`
	Token                                  string                                      `json:"token"`
	ApiKey                                 string                                      `json:"api_key"`
	IsDefault                              bool                                        `json:"is_default"`
	DailyCostLimitUSD                      float64                                     `json:"daily_cost_limit_usd"`
	ModelPolicy                            string                                      `json:"model_policy"`
	ModelMapping                           map[string]string                           `json:"model_mapping"`
	SupportedModels                        []string                                    `json:"supported_models"`
	CostMultiplier                         float64                                     `json:"cost_multiplier"`
	InputCostMultiplier                    float64                                     `json:"input_cost_multiplier"`
	OutputCostMultiplier                   float64                                     `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier            float64                                     `json:"cache_creation_cost_multiplier"`
	CacheCreationCostMultiplier1h          float64                                     `json:"cache_creation_cost_multiplier_1h"`
	CacheReadCostMultiplier                float64                                     `json:"cache_read_cost_multiplier"`
	LongContextCostMultiplier              float64                                     `json:"long_context_cost_multiplier"`
	LongContextInputCostMultiplier         float64                                     `json:"long_context_input_cost_multiplier"`
	LongContextOutputCostMultiplier        float64                                     `json:"long_context_output_cost_multiplier"`
	LongContextCacheCreationCostMultiplier float64                                     `json:"long_context_cache_creation_cost_multiplier"`
	LongContextCacheReadCostMultiplier     float64                                     `json:"long_context_cache_read_cost_multiplier"`
	DisableLongContextPricing              bool                                        `json:"disable_long_context_pricing"`
	ModelMultipliers                       []claudestore.EndpointModelMultiplierRecord `json:"model_multipliers"`
}

func resolveEndpointCredentialGroupID(inputID int64, existing *claudestore.EndpointRecord, channel string) int64 {
	if inputID > 0 {
		return inputID
	}
	if existing == nil {
		return 0
	}
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(channel) == strings.TrimSpace(existing.Channel) {
		return existing.CredentialGroupID
	}
	return 0
}

func (a *App) listClaudeCredentialGroupInfos(ctx context.Context, svc *claudeservice.ClaudeCredentialGroupService, groupStore claudestore.ClaudeCredentialGroupStore, channel string) []ClaudeCredentialGroupInfo {
	if svc == nil {
		return nil
	}
	groups, err := svc.ListByChannel(ctx, channel)
	if err != nil {
		return nil
	}
	result := make([]ClaudeCredentialGroupInfo, 0, len(groups))
	for _, group := range groups {
		result = append(result, a.claudeCredentialGroupToInfo(ctx, groupStore, group))
	}
	return result
}

func (a *App) claudeCredentialGroupToInfo(ctx context.Context, groupStore claudestore.ClaudeCredentialGroupStore, group *claudestore.ClaudeCredentialGroupRecord) ClaudeCredentialGroupInfo {
	if group == nil {
		return ClaudeCredentialGroupInfo{}
	}
	info := ClaudeCredentialGroupInfo{
		Enabled:                                group.Enabled,
		AuthMode:                               group.AuthMode,
		OfficialVerification:                   "not_verified",
		ID:                                     group.ID,
		Channel:                                group.Channel,
		Name:                                   group.Name,
		Token:                                  group.Token,
		ApiKey:                                 group.ApiKey,
		TokenMasked:                            maskToken(group.Token),
		ApiKeyMasked:                           maskToken(group.ApiKey),
		IsDefault:                              group.IsDefault,
		DailyCostLimitUSD:                      group.DailyCostLimitUSD,
		ModelPolicy:                            group.ModelPolicy,
		ModelMapping:                           group.ModelMapping,
		SupportedModels:                        group.SupportedModels,
		CostMultiplier:                         group.CostMultiplier,
		InputCostMultiplier:                    group.InputCostMultiplier,
		OutputCostMultiplier:                   group.OutputCostMultiplier,
		CacheCreationCostMultiplier:            group.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h:          group.CacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:                group.CacheReadCostMultiplier,
		LongContextCostMultiplier:              group.LongContextCostMultiplier,
		LongContextInputCostMultiplier:         group.LongContextInputCostMultiplier,
		LongContextOutputCostMultiplier:        group.LongContextOutputCostMultiplier,
		LongContextCacheCreationCostMultiplier: group.LongContextCacheCreationCostMultiplier,
		LongContextCacheReadCostMultiplier:     group.LongContextCacheReadCostMultiplier,
		DisableLongContextPricing:              group.DisableLongContextPricing,
		ModelMultipliers:                       group.ModelMultipliers,
	}
	if group.Subscription != nil {
		binding := *group.Subscription
		info.LocalKeyConfigured = binding.KeyHash != ""
		binding.KeyHash = ""
		binding.Action = ""
		binding.GroupID = group.ID
		binding.Channel = group.Channel
		info.Subscription = &binding
		if a.claudeSubscriptions != nil && group.AuthMode == subscription.ClientPassthrough {
			info.SubscriptionStatus = a.claudeSubscriptions.Status(ctx, binding)
		}
	}
	if !group.CreatedAt.IsZero() {
		info.CreatedAt = group.CreatedAt.Format("2006-01-02 15:04:05")
	}
	if !group.UpdatedAt.IsZero() {
		info.UpdatedAt = group.UpdatedAt.Format("2006-01-02 15:04:05")
	}
	if groupStore != nil {
		if count, err := groupStore.CountEndpoints(ctx, group.ID); err == nil {
			info.EndpointCount = count
		}
	}
	return info
}

func claudeCredentialGroupInputToRecord(input CreateClaudeCredentialGroupInput) *claudestore.ClaudeCredentialGroupRecord {
	return &claudestore.ClaudeCredentialGroupRecord{
		AuthMode:                               input.AuthMode,
		Subscription:                           input.Subscription,
		Channel:                                input.Channel,
		Name:                                   input.Name,
		Token:                                  input.Token,
		ApiKey:                                 input.ApiKey,
		IsDefault:                              input.IsDefault,
		DailyCostLimitUSD:                      input.DailyCostLimitUSD,
		ModelPolicy:                            input.ModelPolicy,
		ModelMapping:                           input.ModelMapping,
		SupportedModels:                        input.SupportedModels,
		CostMultiplier:                         input.CostMultiplier,
		InputCostMultiplier:                    input.InputCostMultiplier,
		OutputCostMultiplier:                   input.OutputCostMultiplier,
		CacheCreationCostMultiplier:            input.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h:          input.CacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:                input.CacheReadCostMultiplier,
		LongContextCostMultiplier:              input.LongContextCostMultiplier,
		LongContextInputCostMultiplier:         input.LongContextInputCostMultiplier,
		LongContextOutputCostMultiplier:        input.LongContextOutputCostMultiplier,
		LongContextCacheCreationCostMultiplier: input.LongContextCacheCreationCostMultiplier,
		LongContextCacheReadCostMultiplier:     input.LongContextCacheReadCostMultiplier,
		DisableLongContextPricing:              input.DisableLongContextPricing,
		ModelMultipliers:                       input.ModelMultipliers,
	}
}

func (a *App) refreshClaudeRuntimeAfterCredentialGroupChange(ctx context.Context) {
	a.mu.RLock()
	endpointService := a.endpointService
	a.mu.RUnlock()
	if endpointService != nil {
		if err := endpointService.SyncFromDatabase(ctx); err != nil && a.logger != nil {
			a.logger.Warn("⚠️ Claude 凭证分组变更后同步运行时端点失败", "error", err)
		}
	}
	a.syncClaudeRouteConfig(ctx)
	a.syncEndpointMultipliersToTracker(ctx)
	a.emitEndpointUpdate()
}

func (a *App) CreateClaudeCredentialGroup(input CreateClaudeCredentialGroupInput) (ClaudeCredentialGroupInfo, error) {
	a.mu.RLock()
	svc := a.claudeCredentialGroupService
	groupStore := a.claudeCredentialGroupStore
	logger := a.logger
	a.mu.RUnlock()
	if svc == nil {
		return ClaudeCredentialGroupInfo{}, fmt.Errorf("Claude 凭证分组服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	created, err := svc.CreateGroup(ctx, claudeCredentialGroupInputToRecord(input))
	if err != nil {
		return ClaudeCredentialGroupInfo{}, err
	}
	if logger != nil {
		logger.Info("✅ Claude 凭证分组已创建", "channel", created.Channel, "name", created.Name)
	}
	a.refreshClaudeRuntimeAfterCredentialGroupChange(ctx)
	return a.claudeCredentialGroupToInfo(ctx, groupStore, created), nil
}

func (a *App) UpdateClaudeCredentialGroupByID(id int64, input CreateClaudeCredentialGroupInput) (ClaudeCredentialGroupInfo, error) {
	a.mu.RLock()
	svc := a.claudeCredentialGroupService
	groupStore := a.claudeCredentialGroupStore
	logger := a.logger
	a.mu.RUnlock()
	if svc == nil {
		return ClaudeCredentialGroupInfo{}, fmt.Errorf("Claude 凭证分组服务未启用")
	}
	if id <= 0 {
		return ClaudeCredentialGroupInfo{}, fmt.Errorf("凭证分组ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	record := claudeCredentialGroupInputToRecord(input)
	if input.ModelMultipliers == nil || input.ModelPolicy == "" || input.ModelMapping == nil || input.SupportedModels == nil {
		if existing, err := svc.GetByID(ctx, id); err == nil && existing != nil {
			if input.ModelPolicy == "" {
				record.ModelPolicy = existing.ModelPolicy
			}
			if input.ModelMapping == nil {
				record.ModelMapping = existing.ModelMapping
			}
			if input.SupportedModels == nil {
				record.SupportedModels = existing.SupportedModels
			}
			if input.ModelMultipliers == nil {
				record.ModelMultipliers = existing.ModelMultipliers
			}
		}
	}
	if err := svc.UpdateGroup(ctx, id, record); err != nil {
		return ClaudeCredentialGroupInfo{}, err
	}
	if input.Token != "" || input.ApiKey != "" {
		a.repairPoolGroup("claude", id)
	}
	updated, err := svc.GetByID(ctx, id)
	if err != nil {
		return ClaudeCredentialGroupInfo{}, err
	}
	if logger != nil && updated != nil {
		logger.Info("✅ Claude 凭证分组已更新", "id", id, "channel", updated.Channel, "name", updated.Name)
	}
	a.refreshClaudeRuntimeAfterCredentialGroupChange(ctx)
	return a.claudeCredentialGroupToInfo(ctx, groupStore, updated), nil
}

func (a *App) DeleteClaudeCredentialGroupByID(id int64) error {
	a.routingWriteMu.Lock()
	defer a.routingWriteMu.Unlock()
	a.mu.RLock()
	svc := a.claudeCredentialGroupService
	groupStore := a.claudeCredentialGroupStore
	channelStore := a.channelStore
	endpointStore := a.endpointStore
	logger := a.logger
	a.mu.RUnlock()
	if svc == nil {
		return fmt.Errorf("Claude 凭证分组服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("凭证分组ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.deleteCredentialGroup(ctx, "claude", id, func(tx *sql.Tx) error {
		service := claudeservice.NewClaudeCredentialGroupService(groupStore.WithTx(tx), channelStore.WithTx(tx), endpointStore.WithTx(tx))
		return service.DeleteGroup(ctx, id)
	}); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("✅ Claude 凭证分组已删除", "id", id)
	}
	a.refreshClaudeRuntimeAfterCredentialGroupChange(ctx)
	return a.refreshRoutingAfterCredentialGroupDeletion(ctx)
}

func (a *App) SetDefaultClaudeCredentialGroup(id int64) error {
	a.mu.RLock()
	svc := a.claudeCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()
	if svc == nil {
		return fmt.Errorf("Claude 凭证分组服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("凭证分组ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.SetDefault(ctx, id); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("✅ Claude 默认凭证分组已更新", "id", id)
	}
	a.refreshClaudeRuntimeAfterCredentialGroupChange(ctx)
	return nil
}
