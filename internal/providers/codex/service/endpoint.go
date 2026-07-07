package service

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/tracking"
)

var codexEndpointNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _-]{0,63}$`)

// CodexEndpointService 管理 Codex 端点（name + base_url）。
type CodexEndpointService struct {
	endpointStore        store.CodexEndpointStore
	channelStore         store.CodexChannelStore
	credentialGroupStore store.CodexCredentialGroupStore
}

func NewCodexEndpointService(endpointStore store.CodexEndpointStore, channelStore store.CodexChannelStore, credentialGroupStore ...store.CodexCredentialGroupStore) *CodexEndpointService {
	var groupStore store.CodexCredentialGroupStore
	if len(credentialGroupStore) > 0 {
		groupStore = credentialGroupStore[0]
	}
	return &CodexEndpointService{endpointStore: endpointStore, channelStore: channelStore, credentialGroupStore: groupStore}
}

func (s *CodexEndpointService) ListByChannelID(ctx context.Context, channelID int64) ([]*store.CodexEndpointRecord, error) {
	return s.endpointStore.ListByChannelID(ctx, channelID)
}

func (s *CodexEndpointService) GetEndpointByID(ctx context.Context, id int64) (*store.CodexEndpointRecord, error) {
	return s.endpointStore.GetByID(ctx, id)
}

func (s *CodexEndpointService) GetActiveByChannelID(ctx context.Context, channelID int64) (*store.CodexEndpointRecord, error) {
	return s.endpointStore.GetActiveByChannelID(ctx, channelID)
}

// CreateEndpoint 在指定渠道下新建端点。若该渠道下没有激活端点，则自动激活新端点。
func (s *CodexEndpointService) CreateEndpoint(ctx context.Context, record *store.CodexEndpointRecord) (*store.CodexEndpointRecord, error) {
	if err := normalizeAndValidateCodexEndpoint(record); err != nil {
		return nil, err
	}

	channel, err := s.channelStore.GetByID(ctx, record.ChannelID)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, fmt.Errorf("Codex 渠道不存在: %d", record.ChannelID)
	}
	if err := s.ensureEndpointCredentialGroup(ctx, record); err != nil {
		return nil, err
	}

	urlConflict, err := s.findEndpointURLConflict(ctx, record.ChannelID, record.CredentialGroupID, record.BaseURL)
	if err != nil {
		return nil, err
	}
	if urlConflict != nil {
		return nil, fmt.Errorf("该凭证分组下已存在相同的 base_url")
	}

	nameConflict, err := s.findEndpointNameConflict(ctx, record.ChannelID, record.CredentialGroupID, record.Name)
	if err != nil {
		return nil, err
	}
	if nameConflict != nil {
		return nil, fmt.Errorf("该凭证分组下已存在相同的端点名 '%s'", record.Name)
	}

	explicitPriority := record.Priority >= 0
	if record.Priority < 0 {
		maxPriority, err := s.endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, record.ChannelID, record.CredentialGroupID)
		if err != nil {
			return nil, err
		}
		record.Priority = maxPriority + 1
	}
	if explicitPriority {
		conflicting, err := s.endpointStore.GetByChannelCredentialGroupAndPriority(ctx, record.ChannelID, record.CredentialGroupID, record.Priority)
		if err != nil {
			return nil, fmt.Errorf("检查 Codex 端点优先级冲突失败: %w", err)
		}
		if conflicting != nil {
			maxPriority, err := s.endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, record.ChannelID, record.CredentialGroupID)
			if err != nil {
				return nil, fmt.Errorf("获取 Codex 凭证分组最大优先级失败: %w", err)
			}
			if err := s.endpointStore.Update(
				ctx,
				conflicting.ID,
				conflicting.Name,
				conflicting.BaseURL,
				conflicting.CredentialGroupID,
				conflicting.ApiKey,
				conflicting.FailoverEnabled,
				conflicting.SkipHealthCheck,
				maxPriority+1,
				conflicting.TimeoutSeconds,
				conflicting.CostMultiplier,
				conflicting.InputCostMultiplier,
				conflicting.OutputCostMultiplier,
				conflicting.CacheReadCostMultiplier,
				conflicting.BillingOutputTokenSource,
				conflicting.ModelMultipliers,
			); err != nil {
				return nil, fmt.Errorf("Codex 端点优先级替换失败: %w", err)
			}
		}
	}

	record.IsActive = false
	created, err := s.endpointStore.Create(ctx, record)
	if err != nil {
		return nil, err
	}

	active, err := s.endpointStore.GetActiveByChannelID(ctx, record.ChannelID)
	if err != nil {
		return nil, err
	}
	if active == nil {
		if err := s.endpointStore.SetActive(ctx, created.ID); err != nil {
			return nil, err
		}
		return s.endpointStore.GetByID(ctx, created.ID)
	}
	return created, nil
}

func (s *CodexEndpointService) UpdateEndpointByID(ctx context.Context, id int64, name, baseURL string, credentialGroupID int64, apiKey string, failoverEnabled bool, skipHealthCheck bool, priority, timeoutSeconds int, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier float64, billingOutputTokenSource string, modelMultipliers []store.CodexModelMultiplierRecord) error {
	name = strings.TrimSpace(name)
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	billingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(billingOutputTokenSource)
	normalizedModelMultipliers, err := normalizeCodexModelMultipliers(modelMultipliers)
	if err != nil {
		return err
	}
	if err := validateCodexEndpointName(name); err != nil {
		return err
	}
	if err := validateBaseURL(baseURL); err != nil {
		return err
	}
	if err := validateCodexEndpointTimeoutSeconds(timeoutSeconds); err != nil {
		return err
	}

	existing, err := s.endpointStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}
	if credentialGroupID <= 0 {
		credentialGroupID = existing.CredentialGroupID
	}
	if err := s.validateCredentialGroup(ctx, existing.ChannelID, credentialGroupID); err != nil {
		return err
	}
	groupChanged := existing.CredentialGroupID != credentialGroupID
	if groupChanged {
		maxPriority, err := s.endpointStore.GetMaxPriorityByChannelCredentialGroup(ctx, existing.ChannelID, credentialGroupID)
		if err != nil {
			return fmt.Errorf("获取 Codex 目标凭证分组最大优先级失败: %w", err)
		}
		priority = maxPriority + 1
	}

	if existing.Name == name && existing.BaseURL == baseURL && existing.CredentialGroupID == credentialGroupID && existing.ApiKey == apiKey && existing.FailoverEnabled == failoverEnabled && existing.SkipHealthCheck == skipHealthCheck && existing.Priority == priority && existing.TimeoutSeconds == timeoutSeconds && existing.CostMultiplier == costMultiplier && existing.InputCostMultiplier == inputCostMultiplier && existing.OutputCostMultiplier == outputCostMultiplier && existing.CacheReadCostMultiplier == cacheReadCostMultiplier && existing.BillingOutputTokenSource == billingOutputTokenSource && reflect.DeepEqual(existing.ModelMultipliers, normalizedModelMultipliers) {
		return nil
	}

	if existing.BaseURL != baseURL || existing.CredentialGroupID != credentialGroupID {
		urlConflict, err := s.findEndpointURLConflict(ctx, existing.ChannelID, credentialGroupID, baseURL)
		if err != nil {
			return err
		}
		if urlConflict != nil && urlConflict.ID != id {
			return fmt.Errorf("该凭证分组下已存在相同的 base_url")
		}
	}

	if existing.Name != name || existing.CredentialGroupID != credentialGroupID {
		nameConflict, err := s.findEndpointNameConflict(ctx, existing.ChannelID, credentialGroupID, name)
		if err != nil {
			return err
		}
		if nameConflict != nil && nameConflict.ID != id {
			return fmt.Errorf("该凭证分组下已存在相同的端点名 '%s'", name)
		}
	}

	if existing.IsActive && !failoverEnabled {
		nextEndpoint, err := s.findNextFailoverEndpoint(ctx, existing.ChannelID, existing.ID)
		if err != nil {
			return err
		}
		if nextEndpoint == nil {
			return fmt.Errorf("当前激活端点关闭参与转移失败：没有其他参与故障转移的 Codex 端点")
		}
		if err := s.endpointStore.SetActive(ctx, nextEndpoint.ID); err != nil {
			return fmt.Errorf("切换 Codex 激活端点失败: %w", err)
		}
	}

	if !groupChanged && existing.Priority != priority {
		conflicting, err := s.endpointStore.GetByChannelCredentialGroupAndPriority(ctx, existing.ChannelID, credentialGroupID, priority)
		if err != nil {
			return fmt.Errorf("检查 Codex 端点优先级冲突失败: %w", err)
		}
		if conflicting != nil && conflicting.ID != id {
			if err := s.endpointStore.Update(
				ctx,
				conflicting.ID,
				conflicting.Name,
				conflicting.BaseURL,
				conflicting.CredentialGroupID,
				conflicting.ApiKey,
				conflicting.FailoverEnabled,
				conflicting.SkipHealthCheck,
				existing.Priority,
				conflicting.TimeoutSeconds,
				conflicting.CostMultiplier,
				conflicting.InputCostMultiplier,
				conflicting.OutputCostMultiplier,
				conflicting.CacheReadCostMultiplier,
				conflicting.BillingOutputTokenSource,
				conflicting.ModelMultipliers,
			); err != nil {
				return fmt.Errorf("Codex 端点优先级对调失败: %w", err)
			}
		}
	}

	return s.endpointStore.Update(ctx, id, name, baseURL, credentialGroupID, apiKey, failoverEnabled, skipHealthCheck, priority, timeoutSeconds, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier, billingOutputTokenSource, normalizedModelMultipliers)
}

func (s *CodexEndpointService) findNextFailoverEndpoint(ctx context.Context, channelID int64, currentID int64) (*store.CodexEndpointRecord, error) {
	if s == nil || s.endpointStore == nil {
		return nil, fmt.Errorf("Codex 端点存储服务未启用")
	}
	candidates, err := s.endpointStore.ListByChannelID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 端点列表失败: %w", err)
	}
	filtered := make([]*store.CodexEndpointRecord, 0, len(candidates))
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

// DeleteEndpoint 删除端点。若被删除的是当前激活端点：
//   - 该渠道下还有其他端点 → 自动激活第一个（按创建时间）
//   - 该渠道下无其他端点 → 不再有激活端点（调用方负责清理 config.toml）
func (s *CodexEndpointService) DeleteEndpointByID(ctx context.Context, id int64) error {
	existing, err := s.endpointStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	if err := s.endpointStore.DeleteByID(ctx, id); err != nil {
		return err
	}
	if !existing.IsActive {
		return nil
	}

	siblings, err := s.endpointStore.ListByChannelID(ctx, existing.ChannelID)
	if err != nil {
		return err
	}
	if len(siblings) == 0 {
		return nil
	}
	return s.endpointStore.SetActive(ctx, siblings[0].ID)
}

func (s *CodexEndpointService) ActivateEndpoint(ctx context.Context, id int64) error {
	existing, err := s.endpointStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}
	if !existing.FailoverEnabled {
		return fmt.Errorf("Codex 端点未参与故障转移，不能设为当前端点")
	}
	return s.endpointStore.SetActive(ctx, id)
}

func normalizeAndValidateCodexEndpoint(record *store.CodexEndpointRecord) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}
	if record.ChannelID <= 0 {
		return fmt.Errorf("channel_id 无效")
	}
	record.Name = strings.TrimSpace(record.Name)
	record.BaseURL = strings.TrimSpace(record.BaseURL)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	record.BillingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(record.BillingOutputTokenSource)
	modelMultipliers, err := normalizeCodexModelMultipliers(record.ModelMultipliers)
	if err != nil {
		return err
	}
	record.ModelMultipliers = modelMultipliers
	if err := validateCodexEndpointName(record.Name); err != nil {
		return err
	}
	if err := validateCodexEndpointTimeoutSeconds(record.TimeoutSeconds); err != nil {
		return err
	}
	return validateBaseURL(record.BaseURL)
}

func (s *CodexEndpointService) ensureEndpointCredentialGroup(ctx context.Context, record *store.CodexEndpointRecord) error {
	if s.credentialGroupStore == nil {
		return nil
	}
	if record.CredentialGroupID > 0 {
		return s.validateCredentialGroup(ctx, record.ChannelID, record.CredentialGroupID)
	}
	group, err := s.credentialGroupStore.GetDefaultByChannelID(ctx, record.ChannelID)
	if err != nil {
		return err
	}
	if group != nil {
		record.CredentialGroupID = group.ID
	}
	return nil
}

func (s *CodexEndpointService) validateCredentialGroup(ctx context.Context, channelID int64, credentialGroupID int64) error {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return nil
	}
	group, err := s.credentialGroupStore.GetByID(ctx, credentialGroupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("Codex 凭证分组不存在: %d", credentialGroupID)
	}
	if group.ChannelID != channelID {
		return fmt.Errorf("凭证分组不属于当前渠道")
	}
	return nil
}

func (s *CodexEndpointService) findEndpointURLConflict(ctx context.Context, channelID, credentialGroupID int64, baseURL string) (*store.CodexEndpointRecord, error) {
	conflict, err := s.endpointStore.GetByChannelCredentialGroupAndURL(ctx, channelID, credentialGroupID, baseURL)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channelID, credentialGroupID) {
		return nil, nil
	}
	return s.endpointStore.GetByChannelCredentialGroupAndURL(ctx, channelID, 0, baseURL)
}

func (s *CodexEndpointService) findEndpointNameConflict(ctx context.Context, channelID, credentialGroupID int64, name string) (*store.CodexEndpointRecord, error) {
	conflict, err := s.endpointStore.GetByChannelCredentialGroupAndName(ctx, channelID, credentialGroupID, name)
	if err != nil || conflict != nil {
		return conflict, err
	}
	if !s.isDefaultCredentialGroup(ctx, channelID, credentialGroupID) {
		return nil, nil
	}
	return s.endpointStore.GetByChannelCredentialGroupAndName(ctx, channelID, 0, name)
}

func (s *CodexEndpointService) isDefaultCredentialGroup(ctx context.Context, channelID, credentialGroupID int64) bool {
	if s.credentialGroupStore == nil || credentialGroupID <= 0 {
		return false
	}
	group, err := s.credentialGroupStore.GetDefaultByChannelID(ctx, channelID)
	return err == nil && group != nil && group.ID == credentialGroupID
}

func normalizeCodexModelMultipliers(rules []store.CodexModelMultiplierRecord) ([]store.CodexModelMultiplierRecord, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(rules))
	normalized := make([]store.CodexModelMultiplierRecord, 0, len(rules))
	for _, rule := range rules {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			return nil, fmt.Errorf("模型附加倍率重复: %s", model)
		}
		seen[model] = struct{}{}
		normalized = append(normalized, store.CodexModelMultiplierRecord{
			Model:                   model,
			CostMultiplier:          defaultCodexMultiplier(rule.CostMultiplier),
			InputCostMultiplier:     defaultCodexMultiplier(rule.InputCostMultiplier),
			OutputCostMultiplier:    defaultCodexMultiplier(rule.OutputCostMultiplier),
			CacheReadCostMultiplier: defaultCodexMultiplier(rule.CacheReadCostMultiplier),
		})
	}
	return normalized, nil
}

func defaultCodexMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

func validateCodexEndpointName(name string) error {
	if name == "" {
		return fmt.Errorf("端点名不能为空")
	}
	if !codexEndpointNamePattern.MatchString(name) {
		return fmt.Errorf("端点名必须以字母或数字开头，仅允许字母、数字、空格、连字符或下划线，长度 1-64")
	}
	return nil
}

func validateBaseURL(baseURL string) error {
	if baseURL == "" {
		return fmt.Errorf("base_url 不能为空")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("base_url 格式无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("base_url 必须以 http:// 或 https:// 开头")
	}
	if parsed.Host == "" {
		return fmt.Errorf("base_url 缺少 host")
	}
	return nil
}

func validateCodexEndpointTimeoutSeconds(timeoutSeconds int) error {
	if timeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds 不能为负数")
	}
	return nil
}
