package tracking

// calculateCost 计算请求成本（支持分开的 5m/1h 缓存）
func (am *ArchiveManager) calculateCost(req *ActiveRequest) CostBreakdown {
	// 🔧 [P3.3 修复] 通过 snapshot 取得 map 引用，保证使用期间引用稳定
	// 旧实现：直接读 am.pricing/am.endpointMu，与 UpdatePricing/UpdateEndpointMultipliers 写操作 race
	pricingMap, endpointMuMap := am.getPricingSnapshot()

	// 获取请求绑定端点时的计费配置。组合规则在热池中有独立副本，
	// 避免请求处理中修改分组配置改变本次账单。
	multiplierValue := DefaultEndpointMultiplier()
	multiplierValue.ServiceTierSource = req.ServiceTierSource
	hasMultiplier := false
	if endpointMuMap != nil {
		for _, key := range endpointMultiplierLookupKeys(req.Provider, req.Channel, req.GroupName, req.EndpointName) {
			if m, ok := endpointMuMap[key]; ok {
				multiplierValue = m
				hasMultiplier = true
				break
			}
		}
	}
	if req.BillingRulesCaptured {
		hasMultiplier = true
		multiplierValue.ServiceTierSource = req.ServiceTierSource
		multiplierValue.CacheWriteTTLPricing = req.CacheWriteTTLPricing
		multiplierValue.BillingRules = append([]BillingRule(nil), req.BillingRules...)
		if len(req.BillingRules) > 0 {
			if req.BillingGroupMultiplier != nil {
				copyBillingGroupMultipliers(&multiplierValue, *req.BillingGroupMultiplier)
			}
			multiplierValue.BillingOutputTokenSource = req.BillingOutputTokenSource
			hasMultiplier = true
		}
	}
	if req.Provider == "codex" {
		req.ServiceTierSource = billingServiceTierSource(multiplierValue)
		resolveActiveRequestServiceTier(req)
	}
	// Select the price only after resolving the tier from this request's policy.
	pricing, pricingModel, pricingTier, exists := lookupModelPricing(pricingMap, req.Provider, req.ModelName, req.ServiceTier)
	if !exists {
		snapshot := unpricedBillingDecisionSnapshot(billingIdentity{
			provider: req.Provider, requestedTier: req.RequestedServiceTier, responseTier: req.ResponseServiceTier,
			serviceTier: req.ServiceTier, tierSource: req.ServiceTierSource,
		})
		if snapshot != nil {
			req.BillingRuleSnapshot = billingDecisionSnapshotJSON(snapshot)
		}
		return CostBreakdown{}
	}

	// 构建 TokenUsage 对象
	usage := &TokenUsage{
		InputTokens:              req.InputTokens,
		OutputTokens:             req.OutputTokens,
		ReasoningTokens:          req.ReasoningTokens,
		BillableOutputTokens:     req.BillableOutputTokens,
		BillingOutputTokenSource: req.BillingOutputTokenSource,
		CacheCreationTokens:      req.CacheCreationTokens,
		CacheWrite30mTokens:      req.CacheWrite30mTokens,
		CacheWrite12hTokens:      req.CacheWrite12hTokens,
		CacheCreation5mTokens:    req.CacheCreation5mTokens,
		CacheCreation1hTokens:    req.CacheCreation1hTokens,
		CacheReadTokens:          req.CacheReadTokens,
		InputIncludesCacheRead:   req.InputIncludesCacheRead,
	}

	if !hasMultiplier {
		multiplierValue = DefaultEndpointMultiplier()
	}
	cost, effectiveMultiplier, snapshot := calculateRequestCost(usage, pricing, multiplierValue, billingIdentity{
		provider:      req.Provider,
		requestModel:  req.ModelName,
		serviceTier:   req.ServiceTier,
		pricingModel:  pricingModel,
		pricingTier:   pricingTier,
		requestedTier: req.RequestedServiceTier,
		responseTier:  req.ResponseServiceTier,
		tierSource:    req.ServiceTierSource,
	})
	if snapshotJSON := billingDecisionSnapshotJSON(snapshot); snapshotJSON != "" {
		req.BillingRuleSnapshot = snapshotJSON
	}
	// 落库保留分组与规则合并后的实际倍率，来源层次由计费快照记录。
	if snapshot != nil && snapshot.RuleKey != "" {
		req.CostMultiplier = effectiveMultiplier.CostMultiplier
		req.InputCostMultiplier = effectiveMultiplier.InputCostMultiplier
		req.OutputCostMultiplier = effectiveMultiplier.OutputCostMultiplier
		req.CacheCreationCostMultiplier = effectiveMultiplier.CacheCreationCostMultiplier
		req.CacheCreationCostMultiplier1h = effectiveMultiplier.CacheCreationCostMultiplier1h
		req.CacheReadCostMultiplier = effectiveMultiplier.CacheReadCostMultiplier
		req.ModelCostMultiplier = effectiveMultiplier.ModelCostMultiplier
		req.ModelInputCostMultiplier = effectiveMultiplier.ModelInputCostMultiplier
		req.ModelOutputCostMultiplier = effectiveMultiplier.ModelOutputCostMultiplier
		req.ModelCacheReadCostMultiplier = effectiveMultiplier.ModelCacheReadCostMultiplier
	}
	return cost
}
