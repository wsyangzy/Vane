package endpoint

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"ai-relay-forwarder/config"
)

// GroupInfo represents information about an endpoint group
// CooldownUntil 已移除，失败渠道通过手动"重试"按钮重新激活
type GroupInfo struct {
	Name      string
	Priority  int
	IsActive  bool
	Endpoints []*Endpoint
	// Manual control states
	ManuallyPaused       bool
	ManualActivationTime time.Time
	// Forced activation states
	ForcedActivation     bool      // 标记是否为强制激活（无健康端点时激活）
	ForcedActivationTime time.Time // 强制激活时间
}

// GroupManager manages endpoint groups
// 冷却机制已移除
type GroupManager struct {
	groups                 map[string]*GroupInfo
	config                 *config.Config
	mutex                  sync.RWMutex
	channelPriorities      map[string]int
	channelFailoverEnabled map[string]bool
	// Group change notification subscribers
	groupChangeSubscribers []chan string
	subscriberMutex        sync.RWMutex
}

// NewGroupManager creates a new group manager
// 冷却机制已移除
func NewGroupManager(cfg *config.Config) *GroupManager {
	return &GroupManager{
		groups:                 make(map[string]*GroupInfo),
		config:                 cfg,
		channelPriorities:      make(map[string]int),
		channelFailoverEnabled: make(map[string]bool),
		groupChangeSubscribers: make([]chan string, 0),
	}
}

// UpdateConfig updates the group manager configuration
// 冷却机制已移除
func (gm *GroupManager) UpdateConfig(cfg *config.Config) {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()
	gm.config = cfg
}

// UpdateChannelPriorities 更新“渠道(channel)”的优先级映射。
// 渠道优先级仅用于“渠道间”故障转移顺序；渠道内端点故障转移仍由端点 priority 决定。
func (gm *GroupManager) UpdateChannelPriorities(priorities map[string]int) {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	next := make(map[string]int, len(priorities))
	for name, p := range priorities {
		if name == "" {
			continue
		}
		if p <= 0 {
			p = 1
		}
		next[name] = p
	}
	gm.channelPriorities = next

	// 尽最大努力即时同步到已存在的组（不依赖 UpdateGroups 重建）
	for name, group := range gm.groups {
		if group == nil {
			continue
		}
		if p, ok := gm.channelPriorities[name]; ok {
			group.Priority = p
		}
	}
}

// UpdateChannelFailoverEnabled 更新“渠道(channel)”是否参与渠道间故障转移的开关映射。
// 该开关用于前端“暂停/恢复”按钮的持久化状态，并影响：
// - 跨渠道故障转移的候选筛选（跳过暂停渠道）
// - 定时健康检查/延迟检测的覆盖范围
func (gm *GroupManager) UpdateChannelFailoverEnabled(enabled map[string]bool) {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	next := make(map[string]bool, len(enabled))
	for name, v := range enabled {
		if name == "" {
			continue
		}
		next[name] = v
	}
	gm.channelFailoverEnabled = next

	// 同步到已存在的组（尽最大努力即时生效）
	for name, group := range gm.groups {
		if group == nil {
			continue
		}
		if v, ok := gm.channelFailoverEnabled[name]; ok && !v {
			group.ManuallyPaused = true
		} else {
			group.ManuallyPaused = false
		}
	}
}

// IsChannelFailoverEnabled 查询"渠道是否参与渠道间故障转移"。
// 默认值为 true（未配置时视为参与）。
func (gm *GroupManager) IsChannelFailoverEnabled(channel string) bool {
	gm.mutex.RLock()
	defer gm.mutex.RUnlock()
	if channel == "" {
		return true
	}
	v, ok := gm.channelFailoverEnabled[channel]
	if !ok {
		return true
	}
	return v
}

// SetSingleChannelFailoverEnabled 设置单个渠道的故障转移开关状态。
// 仅更新指定渠道，不影响其他渠道的状态。
// 解决恢复一个渠道时影响其他渠道的问题。
func (gm *GroupManager) SetSingleChannelFailoverEnabled(channel string, enabled bool) {
	if channel == "" {
		return
	}

	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	// 更新映射表
	gm.channelFailoverEnabled[channel] = enabled

	// 仅更新对应组的 ManuallyPaused 状态
	if group, ok := gm.groups[channel]; ok && group != nil {
		group.ManuallyPaused = !enabled
		slog.Info(fmt.Sprintf("🔄 [组管理] 渠道 [%s] 故障转移开关已设置为 %v", channel, enabled))
	}
}

// UpdateGroups rebuilds group information from endpoints
// 冷却机制已移除
func (gm *GroupManager) UpdateGroups(endpoints []*Endpoint) {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	// SQLite 模式下需要保留 IsActive 状态
	isSQLiteMode := gm.config.EndpointsStorage.Type == "sqlite"
	oldActiveGroups := snapshotActiveGroups(gm.groups, isSQLiteMode)
	newGroups := buildGroupsFromEndpoints(endpoints, oldActiveGroups)
	applyGroupPriorities(newGroups, gm.channelPriorities)
	applyGroupPauseState(newGroups, gm.channelFailoverEnabled)

	gm.groups = newGroups

	// Update active status
	gm.updateActiveGroups()
}

func snapshotActiveGroups(groups map[string]*GroupInfo, preserve bool) map[string]bool {
	activeGroups := make(map[string]bool)
	if !preserve {
		return activeGroups
	}
	for name, group := range groups {
		if group != nil {
			activeGroups[name] = group.IsActive
		}
	}
	return activeGroups
}

func buildGroupsFromEndpoints(endpoints []*Endpoint, activeGroups map[string]bool) map[string]*GroupInfo {
	newGroups := make(map[string]*GroupInfo)
	// 以"渠道(channel)"作为路由与故障转移单位；未配置 channel 则回退为端点名（兼容旧逻辑）
	for _, ep := range endpoints {
		groupName := ChannelKey(ep)

		group, exists := newGroups[groupName]
		if !exists {
			group = &GroupInfo{
				Name:      groupName,
				Endpoints: make([]*Endpoint, 0, 2),
				IsActive:  activeGroups[groupName],
				Priority:  ep.Config.Priority,
			}
			newGroups[groupName] = group
		}

		group.Endpoints = append(group.Endpoints, ep)

		// 组优先级：取组内最小 endpoint priority（越小越优先）
		if ep.Config.Priority < group.Priority {
			group.Priority = ep.Config.Priority
		}
	}
	return newGroups
}

func applyGroupPriorities(groups map[string]*GroupInfo, channelPriorities map[string]int) {
	// 渠道优先级优先于端点优先级（仅用于渠道间选择顺序）
	for name, group := range groups {
		if group == nil {
			continue
		}
		if p, ok := channelPriorities[name]; ok && p > 0 {
			group.Priority = p
			continue
		}
		if group.Priority <= 0 {
			group.Priority = 1
		}
	}
}

func applyGroupPauseState(groups map[string]*GroupInfo, channelFailoverEnabled map[string]bool) {
	// 渠道级暂停（持久化）：由 channels.failover_enabled 控制
	for name, group := range groups {
		if group == nil {
			continue
		}
		group.ManuallyPaused = false
		if v, ok := channelFailoverEnabled[name]; ok && !v {
			group.ManuallyPaused = true
		}

		// 端点级兜底：当且仅当组内所有端点都 failover_enabled=false 时，该组无法参与跨渠道故障转移。
		if allEndpointsFailoverDisabled(group.Endpoints) {
			group.ManuallyPaused = true
		}
	}
}

func allEndpointsFailoverDisabled(endpoints []*Endpoint) bool {
	allDisabled := true
	for _, ep := range endpoints {
		failoverEnabled := true
		if ep != nil && ep.Config.FailoverEnabled != nil {
			failoverEnabled = *ep.Config.FailoverEnabled
		}
		if failoverEnabled {
			allDisabled = false
			break
		}
	}
	return allDisabled
}

// updateActiveGroups updates which groups are currently active
// 冷却机制已移除
func (gm *GroupManager) updateActiveGroups() {
	// SQLite 模式下，禁用自动激活逻辑（由 enabled 字段控制）
	isSQLiteMode := gm.config.EndpointsStorage.Type == "sqlite"
	// Failover.Enabled 仅控制"渠道间"故障转移/自动切换行为
	autoSwitchEnabled := gm.config.Failover.Enabled

	var newlyActivatedGroup string

	// Track previous active state to detect changes
	previousActiveGroups := make(map[string]bool)
	for _, group := range gm.groups {
		previousActiveGroups[group.Name] = group.IsActive
	}

	// SQLite 模式下跳过自动激活逻辑（手动控制）
	if isSQLiteMode {
		return
	}

	// Determine which groups should be active based on priority
	// Only auto-activate next group if auto switching is enabled
	if autoSwitchEnabled {
		// Auto mode: 保持"粘性"活跃渠道
		sortedGroups := gm.getSortedGroups()

		var desiredActive *GroupInfo
		for _, group := range gm.groups {
			if group.IsActive {
				desiredActive = group
				break
			}
		}

		isAvailable := func(g *GroupInfo) bool {
			if g == nil {
				return false
			}
			return !g.ManuallyPaused
		}

		if !isAvailable(desiredActive) {
			desiredActive = nil
			for _, group := range sortedGroups {
				if isAvailable(group) {
					desiredActive = group
					break
				}
			}
		}

		for _, group := range gm.groups {
			wasActive := group.IsActive
			group.IsActive = (desiredActive != nil && group == desiredActive)
			if group.IsActive && !wasActive {
				newlyActivatedGroup = group.Name
			}
		}
	} else {
		// Manual mode: Only activate priority 1 group at startup if no groups are active
		currentActiveCount := 0
		for _, group := range gm.groups {
			if group.IsActive {
				currentActiveCount++
			}
		}

		// If no groups are active, determine if this is startup or runtime failure
		if currentActiveCount == 0 {
			// Check if this is truly startup (no groups have ever failed) or runtime failure
			isActualStartup := true
			for _, group := range gm.groups {
				if group.ManuallyPaused {
					isActualStartup = false
					break
				}
			}

			// Determine activation strategy based on startup vs runtime context
			var shouldAutoActivate bool
			if isActualStartup {
				shouldAutoActivate = true
				slog.Debug("🚀 [组管理] 检测到系统启动 - 尝试激活最高优先级可用组")
			} else {
				autoSwitchEnabled := gm.config.Failover.Enabled
				if !autoSwitchEnabled && gm.config.Failover.SuspendOnExhausted {
					shouldAutoActivate = false
					slog.Debug("⏸️ [组管理] 运行时故障且启用挂起 - 不激活其他组，等待挂起处理")
				} else {
					shouldAutoActivate = true
					slog.Debug("🔄 [组管理] 运行时故障但未启用挂起 - 尝试激活可用组")
				}
			}

			if shouldAutoActivate {
				sortedGroups := gm.getSortedGroups()
				for _, group := range sortedGroups {
					if !group.ManuallyPaused {
						hasHealthyEndpoints := false
						for _, ep := range group.Endpoints {
							if ep.IsHealthy() {
								hasHealthyEndpoints = true
								break
							}
						}
						if hasHealthyEndpoints {
							wasActive := group.IsActive
							group.IsActive = true
							autoSwitchEnabled := gm.config.Failover.Enabled
							if isActualStartup {
								if autoSwitchEnabled {
									slog.Info(fmt.Sprintf("🚀 [自动模式] 启动时激活最高优先级可用组: %s (有健康端点)", group.Name))
								} else {
									slog.Info(fmt.Sprintf("🚀 [手动模式] 启动时激活最高优先级可用组: %s (有健康端点) - 后续故障将启用挂起", group.Name))
								}
							} else {
								slog.Info(fmt.Sprintf("🔄 [运行时] 激活可用组: %s (优先级: %d, 有健康端点)", group.Name, group.Priority))
							}
							if !wasActive && group.IsActive {
								newlyActivatedGroup = group.Name
							}
							break
						}
					} else if group.ManuallyPaused {
						slog.Debug(fmt.Sprintf("⏸️ [手动模式] 跳过已暂停组: %s (优先级: %d) - 等待手动恢复", group.Name, group.Priority))
					}
				}
			}
		}
	}

	// Notify subscribers if a group was newly activated
	if newlyActivatedGroup != "" {
		if !previousActiveGroups[newlyActivatedGroup] {
			slog.Debug(fmt.Sprintf("📡 [组通知] 检测到组状态变化: %s 变为活跃", newlyActivatedGroup))
			gm.notifyGroupChange(newlyActivatedGroup)
		}
	}
}

// getSortedGroups returns groups sorted by priority (lower number = higher priority)
func (gm *GroupManager) getSortedGroups() []*GroupInfo {
	groups := make([]*GroupInfo, 0, len(gm.groups))
	for _, group := range gm.groups {
		groups = append(groups, group)
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Priority != groups[j].Priority {
			return groups[i].Priority < groups[j].Priority
		}
		return groups[i].Name < groups[j].Name
	})

	return groups
}

// GetActiveGroups returns currently active groups
//
// 🔧 [P3.1 修复] 改用写锁，因为 updateActiveGroups 会修改 group.IsActive
// 旧实现：用 RLock 但内部调用 updateActiveGroups（写操作），多个并发调用同时写 group.IsActive 触发 race
func (gm *GroupManager) GetActiveGroups() []*GroupInfo {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()
	return gm.getActiveGroupsLocked()
}

// getActiveGroupsLocked 内部方法：调用方必须已持有 gm.mutex.Lock()
// 🔧 [P3.1 新增] 抽取无锁版本，供已持锁的调用方使用，避免重入死锁
func (gm *GroupManager) getActiveGroupsLocked() []*GroupInfo {
	gm.updateActiveGroups()

	var active []*GroupInfo
	for _, group := range gm.groups {
		if group.IsActive {
			active = append(active, group)
		}
	}

	// Sort by priority
	sort.Slice(active, func(i, j int) bool {
		if active[i].Priority != active[j].Priority {
			return active[i].Priority < active[j].Priority
		}
		return active[i].Name < active[j].Name
	})

	return active
}

// GetAllGroups returns all groups
//
// 🔧 [P3.1 修复] 改用写锁，原因同 GetActiveGroups
func (gm *GroupManager) GetAllGroups() []*GroupInfo {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	gm.updateActiveGroups()

	groups := make([]*GroupInfo, 0, len(gm.groups))
	for _, group := range gm.groups {
		groups = append(groups, group)
	}

	// Sort by priority
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Priority != groups[j].Priority {
			return groups[i].Priority < groups[j].Priority
		}
		return groups[i].Name < groups[j].Name
	})

	return groups
}

// SetGroupCooldown 将组标记为暂停并停用
// 冷却机制已移除，失败渠道通过手动"重试"按钮重新激活
func (gm *GroupManager) SetGroupCooldown(groupName string) {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	if group, exists := gm.groups[groupName]; exists {
		group.IsActive = false
		group.ManuallyPaused = true
		slog.Warn(fmt.Sprintf("⏸️ [渠道暂停] 渠道 %s 已停用，需要手动点击\"重试\"按钮重新激活", groupName))

		// Update active groups after state change
		gm.updateActiveGroups()

		// Log and notify about next active group
		for _, g := range gm.getSortedGroups() {
			if g.IsActive {
				slog.Info(fmt.Sprintf("🔄 [渠道切换] 切换到下一优先级渠道: %s (优先级: %d)",
					g.Name, g.Priority))
				gm.notifyGroupChange(g.Name)
				break
			}
		}
	}
}

// IsGroupInCooldown 冷却机制已移除，返回 false（向后兼容）
func (gm *GroupManager) IsGroupInCooldown(groupName string) bool {
	return false
}

// GetGroupCooldownRemaining 冷却机制已移除，返回 0（向后兼容）
func (gm *GroupManager) GetGroupCooldownRemaining(groupName string) time.Duration {
	return 0
}

// ManualActivateGroup manually activates a specific group and deactivates others (compatibility function)
func (gm *GroupManager) ManualActivateGroup(groupName string) error {
	return gm.ManualActivateGroupWithForce(groupName, false)
}

// ActivateGroupForFailover 渠道间故障转移激活组（不记录手动激活时间）
// force: 当为true时，即使组内没有健康端点也强制激活
// 冷却机制已移除
func (gm *GroupManager) ActivateGroupForFailover(groupName string, force bool) error {
	return gm.activateGroupInternal(groupName, force, false)
}

// ManualActivateGroupWithForce manually activates a specific group and deactivates others
// force: 当为true时，即使组内没有健康端点也强制激活
// 冷却机制已移除
func (gm *GroupManager) ManualActivateGroupWithForce(groupName string, force bool) error {
	return gm.activateGroupInternal(groupName, force, true)
}

func (gm *GroupManager) activateGroupInternal(groupName string, force bool, recordManual bool) error {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	targetGroup, exists := gm.groups[groupName]
	if !exists {
		return fmt.Errorf("组不存在: %s", groupName)
	}

	// SQLite 模式下跳过健康检查（因为启动时健康检查还没开始）
	isSQLiteMode := gm.config.EndpointsStorage.Type == "sqlite"
	activationLabel := "手动"
	if !recordManual {
		activationLabel = "故障转移"
	}

	// 检查健康端点
	healthyCount := 0
	totalCount := len(targetGroup.Endpoints)
	for _, ep := range targetGroup.Endpoints {
		if ep.IsHealthy() {
			healthyCount++
		}
	}

	// SQLite 模式下，允许激活没有健康端点的组（用户手动控制）
	if isSQLiteMode {
		// SQLite 模式：直接激活，不检查健康状态
		if healthyCount == 0 {
			slog.Info(fmt.Sprintf("🔄 [SQLite模式] 激活端点: %s (健康检查待执行)", groupName))
		} else {
			slog.Info(fmt.Sprintf("🔄 [SQLite模式] 激活端点: %s (健康端点: %d/%d)", groupName, healthyCount, totalCount))
		}
	} else {
		// YAML 模式：保持原有的健康检查逻辑
		// 核心逻辑：强制激活只能在完全没有健康端点时使用
		if healthyCount == 0 {
			// 没有健康端点的情况
			if !force {
				return fmt.Errorf("组 %s 中没有健康的端点，无法激活。如需强制激活请使用强制模式", groupName)
			}
			// 强制激活：只有在没有健康端点时才允许
			slog.Warn(fmt.Sprintf("⚠️ [强制激活] %s激活无健康端点组: %s (健康端点: %d/%d, 操作时间: %s, 风险等级: HIGH)",
				activationLabel, groupName, healthyCount, totalCount, time.Now().Format("2006-01-02 15:04:05")))
			slog.Error(fmt.Sprintf("🚨 [安全警告] 强制激活可能导致请求失败! 组: %s, 建议尽快检查端点健康状态", groupName))

			// 标记强制激活
			targetGroup.ForcedActivation = true
			targetGroup.ForcedActivationTime = time.Now()
		} else {
			// 有健康端点的情况
			if force {
				// 拒绝在有健康端点时使用强制激活
				return fmt.Errorf("组 %s 有 %d 个健康端点，无需强制激活。请使用正常激活", groupName, healthyCount)
			}
			// 正常激活
			targetGroup.ForcedActivation = false
			targetGroup.ForcedActivationTime = time.Time{}

			slog.Info(fmt.Sprintf("🔄 [正常激活] %s激活组: %s (健康端点: %d/%d)",
				activationLabel, groupName, healthyCount, totalCount))
		}
	}

	// 停用所有组
	for _, group := range gm.groups {
		group.IsActive = false
	}

	// 激活目标组
	targetGroup.IsActive = true
	if recordManual {
		targetGroup.ManualActivationTime = time.Now()
	}
	// CooldownUntil 已移除

	if recordManual {
		// 重置目标组所有端点的 RequestFailed 状态
		// 用户手动激活（重试）时，需要重置端点状态让它们重新参与请求路由
		resetCount := 0
		for _, ep := range targetGroup.Endpoints {
			if ep == nil {
				continue
			}
			ep.mutex.Lock()
			if ep.Status.RequestFailed {
				ep.Status.RequestFailed = false
				ep.Status.Healthy = true      // 假定可用，依赖实际请求验证
				ep.Status.NeverChecked = true // 标记为"未检查"，让选择逻辑认为它是可用的
				resetCount++
			}
			ep.mutex.Unlock()
		}
		if resetCount > 0 {
			slog.Info(fmt.Sprintf("🔄 [手动激活] 重置组 %s 中 %d 个端点的请求失败状态", groupName, resetCount))
		}
	}

	// 通知订阅者
	gm.notifyGroupChange(groupName)

	return nil
}

// WasGroupManuallyActivatedSince 判断组是否在指定时间之后被手动激活
func (gm *GroupManager) WasGroupManuallyActivatedSince(groupName string, since time.Time) bool {
	if gm == nil {
		return false
	}
	gm.mutex.RLock()
	defer gm.mutex.RUnlock()

	group, exists := gm.groups[groupName]
	if !exists || group == nil || group.ManualActivationTime.IsZero() {
		return false
	}
	return !group.ManualActivationTime.Before(since)
}

// DeactivateGroup 停用指定组（用于故障转移时停用失败的端点）
// 注意：这只是简单地设置 IsActive=false，不设置 ManuallyPaused 标志
func (gm *GroupManager) DeactivateGroup(groupName string) error {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	targetGroup, exists := gm.groups[groupName]
	if !exists {
		return fmt.Errorf("组不存在: %s", groupName)
	}

	if targetGroup.IsActive {
		targetGroup.IsActive = false
		slog.Info(fmt.Sprintf("🔴 [组管理] 组已停用: %s", groupName))
	}

	return nil
}

// ManualPauseGroup manually pauses a group (prevents it from being auto-activated)
func (gm *GroupManager) ManualPauseGroup(groupName string, duration time.Duration) error {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	targetGroup, exists := gm.groups[groupName]
	if !exists {
		return fmt.Errorf("组不存在: %s", groupName)
	}

	// Pause the group
	targetGroup.ManuallyPaused = true
	gm.channelFailoverEnabled[groupName] = false

	// 非 SQLite 模式下，“暂停渠道”应立即让其退出活跃状态，避免继续被选中。
	// SQLite 模式下活跃状态由 enabled 字段/显式激活控制，这里不强制切换。
	isSQLiteMode := gm.config.EndpointsStorage.Type == "sqlite"
	if !isSQLiteMode && targetGroup.IsActive {
		targetGroup.IsActive = false
	}
	// 重新评估活跃组（非 SQLite 模式下可即时切换到其他可用渠道）
	gm.updateActiveGroups()

	if duration > 0 {
		// Set a timer to automatically unpause
		go func() {
			time.Sleep(duration)
			gm.mutex.Lock()
			defer gm.mutex.Unlock()
			if targetGroup.ManuallyPaused {
				targetGroup.ManuallyPaused = false
				gm.channelFailoverEnabled[groupName] = true
				// Store previous state to check for changes
				prevActiveGroups := make(map[string]bool)
				for _, g := range gm.groups {
					prevActiveGroups[g.Name] = g.IsActive
				}
				gm.updateActiveGroups()
				// Check if any group became newly active
				for _, g := range gm.groups {
					if g.IsActive && !prevActiveGroups[g.Name] {
						gm.notifyGroupChange(g.Name)
						break
					}
				}
				slog.Info(fmt.Sprintf("⏰ [自动恢复] 组 %s 暂停期已结束，重新可用", groupName))
			}
		}()
		slog.Info(fmt.Sprintf("⏸️ [手动暂停] 组 %s 已暂停 %v", groupName, duration))
	} else {
		slog.Info(fmt.Sprintf("⏸️ [手动暂停] 组 %s 已暂停，需要手动恢复", groupName))
	}

	return nil
}

// ResetAllPausedGroups 重置所有渠道的暂停状态
// 当启用渠道间转移时调用，确保所有渠道回到初始状态（显示"激活"而非"重试"）
func (gm *GroupManager) ResetAllPausedGroups() {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	resetCount := 0
	for name, group := range gm.groups {
		if group == nil {
			continue
		}
		if group.ManuallyPaused {
			group.ManuallyPaused = false
			gm.channelFailoverEnabled[name] = true
			resetCount++
		}
	}

	if resetCount > 0 {
		slog.Info(fmt.Sprintf("🔄 [组管理] 已重置 %d 个渠道的暂停状态", resetCount))
		gm.updateActiveGroups()
	}
}

// ManualResumeGroup manually resumes a paused group
func (gm *GroupManager) ManualResumeGroup(groupName string) error {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	targetGroup, exists := gm.groups[groupName]
	if !exists {
		return fmt.Errorf("组不存在: %s", groupName)
	}

	if !targetGroup.ManuallyPaused {
		return fmt.Errorf("组 %s 未处于暂停状态", groupName)
	}

	targetGroup.ManuallyPaused = false
	gm.channelFailoverEnabled[groupName] = true

	// Store previous active groups to detect changes
	prevActiveGroups := make(map[string]bool)
	for _, g := range gm.groups {
		prevActiveGroups[g.Name] = g.IsActive
	}

	gm.updateActiveGroups() // Re-evaluate active groups

	// Check if any group became newly active
	notified := false
	for _, g := range gm.groups {
		if g.IsActive && !prevActiveGroups[g.Name] {
			gm.notifyGroupChange(g.Name)
			slog.Debug(fmt.Sprintf("📡 [组通知] 因恢复组 %s 而激活组 %s", groupName, g.Name))
			notified = true
			break
		}
	}
	if !notified {
		// 即便“粘性活跃渠道”未导致自动回切，也应通知订阅者刷新状态（避免前端/测试卡住）
		gm.notifyGroupChange(groupName)
	}

	slog.Info(fmt.Sprintf("▶️ [手动恢复] 组 %s 已恢复，重新参与自动选择", groupName))
	return nil
}

// GetGroupDetails returns detailed information about all groups
//
// 🔧 [P3.1 修复] 改用写锁，因为 updateActiveGroups 会修改 group.IsActive
// 同时内部调用 getActiveGroupsLocked 避免重入死锁
func (gm *GroupManager) GetGroupDetails() map[string]interface{} {
	gm.mutex.Lock()
	defer gm.mutex.Unlock()

	gm.updateActiveGroups()

	result := make(map[string]interface{})
	groupsData := make([]map[string]interface{}, 0, len(gm.groups))

	for _, group := range gm.groups {
		healthyCount := 0
		unhealthyCount := 0
		totalEndpoints := len(group.Endpoints)

		for _, ep := range group.Endpoints {
			if ep.IsHealthy() {
				healthyCount++
			} else {
				unhealthyCount++
			}
		}

		var status string
		var statusColor string

		if group.IsActive {
			status = "活跃"
			statusColor = "success"
		} else if group.ManuallyPaused {
			status = "已暂停"
			statusColor = "warning"
		} else if healthyCount == 0 {
			status = "无健康端点"
			statusColor = "danger"
		} else {
			status = "可用"
			statusColor = "secondary"
		}

		// 冷却机制已移除，简化状态字段
		groupData := map[string]interface{}{
			"name":                   group.Name,
			"priority":               group.Priority,
			"is_active":              group.IsActive,
			"status":                 status,
			"status_color":           statusColor,
			"total_endpoints":        totalEndpoints,
			"healthy_endpoints":      healthyCount,
			"unhealthy_endpoints":    unhealthyCount,
			"manually_paused":        group.ManuallyPaused,
			"in_cooldown":            false, // 冷却机制已移除，始终为 false
			"cooldown_remaining":     "0s",  // 向后兼容
			"can_activate":           healthyCount > 0 && !group.IsActive,
			"can_pause":              !group.ManuallyPaused,
			"can_resume":             group.ManuallyPaused,
			"forced_activation":      group.ForcedActivation,
			"forced_activation_time": "",
			"activation_type":        "normal",
			"can_force_activate":     healthyCount == 0 && !group.IsActive,
		}

		// 添加强制激活时间
		if !group.ForcedActivationTime.IsZero() {
			groupData["forced_activation_time"] = group.ForcedActivationTime.Format("2006-01-02 15:04:05")
		}

		// 设置激活类型
		if group.ForcedActivation {
			groupData["activation_type"] = "forced"
			// 计算健康状态描述
			if healthyCount == 0 {
				groupData["_computed_health_status"] = "强制激活(无健康端点)"
			} else {
				groupData["_computed_health_status"] = "强制激活(已恢复)"
			}
		}

		if !group.ManualActivationTime.IsZero() {
			groupData["last_manual_activation"] = group.ManualActivationTime.Format("2006-01-02 15:04:05")
		}

		groupsData = append(groupsData, groupData)
	}

	// Sort by priority
	sort.Slice(groupsData, func(i, j int) bool {
		return groupsData[i]["priority"].(int) < groupsData[j]["priority"].(int)
	})

	result["groups"] = groupsData
	result["total_groups"] = len(groupsData)
	// 🔧 [P3.1 修复] 用 getActiveGroupsLocked 避免重入死锁
	result["active_groups"] = len(gm.getActiveGroupsLocked())

	return result
}

// FilterEndpointsByActiveGroups filters endpoints to only include those in active groups
// 一端点一组架构，组名 = 端点名
func (gm *GroupManager) FilterEndpointsByActiveGroups(endpoints []*Endpoint) []*Endpoint {
	activeGroups := gm.GetActiveGroups()
	if len(activeGroups) == 0 {
		return nil
	}

	// Create a map of active group names for quick lookup
	activeGroupNames := make(map[string]bool)
	for _, group := range activeGroups {
		activeGroupNames[group.Name] = true
	}

	// Filter endpoints
	// 组名 = 渠道(channel)，未配置 channel 则回退为端点名
	var filtered []*Endpoint
	for _, ep := range endpoints {
		groupName := ChannelKey(ep)

		if activeGroupNames[groupName] {
			filtered = append(filtered, ep)
		}
	}

	return filtered
}

// SubscribeToGroupChanges subscribes to group change notifications
// Returns a channel that will receive the name of the newly activated group
func (gm *GroupManager) SubscribeToGroupChanges() <-chan string {
	gm.subscriberMutex.Lock()
	defer gm.subscriberMutex.Unlock()

	// Create a buffered channel to avoid blocking the sender
	ch := make(chan string, 10)
	gm.groupChangeSubscribers = append(gm.groupChangeSubscribers, ch)

	slog.Debug(fmt.Sprintf("📡 [组通知] 新增订阅者，当前订阅者数: %d", len(gm.groupChangeSubscribers)))

	return ch
}

// UnsubscribeFromGroupChanges removes a subscriber from group change notifications
func (gm *GroupManager) UnsubscribeFromGroupChanges(ch <-chan string) {
	gm.subscriberMutex.Lock()
	defer gm.subscriberMutex.Unlock()

	// Find and remove the channel from subscribers
	for i, subscriber := range gm.groupChangeSubscribers {
		if subscriber == ch {
			// Remove the channel from the slice
			gm.groupChangeSubscribers = append(gm.groupChangeSubscribers[:i], gm.groupChangeSubscribers[i+1:]...)
			// Close the channel to signal unsubscription
			close(subscriber)
			slog.Debug(fmt.Sprintf("📡 [组通知] 移除订阅者，当前订阅者数: %d", len(gm.groupChangeSubscribers)))
			return
		}
	}
}

// notifyGroupChange sends a non-blocking notification to all subscribers
// This method should be called with appropriate locks already held
func (gm *GroupManager) notifyGroupChange(activatedGroupName string) {
	gm.subscriberMutex.RLock()
	defer gm.subscriberMutex.RUnlock()

	if len(gm.groupChangeSubscribers) == 0 {
		return
	}

	slog.Debug(fmt.Sprintf("📡 [组通知] 广播组切换事件: %s (订阅者数: %d)",
		activatedGroupName, len(gm.groupChangeSubscribers)))

	// Send notification to all subscribers in a non-blocking manner
	for i, subscriber := range gm.groupChangeSubscribers {
		select {
		case subscriber <- activatedGroupName:
			// Successfully sent
		default:
			// Channel is full or closed, log warning
			slog.Warn(fmt.Sprintf("📡 [组通知] 订阅者 #%d 通道已满或已关闭，跳过通知", i))
		}
	}
}
