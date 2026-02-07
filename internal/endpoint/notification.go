// notification.go - 通知相关功能
// 包含 EventBus 事件发布、组状态通知、Web 界面通知等

package endpoint

import (
	"fmt"
	"log/slog"
	"time"

	"claude-request-forwarder/internal/events"
	"claude-request-forwarder/internal/utils"
)

// SetEventBus 设置EventBus事件总线
func (m *Manager) SetEventBus(eventBus events.EventBus) {
	m.eventBus = eventBus
}

// notifyWebInterface 通过EventBus发布端点状态变化事件
func (m *Manager) notifyWebInterface(endpoint *Endpoint) {
	if m.eventBus == nil {
		return
	}

	endpoint.mutex.RLock()
	status := endpoint.Status
	endpoint.mutex.RUnlock()

	// 确定事件类型和优先级
	eventType := events.EventEndpointHealthy
	priority := events.PriorityHigh
	changeType := "status_changed"

	if !status.Healthy {
		eventType = events.EventEndpointUnhealthy
		priority = events.PriorityCritical
		changeType = "health_changed"
	}

	m.eventBus.Publish(events.Event{
		Type:     eventType,
		Source:   "endpoint_manager",
		Priority: priority,
		Data: map[string]interface{}{
			"endpoint":          endpoint.Config.Name,
			"healthy":           status.Healthy,
			"response_time":     utils.FormatResponseTime(status.ResponseTime),
			"last_check":        status.LastCheck.Format("2006-01-02 15:04:05"),
			"consecutive_fails": status.ConsecutiveFails,
			"change_type":       changeType,
		},
	})
}

// ManualActivateGroup manually activates a specific group via web interface
func (m *Manager) ManualActivateGroup(groupName string) error {
	err := m.groupManager.ManualActivateGroup(groupName)
	if err != nil {
		return err
	}

	// 冷却机制已移除

	// Notify web interface about group change
	go m.notifyWebGroupChange("group_manually_activated", groupName)

	return nil
}

// ManualActivateGroupWithForce manually activates a specific group via web interface with force option
func (m *Manager) ManualActivateGroupWithForce(groupName string, force bool) error {
	err := m.groupManager.ManualActivateGroupWithForce(groupName, force)
	if err != nil {
		return err
	}

	// 冷却机制已移除

	// Notify web interface about group change
	if force {
		go m.notifyWebGroupChange("group_force_activated", groupName)
	} else {
		go m.notifyWebGroupChange("group_manually_activated", groupName)
	}

	return nil
}

// ManualPauseGroup manually pauses a group via web interface
func (m *Manager) ManualPauseGroup(groupName string, duration time.Duration) error {
	err := m.groupManager.ManualPauseGroup(groupName, duration)
	if err != nil {
		return err
	}

	// Notify web interface about group change
	go m.notifyWebGroupChange("group_manually_paused", groupName)

	return nil
}

// ManualResumeGroup manually resumes a paused group via web interface
func (m *Manager) ManualResumeGroup(groupName string) error {
	err := m.groupManager.ManualResumeGroup(groupName)
	if err != nil {
		return err
	}

	// Notify web interface about group change
	go m.notifyWebGroupChange("group_manually_resumed", groupName)

	return nil
}

// GetGroupDetails returns detailed information about all groups for web interface
func (m *Manager) GetGroupDetails() map[string]interface{} {
	return m.groupManager.GetGroupDetails()
}

// notifyWebGroupChange notifies the web interface about group management changes
func (m *Manager) notifyWebGroupChange(eventType, groupName string) {
	// 检查EventBus是否可用
	if m.eventBus == nil {
		slog.Debug("[组管理] EventBus未设置，跳过组状态变化通知")
		return
	}

	// 获取组详细信息
	groupDetails := m.GetGroupDetails()

	// 构建事件数据
	data := map[string]interface{}{
		"event":     eventType,
		"group":     groupName,
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"details":   groupDetails,
	}

	// 使用EventBus发布组状态变化事件
	m.eventBus.Publish(events.Event{
		Type:      events.EventGroupStatusChanged,
		Source:    "endpoint_manager",
		Timestamp: time.Now(),
		Priority:  events.PriorityHigh,
		Data:      data,
	})

	slog.Debug(fmt.Sprintf("📢 [组管理] 发布组状态变化事件: %s (组: %s)", eventType, groupName))
}
