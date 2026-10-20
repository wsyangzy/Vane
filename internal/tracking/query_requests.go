package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// QueryRequestDetails queries detailed request records
func (ut *UsageTracker) QueryRequestDetails(ctx context.Context, opts *QueryOptions) ([]RequestDetail, error) {
	opts = ut.scopedQueryOptions(opts)
	sourceOpts := *opts
	sourceOpts.Offset = 0
	if opts.Limit > 0 {
		sourceOpts.Limit = max(opts.Offset, 0) + opts.Limit
	}

	capacity := max(sourceOpts.Limit, 0)
	details := make([]RequestDetail, 0, capacity)
	seen := make(map[string]struct{})
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		sourceDetails, err := ut.queryRequestDetailsFromSource(ctx, source.db, &sourceOpts)
		if err != nil {
			return err
		}
		for _, detail := range sourceDetails {
			if _, exists := seen[detail.RequestID]; exists {
				continue
			}
			seen[detail.RequestID] = struct{}{}
			details = append(details, detail)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(details, func(i, j int) bool {
		if !details[i].StartTime.Equal(details[j].StartTime) {
			return details[i].StartTime.After(details[j].StartTime)
		}
		return details[i].RequestID > details[j].RequestID
	})

	start := max(opts.Offset, 0)
	if start >= len(details) {
		return []RequestDetail{}, nil
	}
	end := len(details)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	return details[start:end], nil
}

func (ut *UsageTracker) queryRequestDetailsFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) ([]RequestDetail, error) {
	if db == nil {
		return nil, fmt.Errorf("read database not initialized")
	}
	optionalColumns := make(map[string]bool, 5)
	columnRows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('request_logs')
		WHERE name IN ('model_reasoning_effort', 'service_tier', 'billing_rule_snapshot', 'http_status_is_upstream', 'route_snapshot', 'stream_outcome')`)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
	}
	for columnRows.Next() {
		var name string
		if err := columnRows.Scan(&name); err != nil {
			_ = columnRows.Close()
			return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
		}
		optionalColumns[name] = true
	}
	if err := columnRows.Err(); err != nil {
		_ = columnRows.Close()
		return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
	}
	if err := columnRows.Close(); err != nil {
		return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
	}
	optionalTextExpr := func(column string) string {
		if optionalColumns[column] {
			return "COALESCE(" + column + ", '')"
		}
		return "''"
	}
	optionalBoolExpr := func(column string) string {
		if optionalColumns[column] {
			return "COALESCE(" + column + ", 0)"
		}
		return "0"
	}

	query := `SELECT id, request_id,
		COALESCE(client_ip, '') as client_ip,
		COALESCE(user_agent, '') as user_agent,
		method, path, start_time, end_time, duration_ms, output_sent_at,
		COALESCE(channel, '') as channel,
		COALESCE(endpoint_name, '') as endpoint_name,
		COALESCE(endpoint_chain, '') as endpoint_chain,
		COALESCE(group_name, '') as group_name,
		COALESCE(model_name, '') as model_name,
		COALESCE(original_model_name, '') as original_model_name,
		COALESCE(model_rewrite_source, '') as model_rewrite_source,
		COALESCE(model_rewrite_target, '') as model_rewrite_target,
		` + optionalTextExpr("route_snapshot") + ` as route_snapshot,
		` + optionalTextExpr("model_reasoning_effort") + ` as model_reasoning_effort,
		` + optionalTextExpr("service_tier") + ` as service_tier,
		COALESCE(privacy_redacted, false) as privacy_redacted,
		COALESCE(privacy_redaction_summary, '') as privacy_redaction_summary,
		COALESCE(is_streaming, false) as is_streaming,
		COALESCE(provider, 'claude') as provider,
		status, ` + optionalTextExpr("stream_outcome") + ` as stream_outcome, CASE WHEN ` + optionalBoolExpr("http_status_is_upstream") + ` = 1 THEN http_status_code ELSE NULL END as http_status_code, ` + optionalBoolExpr("http_status_is_upstream") + ` as http_status_is_upstream, retry_count,
		COALESCE(failure_reason, '') as failure_reason,
		COALESCE(last_failure_reason, '') as last_failure_reason,
		COALESCE(cancel_reason, '') as cancel_reason,
		input_tokens, output_tokens,
		COALESCE(reasoning_tokens, 0) as reasoning_tokens,
		CASE
			WHEN COALESCE(billable_output_tokens, 0) > 0
				OR COALESCE(billing_output_token_source, 'output_tokens') != 'output_tokens'
			THEN COALESCE(billable_output_tokens, 0)
			ELSE output_tokens
		END as billable_output_tokens,
		COALESCE(billing_output_token_source, 'output_tokens') as billing_output_token_source,
		` + optionalTextExpr("billing_rule_snapshot") + ` as billing_rule_snapshot,
		cache_creation_tokens, COALESCE(cache_creation_5m_tokens, 0) as cache_creation_5m_tokens, COALESCE(cache_creation_1h_tokens, 0) as cache_creation_1h_tokens,
		cache_read_tokens,
		input_cost_usd, output_cost_usd, cache_creation_cost_usd,
		COALESCE(cache_creation_5m_cost_usd, 0) as cache_creation_5m_cost_usd,
		COALESCE(cache_creation_1h_cost_usd, 0) as cache_creation_1h_cost_usd,
		cache_read_cost_usd, total_cost_usd,
		COALESCE(cost_multiplier, 1.0) as cost_multiplier,
		COALESCE(input_cost_multiplier, 1.0) as input_cost_multiplier,
		COALESCE(output_cost_multiplier, 1.0) as output_cost_multiplier,
		COALESCE(cache_creation_cost_multiplier, 1.0) as cache_creation_cost_multiplier,
		COALESCE(cache_creation_cost_multiplier_1h, 1.0) as cache_creation_cost_multiplier_1h,
		COALESCE(cache_read_cost_multiplier, 1.0) as cache_read_cost_multiplier,
		COALESCE(model_cost_multiplier, 1.0) as model_cost_multiplier,
		COALESCE(model_input_cost_multiplier, 1.0) as model_input_cost_multiplier,
		COALESCE(model_output_cost_multiplier, 1.0) as model_output_cost_multiplier,
		COALESCE(model_cache_read_cost_multiplier, 1.0) as model_cache_read_cost_multiplier,
		created_at, updated_at
		FROM request_logs WHERE 1=1
			AND COALESCE(environment, 'release') = ?`

	args := []interface{}{queryEnvironment(opts)}

	if opts.StartDate != nil {
		query += " AND start_time >= ?"
		args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
	}
	if opts.EndDate != nil {
		query += " AND start_time <= ?"
		args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
	}
	if opts.ModelName != "" {
		query += " AND model_name = ?"
		args = append(args, opts.ModelName)
	}
	if opts.Channel != "" {
		query += " AND channel = ?"
		args = append(args, opts.Channel)
	}
	if opts.EndpointName != "" {
		query += " AND endpoint_name = ?"
		args = append(args, opts.EndpointName)
	}
	if opts.GroupName != "" {
		query += " AND group_name = ?"
		args = append(args, opts.GroupName)
	}
	if opts.Status != "" {
		// 状态机重构 - 状态与错误分离的兼容查询
		switch opts.Status {
		case "completed":
			// 已完成状态：精确匹配
			query += " AND status = ?"
			args = append(args, "completed")
		case "failed":
			// 失败状态：包含新架构的failed状态 + 旧版本的各种错误状态
			query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
		case "processing":
			// 处理中状态：精确匹配
			query += " AND status = ?"
			args = append(args, "processing")
		case "cancelled":
			// 取消状态：精确匹配
			query += " AND status = ?"
			args = append(args, "cancelled")
		case "suspended":
			// 挂起状态：精确匹配
			query += " AND status = ?"
			args = append(args, "suspended")
		case "pending":
			// 等待状态：精确匹配
			query += " AND status = ?"
			args = append(args, "pending")
		case "forwarding":
			// 转发状态：精确匹配
			query += " AND status = ?"
			args = append(args, "forwarding")
		case "retry":
			// 重试状态：精确匹配
			query += " AND status = ?"
			args = append(args, "retry")
		default:
			// 精确匹配其他状态
			query += " AND status = ?"
			args = append(args, opts.Status)
		}
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}

	query += " ORDER BY start_time DESC, request_id DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query request details: %w", err)
	}
	defer rows.Close()

	var details []RequestDetail
	for rows.Next() {
		var detail RequestDetail
		err := rows.Scan(
			&detail.ID, &detail.RequestID,
			&detail.ClientIP, &detail.UserAgent, &detail.Method, &detail.Path,
			&detail.StartTime, &detail.EndTime, &detail.DurationMs, &detail.OutputSentAt,
			&detail.Channel, &detail.EndpointName, &detail.EndpointChain, &detail.GroupName, &detail.ModelName,
			&detail.OriginalModelName, &detail.ModelRewriteSource, &detail.ModelRewriteTarget,
			&detail.RouteSnapshot,
			&detail.ReasoningEffort,
			&detail.ServiceTier,
			&detail.PrivacyRedacted, &detail.PrivacyRedactionSummary,
			&detail.IsStreaming,
			&detail.Provider,
			&detail.Status, &detail.StreamOutcome, &detail.HTTPStatusCode, &detail.HTTPStatusIsUpstream, &detail.RetryCount,
			&detail.FailureReason, &detail.LastFailureReason, &detail.CancelReason,
			&detail.InputTokens, &detail.OutputTokens,
			&detail.ReasoningTokens, &detail.BillableOutputTokens, &detail.BillingOutputTokenSource, &detail.BillingRuleSnapshot,
			&detail.CacheCreationTokens, &detail.CacheCreation5mTokens, &detail.CacheCreation1hTokens, &detail.CacheReadTokens,
			&detail.InputCostUSD, &detail.OutputCostUSD,
			&detail.CacheCreationCostUSD,
			&detail.CacheCreation5mCostUSD, &detail.CacheCreation1hCostUSD,
			&detail.CacheReadCostUSD, &detail.TotalCostUSD,
			&detail.CostMultiplier,
			&detail.InputCostMultiplier,
			&detail.OutputCostMultiplier,
			&detail.CacheCreationCostMultiplier,
			&detail.CacheCreationCostMultiplier1h,
			&detail.CacheReadCostMultiplier,
			&detail.ModelCostMultiplier,
			&detail.ModelInputCostMultiplier,
			&detail.ModelOutputCostMultiplier,
			&detail.ModelCacheReadCostMultiplier,
			&detail.CreatedAt, &detail.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request detail: %w", err)
		}
		detail.ComputeFirstByteMs()
		details = append(details, detail)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating request detail rows: %w", err)
	}

	return details, nil
}

// CountRequestDetails returns the total count of request details matching the query options
func (ut *UsageTracker) CountRequestDetails(ctx context.Context, opts *QueryOptions) (int, error) {
	opts = ut.scopedQueryOptions(opts)
	total := 0
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		count, err := ut.countRequestDetailsFromSource(ctx, source.db, opts)
		if err != nil {
			return err
		}
		total += count
		return nil
	})
	return total, err
}

func (ut *UsageTracker) countRequestDetailsFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("read database not initialized")
	}

	query := "SELECT COUNT(*) FROM request_logs WHERE 1=1 AND COALESCE(environment, 'release') = ?"
	args := []interface{}{queryEnvironment(opts)}

	if opts.StartDate != nil {
		query += " AND start_time >= ?"
		args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
	}
	if opts.EndDate != nil {
		query += " AND start_time <= ?"
		args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
	}
	if opts.ModelName != "" {
		query += " AND model_name = ?"
		args = append(args, opts.ModelName)
	}
	if opts.Channel != "" {
		query += " AND channel = ?"
		args = append(args, opts.Channel)
	}
	if opts.EndpointName != "" {
		query += " AND endpoint_name = ?"
		args = append(args, opts.EndpointName)
	}
	if opts.GroupName != "" {
		query += " AND group_name = ?"
		args = append(args, opts.GroupName)
	}
	if opts.Status != "" {
		// 与 QueryRequestDetails 保持一致：failed 代表一组失败/错误状态
		switch opts.Status {
		case "failed":
			query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
		default:
			query += " AND status = ?"
			args = append(args, opts.Status)
		}
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}

	var count int
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count request details: %w", err)
	}

	return count, nil
}
