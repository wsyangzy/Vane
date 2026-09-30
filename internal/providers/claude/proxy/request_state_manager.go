package proxy

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// RequestStateManager 请求状态管理器
// 🎯 [SRP重构] 2025-12-26: 专注于状态机和元数据管理
type RequestStateManager struct {
	// 基础元数据
	requestID string
	startTime time.Time

	// 端点信息（需要线程安全）
	endpointMu      sync.RWMutex
	endpointName    string
	groupName       string
	channel         string
	endpointChainMu sync.RWMutex
	endpointChain   []ChainNode

	// 状态信息
	stateMu         sync.RWMutex
	status          string // pending, forwarding, streaming, processing, completed, failed, cancelled
	isCompleted     bool
	finalStatusCode int

	// 计数器（需要线程安全）
	counterMu    sync.Mutex
	attemptCount int // 尝试次数（包含首次，从1开始）
	restartCount int // 重启次数（递归保护）
	maxRestarts  int // 最大重启次数

	// 错误信息（需要线程安全）
	errorMu   sync.RWMutex
	lastError error

	// 🔥 [重复计费防护] 2025-12-26
	hasOutputContent      bool
	hasOutputContentMutex sync.Mutex
}

// NewRequestStateManager 创建新的请求状态管理器
func NewRequestStateManager(requestID string) *RequestStateManager {
	return &RequestStateManager{
		requestID:   requestID,
		startTime:   time.Now(),
		status:      "pending",
		maxRestarts: 3, // 🔧 [会话保障] 2025-12-29: 设置为3次，平衡会话连续性与响应速度（总耗时约30秒）
	}
}

// ========== 状态管理方法 ==========

// UpdateStatus 更新请求状态
//
// 🔧 [P5.2 增强] 添加状态转移合法性日志告警：
//   - 不合法的状态值（拼写错误等）会告警但仍允许设置（向后兼容）
//   - 非法转移（如 completed → forwarding）会告警但仍允许设置
//   - 这样既能渐进发现状态使用问题，又不破坏现有行为
func (rsm *RequestStateManager) UpdateStatus(status string, httpStatus int) {
	rsm.stateMu.Lock()
	defer rsm.stateMu.Unlock()

	// 🔧 [P5.2] 状态合法性校验（仅告警，不阻断）
	if !isValidReqStatus(status) {
		slog.Warn("⚠️ [P5.2] 检测到非标准 status 值",
			"request_id", rsm.requestID,
			"from", rsm.status,
			"to", status,
			"valid_values", validReqStatusValues())
	} else if rsm.status != "" && !isValidReqStatusTransition(rsm.status, status) {
		slog.Warn("⚠️ [P5.2] 检测到非法状态转移",
			"request_id", rsm.requestID,
			"from", rsm.status,
			"to", status,
			"reason", "终态不应再变更（除 cancelled↔failed 例外）")
	}

	rsm.status = status
	if httpStatus > 0 {
		rsm.finalStatusCode = httpStatus
	}
}

// MarkCompletedOnce 标记请求已完成（幂等）
// 返回值: true 表示首次标记完成，false 表示已完成
func (rsm *RequestStateManager) MarkCompletedOnce() bool {
	rsm.stateMu.Lock()
	defer rsm.stateMu.Unlock()
	if rsm.isCompleted {
		return false
	}
	rsm.isCompleted = true
	return true
}

// MarkCompleted 标记请求已完成（兼容旧接口）
func (rsm *RequestStateManager) MarkCompleted() {
	rsm.MarkCompletedOnce()
}

// GetStatus 获取当前状态
func (rsm *RequestStateManager) GetStatus() string {
	rsm.stateMu.RLock()
	defer rsm.stateMu.RUnlock()
	return rsm.status
}

// IsCompleted 检查是否已完成
func (rsm *RequestStateManager) IsCompleted() bool {
	rsm.stateMu.RLock()
	defer rsm.stateMu.RUnlock()
	return rsm.isCompleted
}

// SetFinalStatusCode 设置最终HTTP状态码
func (rsm *RequestStateManager) SetFinalStatusCode(code int) {
	rsm.stateMu.Lock()
	defer rsm.stateMu.Unlock()
	rsm.finalStatusCode = code
}

// GetFinalStatusCode 获取最终HTTP状态码
func (rsm *RequestStateManager) GetFinalStatusCode() int {
	rsm.stateMu.RLock()
	defer rsm.stateMu.RUnlock()
	return rsm.finalStatusCode
}

// ========== 元数据管理方法 ==========

// SetEndpoint 设置端点信息
// 🔧 [并发安全修复] 2025-12-27: 添加写锁保护
func (rsm *RequestStateManager) SetEndpoint(endpointName, groupName, channel string) {
	rsm.endpointMu.Lock()
	defer rsm.endpointMu.Unlock()
	rsm.endpointName = endpointName
	rsm.groupName = groupName
	rsm.channel = channel
}

// ChainNode 链路节点（结构化存储）
type ChainNode struct {
	Hop             int    `json:"hop"`
	Channel         string `json:"channel"`
	CredentialGroup string `json:"credential_group,omitempty"`
	Endpoint        string `json:"endpoint"`
	// Attempts 本跳内实际发出的请求次数（含同端点重试）；整请求累计 = retry_count + 1
	Attempts int `json:"attempts,omitempty"`
	// StartMs 本跳开始时距请求开始的毫秒数；DurationMs 本跳耗时，下一跳开始时写入，
	// 最后一跳由热池在请求终态时补齐（tracking.closeOpenChainHop）。
	// 用相对毫秒而非时间戳：与 start_time 的时区格式无关，前端直接可用
	StartMs    int64  `json:"start_ms"`
	DurationMs *int64 `json:"duration_ms,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

// closeLastHopLocked 结束最后一跳的计时；调用方需持有 endpointChainMu
func (rsm *RequestStateManager) closeLastHopLocked(nowMs int64) {
	if n := len(rsm.endpointChain); n > 0 && rsm.endpointChain[n-1].DurationMs == nil {
		duration := nowMs - rsm.endpointChain[n-1].StartMs
		if duration < 0 {
			duration = 0
		}
		rsm.endpointChain[n-1].DurationMs = &duration
	}
}

// AppendEndpointChain 追加端点到链路（按切换去重）
// 返回最新链路 JSON 数组（形如 [{"channel":"A","credential_group":"G","endpoint":"B"}]）
func (rsm *RequestStateManager) AppendEndpointChain(channel, credentialGroup, endpointName string) string {
	if channel == "" && credentialGroup == "" && endpointName == "" {
		return rsm.GetEndpointChain()
	}

	rsm.endpointChainMu.Lock()
	defer rsm.endpointChainMu.Unlock()

	chainLen := len(rsm.endpointChain)
	if chainLen == 0 ||
		rsm.endpointChain[chainLen-1].Channel != channel ||
		rsm.endpointChain[chainLen-1].CredentialGroup != credentialGroup ||
		rsm.endpointChain[chainLen-1].Endpoint != endpointName {
		nowMs := time.Since(rsm.startTime).Milliseconds()
		rsm.closeLastHopLocked(nowMs)
		rsm.endpointChain = append(rsm.endpointChain, ChainNode{
			Hop:             chainLen + 1,
			Channel:         channel,
			CredentialGroup: credentialGroup,
			Endpoint:        endpointName,
			StartMs:         nowMs,
		})
	}

	data, _ := json.Marshal(rsm.endpointChain)
	return string(data)
}

// GetEndpointChain 获取端点链路 JSON 数组字符串
func (rsm *RequestStateManager) GetEndpointChain() string {
	rsm.endpointChainMu.RLock()
	defer rsm.endpointChainMu.RUnlock()
	if len(rsm.endpointChain) == 0 {
		return ""
	}
	data, _ := json.Marshal(rsm.endpointChain)
	return string(data)
}

func (rsm *RequestStateManager) AnnotateEndpointChainError(statusCode int, errorText string) string {
	if errorText == "" {
		return rsm.GetEndpointChain()
	}

	rsm.endpointChainMu.Lock()
	defer rsm.endpointChainMu.Unlock()
	if len(rsm.endpointChain) == 0 {
		return ""
	}
	last := &rsm.endpointChain[len(rsm.endpointChain)-1]
	last.StatusCode = statusCode
	last.Error = errorText

	data, _ := json.Marshal(rsm.endpointChain)
	return string(data)
}

// GetRequestID 获取请求ID
func (rsm *RequestStateManager) GetRequestID() string {
	return rsm.requestID
}

// GetEndpointName 获取端点名称
// 🔧 [并发安全修复] 2025-12-27: 添加读锁保护
func (rsm *RequestStateManager) GetEndpointName() string {
	rsm.endpointMu.RLock()
	defer rsm.endpointMu.RUnlock()
	return rsm.endpointName
}

// GetGroupName 获取组名称
// 🔧 [并发安全修复] 2025-12-27: 添加读锁保护
func (rsm *RequestStateManager) GetGroupName() string {
	rsm.endpointMu.RLock()
	defer rsm.endpointMu.RUnlock()
	return rsm.groupName
}

// GetChannel 获取渠道
// 🔧 [并发安全修复] 2025-12-27: 添加读锁保护
func (rsm *RequestStateManager) GetChannel() string {
	rsm.endpointMu.RLock()
	defer rsm.endpointMu.RUnlock()
	return rsm.channel
}

// GetDuration 获取请求持续时间
func (rsm *RequestStateManager) GetDuration() time.Duration {
	return time.Since(rsm.startTime)
}

// GetStartTime 获取开始时间
func (rsm *RequestStateManager) GetStartTime() time.Time {
	return rsm.startTime
}

// ========== 计数器管理方法 ==========

// IncrementAttempt 增加尝试次数并返回新值；同时计入当前链路节点，
// 让链路能还原每一跳各自发了几次（最终端点尝试次数由此而来）
func (rsm *RequestStateManager) IncrementAttempt() int {
	rsm.endpointChainMu.Lock()
	if n := len(rsm.endpointChain); n > 0 {
		rsm.endpointChain[n-1].Attempts++
	}
	rsm.endpointChainMu.Unlock()

	rsm.counterMu.Lock()
	defer rsm.counterMu.Unlock()
	rsm.attemptCount++
	return rsm.attemptCount
}

// GetAttemptCount 获取尝试次数
func (rsm *RequestStateManager) GetAttemptCount() int {
	rsm.counterMu.Lock()
	defer rsm.counterMu.Unlock()
	return rsm.attemptCount
}

// GetRetryCount 获取重试次数
// 🔧 [语义修复] 2025-12-27: 基于 attemptCount 计算，重试次数 = 总尝试次数 - 1
func (rsm *RequestStateManager) GetRetryCount() int {
	rsm.counterMu.Lock()
	defer rsm.counterMu.Unlock()
	if rsm.attemptCount <= 1 {
		return 0
	}
	return rsm.attemptCount - 1
}

// CanRestart 检查是否可以重启（递归保护）
func (rsm *RequestStateManager) CanRestart() bool {
	rsm.counterMu.Lock()
	defer rsm.counterMu.Unlock()
	return rsm.restartCount < rsm.maxRestarts
}

// IncrementRestart 增加重启次数并返回新值
func (rsm *RequestStateManager) IncrementRestart() int {
	rsm.counterMu.Lock()
	defer rsm.counterMu.Unlock()
	rsm.restartCount++
	return rsm.restartCount
}

// GetMaxRestarts 获取最大重启次数
func (rsm *RequestStateManager) GetMaxRestarts() int {
	rsm.counterMu.Lock()
	defer rsm.counterMu.Unlock()
	return rsm.maxRestarts
}

// ========== 错误管理方法 ==========

// SetLastError 设置最后错误
// 🔧 [并发安全修复] 2025-12-27: 添加写锁保护
func (rsm *RequestStateManager) SetLastError(err error) {
	rsm.errorMu.Lock()
	defer rsm.errorMu.Unlock()
	rsm.lastError = err
}

// GetLastError 获取最后错误
// 🔧 [并发安全修复] 2025-12-27: 添加读锁保护
func (rsm *RequestStateManager) GetLastError() error {
	rsm.errorMu.RLock()
	defer rsm.errorMu.RUnlock()
	return rsm.lastError
}

// ========== 重复计费防护 ==========

// MarkOutputSent 标记已输出内容
func (rsm *RequestStateManager) MarkOutputSent() {
	rsm.hasOutputContentMutex.Lock()
	defer rsm.hasOutputContentMutex.Unlock()
	rsm.hasOutputContent = true
}

// HasOutputContent 检查是否已输出内容
func (rsm *RequestStateManager) HasOutputContent() bool {
	rsm.hasOutputContentMutex.Lock()
	defer rsm.hasOutputContentMutex.Unlock()
	return rsm.hasOutputContent
}

// ========== 统计信息 ==========

// GetStats 获取状态统计信息
// 🔧 [语义修复] 2025-12-27: retry_count 基于 attemptCount 计算
// 🔧 [并发安全修复] 2025-12-27: 添加读锁保护 endpointName/channel
// 🔧 [模型来源统一] 2025-12-27: 移除 model_name（由 LifecycleManager 管理）
func (rsm *RequestStateManager) GetStats() map[string]any {
	rsm.stateMu.RLock()
	status := rsm.status
	isCompleted := rsm.isCompleted
	finalStatusCode := rsm.finalStatusCode
	rsm.stateMu.RUnlock()

	rsm.endpointMu.RLock()
	endpointName := rsm.endpointName
	channel := rsm.channel
	rsm.endpointMu.RUnlock()

	rsm.counterMu.Lock()
	attemptCount := rsm.attemptCount
	restartCount := rsm.restartCount
	// 计算重试次数：总尝试次数 - 1
	retryCount := 0
	if attemptCount > 1 {
		retryCount = attemptCount - 1
	}
	rsm.counterMu.Unlock()

	return map[string]any{
		"request_id":        rsm.requestID,
		"status":            status,
		"is_completed":      isCompleted,
		"endpoint_name":     endpointName,
		"channel":           channel,
		"attempt_count":     attemptCount,
		"retry_count":       retryCount,
		"restart_count":     restartCount,
		"duration_seconds":  rsm.GetDuration().Seconds(),
		"final_status_code": finalStatusCode,
	}
}
