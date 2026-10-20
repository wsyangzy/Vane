package app

import (
	"context"
	"fmt"
	"time"

	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
)

// ModelPricingInfo 模型定价信息（给前端用的结构体）
type ModelPricingInfo struct {
	ID                         int64   `json:"id"`
	ModelName                  string  `json:"model_name"`
	Provider                   string  `json:"provider"`     // 提供商: claude/codex
	ServiceTier                string  `json:"service_tier"` // 服务档位: 空串=标准档/兜底, flex/batch/priority=档位子行
	DisplayName                string  `json:"display_name"`
	Description                string  `json:"description"`
	InputPrice                 float64 `json:"input_price"`             // USD per 1M tokens
	OutputPrice                float64 `json:"output_price"`            // USD per 1M tokens
	CacheCreationPrice5m       float64 `json:"cache_creation_price_5m"` // 5分钟缓存创建 (USD per 1M tokens)
	CacheCreationPrice1h       float64 `json:"cache_creation_price_1h"` // 1小时缓存创建 (USD per 1M tokens)
	CacheReadPrice             float64 `json:"cache_read_price"`        // USD per 1M tokens
	LongContextThreshold       int64   `json:"long_context_threshold"`
	LongContextInputPrice      float64 `json:"long_context_input_price"`
	LongContextCacheReadPrice  float64 `json:"long_context_cache_read_price"`
	LongContextCacheWritePrice float64 `json:"long_context_cache_write_price"`
	LongContextOutputPrice     float64 `json:"long_context_output_price"`
	IsDefault                  bool    `json:"is_default"`
	CreatedAt                  string  `json:"created_at"`
	UpdatedAt                  string  `json:"updated_at"`
}

// CreateModelPricingInput 创建模型定价的输入参数
type CreateModelPricingInput struct {
	// ID 仅更新时使用。带 ID 时按 ID 精确定位，因此编辑中改 provider 或改档位都能命中原行；
	// 不带则按 (provider, model_name, service_tier) 三元组定位。
	ID                         int64   `json:"id"`
	ModelName                  string  `json:"model_name"`
	Provider                   string  `json:"provider"`
	ServiceTier                string  `json:"service_tier"`
	DisplayName                string  `json:"display_name"`
	Description                string  `json:"description"`
	InputPrice                 float64 `json:"input_price"`
	OutputPrice                float64 `json:"output_price"`
	CacheCreationPrice5m       float64 `json:"cache_creation_price_5m"`
	CacheCreationPrice1h       float64 `json:"cache_creation_price_1h"`
	CacheReadPrice             float64 `json:"cache_read_price"`
	LongContextThreshold       int64   `json:"long_context_threshold"`
	LongContextInputPrice      float64 `json:"long_context_input_price"`
	LongContextCacheReadPrice  float64 `json:"long_context_cache_read_price"`
	LongContextCacheWritePrice float64 `json:"long_context_cache_write_price"`
	LongContextOutputPrice     float64 `json:"long_context_output_price"`
	IsDefault                  bool    `json:"is_default"`
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

	defaultPricing := modelPricingService.GetDefaultPricing(ctx, "")
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

	record := &pricingstore.ModelPricingRecord{
		ModelName:                  input.ModelName,
		Provider:                   provider,
		ServiceTier:                input.ServiceTier,
		DisplayName:                input.DisplayName,
		Description:                input.Description,
		InputPrice:                 input.InputPrice,
		OutputPrice:                input.OutputPrice,
		CacheCreationPrice5m:       input.CacheCreationPrice5m,
		CacheCreationPrice1h:       input.CacheCreationPrice1h,
		CacheReadPrice:             input.CacheReadPrice,
		LongContextThreshold:       input.LongContextThreshold,
		LongContextInputPrice:      input.LongContextInputPrice,
		LongContextCacheReadPrice:  input.LongContextCacheReadPrice,
		LongContextCacheWritePrice: input.LongContextCacheWritePrice,
		LongContextOutputPrice:     input.LongContextOutputPrice,
		IsDefault:                  input.IsDefault,
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

	record := &pricingstore.ModelPricingRecord{
		ID:                         input.ID,
		ModelName:                  modelName, // 使用参数中的 modelName
		Provider:                   provider,
		ServiceTier:                input.ServiceTier,
		DisplayName:                input.DisplayName,
		Description:                input.Description,
		InputPrice:                 input.InputPrice,
		OutputPrice:                input.OutputPrice,
		CacheCreationPrice5m:       input.CacheCreationPrice5m,
		CacheCreationPrice1h:       input.CacheCreationPrice1h,
		CacheReadPrice:             input.CacheReadPrice,
		LongContextThreshold:       input.LongContextThreshold,
		LongContextInputPrice:      input.LongContextInputPrice,
		LongContextCacheReadPrice:  input.LongContextCacheReadPrice,
		LongContextCacheWritePrice: input.LongContextCacheWritePrice,
		LongContextOutputPrice:     input.LongContextOutputPrice,
		IsDefault:                  input.IsDefault,
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

// DeleteModelPricing 删除模型定价（含该模型在该 provider 下的全部档位子行）。
// 必须带 provider：两个 provider 的模型名空间会重叠，不带就是跨 provider 误删。
func (a *App) DeleteModelPricing(provider, modelName string) error {
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

	if err := modelPricingService.DeletePricing(ctx, provider, modelName); err != nil {
		return fmt.Errorf("删除模型定价失败: %w", err)
	}
	a.syncPricingToTracker(ctx)

	if logger != nil {
		logger.Info("✅ 模型定价已删除", "provider", provider, "model", modelName)
	}

	return nil
}

// DeleteModelPricingTier 删除单个档位子行，标准档主行不受影响
func (a *App) DeleteModelPricingTier(provider, modelName, serviceTier string) error {
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

	if err := modelPricingService.DeleteTierPricing(ctx, provider, modelName, serviceTier); err != nil {
		return fmt.Errorf("删除档位定价失败: %w", err)
	}
	a.syncPricingToTracker(ctx)

	if logger != nil {
		logger.Info("✅ 档位定价已删除", "provider", provider, "model", modelName, "tier", serviceTier)
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
func (a *App) pricingRecordToInfo(r *pricingstore.ModelPricingRecord) ModelPricingInfo {
	provider := r.Provider
	if provider == "" {
		provider = "claude"
	}

	info := ModelPricingInfo{
		ID:                         r.ID,
		ModelName:                  r.ModelName,
		Provider:                   provider,
		ServiceTier:                r.ServiceTier,
		DisplayName:                r.DisplayName,
		Description:                r.Description,
		InputPrice:                 r.InputPrice,
		OutputPrice:                r.OutputPrice,
		CacheCreationPrice5m:       r.CacheCreationPrice5m,
		CacheCreationPrice1h:       r.CacheCreationPrice1h,
		CacheReadPrice:             r.CacheReadPrice,
		LongContextThreshold:       r.LongContextThreshold,
		LongContextInputPrice:      r.LongContextInputPrice,
		LongContextCacheReadPrice:  r.LongContextCacheReadPrice,
		LongContextCacheWritePrice: r.LongContextCacheWritePrice,
		LongContextOutputPrice:     r.LongContextOutputPrice,
		IsDefault:                  r.IsDefault,
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
