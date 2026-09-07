// endpoint_manager.go - Codex 端点管理器
// 管理活跃渠道的端点列表，提供故障转移选择逻辑。

package codexproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/codex/store"
)

// 注意：冷却机制已完全移除（参考 Claude 端 internal/providers/claude/proxy/endpoint/cooldown.go 历史经验）
// 原因：loop_retry 模式下冷却机制与循环重试逻辑冲突
// 失败端点仅靠健康检查恢复或被运维通过 UI 重新激活

// CodexEndpoint 运行时端点信息（合并渠道与端点数据）
type CodexEndpoint struct {
	ID                   int64
	ChannelID            int64
	CredentialGroupID    int64
	CredentialGroupName  string
	CredentialGroupOrder int
	DailyCostLimitUSD    float64
	Name                 string
	BaseURL              string
	APIKey               string
	IsActive             bool
	FailoverEnabled      bool
	SkipHealthCheck      bool
	Priority             int
	Timeout              time.Duration
}

type EndpointFailureDetail struct {
	StatusCode int
	Reason     string
}

type credentialGroupRef struct {
	ID   int64
	Name string
}

var errCodexActiveChannelChanged = errors.New("Codex 活跃渠道已被其他操作更改")

func (g credentialGroupRef) valid() bool {
	return g.ID > 0 || g.Name != ""
}

// EndpointManager 管理活跃渠道的端点，提供故障转移选择
type EndpointManager struct {
	// 依赖
	channelStore         store.CodexChannelStore
	endpointStore        store.CodexEndpointStore
	credentialGroupStore store.CodexCredentialGroupStore

	// 运行时状态
	mu             sync.RWMutex
	activationMu   *sync.Mutex
	endpoints      []*CodexEndpoint // 按凭证分组顺序、priority ASC 排序；请求时可按策略临时排序候选
	activeChannel  *store.CodexChannelRecord
	unhealthyMap   map[int64]bool // endpoint ID -> 是否标记为不健康（仅靠健康检查恢复）
	failureDetails map[int64]EndpointFailureDetail
	latencies      map[int64]int64
	strategyType   string

	// 活跃端点变更回调（由 App 注入，用于发送 Wails 事件刷新前端 UI）
	onActiveChanged func()

	logger *slog.Logger
}

// ActivateNextChannelForFailover 激活下一个参与故障转移且拥有可用端点的渠道。
// expectedActiveID 用于阻止旧请求覆盖更新后的手动/自动渠道选择。
func (m *EndpointManager) ActivateNextChannelForFailover(ctx context.Context, tried map[int64]struct{}, expectedActiveID int64) (*store.CodexChannelRecord, error) {
	m.activationMu.Lock()
	defer m.activationMu.Unlock()
	previous, err := m.channelStore.GetActive(ctx)
	if err != nil {
		return nil, err
	}
	if expectedActiveID > 0 && (previous == nil || previous.ID != expectedActiveID) {
		return nil, fmt.Errorf("%w: 期望渠道 ID=%d，当前渠道 ID=%d", errCodexActiveChannelChanged, expectedActiveID, codexChannelID(previous))
	}

	channels, err := m.channelStore.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(channels, func(i, j int) bool {
		if channels[i].Priority != channels[j].Priority {
			return channels[i].Priority < channels[j].Priority
		}
		return channels[i].ID < channels[j].ID
	})
	for _, channel := range channels {
		if channel == nil || !channel.FailoverEnabled {
			continue
		}
		if _, seen := tried[channel.ID]; seen {
			continue
		}
		endpoints, err := m.endpointStore.ListByChannelID(ctx, channel.ID)
		if err != nil {
			return nil, err
		}
		hasCandidate := false
		for _, endpoint := range endpoints {
			if endpoint != nil && endpoint.FailoverEnabled {
				hasCandidate = true
				break
			}
		}
		if !hasCandidate {
			continue
		}
		if err := m.channelStore.SetActive(ctx, channel.ID); err != nil {
			return nil, err
		}
		if err := m.Reload(); err != nil {
			if previous != nil {
				if restoreErr := m.channelStore.SetActive(ctx, previous.ID); restoreErr != nil {
					return nil, fmt.Errorf("加载 Codex 故障转移渠道失败: %v；恢复原渠道失败: %w", err, restoreErr)
				}
				_ = m.Reload()
			}
			return nil, fmt.Errorf("加载 Codex 故障转移渠道失败: %w", err)
		}
		m.notifyStateChanged()
		return channel, nil
	}
	return nil, fmt.Errorf("没有可用的 Codex 故障转移渠道")
}

// NewEndpointManager 创建端点管理器实例
func NewEndpointManager(
	channelStore store.CodexChannelStore,
	endpointStore store.CodexEndpointStore,
	credentialGroupStore store.CodexCredentialGroupStore,
	logger *slog.Logger,
) *EndpointManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &EndpointManager{
		channelStore:         channelStore,
		endpointStore:        endpointStore,
		credentialGroupStore: credentialGroupStore,
		unhealthyMap:         make(map[int64]bool),
		failureDetails:       make(map[int64]EndpointFailureDetail),
		latencies:            make(map[int64]int64),
		activationMu:         &sync.Mutex{},
		logger:               logger,
	}
}

// SetActivationMutex 让运行时自动切换与 App 手动激活共享同一个仲裁边界。
func (m *EndpointManager) SetActivationMutex(mu *sync.Mutex) {
	if mu != nil {
		m.activationMu = mu
	}
}

func codexChannelID(channel *store.CodexChannelRecord) int64 {
	if channel == nil {
		return 0
	}
	return channel.ID
}

// SetConfig 注入运行时配置，供 Codex 端点选择复用全局路由策略。
func (m *EndpointManager) SetConfig(cfg *config.Config) {
	m.mu.Lock()
	m.strategyType = "priority"
	if cfg != nil && cfg.Strategy.Type == "fastest" {
		m.strategyType = "fastest"
	}
	m.mu.Unlock()
}

// Reload 从数据库重新加载活跃渠道及其端点
func (m *EndpointManager) Reload() error {
	ctx := context.Background()

	channel, err := m.channelStore.GetActive(ctx)
	if err != nil {
		return fmt.Errorf("获取活跃 Codex 渠道失败: %w", err)
	}
	if channel == nil {
		m.mu.Lock()
		m.activeChannel = nil
		m.endpoints = nil
		m.mu.Unlock()
		m.logger.Warn("无活跃 Codex 渠道，端点列表已清空")
		return nil
	}

	endpoints, err := m.loadChannelEndpoints(ctx, channel)
	if err != nil {
		return err
	}

	sortCodexEndpoints(endpoints)

	m.mu.Lock()
	m.activeChannel = channel
	m.endpoints = endpoints
	// 健康检查覆盖所有参与转移的渠道，不能按当前活跃渠道裁剪全局端点状态。
	m.mu.Unlock()

	m.logger.Info("Codex 端点已重新加载",
		"channel", channel.Name,
		"endpoint_count", len(endpoints),
	)
	return nil
}

func (m *EndpointManager) loadChannelEndpoints(ctx context.Context, channel *store.CodexChannelRecord) ([]*CodexEndpoint, error) {
	records, err := m.endpointStore.ListByChannelID(ctx, channel.ID)
	if err != nil {
		return nil, fmt.Errorf("获取渠道 %s 的端点列表失败: %w", channel.Name, err)
	}
	endpoints := make([]*CodexEndpoint, 0, len(records))
	var defaultGroup *store.CodexCredentialGroupRecord
	groupByID := make(map[int64]*store.CodexCredentialGroupRecord)
	groupOrderByID := make(map[int64]int)
	if m.credentialGroupStore != nil {
		groups, groupErr := m.credentialGroupStore.ListByChannelID(ctx, channel.ID)
		if groupErr != nil {
			m.logger.Warn("加载 Codex 凭证分组顺序失败，将回退到端点优先级排序",
				"channel", channel.Name,
				"error", groupErr,
			)
		}
		for idx, group := range groups {
			if group == nil {
				continue
			}
			groupByID[group.ID] = group
			groupOrderByID[group.ID] = idx
			if group.IsDefault {
				defaultGroup = group
			}
		}
		if defaultGroup == nil {
			defaultGroup, _ = m.credentialGroupStore.GetDefaultByChannelID(ctx, channel.ID)
			if defaultGroup != nil {
				groupByID[defaultGroup.ID] = defaultGroup
				if _, ok := groupOrderByID[defaultGroup.ID]; !ok {
					groupOrderByID[defaultGroup.ID] = 0
				}
			}
		}
	}
	for _, record := range records {
		group := defaultGroup
		if m.credentialGroupStore != nil && record.CredentialGroupID > 0 {
			if resolved := groupByID[record.CredentialGroupID]; resolved != nil {
				group = resolved
			} else if resolved, groupErr := m.credentialGroupStore.GetByID(ctx, record.CredentialGroupID); groupErr == nil && resolved != nil {
				group = resolved
				groupByID[resolved.ID] = resolved
				groupOrderByID[resolved.ID] = len(groupOrderByID)
			}
		}
		apiKey := record.ApiKey
		if apiKey == "" && group != nil {
			apiKey = group.ApiKey
		}
		if apiKey == "" {
			apiKey = channel.ApiKey
		}
		groupName := ""
		groupOrder := len(groupOrderByID)
		dailyCostLimitUSD := 0.0
		if group != nil {
			groupName = group.Name
			dailyCostLimitUSD = group.DailyCostLimitUSD
			if order, ok := groupOrderByID[group.ID]; ok {
				groupOrder = order
			}
		}
		endpoints = append(endpoints, &CodexEndpoint{
			ID:                   record.ID,
			ChannelID:            record.ChannelID,
			CredentialGroupID:    record.CredentialGroupID,
			CredentialGroupName:  groupName,
			CredentialGroupOrder: groupOrder,
			DailyCostLimitUSD:    dailyCostLimitUSD,
			Name:                 record.Name,
			BaseURL:              record.BaseURL,
			APIKey:               apiKey,
			IsActive:             record.IsActive,
			FailoverEnabled:      record.FailoverEnabled,
			SkipHealthCheck:      record.SkipHealthCheck,
			Priority:             record.Priority,
			Timeout:              time.Duration(record.TimeoutSeconds) * time.Second,
		})
	}
	sortCodexEndpoints(endpoints)
	return endpoints, nil
}

// GetHealthCheckEndpoints 返回当前健康统计范围内的参与端点，不改变活跃渠道。
func (m *EndpointManager) GetHealthCheckEndpoints(ctx context.Context, channelFailoverEnabled bool) ([]*CodexEndpoint, error) {
	channels, err := m.channelStore.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*CodexEndpoint, 0)
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if channelFailoverEnabled {
			if !channel.FailoverEnabled {
				continue
			}
		} else if !channel.IsActive {
			continue
		}
		endpoints, err := m.loadChannelEndpoints(ctx, channel)
		if err != nil {
			return nil, err
		}
		for _, endpoint := range endpoints {
			if endpoint.FailoverEnabled {
				result = append(result, endpoint)
			}
		}
	}
	return result, nil
}

// GetNextEndpoint 获取下一个可用端点（排除当前请求已失败的端点和本地限额策略跳过的凭证分组）
// 逻辑:
// 1. 过滤 failover_enabled = true 的端点
// 2. 排除 failedIDs 中的端点（本次请求已尝试过的）
// 3. 排除 skippedGroups 中的凭证分组（仅用于本地限额策略）
// 4. 过滤健康检查失败的普通端点；skip_health_check=true 的端点绕过健康过滤
// 5. 按配置的路由策略排序，返回第一个
// 6. 全部耗尽返回 error
func (m *EndpointManager) GetNextEndpoint(failedIDs []int64, skippedGroups ...credentialGroupRef) (*CodexEndpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.endpoints) == 0 {
		return nil, fmt.Errorf("无可用 Codex 端点")
	}

	failedSet := make(map[int64]struct{}, len(failedIDs))
	for _, id := range failedIDs {
		failedSet[id] = struct{}{}
	}

	candidates := make([]*CodexEndpoint, 0, len(m.endpoints))
	for _, ep := range m.endpoints {
		if !ep.FailoverEnabled {
			continue
		}
		if _, failed := failedSet[ep.ID]; failed {
			continue
		}
		if credentialGroupSkipped(ep, skippedGroups) {
			continue
		}
		if !m.endpointHealthAllowsRoutingLocked(ep) {
			continue
		}
		candidates = append(candidates, ep)
	}

	if len(candidates) > 0 {
		sortCodexEndpointCandidates(candidates, m.strategyTypeLocked(), m.latencies)
		return candidates[0], nil
	}

	return nil, fmt.Errorf("所有 Codex 端点均不可用（共 %d 个端点）", len(m.endpoints))
}

// GetPrimaryEndpoint 获取主端点。
// priority 驱动路由，IsActive 只在同优先级时作为兜底排序标记。
func (m *EndpointManager) GetPrimaryEndpoint(skippedGroups ...credentialGroupRef) (*CodexEndpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	candidates := make([]*CodexEndpoint, 0, len(m.endpoints))
	for _, e := range m.endpoints {
		if !e.FailoverEnabled || credentialGroupSkipped(e, skippedGroups) {
			continue
		}
		if !m.endpointHealthAllowsRoutingLocked(e) {
			continue
		}
		candidates = append(candidates, e)
	}
	if len(candidates) > 0 {
		sortCodexEndpointCandidates(candidates, m.strategyTypeLocked(), m.latencies)
		return candidates[0], nil
	}
	return nil, fmt.Errorf("无可用 Codex 端点")
}

func (m *EndpointManager) endpointHealthAllowsRoutingLocked(ep *CodexEndpoint) bool {
	if ep == nil {
		return false
	}
	if ep.SkipHealthCheck {
		return true
	}
	return !m.unhealthyMap[ep.ID]
}

func credentialGroupSkipped(ep *CodexEndpoint, skippedGroups []credentialGroupRef) bool {
	if ep == nil || len(skippedGroups) == 0 {
		return false
	}
	for _, group := range skippedGroups {
		if !group.valid() {
			continue
		}
		if group.ID > 0 {
			if ep.CredentialGroupID == group.ID {
				return true
			}
			continue
		}
		if ep.CredentialGroupName == group.Name {
			return true
		}
	}
	return false
}

// RecordLatency 记录健康检查测得的 RTT，供 fastest 策略排序使用。
func (m *EndpointManager) RecordLatency(endpointID int64, ms int64) {
	if endpointID <= 0 {
		return
	}
	m.mu.Lock()
	if m.latencies == nil {
		m.latencies = make(map[int64]int64)
	}
	m.latencies[endpointID] = ms
	m.mu.Unlock()
}

// MarkUnhealthy 标记端点为不健康（仅靠健康检查恢复，不再使用冷却时间）
func (m *EndpointManager) MarkUnhealthy(endpointID int64) {
	m.MarkUnhealthyWithDetail(endpointID, 0, "")
}

// MarkUnhealthyWithDetail 标记端点为不健康，并记录本次请求/探测的失败详情。
func (m *EndpointManager) MarkUnhealthyWithDetail(endpointID int64, statusCode int, reason string) {
	if endpointID <= 0 {
		return
	}

	m.mu.Lock()
	name := m.endpointNameLocked(endpointID)
	_, wasUnhealthy := m.unhealthyMap[endpointID]
	oldDetail, hadDetail := m.failureDetails[endpointID]
	m.unhealthyMap[endpointID] = true
	if statusCode > 0 || reason != "" {
		m.failureDetails[endpointID] = EndpointFailureDetail{StatusCode: statusCode, Reason: reason}
	}
	newDetail := m.failureDetails[endpointID]
	changed := !wasUnhealthy
	if statusCode > 0 || reason != "" {
		changed = changed || !hadDetail || oldDetail != newDetail
	}
	m.mu.Unlock()

	m.logger.Warn("Codex 端点标记为不健康", "endpoint_id", endpointID, "endpoint", name, "status_code", statusCode, "reason", reason)
	if changed {
		m.notifyStateChanged()
	}
}

// SetFailureDetail records the last failure detail without changing routing health.
func (m *EndpointManager) SetFailureDetail(endpointID int64, statusCode int, reason string) {
	if endpointID <= 0 {
		return
	}

	m.mu.Lock()
	oldDetail, hadDetail := m.failureDetails[endpointID]
	if statusCode > 0 || reason != "" {
		m.failureDetails[endpointID] = EndpointFailureDetail{StatusCode: statusCode, Reason: reason}
	} else {
		delete(m.failureDetails, endpointID)
	}
	newDetail, hasDetail := m.failureDetails[endpointID]
	changed := hadDetail != hasDetail || oldDetail != newDetail
	m.mu.Unlock()

	if changed {
		m.notifyStateChanged()
	}
}

// MarkHealthy 标记端点恢复健康（通常由健康检查器在探测成功后调用）
func (m *EndpointManager) MarkHealthy(endpointID int64) {
	if endpointID <= 0 {
		return
	}

	m.mu.Lock()
	_, wasUnhealthy := m.unhealthyMap[endpointID]
	_, hadDetail := m.failureDetails[endpointID]
	delete(m.unhealthyMap, endpointID)
	delete(m.failureDetails, endpointID)
	m.mu.Unlock()

	if wasUnhealthy || hadDetail {
		m.notifyStateChanged()
	}
}

// ResetUnhealthy 重置所有端点的不健康标记
// 用于渠道内循环重试（loop_retry）启动新一轮时清空已失败端点状态，
// 让所有端点重新可被选中
func (m *EndpointManager) ResetUnhealthy() {
	m.mu.Lock()
	if len(m.unhealthyMap) == 0 && len(m.failureDetails) == 0 {
		m.mu.Unlock()
		return
	}
	m.unhealthyMap = make(map[int64]bool)
	m.failureDetails = make(map[int64]EndpointFailureDetail)
	m.mu.Unlock()

	m.logger.Info("Codex 端点不健康标记已重置（loop_retry 新一轮）")
	m.notifyStateChanged()
}

// PromoteActive 将指定端点提升为活跃端点（同步到数据库的 codex_endpoints.is_active 字段）
// 用于故障转移成功后将实际工作的端点标记为活跃，让前端 UI 反映真实路由状态
// 不影响内存中的 endpoints 列表（IsActive 字段在 Reload 时刷新）
func (m *EndpointManager) PromoteActive(endpointID int64) error {
	if endpointID <= 0 {
		return fmt.Errorf("端点 ID 无效: %d", endpointID)
	}

	m.mu.RLock()
	var targetName string
	var alreadyActive bool
	for _, ep := range m.endpoints {
		if ep.ID == endpointID {
			targetName = ep.Name
			alreadyActive = ep.IsActive
			break
		}
	}
	m.mu.RUnlock()

	if targetName == "" {
		return fmt.Errorf("端点 ID %d 不在当前活跃渠道中", endpointID)
	}
	if alreadyActive {
		return nil // 已是活跃端点，无需更新
	}
	if m.endpointStore == nil {
		return fmt.Errorf("Codex 端点存储未初始化")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.endpointStore.SetActive(ctx, endpointID); err != nil {
		return fmt.Errorf("提升端点 %s 为活跃失败: %w", targetName, err)
	}

	// 同步更新内存状态（避免下次 Reload 之前 IsActive 不一致），并重新排序
	// 排序规则与 Reload 一致：凭证分组顺序 → priority ASC → IsActive DESC → ID ASC
	m.mu.Lock()
	for _, ep := range m.endpoints {
		ep.IsActive = ep.ID == endpointID
	}
	sortCodexEndpoints(m.endpoints)
	m.mu.Unlock()

	m.logger.Info("Codex 端点已提升为活跃（故障转移后同步）", "endpoint_id", endpointID, "endpoint", targetName)
	if m.onActiveChanged != nil {
		go m.onActiveChanged()
	}
	return nil
}

// SetOnActiveChanged 注册端点运行时状态变更回调（由 App 在启动时设置）。
// 活跃端点提升与健康状态变化都会触发，用于刷新前端 UI。
func (m *EndpointManager) SetOnActiveChanged(cb func()) {
	m.mu.Lock()
	m.onActiveChanged = cb
	m.mu.Unlock()
}

func (m *EndpointManager) notifyStateChanged() {
	m.mu.RLock()
	cb := m.onActiveChanged
	m.mu.RUnlock()
	if cb != nil {
		go cb()
	}
}

// IsHealthy 判断端点健康检查状态（不在 unhealthyMap 中即视为健康）。
// 请求路由会硬过滤健康检查失败的普通端点；skip_health_check=true 的端点绕过过滤。
func (m *EndpointManager) IsHealthy(endpointID int64) bool {
	if endpointID <= 0 {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return !m.unhealthyMap[endpointID]
}

func (m *EndpointManager) strategyTypeLocked() string {
	if m.strategyType == "fastest" {
		return "fastest"
	}
	return "priority"
}

func sortCodexEndpoints(endpoints []*CodexEndpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		return compareCodexEndpointPriority(endpoints[i], endpoints[j])
	})
}

func sortCodexEndpointCandidates(endpoints []*CodexEndpoint, strategyType string, latencies map[int64]int64) {
	sort.Slice(endpoints, func(i, j int) bool {
		left := endpoints[i]
		right := endpoints[j]
		if left.CredentialGroupOrder != right.CredentialGroupOrder {
			return left.CredentialGroupOrder < right.CredentialGroupOrder
		}
		if strategyType == "fastest" {
			if latencyOrder := compareCodexEndpointLatency(left, right, latencies); latencyOrder != 0 {
				return latencyOrder < 0
			}
		}
		return compareCodexEndpointPriority(left, right)
	})
}

func compareCodexEndpointPriority(left, right *CodexEndpoint) bool {
	if left.CredentialGroupOrder != right.CredentialGroupOrder {
		return left.CredentialGroupOrder < right.CredentialGroupOrder
	}
	if left.Priority != right.Priority {
		return left.Priority < right.Priority
	}
	if left.IsActive != right.IsActive {
		return left.IsActive
	}
	return left.ID < right.ID
}

func compareCodexEndpointLatency(left, right *CodexEndpoint, latencies map[int64]int64) int {
	leftLatency, leftOK := latencyValue(left, latencies)
	rightLatency, rightOK := latencyValue(right, latencies)
	if leftOK && rightOK {
		if leftLatency == rightLatency {
			return 0
		}
		if leftLatency < rightLatency {
			return -1
		}
		return 1
	}
	if leftOK {
		return -1
	}
	if rightOK {
		return 1
	}
	return 0
}

func latencyValue(endpoint *CodexEndpoint, latencies map[int64]int64) (int64, bool) {
	if endpoint == nil || latencies == nil {
		return 0, false
	}
	value, ok := latencies[endpoint.ID]
	if !ok || value < 0 {
		return 0, false
	}
	return value, true
}

func (m *EndpointManager) GetFailureDetail(endpointID int64) (int, string, bool) {
	if endpointID <= 0 {
		return 0, "", false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	detail, ok := m.failureDetails[endpointID]
	if !ok {
		return 0, "", false
	}
	return detail.StatusCode, detail.Reason, true
}

func (m *EndpointManager) endpointNameLocked(endpointID int64) string {
	for _, ep := range m.endpoints {
		if ep.ID == endpointID {
			return ep.Name
		}
	}
	return ""
}

// GetActiveChannel 获取当前活跃渠道信息
func (m *EndpointManager) GetActiveChannel() *store.CodexChannelRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeChannel
}

// GetAllEndpoints 获取所有端点（供健康检查器使用）
func (m *EndpointManager) GetAllEndpoints() []*CodexEndpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*CodexEndpoint, len(m.endpoints))
	copy(result, m.endpoints)
	return result
}
