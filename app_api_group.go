// app_api_group.go - 组管理 API (Wails Bindings)
// 包含组状态查询、激活、暂停、恢复等功能
// 激活端点时同步到数据库

package main

import (
	"context"
	"fmt"
	"time"
)

// ============================================================
// 组管理 API
// ============================================================

// GroupInfo 组信息
type GroupInfo struct {
	Name             string `json:"name"`
	Channel          string `json:"channel"` // 渠道名称（从端点配置获取）
	Active           bool   `json:"active"`
	Paused           bool   `json:"paused"`
	Priority         int    `json:"priority"`
	EndpointCount    int    `json:"endpoint_count"`
	InCooldown       bool   `json:"in_cooldown"`
	CooldownRemainMs int64  `json:"cooldown_remain_ms"`
}

// GetGroups 获取所有组状态
func (a *App) GetGroups() []GroupInfo {
	a.mu.RLock()
	manager := a.endpointManager
	a.mu.RUnlock()

	if manager == nil {
		return []GroupInfo{}
	}

	gm := manager.GetGroupManager()
	if gm == nil {
		return []GroupInfo{}
	}

	groups := gm.GetAllGroups()
	result := make([]GroupInfo, 0, len(groups))

	for _, g := range groups {
		// 从第一个端点获取渠道名称
		channel := ""
		if len(g.Endpoints) > 0 {
			channel = g.Endpoints[0].Config.Channel
		}
		if channel == "" {
			channel = g.Name
		}

		info := GroupInfo{
			Name:          g.Name,
			Channel:       channel,
			Active:        g.IsActive,
			Paused:        g.ManuallyPaused,
			Priority:      g.Priority,
			EndpointCount: len(g.Endpoints),
			InCooldown:    gm.IsGroupInCooldown(g.Name),
		}

		// 获取冷却剩余时间
		remaining := gm.GetGroupCooldownRemaining(g.Name)
		if remaining > 0 {
			info.CooldownRemainMs = remaining.Milliseconds()
		}

		result = append(result, info)
	}

	return result
}

// GetNextAvailableChannel 获取“排除指定渠道后的下一个可用渠道”（用于前端提示/确认）。
// 注意：该方法仅用于 UI 辅助，不会改变任何状态。
func (a *App) GetNextAvailableChannel(excludeChannel string) (string, error) {
	a.mu.RLock()
	manager := a.endpointManager
	a.mu.RUnlock()

	if manager == nil {
		return "", fmt.Errorf("端点管理器未初始化")
	}
	if !manager.IsFailoverEnabled() {
		return "", fmt.Errorf("渠道间故障转移未启用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// best-effort: 确保运行时的“渠道暂停状态”是最新的（SQLite 模式下来自 channels 表）
	a.syncChannelFailoverEnabledToEndpointManager(ctx)

	return manager.SelectNextAvailableChannel(excludeChannel)
}

// ActivateGroup 激活指定组
// 组名 = 渠道(channel)（SQLite 模式）；未配置 channel 时回退为端点名（YAML 模式兼容）
func (a *App) ActivateGroup(name string) error {
	a.mu.RLock()
	manager := a.endpointManager
	endpointService := a.endpointService
	logger := a.logger
	a.mu.RUnlock()

	if manager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	// 如果有 endpointService，同步到数据库
	if endpointService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// SQLite：激活渠道（互斥）
		if err := endpointService.ActivateChannel(ctx, name); err != nil {
			if logger != nil {
				logger.Warn("激活渠道失败", "channel", name, "error", err)
			}
			return err
		} else {
			// 🛑 [手动重试] 仅在激活成功后更新故障转移排序起点。
			manager.SetFailoverStartChannel(name)
			if logger != nil {
				logger.Info("✅ 渠道已同步到数据库", "channel", name, "enabled", true)
			}
			return nil
		}
	}

	// 内存中激活组（兼容 YAML 模式）
	manager.SetFailoverStartChannel(name)
	return manager.ManualActivateGroup(name)
}

// PauseGroup 暂停指定组
func (a *App) PauseGroup(name string) error {
	a.mu.RLock()
	manager := a.endpointManager
	channelService := a.channelService
	endpointService := a.endpointService
	a.mu.RUnlock()

	if manager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	// 若暂停的是当前活跃渠道，并且启用了渠道间故障转移，则需要确保存在可切换渠道。
	wasActive := false
	if gm := manager.GetGroupManager(); gm != nil {
		for _, g := range gm.GetActiveGroups() {
			if g != nil && g.Name == name {
				wasActive = true
				break
			}
		}
	}
	autoSwitchEnabled := manager.IsFailoverEnabled()

	// 暂停/恢复为"无限期直到手动恢复"，且在 SQLite 模式下持久化到 channels.failover_enabled
	if channelService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var next string
		if wasActive && autoSwitchEnabled {
			// 移除全量同步，运行时状态应该已经是准确的
			var err error
			next, err = manager.SelectNextAvailableChannel(name)
			if err != nil || next == "" {
				return fmt.Errorf("无法暂停活跃渠道（无可切换渠道）: %w", err)
			}
		}

		if err := channelService.SetFailoverEnabled(ctx, name, false); err != nil {
			return err
		}
		// 仅更新单个渠道，避免影响其他渠道状态
		manager.SetSingleChannelFailoverEnabled(name, false)
		a.emitEndpointUpdate()

		// SQLite 模式：暂停活跃渠道后，按路由策略自动切换到下一个可用渠道。
		if wasActive && autoSwitchEnabled && next != "" {
			if endpointService == nil {
				return fmt.Errorf("无法自动切换渠道：端点服务未启用")
			}
			if err := endpointService.ActivateChannel(ctx, next); err != nil {
				return fmt.Errorf("暂停成功但自动切换失败: %w", err)
			}
			a.emitEndpointUpdate()
		}

		return nil
	}

	// YAML 模式：预检查是否存在可切换渠道，避免暂停活跃渠道后无可用渠道。
	if wasActive && autoSwitchEnabled {
		if next, err := manager.SelectNextAvailableChannel(name); err != nil || next == "" {
			return fmt.Errorf("无法暂停活跃渠道（无可切换渠道）: %w", err)
		}
	}

	return manager.ManualPauseGroup(name, 0)
}

// IsInterChannelFailoverEnabled 检查是否启用渠道间故障转移
// 前端用于判断是否显示渠道切换器
func (a *App) IsInterChannelFailoverEnabled() bool {
	a.mu.RLock()
	manager := a.endpointManager
	a.mu.RUnlock()

	if manager == nil {
		return false
	}

	return manager.IsFailoverEnabled()
}

// ResumeGroup 恢复指定组
func (a *App) ResumeGroup(name string) error {
	a.mu.RLock()
	manager := a.endpointManager
	channelService := a.channelService
	a.mu.RUnlock()

	if manager == nil {
		return fmt.Errorf("端点管理器未初始化")
	}

	if channelService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := channelService.SetFailoverEnabled(ctx, name, true); err != nil {
			return err
		}
		// 仅更新单个渠道，避免影响其他渠道状态
		manager.SetSingleChannelFailoverEnabled(name, true)
		a.emitEndpointUpdate()
		return nil
	}

	return manager.ManualResumeGroup(name)
}
