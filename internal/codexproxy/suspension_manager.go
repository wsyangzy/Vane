// suspension_manager.go - Codex 请求挂起队列
// 当 loop_retry 全部轮次耗尽后，将请求挂起等待运维通过 UI 重新激活渠道/端点。
//
// 设计原则：
//   - 单激活渠道模型下，挂起仅在该渠道内端点全部失败时触发
//   - 运维在桌面 UI 点击渠道/端点激活按钮 → BroadcastRecovery 唤醒所有挂起请求
//   - 挂起数有上限（maxSuspended），防止挂起队列无限增长导致内存压力
//   - 单次挂起有超时（timeout），防止请求永久阻塞

package codexproxy

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// SuspensionManager Codex 请求挂起管理器
type SuspensionManager struct {
	mu             sync.Mutex
	suspendedCount int
	maxSuspended   int                      // 最大挂起请求数（0 = 无限制）
	defaultTimeout time.Duration            // 默认超时（来自 failover.loop_retry_wait_timeout）
	waiters        map[string]chan struct{} // connID → 唤醒通道
	logger         *slog.Logger

	// onCountChange 挂起数变化时的回调（由 App 注入，用于推送前端事件）
	onCountChange func(count int)
}

// NewSuspensionManager 创建挂起管理器
func NewSuspensionManager(maxSuspended int, defaultTimeout time.Duration, logger *slog.Logger) *SuspensionManager {
	if logger == nil {
		logger = slog.Default()
	}
	if defaultTimeout <= 0 {
		defaultTimeout = 120 * time.Second
	}
	return &SuspensionManager{
		maxSuspended:   maxSuspended,
		defaultTimeout: defaultTimeout,
		waiters:        make(map[string]chan struct{}),
		logger:         logger,
	}
}

// SetOnCountChange 注册挂起数变化回调
func (sm *SuspensionManager) SetOnCountChange(cb func(count int)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onCountChange = cb
}

// GetSuspendedCount 获取当前挂起请求数（供 API/UI 展示）
func (sm *SuspensionManager) GetSuspendedCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.suspendedCount
}

// SuspendResult Suspend 的返回结果
type SuspendResult int

const (
	// SuspendRecovered 被运维 UI 操作唤醒（应重试请求）
	SuspendRecovered SuspendResult = iota
	// SuspendTimedOut 达到 timeout 未被唤醒（应返回 503）
	SuspendTimedOut
	// SuspendCanceled 客户端取消（应直接终止）
	SuspendCanceled
	// SuspendRejected 已达 maxSuspended 上限，未进入队列（应立即返回 503）
	SuspendRejected
)

// Suspend 将请求挂起，阻塞等待 BroadcastRecovery / ctx.Done() / 超时三者之一
//
// 参数：
//   - ctx:     请求上下文（用于检测客户端取消）
//   - connID:  连接/请求标识（用于日志和去重）
//   - timeout: 本次挂起的最大等待时间（<=0 时用 defaultTimeout）
//
// 返回 SuspendResult 枚举，调用方按此决定下一步行为
func (sm *SuspensionManager) Suspend(ctx context.Context, connID string, timeout time.Duration) SuspendResult {
	if timeout <= 0 {
		timeout = sm.defaultTimeout
	}

	// 1. 注册到等待队列（检查上限）
	sm.mu.Lock()
	if sm.maxSuspended > 0 && sm.suspendedCount >= sm.maxSuspended {
		sm.mu.Unlock()
		sm.logger.Warn("[Codex 挂起拒绝] 已达最大挂起请求数",
			"conn_id", connID,
			"current", sm.suspendedCount,
			"max", sm.maxSuspended,
		)
		return SuspendRejected
	}
	ch := make(chan struct{}, 1)
	sm.waiters[connID] = ch
	sm.suspendedCount++
	currentCount := sm.suspendedCount
	cb := sm.onCountChange
	sm.mu.Unlock()

	sm.logger.Info("[Codex 请求挂起] 进入挂起等待",
		"conn_id", connID,
		"current_count", currentCount,
		"timeout", timeout.String(),
	)
	if cb != nil {
		go cb(currentCount)
	}

	// 2. 离开时清理（无论何种结果）
	defer func() {
		sm.mu.Lock()
		if existing, ok := sm.waiters[connID]; ok && existing == ch {
			delete(sm.waiters, connID)
			sm.suspendedCount--
		}
		newCount := sm.suspendedCount
		cb := sm.onCountChange
		sm.mu.Unlock()
		sm.logger.Info("[Codex 挂起结束] 请求离开挂起队列",
			"conn_id", connID,
			"current_count", newCount,
		)
		if cb != nil {
			go cb(newCount)
		}
	}()

	// 3. 等待唤醒、超时或客户端取消
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ch:
		sm.logger.Info("[Codex 挂起唤醒] 收到恢复信号",
			"conn_id", connID,
		)
		return SuspendRecovered
	case <-timer.C:
		sm.logger.Warn("[Codex 挂起超时] 等待超时",
			"conn_id", connID,
			"timeout", timeout.String(),
		)
		return SuspendTimedOut
	case <-ctx.Done():
		sm.logger.Info("[Codex 挂起取消] 客户端取消",
			"conn_id", connID,
			"reason", ctx.Err().Error(),
		)
		return SuspendCanceled
	}
}

// BroadcastRecovery 唤醒所有挂起请求
// 由 App 在运维点击渠道/端点激活按钮时调用
func (sm *SuspensionManager) BroadcastRecovery() {
	sm.mu.Lock()
	count := len(sm.waiters)
	// 复制 waiters 列表后释放锁，避免长时间持锁
	chs := make([]chan struct{}, 0, count)
	for _, ch := range sm.waiters {
		chs = append(chs, ch)
	}
	sm.mu.Unlock()

	if count == 0 {
		return
	}

	sm.logger.Info("[Codex 挂起广播] 唤醒所有挂起请求",
		"count", count,
	)

	// 非阻塞发送（chan 容量为 1，已存在信号时跳过即可）
	for _, ch := range chs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
