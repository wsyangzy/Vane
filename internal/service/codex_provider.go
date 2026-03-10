package service

import (
	"context"
	"fmt"
	"strings"

	"claude-request-forwarder/internal/store"
)

// CodexProviderService 管理 Codex 提供商配置与当前激活项。
type CodexProviderService struct {
	store store.CodexProviderStore
}

func NewCodexProviderService(store store.CodexProviderStore) *CodexProviderService {
	return &CodexProviderService{store: store}
}

func (s *CodexProviderService) ListProviders(ctx context.Context) ([]*store.CodexProviderRecord, error) {
	return s.store.List(ctx)
}

func (s *CodexProviderService) GetProviderByID(ctx context.Context, id int64) (*store.CodexProviderRecord, error) {
	return s.store.GetByID(ctx, id)
}

func (s *CodexProviderService) GetActiveProvider(ctx context.Context) (*store.CodexProviderRecord, error) {
	return s.store.GetActive(ctx)
}

func (s *CodexProviderService) CreateProvider(ctx context.Context, record *store.CodexProviderRecord) (*store.CodexProviderRecord, error) {
	if err := normalizeAndValidateCodexProvider(record); err != nil {
		return nil, err
	}

	existing, err := s.store.GetByName(ctx, record.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("Codex 提供商 '%s' 已存在", record.Name)
	}

	record.IsActive = false
	created, err := s.store.Create(ctx, record)
	if err != nil {
		return nil, err
	}

	active, err := s.store.GetActive(ctx)
	if err != nil {
		return nil, err
	}
	if active == nil {
		if err := s.store.SetActive(ctx, created.ID); err != nil {
			return nil, err
		}
		return s.store.GetByID(ctx, created.ID)
	}

	return created, nil
}

func (s *CodexProviderService) UpdateProviderByID(ctx context.Context, id int64, record *store.CodexProviderRecord) error {
	if err := normalizeAndValidateCodexProvider(record); err != nil {
		return err
	}

	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}

	nameConflict, err := s.store.GetByName(ctx, record.Name)
	if err != nil {
		return err
	}
	if nameConflict != nil && nameConflict.ID != id {
		return fmt.Errorf("Codex 提供商 '%s' 已存在", record.Name)
	}

	return s.store.UpdateByID(ctx, id, record)
}

func (s *CodexProviderService) DeleteProviderByID(ctx context.Context, id int64) error {
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}

	if err := s.store.DeleteByID(ctx, id); err != nil {
		return err
	}
	if !existing.IsActive {
		return nil
	}

	providers, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		return nil
	}

	return s.store.SetActive(ctx, providers[0].ID)
}

func (s *CodexProviderService) ActivateProvider(ctx context.Context, id int64) error {
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}

	return s.store.SetActive(ctx, id)
}

func normalizeAndValidateCodexProvider(record *store.CodexProviderRecord) error {
	if record == nil {
		return fmt.Errorf("record 不能为空")
	}

	record.Name = strings.TrimSpace(record.Name)
	record.ProviderType = strings.TrimSpace(record.ProviderType)
	record.BaseURL = strings.TrimSpace(record.BaseURL)
	record.ApiKey = strings.TrimSpace(record.ApiKey)
	record.Model = strings.TrimSpace(record.Model)
	record.ReasoningEffort = strings.TrimSpace(record.ReasoningEffort)
	record.Website = strings.TrimSpace(record.Website)

	if record.Name == "" {
		return fmt.Errorf("提供商标识不能为空")
	}
	if record.ProviderType == "" {
		return fmt.Errorf("接入类型不能为空")
	}
	if record.BaseURL == "" {
		return fmt.Errorf("Base URL 不能为空")
	}
	if record.ApiKey == "" {
		return fmt.Errorf("API Key 不能为空")
	}
	if record.Model == "" {
		return fmt.Errorf("模型不能为空")
	}
	if record.ReasoningEffort == "" {
		record.ReasoningEffort = "xhigh"
	}

	return nil
}
