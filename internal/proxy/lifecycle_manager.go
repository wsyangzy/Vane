package proxy

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"claude-request-forwarder/internal/endpoint"
	"claude-request-forwarder/internal/events"
	"claude-request-forwarder/internal/monitor"
	"claude-request-forwarder/internal/proxy/handlers"
	"claude-request-forwarder/internal/tracking"
)

// MonitoringMiddlewareInterface 定义监控中间件接口（扩展版）
type MonitoringMiddlewareInterface interface {
	RecordTokenUsage(connID string, endpoint string, tokens *monitor.TokenUsage)
	RecordFailedRequestTokens(connID, endpoint string, tokens *monitor.TokenUsage, failureReason string) // 新增方法
}

// RetryDecision 重试决策结果
type RetryDecision struct {
	RetrySameEndpoint bool   // 是否重试同一端点
	FinalStatus       string // 最终状态
	Reason            string // 决策原因
}

// RetryContext 重试上下文信息
type RetryContext struct {
	RequestID     string             // 请求ID
	Endpoint      *endpoint.Endpoint // 端点信息
	Attempt       int                // 当前尝试次数
	AttemptGlobal int                // 全局尝试次数
	Error         *ErrorContext      // 错误上下文
	IsStreaming   bool               // 是否为流式请求
}

// RequestLifecycleManager 请求生命周期管理器
// 🎯 [SRP重构] 2025-12-26: 重构为协调器，组合专用管理器
type RequestLifecycleManager struct {
	// 🆕 [SRP重构] 组合专用管理器
	stateManager *RequestStateManager // 状态和元数据管理

	// 核心依赖
	usageTracker          *tracking.UsageTracker
	monitoringMiddleware  MonitoringMiddlewareInterface
	errorRecovery         *ErrorRecoveryManager
	eventBus              events.EventBus
	recoverySignalManager *EndpointRecoverySignalManager

	// 错误上下文（保留）
	pendingErrorContext  *ErrorContext
	pendingErrorOriginal error

	// 模型管理（保留独立字段，因为涉及搭便车更新逻辑）
	stateMu          sync.RWMutex
	modelName        string
	modelUpdatedInDB bool

	// 计数器保护（保留，用于 PrepareErrorContext）
	counterMu sync.Mutex
}

// NewRequestLifecycleManager 创建新的请求生命周期管理器
// 🎯 [SRP重构] 使用组合模式，仅创建 stateManager
func NewRequestLifecycleManager(usageTracker *tracking.UsageTracker, monitoringMiddleware MonitoringMiddlewareInterface, requestID string, eventBus events.EventBus) *RequestLifecycleManager {
	stateManager := NewRequestStateManager(requestID)

	return &RequestLifecycleManager{
		stateManager:         stateManager,
		usageTracker:         usageTracker,
		monitoringMiddleware: monitoringMiddleware,
		errorRecovery:        NewErrorRecoveryManager(usageTracker),
		eventBus:             eventBus,
	}
}

// NewRequestLifecycleManagerWithRecoverySignal 创建带端点恢复信号管理器的生命周期管理器
// 🎯 [SRP重构] 使用组合模式，仅创建 stateManager
func NewRequestLifecycleManagerWithRecoverySignal(usageTracker *tracking.UsageTracker, monitoringMiddleware MonitoringMiddlewareInterface, requestID string, eventBus events.EventBus, recoverySignalManager *EndpointRecoverySignalManager) *RequestLifecycleManager {
	stateManager := NewRequestStateManager(requestID)

	return &RequestLifecycleManager{
		stateManager:          stateManager,
		usageTracker:          usageTracker,
		monitoringMiddleware:  monitoringMiddleware,
		errorRecovery:         NewErrorRecoveryManager(usageTracker),
		eventBus:              eventBus,
		recoverySignalManager: recoverySignalManager,
	}
}

// StartRequest 开始请求跟踪
// 🎯 [SRP重构] 使用 stateManager 获取请求ID
// 🆕 [事件简化] 2025-12-27: 使用辅助方法简化事件发布
// 调用 RecordRequestStart 记录请求开始，并发布请求开始事件
func (rlm *RequestLifecycleManager) StartRequest(clientIP, userAgent, method, path string, isStreaming bool) {
	requestID := rlm.stateManager.GetRequestID()

	// 原有的数据记录逻辑
	if rlm.usageTracker != nil && requestID != "" {
		rlm.usageTracker.RecordRequestStart(requestID, clientIP, userAgent, method, path, isStreaming)
		slog.Info(fmt.Sprintf("🚀 Request started [%s]", requestID))
	}

	// 🆕 [事件简化] 使用辅助方法发布请求开始事件
	rlm.publishEvent(events.EventRequestStarted, events.PriorityNormal, map[string]interface{}{
		"client_ip":    clientIP,
		"user_agent":   userAgent,
		"method":       method,
		"path":         path,
		"is_streaming": isStreaming,
		"change_type":  "request_started",
	})
}

// UpdateStatus 更新请求状态
// 🎯 [SRP重构] 使用 stateManager 管理状态，保留模型搭便车逻辑
// 调用 RecordRequestUpdate 记录状态变化，并实现模型信息搭便车更新机制
// 如果retryCount为-1，则使用内部attemptCounter
func (rlm *RequestLifecycleManager) UpdateStatus(status string, retryCount, httpStatus int) {
	// 处理特殊的-1标记，使用内部计数器
	actualRetryCount := retryCount
	if retryCount == -1 {
		actualRetryCount = rlm.GetAttemptCount()
	}

	requestID := rlm.stateManager.GetRequestID()
	endpointName := rlm.stateManager.GetEndpointName()
	groupName := rlm.stateManager.GetGroupName()
	channel := rlm.stateManager.GetChannel()

	if rlm.usageTracker != nil && requestID != "" {
		// 获取当前的模型信息（线程安全）
		currentModel := rlm.GetModelName()

		// 搭便车机制：检查是否需要更新模型到数据库
		rlm.stateMu.Lock()
		shouldUpdateModel := currentModel != "" &&
			currentModel != "unknown" &&
			!rlm.modelUpdatedInDB
		if shouldUpdateModel {
			rlm.modelUpdatedInDB = true // 标记为已更新，避免重复
		}
		rlm.stateMu.Unlock()

		if shouldUpdateModel {
			// 第一次有模型信息时，执行带模型的更新
			opts := tracking.UpdateOptions{
				EndpointName: &endpointName,
				Channel:      &channel,
				GroupName:    &groupName,
				Status:       &status,
				RetryCount:   &actualRetryCount,
				HttpStatus:   &httpStatus,
				ModelName:    &currentModel,
			}
			rlm.usageTracker.RecordRequestUpdate(requestID, opts)
		} else {
			// 正常状态更新（模型已更新过或尚未就绪）
			opts := tracking.UpdateOptions{
				EndpointName: &endpointName,
				Channel:      &channel,
				GroupName:    &groupName,
				Status:       &status,
				RetryCount:   &actualRetryCount,
				HttpStatus:   &httpStatus,
			}
			rlm.usageTracker.RecordRequestUpdate(requestID, opts)
		}
	}

	// 调用统一的状态通知方法
	rlm.notifyStatusChange(status, actualRetryCount, httpStatus)
}

// notifyStatusChange 统一的状态通知方法
// 🎯 [SRP重构] 使用 stateManager 获取状态信息
// 🆕 [事件简化] 2025-12-27: 使用辅助方法简化事件发布
// 负责更新内部状态、发布事件通知和记录状态变更日志
// 这个方法被 UpdateStatus、CompleteRequest、FailRequest、CancelRequest 统一调用
func (rlm *RequestLifecycleManager) notifyStatusChange(status string, retryCount, httpStatus int) {
	// 更新 stateManager 状态
	rlm.stateManager.UpdateStatus(status, httpStatus)

	// 🆕 [事件简化] 使用辅助方法发布状态变更事件
	rlm.publishStatusChangeEvent(status, retryCount, httpStatus)

	// 记录状态变更日志
	requestID := rlm.stateManager.GetRequestID()
	endpointName := rlm.stateManager.GetEndpointName()

	switch status {
	case "forwarding":
		slog.Info(fmt.Sprintf("🎯 [请求转发] [%s] 选择端点: %s",
			requestID, endpointName))
	case "retry":
		slog.Info(fmt.Sprintf("🔄 [需要重试] [%s] 端点: %s (重试次数: %d)",
			requestID, endpointName, retryCount))
	case "processing":
		slog.Info(fmt.Sprintf("⚙️ [请求处理] [%s] 端点: %s, 状态码: %d",
			requestID, endpointName, httpStatus))
	case "suspended":
		slog.Warn(fmt.Sprintf("⏸️ [请求挂起] [%s] 端点: %s",
			requestID, endpointName))
	case "cancelled":
		// 取消日志已在 CancelRequest 方法中记录完整信息，此处跳过避免重复
	case "error":
		slog.Error(fmt.Sprintf("❌ [请求错误] [%s] 端点: %s, 状态码: %d",
			requestID, endpointName, httpStatus))
	case "timeout":
		slog.Error(fmt.Sprintf("⏰ [请求超时] [%s] 端点: %s",
			requestID, endpointName))
	case "completed":
		slog.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s",
			requestID, endpointName))
	case "failed":
		// 失败日志已在 FailRequest 方法中记录完整信息，此处跳过避免重复
	}
}

// CompleteRequest 完成请求跟踪
// 🎯 [SRP重构] 部分委托给 tokenRecorder
// 调用 RecordRequestComplete 记录请求完成，包含Token使用信息和成本计算
// 这是所有请求完成的统一入口，确保架构一致性
func (rlm *RequestLifecycleManager) CompleteRequest(tokens *tracking.TokenUsage) {
	endpointName := rlm.stateManager.GetEndpointName()

	// 🚀 [端点自愈] 无论usageTracker是否为空，都应该广播端点成功信号
	// 这是端点自愈功能的关键，不应该依赖于数据库跟踪功能
	if rlm.recoverySignalManager != nil && endpointName != "" {
		rlm.recoverySignalManager.BroadcastEndpointSuccess(endpointName)
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

			slog.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s (总尝试 %d 个端点)",
				requestID, endpointName, retryCount+1))
			slog.Info(fmt.Sprintf("📊 [Token统计] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
				requestID, modelName, tokens.InputTokens, tokens.OutputTokens,
				totalTokens, cacheTokens, duration.Milliseconds()))
		} else {
			slog.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s, 组: %s, 模型: %s, 耗时: %dms (无Token统计)",
				requestID, endpointName, groupName, modelName, duration.Milliseconds()))
		}

		// 记录请求成功完成到使用跟踪器（包括状态、耗时、Token、成本）
		rlm.usageTracker.RecordRequestSuccess(requestID, modelName, tokens, duration)
		slog.Info(fmt.Sprintf("✅ Request completed [%s]", requestID))
	}

	// 🔧 [修复 isCompleted] 2025-12-27: 标记请求已完成
	rlm.stateManager.MarkCompleted()

	// 调用统一的状态通知方法
	rlm.notifyStatusChange("completed", rlm.stateManager.GetRetryCount(), 200)
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
			slog.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s (总尝试 %d 个端点)",
				requestID, endpointName, retryCount+1))
			slog.Info(fmt.Sprintf("📊 [Token统计] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
				requestID, modelName, tokens.InputTokens, tokens.OutputTokens,
				totalTokens, cacheTokens, duration.Milliseconds()))
		}

		// 🔧 [方案A核心] 使用 RecordRequestSuccessWithQuality 一次性完成所有字段设置
		// 包括 status、tokens、duration 和 failureReason，避免两次独立操作的时序问题
		rlm.usageTracker.RecordRequestSuccessWithQuality(requestID, modelName, tokens, duration, failureReason)

		if failureReason != "" {
			slog.Warn(fmt.Sprintf("⚠️ [数据质量标记] [%s] failure_reason=%s", requestID, failureReason))
		}
		slog.Info(fmt.Sprintf("✅ Request completed [%s]", requestID))
	}

	// 🔧 [修复 isCompleted] 2025-12-27: 标记请求已完成
	rlm.stateManager.MarkCompleted()

	// 调用统一的状态通知方法
	rlm.notifyStatusChange("completed", rlm.stateManager.GetRetryCount(), 200)
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
			slog.Warn(fmt.Sprintf("⚠️ [部分失败记录] [%s] 端点: %s, failure_reason: %s",
				requestID, endpointName, failureReason))
			slog.Info(fmt.Sprintf("📊 [部分Token] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
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
	rlm.CompleteRequest(emptyTokens)

	requestID := rlm.stateManager.GetRequestID()
	slog.Info(fmt.Sprintf("🎯 [非Token响应] [%s] 模型: %s, 内容长度: %d字节",
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

// SetEndpoint 设置端点信息
// 🎯 [SRP重构] 更新 stateManager 并同步热池
// v5.0: 立即更新热池中的端点和渠道信息，确保前端实时显示
func (rlm *RequestLifecycleManager) SetEndpoint(endpointName, groupName, channel string) {
	// 更新 stateManager
	rlm.stateManager.SetEndpoint(endpointName, groupName, channel)

	// 立即更新热池中的端点和渠道信息
	if rlm.usageTracker != nil && rlm.stateManager.GetRequestID() != "" {
		rlm.usageTracker.RecordRequestUpdate(rlm.stateManager.GetRequestID(), tracking.UpdateOptions{
			EndpointName: &endpointName,
			GroupName:    &groupName,
			Channel:      &channel,
		})
	}
}

// SetModel 设置模型名称（线程安全）
// 简单版本，只在模型为空或unknown时设置
func (rlm *RequestLifecycleManager) SetModel(modelName string) {
	rlm.stateMu.Lock()
	defer rlm.stateMu.Unlock()

	// 只在当前模型为空或unknown时设置，避免覆盖更准确的模型信息
	if rlm.modelName == "" || rlm.modelName == "unknown" {
		rlm.modelName = modelName
		slog.Debug(fmt.Sprintf("🏷️ [模型提取] [%s] 从请求中获取模型名称: %s", rlm.stateManager.GetRequestID(), modelName))
	}
}

// SetModelWithComparison 设置模型名称并进行对比检查（线程安全）
// 如果已有模型，会进行对比并在不一致时输出警告，最终以新模型为准
func (rlm *RequestLifecycleManager) SetModelWithComparison(newModelName, source string) {
	rlm.stateMu.Lock()
	defer rlm.stateMu.Unlock()

	// 如果新模型为空或unknown，不进行设置
	if newModelName == "" || newModelName == "unknown" {
		return
	}

	// 如果当前没有模型或为unknown，直接设置
	if rlm.modelName == "" || rlm.modelName == "unknown" {
		rlm.modelName = newModelName
		slog.Debug(fmt.Sprintf("🏷️ [模型提取] [%s] 从%s设置模型名称: %s", rlm.stateManager.GetRequestID(), source, newModelName))
		return
	}

	// 如果两个模型都有值，进行对比
	if rlm.modelName != newModelName {
		slog.Warn(fmt.Sprintf("⚠️ [模型不一致] [%s] 请求体模型: %s, %s模型: %s - 以%s为准",
			rlm.stateManager.GetRequestID(), rlm.modelName, source, newModelName, source))

		// 以新模型（通常是message_start解析的）为准
		rlm.modelName = newModelName
	} else {
		slog.Debug(fmt.Sprintf("✅ [模型一致] [%s] 请求体与%s模型一致: %s", rlm.stateManager.GetRequestID(), source, newModelName))
	}
}

// SetModelName 设置模型名称（兼容性方法，内部调用SetModel）
// 用于在流处理中动态设置正确的模型信息
func (rlm *RequestLifecycleManager) SetModelName(modelName string) {
	rlm.SetModel(modelName)
}

// GetModelName 获取当前模型名称（线程安全）
func (rlm *RequestLifecycleManager) GetModelName() string {
	rlm.stateMu.RLock()
	defer rlm.stateMu.RUnlock()
	return rlm.modelName
}

// HasModel 检查是否已有有效的模型名称（线程安全）
func (rlm *RequestLifecycleManager) HasModel() bool {
	rlm.stateMu.RLock()
	defer rlm.stateMu.RUnlock()
	return rlm.modelName != "" && rlm.modelName != "unknown"
}

// GetRequestID 获取请求ID
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetRequestID() string {
	return rlm.stateManager.GetRequestID()
}

// GetEndpointName 获取端点名称
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetEndpointName() string {
	return rlm.stateManager.GetEndpointName()
}

// GetGroupName 获取组名称
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetGroupName() string {
	return rlm.stateManager.GetGroupName()
}

// GetChannel 获取渠道标签
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetChannel() string {
	return rlm.stateManager.GetChannel()
}

// GetDuration 获取请求持续时间
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetDuration() time.Duration {
	return rlm.stateManager.GetDuration()
}

// GetLastStatus 获取最后状态
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetLastStatus() string {
	return rlm.stateManager.GetStatus()
}

// GetRetryCount 获取重试次数
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetRetryCount() int {
	return rlm.stateManager.GetRetryCount()
}

// IsCompleted 检查请求是否已完成
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) IsCompleted() bool {
	return rlm.stateManager.IsCompleted()
}

// GetStats 获取生命周期统计信息
// 🎯 [SRP重构] 委托给 stateManager 并添加错误信息
func (rlm *RequestLifecycleManager) GetStats() map[string]any {
	// 从 stateManager 获取基础统计信息
	stats := rlm.stateManager.GetStats()

	// 如果有错误信息，包含在统计中
	lastError := rlm.stateManager.GetLastError()
	if lastError != nil {
		stats["last_error"] = lastError.Error()

		// 使用错误恢复管理器分析错误类型
		errorCtx := rlm.errorRecovery.ClassifyError(lastError, rlm.stateManager.GetRequestID(),
			rlm.stateManager.GetEndpointName(), rlm.stateManager.GetGroupName(), rlm.stateManager.GetRetryCount())
		stats["error_type"] = rlm.errorRecovery.getErrorTypeName(errorCtx.ErrorType)
		stats["retryable"] = rlm.errorRecovery.ShouldRetry(errorCtx)
	}

	return stats
}

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

	// 将 handlers.ErrorContext 转换为 proxy.ErrorContext，避免跨包指针依赖
	converted := &ErrorContext{
		RequestID:      errorCtx.RequestID,
		EndpointName:   errorCtx.EndpointName,
		GroupName:      errorCtx.GroupName,
		AttemptCount:   errorCtx.AttemptCount,
		ErrorType:      ErrorType(errorCtx.ErrorType),
		OriginalError:  errorCtx.OriginalError,
		RetryableAfter: errorCtx.RetryableAfter,
		MaxRetries:     errorCtx.MaxRetries,
	}

	rlm.pendingErrorContext = converted
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
			failureReason := rlm.MapErrorTypeToFailureReason(handlers.ErrorType(errorCtx.ErrorType))
			opts := tracking.UpdateOptions{
				FailureReason: &failureReason,
			}
			rlm.usageTracker.RecordRequestUpdate(rlm.stateManager.GetRequestID(), opts)
		}
		slog.Error(fmt.Sprintf("⚠️ [错误记录] [%s] 错误类型: %s, 错误: %v (状态由重试逻辑控制)",
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

// FailRequest 标记请求最终失败
// 🎯 [SRP重构] 使用 stateManager 获取信息
// Phase 3新增: 专门用于标记最终失败的方法
// 设置状态为"failed"并记录失败原因和错误详情
func (rlm *RequestLifecycleManager) FailRequest(failureReason, errorDetail string, httpStatus int) {
	duration := rlm.stateManager.GetDuration()
	modelName := rlm.getModelNameForCost()
	requestID := rlm.stateManager.GetRequestID()
	endpointName := rlm.stateManager.GetEndpointName()

	// 🚀 [架构重构] 使用统一的最终失败记录方法，一次性更新所有相关字段
	if rlm.usageTracker != nil {
		rlm.usageTracker.RecordRequestFinalFailure(requestID, modelName, "failed", failureReason, errorDetail, duration, httpStatus, nil)
	}

	slog.Error(fmt.Sprintf("❌ [请求最终失败] [%s] 端点: %s, 原因: %s, 状态码: %d, 耗时: %dms",
		requestID, endpointName, failureReason, httpStatus, duration.Milliseconds()))

	// 🔧 [修复 isCompleted] 2025-12-27: 标记请求已完成（失败也是终态）
	rlm.stateManager.MarkCompleted()

	// 调用统一的状态通知方法
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

	// 🚀 [架构重构] 使用统一的最终失败记录方法，一次性更新所有相关字段
	if rlm.usageTracker != nil {
		rlm.usageTracker.RecordRequestFinalFailure(requestID, modelName, "cancelled", cancelReason, "", duration, 499, tokens)
	}

	if tokens != nil {
		totalTokens := tokens.InputTokens + tokens.OutputTokens
		slog.Info(fmt.Sprintf("🚫 [请求被取消] [%s] 端点: %s, 组: %s, 耗时: %dms, 原因: %s, Token: %d",
			requestID, endpointName, groupName, duration.Milliseconds(), cancelReason, totalTokens))
	} else {
		slog.Info(fmt.Sprintf("🚫 [请求被取消] [%s] 端点: %s, 组: %s, 耗时: %dms, 原因: %s",
			requestID, endpointName, groupName, duration.Milliseconds(), cancelReason))
	}

	// 🔧 [修复 isCompleted] 2025-12-27: 标记请求已完成（取消也是终态）
	rlm.stateManager.MarkCompleted()

	// 调用统一的状态通知方法
	rlm.notifyStatusChange("cancelled", rlm.stateManager.GetRetryCount(), 499)
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
		// ✅ 检查是否有真实的Token使用
		hasRealTokens := tokens.InputTokens > 0 || tokens.OutputTokens > 0 ||
			tokens.CacheCreationTokens > 0 || tokens.CacheReadTokens > 0

		if !hasRealTokens {
			// 空Token信息不记录
			slog.Debug(fmt.Sprintf("⏭️ [跳过空Token] [%s] 失败请求无实际Token消耗", requestID))
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

		slog.Info(fmt.Sprintf("💾 [失败请求Token记录] [%s] 端点: %s, 原因: %s, 模型: %s, 输入: %d, 输出: %d",
			requestID, endpointName, failureReason, modelName, tokens.InputTokens, tokens.OutputTokens))
	}
}

// IncrementAttempt 线程安全地增加尝试计数
// 🎯 [SRP重构] 委托给 stateManager
// 用于统一重试计数语义，每次端点切换或重试时调用
func (rlm *RequestLifecycleManager) IncrementAttempt() int {
	newCount := rlm.stateManager.IncrementAttempt()
	slog.Debug(fmt.Sprintf("🔢 [尝试计数] [%s] 当前尝试次数: %d", rlm.stateManager.GetRequestID(), newCount))
	return newCount
}

// GetAttemptCount 线程安全地获取当前尝试次数
// 🎯 [SRP重构] 委托给 stateManager
// 返回真实的尝试次数，用于数据库记录和监控
func (rlm *RequestLifecycleManager) GetAttemptCount() int {
	return rlm.stateManager.GetAttemptCount()
}

// OnRetryDecision 处理重试决策结果
// 🎯 [SRP重构] 使用 stateManager 获取信息
func (rlm *RequestLifecycleManager) OnRetryDecision(decision RetryDecision, httpStatus int) {
	actualRetryCount := rlm.GetAttemptCount()
	requestID := rlm.stateManager.GetRequestID()

	if decision.RetrySameEndpoint {
		rlm.UpdateStatus("retry", actualRetryCount, httpStatus)
	} else if decision.FinalStatus != "" {
		rlm.UpdateStatus(decision.FinalStatus, actualRetryCount, httpStatus)
	}

	// 记录决策原因
	slog.Debug(fmt.Sprintf("📋 [重试决策记录] [%s] 状态: %s, 原因: %s",
		requestID, decision.FinalStatus, decision.Reason))
}

// GetRetryContext 获取重试上下文信息
// 🎯 [SRP重构] 使用 stateManager 获取信息
func (rlm *RequestLifecycleManager) GetRetryContext(endpoint *endpoint.Endpoint, err error, attempt int) RetryContext {
	errorRecovery := rlm.errorRecovery
	requestID := rlm.stateManager.GetRequestID()
	endpointName := rlm.stateManager.GetEndpointName()
	groupName := rlm.stateManager.GetGroupName()

	errorCtx := errorRecovery.ClassifyError(err, requestID, endpointName, groupName, attempt-1)

	return RetryContext{
		RequestID:     requestID,
		Endpoint:      endpoint,
		Attempt:       attempt,
		AttemptGlobal: rlm.GetAttemptCount(),
		Error:         errorCtx,
		IsStreaming:   false, // 由调用方设置
	}
}

// mapErrorTypeToFailureReason 将ErrorType映射为failure_reason字符串
// 基于error_recovery.go中定义的11种ErrorType
// MapErrorTypeToFailureReason 将ErrorType映射为failure_reason
func (rlm *RequestLifecycleManager) MapErrorTypeToFailureReason(errorType handlers.ErrorType) string {
	switch errorType {
	case handlers.ErrorTypeRateLimit:
		return "rate_limited"
	case handlers.ErrorTypeServerError:
		return "server_error"
	case handlers.ErrorTypeNetwork:
		return "network_error"
	case handlers.ErrorTypeTimeout:
		return "timeout"
	case handlers.ErrorTypeHTTP:
		return "http_error"
	case handlers.ErrorTypeAuth:
		return "auth_error"
	case handlers.ErrorTypeStream:
		return "stream_error"
	case handlers.ErrorTypeParsing:
		return "parsing_error"
	case handlers.ErrorTypeNoHealthyEndpoints:
		return "no_healthy"
	case handlers.ErrorTypeUnknown:
		return "unknown_error"
	case handlers.ErrorTypeClientCancel:
		// 客户端取消不是failure_reason，而是cancel_reason
		return "client_cancelled"
	default:
		return "unknown_error"
	}
}

// CanRestart 检查是否可以重启请求处理
// 🎯 [SRP重构] 委托给 stateManager
// 🔥 [递归修复] 防止executeStreamingWithRetry无限递归
func (rlm *RequestLifecycleManager) CanRestart() bool {
	return rlm.stateManager.CanRestart()
}

// IncrementRestart 增加重启计数
// 🎯 [SRP重构] 委托给 stateManager
// 🔥 [递归修复] 返回当前重启次数
func (rlm *RequestLifecycleManager) IncrementRestart() int {
	return rlm.stateManager.IncrementRestart()
}

// GetMaxRestarts 获取最大重启次数
// 🎯 [SRP重构] 委托给 stateManager
// 🔥 [递归修复] 用于日志输出
func (rlm *RequestLifecycleManager) GetMaxRestarts() int {
	return rlm.stateManager.GetMaxRestarts()
}

// MarkOutputSent 标记已向客户端输出实际模型内容
// 🎯 [SRP重构] 委托给 stateManager
// 🔥 [重复计费防护] 2025-12-26: 用于防止已输出内容的请求重试导致重复计费
func (rlm *RequestLifecycleManager) MarkOutputSent() {
	rlm.stateManager.MarkOutputSent()
	slog.Debug(fmt.Sprintf("🔒 [输出标记] [%s] 已标记为输出内容，禁止重试", rlm.stateManager.GetRequestID()))
}

// HasOutputContent 检查是否已向客户端输出实际模型内容
// 🎯 [SRP重构] 委托给 stateManager
// 🔥 [重复计费防护] 2025-12-26: 返回true时不应重试该请求
func (rlm *RequestLifecycleManager) HasOutputContent() bool {
	return rlm.stateManager.HasOutputContent()
}

// ========== 事件发布辅助方法 ==========
// 🆕 [事件简化] 2025-12-27: 提取重复的事件发布逻辑

// getCommonEventData 获取事件的通用数据字段
// 返回包含 request_id, endpoint_name, channel, group_name, model_name 的 map
func (rlm *RequestLifecycleManager) getCommonEventData() map[string]interface{} {
	return map[string]interface{}{
		"request_id":    rlm.stateManager.GetRequestID(),
		"endpoint_name": rlm.stateManager.GetEndpointName(),
		"channel":       rlm.stateManager.GetChannel(),
		"group_name":    rlm.stateManager.GetGroupName(),
		"model_name":    rlm.GetModelName(),
	}
}

// publishEvent 发布事件的统一入口
// 自动填充通用字段，合并额外数据，处理 nil 检查
func (rlm *RequestLifecycleManager) publishEvent(eventType events.EventType, priority events.EventPriority, extraData map[string]interface{}) {
	if rlm.eventBus == nil {
		return
	}

	// 获取通用数据
	data := rlm.getCommonEventData()

	// 合并额外数据
	if extraData != nil {
		for k, v := range extraData {
			data[k] = v
		}
	}

	rlm.eventBus.Publish(events.Event{
		Type:     eventType,
		Source:   "lifecycle_manager",
		Priority: priority,
		Data:     data,
	})
}

// publishStatusChangeEvent 发布状态变更事件的便捷方法
// 根据状态自动确定优先级和 change_type
func (rlm *RequestLifecycleManager) publishStatusChangeEvent(status string, retryCount, httpStatus int) {
	// 根据状态确定优先级和 change_type
	priority := events.PriorityNormal
	changeType := "status_changed"

	switch status {
	case "error", "timeout":
		priority = events.PriorityHigh
		changeType = "error_response"
	case "suspended":
		changeType = "suspended_change"
	case "retry":
		changeType = "retry_attempt"
	case "completed":
		changeType = "request_completed"
	case "failed":
		priority = events.PriorityHigh
		changeType = "request_failed"
	case "cancelled":
		changeType = "request_cancelled"
	}

	rlm.publishEvent(events.EventRequestUpdated, priority, map[string]interface{}{
		"status":      status,
		"retry_count": retryCount,
		"http_status": httpStatus,
		"change_type": changeType,
	})
}
