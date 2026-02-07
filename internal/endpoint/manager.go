// manager.go - 端点管理器核心结构和基础功能
// 其他功能已拆分到独立文件：
// - health_check.go: 健康检查相关
// - endpoint_selection.go: 端点选择/路由
// - endpoint_crud.go: 动态端点管理
// - failover.go: 故障转移
// - key_switch.go: Key 切换
// - notification.go: 通知相关

package endpoint

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"claude-request-forwarder/config"
	"claude-request-forwarder/internal/events"
	"claude-request-forwarder/internal/transport"
)

// EndpointStatus represents the health status of an endpoint
// CooldownUntil 和 CooldownReason 已移除，失败渠道通过手动"重试"按钮重新激活
type EndpointStatus struct {
	Healthy          bool
	LastCheck        time.Time
	ResponseTime     time.Duration
	ConsecutiveFails int
	NeverChecked     bool // 表示从未被检测过
	// 请求失败标记，与 Healthy（健康检查状态）分离
	// - Healthy: 由健康检查更新，表示端点是否"可达"
	// - RequestFailed: 由请求转发更新，表示端点是否"因请求失败而被标记"
	// 端点可用条件：Healthy=true && RequestFailed=false
	// 重置时机：loop_retry 触发时重置为 false
	RequestFailed bool
	// 记录最后一次请求失败的 HTTP 状态码，用于 UI 显示
	LastRequestFailCode int
	// 记录最后一次请求失败的原因，用于 UI 显示
	LastRequestFailReason string
}

// Endpoint represents an endpoint with its configuration and status
type Endpoint struct {
	Config config.EndpointConfig
	Status EndpointStatus
	mutex  sync.RWMutex
}

// Manager manages endpoints and their health status
type Manager struct {
	endpoints    []*Endpoint
	endpointsMu  sync.RWMutex // 保护 endpoints 切片的并发访问
	config       *config.Config
	client       *http.Client
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	fastTester   *FastTester
	groupManager *GroupManager
	keyManager   *KeyManager // 管理多 API Key 状态
	// EventBus for decoupled event publishing
	eventBus events.EventBus
	// 健康检查完成回调（用于推送 Wails 事件）
	onHealthCheckComplete func()
	// 故障转移回调（用于同步数据库）
	// 参数: failedChannel 失败的渠道名, newChannel 新激活的渠道名
	onFailoverTriggered func(failedChannel, newChannel string)
	// 健康检查回调防抖机制，避免并发健康检查触发多次回调
	healthCheckDebounceMu    sync.Mutex
	healthCheckDebounceTimer *time.Timer
	// 🛑 [手动重试] 故障转移排序起点（由用户选择的渠道）
	failoverStartMu      sync.RWMutex
	failoverStartChannel string
}

// UpdateChannelPriorities 同步渠道优先级到运行时组管理器，用于“渠道间”故障转移顺序。
func (m *Manager) UpdateChannelPriorities(priorities map[string]int) {
	if m == nil || m.groupManager == nil {
		return
	}
	m.groupManager.UpdateChannelPriorities(priorities)
}

// UpdateChannelFailoverEnabled 同步"渠道是否参与渠道间故障转移"的开关到运行时组管理器。
func (m *Manager) UpdateChannelFailoverEnabled(enabled map[string]bool) {
	if m == nil || m.groupManager == nil {
		return
	}
	m.groupManager.UpdateChannelFailoverEnabled(enabled)
}

// SetSingleChannelFailoverEnabled 设置单个渠道的故障转移开关状态。
// 仅更新指定渠道，不影响其他渠道。
func (m *Manager) SetSingleChannelFailoverEnabled(channel string, enabled bool) {
	if m == nil || m.groupManager == nil {
		return
	}
	m.groupManager.SetSingleChannelFailoverEnabled(channel, enabled)
}

// SetFailoverStartChannel 设置故障转移排序起点（用户手动重试选择的渠道）
func (m *Manager) SetFailoverStartChannel(channel string) {
	if m == nil {
		return
	}
	m.failoverStartMu.Lock()
	m.failoverStartChannel = channel
	m.failoverStartMu.Unlock()
}

// GetFailoverStartChannel 获取故障转移排序起点
func (m *Manager) GetFailoverStartChannel() string {
	if m == nil {
		return ""
	}
	m.failoverStartMu.RLock()
	defer m.failoverStartMu.RUnlock()
	return m.failoverStartChannel
}

// NewManager creates a new endpoint manager
func NewManager(cfg *config.Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	// Create transport with proxy support
	httpTransport, err := transport.CreateTransport(cfg)
	if err != nil {
		slog.Error(fmt.Sprintf("❌ Failed to create HTTP transport with proxy: %s", err.Error()))
		// Fall back to default transport
		httpTransport = &http.Transport{}
	}

	manager := &Manager{
		config: cfg,
		client: &http.Client{
			Timeout:   cfg.Health.Timeout,
			Transport: httpTransport,
		},
		ctx:          ctx,
		cancel:       cancel,
		fastTester:   NewFastTester(cfg),
		groupManager: NewGroupManager(cfg),
		keyManager:   NewKeyManager(), // 初始化 Key 管理器
	}

	// Initialize endpoints
	for _, endpointCfg := range cfg.Endpoints {
		endpoint := &Endpoint{
			Config: endpointCfg,
			Status: EndpointStatus{
				Healthy:      false, // Start pessimistic, let health checks determine actual status
				LastCheck:    time.Now(),
				NeverChecked: true, // 标记为未检测
			},
		}
		manager.endpoints = append(manager.endpoints, endpoint)

		// 初始化端点的 Key 状态
		tokenCount := len(endpointCfg.Tokens)
		if tokenCount == 0 && endpointCfg.Token != "" {
			tokenCount = 1 // 单 Token 算作 1 个
		}
		apiKeyCount := len(endpointCfg.ApiKeys)
		if apiKeyCount == 0 && endpointCfg.ApiKey != "" {
			apiKeyCount = 1 // 单 API Key 算作 1 个
		}
		manager.keyManager.InitEndpoint(endpointKeyFromConfig(endpointCfg), tokenCount, apiKeyCount)
	}

	// Set manager reference in fast tester for dynamic token resolution
	manager.fastTester.SetManager(manager)

	// Initialize groups from endpoints
	manager.groupManager.UpdateGroups(manager.endpoints)

	return manager
}

// Start starts the health checking routine
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.healthCheckLoop()
}

// Stop stops the health checking routine
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
}

// UpdateConfig updates the manager configuration (hot-reload)
// Desktop: 只更新配置参数，不重建端点（端点完全由数据库管理）
func (m *Manager) UpdateConfig(cfg *config.Config) {
	m.config = cfg

	// 只更新 GroupManager 配置
	m.groupManager.UpdateConfig(cfg)
	slog.Debug("🔄 [热更新] 更新配置参数完成，端点保持不变")

	// Update fast tester with new config
	if m.fastTester != nil {
		m.fastTester.UpdateConfig(cfg)
	}

	// Recreate transport with new proxy configuration
	if transport, err := transport.CreateTransport(cfg); err == nil {
		m.client = &http.Client{
			Transport: transport,
			Timeout:   cfg.Health.Timeout,
		}
	}
}

// GetTokenForEndpoint dynamically resolves the token for an endpoint
// If the endpoint has its own token, return it
// If not, find the first endpoint in the same group that has a token
// 支持多 Token 配置：优先使用 tokens 数组中当前激活的 Token
func (m *Manager) GetTokenForEndpoint(ep *Endpoint) string {
	// 1. 优先使用多 Tokens 配置（端点独立管理）
	if len(ep.Config.Tokens) > 0 {
		activeIndex := m.keyManager.GetActiveTokenIndex(endpointKeyFromConfig(ep.Config))
		if activeIndex >= 0 && activeIndex < len(ep.Config.Tokens) {
			return sanitizeCredential(ep.Config.Tokens[activeIndex].Value)
		}
		return sanitizeCredential(ep.Config.Tokens[0].Value) // 回退到第一个
	}

	// 2. 使用单 Token 配置
	if ep.Config.Token != "" {
		return sanitizeCredential(ep.Config.Token)
	}

	// 3. 组内继承（仅对单 Token 保持原有行为，多 Token 不继承）
	groupName := ep.Config.Group
	if groupName == "" {
		groupName = "Default"
	}

	// 使用读锁遍历 endpoints
	m.endpointsMu.RLock()
	defer m.endpointsMu.RUnlock()

	// Search through all endpoints for the same group
	for _, endpoint := range m.endpoints {
		endpointGroup := endpoint.Config.Group
		if endpointGroup == "" {
			endpointGroup = "Default"
		}

		// If same group and has token (only single token inheritance)
		if endpointGroup == groupName && endpoint.Config.Token != "" {
			return sanitizeCredential(endpoint.Config.Token)
		}
	}

	// 4. No token found in the group
	return ""
}

// GetApiKeyForEndpoint dynamically resolves the API key for an endpoint
// If the endpoint has its own api-key, return it
// If not, find the first endpoint in the same group that has an api-key
// 支持多 API Key 配置：优先使用 api-keys 数组中当前激活的 API Key
func (m *Manager) GetApiKeyForEndpoint(ep *Endpoint) string {
	// 1. 优先使用多 ApiKeys 配置（端点独立管理）
	if len(ep.Config.ApiKeys) > 0 {
		activeIndex := m.keyManager.GetActiveApiKeyIndex(endpointKeyFromConfig(ep.Config))
		if activeIndex >= 0 && activeIndex < len(ep.Config.ApiKeys) {
			return sanitizeCredential(ep.Config.ApiKeys[activeIndex].Value)
		}
		return sanitizeCredential(ep.Config.ApiKeys[0].Value) // 回退到第一个
	}

	// 2. 使用单 ApiKey 配置
	if ep.Config.ApiKey != "" {
		return sanitizeCredential(ep.Config.ApiKey)
	}

	// 3. 组内继承（仅对单 ApiKey 保持原有行为，多 ApiKey 不继承）
	groupName := ep.Config.Group
	if groupName == "" {
		groupName = "Default"
	}

	// 使用读锁遍历 endpoints
	m.endpointsMu.RLock()
	defer m.endpointsMu.RUnlock()

	// Search through all endpoints for the same group
	for _, endpoint := range m.endpoints {
		endpointGroup := endpoint.Config.Group
		if endpointGroup == "" {
			endpointGroup = "Default"
		}

		// If same group and has api-key (only single api-key inheritance)
		if endpointGroup == groupName && endpoint.Config.ApiKey != "" {
			return sanitizeCredential(endpoint.Config.ApiKey)
		}
	}

	// 4. No api-key found in the group
	return ""
}

func sanitizeCredential(value string) string {
	v := strings.TrimSpace(value)
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	v = strings.ReplaceAll(v, "\t", "")
	return v
}

// GetConfig returns the manager's configuration
func (m *Manager) GetConfig() *config.Config {
	return m.config
}

// GetGroupManager returns the group manager
func (m *Manager) GetGroupManager() *GroupManager {
	return m.groupManager
}

// SetEndpointLastRequestFailCode 设置端点最后一次请求失败的 HTTP 状态码
// 用于 UI 显示失败原因
// 新增 reason 参数，记录详细错误信息
func (m *Manager) SetEndpointLastRequestFailCode(endpointName string, httpCode int) {
	m.SetEndpointLastRequestFail(endpointName, httpCode, "")
}

// SetEndpointLastRequestFail 设置端点最后一次请求失败的信息（状态码 + 原因）
// 用于 UI 显示详细失败原因
func (m *Manager) SetEndpointLastRequestFail(endpointName string, httpCode int, reason string) {
	if m == nil || endpointName == "" {
		return
	}

	// 先尝试精确匹配
	ep := m.GetEndpointByNameAny(endpointName)
	if ep == nil {
		return
	}

	ep.mutex.Lock()
	ep.Status.LastRequestFailCode = httpCode
	ep.Status.LastRequestFailReason = reason
	ep.mutex.Unlock()
}

// SetEndpointSkipHealthCheck 设置端点是否跳过健康检测（运行时同步）
// 用于前端快捷开关
func (m *Manager) SetEndpointSkipHealthCheck(endpointKey string, skip bool) {
	if m == nil || endpointKey == "" {
		return
	}

	m.endpointsMu.Lock()
	defer m.endpointsMu.Unlock()

	for _, ep := range m.endpoints {
		key := EndpointKey(ep.Config.Channel, ep.Config.Name)
		if key == endpointKey {
			ep.mutex.Lock()
			ep.Config.SkipHealthCheck = skip
			// 如果设置为跳过检测，将状态设为健康（视为可用）
			if skip {
				ep.Status.Healthy = true
			}
			ep.mutex.Unlock()
			return
		}
	}
}
