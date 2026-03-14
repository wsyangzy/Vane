// app_events.go - Wails v3 事件发射
// 将 Go 后端状态变化通知到前端

package main

// 事件名称常量
const (
	EventSystemStatus        = "system:status"
	EventEndpointUpdate      = "endpoint:update"
	EventCodexProviderUpdate = "codex-provider:update"
	EventGroupUpdate         = "group:update"
	EventUsageUpdate         = "usage:update"
	EventConfigReloaded      = "config:reloaded"
	EventError               = "error"
	EventNotification        = "notification"
)

// safeEmitEvent 安全地发射事件到前端
// 使用 recover 防止在窗口销毁过程中调用 EmitEvent 导致 panic
func (a *App) safeEmitEvent(eventName string, data interface{}) {
	defer func() {
		if r := recover(); r != nil {
			if a.logger != nil {
				a.logger.Debug("📡 事件发射被跳过（窗口可能已销毁）", "event", eventName, "recover", r)
			}
		}
	}()

	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	window.EmitEvent(eventName, data)
}

// emitSystemStatus 发送系统状态更新到前端
func (a *App) emitSystemStatus() {
	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	status := a.GetSystemStatus()
	a.safeEmitEvent(EventSystemStatus, status)
}

// emitEndpointUpdate 发送端点状态更新到前端
func (a *App) emitEndpointUpdate() {
	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	endpoints := a.GetEndpoints()
	// 包装为前端期望的格式 { endpoints: [...] }
	data := map[string]interface{}{
		"endpoints": endpoints,
	}

	if a.logger != nil {
		a.logger.Debug("📡 [Wails Event] 推送端点更新", "count", len(endpoints))
	}

	a.safeEmitEvent(EventEndpointUpdate, data)
}

// emitCodexProviderUpdate 发送 Codex Provider 更新到前端
func (a *App) emitCodexProviderUpdate() {
	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	status := a.GetCodexProviderStorageStatus()
	data := map[string]interface{}{
		"enabled":              status.Enabled,
		"total_count":          status.TotalCount,
		"has_active":           status.HasActive,
		"active_provider_id":   status.ActiveProviderID,
		"active_provider_name": status.ActiveProviderName,
	}

	if a.logger != nil {
		a.logger.Debug("📡 [Wails Event] 推送 Codex Provider 更新", "count", status.TotalCount, "has_active", status.HasActive)
	}

	a.safeEmitEvent(EventCodexProviderUpdate, data)
}

// emitGroupUpdate 发送组状态更新到前端
func (a *App) emitGroupUpdate() {
	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	groups := a.GetGroups()
	a.safeEmitEvent(EventGroupUpdate, groups)
}

// emitUsageUpdate 发送使用统计更新到前端
func (a *App) emitUsageUpdate() {
	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	summary, _ := a.GetUsageSummary("", "")
	a.safeEmitEvent(EventUsageUpdate, summary)
}

// emitNotification 发送通知到前端
func (a *App) emitNotification(level, title, message string) {
	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	a.safeEmitEvent(EventNotification, map[string]string{
		"level":   level, // "info", "warning", "error", "success"
		"title":   title,
		"message": message,
	})
}

