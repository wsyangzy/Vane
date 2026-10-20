package tracking

// calculateCost 计算请求成本（支持分开的 5m/1h 缓存）
func (am *ArchiveManager) calculateCost(req *ActiveRequest) CostBreakdown {
	// 🔧 [P3.3 修复] 通过 snapshot 取得 map 引用，保证使用期间引用稳定
	// 旧实现：直接读 am.pricing/am.endpointMu，与 UpdatePricing/UpdateEndpointMultipliers 写操作 race
	pricingMap, endpointMuMap := am.getPricingSnapshot()
	if pricingMap == nil {
		return CostBreakdown{}
	}

	// 查找模型定价：先命中 provider + 档位专属定价，回退到标准档/兜底行，
	// 最后才用 provider 自己的默认价（Codex 未知模型不得回退到 Claude 默认值）。
	pricing, pricingModel, pricingTier, exists := lookupModelPricing(pricingMap, req.Provider, req.ModelName, req.ServiceTier)
	if !exists {
		return CostBreakdown{}
	}

	// 获取请求绑定端点时的计费配置。组合规则在热池中有独立副本，
	// 避免请求处理中修改分组配置改变本次账单。
	multiplierValue := DefaultEndpointMultiplier()
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
		multiplierValue.BillingRules = append([]BillingRule(nil), req.BillingRules...)
		if len(req.BillingRules) > 0 {
			multiplierValue.BillingOutputTokenSource = req.BillingOutputTokenSource
			hasMultiplier = true
		}
	}

	// 构建 TokenUsage 对象
	usage := &TokenUsage{
		InputTokens:              req.InputTokens,
		OutputTokens:             req.OutputTokens,
		ReasoningTokens:          req.ReasoningTokens,
		BillableOutputTokens:     req.BillableOutputTokens,
		BillingOutputTokenSource: req.BillingOutputTokenSource,
		CacheCreationTokens:      req.CacheCreationTokens,
		CacheCreation5mTokens:    req.CacheCreation5mTokens,
		CacheCreation1hTokens:    req.CacheCreation1hTokens,
		CacheReadTokens:          req.CacheReadTokens,
		InputIncludesCacheRead:   req.InputIncludesCacheRead,
	}

	if !hasMultiplier {
		multiplierValue = DefaultEndpointMultiplier()
	}
	cost, effectiveMultiplier, snapshot := calculateRequestCost(usage, pricing, multiplierValue, billingIdentity{
		provider:     req.Provider,
		requestModel: req.ModelName,
		serviceTier:  req.ServiceTier,
		pricingModel: pricingModel,
		pricingTier:  pricingTier,
	})
	if snapshotJSON := billingDecisionSnapshotJSON(snapshot); snapshotJSON != "" {
		req.BillingRuleSnapshot = snapshotJSON
	}
	// 组合规则模式下倍率被规则整体接管，落库的倍率快照需同步为规则的最终值
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
