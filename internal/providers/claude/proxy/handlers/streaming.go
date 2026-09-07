package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/tracking"
)

// StreamingHandler 流式请求处理器
// 负责处理所有流式请求，包括错误恢复、重试机制和流式数据转发
type StreamingHandler struct {
	config                   *config.Config
	endpointManager          *endpoint.Manager
	forwarder                *Forwarder
	usageTracker             *tracking.UsageTracker
	tokenParserFactory       TokenParserFactory
	streamProcessorFactory   StreamProcessorFactory
	errorRecovery            ErrorRecoveryManager
	retryManagerFactory      RetryManagerFactory
	suspensionManagerFactory SuspensionManagerFactory
	privacyFilter            PrivacyFilter
	// 🔧 [修复] 共享SuspensionManager实例，确保全局挂起限制生效
	sharedSuspensionManager SuspensionManager
}

// NewStreamingHandler 创建新的StreamingHandler实例
func NewStreamingHandler(
	cfg *config.Config,
	endpointManager *endpoint.Manager,
	forwarder *Forwarder,
	usageTracker *tracking.UsageTracker,
	tokenParserFactory TokenParserFactory,
	streamProcessorFactory StreamProcessorFactory,
	errorRecovery ErrorRecoveryManager,
	retryManagerFactory RetryManagerFactory,
	suspensionManagerFactory SuspensionManagerFactory,
	// 🔧 [Critical修复] 直接接受共享的SuspensionManager实例
	sharedSuspensionManager SuspensionManager,
) *StreamingHandler {
	return &StreamingHandler{
		config:                   cfg,
		endpointManager:          endpointManager,
		forwarder:                forwarder,
		usageTracker:             usageTracker,
		tokenParserFactory:       tokenParserFactory,
		streamProcessorFactory:   streamProcessorFactory,
		errorRecovery:            errorRecovery,
		retryManagerFactory:      retryManagerFactory,
		suspensionManagerFactory: suspensionManagerFactory,
		// 🔧 [Critical修复] 使用传入的共享SuspensionManager实例
		// 确保流式请求与常规请求共享同一个全局挂起计数器
		sharedSuspensionManager: sharedSuspensionManager,
	}
}

func (sh *StreamingHandler) SetPrivacyFilter(filter PrivacyFilter) {
	sh.privacyFilter = filter
}

// noOpFlusher 是一个不执行实际flush操作的flusher实现
//
// ⚠️ [Deprecated for production] 此类型仅保留供既有单元测试使用。
// 生产代码（handleStreaming）已改为：检测到 ResponseWriter 不支持 Flusher 时
// 直接返回 503，避免静默降级导致客户端长时间等不到流式数据。
type noOpFlusher struct{}

func (f *noOpFlusher) Flush() {
	// 不执行任何操作，避免panic但保持流式处理逻辑
}

// terminateSSEStreamWithError 正确终止 SSE 流，确保客户端不会自动重连
// 修复客户端无限重连问题 - 发送完整的 SSE 结束序列
//
// 🔧 [P0-S2 修复] 改用 json.Marshal 构造 JSON payload，
// 同时确保 message_stop 也走结构化构造路径。
func terminateSSEStreamWithError(w http.ResponseWriter, flusher http.Flusher, errorMsg string) {
	// 1. 发送 Anthropic 格式的 error 事件（让客户端识别为 API 错误而非连接中断）
	writeSSEErrorEvent(w, flusher, "api_error", errorMsg)

	// 2. 发送 message_stop 事件（关键！明确告知客户端流已正常结束）
	writeSSEEvent(w, flusher, "message_stop", map[string]any{"type": "message_stop"})
}

// sendStreamInterruptedMessage 发送流中断错误，触发客户端自动重试
// 改用 Anthropic API 标准的 overloaded_error 格式
// 之前的实现发送"假装完整"的消息，Claude Code 不会识别为需要重试
// 现在发送 overloaded_error，这是 Anthropic 定义的可重试错误类型
// 参考: https://docs.anthropic.com/en/api/errors
//
// ⚠️ 仅用于反代自身产生的中断（重试预算耗尽、已达最大重启次数、挂起超时等）。
//
// 🔧 [P0-S2 修复] 使用 json.Marshal 构造 payload。
func sendStreamInterruptedMessage(w http.ResponseWriter, flusher http.Flusher, message string, modelName string) {
	// modelName 参数保留以保持向后兼容，但不再使用
	_ = modelName

	slog.Debug("📡 发送可重试错误信号", "message", message, "error_type", "overloaded_error")

	writeSSEErrorEvent(w, flusher, "overloaded_error", message)
}

// writeSSEErrorEvent 构造并发送一个 Anthropic 标准格式的 SSE error 事件
// 格式: event: error\ndata: {"type":"error","error":{"type":"<errorType>","message":"<message>"}}\n\n
//
// 🔧 [P0-S2 修复] 使用 json.Marshal 而非手写字符串转义，标准库保证：
//   - 正确转义所有控制字符（\x00-\x1F）
//   - 正确处理无效 UTF-8 序列（替换为 U+FFFD）
//   - 正确转义反斜杠、引号、Unicode
//
// 这是修复 "JSON Parse error" 类问题的核心防线。
func writeSSEErrorEvent(w http.ResponseWriter, flusher http.Flusher, errorType, message string) {
	payload := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errorType,
			"message": message,
		},
	}
	writeSSEEvent(w, flusher, "error", payload)
}

// writeSSEEvent 通用 SSE 事件写入：序列化 payload 为 JSON 并按 SSE 协议输出
// 写入失败仅记录日志（连接可能已断开），上层通过 ctx.Err() 感知客户端取消
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventName string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		// 兜底：json.Marshal 几乎不会失败，但万一失败时退化为静态错误消息
		slog.Error("Failed to marshal SSE payload", "event", eventName, "error", err)
		data = []byte(`{"type":"error","error":{"type":"api_error","message":"internal marshal error"}}`)
	}
	if _, werr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, data); werr != nil {
		slog.Debug("Failed to write SSE event to client (connection likely closed)", "event", eventName, "error", werr)
		return
	}
	flusher.Flush()
}

// HandleStreamingRequest 统一流式请求处理（整合错误恢复与生命周期管理）
func (sh *StreamingHandler) HandleStreamingRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager) {
	connID := lifecycleManager.GetRequestID()

	slog.Info(fmt.Sprintf("🌊 [流式处理] [%s] 开始流式请求处理", connID))
	sh.handleStreaming(ctx, w, r, bodyBytes, lifecycleManager)
}

// handleStreaming 流式处理（带错误恢复）
func (sh *StreamingHandler) handleStreaming(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager) {
	connID := lifecycleManager.GetRequestID()
	turnFailureKey := requestTurnFailureCacheKey(r, bodyBytes)
	ctx = endpoint.ContextWithTurnFailureKey(ctx, turnFailureKey)

	// 🔧 [P2-S2 修复] 获取 Flusher - 不支持时直接 503，拒绝伪装
	// 旧逻辑用 noOpFlusher 静默退化，会导致客户端长时间等不到流式数据，
	// 直至 handler 返回时一次性发送（甚至 socket 已断开）。
	// 直接 503 让上层（中间件 / 客户端）感知到环境不支持流式，按需降级或重试。
	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error(fmt.Sprintf("🚨 [Flusher不支持] [%s] 当前 ResponseWriter 未实现 http.Flusher，拒绝流式请求", connID))
		lifecycleManager.FailRequest("flusher_unsupported",
			"ResponseWriter does not support http.Flusher", http.StatusServiceUnavailable)
		http.Error(w, "Streaming not supported by this proxy chain", http.StatusServiceUnavailable)
		return
	}
	protectedBody, err := applyPrivacyFilter(ctx, sh.privacyFilter, r, "claude", bodyBytes, lifecycleManager)
	if err != nil {
		// 隐私策略拒绝：短路返回，不进入下面的重试与故障转移循环。
		// 流式请求统一用 SSE 终止，避免客户端把 HTTP 状态码误判为认证失败。
		reason, message := "privacy_filter_failed", err.Error()
		status := http.StatusInternalServerError
		if policyErr := AsPrivacyPolicyError(err); policyErr != nil {
			reason, message, status = PrivacyFailureReason(policyErr), policyErr.Message, policyErr.StatusCode
		}
		lifecycleManager.FailRequest(reason, message, status)
		sh.setStreamingHeaders(w)
		terminateSSEStreamWithError(w, flusher, "隐私防护处理失败: "+message)
		return
	}
	bodyBytes = protectedBody
	// 先准备 SSE 头但不提交状态码；若最终没有任何流字节，最终错误处理会清除这些头并透传 HTTP 错误。
	sh.setStreamingHeaders(w)

	// 🔧 [P5.3] 初始化全局重试预算
	// 避免三层重试结构（渠道间×渠道内×单端点）导致最坏 50+ 次重试 + 客户端 socket 失效
	lifecycleManager.InitRetryBudget(
		sh.config.Retry.MaxTotalAttempts,
		sh.config.Retry.MaxTotalDuration,
	)

	// 继续执行流式请求处理
	triedChannels := make(map[string]struct{}, 4)
	// 追踪本次请求链路内已尝试的端点（渠道内严格优先级转移）
	triedEndpoints := make(map[string]struct{}, 8)
	// 追踪循环轮数（用于 MaxLoops 限制）
	loopCount := 0
	sh.executeStreamingWithRetry(ctx, w, r, bodyBytes, lifecycleManager, flusher, triedChannels, triedEndpoints, loopCount)
}

// setStreamingHeaders 设置流式响应头
func (sh *StreamingHandler) setStreamingHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")
}

func clearStreamingHeaders(w http.ResponseWriter) {
	w.Header().Del("Content-Type")
	w.Header().Del("Cache-Control")
	w.Header().Del("Connection")
	w.Header().Del("Transfer-Encoding")
}

// executeStreamingWithRetry 执行带重试的流式处理
// triedChannels: 本次请求链路内已尝试过的渠道（方案B：渠道间故障转移不回访，避免循环）
// triedEndpoints: 本次请求链路内已尝试过的端点（渠道内严格优先级转移，不回退）
// loopCount: 当前循环轮数（用于 MaxLoops 限制）
func (sh *StreamingHandler) executeStreamingWithRetry(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager, flusher http.Flusher, triedChannels map[string]struct{}, triedEndpoints map[string]struct{}, loopCount int) {
	connID := lifecycleManager.GetRequestID()

	// 🔧 [P5.3] 全局重试预算检查 - 在每次进入重试循环前检查
	// 由于 executeStreamingWithRetry 可能被递归调用（端点恢复/故障转移重启），
	// 这个检查能在递归层级累计超过预算时及时终止
	if exhausted, reason := lifecycleManager.CheckRetryBudget(); exhausted {
		slog.Warn(fmt.Sprintf("⏰ [全局重试预算耗尽] [%s] %s", connID, reason))
		lifecycleManager.FailRequest("retry_budget_exhausted", reason, http.StatusGatewayTimeout)
		sendStreamInterruptedMessage(w, flusher, fmt.Sprintf("重试预算耗尽: %s", reason), modelNameOrUnknown(lifecycleManager))
		return
	}

	// 在函数开头检查 context 是否已取消（客户端断开连接）
	select {
	case <-ctx.Done():
		slog.Info(fmt.Sprintf("🚫 [客户端断开] [%s] 检测到客户端已断开连接，停止重试 (loopCount=%d)", connID, loopCount))
		lifecycleManager.CancelRequest("client disconnected", nil)
		return
	default:
	}

	currentChannel := ""

	var lastFailedEndpoint string // 🚀 [端点自愈] 追踪最后失败的端点

	// 获取健康端点，排除已尝试的端点（实现严格优先级转移）
	var endpoints []*endpoint.Endpoint

	// 创建重试管理器（用于获取端点和决策）
	retryMgr := sh.retryManagerFactory.NewRetryManager()

	if len(triedEndpoints) == 0 {
		// 首次尝试，获取所有健康端点
		if sh.endpointManager.GetConfig().Strategy.Type == "fastest" && sh.endpointManager.GetConfig().Strategy.FastTestEnabled {
			endpoints = sh.endpointManager.GetFastestEndpointsWithRealTimeTest(ctx)
		} else {
			endpoints = retryMgr.GetHealthyEndpoints(ctx)
		}
	} else {
		// 非首次尝试，排除已尝试的端点（严格优先级转移，不回退）
		endpoints = retryMgr.GetHealthyEndpointsExcluding(ctx, stringSetKeys(triedEndpoints))
		if len(endpoints) > 0 {
			slog.Info(fmt.Sprintf("🔄 [严格优先级转移] [%s] 排除已尝试端点后剩余 %d 个候选端点", connID, len(endpoints)))
		}
	}
	rawEndpoints := endpoints
	var quotaBlocked bool
	endpoints, quotaBlocked = filterCredentialGroupDailyCostLimits(ctx, sh.usageTracker, endpoints)

	if len(endpoints) == 0 {
		if quotaBlocked && len(rawEndpoints) > 0 {
			currentChannel = endpointRouteName(rawEndpoints[0])
			if triedChannels != nil && currentChannel != "" {
				triedChannels[currentChannel] = struct{}{}
			}
			newChannel, failoverErr := sh.endpointManager.TriggerChannelSwitchWithoutFailure(
				currentChannel,
				stringSetKeys(triedChannels),
			)
			if failoverErr == nil && newChannel != "" {
				slog.Info("[Claude 分组限额触发渠道间故障转移]",
					"request_id", connID,
					"from_channel", currentChannel,
					"to_channel", newChannel,
				)
				sh.executeStreamingWithRetry(ctx, w, r, bodyBytes, lifecycleManager, flusher, triedChannels, triedEndpoints, loopCount)
				return
			}
			lifecycleManager.FailRequest("daily_cost_limit_exceeded", "", http.StatusTooManyRequests)
			clearStreamingHeaders(w)
			WriteUpstreamErrorResponse(w, nil, "", http.StatusTooManyRequests)
			return
		}
		noHealthyErr := fmt.Errorf("no healthy endpoints available")
		lifecycleManager.HandleError(noHealthyErr)
		lifecycleManager.FailRequest("no_healthy_endpoints", "", http.StatusServiceUnavailable)
		clearStreamingHeaders(w)
		WriteUpstreamErrorResponse(w, nil, "", http.StatusServiceUnavailable)
		return
	}

	// 方案B：记录当前尝试的渠道（用于后续跨渠道故障转移时不回访）
	if len(endpoints) > 0 && endpoints[0] != nil {
		currentChannel = endpointRouteName(endpoints[0])
		if triedChannels != nil && currentChannel != "" {
			triedChannels[currentChannel] = struct{}{}
		}
	}

	slog.Info(fmt.Sprintf("🌊 [流式开始] [%s] 流式请求开始，端点数: %d", connID, len(endpoints)))

	// 🔧 [重试逻辑修复] 对每个端点进行max_attempts次重试，而不是只尝试一次
	// 尝试端点直到成功
	var lastErr error           // 声明在外层作用域，供最终错误处理使用
	var lastResp *http.Response // 🔧 [修复] 添加lastResp变量，用于获取真实HTTP状态码
	// 🔢 [重构] 移除currentAttemptCount变量，统一由LifecycleManager管理计数
	for i := 0; i < len(endpoints); i++ {
		ep := endpoints[i]
		lastFailedEndpoint = ep.Config.Name // 🚀 [端点自愈] 记录当前尝试的端点
		// 记录已尝试的端点（确保严格优先级转移，不回退）
		epKey := endpoint.EndpointKeyForConfig(ep.Config)
		if _, alreadyTried := triedEndpoints[epKey]; alreadyTried {
			continue
		}
		triedEndpoints[epKey] = struct{}{}

		// 更新生命周期管理器信息
		routeGroup := endpointRouteName(ep)
		lifecycleManager.SetEndpoint(ep.Config.Name, endpointDisplayGroupName(ep), ep.Config.Channel)
		lifecycleManager.UpdateStatus("forwarding", i, 0)

		// 🔧 [端点上下文修复] 立即设置端点信息到请求上下文，确保所有分支（成功/失败/取消）的日志都能正确记录端点
		*r = *r.WithContext(context.WithValue(r.Context(), "selected_endpoint", ep.Config.Name))

		// ✅ [同端点重试] 对当前端点进行max_attempts次重试
		endpointSuccess := false
		var attempt int                 // 声明在外部，循环结束后仍可访问
		var lastDecision *RetryDecision // 保存最后的重试决策，用于外层逻辑

		for attempt = 1; attempt <= sh.config.Retry.MaxAttempts; attempt++ {
			// 检查是否被取消
			select {
			case <-ctx.Done():
				slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 检测到客户端取消，立即停止重试", connID))
				lifecycleManager.CancelRequest("client disconnected", nil)

				// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
				*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
				sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
				// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
				return
			default:
			}

			// 尝试连接端点
			requestBody := rewriteBodyForEndpoint(sh.forwarder, lifecycleManager, bodyBytes, ep)
			resp, err := sh.forwarder.ForwardRequestToEndpoint(ctx, r, requestBody, ep)
			// 🔧 [修复] 保存最后的响应，用于获取真实HTTP状态码
			lastResp = resp
			if err != nil && ctx.Err() != nil {
				slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 检测到客户端取消，立即停止重试", connID))
				lifecycleManager.CancelRequest("client disconnected", nil)

				// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
				*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
				sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
				// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
				return
			}
			if err == nil && IsSuccessStatus(resp.StatusCode) {
				// 🔢 [成功计数] 成功的尝试记录到生命周期管理器
				lifecycleManager.IncrementAttempt()
				currentAttemptCount := lifecycleManager.GetAttemptCount()

				// ✅ [重试决策] 成功请求的决策日志 - 保持监控完整性
				slog.Info(fmt.Sprintf("✅ [重试决策] 请求成功完成 request_id=%s endpoint=%s attempt=%d reason=请求成功完成",
					connID, ep.Config.Name, currentAttemptCount))

				// ✅ 成功！开始处理响应
				endpointSuccess = true
				slog.Info(fmt.Sprintf("✅ [流式成功] [%s] 端点: %s , 尝试次数: %d",
					connID, ep.Config.Name, currentAttemptCount))

				lifecycleManager.UpdateStatus("processing", currentAttemptCount, resp.StatusCode)

				// 只设置响应头，不提前提交 200；首个上游流字节写出时再由 net/http 提交。
				sh.setStreamingHeaders(w)

				// 创建Token解析器和流式处理器
				tokenParser := sh.tokenParserFactory.NewTokenParserWithUsageTracker(connID, sh.usageTracker)
				processor := sh.streamProcessorFactory.NewStreamProcessor(tokenParser, sh.usageTracker, w, flusher, connID, ep.Config.Name)

				slog.Info(fmt.Sprintf("🚀 [开始流式处理] [%s] 端点: %s", connID, ep.Config.Name))

				// 执行流式处理并获取Token信息和模型名称
				finalTokenUsage, modelName, err := processor.ProcessStreamWithRetry(ctx, resp)
				if err != nil && ctx.Err() != nil {
					slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 流式处理期间检测到客户端取消，立即停止重试", connID))
					lifecycleManager.CancelRequest("client disconnected during stream processing", nil)
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
					sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
					// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
					return
				}

				// 🔥 [重复计费防护] 2025-12-26: 检查是否已发送实际模型输出
				if processor.HasSentContent() {
					lifecycleManager.MarkOutputSent()
					slog.Debug(fmt.Sprintf("🔒 [重复计费防护] [%s] 检测到已发送内容，标记不可重试", connID))
				}

				if err != nil {
					// 🔧 [结构化错误处理] 2025-12-11: 优先使用接口断言处理流不完整错误
					if streamErr, ok := err.(StreamIncompleteErrorInterface); ok {
						// 🚀 [Plan D实现] 2025-12-26: 流不完整时记录部分Token，但继续重试
						parsedModelName := streamErr.GetModelName()
						failureReason := streamErr.GetFailureReason()

						// 设置模型信息
						if parsedModelName != "unknown" && parsedModelName != "" {
							lifecycleManager.SetModelWithComparison(parsedModelName, "stream_incomplete")
						} else if modelName != "unknown" && modelName != "" {
							lifecycleManager.SetModelWithComparison(modelName, "stream_processor")
						}

						// 移除渠道内端点冷却，使用 triedEndpoints 追踪已尝试端点
						// 日志记录端点失败原因
						slog.Warn(fmt.Sprintf("⚠️ [流截断] [%s] endpoint=%s reason=%s",
							connID, ep.Config.Name, failureReason))

						// 任意流字节一旦写出，HTTP 响应就已提交，不能再切换端点或追加自定义错误。
						if processor.HasWrittenResponse() {
							lifecycleManager.RecordTokensForFailedRequest(finalTokenUsage, failureReason)
							lifecycleManager.FailRequest(failureReason, err.Error(), resp.StatusCode)
							*r = *r.WithContext(withFinalStatusCode(r.Context(), resp.StatusCode))
							sh.endpointManager.ClearEndpointsRequestFailure([]string{epKey})
							return
						}

						// 🚀 [Plan D核心修改] 记录部分Token到数据库，但不完成请求；继续尝试其他端点完成响应。
						lifecycleManager.RecordPartialTokens(finalTokenUsage, failureReason, ep.Config.Name)

						slog.Warn(fmt.Sprintf("⚠️ [流截断记录部分Token] [%s] 端点: %s, failure_reason: %s, 模型: %s, 将继续重试其他端点",
							connID, ep.Config.Name, failureReason, parsedModelName))

						// 将此错误视为端点失败，触发故障转移
						lastErr = err
						endpointSuccess = false
						break // 当前端点未写出响应，直接尝试下一个端点
					}

					// 其他流错误也不再从错误文本解析状态或模型。
					if modelName != "unknown" && modelName != "" {
						lifecycleManager.SetModelWithComparison(modelName, "stream_processor")
					}
					lifecycleManager.HandleError(err)

					if processor.HasWrittenResponse() {
						if finalTokenUsage != nil {
							lifecycleManager.RecordTokensForFailedRequest(finalTokenUsage, "stream_error")
						}
						lifecycleManager.FailRequest("stream_error", err.Error(), resp.StatusCode)
						*r = *r.WithContext(withFinalStatusCode(r.Context(), resp.StatusCode))
						slog.Warn(fmt.Sprintf("🛑 [流式处理失败] [%s] 端点: %s, 模型: %s, 错误: %v",
							connID, ep.Config.Name, modelName, err))
						return
					}

					if finalTokenUsage != nil {
						lifecycleManager.RecordPartialTokens(finalTokenUsage, "stream_error", ep.Config.Name)
					}
					lastErr = err
					endpointSuccess = false
					break
				}

				// ✅ 流式处理成功完成，使用生命周期管理器完成请求
				if finalTokenUsage != nil {
					// 设置模型名称并通过生命周期管理器完成请求
					// 使用对比方法，检测并警告模型不一致情况
					if modelName != "unknown" && modelName != "" {
						lifecycleManager.SetModelWithComparison(modelName, "流式响应解析")
					}
					lifecycleManager.CompleteRequest(finalTokenUsage)
				} else {
					// 没有Token信息，使用HandleNonTokenResponse处理
					lifecycleManager.HandleNonTokenResponse("")
				}
				sh.endpointManager.MarkEndpointRequestSuccess(ep)
				return
			}

			// ❌ 出现错误，记录尝试次数
			globalAttemptCount := lifecycleManager.IncrementAttempt()
			lastErr = err

			// 错误处理：如果调用方返回了响应对象，保留服务商原始状态和正文。
			// Forwarder 通常已经把 >=400 转成 EndpointError；此分支兼容测试/替代实现。
			if err == nil && resp != nil && !IsSuccessStatus(resp.StatusCode) {
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamErrorBodyBytes))
				_ = resp.Body.Close()
				if readErr != nil {
					slog.Warn(fmt.Sprintf("⚠️ [响应体读取失败] [%s] 端点: %s, Read错误: %v", connID, ep.Config.Name, readErr))
				}
				targetURL := ep.Config.URL + r.URL.Path
				lastErr = newEndpointErrorFromResponse(&http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, targetURL)
			} else if err != nil && resp != nil {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					slog.Warn(fmt.Sprintf("⚠️ [错误响应体关闭失败] [%s] 端点: %s, Close错误: %v", connID, ep.Config.Name, closeErr))
				}
			}

			// 🔧 使用增强的RetryManager进行统一决策
			errorRecovery := sh.errorRecovery
			errorCtx := errorRecovery.ClassifyError(lastErr, connID, ep.Config.Name, routeGroup, attempt-1)

			// 🔥 [计费感知分类] 2025-12-29: 根据HasOutputContent()动态调整Stream错误的可重试性
			// 核心逻辑：token计费 = HasOutputContent() = true → 不可重试（避免重复计费）
			//          0 tokens = HasOutputContent() = false → 可重试（连接阶段中断）
			errStr := strings.ToLower(lastErr.Error())
			if strings.Contains(errStr, "stream_error") {
				if !lifecycleManager.HasOutputContent() {
					slog.Info(fmt.Sprintf("🔄 [计费感知] [%s] Stream错误但未发送内容(0 tokens)，允许重试: %v", connID, lastErr))
					errorCtx.RetryableAfter = time.Second * time.Duration(1<<uint(attempt-1))
				} else {
					slog.Warn(fmt.Sprintf("🚫 [计费感知] [%s] Stream错误且已发送内容(有计费)，禁止重试: %v", connID, lastErr))
					errorCtx.RetryableAfter = 0
				}
			}

			// 🚀 [状态机重构] Phase 4: 分离状态转换与失败原因记录
			// 预设错误上下文（避免重复分类），由HandleError统一记录失败原因
			lifecycleManager.PrepareErrorContext(errorCtx)
			lifecycleManager.HandleError(lastErr)

			// 使用函数开头创建的重试管理器（避免重复创建）
			// 🔢 [关键修复] 分离局部和全局计数语义
			// attempt: 当前端点内的尝试次数，用于退避计算
			// globalAttemptCount: 全局尝试次数，用于限流策略
			decision := retryMgr.ShouldRetryWithDecision(errorCtx, attempt, globalAttemptCount, true) // 流式请求: isStreaming=true
			lastDecision = &decision                                                                  // 保存决策，供外层逻辑使用

			// 检查决策结果
			if decision.FinalStatus == "cancelled" {
				// 🔧 [修复] 添加生命周期状态更新
				lifecycleManager.CancelRequest("client disconnected", nil)
				slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 检测到客户端取消，立即停止重试", connID))

				// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
				*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
				// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
				return
			}

			// 处理挂起决策
			if decision.SuspendRequest {
				if sh.sharedSuspensionManager.ShouldSuspend(ctx) {
					// 🚀 [状态机重构] Phase 4: 挂起时更新状态
					lifecycleManager.UpdateStatus("suspended", -1, 0)
					slog.Info(fmt.Sprintf("⏸️ [流式挂起] [%s] 原因: %s，失败端点: %s", connID, decision.Reason, ep.Config.Name))
					// 🔧 [P0-S1 修复] 不再向客户端发送非协议的 "data: suspend:" 数据行

					// 🚀 [端点自愈] 使用新的端点恢复等待方法，能区分成功/超时/取消
					// requireNonStream=false：流式请求可以落到 stream_only 端点
					failedEndpointKey := epKey
					result := sh.sharedSuspensionManager.WaitForEndpointRecoveryWithResult(ctx, connID, failedEndpointKey, false)
					switch result {
					case SuspensionSuccess:
						// 🔥 [递归修复] 2025-12-26: 检查重启次数限制，防止栈溢出
						if lifecycleManager.CanRestart() {
							// 🔥 [重复计费防护] 2025-12-26: 检查是否已发送内容
							if lifecycleManager.HasOutputContent() {
								slog.Warn(fmt.Sprintf("🚫 [重复计费防护] [%s] 端点恢复成功但已发送内容，禁止重试", connID))
								lifecycleManager.FailRequest("output_sent_no_retry",
									"Cannot retry after sending partial output to client", http.StatusBadGateway)
								// 使用 sendStreamInterruptedMessage 触发客户端重试
								sendStreamInterruptedMessage(w, flusher, "已发送部分内容，服务端停止重试，请客户端重试", modelNameOrUnknown(lifecycleManager))
								return
							}

							restartCount := lifecycleManager.IncrementRestart()
							slog.Info(fmt.Sprintf("🔄 [端点恢复重启] [%s] 端点 %s 已恢复，重启次数: %d/%d",
								connID, ep.Config.Name, restartCount, lifecycleManager.GetMaxRestarts()))
							// 🔧 [P0-S1 修复] 不再向客户端发送非协议的 "data: resume:" 数据行
							sh.executeStreamingWithRetry(ctx, w, r, bodyBytes, lifecycleManager, flusher, triedChannels, triedEndpoints, loopCount)
							return
						} else {
							slog.Warn(fmt.Sprintf("❌ [重启限制] [%s] 已达最大重启次数 (%d)，终止请求",
								connID, lifecycleManager.GetMaxRestarts()))
							lifecycleManager.FailRequest("max_restarts_exceeded",
								"Exceeded maximum restart attempts", http.StatusBadGateway)
							// 使用 sendStreamInterruptedMessage 触发客户端重试
							sendStreamInterruptedMessage(w, flusher, "已达最大重启次数，请客户端重试", modelNameOrUnknown(lifecycleManager))
							return
						}
					case SuspensionCancelled:
						// 🎯 [挂起取消区分] 用户在挂起期间取消请求，应该记录为取消而非失败
						slog.Info(fmt.Sprintf("🚫 [挂起期间取消] [%s] 用户在挂起期间取消请求", connID))
						// 🔧 [状态码修复] 设置取消状态码到上下文用于日志记录
						*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
						lifecycleManager.CancelRequest("suspended then cancelled", nil)
						sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
						// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
						return
					case SuspensionTimeout:
						// 🔧 [修复] 添加生命周期状态更新
						currentAttemptCount := lifecycleManager.GetAttemptCount()
						lifecycleManager.UpdateStatus("error", currentAttemptCount, http.StatusBadGateway)
						slog.Warn(fmt.Sprintf("⏰ [挂起超时] [%s] 等待端点恢复或组切换超时", connID))
						sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
						// 使用 sendStreamInterruptedMessage 触发客户端重试
						sendStreamInterruptedMessage(w, flusher, "挂起等待超时，请客户端重试", modelNameOrUnknown(lifecycleManager))
						return
					}
				}
			}

			if !decision.RetrySameEndpoint {
				// 获取失败状态码并设置到端点，用于 UI 显示
				failStatusCode := GetStatusCodeFromError(lastErr, lastResp)
				if failStatusCode == 0 {
					failStatusCode = getDefaultStatusCodeForFinalStatus(decision.FinalStatus)
				}
				// 设置端点的失败代码和原因，用于 UI 显示
				failReason := decision.Reason
				if lastErr != nil && failReason == "" {
					failReason = lastErr.Error()
				}
				sh.endpointManager.SetEndpointLastRequestFail(epKey, failStatusCode, failReason)

				if decision.SwitchEndpoint {
					// 移除渠道内端点冷却，使用 triedEndpoints 追踪
					slog.Info(fmt.Sprintf("🔀 [切换端点] [%s] channel=%s endpoint=%s reason=%s",
						connID, ep.Config.Channel, ep.Config.Name, decision.Reason))
					break // 尝试下一个端点
				} else {
					// 🚀 [状态机重构] Phase 4: 最终失败处理
					// 获取失败原因
					failureReason := lifecycleManager.MapErrorTypeToFailureReason(errorCtx.ErrorType)

					// 使用新的FailRequest方法标记最终失败。尚未写出流字节时，
					// 可以安全地按普通 HTTP 响应透传上游状态和正文。
					failureMessage := ExtractUpstreamErrorMessage(lastErr)
					lifecycleManager.FailRequest(failureReason, failureMessage, failStatusCode)
					sh.endpointManager.ClearEndpointsRequestFailure([]string{epKey})

					// 终止重试，不再把提供商错误包装成自定义 SSE 事件。
					slog.Info(fmt.Sprintf("🛑 [终止重试] [%s] 端点: %s, 状态: %s, 状态码: %d, 原因: %s",
						connID, ep.Config.Name, decision.FinalStatus, failStatusCode, decision.Reason))
					clearStreamingHeaders(w)
					WriteUpstreamErrorResponse(w, lastErr, failureMessage, failStatusCode)
					return
				}
			}

			// 🚀 [状态机重构] Phase 4: 重试状态管理
			if decision.RetrySameEndpoint && attempt < sh.config.Retry.MaxAttempts {
				// 更新为重试状态
				lifecycleManager.UpdateStatus("retry", globalAttemptCount, 0)

				// 如果不是最后一次尝试，等待重试延迟
				slog.Info(fmt.Sprintf("⏳ [等待重试] [%s] 端点: %s, 延迟: %v, 原因: %s",
					connID, ep.Config.Name, decision.Delay, decision.Reason))

				// 🔧 [P0-S1 修复] 不再向客户端发送非协议的 "data: retry:" 数据行

				// 等待延迟，同时检查取消
				select {
				case <-ctx.Done():
					slog.Info(fmt.Sprintf("🚫 [重试取消] [%s] 等待重试期间检测到取消", connID))
					lifecycleManager.CancelRequest("client disconnected during retry delay", nil)

					// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
					sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
					// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
					return
				case <-time.After(decision.Delay):
					// 继续下一次重试
				}
			}
		}

		// 🔧 当前端点所有重试都失败了
		if !endpointSuccess {
			// 修复计数逻辑：处理提前break和自然跑满两种情况
			actualAttempts := min(attempt, sh.config.Retry.MaxAttempts)

			// 🚀 [改进版方案1] 使用已保存的重试决策，避免重复错误分类
			var willSwitchEndpoint bool = true
			if lastDecision != nil {
				willSwitchEndpoint = lastDecision.SwitchEndpoint

				// 对于不切换端点的决策（如HTTP错误、流式错误等），直接终止
				if !willSwitchEndpoint && lastDecision.FinalStatus != "" {
					slog.Info(fmt.Sprintf("❌ [决策终止] [%s] %s，不尝试其他端点", connID, lastDecision.Reason))
					// 🚀 [状态机重构] Phase 4: 使用FailRequest方法标记最终失败
					failureReason := "unknown_error"
					if lastErr != nil {
						// 重新分类错误以获取准确的失败原因
						errorRecovery := sh.errorRecovery
						errorCtx := errorRecovery.ClassifyError(lastErr, connID, "", "", 0)
						failureReason = lifecycleManager.MapErrorTypeToFailureReason(errorCtx.ErrorType)
					}
					// 获取真实的HTTP状态码
					statusCode := GetStatusCodeFromError(lastErr, lastResp)
					if statusCode == 0 {
						statusCode = getDefaultStatusCodeForFinalStatus(lastDecision.FinalStatus)
					}
					failureMessage := ExtractUpstreamErrorMessage(lastErr)
					lifecycleManager.FailRequest(failureReason, failureMessage, statusCode)
					sh.endpointManager.ClearEndpointsRequestFailure([]string{epKey})
					clearStreamingHeaders(w)
					WriteUpstreamErrorResponse(w, lastErr, failureMessage, statusCode)
					return
				}
			}

			// 根据是否会切换端点来显示不同的日志
			if actualAttempts == 1 {
				if willSwitchEndpoint {
					slog.Warn(fmt.Sprintf("❌ [端点失败] [%s] 端点: %s 第1次尝试失败，切换端点",
						connID, ep.Config.Name))
				} else {
					slog.Warn(fmt.Sprintf("❌ [端点失败] [%s] 端点: %s 第1次尝试失败，直接终止",
						connID, ep.Config.Name))
				}
			} else {
				slog.Warn(fmt.Sprintf("❌ [端点失败] [%s] 端点: %s 共尝试 %d 次均失败",
					connID, ep.Config.Name, actualAttempts))
			}

			// 渠道内端点切换时标记当前端点为请求失败，防止后续重新选择到此端点
			// 这是关键修复：之前只有渠道间故障转移才设置 RequestFailed，导致禁用渠道间转移时端点循环
			sh.endpointManager.ClearPreferredEndpoint(ep)
			ep.MarkRequestFailed(lastErr)
			// 通知前端状态变化（使用防抖机制避免频繁刷新）
			sh.endpointManager.NotifyEndpointStatusChange()

			// 如果不是最后一个端点，尝试下一个端点
			if i < len(endpoints)-1 {
				// 🔧 [P0-S1 修复] 不再向客户端发送非协议的 "data: retry:" 数据行
				slog.Info(fmt.Sprintf("🔀 [切换备用端点] [%s] %s -> %s",
					connID, ep.Config.Name, endpoints[i+1].Config.Name))
				continue
			}
		}
	}

	// [auto_wait策略特殊处理] 在跨渠道故障转移前，检查渠道内是否有已恢复的端点
	// 只有 auto_wait 策略才执行此逻辑；loop_retry 和 fail_fast 直接跨渠道
	if len(endpoints) > 0 && retryMgr.IsAutoWaitStrategy() {
		// 检查是否已发送内容（若已发送则不再重试）
		if !lifecycleManager.HasOutputContent() {
			// 构建已尝试端点的key列表
			triedKeys := make([]string, 0, len(triedEndpoints))
			for key := range triedEndpoints {
				triedKeys = append(triedKeys, key)
			}

			// 获取渠道内已恢复的端点（之前因冷却被跳过，现在已恢复）
			recoveredEndpoints := retryMgr.GetRecoveredEndpointsInChannel(triedKeys)
			recoveredEndpoints, _ = filterCredentialGroupDailyCostLimits(ctx, sh.usageTracker, recoveredEndpoints)
			if len(recoveredEndpoints) > 0 {
				slog.Info(fmt.Sprintf("🔄 [auto_wait恢复重试] [%s] 发现 %d 个已恢复端点，按优先级重试一次",
					connID, len(recoveredEndpoints)))
				// 🔧 [P0-S1 修复] 不再向客户端发送非协议的 "data: retry:" 数据行

				// 对每个已恢复的端点尝试一次（按优先级顺序）
				for _, ep := range recoveredEndpoints {
					// 检查客户端取消
					select {
					case <-ctx.Done():
						slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 恢复重试期间检测到客户端取消", connID))
						lifecycleManager.CancelRequest("client disconnected during recovery retry", nil)
						*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
						sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
						// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
						return
					default:
					}

					// 更新生命周期管理器信息
					lifecycleManager.SetEndpoint(ep.Config.Name, endpointDisplayGroupName(ep), ep.Config.Channel)
					lifecycleManager.UpdateStatus("forwarding", lifecycleManager.GetAttemptCount(), 0)
					*r = *r.WithContext(context.WithValue(r.Context(), "selected_endpoint", ep.Config.Name))

					slog.Info(fmt.Sprintf("🔄 [auto_wait恢复重试] [%s] 尝试已恢复端点: %s (优先级: %d)",
						connID, ep.Config.Name, ep.Config.Priority))
					// 🔧 [P0-S1 修复] 不再向客户端发送非协议的 "data: retry:" 数据行
					// 内部重试状态已通过上方 slog 记录

					// 尝试转发请求
					requestBody := rewriteBodyForEndpoint(sh.forwarder, lifecycleManager, bodyBytes, ep)
					resp, err := sh.forwarder.ForwardRequestToEndpoint(ctx, r, requestBody, ep)
					if err != nil && ctx.Err() != nil {
						slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 恢复重试期间检测到客户端取消，立即停止重试", connID))
						lifecycleManager.CancelRequest("client disconnected during recovery retry", nil)
						*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
						sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
						// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
						return
					}
					if err == nil && IsSuccessStatus(resp.StatusCode) {
						lifecycleManager.IncrementAttempt()
						currentAttemptCount := lifecycleManager.GetAttemptCount()

						slog.Info(fmt.Sprintf("✅ [auto_wait恢复成功] [%s] 端点: %s 恢复重试成功",
							connID, ep.Config.Name))

						lifecycleManager.UpdateStatus("processing", currentAttemptCount, resp.StatusCode)
						sh.setStreamingHeaders(w)

						// 创建Token解析器和流式处理器
						tokenParser := sh.tokenParserFactory.NewTokenParserWithUsageTracker(connID, sh.usageTracker)
						processor := sh.streamProcessorFactory.NewStreamProcessor(tokenParser, sh.usageTracker, w, flusher, connID, ep.Config.Name)

						// 执行流式处理
						finalTokenUsage, modelName, streamErr := processor.ProcessStreamWithRetry(ctx, resp)
						if streamErr != nil && ctx.Err() != nil {
							slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 恢复流处理期间检测到客户端取消，立即停止重试", connID))
							lifecycleManager.CancelRequest("client disconnected during recovery stream", nil)
							*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
							sh.endpointManager.ClearEndpointsRequestFailure(stringSetKeys(triedEndpoints))
							// 🔧 [P0-S1 修复] 客户端已断开，不再写非协议数据行
							return
						}
						if processor.HasSentContent() {
							lifecycleManager.MarkOutputSent()
						}

						if streamErr != nil {
							slog.Warn(fmt.Sprintf("⚠️ [auto_wait恢复流失败] [%s] 端点: %s 流处理失败: %v",
								connID, ep.Config.Name, streamErr))
							if processor.HasWrittenResponse() {
								if finalTokenUsage != nil {
									lifecycleManager.RecordTokensForFailedRequest(finalTokenUsage, "stream_error")
								}
								lifecycleManager.FailRequest("stream_error", streamErr.Error(), resp.StatusCode)
								*r = *r.WithContext(withFinalStatusCode(r.Context(), resp.StatusCode))
								return
							}
							lastErr = streamErr
							continue
						}

						// 流处理成功
						if finalTokenUsage != nil {
							if modelName != "unknown" && modelName != "" {
								lifecycleManager.SetModelWithComparison(modelName, "流式响应解析")
							}
							lifecycleManager.CompleteRequest(finalTokenUsage)
						} else {
							lifecycleManager.HandleNonTokenResponse("")
						}
						sh.endpointManager.MarkEndpointRequestSuccess(ep)
						return
					}

					// 恢复重试失败，记录错误继续下一个
					if resp != nil {
						if closeErr := resp.Body.Close(); closeErr != nil {
							slog.Warn(fmt.Sprintf("⚠️ [auto_wait响应体关闭失败] [%s] 端点: %s, Close错误: %v",
								connID, ep.Config.Name, closeErr))
						}
					}
					slog.Warn(fmt.Sprintf("⚠️ [auto_wait恢复失败] [%s] 端点: %s 恢复重试失败: %v",
						connID, ep.Config.Name, err))
					lastErr = err

					// 移除渠道内端点冷却，使用 triedEndpoints 追踪
				}

				slog.Warn(fmt.Sprintf("⚠️ [auto_wait恢复耗尽] [%s] 所有已恢复端点均失败，继续跨渠道故障转移", connID))
			}
		}
	}

	// 🔄 [请求级故障转移] 所有端点都失败了，尝试触发故障转移
	if lastFailedEndpoint != "" {
		// 跨渠道切换时，将本次请求中失败过的端点统一进入冷却，避免下一次请求立即重复撞同一批端点
		failedEndpointNames := endpointKeys(endpoints)
		excludeChannels := stringSetKeys(triedChannels)

		// 使用带循环计数的故障转移方法
		newChannel, err := sh.endpointManager.TriggerRequestFailoverWithLoopCount(
			failedEndpointNames,
			excludeChannels,
			"all_retries_exhausted",
			loopCount,
		)

		if err == nil && newChannel != "" {
			failedChannel := ""
			if len(endpoints) > 0 && endpoints[len(endpoints)-1] != nil {
				failedChannel = endpointRouteName(endpoints[len(endpoints)-1])
			}

			// 检测是否开始了新一轮循环
			// 修复: 增加 failedChannel == newChannel 备用检查，解决 triedChannels 检查失效的 bug
			// 当只有一个渠道时，故障转移返回的 newChannel 会与 failedChannel 相同，表示进入循环重试
			newLoopCount, alreadyTried, isSameChannel := nextFailoverLoopState(loopCount, failedChannel, newChannel, triedChannels, triedEndpoints)
			if newLoopCount != loopCount {
				// 开始新一轮：清空已尝试渠道和端点，增加循环计数
				slog.Info(fmt.Sprintf("🔄 [循环轮次] [%s] 开始第 %d 轮循环，清空已尝试列表 (alreadyTried=%v, isSameChannel=%v)",
					connID, newLoopCount, alreadyTried, isSameChannel))
			}

			slog.Warn(fmt.Sprintf("🔄 [渠道间故障转移] [%s] from_channel=%s to_channel=%s last_endpoint=%s failed_endpoints=%d reason=%s loop=%d",
				connID, failedChannel, newChannel, lastFailedEndpoint, len(failedEndpointNames), "all_retries_exhausted", newLoopCount))
			// 🔥 [递归修复] 2025-12-26: 检查重启次数限制，防止栈溢出
			if lifecycleManager.CanRestart() {
				// 🔥 [重复计费防护] 2025-12-26: 检查是否已发送内容
				if lifecycleManager.HasOutputContent() {
					slog.Warn(fmt.Sprintf("🚫 [重复计费防护] [%s] 故障转移成功但已发送内容，禁止重试", connID))
					lifecycleManager.FailRequest("output_sent_no_retry",
						"Cannot retry after sending partial output to client", http.StatusBadGateway)
					sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
					// 使用 sendStreamInterruptedMessage 触发客户端重试
					sendStreamInterruptedMessage(w, flusher, "已发送部分内容，服务端停止重试，请客户端重试", modelNameOrUnknown(lifecycleManager))
					return
				}

				restartCount := lifecycleManager.IncrementRestart()
				slog.Info(fmt.Sprintf("🔄 [故障转移重启] [%s] 渠道切换完成，重启次数: %d/%d",
					connID, restartCount, lifecycleManager.GetMaxRestarts()))
				// 🔧 [P0-S1 修复] 不再向客户端发送非协议的 "data: failover:" 数据行，
				// 避免严格 JSON 解析器（如 Claude Code CLI）误将其当 JSON 解析。
				// 内部状态变更已通过上方 slog 记录。
				sh.executeStreamingWithRetry(ctx, w, r, bodyBytes, lifecycleManager, flusher, triedChannels, triedEndpoints, newLoopCount)
				return
			} else {
				slog.Warn(fmt.Sprintf("❌ [重启限制] [%s] 故障转移成功但已达最大重启次数 (%d)，终止请求",
					connID, lifecycleManager.GetMaxRestarts()))
				lifecycleManager.FailRequest("max_restarts_exceeded",
					"Exceeded maximum restart attempts after failover", http.StatusBadGateway)
				sh.endpointManager.ClearEndpointsRequestFailure(failedEndpointNames)
				// 使用 sendStreamInterruptedMessage 触发客户端重试
				sendStreamInterruptedMessage(w, flusher, "已达最大重启次数，请客户端重试", modelNameOrUnknown(lifecycleManager))
				return
			}
		} else if err != nil {
			// 故障转移失败时，根据恢复策略决定是否立即返回错误
			slog.Warn(fmt.Sprintf("⚠️ [渠道间故障转移失败] [%s] last_endpoint=%s err=%v loop=%d",
				connID, lastFailedEndpoint, err, loopCount))

			// 检查是否是"没有可用渠道"或"达到最大循环轮数"的错误
			// 这些情况下不应该继续挂起等待，应该直接返回错误
			shouldTerminate, isLoopLimitError, errStr := failoverTermination(err)
			if shouldTerminate {
				sh.writeStreamingFailoverTermination(w, r, lifecycleManager, failedEndpointNames, lastErr, errStr, isLoopLimitError, connID)
				return
			}
		}
	}

	// 尚未写出流字节时按普通 HTTP 错误返回：服务商响应原样透传，网络错误使用标准网关状态。
	sh.writeStreamingEndpointExhausted(w, r, lifecycleManager, stringSetKeys(triedEndpoints), lastErr, connID)
}

// UpdateConfig 更新流式处理器配置
// 支持运行时配置热更新，确保设置变更能立即生效
func (sh *StreamingHandler) UpdateConfig(cfg *config.Config) {
	sh.config = cfg
}
