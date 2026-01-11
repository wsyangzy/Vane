// Package service 提供业务逻辑层实现
// 渠道服务 - v6.1.0 新增，v6.2.1 优先级自动递增与冲突交换
package service

import (
	"context"
	"fmt"
	"log/slog"

	"claude-request-forwarder/internal/store"
)

type ChannelService struct {
	store store.ChannelStore
}

func NewChannelService(store store.ChannelStore) *ChannelService {
	return &ChannelService{store: store}
}

func (s *ChannelService) CreateChannel(ctx context.Context, record *store.ChannelRecord) (*store.ChannelRecord, error) {
	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("渠道名称不能为空")
	}

	// v6.2.1: 优先级自动递增（仅当 Priority <= 0 时触发）
	if record.Priority <= 0 {
		maxPriority, err := s.store.GetMaxPriority(ctx)
		if err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [ChannelService] 获取最大优先级失败: %v，使用默认值1", err))
			record.Priority = 1
		} else {
			record.Priority = maxPriority + 1
		}
		slog.Info(fmt.Sprintf("🔢 [ChannelService] 自动分配优先级: %s -> %d", record.Name, record.Priority))
	}

	// 默认参与渠道间故障转移
	record.FailoverEnabled = true
	existing, err := s.store.Get(ctx, record.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("渠道 '%s' 已存在", record.Name)
	}
	return s.store.Create(ctx, record)
}

func (s *ChannelService) EnsureChannel(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}
	existing, err := s.store.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	_, err = s.store.Create(ctx, &store.ChannelRecord{Name: name, FailoverEnabled: true})
	return err
}

func (s *ChannelService) ListChannels(ctx context.Context) ([]*store.ChannelRecord, error) {
	return s.store.List(ctx)
}

func (s *ChannelService) UpdateChannel(ctx context.Context, record *store.ChannelRecord) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	if record.Name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}
	if record.Priority <= 0 {
		record.Priority = 1
	}
	existing, err := s.store.Get(ctx, record.Name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("渠道不存在: %s", record.Name)
	}

	// v6.2.1: 优先级冲突交换
	if record.Priority != existing.Priority {
		conflicting, err := s.store.GetByPriority(ctx, record.Priority)
		if err != nil {
			slog.Warn(fmt.Sprintf("⚠️ [ChannelService] 查询优先级冲突失败: %v", err))
		} else if conflicting != nil && conflicting.Name != record.Name {
			// 将冲突渠道的优先级设为当前渠道的旧优先级（交换）
			conflicting.Priority = existing.Priority
			if err := s.store.Update(ctx, conflicting); err != nil {
				slog.Warn(fmt.Sprintf("⚠️ [ChannelService] 交换优先级失败: %v", err))
			} else {
				slog.Info(fmt.Sprintf("🔄 [ChannelService] 优先级交换: %s(%d) ↔ %s(%d)",
					record.Name, record.Priority, conflicting.Name, conflicting.Priority))
			}
		}
	}

	// 保留 failover_enabled（该字段由暂停/恢复单独控制，不应被普通更新覆盖）
	if !record.FailoverEnabled {
		record.FailoverEnabled = existing.FailoverEnabled
	}
	return s.store.Update(ctx, record)
}

// SetFailoverEnabled 设置渠道是否参与“渠道间故障转移”（用于暂停/恢复）。
// 说明：
// - 若 channels 表无该渠道记录，会自动创建（默认 priority=1）。
func (s *ChannelService) SetFailoverEnabled(ctx context.Context, name string, enabled bool) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("渠道存储服务未启用")
	}
	if name == "" {
		return fmt.Errorf("渠道名称不能为空")
	}

	existing, err := s.store.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		_, err := s.store.Create(ctx, &store.ChannelRecord{
			Name:            name,
			FailoverEnabled: enabled,
		})
		return err
	}

	existing.FailoverEnabled = enabled
	return s.store.Update(ctx, existing)
}

// BackfillChannelsFromEndpoints 将历史 endpoints.channel 反写到 channels 表，确保“端点删光后渠道仍存在”。
// 说明：仅 best-effort，不因单个渠道写入失败而中断。
func (s *ChannelService) BackfillChannelsFromEndpoints(ctx context.Context, endpointStore store.EndpointStore) (int, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}
	if endpointStore == nil {
		return 0, nil
	}

	endpoints, err := endpointStore.List(ctx)
	if err != nil {
		return 0, err
	}

	seen := make(map[string]struct{}, 16)
	added := 0
	for _, ep := range endpoints {
		if ep == nil || ep.Channel == "" {
			continue
		}
		if _, ok := seen[ep.Channel]; ok {
			continue
		}
		seen[ep.Channel] = struct{}{}
		if err := s.EnsureChannel(ctx, ep.Channel); err == nil {
			added++
		}
	}
	return added, nil
}
