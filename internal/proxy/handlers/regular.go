package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"claude-request-forwarder/config"
	"claude-request-forwarder/internal/endpoint"
	"claude-request-forwarder/internal/tracking"
	"claude-request-forwarder/internal/transport"
)

// RegularHandler 常规请求处理器
// 负责处理所有常规请求，包含错误恢复机制和生命周期管理
type RegularHandler struct {
	config                   *config.Config
	endpointManager          *endpoint.Manager
	forwarder                *Forwarder
	usageTracker             *tracking.UsageTracker
	responseProcessor        ResponseProcessor
	tokenAnalyzer            TokenAnalyzer
	retryHandler             RetryHandler
	errorRecoveryFactory     ErrorRecoveryFactory
	retryManagerFactory      RetryManagerFactory
	suspensionManagerFactory SuspensionManagerFactory
	// 🔧 [修复] 共享SuspensionManager实例，确保全局挂起限制生效
	sharedSuspensionManager SuspensionManager
}

// NewRegularHandler 创建新的RegularHandler实例
func NewRegularHandler(
	cfg *config.Config,
	endpointManager *endpoint.Manager,
	forwarder *Forwarder,
	usageTracker *tracking.UsageTracker,
	responseProcessor ResponseProcessor,
	tokenAnalyzer TokenAnalyzer,
	retryHandler RetryHandler,
	errorRecoveryFactory ErrorRecoveryFactory,
	retryManagerFactory RetryManagerFactory,
	suspensionManagerFactory SuspensionManagerFactory,
	// 🔧 [Critical修复] 直接接受共享的SuspensionManager实例
	sharedSuspensionManager SuspensionManager,
) *RegularHandler {
	return &RegularHandler{
		config:                   cfg,
		endpointManager:          endpointManager,
		forwarder:                forwarder,
		usageTracker:             usageTracker,
		responseProcessor:        responseProcessor,
		tokenAnalyzer:            tokenAnalyzer,
		retryHandler:             retryHandler,
		errorRecoveryFactory:     errorRecoveryFactory,
		retryManagerFactory:      retryManagerFactory,
		suspensionManagerFactory: suspensionManagerFactory,
		// 🔧 [Critical修复] 使用传入的共享SuspensionManager实例
		// 确保常规请求与流式请求共享同一个全局挂起计数器
		sharedSuspensionManager: sharedSuspensionManager,
	}
}

// getDefaultStatusCodeForFinalStatus 根据最终状态获取默认HTTP状态码
func getDefaultStatusCodeForFinalStatus(finalStatus string) int {
	switch finalStatus {
	case "cancelled":
		return 499 // nginx风格的客户端取消码
	case "auth_error":
		return http.StatusUnauthorized
	case "rate_limited":
		return http.StatusTooManyRequests
	case "error":
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

// HandleRegularRequestUnified 统一常规请求处理
// 实现与StreamingHandler相同的重试循环模式，应用所有Critical修复
func (rh *RegularHandler) HandleRegularRequestUnified(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager) {
	connID := lifecycleManager.GetRequestID()

	slog.Info(fmt.Sprintf("🔄 [常规架构] [%s] 使用unified v3架构", connID))

	// 创建管理器 - 修复依赖注入
	retryMgr := rh.retryManagerFactory.NewRetryManager()
	errorRecovery := rh.errorRecoveryFactory.NewErrorRecoveryManager(rh.usageTracker)

	// 方案B：本次请求链路内“渠道间故障转移”不回访已尝试渠道，避免冷却结束后循环
	triedChannels := make(map[string]struct{}, 4)

	// 外层循环处理组切换逻辑
	for {
		// 获取端点列表
		endpoints := retryMgr.GetHealthyEndpoints(ctx)
		if len(endpoints) == 0 {
			// 创建特殊错误，交给错误分类和重试系统处理
			noHealthyErr := fmt.Errorf("no healthy endpoints available")
			errorRecovery := rh.errorRecoveryFactory.NewErrorRecoveryManager(rh.usageTracker)
			errorCtx := errorRecovery.ClassifyError(noHealthyErr, connID, "", "", 0)

			if errorCtx.ErrorType == ErrorTypeNoHealthyEndpoints {
				// 尝试获取所有活跃端点，忽略健康状态
				allActiveEndpoints := rh.endpointManager.GetActiveEndpointsIgnoringHealth()

				if len(allActiveEndpoints) > 0 {
					slog.InfoContext(ctx, fmt.Sprintf("🔄 [健康检查回退] [%s] 忽略健康状态，尝试 %d 个活跃端点",
						connID, len(allActiveEndpoints)))
					endpoints = allActiveEndpoints
					// 继续正常处理流程
				} else {
					// 真的没有端点
					lifecycleManager.HandleError(noHealthyErr)
					// 🔧 [修复] 2025-12-11: 所有端点不可用时必须终结请求
					lifecycleManager.FailRequest("no_endpoints", "No endpoints available in active groups", http.StatusServiceUnavailable)
					http.Error(w, "No endpoints available in active groups", http.StatusServiceUnavailable)
					return
				}
			} else {
				// 按原来逻辑处理
				lifecycleManager.HandleError(noHealthyErr)
				// 🔧 [修复] 2025-12-11: 所有端点不可用时必须终结请求
				lifecycleManager.FailRequest("no_healthy_endpoints", "No healthy endpoints available", http.StatusServiceUnavailable)
				http.Error(w, "No healthy endpoints available", http.StatusServiceUnavailable)
				return
			}
		}

		// 记录当前尝试的渠道（用于方案B：后续跨渠道故障转移时不回访）
		currentChannel := endpoints[0].Config.Channel
		if currentChannel == "" {
			currentChannel = endpoints[0].Config.Name
		}
		if currentChannel != "" {
			triedChannels[currentChannel] = struct{}{}
		}

		// 内层循环处理端点重试
		groupSwitchNeeded := false
		for i, ep := range endpoints {
			routeGroup := ep.Config.Channel
			if routeGroup == "" {
				routeGroup = ep.Config.Name
			}
			lifecycleManager.SetEndpoint(ep.Config.Name, routeGroup, ep.Config.Channel)
			lifecycleManager.UpdateStatus("forwarding", i, 0)

			// 🔧 [端点上下文修复] 立即设置端点信息到请求上下文，确保所有分支（成功/失败/取消）的日志都能正确记录端点
			*r = *r.WithContext(context.WithValue(r.Context(), "selected_endpoint", ep.Config.Name))

		attemptLoop:
			for attempt := 1; attempt <= retryMgr.GetMaxAttempts(); attempt++ {
				// 检查取消
				select {
				case <-ctx.Done():
					lifecycleManager.CancelRequest("client disconnected", nil)

					// 🔧 [HTTP状态码修复] 设置最终状态码到请求上下文，而不是WriteHeader
					statusCode := getDefaultStatusCodeForFinalStatus("cancelled") // 返回499
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
					http.Error(w, "Client closed request", statusCode)
					return
				default:
				}

				// 🔢 [关键修复] 每次尝试开始时增加全局计数 - 确保生命周期和重试策略正确
				globalAttemptCount := lifecycleManager.IncrementAttempt()

				// 执行请求
				resp, err := rh.executeRequest(ctx, r, bodyBytes, ep)

				if err == nil && IsSuccessStatus(resp.StatusCode) {
					// ✅ [重试决策] 成功请求的决策日志 - 保持监控完整性
					slog.Info(fmt.Sprintf("✅ [重试决策] 请求成功完成 request_id=%s endpoint=%s attempt=%d reason=请求成功完成",
						connID, ep.Config.Name, attempt))

					lifecycleManager.UpdateStatus("processing", globalAttemptCount, resp.StatusCode)
					rh.processSuccessResponse(ctx, w, resp, lifecycleManager, ep.Config.Name, r)
					return
				}

				// 构造HTTP状态码错误（保持现有逻辑）
				if err == nil && resp != nil && !IsSuccessStatus(resp.StatusCode) {
					// 先尝试从HTTP错误中提取Token信息（如果可能）
					rh.tryExtractTokensFromHttpError(resp, lifecycleManager, ep.Config.Name)

					closeErr := resp.Body.Close()
					if closeErr != nil {
						slog.Warn(fmt.Sprintf("⚠️ [响应体关闭失败] [%s] 端点: %s, Close错误: %v",
							connID, ep.Config.Name, closeErr))
					}
					err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
				} else if err != nil && resp != nil {
					closeErr := resp.Body.Close()
					if closeErr != nil {
						slog.Warn(fmt.Sprintf("⚠️ [错误响应体关闭失败] [%s] 端点: %s, Close错误: %v",
							connID, ep.Config.Name, closeErr))
					}
				}

				// 🔧 使用增强的RetryManager进行统一决策
				errorCtx := errorRecovery.ClassifyError(err, connID, ep.Config.Name, routeGroup, attempt-1)

				// 🚀 [状态机重构] Phase 4: 分离状态转换与失败原因记录
				// 预设错误上下文（避免重复分类），由HandleError统一记录失败原因
				lifecycleManager.PrepareErrorContext(&errorCtx)
				lifecycleManager.HandleError(err)

				// 🔢 [关键修复] 分离局部和全局计数语义
				// localAttempt: 当前端点内的尝试次数，用于退避计算
				// globalAttemptCount: 全局尝试次数，用于限流策略
				decision := retryMgr.ShouldRetryWithDecision(&errorCtx, attempt, globalAttemptCount, false) // 常规请求: isStreaming=false

				// 处理挂起决策
				if decision.SuspendRequest {
					if rh.sharedSuspensionManager.ShouldSuspend(ctx) {
						// 🚀 [状态机重构] Phase 4: 挂起时更新状态
						lifecycleManager.UpdateStatus("suspended", globalAttemptCount, 0)
						slog.Info(fmt.Sprintf("⏸️ [请求挂起] [%s] 原因: %s，失败端点: %s",
							connID, decision.Reason, ep.Config.Name))

						// 🚀 [端点自愈] 使用新的端点恢复等待方法，能区分成功/超时/取消
						result := rh.sharedSuspensionManager.WaitForEndpointRecoveryWithResult(ctx, connID, ep.Config.Name)
						switch result {
						case SuspensionSuccess:
							slog.Info(fmt.Sprintf("🎯 [恢复成功] [%s] 端点 %s 已恢复或组已切换，重新获取端点列表",
								connID, ep.Config.Name))
							groupSwitchNeeded = true
							break
						case SuspensionCancelled:
							// 🎯 [挂起取消区分] 用户在挂起期间取消请求，应该记录为取消而非失败
							slog.Info(fmt.Sprintf("🚫 [挂起期间取消] [%s] 用户在挂起期间取消请求", connID))
							// 🔧 [状态码修复] 设置取消状态码到上下文用于日志记录
							*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
							lifecycleManager.CancelRequest("suspended then cancelled", nil)
							http.Error(w, "Request cancelled during suspension", 499)
							return
						case SuspensionTimeout:
							// 挂起等待超时，记录为失败
							slog.Warn(fmt.Sprintf("⏰ [挂起超时] [%s] 等待端点恢复或组切换超时", connID))
							lifecycleManager.UpdateStatus("error", globalAttemptCount, http.StatusBadGateway)
							http.Error(w, "Request suspended but recovery timeout", http.StatusBadGateway)
							return
						}

						if groupSwitchNeeded {
							break attemptLoop
						}
					}
				}

				if !decision.RetrySameEndpoint {
					if decision.SwitchEndpoint {
						// v6.1+: 当端点因错误被切走时，给该端点加“请求失败冷却”，避免下一次请求立刻回到该端点导致反复切换。
						nextName := ""
						if i+1 < len(endpoints) && endpoints[i+1] != nil {
							nextName = endpoints[i+1].Config.Name
						}

						failedKey := endpoint.EndpointKey(ep.Config.Channel, ep.Config.Name)
						cooldownUntil := ""
						if failedKey != "" {
							until, cdErr := rh.endpointManager.SetEndpointCooldown(failedKey, decision.Reason)
							if cdErr != nil {
								slog.Warn(fmt.Sprintf("⚠️ [端点冷却] [%s] 设置冷却失败 endpoint=%s key=%s err=%v",
									connID, ep.Config.Name, failedKey, cdErr))
							} else {
								cooldownUntil = until.Format("15:04:05")
							}
						}
						if cooldownUntil != "" {
							slog.Warn(fmt.Sprintf("🔁 [渠道内故障转移] [%s] channel=%s %s -> %s cooldown_until=%s reason=%s",
								connID, routeGroup, ep.Config.Name, nextName, cooldownUntil, decision.Reason))
						} else {
							slog.Warn(fmt.Sprintf("🔁 [渠道内故障转移] [%s] channel=%s %s -> %s reason=%s",
								connID, routeGroup, ep.Config.Name, nextName, decision.Reason))
						}
						break // 尝试下一个端点
					} else {
						// 🚀 [状态机重构] Phase 4: 最终失败处理
						// 获取失败原因
						failureReason := lifecycleManager.MapErrorTypeToFailureReason(errorCtx.ErrorType)

						// 获取真实状态码，避免http.Error panic
						statusCode := GetStatusCodeFromError(err, resp)

						// 避免statusCode=0导致http.Error panic
						if statusCode == 0 {
							statusCode = getDefaultStatusCodeForFinalStatus(decision.FinalStatus)
						}

						// 使用新的FailRequest方法标记最终失败（修复：添加HTTP状态码）
						lifecycleManager.FailRequest(failureReason, err.Error(), statusCode)
						http.Error(w, decision.Reason, statusCode)
						return
					}
				}

				// 🚀 [状态机重构] Phase 4: 重试状态管理
				if decision.RetrySameEndpoint && attempt < retryMgr.GetMaxAttempts() {
					// 更新为重试状态
					lifecycleManager.UpdateStatus("retry", globalAttemptCount, 0)

					// 使用统一延迟
					if decision.Delay > 0 {
						time.Sleep(decision.Delay)
					}
				}
			}

			// 如果需要组切换，跳出端点循环
			if groupSwitchNeeded {
				break
			}
		}

		// 如果需要组切换，重新开始外层循环
		if groupSwitchNeeded {
			continue
		}

		// 所有端点都失败了，尝试触发请求级别故障转移
		if len(endpoints) > 0 {
			lastEndpoint := endpoints[len(endpoints)-1]

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
			newChannel, err := rh.endpointManager.TriggerRequestFailoverWithFailedEndpointsExcludingChannels(
				failedEndpointNames,
				excludeChannels,
				"all_retries_exhausted",
			)

			if err == nil && newChannel != "" {
				failedChannel := currentChannel
				slog.Warn(fmt.Sprintf("🔄 [渠道间故障转移] [%s] from_channel=%s to_channel=%s last_endpoint=%s failed_endpoints=%d reason=%s",
					connID, failedChannel, newChannel, lastEndpoint.Config.Name, len(failedEndpointNames), "all_retries_exhausted"))
				// 故障转移成功，重新获取端点列表继续处理
				groupSwitchNeeded = true
				continue
			} else if err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [渠道间故障转移失败] [%s] last_endpoint=%s err=%v",
					connID, lastEndpoint.Config.Name, err))
			}
		}

		// 故障转移失败或无可用端点，终止处理
		break
	}

	// 🔧 [统一挂起策略] 所有端点都失败后，检查是否应该挂起请求（与流式模式保持一致）
	// 注意：客户端取消错误已在上面统一处理，这里不会执行到

	// 🔧 [修复] 使用共享的SuspensionManager实例，确保全局挂起限制生效
	suspensionMgr := rh.sharedSuspensionManager

	// 检查是否应该挂起请求
	if suspensionMgr.ShouldSuspend(ctx) {
		currentEndpoints := rh.endpointManager.GetHealthyEndpoints()
		if cfg := rh.endpointManager.GetConfig(); cfg != nil && cfg.Strategy.Type == "fastest" && cfg.Strategy.FastTestEnabled {
			currentEndpoints = rh.endpointManager.GetFastestEndpointsWithRealTimeTest(ctx)
		}

		// 🚀 [状态机重构] Phase 4: 挂起时更新状态（移除重复的失败原因记录）
		lifecycleManager.UpdateStatus("suspended", -1, 0)

		// 🔢 [语义修复] 在日志中记录端点数量信息，但不影响重试计数语义
		actualAttemptCount := lifecycleManager.GetAttemptCount()
		slog.Info(fmt.Sprintf("⏸️ [常规挂起] [%s] 请求已挂起，尝试次数: %d, 健康端点数: %d, 当前所有组均不可用",
			connID, actualAttemptCount, len(currentEndpoints)))

		// 🚀 [端点自愈] 等待端点恢复，能区分成功/超时/取消
		// 注意：常规模式没有lastFailedEndpoint概念，传空字符串
		result := suspensionMgr.WaitForEndpointRecoveryWithResult(ctx, connID, "")
		switch result {
		case SuspensionSuccess:
			slog.Info(fmt.Sprintf("🚀 [挂起恢复] [%s] 端点已恢复或组切换完成，重新获取端点", connID))

			// 重新获取健康端点
			var newEndpoints []*endpoint.Endpoint
			if rh.endpointManager.GetConfig().Strategy.Type == "fastest" && rh.endpointManager.GetConfig().Strategy.FastTestEnabled {
				newEndpoints = rh.endpointManager.GetFastestEndpointsWithRealTimeTest(ctx)
			} else {
				newEndpoints = rh.endpointManager.GetHealthyEndpoints()
			}

			if len(newEndpoints) > 0 {
				slog.Info(fmt.Sprintf("🔄 [重新开始] [%s] 获取到 %d 个新端点，重新开始常规处理", connID, len(newEndpoints)))

				// 🔧 [生命周期修复] 恢复时必须更新生命周期管理器的端点信息
				// 设置第一个新端点的信息到生命周期管理器
				firstEndpoint := newEndpoints[0]
				firstRouteGroup := firstEndpoint.Config.Channel
				if firstRouteGroup == "" {
					firstRouteGroup = firstEndpoint.Config.Name
				}
				lifecycleManager.SetEndpoint(firstEndpoint.Config.Name, firstRouteGroup, firstEndpoint.Config.Channel)

				// 重新获取健康端点并重新尝试（递归调用）
				rh.HandleRegularRequestUnified(ctx, w, r, bodyBytes, lifecycleManager)
				return
			}
		case SuspensionCancelled:
			// 🎯 [挂起取消区分] 用户在挂起期间取消请求，应该记录为取消而非失败
			slog.Info(fmt.Sprintf("🚫 [挂起期间取消] [%s] 用户在挂起期间取消请求", connID))
			// 🔧 [状态码修复] 设置取消状态码到上下文用于日志记录
			*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
			lifecycleManager.CancelRequest("suspended then cancelled", nil)
			http.Error(w, "Request cancelled during suspension", 499)
			return
		case SuspensionTimeout:
			slog.Warn(fmt.Sprintf("⏰ [挂起超时] [%s] 挂起等待超时", connID))
			// 继续执行下面的失败处理逻辑
		}
	}

	// 🚀 [状态机重构] Phase 4: 最终失败处理
	// 所有端点都失败了，使用FailRequest方法标记最终失败（修复：添加HTTP状态码）
	lifecycleManager.FailRequest("endpoint_exhausted", "All endpoints failed", http.StatusBadGateway)
	http.Error(w, "All endpoints failed", http.StatusBadGateway)
}

// executeRequest 执行单个请求
func (rh *RegularHandler) executeRequest(ctx context.Context, r *http.Request, bodyBytes []byte, endpoint *endpoint.Endpoint) (*http.Response, error) {
	// 创建目标请求
	targetURL := endpoint.Config.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 复制和修改头部
	rh.forwarder.CopyHeaders(r, req, endpoint)

	// 创建HTTP传输
	httpTransport, err := transport.CreateTransport(rh.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	client := &http.Client{
		Timeout:   endpoint.Config.Timeout,
		Transport: httpTransport,
	}

	// 执行请求
	return client.Do(req)
}

// processSuccessResponse 处理成功响应
func (rh *RegularHandler) processSuccessResponse(ctx context.Context, w http.ResponseWriter, resp *http.Response, lifecycleManager RequestLifecycleManager, endpointName string, r *http.Request) {
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Failed to close response body", "request_id", lifecycleManager.GetRequestID(), "error", err)
		}
	}()

	// 复制响应头（排除Content-Encoding用于gzip处理）
	rh.responseProcessor.CopyResponseHeaders(resp, w)

	// 写入状态码
	w.WriteHeader(resp.StatusCode)

	// 读取并处理响应体
	responseBytes, err := rh.responseProcessor.ProcessResponseBody(resp)
	if err != nil {
		connID := lifecycleManager.GetRequestID()
		lifecycleManager.HandleError(fmt.Errorf("failed to process response: %w", err))
		slog.Error("Failed to process response body", "request_id", connID, "error", err)
		// 🔧 [修复] 2025-12-11: 响应体读取失败时必须终结请求，否则会滞留在内存热池
		lifecycleManager.FailRequest("response_read_error", err.Error(), resp.StatusCode)
		return
	}

	// 写入响应体到客户端
	if _, err := w.Write(responseBytes); err != nil {
		connID := lifecycleManager.GetRequestID()
		lifecycleManager.HandleError(fmt.Errorf("failed to write response: %w", err))
		slog.Error("Failed to write response to client", "request_id", connID, "error", err)
		// 🔧 [修复] 2025-12-11: 写入失败时必须终结请求，否则会滞留在内存热池
		lifecycleManager.FailRequest("response_write_error", err.Error(), resp.StatusCode)
		return
	}

	// ✅ 同步Token解析：简化逻辑，避免协程控制问题
	connID := lifecycleManager.GetRequestID()
	slog.Debug(fmt.Sprintf("🔄 [Token解析] [%s] 开始Token解析", connID))

	// 🔍 [路径过滤] 跳过count_tokens端点的Token解析
	if r.URL.Path == "/v1/messages/count_tokens" {
		slog.Debug(fmt.Sprintf("🔍 [路径过滤] [%s] 跳过count_tokens端点的Token解析", connID))
		// count_tokens端点不需要Token解析，直接完成请求
		lifecycleManager.CompleteRequest(nil)
		return
	}

	// 对于常规请求，同步解析Token信息（如果存在）
	tokenUsage, modelName := rh.tokenAnalyzer.AnalyzeResponseForTokensUnified(responseBytes, connID, endpointName)

	// 使用生命周期管理器完成请求
	if tokenUsage != nil {
		// 设置模型名称并完成请求
		// 使用对比方法，检测并警告模型不一致情况
		if modelName != "unknown" && modelName != "" {
			lifecycleManager.SetModelWithComparison(modelName, "常规响应解析")
		}
		lifecycleManager.CompleteRequest(tokenUsage)
		slog.Info(fmt.Sprintf("✅ [常规请求Token完成] [%s] 端点: %s, 模型: %s, 输入: %d, 输出: %d",
			connID, endpointName, modelName, tokenUsage.InputTokens, tokenUsage.OutputTokens))
	} else {
		// 处理非Token响应
		lifecycleManager.HandleNonTokenResponse(string(responseBytes))
		slog.Info(fmt.Sprintf("✅ [常规请求完成] [%s] 端点: %s, 响应类型: %s",
			connID, endpointName, modelName))
	}
}

// HandleRegularRequest handles non-streaming requests
func (rh *RegularHandler) HandleRegularRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte) {
	var selectedEndpointName string

	// Get connection ID from request context (set by logging middleware)
	connID := ""
	if connIDValue, ok := r.Context().Value("conn_id").(string); ok {
		connID = connIDValue
	}

	// TODO: 创建重试处理器

	operation := func(ep *endpoint.Endpoint, connectionID string) (*http.Response, error) {
		// Store the selected endpoint name for logging
		selectedEndpointName = ep.Config.Name

		// 🔧 [端点上下文修复] 立即设置端点信息到请求上下文，确保所有分支的日志都能正确记录端点
		*r = *r.WithContext(context.WithValue(r.Context(), "selected_endpoint", ep.Config.Name))

		// TODO: Update connection endpoint in monitoring (if we have a monitoring middleware)

		// Create request to target endpoint
		targetURL := ep.Config.URL + r.URL.Path
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		req, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Copy headers from original request
		rh.forwarder.CopyHeaders(r, req, ep)

		// Create HTTP client with timeout and proxy support
		httpTransport, err := transport.CreateTransport(rh.config)
		if err != nil {
			return nil, fmt.Errorf("failed to create transport: %w", err)
		}

		client := &http.Client{
			Timeout:   ep.Config.Timeout,
			Transport: httpTransport,
		}

		// Make the request
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		// Return the response - retry logic will check status code
		return resp, nil
	}

	// Execute with retry logic - 使用retryHandler
	finalResp, lastErr := rh.retryHandler.ExecuteWithContext(ctx, operation, connID)

	if lastErr != nil {
		// Check if the error is due to no healthy endpoints
		if strings.Contains(lastErr.Error(), "no healthy endpoints") {
			http.Error(w, "Service Unavailable: No healthy endpoints available", http.StatusServiceUnavailable)
		} else {
			// If all retries failed, return error
			http.Error(w, "All endpoints failed: "+lastErr.Error(), http.StatusBadGateway)
		}
		return
	}

	if finalResp == nil {
		http.Error(w, "No response received from any endpoint", http.StatusBadGateway)
		return
	}

	defer finalResp.Body.Close()

	// Copy response headers (except Content-Encoding for gzip handling)
	for key, values := range finalResp.Header {
		// Skip Content-Encoding header as we handle gzip decompression ourselves
		if strings.ToLower(key) == "content-encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(finalResp.StatusCode)

	// Read and decompress response body if needed
	slog.DebugContext(ctx, fmt.Sprintf("🔄 [开始读取响应] [%s] 端点: %s, Content-Encoding: %s",
		connID, selectedEndpointName, finalResp.Header.Get("Content-Encoding")))

	// 使用响应处理器读取响应
	bodyBytes, err := io.ReadAll(finalResp.Body)
	if err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("❌ [响应读取失败] [%s] 端点: %s, 错误: %v", connID, selectedEndpointName, err))
		http.Error(w, "Failed to read response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.DebugContext(ctx, fmt.Sprintf("✅ [响应读取成功] [%s] 端点: %s, 长度: %d字节",
		connID, selectedEndpointName, len(bodyBytes)))

	bodyContent := string(bodyBytes)
	slog.DebugContext(ctx, fmt.Sprintf("🐛 [调试响应头] 端点: %s, 响应头: %v", selectedEndpointName, finalResp.Header))

	// Pass the complete response content to logger - let the logger decide how to handle truncation
	slog.DebugContext(ctx, fmt.Sprintf("🐛 [调试响应] 端点: %s, 状态码: %d, 长度: %d字节, 响应内容: %s",
		selectedEndpointName, finalResp.StatusCode, len(bodyContent), bodyContent))

	// TODO: Analyze the complete response for token usage
	slog.DebugContext(ctx, fmt.Sprintf("🔍 [开始Token分析] [%s] 端点: %s", connID, selectedEndpointName))
	slog.DebugContext(ctx, fmt.Sprintf("✅ [Token分析完成] [%s] 端点: %s", connID, selectedEndpointName))

	// Write the body to client
	_, writeErr := w.Write(bodyBytes)
	if writeErr != nil {
		// Log error but don't return error response as headers are already sent
		slog.Error("Failed to write response to client", "request_id", connID, "error", writeErr)
	}
}

// tryExtractTokensFromHttpError 尝试从HTTP错误响应中提取Token信息
// 注意：此方法必须在响应体关闭前调用
func (rh *RegularHandler) tryExtractTokensFromHttpError(resp *http.Response, lifecycleManager RequestLifecycleManager, endpointName string) {
	if resp == nil || resp.Body == nil {
		return
	}

	// ✅ 只对可能包含Token信息的错误码进行解析
	if resp.StatusCode != 429 && resp.StatusCode != 413 && resp.StatusCode < 500 {
		return
	}

	// ✅ 同步解析，确保在响应体关闭前完成
	defer func() {
		if r := recover(); r != nil {
			slog.Warn(fmt.Sprintf("⚠️ [错误响应解析恢复] 解析过程中出现异常: %v", r))
		}
	}()

	responseBytes, err := rh.responseProcessor.ProcessResponseBody(resp)
	if err != nil || len(responseBytes) == 0 {
		return
	}

	tokenUsage, modelName := rh.tokenAnalyzer.AnalyzeResponseForTokensUnified(responseBytes, lifecycleManager.GetRequestID(), endpointName)
	if tokenUsage != nil {
		// ✅ 修复：将解析到的模型信息设置到生命周期管理器
		if modelName != "" && modelName != "unknown" {
			lifecycleManager.SetModel(modelName)
		}

		lifecycleManager.RecordTokensForFailedRequest(tokenUsage, fmt.Sprintf("http_%d", resp.StatusCode))
		slog.Info(fmt.Sprintf("💾 [HTTP错误Token记录] [%s] 端点: %s, 状态码: %d, 模型: %s",
			lifecycleManager.GetRequestID(), endpointName, resp.StatusCode, modelName))
	}
}
