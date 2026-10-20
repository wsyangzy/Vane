package proxy

import (
	"errors"
	"fmt"

	"github.com/wsyangzy/Vane/internal/platform/monitor"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/handlers"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// PrepareErrorContext 预先注入错误上下文，在下次 HandleError 时复用
// 仅针对同一个错误对象有效，避免重复分类与重复日志
func (rlm *RequestLifecycleManager) PrepareErrorContext(errorCtx *handlers.ErrorContext) {
	rlm.counterMu.Lock()
	defer rlm.counterMu.Unlock()

	if errorCtx == nil {
		rlm.pendingErrorContext = nil
		rlm.pendingErrorOriginal = nil
		return
	}

	rlm.pendingErrorContext = errorCtx
	rlm.pendingErrorOriginal = errorCtx.OriginalError
}

// consumePreparedErrorContext 尝试取出与指定错误匹配的预计算上下文
func (rlm *RequestLifecycleManager) consumePreparedErrorContext(err error) *ErrorContext {
	rlm.counterMu.Lock()
	defer rlm.counterMu.Unlock()

	if rlm.pendingErrorContext == nil || err == nil {
		return nil
	}

	// 只有当错误对象匹配时才复用，确保不跨错误复用
	if rlm.pendingErrorOriginal != nil {
		if errors.Is(err, rlm.pendingErrorOriginal) {
			ctx := rlm.pendingErrorContext
			rlm.pendingErrorContext = nil
			rlm.pendingErrorOriginal = nil
			return ctx
		}
	}

	// 不匹配则丢弃预计算结果，避免影响后续错误
	rlm.pendingErrorContext = nil
	rlm.pendingErrorOriginal = nil
	return nil
}

// HandleError 处理请求过程中的错误
// 🎯 [SRP重构] 使用 stateManager 管理错误状态
// Phase 3重构: 实现状态与错误分离
// - 取消错误: 设置状态为"cancelled" + 记录cancel_reason
// - 其他错误: 不改变状态，只记录failure_reason + last_failure_reason
func (rlm *RequestLifecycleManager) HandleError(err error) {
	if err == nil {
		return
	}
	if rlm.stateManager.IsCompleted() {
		rlm.logger.Debug(fmt.Sprintf("⏭️ [错误忽略] [%s] 请求已终态，忽略错误: %v",
			rlm.stateManager.GetRequestID(), err))
		return
	}

	// 更新 stateManager 中的错误信息
	rlm.stateManager.SetLastError(err)

	// 优先复用预计算的错误分类，避免重复日志
	errorCtx := rlm.consumePreparedErrorContext(err)
	if errorCtx == nil {
		errorCtx = rlm.errorRecovery.ClassifyError(err, rlm.stateManager.GetRequestID(),
			rlm.stateManager.GetEndpointName(), rlm.stateManager.GetGroupName(), rlm.stateManager.GetRetryCount())
	}

	// Phase 3核心逻辑: 状态与错误分离
	switch errorCtx.ErrorType {
	case ErrorTypeClientCancel:
		// 🔧 [重构] 使用统一的CancelRequest方法处理取消
		// 这里通常没有Token信息，因为是在请求处理早期阶段取消
		rlm.CancelRequest(err.Error(), nil)
	default:
		// 其他错误: 不改变状态，只记录failure_reason
		// 状态转换由重试逻辑控制(retry/suspended/failed)，不在HandleError中处理
		if rlm.usageTracker != nil {
			failureReason := rlm.MapErrorTypeToFailureReason(errorCtx.ErrorType)
			chain := rlm.stateManager.AnnotateEndpointChainError(
				handlers.GetStatusCodeFromError(err, nil),
				handlers.ExtractUpstreamErrorMessage(err),
				tracking.DiagnosticHeaders(handlers.UpstreamErrorHeaders(err)),
			)
			opts := tracking.UpdateOptions{
				FailureReason: &failureReason,
			}
			if chain != "" {
				opts.EndpointChain = &chain
			}
			rlm.usageTracker.RecordRequestUpdate(rlm.stateManager.GetRequestID(), opts)
		}
		rlm.logger.Error(fmt.Sprintf("⚠️ [错误记录] [%s] 错误类型: %s, 错误: %v (状态由重试逻辑控制)",
			rlm.stateManager.GetRequestID(), rlm.errorRecovery.getErrorTypeName(errorCtx.ErrorType), err))
	}
}

// getModelNameForCost 获取用于成本计算的模型名
// 🔧 [重构] 2025-12-11: 提取公共逻辑，避免代码重复
func (rlm *RequestLifecycleManager) getModelNameForCost() string {
	modelName := rlm.GetModelName()
	if modelName == "" {
		return "unknown"
	}
	return modelName
}

// FailRequest 标记请求最终失败。本地失败没有上游状态，因此不接受也不记录 HTTP 状态码。
// 🎯 [SRP重构] 使用 stateManager 获取信息
// Phase 3新增: 专门用于标记最终失败的方法
// 设置状态为"failed"并记录失败原因和错误详情
func (rlm *RequestLifecycleManager) FailRequest(failureReason, errorDetail string) {
	// Local failures have no provider response. Keep the protocol fallback out
	// of lifecycle state, events, and request tracking; only the explicit
	// WithUpstreamStatus path may persist an HTTP status.
	duration := rlm.stateManager.GetDuration()
	modelName := rlm.getModelNameForCost()
	requestID := rlm.stateManager.GetRequestID()
	endpointName := rlm.stateManager.GetEndpointName()
	if !rlm.stateManager.MarkCompletedOnce() {
		rlm.logger.Debug(fmt.Sprintf("⏭️ [失败忽略] [%s] 请求已终态，忽略失败更新: %s",
			requestID, failureReason))
		return
	}

	// 🚀 [架构重构] 使用统一的最终失败记录方法，一次性更新所有相关字段
	if rlm.usageTracker != nil {
		rlm.syncRetryCount(requestID)
		rlm.usageTracker.RecordRequestFinalFailure(requestID, modelName, "failed", failureReason, errorDetail, duration, 0, nil)
	}

	rlm.logger.Error(fmt.Sprintf("❌ [请求最终失败] [%s] 端点: %s, 原因: %s, 耗时: %dms",
		requestID, endpointName, failureReason, duration.Milliseconds()))

	// 调用统一的状态通知方法
	rlm.notifyStatusChange("failed", rlm.stateManager.GetRetryCount(), 0)
}

// FailRequestWithUpstreamStatus records a failure whose status was carried by
// a real upstream HTTP response. Relay-generated control statuses stay hidden.
func (rlm *RequestLifecycleManager) FailRequestWithUpstreamStatus(failureReason, errorDetail string, httpStatus int) {
	duration := rlm.stateManager.GetDuration()
	modelName := rlm.getModelNameForCost()
	requestID := rlm.stateManager.GetRequestID()
	if !rlm.stateManager.MarkCompletedOnce() {
		return
	}
	if rlm.usageTracker != nil {
		rlm.syncRetryCount(requestID)
		rlm.usageTracker.RecordRequestFinalFailureWithUpstreamStatus(requestID, modelName, "failed", failureReason, errorDetail, duration, httpStatus, nil)
	}
	rlm.notifyStatusChange("failed", rlm.stateManager.GetRetryCount(), httpStatus)
}

// CancelRequest 标记请求被取消
// 🎯 [SRP重构] 使用 stateManager 获取信息
// 统一的取消处理方法，确保记录完成时间和耗时
// tokens参数可以为nil（无计费信息）或包含已产生的Token信息
func (rlm *RequestLifecycleManager) CancelRequest(cancelReason string, tokens *tracking.TokenUsage) {
	duration := rlm.stateManager.GetDuration()
	modelName := rlm.getModelNameForCost()
	requestID := rlm.stateManager.GetRequestID()
	endpointName := rlm.stateManager.GetEndpointName()
	groupName := rlm.stateManager.GetGroupName()
	if !rlm.stateManager.MarkCompletedOnce() {
		rlm.logger.Debug(fmt.Sprintf("⏭️ [取消忽略] [%s] 请求已终态，忽略取消更新: %s",
			requestID, cancelReason))
		return
	}

	// 🚀 [架构重构] 使用统一的最终失败记录方法，一次性更新所有相关字段
	if rlm.usageTracker != nil {
		rlm.syncRetryCount(requestID)
		rlm.usageTracker.RecordRequestFinalFailure(requestID, modelName, "cancelled", cancelReason, "", duration, 0, tokens)
	}

	if tokens != nil {
		totalTokens := tokens.InputTokens + tokens.OutputTokens
		rlm.logger.Info(fmt.Sprintf("🚫 [请求被取消] [%s] 端点: %s, 组: %s, 耗时: %dms, 原因: %s, Token: %d",
			requestID, endpointName, groupName, duration.Milliseconds(), cancelReason, totalTokens))
	} else {
		rlm.logger.Info(fmt.Sprintf("🚫 [请求被取消] [%s] 端点: %s, 组: %s, 耗时: %dms, 原因: %s",
			requestID, endpointName, groupName, duration.Milliseconds(), cancelReason))
	}

	// 调用统一的状态通知方法
	rlm.notifyStatusChange("cancelled", rlm.stateManager.GetRetryCount(), 0)
}

// GetLastError 获取最后一次错误
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetLastError() error {
	return rlm.stateManager.GetLastError()
}

// calculateCost 计算Token使用成本的辅助方法
func (rlm *RequestLifecycleManager) calculateCost(tokens *tracking.TokenUsage, pricing tracking.ModelPricing) float64 {
	if tokens == nil {
		return 0.0
	}

	inputCost := float64(tokens.InputTokens) * pricing.Input / 1000000
	outputCost := float64(tokens.OutputTokens) * pricing.Output / 1000000
	cacheCost := float64(tokens.CacheCreationTokens) * pricing.CacheCreation / 1000000

	return inputCost + outputCost + cacheCost
}

// SetFinalStatusCode 设置最终状态码
// 🎯 [SRP重构] 委托给 stateManager
// 用于记录请求的实际HTTP状态码，替代硬编码的状态码
func (rlm *RequestLifecycleManager) SetFinalStatusCode(statusCode int) {
	rlm.stateManager.SetFinalStatusCode(statusCode)
}

// GetFinalStatusCode 获取最终状态码
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetFinalStatusCode() int {
	return rlm.stateManager.GetFinalStatusCode()
}

// RecordTokensForFailedRequest 为失败请求记录Token信息
// 🎯 [SRP重构] 使用 stateManager 获取信息
// 与 CompleteRequest 的区别：只记录Token统计，不改变请求状态
func (rlm *RequestLifecycleManager) RecordTokensForFailedRequest(tokens *tracking.TokenUsage, failureReason string) {
	requestID := rlm.stateManager.GetRequestID()
	endpointName := rlm.stateManager.GetEndpointName()

	if requestID != "" && tokens != nil {
		if hasInvalidTokenUsage(tokens) {
			rlm.logger.Warn(fmt.Sprintf("⏭️ [跳过非法Token] [%s] 失败请求包含负数Token，忽略记录", requestID))
			return
		}

		// ✅ 检查是否有真实的Token使用
		hasRealTokens := tokens.InputTokens > 0 || tokens.OutputTokens > 0 ||
			tokens.CacheCreationTokens > 0 || tokens.CacheReadTokens > 0

		if !hasRealTokens {
			// 空Token信息不记录
			rlm.logger.Debug(fmt.Sprintf("⏭️ [跳过空Token] [%s] 失败请求无实际Token消耗", requestID))
			return
		}

		duration := rlm.stateManager.GetDuration()
		modelName := rlm.GetModelName()
		if modelName == "" {
			modelName = "unknown"
		}

		// ✅ 只记录Token统计到UsageTracker，不调用 RecordRequestComplete
		if rlm.usageTracker != nil {
			rlm.usageTracker.RecordFailedRequestTokens(requestID, modelName, tokens, duration, failureReason)
		}

		// ✅ 记录到监控中间件（总是调用，即使usageTracker为nil）
		if rlm.monitoringMiddleware != nil {
			monitorTokens := &monitor.TokenUsage{
				InputTokens:         tokens.InputTokens,
				OutputTokens:        tokens.OutputTokens,
				CacheCreationTokens: tokens.CacheCreationTokens,
				CacheReadTokens:     tokens.CacheReadTokens,
			}
			// 新增失败请求Token记录方法
			rlm.monitoringMiddleware.RecordFailedRequestTokens(requestID, endpointName, monitorTokens, failureReason)
		}

		rlm.logger.Info(fmt.Sprintf("💾 [失败请求Token记录] [%s] 端点: %s, 原因: %s, 模型: %s, 输入: %d, 输出: %d",
			requestID, endpointName, failureReason, modelName, tokens.InputTokens, tokens.OutputTokens))
	}
}

func hasInvalidTokenUsage(tokens *tracking.TokenUsage) bool {
	if tokens == nil {
		return false
	}

	return tokens.InputTokens < 0 ||
		tokens.OutputTokens < 0 ||
		tokens.CacheCreationTokens < 0 ||
		tokens.CacheReadTokens < 0 ||
		tokens.CacheCreation5mTokens < 0 ||
		tokens.CacheCreation1hTokens < 0
}
