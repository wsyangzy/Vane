package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/proxy/handlers" // 🎯 [挂起取消区分] 新增handlers包导入
)

// SuspensionManager 管理请求挂起逻辑
// 从RetryHandler中分离出来，专门负责请求挂起的判断和等待逻辑
type SuspensionManager struct {
	config                *config.Config
	endpointManager       *endpoint.Manager
	groupManager          *endpoint.GroupManager
	recoverySignalManager *EndpointRecoverySignalManager // 端点恢复信号管理器

	// 挂起请求计数相关字段
	suspendedRequestsMutex  sync.RWMutex
	suspendedRequestsCount  int
	suspendedEndpointCounts map[string]int // 按端点键统计当前挂起请求数（channel::name）

	// 🛑 [循环终止] 等待用户操作的挂起计数
	manualRetryMutex         sync.RWMutex
	manualRetryRequestsCount int
	manualRetryRequests      map[string][]string
	manualRetryChannelCounts map[string]int
}

// NewSuspensionManager 创建新的挂起管理器
func NewSuspensionManager(cfg *config.Config, endpointManager *endpoint.Manager, groupManager *endpoint.GroupManager) *SuspensionManager {
	return &SuspensionManager{
		config:                   cfg,
		endpointManager:          endpointManager,
		groupManager:             groupManager,
		suspendedEndpointCounts:  make(map[string]int),
		manualRetryRequests:      make(map[string][]string),
		manualRetryChannelCounts: make(map[string]int),
	}
}

// NewSuspensionManagerWithRecoverySignal 创建带端点恢复信号的挂起管理器
func NewSuspensionManagerWithRecoverySignal(cfg *config.Config, endpointManager *endpoint.Manager, groupManager *endpoint.GroupManager, recoverySignalManager *EndpointRecoverySignalManager) *SuspensionManager {
	return &SuspensionManager{
		config:                   cfg,
		endpointManager:          endpointManager,
		groupManager:             groupManager,
		recoverySignalManager:    recoverySignalManager,
		suspendedEndpointCounts:  make(map[string]int),
		manualRetryRequests:      make(map[string][]string),
		manualRetryChannelCounts: make(map[string]int),
	}
}

// ShouldSuspend 判断是否应该挂起请求
// 恢复挂起功能，当"允许客户端重试"开启时挂起请求等待用户点击重试按钮
// 挂起条件：InterruptClientOnExhausted = false（即不中断客户端重试）
func (sm *SuspensionManager) ShouldSuspend(ctx context.Context) bool {
	if sm.config == nil {
		return false
	}
	// 当所有渠道耗尽时始终挂起请求等待恢复，不中断客户端连接
	// 历史 ShouldInterruptClientOnExhausted 已废弃（始终返回 false），此处保持挂起语义
	return true
}

// WaitForGroupSwitch 挂起请求并等待组切换通知
// 迁移自 RetryHandler.waitForGroupSwitch，但移除状态管理部分，只保留挂起等待逻辑
// 返回是否成功切换到新组
func (sm *SuspensionManager) WaitForGroupSwitch(ctx context.Context, connID string) bool {
	failedEndpoint := ""
	// 检查配置和管理器是否存在
	if sm.config == nil {
		slog.InfoContext(ctx, "🔍 [挂起等待] 配置为空，无法挂起请求")
		return false
	}
	if sm.groupManager == nil {
		slog.InfoContext(ctx, "🔍 [挂起等待] 组管理器为空，无法挂起请求")
		return false
	}
	if sm.endpointManager == nil {
		slog.InfoContext(ctx, "🔍 [挂起等待] 端点管理器为空，无法挂起请求")
		return false
	}
	// 增加挂起请求计数
	sm.suspendedRequestsMutex.Lock()
	sm.suspendedRequestsCount++
	currentCount := sm.suspendedRequestsCount
	sm.suspendedRequestsMutex.Unlock()

	// 确保在退出时减少计数
	defer func() {
		sm.suspendedRequestsMutex.Lock()
		sm.suspendedRequestsCount--
		if failedEndpoint != "" && sm.suspendedEndpointCounts != nil {
			if count, exists := sm.suspendedEndpointCounts[failedEndpoint]; exists {
				if count <= 1 {
					delete(sm.suspendedEndpointCounts, failedEndpoint)
				} else {
					sm.suspendedEndpointCounts[failedEndpoint] = count - 1
				}
			}
		}
		newCount := sm.suspendedRequestsCount
		sm.suspendedRequestsMutex.Unlock()
		slog.InfoContext(ctx, fmt.Sprintf("⬇️ [挂起结束] 连接 %s 请求挂起结束，当前挂起数: %d", connID, newCount))
	}()

	slog.InfoContext(ctx, fmt.Sprintf("⏸️ [请求挂起] 连接 %s 请求已挂起，等待组切换 (当前挂起数: %d)", connID, currentCount))

	// 订阅组切换通知
	groupChangeNotify := sm.groupManager.SubscribeToGroupChanges()
	defer func() {
		// 确保清理订阅，防止内存泄漏
		sm.groupManager.UnsubscribeFromGroupChanges(groupChangeNotify)
		slog.DebugContext(ctx, fmt.Sprintf("🔌 [订阅清理] 连接 %s 组切换通知订阅已清理", connID))
	}()

	// 创建超时context
	timeout := sm.config.Failover.GetSuspendTimeout()
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	slog.InfoContext(ctx, fmt.Sprintf("⏰ [挂起超时] 连接 %s 挂起超时设置: %v，等待组切换通知...", connID, timeout))

	// 等待组切换通知或超时
	select {
	case newGroupName := <-groupChangeNotify:
		// 收到组切换通知
		slog.InfoContext(ctx, fmt.Sprintf("📡 [组切换通知] 连接 %s 收到组切换通知: %s，验证新组可用性", connID, newGroupName))

		// 验证新激活的组是否有健康端点
		newEndpoints := sm.endpointManager.GetHealthyEndpoints()
		if len(newEndpoints) > 0 {
			slog.InfoContext(ctx, fmt.Sprintf("✅ [切换成功] 连接 %s 新组 %s 有 %d 个健康端点，恢复请求处理",
				connID, newGroupName, len(newEndpoints)))
			return true
		} else {
			slog.WarnContext(ctx, fmt.Sprintf("⚠️ [切换无效] 连接 %s 新组 %s 暂无健康端点，挂起失败",
				connID, newGroupName))
			return false
		}

	case <-timeoutCtx.Done():
		// 挂起超时
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			slog.WarnContext(ctx, fmt.Sprintf("⏰ [挂起超时] 连接 %s 挂起等待超时 (%v)，停止等待", connID, timeout))
		} else {
			slog.InfoContext(ctx, fmt.Sprintf("🔄 [上下文取消] 连接 %s 挂起期间上下文被取消", connID))
		}
		return false

	case <-ctx.Done():
		// 原始请求被取消
		switch ctxErr := ctx.Err(); {
		case errors.Is(ctxErr, context.Canceled):
			slog.InfoContext(ctx, fmt.Sprintf("❌ [请求取消] 连接 %s 原始请求被客户端取消，结束挂起", connID))
		case errors.Is(ctxErr, context.DeadlineExceeded):
			slog.InfoContext(ctx, fmt.Sprintf("⏰ [请求超时] 连接 %s 原始请求上下文超时，结束挂起", connID))
		default:
			slog.InfoContext(ctx, fmt.Sprintf("❌ [请求异常] 连接 %s 原始请求上下文异常: %v，结束挂起", connID, ctxErr))
		}
		return false
	}
}

// GetSuspendedRequestsCount 返回当前挂起的请求数量
func (sm *SuspensionManager) GetSuspendedRequestsCount() int {
	sm.suspendedRequestsMutex.RLock()
	defer sm.suspendedRequestsMutex.RUnlock()
	return sm.suspendedRequestsCount
}

// GetSuspendedEndpointCounts 返回当前挂起请求的端点计数
func (sm *SuspensionManager) GetSuspendedEndpointCounts() map[string]int {
	sm.suspendedRequestsMutex.RLock()
	defer sm.suspendedRequestsMutex.RUnlock()

	if len(sm.suspendedEndpointCounts) == 0 {
		return map[string]int{}
	}

	result := make(map[string]int, len(sm.suspendedEndpointCounts))
	for endpointName, count := range sm.suspendedEndpointCounts {
		result[endpointName] = count
	}
	return result
}

// BeginManualRetry 记录“等待用户操作”的挂起请求（达到最大循环轮数）
func (sm *SuspensionManager) BeginManualRetry(requestID string, channels []string) {
	if requestID == "" || len(channels) == 0 {
		return
	}

	uniqueChannels := make([]string, 0, len(channels))
	seen := make(map[string]struct{}, len(channels))
	for _, name := range channels {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		uniqueChannels = append(uniqueChannels, name)
	}
	if len(uniqueChannels) == 0 {
		return
	}

	sm.manualRetryMutex.Lock()
	defer sm.manualRetryMutex.Unlock()

	if sm.manualRetryRequests == nil {
		sm.manualRetryRequests = make(map[string][]string)
	}
	if sm.manualRetryChannelCounts == nil {
		sm.manualRetryChannelCounts = make(map[string]int)
	}
	if _, exists := sm.manualRetryRequests[requestID]; exists {
		return
	}

	sm.manualRetryRequests[requestID] = uniqueChannels
	sm.manualRetryRequestsCount++
	for _, name := range uniqueChannels {
		sm.manualRetryChannelCounts[name]++
	}
}

// EndManualRetry 结束“等待用户操作”的挂起请求
func (sm *SuspensionManager) EndManualRetry(requestID string) {
	if requestID == "" {
		return
	}

	sm.manualRetryMutex.Lock()
	defer sm.manualRetryMutex.Unlock()

	channels, exists := sm.manualRetryRequests[requestID]
	if !exists {
		return
	}
	delete(sm.manualRetryRequests, requestID)
	if sm.manualRetryRequestsCount > 0 {
		sm.manualRetryRequestsCount--
	}
	for _, name := range channels {
		if name == "" {
			continue
		}
		if count, ok := sm.manualRetryChannelCounts[name]; ok {
			if count <= 1 {
				delete(sm.manualRetryChannelCounts, name)
			} else {
				sm.manualRetryChannelCounts[name] = count - 1
			}
		}
	}
}

// GetManualRetryRequestsCount 获取等待用户操作的挂起请求数
func (sm *SuspensionManager) GetManualRetryRequestsCount() int {
	sm.manualRetryMutex.RLock()
	defer sm.manualRetryMutex.RUnlock()
	return sm.manualRetryRequestsCount
}

// GetManualRetryChannelCounts 获取等待用户操作的渠道计数
func (sm *SuspensionManager) GetManualRetryChannelCounts() map[string]int {
	sm.manualRetryMutex.RLock()
	defer sm.manualRetryMutex.RUnlock()

	if len(sm.manualRetryChannelCounts) == 0 {
		return map[string]int{}
	}

	result := make(map[string]int, len(sm.manualRetryChannelCounts))
	for name, count := range sm.manualRetryChannelCounts {
		result[name] = count
	}
	return result
}

// UpdateConfig 更新配置
func (sm *SuspensionManager) UpdateConfig(cfg *config.Config) {
	sm.config = cfg
}

// WaitForEndpointRecovery 挂起请求并等待端点恢复或组切换通知
// 新增的端点自愈功能：监听指定端点的恢复信号
// 参数：
//   - ctx: 上下文
//   - connID: 连接ID
//   - failedEndpoint: 失败的端点名称
//
// 返回：是否成功恢复（端点恢复或组切换）
func (sm *SuspensionManager) WaitForEndpointRecovery(ctx context.Context, connID, failedEndpoint string) bool {
	// 检查配置和管理器是否存在
	if sm.config == nil {
		slog.InfoContext(ctx, "🔍 [端点恢复等待] 配置为空，无法挂起请求")
		return false
	}
	if sm.groupManager == nil {
		slog.InfoContext(ctx, "🔍 [端点恢复等待] 组管理器为空，无法挂起请求")
		return false
	}
	if sm.endpointManager == nil {
		slog.InfoContext(ctx, "🔍 [端点恢复等待] 端点管理器为空，无法挂起请求")
		return false
	}

	// 增加挂起请求计数
	sm.suspendedRequestsMutex.Lock()
	sm.suspendedRequestsCount++
	if failedEndpoint != "" {
		if sm.suspendedEndpointCounts == nil {
			sm.suspendedEndpointCounts = make(map[string]int)
		}
		sm.suspendedEndpointCounts[failedEndpoint]++
	}
	currentCount := sm.suspendedRequestsCount
	sm.suspendedRequestsMutex.Unlock()

	// 确保在退出时减少计数
	defer func() {
		sm.suspendedRequestsMutex.Lock()
		sm.suspendedRequestsCount--
		if failedEndpoint != "" && sm.suspendedEndpointCounts != nil {
			if count, exists := sm.suspendedEndpointCounts[failedEndpoint]; exists {
				if count <= 1 {
					delete(sm.suspendedEndpointCounts, failedEndpoint)
				} else {
					sm.suspendedEndpointCounts[failedEndpoint] = count - 1
				}
			}
		}
		newCount := sm.suspendedRequestsCount
		sm.suspendedRequestsMutex.Unlock()
		slog.InfoContext(ctx, fmt.Sprintf("⬇️ [挂起结束] 连接 %s 请求挂起结束，当前挂起数: %d", connID, newCount))
	}()

	slog.InfoContext(ctx, fmt.Sprintf("⏸️ [端点恢复挂起] 连接 %s 请求已挂起，等待端点 %s 恢复或组切换 (当前挂起数: %d)",
		connID, failedEndpoint, currentCount))

	// 创建超时context
	timeout := sm.config.Failover.GetSuspendTimeout()
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	slog.InfoContext(ctx, fmt.Sprintf("⏰ [挂起超时] 连接 %s 挂起超时设置: %v，等待端点 %s 恢复或组切换...",
		connID, timeout, failedEndpoint))

	// 订阅端点恢复信号（如果有恢复信号管理器）
	var endpointRecoveryCh chan string
	if sm.recoverySignalManager != nil && failedEndpoint != "" {
		endpointRecoveryCh = sm.recoverySignalManager.Subscribe(failedEndpoint)
		defer func() {
			if endpointRecoveryCh != nil {
				sm.recoverySignalManager.Unsubscribe(failedEndpoint, endpointRecoveryCh)
			}
		}()
	}

	// 订阅组切换通知
	groupChangeNotify := sm.groupManager.SubscribeToGroupChanges()
	defer func() {
		// 确保清理订阅，防止内存泄漏
		sm.groupManager.UnsubscribeFromGroupChanges(groupChangeNotify)
		slog.DebugContext(ctx, fmt.Sprintf("🔌 [订阅清理] 连接 %s 组切换通知订阅已清理", connID))
	}()

	// 等待恢复信号：端点恢复 > 组切换 > 超时
	for {
		select {
		case recoveredEndpoint := <-endpointRecoveryCh:
			// 🚀 [优先级1] 端点恢复信号 - 立即重试原端点
			slog.InfoContext(ctx, fmt.Sprintf("🎯 [端点自愈] 连接 %s 端点 %s 已恢复，立即重试原端点",
				connID, recoveredEndpoint))
			return true

		case newGroupName := <-groupChangeNotify:
			// 🔄 [优先级2] 组切换通知
			slog.InfoContext(ctx, fmt.Sprintf("📡 [组切换通知] 连接 %s 收到组切换通知: %s，验证新组可用性",
				connID, newGroupName))

			// 验证新激活的组是否有健康端点
			newEndpoints := sm.endpointManager.GetHealthyEndpoints()
			if len(newEndpoints) > 0 {
				slog.InfoContext(ctx, fmt.Sprintf("✅ [切换成功] 连接 %s 新组 %s 有 %d 个健康端点，恢复请求处理",
					connID, newGroupName, len(newEndpoints)))
				return true
			} else {
				slog.WarnContext(ctx, fmt.Sprintf("⚠️ [切换无效] 连接 %s 新组 %s 暂无健康端点，继续等待",
					connID, newGroupName))
				// 继续等待其他恢复信号
			}

		case <-timeoutCtx.Done():
			// ⏰ [优先级3] 挂起超时
			if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
				slog.WarnContext(ctx, fmt.Sprintf("⏰ [挂起超时] 连接 %s 挂起等待超时 (%v)，停止等待", connID, timeout))
			} else {
				slog.InfoContext(ctx, fmt.Sprintf("🔄 [上下文取消] 连接 %s 挂起期间上下文被取消", connID))
			}
			return false

		case <-ctx.Done():
			// ❌ [优先级4] 原始请求被取消
			switch ctxErr := ctx.Err(); {
			case errors.Is(ctxErr, context.Canceled):
				slog.InfoContext(ctx, fmt.Sprintf("❌ [请求取消] 连接 %s 原始请求被客户端取消，结束挂起", connID))
			case errors.Is(ctxErr, context.DeadlineExceeded):
				slog.InfoContext(ctx, fmt.Sprintf("⏰ [请求超时] 连接 %s 原始请求上下文超时，结束挂起", connID))
			default:
				slog.InfoContext(ctx, fmt.Sprintf("❌ [请求异常] 连接 %s 原始请求上下文异常: %v，结束挂起", connID, ctxErr))
			}
			return false
		}
	}
}

// WaitForEndpointRecoveryWithResult 🎯 [挂起取消区分] 带结果的端点恢复等待方法
// 功能与WaitForEndpointRecovery相同，但返回详细的结果类型以区分成功、超时、取消
// 这是对现有方法的增强版本，保持向后兼容性
// 当渠道间转移禁用时，仅监听用户点击"重试"按钮的组切换通知，不监听端点自愈信号
func (sm *SuspensionManager) WaitForEndpointRecoveryWithResult(ctx context.Context, connID, failedEndpoint string, requireNonStream bool) handlers.SuspensionResult {
	return sm.waitForEndpointRecoveryWithResult(ctx, connID, failedEndpoint, false, requireNonStream)
}

// WaitForEndpointRecoveryWithResultManual 🛑 [循环终止] 仅等待用户操作的挂起
func (sm *SuspensionManager) WaitForEndpointRecoveryWithResultManual(ctx context.Context, connID, failedEndpoint string, requireNonStream bool) handlers.SuspensionResult {
	return sm.waitForEndpointRecoveryWithResult(ctx, connID, failedEndpoint, true, requireNonStream)
}

// hasResumableEndpoint 判断当前是否存在能承接该挂起请求的健康端点。
// requireNonStream=true 时跳过 stream_only 端点：否则唯一健康端点是 stream_only 时
// 会把挂起的非流请求错误唤醒，醒来后拿到空端点列表，白跑一轮渠道转移。
func (sm *SuspensionManager) hasResumableEndpoint(requireNonStream bool) bool {
	for _, ep := range sm.endpointManager.GetHealthyEndpoints() {
		if requireNonStream && ep.Config.StreamOnly {
			continue
		}
		return true
	}
	return false
}

func (sm *SuspensionManager) waitForEndpointRecoveryWithResult(ctx context.Context, connID, failedEndpoint string, manualOnly, requireNonStream bool) handlers.SuspensionResult {
	// 检查配置和管理器是否存在
	if sm.config == nil {
		slog.InfoContext(ctx, "🔍 [端点恢复等待] 配置为空，无法挂起请求")
		return handlers.SuspensionTimeout
	}
	if sm.groupManager == nil {
		slog.InfoContext(ctx, "🔍 [端点恢复等待] 组管理器为空，无法挂起请求")
		return handlers.SuspensionTimeout
	}
	if sm.endpointManager == nil {
		slog.InfoContext(ctx, "🔍 [端点恢复等待] 端点管理器为空，无法挂起请求")
		return handlers.SuspensionTimeout
	}

	// 增加挂起请求计数
	sm.suspendedRequestsMutex.Lock()
	sm.suspendedRequestsCount++
	if failedEndpoint != "" {
		if sm.suspendedEndpointCounts == nil {
			sm.suspendedEndpointCounts = make(map[string]int)
		}
		sm.suspendedEndpointCounts[failedEndpoint]++
	}
	currentCount := sm.suspendedRequestsCount
	sm.suspendedRequestsMutex.Unlock()

	waitStartTime := time.Now()

	// 确保在退出时减少计数
	defer func() {
		sm.suspendedRequestsMutex.Lock()
		sm.suspendedRequestsCount--
		if failedEndpoint != "" && sm.suspendedEndpointCounts != nil {
			if count, exists := sm.suspendedEndpointCounts[failedEndpoint]; exists {
				if count <= 1 {
					delete(sm.suspendedEndpointCounts, failedEndpoint)
				} else {
					sm.suspendedEndpointCounts[failedEndpoint] = count - 1
				}
			}
		}
		newCount := sm.suspendedRequestsCount
		sm.suspendedRequestsMutex.Unlock()
		slog.InfoContext(ctx, fmt.Sprintf("⬇️ [挂起结束] 连接 %s 请求挂起结束，当前挂起数: %d", connID, newCount))
	}()

	// 根据渠道间转移配置决定等待模式
	isChannelFailoverEnabled := sm.endpointManager != nil && sm.endpointManager.IsFailoverEnabled()
	allowEndpointRecovery := isChannelFailoverEnabled && !manualOnly
	if manualOnly {
		slog.InfoContext(ctx, fmt.Sprintf("⏸️ [循环终止等待] 连接 %s 请求已挂起，等待用户操作 (当前挂起数: %d)",
			connID, currentCount))
	} else if allowEndpointRecovery {
		slog.InfoContext(ctx, fmt.Sprintf("⏸️ [端点恢复挂起] 连接 %s 请求已挂起，等待端点 %s 恢复或组切换 (当前挂起数: %d)",
			connID, failedEndpoint, currentCount))
	} else {
		slog.InfoContext(ctx, fmt.Sprintf("⏸️ [重试按钮等待] 连接 %s 请求已挂起，等待用户点击重试按钮 (当前挂起数: %d)",
			connID, currentCount))
	}

	// 创建超时context
	timeout := sm.config.Failover.GetSuspendTimeout()
	if manualOnly {
		timeout = sm.config.Failover.GetLoopRetrySuspendTimeout()
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	slog.InfoContext(ctx, fmt.Sprintf("⏰ [挂起超时] 连接 %s 挂起超时设置: %v", connID, timeout))

	// 仅当渠道间转移启用时才订阅端点恢复信号
	// 当渠道间转移禁用/手动等待时，仅监听用户点击"重试"按钮的组切换通知
	var endpointRecoveryCh chan string
	if allowEndpointRecovery && sm.recoverySignalManager != nil && failedEndpoint != "" {
		endpointRecoveryCh = sm.recoverySignalManager.Subscribe(failedEndpoint)
		defer func() {
			if endpointRecoveryCh != nil {
				sm.recoverySignalManager.Unsubscribe(failedEndpoint, endpointRecoveryCh)
			}
		}()
	}

	// 订阅组切换通知（用户点击"重试"按钮会触发此通知）
	groupChangeNotify := sm.groupManager.SubscribeToGroupChanges()
	defer func() {
		// 确保清理订阅，防止内存泄漏
		sm.groupManager.UnsubscribeFromGroupChanges(groupChangeNotify)
		slog.DebugContext(ctx, fmt.Sprintf("🔌 [订阅清理] 连接 %s 组切换通知订阅已清理", connID))
	}()

	// 等待恢复信号
	for {
		select {
		case recoveredEndpoint := <-endpointRecoveryCh:
			// 🚀 [优先级1] 端点恢复信号 - 仅当渠道间转移启用时有效
			slog.InfoContext(ctx, fmt.Sprintf("🎯 [端点自愈] 连接 %s 端点 %s 已恢复，立即重试原端点",
				connID, recoveredEndpoint))
			return handlers.SuspensionSuccess

		case newGroupName := <-groupChangeNotify:
			// 🔄 [优先级2] 组切换通知（用户点击重试按钮触发）
			if manualOnly && !sm.groupManager.WasGroupManuallyActivatedSince(newGroupName, waitStartTime) {
				slog.DebugContext(ctx, fmt.Sprintf("⏸️ [循环终止等待] 连接 %s 忽略非手动组切换通知: %s", connID, newGroupName))
				continue
			}
			slog.InfoContext(ctx, fmt.Sprintf("📡 [组切换通知] 连接 %s 收到组切换通知: %s，验证新组可用性",
				connID, newGroupName))

			// 验证新激活的组是否有能承接本请求的健康端点
			if sm.hasResumableEndpoint(requireNonStream) {
				slog.InfoContext(ctx, fmt.Sprintf("✅ [切换成功] 连接 %s 新组 %s 有可用端点，恢复请求处理",
					connID, newGroupName))
				return handlers.SuspensionSuccess
			} else {
				slog.WarnContext(ctx, fmt.Sprintf("⚠️ [切换无效] 连接 %s 新组 %s 暂无可承接该请求的端点，继续等待",
					connID, newGroupName))
				// 继续等待其他恢复信号
			}

		case <-timeoutCtx.Done():
			// ⏰ [优先级3] 挂起超时
			if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
				slog.WarnContext(ctx, fmt.Sprintf("⏰ [挂起超时] 连接 %s 挂起等待超时 (%v)，停止等待", connID, timeout))
			} else {
				slog.InfoContext(ctx, fmt.Sprintf("🔄 [上下文取消] 连接 %s 挂起期间超时上下文被取消", connID))
			}
			return handlers.SuspensionTimeout

		case <-ctx.Done():
			// ❌ [优先级4] 原始请求被取消
			switch ctxErr := ctx.Err(); {
			case errors.Is(ctxErr, context.Canceled):
				slog.InfoContext(ctx, fmt.Sprintf("❌ [请求取消] 连接 %s 原始请求被客户端取消，结束挂起", connID))
				return handlers.SuspensionCancelled
			case errors.Is(ctxErr, context.DeadlineExceeded):
				slog.InfoContext(ctx, fmt.Sprintf("⏰ [请求超时] 连接 %s 原始请求上下文超时，结束挂起", connID))
				return handlers.SuspensionTimeout
			default:
				slog.InfoContext(ctx, fmt.Sprintf("❌ [请求异常] 连接 %s 原始请求上下文异常: %v，结束挂起", connID, ctxErr))
				return handlers.SuspensionCancelled
			}
		}
	}
}
