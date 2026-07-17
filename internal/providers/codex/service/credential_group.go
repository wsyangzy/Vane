package service

import (
	"context"
	"fmt"
	"strings"

	"ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/tracking"
)

const DefaultCodexCredentialGroupName = "默认分组"

// CodexCredentialGroupService 管理 Codex 渠道下的 API Key 分组。
type CodexCredentialGroupService struct {
	groupStore    store.CodexCredentialGroupStore
	channelStore  store.CodexChannelStore
	endpointStore store.CodexEndpointStore
}

func NewCodexCredentialGroupService(groupStore store.CodexCredentialGroupStore, channelStore store.CodexChannelStore, endpointStore store.CodexEndpointStore) *CodexCredentialGroupService {
	return &CodexCredentialGroupService{groupStore: groupStore, channelStore: channelStore, endpointStore: endpointStore}
}

func (s *CodexCredentialGroupService) ListByChannelID(ctx context.Context, channelID int64) ([]*store.CodexCredentialGroupRecord, error) {
	return s.groupStore.ListByChannelID(ctx, channelID)
}

func (s *CodexCredentialGroupService) GetByID(ctx context.Context, id int64) (*store.CodexCredentialGroupRecord, error) {
	return s.groupStore.GetByID(ctx, id)
}

func (s *CodexCredentialGroupService) GetDefaultByChannelID(ctx context.Context, channelID int64) (*store.CodexCredentialGroupRecord, error) {
	return s.groupStore.GetDefaultByChannelID(ctx, channelID)
}

func (s *CodexCredentialGroupService) EnsureDefaultGroup(ctx context.Context, channel *store.CodexChannelRecord) (*store.CodexCredentialGroupRecord, error) {
	if channel == nil {
		return nil, fmt.Errorf("Codex 渠道为空")
	}
	if strings.TrimSpace(channel.ApiKey) == "" {
		return nil, nil
	}
	existing, err := s.groupStore.GetDefaultByChannelID(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	record := &store.CodexCredentialGroupRecord{
		ChannelID:                              channel.ID,
		Name:                                   DefaultCodexCredentialGroupName,
		ApiKey:                                 channel.ApiKey,
		IsDefault:                              true,
		CostMultiplier:                         1.0,
		InputCostMultiplier:                    1.0,
		OutputCostMultiplier:                   1.0,
		CacheCreationCostMultiplier:            1.0,
		CacheReadCostMultiplier:                1.0,
		LongContextCostMultiplier:              1.0,
		LongContextInputCostMultiplier:         1.0,
		LongContextOutputCostMultiplier:        1.0,
		LongContextCacheCreationCostMultiplier: 1.0,
		LongContextCacheReadCostMultiplier:     1.0,
	}
	if s.endpointStore != nil {
		endpoints, err := s.endpointStore.ListByChannelID(ctx, channel.ID)
		if err != nil {
			return nil, err
		}
		for _, endpoint := range endpoints {
			if endpoint == nil {
				continue
			}
			record.ModelMultipliers = endpoint.ModelMultipliers
			break
		}
	}
	return s.CreateGroup(ctx, record)
}

func (s *CodexCredentialGroupService) CreateGroup(ctx context.Context, record *store.CodexCredentialGroupRecord) (*store.CodexCredentialGroupRecord, error) {
	if err := s.normalizeAndValidate(ctx, record, 0); err != nil {
		return nil, err
	}
	return s.groupStore.Create(ctx, record)
}

func (s *CodexCredentialGroupService) UpdateGroup(ctx context.Context, id int64, record *store.CodexCredentialGroupRecord) error {
	existing, err := s.groupStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 凭证分组不存在: %d", id)
	}
	record.ID = id
	record.ChannelID = existing.ChannelID
	if err := s.normalizeAndValidate(ctx, record, id); err != nil {
		return err
	}
	if existing.IsDefault && !record.IsDefault {
		return fmt.Errorf("默认凭证分组不能取消默认状态，请将其他分组设为默认")
	}
	return s.groupStore.Update(ctx, record)
}

func (s *CodexCredentialGroupService) DeleteGroup(ctx context.Context, id int64) error {
	existing, err := s.groupStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 凭证分组不存在: %d", id)
	}
	if existing.IsDefault {
		return fmt.Errorf("默认凭证分组不能删除")
	}
	count, err := s.groupStore.CountEndpoints(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("凭证分组仍被 %d 个端点引用，不能删除", count)
	}
	return s.groupStore.DeleteByID(ctx, id)
}

func (s *CodexCredentialGroupService) SetDefault(ctx context.Context, id int64) error {
	existing, err := s.groupStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 凭证分组不存在: %d", id)
	}
	return s.groupStore.SetDefault(ctx, id)
}

func (s *CodexCredentialGroupService) normalizeAndValidate(ctx context.Context, record *store.CodexCredentialGroupRecord, currentID int64) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	record.Name = strings.TrimSpace(record.Name)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	if record.DailyCostLimitUSD < 0 {
		return fmt.Errorf("每日成本限额不能小于 0")
	}
	record.BillingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(record.BillingOutputTokenSource)
	record.CostMultiplier = defaultCodexMultiplier(record.CostMultiplier)
	record.InputCostMultiplier = defaultCodexMultiplier(record.InputCostMultiplier)
	record.OutputCostMultiplier = defaultCodexMultiplier(record.OutputCostMultiplier)
	record.CacheCreationCostMultiplier = defaultCodexMultiplier(record.CacheCreationCostMultiplier)
	record.CacheReadCostMultiplier = defaultCodexMultiplier(record.CacheReadCostMultiplier)
	record.LongContextCostMultiplier = defaultCodexMultiplier(record.LongContextCostMultiplier)
	record.LongContextInputCostMultiplier = defaultCodexMultiplier(record.LongContextInputCostMultiplier)
	record.LongContextOutputCostMultiplier = defaultCodexMultiplier(record.LongContextOutputCostMultiplier)
	record.LongContextCacheCreationCostMultiplier = defaultCodexMultiplier(record.LongContextCacheCreationCostMultiplier)
	record.LongContextCacheReadCostMultiplier = defaultCodexMultiplier(record.LongContextCacheReadCostMultiplier)
	modelMultipliers, err := normalizeCodexModelMultipliers(record.ModelMultipliers)
	if err != nil {
		return err
	}
	record.ModelMultipliers = modelMultipliers

	if record.ChannelID <= 0 {
		return fmt.Errorf("channel_id 无效")
	}
	if record.Name == "" {
		return fmt.Errorf("凭证分组名不能为空")
	}
	if record.ApiKey == "" {
		return fmt.Errorf("API Key 不能为空")
	}
	channel, err := s.channelStore.GetByID(ctx, record.ChannelID)
	if err != nil {
		return err
	}
	if channel == nil {
		return fmt.Errorf("Codex 渠道不存在: %d", record.ChannelID)
	}
	conflict, err := s.groupStore.GetByChannelAndName(ctx, record.ChannelID, record.Name)
	if err != nil {
		return err
	}
	if conflict != nil && conflict.ID != currentID {
		return fmt.Errorf("该渠道下已存在相同的凭证分组名 '%s'", record.Name)
	}
	return nil
}
