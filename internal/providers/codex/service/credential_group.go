package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

const DefaultCodexCredentialGroupName = "默认分组"

// CodexCredentialGroupService 管理 Codex 渠道下的 API Key 分组。
type CodexCredentialGroupService struct {
	mu              sync.Mutex
	validateAccount func(context.Context, string) error
	groupStore      store.CodexCredentialGroupStore
	channelStore    store.CodexChannelStore
	endpointStore   store.CodexEndpointStore
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
		ModelPolicy:                            store.CodexModelPolicyInherit,
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.normalizeSubscription(ctx, record, nil); err != nil {
		return nil, err
	}
	if err := s.normalizeAndValidate(ctx, record, 0); err != nil {
		return nil, err
	}
	return s.groupStore.Create(ctx, record)
}

func (s *CodexCredentialGroupService) UpdateGroup(ctx context.Context, id int64, record *store.CodexCredentialGroupRecord) error {
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
		return fmt.Errorf("Codex 凭证分组不存在: %d", id)
	}
	record.ID = id
	record.ChannelID = existing.ChannelID
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
	// The store rechecks activation/participation and deletes the group's
	// inactive endpoints atomically with the group record.
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
	record.ModelPolicy = strings.ToLower(strings.TrimSpace(record.ModelPolicy))
	switch record.ModelPolicy {
	case "":
		record.ModelPolicy = store.CodexModelPolicyInherit
	case store.CodexModelPolicyInherit, store.CodexModelPolicyOverride, store.CodexModelPolicyPassthrough:
	default:
		return fmt.Errorf("未知的 Codex 模型策略: %q", record.ModelPolicy)
	}
	record.ModelMapping = normalizeCodexModelMapping(record.ModelMapping)
	record.SupportedModels = normalizeCodexSupportedModels(record.SupportedModels)
	if record.ModelPolicy == store.CodexModelPolicyPassthrough {
		record.ModelMapping = nil
	}
	if record.DailyCostLimitUSD < 0 {
		return fmt.Errorf("每日成本限额不能小于 0")
	}
	if record.ServiceTierSource != "" && record.ServiceTierSource != "request" && record.ServiceTierSource != "response" {
		return fmt.Errorf("未知的服务档位计费依据: %q", record.ServiceTierSource)
	}
	record.ServiceTierSource = tracking.NormalizeServiceTierSource(record.ServiceTierSource)
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
	billingRules, err := normalizeCodexBillingRules(record.BillingRules)
	if err != nil {
		return err
	}
	record.BillingRules = billingRules

	if record.ChannelID <= 0 {
		return fmt.Errorf("channel_id 无效")
	}
	if record.Name == "" {
		return fmt.Errorf("凭证分组名不能为空")
	}
	if record.ApiKey == "" && record.AuthMode != "client_passthrough" {
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

func normalizeCodexSupportedModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	normalized := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeCodexBillingRules(rules []store.CodexBillingRuleRecord) ([]store.CodexBillingRuleRecord, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(rules))
	normalized := make([]store.CodexBillingRuleRecord, 0, len(rules))
	catchAllCount := 0
	for _, rule := range rules {
		rule.Model = strings.TrimSpace(rule.Model)
		rule.ServiceTier = strings.ToLower(strings.TrimSpace(rule.ServiceTier))
		if rule.ServiceTier != "" {
			rule.ServiceTier = tracking.NormalizeServiceTierForBilling(rule.ServiceTier)
			if rule.ServiceTier == "" {
				return nil, fmt.Errorf("未知的 Codex 计费规则服务档位")
			}
		}
		rule.Context = strings.ToLower(strings.TrimSpace(rule.Context))
		if rule.Context != "" && rule.Context != tracking.BillingContextNormal && rule.Context != tracking.BillingContextLong {
			return nil, fmt.Errorf("未知的 Codex 计费规则上下文: %q", rule.Context)
		}
		rule.Mode = strings.ToLower(strings.TrimSpace(rule.Mode))
		rule.GroupMultiplierMode = tracking.DefaultBillingRuleGroupMultiplierMode(rule.GroupMultiplierMode, rule.Model == "" && rule.ServiceTier == "" && rule.Context == "")
		if rule.GroupMultiplierMode != tracking.BillingGroupMultiplierInherit && rule.GroupMultiplierMode != tracking.BillingGroupMultiplierOverride {
			return nil, fmt.Errorf("未知的 Codex 计费规则分组倍率关系: %q", rule.GroupMultiplierMode)
		}
		if rule.Mode != tracking.BillingRuleModeMultiplier && rule.Mode != tracking.BillingRuleModeFixed {
			return nil, fmt.Errorf("未知的 Codex 计费规则模式: %q", rule.Mode)
		}

		selectorKey := rule.Model + "\x00" + rule.ServiceTier + "\x00" + rule.Context
		if _, exists := seen[selectorKey]; exists {
			return nil, fmt.Errorf("Codex 计费规则选择器重复")
		}
		seen[selectorKey] = struct{}{}
		if rule.Model == "" && rule.ServiceTier == "" && rule.Context == "" {
			catchAllCount++
		}

		if rule.Mode == tracking.BillingRuleModeMultiplier {
			values := []float64{rule.CostMultiplier, rule.InputCostMultiplier, rule.OutputCostMultiplier, rule.CacheCreationCostMultiplier, rule.CacheReadCostMultiplier}
			for _, value := range values {
				if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					return nil, fmt.Errorf("Codex 计费规则倍率必须是大于 0 的有限数值")
				}
			}
			rule.InputPrice = 0
			rule.OutputPrice = 0
			rule.CacheCreationPrice = 0
			rule.CacheReadPrice = 0
		} else {
			values := []float64{rule.InputPrice, rule.OutputPrice, rule.CacheCreationPrice, rule.CacheReadPrice}
			for _, value := range values {
				if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					return nil, fmt.Errorf("Codex 计费规则固定单价必须是大于或等于 0 的有限数值")
				}
			}
			rule.CostMultiplier = 1
			rule.InputCostMultiplier = 1
			rule.OutputCostMultiplier = 1
			rule.CacheCreationCostMultiplier = 1
			rule.CacheReadCostMultiplier = 1
		}
		normalized = append(normalized, rule)
	}
	if catchAllCount != 1 {
		return nil, fmt.Errorf("Codex 组合计费规则必须且只能包含一条默认规则")
	}
	return normalized, nil
}
