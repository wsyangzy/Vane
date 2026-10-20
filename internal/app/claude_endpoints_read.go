package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
)

func (a *App) GetEndpointStorageStatus() EndpointStorageStatus {
	a.mu.RLock()
	endpointService := a.endpointService
	a.mu.RUnlock()

	status := EndpointStorageStatus{
		StorageType: "sqlite",
	}

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

func (a *App) GetEndpointRecords() ([]EndpointRecordInfo, error) {
	a.mu.RLock()
	endpointService := a.endpointService
	endpointManager := a.endpointManager
	a.mu.RUnlock()

	if endpointService == nil {
		return nil, fmt.Errorf("端点存储服务未初始化")
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

func maskToken(token string) string {
	if token == "" {
		return "" // 空 token 返回空字符串，不显示"已配置"
	}
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}
