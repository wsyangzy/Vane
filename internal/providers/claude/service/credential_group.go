package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/wsyangzy/Vane/internal/providers/claude/store"
)

const DefaultClaudeCredentialGroupName = "默认分组"

// ClaudeCredentialGroupService 管理 Claude 渠道下的 Token/API Key 分组。
type ClaudeCredentialGroupService struct {
	validateAccount func(context.Context, string) error
	mu              sync.Mutex
	groupStore      store.ClaudeCredentialGroupStore
	channelStore    store.ChannelStore
	endpointStore   store.EndpointStore
}

func NewClaudeCredentialGroupService(groupStore store.ClaudeCredentialGroupStore, channelStore store.ChannelStore, endpointStore store.EndpointStore) *ClaudeCredentialGroupService {
	return &ClaudeCredentialGroupService{
		groupStore:    groupStore,
		channelStore:  channelStore,
		endpointStore: endpointStore,
	}
}

func (s *ClaudeCredentialGroupService) ListByChannel(ctx context.Context, channel string) ([]*store.ClaudeCredentialGroupRecord, error) {
	return s.groupStore.ListByChannel(ctx, strings.TrimSpace(channel))
}

func (s *ClaudeCredentialGroupService) GetByID(ctx context.Context, id int64) (*store.ClaudeCredentialGroupRecord, error) {
	return s.groupStore.GetByID(ctx, id)
}

func (s *ClaudeCredentialGroupService) GetDefaultByChannel(ctx context.Context, channel string) (*store.ClaudeCredentialGroupRecord, error) {
	return s.groupStore.GetDefaultByChannel(ctx, strings.TrimSpace(channel))
}

func (s *ClaudeCredentialGroupService) EnsureDefaultGroup(ctx context.Context, channel string) (*store.ClaudeCredentialGroupRecord, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return nil, fmt.Errorf("渠道不能为空")
	}
	if existing, err := s.groupStore.GetDefaultByChannel(ctx, channel); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	record := &store.ClaudeCredentialGroupRecord{
		Channel:                                channel,
		Name:                                   DefaultClaudeCredentialGroupName,
		IsDefault:                              true,
		CostMultiplier:                         1.0,
		InputCostMultiplier:                    1.0,
		OutputCostMultiplier:                   1.0,
		CacheCreationCostMultiplier:            1.0,
		CacheCreationCostMultiplier1h:          1.0,
		CacheReadCostMultiplier:                1.0,
		LongContextCostMultiplier:              1.0,
		LongContextInputCostMultiplier:         1.0,
		LongContextOutputCostMultiplier:        1.0,
		LongContextCacheCreationCostMultiplier: 1.0,
		LongContextCacheReadCostMultiplier:     1.0,
	}

	if s.endpointStore != nil {
		endpoints, err := s.endpointStore.ListByChannel(ctx, channel)
		if err != nil {
			return nil, err
		}
		for _, endpoint := range endpoints {
			if endpoint == nil {
				continue
			}
			record.Token = endpoint.Token
			record.ApiKey = endpoint.ApiKey
			record.CostMultiplier = normalizePositiveMultiplier(endpoint.CostMultiplier)
			record.InputCostMultiplier = normalizePositiveMultiplier(endpoint.InputCostMultiplier)
			record.OutputCostMultiplier = normalizePositiveMultiplier(endpoint.OutputCostMultiplier)
			record.CacheCreationCostMultiplier = normalizePositiveMultiplier(endpoint.CacheCreationCostMultiplier)
			record.CacheCreationCostMultiplier1h = normalizePositiveMultiplier(endpoint.CacheCreationCostMultiplier1h)
			record.CacheReadCostMultiplier = normalizePositiveMultiplier(endpoint.CacheReadCostMultiplier)
			record.ModelMultipliers = endpoint.ModelMultipliers
			break
		}
	}

	return s.groupStore.Create(ctx, record)
}

// EnsureDefaultGroupsFromEndpoints ensures every channel has a default group and
// backfills legacy endpoints without credential_group_id to that group.
func (s *ClaudeCredentialGroupService) EnsureDefaultGroupsFromEndpoints(ctx context.Context) (int, error) {
	if s == nil || s.groupStore == nil || s.endpointStore == nil || s.channelStore == nil {
		return 0, nil
	}

	channels, err := s.channelStore.List(ctx)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, channel := range channels {
		if channel == nil || strings.TrimSpace(channel.Name) == "" {
			continue
		}
		group, err := s.EnsureDefaultGroup(ctx, channel.Name)
		if err != nil {
			return updated, err
		}
		endpoints, err := s.endpointStore.ListByChannel(ctx, channel.Name)
		if err != nil {
			return updated, err
		}
		for _, endpoint := range endpoints {
			if endpoint == nil || endpoint.CredentialGroupID > 0 {
				continue
			}
			endpoint.CredentialGroupID = group.ID
			if err := s.endpointStore.UpdateByID(ctx, endpoint); err != nil {
				return updated, err
			}
			updated++
		}
	}
	return updated, nil
}

func (s *ClaudeCredentialGroupService) CreateGroup(ctx context.Context, record *store.ClaudeCredentialGroupRecord) (*store.ClaudeCredentialGroupRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record == nil {
		return nil, fmt.Errorf("record 不能为空")
	}
	if err := s.normalizeSubscription(ctx, record, nil); err != nil {
		return nil, err
	}
	if err := s.normalizeAndValidate(ctx, record, 0); err != nil {
		return nil, err
	}
	return s.groupStore.Create(ctx, record)
}

func (s *ClaudeCredentialGroupService) UpdateGroup(ctx context.Context, id int64, record *store.ClaudeCredentialGroupRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	existing, err := s.groupStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Claude 凭证分组不存在: %d", id)
	}
	record.ID = id
	record.Channel = existing.Channel
	if err := s.normalizeSubscription(ctx, record, existing); err != nil {
		return err
	}
	if err := s.normalizeAndValidate(ctx, record, id); err != nil {
		return err
	}
	if existing.IsDefault && !record.IsDefault {
		return fmt.Errorf("默认凭证分组不能取消默认状态，请将其他分组设为默认")
	}
	return s.groupStore.Update(ctx, record)
}

func (s *ClaudeCredentialGroupService) DeleteGroup(ctx context.Context, id int64) error {
	existing, err := s.groupStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Claude 凭证分组不存在: %d", id)
	}
	if existing.IsDefault {
		return fmt.Errorf("默认凭证分组不能删除")
	}
	return s.groupStore.DeleteByID(ctx, id)
}

func (s *ClaudeCredentialGroupService) SetDefault(ctx context.Context, id int64) error {
	existing, err := s.groupStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Claude 凭证分组不存在: %d", id)
	}
	return s.groupStore.SetDefault(ctx, id)
}

func (s *ClaudeCredentialGroupService) normalizeAndValidate(ctx context.Context, record *store.ClaudeCredentialGroupRecord, currentID int64) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	record.Channel = strings.TrimSpace(record.Channel)
	record.Name = strings.TrimSpace(record.Name)
	record.Token = strings.TrimSpace(record.Token)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	record.ModelPolicy = strings.ToLower(strings.TrimSpace(record.ModelPolicy))
	switch record.ModelPolicy {
	case "":
		record.ModelPolicy = store.ClaudeModelPolicyInherit
	case store.ClaudeModelPolicyInherit, store.ClaudeModelPolicyOverride, store.ClaudeModelPolicyPassthrough:
	default:
		return fmt.Errorf("未知的 Claude 模型策略: %q", record.ModelPolicy)
	}
	record.ModelMapping = normalizeClaudeModelMapping(record.ModelMapping)
	record.SupportedModels = normalizeClaudeSupportedModels(record.SupportedModels)
	if record.ModelPolicy == store.ClaudeModelPolicyPassthrough {
		record.ModelMapping = nil
	}
	if record.DailyCostLimitUSD < 0 {
		return fmt.Errorf("每日成本限额不能小于 0")
	}
	record.CostMultiplier = normalizePositiveMultiplier(record.CostMultiplier)
	record.InputCostMultiplier = normalizePositiveMultiplier(record.InputCostMultiplier)
	record.OutputCostMultiplier = normalizePositiveMultiplier(record.OutputCostMultiplier)
	record.CacheCreationCostMultiplier = normalizePositiveMultiplier(record.CacheCreationCostMultiplier)
	record.CacheCreationCostMultiplier1h = normalizePositiveMultiplier(record.CacheCreationCostMultiplier1h)
	record.CacheReadCostMultiplier = normalizePositiveMultiplier(record.CacheReadCostMultiplier)
	record.LongContextCostMultiplier = normalizePositiveMultiplier(record.LongContextCostMultiplier)
	record.LongContextInputCostMultiplier = normalizePositiveMultiplier(record.LongContextInputCostMultiplier)
	record.LongContextOutputCostMultiplier = normalizePositiveMultiplier(record.LongContextOutputCostMultiplier)
	record.LongContextCacheCreationCostMultiplier = normalizePositiveMultiplier(record.LongContextCacheCreationCostMultiplier)
	record.LongContextCacheReadCostMultiplier = normalizePositiveMultiplier(record.LongContextCacheReadCostMultiplier)
	modelMultipliers, err := normalizeEndpointModelMultipliers(record.ModelMultipliers)
	if err != nil {
		return err
	}
	record.ModelMultipliers = modelMultipliers

	if record.Channel == "" {
		return fmt.Errorf("渠道不能为空")
	}
	if record.Name == "" {
		return fmt.Errorf("凭证分组名不能为空")
	}
	if s.channelStore != nil {
		channel, err := s.channelStore.Get(ctx, record.Channel)
		if err != nil {
			return err
		}
		if channel == nil {
			return fmt.Errorf("渠道不存在: %s", record.Channel)
		}
	}
	conflict, err := s.groupStore.GetByChannelAndName(ctx, record.Channel, record.Name)
	if err != nil {
		return err
	}
	if conflict != nil && conflict.ID != currentID {
		return fmt.Errorf("该渠道下已存在相同的凭证分组名 '%s'", record.Name)
	}
	return nil
}

func normalizeClaudeModelMapping(mapping map[string]string) map[string]string {
	if len(mapping) == 0 {
		return nil
	}
	result := make(map[string]string, len(mapping))
	for source, target := range mapping {
		source, target = strings.TrimSpace(source), strings.TrimSpace(target)
		if source != "" && target != "" {
			result[source] = target
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeClaudeSupportedModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
