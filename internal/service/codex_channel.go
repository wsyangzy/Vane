package service

import (
	"context"
	"fmt"
	"strings"

	"ai-relay-forwarder/internal/store"
)

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

	return s.channelStore.UpdateByID(ctx, id, record)
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

func (s *CodexChannelService) ActivateChannel(ctx context.Context, id int64) error {
	existing, err := s.channelStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", id)
	}
	return s.channelStore.SetActive(ctx, id)
}

// EnsureActiveEndpoint 确保激活渠道下至少有一个激活端点。
// 若当前激活渠道下没有 is_active=1 的端点：
//   - 有端点：自动激活创建时间最早的那个
//   - 无端点：返回 nil endpoint（调用方需要决定如何处理 config.toml）
func (s *CodexChannelService) EnsureActiveEndpoint(ctx context.Context, channelID int64) (*store.CodexEndpointRecord, error) {
	active, err := s.endpointStore.GetActiveByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return active, nil
	}

	endpoints, err := s.endpointStore.ListByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, nil
	}

	// 选择 ListByChannelID 返回的第一项（按 is_active DESC, created_at ASC 排序，没有激活时取最早）
	first := endpoints[0]
	if err := s.endpointStore.SetActive(ctx, first.ID); err != nil {
		return nil, err
	}
	return s.endpointStore.GetByID(ctx, first.ID)
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

	if record.Name == "" {
		return fmt.Errorf("渠道名不能为空")
	}
	if !codexProviderNamePattern.MatchString(record.Name) {
		return fmt.Errorf("渠道名仅允许字母、数字、连字符和下划线，且必须以字母或数字开头")
	}
	if record.ProviderType == "" {
		record.ProviderType = "openai-compatible"
	}
	if record.ApiKey == "" {
		return fmt.Errorf("API Key 不能为空")
	}
	// 注：model / reasoning_effort / enable_1m_context 三个字段已迁移到 Codex 全局配置
	// （~/.codex/config.toml），由「Codex 配置」弹窗独立管理；渠道层不再校验也不强制填充
	// 默认值。数据库列允许为空，Codex 代理启用时同步逻辑会跳过 config.toml 写入。

	return nil
}
