package proxy

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/handlers"
)

// IncrementAttempt 线程安全地增加尝试计数
// 🎯 [SRP重构] 委托给 stateManager
// 用于统一重试计数语义，每次端点切换或重试时调用
// 移除高频 DEBUG 日志，避免循环重试时日志膨胀
func (rlm *RequestLifecycleManager) IncrementAttempt() int {
	return rlm.stateManager.IncrementAttempt()
}

// GetAttemptCount 线程安全地获取当前尝试次数
// 🎯 [SRP重构] 委托给 stateManager
// 返回真实的尝试次数，用于数据库记录和监控
func (rlm *RequestLifecycleManager) GetAttemptCount() int {
	return rlm.stateManager.GetAttemptCount()
}

// InitRetryBudget 初始化全局重试预算追踪
//
// 🔧 [P5.3 新增] 由 handlers 在重试循环开始前调用
// 参数：
//   - maxAttempts: 全局最大总尝试次数（含所有端点+渠道）
//   - maxDuration: 全局最大重试总时长
//
// 多次调用只生效一次（用 sync.Once 保证）
func (rlm *RequestLifecycleManager) InitRetryBudget(maxAttempts int, maxDuration time.Duration) {
	rlm.budgetInitOnce.Do(func() {
		rlm.budgetStartTime = time.Now()
		rlm.budgetMaxAttempts = maxAttempts
		rlm.budgetMaxDuration = maxDuration
	})
}

// CheckRetryBudget 检查全局重试预算是否耗尽
//
// 🔧 [P5.3 新增] 在每次重试决策前调用
// 返回值：
//   - exhausted: true 表示预算耗尽，应该立即停止重试
//   - reason: 耗尽原因，用于日志和错误信息
func (rlm *RequestLifecycleManager) CheckRetryBudget() (exhausted bool, reason string) {
	// 预算未初始化（默认值），允许无限重试（向后兼容）
	if rlm.budgetMaxAttempts <= 0 && rlm.budgetMaxDuration <= 0 {
		return false, ""
	}

	// 检查总尝试次数预算
	if rlm.budgetMaxAttempts > 0 {
		current := rlm.GetAttemptCount()
		if current >= rlm.budgetMaxAttempts {
			return true, fmt.Sprintf("retry budget exhausted: %d attempts (limit=%d)",
				current, rlm.budgetMaxAttempts)
		}
	}

	// 检查总时长预算
	if rlm.budgetMaxDuration > 0 && !rlm.budgetStartTime.IsZero() {
		elapsed := time.Since(rlm.budgetStartTime)
		if elapsed >= rlm.budgetMaxDuration {
			return true, fmt.Sprintf("retry budget exhausted: %v elapsed (limit=%v)",
				elapsed.Round(time.Millisecond), rlm.budgetMaxDuration)
		}
	}

	return false, ""
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
	rlm.logger.Debug(fmt.Sprintf("📋 [重试决策记录] [%s] 状态: %s, 原因: %s",
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
	if rlm.usageTracker != nil {
		rlm.usageTracker.RecordOutputSent(rlm.stateManager.GetRequestID())
	}
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
