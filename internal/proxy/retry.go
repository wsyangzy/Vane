package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"claude-request-forwarder/config"
	"claude-request-forwarder/internal/endpoint"
	"claude-request-forwarder/internal/tracking"
)

// RetryHandler handles retry logic with exponential backoff
// @Deprecated: 将在v3.3.0版本中完全移除
// 请使用 internal/proxy/retry.RetryController 替代
// 迁移指南: docs/migration/retry_v3.3.md
//
// 新的重试架构提供了以下优势：
// - 统一的重试策略（常规和流式请求使用相同算法）
// - 更好的错误分类和决策逻辑
// - 支持限流错误的特殊处理
// - 更清晰的代码结构和可测试性
type RetryHandler struct {
	config               *config.Config
	endpointManager      *endpoint.Manager
	monitoringMiddleware interface {
		RecordRetry(connID string, endpoint string)
	}
	usageTracker *tracking.UsageTracker

	// Request suspension related fields
	suspendedRequestsMutex sync.RWMutex
	suspendedRequestsCount int
}

// NewRetryHandler creates a new retry handler
// @Deprecated: 将在v3.3.0版本中完全移除
// 请使用 internal/proxy/retry.NewRetryController 替代
// 迁移指南: docs/migration/retry_v3.3.md
func NewRetryHandler(cfg *config.Config) *RetryHandler {
	return &RetryHandler{
		config: cfg,
	}
}

// SetEndpointManager sets the endpoint manager
func (rh *RetryHandler) SetEndpointManager(manager *endpoint.Manager) {
	rh.endpointManager = manager
}

// SetMonitoringMiddleware sets the monitoring middleware
func (rh *RetryHandler) SetMonitoringMiddleware(mm interface {
	RecordRetry(connID string, endpoint string)
}) {
	rh.monitoringMiddleware = mm
}

// SetUsageTracker sets the usage tracker
func (rh *RetryHandler) SetUsageTracker(ut *tracking.UsageTracker) {
	rh.usageTracker = ut
}

// Operation represents a function that can be retried, returns response and error
type Operation func(ep *endpoint.Endpoint, connID string) (*http.Response, error)

// RetryableError represents an error that can be retried with additional context
type RetryableError struct {
	Err         error
	StatusCode  int
	IsRetryable bool
	Reason      string
}

func (re *RetryableError) Error() string {
	if re.Err != nil {
		return re.Err.Error()
	}
	return fmt.Sprintf("HTTP %d", re.StatusCode)
}

// Execute executes an operation with retry and fallback logic
// @Deprecated: 将在v3.3.0版本中完全移除
// 请使用 RetryController.ExecuteWithRetry 替代
// 迁移指南: docs/migration/retry_v3.3.md
func (rh *RetryHandler) Execute(operation Operation, connID string) (*http.Response, error) {
	return rh.ExecuteWithContext(context.Background(), operation, connID)
}

// ExecuteWithContext executes an operation with context, retry and fallback logic with dynamic group management
// @Deprecated: 将在v3.3.0版本中完全移除
// 请使用 RetryController.ExecuteWithRetry 替代
// 迁移指南: docs/migration/retry_v3.3.md
func (rh *RetryHandler) ExecuteWithContext(ctx context.Context, operation Operation, connID string) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response
	var totalEndpointsAttempted int

	// 冷却机制已移除，改为追踪已暂停的渠道
	groupsPausedThisRequest := make(map[string]bool)

	for {
		// Get healthy endpoints from currently active groups only (no auto group switching)
		var endpoints []*endpoint.Endpoint
		if rh.endpointManager.GetConfig().Strategy.Type == "fastest" && rh.endpointManager.GetConfig().Strategy.FastTestEnabled {
			endpoints = rh.endpointManager.GetFastestEndpointsWithRealTimeTest(ctx)
		} else {
			endpoints = rh.endpointManager.GetHealthyEndpoints()
		}

		// If no endpoints available from active groups, check if we should suspend request
		if len(endpoints) == 0 {
			// 检查是否应该挂起请求
			if rh.shouldSuspendRequest(ctx) {
				slog.InfoContext(ctx, fmt.Sprintf("🔄 [尝试挂起] 连接 %s 当前活跃组无可用端点，尝试挂起请求等待组切换", connID))

				// 挂起请求等待组切换
				if rh.waitForGroupSwitch(ctx, connID) {
					slog.InfoContext(ctx, fmt.Sprintf("🚀 [挂起恢复] 连接 %s 组切换完成，重新进入重试循环", connID))
					// 状态管理已迁移到LifecycleManager，此处不再记录状态
					// 历史注释：更新请求状态为转发中（从挂起状态恢复）
					continue // 重新进入外层循环，获取新的端点列表
				} else {
					slog.WarnContext(ctx, fmt.Sprintf("⚠️ [挂起失败] 连接 %s 挂起等待超时或被取消，继续原有错误处理", connID))
					// 状态管理已迁移到LifecycleManager，此处不再记录状态
					// 历史注释：更新请求状态为超时（挂起失败）
					// 继续执行原有的错误处理逻辑
				}
			}

			slog.WarnContext(ctx, "⚠️ [无可用端点] 当前活跃组中没有健康的端点，需要手动切换到其他组")
			break
		}

		// Group endpoints by group name for failure tracking
		groupEndpoints := make(map[string][]*endpoint.Endpoint)
		for _, ep := range endpoints {
			groupName := ep.Config.Group
			if groupName == "" {
				groupName = "Default"
			}
			groupEndpoints[groupName] = append(groupEndpoints[groupName], ep)
		}

		// Track which groups failed completely in this iteration
		groupsFailedThisIteration := make(map[string]bool)
		endpointsTriedThisIteration := 0

		// Try each endpoint in current endpoint set
		for endpointIndex, ep := range endpoints {
			totalEndpointsAttempted++
			endpointsTriedThisIteration++

			// Add endpoint info to context for logging
			ctxWithEndpoint := context.WithValue(ctx, "selected_endpoint", ep.Config.Name)

			groupName := ep.Config.Group
			if groupName == "" {
				groupName = "Default"
			}

			slog.InfoContext(ctxWithEndpoint, fmt.Sprintf("🎯 [请求转发] [%s] 选择端点: %s (组: %s, 总尝试 %d)",
				connID, ep.Config.Name, groupName, totalEndpointsAttempted))

			// 状态管理已迁移到LifecycleManager，此处不再记录状态
			// 历史注释：Record endpoint selection in usage tracking

			// Retry logic for current endpoint
			for attempt := 1; attempt <= rh.config.Retry.MaxAttempts; attempt++ {
				select {
				case <-ctx.Done():
					if lastResp != nil {
						lastResp.Body.Close()
					}
					// 状态管理已迁移到LifecycleManager，此处不再记录状态
					// 历史注释：记录请求取消状态
					return nil, ctx.Err()
				default:
				}

				// Execute operation
				resp, err := operation(ep, connID)
				if err == nil && resp != nil {
					// Check if response status code indicates success or should be retried
					retryDecision := rh.shouldRetryStatusCode(resp.StatusCode)

					if !retryDecision.IsRetryable {
						// 区分真正的成功和不可重试的错误
						if resp.StatusCode >= 200 && resp.StatusCode < 400 {
							// 2xx/3xx - 真正的成功
							slog.InfoContext(ctxWithEndpoint, fmt.Sprintf("✅ [请求成功] [%s] 端点: %s, 状态码: %d (总尝试 %d 个端点)",
								connID, ep.Config.Name, resp.StatusCode, totalEndpointsAttempted))

							// 状态管理已迁移到LifecycleManager，此处不再记录状态
							// 历史注释：Record success in usage tracking
						} else {
							// 4xx/5xx - 不可重试的错误（如404, 401等）
							slog.ErrorContext(ctxWithEndpoint, fmt.Sprintf("❌ [请求失败] [%s] 端点: %s, 状态码: %d - %s (总尝试 %d 个端点)",
								connID, ep.Config.Name, resp.StatusCode, retryDecision.Reason, totalEndpointsAttempted))

							// 状态管理已迁移到LifecycleManager，此处不再记录状态
							// 历史注释：Record error in usage tracking
						}

						return resp, nil
					}

					// Status code indicates we should retry
					slog.WarnContext(ctxWithEndpoint, fmt.Sprintf("🔄 [需要重试] [%s] 端点: %s, 组: %s (尝试 %d/%d) - 状态码: %d (%s)",
						connID, ep.Config.Name, groupName, attempt, rh.config.Retry.MaxAttempts, resp.StatusCode, retryDecision.Reason))

					// Close the response body before retrying
					resp.Body.Close()
					lastErr = &RetryableError{
						StatusCode:  resp.StatusCode,
						IsRetryable: true,
						Reason:      retryDecision.Reason,
					}
				} else {
					// Network error or other failure
					lastErr = err
					if err != nil {
						// 状态管理已迁移到LifecycleManager，类型判断不再需要
						// 历史注释：确定错误状态类型

						slog.WarnContext(ctxWithEndpoint, fmt.Sprintf("❌ [网络错误] [%s] 端点: %s, 组: %s (尝试 %d/%d) - 错误: %s",
							connID, ep.Config.Name, groupName, attempt, rh.config.Retry.MaxAttempts, err.Error()))

						// 状态管理已迁移到LifecycleManager，此处不再记录状态
						// 历史注释：Record error with proper status in usage tracking
					}
				}

				// Don't wait after the last attempt on the current endpoint
				if attempt == rh.config.Retry.MaxAttempts {
					break
				}

				// Record retry (we're about to retry)
				if rh.monitoringMiddleware != nil && connID != "" {
					rh.monitoringMiddleware.RecordRetry(connID, ep.Config.Name)
				}

				// 状态管理已迁移到LifecycleManager，此处不再记录状态
				// 历史注释：更新状态为retry（同端点重试也是重试状态）

				// Calculate delay with exponential backoff
				delay := rh.calculateDelay(attempt)

				slog.InfoContext(ctxWithEndpoint, fmt.Sprintf("⏳ [等待重试] [%s] 端点: %s, 组: %s - %s后进行第%d次尝试",
					connID, ep.Config.Name, groupName, delay.String(), attempt+1))

				// Wait before retry
				select {
				case <-ctx.Done():
					if lastResp != nil {
						lastResp.Body.Close()
					}
					// 状态管理已迁移到LifecycleManager，此处不再记录状态
					// 历史注释：记录请求取消状态
					return nil, ctx.Err()
				case <-time.After(delay):
					// Continue to next attempt
				}
			}

			slog.ErrorContext(ctxWithEndpoint, fmt.Sprintf("💥 [端点失败] [%s] 端点 %s, 组: %s 所有 %d 次尝试均失败",
				connID, ep.Config.Name, groupName, rh.config.Retry.MaxAttempts))

			// Check if all endpoints in this group have been tried and failed in this iteration
			groupEndpointsCount := len(groupEndpoints[groupName])
			failedEndpointsInGroup := 0
			for _, groupEp := range groupEndpoints[groupName] {
				// Count endpoints in this group that we've already tried in this iteration
				for i := 0; i <= endpointIndex; i++ {
					if endpoints[i].Config.Name == groupEp.Config.Name {
						failedEndpointsInGroup++
						break
					}
				}
			}

			// If all endpoints in current group have failed in this iteration, mark group as failed
			if failedEndpointsInGroup == groupEndpointsCount {
				groupsFailedThisIteration[groupName] = true
			}
		}

		// After trying all endpoints in current iteration, pause failed groups
		// 冷却机制已移除，SetGroupCooldown 现在只是暂停渠道
		if rh.endpointManager.GetConfig().Failover.Enabled {
			for groupName := range groupsFailedThisIteration {
				if !groupsPausedThisRequest[groupName] {
					slog.WarnContext(ctx, fmt.Sprintf("⏸️ [渠道暂停] 渠道 %s 中所有端点均已失败，暂停该渠道", groupName))
					rh.endpointManager.GetGroupManager().SetGroupCooldown(groupName)
					groupsPausedThisRequest[groupName] = true
				}
			}
		}

		// Check if automatic switching between channels is enabled
		if rh.endpointManager.GetConfig().Failover.Enabled {
			// Auto mode: Check if there are still active groups available
			// Get fresh endpoint list to see if any new groups became active
			var newEndpoints []*endpoint.Endpoint
			if rh.endpointManager.GetConfig().Strategy.Type == "fastest" && rh.endpointManager.GetConfig().Strategy.FastTestEnabled {
				newEndpoints = rh.endpointManager.GetFastestEndpointsWithRealTimeTest(ctx)
			} else {
				newEndpoints = rh.endpointManager.GetHealthyEndpoints()
			}

			// If we have new endpoints available (from different groups), continue the retry loop
			if len(newEndpoints) > 0 && len(groupsFailedThisIteration) > 0 {
				// Check if the new endpoints are from different groups than what we just tried
				newGroupsAvailable := false
				newGroups := make(map[string]bool)
				for _, ep := range newEndpoints {
					groupName := ep.Config.Group
					if groupName == "" {
						groupName = "Default"
					}
					newGroups[groupName] = true
				}

				// Check if any new group is available that wasn't in the failed iteration
				for newGroup := range newGroups {
					if !groupsFailedThisIteration[newGroup] {
						newGroupsAvailable = true
						break
					}
				}

				if newGroupsAvailable {
					slog.InfoContext(ctx, fmt.Sprintf("🔄 [自动组切换] 检测到新的活跃组，继续重试 (已尝试 %d 个端点)", totalEndpointsAttempted))
					continue // Continue outer loop with fresh endpoint list
				}
			}
		} else {
			// Manual mode: Check if any groups failed this iteration for manual intervention alert
			if len(groupsFailedThisIteration) > 0 {
				failedGroupNames := make([]string, 0, len(groupsFailedThisIteration))
				for groupName := range groupsFailedThisIteration {
					failedGroupNames = append(failedGroupNames, groupName)
				}
				slog.WarnContext(ctx, fmt.Sprintf("⚠️ [需要手动干预] 组失败需要手动切换，失败的组: %v - 请通过Web界面选择其他可用组", failedGroupNames))
			}
			// In manual mode, continue the outer loop to check if requests should be suspended
			// The outer loop will detect len(endpoints) == 0 and trigger suspension logic if enabled
			slog.InfoContext(ctx, "🔄 [手动模式] 继续外层循环检查是否需要挂起请求")
			continue
		}

		// Auto mode: No more endpoints in current active group, stop retry loop
		break
	}

	// Check if automatic switching is enabled and provide appropriate error message
	if rh.endpointManager.GetConfig().Failover.Enabled {
		// Auto mode error message
		slog.ErrorContext(ctx, fmt.Sprintf("💥 [全部失败] 所有活跃组均不可用 - 总共尝试了 %d 个端点 - 最后错误: %v",
			totalEndpointsAttempted, lastErr))
		return nil, fmt.Errorf("all active groups exhausted after trying %d endpoints, last error: %w", totalEndpointsAttempted, lastErr)
	} else {
		// Manual mode: Check if there are other available groups that can be manually activated
		allGroups := rh.endpointManager.GetGroupManager().GetAllGroups()
		availableGroups := make([]string, 0)
		for _, group := range allGroups {
			// 冷却机制已移除，只检查是否活跃和是否被暂停
			if !group.IsActive && !group.ManuallyPaused {
				// Check if group has healthy endpoints
				healthyInGroup := 0
				for _, ep := range group.Endpoints {
					if ep.IsHealthy() {
						healthyInGroup++
					}
				}
				if healthyInGroup > 0 {
					availableGroups = append(availableGroups, fmt.Sprintf("%s(%d个健康端点)", group.Name, healthyInGroup))
				}
			}
		}

		if len(availableGroups) > 0 {
			slog.ErrorContext(ctx, fmt.Sprintf("💥 [当前组不可用] 已尝试 %d 个端点均失败 - 可用备用组: %v - 请通过Web界面手动切换",
				totalEndpointsAttempted, availableGroups))
			return nil, fmt.Errorf("current active group exhausted after trying %d endpoints, available backup groups: %v, please switch manually via web interface, last error: %w",
				totalEndpointsAttempted, availableGroups, lastErr)
		} else {
			slog.ErrorContext(ctx, fmt.Sprintf("💥 [全部不可用] 所有组均不可用或已暂停 - 总共尝试了 %d 个端点 - 最后错误: %v",
				totalEndpointsAttempted, lastErr))
			return nil, fmt.Errorf("all groups exhausted or paused after trying %d endpoints, last error: %w", totalEndpointsAttempted, lastErr)
		}
	}
}

// calculateDelay calculates the delay for exponential backoff
// @Deprecated: 将在v3.3.0版本中完全移除
// 请使用 RetryController.CalculateBackoff 替代
// 迁移指南: docs/migration/retry_v3.3.md
func (rh *RetryHandler) calculateDelay(attempt int) time.Duration {
	// Calculate exponential backoff: base_delay * (multiplier ^ (attempt - 1))
	multiplier := math.Pow(rh.config.Retry.Multiplier, float64(attempt-1))
	delay := time.Duration(float64(rh.config.Retry.BaseDelay) * multiplier)

	// Cap at maximum delay
	if delay > rh.config.Retry.MaxDelay {
		delay = rh.config.Retry.MaxDelay
	}

	return delay
}

// shouldRetryStatusCode determines if an HTTP status code should trigger a retry
// @Deprecated: 将在v3.3.0版本中完全移除
// 请使用 RetryController.ShouldRetry 替代
// 迁移指南: docs/migration/retry_v3.3.md
func (rh *RetryHandler) shouldRetryStatusCode(statusCode int) *RetryableError {
	switch {
	case statusCode >= 200 && statusCode < 400:
		// 2xx Success and 3xx Redirects - don't retry
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: false,
			Reason:      "请求成功",
		}
	case statusCode == 400:
		// 400 Bad Request - should retry (could be temporary issue)
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: true,
			Reason:      "请求格式错误",
		}
	case statusCode == 401:
		// 401 Unauthorized - don't retry (auth issue)
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: false,
			Reason:      "身份验证失败，不重试",
		}
	case statusCode == 403:
		// 403 Forbidden - should retry (permission issue)
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: true,
			Reason:      "权限不足，重试中",
		}
	case statusCode == 404:
		// 404 Not Found - don't retry (resource doesn't exist)
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: false,
			Reason:      "资源不存在，不重试",
		}
	case statusCode == 429:
		// 429 Too Many Requests - should retry
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: true,
			Reason:      "请求频率过高",
		}
	case statusCode >= 400 && statusCode < 500:
		// Other 4xx Client Errors - don't retry by default
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: false,
			Reason:      "客户端错误，不重试",
		}
	case statusCode >= 500 && statusCode < 600:
		// 5xx Server Errors - should retry
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: true,
			Reason:      "服务器错误",
		}
	default:
		// Unknown status code - don't retry by default
		return &RetryableError{
			StatusCode:  statusCode,
			IsRetryable: false,
			Reason:      "未知状态码",
		}
	}
}

// IsRetryableError determines if an error should trigger a retry
// @Deprecated: 将在v3.3.0版本中完全移除
// 请使用 RetryController.ShouldRetry 替代
// 迁移指南: docs/migration/retry_v3.3.md
func (rh *RetryHandler) IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Handle RetryableError type
	if retryErr, ok := err.(*RetryableError); ok {
		return retryErr.IsRetryable
	}

	// Add logic to determine which errors are retryable
	// For now, we retry all errors except context cancellation
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	// Network errors, timeout errors etc. should be retried
	errorStr := strings.ToLower(err.Error())
	if strings.Contains(errorStr, "timeout") ||
		strings.Contains(errorStr, "connection refused") ||
		strings.Contains(errorStr, "connection reset") ||
		strings.Contains(errorStr, "no such host") ||
		strings.Contains(errorStr, "network unreachable") {
		return true
	}

	return true
}

// determineErrorStatus 根据错误类型和上下文确定状态
func (rh *RetryHandler) determineErrorStatus(err error, ctx context.Context) string {
	// 优先检查context状态
	if ctx.Err() == context.Canceled {
		return "cancelled" // 用户取消请求
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout" // 请求超时
	}

	// 检查错误本身
	if err != nil {
		if err == context.Canceled {
			return "cancelled"
		}
		if err == context.DeadlineExceeded {
			return "timeout"
		}
		// 检查错误消息中的取消标识
		errorStr := strings.ToLower(err.Error())
		if strings.Contains(errorStr, "context canceled") {
			return "cancelled"
		}
		if strings.Contains(errorStr, "context deadline exceeded") {
			return "timeout"
		}
	}

	return "error" // 其他错误
}

// UpdateConfig updates the retry handler configuration
func (rh *RetryHandler) UpdateConfig(cfg *config.Config) {
	rh.config = cfg
}

// GetSuspendedRequestsCount returns the current number of suspended requests
func (rh *RetryHandler) GetSuspendedRequestsCount() int {
	rh.suspendedRequestsMutex.RLock()
	defer rh.suspendedRequestsMutex.RUnlock()
	return rh.suspendedRequestsCount
}

// shouldSuspendRequest determines if a request should be suspended
// auto_wait 策略已移除，此方法始终返回 false
func (rh *RetryHandler) shouldSuspendRequest(ctx context.Context) bool {
	// 不再支持请求挂起功能
	return false
}

// waitForGroupSwitch suspends the request and waits for group switch notification
// auto_wait 策略已移除，此方法始终返回 false
func (rh *RetryHandler) waitForGroupSwitch(ctx context.Context, connID string) bool {
	// 不再支持请求挂起功能
	return false
}
