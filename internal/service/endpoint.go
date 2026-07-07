// Package service 提供业务逻辑层实现
// 端点服务 - 新增 (2025-12-05)
package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/store"
)

// EndpointService 端点管理业务服务
// 连接 EndpointStore（数据持久化）和 EndpointManager（运行时管理）
type EndpointService struct {
	store                store.EndpointStore
	manager              *endpoint.Manager
	config               *config.Config
	credentialGroupStore store.ClaudeCredentialGroupStore
}

// NewEndpointService 创建端点服务实例
func NewEndpointService(
	endpointStore store.EndpointStore,
	manager *endpoint.Manager,
	cfg *config.Config,
	credentialGroupStore ...store.ClaudeCredentialGroupStore,
) *EndpointService {
	var groupStore store.ClaudeCredentialGroupStore
	if len(credentialGroupStore) > 0 {
		groupStore = credentialGroupStore[0]
	}
	return &EndpointService{
		store:                endpointStore,
		manager:              manager,
		config:               cfg,
		credentialGroupStore: groupStore,
	}
}

// CreateEndpoint 创建新端点
// 1. 保存到数据库
// 2. 添加到运行时管理器
// 优先级自动递增（负数表示自动分配；0 是最高优先级）
func (s *EndpointService) CreateEndpoint(ctx context.Context, record *store.EndpointRecord) (*store.EndpointRecord, error) {
	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return nil, err
	}
	if err := s.ensureEndpointCredentialGroup(ctx, record); err != nil {
		return nil, err
	}

	// 允许不同凭证分组同名端点，但同一凭证分组内必须唯一
	if existing, err := s.findEndpointNameConflict(ctx, record.Channel, record.CredentialGroupID, record.Name); err != nil {
		return nil, fmt.Errorf("校验端点名称失败: %w", err)
	} else if existing != nil {
		return nil, fmt.Errorf(
			"同一凭证分组内端点名称必须唯一：渠道 '%s' 已存在同名端点 '%s'，请修改名称或选择其他凭证分组",
			record.Channel,
			record.Name,
		)
	}
	if existing, err := s.findEndpointURLConflict(ctx, record.Channel, record.CredentialGroupID, record.URL); err != nil {
		return nil, fmt.Errorf("校验端点 URL 失败: %w", err)
	} else if existing != nil {
		return nil, fmt.Errorf("同一凭证分组内已存在相同的端点 URL，请修改 URL 或选择其他凭证分组")
	}

	explicitPriority := record.Priority >= 0
	// 优先级自动递增
	if record.Priority < 0 {
		maxPriority, err := s.store.GetMaxPriorityByChannelCredentialGroup(ctx, record.Channel, record.CredentialGroupID)
		if err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 获取凭证分组最大优先级失败: %v，使用默认值0", err))
			record.Priority = 0
		} else {
			record.Priority = maxPriority + 1
		}
		slog.Info(fmt.Sprintf("🔢 [EndpointService] 自动分配优先级: %s -> %d", record.Name, record.Priority))
	}
	if explicitPriority {
		conflicting, err := s.store.GetByChannelCredentialGroupAndPriority(ctx, record.Channel, record.CredentialGroupID, record.Priority)
		if err != nil {
			return nil, fmt.Errorf("检查端点优先级冲突失败: %w", err)
		}
		if conflicting != nil {
			maxPriority, err := s.store.GetMaxPriorityByChannelCredentialGroup(ctx, record.Channel, record.CredentialGroupID)
			if err != nil {
				return nil, fmt.Errorf("获取凭证分组最大优先级失败: %w", err)
			}
			conflicting.Priority = maxPriority + 1
			if err := s.store.UpdateByID(ctx, conflicting); err != nil {
				return nil, fmt.Errorf("端点优先级替换失败: %w", err)
			}
			conflictingCfg := s.recordToConfig(conflicting)
			conflictingKey := EndpointRuntimeKey(conflicting)
			if err := s.manager.UpdateEndpointConfig(conflictingKey, conflictingCfg); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新冲突端点运行时配置失败: %v", err))
			}
		}
	}

	// 保存到数据库
	created, err := s.store.Create(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("保存端点到数据库失败: %w", err)
	}

	// 转换为配置并添加到管理器
	cfg := s.recordToConfig(created)
	if err := s.manager.AddEndpoint(cfg); err != nil {
		// 回滚数据库操作
		_ = s.store.DeleteByID(ctx, created.ID)
		return nil, fmt.Errorf("添加端点到管理器失败: %w", err)
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 创建端点成功: %s (优先级: %d)", record.Name, record.Priority))
	return created, nil
}

// GetEndpoint 获取端点详情
func (s *EndpointService) GetEndpoint(ctx context.Context, name string) (*store.EndpointRecord, error) {
	record, err := s.store.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("获取端点失败: %w", err)
	}
	return record, nil
}

// GetEndpointByID 获取端点详情（按ID）
func (s *EndpointService) GetEndpointByID(ctx context.Context, id int64) (*store.EndpointRecord, error) {
	record, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取端点失败: %w", err)
	}
	return record, nil
}

// ListEndpoints 列出所有端点
func (s *EndpointService) ListEndpoints(ctx context.Context) ([]*store.EndpointRecord, error) {
	records, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出端点失败: %w", err)
	}
	return records, nil
}

// ListEndpointsByChannel 按渠道列出端点
func (s *EndpointService) ListEndpointsByChannel(ctx context.Context, channel string) ([]*store.EndpointRecord, error) {
	records, err := s.store.ListByChannel(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("按渠道列出端点失败: %w", err)
	}
	return records, nil
}

// UpdateEndpoint 更新端点配置
// 1. 更新数据库
// 2. 更新运行时管理器
// 优先级冲突时自动对调
func (s *EndpointService) UpdateEndpoint(ctx context.Context, record *store.EndpointRecord) error {
	if record == nil || record.ID <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}

	// 验证端点存在
	existing, err := s.store.GetByID(ctx, record.ID)
	if err != nil {
		return fmt.Errorf("查询端点失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("端点不存在: %d", record.ID)
	}

	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return err
	}
	if record.CredentialGroupID <= 0 {
		record.CredentialGroupID = existing.CredentialGroupID
	}
	if record.Channel != existing.Channel && record.CredentialGroupID == existing.CredentialGroupID {
		record.CredentialGroupID = 0
		record.CredentialGroupName = ""
	}
	if err := s.ensureEndpointCredentialGroup(ctx, record); err != nil {
		return err
	}

	// 允许不同凭证分组同名，需确保"同分组内唯一"约束
	if other, err := s.findEndpointNameConflict(ctx, record.Channel, record.CredentialGroupID, record.Name); err != nil {
		return fmt.Errorf("校验端点名称失败: %w", err)
	} else if other != nil && other.ID != record.ID {
		return fmt.Errorf(
			"同一凭证分组内端点名称必须唯一：渠道 '%s' 已存在同名端点 '%s'，请修改名称或选择其他凭证分组",
			record.Channel,
			record.Name,
		)
	}
	if other, err := s.findEndpointURLConflict(ctx, record.Channel, record.CredentialGroupID, record.URL); err != nil {
		return fmt.Errorf("校验端点 URL 失败: %w", err)
	} else if other != nil && other.ID != record.ID {
		return fmt.Errorf("同一凭证分组内已存在相同的端点 URL，请修改 URL 或选择其他凭证分组")
	}

	scopeChanged := record.Channel != existing.Channel || record.CredentialGroupID != existing.CredentialGroupID
	// 渠道或凭证分组变更时，自动调整优先级为目标分组的下一个可用值。
	if scopeChanged {
		maxPriority, err := s.store.GetMaxPriorityByChannelCredentialGroup(ctx, record.Channel, record.CredentialGroupID)
		if err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 获取目标凭证分组最大优先级失败: %v", err))
			maxPriority = 0
		}
		newPriority := maxPriority + 1
		slog.Info(fmt.Sprintf("🔄 [EndpointService] 端点归属变更: %s/%d -> %s/%d，优先级自动调整: %d -> %d",
			existing.Channel, existing.CredentialGroupID, record.Channel, record.CredentialGroupID, record.Priority, newPriority))
		record.Priority = newPriority
	}

	// 优先级冲突检测与对调
	// 仅当优先级发生变化且仍在同一凭证分组内时触发
	if record.Priority != existing.Priority && !scopeChanged {
		conflicting, err := s.store.GetByChannelCredentialGroupAndPriority(ctx, record.Channel, record.CredentialGroupID, record.Priority)
		if err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 检查优先级冲突失败: %v", err))
		} else if conflicting != nil && conflicting.ID != record.ID {
			// 存在冲突，执行优先级对调
			oldPriority := existing.Priority
			slog.Info(fmt.Sprintf("🔄 [EndpointService] 优先级冲突检测: %s(ID:%d, 新优先级:%d) 与 %s(ID:%d, 当前优先级:%d)，执行对调",
				record.Name, record.ID, record.Priority,
				conflicting.Name, conflicting.ID, conflicting.Priority))

			// 先更新冲突端点的优先级为原端点的旧优先级
			conflicting.Priority = oldPriority
			if err := s.store.UpdateByID(ctx, conflicting); err != nil {
				return fmt.Errorf("优先级对调失败（更新冲突端点）: %w", err)
			}

			// 同步冲突端点到运行时管理器
			conflictingCfg := s.recordToConfig(conflicting)
			conflictingKey := EndpointRuntimeKey(conflicting)
			if err := s.manager.UpdateEndpointConfig(conflictingKey, conflictingCfg); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新冲突端点运行时配置失败: %v", err))
			}

			slog.Info(fmt.Sprintf("✅ [EndpointService] 优先级对调完成: %s -> %d, %s -> %d",
				conflicting.Name, conflicting.Priority, record.Name, record.Priority))
		}
	}

	if existing.Enabled && !record.FailoverEnabled {
		nextEndpoint, err := s.findNextFailoverEndpoint(ctx, existing.Channel, existing.ID)
		if err != nil {
			return err
		}
		if nextEndpoint == nil {
			return fmt.Errorf("当前路由端点关闭参与转移失败：没有其他参与渠道内故障转移的端点")
		}
		if _, err := s.ActivateEndpointByID(ctx, nextEndpoint.ID); err != nil {
			return fmt.Errorf("切换当前路由端点失败: %w", err)
		}
		record.Enabled = false
	}

	// 更新数据库（按 ID）
	if err := s.store.UpdateByID(ctx, record); err != nil {
		return fmt.Errorf("更新数据库失败: %w", err)
	}

	// 更新运行时管理器
	cfg := s.recordToConfig(record)
	oldKey := EndpointRuntimeKey(existing)
	if err := s.manager.UpdateEndpointConfig(oldKey, cfg); err != nil {
		slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新运行时管理器失败: %v", err))
		// 不回滚数据库，下次重启会同步
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 更新端点成功: %s (优先级: %d)", record.Name, record.Priority))
	return nil
}

func (s *EndpointService) findNextFailoverEndpoint(ctx context.Context, channel string, currentID int64) (*store.EndpointRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("端点存储服务未启用")
	}
	candidates, err := s.store.ListByChannel(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("获取渠道端点列表失败: %w", err)
	}
	filtered := make([]*store.EndpointRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == currentID || !candidate.FailoverEnabled {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Priority != filtered[j].Priority {
			return filtered[i].Priority < filtered[j].Priority
		}
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].ID < filtered[j].ID
	})
	return filtered[0], nil
}

// DeleteEndpoint 删除端点
// 1. 从运行时管理器移除
// 2. 从数据库删除
func (s *EndpointService) DeleteEndpoint(ctx context.Context, name string) error {
	// 验证端点存在
	existing, err := s.store.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("查询端点失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("端点 '%s' 不存在", name)
	}

	// 先从运行时管理器移除
	if err := s.manager.RemoveEndpoint(EndpointRuntimeKey(existing)); err != nil {
		slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 从管理器移除端点失败: %v", err))
		// 继续删除数据库记录
	}

	// 从数据库删除
	if err := s.store.DeleteByID(ctx, existing.ID); err != nil {
		return fmt.Errorf("从数据库删除失败: %w", err)
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 删除端点成功: %s", name))
	return nil
}

// DeleteEndpointByID 删除端点（按ID，避免同名歧义）
func (s *EndpointService) DeleteEndpointByID(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("端点ID不能为空")
	}
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("查询端点失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("端点不存在: %d", id)
	}

	// 先从运行时管理器移除
	if err := s.manager.RemoveEndpoint(EndpointRuntimeKey(existing)); err != nil {
		slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 从管理器移除端点失败: %v", err))
	}

	if err := s.store.DeleteByID(ctx, id); err != nil {
		return fmt.Errorf("从数据库删除失败: %w", err)
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 删除端点成功: %s", existing.Name))
	return nil
}

// ToggleEndpoint 切换端点启用状态
func (s *EndpointService) ToggleEndpoint(ctx context.Context, name string, enabled bool) error {
	record, err := s.store.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return fmt.Errorf("端点 '%s' 不存在", name)
	}

	record.Enabled = enabled
	return s.UpdateEndpoint(ctx, record)
}

// ActivateChannel 激活指定渠道（SQLite 模式）
// 语义：同一时间仅允许一个渠道处于激活状态；激活渠道会启用该渠道下所有端点。
func (s *EndpointService) ActivateChannel(ctx context.Context, channel string) error {
	if channel == "" {
		return fmt.Errorf("渠道不能为空")
	}

	// 互斥：先停用所有端点
	if err := s.DisableAllEndpoints(ctx); err != nil {
		return err
	}

	records, err := s.store.ListByChannel(ctx, channel)
	if err != nil {
		return fmt.Errorf("获取渠道端点列表失败: %w", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("渠道 '%s' 下没有端点", channel)
	}

	for _, record := range records {
		record.Enabled = true
		if err := s.store.Update(ctx, record); err != nil {
			return fmt.Errorf("启用端点失败: %s - %w", record.Name, err)
		}

		// 同步运行时配置
		cfg := s.recordToConfig(record)
		if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(record), cfg); err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新运行时管理器失败: %s - %v", record.Name, err))
		}
	}

	// 激活运行时渠道（组）
	if err := s.manager.ManualActivateGroup(channel); err != nil {
		return fmt.Errorf("激活渠道失败: %w", err)
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 已激活渠道: %s (端点数: %d)", channel, len(records)))
	return nil
}

// ActivateEndpointByID 激活指定端点为当前渠道首选端点。
//
// enabled 在 SQLite 模式下仍承担“当前活跃渠道”的持久化标记；这里仅保留指定端点
// enabled=true，用于 UI 和路由排序识别当前首选端点。故障转移候选池仍由
// failover_enabled 控制，其他同渠道端点不会因此退出故障转移链路。
func (s *EndpointService) ActivateEndpointByID(ctx context.Context, id int64) (*store.EndpointRecord, error) {
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

	records, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取端点列表失败: %w", err)
	}

	for _, record := range records {
		if record == nil {
			continue
		}
		nextEnabled := record.ID == target.ID
		if record.Enabled == nextEnabled {
			if record.ID == target.ID {
				target = record
			}
			continue
		}

		record.Enabled = nextEnabled
		if err := s.store.Update(ctx, record); err != nil {
			return nil, fmt.Errorf("更新端点激活状态失败: %s - %w", record.Name, err)
		}

		if s.manager != nil {
			cfg := s.recordToConfig(record)
			if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(record), cfg); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新运行时管理器失败: %s - %v", record.Name, err))
			}
		}

		if record.ID == target.ID {
			target = record
		}
	}

	if s.manager != nil {
		if err := s.manager.ManualActivateGroup(target.Channel); err != nil {
			return nil, fmt.Errorf("激活渠道失败: %w", err)
		}
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 已激活端点: %s/%s", target.Channel, target.Name))
	return target, nil
}

// DeactivateChannel 停用指定渠道（SQLite 模式）
// 注意：这会禁用该渠道下所有端点。
func (s *EndpointService) DeactivateChannel(ctx context.Context, channel string) error {
	if channel == "" {
		return fmt.Errorf("渠道不能为空")
	}

	// 若停用的是当前活跃渠道，且启用“渠道间故障转移”，则应自动切换到下一个可用渠道（按路由策略选择）。
	wasActive := false
	var autoSwitchEnabled bool
	if s.manager != nil {
		if cfg := s.manager.GetConfig(); cfg != nil {
			autoSwitchEnabled = cfg.Failover.Enabled
		}
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

	for _, record := range records {
		if !record.Enabled {
			continue
		}
		record.Enabled = false
		if err := s.store.Update(ctx, record); err != nil {
			return fmt.Errorf("禁用端点失败: %s - %w", record.Name, err)
		}

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
			if err := s.ActivateChannel(ctx, next); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 停用活跃渠道后自动切换失败: %s -> %s: %v", channel, next, err))
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

// ImportFromYAML 从 YAML 配置导入端点
// clearExisting: 是否清除现有端点
func (s *EndpointService) ImportFromYAML(ctx context.Context, endpoints []config.EndpointConfig, clearExisting bool) (int, error) {
	if clearExisting {
		// 清除现有端点
		existing, err := s.store.List(ctx)
		if err != nil {
			return 0, fmt.Errorf("获取现有端点失败: %w", err)
		}

		names := make([]string, len(existing))
		for i, ep := range existing {
			names[i] = ep.Name
		}

		if len(names) > 0 {
			if err := s.store.BatchDelete(ctx, names); err != nil {
				return 0, fmt.Errorf("清除现有端点失败: %w", err)
			}
		}
	}

	// 转换并导入
	records := make([]*store.EndpointRecord, 0, len(endpoints))
	for _, ep := range endpoints {
		record := s.configToRecord(ep)
		records = append(records, record)
	}

	if err := s.store.BatchCreate(ctx, records); err != nil {
		return 0, fmt.Errorf("批量导入失败: %w", err)
	}

	// 重新加载到管理器
	// 注意：这里简化处理，实际可能需要更精细的同步
	for _, ep := range endpoints {
		_ = s.manager.AddEndpoint(ep)
	}

	slog.Info(fmt.Sprintf("✅ [EndpointService] 从 YAML 导入 %d 个端点", len(records)))
	return len(records), nil
}

// SyncFromDatabase 从数据库同步端点到管理器
// Desktop: 加载所有端点参与健康检查，并同步 enabled=true 的端点到组激活状态
func (s *EndpointService) SyncFromDatabase(ctx context.Context) error {
	// 获取所有端点（包括 enabled=false 的）
	records, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("获取端点列表失败: %w", err)
	}

	slog.Info(fmt.Sprintf("🔄 [EndpointService] 从数据库同步 %d 个端点", len(records)))

	// 转换为配置数组
	endpoints := make([]config.EndpointConfig, len(records))
	enabledChannelCount := make(map[string]int)
	for i, record := range records {
		endpoints[i] = s.recordToConfig(record)
		// 统计 enabled=true 的渠道分布
		if record.Enabled && record.Channel != "" {
			enabledChannelCount[record.Channel]++
		}
	}

	// 使用专门的同步方法（不走 UpdateConfig）
	s.manager.SyncEndpoints(endpoints)

	// 同步 enabled=true 的渠道到组激活状态（以渠道为路由单位）
	if len(enabledChannelCount) > 0 {
		// 选择 enabled 端点数最多的渠道；若并列则按字典序稳定选择
		var selectedChannel string
		var maxCount int
		for ch, cnt := range enabledChannelCount {
			if cnt > maxCount || (cnt == maxCount && (selectedChannel == "" || ch < selectedChannel)) {
				selectedChannel = ch
				maxCount = cnt
			}
		}

		// 若存在多个 enabled 渠道，修正为互斥激活（避免 UI/路由语义混乱）
		if len(enabledChannelCount) > 1 {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 检测到多个 enabled 渠道，自动修正为只保留: %s", selectedChannel))
			for _, record := range records {
				if record.Enabled && record.Channel != selectedChannel {
					record.Enabled = false
					if err := s.store.Update(ctx, record); err != nil {
						slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 修正禁用端点失败: %s - %v", record.Name, err))
						continue
					}
					cfg := s.recordToConfig(record)
					if err := s.manager.UpdateEndpointConfig(EndpointRuntimeKey(record), cfg); err != nil {
						slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 更新运行时管理器失败: %s - %v", record.Name, err))
					}
				}
			}
		}

		if err := s.manager.ManualActivateGroup(selectedChannel); err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [EndpointService] 激活渠道失败: %s - %v", selectedChannel, err))
		} else {
			slog.Info(fmt.Sprintf("✅ [EndpointService] 已激活渠道: %s", selectedChannel))
		}
	}

	return nil
}

// GetEndpointWithHealth 获取端点详情（包含健康状态）
func (s *EndpointService) GetEndpointWithHealth(ctx context.Context, name string) (map[string]interface{}, error) {
	record, err := s.store.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("获取端点失败: %w", err)
	}
	if record == nil {
		return nil, fmt.Errorf("端点 '%s' 不存在", name)
	}

	// 获取健康状态
	status := s.manager.GetEndpointStatus(EndpointRuntimeKey(record))

	return map[string]interface{}{
		"id":                                record.ID,
		"channel":                           record.Channel,
		"credential_group_id":               record.CredentialGroupID,
		"credential_group_name":             record.CredentialGroupName,
		"name":                              record.Name,
		"url":                               record.URL,
		"token_masked":                      maskToken(record.Token),
		"priority":                          record.Priority,
		"failover_enabled":                  record.FailoverEnabled,
		"timeout_seconds":                   record.TimeoutSeconds,
		"cost_multiplier":                   record.CostMultiplier,
		"input_cost_multiplier":             record.InputCostMultiplier,
		"output_cost_multiplier":            record.OutputCostMultiplier,
		"cache_creation_cost_multiplier":    record.CacheCreationCostMultiplier,
		"cache_creation_cost_multiplier_1h": record.CacheCreationCostMultiplier1h,
		"cache_read_cost_multiplier":        record.CacheReadCostMultiplier,
		"model_multipliers":                 record.ModelMultipliers,
		"enabled":                           record.Enabled,
		"created_at":                        record.CreatedAt,
		"updated_at":                        record.UpdatedAt,
		"health": map[string]interface{}{
			"healthy":          status.Healthy,
			"never_checked":    status.NeverChecked,
			"last_check":       status.LastCheck.Format("2006-01-02 15:04:05"),
			"response_time_ms": status.ResponseTime.Milliseconds(),
		},
	}, nil
}

// validateRecord 验证端点记录
func (s *EndpointService) validateRecord(record *store.EndpointRecord) error {
	if record.Name == "" {
		return fmt.Errorf("端点名称不能为空")
	}
	if record.URL == "" {
		return fmt.Errorf("端点 URL 不能为空")
	}
	if record.Channel == "" {
		return fmt.Errorf("端点渠道不能为空")
	}
	modelMultipliers, err := normalizeEndpointModelMultipliers(record.ModelMultipliers)
	if err != nil {
		return err
	}
	record.ModelMultipliers = modelMultipliers
	return nil
}

func normalizeEndpointModelMultipliers(rules []store.EndpointModelMultiplierRecord) ([]store.EndpointModelMultiplierRecord, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	normalized := make([]store.EndpointModelMultiplierRecord, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			return nil, fmt.Errorf("模型附加倍率重复：%s", model)
		}
		seen[model] = struct{}{}

		normalized = append(normalized, store.EndpointModelMultiplierRecord{
			Model:                   model,
			CostMultiplier:          normalizePositiveMultiplier(rule.CostMultiplier),
			InputCostMultiplier:     normalizePositiveMultiplier(rule.InputCostMultiplier),
			OutputCostMultiplier:    normalizePositiveMultiplier(rule.OutputCostMultiplier),
			CacheReadCostMultiplier: normalizePositiveMultiplier(rule.CacheReadCostMultiplier),
		})
	}
	return normalized, nil
}

func normalizePositiveMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

// recordToConfig 将数据库记录转换为配置对象
func (s *EndpointService) recordToConfig(record *store.EndpointRecord) config.EndpointConfig {
	token := record.Token
	apiKey := record.ApiKey
	credentialGroupName := record.CredentialGroupName
	credentialGroupID := record.CredentialGroupID
	credentialGroupOrder := 0
	if s.credentialGroupStore != nil {
		var group *store.ClaudeCredentialGroupRecord
		if record.CredentialGroupID > 0 {
			group, _ = s.credentialGroupStore.GetByID(context.Background(), record.CredentialGroupID)
		}
		if group == nil {
			group, _ = s.credentialGroupStore.GetDefaultByChannel(context.Background(), record.Channel)
		}
		if group != nil {
			credentialGroupID = group.ID
			credentialGroupName = group.Name
			credentialGroupOrder = s.credentialGroupOrder(context.Background(), record.Channel, group.ID)
			if token == "" {
				token = group.Token
			}
			if apiKey == "" {
				apiKey = group.ApiKey
			}
		}
	}

	cfg := config.EndpointConfig{
		Name:                 record.Name,
		URL:                  record.URL,
		Channel:              record.Channel,
		RuntimeKey:           EndpointRuntimeKey(record),
		CredentialGroupID:    credentialGroupID,
		CredentialGroupName:  credentialGroupName,
		CredentialGroupOrder: credentialGroupOrder,
		Priority:             record.Priority,
		Token:                token,
		ApiKey:               apiKey,
		Headers:              record.Headers,
		Timeout:              time.Duration(record.TimeoutSeconds) * time.Second,
		SupportsCountTokens:  record.SupportsCountTokens,
		SkipHealthCheck:      record.SkipHealthCheck,
	}

	// 设置 Enabled（是否作为代理端点）
	enabled := record.Enabled
	cfg.Enabled = &enabled

	// 设置 FailoverEnabled
	fe := record.FailoverEnabled
	cfg.FailoverEnabled = &fe

	// 设置 Cooldown
	if record.CooldownSeconds != nil {
		cd := time.Duration(*record.CooldownSeconds) * time.Second
		cfg.Cooldown = &cd
	}

	return cfg
}

func (s *EndpointService) credentialGroupOrder(ctx context.Context, channel string, credentialGroupID int64) int {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return 0
	}
	groups, err := s.credentialGroupStore.ListByChannel(ctx, channel)
	if err != nil {
		return 0
	}
	for idx, group := range groups {
		if group != nil && group.ID == credentialGroupID {
			return idx
		}
	}
	return len(groups)
}

// EndpointRuntimeKey returns a collision-free runtime key for SQLite endpoints.
func EndpointRuntimeKey(record *store.EndpointRecord) string {
	if record == nil {
		return ""
	}
	if record.CredentialGroupID > 0 {
		return fmt.Sprintf("%s::g%d::%s", record.Channel, record.CredentialGroupID, record.Name)
	}
	return endpoint.EndpointKey(record.Channel, record.Name)
}

func (s *EndpointService) ensureEndpointCredentialGroup(ctx context.Context, record *store.EndpointRecord) error {
	if s.credentialGroupStore == nil || record == nil {
		return nil
	}
	if record.CredentialGroupID > 0 {
		return s.validateCredentialGroup(ctx, record.Channel, record.CredentialGroupID)
	}
	group, err := s.credentialGroupStore.GetDefaultByChannel(ctx, record.Channel)
	if err != nil {
		return err
	}
	if group == nil {
		group, err = s.credentialGroupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
			Channel:                       record.Channel,
			Name:                          DefaultClaudeCredentialGroupName,
			Token:                         record.Token,
			ApiKey:                        record.ApiKey,
			IsDefault:                     true,
			CostMultiplier:                normalizePositiveMultiplier(record.CostMultiplier),
			InputCostMultiplier:           normalizePositiveMultiplier(record.InputCostMultiplier),
			OutputCostMultiplier:          normalizePositiveMultiplier(record.OutputCostMultiplier),
			CacheCreationCostMultiplier:   normalizePositiveMultiplier(record.CacheCreationCostMultiplier),
			CacheCreationCostMultiplier1h: normalizePositiveMultiplier(record.CacheCreationCostMultiplier1h),
			CacheReadCostMultiplier:       normalizePositiveMultiplier(record.CacheReadCostMultiplier),
		})
		if err != nil {
			return err
		}
	}
	record.CredentialGroupID = group.ID
	record.CredentialGroupName = group.Name
	return nil
}

func (s *EndpointService) validateCredentialGroup(ctx context.Context, channel string, credentialGroupID int64) error {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return nil
	}
	group, err := s.credentialGroupStore.GetByID(ctx, credentialGroupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("Claude 凭证分组不存在: %d", credentialGroupID)
	}
	if group.Channel != channel {
		return fmt.Errorf("凭证分组不属于当前渠道")
	}
	return nil
}

func (s *EndpointService) findEndpointNameConflict(ctx context.Context, channel string, credentialGroupID int64, name string) (*store.EndpointRecord, error) {
	conflict, err := s.store.GetByChannelCredentialGroupAndName(ctx, channel, credentialGroupID, name)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channel, credentialGroupID) {
		return nil, nil
	}
	return s.store.GetByChannelCredentialGroupAndName(ctx, channel, 0, name)
}

func (s *EndpointService) findEndpointURLConflict(ctx context.Context, channel string, credentialGroupID int64, url string) (*store.EndpointRecord, error) {
	conflict, err := s.store.GetByChannelCredentialGroupAndURL(ctx, channel, credentialGroupID, url)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channel, credentialGroupID) {
		return nil, nil
	}
	return s.store.GetByChannelCredentialGroupAndURL(ctx, channel, 0, url)
}

func (s *EndpointService) isDefaultCredentialGroup(ctx context.Context, channel string, credentialGroupID int64) bool {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return false
	}
	group, err := s.credentialGroupStore.GetDefaultByChannel(ctx, channel)
	return err == nil && group != nil && group.ID == credentialGroupID
}

// configToRecord 将配置对象转换为数据库记录
func (s *EndpointService) configToRecord(cfg config.EndpointConfig) *store.EndpointRecord {
	record := &store.EndpointRecord{
		Channel:             cfg.Channel,
		Name:                cfg.Name,
		URL:                 cfg.URL,
		Token:               cfg.Token,
		ApiKey:              cfg.ApiKey,
		Headers:             cfg.Headers,
		Priority:            cfg.Priority,
		FailoverEnabled:     true, // 默认参与故障转移
		SkipHealthCheck:     cfg.SkipHealthCheck,
		TimeoutSeconds:      int(cfg.Timeout.Seconds()),
		SupportsCountTokens: cfg.SupportsCountTokens,
		CostMultiplier:      1.0,
		Enabled:             true,
	}

	if record.Channel == "" {
		// 兼容：未配置 channel 时回退为端点名
		record.Channel = cfg.Name
	}

	if cfg.FailoverEnabled != nil {
		record.FailoverEnabled = *cfg.FailoverEnabled
	}

	if cfg.Cooldown != nil {
		cd := int(cfg.Cooldown.Seconds())
		record.CooldownSeconds = &cd
	}

	if record.TimeoutSeconds == 0 {
		record.TimeoutSeconds = 300 // 默认 5 分钟
	}

	return record
}

// maskToken 脱敏 Token
func maskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}
