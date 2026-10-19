// endpoint_selection.go - 端点选择/路由功能
// 包含健康端点获取、故障转移端点选择、排序策略等

package endpoint

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// GetHealthyEndpoints returns routable endpoints from active groups based on strategy.
// 以"渠道(channel)"为单位路由，优先只返回当前激活渠道内的端点；
// 跨渠道切换由请求级故障转移触发（见 TriggerRequestFailover）。
// 健康检查状态是路由硬过滤；skip_health_check=true 的端点绕过该过滤。
// 冷却机制已移除
func (m *Manager) GetHealthyEndpoints() []*Endpoint {
	return m.GetHealthyEndpointsForTurn("")
}

func (m *Manager) GetHealthyEndpointsForTurn(turnKey string) []*Endpoint {
	candidates := m.activeEndpointCandidates(nil, turnKey)
	if len(candidates) == 0 {
		return nil
	}
	return m.sortHealthyEndpoints(candidates, true)
}

// MarkEndpointRequestSuccess records the last endpoint that completed a request
// so the next request in the same channel starts on the known-good route.
func (m *Manager) MarkEndpointRequestSuccess(ep *Endpoint) {
	if m == nil || ep == nil {
		return
	}
	channel := ChannelKey(ep)
	key := endpointKeyFromConfig(ep.Config)
	if channel == "" || key == "" {
		return
	}
	m.preferredEndpointMu.Lock()
	if m.preferredEndpointKeys == nil {
		m.preferredEndpointKeys = make(map[string]string)
	}
	m.preferredEndpointKeys[channel] = key
	m.preferredEndpointMu.Unlock()
}

// ClearPreferredEndpoint drops a stale successful-route hint when that endpoint fails.
func (m *Manager) ClearPreferredEndpoint(ep *Endpoint) {
	if m == nil || ep == nil {
		return
	}
	channel := ChannelKey(ep)
	key := endpointKeyFromConfig(ep.Config)
	m.preferredEndpointMu.Lock()
	if m.preferredEndpointKeys[channel] == key {
		delete(m.preferredEndpointKeys, channel)
	}
	m.preferredEndpointMu.Unlock()
}

func (m *Manager) preferredEndpointKey(channel string) string {
	if m == nil || channel == "" {
		return ""
	}
	m.preferredEndpointMu.RLock()
	key := m.preferredEndpointKeys[channel]
	m.preferredEndpointMu.RUnlock()
	return key
}

func (m *Manager) clearPreferredEndpoints() {
	if m == nil {
		return
	}
	m.preferredEndpointMu.Lock()
	m.preferredEndpointKeys = make(map[string]string)
	m.preferredEndpointMu.Unlock()
}

func (m *Manager) activeEndpointCandidates(excludeKeys []string, turnKey string) []*Endpoint {
	if m == nil || m.groupManager == nil {
		return nil
	}
	activeEndpoints := m.groupManager.FilterEndpointsByActiveGroups(m.GetAllEndpoints())
	if len(activeEndpoints) == 0 {
		return nil
	}
	var ordinary []*Endpoint
	for _, ep := range activeEndpoints {
		if ep != nil && ep.Config.AuthMode != "client_passthrough" {
			ordinary = append(ordinary, ep)
		}
	}
	return filterRoutableEndpoints(ordinary, excludeKeys)
}

// filterRoutableEndpoints 请求选路的通用过滤：排除本次请求已尝试的端点、
// 不参与故障转移的端点，以及健康检查判定为不可用的端点。
func filterRoutableEndpoints(endpoints []*Endpoint, excludeKeys []string) []*Endpoint {
	excludeSet := make(map[string]struct{}, len(excludeKeys))
	for _, key := range excludeKeys {
		if key != "" {
			excludeSet[key] = struct{}{}
		}
	}

	filtered := make([]*Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep == nil {
			continue
		}
		epKey := endpointKeyFromConfig(ep.Config)
		if _, excluded := excludeSet[epKey]; excluded {
			continue
		}
		failoverEnabled := true
		if ep.Config.FailoverEnabled != nil {
			failoverEnabled = *ep.Config.FailoverEnabled
		}
		if !failoverEnabled {
			continue
		}
		if ep.Config.AuthMode != "client_passthrough" && !endpointHealthAllowsRouting(ep) {
			continue
		}
		filtered = append(filtered, ep)
	}
	return filtered
}

// GetRoutableEndpointsForChannel 返回指定渠道的可路由端点，只供本次请求使用。
// 与激活渠道的选路过滤相同，但不经过激活渠道过滤，也不读写全局激活状态；
// groupID > 0 时只取该凭证分组。已暂停的渠道返回空。
func (m *Manager) GetRoutableEndpointsForChannel(channel string, groupID int64, excludeKeys []string) []*Endpoint {
	if m == nil || m.groupManager == nil || channel == "" {
		return nil
	}
	if m.groupManager.IsGroupManuallyPaused(channel) {
		return nil
	}
	all := m.GetAllEndpoints()
	inChannel := make([]*Endpoint, 0, len(all))
	for _, ep := range all {
		if ep == nil || ChannelKey(ep) != channel {
			continue
		}
		if groupID > 0 && ep.Config.CredentialGroupID != groupID {
			continue
		}
		inChannel = append(inChannel, ep)
	}
	candidates := filterRoutableEndpoints(inChannel, excludeKeys)
	if len(candidates) == 0 {
		return nil
	}
	return m.sortHealthyEndpoints(candidates, false)
}

// RoutableChannelsByPriority 按渠道优先级返回未暂停的渠道名，供请求级改道挑选候选渠道。
func (m *Manager) RoutableChannelsByPriority() []string {
	if m == nil || m.groupManager == nil {
		return nil
	}
	groups := m.groupManager.GetAllGroups()
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		if group == nil || group.Name == "" || group.ManuallyPaused {
			continue
		}
		names = append(names, group.Name)
	}
	return names
}

// ActiveChannelName 返回当前激活渠道名（无激活渠道时为空串）
func (m *Manager) ActiveChannelName() string {
	if m == nil || m.groupManager == nil {
		return ""
	}
	active, _ := m.groupManager.ActiveGroupSnapshot()
	return active
}

// sortHealthyEndpoints sorts healthy endpoints by credential group first, then by the configured
// in-group strategy. This keeps failover deterministic: default credential group first, then
// other groups by creation order, exhausting a group before moving to the next one.
func (m *Manager) sortHealthyEndpoints(healthy []*Endpoint, showLogs bool) []*Endpoint {
	// Sort based on strategy
	switch m.config.Strategy.Type {
	case "priority":
		sort.Slice(healthy, func(i, j int) bool {
			if preferred := m.comparePreferredEndpoint(healthy[i], healthy[j]); preferred != 0 {
				return preferred < 0
			}
			healthy[i].mutex.RLock()
			healthy[j].mutex.RLock()
			defer healthy[i].mutex.RUnlock()
			defer healthy[j].mutex.RUnlock()
			if groupOrder := compareEndpointCredentialGroupOrder(healthy[i], healthy[j]); groupOrder != 0 {
				return groupOrder < 0
			}
			if healthy[i].Config.Priority != healthy[j].Config.Priority {
				return healthy[i].Config.Priority < healthy[j].Config.Priority
			}
			if preferred := compareConfiguredPreferredEndpoint(healthy[i], healthy[j]); preferred != 0 {
				return preferred < 0
			}
			return endpointSortKey(healthy[i]) < endpointSortKey(healthy[j])
		})
	case "fastest":
		// Log endpoint latencies for fastest strategy (only if showLogs is true)
		if len(healthy) > 1 && showLogs {
			slog.Info("📊 [Fastest Strategy] 基于健康检查的端点延迟排序:")
			for _, ep := range healthy {
				ep.mutex.RLock()
				responseTime := ep.Status.ResponseTime
				ep.mutex.RUnlock()
				slog.Info(fmt.Sprintf("  ⏱️ %s - 延迟: %dms (来源: 定期健康检查)",
					ep.Config.Name, responseTime.Milliseconds()))
			}
		}

		sort.Slice(healthy, func(i, j int) bool {
			if preferred := m.comparePreferredEndpoint(healthy[i], healthy[j]); preferred != 0 {
				return preferred < 0
			}
			healthy[i].mutex.RLock()
			healthy[j].mutex.RLock()
			defer healthy[i].mutex.RUnlock()
			defer healthy[j].mutex.RUnlock()
			if groupOrder := compareEndpointCredentialGroupOrder(healthy[i], healthy[j]); groupOrder != 0 {
				return groupOrder < 0
			}
			if healthy[i].Status.ResponseTime != healthy[j].Status.ResponseTime {
				return healthy[i].Status.ResponseTime < healthy[j].Status.ResponseTime
			}
			if healthy[i].Config.Priority != healthy[j].Config.Priority {
				return healthy[i].Config.Priority < healthy[j].Config.Priority
			}
			if preferred := compareConfiguredPreferredEndpoint(healthy[i], healthy[j]); preferred != 0 {
				return preferred < 0
			}
			return endpointSortKey(healthy[i]) < endpointSortKey(healthy[j])
		})
	}

	return healthy
}

func endpointHealthAllowsRouting(ep *Endpoint) bool {
	if ep == nil {
		return false
	}
	if ep.Config.SkipHealthCheck {
		return true
	}
	ep.mutex.RLock()
	healthy := ep.Status.Healthy
	neverChecked := ep.Status.NeverChecked
	ep.mutex.RUnlock()
	return healthy || neverChecked
}

func compareEndpointCredentialGroupOrder(left, right *Endpoint) int {
	leftOrder := 0
	if left != nil {
		leftOrder = left.Config.CredentialGroupOrder
	}
	rightOrder := 0
	if right != nil {
		rightOrder = right.Config.CredentialGroupOrder
	}
	if leftOrder == rightOrder {
		return 0
	}
	if leftOrder < rightOrder {
		return -1
	}
	return 1
}

func compareConfiguredPreferredEndpoint(left, right *Endpoint) int {
	leftPreferred := left != nil && left.Config.Enabled != nil && *left.Config.Enabled
	rightPreferred := right != nil && right.Config.Enabled != nil && *right.Config.Enabled
	if leftPreferred == rightPreferred {
		return 0
	}
	if leftPreferred {
		return -1
	}
	return 1
}

func (m *Manager) comparePreferredEndpoint(left, right *Endpoint) int {
	if left == nil || right == nil {
		return 0
	}
	leftChannel := ChannelKey(left)
	if leftChannel == "" || leftChannel != ChannelKey(right) {
		return 0
	}
	preferred := m.preferredEndpointKey(leftChannel)
	if preferred == "" {
		return 0
	}
	leftPreferred := endpointKeyFromConfig(left.Config) == preferred
	rightPreferred := endpointKeyFromConfig(right.Config) == preferred
	if leftPreferred == rightPreferred {
		return 0
	}
	if leftPreferred {
		return -1
	}
	return 1
}

func endpointSortKey(ep *Endpoint) string {
	if ep == nil {
		return ""
	}
	if key := endpointKeyFromConfig(ep.Config); key != "" {
		return key
	}
	return ep.Config.Name
}

func sortFastTestResultsByGroupThenResponseTime(results []*FastTestResult) []*FastTestResult {
	successful := make([]*FastTestResult, 0, len(results))
	for _, result := range results {
		if result != nil && result.Success {
			successful = append(successful, result)
		}
	}

	sort.Slice(successful, func(i, j int) bool {
		left := successful[i]
		right := successful[j]
		leftEndpoint := left.Endpoint
		rightEndpoint := right.Endpoint
		if groupOrder := compareEndpointCredentialGroupOrder(leftEndpoint, rightEndpoint); groupOrder != 0 {
			return groupOrder < 0
		}
		if left.ResponseTime != right.ResponseTime {
			return left.ResponseTime < right.ResponseTime
		}
		leftPriority := 0
		if leftEndpoint != nil {
			leftPriority = leftEndpoint.Config.Priority
		}
		rightPriority := 0
		if rightEndpoint != nil {
			rightPriority = rightEndpoint.Config.Priority
		}
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return endpointSortKey(leftEndpoint) < endpointSortKey(rightEndpoint)
	})

	return successful
}

// GetFastestEndpointsWithRealTimeTest returns endpoints from active groups sorted by real-time testing.
// 以“渠道(channel)”为单位路由，只测试/排序当前激活渠道内端点。
func (m *Manager) GetFastestEndpointsWithRealTimeTest(ctx context.Context) []*Endpoint {
	healthy := m.activeEndpointCandidates(nil, TurnFailureKeyFromContext(ctx))
	if len(healthy) == 0 {
		return healthy
	}

	// If not using fastest strategy or fast test disabled, apply sorting with logging
	if m.config.Strategy.Type != "fastest" || !m.config.Strategy.FastTestEnabled {
		return m.sortHealthyEndpoints(healthy, true) // Show logs
	}

	// Check if we have cached fast test results first
	testResults, usedCache := m.fastTester.TestEndpointsParallel(ctx, healthy)

	// Only show health check sorting if we're NOT using cache
	if !usedCache && m.config.Strategy.Type == "fastest" && len(healthy) > 1 {
		slog.InfoContext(ctx, "📊 [Fastest Strategy] 基于健康检查的活跃组端点延迟排序:")
		for _, ep := range healthy {
			ep.mutex.RLock()
			responseTime := ep.Status.ResponseTime
			group := ep.Config.Group
			ep.mutex.RUnlock()
			slog.InfoContext(ctx, fmt.Sprintf("  ⏱️ %s (组: %s) - 延迟: %dms (来源: 定期健康检查)",
				ep.Config.Name, group, responseTime.Milliseconds()))
		}
	}

	// Log ALL test results first (including failures) - but only if cache wasn't used
	if len(testResults) > 0 && !usedCache {
		slog.InfoContext(ctx, "🔍 [Fastest Response Mode] 活跃组端点性能测试结果:")
		successCount := 0
		for _, result := range testResults {
			group := result.Endpoint.Config.Group
			if result.Success {
				successCount++
				slog.InfoContext(ctx, fmt.Sprintf("  ✅ 健康 %s (组: %s) - 响应时间: %dms",
					result.Endpoint.Config.Name, group,
					result.ResponseTime.Milliseconds()))
			} else {
				errorMsg := ""
				if result.Error != nil {
					errorMsg = fmt.Sprintf(" - 错误: %s", result.Error.Error())
				}
				slog.InfoContext(ctx, fmt.Sprintf("  ❌ 异常 %s (组: %s) - 响应时间: %dms%s",
					result.Endpoint.Config.Name, group,
					result.ResponseTime.Milliseconds(),
					errorMsg))
			}
		}

		slog.InfoContext(ctx, fmt.Sprintf("📊 [测试摘要] 活跃组测试: %d个端点, 健康: %d个, 异常: %d个",
			len(testResults), successCount, len(testResults)-successCount))
	}

	// Sort by credential group first, then response time within the group (only successful results).
	sortedResults := sortFastTestResultsByGroupThenResponseTime(testResults)

	if len(sortedResults) == 0 {
		slog.WarnContext(ctx, "⚠️ [Fastest Response Mode] 活跃组所有端点测试失败，回退到健康检查模式")
		return healthy // Fall back to health check results if no fast tests succeeded
	}

	// Convert back to endpoint slice
	endpoints := make([]*Endpoint, 0, len(sortedResults))
	for _, result := range sortedResults {
		endpoints = append(endpoints, result.Endpoint)
	}
	// Fast-test ordering is still the tie-breaker for the remaining endpoints,
	// but a route that just succeeded should be tried first on the next request.
	m.prioritizePreferredEndpoints(endpoints)

	// Log the successful endpoint ranking
	if len(endpoints) > 0 {
		// Show the fastest endpoint selection
		fastestEndpoint := endpoints[0]
		var fastestTime int64
		for _, result := range sortedResults {
			if result.Endpoint == fastestEndpoint {
				fastestTime = result.ResponseTime.Milliseconds()
				break
			}
		}

		cacheIndicator := ""
		if usedCache {
			cacheIndicator = " (缓存)"
		}

		slog.InfoContext(ctx, fmt.Sprintf("🚀 [Fastest Response Mode] 选择最快端点: %s - %dms%s",
			fastestEndpoint.Config.Name, fastestTime, cacheIndicator))

		// Show other available endpoints if there are more than one
		if len(endpoints) > 1 && !usedCache {
			slog.InfoContext(ctx, "📋 [备用端点] 其他可用端点:")
			for i := 1; i < len(endpoints); i++ {
				ep := endpoints[i]
				var responseTime int64
				var epGroup string
				for _, result := range sortedResults {
					if result.Endpoint == ep {
						responseTime = result.ResponseTime.Milliseconds()
						epGroup = result.Endpoint.Config.Group
						break
					}
				}
				slog.InfoContext(ctx, fmt.Sprintf("  🔄 备用 %s (组: %s) - 响应时间: %dms",
					ep.Config.Name, epGroup, responseTime))
			}
		}
	}

	return endpoints
}

func (m *Manager) prioritizePreferredEndpoints(endpoints []*Endpoint) {
	if len(endpoints) < 2 {
		return
	}
	preferred := m.preferredEndpointKey(ChannelKey(endpoints[0]))
	if preferred == "" {
		return
	}
	index := -1
	for i, ep := range endpoints {
		if ep != nil && endpointKeyFromConfig(ep.Config) == preferred {
			index = i
			break
		}
	}
	if index <= 0 {
		return
	}
	preferredEndpoint := endpoints[index]
	copy(endpoints[1:index+1], endpoints[0:index])
	endpoints[0] = preferredEndpoint
}

// GetEndpointByName returns an endpoint by key, only from active groups.
// 兼容旧调用方仅传端点名称。
func (m *Manager) GetEndpointByName(endpointKey string) *Endpoint {
	// 使用快照机制
	m.endpointsMu.RLock()
	snapshot := make([]*Endpoint, len(m.endpoints))
	copy(snapshot, m.endpoints)
	m.endpointsMu.RUnlock()

	// First filter by active groups
	activeEndpoints := m.groupManager.FilterEndpointsByActiveGroups(snapshot)

	// Then find by name
	for _, endpoint := range activeEndpoints {
		if endpointKeyFromConfig(endpoint.Config) == endpointKey {
			return endpoint
		}
	}
	if endpointKey != "" && !strings.Contains(endpointKey, endpointKeySeparator) {
		var found *Endpoint
		for _, endpoint := range activeEndpoints {
			if endpoint.Config.Name != endpointKey {
				continue
			}
			if found != nil {
				return nil
			}
			found = endpoint
		}
		return found
	}
	return nil
}

// GetEndpointByNameAny returns an endpoint by key from all endpoints (ignoring group status)
// 兼容旧调用方仅传端点名称。
func (m *Manager) GetEndpointByNameAny(endpointKey string) *Endpoint {
	m.endpointsMu.RLock()
	defer m.endpointsMu.RUnlock()

	// 优先按 endpointKey（channel::name）查找
	for _, endpoint := range m.endpoints {
		if endpointKeyFromConfig(endpoint.Config) == endpointKey {
			return endpoint
		}
	}

	// 兼容：旧调用方仅传 name（当且仅当全局唯一时允许回退）
	if endpointKey != "" && !strings.Contains(endpointKey, endpointKeySeparator) {
		var found *Endpoint
		for _, endpoint := range m.endpoints {
			if endpoint.Config.Name != endpointKey {
				continue
			}
			if found != nil {
				// 多渠道同名：回退会产生歧义，直接返回 nil
				return nil
			}
			found = endpoint
		}
		return found
	}
	return nil
}

// GetAllEndpoints returns all endpoints (deprecated: use GetEndpoints instead)
func (m *Manager) GetAllEndpoints() []*Endpoint {
	m.endpointsMu.RLock()
	defer m.endpointsMu.RUnlock()

	result := make([]*Endpoint, len(m.endpoints))
	copy(result, m.endpoints)
	return result
}

// GetEndpoints returns all endpoints for Web interface
func (m *Manager) GetEndpoints() []*Endpoint {
	m.endpointsMu.RLock()
	defer m.endpointsMu.RUnlock()

	result := make([]*Endpoint, len(m.endpoints))
	copy(result, m.endpoints)
	return result
}

// GetEndpointStatus returns the status of an endpoint by name
func (m *Manager) GetEndpointStatus(endpointKey string) EndpointStatus {
	m.endpointsMu.RLock()
	defer m.endpointsMu.RUnlock()

	for _, ep := range m.endpoints {
		if endpointKeyFromConfig(ep.Config) == endpointKey {
			ep.mutex.RLock()
			status := ep.Status
			ep.mutex.RUnlock()
			return status
		}
	}
	if endpointKey != "" && !strings.Contains(endpointKey, endpointKeySeparator) {
		var found *Endpoint
		for _, ep := range m.endpoints {
			if ep.Config.Name != endpointKey {
				continue
			}
			if found != nil {
				return EndpointStatus{}
			}
			found = ep
		}
		if found != nil {
			found.mutex.RLock()
			status := found.Status
			found.mutex.RUnlock()
			return status
		}
	}
	return EndpointStatus{}
}

// GetEndpointCount 返回当前端点数量（新增）
func (m *Manager) GetEndpointCount() int {
	m.endpointsMu.RLock()
	defer m.endpointsMu.RUnlock()
	return len(m.endpoints)
}

// GetHealthyEndpointsExcluding 获取健康端点列表，排除已尝试的端点
// 用于实现严格优先级转移逻辑，确保在同一请求链路内只能向下一优先级转移
// 冷却机制已移除
// excludeKeys: 已尝试端点的 key 列表（channel::name 格式）
func (m *Manager) GetHealthyEndpointsExcluding(excludeKeys []string) []*Endpoint {
	return m.GetHealthyEndpointsExcludingForTurn(excludeKeys, "")
}

func (m *Manager) GetHealthyEndpointsExcludingForTurn(excludeKeys []string, turnKey string) []*Endpoint {
	candidates := m.activeEndpointCandidates(excludeKeys, turnKey)
	if len(candidates) == 0 {
		return nil
	}
	return m.sortHealthyEndpoints(candidates, false)
}

// GetRecoveredEndpointsInChannel 获取当前渠道内已恢复的端点
// 冷却机制已移除，此函数现在返回所有健康的已尝试端点（用于循环重试）
// triedKeys: 本次请求已尝试过的端点 key 列表
// 返回：按优先级排序的健康端点列表
func (m *Manager) GetRecoveredEndpointsInChannel(triedKeys []string) []*Endpoint {
	// 构建已尝试集合
	triedSet := make(map[string]struct{}, len(triedKeys))
	for _, key := range triedKeys {
		if key != "" {
			triedSet[key] = struct{}{}
		}
	}

	// 使用快照机制
	m.endpointsMu.RLock()
	snapshot := make([]*Endpoint, len(m.endpoints))
	copy(snapshot, m.endpoints)
	m.endpointsMu.RUnlock()

	// 获取活跃渠道的端点
	activeEndpoints := m.groupManager.FilterEndpointsByActiveGroups(snapshot)

	var recovered []*Endpoint
	for _, endpoint := range activeEndpoints {
		epKey := endpointKeyFromConfig(endpoint.Config)

		// 检查是否参与故障转移
		failoverEnabled := true
		if endpoint.Config.FailoverEnabled != nil {
			failoverEnabled = *endpoint.Config.FailoverEnabled
		}
		if !failoverEnabled {
			continue
		}

		endpoint.mutex.RLock()
		isHealthy := endpoint.Status.Healthy
		skipHealthCheck := endpoint.Config.SkipHealthCheck
		endpoint.mutex.RUnlock()

		if _, tried := triedSet[epKey]; tried {
			if isHealthy || skipHealthCheck {
				recovered = append(recovered, endpoint)
				slog.Debug(fmt.Sprintf("🔄 [端点恢复] 发现可用端点: %s", endpoint.Config.Name))
			}
		}
	}

	// 按优先级排序
	if len(recovered) > 0 {
		return m.sortHealthyEndpoints(recovered, false)
	}

	return nil
}
