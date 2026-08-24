// Package service 提供业务逻辑层实现
// 模型定价服务 - 新增 (2025-12-06)
package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
	"ai-relay-forwarder/internal/store"
	"ai-relay-forwarder/internal/tracking"
)

// ModelPricingService 模型定价管理业务服务
type ModelPricingService struct {
	store store.ModelPricingStore

	// 内存缓存（用于快速查询）
	cache   map[string]*store.ModelPricingRecord
	cacheMu sync.RWMutex

	// 默认定价缓存（按 provider 区分）
	defaultPricingCache map[string]*store.ModelPricingRecord

	cacheHits          atomic.Uint64
	cacheMisses        atomic.Uint64
	cacheInvalidations atomic.Uint64
	lastUpdatedAt      time.Time
	lastInvalidatedAt  time.Time
}

// NewModelPricingService 创建模型定价服务实例
func NewModelPricingService(st store.ModelPricingStore) *ModelPricingService {
	return &ModelPricingService{
		store:               st,
		cache:               make(map[string]*store.ModelPricingRecord),
		defaultPricingCache: make(map[string]*store.ModelPricingRecord),
	}
}

// CreatePricing 创建新的模型定价
func (s *ModelPricingService) CreatePricing(ctx context.Context, record *store.ModelPricingRecord) (*store.ModelPricingRecord, error) {
	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return nil, err
	}

	// 检查是否已存在
	existing, err := s.store.Get(ctx, record.ModelName)
	if err != nil {
		return nil, fmt.Errorf("检查定价是否存在失败: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("模型定价 '%s' 已存在", record.ModelName)
	}

	// 如果设置为默认，先清除其他默认标记
	if record.IsDefault {
		if err := s.clearDefaultFlag(ctx); err != nil {
			return nil, err
		}
	}

	// 创建记录
	created, err := s.store.Create(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("创建模型定价失败: %w", err)
	}

	// 清除缓存，强制后续读取从数据库加载新事实
	s.clearCache()

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 创建模型定价: %s", record.ModelName))
	return created, nil
}

// GetPricing 获取模型定价
func (s *ModelPricingService) GetPricing(ctx context.Context, modelName string) (*store.ModelPricingRecord, error) {
	// 先查缓存
	s.cacheMu.RLock()
	if cached, ok := s.cache[modelName]; ok {
		s.cacheMu.RUnlock()
		s.cacheHits.Add(1)
		return cached, nil
	}
	s.cacheMu.RUnlock()
	s.cacheMisses.Add(1)

	// 查数据库
	record, err := s.store.Get(ctx, modelName)
	if err != nil {
		return nil, fmt.Errorf("获取模型定价失败: %w", err)
	}

	if record != nil {
		s.updateCache(record)
	}

	return record, nil
}

// GetPricingOrDefault 获取模型定价（provider-aware），如果不存在则返回该 provider 的默认定价
func (s *ModelPricingService) GetPricingOrDefault(ctx context.Context, modelName, provider string) *store.ModelPricingRecord {
	if provider == "" {
		provider = "claude"
	}
	// 先尝试获取指定模型的定价
	record, err := s.GetPricing(ctx, modelName)
	if err == nil && record != nil {
		return record
	}

	// 返回对应 provider 的默认定价
	return s.GetDefaultPricing(ctx, provider)
}

// GetDefaultPricing 获取指定 provider 的默认定价
func (s *ModelPricingService) GetDefaultPricing(ctx context.Context, provider string) *store.ModelPricingRecord {
	if provider == "" {
		provider = "claude"
	}
	s.cacheMu.RLock()
	cacheKey := provider + "::default"
	if cached, ok := s.defaultPricingCache[cacheKey]; ok && cached != nil {
		s.cacheMu.RUnlock()
		s.cacheHits.Add(1)
		return cached
	}
	s.cacheMu.RUnlock()
	s.cacheMisses.Add(1)

	// 从数据库获取指定 provider 的默认
	record, err := s.store.GetDefaultByProvider(ctx, provider)
	if err != nil || record == nil {
		// 返回硬编码的默认值
		modelName := "_default"
		if provider == "codex" {
			modelName = "_codex_default"
		}
		record = &store.ModelPricingRecord{
			ModelName:            modelName,
			Provider:             provider,
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75,
			CacheCreationPrice1h: 6.0,
			CacheReadPrice:       0.30,
			IsDefault:            true,
		}
	}

	s.cacheMu.Lock()
	s.defaultPricingCache[cacheKey] = record
	s.cacheMu.Unlock()

	return record
}

// ListPricings 列出所有模型定价
func (s *ModelPricingService) ListPricings(ctx context.Context) ([]*store.ModelPricingRecord, error) {
	records, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出模型定价失败: %w", err)
	}
	return records, nil
}

// ListPricingsByProvider 按 provider 列出模型定价
func (s *ModelPricingService) ListPricingsByProvider(ctx context.Context, provider string) ([]*store.ModelPricingRecord, error) {
	if provider == "" {
		provider = "claude"
	}
	records, err := s.store.ListByProvider(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("列出模型定价失败 (provider=%s): %w", provider, err)
	}
	return records, nil
}

// UpdatePricing 更新模型定价
func (s *ModelPricingService) UpdatePricing(ctx context.Context, record *store.ModelPricingRecord) error {
	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return err
	}

	// 验证存在
	existing, err := s.store.Get(ctx, record.ModelName)
	if err != nil {
		return fmt.Errorf("查询模型定价失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("模型定价 '%s' 不存在", record.ModelName)
	}

	// 如果设置为默认，先清除其他默认标记
	if record.IsDefault && !existing.IsDefault {
		if err := s.clearDefaultFlag(ctx); err != nil {
			return err
		}
	}

	// 更新数据库
	if err := s.store.Update(ctx, record); err != nil {
		return fmt.Errorf("更新模型定价失败: %w", err)
	}

	// 清除缓存，强制后续读取从数据库加载新事实
	s.clearCache()

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 更新模型定价: %s", record.ModelName))
	return nil
}

// DeletePricing 删除模型定价
func (s *ModelPricingService) DeletePricing(ctx context.Context, modelName string) error {
	// 验证存在
	existing, err := s.store.Get(ctx, modelName)
	if err != nil {
		return fmt.Errorf("查询模型定价失败: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("模型定价 '%s' 不存在", modelName)
	}

	// 不允许删除默认定价
	if existing.IsDefault {
		return fmt.Errorf("不能删除默认定价，请先设置其他模型为默认")
	}

	// 删除数据库记录
	if err := s.store.Delete(ctx, modelName); err != nil {
		return fmt.Errorf("删除模型定价失败: %w", err)
	}

	// 从缓存移除
	s.clearCache()

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 删除模型定价: %s", modelName))
	return nil
}

// SetDefaultPricing 设置默认定价
func (s *ModelPricingService) SetDefaultPricing(ctx context.Context, modelName string) error {
	if err := s.store.SetDefault(ctx, modelName); err != nil {
		return fmt.Errorf("设置默认定价失败: %w", err)
	}

	// 清除缓存，强制重新加载
	s.clearCache()

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 设置默认定价: %s", modelName))
	return nil
}

// ImportFromYAML 从 YAML 配置导入模型定价
// clearExisting: 是否清除现有定价
func (s *ModelPricingService) ImportFromYAML(ctx context.Context, modelPricing map[string]tracking.ModelPricing, defaultPricing tracking.ModelPricing, clearExisting bool) (int, error) {
	records := make([]*store.ModelPricingRecord, 0, len(modelPricing)+1)

	// 转换模型定价
	for modelName, pricing := range modelPricing {
		record := &store.ModelPricingRecord{
			ModelName:                  modelName,
			InputPrice:                 pricing.Input,
			OutputPrice:                pricing.Output,
			CacheCreationPrice5m:       pricing.CacheCreation, // YAML 中的 CacheCreation 默认为 5m
			CacheCreationPrice1h:       pricing.Input * 2.0,   // 1h 价格自动计算
			CacheReadPrice:             pricing.CacheRead,
			LongContextThreshold:       pricing.LongContextThreshold,
			LongContextInputPrice:      pricing.LongContextInputPrice,
			LongContextCacheReadPrice:  pricing.LongContextCacheReadPrice,
			LongContextCacheWritePrice: pricing.LongContextCacheWritePrice,
			LongContextOutputPrice:     pricing.LongContextOutputPrice,
			IsDefault:                  false,
		}
		records = append(records, record)
	}

	// 添加默认定价
	defaultRecord := &store.ModelPricingRecord{
		ModelName:                  "_default",
		DisplayName:                "默认定价",
		Description:                "未知模型使用的默认定价",
		InputPrice:                 defaultPricing.Input,
		OutputPrice:                defaultPricing.Output,
		CacheCreationPrice5m:       defaultPricing.CacheCreation,
		CacheCreationPrice1h:       defaultPricing.Input * 2.0,
		CacheReadPrice:             defaultPricing.CacheRead,
		LongContextThreshold:       defaultPricing.LongContextThreshold,
		LongContextInputPrice:      defaultPricing.LongContextInputPrice,
		LongContextCacheReadPrice:  defaultPricing.LongContextCacheReadPrice,
		LongContextCacheWritePrice: defaultPricing.LongContextCacheWritePrice,
		LongContextOutputPrice:     defaultPricing.LongContextOutputPrice,
		IsDefault:                  true,
	}
	records = append(records, defaultRecord)

	// 使用 upsert 批量导入（会更新已存在的记录）
	if err := s.store.BatchUpsert(ctx, records); err != nil {
		return 0, fmt.Errorf("批量导入失败: %w", err)
	}

	// 清除缓存，强制重新加载
	s.clearCache()

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 从 YAML 导入 %d 个模型定价", len(records)))
	return len(records), nil
}

// LoadCache 加载所有定价到缓存
func (s *ModelPricingService) LoadCache(ctx context.Context) error {
	records, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("加载模型定价缓存失败: %w", err)
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	s.cache = make(map[string]*store.ModelPricingRecord)
	s.defaultPricingCache = make(map[string]*store.ModelPricingRecord)
	for _, record := range records {
		s.cache[record.ModelName] = record
		if record.IsDefault {
			provider := record.Provider
			if provider == "" {
				provider = "claude"
			}
			s.defaultPricingCache[provider+"::default"] = record
		}
	}
	s.lastUpdatedAt = time.Now()

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 加载 %d 个模型定价到缓存", len(records)))
	return nil
}

// GetPricingCount 获取定价数量
func (s *ModelPricingService) GetPricingCount(ctx context.Context) (int, error) {
	return s.store.Count(ctx)
}

// ToTrackingPricing 转换为 tracking.ModelPricing 格式（兼容现有代码）
// 注意：CacheCreation 使用 5m 价格，因为大多数请求使用默认 5 分钟缓存
func (s *ModelPricingService) ToTrackingPricing(record *store.ModelPricingRecord) tracking.ModelPricing {
	return tracking.ModelPricing{
		Input:                      record.InputPrice,
		Output:                     record.OutputPrice,
		CacheCreation:              record.CacheCreationPrice5m, // 使用 5m 价格作为默认
		CacheRead:                  record.CacheReadPrice,
		LongContextThreshold:       record.LongContextThreshold,
		LongContextInputPrice:      record.LongContextInputPrice,
		LongContextCacheReadPrice:  record.LongContextCacheReadPrice,
		LongContextCacheWritePrice: record.LongContextCacheWritePrice,
		LongContextOutputPrice:     record.LongContextOutputPrice,
	}
}

// validateRecord 验证模型定价记录
func (s *ModelPricingService) validateRecord(record *store.ModelPricingRecord) error {
	if record.ModelName == "" {
		return fmt.Errorf("模型名称不能为空")
	}
	if record.InputPrice < 0 {
		return fmt.Errorf("输入价格不能为负数")
	}
	if record.OutputPrice < 0 {
		return fmt.Errorf("输出价格不能为负数")
	}
	if record.LongContextThreshold < 0 {
		return fmt.Errorf("Long context 阈值不能为负数")
	}
	if record.LongContextInputPrice < 0 {
		return fmt.Errorf("Long context 输入价格不能为负数")
	}
	if record.LongContextCacheReadPrice < 0 {
		return fmt.Errorf("Long context 缓存读取价格不能为负数")
	}
	if record.LongContextCacheWritePrice < 0 {
		return fmt.Errorf("Long context 缓存写入价格不能为负数")
	}
	if record.LongContextOutputPrice < 0 {
		return fmt.Errorf("Long context 输出价格不能为负数")
	}
	return nil
}

// clearDefaultFlag 清除所有默认标记
func (s *ModelPricingService) clearDefaultFlag(ctx context.Context) error {
	records, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("获取定价列表失败: %w", err)
	}

	for _, record := range records {
		if record.IsDefault {
			record.IsDefault = false
			if err := s.store.Update(ctx, record); err != nil {
				return fmt.Errorf("清除默认标记失败: %w", err)
			}
		}
	}

	return nil
}

// updateCache 更新缓存
func (s *ModelPricingService) updateCache(record *store.ModelPricingRecord) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	s.cache[record.ModelName] = record
	if record.IsDefault {
		if s.defaultPricingCache == nil {
			s.defaultPricingCache = make(map[string]*store.ModelPricingRecord)
		}
		provider := record.Provider
		if provider == "" {
			provider = "claude"
		}
		s.defaultPricingCache[provider+"::default"] = record
	}
	s.lastUpdatedAt = time.Now()
}

// clearCache 清除缓存
func (s *ModelPricingService) clearCache() int {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	removed := len(s.cache)
	s.cache = make(map[string]*store.ModelPricingRecord)
	s.defaultPricingCache = make(map[string]*store.ModelPricingRecord)
	s.cacheInvalidations.Add(1)
	s.lastInvalidatedAt = time.Now()
	return removed
}

// InvalidateCache clears the model-pricing read cache.
func (s *ModelPricingService) InvalidateCache() int {
	if s == nil {
		return 0
	}
	return s.clearCache()
}

// CacheStats returns a cache status snapshot for unified cache observability.
func (s *ModelPricingService) CacheStats() appcache.Stats {
	if s == nil {
		return appcache.Stats{Name: "model-pricing"}
	}
	s.cacheMu.RLock()
	stats := appcache.Stats{
		Name:              "model-pricing",
		Enabled:           true,
		Size:              len(s.cache),
		Hits:              s.cacheHits.Load(),
		Misses:            s.cacheMisses.Load(),
		Invalidations:     s.cacheInvalidations.Load(),
		LastUpdatedAt:     s.lastUpdatedAt,
		LastInvalidatedAt: s.lastInvalidatedAt,
	}
	s.cacheMu.RUnlock()
	return stats
}
