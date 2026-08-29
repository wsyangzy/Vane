// app_api_storage.go - 端点存储管理 API (Wails Bindings)
// 提供 SQLite 端点存储的增删改查功能

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	claudeservice "ai-relay-forwarder/internal/providers/claude/service"
	claudestore "ai-relay-forwarder/internal/providers/claude/store"
)

// ============================================================
// 端点存储管理 API (SQLite)
// ============================================================
// 这些 API 仅在 endpoints_storage.type 为 "sqlite" 时可用
// 提供端点的增删改查功能

// EndpointRecordInfo 端点记录信息（给前端用的结构体）
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
	StreamOnly                    bool                                        `json:"stream_only"` // 仅支持流式请求（非流请求不会路由到此端点）
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

// CreateEndpointInput 创建端点的输入参数
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
	StreamOnly                    bool                                        `json:"stream_only"` // 仅支持流式请求（非流请求不会路由到此端点）
	CostMultiplier                float64                                     `json:"cost_multiplier"`
	InputCostMultiplier           float64                                     `json:"input_cost_multiplier"`
	OutputCostMultiplier          float64                                     `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier   float64                                     `json:"cache_creation_cost_multiplier"`
	CacheCreationCostMultiplier1h float64                                     `json:"cache_creation_cost_multiplier_1h"`
	CacheReadCostMultiplier       float64                                     `json:"cache_read_cost_multiplier"`
	ModelMultipliers              []claudestore.EndpointModelMultiplierRecord `json:"model_multipliers"`
}

type ClaudeCredentialGroupInfo struct {
	ID                                     int64                                       `json:"id"`
	Channel                                string                                      `json:"channel"`
	Name                                   string                                      `json:"name"`
	Token                                  string                                      `json:"token"`
	ApiKey                                 string                                      `json:"api_key"`
	TokenMasked                            string                                      `json:"token_masked"`
	ApiKeyMasked                           string                                      `json:"api_key_masked"`
	IsDefault                              bool                                        `json:"is_default"`
	DailyCostLimitUSD                      float64                                     `json:"daily_cost_limit_usd"`
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
	Channel                                string                                      `json:"channel"`
	Name                                   string                                      `json:"name"`
	Token                                  string                                      `json:"token"`
	ApiKey                                 string                                      `json:"api_key"`
	IsDefault                              bool                                        `json:"is_default"`
	DailyCostLimitUSD                      float64                                     `json:"daily_cost_limit_usd"`
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

// EndpointStorageStatus 端点存储状态
type EndpointStorageStatus struct {
	Enabled      bool   `json:"enabled"`       // 是否启用 SQLite 存储
	StorageType  string `json:"storage_type"`  // "yaml" 或 "sqlite"
	TotalCount   int    `json:"total_count"`   // 端点总数
	EnabledCount int    `json:"enabled_count"` // 已启用端点数
}

// GetEndpointStorageStatus 获取端点存储状态
func (a *App) GetEndpointStorageStatus() EndpointStorageStatus {
	a.mu.RLock()
	cfg := a.config
	endpointService := a.endpointService
	a.mu.RUnlock()

	status := EndpointStorageStatus{
		StorageType: "yaml", // 默认
	}

	if cfg != nil {
		status.StorageType = cfg.EndpointsStorage.Type
	}

	// 如果使用 SQLite 存储且服务已初始化
	if endpointService != nil {
		status.Enabled = true

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		records, err := endpointService.ListEndpoints(ctx)
		if err == nil {
			status.TotalCount = len(records)
			for _, r := range records {
				if r.Enabled {
					status.EnabledCount++
				}
			}
		}
	}

	return status
}

// GetEndpointRecords 获取所有端点记录（SQLite 存储）
func (a *App) GetEndpointRecords() ([]EndpointRecordInfo, error) {
	a.mu.RLock()
	endpointService := a.endpointService
	endpointManager := a.endpointManager
	a.mu.RUnlock()

	if endpointService == nil {
		return nil, fmt.Errorf("端点存储服务未启用 (需要设置 endpoints_storage.type: sqlite)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := endpointService.ListEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取端点列表失败: %w", err)
	}

	result := make([]EndpointRecordInfo, 0, len(records))
	for _, r := range records {
		info := a.recordToInfo(r)

		// 获取运行时健康状态
		if endpointManager != nil {
			status := endpointManager.GetEndpointStatus(claudeservice.EndpointRuntimeKey(r))
			info.Healthy = status.Healthy
			info.NeverChecked = status.NeverChecked
			info.ResponseTimeMs = float64(status.ResponseTime) / float64(time.Millisecond)
			// 格式化最后检查时间
			if !status.LastCheck.IsZero() {
				info.LastCheck = status.LastCheck.Format("2006-01-02 15:04:05")
			}
			// 统一健康状态枚举（用于前端统计/图表）
			if info.SkipHealthCheck {
				info.HealthState = "skipped"
				info.NeverChecked = true
			} else if status.NeverChecked {
				info.HealthState = "unchecked"
			} else if status.Healthy {
				info.HealthState = "healthy"
			} else {
				info.HealthState = "unhealthy"
			}
			// 填充错误信息
			info.LastFailCode = status.LastRequestFailCode
			info.LastFailReason = status.LastRequestFailReason
			// 冷却机制已移除
			info.InCooldown = false
			info.CooldownUntil = ""
			info.CooldownReason = ""
		}

		result = append(result, info)
	}

	return result, nil
}

// GetEndpointRecord 获取单个端点详情
func (a *App) GetEndpointRecord(name string) (EndpointRecordInfo, error) {
	a.mu.RLock()
	endpointService := a.endpointService
	a.mu.RUnlock()

	if endpointService == nil {
		return EndpointRecordInfo{}, fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	detail, err := endpointService.GetEndpointWithHealth(ctx, name)
	if err != nil {
		return EndpointRecordInfo{}, err
	}

	// 转换为 EndpointRecordInfo
	info := EndpointRecordInfo{
		Name:    name,
		Enabled: true,
	}

	// 从 map 中提取字段
	if v, ok := detail["id"].(int64); ok {
		info.ID = v
	}
	if v, ok := detail["channel"].(string); ok {
		info.Channel = v
	}
	if v, ok := detail["credential_group_id"].(int64); ok {
		info.CredentialGroupID = v
	}
	if v, ok := detail["credential_group_name"].(string); ok {
		info.CredentialGroupName = v
	}
	if v, ok := detail["url"].(string); ok {
		info.URL = v
	}
	if v, ok := detail["token_masked"].(string); ok {
		info.TokenMasked = v
	}
	if v, ok := detail["priority"].(int); ok {
		info.Priority = v
	}
	if v, ok := detail["failover_enabled"].(bool); ok {
		info.FailoverEnabled = v
	}
	if v, ok := detail["skip_health_check"].(bool); ok {
		info.SkipHealthCheck = v
	}
	if v, ok := detail["timeout_seconds"].(int); ok {
		info.TimeoutSeconds = v
	}
	if v, ok := detail["cost_multiplier"].(float64); ok {
		info.CostMultiplier = v
	}
	if v, ok := detail["input_cost_multiplier"].(float64); ok {
		info.InputCostMultiplier = v
	}
	if v, ok := detail["output_cost_multiplier"].(float64); ok {
		info.OutputCostMultiplier = v
	}
	if v, ok := detail["cache_creation_cost_multiplier"].(float64); ok {
		info.CacheCreationCostMultiplier = v
	}
	if v, ok := detail["cache_creation_cost_multiplier_1h"].(float64); ok {
		info.CacheCreationCostMultiplier1h = v
	}
	if v, ok := detail["cache_read_cost_multiplier"].(float64); ok {
		info.CacheReadCostMultiplier = v
	}
	if v, ok := detail["model_multipliers"].([]claudestore.EndpointModelMultiplierRecord); ok {
		info.ModelMultipliers = v
	}
	if v, ok := detail["enabled"].(bool); ok {
		info.Enabled = v
	}
	info.CreatedAt = formatEndpointTimeValue(detail["created_at"])
	info.UpdatedAt = formatEndpointTimeValue(detail["updated_at"])

	// 健康状态
	if health, ok := detail["health"].(map[string]interface{}); ok {
		if v, ok := health["healthy"].(bool); ok {
			info.Healthy = v
		}
		if v, ok := health["response_time_ms"].(int64); ok {
			info.ResponseTimeMs = float64(v)
		}
	}

	return info, nil
}

func formatEndpointTimeValue(value interface{}) string {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return ""
		}
		return v.Format("2006-01-02 15:04:05")
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

// CreateEndpointRecord 创建新端点
func (a *App) CreateEndpointRecord(input CreateEndpointInput) error {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	// 设置默认值（注意：Priority 由 service 层自动递增，这里不设默认值）
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 300
	}
	if input.CostMultiplier == 0 {
		input.CostMultiplier = 1.0
	}
	if input.CacheCreationCostMultiplier1h == 0 {
		input.CacheCreationCostMultiplier1h = 1.0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// SQLite 模式下 enabled 表示渠道当前是否活跃；向活跃渠道新增端点时应继承该状态。
	shouldEnableNewEndpoint := false
	if input.Channel != "" {
		existingEndpoints, err := endpointService.ListEndpointsByChannel(ctx, input.Channel)
		if err != nil {
			return fmt.Errorf("获取渠道端点状态失败: %w", err)
		}
		for _, endpoint := range existingEndpoints {
			if endpoint != nil && endpoint.Enabled {
				shouldEnableNewEndpoint = true
				break
			}
		}
	}

	record := &claudestore.EndpointRecord{
		Channel:                       input.Channel,
		CredentialGroupID:             input.CredentialGroupID,
		Name:                          input.Name,
		URL:                           input.URL,
		Token:                         input.Token,
		ApiKey:                        input.ApiKey,
		Headers:                       input.Headers,
		Priority:                      input.Priority,
		FailoverEnabled:               input.FailoverEnabled,
		SkipHealthCheck:               input.SkipHealthCheck,
		CooldownSeconds:               input.CooldownSeconds,
		TimeoutSeconds:                input.TimeoutSeconds,
		SupportsCountTokens:           input.SupportsCountTokens,
		StreamOnly:                    input.StreamOnly,
		CostMultiplier:                input.CostMultiplier,
		InputCostMultiplier:           input.InputCostMultiplier,
		OutputCostMultiplier:          input.OutputCostMultiplier,
		CacheCreationCostMultiplier:   input.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: input.CacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       input.CacheReadCostMultiplier,
		ModelMultipliers:              input.ModelMultipliers,
		Enabled:                       shouldEnableNewEndpoint,
	}

	// 若渠道表已启用，确保渠道已存在（兼容旧数据：允许端点创建时补齐渠道记录）
	if channelService != nil && input.Channel != "" {
		_ = channelService.EnsureChannel(ctx, input.Channel)
	}

	_, err := endpointService.CreateEndpoint(ctx, record)
	if err != nil {
		return fmt.Errorf("创建端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点已创建", "name", input.Name, "channel", input.Channel)
	}

	// endpointService.CreateEndpoint 已经将端点添加到内存并触发健康检测
	// 不需要再次调用 AddEndpoint，否则会导致 "端点已存在" 错误

	// 创建成功后，异步同步端点倍率到 UsageTracker
	go a.syncEndpointMultipliersToTracker(context.Background())

	return nil
}

// UpdateEndpointRecord 更新端点
func (a *App) UpdateEndpointRecord(name string, input CreateEndpointInput) error {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	oldName := strings.TrimSpace(name)
	if oldName == "" {
		return fmt.Errorf("端点名称不能为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// endpoints 允许不同渠道同名，旧接口仅靠 name 定位会产生歧义。
	// 当检测到 name 在多个渠道重复时，要求前端使用按 ID 更新接口。
	db := storeDB
	if db == nil && usageTracker != nil {
		db = usageTracker.GetDB()
	}
	if db != nil {
		var cnt int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM endpoints WHERE name = ?", oldName).Scan(&cnt); err == nil {
			if cnt > 1 {
				return fmt.Errorf("端点名称 '%s' 在多个渠道中重复，请升级客户端后使用按ID更新", oldName)
			}
		}
	}

	// 从数据库获取当前记录，用于保留敏感字段
	existingRecord, err := endpointService.GetEndpoint(ctx, oldName)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if existingRecord == nil {
		return fmt.Errorf("端点 '%s' 不存在", oldName)
	}

	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = existingRecord.Channel
	}
	newName := strings.TrimSpace(input.Name)
	if newName == "" {
		newName = existingRecord.Name
	}

	// 确保目标渠道存在（避免出现“孤儿渠道/端点”）
	if channelService != nil && channel != "" {
		_ = channelService.EnsureChannel(ctx, channel)
	}

	// 处理 Token: 如果前端传空值，保留原有 Token（防止误删）
	token := input.Token
	if token == "" {
		token = existingRecord.Token
	}

	// 处理 ApiKey: 如果前端传空值，保留原有 ApiKey（防止误删）
	apiKey := input.ApiKey
	if apiKey == "" {
		apiKey = existingRecord.ApiKey
	}

	// 兼容：前端未传 1h 倍率时，保留旧值
	cacheCreationCostMultiplier1h := input.CacheCreationCostMultiplier1h
	if cacheCreationCostMultiplier1h == 0 {
		cacheCreationCostMultiplier1h = existingRecord.CacheCreationCostMultiplier1h
	}
	modelMultipliers := input.ModelMultipliers
	if modelMultipliers == nil {
		modelMultipliers = existingRecord.ModelMultipliers
	}

	record := &claudestore.EndpointRecord{
		ID:                            existingRecord.ID,
		Channel:                       channel,
		CredentialGroupID:             resolveEndpointCredentialGroupID(input.CredentialGroupID, existingRecord, channel),
		Name:                          newName,
		URL:                           input.URL,
		Token:                         token,  // 空值时保留原有值
		ApiKey:                        apiKey, // 空值时保留原有值
		Headers:                       input.Headers,
		Priority:                      input.Priority,
		FailoverEnabled:               input.FailoverEnabled,
		SkipHealthCheck:               input.SkipHealthCheck,
		CooldownSeconds:               input.CooldownSeconds,
		TimeoutSeconds:                input.TimeoutSeconds,
		SupportsCountTokens:           input.SupportsCountTokens,
		StreamOnly:                    input.StreamOnly,
		CostMultiplier:                input.CostMultiplier,
		InputCostMultiplier:           input.InputCostMultiplier,
		OutputCostMultiplier:          input.OutputCostMultiplier,
		CacheCreationCostMultiplier:   input.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: cacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       input.CacheReadCostMultiplier,
		ModelMultipliers:              modelMultipliers,
		Enabled:                       existingRecord.Enabled, // 保持原有激活状态
	}

	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		// 提示更清晰的“同渠道同名”冲突信息
		msg := err.Error()
		// EndpointService 已返回用户可读的冲突原因（避免上层泛化覆盖）
		if strings.Contains(msg, "同一渠道内端点名称必须唯一") {
			return err
		}
		if strings.Contains(msg, "UNIQUE constraint failed: endpoints.channel, endpoints.name") ||
			strings.Contains(msg, "渠道 '") && strings.Contains(msg, " 已存在") {
			return fmt.Errorf("同一渠道 '%s' 下已存在端点名称 '%s'，请修改名称或选择其他渠道", channel, newName)
		}
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		if newName == existingRecord.Name && channel == existingRecord.Channel {
			logger.Info("✅ 端点已更新", "name", oldName)
		} else {
			logger.Info("✅ 端点已更新", "from", endpoint.EndpointKey(existingRecord.Channel, existingRecord.Name), "to", endpoint.EndpointKey(channel, newName))
		}
	}

	// best-effort：同步历史记录中的 endpoint_name（仅限“同一渠道内改名”，避免跨渠道误更新）
	if db != nil && existingRecord.Channel == channel && existingRecord.Name != newName {
		_, _ = db.ExecContext(ctx,
			"UPDATE request_logs SET endpoint_name = ? WHERE endpoint_name = ? AND (group_name = ? OR channel = ?)",
			newName, existingRecord.Name, channel, channel,
		)
		_, _ = db.ExecContext(ctx,
			"UPDATE usage_summary SET endpoint_name = ? WHERE endpoint_name = ? AND group_name = ?",
			newName, existingRecord.Name, channel,
		)
	}

	// 更新成功后，异步同步端点倍率到 UsageTracker
	// 确保成本计算使用最新的倍率配置
	go a.syncEndpointMultipliersToTracker(context.Background())

	// 推送前端刷新
	a.emitEndpointUpdate()

	return nil
}

// UpdateEndpointRecordByID 更新端点（按ID，避免同名歧义）
func (a *App) UpdateEndpointRecordByID(id int64, input CreateEndpointInput) error {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	existingRecord, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if existingRecord == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	db := storeDB
	if db == nil && usageTracker != nil {
		db = usageTracker.GetDB()
	}

	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = existingRecord.Channel
	}
	newName := strings.TrimSpace(input.Name)
	if newName == "" {
		newName = existingRecord.Name
	}

	// 确保目标渠道存在（避免出现“孤儿渠道/端点”）
	if channelService != nil && channel != "" {
		_ = channelService.EnsureChannel(ctx, channel)
	}

	// 处理 Token: 如果前端传空值，保留原有 Token（防止误删）
	token := input.Token
	if token == "" {
		token = existingRecord.Token
	}

	// 处理 ApiKey: 如果前端传空值，保留原有 ApiKey（防止误删）
	apiKey := input.ApiKey
	if apiKey == "" {
		apiKey = existingRecord.ApiKey
	}

	// 兼容：前端未传 1h 倍率时，保留旧值
	cacheCreationCostMultiplier1h := input.CacheCreationCostMultiplier1h
	if cacheCreationCostMultiplier1h == 0 {
		cacheCreationCostMultiplier1h = existingRecord.CacheCreationCostMultiplier1h
	}
	modelMultipliers := input.ModelMultipliers
	if modelMultipliers == nil {
		modelMultipliers = existingRecord.ModelMultipliers
	}

	record := &claudestore.EndpointRecord{
		ID:                            existingRecord.ID,
		Channel:                       channel,
		CredentialGroupID:             resolveEndpointCredentialGroupID(input.CredentialGroupID, existingRecord, channel),
		Name:                          newName,
		URL:                           input.URL,
		Token:                         token,  // 空值时保留原有值
		ApiKey:                        apiKey, // 空值时保留原有值
		Headers:                       input.Headers,
		Priority:                      input.Priority,
		FailoverEnabled:               input.FailoverEnabled,
		SkipHealthCheck:               input.SkipHealthCheck,
		CooldownSeconds:               input.CooldownSeconds,
		TimeoutSeconds:                input.TimeoutSeconds,
		SupportsCountTokens:           input.SupportsCountTokens,
		StreamOnly:                    input.StreamOnly,
		CostMultiplier:                input.CostMultiplier,
		InputCostMultiplier:           input.InputCostMultiplier,
		OutputCostMultiplier:          input.OutputCostMultiplier,
		CacheCreationCostMultiplier:   input.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: cacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       input.CacheReadCostMultiplier,
		ModelMultipliers:              modelMultipliers,
		Enabled:                       existingRecord.Enabled, // 保持原有激活状态
	}

	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "同一渠道内端点名称必须唯一") {
			return err
		}
		if strings.Contains(msg, "UNIQUE constraint failed: endpoints.channel, endpoints.name") ||
			(strings.Contains(msg, "渠道 '") && strings.Contains(msg, " 已存在")) {
			return fmt.Errorf("同一渠道 '%s' 下已存在端点名称 '%s'，请修改名称或选择其他渠道", channel, newName)
		}
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		if newName == existingRecord.Name && channel == existingRecord.Channel {
			logger.Info("✅ 端点已更新", "id", id, "name", newName)
		} else {
			logger.Info("✅ 端点已更新", "id", id, "from", endpoint.EndpointKey(existingRecord.Channel, existingRecord.Name), "to", endpoint.EndpointKey(channel, newName))
		}
	}

	// best-effort：同步历史记录中的 endpoint_name（仅限“同一渠道内改名”，避免跨渠道误更新）
	if db != nil && existingRecord.Channel == channel && existingRecord.Name != newName {
		_, _ = db.ExecContext(ctx,
			"UPDATE request_logs SET endpoint_name = ? WHERE endpoint_name = ? AND (group_name = ? OR channel = ?)",
			newName, existingRecord.Name, channel, channel,
		)
		_, _ = db.ExecContext(ctx,
			"UPDATE usage_summary SET endpoint_name = ? WHERE endpoint_name = ? AND group_name = ?",
			newName, existingRecord.Name, channel,
		)
	}

	go a.syncEndpointMultipliersToTracker(context.Background())
	a.emitEndpointUpdate()

	return nil
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

// SetEndpointFailoverEnabled 设置端点是否参与故障转移（快捷开关）
// 仅影响当前端点所在渠道内的候选端点集合。
func (a *App) SetEndpointFailoverEnabled(name string, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record, err := endpointService.GetEndpoint(ctx, name)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点 '%s' 不存在", name)
	}

	record.FailoverEnabled = enabled
	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点故障转移参与状态已更新", "name", name, "enabled", enabled)
	}

	return nil
}

// SetEndpointFailoverEnabledByID 设置端点是否参与故障转移（按ID，避免同名歧义）
func (a *App) SetEndpointFailoverEnabledByID(id int64, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	record.FailoverEnabled = enabled
	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		return fmt.Errorf("更新端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点故障转移参与状态已更新", "id", id, "name", record.Name, "enabled", enabled)
	}

	a.emitEndpointUpdate()
	return nil
}

// SetEndpointSkipHealthCheckByID 设置端点是否跳过健康检测（按ID，避免同名歧义）
// 新增快捷开关
func (a *App) SetEndpointSkipHealthCheckByID(id int64, skip bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	record.SkipHealthCheck = skip
	if err := endpointService.UpdateEndpoint(ctx, record); err != nil {
		return fmt.Errorf("更新端点失败: %w", err)
	}

	// 同步到运行时 endpoint manager
	if endpointManager != nil {
		endpointManager.SetEndpointSkipHealthCheck(claudeservice.EndpointRuntimeKey(record), skip)
	}

	if logger != nil {
		logger.Info("✅ 端点跳过健康检测状态已更新", "id", id, "name", record.Name, "skip", skip)
	}

	a.emitEndpointUpdate()
	return nil
}

// DeleteEndpointRecord 删除端点
func (a *App) DeleteEndpointRecord(name string) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := endpointService.DeleteEndpoint(ctx, name); err != nil {
		return fmt.Errorf("删除端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点已删除", "name", name)
	}

	// 删除成功后，异步同步端点倍率到 UsageTracker
	go a.syncEndpointMultipliersToTracker(context.Background())

	return nil
}

// DeleteEndpointRecordByID 删除端点（按ID，避免同名歧义）
func (a *App) DeleteEndpointRecordByID(id int64) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := endpointService.DeleteEndpointByID(ctx, id); err != nil {
		return fmt.Errorf("删除端点失败: %w", err)
	}

	if logger != nil {
		logger.Info("✅ 端点已删除", "id", id)
	}

	go a.syncEndpointMultipliersToTracker(context.Background())
	a.emitEndpointUpdate()
	return nil
}

// ToggleEndpointRecord 切换端点启用状态
// 切换语义升级为“激活/停用渠道(channel)”
func (a *App) ToggleEndpointRecord(name string, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if enabled {
		// 激活：以端点所属渠道为单位
		record, err := endpointService.GetEndpoint(ctx, name)
		if err != nil {
			return fmt.Errorf("获取端点配置失败: %w", err)
		}
		if record == nil {
			return fmt.Errorf("端点 '%s' 不存在", name)
		}
		if err := endpointService.ActivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("激活渠道失败: %w", err)
		}
	} else {
		// 停用：以端点所属渠道为单位
		record, err := endpointService.GetEndpoint(ctx, name)
		if err != nil {
			return fmt.Errorf("获取端点配置失败: %w", err)
		}
		if record == nil {
			return fmt.Errorf("端点 '%s' 不存在", name)
		}
		if err := endpointService.DeactivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("停用渠道失败: %w", err)
		}
	}

	status := map[bool]string{true: "启用", false: "禁用"}[enabled]

	if logger != nil {
		logger.Info("✅ 端点状态已切换", "name", name, "status", status)
	}

	return nil
}

// ToggleEndpointRecordByID 切换端点启用状态（按ID，避免同名歧义）
// 切换语义升级为“激活/停用渠道(channel)”
func (a *App) ToggleEndpointRecordByID(id int64, enabled bool) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record, err := endpointService.GetEndpointByID(ctx, id)
	if err != nil {
		return fmt.Errorf("获取端点配置失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	if enabled {
		if err := endpointService.ActivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("激活渠道失败: %w", err)
		}
	} else {
		if err := endpointService.DeactivateChannel(ctx, record.Channel); err != nil {
			return fmt.Errorf("停用渠道失败: %w", err)
		}
	}

	status := map[bool]string{true: "启用", false: "禁用"}[enabled]
	if logger != nil {
		logger.Info("✅ 端点状态已切换", "id", id, "channel", record.Channel, "name", record.Name, "status", status)
	}

	a.emitEndpointUpdate()
	return nil
}

// ActivateEndpointRecordByID 将指定端点设为当前渠道首选端点（按ID，避免同名歧义）。
func (a *App) ActivateEndpointRecordByID(id int64) error {
	a.mu.RLock()
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if endpointService == nil {
		return fmt.Errorf("端点存储服务未启用")
	}
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	record, err := endpointService.ActivateEndpointByID(ctx, id)
	if err != nil {
		return err
	}

	if logger != nil && record != nil {
		logger.Info("✅ 当前端点已切换", "id", id, "channel", record.Channel, "name", record.Name)
	}

	a.emitEndpointUpdate()
	return nil
}

// ChannelInfo 渠道信息
type ChannelInfo struct {
	Name             string                      `json:"name"`
	Website          string                      `json:"website,omitempty"`
	Priority         int                         `json:"priority"`
	ModelMapping     map[string]string           `json:"model_mapping"`
	CreatedAt        string                      `json:"created_at"`
	EndpointCount    int                         `json:"endpoint_count"`
	CredentialGroups []ClaudeCredentialGroupInfo `json:"credential_groups"`
}

// GetChannels 获取所有渠道
func (a *App) GetChannels() ([]ChannelInfo, error) {
	a.mu.RLock()
	endpointService := a.endpointService
	channelService := a.channelService
	usageTracker := a.usageTracker
	claudeCredentialGroupService := a.claudeCredentialGroupService
	claudeCredentialGroupStore := a.claudeCredentialGroupStore
	a.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	channelCount := make(map[string]int)
	// 统计每个渠道的端点数（兼容：端点表中存在但 channels 表未收录的渠道）
	// 优先用 EndpointService（字段最全）；若因旧库缺字段导致失败，则降级为聚合 SQL（只依赖 channel 列）
	if endpointService != nil {
		records, err := endpointService.ListEndpoints(ctx)
		if err == nil {
			for _, r := range records {
				if r.Channel == "" {
					continue
				}
				channelCount[r.Channel]++
			}
		} else if usageTracker != nil && usageTracker.GetDB() != nil {
			rows, qerr := usageTracker.GetDB().QueryContext(ctx, "SELECT channel, COUNT(*) FROM endpoints GROUP BY channel")
			if qerr == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					var count int
					if err := rows.Scan(&name, &count); err != nil {
						continue
					}
					if name == "" {
						continue
					}
					channelCount[name] = count
				}
			}
		}
	}

	channelWebsite := make(map[string]string)
	channelPriority := make(map[string]int)
	channelModelMapping := make(map[string]map[string]string)
	channelCreatedAt := make(map[string]string)
	orderedChannels := make([]string, 0, 16)
	if channelService != nil {
		channelRecords, err := channelService.ListChannels(ctx)
		if err != nil {
			return nil, fmt.Errorf("获取渠道列表失败: %w", err)
		}
		for _, c := range channelRecords {
			if c == nil || c.Name == "" {
				continue
			}
			channelWebsite[c.Name] = c.Website
			if c.Priority <= 0 {
				channelPriority[c.Name] = 1
			} else {
				channelPriority[c.Name] = c.Priority
			}
			channelModelMapping[c.Name] = c.ModelMapping
			if !c.CreatedAt.IsZero() {
				channelCreatedAt[c.Name] = c.CreatedAt.Format("2006-01-02 15:04:05")
			}
			if _, ok := channelCount[c.Name]; !ok {
				channelCount[c.Name] = 0
			}
			orderedChannels = append(orderedChannels, c.Name)
		}
	}

	seen := make(map[string]struct{}, len(channelCount))
	result := make([]ChannelInfo, 0, len(channelCount))

	// 1) channels 表中的渠道（已按 store 层排序），优先输出，保证 UI 顺序稳定
	for _, name := range orderedChannels {
		count := channelCount[name]
		credentialGroups := a.listClaudeCredentialGroupInfos(ctx, claudeCredentialGroupService, claudeCredentialGroupStore, name)
		result = append(result, ChannelInfo{
			Name:             name,
			Website:          channelWebsite[name],
			Priority:         channelPriority[name],
			ModelMapping:     channelModelMapping[name],
			CreatedAt:        channelCreatedAt[name],
			EndpointCount:    count,
			CredentialGroups: credentialGroups,
		})
		seen[name] = struct{}{}
	}

	// 2) 兼容：端点表里存在但 channels 表未收录的渠道（作为兜底）
	for name, count := range channelCount {
		if _, ok := seen[name]; ok {
			continue
		}
		credentialGroups := a.listClaudeCredentialGroupInfos(ctx, claudeCredentialGroupService, claudeCredentialGroupStore, name)
		result = append(result, ChannelInfo{
			Name:             name,
			Website:          channelWebsite[name],
			Priority:         999,
			ModelMapping:     channelModelMapping[name],
			CreatedAt:        "",
			EndpointCount:    count,
			CredentialGroups: credentialGroups,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		pi := result[i].Priority
		pj := result[j].Priority
		if pi <= 0 {
			pi = 1
		}
		if pj <= 0 {
			pj = 1
		}
		if pi != pj {
			return pi < pj
		}
		// 同优先级：按创建时间倒序（新创建排在前面）
		if result[i].CreatedAt != result[j].CreatedAt {
			return result[i].CreatedAt > result[j].CreatedAt
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
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
		ID:                                     group.ID,
		Channel:                                group.Channel,
		Name:                                   group.Name,
		Token:                                  group.Token,
		ApiKey:                                 group.ApiKey,
		TokenMasked:                            maskToken(group.Token),
		ApiKeyMasked:                           maskToken(group.ApiKey),
		IsDefault:                              group.IsDefault,
		DailyCostLimitUSD:                      group.DailyCostLimitUSD,
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
		Channel:                                input.Channel,
		Name:                                   input.Name,
		Token:                                  input.Token,
		ApiKey:                                 input.ApiKey,
		IsDefault:                              input.IsDefault,
		DailyCostLimitUSD:                      input.DailyCostLimitUSD,
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
	if input.ModelMultipliers == nil {
		if existing, err := svc.GetByID(ctx, id); err == nil && existing != nil {
			record.ModelMultipliers = existing.ModelMultipliers
		}
	}
	if err := svc.UpdateGroup(ctx, id, record); err != nil {
		return ClaudeCredentialGroupInfo{}, err
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
	if err := svc.DeleteGroup(ctx, id); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("✅ Claude 凭证分组已删除", "id", id)
	}
	a.refreshClaudeRuntimeAfterCredentialGroupChange(ctx)
	return nil
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

type CreateChannelInput struct {
	Name         string            `json:"name"`
	Website      string            `json:"website,omitempty"`
	Priority     int               `json:"priority"`
	ModelMapping map[string]string `json:"model_mapping"`
}

func (a *App) CreateChannel(input CreateChannelInput) error {
	a.mu.RLock()
	channelService := a.channelService
	claudeCredentialGroupService := a.claudeCredentialGroupService
	logger := a.logger
	a.mu.RUnlock()

	if channelService == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Website = strings.TrimSpace(input.Website)
	if input.Name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}
	// Priority 由 service 层处理（自动递增或冲突交换）

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := channelService.CreateChannel(ctx, &claudestore.ChannelRecord{
		Name:         input.Name,
		Website:      input.Website,
		Priority:     input.Priority,
		ModelMapping: input.ModelMapping,
	})
	if err != nil {
		return err
	}
	if claudeCredentialGroupService != nil {
		if _, err := claudeCredentialGroupService.EnsureDefaultGroup(ctx, input.Name); err != nil && logger != nil {
			logger.Warn("⚠️ 创建渠道默认凭证分组失败", "channel", input.Name, "error", err)
		}
	}
	if logger != nil {
		logger.Info("✅ 渠道已创建", "name", input.Name)
	}

	// 同步渠道优先级到运行时（用于渠道间故障转移顺序）
	if syncErr := a.syncChannelRuntimeState(ctx); syncErr != nil {
		retryCtx, retryCancel := context.WithTimeout(context.Background(), 3*time.Second)
		retryErr := a.syncChannelRuntimeState(retryCtx)
		retryCancel()
		if retryErr != nil && logger != nil {
			logger.Warn("⚠️ 渠道已创建，但运行时状态同步失败；将在后续更新或重启时重试",
				"channel", input.Name,
				"error", syncErr,
				"retry_error", retryErr,
			)
		}
	}
	return nil
}

type UpdateChannelInput struct {
	Name         string            `json:"name"`
	Website      string            `json:"website,omitempty"`
	Priority     int               `json:"priority"`
	ModelMapping map[string]string `json:"model_mapping"`
}

func (a *App) UpdateChannel(input UpdateChannelInput) error {
	a.mu.RLock()
	channelService := a.channelService
	logger := a.logger
	a.mu.RUnlock()

	if channelService == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}

	input.Name = strings.TrimSpace(input.Name)
	input.Website = strings.TrimSpace(input.Website)
	if input.Name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}
	// Priority 由 service 层处理（自动递增或冲突交换）

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := channelService.UpdateChannel(ctx, &claudestore.ChannelRecord{
		Name:         input.Name,
		Website:      input.Website,
		Priority:     input.Priority,
		ModelMapping: input.ModelMapping,
	}); err != nil {
		return err
	}

	if err := a.syncChannelRuntimeState(ctx); err != nil {
		return fmt.Errorf("渠道已更新，但同步运行时状态失败: %w", err)
	}

	// 推送前端刷新（channelsMeta & groups）
	a.emitEndpointUpdate()

	if logger != nil {
		logger.Info("✅ 渠道已更新", "name", input.Name, "priority", input.Priority)
	}
	return nil
}

func (a *App) syncChannelRuntimeState(ctx context.Context) error {
	a.mu.RLock()
	endpointManager := a.endpointManager
	a.mu.RUnlock()
	if endpointManager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	if err := a.syncChannelPrioritiesToEndpointManager(ctx); err != nil {
		return err
	}
	if err := a.syncChannelFailoverEnabledToEndpointManager(ctx); err != nil {
		return err
	}
	a.syncChannelModelMappingsToEndpointManager(ctx)
	return nil
}

func (a *App) syncChannelPrioritiesToEndpointManager(ctx context.Context) error {
	a.mu.RLock()
	channelStore := a.channelStore
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()

	if channelStore == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	if endpointManager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	baseCtx := ctx
	if baseCtx == nil || baseCtx.Err() != nil {
		baseCtx = context.Background()
	}

	syncCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	defer cancel()

	records, err := channelStore.List(syncCtx)
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步渠道优先级失败", "error", err)
		}
		return fmt.Errorf("同步渠道优先级失败: %w", err)
	}

	priorities := make(map[string]int, len(records))
	for _, r := range records {
		if r == nil || r.Name == "" {
			continue
		}
		p := r.Priority
		if p <= 0 {
			p = 1
		}
		priorities[r.Name] = p
	}
	endpointManager.UpdateChannelPriorities(priorities)
	return nil
}

func (a *App) syncChannelFailoverEnabledToEndpointManager(ctx context.Context) error {
	a.mu.RLock()
	channelStore := a.channelStore
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()

	if channelStore == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	if endpointManager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	baseCtx := ctx
	if baseCtx == nil || baseCtx.Err() != nil {
		baseCtx = context.Background()
	}

	syncCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	defer cancel()

	records, err := channelStore.List(syncCtx)
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步渠道故障转移开关失败", "error", err)
		}
		return fmt.Errorf("同步渠道故障转移开关失败: %w", err)
	}

	enabled := make(map[string]bool, len(records))
	for _, r := range records {
		if r == nil || r.Name == "" {
			continue
		}
		enabled[r.Name] = r.FailoverEnabled
	}
	endpointManager.UpdateChannelFailoverEnabled(enabled)
	return nil
}

func (a *App) syncChannelModelMappingsToEndpointManager(ctx context.Context) {
	a.mu.RLock()
	channelStore := a.channelStore
	endpointManager := a.endpointManager
	logger := a.logger
	a.mu.RUnlock()

	if channelStore == nil || endpointManager == nil {
		return
	}

	baseCtx := ctx
	if baseCtx == nil || baseCtx.Err() != nil {
		baseCtx = context.Background()
	}

	syncCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	defer cancel()

	records, err := channelStore.List(syncCtx)
	if err != nil {
		if logger != nil {
			logger.Warn("⚠️ 同步渠道模型映射失败", "error", err)
		}
		return
	}

	mappings := make(map[string]map[string]string, len(records))
	for _, r := range records {
		if r == nil || r.Name == "" || len(r.ModelMapping) == 0 {
			continue
		}
		mappings[r.Name] = r.ModelMapping
	}
	endpointManager.UpdateChannelModelMappings(mappings)
}

// DeleteChannel 删除渠道
// deleteEndpoints=true 时会一并删除该渠道下所有端点。
func (a *App) DeleteChannel(name string, deleteEndpoints bool) error {
	a.mu.RLock()
	channelStore := a.channelStore
	channelService := a.channelService
	endpointStore := a.endpointStore
	endpointService := a.endpointService
	claudeCredentialGroupStore := a.claudeCredentialGroupStore
	usageTracker := a.usageTracker
	storeDB := a.storeDB
	logger := a.logger
	a.mu.RUnlock()

	if channelService == nil || channelStore == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	if usageTracker == nil || usageTracker.GetDB() == nil {
		if storeDB == nil {
			return fmt.Errorf("数据库未初始化")
		}
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db := storeDB
	if db == nil && usageTracker != nil {
		db = usageTracker.GetDB()
	}
	if db == nil {
		return fmt.Errorf("数据库未初始化")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if endpointStore == nil {
		endpointStore = claudestore.NewSQLiteEndpointStore(db)
	}
	endpointStore = endpointStore.WithTx(tx)
	channelStore = channelStore.WithTx(tx)
	if claudeCredentialGroupStore != nil {
		claudeCredentialGroupStore = claudeCredentialGroupStore.WithTx(tx)
	}

	// 端点检查/删除
	endpoints, err := endpointStore.ListByChannel(ctx, name)
	if err != nil {
		return fmt.Errorf("获取渠道端点失败: %w", err)
	}
	if len(endpoints) > 0 && !deleteEndpoints {
		return fmt.Errorf("渠道 '%s' 下仍有 %d 个端点，请勾选一并删除", name, len(endpoints))
	}
	if len(endpoints) > 0 {
		names := make([]string, 0, len(endpoints))
		for _, ep := range endpoints {
			if ep != nil && ep.Name != "" {
				names = append(names, ep.Name)
			}
		}
		if len(names) > 0 {
			if err := endpointStore.BatchDelete(ctx, names); err != nil {
				return fmt.Errorf("删除渠道端点失败: %w", err)
			}
		}
	}
	if claudeCredentialGroupStore != nil {
		if err := claudeCredentialGroupStore.DeleteByChannel(ctx, name); err != nil {
			return err
		}
	}

	// 删除渠道
	if err := channelStore.Delete(ctx, name); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	// 同步运行时配置（删除端点/组）
	if endpointService != nil {
		if err := endpointService.SyncFromDatabase(ctx); err != nil && logger != nil {
			logger.Warn("⚠️ 删除渠道后同步运行时配置失败", "error", err)
		}
	}
	a.syncChannelModelMappingsToEndpointManager(ctx)

	// 推送前端刷新
	a.emitEndpointUpdate()

	if logger != nil {
		logger.Info("✅ 渠道已删除", "name", name, "delete_endpoints", deleteEndpoints)
	}

	return nil
}

// GetEndpointsByChannel 按渠道获取端点
func (a *App) GetEndpointsByChannel(channel string) ([]EndpointRecordInfo, error) {
	a.mu.RLock()
	endpointService := a.endpointService
	endpointManager := a.endpointManager
	a.mu.RUnlock()

	if endpointService == nil {
		return nil, fmt.Errorf("端点存储服务未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	records, err := endpointService.ListEndpointsByChannel(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("获取渠道端点列表失败: %w", err)
	}

	result := make([]EndpointRecordInfo, 0, len(records))
	for _, r := range records {
		info := a.recordToInfo(r)

		// 获取运行时健康状态
		if endpointManager != nil {
			status := endpointManager.GetEndpointStatus(claudeservice.EndpointRuntimeKey(r))
			info.Healthy = status.Healthy
			info.ResponseTimeMs = float64(status.ResponseTime) / float64(time.Millisecond)
			// 格式化最后检查时间
			if !status.LastCheck.IsZero() {
				info.LastCheck = status.LastCheck.Format("2006-01-02 15:04:05")
			}
			// 冷却机制已移除
			info.InCooldown = false
			info.CooldownUntil = ""
			info.CooldownReason = ""
		}

		result = append(result, info)
	}

	return result, nil
}

// recordToInfo 将数据库记录转换为前端 Info 结构
func (a *App) recordToInfo(r *claudestore.EndpointRecord) EndpointRecordInfo {
	info := EndpointRecordInfo{
		ID:                            r.ID,
		Channel:                       r.Channel,
		CredentialGroupID:             r.CredentialGroupID,
		CredentialGroupName:           r.CredentialGroupName,
		Name:                          r.Name,
		URL:                           r.URL,
		Token:                         r.Token,  // 本地桌面应用，直接返回原始 Token
		ApiKey:                        r.ApiKey, // 本地桌面应用，直接返回原始 ApiKey
		TokenMasked:                   maskToken(r.Token),
		ApiKeyMasked:                  maskToken(r.ApiKey),
		Headers:                       r.Headers,
		Priority:                      r.Priority,
		FailoverEnabled:               r.FailoverEnabled,
		SkipHealthCheck:               r.SkipHealthCheck,
		CooldownSeconds:               r.CooldownSeconds,
		TimeoutSeconds:                r.TimeoutSeconds,
		SupportsCountTokens:           r.SupportsCountTokens,
		StreamOnly:                    r.StreamOnly,
		CostMultiplier:                r.CostMultiplier,
		InputCostMultiplier:           r.InputCostMultiplier,
		OutputCostMultiplier:          r.OutputCostMultiplier,
		CacheCreationCostMultiplier:   r.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: r.CacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       r.CacheReadCostMultiplier,
		ModelMultipliers:              r.ModelMultipliers,
		Enabled:                       r.Enabled,
		HealthState:                   "",
		NeverChecked:                  false,
	}

	if !r.CreatedAt.IsZero() {
		info.CreatedAt = r.CreatedAt.Format("2006-01-02 15:04:05")
	}
	if !r.UpdatedAt.IsZero() {
		info.UpdatedAt = r.UpdatedAt.Format("2006-01-02 15:04:05")
	}

	return info
}

// maskToken Token 脱敏显示
func maskToken(token string) string {
	if token == "" {
		return "" // 空 token 返回空字符串，不显示"已配置"
	}
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}
