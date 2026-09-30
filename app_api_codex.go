package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ai-relay-forwarder/internal/providers/codex/proxy"
	codexstore "ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/routing"
	"ai-relay-forwarder/internal/tracking"
)

// ============================================================================
// 旧 Provider DTO（保留以维持 wailsjs/go/models.ts 生成稳定）
// 自 2026-05-13 起，Codex 已切换到「渠道-端点」两层模型；旧接口转调用新接口。
// ============================================================================

// CodexProviderInfo 旧前端 DTO；现在通过组装 channel + 激活端点回退构造。
type CodexProviderInfo struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	BaseURL         string `json:"base_url"`
	ApiKey          string `json:"api_key"`
	ApiKeyMasked    string `json:"api_key_masked"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
	IsActive        bool   `json:"is_active"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type CreateCodexProviderInput struct {
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	BaseURL         string `json:"base_url"`
	ApiKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
}

type UpdateCodexProviderInput struct {
	Name            string `json:"name"`
	ProviderType    string `json:"provider_type"`
	BaseURL         string `json:"base_url"`
	ApiKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"model_reasoning_effort"`
	Enable1MContext bool   `json:"enable_1m_context"`
	Website         string `json:"website"`
}

type CodexProviderStorageStatus struct {
	Enabled            bool   `json:"enabled"`
	TotalCount         int    `json:"total_count"`
	HasActive          bool   `json:"has_active"`
	ActiveProviderID   int64  `json:"active_provider_id"`
	ActiveProviderName string `json:"active_provider_name"`
}

// ============================================================================
// 新「渠道-端点」DTO
// ============================================================================

// CodexChannelInfo 提供给前端展示的渠道信息（含端点列表）。
type CodexChannelInfo struct {
	ID               int64                      `json:"id"`
	Name             string                     `json:"name"`
	ProviderType     string                     `json:"provider_type"`
	ApiKey           string                     `json:"api_key"`
	ApiKeyMasked     string                     `json:"api_key_masked"`
	Model            string                     `json:"model"`
	ModelMapping     map[string]string          `json:"model_mapping"`
	ReasoningEffort  string                     `json:"model_reasoning_effort"`
	Enable1MContext  bool                       `json:"enable_1m_context"`
	Website          string                     `json:"website"`
	IsActive         bool                       `json:"is_active"`
	Priority         int                        `json:"priority"`
	FailoverEnabled  bool                       `json:"failover_enabled"`
	NonStreamRoute   *NonStreamRouteInfo        `json:"non_stream_route"` // 非流改道配置，关闭时为 null
	PrivacyLevel     string                     `json:"privacy_level"`    // 隐私级别：""=跟随全局 / third_party / trusted
	CredentialGroups []CodexCredentialGroupInfo `json:"credential_groups"`
	Endpoints        []CodexEndpointInfo        `json:"endpoints"`
	CreatedAt        string                     `json:"created_at"`
	UpdatedAt        string                     `json:"updated_at"`
}

type CodexChannelCandidateInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// CodexCredentialGroupInfo 提供给前端展示的凭证分组信息。
type CodexCredentialGroupInfo struct {
	ID                                     int64                                   `json:"id"`
	ChannelID                              int64                                   `json:"channel_id"`
	Name                                   string                                  `json:"name"`
	ApiKey                                 string                                  `json:"api_key"`
	ApiKeyMasked                           string                                  `json:"api_key_masked"`
	IsDefault                              bool                                    `json:"is_default"`
	DailyCostLimitUSD                      float64                                 `json:"daily_cost_limit_usd"`
	ModelPolicy                            string                                  `json:"modelPolicy"`
	ModelMapping                           map[string]string                       `json:"modelMapping"`
	SupportedModels                        []string                                `json:"supportedModels"`
	CostMultiplier                         float64                                 `json:"costMultiplier"`
	InputCostMultiplier                    float64                                 `json:"inputCostMultiplier"`
	OutputCostMultiplier                   float64                                 `json:"outputCostMultiplier"`
	CacheCreationCostMultiplier            float64                                 `json:"cacheCreationCostMultiplier"`
	CacheReadCostMultiplier                float64                                 `json:"cacheReadCostMultiplier"`
	LongContextCostMultiplier              float64                                 `json:"longContextCostMultiplier"`
	LongContextInputCostMultiplier         float64                                 `json:"longContextInputCostMultiplier"`
	LongContextOutputCostMultiplier        float64                                 `json:"longContextOutputCostMultiplier"`
	LongContextCacheCreationCostMultiplier float64                                 `json:"longContextCacheCreationCostMultiplier"`
	LongContextCacheReadCostMultiplier     float64                                 `json:"longContextCacheReadCostMultiplier"`
	DisableLongContextPricing              bool                                    `json:"disableLongContextPricing"`
	BillingOutputTokenSource               string                                  `json:"billingOutputTokenSource"`
	ModelMultipliers                       []codexstore.CodexModelMultiplierRecord `json:"modelMultipliers"`
	BillingRules                           []codexstore.CodexBillingRuleRecord     `json:"billingRules"`
	CreatedAt                              string                                  `json:"created_at"`
	UpdatedAt                              string                                  `json:"updated_at"`
}

// CodexEndpointInfo 提供给前端展示的端点信息（name + base_url）。
type CodexEndpointInfo struct {
	ID                                     int64                                   `json:"id"`
	ChannelID                              int64                                   `json:"channel_id"`
	CredentialGroupID                      int64                                   `json:"credential_group_id"`
	CredentialGroupName                    string                                  `json:"credential_group_name"`
	ApiKey                                 string                                  `json:"api_key"`
	ApiKeyMasked                           string                                  `json:"api_key_masked"`
	Name                                   string                                  `json:"name"`
	BaseURL                                string                                  `json:"base_url"`
	IsActive                               bool                                    `json:"is_active"`
	FailoverEnabled                        bool                                    `json:"failover_enabled"`
	SkipHealthCheck                        bool                                    `json:"skip_health_check"`
	Healthy                                bool                                    `json:"healthy"`
	HealthState                            string                                  `json:"health_state"`
	HealthInScope                          bool                                    `json:"health_in_scope"`
	ResponseTimeMS                         int64                                   `json:"response_time_ms"`
	LastCheck                              string                                  `json:"last_check"`
	LastFailCode                           int                                     `json:"last_fail_code"`
	LastFailReason                         string                                  `json:"last_fail_reason"`
	Priority                               int                                     `json:"priority"`
	TimeoutSeconds                         int                                     `json:"timeout_seconds"`
	CostMultiplier                         float64                                 `json:"costMultiplier"`
	InputCostMultiplier                    float64                                 `json:"inputCostMultiplier"`
	OutputCostMultiplier                   float64                                 `json:"outputCostMultiplier"`
	CacheCreationCostMultiplier            float64                                 `json:"cacheCreationCostMultiplier"`
	CacheReadCostMultiplier                float64                                 `json:"cacheReadCostMultiplier"`
	LongContextCostMultiplier              float64                                 `json:"longContextCostMultiplier"`
	LongContextInputCostMultiplier         float64                                 `json:"longContextInputCostMultiplier"`
	LongContextOutputCostMultiplier        float64                                 `json:"longContextOutputCostMultiplier"`
	LongContextCacheCreationCostMultiplier float64                                 `json:"longContextCacheCreationCostMultiplier"`
	LongContextCacheReadCostMultiplier     float64                                 `json:"longContextCacheReadCostMultiplier"`
	DisableLongContextPricing              bool                                    `json:"disableLongContextPricing"`
	BillingOutputTokenSource               string                                  `json:"billingOutputTokenSource"`
	ModelMultipliers                       []codexstore.CodexModelMultiplierRecord `json:"modelMultipliers"`
	CreatedAt                              string                                  `json:"created_at"`
	UpdatedAt                              string                                  `json:"updated_at"`
}

// CreateCodexChannelInput 创建渠道（不含端点）。
type CreateCodexChannelInput struct {
	Name            string            `json:"name"`
	ProviderType    string            `json:"provider_type"`
	ApiKey          string            `json:"api_key"`
	Model           string            `json:"model"`
	ModelMapping    map[string]string `json:"model_mapping"`
	ReasoningEffort string            `json:"model_reasoning_effort"`
	Enable1MContext bool              `json:"enable_1m_context"`
	Website         string            `json:"website"`
	Priority        int               `json:"priority"`
	FailoverEnabled *bool             `json:"failover_enabled"`
}

type UpdateCodexChannelInput struct {
	Name            string            `json:"name"`
	ProviderType    string            `json:"provider_type"`
	ApiKey          string            `json:"api_key"`
	Model           string            `json:"model"`
	ModelMapping    map[string]string `json:"model_mapping"`
	ReasoningEffort string            `json:"model_reasoning_effort"`
	Enable1MContext bool              `json:"enable_1m_context"`
	Website         string            `json:"website"`
	Priority        int               `json:"priority"`
	FailoverEnabled *bool             `json:"failover_enabled"`
}

type CreateCodexCredentialGroupInput struct {
	ChannelID                              int64                                   `json:"channel_id"`
	Name                                   string                                  `json:"name"`
	ApiKey                                 string                                  `json:"api_key"`
	IsDefault                              bool                                    `json:"is_default"`
	DailyCostLimitUSD                      float64                                 `json:"daily_cost_limit_usd"`
	CostMultiplier                         float64                                 `json:"costMultiplier"`
	InputCostMultiplier                    float64                                 `json:"inputCostMultiplier"`
	OutputCostMultiplier                   float64                                 `json:"outputCostMultiplier"`
	CacheCreationCostMultiplier            float64                                 `json:"cacheCreationCostMultiplier"`
	CacheReadCostMultiplier                float64                                 `json:"cacheReadCostMultiplier"`
	LongContextCostMultiplier              float64                                 `json:"longContextCostMultiplier"`
	LongContextInputCostMultiplier         float64                                 `json:"longContextInputCostMultiplier"`
	LongContextOutputCostMultiplier        float64                                 `json:"longContextOutputCostMultiplier"`
	LongContextCacheCreationCostMultiplier float64                                 `json:"longContextCacheCreationCostMultiplier"`
	LongContextCacheReadCostMultiplier     float64                                 `json:"longContextCacheReadCostMultiplier"`
	DisableLongContextPricing              bool                                    `json:"disableLongContextPricing"`
	ModelPolicy                            string                                  `json:"modelPolicy"`
	ModelMapping                           map[string]string                       `json:"modelMapping"`
	SupportedModels                        []string                                `json:"supportedModels"`
	BillingOutputTokenSource               string                                  `json:"billingOutputTokenSource"`
	ModelMultipliers                       []codexstore.CodexModelMultiplierRecord `json:"modelMultipliers"`
	BillingRules                           []codexstore.CodexBillingRuleRecord     `json:"billingRules"`
}

type UpdateCodexCredentialGroupInput struct {
	Name                                   string                                  `json:"name"`
	ApiKey                                 string                                  `json:"api_key"`
	IsDefault                              bool                                    `json:"is_default"`
	DailyCostLimitUSD                      float64                                 `json:"daily_cost_limit_usd"`
	CostMultiplier                         float64                                 `json:"costMultiplier"`
	InputCostMultiplier                    float64                                 `json:"inputCostMultiplier"`
	OutputCostMultiplier                   float64                                 `json:"outputCostMultiplier"`
	CacheCreationCostMultiplier            float64                                 `json:"cacheCreationCostMultiplier"`
	CacheReadCostMultiplier                float64                                 `json:"cacheReadCostMultiplier"`
	LongContextCostMultiplier              float64                                 `json:"longContextCostMultiplier"`
	LongContextInputCostMultiplier         float64                                 `json:"longContextInputCostMultiplier"`
	LongContextOutputCostMultiplier        float64                                 `json:"longContextOutputCostMultiplier"`
	LongContextCacheCreationCostMultiplier float64                                 `json:"longContextCacheCreationCostMultiplier"`
	LongContextCacheReadCostMultiplier     float64                                 `json:"longContextCacheReadCostMultiplier"`
	DisableLongContextPricing              bool                                    `json:"disableLongContextPricing"`
	ModelPolicy                            string                                  `json:"modelPolicy"`
	ModelMapping                           map[string]string                       `json:"modelMapping"`
	SupportedModels                        []string                                `json:"supportedModels"`
	BillingOutputTokenSource               string                                  `json:"billingOutputTokenSource"`
	ModelMultipliers                       []codexstore.CodexModelMultiplierRecord `json:"modelMultipliers"`
	BillingRules                           []codexstore.CodexBillingRuleRecord     `json:"billingRules"`
}

type CreateCodexEndpointInput struct {
	ChannelID                int64                                   `json:"channel_id"`
	CredentialGroupID        int64                                   `json:"credential_group_id"`
	ApiKey                   string                                  `json:"api_key"`
	Name                     string                                  `json:"name"`
	BaseURL                  string                                  `json:"base_url"`
	FailoverEnabled          *bool                                   `json:"failover_enabled"` // nil 时默认 true
	SkipHealthCheck          bool                                    `json:"skip_health_check"`
	Priority                 *int                                    `json:"priority"`
	TimeoutSeconds           int                                     `json:"timeout_seconds"`         // 0 = 使用全局超时
	CostMultiplier           float64                                 `json:"costMultiplier"`          // 默认 1.0
	InputCostMultiplier      float64                                 `json:"inputCostMultiplier"`     // 默认 1.0
	OutputCostMultiplier     float64                                 `json:"outputCostMultiplier"`    // 默认 1.0
	CacheReadCostMultiplier  float64                                 `json:"cacheReadCostMultiplier"` // 默认 1.0
	BillingOutputTokenSource string                                  `json:"billingOutputTokenSource"`
	ModelMultipliers         []codexstore.CodexModelMultiplierRecord `json:"modelMultipliers"`
}

type UpdateCodexEndpointInput struct {
	Name                     string                                  `json:"name"`
	BaseURL                  string                                  `json:"base_url"`
	CredentialGroupID        *int64                                  `json:"credential_group_id"`
	ApiKey                   *string                                 `json:"api_key"`
	FailoverEnabled          *bool                                   `json:"failover_enabled"`
	SkipHealthCheck          *bool                                   `json:"skip_health_check"`
	Priority                 *int                                    `json:"priority"`
	TimeoutSeconds           *int                                    `json:"timeout_seconds"`
	CostMultiplier           *float64                                `json:"costMultiplier"`
	InputCostMultiplier      *float64                                `json:"inputCostMultiplier"`
	OutputCostMultiplier     *float64                                `json:"outputCostMultiplier"`
	CacheReadCostMultiplier  *float64                                `json:"cacheReadCostMultiplier"`
	BillingOutputTokenSource *string                                 `json:"billingOutputTokenSource"`
	ModelMultipliers         []codexstore.CodexModelMultiplierRecord `json:"modelMultipliers"`
}

// ============================================================================
// Storage Status
// ============================================================================

func (a *App) GetCodexProviderStorageStatus() CodexProviderStorageStatus {
	status := CodexProviderStorageStatus{}
	a.ensureCodexProviderService()

	a.mu.RLock()
	channelService := a.codexChannelService
	a.mu.RUnlock()
	if channelService == nil {
		return status
	}

	status.Enabled = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	channels, err := channelService.ListChannels(ctx)
	if err == nil {
		status.TotalCount = len(channels)
	}

	active, err := channelService.GetActiveChannel(ctx)
	if err == nil && active != nil {
		status.HasActive = true
		status.ActiveProviderID = active.ID
		status.ActiveProviderName = active.Name
	}

	return status
}

// ============================================================================
// 新 Channel API
// ============================================================================

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
		if defaultGroup != nil && defaultGroup.ApiKey == previous.ApiKey {
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

// SetCodexChannelFailoverEnabled 只更新渠道是否参与渠道间故障转移。
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

func (a *App) reloadCodexEndpointManager() error {
	a.mu.RLock()
	manager := a.codexEndpointMgr
	a.mu.RUnlock()
	if manager == nil {
		return nil
	}
	return manager.Reload()
}

// ============================================================================
// 新 Credential Group API
// ============================================================================

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

	created, err := credentialGroupService.CreateGroup(ctx, codexCredentialGroupInputToRecord(input.ChannelID, input.Name, input.ApiKey, input.IsDefault, input.DailyCostLimitUSD, input.ModelPolicy, input.ModelMapping, input.SupportedModels, input.CostMultiplier, input.InputCostMultiplier, input.OutputCostMultiplier, input.CacheCreationCostMultiplier, input.CacheReadCostMultiplier, input.LongContextCostMultiplier, input.LongContextInputCostMultiplier, input.LongContextOutputCostMultiplier, input.LongContextCacheCreationCostMultiplier, input.LongContextCacheReadCostMultiplier, input.DisableLongContextPricing, input.BillingOutputTokenSource, input.ModelMultipliers, input.BillingRules))
	if err != nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("创建 Codex 凭证分组失败: %w", err)
	}
	if logger != nil {
		logger.Info("✅ Codex 凭证分组已创建", "channel_id", created.ChannelID, "name", created.Name, "is_default", created.IsDefault)
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
	if record.ApiKey == "" {
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
	updated, err := credentialGroupService.GetByID(ctx, id)
	if err != nil {
		return CodexCredentialGroupInfo{}, fmt.Errorf("读取更新后的 Codex 凭证分组失败: %w", err)
	}
	if logger != nil {
		logger.Info("✅ Codex 凭证分组已更新", "id", id, "name", updated.Name, "is_default", updated.IsDefault)
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return buildCodexCredentialGroupInfo(updated), nil
}

func (a *App) DeleteCodexCredentialGroupByID(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	credentialGroupService := a.codexCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()
	if credentialGroupService == nil {
		return fmt.Errorf("Codex 凭证分组服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := credentialGroupService.DeleteGroup(ctx, id); err != nil {
		return fmt.Errorf("删除 Codex 凭证分组失败: %w", err)
	}
	if logger != nil {
		logger.Info("✅ Codex 凭证分组已删除", "id", id)
	}
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return nil
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
	a.emitCodexProviderUpdate()
	go a.syncEndpointMultipliersToTracker(context.Background())
	return nil
}

// ============================================================================
// 新 Endpoint API
// ============================================================================

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

// ============================================================================
// 旧 Provider API（兼容垫片，转调用新 Channel/Endpoint 接口）
// id 视为 channel_id；旧 input 拆为 channel + endpoint。
// 当前前端已切换到新接口，旧接口仅保留用于兼容外部调用。
// ============================================================================

func (a *App) GetCodexProviders() ([]CodexProviderInfo, error) {
	channels, err := a.GetCodexChannels()
	if err != nil {
		return nil, err
	}
	result := make([]CodexProviderInfo, 0, len(channels))
	for _, ch := range channels {
		var baseURL string
		for _, ep := range ch.Endpoints {
			if ep.IsActive {
				baseURL = ep.BaseURL
				break
			}
		}
		if baseURL == "" && len(ch.Endpoints) > 0 {
			baseURL = ch.Endpoints[0].BaseURL
		}
		result = append(result, CodexProviderInfo{
			ID:              ch.ID,
			Name:            ch.Name,
			ProviderType:    ch.ProviderType,
			BaseURL:         baseURL,
			ApiKey:          ch.ApiKey,
			ApiKeyMasked:    ch.ApiKeyMasked,
			Model:           ch.Model,
			ReasoningEffort: ch.ReasoningEffort,
			Enable1MContext: ch.Enable1MContext,
			Website:         ch.Website,
			IsActive:        ch.IsActive,
			CreatedAt:       ch.CreatedAt,
			UpdatedAt:       ch.UpdatedAt,
		})
	}
	return result, nil
}

func (a *App) CreateCodexProvider(input CreateCodexProviderInput) error {
	if err := a.CreateCodexChannel(CreateCodexChannelInput{
		Name:            input.Name,
		ProviderType:    input.ProviderType,
		ApiKey:          input.ApiKey,
		Model:           input.Model,
		ReasoningEffort: input.ReasoningEffort,
		Enable1MContext: input.Enable1MContext,
		Website:         input.Website,
	}); err != nil {
		return err
	}

	a.mu.RLock()
	channelService := a.codexChannelService
	a.mu.RUnlock()
	if channelService == nil {
		return fmt.Errorf("Codex 渠道服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := channelService.GetActiveChannel(ctx)
	if err != nil || created == nil || created.Name != input.Name {
		ch, _ := channelService.GetChannelByName(ctx, input.Name)
		if ch == nil {
			return fmt.Errorf("创建 Codex 端点失败: 找不到名为 '%s' 的渠道", input.Name)
		}
		return a.CreateCodexEndpoint(CreateCodexEndpointInput{ChannelID: ch.ID, Name: "默认端点", BaseURL: input.BaseURL})
	}
	return a.CreateCodexEndpoint(CreateCodexEndpointInput{ChannelID: created.ID, Name: "默认端点", BaseURL: input.BaseURL})
}

func (a *App) UpdateCodexProviderByID(id int64, input UpdateCodexProviderInput) error {
	if err := a.UpdateCodexChannelByID(id, UpdateCodexChannelInput{
		Name:            input.Name,
		ProviderType:    input.ProviderType,
		ApiKey:          input.ApiKey,
		Model:           input.Model,
		ReasoningEffort: input.ReasoningEffort,
		Enable1MContext: input.Enable1MContext,
		Website:         input.Website,
	}); err != nil {
		return err
	}

	a.mu.RLock()
	endpointService := a.codexEndpointService
	a.mu.RUnlock()
	if endpointService == nil {
		return fmt.Errorf("Codex 端点服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	active, err := endpointService.GetActiveByChannelID(ctx, id)
	if err != nil {
		return err
	}
	if active != nil && active.BaseURL != input.BaseURL {
		return a.UpdateCodexEndpointByID(active.ID, UpdateCodexEndpointInput{Name: active.Name, BaseURL: input.BaseURL})
	}
	if active == nil {
		// 渠道下没有端点，新建一个
		return a.CreateCodexEndpoint(CreateCodexEndpointInput{ChannelID: id, Name: "默认端点", BaseURL: input.BaseURL})
	}
	return nil
}

func (a *App) DeleteCodexProviderByID(id int64) error {
	return a.DeleteCodexChannelByID(id)
}

func (a *App) ActivateCodexProvider(id int64) error {
	return a.ActivateCodexChannel(id)
}

// ============================================================================
// 内部转换辅助
// ============================================================================

func codexChannelInputToRecord(name, providerType, apiKey, model string, modelMapping map[string]string, reasoningEffort, website string, enable1M bool) *codexstore.CodexChannelRecord {
	return &codexstore.CodexChannelRecord{
		Name:            name,
		ProviderType:    providerType,
		ApiKey:          apiKey,
		Model:           model,
		ModelMapping:    modelMapping,
		ReasoningEffort: reasoningEffort,
		Enable1MContext: enable1M,
		Website:         website,
	}
}

func codexCredentialGroupInputToRecord(channelID int64, name, apiKey string, isDefault bool, dailyCostLimitUSD float64, modelPolicy string, modelMapping map[string]string, supportedModels []string, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheCreationCostMultiplier, cacheReadCostMultiplier, longContextCostMultiplier, longContextInputCostMultiplier, longContextOutputCostMultiplier, longContextCacheCreationCostMultiplier, longContextCacheReadCostMultiplier float64, disableLongContextPricing bool, billingOutputTokenSource string, modelMultipliers []codexstore.CodexModelMultiplierRecord, billingRules []codexstore.CodexBillingRuleRecord) *codexstore.CodexCredentialGroupRecord {
	return &codexstore.CodexCredentialGroupRecord{
		ChannelID:                              channelID,
		Name:                                   name,
		ApiKey:                                 apiKey,
		IsDefault:                              isDefault,
		DailyCostLimitUSD:                      dailyCostLimitUSD,
		ModelPolicy:                            modelPolicy,
		ModelMapping:                           modelMapping,
		SupportedModels:                        supportedModels,
		CostMultiplier:                         costMultiplier,
		InputCostMultiplier:                    inputCostMultiplier,
		OutputCostMultiplier:                   outputCostMultiplier,
		CacheCreationCostMultiplier:            cacheCreationCostMultiplier,
		CacheReadCostMultiplier:                cacheReadCostMultiplier,
		LongContextCostMultiplier:              longContextCostMultiplier,
		LongContextInputCostMultiplier:         longContextInputCostMultiplier,
		LongContextOutputCostMultiplier:        longContextOutputCostMultiplier,
		LongContextCacheCreationCostMultiplier: longContextCacheCreationCostMultiplier,
		LongContextCacheReadCostMultiplier:     longContextCacheReadCostMultiplier,
		DisableLongContextPricing:              disableLongContextPricing,
		BillingOutputTokenSource:               tracking.NormalizeBillingOutputTokenSource(billingOutputTokenSource),
		ModelMultipliers:                       modelMultipliers,
		BillingRules:                           billingRules,
	}
}

func buildCodexCredentialGroupInfo(record *codexstore.CodexCredentialGroupRecord) CodexCredentialGroupInfo {
	if record == nil {
		return CodexCredentialGroupInfo{}
	}
	return CodexCredentialGroupInfo{
		ID:                                     record.ID,
		ChannelID:                              record.ChannelID,
		Name:                                   record.Name,
		ApiKey:                                 record.ApiKey,
		ApiKeyMasked:                           maskToken(record.ApiKey),
		IsDefault:                              record.IsDefault,
		DailyCostLimitUSD:                      record.DailyCostLimitUSD,
		ModelPolicy:                            record.ModelPolicy,
		ModelMapping:                           record.ModelMapping,
		SupportedModels:                        record.SupportedModels,
		CostMultiplier:                         record.CostMultiplier,
		InputCostMultiplier:                    record.InputCostMultiplier,
		OutputCostMultiplier:                   record.OutputCostMultiplier,
		CacheCreationCostMultiplier:            record.CacheCreationCostMultiplier,
		CacheReadCostMultiplier:                record.CacheReadCostMultiplier,
		LongContextCostMultiplier:              record.LongContextCostMultiplier,
		LongContextInputCostMultiplier:         record.LongContextInputCostMultiplier,
		LongContextOutputCostMultiplier:        record.LongContextOutputCostMultiplier,
		LongContextCacheCreationCostMultiplier: record.LongContextCacheCreationCostMultiplier,
		LongContextCacheReadCostMultiplier:     record.LongContextCacheReadCostMultiplier,
		DisableLongContextPricing:              record.DisableLongContextPricing,
		BillingOutputTokenSource:               tracking.NormalizeBillingOutputTokenSource(record.BillingOutputTokenSource),
		ModelMultipliers:                       record.ModelMultipliers,
		BillingRules:                           record.BillingRules,
		CreatedAt:                              formatCodexTime(record.CreatedAt),
		UpdatedAt:                              formatCodexTime(record.UpdatedAt),
	}
}

func copyCodexModelMapping(modelMapping map[string]string) map[string]string {
	if len(modelMapping) == 0 {
		return map[string]string{}
	}
	copied := make(map[string]string, len(modelMapping))
	for source, target := range modelMapping {
		copied[source] = target
	}
	return copied
}

func (a *App) buildCodexChannelInfo(record *codexstore.CodexChannelRecord, endpoints []*codexstore.CodexEndpointRecord, credentialGroups []*codexstore.CodexCredentialGroupRecord) CodexChannelInfo {
	a.mu.RLock()
	endpointMgr := a.codexEndpointMgr
	healthCheck := a.codexHealthCheck
	a.mu.RUnlock()

	activeChannelID := int64(0)
	if endpointMgr != nil {
		if active := endpointMgr.GetActiveChannel(); active != nil {
			activeChannelID = active.ID
		}
	}

	var latencies map[int64]int64
	var lastChecks map[int64]time.Time
	if healthCheck != nil {
		latencies = healthCheck.GetLatencies()
		lastChecks = healthCheck.GetLastChecks()
	}

	info := CodexChannelInfo{
		ID:              record.ID,
		Name:            record.Name,
		ProviderType:    record.ProviderType,
		ApiKey:          record.ApiKey,
		ApiKeyMasked:    maskToken(record.ApiKey),
		Model:           record.Model,
		ModelMapping:    copyCodexModelMapping(record.ModelMapping),
		ReasoningEffort: record.ReasoningEffort,
		Enable1MContext: record.Enable1MContext,
		Website:         record.Website,
		IsActive:        record.IsActive,
		Priority:        record.Priority,
		FailoverEnabled: record.FailoverEnabled,
		Endpoints:       make([]CodexEndpointInfo, 0, len(endpoints)),
	}
	info.CredentialGroups = make([]CodexCredentialGroupInfo, 0, len(credentialGroups))
	groupNames := make(map[int64]string, len(credentialGroups))
	for _, group := range credentialGroups {
		info.CredentialGroups = append(info.CredentialGroups, buildCodexCredentialGroupInfo(group))
		groupNames[group.ID] = group.Name
	}

	channelFailoverEnabled := a.config != nil && a.config.CodexFailover.Enabled
	for _, ep := range endpoints {
		healthy, healthState, healthInScope, responseTimeMS := codexEndpointRuntimeHealth(record, ep, endpointMgr, activeChannelID, channelFailoverEnabled, latencies)
		lastFailCode := 0
		lastFailReason := ""
		if endpointMgr != nil && healthInScope {
			lastFailCode, lastFailReason, _ = endpointMgr.GetFailureDetail(ep.ID)
		}
		info.Endpoints = append(info.Endpoints, CodexEndpointInfo{
			ID:                       ep.ID,
			ChannelID:                ep.ChannelID,
			CredentialGroupID:        ep.CredentialGroupID,
			CredentialGroupName:      groupNames[ep.CredentialGroupID],
			ApiKey:                   ep.ApiKey,
			ApiKeyMasked:             maskToken(ep.ApiKey),
			Name:                     ep.Name,
			BaseURL:                  ep.BaseURL,
			IsActive:                 ep.IsActive,
			FailoverEnabled:          ep.FailoverEnabled,
			SkipHealthCheck:          ep.SkipHealthCheck,
			Healthy:                  healthy,
			HealthState:              healthState,
			HealthInScope:            healthInScope,
			ResponseTimeMS:           responseTimeMS,
			LastCheck:                formatCodexTime(lastChecks[ep.ID]),
			LastFailCode:             lastFailCode,
			LastFailReason:           lastFailReason,
			Priority:                 ep.Priority,
			TimeoutSeconds:           ep.TimeoutSeconds,
			CostMultiplier:           ep.CostMultiplier,
			InputCostMultiplier:      ep.InputCostMultiplier,
			OutputCostMultiplier:     ep.OutputCostMultiplier,
			CacheReadCostMultiplier:  ep.CacheReadCostMultiplier,
			BillingOutputTokenSource: tracking.NormalizeBillingOutputTokenSource(ep.BillingOutputTokenSource),
			ModelMultipliers:         ep.ModelMultipliers,
			CreatedAt:                formatCodexTime(ep.CreatedAt),
			UpdatedAt:                formatCodexTime(ep.UpdatedAt),
		})
	}

	info.CreatedAt = formatCodexTime(record.CreatedAt)
	info.UpdatedAt = formatCodexTime(record.UpdatedAt)
	return info
}

func codexEndpointRuntimeHealth(
	channel *codexstore.CodexChannelRecord,
	endpoint *codexstore.CodexEndpointRecord,
	manager *codexproxy.EndpointManager,
	activeChannelID int64,
	channelFailoverEnabled bool,
	latencies map[int64]int64,
) (healthy bool, healthState string, healthInScope bool, responseTimeMS int64) {
	if channel == nil || endpoint == nil {
		return false, "unchecked", false, 0
	}
	channelInScope := channel.IsActive
	if channelFailoverEnabled {
		channelInScope = channel.FailoverEnabled
	}
	if !channelInScope || (!channelFailoverEnabled && manager != nil && activeChannelID != channel.ID) {
		return false, "inactive", false, 0
	}
	if !endpoint.FailoverEnabled {
		return false, "excluded", false, 0
	}
	latency, hasLatency := latencies[endpoint.ID]
	if latency > 0 {
		responseTimeMS = latency
	}
	if endpoint.SkipHealthCheck {
		return false, "skipped", true, responseTimeMS
	}

	if hasLatency && latency < 0 {
		return false, "unhealthy", true, responseTimeMS
	}
	if manager != nil && !manager.IsHealthy(endpoint.ID) {
		return false, "unhealthy", true, responseTimeMS
	}
	if hasLatency {
		return true, "healthy", true, responseTimeMS
	}
	return false, "unchecked", true, 0
}

func formatCodexTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

// ============================================================================
// 挂起请求相关 API
// ============================================================================

// GetCodexSuspendedCount 获取当前 Codex 挂起请求数（供前端展示警示色用）
// 当 loop_retry 全部耗尽且未被运维通过 UI 重新激活时，请求会进入挂起队列。
func (a *App) GetCodexSuspendedCount() int {
	a.mu.RLock()
	sm := a.codexSuspensionMgr
	a.mu.RUnlock()
	if sm == nil {
		return 0
	}
	return sm.GetSuspendedCount()
}

// broadcastCodexRecovery 唤醒所有挂起的 Codex 请求
// 由 ActivateCodexEndpoint / ActivateCodexChannel 在运维成功激活后调用
// 已激活端点/渠道的"重复点击"同样会触发此方法（运维干预语义）
func (a *App) broadcastCodexRecovery() {
	a.mu.RLock()
	sm := a.codexSuspensionMgr
	a.mu.RUnlock()
	if sm == nil {
		return
	}
	sm.BroadcastRecovery()
}
