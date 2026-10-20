package tracking

import (
	"log/slog"
	"strings"

	_ "modernc.org/sqlite"
)

// UpdatePricing 更新模型定价（运行时动态更新）
func (ut *UsageTracker) UpdatePricing(pricing map[string]ModelPricing) {
	ut.mu.Lock()
	defer ut.mu.Unlock()

	ut.pricing = pricing

	// 同步到 ArchiveManager
	if ut.archiveManager != nil {
		ut.archiveManager.UpdatePricing(pricing)
	}

	slog.Info("Model pricing updated", "model_count", len(pricing))
}

// UpdateEndpointMultipliers 更新端点成本倍率
// 成本计算公式：模型基础定价 * 端点倍率
func (ut *UsageTracker) UpdateEndpointMultipliers(multipliers map[string]EndpointMultiplier) {
	ut.mu.Lock()
	defer ut.mu.Unlock()

	ut.endpointMu = multipliers

	// 同步到 ArchiveManager
	if ut.archiveManager != nil {
		ut.archiveManager.UpdateEndpointMultipliers(multipliers)
	}

	slog.Info("Endpoint multipliers updated", "endpoint_count", len(multipliers))
}

// GetEndpointMultiplier 获取端点倍率（用于成本计算）
func (ut *UsageTracker) GetEndpointMultiplier(endpointName string) EndpointMultiplier {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	if ut.endpointMu == nil {
		return DefaultEndpointMultiplier()
	}

	if m, exists := ut.endpointMu[endpointName]; exists {
		return m
	}

	// 返回默认倍率 1.0
	return DefaultEndpointMultiplier()
}

func (ut *UsageTracker) GetEndpointMultiplierForEndpoint(provider, channelName, endpointName string) EndpointMultiplier {
	return ut.GetEndpointMultiplierForEndpointWithGroup(provider, channelName, "", endpointName)
}

func (ut *UsageTracker) GetEndpointMultiplierForEndpointWithGroup(provider, channelName, groupName, endpointName string) EndpointMultiplier {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	if ut.endpointMu == nil {
		return DefaultEndpointMultiplier()
	}
	for _, key := range endpointMultiplierLookupKeys(provider, channelName, groupName, endpointName) {
		if m, exists := ut.endpointMu[key]; exists {
			return m
		}
	}
	return DefaultEndpointMultiplier()
}

func (ut *UsageTracker) GetEndpointMultiplierForEndpointAndModel(provider, channelName, endpointName, modelName string) EndpointMultiplier {
	return ut.GetEndpointMultiplierForEndpointAndModelWithGroup(provider, channelName, "", endpointName, modelName)
}

func (ut *UsageTracker) GetEndpointMultiplierForEndpointAndModelWithGroup(provider, channelName, groupName, endpointName, modelName string) EndpointMultiplier {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	if ut.endpointMu == nil {
		return DefaultEndpointMultiplier()
	}
	for _, key := range endpointMultiplierLookupKeys(provider, channelName, groupName, endpointName) {
		if m, exists := ut.endpointMu[key]; exists {
			return ResolveEndpointMultiplierForModel(m, modelName)
		}
	}
	return DefaultEndpointMultiplier()
}

// GetPricing 获取模型定价
// GetPricing 按模型名获取定价（legacy 入口，不带 provider 与档位维度）。
//
// 仅供拿不到 provider 上下文的降级路径使用：失败请求的 token 恢复、成本兜底估算
// （RequestCompleteData 只有 ModelName）。这些路径同样不带倍率（multiplier 传 nil），
// 其结果会在热池归档时被 archive_manager.calculateCost 的完整计算覆盖。
//
// 跨 provider 同名模型在此取第一个匹配（claude 优先）。要精确定价请走
// modelPricingLookupKeys 并透传 provider 与 service tier。
func (ut *UsageTracker) GetPricing(modelName string) ModelPricing {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	for _, key := range []string{
		ModelPricingKey("claude", modelName, ""),
		ModelPricingKey("codex", modelName, ""),
		strings.TrimSpace(modelName),
	} {
		if key == "" {
			continue
		}
		if pricing, exists := ut.pricing[key]; exists {
			return pricing
		}
	}
	return ut.config.DefaultPricing
}

// GetConfiguredModels 获取配置中的所有模型列表
func (ut *UsageTracker) GetConfiguredModels() []string {
	ut.mu.RLock()
	defer ut.mu.RUnlock()

	models := make([]string, 0, len(ut.pricing))
	for modelName := range ut.pricing {
		models = append(models, modelName)
	}

	return models
}

// calculateCost 计算请求成本
func (ut *UsageTracker) calculateCost(modelName string, tokens *TokenUsage) (inputCost, outputCost, cacheCost, readCost, totalCost float64) {
	pricing := ut.GetPricing(modelName)

	cost := CalculateCost(tokens, &pricing, nil)
	inputCost = cost.InputCost
	outputCost = cost.OutputCost
	cacheCost = cost.CacheCreationCost
	readCost = cost.CacheReadCost
	totalCost = cost.TotalCost

	return
}
