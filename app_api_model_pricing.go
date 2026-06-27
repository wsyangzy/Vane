// app_api_model_pricing.go - 模型定价管理 API (Wails Bindings)
// 提供 SQLite 模型定价存储的增删改查功能
// 创建时间: 2025-12-06

package main

import (
	"context"
	"fmt"
	"time"

	"ai-relay-forwarder/internal/store"
)

// ============================================================
// 模型定价管理 API (SQLite)
// ============================================================

// ModelPricingInfo 模型定价信息（给前端用的结构体）
type ModelPricingInfo struct {
	ID                   int64   `json:"id"`
	ModelName            string  `json:"model_name"`
	Provider             string  `json:"provider"` // 提供商: claude/codex
	DisplayName          string  `json:"display_name"`
	Description          string  `json:"description"`
	InputPrice           float64 `json:"input_price"`             // USD per 1M tokens
	OutputPrice          float64 `json:"output_price"`            // USD per 1M tokens
	CacheCreationPrice5m float64 `json:"cache_creation_price_5m"` // 5分钟缓存创建 (USD per 1M tokens)
	CacheCreationPrice1h float64 `json:"cache_creation_price_1h"` // 1小时缓存创建 (USD per 1M tokens)
	CacheReadPrice       float64 `json:"cache_read_price"`        // USD per 1M tokens
	IsDefault            bool    `json:"is_default"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

// CreateModelPricingInput 创建模型定价的输入参数
type CreateModelPricingInput struct {
	ModelName            string  `json:"model_name"`
	Provider             string  `json:"provider"`
	DisplayName          string  `json:"display_name"`
	Description          string  `json:"description"`
	InputPrice           float64 `json:"input_price"`
	OutputPrice          float64 `json:"output_price"`
	CacheCreationPrice5m float64 `json:"cache_creation_price_5m"`
	CacheCreationPrice1h float64 `json:"cache_creation_price_1h"`
	CacheReadPrice       float64 `json:"cache_read_price"`
	IsDefault            bool    `json:"is_default"`
}

// ModelPricingStorageStatus 模型定价存储状态
type ModelPricingStorageStatus struct {
	Enabled    bool `json:"enabled"`     // 是否启用
	TotalCount int  `json:"total_count"` // 定价总数
	HasDefault bool `json:"has_default"` // 是否有默认定价
}

// GetModelPricingStorageStatus 获取模型定价存储状态
func (a *App) GetModelPricingStorageStatus() ModelPricingStorageStatus {
	status := ModelPricingStorageStatus{
		Enabled: false,
	}
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	a.mu.RUnlock()

	if modelPricingService == nil {
		return status
	}

	status.Enabled = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := modelPricingService.GetPricingCount(ctx)
	if err == nil {
		status.TotalCount = count
	}

	defaultPricing := modelPricingService.GetDefaultPricing(ctx)
	status.HasDefault = defaultPricing != nil

	return status
}

// GetModelPricings 获取所有模型定价
func (a *App) GetModelPricings() ([]ModelPricingInfo, error) {
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	a.mu.RUnlock()

	if modelPricingService == nil {
		return nil, fmt.Errorf("模型定价服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := modelPricingService.ListPricings(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取模型定价列表失败: %w", err)
	}

	result := make([]ModelPricingInfo, 0, len(records))
	for _, r := range records {
		result = append(result, a.pricingRecordToInfo(r))
	}

	return result, nil
}

// GetModelPricingsByProvider 按 provider 获取模型定价列表
func (a *App) GetModelPricingsByProvider(provider string) ([]ModelPricingInfo, error) {
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	a.mu.RUnlock()

	if modelPricingService == nil {
		return nil, fmt.Errorf("模型定价服务未就绪，请稍后重试")
	}

	if provider == "" {
		provider = "claude"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := modelPricingService.ListPricingsByProvider(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("获取模型定价列表失败: %w", err)
	}

	result := make([]ModelPricingInfo, 0, len(records))
	for _, r := range records {
		result = append(result, a.pricingRecordToInfo(r))
	}

	return result, nil
}

// GetModelPricing 获取单个模型定价
func (a *App) GetModelPricing(modelName string) (ModelPricingInfo, error) {
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	a.mu.RUnlock()

	if modelPricingService == nil {
		return ModelPricingInfo{}, fmt.Errorf("模型定价服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	record, err := modelPricingService.GetPricing(ctx, modelName)
	if err != nil {
		return ModelPricingInfo{}, fmt.Errorf("获取模型定价失败: %w", err)
	}
	if record == nil {
		return ModelPricingInfo{}, fmt.Errorf("模型定价 '%s' 不存在", modelName)
	}

	return a.pricingRecordToInfo(record), nil
}

// CreateModelPricing 创建新模型定价
func (a *App) CreateModelPricing(input CreateModelPricingInput) error {
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	logger := a.logger
	a.mu.RUnlock()

	if modelPricingService == nil {
		return fmt.Errorf("模型定价服务未就绪，请稍后重试")
	}

	// 设置默认 provider
	provider := input.Provider
	if provider == "" {
		provider = "claude"
	}

	// 设置默认值
	if input.InputPrice == 0 {
		input.InputPrice = 3.0
	}
	if input.OutputPrice == 0 {
		input.OutputPrice = 15.0
	}
	if input.CacheCreationPrice5m == 0 && provider == "claude" {
		input.CacheCreationPrice5m = input.InputPrice * 1.25
	}
	if input.CacheCreationPrice1h == 0 && provider == "claude" {
		input.CacheCreationPrice1h = input.InputPrice * 2.0
	}
	if input.CacheReadPrice == 0 && provider == "claude" {
		input.CacheReadPrice = input.InputPrice * 0.1
	}

	record := &store.ModelPricingRecord{
		ModelName:            input.ModelName,
		Provider:             provider,
		DisplayName:          input.DisplayName,
		Description:          input.Description,
		InputPrice:           input.InputPrice,
		OutputPrice:          input.OutputPrice,
		CacheCreationPrice5m: input.CacheCreationPrice5m,
		CacheCreationPrice1h: input.CacheCreationPrice1h,
		CacheReadPrice:       input.CacheReadPrice,
		IsDefault:            input.IsDefault,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := modelPricingService.CreatePricing(ctx, record)
	if err != nil {
		return fmt.Errorf("创建模型定价失败: %w", err)
	}
	a.syncPricingToTracker(ctx)

	if logger != nil {
		logger.Info("✅ 模型定价已创建", "model", input.ModelName, "provider", provider)
	}

	return nil
}

// UpdateModelPricing 更新模型定价
func (a *App) UpdateModelPricing(modelName string, input CreateModelPricingInput) error {
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	logger := a.logger
	a.mu.RUnlock()

	if modelPricingService == nil {
		return fmt.Errorf("模型定价服务未就绪，请稍后重试")
	}

	provider := input.Provider
	if provider == "" {
		provider = "claude"
	}

	record := &store.ModelPricingRecord{
		ModelName:            modelName, // 使用参数中的 modelName
		Provider:             provider,
		DisplayName:          input.DisplayName,
		Description:          input.Description,
		InputPrice:           input.InputPrice,
		OutputPrice:          input.OutputPrice,
		CacheCreationPrice5m: input.CacheCreationPrice5m,
		CacheCreationPrice1h: input.CacheCreationPrice1h,
		CacheReadPrice:       input.CacheReadPrice,
		IsDefault:            input.IsDefault,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := modelPricingService.UpdatePricing(ctx, record); err != nil {
		return fmt.Errorf("更新模型定价失败: %w", err)
	}
	a.syncPricingToTracker(ctx)

	if logger != nil {
		logger.Info("✅ 模型定价已更新", "model", modelName, "provider", provider)
	}

	return nil
}

// DeleteModelPricing 删除模型定价
func (a *App) DeleteModelPricing(modelName string) error {
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	logger := a.logger
	a.mu.RUnlock()

	if modelPricingService == nil {
		return fmt.Errorf("模型定价服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := modelPricingService.DeletePricing(ctx, modelName); err != nil {
		return fmt.Errorf("删除模型定价失败: %w", err)
	}
	a.syncPricingToTracker(ctx)

	if logger != nil {
		logger.Info("✅ 模型定价已删除", "model", modelName)
	}

	return nil
}

// SetDefaultModelPricing 设置默认模型定价
func (a *App) SetDefaultModelPricing(modelName string) error {
	a.ensureModelPricingService()
	a.mu.RLock()
	modelPricingService := a.modelPricingService
	logger := a.logger
	a.mu.RUnlock()

	if modelPricingService == nil {
		return fmt.Errorf("模型定价服务未就绪，请稍后重试")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := modelPricingService.SetDefaultPricing(ctx, modelName); err != nil {
		return fmt.Errorf("设置默认定价失败: %w", err)
	}
	a.syncPricingToTracker(ctx)

	if logger != nil {
		logger.Info("✅ 已设置默认模型定价", "model", modelName)
	}

	return nil
}

// pricingRecordToInfo 将数据库记录转换为前端 Info 结构
func (a *App) pricingRecordToInfo(r *store.ModelPricingRecord) ModelPricingInfo {
	provider := r.Provider
	if provider == "" {
		provider = "claude"
	}

	info := ModelPricingInfo{
		ID:                   r.ID,
		ModelName:            r.ModelName,
		Provider:             provider,
		DisplayName:          r.DisplayName,
		Description:          r.Description,
		InputPrice:           r.InputPrice,
		OutputPrice:          r.OutputPrice,
		CacheCreationPrice5m: r.CacheCreationPrice5m,
		CacheCreationPrice1h: r.CacheCreationPrice1h,
		CacheReadPrice:       r.CacheReadPrice,
		IsDefault:            r.IsDefault,
	}

	if !r.CreatedAt.IsZero() {
		info.CreatedAt = r.CreatedAt.Format("2006-01-02 15:04:05")
	}
	if !r.UpdatedAt.IsZero() {
		info.UpdatedAt = r.UpdatedAt.Format("2006-01-02 15:04:05")
	} else if !r.CreatedAt.IsZero() {
		info.UpdatedAt = info.CreatedAt
	}

	return info
}
