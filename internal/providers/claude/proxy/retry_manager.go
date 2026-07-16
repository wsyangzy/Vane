package proxy

import (
	"context"
	"math"
	"net/http"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/proxy/handlers"
)

// RetryManager 重试管理器
// 负责重试算法逻辑、基于错误分类的重试决策、指数退避延迟计算
// 不涉及状态管理和数据库操作
type RetryManager struct {
	config        *config.Config
	errorRecovery *ErrorRecoveryManager
	endpointMgr   *endpoint.Manager
}

// NewRetryManager 创建重试管理器
func NewRetryManager(cfg *config.Config, errorRecovery *ErrorRecoveryManager, endpointMgr *endpoint.Manager) *RetryManager {
	return &RetryManager{
		config:        cfg,
		errorRecovery: errorRecovery,
		endpointMgr:   endpointMgr,
	}
}

// ShouldRetry 基于错误分类的重试决策
// 参数:
//   - errorCtx: 错误上下文信息
//   - attempt: 当前尝试次数（从1开始）
//
// 返回:
//   - bool: 是否应该重试
//   - time.Duration: 重试延迟时间
func (rm *RetryManager) ShouldRetry(errorCtx *handlers.ErrorContext, attempt int) (bool, time.Duration) {
	// 超过最大重试次数
	if attempt >= rm.config.Retry.MaxAttempts {
		return false, 0
	}

	// 基于错误类型判断
	switch errorCtx.ErrorType {
	case handlers.ErrorTypeNetwork, handlers.ErrorTypeConnectionTimeout, handlers.ErrorTypeServerError:
		// 网络错误、连接超时、服务器错误可重试
		return true, rm.calculateBackoff(attempt)
	case handlers.ErrorTypeEOF, handlers.ErrorTypeResponseTimeout, handlers.ErrorTypeTimeout:
		// EOF、响应超时不可重试（避免重复计费）
		return false, 0
	case handlers.ErrorTypeHTTP, handlers.ErrorTypeClientCancel:
		// HTTP错误（4xx）、客户端取消不可重试
		return false, 0
	case handlers.ErrorTypeAuth:
		// 2025-12-10: 认证/权限错误不在同一端点重试（故障转移在 ShouldRetryWithDecision 处理）
		return false, 0
	case handlers.ErrorTypeRateLimit:
		// 限流错误可重试，但使用更长的延迟
		return true, rm.calculateRateLimitBackoff(attempt)
	case handlers.ErrorTypeStream:
		// 流处理错误不可重试（数据已部分发送）
		return false, 0
	case handlers.ErrorTypeParsing:
		// 解析错误可重试
		return true, rm.calculateBackoff(attempt)
	case handlers.ErrorTypeNoHealthyEndpoints:
		// 健康检查限制错误 - 特殊策略：允许至少一次实际转发尝试，忽略健康检查状态
		if attempt < 1 {
			return true, 0 // 立即重试，不延迟
		}
		return false, 0 // 只允许一次尝试
	default:
		// 未知错误不重试（保守策略）
		return false, 0
	}
}

// GetHealthyEndpoints 获取健康端点列表
func (rm *RetryManager) GetHealthyEndpoints(ctx context.Context) []*endpoint.Endpoint {
	// 如果启用了快速测试且策略为fastest，使用实时测试结果
	if rm.endpointMgr.GetConfig().Strategy.Type == "fastest" && rm.endpointMgr.GetConfig().Strategy.FastTestEnabled {
		return rm.endpointMgr.GetFastestEndpointsWithRealTimeTest(ctx)
	}
	return rm.endpointMgr.GetHealthyEndpointsForTurn(endpoint.TurnFailureKeyFromContext(ctx))
}

// GetHealthyEndpointsExcluding 获取健康端点列表，排除已尝试的端点
// 用于实现严格优先级转移逻辑，确保在同一请求链路内只能向下一优先级转移
func (rm *RetryManager) GetHealthyEndpointsExcluding(ctx context.Context, excludeKeys []string) []*endpoint.Endpoint {
	return rm.endpointMgr.GetHealthyEndpointsExcludingForTurn(excludeKeys, endpoint.TurnFailureKeyFromContext(ctx))
}

// GetRecoveredEndpointsInChannel 获取当前渠道内已恢复的端点
// 用于 auto_wait 策略下最后一个端点失败后的重试逻辑
func (rm *RetryManager) GetRecoveredEndpointsInChannel(triedKeys []string) []*endpoint.Endpoint {
	return rm.endpointMgr.GetRecoveredEndpointsInChannel(triedKeys)
}

// IsAutoWaitStrategy 检查是否为 auto_wait 策略
// auto_wait 策略已移除，始终返回 false
func (rm *RetryManager) IsAutoWaitStrategy() bool {
	return false // auto_wait 已移除
}

// calculateBackoff 计算指数退避延迟
func (rm *RetryManager) calculateBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return rm.config.Retry.BaseDelay
	}

	baseDelay := rm.config.Retry.BaseDelay
	maxDelay := rm.config.Retry.MaxDelay
	multiplier := rm.config.Retry.Multiplier

	// 指数退避: baseDelay * (multiplier ^ (attempt-1))
	delay := time.Duration(float64(baseDelay) * math.Pow(multiplier, float64(attempt-1)))

	// 限制最大延迟
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// calculateRateLimitBackoff 计算限流错误的退避延迟
// 限流错误使用更保守的延迟策略
func (rm *RetryManager) calculateRateLimitBackoff(attempt int) time.Duration {
	baseDelay := rm.config.Retry.BaseDelay
	maxDelay := rm.config.Retry.MaxDelay

	// 限流错误使用更长的基础延迟
	rateLimitBaseDelay := baseDelay * 3

	// 限流错误的指数退避系数更大
	delay := time.Duration(float64(rateLimitBaseDelay) * math.Pow(2.5, float64(attempt-1)))

	// 限制最大延迟，但允许更长的等待时间
	rateLimitMaxDelay := maxDelay * 2
	if delay > rateLimitMaxDelay {
		delay = rateLimitMaxDelay
	}

	return delay
}

// GetMaxAttempts 获取最大重试次数
func (rm *RetryManager) GetMaxAttempts() int {
	return rm.config.Retry.MaxAttempts
}

// GetConfig 获取配置信息（用于测试）
func (rm *RetryManager) GetConfig() *config.Config {
	return rm.config
}

// GetErrorRecoveryManager 获取错误恢复管理器（用于测试）
func (rm *RetryManager) GetErrorRecoveryManager() *ErrorRecoveryManager {
	return rm.errorRecovery
}

// GetEndpointManager 获取端点管理器（用于测试）
func (rm *RetryManager) GetEndpointManager() *endpoint.Manager {
	return rm.endpointMgr
}

// ShouldRetryWithDecision 基于错误分类的详细重试决策
// 完全复制retry/policy.go的决策逻辑，确保行为一致
// 参数:
//   - errorCtx: 错误上下文信息
//   - localAttempt: 当前端点的尝试次数（从1开始，用于退避计算）
//   - globalAttempt: 全局尝试次数（用于限流策略）
//   - isStreaming: 是否为流式请求
//
// 返回:
//   - handlers.RetryDecision: 详细的重试决策信息
func (rm *RetryManager) ShouldRetryWithDecision(errorCtx *handlers.ErrorContext, localAttempt int, globalAttempt int, isStreaming bool) handlers.RetryDecision {
	// 如果没有错误上下文，默认不重试
	if errorCtx == nil {
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    false,
			SuspendRequest:    false,
			FinalStatus:       "completed",
			Reason:            "没有错误，无需重试",
		}
	}

	// 🔧 [关键修复] 分离局部和全局计数语义
	// localAttempt: 用于退避计算和端点内重试判断
	// globalAttempt: 仅用于限流策略和全局挂起判断

	// 使用 ErrorType 枚举进行类型安全的判断
	switch errorCtx.ErrorType {
	case handlers.ErrorTypeClientCancel:
		// 客户端取消错误 - 立即停止，不重试
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    false,
			SuspendRequest:    false,
			FinalStatus:       "cancelled",
			Reason:            "客户端取消请求，立即停止",
		}
	default:
		// 简化策略：任何失败都直接切换端点，不做同端点重试和挂起
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			FinalStatus:       "",
			Reason:            "失败后切换端点",
		}
	}
}

// GetDefaultStatusCodeForFinalStatus 根据最终状态获取默认HTTP状态码
func GetDefaultStatusCodeForFinalStatus(finalStatus string) int {
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
