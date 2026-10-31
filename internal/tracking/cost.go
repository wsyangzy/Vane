package tracking

import (
	_ "modernc.org/sqlite"
)

// CostBreakdown 成本分解结构
type CostBreakdown struct {
	InputCost           float64
	OutputCost          float64
	CacheCreationCost   float64 // 总缓存创建成本 (5m + 1h)
	CacheCreation5mCost float64 // 5分钟缓存创建成本
	CacheCreation1hCost float64 // 1小时缓存创建成本
	CacheReadCost       float64
	TotalCost           float64
}

// CalculateCost 统一的成本计算函数（支持分开的缓存定价）
// 消除 archive_manager.go 和 tracker.go 中的重复代码
//
// 参数:
//   - usage: Token 使用量（包含分开的 5m/1h 缓存 tokens）
//   - pricing: 模型定价，nil 时返回零成本
//   - multiplier: 端点倍率，nil 时使用默认倍率 1.0
func CalculateCost(usage *TokenUsage, pricing *ModelPricing, multiplier *EndpointMultiplier) CostBreakdown {
	if pricing == nil || usage == nil {
		return CostBreakdown{}
	}

	// 使用默认倍率
	var m EndpointMultiplier
	if multiplier != nil {
		m = *multiplier
	} else {
		m = DefaultEndpointMultiplier()
	}

	// 单价选择与分项倍率合并统一由 resolveBillingFactors 负责，计费快照复用同一份，
	// 避免 long context 判定在两处漂移
	f := resolveBillingFactors(usage, pricing, &m)

	inputTokensForCost := usage.InputTokens
	if usage.InputIncludesCacheRead {
		inputTokensForCost -= min(inputTokensForCost, max(usage.CacheReadTokens, int64(0)))
		inputTokensForCost -= min(inputTokensForCost, max(usage.CacheCreationTokens, int64(0)))
	}

	// 基础成本计算（每百万 token）× 分项倍率
	inputCost := float64(inputTokensForCost) * f.inputPrice / 1_000_000 * f.inputMult
	outputTokensForCost := ResolveBillableOutputTokens(usage)
	outputCost := float64(outputTokensForCost) * f.outputPrice / 1_000_000 * f.outputMult
	cacheReadCost := float64(usage.CacheReadTokens) * f.cacheReadPrice / 1_000_000 * f.cacheReadMult

	// 分开计算 5m 和 1h 缓存成本
	var cache5mCost, cache1hCost float64
	if pricing.CodexCachePricing {
		allocation := resolveCacheWriteAllocation(usage, pricing, &m, f)
		// Codex TTL costs are retained in the decision snapshot; legacy TTL
		// columns remain reserved for Claude's 5m/1h accounting.
		cache5mCost = float64(allocation.tokens30m) * f.cacheCreationPrice / 1e6 * f.cacheCreationMult
		cache1hCost = float64(allocation.tokens12h) * allocation.price12h / 1e6 * f.cacheCreationMult
	} else if usage.CacheCreation5mTokens > 0 || usage.CacheCreation1hTokens > 0 {
		// 有分开的缓存数据，分别计算
		cache5mCost = float64(usage.CacheCreation5mTokens) * f.cacheCreationPrice / 1_000_000 * f.cacheCreationMult
		cache1hCost = float64(usage.CacheCreation1hTokens) * f.cacheCreation1hPrice / 1_000_000 * f.cacheCreation1hMult
	} else {
		// 向后兼容：只有总数，默认使用 5m 定价
		cache5mCost = float64(usage.CacheCreationTokens) * f.cacheCreationPrice / 1_000_000 * f.cacheCreationMult
	}

	// 总倍率：分组/端点总倍率 × 模型总倍率，应用到所有分项
	if f.totalMult != 1.0 {
		inputCost *= f.totalMult
		outputCost *= f.totalMult
		cache5mCost *= f.totalMult
		cache1hCost *= f.totalMult
		cacheReadCost *= f.totalMult
	}
	cacheCreationCost := cache5mCost + cache1hCost

	result := CostBreakdown{
		InputCost:           inputCost,
		OutputCost:          outputCost,
		CacheCreationCost:   cacheCreationCost,
		CacheCreation5mCost: cache5mCost,
		CacheCreation1hCost: cache1hCost,
		CacheReadCost:       cacheReadCost,
		TotalCost:           inputCost + outputCost + cacheCreationCost + cacheReadCost,
	}
	if pricing.CodexCachePricing {
		result.CacheCreation5mCost = 0
		result.CacheCreation1hCost = 0
	}
	return result
}

// CalculateCostFromTokens 从独立 token 计数计算成本（向后兼容便捷方法）
// 新代码应直接构造 TokenUsage 并调用 CalculateCost
func CalculateCostFromTokens(inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64,
	pricing *ModelPricing, multiplier *EndpointMultiplier, use1hCache bool) CostBreakdown {

	// 构建 TokenUsage，根据 use1hCache 决定放入哪个字段
	usage := &TokenUsage{
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		CacheReadTokens: cacheReadTokens,
	}
	if use1hCache {
		usage.CacheCreation1hTokens = cacheCreationTokens
	} else {
		usage.CacheCreation5mTokens = cacheCreationTokens
	}
	usage.CacheCreationTokens = cacheCreationTokens // 保持总数

	return CalculateCost(usage, pricing, multiplier)
}
