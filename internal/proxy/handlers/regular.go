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
	// 使用默认参数调用完整版本
	rh.HandleRegularRequestUnifiedWithRetry(ctx, w, r, bodyBytes, lifecycleManager, nil, nil, 0)
}

// HandleRegularRequestUnifiedWithRetry 带重试状态的统一常规请求处理
// triedChannels: 已尝试的渠道（用于跨渠道故障转移时避免回访）
// triedEndpoints: 已尝试的端点（用于渠道内故障转移时避免回访）
// loopCount: 当前循环重试轮数（用于限制最大循环次数）
func (rh *RegularHandler) HandleRegularRequestUnifiedWithRetry(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, lifecycleManager RequestLifecycleManager, triedChannels map[string]struct{}, triedEndpoints map[string]struct{}, loopCount int) {
	connID := lifecycleManager.GetRequestID()

	slog.Info(fmt.Sprintf("🔄 [常规架构] [%s] 使用unified v3架构", connID))

	// 创建管理器 - 修复依赖注入
	retryMgr := rh.retryManagerFactory.NewRetryManager()
	errorRecovery := rh.errorRecoveryFactory.NewErrorRecoveryManager(rh.usageTracker)

	// 方案B：本次请求链路内"渠道间故障转移"不回访已尝试渠道，避免冷却结束后循环
	// 支持从外部传入已尝试状态（递归调用时保持状态）
	if triedChannels == nil {
		triedChannels = make(map[string]struct{}, 4)
	}
	if triedEndpoints == nil {
		triedEndpoints = make(map[string]struct{}, 8)
	}

	// 外层循环处理组切换逻辑
	for {
		// 获取端点时排除已尝试的端点
		var endpoints []*endpoint.Endpoint
		if len(triedEndpoints) == 0 {
			// 首次获取，不排除任何端点
			endpoints = filterStreamOnlyEndpoints(retryMgr.GetHealthyEndpoints(ctx))
		} else {
			// 后续获取，排除已尝试的端点
			excludeKeys := make([]string, 0, len(triedEndpoints))
			for key := range triedEndpoints {
				excludeKeys = append(excludeKeys, key)
			}
			endpoints = filterStreamOnlyEndpoints(retryMgr.GetHealthyEndpointsExcluding(ctx, excludeKeys))
		}
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
					endpoints = filterStreamOnlyEndpoints(allActiveEndpoints)
					// 继续正常处理流程
				} else {
					// 真的没有端点，直接失败（不挂起）
					lifecycleManager.HandleError(noHealthyErr)
					lifecycleManager.FailRequest("no_endpoints", "No endpoints available in active groups", http.StatusUnprocessableEntity)
					http.Error(w, "No endpoints available in active groups", http.StatusUnprocessableEntity)
					return
				}
			} else {
				// 按原来逻辑处理，直接失败（不挂起）
				lifecycleManager.HandleError(noHealthyErr)
				lifecycleManager.FailRequest("no_healthy_endpoints", "No healthy endpoints available", http.StatusUnprocessableEntity)
				http.Error(w, "No healthy endpoints available", http.StatusUnprocessableEntity)
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
			// 记录已尝试的端点（确保严格优先级转移，不回退）
			epKey := endpoint.EndpointKey(ep.Config.Channel, ep.Config.Name)
			triedEndpoints[epKey] = struct{}{}

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
				if err != nil && ctx.Err() != nil {
					lifecycleManager.CancelRequest("client disconnected", nil)

					// 🔧 [HTTP状态码修复] 设置最终状态码到请求上下文，而不是WriteHeader
					statusCode := getDefaultStatusCodeForFinalStatus("cancelled") // 返回499
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
					http.Error(w, "Client closed request", statusCode)
					return
				}

				if err == nil && IsSuccessStatus(resp.StatusCode) {
					// ✅ [重试决策] 成功请求的决策日志 - 保持监控完整性
					slog.Info(fmt.Sprintf("✅ [重试决策] 请求成功完成 request_id=%s endpoint=%s attempt=%d reason=请求成功完成",
						connID, ep.Config.Name, attempt))

					lifecycleManager.UpdateStatus("processing", globalAttemptCount, resp.StatusCode)
					rh.processSuccessResponse(ctx, w, resp, lifecycleManager, ep.Config.Name, r)
					return
				}

				// 构造HTTP状态码错误：保留提供商原始响应体用于透传
				if err == nil && resp != nil && !IsSuccessStatus(resp.StatusCode) {
					// 读取原始响应体（最多 64KB），用于 token 提取和最终透传
					respBodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
					contentType := resp.Header.Get("Content-Type")
					resp.Body.Close()

					// 用已读取的字节替换 Body，让 tryExtractTokensFromHttpError 仍能解析
					resp.Body = io.NopCloser(bytes.NewReader(respBodyBytes))
					rh.tryExtractTokensFromHttpError(resp, lifecycleManager, ep.Config.Name)

					targetURL := ep.Config.URL + r.URL.Path
					err = &EndpointError{
						StatusCode:  resp.StatusCode,
						Body:        respBodyBytes,
						ContentType: contentType,
						URL:         targetURL,
					}
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
				if decision.FinalStatus == "cancelled" {
					lifecycleManager.CancelRequest("client disconnected", nil)
					statusCode := getDefaultStatusCodeForFinalStatus("cancelled")
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
					http.Error(w, "Client closed request", statusCode)
					return
				}

				// 处理挂起决策
				if decision.SuspendRequest {
					if rh.sharedSuspensionManager.ShouldSuspend(ctx) {
						// 🚀 [状态机重构] Phase 4: 挂起时更新状态
						lifecycleManager.UpdateStatus("suspended", globalAttemptCount, 0)
						slog.Info(fmt.Sprintf("⏸️ [请求挂起] [%s] 原因: %s，失败端点: %s",
							connID, decision.Reason, ep.Config.Name))

						// 🚀 [端点自愈] 使用新的端点恢复等待方法，能区分成功/超时/取消
						failedEndpointKey := endpoint.EndpointKey(ep.Config.Channel, ep.Config.Name)
						result := rh.sharedSuspensionManager.WaitForEndpointRecoveryWithResult(ctx, connID, failedEndpointKey)
						switch result {
						case SuspensionSuccess:
							slog.Info(fmt.Sprintf("🎯 [恢复成功] [%s] 端点 %s 已恢复或组已切换，重新获取端点列表",
								connID, ep.Config.Name))
							groupSwitchNeeded = true
						case SuspensionCancelled:
							// 🎯 [挂起取消区分] 用户在挂起期间取消请求，应该记录为取消而非失败
							slog.Info(fmt.Sprintf("🚫 [挂起期间取消] [%s] 用户在挂起期间取消请求", connID))
							// 🔧 [状态码修复] 设置取消状态码到上下文用于日志记录
							*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", 499))
							lifecycleManager.CancelRequest("suspended then cancelled", nil)
							http.Error(w, "Request cancelled during suspension", 499)
							return
						case SuspensionTimeout:
							// 使用 422 状态码，防止客户端自动重试
							// 挂起等待超时，记录为失败
							slog.Warn(fmt.Sprintf("⏰ [挂起超时] [%s] 等待端点恢复或组切换超时", connID))
							lifecycleManager.UpdateStatus("error", globalAttemptCount, http.StatusUnprocessableEntity)
							http.Error(w, "Request suspended but recovery timeout", http.StatusUnprocessableEntity)
							return
						}

						if groupSwitchNeeded {
							break attemptLoop
						}
					}
				}

				if !decision.RetrySameEndpoint {
					// 获取失败状态码并设置到端点，用于 UI 显示
					failStatusCode := GetStatusCodeFromError(err, resp)
					if failStatusCode == 0 {
						failStatusCode = getDefaultStatusCodeForFinalStatus(decision.FinalStatus)
					}
					// 设置端点的失败代码和原因，用于 UI 显示
					failReason := decision.Reason
					if err != nil && failReason == "" {
						failReason = err.Error()
					}
					rh.endpointManager.SetEndpointLastRequestFail(ep.Config.Name, failStatusCode, failReason)

					if decision.SwitchEndpoint {
						// 渠道内端点切换时标记当前端点为请求失败，防止后续重新选择到此端点
						ep.MarkRequestFailed(err)
						// 通知前端状态变化（使用防抖机制避免频繁刷新）
						rh.endpointManager.NotifyEndpointStatusChange()

						// 移除渠道内端点冷却，使用 triedEndpoints 追踪
						nextName := ""
						if i+1 < len(endpoints) && endpoints[i+1] != nil {
							nextName = endpoints[i+1].Config.Name
						}
						slog.Info(fmt.Sprintf("🔀 [切换端点] [%s] channel=%s %s -> %s reason=%s",
							connID, routeGroup, ep.Config.Name, nextName, decision.Reason))
						break // 尝试下一个端点
					} else {
						// 🚀 [状态机重构] Phase 4: 最终失败处理
						// 获取失败原因
						failureReason := lifecycleManager.MapErrorTypeToFailureReason(errorCtx.ErrorType)

						// 使用新的FailRequest方法标记最终失败（修复：添加HTTP状态码）
						lifecycleManager.FailRequest(failureReason, err.Error(), failStatusCode)
						WriteErrorResponse(w, err, decision.Reason, failStatusCode)
						return
					}
				}

				// 🚀 [状态机重构] Phase 4: 重试状态管理
				if decision.RetrySameEndpoint && attempt < retryMgr.GetMaxAttempts() {
					// 更新为重试状态
					lifecycleManager.UpdateStatus("retry", globalAttemptCount, 0)

					// 使用统一延迟
					if decision.Delay > 0 {
						select {
						case <-ctx.Done():
							lifecycleManager.CancelRequest("client disconnected during retry delay", nil)
							statusCode := getDefaultStatusCodeForFinalStatus("cancelled")
							*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
							http.Error(w, "Client closed request", statusCode)
							return
						case <-time.After(decision.Delay):
						}
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

		// [auto_wait策略特殊处理] 在跨渠道故障转移前，检查渠道内是否有已恢复的端点
		// 仅当策略为 auto_wait 时才执行此逻辑
		if len(endpoints) > 0 && retryMgr.IsAutoWaitStrategy() {
			// 获取已尝试端点的 key 列表
			triedKeys := make([]string, 0, len(triedEndpoints))
			for key := range triedEndpoints {
				triedKeys = append(triedKeys, key)
			}

			// 检查是否有已恢复的端点
			recoveredEndpoints := retryMgr.GetRecoveredEndpointsInChannel(triedKeys)
			if len(recoveredEndpoints) > 0 {
				slog.Info(fmt.Sprintf("🔄 [auto_wait恢复重试] [%s] 发现 %d 个已恢复端点，按优先级顺序重试",
					connID, len(recoveredEndpoints)))

				// 对已恢复的端点进行一轮重试（每个端点只尝试一次）
				for _, ep := range recoveredEndpoints {
					// 检查取消
					select {
					case <-ctx.Done():
						lifecycleManager.CancelRequest("client disconnected", nil)
						statusCode := getDefaultStatusCodeForFinalStatus("cancelled")
						*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
						http.Error(w, "Client closed request", statusCode)
						return
					default:
					}

					epKey := endpoint.EndpointKey(ep.Config.Channel, ep.Config.Name)
					routeGroup := ep.Config.Channel
					if routeGroup == "" {
						routeGroup = ep.Config.Name
					}
					lifecycleManager.SetEndpoint(ep.Config.Name, routeGroup, ep.Config.Channel)
					*r = *r.WithContext(context.WithValue(r.Context(), "selected_endpoint", ep.Config.Name))

					globalAttemptCount := lifecycleManager.IncrementAttempt()
					slog.Info(fmt.Sprintf("🔄 [auto_wait恢复重试] [%s] 尝试已恢复端点: %s (优先级: %d)",
						connID, ep.Config.Name, ep.Config.Priority))

					// 执行请求
					resp, err := rh.executeRequest(ctx, r, bodyBytes, ep)
					if err != nil && ctx.Err() != nil {
						lifecycleManager.CancelRequest("client disconnected during recovery retry", nil)
						statusCode := getDefaultStatusCodeForFinalStatus("cancelled")
						*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
						http.Error(w, "Client closed request", statusCode)
						return
					}

					if err == nil && IsSuccessStatus(resp.StatusCode) {
						slog.Info(fmt.Sprintf("✅ [auto_wait恢复重试成功] [%s] 端点 %s 重试成功",
							connID, ep.Config.Name))
						lifecycleManager.UpdateStatus("processing", globalAttemptCount, resp.StatusCode)
						rh.processSuccessResponse(ctx, w, resp, lifecycleManager, ep.Config.Name, r)
						return
					}

					// 重试失败，记录日志并继续下一个
					if resp != nil {
						resp.Body.Close()
					}
					slog.Warn(fmt.Sprintf("⚠️ [auto_wait恢复重试失败] [%s] 端点 %s 重试失败: %v",
						connID, epKey, err))
				}

				slog.Info(fmt.Sprintf("⚠️ [auto_wait恢复重试结束] [%s] 所有恢复端点重试完毕，继续跨渠道故障转移",
					connID))
			}
		}

		// 所有端点都失败了，尝试触发请求级别故障转移
		if len(endpoints) > 0 {
			lastEndpoint := endpoints[len(endpoints)-1]

			// 跨渠道切换时，将本次请求中失败过的端点统一进入冷却，避免下一次请求立即重复撞同一批端点
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

			// 使用带循环计数的故障转移方法
			newChannel, err := rh.endpointManager.TriggerRequestFailoverWithLoopCount(
				failedEndpointNames,
				excludeChannels,
				"all_retries_exhausted",
				loopCount,
			)

			if err == nil && newChannel != "" {
				failedChannel := currentChannel

				// 检测是否开始了新一轮循环
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
					connID, failedChannel, newChannel, lastEndpoint.Config.Name, len(failedEndpointNames), "all_retries_exhausted", newLoopCount))

				// 更新 loopCount 供下一轮使用
				loopCount = newLoopCount

				// 故障转移成功，重新获取端点列表继续处理
				groupSwitchNeeded = true
				continue
			} else if err != nil {
				// 故障转移失败时，根据恢复策略决定是否立即返回错误
				slog.Warn(fmt.Sprintf("⚠️ [渠道间故障转移失败] [%s] last_endpoint=%s err=%v loop=%d",
					connID, lastEndpoint.Config.Name, err, loopCount))

				// 检查是否是"没有可用渠道"或"达到最大循环轮数"的错误
				// 这些情况下不应该继续挂起等待，应该直接返回错误
				errStr := err.Error()
				isNoChannelsError := strings.Contains(errStr, "没有可用的故障转移渠道") ||
					strings.Contains(errStr, "所有渠道均已尝试")
				isLoopLimitError := strings.Contains(errStr, "共循环") ||
					strings.Contains(errStr, "最大循环轮数")

				if isNoChannelsError {
					// 传递 nil 而不是 err，保留端点在 SetEndpointLastRequestFail 中设置的真实失败原因
					// 避免用故障转移函数返回的错误（如"故障转移未启用"）覆盖端点实际失败原因
					rh.endpointManager.MarkEndpointsRequestFailed(failedEndpointNames, nil)
					statusCode := http.StatusUnprocessableEntity
					failureReason := "all_channels_unavailable"
					if isLoopLimitError {
						failureReason = "max_loops_exceeded"
					}
					*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
					lifecycleManager.FailRequest(failureReason, errStr, statusCode)
					slog.Warn(fmt.Sprintf("🛑 [故障转移终止] [%s] %s (立即返回错误)", connID, errStr))
					http.Error(w, errStr, statusCode)
					return
				}
			}
		}

		// 故障转移失败或无可用端点，终止处理
		break
	}

	// 🚀 [状态机重构] Phase 4: 最终失败处理（不挂起）
	errMsg := "服务暂时不可用: 当前渠道所有端点均不可用"
	statusCode := http.StatusUnprocessableEntity
	*r = *r.WithContext(context.WithValue(r.Context(), "final_status_code", statusCode))
	lifecycleManager.FailRequest("endpoint_exhausted", errMsg, statusCode)
	slog.Warn(fmt.Sprintf("⚠️ [当前渠道端点耗尽] [%s] %s (立即返回错误)", connID, errMsg))
	http.Error(w, errMsg, statusCode)
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

	client, err := rh.forwarder.NewClient(endpoint.Config.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// 执行请求
	return client.Do(req)
}

// processSuccessResponse 处理成功响应
func (rh *RegularHandler) processSuccessResponse(_ context.Context, w http.ResponseWriter, resp *http.Response, lifecycleManager RequestLifecycleManager, endpointName string, r *http.Request) {
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

		client, err := rh.forwarder.NewClient(ep.Config.Timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create transport: %w", err)
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
		// 使用 422 状态码，防止客户端自动重试
		// Check if the error is due to no healthy endpoints
		if strings.Contains(lastErr.Error(), "no healthy endpoints") {
			http.Error(w, "Service Unavailable: No healthy endpoints available", http.StatusUnprocessableEntity)
		} else {
			// If all retries failed, return error
			http.Error(w, "All endpoints failed: "+lastErr.Error(), http.StatusUnprocessableEntity)
		}
		return
	}

	if finalResp == nil {
		http.Error(w, "No response received from any endpoint", http.StatusUnprocessableEntity)
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

// UpdateConfig 更新常规处理器配置
// 支持运行时配置热更新，确保设置变更能立即生效
func (rh *RegularHandler) UpdateConfig(cfg *config.Config) {
	rh.config = cfg
}

// filterStreamOnlyEndpoints 从端点列表中移除仅支持流式的端点。
// 常规（非流式）请求不应路由到 stream_only=true 的端点，
// 以避免提供商因请求类型不匹配而拒绝服务。
func filterStreamOnlyEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	filtered := endpoints[:0:0] // 复用底层数组，避免额外分配
	for _, ep := range endpoints {
		if !ep.Config.StreamOnly {
			filtered = append(filtered, ep)
		}
	}
	return filtered
}
