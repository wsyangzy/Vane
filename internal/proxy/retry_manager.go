package proxy

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"claude-request-forwarder/config"
	"claude-request-forwarder/internal/endpoint"
	"claude-request-forwarder/internal/proxy/handlers"
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
	// 否则返回健康的端点
	return rm.endpointMgr.GetHealthyEndpoints()
}

// GetHealthyEndpointsExcluding 获取健康端点列表，排除已尝试的端点（v6.1+）
// 用于实现严格优先级转移逻辑，确保在同一请求链路内只能向下一优先级转移
func (rm *RetryManager) GetHealthyEndpointsExcluding(ctx context.Context, excludeKeys []string) []*endpoint.Endpoint {
	return rm.endpointMgr.GetHealthyEndpointsExcluding(excludeKeys)
}

// GetRecoveredEndpointsInChannel 获取当前渠道内已恢复的端点（v6.1+）
// 用于 auto_wait 策略下最后一个端点失败后的重试逻辑
func (rm *RetryManager) GetRecoveredEndpointsInChannel(triedKeys []string) []*endpoint.Endpoint {
	return rm.endpointMgr.GetRecoveredEndpointsInChannel(triedKeys)
}

// IsAutoWaitStrategy 检查是否为 auto_wait 策略
// v6.2: auto_wait 策略已移除，始终返回 false
func (rm *RetryManager) IsAutoWaitStrategy() bool {
	return false // v6.2: auto_wait 已移除
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

	case handlers.ErrorTypeEOF:
		// v6.2.1: EOF 错误 - 连接中断，不在同一端点重试（避免重复计费），但允许切换端点
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true, // 切换到新端点可能成功
			SuspendRequest:    false,
			FinalStatus:       "",   // 不设最终状态，允许继续尝试其他端点
			Reason:            "EOF连接中断，不重试同端点，切换端点尝试",
		}

	case handlers.ErrorTypeResponseTimeout:
		// v6.2.1: 响应超时 - 服务器可能已处理，不在同一端点重试，但允许切换端点
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true, // 当前端点可能有问题，切换可能成功
			SuspendRequest:    false,
			FinalStatus:       "",   // 不设最终状态，允许继续尝试其他端点
			Reason:            "响应超时，不重试同端点，切换端点尝试",
		}

	case handlers.ErrorTypeTimeout:
		// v6.2.1: 兼容旧代码的超时错误 - 按响应超时处理，允许切换端点
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			FinalStatus:       "",
			Reason:            "超时错误，切换端点尝试",
		}

	case handlers.ErrorTypeConnectionTimeout:
		// 连接超时 - 还没连上服务器，可以安全重试
		if localAttempt < rm.config.Retry.MaxAttempts {
			return handlers.RetryDecision{
				RetrySameEndpoint: true,
				SwitchEndpoint:    false,
				SuspendRequest:    false,
				Delay:             rm.calculateBackoff(localAttempt),
				Reason:            "连接超时，在同一端点重试",
			}
		}
		// 达到最大重试次数，切换端点
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			Reason:            "连接超时重试达到上限，切换端点",
		}

	case handlers.ErrorTypeNetwork:
		// 网络错误：可以在同一端点重试，也可以切换端点
		if localAttempt < rm.config.Retry.MaxAttempts {
			return handlers.RetryDecision{
				RetrySameEndpoint: true,
				SwitchEndpoint:    false,
				SuspendRequest:    false,
				Delay:             rm.calculateBackoff(localAttempt),
				Reason:            "网络错误，在同一端点重试",
			}
		}
		// 达到最大重试次数，尝试切换端点
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			Reason:            "网络错误重试达到上限，切换端点",
		}

	case handlers.ErrorTypeHTTP:
		// HTTP错误：通常是4xx错误，不应在同一端点重试；
		// 但部分4xx（如 400）在“多端点/多渠道”场景下可能是端点侧差异导致，允许切换端点继续尝试。
		statusCode := handlers.GetStatusCodeFromError(errorCtx.OriginalError, nil)
		if statusCode != 0 && handlers.IsRetryableStatus(statusCode) {
			return handlers.RetryDecision{
				RetrySameEndpoint: false,
				SwitchEndpoint:    true,
				SuspendRequest:    false,
				FinalStatus:       "",
				Reason:            fmt.Sprintf("HTTP %d，尝试切换端点", statusCode),
			}
		}
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    false,
			SuspendRequest:    false,
			FinalStatus:       "error",
			Reason:            "HTTP错误，无需重试",
		}

	case handlers.ErrorTypeServerError:
		// 🔧 [修复] 服务器错误：先在同一端点重试，达到上限后切换端点
		// 恢复正确行为：同端点重试到MaxAttempts，然后切换
		if localAttempt < rm.config.Retry.MaxAttempts {
			return handlers.RetryDecision{
				RetrySameEndpoint: true,
				SwitchEndpoint:    false,
				SuspendRequest:    false,
				Delay:             rm.calculateBackoff(localAttempt),
				Reason:            "服务器错误，在同一端点重试",
			}
		}
		// 达到最大重试次数，尝试切换端点
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			Reason:            "服务器错误重试达到上限，切换端点",
		}

	case handlers.ErrorTypeStream:
		// v6.2.1: 流式错误 - 响应已接收但解析失败，不在同一端点重试，但允许切换端点
		// 切换端点不会导致重复计费（不同端点是独立请求）
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true, // 切换到新端点可能成功
			SuspendRequest:    false,
			FinalStatus:       "",   // 不设最终状态，允许继续尝试其他端点
			Reason:            "流式处理错误，切换端点尝试",
		}

	case handlers.ErrorTypeAuth:
		// 2025-12-10: 认证/权限错误（401/403）支持故障转移
		// 不同端点可能配置了不同的 token，切换端点可能解决问题
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true, // 尝试切换到其他端点
			SuspendRequest:    false,
			FinalStatus:       "", // 不设最终状态，允许继续尝试其他端点
			Reason:            "认证/权限错误，尝试切换端点",
		}

	case handlers.ErrorTypeRateLimit:
		// 限流错误：先在同一端点重试，达到上限后切换端点
		if localAttempt < rm.config.Retry.MaxAttempts {
			delay := rm.calculateRateLimitBackoff(localAttempt)
			return handlers.RetryDecision{
				RetrySameEndpoint: true,
				SwitchEndpoint:    false,
				SuspendRequest:    delay > 30*time.Second, // 如果延迟太长，考虑挂起
				Delay:             delay,
				Reason:            "限流错误，在同一端点重试",
			}
		}
		// 达到最大重试次数，尝试切换端点或挂起
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			Reason:            "限流错误重试达到上限，切换端点",
		}

	case handlers.ErrorTypeParsing:
		// 解析错误：通常是响应格式问题，切换端点重试
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			Delay:             rm.calculateBackoff(localAttempt),
			Reason:            "解析错误，切换端点重试",
		}

	case handlers.ErrorTypeNoHealthyEndpoints:
		// 健康检查限制错误：立即尝试所有活跃端点，不延迟
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true,
			SuspendRequest:    false,
			Delay:             0, // 立即尝试，不延迟
			Reason:            "健康检查限制，尝试所有活跃端点",
		}

	default:
		// v6.2.1: 未知错误：默认允许切换端点
		// 切换端点是安全的（不会重复计费），不切换只会让客户端中断
		// 对明显的"端点配置/证书错误"，也允许切换端点
		if errorCtx.OriginalError != nil {
			errStr := strings.ToLower(errorCtx.OriginalError.Error())
			if strings.Contains(errStr, "x509:") || strings.Contains(errStr, "tls: failed to verify certificate") {
				return handlers.RetryDecision{
					RetrySameEndpoint: false,
					SwitchEndpoint:    true,
					SuspendRequest:    false,
					FinalStatus:       "",
					Reason:            "TLS/证书校验失败，切换端点",
				}
			}
		}

		// 其他未知错误：允许切换端点尝试
		return handlers.RetryDecision{
			RetrySameEndpoint: false,
			SwitchEndpoint:    true, // v6.2.1: 切换端点是安全的
			SuspendRequest:    false,
			FinalStatus:       "",   // 不设最终状态，允许继续尝试其他端点
			Reason:            "未知错误，切换端点尝试",
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
