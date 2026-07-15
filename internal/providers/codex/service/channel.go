package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"ai-relay-forwarder/internal/providers/codex/store"
)

var codexChannelNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _-]{0,63}$`)

// CodexChannelService 管理 Codex 渠道（共用配置层）。
type CodexChannelService struct {
	channelStore  store.CodexChannelStore
	endpointStore store.CodexEndpointStore
}

func NewCodexChannelService(channelStore store.CodexChannelStore, endpointStore store.CodexEndpointStore) *CodexChannelService {
	return &CodexChannelService{channelStore: channelStore, endpointStore: endpointStore}
}

func (s *CodexChannelService) ListChannels(ctx context.Context) ([]*store.CodexChannelRecord, error) {
	return s.channelStore.List(ctx)
}

func (s *CodexChannelService) GetChannelByID(ctx context.Context, id int64) (*store.CodexChannelRecord, error) {
	return s.channelStore.GetByID(ctx, id)
}

func (s *CodexChannelService) GetChannelByName(ctx context.Context, name string) (*store.CodexChannelRecord, error) {
	return s.channelStore.GetByName(ctx, name)
}

func (s *CodexChannelService) GetActiveChannel(ctx context.Context) (*store.CodexChannelRecord, error) {
	return s.channelStore.GetActive(ctx)
}

// CreateChannel 写入新渠道。若当前没有任何激活渠道，新建后自动激活。
func (s *CodexChannelService) CreateChannel(ctx context.Context, record *store.CodexChannelRecord) (*store.CodexChannelRecord, error) {
	if err := normalizeAndValidateCodexChannel(record); err != nil {
		return nil, err
	}

	existing, err := s.channelStore.GetByName(ctx, record.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("Codex 渠道 '%s' 已存在", record.Name)
	}

	record.IsActive = false
	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return nil, err
	}
	maxPriority := -1
	var conflicting *store.CodexChannelRecord
	for _, channel := range channels {
		if channel.Priority > maxPriority {
			maxPriority = channel.Priority
		}
		if channel.Priority == record.Priority {
			conflicting = channel
		}
	}
	if record.Priority < 0 {
		record.Priority = maxPriority + 1
	} else if conflicting != nil {
		moved := *conflicting
		moved.Priority = maxPriority + 1
		if err := s.channelStore.UpdateByID(ctx, moved.ID, &moved); err != nil {
			return nil, fmt.Errorf("Codex 渠道优先级顺延失败: %w", err)
		}
	}
	created, err := s.channelStore.Create(ctx, record)
	if err != nil {
		return nil, err
	}

	active, err := s.channelStore.GetActive(ctx)
	if err != nil {
		return nil, err
	}
	if active == nil {
		if err := s.channelStore.SetActive(ctx, created.ID); err != nil {
			return nil, err
		}
		return s.channelStore.GetByID(ctx, created.ID)
	}
	return created, nil
}

func (s *CodexChannelService) UpdateChannelByID(ctx context.Context, id int64, record *store.CodexChannelRecord) error {
	if err := normalizeAndValidateCodexChannel(record); err != nil {
		return err
	}

	existing, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}

	nameConflict, err := s.channelStore.GetByName(ctx, record.Name)
	if err != nil {
		return err
	}
	if nameConflict != nil && nameConflict.ID != id {
		return fmt.Errorf("Codex 渠道 '%s' 已存在", record.Name)
	}
	if existing.IsActive && !record.FailoverEnabled {
		return fmt.Errorf("当前接管渠道不能暂停，请先切换到其他渠道")
	}
	if existing.Priority != record.Priority {
		channels, err := s.channelStore.List(ctx)
		if err != nil {
			return err
		}
		for _, channel := range channels {
			if channel.ID != id && channel.Priority == record.Priority {
				swapped := *channel
				swapped.Priority = existing.Priority
				if err := s.channelStore.UpdateByID(ctx, swapped.ID, &swapped); err != nil {
					return fmt.Errorf("Codex 渠道优先级对调失败: %w", err)
				}
				break
			}
		}
	} else if record.FailoverEnabled && !existing.FailoverEnabled {
		if err := s.validateUniquePriority(ctx, id, record.Priority); err != nil {
			return err
		}
	}

	return s.channelStore.UpdateByID(ctx, id, record)
}

func (s *CodexChannelService) validateUniquePriority(ctx context.Context, excludeID int64, priority int) error {
	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if channel.ID != excludeID && channel.Priority == priority {
			return fmt.Errorf("Codex 渠道优先级 P%d 已被渠道 '%s' 使用", priority, channel.Name)
		}
	}
	return nil
}

// DeleteChannel 删除渠道。若被删除的是当前激活渠道：尝试激活下一个渠道。
// 端点表通过 ON DELETE CASCADE 由 SQLite 自动级联删除。
func (s *CodexChannelService) DeleteChannelByID(ctx context.Context, id int64) error {
	existing, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}

	if err := s.channelStore.DeleteByID(ctx, id); err != nil {
		return err
	}
	if !existing.IsActive {
		return nil
	}

	// 当前激活渠道被删，尝试自动激活下一个
	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return nil
	}
	return s.channelStore.SetActive(ctx, channels[0].ID)
}

func (s *CodexChannelService) ActivateChannel(ctx context.Context, id int64, requireFailoverEnabled bool) error {
	existing, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	if requireFailoverEnabled && !existing.FailoverEnabled {
		return fmt.Errorf("Codex 渠道 '%s' 已暂停，请先恢复渠道间故障转移", existing.Name)
	}
	return s.channelStore.SetActive(ctx, id)
}

// GetNextFailoverChannel 返回最高优先级且至少包含一个参与转移端点的候选渠道。
func (s *CodexChannelService) GetNextFailoverChannel(ctx context.Context, excludeID int64) (*store.CodexChannelRecord, error) {
	if s.endpointStore == nil {
		return nil, fmt.Errorf("Codex 端点存储未就绪")
	}
	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(channels, func(i, j int) bool {
		if channels[i].Priority != channels[j].Priority {
			return channels[i].Priority < channels[j].Priority
		}
		return channels[i].ID < channels[j].ID
	})
	for _, channel := range channels {
		if channel == nil || channel.ID == excludeID || !channel.FailoverEnabled {
			continue
		}
		endpoints, err := s.endpointStore.ListByChannelID(ctx, channel.ID)
		if err != nil {
			return nil, err
		}
		for _, endpoint := range endpoints {
			if endpoint != nil && endpoint.FailoverEnabled {
				return channel, nil
			}
		}
	}
	return nil, fmt.Errorf("没有可用于接管的 Codex 渠道")
}

func (s *CodexChannelService) SetFailoverEnabled(ctx context.Context, id int64, enabled bool) error {
	channel, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if channel == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	if !enabled && channel.IsActive {
		return fmt.Errorf("当前接管渠道不能暂停，请先切换到其他渠道")
	}
	if enabled {
		if err := s.validateUniquePriority(ctx, id, channel.Priority); err != nil {
			return err
		}
	}
	return s.channelStore.SetFailoverEnabled(ctx, id, enabled)
}

// EnsureActiveEndpoint 确保激活渠道下至少有一个激活端点。
// 若当前激活渠道下没有 is_active=1 的端点：
//   - 有端点：自动激活创建时间最早的那个
//   - 无端点：返回 nil endpoint（调用方需要决定如何处理 config.toml）
func (s *CodexChannelService) EnsureActiveEndpoint(ctx context.Context, channelID int64, requireFailoverEnabled bool) (*store.CodexEndpointRecord, error) {
	active, err := s.endpointStore.GetActiveByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if active != nil && (!requireFailoverEnabled || active.FailoverEnabled) {
		return active, nil
	}

	endpoints, err := s.endpointStore.ListByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		if endpoint == nil || (requireFailoverEnabled && !endpoint.FailoverEnabled) {
			continue
		}
		if err := s.endpointStore.SetActive(ctx, endpoint.ID); err != nil {
			return nil, err
		}
		return s.endpointStore.GetByID(ctx, endpoint.ID)
	}
	return nil, nil
}

func normalizeAndValidateCodexChannel(record *store.CodexChannelRecord) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}

	record.Name = strings.TrimSpace(record.Name)
	record.ProviderType = strings.TrimSpace(record.ProviderType)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	record.Model = strings.TrimSpace(record.Model)
	record.ReasoningEffort = strings.TrimSpace(record.ReasoningEffort)
	record.Website = strings.TrimSpace(record.Website)
	record.ModelMapping = normalizeCodexModelMapping(record.ModelMapping)

	if record.Name == "" {
		return fmt.Errorf("渠道名不能为空")
	}
	if !codexChannelNamePattern.MatchString(record.Name) {
		return fmt.Errorf("渠道名必须以字母或数字开头，仅允许字母、数字、空格、连字符或下划线，长度 1-64")
	}
	if record.ProviderType == "" {
		record.ProviderType = "openai-compatible"
	}
	// ApiKey is now a compatibility fallback. New Codex credentials live in
	// codex_credential_groups, so channels can be saved without a standalone key.
	// 注：model / reasoning_effort / enable_1m_context 三个字段已迁移到 Codex 全局配置
	// （~/.codex/config.toml），由「Codex 配置」弹窗独立管理；渠道层不再校验也不强制填充
	// 默认值。数据库列允许为空，Codex 代理启用时同步逻辑会跳过 config.toml 写入。

	return nil
}

func normalizeCodexModelMapping(modelMapping map[string]string) map[string]string {
	if len(modelMapping) == 0 {
		return map[string]string{}
	}

	normalized := make(map[string]string, len(modelMapping))
	for source, target := range modelMapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			continue
		}
		normalized[source] = target
	}
	return normalized
}
