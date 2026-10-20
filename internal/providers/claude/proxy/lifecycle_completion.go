package proxy

import (
	"fmt"
	"strings"

	"github.com/wsyangzy/Vane/internal/platform/monitor"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// CompleteRequest 完成请求跟踪
// 🎯 [SRP重构] 部分委托给 tokenRecorder
// 调用 RecordRequestComplete 记录请求完成，包含Token使用信息和成本计算
// 这是所有请求完成的统一入口，确保架构一致性
func (rlm *RequestLifecycleManager) CompleteRequest(tokens *tracking.TokenUsage) {
	rlm.completeRequest(tokens, 0, false)
}

// CompleteRequestWithUpstreamStatus records a successful response status only
// when it came from the real upstream HTTP response.
func (rlm *RequestLifecycleManager) CompleteRequestWithUpstreamStatus(tokens *tracking.TokenUsage, httpStatus int) {
	rlm.completeRequest(tokens, httpStatus, true)
}

func (rlm *RequestLifecycleManager) completeRequest(tokens *tracking.TokenUsage, httpStatus int, httpStatusIsUpstream bool) {
	endpointName := rlm.stateManager.GetEndpointName()
	if !rlm.stateManager.MarkCompletedOnce() {
		rlm.logger.Debug(fmt.Sprintf("⏭️ [完成忽略] [%s] 请求已终态，忽略完成更新",
			rlm.stateManager.GetRequestID()))
		return
	}

	// 🚀 [端点自愈] 无论usageTracker是否为空，都应该广播端点成功信号
	// 这是端点自愈功能的关键，不应该依赖于数据库跟踪功能
	if rlm.recoverySignalManager != nil && endpointName != "" {
		rlm.recoverySignalManager.BroadcastEndpointSuccess(endpointName)
	}

	if hasInvalidTokenUsage(tokens) {
		rlm.logger.Warn(fmt.Sprintf("⏭️ [跳过非法Token] [%s] 完成请求包含负数Token，忽略计费记录",
			rlm.stateManager.GetRequestID()))
		tokens = nil
	}

	// 委托给 tokenRecorder 记录完成
	if rlm.usageTracker != nil && rlm.stateManager.GetRequestID() != "" {
		// 使用线程安全的方式获取模型信息
		modelName := rlm.GetModelName()
		if modelName == "" {
			modelName = "unknown"
		}

		// 同时记录到监控中间件（用于Web图表显示）
		if rlm.monitoringMiddleware != nil && tokens != nil {
			monitorTokens := &monitor.TokenUsage{
				InputTokens:         tokens.InputTokens,
				OutputTokens:        tokens.OutputTokens,
				CacheCreationTokens: tokens.CacheCreationTokens,
				CacheReadTokens:     tokens.CacheReadTokens,
			}
			rlm.monitoringMiddleware.RecordTokenUsage(rlm.stateManager.GetRequestID(), endpointName, monitorTokens)
		}

		// 增强的完成日志，包含更详细信息
		duration := rlm.stateManager.GetDuration()
		requestID := rlm.stateManager.GetRequestID()
		groupName := rlm.stateManager.GetGroupName()
		retryCount := rlm.stateManager.GetRetryCount()

		if tokens != nil {
			totalTokens := tokens.InputTokens + tokens.OutputTokens
			cacheTokens := tokens.CacheCreationTokens + tokens.CacheReadTokens

			rlm.logger.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s (总尝试 %d 个端点)",
				requestID, endpointName, retryCount+1))
			rlm.logger.Info(fmt.Sprintf("📊 [Token统计] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
				requestID, modelName, tokens.InputTokens, tokens.OutputTokens,
				totalTokens, cacheTokens, duration.Milliseconds()))
		} else {
			rlm.logger.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s, 组: %s, 模型: %s, 耗时: %dms (无Token统计)",
				requestID, endpointName, groupName, modelName, duration.Milliseconds()))
		}

		// 记录请求成功完成到使用跟踪器（包括状态、耗时、Token、成本）
		rlm.syncRetryCount(requestID)
		if httpStatusIsUpstream {
			rlm.usageTracker.RecordRequestSuccessWithHTTPStatus(requestID, modelName, tokens, duration, httpStatus)
		} else {
			rlm.usageTracker.RecordRequestSuccess(requestID, modelName, tokens, duration)
		}
		rlm.logger.Info(fmt.Sprintf("✅ Request completed [%s]", requestID))
	}

	// 调用统一的状态通知方法
	statusCode := 0
	if httpStatusIsUpstream {
		statusCode = httpStatus
	}
	rlm.notifyStatusChange("completed", rlm.stateManager.GetRetryCount(), statusCode)
}

// CompleteRequestWithQuality 完成请求并标记数据质量问题
// 🎯 [SRP重构] 使用 stateManager 获取信息
// 🆕 [流完整性追踪] 2025-12-11
// 🔧 [方案A实现] 2025-12-20: 原子操作，在 CompleteAndArchive 中一次性设置所有字段包括 failureReason
// 用于处理流不完整但已完成的请求，记录 failure_reason 以标记数据质量问题
// 参数:
//   - tokens: Token使用统计
//   - failureReason: 数据质量问题标识（如 "incomplete_stream", "stream_truncated"）
func (rlm *RequestLifecycleManager) CompleteRequestWithQuality(tokens *tracking.TokenUsage, failureReason string) {
	duration := rlm.stateManager.GetDuration()
	endpointName := rlm.stateManager.GetEndpointName()
	if !rlm.stateManager.MarkCompletedOnce() {
		rlm.logger.Debug(fmt.Sprintf("⏭️ [完成忽略] [%s] 请求已终态，忽略完成更新",
			rlm.stateManager.GetRequestID()))
		return
	}

	// 🚀 [端点自愈] 无论usageTracker是否为空，都应该广播端点成功信号
	if rlm.recoverySignalManager != nil && endpointName != "" {
		rlm.recoverySignalManager.BroadcastEndpointSuccess(endpointName)
	}

	requestID := rlm.stateManager.GetRequestID()
	if rlm.usageTracker != nil && requestID != "" {
		modelName := rlm.GetModelName()
		if modelName == "" {
			modelName = "unknown"
		}

		// 同时记录到监控中间件
		if rlm.monitoringMiddleware != nil && tokens != nil {
			monitorTokens := &monitor.TokenUsage{
				InputTokens:         tokens.InputTokens,
				OutputTokens:        tokens.OutputTokens,
				CacheCreationTokens: tokens.CacheCreationTokens,
				CacheReadTokens:     tokens.CacheReadTokens,
			}
			rlm.monitoringMiddleware.RecordTokenUsage(requestID, endpointName, monitorTokens)
		}

		// 日志记录
		retryCount := rlm.stateManager.GetRetryCount()
		if tokens != nil {
			totalTokens := tokens.InputTokens + tokens.OutputTokens
			cacheTokens := tokens.CacheCreationTokens + tokens.CacheReadTokens
			rlm.logger.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s (总尝试 %d 个端点)",
				requestID, endpointName, retryCount+1))
			rlm.logger.Info(fmt.Sprintf("📊 [Token统计] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
				requestID, modelName, tokens.InputTokens, tokens.OutputTokens,
				totalTokens, cacheTokens, duration.Milliseconds()))
		}

		// 🔧 [方案A核心] 使用 RecordRequestSuccessWithQuality 一次性完成所有字段设置
		// 包括 status、tokens、duration 和 failureReason，避免两次独立操作的时序问题
		rlm.syncRetryCount(requestID)
		rlm.usageTracker.RecordRequestSuccessWithQuality(requestID, modelName, tokens, duration, failureReason)

		if failureReason != "" {
			rlm.logger.Warn(fmt.Sprintf("⚠️ [数据质量标记] [%s] failure_reason=%s", requestID, failureReason))
		}
		rlm.logger.Info(fmt.Sprintf("✅ Request completed [%s]", requestID))
	}

	// 调用统一的状态通知方法
	rlm.notifyStatusChange("completed", rlm.stateManager.GetRetryCount(), 0)
}

// RecordPartialTokens 记录部分失败的Token消耗（用于流截断等场景）
// 🎯 [SRP重构] 使用 stateManager 获取信息
// 🆕 [方案D实现] 2025-12-26: 记录+重试双轨制
// 功能：记录已消耗的Token到数据库，但不标记请求完成，允许继续重试其他端点
// 参数:
//   - tokens: 已消耗的Token统计
//   - failureReason: 失败原因（如 "stream_truncated"）
//   - endpointName: 失败的端点名称（用于部分记录标识）
func (rlm *RequestLifecycleManager) RecordPartialTokens(tokens *tracking.TokenUsage, failureReason, endpointName string) {
	duration := rlm.stateManager.GetDuration()
	requestID := rlm.stateManager.GetRequestID()

	if rlm.usageTracker != nil && requestID != "" {
		modelName := rlm.GetModelName()
		if modelName == "" {
			modelName = "unknown"
		}

		// 记录到监控中间件（内存统计）
		if rlm.monitoringMiddleware != nil && tokens != nil {
			monitorTokens := &monitor.TokenUsage{
				InputTokens:         tokens.InputTokens,
				OutputTokens:        tokens.OutputTokens,
				CacheCreationTokens: tokens.CacheCreationTokens,
				CacheReadTokens:     tokens.CacheReadTokens,
			}
			// 使用特殊方法记录失败请求的Token
			rlm.monitoringMiddleware.RecordFailedRequestTokens(requestID, endpointName, monitorTokens, failureReason)
		}

		// 🔧 [关键] 记录部分失败到数据库，但不从热池删除请求
		// 这样后续重试时仍然可以更新同一个请求ID
		if tokens != nil {
			totalTokens := tokens.InputTokens + tokens.OutputTokens
			cacheTokens := tokens.CacheCreationTokens + tokens.CacheReadTokens
			rlm.logger.Warn(fmt.Sprintf("⚠️ [部分失败记录] [%s] 端点: %s, failure_reason: %s",
				requestID, endpointName, failureReason))
			rlm.logger.Info(fmt.Sprintf("📊 [部分Token] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
				requestID, modelName, tokens.InputTokens, tokens.OutputTokens,
				totalTokens, cacheTokens, duration.Milliseconds()))

			// 调用tracker的部分记录方法（需要在tracker.go中添加）
			rlm.usageTracker.RecordPartialFailure(requestID, modelName, tokens, duration, failureReason, endpointName)
		}
	}

	// ⚠️ 注意：不调用 notifyStatusChange，不更新请求状态为完成
	// 请求仍然处于活跃状态，可以继续重试其他端点
}

// HandleNonTokenResponse 处理非Token响应的Fallback机制
// 🎯 [SRP重构] 使用 stateManager 获取信息
// 用于处理不包含Token信息的响应（如健康检查、配置查询等）
func (rlm *RequestLifecycleManager) HandleNonTokenResponse(responseContent string) {
	rlm.handleNonTokenResponse(responseContent, 0, false)
}

// HandleNonTokenResponseWithUpstreamStatus preserves the real status for a
// successful response that does not contain token usage information.
func (rlm *RequestLifecycleManager) HandleNonTokenResponseWithUpstreamStatus(responseContent string, httpStatus int) {
	rlm.handleNonTokenResponse(responseContent, httpStatus, true)
}

func (rlm *RequestLifecycleManager) handleNonTokenResponse(responseContent string, httpStatus int, httpStatusIsUpstream bool) {
	// 分析响应内容，确定合适的模型名
	modelName := rlm.analyzeResponseType(responseContent)

	// 创建空Token使用统计
	emptyTokens := &tracking.TokenUsage{
		InputTokens:         0,
		OutputTokens:        0,
		CacheCreationTokens: 0,
		CacheReadTokens:     0,
	}

	// 完成请求记录
	if httpStatusIsUpstream {
		rlm.CompleteRequestWithUpstreamStatus(emptyTokens, httpStatus)
	} else {
		rlm.CompleteRequest(emptyTokens)
	}

	requestID := rlm.stateManager.GetRequestID()
	rlm.logger.Info(fmt.Sprintf("🎯 [非Token响应] [%s] 模型: %s, 内容长度: %d字节",
		requestID, modelName, len(responseContent)))
}

// analyzeResponseType 分析响应类型，返回合适的模型名
func (rlm *RequestLifecycleManager) analyzeResponseType(responseContent string) string {
	if len(responseContent) == 0 {
		return "empty_response"
	}

	// 检查是否为错误响应
	if strings.Contains(strings.ToLower(responseContent), "error") {
		return "error_response"
	}

	// 检查是否为模型列表响应（健康检查）
	if strings.Contains(responseContent, `"data"`) &&
		strings.Contains(responseContent, `"id"`) {
		return "models_list"
	}

	// 检查是否为系统配置响应
	if strings.Contains(responseContent, `"config"`) ||
		strings.Contains(responseContent, `"version"`) {
		return "config_response"
	}

	// 默认为非Token响应
	return "non_token_response"
}
