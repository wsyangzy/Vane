// app_events.go - Wails v3 事件发射
// 将 Go 后端状态变化通知到前端

package main

// 事件名称常量
const (
	EventSystemStatus        = "system:status"
	EventEndpointUpdate      = "endpoint:update"
	EventCodexProviderUpdate = "codex-provider:update"
	EventCodexSuspension     = "codex-provider:suspension"
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
	codexMgr := a.codexEndpointMgr
	healthCheck := a.codexHealthCheck
	a.mu.RUnlock()

	// 触发端点管理器重新加载（覆盖渠道/端点 CRUD + 激活切换场景）
	// 否则 EndpointManager 内部缓存的端点列表会陈旧，导致健康检查/路由失效
	if codexMgr != nil {
		if err := codexMgr.Reload(); err != nil {
			if a.logger != nil {
				a.logger.Warn("Codex 端点管理器重新加载失败", "error", err)
			}
		}
	}

	// 重新加载后立即触发一次健康探测，避免用户激活新渠道后还要等下一个 ticker 周期（30s）
	if healthCheck != nil {
		healthCheck.CheckNow()
	}

	a.emitCodexProviderStateUpdate()
}

// emitCodexProviderStateUpdate 只推送运行时状态，不触发重新加载或健康探测。
func (a *App) emitCodexProviderStateUpdate() {
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

// emitCodexSuspensionUpdate 发送 Codex 挂起请求数变化到前端
// 由 SuspensionManager.onCountChange 回调触发；count = 当前挂起请求总数
func (a *App) emitCodexSuspensionUpdate(count int) {
	a.mu.RLock()
	window := a.window
	running := a.isRunning
	a.mu.RUnlock()

	if !running || window == nil {
		return
	}

	data := map[string]interface{}{
		"suspended_count": count,
	}

	if a.logger != nil {
		a.logger.Debug("📡 [Wails Event] 推送 Codex 挂起数更新", "count", count)
	}

	a.safeEmitEvent(EventCodexSuspension, data)
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

	summary, _ := a.GetUsageSummary("", "", "")
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
