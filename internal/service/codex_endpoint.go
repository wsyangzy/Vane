package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"ai-relay-forwarder/internal/store"
)

var codexEndpointNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}\s_\-]{0,63}$`)

// CodexEndpointService 管理 Codex 端点（name + base_url）。
type CodexEndpointService struct {
	endpointStore store.CodexEndpointStore
	channelStore  store.CodexChannelStore
}

func NewCodexEndpointService(endpointStore store.CodexEndpointStore, channelStore store.CodexChannelStore) *CodexEndpointService {
	return &CodexEndpointService{endpointStore: endpointStore, channelStore: channelStore}
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

	urlConflict, err := s.endpointStore.GetByChannelAndURL(ctx, record.ChannelID, record.BaseURL)
	if err != nil {
		return nil, err
	}
	if urlConflict != nil {
		return nil, fmt.Errorf("该渠道下已存在相同的 base_url")
	}

	nameConflict, err := s.endpointStore.GetByChannelAndName(ctx, record.ChannelID, record.Name)
	if err != nil {
		return nil, err
	}
	if nameConflict != nil {
		return nil, fmt.Errorf("该渠道下已存在相同的端点名 '%s'", record.Name)
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

func (s *CodexEndpointService) UpdateEndpointByID(ctx context.Context, id int64, name, baseURL string, failoverEnabled bool, priority int, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier float64) error {
	name = strings.TrimSpace(name)
	baseURL = strings.TrimSpace(baseURL)
	if err := validateCodexEndpointName(name); err != nil {
		return err
	}
	if err := validateBaseURL(baseURL); err != nil {
		return err
	}

	existing, err := s.endpointStore.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 端点不存在: %d", id)
	}

	if existing.Name == name && existing.BaseURL == baseURL && existing.FailoverEnabled == failoverEnabled && existing.Priority == priority && existing.CostMultiplier == costMultiplier && existing.InputCostMultiplier == inputCostMultiplier && existing.OutputCostMultiplier == outputCostMultiplier && existing.CacheReadCostMultiplier == cacheReadCostMultiplier {
		return nil
	}

	if existing.BaseURL != baseURL {
		urlConflict, err := s.endpointStore.GetByChannelAndURL(ctx, existing.ChannelID, baseURL)
		if err != nil {
			return err
		}
		if urlConflict != nil && urlConflict.ID != id {
			return fmt.Errorf("该渠道下已存在相同的 base_url")
		}
	}

	if existing.Name != name {
		nameConflict, err := s.endpointStore.GetByChannelAndName(ctx, existing.ChannelID, name)
		if err != nil {
			return err
		}
		if nameConflict != nil && nameConflict.ID != id {
			return fmt.Errorf("该渠道下已存在相同的端点名 '%s'", name)
		}
	}

	return s.endpointStore.Update(ctx, id, name, baseURL, failoverEnabled, priority, costMultiplier, inputCostMultiplier, outputCostMultiplier, cacheReadCostMultiplier)
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
	if err := validateCodexEndpointName(record.Name); err != nil {
		return err
	}
	return validateBaseURL(record.BaseURL)
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
