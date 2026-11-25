package app

import (
	"context"
	"fmt"
	"strings"
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

	completionQuery := params.CompletedSince != "" || params.CompletedBefore != ""
	if startTime.IsZero() && !completionQuery {
		now := time.Now().In(loc)
		startTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	}
	if endTime.IsZero() && !completionQuery {
		now := time.Now().In(loc)
		endTime = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, loc)
	}

	opts := &tracking.QueryOptions{
		ModelName:    params.Model,
		ModelNames:   params.Models,
		Channel:      params.Channel,
		EndpointName: params.Endpoint,
		GroupName:    params.Group,
		Status:       params.Status,
		Provider:     params.Provider,
		Limit:        limit,
		Offset:       offset,
	}
	if !startTime.IsZero() {
		opts.StartDate = &startTime
	}
	if !endTime.IsZero() {
		opts.EndDate = &endTime
	}
	if parsed, err := time.Parse(time.RFC3339Nano, params.CompletedSince); err == nil {
		opts.CompletedSince = &parsed
	}
	if parsed, err := time.Parse(time.RFC3339Nano, params.CompletedBefore); err == nil {
		opts.CompletedBefore = &parsed
	}
	return opts
}

func (a *App) GetRequests(params RequestQueryParams) (RequestListResult, error) {
	if requestID := strings.TrimSpace(params.RequestID); requestID != "" {
		return a.getRequestByID(requestID)
	}
	for name, value := range map[string]string{"completed_since": params.CompletedSince, "completed_before": params.CompletedBefore} {
		if value != "" {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil || parsed.Nanosecond() != 0 {
				return RequestListResult{}, fmt.Errorf("%s must be an RFC3339 timestamp at whole-second precision", name)
			}
		}
	}
	if params.CompletedSince != "" && params.CompletedBefore != "" {
		since, _ := time.Parse(time.RFC3339Nano, params.CompletedSince)
		before, _ := time.Parse(time.RFC3339Nano, params.CompletedBefore)
		if !since.Before(before) {
			return RequestListResult{}, fmt.Errorf("completed_since must precede completed_before")
		}
	}
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

func (a *App) getRequestByID(requestID string) (RequestListResult, error) {
	a.mu.RLock()
	tracker := a.usageTracker
	a.mu.RUnlock()
	result := RequestListResult{Requests: []RequestRecord{}, Page: 1, PageSize: 1}
	if tracker == nil {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	detail, err := tracker.FindRequestDetail(ctx, requestID)
	if err != nil || detail == nil {
		return result, err
	}
	result.Requests = append(result.Requests, requestDetailToRecord(*detail))
	result.Total = 1
	if detail.EndTime != nil {
		a.enrichRequestDiagnostics(ctx, result.Requests)
	}
	return result, nil
}

func requestDetailToRecord(r tracking.RequestDetail) RequestRecord {
	// 使用统一的时间格式（2025-12-04 17:18:48）
	endpointChain := parseEndpointChain(r.EndpointChain)
	retryCount := r.RetryCount
	// Historical endpoint_chain nodes provide a lower bound for routing attempts, not an exact transport retry count.
	chainAttempts := 0
	for _, node := range endpointChain {
		if node.Local && node.Attempts == 0 && (strings.HasPrefix(node.Error, "account_") || strings.HasPrefix(node.Error, "subscription_") || strings.HasPrefix(node.Error, "profile_") || strings.HasPrefix(node.Error, "candidate_")) {
			continue
		}
		chainAttempts++
	}
	if chainRetryFloor := chainAttempts - 1; chainRetryFloor > retryCount {
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
		ContextSummary:                r.ContextSummary,
		Provider:                      r.Provider, // 提供商标识
		Status:                        r.Status,
		InProgress:                    r.ID == 0 && r.EndTime == nil,
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
