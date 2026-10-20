package app

import (
	"context"
	"fmt"
	"strings"

	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func (a *App) initDefaultModelPricing(ctx context.Context) {
	// Claude 官方定价 (2025年最新)
	// 5分钟缓存: input * 1.25, 1小时缓存: input * 2.0, 读取: input * 0.1
	defaultPricings := []*pricingstore.ModelPricingRecord{
		// 默认定价
		{
			ModelName:            "_default",
			DisplayName:          "默认定价",
			Description:          "未知模型使用的默认定价",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75, // 3.0 * 1.25
			CacheCreationPrice1h: 6.0,  // 3.0 * 2.0
			CacheReadPrice:       0.30, // 3.0 * 0.1
			IsDefault:            true,
		},
		// Claude Sonnet 4
		{
			ModelName:            "claude-sonnet-4-20250514",
			DisplayName:          "Claude Sonnet 4",
			Description:          "Claude Sonnet 4 (2025-05-14)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75,
			CacheCreationPrice1h: 6.0,
			CacheReadPrice:       0.30,
		},
		// Claude 3.5 Sonnet
		{
			ModelName:            "claude-3-5-sonnet-20241022",
			DisplayName:          "Claude 3.5 Sonnet",
			Description:          "Claude 3.5 Sonnet (2024-10-22)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75,
			CacheCreationPrice1h: 6.0,
			CacheReadPrice:       0.30,
		},
		// Claude 3.5 Haiku
		{
			ModelName:            "claude-3-5-haiku-20241022",
			DisplayName:          "Claude 3.5 Haiku",
			Description:          "Claude 3.5 Haiku (2024-10-22)",
			InputPrice:           0.80,
			OutputPrice:          4.0,
			CacheCreationPrice5m: 1.0,  // 0.80 * 1.25
			CacheCreationPrice1h: 1.6,  // 0.80 * 2.0
			CacheReadPrice:       0.08, // 0.80 * 0.1
		},
		// Claude Opus 4
		{
			ModelName:            "claude-opus-4-20250514",
			DisplayName:          "Claude Opus 4",
			Description:          "Claude Opus 4 (2025-05-14)",
			InputPrice:           15.0,
			OutputPrice:          75.0,
			CacheCreationPrice5m: 18.75, // 15.0 * 1.25
			CacheCreationPrice1h: 30.0,  // 15.0 * 2.0
			CacheReadPrice:       1.50,  // 15.0 * 0.1
		},
		// ========== Claude 4.5 系列 (2025年最新) ==========
		// Claude Sonnet 4.5
		{
			ModelName:            "claude-sonnet-4-5-20250929",
			DisplayName:          "Claude Sonnet 4.5",
			Description:          "Claude Sonnet 4.5 (2025-09-29)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75, // 3.0 * 1.25
			CacheCreationPrice1h: 6.0,  // 3.0 * 2.0
			CacheReadPrice:       0.30, // 3.0 * 0.1
		},
		// Claude Haiku 4.5
		{
			ModelName:            "claude-haiku-4-5-20251001",
			DisplayName:          "Claude Haiku 4.5",
			Description:          "Claude Haiku 4.5 (2025-10-01)",
			InputPrice:           1.0,
			OutputPrice:          5.0,
			CacheCreationPrice5m: 1.25, // 1.0 * 1.25
			CacheCreationPrice1h: 2.0,  // 1.0 * 2.0
			CacheReadPrice:       0.10, // 1.0 * 0.1
		},
		// Claude Opus 4.5
		{
			ModelName:            "claude-opus-4-5-20251101",
			DisplayName:          "Claude Opus 4.5",
			Description:          "Claude Opus 4.5 (2025-11-01)",
			InputPrice:           5.0,
			OutputPrice:          25.0,
			CacheCreationPrice5m: 6.25, // 5.0 * 1.25
			CacheCreationPrice1h: 10.0, // 5.0 * 2.0
			CacheReadPrice:       0.50, // 5.0 * 0.1
		},
		// ========== 旧版本兼容 ==========
		{
			ModelName:            "claude-3-opus-20240229",
			DisplayName:          "Claude 3 Opus",
			Description:          "Claude 3 Opus (2024-02-29)",
			InputPrice:           15.0,
			OutputPrice:          75.0,
			CacheCreationPrice5m: 18.75,
			CacheCreationPrice1h: 30.0,
			CacheReadPrice:       1.50,
		},
		{
			ModelName:            "claude-3-sonnet-20240229",
			DisplayName:          "Claude 3 Sonnet",
			Description:          "Claude 3 Sonnet (2024-02-29)",
			InputPrice:           3.0,
			OutputPrice:          15.0,
			CacheCreationPrice5m: 3.75,
			CacheCreationPrice1h: 6.0,
			CacheReadPrice:       0.30,
		},
		{
			ModelName:            "claude-3-haiku-20240307",
			DisplayName:          "Claude 3 Haiku",
			Description:          "Claude 3 Haiku (2024-03-07)",
			InputPrice:           0.25,
			OutputPrice:          1.25,
			CacheCreationPrice5m: 0.31,  // 0.25 * 1.25
			CacheCreationPrice1h: 0.50,  // 0.25 * 2.0
			CacheReadPrice:       0.025, // 0.25 * 0.1
		},
		// ========== Codex 模型定价（OpenAI 格式，无 cache 概念）==========
		{
			ModelName:   "o3",
			DisplayName: "O3",
			Provider:    "codex",
			InputPrice:  2.0,
			OutputPrice: 8.0,
			Description: "O3 标准模型",
		},
		{
			ModelName:   "o4-mini",
			DisplayName: "O4 Mini",
			Provider:    "codex",
			InputPrice:  1.10,
			OutputPrice: 4.40,
			Description: "O4 Mini 轻量模型",
		},
		{
			ModelName:   "codex-mini-latest",
			DisplayName: "Codex Mini Latest",
			Provider:    "codex",
			InputPrice:  1.50,
			OutputPrice: 6.0,
			Description: "Codex Mini 最新版本",
		},
		{
			ModelName:   "_codex_default",
			DisplayName: "Codex 默认定价",
			Provider:    "codex",
			InputPrice:  2.0,
			OutputPrice: 8.0,
			IsDefault:   true,
			Description: "Codex 默认定价方案",
		},
	}

	if err := a.modelPricingStore.BatchUpsert(ctx, defaultPricings); err != nil {
		a.logger.Error("❌ 初始化默认模型定价失败", "error", err)
		return
	}

	a.logger.Info("✅ 已初始化默认模型定价", "count", len(defaultPricings))
}

func (a *App) syncPricingToTracker(ctx context.Context) {
	if a.usageTracker == nil || a.modelPricingService == nil {
		return
	}

	records, err := a.modelPricingService.ListPricings(ctx)
	if err != nil {
		a.logger.Warn("⚠️ 获取模型定价列表失败", "error", err)
		return
	}

	// 转换为 tracking.ModelPricing 格式
	// key 必须带 provider 与档位：ListPricings 返回两个 provider 的全部记录，
	// 若仅以模型名为 key，同名模型会静默互相覆盖（详见 tracking.ModelPricingKey）。
	pricing := make(map[string]tracking.ModelPricing)
	for _, r := range records {
		pricing[tracking.ModelPricingKey(r.Provider, r.ModelName, r.ServiceTier)] = a.modelPricingService.ToTrackingPricing(r)
	}

	// 更新 UsageTracker 的定价缓存
	a.usageTracker.UpdatePricing(pricing)
	a.logger.Debug("已同步模型定价到 UsageTracker", "count", len(pricing))
}

func (a *App) syncEndpointMultipliersToTracker(ctx context.Context) {
	if a.usageTracker == nil {
		return
	}

	multipliers := make(map[string]tracking.EndpointMultiplier)

	// 1. Claude 端点倍率
	if a.endpointStore != nil {
		endpoints, err := a.endpointStore.List(ctx)
		if err != nil {
			a.logger.Warn("⚠️ 获取端点列表失败", "error", err)
		} else {
			legacyKeyCounts := make(map[string]int, len(endpoints))
			for _, ep := range endpoints {
				if ep == nil {
					continue
				}
				legacyKeyCounts[tracking.EndpointMultiplierKey("claude", ep.Channel, ep.Name)]++
			}
			claudeGroupsByID := make(map[int64]*claudestore.ClaudeCredentialGroupRecord)
			claudeDefaultGroupByChannel := make(map[string]*claudestore.ClaudeCredentialGroupRecord)
			if a.claudeCredentialGroupStore != nil {
				for _, ep := range endpoints {
					if ep == nil {
						continue
					}
					if ep.CredentialGroupID > 0 {
						if _, ok := claudeGroupsByID[ep.CredentialGroupID]; !ok {
							if group, err := a.claudeCredentialGroupStore.GetByID(ctx, ep.CredentialGroupID); err == nil && group != nil {
								claudeGroupsByID[ep.CredentialGroupID] = group
							}
						}
					}
					if ep.Channel != "" {
						if _, ok := claudeDefaultGroupByChannel[ep.Channel]; !ok {
							if group, err := a.claudeCredentialGroupStore.GetDefaultByChannel(ctx, ep.Channel); err == nil && group != nil {
								claudeDefaultGroupByChannel[ep.Channel] = group
							}
						}
					}
				}
			}
			for _, ep := range endpoints {
				if ep == nil {
					continue
				}
				group := claudeGroupsByID[ep.CredentialGroupID]
				if group == nil {
					group = claudeDefaultGroupByChannel[ep.Channel]
				}
				groupName := ep.CredentialGroupName
				groupCostMultiplier := 1.0
				groupInputMultiplier := 1.0
				groupOutputMultiplier := 1.0
				groupCacheCreationMultiplier := 1.0
				groupCacheCreation1hMultiplier := 1.0
				groupCacheReadMultiplier := 1.0
				groupLongContextMultiplier := 1.0
				groupLongContextInputMultiplier := 1.0
				groupLongContextOutputMultiplier := 1.0
				groupLongContextCacheCreationMultiplier := 1.0
				groupLongContextCacheReadMultiplier := 1.0
				groupDisableLongContextPricing := false
				groupModelMultipliers := []tracking.ModelMultiplier(nil)
				if group != nil {
					groupName = group.Name
					groupCostMultiplier = defaultPositiveMultiplier(group.CostMultiplier)
					groupInputMultiplier = defaultPositiveMultiplier(group.InputCostMultiplier)
					groupOutputMultiplier = defaultPositiveMultiplier(group.OutputCostMultiplier)
					groupCacheCreationMultiplier = defaultPositiveMultiplier(group.CacheCreationCostMultiplier)
					groupCacheCreation1hMultiplier = defaultPositiveMultiplier(group.CacheCreationCostMultiplier1h)
					groupCacheReadMultiplier = defaultPositiveMultiplier(group.CacheReadCostMultiplier)
					groupLongContextMultiplier = defaultPositiveMultiplier(group.LongContextCostMultiplier)
					groupLongContextInputMultiplier = defaultPositiveMultiplier(group.LongContextInputCostMultiplier)
					groupLongContextOutputMultiplier = defaultPositiveMultiplier(group.LongContextOutputCostMultiplier)
					groupLongContextCacheCreationMultiplier = defaultPositiveMultiplier(group.LongContextCacheCreationCostMultiplier)
					groupLongContextCacheReadMultiplier = defaultPositiveMultiplier(group.LongContextCacheReadCostMultiplier)
					groupDisableLongContextPricing = group.DisableLongContextPricing
					groupModelMultipliers = endpointModelMultipliersToTracking(group.ModelMultipliers)
				}
				if groupName == "" && ep.CredentialGroupID > 0 {
					groupName = fmt.Sprintf("g%d", ep.CredentialGroupID)
				}
				// Claude 的 API Key 与计费配置归属凭证分组；端点只表示线路配置。
				// 历史端点倍率字段保留为兼容存储，但不再参与运行时计费（与 Codex 一致）。
				multiplier := tracking.EndpointMultiplier{
					CostMultiplier:                         groupCostMultiplier,
					InputCostMultiplier:                    groupInputMultiplier,
					OutputCostMultiplier:                   groupOutputMultiplier,
					CacheCreationCostMultiplier:            groupCacheCreationMultiplier,
					CacheCreationCostMultiplier1h:          groupCacheCreation1hMultiplier,
					CacheReadCostMultiplier:                groupCacheReadMultiplier,
					LongContextCostMultiplier:              groupLongContextMultiplier,
					LongContextInputCostMultiplier:         groupLongContextInputMultiplier,
					LongContextOutputCostMultiplier:        groupLongContextOutputMultiplier,
					LongContextCacheCreationCostMultiplier: groupLongContextCacheCreationMultiplier,
					LongContextCacheReadCostMultiplier:     groupLongContextCacheReadMultiplier,
					DisableLongContextPricing:              groupDisableLongContextPricing,
					ModelMultipliers:                       mergeTrackingModelMultipliers(endpointModelMultipliersToTracking(ep.ModelMultipliers), groupModelMultipliers),
				}
				keys := []string{
					tracking.EndpointMultiplierGroupKey("claude", ep.Channel, groupName, ep.Name),
					tracking.ScopeEndpointMultiplierKey("claude", claudeservice.EndpointRuntimeKey(ep)),
				}
				legacyKey := tracking.EndpointMultiplierKey("claude", ep.Channel, ep.Name)
				if legacyKeyCounts[legacyKey] <= 1 {
					keys = append(keys, legacyKey)
				}
				seenKeys := make(map[string]struct{}, len(keys))
				for _, key := range keys {
					if key == "" {
						continue
					}
					if _, ok := seenKeys[key]; ok {
						continue
					}
					seenKeys[key] = struct{}{}
					multipliers[key] = multiplier
				}
			}
		}
	}

	// 2. Codex 端点倍率（每个端点的 key = channel.name::credential_group.name::endpoint.name）
	if a.codexChannelStore != nil && a.codexEndpointStore != nil {
		channels, err := a.codexChannelStore.List(ctx)
		if err != nil {
			a.logger.Warn("⚠️ 获取 Codex 渠道列表失败", "error", err)
		} else {
			for _, ch := range channels {
				var defaultGroup *codexstore.CodexCredentialGroupRecord
				if a.codexCredentialGroupStore != nil {
					defaultGroup, _ = a.codexCredentialGroupStore.GetDefaultByChannelID(ctx, ch.ID)
				}
				codexEndpoints, err := a.codexEndpointStore.ListByChannelID(ctx, ch.ID)
				if err != nil {
					a.logger.Warn("⚠️ 获取 Codex 端点列表失败", "channel", ch.Name, "error", err)
					continue
				}
				legacyKeyCounts := make(map[string]int, len(codexEndpoints))
				for _, ep := range codexEndpoints {
					if ep == nil {
						continue
					}
					legacyKeyCounts[tracking.EndpointMultiplierKey("codex", ch.Name, ep.Name)]++
				}
				for _, ep := range codexEndpoints {
					if ep == nil {
						continue
					}
					group := defaultGroup
					if a.codexCredentialGroupStore != nil && ep.CredentialGroupID > 0 {
						if resolved, err := a.codexCredentialGroupStore.GetByID(ctx, ep.CredentialGroupID); err == nil && resolved != nil {
							group = resolved
						}
					}
					groupName := ""
					groupCostMultiplier := 1.0
					groupInputMultiplier := 1.0
					groupOutputMultiplier := 1.0
					groupCacheCreationMultiplier := 1.0
					groupCacheReadMultiplier := 1.0
					groupLongContextMultiplier := 1.0
					groupLongContextInputMultiplier := 1.0
					groupLongContextOutputMultiplier := 1.0
					groupLongContextCacheCreationMultiplier := 1.0
					groupLongContextCacheReadMultiplier := 1.0
					groupDisableLongContextPricing := false
					groupModelMultipliers := []tracking.ModelMultiplier(nil)
					groupBillingRules := []tracking.BillingRule(nil)
					billingOutputTokenSource := "output_tokens"
					if group != nil {
						groupName = group.Name
						groupCostMultiplier = defaultPositiveMultiplier(group.CostMultiplier)
						groupInputMultiplier = defaultPositiveMultiplier(group.InputCostMultiplier)
						groupOutputMultiplier = defaultPositiveMultiplier(group.OutputCostMultiplier)
						groupCacheCreationMultiplier = defaultPositiveMultiplier(group.CacheCreationCostMultiplier)
						groupCacheReadMultiplier = defaultPositiveMultiplier(group.CacheReadCostMultiplier)
						groupLongContextMultiplier = defaultPositiveMultiplier(group.LongContextCostMultiplier)
						groupLongContextInputMultiplier = defaultPositiveMultiplier(group.LongContextInputCostMultiplier)
						groupLongContextOutputMultiplier = defaultPositiveMultiplier(group.LongContextOutputCostMultiplier)
						groupLongContextCacheCreationMultiplier = defaultPositiveMultiplier(group.LongContextCacheCreationCostMultiplier)
						groupLongContextCacheReadMultiplier = defaultPositiveMultiplier(group.LongContextCacheReadCostMultiplier)
						groupDisableLongContextPricing = group.DisableLongContextPricing
						groupModelMultipliers = codexModelMultipliersToTracking(group.ModelMultipliers)
						groupBillingRules = codexBillingRulesToTracking(group.BillingRules)
						billingOutputTokenSource = tracking.NormalizeBillingOutputTokenSource(group.BillingOutputTokenSource)
					}
					if groupName == "" && ep.CredentialGroupID > 0 {
						groupName = fmt.Sprintf("g%d", ep.CredentialGroupID)
					}
					// Codex 的 API Key 与计费配置归属凭证分组；端点只表示 base_url 线路配置。
					// 历史端点倍率字段保留为兼容存储，但不再参与运行时计费。
					multiplier := tracking.EndpointMultiplier{
						CostMultiplier:                         groupCostMultiplier,
						InputCostMultiplier:                    groupInputMultiplier,
						OutputCostMultiplier:                   groupOutputMultiplier,
						CacheCreationCostMultiplier:            groupCacheCreationMultiplier,
						CacheCreationCostMultiplier1h:          1.0,
						CacheReadCostMultiplier:                groupCacheReadMultiplier,
						LongContextCostMultiplier:              groupLongContextMultiplier,
						LongContextInputCostMultiplier:         groupLongContextInputMultiplier,
						LongContextOutputCostMultiplier:        groupLongContextOutputMultiplier,
						LongContextCacheCreationCostMultiplier: groupLongContextCacheCreationMultiplier,
						LongContextCacheReadCostMultiplier:     groupLongContextCacheReadMultiplier,
						DisableLongContextPricing:              groupDisableLongContextPricing,
						BillingOutputTokenSource:               billingOutputTokenSource,
						ModelMultipliers:                       mergeTrackingModelMultipliers(codexModelMultipliersToTracking(ep.ModelMultipliers), groupModelMultipliers),
						BillingRules:                           groupBillingRules,
					}
					legacyKey := tracking.EndpointMultiplierKey("codex", ch.Name, ep.Name)
					groupKey := tracking.EndpointMultiplierGroupKey("codex", ch.Name, groupName, ep.Name)
					keys := make([]string, 0, 2)
					if groupKey != "" && (groupKey != legacyKey || legacyKeyCounts[legacyKey] <= 1) {
						keys = append(keys, groupKey)
					}
					if legacyKeyCounts[legacyKey] <= 1 {
						keys = append(keys, legacyKey)
					}
					seenKeys := make(map[string]struct{}, len(keys))
					for _, key := range keys {
						if key == "" {
							continue
						}
						if _, ok := seenKeys[key]; ok {
							continue
						}
						seenKeys[key] = struct{}{}
						multipliers[key] = multiplier
					}
				}
			}
		}
	}

	// 更新 UsageTracker 的端点倍率缓存
	a.usageTracker.UpdateEndpointMultipliers(multipliers)
	a.logger.Debug("已同步端点倍率到 UsageTracker", "count", len(multipliers))
}

func endpointModelMultipliersToTracking(rules []claudestore.EndpointModelMultiplierRecord) []tracking.ModelMultiplier {
	if len(rules) == 0 {
		return nil
	}
	result := make([]tracking.ModelMultiplier, 0, len(rules))
	for _, rule := range rules {
		result = append(result, tracking.ModelMultiplier{
			Model:                   rule.Model,
			CostMultiplier:          rule.CostMultiplier,
			InputCostMultiplier:     rule.InputCostMultiplier,
			OutputCostMultiplier:    rule.OutputCostMultiplier,
			CacheReadCostMultiplier: rule.CacheReadCostMultiplier,
		})
	}
	return result
}

func codexModelMultipliersToTracking(rules []codexstore.CodexModelMultiplierRecord) []tracking.ModelMultiplier {
	if len(rules) == 0 {
		return nil
	}
	result := make([]tracking.ModelMultiplier, 0, len(rules))
	for _, rule := range rules {
		result = append(result, tracking.ModelMultiplier{
			Model:                   rule.Model,
			CostMultiplier:          rule.CostMultiplier,
			InputCostMultiplier:     rule.InputCostMultiplier,
			OutputCostMultiplier:    rule.OutputCostMultiplier,
			CacheReadCostMultiplier: rule.CacheReadCostMultiplier,
		})
	}
	return result
}

func codexBillingRulesToTracking(records []codexstore.CodexBillingRuleRecord) []tracking.BillingRule {
	if len(records) == 0 {
		return nil
	}
	result := make([]tracking.BillingRule, 0, len(records))
	for _, record := range records {
		result = append(result, tracking.BillingRule{
			Model:                       record.Model,
			ServiceTier:                 record.ServiceTier,
			Context:                     record.Context,
			Mode:                        record.Mode,
			CostMultiplier:              record.CostMultiplier,
			InputCostMultiplier:         record.InputCostMultiplier,
			OutputCostMultiplier:        record.OutputCostMultiplier,
			CacheCreationCostMultiplier: record.CacheCreationCostMultiplier,
			CacheReadCostMultiplier:     record.CacheReadCostMultiplier,
			InputPrice:                  record.InputPrice,
			OutputPrice:                 record.OutputPrice,
			CacheCreationPrice:          record.CacheCreationPrice,
			CacheReadPrice:              record.CacheReadPrice,
		})
	}
	return result
}

func mergeTrackingModelMultipliers(primary, fallback []tracking.ModelMultiplier) []tracking.ModelMultiplier {
	if len(primary) == 0 {
		return fallback
	}
	if len(fallback) == 0 {
		return primary
	}

	result := make([]tracking.ModelMultiplier, 0, len(primary)+len(fallback))
	seen := make(map[string]struct{}, len(primary)+len(fallback))
	appendRule := func(rule tracking.ModelMultiplier) {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		rule.Model = model
		result = append(result, rule)
	}
	for _, rule := range primary {
		appendRule(rule)
	}
	for _, rule := range fallback {
		appendRule(rule)
	}
	return result
}

func defaultPositiveMultiplier(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}
