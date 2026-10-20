package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// insertRequestStart 插入请求开始记录
func (ut *UsageTracker) insertRequestStart(ctx context.Context, tx *sql.Tx, event RequestEvent) error {
	data, ok := event.Data.(RequestStartData)
	if !ok {
		return fmt.Errorf("invalid start event data type")
	}

	// 使用适配器构建INSERT OR REPLACE查询
	columns := []string{"request_id", "client_ip", "user_agent", "method", "path", "start_time", "status", "is_streaming", "environment", "updated_at"}
	placeholders := []string{"?", "?", "?", "?", "?", "?", "'pending'", "?", "?", ut.adapter.BuildDateTimeNow()}
	query := ut.adapter.BuildInsertOrReplaceQuery("request_logs", columns, placeholders)

	_, err := tx.ExecContext(ctx, query,
		event.RequestID,
		data.ClientIP,
		data.UserAgent,
		data.Method,
		data.Path,
		event.Timestamp,
		data.IsStreaming,
		ut.environment)

	return err
}

// updateRequestStatus 更新请求状态
func (ut *UsageTracker) updateRequestStatus(ctx context.Context, tx *sql.Tx, event RequestEvent) error {
	data, ok := event.Data.(RequestUpdateData)
	if !ok {
		return fmt.Errorf("invalid update event data type")
	}
	httpStatus, httpStatusIsUpstream := normalizeUpstreamHTTPStatus(data.HTTPStatus, data.HTTPStatusIsUpstream)
	var persistedHTTPStatus any
	if httpStatusIsUpstream {
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

		result, err := tx.ExecContext(ctx, query,
			data.Status,
			data.RetryCount,
			persistedHTTPStatus,
			httpStatusIsUpstream,
			event.RequestID)

		if err != nil {
			return err
		}

		// 检查是否更新了记录
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if rowsAffected == 0 {
			// 记录不存在，先创建一个基本记录
			return ut.insertRequestStart(ctx, tx, RequestEvent{
				Type:      "start",
				RequestID: event.RequestID,
				Timestamp: event.Timestamp,
				Data: RequestStartData{
					ClientIP:    "unknown",
					UserAgent:   "unknown",
					Method:      "unknown",
					Path:        "unknown",
					IsStreaming: false, // 默认为非流式
				},
			})
		}

		return nil
	}

	// 正常更新所有字段
	query := fmt.Sprintf(`UPDATE request_logs SET
		endpoint_name = ?,
		group_name = ?,
		status = ?,
		retry_count = ?,
		http_status_code = ?,
		http_status_is_upstream = ?,
		updated_at = %s
	WHERE request_id = ?`, ut.adapter.BuildDateTimeNow())

	result, err := tx.ExecContext(ctx, query,
		data.EndpointName,
		data.GroupName,
		data.Status,
		data.RetryCount,
		persistedHTTPStatus,
		httpStatusIsUpstream,
		event.RequestID)

	if err != nil {
		return err
	}

	// 检查是否更新了记录
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		// 记录不存在，使用适配器构建INSERT OR REPLACE查询
		columns := []string{"request_id", "endpoint_name", "group_name", "status", "retry_count", "http_status_code", "http_status_is_upstream", "start_time", "environment", "updated_at"}
		placeholders := []string{"?", "?", "?", "?", "?", "?", "?", "?", "?", ut.adapter.BuildDateTimeNow()}
		insertQuery := ut.adapter.BuildInsertOrReplaceQuery("request_logs", columns, placeholders)

		_, err = tx.ExecContext(ctx, insertQuery,
			event.RequestID,
			data.EndpointName,
			data.GroupName,
			data.Status,
			data.RetryCount,
			persistedHTTPStatus,
			httpStatusIsUpstream,
			event.Timestamp,
			ut.environment)
	}

	return err
}

// completeRequest 完成请求记录
func (ut *UsageTracker) completeRequest(ctx context.Context, tx *sql.Tx, event RequestEvent) error {
	data, ok := event.Data.(RequestCompleteData)
	if !ok {
		return fmt.Errorf("invalid complete event data type")
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
			return fmt.Errorf("failed to get start time for duration calculation: %w", err)
		}
	} else {
		// 计算正确的持续时间：end_time - start_time
		durationMs = event.Timestamp.Sub(startTime).Milliseconds()
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

	result, err := tx.ExecContext(ctx, query,
		event.Timestamp,
		durationMs, // 使用计算出的正确持续时间
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
		event.RequestID)

	if err != nil {
		return err
	}

	// 检查是否更新了记录
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		// 记录不存在，创建完整记录
		insertQuery := `INSERT INTO request_logs (
			request_id, start_time, end_time, duration_ms, model_name,
			input_tokens, output_tokens, reasoning_tokens, billable_output_tokens, billing_output_token_source,
			cache_creation_tokens, cache_read_tokens,
			input_cost_usd, output_cost_usd, cache_creation_cost_usd,
			cache_read_cost_usd, total_cost_usd, environment, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'completed')`

		// 使用已计算的 startTime 和 durationMs
		_, err = tx.ExecContext(ctx, insertQuery,
			event.RequestID,
			startTime,
			event.Timestamp,
			durationMs,
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
			ut.environment)
	}

	return err
}
