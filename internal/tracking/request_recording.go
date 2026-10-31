package tracking

import (
	"log/slog"

	_ "modernc.org/sqlite"
)

// RecordRequestStart 记录请求开始
func (ut *UsageTracker) RecordRequestStart(requestID, clientIP, userAgent, method, path string, isStreaming bool) {
	if ut.config == nil || !ut.config.Enabled {
		return
	}

	// 🔥 热池模式：直接添加到内存热池
	if ut.hotPoolEnabled && ut.hotPool != nil {
		req := NewActiveRequest(requestID, clientIP, userAgent, method, path, isStreaming)
		if err := ut.hotPool.Add(req); err != nil {
			// 🔧 [P5.8] 热池满 → 触发告警
			ut.triggerHotPoolAlert("hot_pool_full")
			// 🔧 [P5.1] 不再降级到 traditional eventChan，改为持久化失败队列
			ut.recordToFailureQueue("start", map[string]any{
				"request_id":   requestID,
				"client_ip":    clientIP,
				"user_agent":   userAgent,
				"method":       method,
				"path":         path,
				"is_streaming": isStreaming,
				"error":        err.Error(),
			})
		}
		return
	}

	// 🔧 [P5.1] HotPool 未启用的极端场景：直接走失败队列
	// （正常运行 hotPoolEnabled=true，此分支不会触发）
	ut.recordToFailureQueue("start", map[string]any{
		"request_id": requestID,
		"reason":     "hot_pool_disabled",
	})
}

// RecordRequestUpdate 统一的请求更新方法
// 支持可选字段更新，只更新非nil的字段，适用于所有中间过程状态变更
func (ut *UsageTracker) RecordRequestUpdate(requestID string, opts UpdateOptions) {
	if ut.config == nil || !ut.config.Enabled {
		return
	}

	// 🔥 热池模式：直接更新内存（核心优化点：无数据库写入）
	if ut.hotPoolEnabled && ut.hotPool != nil {
		err := ut.hotPool.Update(requestID, func(req *ActiveRequest) {
			// 只更新非nil的字段
			if opts.EndpointName != nil {
				req.EndpointName = *opts.EndpointName
			}
			if opts.EndpointChain != nil {
				req.EndpointChain = *opts.EndpointChain
			}
			if opts.Channel != nil {
				req.Channel = *opts.Channel
			}
			if opts.GroupName != nil {
				req.GroupName = *opts.GroupName
			}
			if opts.Status != nil {
				req.Status = *opts.Status
			}
			if opts.RetryCount != nil {
				req.RetryCount = *opts.RetryCount
			}
			if opts.HttpStatus != nil {
				isUpstream := opts.HttpStatusIsUpstream != nil && *opts.HttpStatusIsUpstream
				req.HTTPStatus, req.HTTPStatusIsUpstream = normalizeUpstreamHTTPStatus(*opts.HttpStatus, isUpstream)
			} else if opts.HttpStatusIsUpstream != nil && !*opts.HttpStatusIsUpstream {
				req.HTTPStatus = 0
				req.HTTPStatusIsUpstream = false
			}
			if opts.ModelName != nil {
				req.ModelName = *opts.ModelName
			}
			if opts.OriginalModelName != nil {
				req.OriginalModelName = *opts.OriginalModelName
			}
			if opts.ModelRewriteSource != nil {
				req.ModelRewriteSource = *opts.ModelRewriteSource
			}
			if opts.ModelRewriteTarget != nil {
				req.ModelRewriteTarget = *opts.ModelRewriteTarget
			}
			if opts.RouteSnapshot != nil {
				req.RouteSnapshot = *opts.RouteSnapshot
			}
			if opts.PrivacyRedacted != nil {
				req.PrivacyRedacted = *opts.PrivacyRedacted
			}
			if opts.PrivacySummary != nil {
				req.PrivacySummary = *opts.PrivacySummary
			}
			if opts.FailureReason != nil {
				req.FailureReason = *opts.FailureReason
			}
			if opts.Provider != nil {
				req.Provider = *opts.Provider
			}
			if opts.ReasoningEffort != nil {
				req.ReasoningEffort = *opts.ReasoningEffort
			}
			if opts.RequestedServiceTier != nil {
				req.RequestedServiceTier = *opts.RequestedServiceTier
			}
			if opts.ResponseServiceTier != nil {
				req.ResponseServiceTier = *opts.ResponseServiceTier
			}
			if opts.RequestedServiceTier != nil || opts.ResponseServiceTier != nil {
				resolveActiveRequestServiceTier(req)
			}
			if opts.ServiceTier != nil {
				req.ServiceTier = *opts.ServiceTier
				if req.RequestedServiceTier == "" && opts.ResponseServiceTier == nil {
					req.RequestedServiceTier = *opts.ServiceTier
				}
			}
		})
		if err != nil {
			// 🔧 [P5.1] 不再降级到 traditional eventChan，改为 Debug 日志
			// 请求可能已归档或从未记录（如热池启动前的请求），属于正常情况
			slog.Debug("🔥 热池更新请求失败（请求可能已归档或从未记录）",
				"request_id", requestID,
				"error", err)
		}
		return
	}

	// 🔧 [P5.1] HotPool 未启用时，update 操作无意义（无活跃请求记录）
	slog.Debug("HotPool 未启用，跳过 update", "request_id", requestID)
}

// RecordPrivacyRedaction 记录隐私脱敏命中摘要。
// summary 只含规则名与命中数；命中原文不得落库，故此处不接受任何原文参数。
func (ut *UsageTracker) RecordPrivacyRedaction(requestID string, summary string) {
	if requestID == "" || summary == "" {
		return
	}
	redacted := true
	ut.RecordRequestUpdate(requestID, UpdateOptions{
		PrivacyRedacted: &redacted,
		PrivacySummary:  &summary,
	})
}

// RecordOutputSent records when the first response bytes were sent to the client.
func (ut *UsageTracker) RecordOutputSent(requestID string) {
	if ut.config == nil || !ut.config.Enabled || requestID == "" {
		return
	}

	if ut.hotPoolEnabled && ut.hotPool != nil {
		now := ut.now()
		if err := ut.hotPool.Update(requestID, func(req *ActiveRequest) {
			if req.OutputSentAt == nil {
				req.OutputSentAt = &now
			}
		}); err != nil {
			slog.Debug("🔥 热池记录首次输出失败（请求可能已归档或从未记录）",
				"request_id", requestID,
				"error", err)
		}
		return
	}

	slog.Debug("HotPool 未启用，跳过首次输出记录", "request_id", requestID)
}
