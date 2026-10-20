package app

import (
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
)

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

type CodexCredentialGroupInfo struct {
	AuthMode                               string                                  `json:"auth_mode"`
	Subscription                           *subscription.Binding                   `json:"subscription,omitempty"`
	LocalKeyConfigured                     bool                                    `json:"local_key_configured"`
	SubscriptionStatus                     []subscription.Pause                    `json:"subscription_status"`
	OfficialVerification                   string                                  `json:"official_verification"`
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
	AuthMode                               string                                  `json:"auth_mode"`
	Subscription                           *subscription.Binding                   `json:"subscription,omitempty"`
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
	AuthMode                               string                                  `json:"auth_mode"`
	Subscription                           *subscription.Binding                   `json:"subscription,omitempty"`
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
