package codexproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// cycleResult 端点循环结果
type cycleResult struct {
	completed           bool   // true: 响应已写给客户端；false: 端点耗尽，外层 loop 决定是否再来一轮
	routeChanged        bool   // true: 共享活跃渠道已变化，外层应按新渠道重建请求路由
	lastErr             error  // 最后一次端点失败的错误
	lastStatus          int    // 最后一次端点失败的 HTTP 状态码（0 = 网络错误）
	lastSummary         string // 最后一次上游错误响应摘要
	lastUpstreamStatus  int
	lastUpstreamHeaders http.Header
	lastUpstreamBody    []byte
}

// tryEndpointCycle 单轮端点列表遍历（含每个端点的同端点重试）
// 返回 completed=true 表示已写响应给客户端；false 表示需要外层 loop 判断
func (h *Handler) tryEndpointCycle(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	requestID string,
	startTime time.Time,
	isStreaming bool,
	channelName string,
	requestModel string,
	failedIDs *[]int64,
	endpointChain *[]chainNode,
	attemptCount *int,
) cycleResult {
	return h.tryEndpointCycleForStage(w, r, body, requestID, startTime, isStreaming, channelName, requestModel, failedIDs, endpointChain, attemptCount, nil)
}

// tryEndpointCycleForStage stage 非空时是请求级路由阶段：只在该阶段的渠道内换端点，
// 不跟随也不写入全局激活渠道（不检测激活渠道变化、不提升活跃端点、不改首选端点）。
func (h *Handler) tryEndpointCycleForStage(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	requestID string,
	startTime time.Time,
	isStreaming bool,
	channelName string,
	requestModel string,
	failedIDs *[]int64,
	endpointChain *[]chainNode,
	attemptCount *int,
	stage *codexRouteStage,
) cycleResult {
	originalBody := append([]byte(nil), body...)
	requestedModel := requestModel
	routed := stage != nil
	if !routed && h.activeChannelChanged(channelName) {
		return cycleResult{routeChanged: true}
	}
	// 选择起始端点
	var skippedGroups []credentialGroupRef
	var quotaSkippedGroups []credentialGroupRef
	endpoint, quotaSkippedGroups, err := h.selectQuotaEligibleEndpointForModel(
		r.Context(), channelName, requestedModel, *failedIDs, skippedGroups, quotaSkippedGroups, true, stage,
	)
	if err != nil && !errors.Is(err, errCredentialGroupDailyCostLimitExceeded) && !errors.Is(err, errRouteCandidateIneligible) && len(*endpointChain) == 0 {
		// 新请求不能被上一请求留下的全局 unhealthy 状态直接挡在选路之前。
		// 仅在尚未产生真实链路时重置一次；凭证分组跳过状态仍然保留。
		h.manager.ResetUnhealthy()
		endpoint, quotaSkippedGroups, err = h.selectQuotaEligibleEndpointForModel(
			r.Context(), channelName, requestedModel, *failedIDs, skippedGroups, quotaSkippedGroups, true, stage,
		)
	}
	if err != nil {
		h.logger.Warn("[Codex 当前轮无可用端点]",
			"provider", "codex",
			"request_id", requestID,
			"error", err,
			"endpoints_tried", len(*endpointChain),
			"channel", channelName,
		)
		// 交给外层 loop_retry；下一轮会重置历史 unhealthy 状态后重新选路。
		status := 0
		if errors.Is(err, errCredentialGroupDailyCostLimitExceeded) {
			status = http.StatusTooManyRequests
		}
		return cycleResult{completed: false, lastErr: err, lastStatus: status}
	}

	// 整条端点链上最后一次真实的上游 HTTP 响应。后面的端点只剩网络错误时，
	// 耗尽后透传的仍应是它，而不是被"没有响应"覆盖成 Relay 自造的 503。
	var lastUpstreamStatus int
	var lastUpstreamHeaders http.Header
	var lastUpstreamBody []byte
	if routed && stage.snapshotJSON != "" {
		h.recordRouteSnapshot(requestID, stage.snapshotJSON)
	}

	// 遍历当前轮可用端点
	for {
		if !routed && h.activeChannelChanged(channelName) {
			return cycleResult{routeChanged: true}
		}
		channel := h.manager.GetActiveChannel()
		// 使用本请求统一的隐私正文，再做模型改写（只动顶层 model 字段）
		targetChannel := channel
		if routed {
			targetChannel = stage.channel
		}
		attemptBody := h.outboundBody(r, requestID, targetChannel, originalBody)
		var attemptRewrite modelRewriteResult
		switch {
		case routed && stage.upstreamModel != "":
			attemptRewrite = rewriteModelForRoute(attemptBody, stage.upstreamModel, h.logger, requestID)
		case routed:
			attemptRewrite = rewriteModelForEndpoint(attemptBody, endpoint, stage.channel, h.logger, requestID)
		default:
			attemptRewrite = rewriteModelForEndpoint(attemptBody, endpoint, channel, h.logger, requestID)
		}
		body = attemptRewrite.Body
		requestModel = h.tokenParser.ParseModelFromBody(body)
		if h.usageTracker != nil && attemptRewrite.Source != "" {
			h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{
				OriginalModelName:  strPtr(attemptRewrite.OriginalModel),
				ModelRewriteSource: strPtr(attemptRewrite.Source),
				ModelRewriteTarget: strPtr(attemptRewrite.NewModel),
			})
		}
		// 追加链路节点（按 channel+credential_group+endpoint 切换去重）
		n := len(*endpointChain)
		previousAttemptFailed := n > 0 && ((*endpointChain)[n-1].StatusCode != 0 || (*endpointChain)[n-1].Error != "")
		if n == 0 ||
			previousAttemptFailed ||
			(*endpointChain)[n-1].Channel != channelName ||
			(*endpointChain)[n-1].CredentialGroup != endpoint.CredentialGroupName ||
			(*endpointChain)[n-1].Endpoint != endpoint.Name {
			closeLastHop(*endpointChain, startTime)
			*endpointChain = append(*endpointChain, chainNode{
				Hop:             n + 1,
				Channel:         channelName,
				CredentialGroup: endpoint.CredentialGroupName,
				Endpoint:        endpoint.Name,
				StartMs:         time.Since(startTime).Milliseconds(),
			})
		}
		h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)

		// 🆕 [Codex 生命周期] 端点选择日志
		h.logger.Info("[Codex 端点选择]",
			"provider", "codex",
			"request_id", requestID,
			"endpoint", endpoint.Name,
			"credential_group", endpoint.CredentialGroupName,
			"hop", len(*endpointChain),
			"channel", channelName,
		)

		h.logger.Debug("转发请求",
			"request_id", requestID,
			"endpoint", endpoint.Name,
			"url", endpoint.BaseURL,
			"streaming", isStreaming,
		)

		// 同端点重试转发
		var onAttempt func()
		if routed && stage.execution != nil {
			onAttempt = func() {
				stage.execution.Begin(stage.candidate(endpoint, requestedModel))
				h.recordRouteSnapshot(requestID, stage.executionSnapshot())
			}
		}
		resp, fwdErr, attempts := h.forwardWithRetry(r, body, endpoint, isStreaming, requestID, onAttempt)
		if attempts > 0 {
			(*endpointChain)[len(*endpointChain)-1].Attempts += attempts
			opts := tracking.UpdateOptions{}
			if attemptCount != nil {
				*attemptCount += attempts
				retryCount := *attemptCount - 1
				opts.RetryCount = &retryCount
			}
			// 成功路径不会再写链路；这里先把本跳次数落进热池，终态归档时才完整
			if chainBytes, err := json.Marshal(*endpointChain); err == nil {
				chainStr := string(chainBytes)
				opts.EndpointChain = &chainStr
			}
			if h.usageTracker != nil {
				h.usageTracker.RecordRequestUpdate(requestID, opts)
			}
		}

		// 客户端取消（context.Canceled）：直接结束
		if errors.Is(fwdErr, context.Canceled) {
			h.logger.Info("[Codex 请求取消]",
				"provider", "codex",
				"request_id", requestID,
				"endpoint", endpoint.Name,
				"duration_ms", time.Since(startTime).Milliseconds(),
				"reason", "client_canceled",
			)
			h.recordFailure(requestID, startTime, "client_cancelled", "client_canceled", nil, requestModel)
			if resp != nil {
				resp.Body.Close()
			}
			return cycleResult{completed: true}
		}

		// 成功路径：无错误且 status < 400
		if fwdErr == nil && resp != nil && resp.StatusCode < 400 {
			if routed && h.states != nil {
				resp.Body = routing.CaptureResponse(resp.Body, func(data []byte) { h.observeRoutedState(r, stage.candidate(endpoint, requestedModel), data) })
			}
			if isStreaming {
				streamErr, served := h.handleStreaming(w, resp, requestID, startTime, requestModel, endpoint.Name, endpoint.CredentialGroupName, channelName, len(*endpointChain))
				if streamErr == nil {
					h.markEndpointServed(endpoint, served, len(*failedIDs) > 0, channelName, requestID, routed)
					return cycleResult{completed: true}
				}
				fwdErr = streamErr
			} else {
				served := h.handleNonStreaming(w, resp, requestID, startTime, requestModel, endpoint.Name, endpoint.CredentialGroupName, channelName, len(*endpointChain))
				h.markEndpointServed(endpoint, served, len(*failedIDs) > 0, channelName, requestID, routed)
				return cycleResult{completed: true}
			}
		}

		// 失败路径：先读取上游响应摘要（仅当 resp 非空）
		var errSummary string
		var errBody []byte
		var status int
		if resp != nil {
			status = resp.StatusCode
			if status == http.StatusUnauthorized && endpoint.AuthMode == subscription.ClientPassthrough && h.subscriptions != nil {
				if resp.Request != nil {
					h.subscriptions.RejectCredential(resp.Request.Context())
				}
			}
			errBody, errSummary = h.readErrorBody(resp)
			errBody = []byte(subscription.Redact(r.Context(), string(errBody)))
			errSummary = subscription.Redact(r.Context(), errSummary)
			lastUpstreamStatus = status
			lastUpstreamHeaders = resp.Header.Clone()
			lastUpstreamBody = append([]byte(nil), errBody...)
		} else if streamErr, ok := fwdErr.(*UpstreamStreamError); ok {
			// status 留 0：响应已被流式路径消费掉，这里拿不到上游的状态码。
			// 链路里显示成空白，而不是编一个从未返回过的 429/401。
			errSummary = streamErr.Error()
		}

		// 三态分类
		action := h.classifier.Classify(resp, fwdErr)
		var routeFailure routing.Failure
		if routed {
			if errors.Is(fwdErr, routing.ErrBudgetExhausted) {
				return cycleResult{lastErr: fwdErr, lastUpstreamStatus: lastUpstreamStatus, lastUpstreamHeaders: lastUpstreamHeaders, lastUpstreamBody: lastUpstreamBody}
			}
			routeFailure = codexRouteFailure(status, errBody, fwdErr)
			if stage.execution != nil {
				stage.execution.Fail(stage.candidate(endpoint, requestedModel), routeFailure)
				h.recordRouteSnapshot(requestID, stage.executionSnapshot())
			}
			if routeFailure.Terminal || (status == http.StatusUnauthorized && subscription.FromContext(r.Context()) != nil) {
				action = ActionFailFast
			} else {
				action = ActionFailover
			}
		}

		// FailFast：透传上游 4xx 错误响应，不再尝试任何重试或切换
		if action == ActionFailFast {
			if resp != nil {
				detail := string(errBody)
				if strings.TrimSpace(detail) == "" {
					detail = errSummary
				}
				// 链路存上游完整原文（不是日志用的 500 字单行摘要），排查时才能看清服务商的真实报错
				annotateLastChainError(endpointChain, status, chainBodyText(errBody, errSummary), resp.Header, false)
				h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)
				h.manager.SetFailureDetail(endpoint.ID, status, detail)
				h.logger.Error("[Codex 请求失败]",
					"provider", "codex",
					"request_id", requestID,
					"failure_reason", "server_error",
					"detail", detail,
					"duration_ms", time.Since(startTime).Milliseconds(),
					"endpoints_tried", len(*endpointChain),
					"channel", channelName,
				)
				h.recordUpstreamFailure(requestID, startTime, "server_error", detail, resp.StatusCode, nil, requestModel)
				h.passthroughWithBody(w, resp, errBody)
				return cycleResult{completed: true}
			}
			// 无上游响应的 FailFast：目前只有响应头超时会走到这里（请求已被上游接收，不重放）
			detail := "请求失败"
			if fwdErr != nil {
				detail = fwdErr.Error()
			}
			annotateLastChainError(endpointChain, 0, detail, nil, true)
			h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)
			h.manager.SetFailureDetail(endpoint.ID, 0, detail)
			h.logger.Error("[Codex 请求失败]",
				"provider", "codex",
				"request_id", requestID,
				"failure_reason", "network_error",
				"detail", detail,
				"duration_ms", time.Since(startTime).Milliseconds(),
				"endpoints_tried", len(*endpointChain),
				"channel", channelName,
			)
			h.recordFailure(requestID, startTime, "network_error", detail, nil, requestModel)
			h.writeError(w, http.StatusBadGateway, detail)
			return cycleResult{completed: true}
		}

		// ActionRetry / ActionFailover：标记当前端点不健康并切换下一个
		if resp != nil {
			resp.Body.Close()
		}
		failedEndpointName := endpoint.Name
		*failedIDs = appendUniqueInt64(*failedIDs, endpoint.ID)

		// 失败原因日志
		var reason string
		if fwdErr != nil {
			reason = fwdErr.Error()
			h.logger.Warn(fmt.Sprintf("⚠️ [Codex故障转移] [%s] 失败端点: %s, 已尝试: %d 端点, 原因: %v",
				requestID, failedEndpointName, len(*endpointChain), fwdErr),
				"provider", "codex", "request_id", requestID)
		} else {
			reason = errSummary
			h.logger.Warn(fmt.Sprintf("🔁 [Codex故障转移] [%s] 端点: %s, HTTP状态: %d, 上游响应: %s",
				requestID, failedEndpointName, status, errSummary),
				"provider", "codex", "request_id", requestID)
		}
		var respHeaders http.Header
		if resp != nil {
			respHeaders = resp.Header
		}
		chainText := chainErrorText(fwdErr, errSummary, reason)
		if status > 0 {
			// 拿到上游响应时存完整原文，而不是日志摘要
			chainText = chainBodyText(errBody, chainText)
		}
		_, isStreamEvent := fwdErr.(*UpstreamStreamError)
		annotateLastChainError(endpointChain, status, chainText, respHeaders, resp == nil && !isStreamEvent)
		h.recordForwarding(requestID, endpoint.Name, channelName, requestModel, *endpointChain)
		if !routed || routeFailure.Scope == routing.ScopeEndpoint {
			h.manager.MarkUnhealthyWithDetail(endpoint.ID, status, reason)
		}

		// 上游明确给了 retry after 提示：先等够再发下一次，无论下一跳是换端点还是新一轮循环。
		if wait := retryAfterHint(resp, reason); wait > 0 && h.manager.PoolRuntime() == nil && subscription.FromContext(r.Context()) == nil && (!routed || routeFailure.Scope != routing.ScopeModel) {
			h.logger.Info("[Codex 遵守上游 retry-after]",
				"provider", "codex",
				"request_id", requestID,
				"endpoint", failedEndpointName,
				"wait_ms", wait.Milliseconds(),
			)
			if waitErr := routing.BudgetFromContext(r.Context()).Wait(r.Context(), wait); waitErr != nil {
				if routed {
					h.recordRouteSnapshot(requestID, stage.executionSnapshot())
				}
				if errors.Is(waitErr, routing.ErrBudgetExhausted) {
					return cycleResult{lastErr: waitErr, lastUpstreamStatus: lastUpstreamStatus, lastUpstreamHeaders: lastUpstreamHeaders, lastUpstreamBody: lastUpstreamBody}
				}
				h.recordFailure(requestID, startTime, "client_cancelled", "client_canceled_during_retry_after", nil, requestModel)
				return cycleResult{completed: true}
			}
		}

		// 选下个端点
		next, updatedQuotaSkippedGroups, nextErr := h.selectQuotaEligibleEndpointForModel(
			r.Context(), channelName, requestedModel, *failedIDs, skippedGroups, quotaSkippedGroups, false, stage,
		)
		quotaSkippedGroups = updatedQuotaSkippedGroups
		if nextErr != nil {
			// 当前轮端点列表耗尽，返回让外层 loop 决定是否再来一轮
			result := cycleResult{
				completed:   false,
				lastErr:     endpointCycleLastError(nextErr, fwdErr, status),
				lastStatus:  quotaAwareStatus(nextErr, status),
				lastSummary: errSummary,
			}
			if lastUpstreamStatus > 0 && !errors.Is(nextErr, errCredentialGroupDailyCostLimitExceeded) {
				result.lastUpstreamStatus = lastUpstreamStatus
				result.lastUpstreamHeaders = lastUpstreamHeaders
				result.lastUpstreamBody = lastUpstreamBody
			}
			return result
		}

		h.logger.Warn("[Codex 故障转移]",
			"provider", "codex",
			"request_id", requestID,
			"failed_endpoint", failedEndpointName,
			"retry_count", len(*endpointChain),
			"next_endpoint", next.Name,
			"reason", reason,
		)
		endpoint = next
	}
}

// markEndpointServed 只有上游把这次请求完整服务掉了，才把端点记为健康并设为下一次请求的首选。
// 流中途报错、正文读取失败的端点不能成为首选，否则下一次请求会优先撞回它。
// routed 为 true 时是请求级路由：只记录端点健康，不改首选端点、不提升活跃端点。
func (h *Handler) markEndpointServed(endpoint *CodexEndpoint, served bool, failedOver bool, channelName, requestID string, routed bool) {
	if !served {
		h.manager.ClearPreferredEndpoint(endpoint.ID)
		return
	}
	h.manager.MarkHealthy(endpoint.ID)
	if routed {
		return
	}
	h.manager.MarkEndpointRequestSuccess(endpoint.ID)
	// 故障转移后将当前成功端点提升为活跃，刷新前端 UI
	if failedOver && !h.activeChannelChanged(channelName) {
		if err := h.manager.PromoteActive(endpoint.ID); err != nil {
			h.logger.Warn("故障转移后提升活跃端点失败",
				"request_id", requestID,
				"endpoint_id", endpoint.ID,
				"endpoint", endpoint.Name,
				"error", err,
			)
		}
	}
}

// forwardWithRetry 单端点同端点重试（指数退避）
// 仅在 classifier 返回 ActionRetry 时重试（瞬时错误：网络错误、408、425）
// 其他情况（成功、ActionFailover、ActionFailFast）立即返回，由外层 cycle 处理
func (h *Handler) forwardWithRetry(
	r *http.Request,
	body []byte,
	endpoint *CodexEndpoint,
	isStreaming bool,
	requestID string,
	onAttempt ...func(),
) (*http.Response, error, int) {
	fwd := h.forwarder
	if isStreaming {
		fwd = h.streamFwd
	}

	retryCfg := h.retryConfig()
	maxAttempts := retryCfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastResp *http.Response
	var lastErr error
	attempts := 0

	if h.manager.PoolRuntime() != nil {
		maxAttempts = 1
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := computeBackoff(attempt-1, retryCfg.BaseDelay, retryCfg.MaxDelay, retryCfg.Multiplier)
			h.logger.Info("[Codex 同端点重试]",
				"provider", "codex",
				"request_id", requestID,
				"endpoint", endpoint.Name,
				"attempt", attempt+1,
				"max_attempts", maxAttempts,
				"delay_ms", delay.Milliseconds(),
			)
			if err := routing.BudgetFromContext(r.Context()).Wait(r.Context(), delay); err != nil {
				return lastResp, err, attempts
			}
		}

		sendRequest, lease, admitErr := h.admitSubscription(r, endpoint, body)
		if admitErr != nil {
			routing.PoolAttemptFromContext(sendRequest.Context()).Finish(false)
			return lastResp, admitErr, attempts
		}
		if err := routing.BudgetFromContext(r.Context()).Begin(r.Context()); err != nil {
			lease.Finish(0, nil, nil, false)
			routing.PoolAttemptFromContext(sendRequest.Context()).Finish(false)
			return lastResp, err, attempts
		}
		if lastResp != nil {
			lastResp.Body.Close()
			lastResp = nil
		}
		if len(onAttempt) > 0 && onAttempt[0] != nil {
			onAttempt[0]()
		}
		resp, err := fwd.Forward(sendRequest, body, endpoint)
		attempts++
		lastResp = resp
		lastErr = err

		// 客户端取消直接返回
		if errors.Is(err, context.Canceled) {
			return resp, err, attempts
		}

		// 只有 ActionRetry 才继续重试；其他动作（含成功、ActionFailover、ActionFailFast）立即返回
		action := h.classifier.Classify(resp, err)
		if action != ActionRetry {
			return resp, err, attempts
		}
		// ActionRetry 但已是最后一次：返回让外层 cycle 切换端点
		if attempt+1 >= maxAttempts {
			return resp, err, attempts
		}
	}

	return lastResp, lastErr, attempts
}

// buildExhaustedDetail 构造端点耗尽时的错误描述
func (h *Handler) buildExhaustedDetail(maxLoops int, chain []chainNode, lastErr error, lastStatus int, lastSummary string) string {
	if strings.TrimSpace(lastSummary) != "" {
		return lastSummary
	}
	if lastErr != nil {
		return lastErr.Error()
	}
	return ""
}

// activeChannelName 获取当前活跃 Codex 渠道名（无则返回空串）
func (h *Handler) activeChannelName() string {
	if h.manager == nil {
		return ""
	}
	ch := h.manager.GetActiveChannel()
	if ch == nil {
		return ""
	}
	return ch.Name
}

func (h *Handler) activeChannelChanged(expectedName string) bool {
	if h.manager == nil || expectedName == "" {
		return false
	}
	active := h.manager.GetActiveChannel()
	return active != nil && active.Name != expectedName
}
