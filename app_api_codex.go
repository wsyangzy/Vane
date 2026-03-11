package main

import (
	"context"
	"fmt"
	"time"

	"claude-request-forwarder/internal/store"
)

// CodexProviderInfo 提供给前端展示的 Codex 提供商信息。
type CodexProviderInfo struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	ProviderType     string `json:"provider_type"`
	BaseURL          string `json:"base_url"`
	ApiKey           string `json:"api_key"`
	ApiKeyMasked     string `json:"api_key_masked"`
	Model            string `json:"model"`
	ReasoningEffort  string `json:"model_reasoning_effort"`
	Website          string `json:"website"`
	IsActive         bool   `json:"is_active"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// CreateCodexProviderInput 创建 Codex 提供商的输入参数。
type CreateCodexProviderInput struct {
	Name             string `json:"name"`
	ProviderType     string `json:"provider_type"`
	BaseURL          string `json:"base_url"`
	ApiKey           string `json:"api_key"`
	Model            string `json:"model"`
	ReasoningEffort  string `json:"model_reasoning_effort"`
	Website          string `json:"website"`
}

// UpdateCodexProviderInput 更新 Codex 提供商的输入参数。
type UpdateCodexProviderInput struct {
	Name             string `json:"name"`
	ProviderType     string `json:"provider_type"`
	BaseURL          string `json:"base_url"`
	ApiKey           string `json:"api_key"`
	Model            string `json:"model"`
	ReasoningEffort  string `json:"model_reasoning_effort"`
	Website          string `json:"website"`
}

// CodexProviderStorageStatus 表示 Codex 提供商存储状态。
type CodexProviderStorageStatus struct {
	Enabled            bool   `json:"enabled"`
	TotalCount         int    `json:"total_count"`
	HasActive          bool   `json:"has_active"`
	ActiveProviderID   int64  `json:"active_provider_id"`
	ActiveProviderName string `json:"active_provider_name"`
}

func (a *App) GetCodexProviderStorageStatus() CodexProviderStorageStatus {
	status := CodexProviderStorageStatus{}
	a.ensureCodexProviderService()

	a.mu.RLock()
	codexProviderService := a.codexProviderService
	a.mu.RUnlock()
	if codexProviderService == nil {
		return status
	}

	status.Enabled = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	providers, err := codexProviderService.ListProviders(ctx)
	if err == nil {
		status.TotalCount = len(providers)
	}

	active, err := codexProviderService.GetActiveProvider(ctx)
	if err == nil && active != nil {
		status.HasActive = true
		status.ActiveProviderID = active.ID
		status.ActiveProviderName = active.Name
	}

	return status
}

func (a *App) GetCodexProviders() ([]CodexProviderInfo, error) {
	a.ensureCodexProviderService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.mu.RLock()
	codexProviderService := a.codexProviderService
	a.mu.RUnlock()
	if codexProviderService == nil {
		return nil, fmt.Errorf("Codex 提供商服务未就绪，请稍后重试")
	}

	records, err := codexProviderService.ListProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 Codex 提供商列表失败: %w", err)
	}

	result := make([]CodexProviderInfo, 0, len(records))
	for _, record := range records {
		result = append(result, a.codexProviderRecordToInfo(record))
	}
	return result, nil
}

func (a *App) CreateCodexProvider(input CreateCodexProviderInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	codexProviderService := a.codexProviderService
	logger := a.logger
	a.mu.RUnlock()
	if codexProviderService == nil {
		return fmt.Errorf("Codex 提供商服务未就绪，请稍后重试")
	}

	record := &store.CodexProviderRecord{
		Name:            input.Name,
		ProviderType:    input.ProviderType,
		BaseURL:         input.BaseURL,
		ApiKey:          input.ApiKey,
		Model:           input.Model,
		ReasoningEffort: input.ReasoningEffort,
		Website:         input.Website,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	created, err := codexProviderService.CreateProvider(ctx, record)
	if err != nil {
		return fmt.Errorf("创建 Codex 提供商失败: %w", err)
	}

	if created != nil && created.IsActive {
		if syncErr := a.syncActiveCodexProviderToConfig(created, ""); syncErr != nil {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer rollbackCancel()
			_ = codexProviderService.DeleteProviderByID(rollbackCtx, created.ID)
			return fmt.Errorf("创建 Codex 提供商失败: 配置接管未生效: %w", syncErr)
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 提供商已创建", "name", created.Name, "provider_type", created.ProviderType)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) UpdateCodexProviderByID(id int64, input UpdateCodexProviderInput) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	codexProviderService := a.codexProviderService
	logger := a.logger
	a.mu.RUnlock()
	if codexProviderService == nil {
		return fmt.Errorf("Codex 提供商服务未就绪，请稍后重试")
	}

	record := &store.CodexProviderRecord{
		Name:            input.Name,
		ProviderType:    input.ProviderType,
		BaseURL:         input.BaseURL,
		ApiKey:          input.ApiKey,
		Model:           input.Model,
		ReasoningEffort: input.ReasoningEffort,
		Website:         input.Website,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previous, err := codexProviderService.GetProviderByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取 Codex 提供商失败: %w", err)
	}
	if previous == nil {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}

	if err := codexProviderService.UpdateProviderByID(ctx, id, record); err != nil {
		return fmt.Errorf("更新 Codex 提供商失败: %w", err)
	}

	updated, err := codexProviderService.GetProviderByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取更新后的 Codex 提供商失败: %w", err)
	}
	if updated != nil && updated.IsActive {
		if syncErr := a.syncActiveCodexProviderToConfig(updated, previous.Name); syncErr != nil {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer rollbackCancel()
			_ = codexProviderService.UpdateProviderByID(rollbackCtx, id, previous)
			return fmt.Errorf("更新 Codex 提供商失败: 配置接管未生效: %w", syncErr)
		}
	}

	updatedName := record.Name
	if updated != nil {
		updatedName = updated.Name
	}

	if logger != nil {
		logger.Info("✅ Codex 提供商已更新", "id", id, "name", updatedName)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) DeleteCodexProviderByID(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	codexProviderService := a.codexProviderService
	logger := a.logger
	a.mu.RUnlock()
	if codexProviderService == nil {
		return fmt.Errorf("Codex 提供商服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	target, err := codexProviderService.GetProviderByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取待删除的 Codex 提供商失败: %w", err)
	}
	if target == nil {
		return fmt.Errorf("Codex 提供商不存在: %d", id)
	}

	if err := codexProviderService.DeleteProviderByID(ctx, id); err != nil {
		return fmt.Errorf("删除 Codex 提供商失败: %w", err)
	}

	if target.IsActive {
		active, activeErr := codexProviderService.GetActiveProvider(ctx)
		if activeErr != nil {
			if logger != nil {
				logger.Warn("⚠️ 读取当前激活的 Codex 提供商失败", "error", activeErr)
			}
		} else if syncErr := a.removeCodexProviderConfig(target.Name, active); syncErr != nil {
			if logger != nil {
				logger.Warn("⚠️ 同步 Codex 配置失败", "error", syncErr, "deleted_provider", target.Name)
			}
		}
	}

	if logger != nil {
		logger.Info("✅ Codex 提供商已删除", "id", id)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) ActivateCodexProvider(id int64) error {
	a.ensureCodexProviderService()

	a.mu.RLock()
	codexProviderService := a.codexProviderService
	logger := a.logger
	a.mu.RUnlock()
	if codexProviderService == nil {
		return fmt.Errorf("Codex 提供商服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previousActive, err := codexProviderService.GetActiveProvider(ctx)
	if err != nil {
		return fmt.Errorf("读取当前激活的 Codex 提供商失败: %w", err)
	}

	if err := codexProviderService.ActivateProvider(ctx, id); err != nil {
		return fmt.Errorf("切换 Codex 提供商失败: %w", err)
	}

	activated, err := codexProviderService.GetProviderByID(ctx, id)
	if err != nil {
		return fmt.Errorf("读取激活后的 Codex 提供商失败: %w", err)
	}
	if activated == nil {
		return fmt.Errorf("激活后的 Codex 提供商不存在: %d", id)
	}

	previousName := ""
	if previousActive != nil && previousActive.ID != id {
		previousName = previousActive.Name
	}

	if err := a.syncActiveCodexProviderToConfig(activated, previousName); err != nil {
		if previousActive != nil && previousActive.ID != id {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer rollbackCancel()
			_ = codexProviderService.ActivateProvider(rollbackCtx, previousActive.ID)
		}
		return fmt.Errorf("切换 Codex 提供商失败: 配置接管未生效: %w", err)
	}

	if logger != nil {
		logger.Info("✅ Codex 提供商已激活", "id", id, "provider", activated.Name, "model", activated.Model, "reasoning_effort", activated.ReasoningEffort)
	}
	go a.emitCodexProviderUpdate()
	return nil
}

func (a *App) codexProviderRecordToInfo(record *store.CodexProviderRecord) CodexProviderInfo {
	info := CodexProviderInfo{
		ID:              record.ID,
		Name:            record.Name,
		ProviderType:    record.ProviderType,
		BaseURL:         record.BaseURL,
		ApiKey:          record.ApiKey,
		ApiKeyMasked:    maskToken(record.ApiKey),
		Model:           record.Model,
		ReasoningEffort: record.ReasoningEffort,
		Website:         record.Website,
		IsActive:        record.IsActive,
	}

	if !record.CreatedAt.IsZero() {
		info.CreatedAt = record.CreatedAt.Format("2006-01-02 15:04:05")
	}
	if !record.UpdatedAt.IsZero() {
		info.UpdatedAt = record.UpdatedAt.Format("2006-01-02 15:04:05")
	}

	return info
}

