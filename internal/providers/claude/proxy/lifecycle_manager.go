package proxy

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/events"
	"github.com/wsyangzy/Vane/internal/platform/monitor"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/tracking"
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
	logger                *slog.Logger // provider 分流日志（provider=claude）

	// 错误上下文（保留）
	pendingErrorContext  *ErrorContext
	pendingErrorOriginal error

	// 模型管理（保留独立字段，因为涉及搭便车更新逻辑）
	stateMu          sync.RWMutex
	modelName        string
	modelUpdatedInDB bool

	// 计数器保护（保留，用于 PrepareErrorContext）
	counterMu sync.Mutex

	// 🔧 [P5.3 新增] 全局重试预算追踪
	// budgetStartTime: 重试预算开始计时的时间点（首次进入重试循环）
	// budgetMaxAttempts/budgetMaxDuration: 从 config.Retry 加载的预算上限
	budgetStartTime   time.Time
	budgetMaxAttempts int
	budgetMaxDuration time.Duration
	budgetInitOnce    sync.Once
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
		logger:               slog.Default().With("provider", "claude"),
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
		logger:                slog.Default().With("provider", "claude"),
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
		rlm.logger.Info(fmt.Sprintf("🚀 Request started [%s]", requestID))
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

// RecordPrivacyRedaction 记录隐私脱敏摘要。
//
// summaryJSON 只含规则名与命中数。命中原文与请求正文一律不得进入日志或数据库：
// 日志会广播到前端日志查看器并落盘，正文进来就等于把整段客户对话又存了一份。
func (rlm *RequestLifecycleManager) RecordPrivacyRedaction(summaryJSON string) {
	if summaryJSON == "" {
		return
	}
	requestID := rlm.stateManager.GetRequestID()
	logger := rlm.logger
	if logger == nil {
		logger = slog.Default().With("provider", "claude")
	}
	logger.Debug("[隐私防护] 请求正文已脱敏",
		"request_id", requestID,
		"privacy_redaction_summary", summaryJSON,
	)
	if rlm.usageTracker != nil && requestID != "" {
		rlm.usageTracker.RecordPrivacyRedaction(requestID, summaryJSON)
	}
}

// UpdateStatus 更新请求状态
// 🎯 [SRP重构] 使用 stateManager 管理状态，保留模型搭便车逻辑
// 调用 RecordRequestUpdate 记录状态变化，并实现模型信息搭便车更新机制
//
// retry_count 的口径与 Codex 一致：整请求累计尝试次数 - 1（跨端点、跨渠道、跨路由阶段）。
// 入参 retryCount 只用于日志与事件：各调用点历史上分别传过端点下标、全局尝试数，
// 直接落库会让同一请求的 retry_count 随最后一次调用来回跳，因此持久化值一律取内部计数。
func (rlm *RequestLifecycleManager) UpdateStatus(status string, retryCount, httpStatus int) {
	if rlm.stateManager.IsCompleted() {
		rlm.logger.Debug(fmt.Sprintf("⏭️ [状态忽略] [%s] 请求已终态，忽略状态更新: %s",
			rlm.stateManager.GetRequestID(), status))
		return
	}

	// 处理特殊的-1标记，使用内部计数器
	eventRetryCount := retryCount
	if retryCount == -1 {
		eventRetryCount = rlm.GetAttemptCount()
	}
	actualRetryCount := rlm.stateManager.GetRetryCount()

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
			}
			rlm.usageTracker.RecordRequestUpdate(requestID, opts)
		}
	}

	// 调用统一的状态通知方法
	rlm.notifyStatusChange(status, eventRetryCount, httpStatus)
}

// syncRetryCount 在终态落库前补写 retry_count 与链路（含每跳尝试次数）。
// 最后一次尝试失败后通常直接进入 FailRequest，中间没有 UpdateStatus；成功的那次尝试
// 也不会再触发链路写入。不补写的话热池里停留的是上一次更新时的值，会少算最后一次尝试。
func (rlm *RequestLifecycleManager) syncRetryCount(requestID string) {
	if rlm.usageTracker == nil || requestID == "" {
		return
	}
	retryCount := rlm.stateManager.GetRetryCount()
	opts := tracking.UpdateOptions{RetryCount: &retryCount}
	if chain := rlm.stateManager.GetEndpointChain(); chain != "" {
		opts.EndpointChain = &chain
	}
	rlm.usageTracker.RecordRequestUpdate(requestID, opts)
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
		rlm.logger.Info(fmt.Sprintf("🎯 [请求转发] [%s] 选择端点: %s",
			requestID, endpointName))
	case "retry":
		rlm.logger.Info(fmt.Sprintf("🔄 [需要重试] [%s] 端点: %s (重试次数: %d)",
			requestID, endpointName, retryCount))
	case "processing":
		rlm.logger.Info(fmt.Sprintf("⚙️ [请求处理] [%s] 端点: %s, 状态码: %d",
			requestID, endpointName, httpStatus))
	case "suspended":
		rlm.logger.Warn(fmt.Sprintf("⏸️ [请求挂起] [%s] 端点: %s",
			requestID, endpointName))
	case "cancelled":
		// 取消日志已在 CancelRequest 方法中记录完整信息，此处跳过避免重复
	case "error":
		rlm.logger.Error(fmt.Sprintf("❌ [请求错误] [%s] 端点: %s, 状态码: %d",
			requestID, endpointName, httpStatus))
	case "timeout":
		rlm.logger.Error(fmt.Sprintf("⏰ [请求超时] [%s] 端点: %s",
			requestID, endpointName))
	case "completed":
		rlm.logger.Info(fmt.Sprintf("✅ [请求完成] [%s] 端点: %s",
			requestID, endpointName))
	case "failed":
		// 失败日志已在 FailRequest 方法中记录完整信息，此处跳过避免重复
	}
}
