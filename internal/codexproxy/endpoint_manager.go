// endpoint_manager.go - Codex 端点管理器
// 管理活跃渠道的端点列表，提供故障转移选择逻辑。

package codexproxy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"claude-request-forwarder/internal/store"
)

// CodexEndpoint 运行时端点信息（合并渠道与端点数据）
type CodexEndpoint struct {
	ID              int64
	ChannelID       int64
	Name            string
	BaseURL         string
	APIKey          string // 来自所属 channel
	IsActive        bool
	FailoverEnabled bool
	Priority        int
}

// EndpointManager 管理活跃渠道的端点，提供故障转移选择
type EndpointManager struct {
	// 依赖
	channelStore  store.CodexChannelStore
	endpointStore store.CodexEndpointStore

	// 运行时状态
	mu            sync.RWMutex
	endpoints     []*CodexEndpoint          // 按 priority ASC 排序
	activeChannel *store.CodexChannelRecord
	unhealthyMap  map[string]time.Time // name -> unhealthy until
	cooldown      time.Duration        // 默认冷却时间

	// 活跃端点变更回调（由 App 注入，用于发送 Wails 事件刷新前端 UI）
	onActiveChanged func()

	logger *slog.Logger
}

// NewEndpointManager 创建端点管理器实例
func NewEndpointManager(
	channelStore store.CodexChannelStore,
	endpointStore store.CodexEndpointStore,
	logger *slog.Logger,
) *EndpointManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &EndpointManager{
		channelStore:  channelStore,
		endpointStore: endpointStore,
		unhealthyMap:  make(map[string]time.Time),
		cooldown:      30 * time.Second,
		logger:        logger,
	}
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

	records, err := m.endpointStore.ListByChannelID(ctx, channel.ID)
	if err != nil {
		return fmt.Errorf("获取渠道 %s 的端点列表失败: %w", channel.Name, err)
	}

	endpoints := make([]*CodexEndpoint, 0, len(records))
	for _, r := range records {
		endpoints = append(endpoints, &CodexEndpoint{
			ID:              r.ID,
			ChannelID:       r.ChannelID,
			Name:            r.Name,
			BaseURL:         r.BaseURL,
			APIKey:          channel.ApiKey,
			IsActive:        r.IsActive,
			FailoverEnabled: r.FailoverEnabled,
			Priority:        r.Priority,
		})
	}

	// 排序：
	// 1. priority ASC（数字小优先）
	// 2. 同 priority 时，IsActive=true 的端点优先（用户手动设置的活跃端点）
	// 3. 同 priority 且都非活跃时，按 ID 升序（创建时间早的优先，提供确定性顺序）
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Priority != endpoints[j].Priority {
			return endpoints[i].Priority < endpoints[j].Priority
		}
		if endpoints[i].IsActive != endpoints[j].IsActive {
			return endpoints[i].IsActive // true 排前面
		}
		return endpoints[i].ID < endpoints[j].ID
	})

	m.mu.Lock()
	m.activeChannel = channel
	m.endpoints = endpoints
	m.mu.Unlock()

	m.logger.Info("Codex 端点已重新加载",
		"channel", channel.Name,
		"endpoint_count", len(endpoints),
	)
	return nil
}

// GetNextEndpoint 获取下一个可用端点（排除已失败的和不健康的）
// 逻辑:
// 1. 过滤 failover_enabled = true 的端点
// 2. 排除 unhealthyMap 中未过期的端点
// 3. 排除 failedNames 中的端点（本次请求已尝试过的）
// 4. 按 priority ASC 排序，返回第一个
// 5. 全部耗尽返回 error
func (m *EndpointManager) GetNextEndpoint(failedNames []string) (*CodexEndpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.endpoints) == 0 {
		return nil, fmt.Errorf("无可用 Codex 端点")
	}

	now := time.Now()
	failedSet := make(map[string]struct{}, len(failedNames))
	for _, name := range failedNames {
		failedSet[name] = struct{}{}
	}

	for _, ep := range m.endpoints {
		if !ep.FailoverEnabled {
			continue
		}
		if _, failed := failedSet[ep.Name]; failed {
			continue
		}
		if until, unhealthy := m.unhealthyMap[ep.Name]; unhealthy && now.Before(until) {
			continue
		}
		return ep, nil
	}

	return nil, fmt.Errorf("所有 Codex 端点均不可用（共 %d 个端点）", len(m.endpoints))
}

// GetPrimaryEndpoint 获取主端点
// 优先级：按 priority ASC 排序选择第一个健康的端点
// 这样 priority 才能真正驱动路由（不再仅依赖 IsActive 标记）
// IsActive 仅作为 UI"当前激活"的展示标记，由 PromoteActive 跟随实际路由更新
func (m *EndpointManager) GetPrimaryEndpoint() (*CodexEndpoint, error) {
	// 复用 GetNextEndpoint 的健康/优先级筛选逻辑，传入空 failedNames 表示首次选择
	ep, err := m.GetNextEndpoint(nil)
	if err == nil {
		return ep, nil
	}

	// 健康端点全部耗尽时降级：返回 IsActive 标记的端点（即便不健康）
	// 进一步降级：返回第一个端点（如果有的话）
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.endpoints {
		if e.IsActive {
			return e, nil
		}
	}
	if len(m.endpoints) > 0 {
		return m.endpoints[0], nil
	}
	return nil, fmt.Errorf("无可用 Codex 端点")
}

// MarkUnhealthy 标记端点为不健康，在 cooldown 时间内不会被选中
func (m *EndpointManager) MarkUnhealthy(name string, cooldown time.Duration) {
	if cooldown <= 0 {
		cooldown = m.cooldown
	}
	m.mu.Lock()
	m.unhealthyMap[name] = time.Now().Add(cooldown)
	m.mu.Unlock()

	m.logger.Warn("Codex 端点标记为不健康",
		"endpoint", name,
		"cooldown", cooldown.String(),
	)
}

// MarkHealthy 标记端点恢复健康
func (m *EndpointManager) MarkHealthy(name string) {
	m.mu.Lock()
	delete(m.unhealthyMap, name)
	m.mu.Unlock()
}

// PromoteActive 将指定端点提升为活跃端点（同步到数据库的 codex_endpoints.is_active 字段）
// 用于故障转移成功后将实际工作的端点标记为活跃，让前端 UI 反映真实路由状态
// 不影响内存中的 endpoints 列表（IsActive 字段在 Reload 时刷新）
func (m *EndpointManager) PromoteActive(name string) error {
	m.mu.RLock()
	var targetID int64
	var alreadyActive bool
	for _, ep := range m.endpoints {
		if ep.Name == name {
			targetID = ep.ID
			alreadyActive = ep.IsActive
			break
		}
	}
	m.mu.RUnlock()

	if targetID == 0 {
		return fmt.Errorf("端点 %s 不在当前活跃渠道中", name)
	}
	if alreadyActive {
		return nil // 已是活跃端点，无需更新
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.endpointStore.SetActive(ctx, targetID); err != nil {
		return fmt.Errorf("提升端点 %s 为活跃失败: %w", name, err)
	}

	// 同步更新内存状态（避免下次 Reload 之前 IsActive 不一致），并重新排序
	// 排序规则与 Reload 一致：priority ASC → IsActive DESC → ID ASC
	m.mu.Lock()
	for _, ep := range m.endpoints {
		ep.IsActive = ep.Name == name
	}
	sort.Slice(m.endpoints, func(i, j int) bool {
		if m.endpoints[i].Priority != m.endpoints[j].Priority {
			return m.endpoints[i].Priority < m.endpoints[j].Priority
		}
		if m.endpoints[i].IsActive != m.endpoints[j].IsActive {
			return m.endpoints[i].IsActive
		}
		return m.endpoints[i].ID < m.endpoints[j].ID
	})
	m.mu.Unlock()

	m.logger.Info("Codex 端点已提升为活跃（故障转移后同步）", "endpoint", name)
	if m.onActiveChanged != nil {
		go m.onActiveChanged()
	}
	return nil
}

// SetOnActiveChanged 注册活跃端点变更回调（由 App 在启动时设置）
// 用于在故障转移成功提升端点后触发 Wails 事件，前端 UI 实时刷新
func (m *EndpointManager) SetOnActiveChanged(cb func()) {
	m.mu.Lock()
	m.onActiveChanged = cb
	m.mu.Unlock()
}

// IsHealthy 判断端点是否健康（不在 unhealthyMap 中或冷却已过期）
// 用于前端健康图表统计；与 GetNextEndpoint 的健康判断逻辑保持一致。
func (m *EndpointManager) IsHealthy(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	until, exists := m.unhealthyMap[name]
	if !exists {
		return true
	}
	return !time.Now().Before(until)
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
