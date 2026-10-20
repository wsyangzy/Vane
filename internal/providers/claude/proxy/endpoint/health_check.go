// health_check.go - 健康检查相关功能
// 包含定时健康检查、手动检查、批量检查等

package endpoint

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	display "github.com/wsyangzy/Vane/internal/platform/display"
)

// SetOnHealthCheckComplete 设置健康检查完成回调
// 用于 Wails 桌面应用在定时健康检查完成后推送事件到前端
func (m *Manager) SetOnHealthCheckComplete(fn func()) {
	m.onHealthCheckComplete = fn
}

// triggerHealthCheckCompleteDebounced 防抖触发健康检查完成回调
// 避免并发健康检查时多次触发回调导致日志膨胀
// 500ms 内多次调用只会执行最后一次
func (m *Manager) triggerHealthCheckCompleteDebounced() {
	if m.onHealthCheckComplete == nil {
		return
	}

	m.healthCheckDebounceMu.Lock()
	defer m.healthCheckDebounceMu.Unlock()

	// 取消之前的定时器（如果存在）
	if m.healthCheckDebounceTimer != nil {
		m.healthCheckDebounceTimer.Stop()
	}

	// 设置新的定时器，500ms 后触发回调
	m.healthCheckDebounceTimer = time.AfterFunc(500*time.Millisecond, func() {
		if m.onHealthCheckComplete != nil {
			m.onHealthCheckComplete()
		}
	})
}

// refreshGroupActivation 刷新组激活状态
// 当端点健康状态变化时调用，用于重新评估哪些组应该被激活
// 解决新增端点后不会自动激活的问题
// 使用防抖机制触发回调，避免并发检查时多次触发
func (m *Manager) refreshGroupActivation() {
	// 防御性检查：确保 groupManager 已初始化
	if m.groupManager == nil {
		return
	}

	m.endpointsMu.RLock()
	snapshot := make([]*Endpoint, len(m.endpoints))
	copy(snapshot, m.endpoints)
	m.endpointsMu.RUnlock()

	m.groupManager.UpdateGroups(snapshot)
	slog.Debug("🔄 [组管理] 端点健康状态变化，已刷新组激活状态")

	// 使用防抖机制触发健康检查完成回调
	m.triggerHealthCheckCompleteDebounced()
}

// healthCheckLoop runs the health check routine
func (m *Manager) healthCheckLoop() {
	defer m.wg.Done()

	// 获取当前检查间隔
	getCheckInterval := func() time.Duration {
		interval := m.config.Health.CheckInterval
		if interval <= 0 {
			interval = 30 * time.Second // 默认30秒检查一次
		}
		return interval
	}

	currentInterval := getCheckInterval()
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	// Initial health check
	m.performHealthChecks()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.performHealthChecks()

			// 检查配置是否变化，动态调整间隔
			newInterval := getCheckInterval()
			if newInterval != currentInterval {
				slog.Info("🔄 [健康检查] 间隔已更新", "old", currentInterval, "new", newInterval)
				currentInterval = newInterval
				ticker.Reset(currentInterval)
			}
		}
	}
}

// performHealthChecks performs health checks on all endpoints
func (m *Manager) performHealthChecks() {
	// 循环重试模式下，如果配置了跳过健康检测，则不执行定时健康检查
	if m.config.Failover.ShouldSkipHealthCheck() {
		slog.Debug("🩺 [健康检查] 循环重试模式已启用跳过检测，跳过定时健康检查")
		return
	}

	// 使用快照机制，不阻塞动态增删操作
	m.endpointsMu.RLock()
	snapshot := make([]*Endpoint, len(m.endpoints))
	copy(snapshot, m.endpoints)
	m.endpointsMu.RUnlock()

	var endpointsToCheck []*Endpoint

	if len(snapshot) == 0 {
		slog.Debug("🩺 [健康检查] 没有配置的端点，跳过健康检查")
		return
	}

	// 健康检查覆盖范围（与 TODO.md 的资源优化要求对齐）：
	// - 启用“渠道间故障转移”时：检查所有“参与故障转移”的渠道，且仅检查其中参与故障转移的端点
	// - 未启用时：只检查当前活跃渠道中参与故障转移的端点
	if m.IsFailoverEnabled() {
		for _, ep := range snapshot {
			if ep == nil {
				continue
			}
			// 渠道级开关：暂停渠道不参与跨渠道故障转移，也不做健康检查
			if m.groupManager != nil && !m.groupManager.IsChannelFailoverEnabled(ChannelKey(ep)) {
				continue
			}
			// 端点级开关：不参与故障转移的端点不做健康检查
			failoverEnabled := true
			if ep.Config.FailoverEnabled != nil {
				failoverEnabled = *ep.Config.FailoverEnabled
			}
			if !failoverEnabled {
				continue
			}
			endpointsToCheck = append(endpointsToCheck, ep)
		}
		channelSet := make(map[string]struct{}, len(endpointsToCheck))
		for _, ep := range endpointsToCheck {
			key := ChannelKey(ep)
			if key == "" {
				continue
			}
			channelSet[key] = struct{}{}
		}
		slog.Debug(fmt.Sprintf("🩺 [健康检查] 渠道间故障转移模式：检查 %d 个渠道（%d 个参与端点）（总端点 %d）",
			len(channelSet), len(endpointsToCheck), len(snapshot)))
	} else {
		active := m.groupManager.FilterEndpointsByActiveGroups(snapshot)
		for _, ep := range active {
			if ep == nil {
				continue
			}
			failoverEnabled := true
			if ep.Config.FailoverEnabled != nil {
				failoverEnabled = *ep.Config.FailoverEnabled
			}
			if !failoverEnabled {
				continue
			}
			endpointsToCheck = append(endpointsToCheck, ep)
		}
		channelSet := make(map[string]struct{}, len(endpointsToCheck))
		for _, ep := range endpointsToCheck {
			key := ChannelKey(ep)
			if key == "" {
				continue
			}
			channelSet[key] = struct{}{}
		}
		slog.Debug(fmt.Sprintf("🩺 [健康检查] 非故障转移模式：检查 %d 个活跃渠道（%d 个参与端点）（总端点 %d）",
			len(channelSet), len(endpointsToCheck), len(snapshot)))
	}

	if len(endpointsToCheck) == 0 {
		slog.Debug("🩺 [健康检查] 没有需要检查的端点，跳过健康检查")
		return
	}

	var wg sync.WaitGroup

	// Check the determined endpoints based on mode
	for _, endpoint := range endpointsToCheck {
		wg.Add(1)
		go func(ep *Endpoint) {
			defer wg.Done()
			m.checkEndpointHealth(ep)
		}(endpoint)
	}

	wg.Wait()

	// Count healthy endpoints after checks
	healthyCount := 0
	for _, ep := range endpointsToCheck {
		if ep.IsHealthy() {
			healthyCount++
		}
	}

	// 使用防抖机制触发回调，避免与 updateEndpointStatus 中的回调重复
	m.triggerHealthCheckCompleteDebounced()
}

// checkEndpointHealth checks the health of a single endpoint
//
// 🔧 [P5.4 新增] 经过 healthThrottleAllow 节流（外部调用方应通过 checkEndpointHealthForced 绕过节流）
// 注意：本函数自身不再做节流，由各触发源在调用前显式判断
func (m *Manager) checkEndpointHealth(endpoint *Endpoint) {
	// 🔧 [P5.4] 节流检查：避免多触发源重复检查同一端点
	if !m.healthThrottleAllow(endpoint) {
		slog.Debug("⏸️ [健康检查节流] 跳过短时间内重复检查",
			"endpoint", endpoint.Config.Name)
		return
	}
	m.checkEndpointHealthInternal(endpoint)
}

// checkEndpointHealthForced 强制健康检查（绕过节流），用于手动触发场景
// 🔧 [P5.4 新增]
func (m *Manager) checkEndpointHealthForced(endpoint *Endpoint) {
	// 标记 lastCheck 时间，让后续节流窗口正确计算
	key := endpoint.Config.Channel + "/" + endpoint.Config.Name
	m.healthThrottleMu.Lock()
	m.healthThrottleLastTime[key] = time.Now()
	m.healthThrottleMu.Unlock()

	m.checkEndpointHealthInternal(endpoint)
}

// healthThrottleAllow 检查指定端点是否允许执行健康检查
// 🔧 [P5.4 新增]
//   - 返回 true：允许检查（并自动更新 lastCheck 时间）
//   - 返回 false：在节流窗口内，跳过本次检查
func (m *Manager) healthThrottleAllow(endpoint *Endpoint) bool {
	key := endpoint.Config.Channel + "/" + endpoint.Config.Name

	m.healthThrottleMu.Lock()
	defer m.healthThrottleMu.Unlock()

	now := time.Now()
	if last, ok := m.healthThrottleLastTime[key]; ok {
		if now.Sub(last) < m.healthThrottleInterval {
			return false
		}
	}
	m.healthThrottleLastTime[key] = now
	return true
}

// checkEndpointHealthInternal 实际的健康检查逻辑（无节流）
// 🔧 [P5.4 重命名] 原 checkEndpointHealth 的实现，由节流入口调用
func (m *Manager) checkEndpointHealthInternal(endpoint *Endpoint) {
	// Subscription auth belongs to a real caller request. No timer/manual
	// health check may invent credentials or consume subscription quota.
	if endpoint.Config.AuthMode == "client_passthrough" {
		return
	}
	// 跳过健康检查（部分中转商不支持健康检查端点）
	if endpoint.Config.SkipHealthCheck {
		// 仅进行“可达性/延迟探测”，不根据 HTTP 状态码判定健康与否，避免误判为不健康；
		// 仍保留 NeverChecked = true，用于前端展示“跳过检测”。
		start := time.Now()
		responseTime := time.Duration(0)

		baseURL := strings.TrimSpace(endpoint.Config.URL)
		probeURL := baseURL + m.config.Health.HealthPath
		req, err := http.NewRequestWithContext(m.ctx, http.MethodGet, probeURL, nil)
		probeSucceeded := false
		if err == nil {
			token := m.GetTokenForEndpoint(endpoint)
			if token == "" {
				token = m.GetApiKeyForEndpoint(endpoint)
			}
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			resp, doErr := m.client.Do(req)
			responseTime = time.Since(start)
			if resp != nil && resp.Body != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
				_ = resp.Body.Close()
			}
			// 🔧 [P0.7 修复] 跳过健康检查路径下，仍然要记录探测错误（便于排查端点不可达问题）
			// 旧实现：_ = doErr 直接丢弃，端点真正不可达时无任何日志线索
			if doErr != nil {
				slog.Debug("📡 [跳过健康检查] 探测请求错误（仅记录，不影响健康判定）",
					"endpoint", endpoint.Config.Name,
					"url", probeURL,
					"error", doErr)
			} else {
				probeSucceeded = true
			}
		}

		endpoint.mutex.Lock()
		endpoint.Status.LastCheck = time.Now()
		endpoint.Status.ResponseTime = responseTime
		endpoint.Status.Healthy = true // 假定可用，依赖实际请求/兜底机制验证
		if probeSucceeded {
			endpoint.Status.RequestFailed = false
			endpoint.Status.LastRequestFailCode = 0
			endpoint.Status.LastRequestFailReason = ""
		}
		// 保持 NeverChecked = true 以区分"跳过检测"与"正常健康"
		endpoint.mutex.Unlock()
		return
	}

	start := time.Now()

	baseURL := strings.TrimSpace(endpoint.Config.URL)
	healthURL := baseURL + m.config.Health.HealthPath
	req, err := http.NewRequestWithContext(m.ctx, "GET", healthURL, nil)
	if err != nil {
		// 请求创建失败时记录错误原因
		m.updateEndpointStatus(endpoint, false, 0, 0, fmt.Sprintf("创建请求失败: %s", err.Error()))
		return
	}

	// Add authorization header with dynamically resolved token
	token := m.GetTokenForEndpoint(endpoint)
	if token == "" {
		token = m.GetApiKeyForEndpoint(endpoint)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := m.client.Do(req)
	responseTime := time.Since(start)

	if err != nil {
		// 网络错误时记录错误原因
		m.updateEndpointStatus(endpoint, false, responseTime, 0, err.Error())
		return
	}

	// 🔧 [P0.6 修复] drain 响应体后再 Close，让底层 TCP 连接可被复用
	// 旧实现：直接 resp.Body.Close() 会丢弃未读字节，HTTP Transport 不能复用连接
	// 新实现：先 io.Copy(io.Discard, body) 排空，连接可回到 idle 池供下次健康检查复用
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()

	// Only consider 2xx as healthy for API endpoints
	// 2xx: Success responses only
	// All other status codes (including 4xx, 5xx) are considered unhealthy
	healthy := (resp.StatusCode >= 200 && resp.StatusCode < 300)

	// 传递状态码和失败原因
	failCode := 0
	failReason := ""
	if !healthy {
		failCode = resp.StatusCode
		failReason = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	m.updateEndpointStatus(endpoint, healthy, responseTime, failCode, failReason)
}

// updateEndpointStatus updates the health status of an endpoint
// 添加 failCode 和 failReason 参数用于记录失败详情
func (m *Manager) updateEndpointStatus(endpoint *Endpoint, healthy bool, responseTime time.Duration, failCode int, failReason string) {
	endpoint.mutex.Lock()

	endpoint.Status.LastCheck = time.Now()
	endpoint.Status.ResponseTime = responseTime
	endpoint.Status.NeverChecked = false // 标记为已检测

	// 记录状态变化前的健康状态
	wasUnhealthy := !endpoint.Status.Healthy

	if healthy {
		// Endpoint is healthy
		endpoint.Status.Healthy = true
		endpoint.Status.RequestFailed = false
		endpoint.Status.ConsecutiveFails = 0
		// 健康时清除错误信息
		endpoint.Status.LastRequestFailCode = 0
		endpoint.Status.LastRequestFailReason = ""

		// Log recovery if endpoint was previously unhealthy
		if wasUnhealthy {
			slog.Info(fmt.Sprintf("✅ [健康检查] 端点恢复正常: %s - 响应时间: %dms",
				endpoint.Config.Name, responseTime.Milliseconds()))
		}
	} else {
		// Endpoint failed health check
		endpoint.Status.ConsecutiveFails++
		wasHealthy := endpoint.Status.Healthy

		// Mark as unhealthy immediately on any failure
		endpoint.Status.Healthy = false

		// 记录失败详情
		endpoint.Status.LastRequestFailCode = failCode
		endpoint.Status.LastRequestFailReason = failReason

		// Log the failure
		if wasHealthy {
			slog.Warn(fmt.Sprintf("❌ [健康检查] 端点标记为不可用: %s - 连续失败: %d次, 响应时间: %dms, 状态码: %d, 原因: %s",
				endpoint.Config.Name, endpoint.Status.ConsecutiveFails, responseTime.Milliseconds(), failCode, failReason))
		}
	}

	endpoint.mutex.Unlock()

	// 通知Web界面端点状态变化
	go m.notifyWebInterface(endpoint)

	// 当端点从不健康变为健康时，重新评估组的激活状态
	// 这对新增端点后立即激活特别重要
	if healthy && wasUnhealthy {
		go m.refreshGroupActivation()
	}
}

// IsHealthy returns the health status of an endpoint
func (e *Endpoint) IsHealthy() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.Status.Healthy
}

// GetResponseTime returns the last response time of an endpoint
func (e *Endpoint) GetResponseTime() time.Duration {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.Status.ResponseTime
}

// GetStatus returns a copy of the endpoint status
func (e *Endpoint) GetStatus() EndpointStatus {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.Status
}

// GetHealthState 返回端点的健康状态字符串用于前端展示
// 状态包括: "skipped"(跳过检测), "unchecked"(未检测), "healthy"(健康), "request_failed"(请求失败), "unhealthy"(不健康)
// 新增 request_failed 状态，区分健康检查失败和请求转发失败
func (e *Endpoint) GetHealthState() string {
	skipHealthCheck := e.Config.SkipHealthCheck

	e.mutex.RLock()
	neverChecked := e.Status.NeverChecked
	requestFailed := e.Status.RequestFailed
	healthy := e.Status.Healthy
	e.mutex.RUnlock()

	// 1. 请求失败标记（优先级高于健康检查状态）
	// RequestFailed 表示端点因请求转发失败而被标记，仅用于 UI/诊断展示。
	if requestFailed {
		return "request_failed"
	}

	// 2. 配置为跳过健康检查
	if skipHealthCheck {
		return "skipped"
	}

	// 3. 从未检测过
	if neverChecked {
		return "unchecked"
	}

	// 4. 健康或不健康（健康检查结果）
	if healthy {
		return "healthy"
	}

	return "unhealthy"
}

// MarkRequestFailed 标记端点为请求失败状态
// 新增方法，用于渠道内端点切换时立即标记失败端点，供 UI 展示失败状态。
// 仅作为诊断标记使用，不应覆盖健康检查结果
// err: 失败的原因错误（可为 nil）
func (e *Endpoint) MarkRequestFailed(err error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.Status.RequestFailed = true
	e.Status.NeverChecked = false // 请求已触达过该端点，展示上不再视为未检测

	// 记录失败原因供 UI 显示
	if err != nil {
		e.Status.LastRequestFailReason = err.Error()
	}
}

// NotifyEndpointStatusChange 通知前端端点状态发生变化
// 新增公开方法，供 handlers 在调用 MarkRequestFailed 后触发前端刷新
// 使用防抖机制，避免高并发时频繁触发
func (m *Manager) NotifyEndpointStatusChange() {
	m.triggerHealthCheckCompleteDebounced()
}

// MarkEndpointsRequestFailed 批量标记端点为请求失败状态
// 用于渠道间故障转移禁用时，标记所有失败端点
// endpointKeys: 端点键列表（格式: channel::name 或 name）
// lastError: 最后一个错误（用于记录失败原因）
func (m *Manager) MarkEndpointsRequestFailed(endpointKeys []string, lastError error) {
	if m == nil || len(endpointKeys) == 0 {
		return
	}

	var markedEndpoints []string
	for _, key := range endpointKeys {
		ep := m.GetEndpointByNameAny(key)
		if ep == nil {
			continue
		}
		ep.mutex.Lock()
		ep.Status.RequestFailed = true
		ep.Status.NeverChecked = false
		if lastError != nil {
			ep.Status.LastRequestFailReason = lastError.Error()
		}
		ep.mutex.Unlock()
		markedEndpoints = append(markedEndpoints, key)
	}

	if len(markedEndpoints) > 0 {
		slog.Info(fmt.Sprintf("❌ [端点状态更新] 端点标记为请求失败: %v (共%d个)", markedEndpoints, len(markedEndpoints)))
		// 触发前端刷新
		m.triggerHealthCheckCompleteDebounced()
	}
}

// ManualHealthCheck performs a manual health check on a specific endpoint by name
func (m *Manager) ManualHealthCheck(endpointName string) error {
	var targetEndpoint *Endpoint

	// 使用读锁查找端点
	m.endpointsMu.RLock()
	for _, endpoint := range m.endpoints {
		if endpoint.Config.Name == endpointName {
			targetEndpoint = endpoint
			break
		}
	}
	m.endpointsMu.RUnlock()

	if targetEndpoint == nil {
		return fmt.Errorf("端点 '%s' 未找到", endpointName)
	}

	// Perform health check on the endpoint
	slog.Info(fmt.Sprintf("🔍 [手动检查] 开始检查端点: %s", endpointName))
	// 🔧 [P5.4] 手动检查走 Forced 版本，绕过节流
	m.checkEndpointHealthForced(targetEndpoint)

	// Get status and log completion with response time
	status := targetEndpoint.Status
	healthStatus := "健康"
	if !status.Healthy {
		if status.NeverChecked {
			healthStatus = "未检测"
		} else {
			healthStatus = "不健康"
		}
	}

	slog.Info(fmt.Sprintf("🔍 [手动检查] 检查完成: %s - 状态: %s, 响应时间: %s",
		endpointName, healthStatus, display.FormatResponseTime(status.ResponseTime)))

	return nil
}

// BatchHealthCheckAll 批量检测端点的健康状态
// 检测范围与定时健康检查一致：
// - 开启渠道间转移时：检测所有参与的渠道中所有参与转移的端点
// - 关闭渠道间转移时：仅检测活跃渠道中参与转移的端点
// 并发执行检查，使用信号量限制并发数量
func (m *Manager) BatchHealthCheckAll() (int, int, error) {
	slog.Info("🔍 [批量健康检测] 开始检测端点")

	// 使用快照机制获取端点列表
	m.endpointsMu.RLock()
	snapshot := make([]*Endpoint, len(m.endpoints))
	copy(snapshot, m.endpoints)
	m.endpointsMu.RUnlock()

	if len(snapshot) == 0 {
		return 0, 0, fmt.Errorf("没有配置任何端点")
	}

	// 根据渠道间故障转移状态筛选需要检测的端点
	var endpointsToCheck []*Endpoint

	if m.IsFailoverEnabled() {
		// 开启渠道间转移：检测所有参与的渠道中参与转移的端点
		for _, ep := range snapshot {
			if ep == nil {
				continue
			}
			// 渠道级开关：暂停渠道不检测
			if m.groupManager != nil && !m.groupManager.IsChannelFailoverEnabled(ChannelKey(ep)) {
				continue
			}
			// 端点级开关：不参与故障转移的端点不检测
			failoverEnabled := true
			if ep.Config.FailoverEnabled != nil {
				failoverEnabled = *ep.Config.FailoverEnabled
			}
			if !failoverEnabled {
				continue
			}
			endpointsToCheck = append(endpointsToCheck, ep)
		}
		channelSet := make(map[string]struct{}, len(endpointsToCheck))
		for _, ep := range endpointsToCheck {
			if key := ChannelKey(ep); key != "" {
				channelSet[key] = struct{}{}
			}
		}
		slog.Info(fmt.Sprintf("🔍 [批量健康检测] 渠道间转移模式：检测 %d 个渠道（%d 个端点）",
			len(channelSet), len(endpointsToCheck)))
	} else {
		// 关闭渠道间转移：仅检测活跃渠道中参与转移的端点
		active := m.groupManager.FilterEndpointsByActiveGroups(snapshot)
		for _, ep := range active {
			if ep == nil {
				continue
			}
			failoverEnabled := true
			if ep.Config.FailoverEnabled != nil {
				failoverEnabled = *ep.Config.FailoverEnabled
			}
			if !failoverEnabled {
				continue
			}
			endpointsToCheck = append(endpointsToCheck, ep)
		}
		channelSet := make(map[string]struct{}, len(endpointsToCheck))
		for _, ep := range endpointsToCheck {
			if key := ChannelKey(ep); key != "" {
				channelSet[key] = struct{}{}
			}
		}
		slog.Info(fmt.Sprintf("🔍 [批量健康检测] 单渠道模式：检测 %d 个活跃渠道（%d 个端点）",
			len(channelSet), len(endpointsToCheck)))
	}

	if len(endpointsToCheck) == 0 {
		return 0, 0, fmt.Errorf("没有符合条件的端点需要检测")
	}

	// 使用信号量限制并发数量（最多 20 个并发）
	const maxConcurrentChecks = 20
	semaphore := make(chan struct{}, maxConcurrentChecks)

	// 使用 WaitGroup 并发检测所有端点
	var wg sync.WaitGroup
	var healthyCount, unhealthyCount int
	var countMu sync.Mutex

	for _, endpoint := range endpointsToCheck {
		wg.Add(1)
		semaphore <- struct{}{} // 获取信号量

		go func(ep *Endpoint) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放信号量

			// 🔧 [P5.4] 批量检查走 Forced 版本，整批已是统一调度，无需节流
			m.checkEndpointHealthForced(ep)

			// 获取检测结果（需要加锁读取）
			ep.mutex.RLock()
			healthy := ep.Status.Healthy
			responseTime := ep.Status.ResponseTime
			ep.mutex.RUnlock()

			// 统计检测结果
			countMu.Lock()
			if healthy {
				healthyCount++
			} else {
				unhealthyCount++
			}
			countMu.Unlock()

			// 记录检测结果
			healthStatus := "❌ 不健康"
			if healthy {
				healthStatus = "✅ 健康"
			}
			slog.Debug(fmt.Sprintf("   检测端点: %s - 状态: %s, 响应时间: %s",
				ep.Config.Name,
				healthStatus,
				display.FormatResponseTime(responseTime),
			))
		}(endpoint)
	}

	// 等待所有检测完成
	wg.Wait()

	slog.Info(fmt.Sprintf("✅ [批量健康检测] 完成，共检测 %d 个端点 (健康: %d, 不健康: %d)",
		len(endpointsToCheck), healthyCount, unhealthyCount))

	// 使用防抖机制触发回调
	m.triggerHealthCheckCompleteDebounced()

	return healthyCount, unhealthyCount, nil
}
