package proxy

import (
	"github.com/wsyangzy/Vane/internal/platform/events"
)

// getCommonEventData 获取事件的通用数据字段
// 返回包含 request_id, endpoint_name, channel, group_name, model_name 的 map
func (rlm *RequestLifecycleManager) getCommonEventData() map[string]interface{} {
	return map[string]interface{}{
		"request_id":    rlm.stateManager.GetRequestID(),
		"endpoint_name": rlm.stateManager.GetEndpointName(),
		"channel":       rlm.stateManager.GetChannel(),
		"group_name":    rlm.stateManager.GetGroupName(),
		"model_name":    rlm.GetModelName(),
	}
}

// publishEvent 发布事件的统一入口
// 自动填充通用字段，合并额外数据，处理 nil 检查
func (rlm *RequestLifecycleManager) publishEvent(eventType events.EventType, priority events.EventPriority, extraData map[string]interface{}) {
	if rlm.eventBus == nil {
		return
	}

	// 获取通用数据
	data := rlm.getCommonEventData()

	// 合并额外数据
	if extraData != nil {
		for k, v := range extraData {
			data[k] = v
		}
	}

	rlm.eventBus.Publish(events.Event{
		Type:     eventType,
		Source:   "lifecycle_manager",
		Priority: priority,
		Data:     data,
	})
}

// publishStatusChangeEvent 发布状态变更事件的便捷方法
// 根据状态自动确定优先级和 change_type
func (rlm *RequestLifecycleManager) publishStatusChangeEvent(status string, retryCount, httpStatus int) {
	// 根据状态确定优先级和 change_type
	priority := events.PriorityNormal
	changeType := "status_changed"

	switch status {
	case "error", "timeout":
		priority = events.PriorityHigh
		changeType = "error_response"
	case "suspended":
		changeType = "suspended_change"
	case "retry":
		changeType = "retry_attempt"
	case "completed":
		changeType = "request_completed"
	case "failed":
		priority = events.PriorityHigh
		changeType = "request_failed"
	case "cancelled":
		changeType = "request_cancelled"
	}

	rlm.publishEvent(events.EventRequestUpdated, priority, map[string]interface{}{
		"status":      status,
		"retry_count": retryCount,
		"http_status": httpStatus,
		"change_type": changeType,
	})
}
