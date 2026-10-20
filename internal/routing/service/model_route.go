// Package service 提供业务逻辑层实现
// 模型路由规则服务 - 新增 (2026-09-28)
package service

import (
	"context"
	"fmt"
	"strings"

	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

// ModelRouteService 管理模型路由规则。规则按 provider 隔离；
// 目标的存在性由调用方传入的 provider 目标目录校验，本服务不感知具体 provider 的存储。
type ModelRouteService struct {
	store        routingstore.ModelRouteStore
	pricingStore pricingstore.ModelPricingStore
}

// NewModelRouteService 创建模型路由规则服务；pricingStore 可为 nil（不做定价告警）
func NewModelRouteService(routeStore routingstore.ModelRouteStore, pricingStore pricingstore.ModelPricingStore) *ModelRouteService {
	return &ModelRouteService{store: routeStore, pricingStore: pricingStore}
}

// List 列出 provider 下全部规则
func (s *ModelRouteService) List(ctx context.Context, provider string) ([]*routingstore.ModelRouteRecord, error) {
	provider, err := normalizeRouteProvider(provider)
	if err != nil {
		return nil, err
	}
	return s.store.List(ctx, provider)
}

// EnabledRules 返回 provider 下已启用的规则，转换为运行时快照
func (s *ModelRouteService) EnabledRules(ctx context.Context, provider string) ([]routing.ModelRule, error) {
	provider, err := normalizeRouteProvider(provider)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListEnabled(ctx, provider)
	if err != nil {
		return nil, err
	}
	rules := make([]routing.ModelRule, 0, len(records))
	for _, record := range records {
		if record == nil || len(record.Targets) == 0 {
			continue
		}
		rules = append(rules, routing.ModelRule{
			ID:              record.ID,
			SourceChannelID: record.SourceChannelID,
			Name:            record.Name,
			Priority:        record.Priority,
			MatchModel:      record.MatchModel,
			Mode:            record.Mode,
			Targets:         record.Targets,
		})
	}
	return rules, nil
}

// Save 校验并保存规则（ID 为 0 时新建），返回保存后的记录与告警。
// 告警不阻止保存：目标渠道暂停、目标声明不接非流、目标模型缺定价都可能是有意为之。
func (s *ModelRouteService) Save(ctx context.Context, record *routingstore.ModelRouteRecord, catalog routing.Catalog) (*routingstore.ModelRouteRecord, []string, error) {
	if record == nil {
		return nil, nil, fmt.Errorf("record 不能为空")
	}
	working := *record
	provider, err := normalizeRouteProvider(working.Provider)
	if err != nil {
		return nil, nil, err
	}
	working.Provider = provider
	working.Name = strings.TrimSpace(working.Name)
	working.MatchModel = strings.TrimSpace(working.MatchModel)
	working.Mode = routing.NormalizeMode(working.Mode)
	working.Targets = routing.NormalizeTargets(working.Targets)
	switch {
	case working.SourceChannelID <= 0:
		return nil, nil, fmt.Errorf("模型路由规则必须挂在一个来源渠道下")
	case catalog.Channel(working.SourceChannelID) == nil:
		return nil, nil, fmt.Errorf("来源渠道不存在（ID=%d）", working.SourceChannelID)
	case working.Name == "":
		return nil, nil, fmt.Errorf("规则名称不能为空")
	case working.MatchModel == "":
		return nil, nil, fmt.Errorf("匹配模型不能为空")
	case working.Mode == "":
		return nil, nil, fmt.Errorf("路由模式无效（可选 prefer / fallback）")
	case len(working.Targets) == 0:
		return nil, nil, fmt.Errorf("至少需要一个路由目标")
	}
	if err := catalog.Validate(working.Targets); err != nil {
		return nil, nil, err
	}
	for _, target := range working.Targets {
		// 目标是来源渠道自身时，prefer 会原地打转、fallback 会把同一渠道试两遍
		if target.ChannelID == working.SourceChannelID {
			return nil, nil, fmt.Errorf("模型路由的目标不能是来源渠道自身；只想改写模型请使用渠道的模型映射")
		}
	}

	if working.ID == 0 {
		created, err := s.store.Create(ctx, &working)
		if err != nil {
			return nil, nil, err
		}
		return created, s.TargetWarnings(ctx, provider, working.Targets, catalog), nil
	}

	existing, err := s.store.GetByID(ctx, working.ID)
	if err != nil {
		return nil, nil, err
	}
	if existing == nil {
		return nil, nil, fmt.Errorf("模型路由规则不存在: %d", working.ID)
	}
	if existing.Provider != provider {
		return nil, nil, fmt.Errorf("不能修改规则所属的 provider")
	}
	if existing.SourceChannelID != working.SourceChannelID {
		return nil, nil, fmt.Errorf("不能修改规则所属的来源渠道")
	}
	if err := s.store.Update(ctx, &working); err != nil {
		return nil, nil, err
	}
	saved, err := s.store.GetByID(ctx, working.ID)
	if err != nil {
		return nil, nil, err
	}
	return saved, s.TargetWarnings(ctx, provider, working.Targets, catalog), nil
}

// ListBySource 列出挂在某个来源渠道上的规则
func (s *ModelRouteService) ListBySource(ctx context.Context, provider string, sourceChannelID int64) ([]*routingstore.ModelRouteRecord, error) {
	provider, err := normalizeRouteProvider(provider)
	if err != nil {
		return nil, err
	}
	return s.store.ListBySource(ctx, provider, sourceChannelID)
}

// DeleteBySource 渠道被删除时清理其下规则
func (s *ModelRouteService) DeleteBySource(ctx context.Context, provider string, sourceChannelID int64) error {
	provider, err := normalizeRouteProvider(provider)
	if err != nil {
		return err
	}
	return s.store.DeleteBySource(ctx, provider, sourceChannelID)
}

// Get 按 ID 获取规则
func (s *ModelRouteService) Get(ctx context.Context, id int64) (*routingstore.ModelRouteRecord, error) {
	return s.store.GetByID(ctx, id)
}

// Delete 删除规则
func (s *ModelRouteService) Delete(ctx context.Context, id int64) error {
	return s.store.Delete(ctx, id)
}

// SetEnabled 启用或停用规则
func (s *ModelRouteService) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	return s.store.SetEnabled(ctx, id, enabled)
}

// TargetWarnings 目标配置的告警：渠道暂停（命中时会被跳过）、渠道自身开启了非流改道
// （说明它不接非流请求）、强制改写的上游模型在本 provider 定价表中没有定价行（成本会落到兜底价）。
func (s *ModelRouteService) TargetWarnings(ctx context.Context, provider string, targets []routing.Target, catalog routing.Catalog) []string {
	var warnings []string
	seenModels := make(map[string]struct{}, len(targets))
	for i, target := range targets {
		channel := catalog.Channel(target.ChannelID)
		if channel == nil {
			continue
		}
		label := fmt.Sprintf("第 %d 个目标（%s）", i+1, channel.Name)
		if channel.Paused && !channel.Active {
			warnings = append(warnings, label+"所在渠道已暂停，命中时会被跳过")
		}
		if channel.NonStream != nil {
			warnings = append(warnings, label+"所在渠道开启了非流改道，说明它不接非流请求")
		}
		model := strings.TrimSpace(target.UpstreamModel)
		if model == "" || s.pricingStore == nil {
			continue
		}
		if _, seen := seenModels[model]; seen {
			continue
		}
		seenModels[model] = struct{}{}
		pricing, err := s.pricingStore.GetTier(ctx, provider, model, "")
		if err == nil && pricing == nil {
			warnings = append(warnings, fmt.Sprintf("上游模型 %s 在 %s 定价表中没有定价行，成本将按兜底价计算", model, provider))
		}
	}
	return warnings
}

func normalizeRouteProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "claude":
		return "claude", nil
	case "codex":
		return "codex", nil
	default:
		return "", fmt.Errorf("不支持的 provider: %q", provider)
	}
}
