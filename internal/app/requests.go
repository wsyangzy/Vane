package app

import (
	"context"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking"
)

func buildRequestQueryOptions(params RequestQueryParams, timezone string, limit, offset int) *tracking.QueryOptions {
	loc := time.Local
	if timezone != "" {
		if parsedLoc, err := time.LoadLocation(timezone); err == nil {
			loc = parsedLoc
		}
	}

	var startTime, endTime time.Time
	if params.StartDate != "" {
		if t, err := parseTimeWithLocation(params.StartDate, loc); err == nil {
			startTime = t.In(loc)
		}
	}
	if params.EndDate != "" {
		if t, err := parseTimeWithLocation(params.EndDate, loc); err == nil {
			endTime = t.In(loc)
		}
	}

	if startTime.IsZero() {
		now := time.Now().In(loc)
		startTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	}
	if endTime.IsZero() {
		now := time.Now().In(loc)
		endTime = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, loc)
	}

	return &tracking.QueryOptions{
		StartDate:    &startTime,
		EndDate:      &endTime,
		ModelName:    params.Model,
		Channel:      params.Channel,
		EndpointName: params.Endpoint,
		GroupName:    params.Group,
		Status:       params.Status,
		Provider:     params.Provider,
		Limit:        limit,
		Offset:       offset,
	}
}

func (a *App) GetRequests(params RequestQueryParams) (RequestListResult, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	endpointManager := a.endpointManager
	endpointStore := a.endpointStore
	claudeCredentialGroupStore := a.claudeCredentialGroupStore
	a.mu.RUnlock()

	if usageTracker == nil {
		return RequestListResult{}, nil
	}

	page := params.Page
	pageSize := params.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize

	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()

	timezone := ""
	if cfg != nil {
		timezone = cfg.Timezone
	}
	opts := buildRequestQueryOptions(params, timezone, pageSize, offset)

	requests, total, err := usageTracker.QueryRequestDetailsWithHotPool(ctx, opts)
	if err != nil {
		return RequestListResult{}, err
	}

	result := RequestListResult{
		Requests: make([]RequestRecord, 0, len(requests)),
		Page:     page,
		PageSize: pageSize,
		Total:    int(total),
	}

	credentialGroupLookup := endpointChainCredentialGroupLookup(ctx, endpointManager, endpointStore, claudeCredentialGroupStore)
	for _, r := range requests {
		record := requestDetailToRecord(r)
		enrichEndpointChainCredentialGroups(record.Provider, record.EndpointChain, credentialGroupLookup)
		result.Requests = append(result.Requests, record)
	}
	a.enrichRequestDiagnostics(ctx, result.Requests)

	return result, nil
}

func requestDetailToRecord(r tracking.RequestDetail) RequestRecord {
	// 使用统一的时间格式（2025-12-04 17:18:48）
	endpointChain := parseEndpointChain(r.EndpointChain)
	retryCount := r.RetryCount
	// Historical endpoint_chain nodes provide a lower bound for routing attempts, not an exact transport retry count.
	if chainRetryFloor := len(endpointChain) - 1; chainRetryFloor > retryCount {
		retryCount = chainRetryFloor
	}
	record := RequestRecord{
		RequestID:                     r.RequestID,
		Timestamp:                     r.StartTime.Format("2006-01-02 15:04:05"),
		ClientIP:                      r.ClientIP,
		UserAgent:                     r.UserAgent,
		Method:                        r.Method,
		Path:                          r.Path,
		Channel:                       r.Channel, // 渠道标签
		Endpoint:                      r.EndpointName,
		EndpointChain:                 endpointChain,
		Group:                         r.GroupName,
		Model:                         r.ModelName,
		OriginalModelName:             r.OriginalModelName,
		ModelRewriteSource:            r.ModelRewriteSource,
		ModelRewriteTarget:            r.ModelRewriteTarget,
		RouteSnapshot:                 r.RouteSnapshot,
		ReasoningEffort:               r.ReasoningEffort,
		ServiceTier:                   r.ServiceTier,
		PrivacyRedacted:               r.PrivacyRedacted,
		PrivacyRedactionSummary:       r.PrivacyRedactionSummary,
		Provider:                      r.Provider, // 提供商标识
		Status:                        r.Status,
		StreamOutcome:                 r.StreamOutcome,
		HTTPStatusIsUpstream:          r.HTTPStatusIsUpstream,
		RetryCount:                    retryCount,
		FailureReason:                 r.FailureReason,
		LastFailureReason:             r.LastFailureReason,
		CancelReason:                  r.CancelReason,
		InputTokens:                   r.InputTokens,
		OutputTokens:                  r.OutputTokens,
		ReasoningTokens:               r.ReasoningTokens,
		BillableOutputTokens:          r.BillableOutputTokens,
		BillingOutputTokenSource:      r.BillingOutputTokenSource,
		BillingRuleSnapshot:           r.BillingRuleSnapshot,
		CacheCreationTokens:           r.CacheCreationTokens,
		CacheCreation5mTokens:         r.CacheCreation5mTokens,
		CacheCreation1hTokens:         r.CacheCreation1hTokens,
		CacheReadTokens:               r.CacheReadTokens,
		IsStreaming:                   r.IsStreaming,
		Cost:                          r.TotalCostUSD,
		InputCostUSD:                  r.InputCostUSD,
		OutputCostUSD:                 r.OutputCostUSD,
		CacheCreationCostUSD:          r.CacheCreationCostUSD,
		CacheCreation5mCostUSD:        r.CacheCreation5mCostUSD,
		CacheCreation1hCostUSD:        r.CacheCreation1hCostUSD,
		CacheReadCostUSD:              r.CacheReadCostUSD,
		CostMultiplier:                r.CostMultiplier,
		InputCostMultiplier:           r.InputCostMultiplier,
		OutputCostMultiplier:          r.OutputCostMultiplier,
		CacheCreationCostMultiplier:   r.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: r.CacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       r.CacheReadCostMultiplier,
		ModelCostMultiplier:           r.ModelCostMultiplier,
		ModelInputCostMultiplier:      r.ModelInputCostMultiplier,
		ModelOutputCostMultiplier:     r.ModelOutputCostMultiplier,
		ModelCacheReadCostMultiplier:  r.ModelCacheReadCostMultiplier,
	}

	if r.HTTPStatusCode != nil && r.HTTPStatusIsUpstream {
		record.HTTPStatus = *r.HTTPStatusCode
	}
	if r.DurationMs != nil {
		record.ResponseTime = *r.DurationMs
	}
	if r.FirstByteMs != nil {
		firstByteMs := *r.FirstByteMs
		record.FirstByteMs = &firstByteMs
	}
	return record
}
