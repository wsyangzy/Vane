package tracking

import (
	"time"

	_ "modernc.org/sqlite"
)

// ActiveRequestToDetail 将 ActiveRequest 转换为 RequestDetail
func (ut *UsageTracker) ActiveRequestToDetail(req *ActiveRequest) RequestDetail {
	// 统一在请求级别加读锁，避免并发更新导致数据竞争
	req.mu.RLock()
	requestID := req.RequestID
	clientIP := req.ClientIP
	userAgent := req.UserAgent
	method := req.Method
	path := req.Path
	startTime := req.StartTime
	endTimeSnapshot := req.EndTime
	durationMsSnapshot := req.DurationMs
	outputSentAtSnapshot := req.OutputSentAt
	endpointName := req.EndpointName
	endpointChain := req.EndpointChain
	channel := req.Channel
	groupName := req.GroupName
	modelName := req.ModelName
	originalModelName := req.OriginalModelName
	modelRewriteSource := req.ModelRewriteSource
	modelRewriteTarget := req.ModelRewriteTarget
	routeSnapshot := req.RouteSnapshot
	reasoningEffort := req.ReasoningEffort
	serviceTier := req.ServiceTier
	requestedTier, responseTier, tierSource := req.RequestedServiceTier, req.ResponseServiceTier, req.ServiceTierSource
	cacheWrite30m, cacheWrite12h, cacheWriteTTLPricing := req.CacheWrite30mTokens, req.CacheWrite12hTokens, req.CacheWriteTTLPricing
	privacyRedacted := req.PrivacyRedacted
	privacySummary := req.PrivacySummary
	isStreaming := req.IsStreaming
	provider := req.Provider
	status := req.Status
	streamOutcome := req.StreamOutcome
	httpStatusSnapshot := req.HTTPStatus
	httpStatusIsUpstream := req.HTTPStatusIsUpstream
	retryCount := req.RetryCount
	failureReason := req.FailureReason
	lastFailureReason := req.LastFailureReason
	cancelReason := req.CancelReason
	inputTokens := req.InputTokens
	outputTokens := req.OutputTokens
	reasoningTokens := req.ReasoningTokens
	billingOutputTokenSource := NormalizeBillingOutputTokenSource(req.BillingOutputTokenSource)
	billableOutputTokens := BillableOutputTokens(outputTokens, reasoningTokens, billingOutputTokenSource)
	cacheCreationTokens := req.CacheCreationTokens
	cacheCreation5mTokens := req.CacheCreation5mTokens
	cacheCreation1hTokens := req.CacheCreation1hTokens
	cacheReadTokens := req.CacheReadTokens
	inputIncludesCacheRead := req.InputIncludesCacheRead
	// 倍率快照（用于详情页计费过程展示）
	costMultSnap := req.CostMultiplier
	inputCostMultSnap := req.InputCostMultiplier
	outputCostMultSnap := req.OutputCostMultiplier
	cacheCreationCostMultSnap := req.CacheCreationCostMultiplier
	cacheCreationCostMult1hSnap := req.CacheCreationCostMultiplier1h
	cacheReadCostMultSnap := req.CacheReadCostMultiplier
	modelCostMultSnap := req.ModelCostMultiplier
	modelInputCostMultSnap := req.ModelInputCostMultiplier
	modelOutputCostMultSnap := req.ModelOutputCostMultiplier
	modelCacheReadCostMultSnap := req.ModelCacheReadCostMultiplier
	billingRuleSnapshot := req.BillingRuleSnapshot
	billingRules := append([]BillingRule(nil), req.BillingRules...)
	billingRulesCaptured := req.BillingRulesCaptured
	billingGroupMultiplier := req.BillingGroupMultiplier
	req.mu.RUnlock()

	var endTime *time.Time
	var durationMs *int64
	var outputSentAt *time.Time
	var firstByteMs *int64
	var httpStatus *int

	if endTimeSnapshot != nil {
		t := *endTimeSnapshot
		endTime = &t
	}
	if durationMsSnapshot > 0 {
		d := durationMsSnapshot
		durationMs = &d
	}
	if outputSentAtSnapshot != nil {
		t := *outputSentAtSnapshot
		outputSentAt = &t
		ms := t.Sub(startTime).Milliseconds()
		if ms < 0 {
			ms = 0
		}
		firstByteMs = &ms
	}
	if httpStatusIsUpstream && httpStatusSnapshot > 0 {
		s := httpStatusSnapshot
		httpStatus = &s
	}

	// 计算成本（使用公共函数消除重复代码）
	var cost CostBreakdown
	var pricing ModelPricing
	var hasPricing bool
	var pricingModel, pricingTier string
	multiplierValue := DefaultEndpointMultiplier()
	multiplierValue.ServiceTierSource = tierSource
	multiplier := &multiplierValue

	ut.mu.RLock()
	if ut.endpointMu != nil {
		for _, key := range endpointMultiplierLookupKeys(provider, channel, groupName, endpointName) {
			if m, ok := ut.endpointMu[key]; ok {
				multiplierValue = m
				break
			}
		}
	}
	if billingRulesCaptured {
		multiplierValue.ServiceTierSource = tierSource
		multiplierValue.CacheWriteTTLPricing = cacheWriteTTLPricing
		multiplierValue.BillingRules = billingRules
		if len(billingRules) > 0 {
			if billingGroupMultiplier != nil {
				copyBillingGroupMultipliers(&multiplierValue, *billingGroupMultiplier)
			}
			multiplierValue.BillingOutputTokenSource = billingOutputTokenSource
		}
	}
	if provider == "codex" {
		tierSource = billingServiceTierSource(multiplierValue)
		serviceTier, _ = ResolveBillingServiceTier(requestedTier, responseTier, tierSource)
	}
	pricing, pricingModel, pricingTier, hasPricing = lookupModelPricing(ut.pricing, provider, modelName, serviceTier)
	ut.mu.RUnlock()

	if hasPricing {
		usage := &TokenUsage{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
			ReasoningTokens:          reasoningTokens,
			BillableOutputTokens:     billableOutputTokens,
			BillingOutputTokenSource: billingOutputTokenSource,
			CacheCreationTokens:      cacheCreationTokens,
			CacheWrite30mTokens:      cacheWrite30m,
			CacheWrite12hTokens:      cacheWrite12h,
			CacheCreation5mTokens:    cacheCreation5mTokens,
			CacheCreation1hTokens:    cacheCreation1hTokens,
			CacheReadTokens:          cacheReadTokens,
			InputIncludesCacheRead:   inputIncludesCacheRead,
		}
		var resolvedSnapshot *BillingDecisionSnapshot
		cost, multiplierValue, resolvedSnapshot = calculateRequestCost(usage, pricing, multiplierValue, billingIdentity{
			provider:      provider,
			requestModel:  modelName,
			serviceTier:   serviceTier,
			pricingModel:  pricingModel,
			pricingTier:   pricingTier,
			requestedTier: requestedTier,
			responseTier:  responseTier,
			tierSource:    tierSource,
		})
		multiplier = &multiplierValue
		if snapshotJSON := billingDecisionSnapshotJSON(resolvedSnapshot); snapshotJSON != "" {
			billingRuleSnapshot = snapshotJSON
		}
		// 显示合并后的实际倍率，来源层次由计费快照记录。
		if resolvedSnapshot != nil && resolvedSnapshot.RuleKey != "" {
			costMultSnap = multiplierValue.CostMultiplier
			inputCostMultSnap = multiplierValue.InputCostMultiplier
			outputCostMultSnap = multiplierValue.OutputCostMultiplier
			cacheCreationCostMultSnap = multiplierValue.CacheCreationCostMultiplier
			cacheCreationCostMult1hSnap = multiplierValue.CacheCreationCostMultiplier1h
			cacheReadCostMultSnap = multiplierValue.CacheReadCostMultiplier
			modelCostMultSnap = multiplierValue.ModelCostMultiplier
			modelInputCostMultSnap = multiplierValue.ModelInputCostMultiplier
			modelOutputCostMultSnap = multiplierValue.ModelOutputCostMultiplier
			modelCacheReadCostMultSnap = multiplierValue.ModelCacheReadCostMultiplier
		}
	}
	if !hasPricing && provider == "codex" {
		billingRuleSnapshot = billingDecisionSnapshotJSON(unpricedBillingDecisionSnapshot(billingIdentity{
			provider: provider, requestedTier: requestedTier, responseTier: responseTier,
			serviceTier: serviceTier, tierSource: tierSource,
		}))
	}

	return RequestDetail{
		ID:                            0, // 热池中的请求还没有数据库ID
		RequestID:                     requestID,
		ClientIP:                      clientIP,
		UserAgent:                     userAgent,
		Method:                        method,
		Path:                          path,
		StartTime:                     startTime,
		EndTime:                       endTime,
		DurationMs:                    durationMs,
		OutputSentAt:                  outputSentAt,
		FirstByteMs:                   firstByteMs,
		EndpointName:                  endpointName,
		EndpointChain:                 endpointChain,
		Channel:                       channel, // 渠道标签
		GroupName:                     groupName,
		ModelName:                     modelName,
		OriginalModelName:             originalModelName,
		ModelRewriteSource:            modelRewriteSource,
		ModelRewriteTarget:            modelRewriteTarget,
		RouteSnapshot:                 routeSnapshot,
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   serviceTier,
		PrivacyRedacted:               privacyRedacted,
		PrivacyRedactionSummary:       privacySummary,
		IsStreaming:                   isStreaming,
		Provider:                      provider, // 提供商标识
		Status:                        status,
		StreamOutcome:                 streamOutcome,
		HTTPStatusCode:                httpStatus,
		HTTPStatusIsUpstream:          httpStatusIsUpstream,
		RetryCount:                    retryCount,
		FailureReason:                 failureReason,
		LastFailureReason:             lastFailureReason,
		CancelReason:                  cancelReason,
		InputTokens:                   inputTokens,
		OutputTokens:                  outputTokens,
		ReasoningTokens:               reasoningTokens,
		BillableOutputTokens:          billableOutputTokens,
		BillingOutputTokenSource:      billingOutputTokenSource,
		BillingRuleSnapshot:           billingRuleSnapshot,
		CacheCreationTokens:           cacheCreationTokens,
		CacheCreation5mTokens:         cacheCreation5mTokens,
		CacheCreation1hTokens:         cacheCreation1hTokens,
		CacheReadTokens:               cacheReadTokens,
		InputCostUSD:                  cost.InputCost,
		OutputCostUSD:                 cost.OutputCost,
		CacheCreationCostUSD:          cost.CacheCreationCost,
		CacheCreation5mCostUSD:        cost.CacheCreation5mCost,
		CacheCreation1hCostUSD:        cost.CacheCreation1hCost,
		CacheReadCostUSD:              cost.CacheReadCost,
		TotalCostUSD:                  cost.TotalCost,
		CostMultiplier:                normalizeMultiplierSnapshot(costMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.CostMultiplier }),
		InputCostMultiplier:           normalizeMultiplierSnapshot(inputCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.InputCostMultiplier }),
		OutputCostMultiplier:          normalizeMultiplierSnapshot(outputCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.OutputCostMultiplier }),
		CacheCreationCostMultiplier:   normalizeMultiplierSnapshot(cacheCreationCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.CacheCreationCostMultiplier }),
		CacheCreationCostMultiplier1h: normalizeMultiplierSnapshot(cacheCreationCostMult1hSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.CacheCreationCostMultiplier1h }),
		CacheReadCostMultiplier:       normalizeMultiplierSnapshot(cacheReadCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.CacheReadCostMultiplier }),
		ModelCostMultiplier:           normalizeMultiplierSnapshot(modelCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.ModelCostMultiplier }),
		ModelInputCostMultiplier:      normalizeMultiplierSnapshot(modelInputCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.ModelInputCostMultiplier }),
		ModelOutputCostMultiplier:     normalizeMultiplierSnapshot(modelOutputCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.ModelOutputCostMultiplier }),
		ModelCacheReadCostMultiplier:  normalizeMultiplierSnapshot(modelCacheReadCostMultSnap, multiplier, func(m *EndpointMultiplier) float64 { return m.ModelCacheReadCostMultiplier }),
		CreatedAt:                     startTime,
		UpdatedAt:                     ut.now(),
	}
}

// normalizeMultiplierSnapshot 优先使用 ActiveRequest 上的快照值；
// 若快照为 0/未设置，则回退到当前端点倍率配置；都没有时返回 1.0。
func normalizeMultiplierSnapshot(snapshot float64, current *EndpointMultiplier, accessor func(*EndpointMultiplier) float64) float64 {
	if snapshot > 0 {
		return snapshot
	}
	if current != nil {
		if v := accessor(current); v > 0 {
			return v
		}
	}
	return 1.0
}
