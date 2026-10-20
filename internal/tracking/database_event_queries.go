package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// buildWriteQuery 构建写操作查询和参数
func (ut *UsageTracker) buildWriteQuery(event RequestEvent) (string, []interface{}, error) {
	switch event.Type {
	case "start":
		return ut.buildStartQuery(event)
	case "update":
		return ut.buildUpdateQuery(event)
	case "flexible_update": // 新增：统一的可选字段更新
		return ut.buildFlexibleUpdateQuery(event)
	case "success": // 新增：成功完成，替代"complete"
		return ut.buildSuccessQuery(event)
	case "final_failure": // 新增：失败/取消完成
		return ut.buildFinalFailureQuery(event)
	case "complete":
		// 对于complete事件，直接使用传入的持续时间，不需要查询数据库
		data, ok := event.Data.(RequestCompleteData)
		if !ok {
			return "", nil, fmt.Errorf("invalid complete event data type")
		}

		// 计算成本
		tokens := tokenUsageFromCompleteData(data)

		inputCost, outputCost, cacheCost, readCost, totalCost := ut.calculateCost(data.ModelName, tokens)

		query := fmt.Sprintf(`UPDATE request_logs SET
			end_time = ?,
			duration_ms = ?,
			model_name = ?,
			input_tokens = ?,
			output_tokens = ?,
			reasoning_tokens = ?,
			billable_output_tokens = ?,
			billing_output_token_source = ?,
			cache_creation_tokens = ?,
			cache_read_tokens = ?,
			input_cost_usd = ?,
			output_cost_usd = ?,
			cache_creation_cost_usd = ?,
			cache_read_cost_usd = ?,
			total_cost_usd = ?,
			status = CASE WHEN status != 'completed' THEN 'completed' ELSE status END,
			updated_at = %s
		WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

		args := []interface{}{
			event.Timestamp,
			data.Duration.Milliseconds(), // 直接使用生命周期管理器计算的持续时间
			data.ModelName,
			data.InputTokens,
			data.OutputTokens,
			data.ReasoningTokens,
			ResolveBillableOutputTokens(tokens),
			NormalizeBillingOutputTokenSource(data.BillingOutputTokenSource),
			data.CacheCreationTokens,
			data.CacheReadTokens,
			inputCost,
			outputCost,
			cacheCost,
			readCost,
			totalCost,
			event.RequestID,
		}

		return query, args, nil
	case "failed_request_tokens":
		// 处理失败请求Token事件：只记录Token统计，不更新请求状态
		data, ok := event.Data.(RequestCompleteData)
		if !ok {
			return "", nil, fmt.Errorf("invalid failed_request_tokens event data type")
		}

		// 计算成本（使用完整的 5m/1h 字段）
		tokens := tokenUsageFromCompleteData(data)

		pricing := ut.GetPricing(data.ModelName)
		costBreakdown := CalculateCost(tokens, &pricing, nil)

		// 只更新Token相关字段和成本，不更新状态
		query := fmt.Sprintf(`UPDATE request_logs SET
			model_name = COALESCE(?, model_name),
			input_tokens = ?,
			output_tokens = ?,
			reasoning_tokens = ?,
			billable_output_tokens = ?,
			billing_output_token_source = ?,
			cache_creation_tokens = ?,
			cache_creation_5m_tokens = ?,
			cache_creation_1h_tokens = ?,
			cache_read_tokens = ?,
			input_cost_usd = ?,
			output_cost_usd = ?,
			cache_creation_cost_usd = ?,
			cache_creation_5m_cost_usd = ?,
			cache_creation_1h_cost_usd = ?,
			cache_read_cost_usd = ?,
			total_cost_usd = ?,
			duration_ms = COALESCE(?, duration_ms),
			updated_at = %s
		WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

		args := []interface{}{
			data.ModelName,
			data.InputTokens,
			data.OutputTokens,
			data.ReasoningTokens,
			ResolveBillableOutputTokens(tokens),
			NormalizeBillingOutputTokenSource(data.BillingOutputTokenSource),
			data.CacheCreationTokens,
			data.CacheCreation5mTokens,
			data.CacheCreation1hTokens,
			data.CacheReadTokens,
			costBreakdown.InputCost,
			costBreakdown.OutputCost,
			costBreakdown.CacheCreationCost,
			costBreakdown.CacheCreation5mCost,
			costBreakdown.CacheCreation1hCost,
			costBreakdown.CacheReadCost,
			costBreakdown.TotalCost,
			data.Duration.Milliseconds(),
			event.RequestID,
		}

		return query, args, nil
	case "token_recovery":
		// 🔧 [Fallback修复] 处理Token恢复事件：只更新Token字段和成本，不更新状态
		data, ok := event.Data.(RequestCompleteData)
		if !ok {
			return "", nil, fmt.Errorf("invalid token_recovery event data type")
		}

		// 计算成本（使用完整的 5m/1h 字段）
		tokens := tokenUsageFromCompleteData(data)

		pricing := ut.GetPricing(data.ModelName)
		costBreakdown := CalculateCost(tokens, &pricing, nil)

		// 🔧 专用于恢复场景：更新任何状态的请求的Token字段，因为这是恢复不完整的数据
		query := fmt.Sprintf(`UPDATE request_logs SET
			model_name = COALESCE(?, model_name),
			input_tokens = ?,
			output_tokens = ?,
			reasoning_tokens = ?,
			billable_output_tokens = ?,
			billing_output_token_source = ?,
			cache_creation_tokens = ?,
			cache_creation_5m_tokens = ?,
			cache_creation_1h_tokens = ?,
			cache_read_tokens = ?,
			input_cost_usd = ?,
			output_cost_usd = ?,
			cache_creation_cost_usd = ?,
			cache_creation_5m_cost_usd = ?,
			cache_creation_1h_cost_usd = ?,
			cache_read_cost_usd = ?,
			total_cost_usd = ?,
			updated_at = %s
		WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

		args := []interface{}{
			data.ModelName,
			data.InputTokens,
			data.OutputTokens,
			data.ReasoningTokens,
			ResolveBillableOutputTokens(tokens),
			NormalizeBillingOutputTokenSource(data.BillingOutputTokenSource),
			data.CacheCreationTokens,
			data.CacheCreation5mTokens,
			data.CacheCreation1hTokens,
			data.CacheReadTokens,
			costBreakdown.InputCost,
			costBreakdown.OutputCost,
			costBreakdown.CacheCreationCost,
			costBreakdown.CacheCreation5mCost,
			costBreakdown.CacheCreation1hCost,
			costBreakdown.CacheReadCost,
			costBreakdown.TotalCost,
			event.RequestID,
		}

		return query, args, nil
	default:
		return "", nil, fmt.Errorf("unknown event type: %s", event.Type)
	}
}

// buildStartQuery 构建开始事件查询
func (ut *UsageTracker) buildStartQuery(event RequestEvent) (string, []interface{}, error) {
	data, ok := event.Data.(RequestStartData)
	if !ok {
		return "", nil, fmt.Errorf("invalid start event data type")
	}

	// 使用适配器构建INSERT OR REPLACE查询
	columns := []string{"request_id", "client_ip", "user_agent", "method", "path", "start_time", "status", "is_streaming", "environment", "updated_at"}
	placeholders := []string{"?", "?", "?", "?", "?", "?", "'pending'", "?", "?", ut.adapter.BuildDateTimeNow()}

	query := ut.adapter.BuildInsertOrReplaceQuery("request_logs", columns, placeholders)

	args := []interface{}{
		event.RequestID,
		data.ClientIP,
		data.UserAgent,
		data.Method,
		data.Path,
		event.Timestamp,
		data.IsStreaming,
		ut.environment,
	}

	return query, args, nil
}

// buildUpdateQuery 构建更新事件查询
func (ut *UsageTracker) buildUpdateQuery(event RequestEvent) (string, []interface{}, error) {
	data, ok := event.Data.(RequestUpdateData)
	if !ok {
		return "", nil, fmt.Errorf("invalid update event data type")
	}
	httpStatus, isUpstream := normalizeUpstreamHTTPStatus(data.HTTPStatus, data.HTTPStatusIsUpstream)
	var persistedHTTPStatus any
	if isUpstream {
		persistedHTTPStatus = httpStatus
	}

	// 如果端点名和组名都为空，只更新状态相关字段
	if data.EndpointName == "" && data.GroupName == "" {
		query := fmt.Sprintf(`UPDATE request_logs SET
			status = ?,
			retry_count = ?,
			http_status_code = ?,
			http_status_is_upstream = ?,
			updated_at = %s
		WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

		args := []interface{}{
			data.Status,
			data.RetryCount,
			persistedHTTPStatus,
			isUpstream,
			event.RequestID,
		}

		return query, args, nil
	}

	// 正常更新所有字段（避免 INSERT OR REPLACE / UPSERT 带来的数据丢失风险）
	query := fmt.Sprintf(`UPDATE request_logs SET
		endpoint_name = ?,
		group_name = ?,
		status = ?,
		retry_count = ?,
		http_status_code = ?,
		http_status_is_upstream = ?,
		updated_at = %s
	WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

	args := []interface{}{
		data.EndpointName,
		data.GroupName,
		data.Status,
		data.RetryCount,
		persistedHTTPStatus,
		isUpstream,
		event.RequestID,
	}

	return query, args, nil
}

// buildFlexibleUpdateQuery 构建统一的可选字段更新查询
// 支持可选字段更新，只更新非nil的字段
func (ut *UsageTracker) buildFlexibleUpdateQuery(event RequestEvent) (string, []interface{}, error) {
	opts, ok := event.Data.(UpdateOptions)
	if !ok {
		return "", nil, fmt.Errorf("invalid flexible_update event data type")
	}

	// 构建动态UPDATE语句
	var setParts []string
	var args []interface{}

	// 根据UpdateOptions中的非nil字段构建SET子句
	if opts.EndpointName != nil {
		setParts = append(setParts, "endpoint_name = ?")
		args = append(args, *opts.EndpointName)
	}
	if opts.EndpointChain != nil {
		setParts = append(setParts, "endpoint_chain = ?")
		args = append(args, *opts.EndpointChain)
	}
	if opts.Channel != nil {
		setParts = append(setParts, "channel = ?")
		args = append(args, *opts.Channel)
	}
	if opts.GroupName != nil {
		setParts = append(setParts, "group_name = ?")
		args = append(args, *opts.GroupName)
	}
	if opts.Status != nil {
		setParts = append(setParts, "status = ?")
		args = append(args, *opts.Status)
	}
	if opts.RetryCount != nil {
		setParts = append(setParts, "retry_count = ?")
		args = append(args, *opts.RetryCount)
	}
	if opts.HttpStatus != nil {
		isUpstream := opts.HttpStatusIsUpstream != nil && *opts.HttpStatusIsUpstream
		httpStatus, isUpstream := normalizeUpstreamHTTPStatus(*opts.HttpStatus, isUpstream)
		setParts = append(setParts, "http_status_code = ?")
		if isUpstream {
			args = append(args, httpStatus)
		} else {
			args = append(args, nil)
		}
		setParts = append(setParts, "http_status_is_upstream = ?")
		args = append(args, isUpstream)
	}
	if opts.HttpStatusIsUpstream != nil && opts.HttpStatus == nil {
		setParts = append(setParts, "http_status_is_upstream = ?")
		args = append(args, *opts.HttpStatusIsUpstream)
		if !*opts.HttpStatusIsUpstream {
			setParts = append(setParts, "http_status_code = NULL")
		}
	}
	if opts.ModelName != nil {
		setParts = append(setParts, "model_name = ?")
		args = append(args, *opts.ModelName)
	}
	if opts.OriginalModelName != nil {
		setParts = append(setParts, "original_model_name = ?")
		args = append(args, *opts.OriginalModelName)
	}
	if opts.ModelRewriteSource != nil {
		setParts = append(setParts, "model_rewrite_source = ?")
		args = append(args, *opts.ModelRewriteSource)
	}
	if opts.ModelRewriteTarget != nil {
		setParts = append(setParts, "model_rewrite_target = ?")
		args = append(args, *opts.ModelRewriteTarget)
	}
	if opts.RouteSnapshot != nil {
		setParts = append(setParts, "route_snapshot = ?")
		args = append(args, nullString(*opts.RouteSnapshot))
	}
	if opts.EndTime != nil {
		setParts = append(setParts, "end_time = ?")
		args = append(args, *opts.EndTime)
	}
	if opts.Duration != nil {
		setParts = append(setParts, "duration_ms = ?")
		args = append(args, opts.Duration.Milliseconds())
	}
	if opts.FailureReason != nil {
		setParts = append(setParts, "failure_reason = ?")
		args = append(args, *opts.FailureReason)
	}

	// 如果没有字段需要更新，返回错误
	if len(setParts) == 0 {
		return "", nil, fmt.Errorf("no fields to update in flexible_update event")
	}

	// 总是更新updated_at字段
	setParts = append(setParts, fmt.Sprintf("updated_at = %s", ut.adapter.BuildDateTimeNow()))

	// 构建完整的UPDATE语句
	query := fmt.Sprintf("UPDATE request_logs SET %s WHERE request_id = ?",
		strings.Join(setParts, ", "))

	// 添加WHERE条件的参数
	args = append(args, event.RequestID)

	return query, args, nil
}

// buildSuccessQuery 构建成功完成的查询
// 一次性更新所有成功相关字段：status='completed', end_time, duration_ms, Token和成本信息
// 🔧 [方案A补充] 2025-12-20: 增加 failure_reason 写入支持
func (ut *UsageTracker) buildSuccessQuery(event RequestEvent) (string, []interface{}, error) {
	data, ok := event.Data.(RequestCompleteData)
	if !ok {
		return "", nil, fmt.Errorf("invalid success event data type")
	}

	// 计算成本
	tokens := tokenUsageFromCompleteData(data)

	inputCost, outputCost, cacheCost, readCost, totalCost := ut.calculateCost(data.ModelName, tokens)

	// 🔧 [方案A补充] 支持 failure_reason 写入（用于数据质量标记，如 stream_truncated）
	query := fmt.Sprintf(`UPDATE request_logs SET
		end_time = ?,
		duration_ms = ?,
		model_name = ?,
		input_tokens = ?,
		output_tokens = ?,
		reasoning_tokens = ?,
		billable_output_tokens = ?,
		billing_output_token_source = ?,
		cache_creation_tokens = ?,
		cache_read_tokens = ?,
		input_cost_usd = ?,
		output_cost_usd = ?,
		cache_creation_cost_usd = ?,
		cache_read_cost_usd = ?,
		total_cost_usd = ?,
		failure_reason = ?,
		status = 'completed',
		updated_at = %s
	WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

	args := []interface{}{
		event.Timestamp,
		data.Duration.Milliseconds(),
		data.ModelName,
		data.InputTokens,
		data.OutputTokens,
		data.ReasoningTokens,
		ResolveBillableOutputTokens(tokens),
		NormalizeBillingOutputTokenSource(data.BillingOutputTokenSource),
		data.CacheCreationTokens,
		data.CacheReadTokens,
		inputCost,
		outputCost,
		cacheCost,
		readCost,
		totalCost,
		data.FailureReason, // 🔧 [方案A补充] 写入 failure_reason（可为空）
		event.RequestID,
	}

	return query, args, nil
}

// buildFinalFailureQuery 构建失败/取消完成的查询
// 一次性更新所有失败/取消相关字段：status, end_time, duration_ms, failure_reason/cancel_reason, 可选Token
// 🔧 [修复] 2025-12-11: 添加 model_name 和 5m/1h 缓存字段支持
func (ut *UsageTracker) buildFinalFailureQuery(event RequestEvent) (string, []interface{}, error) {
	data, ok := event.Data.(map[string]interface{})
	if !ok {
		return "", nil, fmt.Errorf("invalid final_failure event data type")
	}

	status, _ := data["status"].(string)
	reason, _ := data["reason"].(string)
	errorDetail, _ := data["error_detail"].(string)
	duration, _ := data["duration"].(time.Duration)
	httpStatus, _ := data["http_status"].(int)
	httpStatusIsUpstream, _ := data["http_status_is_upstream"].(bool)
	httpStatus, httpStatusIsUpstream = normalizeUpstreamHTTPStatus(httpStatus, httpStatusIsUpstream)
	var persistedHTTPStatus any
	if httpStatusIsUpstream {
		persistedHTTPStatus = httpStatus
	}
	modelName, _ := data["model_name"].(string) // 🔧 [修复] 2025-12-11: 读取模型名
	inputTokens := data["input_tokens"]
	outputTokens := data["output_tokens"]
	reasoningTokens := data["reasoning_tokens"]
	billableOutputTokens := data["billable_output_tokens"]
	billingOutputTokenSource := data["billing_output_token_source"]
	cacheCreationTokens := data["cache_creation_tokens"]
	cacheCreation5mTokens := data["cache_creation_5m_tokens"] // 🔧 [修复] 2025-12-11
	cacheCreation1hTokens := data["cache_creation_1h_tokens"] // 🔧 [修复] 2025-12-11
	cacheReadTokens := data["cache_read_tokens"]

	// 根据状态设置相应的reason字段
	var query string
	var args []interface{}

	if status == "cancelled" {
		query = fmt.Sprintf(`UPDATE request_logs SET
			end_time = ?,
			duration_ms = ?,
			status = 'cancelled',
			cancel_reason = ?,
			failure_reason = NULL,
			last_failure_reason = NULL,
			http_status_code = ?,
			http_status_is_upstream = ?,
			model_name = COALESCE(NULLIF(NULLIF(?, ''), 'unknown'), model_name),
			input_tokens = COALESCE(?, input_tokens),
			output_tokens = COALESCE(?, output_tokens),
			reasoning_tokens = COALESCE(?, reasoning_tokens),
			billable_output_tokens = COALESCE(?, billable_output_tokens),
			billing_output_token_source = COALESCE(?, billing_output_token_source),
			cache_creation_tokens = COALESCE(?, cache_creation_tokens),
			cache_creation_5m_tokens = COALESCE(?, cache_creation_5m_tokens),
			cache_creation_1h_tokens = COALESCE(?, cache_creation_1h_tokens),
			cache_read_tokens = COALESCE(?, cache_read_tokens),
			updated_at = %s
		WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

		args = []interface{}{
			event.Timestamp,
			duration.Milliseconds(),
			reason, // cancel_reason
			persistedHTTPStatus,
			httpStatusIsUpstream,
			modelName, // 🔧 [修复] 2025-12-11: model_name (空字符串和 'unknown' 都会被忽略)
			inputTokens,
			outputTokens,
			reasoningTokens,
			billableOutputTokens,
			billingOutputTokenSource,
			cacheCreationTokens,
			cacheCreation5mTokens, // 🔧 [修复] 2025-12-11
			cacheCreation1hTokens, // 🔧 [修复] 2025-12-11
			cacheReadTokens,
			event.RequestID,
		}
	} else {
		// status == "failed"
		query = fmt.Sprintf(`UPDATE request_logs SET
			end_time = ?,
			duration_ms = ?,
			status = 'failed',
			failure_reason = ?,
			last_failure_reason = ?,
			http_status_code = ?,
			http_status_is_upstream = ?,
			model_name = COALESCE(NULLIF(NULLIF(?, ''), 'unknown'), model_name),
			input_tokens = COALESCE(?, input_tokens),
			output_tokens = COALESCE(?, output_tokens),
			reasoning_tokens = COALESCE(?, reasoning_tokens),
			billable_output_tokens = COALESCE(?, billable_output_tokens),
			billing_output_token_source = COALESCE(?, billing_output_token_source),
			cache_creation_tokens = COALESCE(?, cache_creation_tokens),
			cache_creation_5m_tokens = COALESCE(?, cache_creation_5m_tokens),
			cache_creation_1h_tokens = COALESCE(?, cache_creation_1h_tokens),
			cache_read_tokens = COALESCE(?, cache_read_tokens),
			updated_at = %s
		WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

		args = []interface{}{
			event.Timestamp,
			duration.Milliseconds(),
			reason,      // failure_reason
			errorDetail, // last_failure_reason
			persistedHTTPStatus,
			httpStatusIsUpstream,
			modelName, // 🔧 [修复] 2025-12-11: model_name (空字符串和 'unknown' 都会被忽略)
			inputTokens,
			outputTokens,
			reasoningTokens,
			billableOutputTokens,
			billingOutputTokenSource,
			cacheCreationTokens,
			cacheCreation5mTokens, // 🔧 [修复] 2025-12-11
			cacheCreation1hTokens, // 🔧 [修复] 2025-12-11
			cacheReadTokens,
			event.RequestID,
		}
	}

	return query, args, nil
}

// buildCompleteQueryWithTx 在事务中构建完成事件查询，可以查询start_time计算准确持续时间
func (ut *UsageTracker) buildCompleteQueryWithTx(ctx context.Context, tx *sql.Tx, event RequestEvent) (string, []interface{}, error) {
	data, ok := event.Data.(RequestCompleteData)
	if !ok {
		return "", nil, fmt.Errorf("invalid complete event data type")
	}

	// 首先获取请求的开始时间来计算正确的持续时间
	var startTime time.Time
	var durationMs int64

	queryStartTime := `SELECT start_time FROM request_logs WHERE request_id = ?`
	err := tx.QueryRowContext(ctx, queryStartTime, event.RequestID).Scan(&startTime)
	if err != nil {
		if err == sql.ErrNoRows {
			// 记录不存在，使用估算的开始时间
			startTime = event.Timestamp.Add(-data.Duration)
			durationMs = data.Duration.Milliseconds()
		} else {
			return "", nil, fmt.Errorf("failed to get start time for duration calculation: %w", err)
		}
	} else {
		// 计算正确的持续时间：end_time - start_time
		durationMs = event.Timestamp.Sub(startTime).Milliseconds()
	}

	// 计算成本
	tokens := tokenUsageFromCompleteData(data)

	inputCost, outputCost, cacheCost, readCost, totalCost := ut.calculateCost(data.ModelName, tokens)

	// 使用计算出的准确持续时间
	query := fmt.Sprintf(`UPDATE request_logs SET
		end_time = ?,
		duration_ms = ?,
		model_name = ?,
		input_tokens = ?,
		output_tokens = ?,
		reasoning_tokens = ?,
		billable_output_tokens = ?,
		billing_output_token_source = ?,
		cache_creation_tokens = ?,
		cache_read_tokens = ?,
		input_cost_usd = ?,
		output_cost_usd = ?,
		cache_creation_cost_usd = ?,
		cache_read_cost_usd = ?,
		total_cost_usd = ?,
		status = CASE WHEN status != 'completed' THEN 'completed' ELSE status END,
		updated_at = %s
	WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

	args := []interface{}{
		event.Timestamp,
		durationMs, // 使用计算出的准确持续时间
		data.ModelName,
		data.InputTokens,
		data.OutputTokens,
		data.ReasoningTokens,
		ResolveBillableOutputTokens(tokens),
		NormalizeBillingOutputTokenSource(data.BillingOutputTokenSource),
		data.CacheCreationTokens,
		data.CacheReadTokens,
		inputCost,
		outputCost,
		cacheCost,
		readCost,
		totalCost,
		event.RequestID,
	}

	return query, args, nil
}
