package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

// ActivateChannel 激活指定渠道（SQLite 模式）
// 语义：同一时间仅允许一个渠道处于激活状态；激活渠道会启用该渠道下所有端点。
func (s *EndpointService) ActivateChannel(ctx context.Context, channel string) error {
	s.activationMu.Lock()
	defer s.activationMu.Unlock()

	previousActive, err := s.store.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("获取当前活跃渠道失败: %w", err)
	}
	if _, err := activeChannelFromRecords(previousActive); err != nil {
		return err
	}
	if err := s.syncActiveChannelLocked(ctx, channel); err != nil {
		return err
	}

	// 手动激活必须记录手动操作时间，供挂起请求只响应真实用户操作。
	if err := s.manager.ManualActivateGroup(channel); err != nil {
		cause := fmt.Errorf("激活渠道失败: %w", err)
		if restoreErr := s.restoreActiveSelectionLocked(previousActive); restoreErr != nil {
			return fmt.Errorf("%w；恢复原活跃渠道失败: %v", cause, restoreErr)
		}
		return cause
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 已激活渠道: %s", channel))
	return nil
}

// ActivateChannelForFailover 将已完成的运行时自动切换同步到 SQLite。
// expectedActiveChannel 是自动决策开始时的活跃渠道；如果用户已手动切换，
// 则拒绝过期写入，避免旧请求覆盖较新的用户选择。
func (s *EndpointService) ActivateChannelForFailover(ctx context.Context, channel, expectedActiveChannel string, activationRevision uint64) (bool, error) {
	s.activationMu.Lock()
	defer s.activationMu.Unlock()

	if s.manager == nil || !s.manager.IsFailoverEnabled() {
		return false, fmt.Errorf("渠道间故障转移已关闭")
	}
	if s.manager.GetGroupManager() == nil ||
		!s.manager.GetGroupManager().IsActivationCurrent(channel, activationRevision) {
		return false, nil
	}

	activeChannel, err := s.getPersistedActiveChannelLocked(ctx)
	if err != nil {
		return false, err
	}
	if activeChannel != expectedActiveChannel {
		slog.Info("⏭️ [故障转移回调] 活跃渠道已变化，跳过过期持久化",
			"expected_channel", expectedActiveChannel,
			"active_channel", activeChannel,
			"candidate_channel", channel,
		)
		return false, nil
	}

	if err := s.syncActiveChannelLocked(ctx, channel); err != nil {
		return false, err
	}

	// 运行时组已由 EndpointManager 的自动路径激活。这里不能再次调用
	// ManualActivateGroup，否则自动切换会被错误记录成用户手动操作。
	slog.Info(fmt.Sprintf("✅ [EndpointService] 已同步故障转移渠道: %s", channel))
	return true, nil
}

// EnsureActiveChannelParticipates keeps the active runtime channel eligible when cross-channel failover is enabled.
func (s *EndpointService) EnsureActiveChannelParticipates(ctx context.Context, channelService *ChannelService) (bool, error) {
	if s == nil || s.manager == nil || channelService == nil {
		return false, nil
	}

	s.activationMu.Lock()
	defer s.activationMu.Unlock()

	groupManager := s.manager.GetGroupManager()
	if groupManager == nil {
		return false, nil
	}
	activeChannel, _ := groupManager.ActiveGroupSnapshot()
	if activeChannel == "" {
		var err error
		activeChannel, err = s.getPersistedActiveChannelLocked(ctx)
		if err != nil {
			return false, err
		}
	}
	if activeChannel == "" {
		return false, nil
	}

	runtimeResumed := !groupManager.IsChannelFailoverEnabled(activeChannel)
	persistedResumed, err := channelService.EnsureActiveChannelParticipates(ctx, activeChannel)
	if err != nil {
		return false, err
	}
	if runtimeResumed || persistedResumed {
		s.manager.SetSingleChannelFailoverEnabled(activeChannel, true)
	}
	return runtimeResumed || persistedResumed, nil
}

func (s *EndpointService) getPersistedActiveChannelLocked(ctx context.Context) (string, error) {
	records, err := s.store.ListEnabled(ctx)
	if err != nil {
		return "", fmt.Errorf("获取当前活跃渠道失败: %w", err)
	}
	return activeChannelFromRecords(records)
}

func activeChannelFromRecords(records []*store.EndpointRecord) (string, error) {
	activeChannel := ""
	for _, record := range records {
		if record == nil || record.Channel == "" {
			continue
		}
		if activeChannel == "" {
			activeChannel = record.Channel
			continue
		}
		if record.Channel != activeChannel {
			return "", fmt.Errorf("活跃渠道状态不一致: %s, %s", activeChannel, record.Channel)
		}
	}
	return activeChannel, nil
}

func (s *EndpointService) restoreActiveSelectionLocked(records []*store.EndpointRecord) error {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), endpointRecoveryTimeout)
	defer cancel()

	if len(records) == 0 {
		return fmt.Errorf("没有可恢复的原活跃端点")
	}
	channel, err := activeChannelFromRecords(records)
	if err != nil {
		return err
	}
	if len(records) == 1 {
		if err := s.store.SetActiveEndpoint(recoveryCtx, records[0].ID); err != nil {
			return fmt.Errorf("恢复原当前路由端点失败: %w", err)
		}
	} else {
		if err := s.store.SetActiveChannel(recoveryCtx, channel); err != nil {
			return fmt.Errorf("恢复原活跃渠道失败: %w", err)
		}
	}
	if s.manager != nil {
		if err := s.SyncFromDatabase(recoveryCtx); err != nil {
			return fmt.Errorf("恢复原活跃状态后同步运行时失败: %w", err)
		}
	}
	return nil
}

func (s *EndpointService) syncActiveChannelLocked(ctx context.Context, channel string) error {
	if channel == "" {
		return fmt.Errorf("渠道不能为空")
	}

	targetRecords, err := s.store.ListByChannel(ctx, channel)
	if err != nil {
		return fmt.Errorf("获取渠道端点列表失败: %w", err)
	}
	if len(targetRecords) == 0 {
		return fmt.Errorf("渠道 '%s' 下没有端点", channel)
	}
	records, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("获取端点列表失败: %w", err)
	}
	if err := s.store.SetActiveChannel(ctx, channel); err != nil {
		return err
	}

	for _, record := range records {
		record.Enabled = record.Channel == channel
		// 同步运行时配置
		cfg := s.recordToConfig(record)
		if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(record), cfg); err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新运行时管理器失败: %s - %v", record.Name, err))
		}
	}

	slog.Debug(fmt.Sprintf("🔄 [EndpointService] 已同步渠道持久化状态: %s (端点数: %d)", channel, len(targetRecords)))
	return nil
}

// ActivateEndpointByID 激活指定端点为当前渠道首选端点。
//
// enabled 在 SQLite 模式下仍承担“当前活跃渠道”的持久化标记；这里仅保留指定端点
// enabled=true，用于 UI 展示，并在端点 priority 相同时作为路由排序的兜底首选。
// 故障转移候选池仍由 failover_enabled 控制，其他同渠道端点不会因此退出故障转移链路。
func (s *EndpointService) ActivateEndpointByID(ctx context.Context, id int64) (*store.EndpointRecord, error) {
	s.activationMu.Lock()
	defer s.activationMu.Unlock()
	return s.activateEndpointByIDLocked(ctx, id)
}

func (s *EndpointService) activateEndpointByIDLocked(ctx context.Context, id int64) (*store.EndpointRecord, error) {
	if id <= 0 {
		return nil, fmt.Errorf("端点ID不能为空")
	}

	target, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取端点配置失败: %w", err)
	}
	if target == nil {
		return nil, fmt.Errorf("端点不存在: %d", id)
	}
	if !target.FailoverEnabled {
		return nil, fmt.Errorf("端点未参与渠道内故障转移，不能设为当前路由端点")
	}
	previousActive, err := s.store.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取当前活跃端点失败: %w", err)
	}
	if _, err := activeChannelFromRecords(previousActive); err != nil {
		return nil, err
	}

	records, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取端点列表失败: %w", err)
	}
	if err := s.store.SetActiveEndpoint(ctx, target.ID); err != nil {
		return nil, err
	}

	runtimeErrors := make([]string, 0)
	for _, record := range records {
		if record == nil {
			continue
		}
		nextEnabled := record.ID == target.ID
		record.Enabled = nextEnabled
		if s.manager != nil {
			cfg := s.recordToConfig(record)
			if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(record), cfg); err != nil {
				runtimeErrors = append(runtimeErrors, fmt.Sprintf("更新运行时管理器失败 %s: %v", record.Name, err))
			}
		}

		if record.ID == target.ID {
			target = record
		}
	}
	if len(runtimeErrors) > 0 {
		if recoveryErr := s.recoverRuntimeFromDatabase(fmt.Errorf("%s", strings.Join(runtimeErrors, "; "))); recoveryErr != nil {
			if restoreErr := s.restoreActiveSelectionLocked(previousActive); restoreErr != nil {
				return nil, fmt.Errorf("%w；恢复原活跃状态失败: %v", recoveryErr, restoreErr)
			}
			return nil, recoveryErr
		}
	}

	if s.manager != nil {
		if err := s.manager.ManualActivateGroup(target.Channel); err != nil {
			cause := fmt.Errorf("激活渠道失败: %w", err)
			if restoreErr := s.restoreActiveSelectionLocked(previousActive); restoreErr != nil {
				return nil, fmt.Errorf("%w；恢复原活跃状态失败: %v", cause, restoreErr)
			}
			return nil, cause
		}
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 已激活端点: %s/%s", target.Channel, target.Name))
	return target, nil
}

// DeactivateChannel 停用指定渠道（SQLite 模式）
// 注意：这会禁用该渠道下所有端点。
func (s *EndpointService) DeactivateChannel(ctx context.Context, channel string) error {
	s.activationMu.Lock()
	defer s.activationMu.Unlock()

	if channel == "" {
		return fmt.Errorf("渠道不能为空")
	}

	// 若停用的是当前活跃渠道，且启用“渠道间故障转移”，则应自动切换到下一个可用渠道（按路由策略选择）。
	wasActive := false
	var autoSwitchEnabled bool
	if s.manager != nil {
		autoSwitchEnabled = s.manager.IsFailoverEnabled()
		if gm := s.manager.GetGroupManager(); gm != nil {
			for _, g := range gm.GetActiveGroups() {
				if g != nil && g.Name == channel {
					wasActive = true
					break
				}
			}
		}
	}

	records, err := s.store.ListByChannel(ctx, channel)
	if err != nil {
		return fmt.Errorf("获取渠道端点列表失败: %w", err)
	}
	if len(records) == 0 {
		return nil
	}
	if err := s.store.SetChannelEnabled(ctx, channel, false); err != nil {
		return err
	}

	for _, record := range records {
		record.Enabled = false
		// 同步运行时配置
		cfg := s.recordToConfig(record)
		if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(record), cfg); err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新运行时管理器失败: %s - %v", record.Name, err))
		}
	}

	if gm := s.manager.GetGroupManager(); gm != nil {
		_ = gm.DeactivateGroup(channel)
	}

	if wasActive && autoSwitchEnabled && s.manager != nil {
		if next, err := s.manager.SelectNextAvailableChannel(channel); err == nil && next != "" {
			if err := s.syncActiveChannelLocked(ctx, next); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 停用活跃渠道后自动切换失败: %s -> %s: %v", channel, next, err))
			} else if err := s.manager.ManualActivateGroup(next); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 停用活跃渠道后运行时切换失败: %s -> %s: %v", channel, next, err))
			} else {
				slog.Info(fmt.Sprintf("✅ [EndpointService] 停用活跃渠道后已自动切换: %s -> %s", channel, next))
			}
		} else {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 停用活跃渠道后无可切换渠道: %s", channel))
		}
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 已停用渠道: %s", channel))
	return nil
}

// DisableAllEndpoints 禁用所有端点
// 用于实现互斥激活（激活一个端点前先禁用所有）
func (s *EndpointService) DisableAllEndpoints(ctx context.Context) error {
	records, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("获取端点列表失败: %w", err)
	}

	// 批量更新所有端点为 enabled=false
	for _, record := range records {
		if record.Enabled {
			record.Enabled = false
			if err := s.store.Update(ctx, record); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 禁用端点失败: %s - %v", record.Name, err))
			}
		}
	}

	slog.Info("🔄 [EndpointService] 已禁用所有端点（准备激活新端点）")
	return nil
}
