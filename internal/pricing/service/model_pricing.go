// Package service 提供业务逻辑层实现
// 模型定价服务 - 新增 (2025-12-06)
package service

import (
	"context"
	"fmt"
	"log/slog"

	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// ModelPricingService 模型定价管理业务服务
//
// 不设读缓存：计费走 UsageTracker 自己的定价表（syncPricingToTracker 同步，key 含 provider 与档位），
// 这里只服务低频的管理页读写，直接查库即可保证读到最新事实。
type ModelPricingService struct {
	store pricingstore.ModelPricingStore
}

// NewModelPricingService 创建模型定价服务实例
func NewModelPricingService(st pricingstore.ModelPricingStore) *ModelPricingService {
	return &ModelPricingService{store: st}
}

// CreatePricing 创建新的模型定价
func (s *ModelPricingService) CreatePricing(ctx context.Context, record *pricingstore.ModelPricingRecord) (*pricingstore.ModelPricingRecord, error) {
	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return nil, err
	}

	// 检查是否已存在：按三元组定位，否则新建档位子行时会被标准档主行误判为重复
	existing, err := s.findExistingRecord(ctx, record)
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

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 创建模型定价: %s", record.ModelName))
	return created, nil
}

// GetPricing 获取模型的标准档主行定价
func (s *ModelPricingService) GetPricing(ctx context.Context, modelName string) (*pricingstore.ModelPricingRecord, error) {
	record, err := s.store.Get(ctx, modelName)
	if err != nil {
		return nil, fmt.Errorf("获取模型定价失败: %w", err)
	}
	return record, nil
}

// GetPricingOrDefault 获取模型定价（provider-aware），如果不存在则返回该 provider 的默认定价
func (s *ModelPricingService) GetPricingOrDefault(ctx context.Context, modelName, provider string) *pricingstore.ModelPricingRecord {
	provider = normalizeProvider(provider)
	// 先尝试获取指定模型的定价
	record, err := s.GetPricing(ctx, modelName)
	if err == nil && record != nil {
		return record
	}

	// 返回对应 provider 的默认定价
	return s.GetDefaultPricing(ctx, provider)
}

// GetDefaultPricing 获取指定 provider 的默认定价
func (s *ModelPricingService) GetDefaultPricing(ctx context.Context, provider string) *pricingstore.ModelPricingRecord {
	provider = normalizeProvider(provider)

	// 从数据库获取指定 provider 的默认
	record, err := s.store.GetDefaultByProvider(ctx, provider)
	if err != nil || record == nil {
		// 返回硬编码的默认值
		modelName := "_default"
		if provider == "codex" {
			modelName = "_codex_default"
		}
		record = &pricingstore.ModelPricingRecord{
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

	return record
}

// ListPricings 列出所有模型定价
func (s *ModelPricingService) ListPricings(ctx context.Context) ([]*pricingstore.ModelPricingRecord, error) {
	records, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出模型定价失败: %w", err)
	}
	return records, nil
}

// ListPricingsByProvider 按 provider 列出模型定价
func (s *ModelPricingService) ListPricingsByProvider(ctx context.Context, provider string) ([]*pricingstore.ModelPricingRecord, error) {
	provider = normalizeProvider(provider)
	records, err := s.store.ListByProvider(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("列出模型定价失败 (provider=%s): %w", provider, err)
	}
	return records, nil
}

// UpdatePricing 更新模型定价
func (s *ModelPricingService) UpdatePricing(ctx context.Context, record *pricingstore.ModelPricingRecord) error {
	// 验证必填字段
	if err := s.validateRecord(record); err != nil {
		return err
	}

	// 验证存在：档位子行必须按三元组定位，用 Get 只会查到标准档主行
	existing, err := s.findExistingRecord(ctx, record)
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

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 更新模型定价: %s", record.ModelName))
	return nil
}

// DeletePricing 删除模型定价（含该模型在该 provider 下的全部档位子行）
func (s *ModelPricingService) DeletePricing(ctx context.Context, provider, modelName string) error {
	provider = normalizeProvider(provider)

	// 验证存在（标准档主行）
	existing, err := s.store.GetTier(ctx, provider, modelName, "")
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
	if err := s.store.Delete(ctx, provider, modelName); err != nil {
		return fmt.Errorf("删除模型定价失败: %w", err)
	}

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 删除模型定价: %s/%s", provider, modelName))
	return nil
}

// DeleteTierPricing 删除单个档位子行，标准档主行不受影响
func (s *ModelPricingService) DeleteTierPricing(ctx context.Context, provider, modelName, serviceTier string) error {
	provider = normalizeProvider(provider)

	if err := s.store.DeleteTier(ctx, provider, modelName, serviceTier); err != nil {
		return fmt.Errorf("删除档位定价失败: %w", err)
	}

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 删除档位定价: %s/%s/%s", provider, modelName, serviceTier))
	return nil
}

// SetDefaultPricing 设置默认定价
func (s *ModelPricingService) SetDefaultPricing(ctx context.Context, modelName string) error {
	if err := s.store.SetDefault(ctx, modelName); err != nil {
		return fmt.Errorf("设置默认定价失败: %w", err)
	}

	slog.Info(fmt.Sprintf("✅ [ModelPricingService] 设置默认定价: %s", modelName))
	return nil
}

// GetPricingCount 获取定价数量
func (s *ModelPricingService) GetPricingCount(ctx context.Context) (int, error) {
	return s.store.Count(ctx)
}

// ToTrackingPricing 转换为 tracking.ModelPricing 格式（兼容现有代码）
// 注意：CacheCreation 使用 5m 价格，因为大多数请求使用默认 5 分钟缓存
func (s *ModelPricingService) ToTrackingPricing(record *pricingstore.ModelPricingRecord) tracking.ModelPricing {
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

// normalizeProvider 空 provider 归一化为 claude，与 DB 的 COALESCE(provider,'claude') 口径一致
func normalizeProvider(provider string) string {
	if provider == "" {
		return "claude"
	}
	return provider
}

// findExistingRecord 定位一条已存在的定价行。
// ID 优先：ID 跨 provider 唯一，编辑时把 provider 从 claude 改成 codex 也能命中原行；
// 无 ID（新建校验）时按 (provider, model_name, service_tier) 三元组定位，
// 用 Get 只会查到标准档主行，会把新建档位子行误判成重复。
func (s *ModelPricingService) findExistingRecord(ctx context.Context, record *pricingstore.ModelPricingRecord) (*pricingstore.ModelPricingRecord, error) {
	if record.ID > 0 {
		return s.store.GetByID(ctx, record.ID)
	}
	return s.store.GetTier(ctx, normalizeProvider(record.Provider), record.ModelName, record.ServiceTier)
}

// validateRecord 验证模型定价记录
func (s *ModelPricingService) validateRecord(record *pricingstore.ModelPricingRecord) error {
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
