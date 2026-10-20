package app

import (
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
)

type EndpointRecordInfo struct {
	ID                            int64                                       `json:"id"`
	Channel                       string                                      `json:"channel"`
	CredentialGroupID             int64                                       `json:"credential_group_id"`
	CredentialGroupName           string                                      `json:"credential_group_name"`
	Name                          string                                      `json:"name"`
	URL                           string                                      `json:"url"`
	Token                         string                                      `json:"token"`        // 本地桌面应用，直接返回原始 Token
	ApiKey                        string                                      `json:"api_key"`      // 本地桌面应用，直接返回原始 ApiKey
	TokenMasked                   string                                      `json:"token_masked"` // 脱敏后的 Token（列表展示用）
	ApiKeyMasked                  string                                      `json:"api_key_masked"`
	Headers                       map[string]string                           `json:"headers"`
	Priority                      int                                         `json:"priority"`
	FailoverEnabled               bool                                        `json:"failover_enabled"`
	SkipHealthCheck               bool                                        `json:"skip_health_check"` // 跳过健康检查
	CooldownSeconds               *int                                        `json:"cooldown_seconds"`
	TimeoutSeconds                int                                         `json:"timeout_seconds"`
	SupportsCountTokens           bool                                        `json:"supports_count_tokens"`
	CostMultiplier                float64                                     `json:"cost_multiplier"`
	InputCostMultiplier           float64                                     `json:"input_cost_multiplier"`
	OutputCostMultiplier          float64                                     `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier   float64                                     `json:"cache_creation_cost_multiplier"`
	CacheCreationCostMultiplier1h float64                                     `json:"cache_creation_cost_multiplier_1h"`
	CacheReadCostMultiplier       float64                                     `json:"cache_read_cost_multiplier"`
	ModelMultipliers              []claudestore.EndpointModelMultiplierRecord `json:"model_multipliers"`
	Enabled                       bool                                        `json:"enabled"`
	CreatedAt                     string                                      `json:"created_at"`
	UpdatedAt                     string                                      `json:"updated_at"`
	// 运行时健康状态
	Healthy        bool    `json:"healthy"`
	HealthState    string  `json:"health_state"`  // 健康状态枚举 (healthy/unhealthy/unchecked/skipped)
	NeverChecked   bool    `json:"never_checked"` // 是否从未检测过（跳过检测也保持为 true）
	LastCheck      string  `json:"last_check"`    // 最后健康检查时间
	ResponseTimeMs float64 `json:"response_time_ms"`
	// 错误信息
	LastFailCode   int    `json:"last_fail_code"`   // HTTP 状态码（0 表示无失败或未知）
	LastFailReason string `json:"last_fail_reason"` // 失败原因描述
	// 冷却状态（请求级故障转移）
	InCooldown     bool   `json:"in_cooldown"`     // 是否处于冷却中
	CooldownUntil  string `json:"cooldown_until"`  // 冷却截止时间
	CooldownReason string `json:"cooldown_reason"` // 冷却原因
}

type CreateEndpointInput struct {
	Channel                       string                                      `json:"channel"`
	CredentialGroupID             int64                                       `json:"credential_group_id"`
	Name                          string                                      `json:"name"`
	URL                           string                                      `json:"url"`
	Token                         string                                      `json:"token"`
	ApiKey                        string                                      `json:"api_key"`
	Headers                       map[string]string                           `json:"headers"`
	Priority                      int                                         `json:"priority"`
	FailoverEnabled               bool                                        `json:"failover_enabled"`
	SkipHealthCheck               bool                                        `json:"skip_health_check"` // 跳过健康检查
	CooldownSeconds               *int                                        `json:"cooldown_seconds"`
	TimeoutSeconds                int                                         `json:"timeout_seconds"`
	SupportsCountTokens           bool                                        `json:"supports_count_tokens"`
	CostMultiplier                float64                                     `json:"cost_multiplier"`
	InputCostMultiplier           float64                                     `json:"input_cost_multiplier"`
	OutputCostMultiplier          float64                                     `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier   float64                                     `json:"cache_creation_cost_multiplier"`
	CacheCreationCostMultiplier1h float64                                     `json:"cache_creation_cost_multiplier_1h"`
	CacheReadCostMultiplier       float64                                     `json:"cache_read_cost_multiplier"`
	ModelMultipliers              []claudestore.EndpointModelMultiplierRecord `json:"model_multipliers"`
}

type EndpointStorageStatus struct {
	Enabled      bool   `json:"enabled"`       // 是否启用 SQLite 存储
	StorageType  string `json:"storage_type"`  // 固定为 "sqlite"
	TotalCount   int    `json:"total_count"`   // 端点总数
	EnabledCount int    `json:"enabled_count"` // 已启用端点数
}
