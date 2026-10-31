package tracking

import (
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

// RecordRequestSuccess 记录请求成功完成
// 一次性更新所有成功相关字段：status='completed', end_time, duration_ms, Token和成本信息
func (ut *UsageTracker) RecordRequestSuccess(requestID, modelName string, tokens *TokenUsage, duration time.Duration) {
	ut.recordRequestSuccess(requestID, modelName, tokens, duration, 200, false, "")
}

// RecordRequestSuccessWithQuality 记录请求成功完成（支持数据质量标记）
// 🔧 [方案A实现] 2025-12-20: 原子操作，在 CompleteAndArchive 中一次性设置所有字段包括 failureReason
// 一次性更新所有成功相关字段：status='completed', end_time, duration_ms, Token、成本信息和可选的 failure_reason
func (ut *UsageTracker) RecordRequestSuccessWithQuality(requestID, modelName string, tokens *TokenUsage, duration time.Duration, failureReason string) {
	ut.recordRequestSuccess(requestID, modelName, tokens, duration, 200, false, failureReason)
}

// RecordRequestSuccessWithHTTPStatus 记录带真实上游状态码的成功请求。
func (ut *UsageTracker) RecordRequestSuccessWithHTTPStatus(requestID, modelName string, tokens *TokenUsage, duration time.Duration, httpStatus int) {
	ut.recordRequestSuccess(requestID, modelName, tokens, duration, httpStatus, true, "")
}

func (ut *UsageTracker) recordRequestSuccess(requestID, modelName string, tokens *TokenUsage, duration time.Duration, httpStatus int, httpStatusIsUpstream bool, failureReason string) {
	if ut.config == nil || !ut.config.Enabled {
		return
	}
	httpStatus, httpStatusIsUpstream = normalizeUpstreamHTTPStatus(httpStatus, httpStatusIsUpstream)

	// 🚀 [架构修复] 支持 nil tokens，确保耗时信息总是被记录
	var inputTokens, outputTokens, reasoningTokens, billableOutputTokens, cacheCreationTokens, cacheReadTokens int64
	var cacheCreation5mTokens, cacheCreation1hTokens, cacheWrite30mTokens, cacheWrite12hTokens int64
	var billingOutputTokenSource string
	inputIncludesCacheRead := false
	if tokens != nil {
		inputTokens = tokens.InputTokens
		outputTokens = tokens.OutputTokens
		reasoningTokens = tokens.ReasoningTokens
		billableOutputTokens = ResolveBillableOutputTokens(tokens)
		billingOutputTokenSource = tokens.BillingOutputTokenSource
		cacheCreationTokens = tokens.CacheCreationTokens
		cacheWrite30mTokens = tokens.CacheWrite30mTokens
		cacheWrite12hTokens = tokens.CacheWrite12hTokens
		cacheCreation5mTokens = tokens.CacheCreation5mTokens
		cacheCreation1hTokens = tokens.CacheCreation1hTokens
		cacheReadTokens = tokens.CacheReadTokens
		inputIncludesCacheRead = tokens.InputIncludesCacheRead

		// 🔧 [向后兼容修复] 如果 5m/1h 都是 0 但总数不为 0，
		// 将总数分配给 5m 字段，与成本计算逻辑保持一致（tracker.go:185-193）
		if cacheCreation5mTokens == 0 && cacheCreation1hTokens == 0 && cacheCreationTokens > 0 {
			cacheCreation5mTokens = cacheCreationTokens // 默认按 5m 缓存处理
			slog.Debug("🔄 [缓存Token分配] 向后兼容模式：将总缓存Token分配到5m字段",
				"request_id", requestID,
				"cache_creation_tokens", cacheCreationTokens)
		}
	}
	// 如果 tokens 为 nil，所有 token 字段都是 0，但 duration 仍然会被记录

	// 🔥 热池模式：完成请求并归档（核心：单次数据库写入）
	if ut.hotPoolEnabled && ut.hotPool != nil {
		now := ut.now()
		err := ut.hotPool.CompleteAndArchive(requestID, func(req *ActiveRequest) {
			req.Status = "completed"
			if httpStatusIsUpstream {
				req.HTTPStatus = httpStatus
				req.HTTPStatusIsUpstream = true
			} else if !req.HTTPStatusIsUpstream {
				req.HTTPStatus = 0
			}
			req.ModelName = modelName
			// 🔧 [P1.4 防御] 仅当本次提供的 token 非零，或之前没有被失败路径记录过时，才覆盖
			// 避免"先 RecordFailedRequestTokens 记录了真实 token → 后 CompleteAndArchive 传入零值覆盖"
			shouldOverwriteTokens := !req.TokenRecordedByFailedPath ||
				inputTokens > 0 || outputTokens > 0 ||
				reasoningTokens > 0 || billableOutputTokens > 0 ||
				cacheCreationTokens > 0 || cacheCreation5mTokens > 0 ||
				cacheCreation1hTokens > 0 || cacheReadTokens > 0
			if shouldOverwriteTokens {
				applyTokenUsageToActiveRequest(req, &TokenUsage{
					InputTokens:              inputTokens,
					OutputTokens:             outputTokens,
					ReasoningTokens:          reasoningTokens,
					BillableOutputTokens:     billableOutputTokens,
					BillingOutputTokenSource: billingOutputTokenSource,
					CacheCreationTokens:      cacheCreationTokens,
					CacheWrite30mTokens:      cacheWrite30mTokens,
					CacheWrite12hTokens:      cacheWrite12hTokens,
					CacheCreation5mTokens:    cacheCreation5mTokens,
					CacheCreation1hTokens:    cacheCreation1hTokens,
					CacheReadTokens:          cacheReadTokens,
					InputIncludesCacheRead:   inputIncludesCacheRead,
				})
			} else {
				// 保留 RecordFailedRequestTokens 记录的 token 值
				slog.Debug("🔒 [P1.4 防御] 保留失败路径记录的 token，避免被零值覆盖",
					"request_id", requestID,
					"existing_input", req.InputTokens,
					"existing_output", req.OutputTokens)
			}
			req.EndTime = &now
			req.DurationMs = duration.Milliseconds()
			// 🔧 [方案A实现] 2025-12-20: 显式覆盖 failureReason（无论是否为空）
			// 避免之前中途错误设置的旧值残留，导致"成功但带失败原因"的误标
			req.FailureReason = failureReason
			req.LastFailureReason = ""
			// 成本在归档时计算
		})
		if err != nil {
			// 🔧 [P5.1] 不再降级到 traditional eventChan，改为持久化失败队列
			ut.triggerHotPoolAlert("complete_archive_failed")
			ut.recordToFailureQueue("complete", map[string]any{
				"request_id":                  requestID,
				"model_name":                  modelName,
				"input_tokens":                inputTokens,
				"output_tokens":               outputTokens,
				"reasoning_tokens":            reasoningTokens,
				"billable_output_tokens":      billableOutputTokens,
				"billing_output_token_source": billingOutputTokenSource,
				"cache_creation_tokens":       cacheCreationTokens,
				"cache_creation_5m_tokens":    cacheCreation5mTokens,
				"cache_creation_1h_tokens":    cacheCreation1hTokens,
				"cache_read_tokens":           cacheReadTokens,
				"duration_ms":                 duration.Milliseconds(),
				"http_status":                 httpStatus,
				"http_status_is_upstream":     httpStatusIsUpstream,
				"failure_reason":              failureReason,
				"error":                       err.Error(),
			})
		}
		return
	}

	// 🔧 [P5.1] HotPool 未启用的极端场景：直接走失败队列
	ut.recordToFailureQueue("complete", map[string]any{
		"request_id":              requestID,
		"model_name":              modelName,
		"reason":                  "hot_pool_disabled",
		"input_tokens":            inputTokens,
		"output_tokens":           outputTokens,
		"http_status":             httpStatus,
		"http_status_is_upstream": httpStatusIsUpstream,
	})
}

// RecordRequestFinalFailure 记录请求最终失败或取消
// 一次性更新所有失败/取消相关字段：status, end_time, duration_ms, failure_reason/cancel_reason, http_status_code, 可选Token
// 🔧 [修复] 2025-12-11: 添加 modelName 参数，确保取消/失败请求能正确计算成本
func (ut *UsageTracker) RecordRequestFinalFailure(requestID, modelName, status, reason, errorDetail string, duration time.Duration, httpStatus int, tokens *TokenUsage) {
	ut.recordRequestFinalFailure(requestID, modelName, status, reason, errorDetail, duration, httpStatus, false, tokens)
}

// RecordRequestFinalFailureWithUpstreamStatus records a final failure and marks
// the status code as provider-originated when it came from a real HTTP response.
func (ut *UsageTracker) RecordRequestFinalFailureWithUpstreamStatus(requestID, modelName, status, reason, errorDetail string, duration time.Duration, httpStatus int, tokens *TokenUsage) {
	ut.recordRequestFinalFailure(requestID, modelName, status, reason, errorDetail, duration, httpStatus, true, tokens)
}

func (ut *UsageTracker) recordRequestFinalFailure(requestID, modelName, status, reason, errorDetail string, duration time.Duration, httpStatus int, httpStatusIsUpstream bool, tokens *TokenUsage) {
	if ut.config == nil || !ut.config.Enabled {
		return
	}
	httpStatus, httpStatusIsUpstream = normalizeUpstreamHTTPStatus(httpStatus, httpStatusIsUpstream)

	// 处理Token信息（失败/取消时可能有也可能没有）
	var inputTokens, outputTokens, reasoningTokens, billableOutputTokens, cacheCreationTokens, cacheReadTokens int64
	var cacheCreation5mTokens, cacheCreation1hTokens, cacheWrite30mTokens, cacheWrite12hTokens int64 // 🔧 [修复] 2025-12-11: 添加 5m/1h 缓存字段
	var billingOutputTokenSource string
	inputIncludesCacheRead := false
	if tokens != nil {
		inputTokens = tokens.InputTokens
		outputTokens = tokens.OutputTokens
		reasoningTokens = tokens.ReasoningTokens
		billableOutputTokens = ResolveBillableOutputTokens(tokens)
		billingOutputTokenSource = tokens.BillingOutputTokenSource
		cacheCreationTokens = tokens.CacheCreationTokens
		cacheWrite30mTokens = tokens.CacheWrite30mTokens
		cacheWrite12hTokens = tokens.CacheWrite12hTokens
		cacheCreation5mTokens = tokens.CacheCreation5mTokens
		cacheCreation1hTokens = tokens.CacheCreation1hTokens
		cacheReadTokens = tokens.CacheReadTokens
		inputIncludesCacheRead = tokens.InputIncludesCacheRead

		// 🔧 [向后兼容修复] 如果 5m/1h 都是 0 但总数不为 0，
		// 将总数分配给 5m 字段，与成本计算逻辑保持一致
		if cacheCreation5mTokens == 0 && cacheCreation1hTokens == 0 && cacheCreationTokens > 0 {
			cacheCreation5mTokens = cacheCreationTokens
		}
	}

	var legacyTokens *TokenUsage
	if tokens != nil {
		legacyTokens = &TokenUsage{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
			ReasoningTokens:          reasoningTokens,
			BillableOutputTokens:     billableOutputTokens,
			BillingOutputTokenSource: billingOutputTokenSource,
			CacheCreationTokens:      cacheCreationTokens,
			CacheWrite30mTokens:      cacheWrite30mTokens,
			CacheWrite12hTokens:      cacheWrite12hTokens,
			CacheCreation5mTokens:    cacheCreation5mTokens,
			CacheCreation1hTokens:    cacheCreation1hTokens,
			CacheReadTokens:          cacheReadTokens,
			InputIncludesCacheRead:   inputIncludesCacheRead,
		}
	}

	// 🔥 热池模式：完成失败请求并归档
	if ut.hotPoolEnabled && ut.hotPool != nil {
		now := ut.now()
		err := ut.hotPool.CompleteAndArchive(requestID, func(req *ActiveRequest) {
			req.Status = status
			if httpStatusIsUpstream {
				req.HTTPStatus = httpStatus
				req.HTTPStatusIsUpstream = true
			} else {
				req.HTTPStatus = 0
				req.HTTPStatusIsUpstream = false
			}
			// 🔧 [修复] 2025-12-11: 设置模型名，否则成本计算会失败
			if modelName != "" && modelName != "unknown" {
				req.ModelName = modelName
			}
			// 🔧 [修复] 2025-12-15: 只有当 tokens 参数不为 nil 时才更新 token 字段
			// 否则保留热池中已有的值（可能由 RecordFailedRequestTokens 设置）
			if tokens != nil {
				applyTokenUsageToActiveRequest(req, legacyTokens)
			}
			req.EndTime = &now
			req.DurationMs = duration.Milliseconds()
			// 根据状态设置原因字段
			if status == "cancelled" {
				req.CancelReason = reason
				// 取消为终态：清理可能已写入的 failure_reason，避免前端同时展示 failure_reason + cancel_reason 造成误解。
				req.FailureReason = ""
				req.LastFailureReason = ""
			} else {
				req.FailureReason = reason
				req.LastFailureReason = errorDetail
			}
		})
		if err != nil {
			// 🔧 [P5.1] 不再降级到 traditional eventChan，改为持久化失败队列
			ut.triggerHotPoolAlert("fail_archive_failed")
			ut.recordToFailureQueue("fail", map[string]any{
				"request_id":              requestID,
				"model_name":              modelName,
				"status":                  status,
				"reason":                  reason,
				"error_detail":            errorDetail,
				"duration_ms":             duration.Milliseconds(),
				"http_status":             httpStatus,
				"http_status_is_upstream": httpStatusIsUpstream,
				"input_tokens": func() int64 {
					if legacyTokens != nil {
						return legacyTokens.InputTokens
					}
					return 0
				}(),
				"output_tokens": func() int64 {
					if legacyTokens != nil {
						return legacyTokens.OutputTokens
					}
					return 0
				}(),
				"reasoning_tokens": func() int64 {
					if legacyTokens != nil {
						return legacyTokens.ReasoningTokens
					}
					return 0
				}(),
				"billable_output_tokens": func() int64 {
					if legacyTokens != nil {
						return ResolveBillableOutputTokens(legacyTokens)
					}
					return 0
				}(),
				"error": err.Error(),
			})
		}
		return
	}

	// 🔧 [P5.1] HotPool 未启用的极端场景：直接走失败队列
	ut.recordToFailureQueue("fail", map[string]any{
		"request_id": requestID,
		"reason":     "hot_pool_disabled",
		"status":     status,
		"detail":     errorDetail,
	})
}
