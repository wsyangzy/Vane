package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"claude-request-forwarder/config"
	"claude-request-forwarder/internal/endpoint"
	"claude-request-forwarder/internal/tracking"

	"github.com/google/uuid"
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
	errorRecoveryFactory     ErrorRecoveryFactory
	retryManagerFactory      RetryManagerFactory
	suspensionManagerFactory SuspensionManagerFactory
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
	errorRecoveryFactory ErrorRecoveryFactory,
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
		errorRecoveryFactory:     errorRecoveryFactory,
		retryManagerFactory:      retryManagerFactory,
		suspensionManagerFactory: suspensionManagerFactory,
		// 🔧 [Critical修复] 使用传入的共享SuspensionManager实例
		// 确保流式请求与常规请求共享同一个全局挂起计数器
		sharedSuspensionManager: sharedSuspensionManager,
	}
}

// noOpFlusher 是一个不执行实际flush操作的flusher实现
type noOpFlusher struct{}

func (f *noOpFlusher) Flush() {
	// 不执行任何操作，避免panic但保持流式处理逻辑
}

// sendAnthropicRetryableError 发送 Anthropic API 标准格式的可重试错误事件
// 使用 overloaded_error 类型，Claude Code 等客户端会识别并自动重试
func sendAnthropicRetryableError(w http.ResponseWriter, flusher http.Flusher, message string) {
	// 发送 SSE 格式的 error 事件
	// 格式: event: error\ndata: {"type":"error","error":{"type":"xxx","message":"xxx"}}\n\n
	fmt.Fprintf(w, "event: error\n")
	fmt.Fprintf(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"%s\"}}\n\n",
		escapeJSONString(message))
	flusher.Flush()
}

// terminateSSEStreamWithError 正确终止 SSE 流，确保客户端不会自动重连
// v6.2.6: 修复客户端无限重连问题 - 发送完整的 SSE 结束序列
func terminateSSEStreamWithError(w http.ResponseWriter, flusher http.Flusher, errorMsg string) {
	// 1. 发送错误数据
	fmt.Fprintf(w, "data: error: %s\n\n", errorMsg)
	flusher.Flush()

	// 2. 发送 Anthropic 格式的 error 事件（让客户端识别为 API 错误而非连接中断）
	fmt.Fprintf(w, "event: error\n")
	fmt.Fprintf(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"%s\"}}\n\n",
		escapeJSONString(errorMsg))
	flusher.Flush()

	// 3. 发送 message_stop 事件（关键！明确告知客户端流已正常结束）
	fmt.Fprintf(w, "event: message_stop\n")
	fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
	flusher.Flush()
}

// sendStreamInterruptedMessage 发送流中断消息，触发客户端自动重试
// 模拟上游代理的行为：当 EOF 发生时，发送一个完整的新消息（第二个 message_start）
// Claude Code 会识别这种模式并自动重试请求
// 参考: req-fd3a8dd9.debug 中观察到的上游代理行为
func sendStreamInterruptedMessage(w http.ResponseWriter, flusher http.Flusher, message string, modelName string) {
	// 使用 UUID 作为 message id（模拟上游代理的行为，与正常的 msg_ 前缀不同）
	msgID := uuid.New().String()

	// 1. message_start - 新消息开始（使用 Anthropic 格式的 usage 字段）
	fmt.Fprintf(w, "event: message_start\n")
	fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"%s\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"%s\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n",
		msgID, escapeJSONString(modelName))
	flusher.Flush()

	// 2. content_block_start - 开始 text 类型的内容块
	fmt.Fprintf(w, "event: content_block_start\n")
	fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
	flusher.Flush()

	// 3. content_block_delta - 发送错误信息内容
	fmt.Fprintf(w, "event: content_block_delta\n")
	fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"%s\"}}\n\n",
		escapeJSONString(message))
	flusher.Flush()

	// 4. content_block_stop - 内容块结束
	fmt.Fprintf(w, "event: content_block_stop\n")
	fmt.Fprintf(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	flusher.Flush()

	// 5. message_delta - 消息增量更新（包含 stop_reason 和 usage，让客户端认为流完整）
	fmt.Fprintf(w, "event: message_delta\n")
	fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":0}}\n\n")
	flusher.Flush()

	// 6. message_stop - 消息结束（关键！让客户端识别为完整消息从而触发重试）
	fmt.Fprintf(w, "event: message_stop\n")
	fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
	flusher.Flush()
}

// isStreamingEOFError 检查错误是否为流式传输过程中的 EOF 错误
// 只匹配在流式响应过程中服务端突然断开的情况（已收到 200 响应）
func isStreamingEOFError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// 检查是否包含 stream_status 前缀（表示已进入流式处理阶段）且包含 eof
	return strings.Contains(errStr, "stream_status:") && strings.Contains(errStr, "eof")
}

// escapeJSONString 对字符串进行 JSON 转义
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// HandleStreamingRequest 统一流式请求处理
// 使用V2架构整合错误恢复机制和生命周期管理的流式处理
func (sh *StreamingHandler) HandleStreamingRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager) {
	connID := lifecycleManager.GetRequestID()

	slog.Info(fmt.Sprintf("🌊 [流式架构] [%s] 使用streaming v2架构", connID))
	slog.Info(fmt.Sprintf("🌊 [流式处理] [%s] 开始流式请求处理", connID))
	sh.handleStreamingV2(ctx, w, r, bodyBytes, lifecycleManager)
}

// handleStreamingV2 流式处理（带错误恢复）
func (sh *StreamingHandler) handleStreamingV2(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager) {
	connID := lifecycleManager.GetRequestID()

	// 设置流式响应头
	sh.setStreamingHeaders(w)

	// 获取Flusher - 如果不支持，使用无flush模式继续流式处理
	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Warn(fmt.Sprintf("🌊 [Flusher不支持] [%s] 将使用无flush模式的流式处理", connID))
		// 创建一个mock flusher，不执行实际flush操作
		flusher = &noOpFlusher{}
	}

	// 继续执行流式请求处理
	triedChannels := make(map[string]struct{}, 4)
	// v6.1+: 追踪本次请求链路内已尝试的端点（渠道内严格优先级转移）
	triedEndpoints := make(map[string]struct{}, 8)
	// v6.2: 追踪循环轮数（用于 MaxLoops 限制）
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

// executeStreamingWithRetry 执行带重试的流式处理
// triedChannels: 本次请求链路内已尝试过的渠道（方案B：渠道间故障转移不回访，避免循环）
// triedEndpoints: v6.1+ 本次请求链路内已尝试过的端点（渠道内严格优先级转移，不回退）
// loopCount: v6.2 当前循环轮数（用于 MaxLoops 限制）
func (sh *StreamingHandler) executeStreamingWithRetry(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager, flusher http.Flusher, triedChannels map[string]struct{}, triedEndpoints map[string]struct{}, loopCount int) {
	connID := lifecycleManager.GetRequestID()

	// v6.2.7: 在函数开头检查 context 是否已取消（客户端断开连接）
	select {
	case <-ctx.Done():
		slog.Info(fmt.Sprintf("🚫 [客户端断开] [%s] 检测到客户端已断开连接，停止重试 (loopCount=%d)", connID, loopCount))
		lifecycleManager.CancelRequest("client disconnected", nil)
		return
	default:
	}

	var lastFailedEndpoint string // 🚀 [端点自愈] 追踪最后失败的端点

	// v6.1+: 获取健康端点，排除已尝试的端点（实现严格优先级转移）
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
		excludeKeys := make([]string, 0, len(triedEndpoints))
		for key := range triedEndpoints {
			excludeKeys = append(excludeKeys, key)
		}
		endpoints = retryMgr.GetHealthyEndpointsExcluding(ctx, excludeKeys)
		if len(endpoints) > 0 {
			slog.Info(fmt.Sprintf("🔄 [严格优先级转移] [%s] 排除已尝试端点后剩余 %d 个候选端点", connID, len(endpoints)))
		}
	}

	if len(endpoints) == 0 {
		// 创建特殊错误，交给错误分类和重试系统处理
		noHealthyErr := fmt.Errorf("no healthy endpoints available")
		errorRecovery := sh.errorRecoveryFactory.NewErrorRecoveryManager(sh.usageTracker)
		errorCtx := errorRecovery.ClassifyError(noHealthyErr, connID, "", "", 0)

		if errorCtx.ErrorType == ErrorTypeNoHealthyEndpoints {
			// 尝试获取所有活跃端点，忽略健康状态
			allActiveEndpoints := sh.endpointManager.GetActiveEndpointsIgnoringHealth()

			if len(allActiveEndpoints) > 0 {
				slog.InfoContext(ctx, fmt.Sprintf("🔄 [健康检查回退] [%s] 忽略健康状态，尝试 %d 个活跃端点",
					connID, len(allActiveEndpoints)))
				endpoints = allActiveEndpoints
				// 继续正常处理流程
			} else {
				// 真的没有端点
				lifecycleManager.HandleError(noHealthyErr)
				// 🔧 [修复] 2025-12-11: 所有端点不可用时必须终结请求
				// v6.2.7: 使用 422 (Unprocessable Entity) 而非 503，防止 Claude Code 客户端自动重试
				// 503 表示"服务暂时不可用"，客户端会自动重试；422 表示"无法处理请求"，客户端不会重试
				lifecycleManager.FailRequest("no_endpoints", "No endpoints available in active groups", http.StatusUnprocessableEntity)
				w.WriteHeader(http.StatusUnprocessableEntity)
				// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
				terminateSSEStreamWithError(w, flusher, "No endpoints available in active groups")
				return
			}
		} else {
			// 按原来逻辑处理
			lifecycleManager.HandleError(noHealthyErr)
			// 🔧 [修复] 2025-12-11: 所有端点不可用时必须终结请求
			// v6.2.7: 使用 422 (Unprocessable Entity) 而非 503，防止 Claude Code 客户端自动重试
			lifecycleManager.FailRequest("no_healthy_endpoints", "No healthy endpoints available", http.StatusUnprocessableEntity)
			w.WriteHeader(http.StatusUnprocessableEntity)
			// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
			terminateSSEStreamWithError(w, flusher, "No healthy endpoints available")
			return
		}
	}

	// 方案B：记录当前尝试的渠道（用于后续跨渠道故障转移时不回访）
	if triedChannels != nil && len(endpoints) > 0 && endpoints[0] != nil {
		currentChannel := endpoints[0].Config.Channel
		if currentChannel == "" {
			currentChannel = endpoints[0].Config.Name
		}
		if currentChannel != "" {
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

		// v6.1+: 记录已尝试的端点（确保严格优先级转移，不回退）
		epKey := endpoint.EndpointKey(ep.Config.Channel, ep.Config.Name)
		triedEndpoints[epKey] = struct{}{}

		// 更新生命周期管理器信息
		routeGroup := ep.Config.Channel
		if routeGroup == "" {
			routeGroup = ep.Config.Name
		}
		lifecycleManager.SetEndpoint(ep.Config.Name, routeGroup, ep.Config.Channel)
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
				fmt.Fprintf(w, "data: cancelled: 客户端取消请求\n\n")
				flusher.Flush()
				return
			default:
			}

			// 尝试连接端点
			resp, err := sh.forwarder.ForwardRequestToEndpoint(ctx, r, bodyBytes, ep)
			// 🔧 [修复] 保存最后的响应，用于获取真实HTTP状态码
			lastResp = resp
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

				// 处理流式响应 - 使用现有的流式处理逻辑
				w.WriteHeader(resp.StatusCode)

				// 创建Token解析器和流式处理器
				tokenParser := sh.tokenParserFactory.NewTokenParserWithUsageTracker(connID, sh.usageTracker)
				processor := sh.streamProcessorFactory.NewStreamProcessor(tokenParser, sh.usageTracker, w, flusher, connID, ep.Config.Name)

				slog.Info(fmt.Sprintf("🚀 [开始流式处理] [%s] 端点: %s", connID, ep.Config.Name))

				// 执行流式处理并获取Token信息和模型名称
				finalTokenUsage, modelName, err := processor.ProcessStreamWithRetry(ctx, resp)

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

						// v6.2: 移除渠道内端点冷却，使用 triedEndpoints 追踪已尝试端点
						// 日志记录端点失败原因
						slog.Warn(fmt.Sprintf("⚠️ [流截断] [%s] endpoint=%s reason=%s",
							connID, ep.Config.Name, failureReason))

						// 🔥 [重复计费防护] 若已向客户端输出内容，则不再尝试其他端点（避免“部分输出 + 重试”导致重复计费/重复内容）。
						if lifecycleManager.HasOutputContent() {
							lifecycleManager.RecordTokensForFailedRequest(finalTokenUsage, failureReason)
							lifecycleManager.FailRequest(failureReason, err.Error(), http.StatusBadGateway)
							// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
							terminateSSEStreamWithError(w, flusher, "流式响应中断，已发送部分内容，停止重试避免重复计费")
							return
						}

						// 🚀 [Plan D核心修改] 记录部分Token到数据库，但不完成请求；继续尝试其他端点完成响应。
						lifecycleManager.RecordPartialTokens(finalTokenUsage, failureReason, ep.Config.Name)

						slog.Warn(fmt.Sprintf("⚠️ [流截断记录部分Token] [%s] 端点: %s, failure_reason: %s, 模型: %s, 将继续重试其他端点",
							connID, ep.Config.Name, failureReason, parsedModelName))

						// 将此错误视为端点失败，触发故障转移
						lastErr = err
						endpointSuccess = false
						continue // 跳过当前端点，尝试下一个
					}

					// 处理其他类型的错误（保留原有逻辑用于兼容）
					var status, parsedModelName string = "error", "unknown"

					// ✅ 从错误信息中提取状态和模型信息
					if strings.HasPrefix(err.Error(), "stream_status:") {
						parts := strings.SplitN(err.Error(), ":", 5)
						if len(parts) >= 4 {
							status = parts[1] // 状态：cancelled, timeout, error
							if parts[2] == "model" && len(parts) > 3 && parts[3] != "" {
								parsedModelName = parts[3] // 模型：claude-sonnet-4-20250514
							}
						}
					}

					// ✅ 确保生命周期管理器获得正确的模型信息
					// 优先使用从错误包装器中解析的模型信息
					if parsedModelName != "unknown" && parsedModelName != "" {
						lifecycleManager.SetModelWithComparison(parsedModelName, "stream_status")
					} else if modelName != "unknown" && modelName != "" {
						// ✅ 如果错误包装器中没有模型信息，使用ProcessStreamWithRetry返回的模型信息
						lifecycleManager.SetModelWithComparison(modelName, "stream_processor")
					}

					// 🚀 [状态机重构] Phase 4: 统一使用HandleError处理错误，遵循状态错误分离原则
					// 设置failure_reason，让错误分类器正确识别stream_status错误
					lifecycleManager.HandleError(err)

					// 🚀 [HTTP状态码修复] 流式API错误应该映射为207 Multi-Status
					statusCode := GetStatusCodeFromError(err, resp)
					if status == "error" || status == "stream_error" {
						statusCode = http.StatusMultiStatus // 207: HTTP连接成功，但API业务层面有错误
					} else if status == "cancelled" {
						statusCode = 499 // 客户端取消
					}

					// 🚀 [语义修复] 区分取消和失败的不同处理方式
					if status == "cancelled" {
						// 取消请求：直接传递Token信息给CancelRequest，保持语义一致性
						// 避免先调用RecordTokensForFailedRequest再CancelRequest的语义矛盾
						lifecycleManager.CancelRequest("stream processing cancelled", finalTokenUsage)
					} else {
						// 流式错误：先记录失败Token，再使用FailRequest设置最终状态
						if finalTokenUsage != nil {
							lifecycleManager.RecordTokensForFailedRequest(finalTokenUsage, status)
						} else {
							// 无Token信息，仅记录失败状态
							slog.Info(fmt.Sprintf("❌ [流式失败无Token] [%s] 端点: %s, 状态: %s, 无Token信息可保存",
								connID, ep.Config.Name, status))
						}
						// 使用FailRequest设置最终状态为failed
						// 这样status=failed, failure_reason=stream_error, http_status=207
						lifecycleManager.FailRequest(status, err.Error(), statusCode)
					}

					// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))

					slog.Warn(fmt.Sprintf("🔄 [流式处理失败] [%s] 端点: %s, 状态: %s, 模型: %s, 错误: %v",
						connID, ep.Config.Name, status, parsedModelName, err))

					// 根据状态决定是否发送错误信息
					if status == "cancelled" {
						fmt.Fprintf(w, "data: cancelled: 客户端取消请求\n\n")
						flusher.Flush()
					} else if isStreamingEOFError(err) && sh.config.RequestSuspend.EOFRetryHint {
						// 🔄 [EOF重试提示] 流式传输过程中 EOF：发送中断消息触发客户端自动重试
						// 优先使用从错误信息中解析的模型名称（parsedModelName），因为它是流处理过程中解析的
						// 其次使用 ProcessStreamWithRetry 返回的 modelName
						interruptModelName := parsedModelName
						if interruptModelName == "" || interruptModelName == "unknown" {
							interruptModelName = modelName
						}
						if interruptModelName == "" || interruptModelName == "unknown" {
							interruptModelName = "claude-3-5-sonnet-20241022" // 默认模型
						}
						slog.Info(fmt.Sprintf("🔄 [EOF重试提示] [%s] 流式传输中断，发送中断消息触发客户端重试, 模型: %s", connID, interruptModelName))
						sendStreamInterruptedMessage(w, flusher, "Connection interrupted", interruptModelName)
					} else {
						// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
						terminateSSEStreamWithError(w, flusher, fmt.Sprintf("流式处理失败: %v", err))
					}
					return
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
				return
			}

			// ❌ 出现错误，记录尝试次数
			globalAttemptCount := lifecycleManager.IncrementAttempt()
			lastErr = err

			// 错误处理 - 先构造HTTP状态码错误（保持现有逻辑）
			if err == nil && resp != nil && !IsSuccessStatus(resp.StatusCode) {
				closeErr := resp.Body.Close() // 立即关闭非成功响应体
				if closeErr != nil {
					slog.Warn(fmt.Sprintf("⚠️ [响应体关闭失败] [%s] 端点: %s, Close错误: %v", connID, ep.Config.Name, closeErr))
				}
				// 构造HTTP状态码错误，确保RetryManager能正确分类429等状态
				lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
			} else if err != nil && resp != nil {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					slog.Warn(fmt.Sprintf("⚠️ [错误响应体关闭失败] [%s] 端点: %s, Close错误: %v", connID, ep.Config.Name, closeErr))
				}
			}

			// 🔧 使用增强的RetryManager进行统一决策
			errorRecovery := sh.errorRecoveryFactory.NewErrorRecoveryManager(sh.usageTracker)
			errorCtx := errorRecovery.ClassifyError(lastErr, connID, ep.Config.Name, routeGroup, attempt-1)

			// 🔥 [计费感知分类] 2025-12-29: 根据HasOutputContent()动态调整Stream错误的可重试性
			// 核心逻辑：token计费 = HasOutputContent() = true → 不可重试（避免重复计费）
			//          0 tokens = HasOutputContent() = false → 可重试（连接阶段中断）
			errStr := strings.ToLower(lastErr.Error())
			if strings.HasPrefix(errStr, "stream_status:") || strings.Contains(errStr, "stream_error") {
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
			lifecycleManager.PrepareErrorContext(&errorCtx)
			lifecycleManager.HandleError(lastErr)

			// v6.1+: 使用函数开头创建的重试管理器（避免重复创建）
			// 🔢 [关键修复] 分离局部和全局计数语义
			// attempt: 当前端点内的尝试次数，用于退避计算
			// globalAttemptCount: 全局尝试次数，用于限流策略
			decision := retryMgr.ShouldRetryWithDecision(&errorCtx, attempt, globalAttemptCount, true) // 流式请求: isStreaming=true
			lastDecision = &decision                                                                   // 保存决策，供外层逻辑使用

			// 检查决策结果
			if decision.FinalStatus == "cancelled" {
				// 🔧 [修复] 添加生命周期状态更新
				lifecycleManager.CancelRequest("client disconnected", nil)
				slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 检测到客户端取消，立即停止重试", connID))

				// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
				*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
				fmt.Fprintf(w, "data: cancelled: 客户端取消请求\n\n")
				flusher.Flush()
				return
			}

			// 处理挂起决策
			if decision.SuspendRequest {
				if sh.sharedSuspensionManager.ShouldSuspend(ctx) {
					// 🚀 [状态机重构] Phase 4: 挂起时更新状态
					lifecycleManager.UpdateStatus("suspended", -1, 0)
					slog.Info(fmt.Sprintf("⏸️ [流式挂起] [%s] 原因: %s，失败端点: %s", connID, decision.Reason, ep.Config.Name))
					fmt.Fprintf(w, "data: suspend: 请求已挂起，等待端点 %s 恢复或组切换...\n\n", ep.Config.Name)
					flusher.Flush()

					// 🚀 [端点自愈] 使用新的端点恢复等待方法，能区分成功/超时/取消
					result := sh.sharedSuspensionManager.WaitForEndpointRecoveryWithResult(ctx, connID, ep.Config.Name)
					switch result {
					case SuspensionSuccess:
						// 🔥 [递归修复] 2025-12-26: 检查重启次数限制，防止栈溢出
						if lifecycleManager.CanRestart() {
							// 🔥 [重复计费防护] 2025-12-26: 检查是否已发送内容
							if lifecycleManager.HasOutputContent() {
								slog.Warn(fmt.Sprintf("🚫 [重复计费防护] [%s] 端点恢复成功但已发送内容，禁止重试", connID))
								lifecycleManager.FailRequest("output_sent_no_retry",
									"Cannot retry after sending partial output to client", http.StatusBadGateway)
								// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
								terminateSSEStreamWithError(w, flusher, "已发送部分内容，无法重试")
								return
							}

							restartCount := lifecycleManager.IncrementRestart()
							slog.Info(fmt.Sprintf("🔄 [端点恢复重启] [%s] 端点 %s 已恢复，重启次数: %d/%d",
								connID, ep.Config.Name, restartCount, lifecycleManager.GetMaxRestarts()))
							fmt.Fprintf(w, "data: resume: 端点已恢复，重新开始处理...\n\n")
							flusher.Flush()
							sh.executeStreamingWithRetry(ctx, w, r, bodyBytes, lifecycleManager, flusher, triedChannels, triedEndpoints, loopCount)
							return
						} else {
							slog.Warn(fmt.Sprintf("❌ [重启限制] [%s] 已达最大重启次数 (%d)，终止请求",
								connID, lifecycleManager.GetMaxRestarts()))
							// v6.2.7: 使用 422 而非 503，防止 Claude Code 客户端自动重试
							lifecycleManager.FailRequest("max_restarts_exceeded",
								"Exceeded maximum restart attempts", http.StatusUnprocessableEntity)
							// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
							terminateSSEStreamWithError(w, flusher, "已达最大重启次数，请求终止")
							return
						}
					case SuspensionCancelled:
						// 🎯 [挂起取消区分] 用户在挂起期间取消请求，应该记录为取消而非失败
						slog.Info(fmt.Sprintf("🚫 [挂起期间取消] [%s] 用户在挂起期间取消请求", connID))
						// 🔧 [状态码修复] 设置取消状态码到上下文用于日志记录
						*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
						lifecycleManager.CancelRequest("suspended then cancelled", nil)
						fmt.Fprintf(w, "data: cancelled: 客户端取消请求\n\n")
						flusher.Flush()
						return
					case SuspensionTimeout:
						// 🔧 [修复] 添加生命周期状态更新
						currentAttemptCount := lifecycleManager.GetAttemptCount()
						lifecycleManager.UpdateStatus("error", currentAttemptCount, http.StatusBadGateway)
						slog.Warn(fmt.Sprintf("⏰ [挂起超时] [%s] 等待端点恢复或组切换超时", connID))
						// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
						terminateSSEStreamWithError(w, flusher, "挂起等待超时")
						return
					}
				}
			}

			if !decision.RetrySameEndpoint {
				// v6.2.9: 获取失败状态码并设置到端点，用于 UI 显示
				failStatusCode := GetStatusCodeFromError(lastErr, lastResp)
				if failStatusCode == 0 {
					switch decision.FinalStatus {
					case "cancelled":
						failStatusCode = 499
					case "auth_error":
						failStatusCode = http.StatusUnauthorized
					case "rate_limited":
						failStatusCode = http.StatusTooManyRequests
					default:
						failStatusCode = http.StatusBadGateway
					}
				}
				// v6.2.10: 设置端点的失败代码和原因，用于 UI 显示
				failReason := decision.Reason
				if lastErr != nil && failReason == "" {
					failReason = lastErr.Error()
				}
				sh.endpointManager.SetEndpointLastRequestFail(ep.Config.Name, failStatusCode, failReason)

				if decision.SwitchEndpoint {
					// v6.2: 移除渠道内端点冷却，使用 triedEndpoints 追踪
					slog.Info(fmt.Sprintf("🔀 [切换端点] [%s] channel=%s endpoint=%s reason=%s",
						connID, ep.Config.Channel, ep.Config.Name, decision.Reason))
					break // 尝试下一个端点
				} else {
					// 🚀 [状态机重构] Phase 4: 最终失败处理
					// 获取失败原因
					failureReason := lifecycleManager.MapErrorTypeToFailureReason(errorCtx.ErrorType)

					// 使用新的FailRequest方法标记最终失败
					// v6.2: 移除流式终态错误时的端点冷却，使用 triedEndpoints 追踪
					lifecycleManager.FailRequest(failureReason, lastErr.Error(), failStatusCode)

					// 终止重试
					slog.Info(fmt.Sprintf("🛑 [终止重试] [%s] 端点: %s, 状态: %s, 状态码: %d, 原因: %s",
						connID, ep.Config.Name, decision.FinalStatus, failStatusCode, decision.Reason))
					// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
					terminateSSEStreamWithError(w, flusher, decision.Reason)
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

				// 向客户端发送重试信息
				fmt.Fprintf(w, "data: retry: 重试端点 %s (尝试 %d/%d)，等待 %v...\n\n",
					ep.Config.Name, attempt+1, sh.config.Retry.MaxAttempts, decision.Delay)
				flusher.Flush()

				// 等待延迟，同时检查取消
				select {
				case <-ctx.Done():
					slog.Info(fmt.Sprintf("🚫 [重试取消] [%s] 等待重试期间检测到取消", connID))
					lifecycleManager.CancelRequest("client disconnected during retry delay", nil)

					// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
					fmt.Fprintf(w, "data: cancelled: 客户端取消请求\n\n")
					flusher.Flush()
					return
				case <-time.After(decision.Delay):
					// 继续下一次重试
				}
			}
		}

		// 🔧 当前端点所有重试都失败了
		if !endpointSuccess {
			// 修复计数逻辑：处理提前break和自然跑满两种情况
			actualAttempts := attempt
			if actualAttempts > sh.config.Retry.MaxAttempts {
				actualAttempts = sh.config.Retry.MaxAttempts
			}

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
						errorRecovery := sh.errorRecoveryFactory.NewErrorRecoveryManager(sh.usageTracker)
						errorCtx := errorRecovery.ClassifyError(lastErr, connID, "", "", 0)
						failureReason = lifecycleManager.MapErrorTypeToFailureReason(errorCtx.ErrorType)
					}
					// 获取真实的HTTP状态码
					statusCode := GetStatusCodeFromError(lastErr, lastResp)
					if statusCode == 0 {
						// 根据决策状态设置合适的默认状态码
						if lastDecision != nil && lastDecision.FinalStatus != "" {
							switch lastDecision.FinalStatus {
							case "cancelled":
								statusCode = 499 // nginx风格的客户端取消码
							case "auth_error":
								statusCode = http.StatusUnauthorized
							case "rate_limited":
								statusCode = http.StatusTooManyRequests
							default:
								statusCode = http.StatusBadGateway
							}
						} else {
							statusCode = http.StatusBadGateway
						}
					}
					lifecycleManager.FailRequest(failureReason, lastDecision.Reason, statusCode)
					// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
					terminateSSEStreamWithError(w, flusher, lastDecision.Reason)
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

			// 如果不是最后一个端点，尝试下一个端点
			if i < len(endpoints)-1 {
				fmt.Fprintf(w, "data: retry: 切换到备用端点: %s\n\n", endpoints[i+1].Config.Name)
				flusher.Flush()
				continue
			}
		}
	}

	// v6.1+: [auto_wait策略特殊处理] 在跨渠道故障转移前，检查渠道内是否有已恢复的端点
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
			if len(recoveredEndpoints) > 0 {
				slog.Info(fmt.Sprintf("🔄 [auto_wait恢复重试] [%s] 发现 %d 个已恢复端点，按优先级重试一次",
					connID, len(recoveredEndpoints)))
				fmt.Fprintf(w, "data: retry: 检测到 %d 个端点已恢复，尝试重试...\n\n", len(recoveredEndpoints))
				flusher.Flush()

				// 对每个已恢复的端点尝试一次（按优先级顺序）
				for _, ep := range recoveredEndpoints {
					// 检查客户端取消
					select {
					case <-ctx.Done():
						slog.Info(fmt.Sprintf("🚫 [客户端取消检测] [%s] 恢复重试期间检测到客户端取消", connID))
						lifecycleManager.CancelRequest("client disconnected during recovery retry", nil)
						*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
						fmt.Fprintf(w, "data: cancelled: 客户端取消请求\n\n")
						flusher.Flush()
						return
					default:
					}

					// 更新生命周期管理器信息
					routeGroup := ep.Config.Channel
					if routeGroup == "" {
						routeGroup = ep.Config.Name
					}
					lifecycleManager.SetEndpoint(ep.Config.Name, routeGroup, ep.Config.Channel)
					lifecycleManager.UpdateStatus("forwarding", lifecycleManager.GetAttemptCount(), 0)
					*r = *r.WithContext(context.WithValue(r.Context(), "selected_endpoint", ep.Config.Name))

					slog.Info(fmt.Sprintf("🔄 [auto_wait恢复重试] [%s] 尝试已恢复端点: %s (优先级: %d)",
						connID, ep.Config.Name, ep.Config.Priority))
					fmt.Fprintf(w, "data: retry: 尝试已恢复端点: %s\n\n", ep.Config.Name)
					flusher.Flush()

					// 尝试转发请求
					resp, err := sh.forwarder.ForwardRequestToEndpoint(ctx, r, bodyBytes, ep)
					if err == nil && IsSuccessStatus(resp.StatusCode) {
						lifecycleManager.IncrementAttempt()
						currentAttemptCount := lifecycleManager.GetAttemptCount()

						slog.Info(fmt.Sprintf("✅ [auto_wait恢复成功] [%s] 端点: %s 恢复重试成功",
							connID, ep.Config.Name))

						lifecycleManager.UpdateStatus("processing", currentAttemptCount, resp.StatusCode)
						w.WriteHeader(resp.StatusCode)

						// 创建Token解析器和流式处理器
						tokenParser := sh.tokenParserFactory.NewTokenParserWithUsageTracker(connID, sh.usageTracker)
						processor := sh.streamProcessorFactory.NewStreamProcessor(tokenParser, sh.usageTracker, w, flusher, connID, ep.Config.Name)

						// 执行流式处理
						finalTokenUsage, modelName, streamErr := processor.ProcessStreamWithRetry(ctx, resp)
						if processor.HasSentContent() {
							lifecycleManager.MarkOutputSent()
						}

						if streamErr != nil {
							// 流处理出错，记录并继续尝试下一个
							slog.Warn(fmt.Sprintf("⚠️ [auto_wait恢复流失败] [%s] 端点: %s 流处理失败: %v",
								connID, ep.Config.Name, streamErr))
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
						return
					}

					// 恢复重试失败，记录错误继续下一个
					if resp != nil {
						resp.Body.Close()
					}
					slog.Warn(fmt.Sprintf("⚠️ [auto_wait恢复失败] [%s] 端点: %s 恢复重试失败: %v",
						connID, ep.Config.Name, err))
					lastErr = err

					// v6.2: 移除渠道内端点冷却，使用 triedEndpoints 追踪
				}

				slog.Warn(fmt.Sprintf("⚠️ [auto_wait恢复耗尽] [%s] 所有已恢复端点均失败，继续跨渠道故障转移", connID))
			}
		}
	}

	// 🔄 [请求级故障转移] 所有端点都失败了，尝试触发故障转移
	if lastFailedEndpoint != "" {
		// v6.0+：跨渠道切换时，将本次请求中失败过的端点统一进入冷却，避免下一次请求立即重复撞同一批端点
		failedEndpointNames := make([]string, 0, len(endpoints))
		seen := make(map[string]struct{}, len(endpoints))
		for _, ep := range endpoints {
			key := endpoint.EndpointKey(ep.Config.Channel, ep.Config.Name)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			failedEndpointNames = append(failedEndpointNames, key)
		}

		excludeChannels := make([]string, 0, len(triedChannels))
		for name := range triedChannels {
			if name == "" {
				continue
			}
			excludeChannels = append(excludeChannels, name)
		}

		// v6.2: 使用带循环计数的故障转移方法
		newChannel, err := sh.endpointManager.TriggerRequestFailoverWithLoopCount(
			failedEndpointNames,
			excludeChannels,
			"all_retries_exhausted",
			loopCount,
		)

		if err == nil && newChannel != "" {
			failedChannel := ""
			if len(endpoints) > 0 && endpoints[len(endpoints)-1] != nil {
				failedChannel = endpoints[len(endpoints)-1].Config.Channel
				if failedChannel == "" {
					failedChannel = endpoints[len(endpoints)-1].Config.Name
				}
			}

			// v6.2.8: 检测是否开始了新一轮循环
			// 修复: 增加 failedChannel == newChannel 备用检查，解决 triedChannels 检查失效的 bug
			// 当只有一个渠道时，故障转移返回的 newChannel 会与 failedChannel 相同，表示进入循环重试
			newLoopCount := loopCount
			_, alreadyTried := triedChannels[newChannel]
			isSameChannel := failedChannel != "" && failedChannel == newChannel
			if alreadyTried || isSameChannel {
				// 开始新一轮：清空已尝试渠道和端点，增加循环计数
				newLoopCount++
				slog.Info(fmt.Sprintf("🔄 [循环轮次] [%s] 开始第 %d 轮循环，清空已尝试列表 (alreadyTried=%v, isSameChannel=%v)",
					connID, newLoopCount, alreadyTried, isSameChannel))
				// 清空已尝试的渠道和端点
				for k := range triedChannels {
					delete(triedChannels, k)
				}
				for k := range triedEndpoints {
					delete(triedEndpoints, k)
				}
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
					// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
					terminateSSEStreamWithError(w, flusher, "已发送部分内容，无法重试")
					return
				}

				restartCount := lifecycleManager.IncrementRestart()
				slog.Info(fmt.Sprintf("🔄 [故障转移重启] [%s] 渠道切换完成，重启次数: %d/%d",
					connID, restartCount, lifecycleManager.GetMaxRestarts()))
				fmt.Fprintf(w, "data: failover: 渠道切换完成，恢复处理 (新渠道: %s)\n\n", newChannel)
				flusher.Flush()
				sh.executeStreamingWithRetry(ctx, w, r, bodyBytes, lifecycleManager, flusher, triedChannels, triedEndpoints, newLoopCount)
				return
			} else {
				slog.Warn(fmt.Sprintf("❌ [重启限制] [%s] 故障转移成功但已达最大重启次数 (%d)，终止请求",
					connID, lifecycleManager.GetMaxRestarts()))
				// v6.2.7: 使用 422 而非 503，防止 Claude Code 客户端自动重试
				lifecycleManager.FailRequest("max_restarts_exceeded",
					"Exceeded maximum restart attempts after failover", http.StatusUnprocessableEntity)
				// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
				terminateSSEStreamWithError(w, flusher, "已达最大重启次数，请求终止")
				return
			}
		} else if err != nil {
			// v6.2.5: 故障转移失败时，根据恢复策略决定是否立即返回错误
			slog.Warn(fmt.Sprintf("⚠️ [渠道间故障转移失败] [%s] last_endpoint=%s err=%v loop=%d",
				connID, lastFailedEndpoint, err, loopCount))

			// 检查是否是"没有可用渠道"或"达到最大循环轮数"的错误
			// 这些情况下不应该继续挂起等待，应该直接返回错误
			errStr := err.Error()
			isNoChannelsError := strings.Contains(errStr, "没有可用的故障转移渠道") ||
				strings.Contains(errStr, "所有渠道均已尝试")

			if isNoChannelsError {
				// 所有渠道都不可用或已达最大循环轮数，直接返回错误
				// v6.2.7: 使用 422 而非 503，防止 Claude Code 客户端自动重试
				statusCode := http.StatusUnprocessableEntity
				*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
				lifecycleManager.FailRequest("all_channels_unavailable", errStr, statusCode)
				slog.Error(fmt.Sprintf("❌ [所有渠道不可用] [%s] %s", connID, errStr))
				// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
				terminateSSEStreamWithError(w, flusher, fmt.Sprintf("所有渠道均不可用: %s", errStr))
				return
			}
		}
	}

	// 🔧 所有当前端点都失败，检查是否应该挂起请求
	// 注意：客户端取消错误已在上面统一处理，这里不会执行到

	// 🔧 [修复] 使用共享的SuspensionManager实例，确保全局挂起限制生效
	suspensionMgr := sh.sharedSuspensionManager

	// 检查是否应该挂起请求
	if suspensionMgr.ShouldSuspend(ctx) {
		currentEndpoints := sh.endpointManager.GetHealthyEndpoints()
		if cfg := sh.endpointManager.GetConfig(); cfg != nil && cfg.Strategy.Type == "fastest" && cfg.Strategy.FastTestEnabled {
			currentEndpoints = sh.endpointManager.GetFastestEndpointsWithRealTimeTest(ctx)
		}

		// 🚀 [状态机重构] Phase 4: 挂起时更新状态（移除重复的失败原因记录）
		lifecycleManager.UpdateStatus("suspended", -1, 0)
		fmt.Fprintf(w, "data: suspend: 当前所有组均不可用，请求已挂起等待组切换...\n\n")
		flusher.Flush()

		// 🔢 [语义修复] 在日志中记录端点数量信息，但不影响重试计数语义
		actualAttemptCount := lifecycleManager.GetAttemptCount()
		slog.Info(fmt.Sprintf("⏸️ [流式挂起] [%s] 请求已挂起，尝试次数: %d, 健康端点数: %d, 最后失败端点: %s",
			connID, actualAttemptCount, len(currentEndpoints), lastFailedEndpoint))

		// 🚀 [端点自愈] 等待端点恢复，能区分成功/超时/取消
		result := suspensionMgr.WaitForEndpointRecoveryWithResult(ctx, connID, lastFailedEndpoint)
		switch result {
		case SuspensionSuccess:
			slog.Info(fmt.Sprintf("🚀 [挂起恢复] [%s] 端点 %s 已恢复或组切换完成，重新获取端点", connID, lastFailedEndpoint))
			fmt.Fprintf(w, "data: resume: 组切换完成，恢复处理...\n\n")
			flusher.Flush()

			// 重新获取健康端点
			var newEndpoints []*endpoint.Endpoint
			if sh.endpointManager.GetConfig().Strategy.Type == "fastest" && sh.endpointManager.GetConfig().Strategy.FastTestEnabled {
				newEndpoints = sh.endpointManager.GetFastestEndpointsWithRealTimeTest(ctx)
			} else {
				newEndpoints = sh.endpointManager.GetHealthyEndpoints()
			}

			if len(newEndpoints) > 0 {
				// 更新端点列表，重新开始处理
				endpoints = newEndpoints
				slog.Info(fmt.Sprintf("🔄 [重新开始] [%s] 获取到 %d 个新端点，重新开始流式处理", connID, len(newEndpoints)))

				// 🔧 [生命周期修复] 恢复时必须更新生命周期管理器的端点信息
				// 设置第一个新端点的信息到生命周期管理器
				firstEndpoint := newEndpoints[0]
				firstRouteGroup := firstEndpoint.Config.Channel
				if firstRouteGroup == "" {
					firstRouteGroup = firstEndpoint.Config.Name
				}
				lifecycleManager.SetEndpoint(firstEndpoint.Config.Name, firstRouteGroup, firstEndpoint.Config.Channel)

				// 🔥 [递归修复] 2025-12-26: 检查重启次数限制，防止栈溢出
				if lifecycleManager.CanRestart() {
					// 🔥 [重复计费防护] 2025-12-26: 检查是否已发送内容
					if lifecycleManager.HasOutputContent() {
						slog.Warn(fmt.Sprintf("🚫 [重复计费防护] [%s] 暂停恢复成功但已发送内容，禁止重试", connID))
						lifecycleManager.FailRequest("output_sent_no_retry",
							"Cannot retry after sending partial output to client", http.StatusBadGateway)
						// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
						terminateSSEStreamWithError(w, flusher, "已发送部分内容，无法重试")
						return
					}

					restartCount := lifecycleManager.IncrementRestart()
					slog.Info(fmt.Sprintf("🔄 [暂停恢复重启] [%s] 获取新端点成功，重启次数: %d/%d",
						connID, restartCount, lifecycleManager.GetMaxRestarts()))
					sh.executeStreamingWithRetry(ctx, w, r, bodyBytes, lifecycleManager, flusher, triedChannels, triedEndpoints, loopCount)
					return
				} else {
					slog.Warn(fmt.Sprintf("❌ [重启限制] [%s] 暂停恢复成功但已达最大重启次数 (%d)，终止请求",
						connID, lifecycleManager.GetMaxRestarts()))
					// v6.2.7: 使用 422 而非 503，防止 Claude Code 客户端自动重试
					lifecycleManager.FailRequest("max_restarts_exceeded",
						"Exceeded maximum restart attempts after suspension recovery", http.StatusUnprocessableEntity)
					// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
					terminateSSEStreamWithError(w, flusher, "已达最大重启次数，请求终止")
					return
				}
			}
		case SuspensionCancelled:
			// 🎯 [挂起取消区分] 用户在挂起期间取消请求，应该记录为取消而非失败
			slog.Info(fmt.Sprintf("🚫 [挂起期间取消] [%s] 用户在挂起期间取消请求", connID))
			// 🔧 [状态码修复] 设置取消状态码到上下文用于日志记录
			*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
			lifecycleManager.CancelRequest("suspended then cancelled", nil)
			fmt.Fprintf(w, "data: cancelled: 客户端取消请求\n\n")
			flusher.Flush()
			return
		case SuspensionTimeout:
			slog.Warn(fmt.Sprintf("⏰ [挂起超时] [%s] 挂起等待超时", connID))
			// 继续执行下面的失败处理逻辑
		}
	}

	// 🚀 [状态机重构] Phase 4: 最终失败处理
	// v6.2.7: 所有端点都耗尽时，强制使用 422 状态码，防止 Claude Code 客户端自动重试
	// 不再依赖 GetStatusCodeFromError，因为上游可能返回 502/503 等可重试状态码
	// 但在 "endpoint_exhausted" 场景下，重试没有意义（所有端点都已尝试过），应该明确告知客户端停止重试
	statusCode := http.StatusUnprocessableEntity

	// 🔧 [日志状态码] 设置真实错误码到上下文用于日志记录
	*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))

	lifecycleManager.FailRequest("endpoint_exhausted", fmt.Sprintf("服务暂时不可用: 当前渠道所有端点均不可用, 最后错误: %v", lastErr), statusCode)
	// v6.2.6: 使用正确的 SSE 终止序列，防止客户端自动重连
	terminateSSEStreamWithError(w, flusher, fmt.Sprintf("服务暂时不可用: 当前渠道所有端点均不可用, 最后错误: %v", lastErr))
}
