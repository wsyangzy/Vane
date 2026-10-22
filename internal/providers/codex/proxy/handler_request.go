package codexproxy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

// generateRequestID 生成请求 ID（统一 req- 前缀）
func generateRequestID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "req-" + hex.EncodeToString(b)
}

// strPtr 返回字符串指针（用于 UpdateOptions）
func strPtr(s string) *string { return &s }

func appendUniqueInt64(ids []int64, values ...int64) []int64 {
	if len(values) == 0 {
		return ids
	}
	seen := make(map[int64]struct{}, len(ids)+len(values))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	for _, id := range values {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func credentialGroupRefFromEndpoint(endpoint *CodexEndpoint) credentialGroupRef {
	if endpoint == nil {
		return credentialGroupRef{}
	}
	return credentialGroupRef{ID: endpoint.CredentialGroupID, Name: endpoint.CredentialGroupName}
}

// ServeHTTP 实现 http.Handler
// 三层重试结构：
//  1. 外层 loop_retry: 端点列表耗尽后是否回到首位再来一轮（受 failover.loop_retry/max_loops 控制）
//  2. 中层 endpoint cycle: 按优先级遍历当前轮可用端点
//  3. 内层 same-endpoint retry: 单端点的瞬时错误指数退避重试（受 retry.* 控制）
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := generateRequestID()
	if _, ok := routing.PoolCallerFromContext(r.Context()); ok {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			h.writeError(w, http.StatusUpgradeRequired, "使用 HTTP Responses 连接重试")
			return
		}
		if !subscription.AllowedPath(r) {
			h.writeError(w, http.StatusForbidden, "pool_path_rejected")
			return
		}
		r.Header = r.Header.Clone()
		for name := range r.Header {
			if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "X-Relay-Key") || strings.EqualFold(name, "ChatGPT-Account-ID") {
				delete(r.Header, name)
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		}
	}

	if sub := subscription.FromContext(r.Context()); sub != nil && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		h.writeError(w, http.StatusUpgradeRequired, "使用 HTTP Responses 连接重试")
		return
	}

	if r.Body == nil {
		r.Body = http.NoBody
	}
	// 1. 读取并缓存请求体（重试时需要重放）
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "读取请求体失败")
		return
	}
	defer r.Body.Close()
	originalBody := append([]byte(nil), body...)
	if h.privacyService != nil {
		result, privacyErr := h.privacyService.Apply(r.Context(), privacy.Request{
			RequestID:    requestID,
			Path:         r.URL.Path,
			Method:       r.Method,
			ProviderType: "codex",
			ContentType:  r.Header.Get("Content-Type"),
		}, originalBody)
		if privacyErr != nil {
			// 隐私策略拒绝：必须短路返回，不得进入重试 / 故障转移 / 端点冷却
			var policyErr *privacy.PolicyError
			if errors.As(privacyErr, &policyErr) {
				h.logger.Warn("隐私策略拒绝请求，已阻止上游发送",
					"request_id", requestID, "code", policyErr.Code, "path", r.URL.Path)
				h.writeError(w, policyErr.StatusCode, policyErr.Message)
				return
			}
			h.logger.Error("隐私防护预检失败，已阻止上游发送", "request_id", requestID, "error", privacyErr)
			h.writeError(w, http.StatusInternalServerError, "隐私防护处理失败")
			return
		}
		// 挂到请求 ctx 上：每次尝试选定端点后按其渠道隐私级别挑选实际发送的正文
		// （三方中转在全局「仅检测」下也强制脱敏、可信渠道只检测）。
		// 脱敏摘要不在这里记：发往可信渠道时正文没被改写，提前记录会让请求详情误标「已脱敏」；
		// 由 outboundBody 在第一次真正发出改写正文时记录。
		scanned := append([]byte(nil), originalBody...)
		r = r.WithContext(contextWithCodexPrivacy(r.Context(), &codexPrivacyBodies{original: scanned, result: result}))
		originalBody = result.Body
	}

	// 模型改写必须等候选端点（及其凭证分组）选定后再执行；这里保留隐私处理后的
	// 原始 body，故障转移时每个分组都从同一份请求重新计算自己的策略。
	channelName := ""
	if h.manager != nil {
		if ch := h.manager.GetActiveChannel(); ch != nil {
			channelName = ch.Name
		}
	}
	body = originalBody

	// 2. 检测是否流式请求
	isStreaming := bytes.Contains(body, []byte(`"stream":true`)) ||
		bytes.Contains(body, []byte(`"stream": true`))

	// 3. 从请求体提取模型名（用于在响应未携带 model 时填充）
	requestModel := h.tokenParser.ParseModelFromBody(originalBody)

	// 3b. 从请求体提取 reasoning 配置（Codex 请求体 reasoning: {effort, mode, summary}）
	reasoningJSON := extractReasoning(body)

	// 3c. 从请求体提取 service_tier（fast mode: auto/fast/priority）
	serviceTier := extractServiceTier(body)

	// 🆕 [Codex 生命周期] 请求开始日志
	h.logger.Info("[Codex 请求开始]",
		"provider", "codex",
		"request_id", requestID,
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr,
		"streaming", isStreaming,
		"channel", channelName,
		"model", requestModel,
		"model_rewrite_source", "",
		"reasoning", reasoningJSON,
		"service_tier", serviceTier,
	)

	// 5. 记录请求开始（追踪：Provider=codex + 渠道 + 请求体模型）
	h.recordStart(requestID, r, isStreaming, channelName, requestModel, modelRewriteResult{}, reasoningJSON, serviceTier)

	// 请求级路由（模型规则 > 非流改道）：命中后只按阶段尝试目标，全局激活渠道全程不读写
	if route := h.planCodexRoute(r, originalBody, isStreaming, requestID); route != nil {
		h.serveRouted(w, r, originalBody, requestID, startTime, isStreaming, route)
		return
	}

	if subscription.FromContext(r.Context()) != nil {
		h.writeError(w, 400, "subscription_route_unavailable")
		return
	}
	// 6. 决定外层循环轮数（loop_retry）
	maxLoops := 1
	if failoverCfg := h.failoverConfig(); failoverCfg.LoopRetry {
		maxLoops = failoverCfg.GetMaxLoops()
		if maxLoops <= 0 {
			maxLoops = 1
		}
	}

	// 7. 渠道内循环重试 + 挂起等待运维干预（最多挂起一次）
	var failedIDs []int64
	var endpointChain []chainNode
	attemptCount := 0
	var lastErr error
	var lastStatus int
	var lastSummary string
	var lastUpstreamStatus int
	var lastUpstreamHeaders http.Header
	var lastUpstreamBody []byte
	suspendedOnce := false
	activeChannelID := int64(0)

outerLoop:
	for {
	rounds:
		for loopRound := 0; loopRound < maxLoops; loopRound++ {
			triedChannels := make(map[int64]struct{})
			if loopRound > 0 {
				h.manager.ResetUnhealthy()
				failedIDs = failedIDs[:0]
				if h.failoverConfig().Enabled {
					// 渠道间转移开启时，新一轮从最高优先级参与渠道开始。
					first, switchErr := h.manager.ActivateNextChannelForFailover(r.Context(), triedChannels, activeChannelID)
					if errors.Is(switchErr, errCodexActiveChannelChanged) {
						continue outerLoop
					}
					if switchErr != nil {
						lastErr = switchErr
						break rounds
					}
					triedChannels[first.ID] = struct{}{}
					activeChannelID = first.ID
					channelName = first.Name
					body = originalBody
					requestModel = h.tokenParser.ParseModelFromBody(originalBody)
				} else if active := h.manager.GetActiveChannel(); active != nil {
					// 渠道间转移关闭时，只重试当前活跃渠道，不写全局激活状态。
					triedChannels[active.ID] = struct{}{}
					activeChannelID = active.ID
					channelName = active.Name
					body = originalBody
					requestModel = h.tokenParser.ParseModelFromBody(originalBody)
				} else {
					lastErr = fmt.Errorf("无活跃 Codex 渠道")
					break rounds
				}
				h.logger.Info("[Codex 循环重试]",
					"provider", "codex",
					"request_id", requestID,
					"round", loopRound+1,
					"max_loops", maxLoops,
				)
			} else if active := h.manager.GetActiveChannel(); active != nil {
				// 首轮从当前接管渠道开始；挂起恢复后也重新建立本轮状态。
				triedChannels[active.ID] = struct{}{}
				activeChannelID = active.ID
				channelName = active.Name
				body = originalBody
				requestModel = h.tokenParser.ParseModelFromBody(originalBody)
			}

			for {
				result := h.tryEndpointCycle(
					w, r, body, requestID, startTime,
					isStreaming, channelName, requestModel,
					&failedIDs, &endpointChain, &attemptCount,
				)
				if result.routeChanged {
					lastUpstreamStatus = 0
					lastUpstreamHeaders = nil
					lastUpstreamBody = nil
					failedIDs = failedIDs[:0]
					continue outerLoop
				}
				if result.completed {
					return
				}
				lastErr = result.lastErr
				lastStatus = result.lastStatus
				lastSummary = result.lastSummary
				// 只有本轮真的拿到过上游响应才覆盖：后续渠道/轮次全是网络错误时，
				// 仍要把最后一次真实的上游状态、头和正文透传给客户端。
				if result.lastUpstreamStatus > 0 {
					lastUpstreamStatus = result.lastUpstreamStatus
					lastUpstreamHeaders = result.lastUpstreamHeaders
					lastUpstreamBody = result.lastUpstreamBody
				}
				if !h.failoverConfig().Enabled {
					break
				}
				next, switchErr := h.manager.ActivateNextChannelForFailover(r.Context(), triedChannels, activeChannelID)
				if errors.Is(switchErr, errCodexActiveChannelChanged) {
					failedIDs = failedIDs[:0]
					continue outerLoop
				}
				if switchErr != nil {
					break
				}
				triedChannels[next.ID] = struct{}{}
				activeChannelID = next.ID
				channelName = next.Name
				body = originalBody
				requestModel = h.tokenParser.ParseModelFromBody(originalBody)
				failedIDs = failedIDs[:0]
				h.logger.Info("[Codex 渠道间故障转移]", "request_id", requestID, "channel", channelName, "round", loopRound+1)
			}
			if errors.Is(lastErr, errCredentialGroupDailyCostLimitExceeded) {
				break rounds
			}
		}

		// loop_retry 全部耗尽 → 尝试挂起等待运维干预（最多挂起一次）
		failoverCfg := h.failoverConfig()
		canSuspend := !errors.Is(lastErr, errCredentialGroupDailyCostLimitExceeded) &&
			!suspendedOnce && h.suspensionMgr != nil &&
			failoverCfg.LoopRetrySuspendTimeout > 0
		if !canSuspend {
			break outerLoop
		}
		suspendedOnce = true

		suspendRes := h.suspensionMgr.Suspend(r.Context(), requestID, failoverCfg.LoopRetrySuspendTimeout)
		switch suspendRes {
		case SuspendRecovered:
			// 重置状态再来一轮 loop_retry
			failedIDs = failedIDs[:0]
			h.manager.ResetUnhealthy()
			h.logger.Info("[Codex 挂起恢复] 运维干预后重新进入端点循环",
				"provider", "codex",
				"request_id", requestID,
				"endpoints_tried", len(endpointChain),
			)
			continue outerLoop

		case SuspendCanceled:
			// 客户端在挂起期间取消
			h.logger.Info("[Codex 请求取消]",
				"provider", "codex",
				"request_id", requestID,
				"duration_ms", time.Since(startTime).Milliseconds(),
				"reason", "client_canceled_during_suspend",
			)
			h.recordFailure(requestID, startTime, "client_cancelled", "client_canceled_during_suspend", nil, requestModel)
			return

		case SuspendTimedOut:
			// 超时未被唤醒，落入失败逻辑
			h.logger.Warn("[Codex 挂起超时] 运维未在超时前激活渠道/端点",
				"provider", "codex",
				"request_id", requestID,
				"timeout", failoverCfg.LoopRetrySuspendTimeout.String(),
			)
			break outerLoop

		case SuspendRejected:
			// 挂起队列已满，落入失败逻辑
			h.logger.Warn("[Codex 挂起拒绝] 已达最大挂起请求数",
				"provider", "codex",
				"request_id", requestID,
			)
			break outerLoop
		}
	}

	// 8. 所有循环耗尽 + 挂起也失败
	statusCode := http.StatusServiceUnavailable
	failureReason := "endpoint_exhausted"
	detail := lastSummary
	if strings.TrimSpace(detail) == "" && lastErr != nil {
		detail = lastErr.Error()
	}
	if errors.Is(lastErr, errCredentialGroupDailyCostLimitExceeded) {
		statusCode = http.StatusTooManyRequests
		failureReason = "daily_cost_limit_exceeded"
		if lastErr != nil {
			detail = lastErr.Error()
		}
	} else if lastStatus >= 400 {
		statusCode = lastStatus
	}
	if lastUpstreamStatus > 0 {
		statusCode = lastUpstreamStatus
		if message := strings.TrimSpace(string(lastUpstreamBody)); message != "" {
			detail = message
		} else if strings.TrimSpace(lastSummary) != "" {
			detail = lastSummary
		}
	}
	h.logger.Error("[Codex 请求失败]",
		"provider", "codex",
		"request_id", requestID,
		"failure_reason", failureReason,
		"detail", detail,
		"duration_ms", time.Since(startTime).Milliseconds(),
		"endpoints_tried", len(endpointChain),
		"loops_tried", maxLoops,
		"channel", channelName,
	)
	if lastUpstreamStatus > 0 {
		h.recordUpstreamFailure(requestID, startTime, failureReason, detail, lastUpstreamStatus, nil, requestModel)
	} else {
		h.recordFailure(requestID, startTime, failureReason, detail, nil, requestModel)
	}
	if lastUpstreamStatus > 0 {
		response := &http.Response{
			StatusCode: lastUpstreamStatus,
			Header:     lastUpstreamHeaders,
			Body:       io.NopCloser(bytes.NewReader(lastUpstreamBody)),
		}
		h.passthroughWithBody(w, response, lastUpstreamBody)
		return
	}
	h.writeError(w, statusCode, detail)
}

// extractReasoning 从 Codex 请求体 JSON 中提取完整的 reasoning 配置并序列化为 JSON
// 请求体格式: {"reasoning":{"effort":"xhigh","mode":"auto","summary":"concise"},...}
// 返回 "" 表示请求体未设置 reasoning（将使用 Codex CLI 默认值）
func extractReasoning(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var partial struct {
		Reasoning json.RawMessage `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &partial); err != nil || len(partial.Reasoning) == 0 || bytes.Equal(bytes.TrimSpace(partial.Reasoning), []byte("null")) {
		return ""
	}
	return string(partial.Reasoning)
}

// extractServiceTier 从 Codex 请求体 JSON 中提取 service_tier
// 常见取值: auto/default/batch/flex/priority；部分兼容服务商使用 fast。
func extractServiceTier(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var partial struct {
		ServiceTier string `json:"service_tier"`
	}
	if err := json.Unmarshal(body, &partial); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(partial.ServiceTier))
}
