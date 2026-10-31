package tracking

import (
	"strings"

	_ "modernc.org/sqlite"
)

// DefaultEndpointMultiplier 返回默认端点倍率（全部为 1.0）
func DefaultEndpointMultiplier() EndpointMultiplier {
	return EndpointMultiplier{
		CostMultiplier:                         1.0,
		InputCostMultiplier:                    1.0,
		OutputCostMultiplier:                   1.0,
		CacheCreationCostMultiplier:            1.0,
		CacheCreationCostMultiplier1h:          1.0,
		CacheReadCostMultiplier:                1.0,
		LongContextCostMultiplier:              1.0,
		LongContextInputCostMultiplier:         1.0,
		LongContextOutputCostMultiplier:        1.0,
		LongContextCacheCreationCostMultiplier: 1.0,
		LongContextCacheReadCostMultiplier:     1.0,
		BillingOutputTokenSource:               BillingOutputTokenSourceOutput,
		ModelCostMultiplier:                    1.0,
		ModelInputCostMultiplier:               1.0,
		ModelOutputCostMultiplier:              1.0,
		ModelCacheReadCostMultiplier:           1.0,
	}
}

// NormalizeServiceTierForBilling maps request/response aliases to the four
// configurable billing tiers. "auto" intentionally stays unresolved because
// the request alone does not identify the tier selected by the upstream.
func NormalizeServiceTierForBilling(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default", "standard", "standard_only":
		return ServiceTierStandard
	case "batch":
		return ServiceTierBatch
	case "flex":
		return ServiceTierFlex
	case "fast", "priority":
		return ServiceTierFast
	case "ultrafast":
		return ServiceTierUltrafast
	default:
		return ""
	}
}

func ResolveEndpointMultiplierForModel(mult EndpointMultiplier, modelName string) EndpointMultiplier {
	mult.ModelCostMultiplier = 1.0
	mult.ModelInputCostMultiplier = 1.0
	mult.ModelOutputCostMultiplier = 1.0
	mult.ModelCacheReadCostMultiplier = 1.0

	modelName = strings.TrimSpace(modelName)
	if modelName == "" || len(mult.ModelMultipliers) == 0 {
		mult.ModelMultipliers = nil
		return mult
	}

	for _, rule := range mult.ModelMultipliers {
		if strings.TrimSpace(rule.Model) != modelName {
			continue
		}
		mult.ModelCostMultiplier = ensurePositiveMultiplier(rule.CostMultiplier)
		mult.ModelInputCostMultiplier = ensurePositiveMultiplier(rule.InputCostMultiplier)
		mult.ModelOutputCostMultiplier = ensurePositiveMultiplier(rule.OutputCostMultiplier)
		mult.ModelCacheReadCostMultiplier = ensurePositiveMultiplier(rule.CacheReadCostMultiplier)
		break
	}
	mult.ModelMultipliers = nil
	return mult
}

func resolveBillingRule(rules []BillingRule, modelName, serviceTier, contextMode string) *BillingRule {
	modelName = strings.TrimSpace(modelName)
	serviceTier = NormalizeServiceTierForBilling(serviceTier)
	bestSelectorCount := -1
	bestSelectorPriority := -1
	var selected *BillingRule
	for i := range rules {
		rule := &rules[i]
		if rule.Model != "" && rule.Model != modelName {
			continue
		}
		if rule.ServiceTier != "" && rule.ServiceTier != serviceTier {
			continue
		}
		if rule.Context != "" && rule.Context != contextMode {
			continue
		}
		selectorCount := 0
		selectorPriority := 0
		if rule.Model != "" {
			selectorCount++
			selectorPriority += 4
		}
		if rule.ServiceTier != "" {
			selectorCount++
			selectorPriority += 2
		}
		if rule.Context != "" {
			selectorCount++
			selectorPriority++
		}
		if selectorCount > bestSelectorCount || (selectorCount == bestSelectorCount && selectorPriority > bestSelectorPriority) {
			selected = rule
			bestSelectorCount = selectorCount
			bestSelectorPriority = selectorPriority
		}
	}
	return selected
}

// copyBillingGroupMultipliers copies only the base factors used by explicit
// inheritance, without importing legacy model or long-context modifiers.
func copyBillingGroupMultipliers(dst *EndpointMultiplier, group EndpointMultiplier) {
	dst.DisableLongContextPricing = group.DisableLongContextPricing
	dst.CostMultiplier = ensurePositiveMultiplier(group.CostMultiplier)
	dst.InputCostMultiplier = ensurePositiveMultiplier(group.InputCostMultiplier)
	dst.OutputCostMultiplier = ensurePositiveMultiplier(group.OutputCostMultiplier)
	dst.CacheCreationCostMultiplier = ensurePositiveMultiplier(group.CacheCreationCostMultiplier)
	dst.CacheCreationCostMultiplier1h = dst.CacheCreationCostMultiplier
	dst.CacheReadCostMultiplier = ensurePositiveMultiplier(group.CacheReadCostMultiplier)
}

// DefaultBillingRuleGroupMultiplierMode makes every model/tier/context rule
// inherit group factors, including records written by older clients. Only the
// selector-free legacy fallback can contain the group rate already.
func DefaultBillingRuleGroupMultiplierMode(mode string, catchAll bool) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" && mode != BillingGroupMultiplierInherit && mode != BillingGroupMultiplierOverride {
		return mode
	}
	if catchAll && mode != BillingGroupMultiplierInherit {
		return BillingGroupMultiplierOverride
	}
	return BillingGroupMultiplierInherit
}

// resolveBillingForRequest 应用凭证分组的组合计费规则。规则列表为空时走 legacy
// 链路（模型倍率 + 分组/端点倍率）；非空时只选中一条规则。例外规则始终叠加分组
// 基础倍率；旧兜底保留已包含基础倍率的算法。legacy 模型和长上下文倍率不再叠加。
func resolveBillingForRequest(pricing ModelPricing, mult EndpointMultiplier, usage *TokenUsage, modelName, serviceTier string) (ModelPricing, EndpointMultiplier, *BillingRule) {
	if len(mult.BillingRules) == 0 {
		resolved := ResolveEndpointMultiplierForModel(mult, modelName)
		return pricing, resolved, nil
	}

	contextMode := BillingContextNormal
	if !mult.DisableLongContextPricing && usage != nil && pricing.LongContextThreshold > 0 && usage.InputTokens > pricing.LongContextThreshold {
		contextMode = BillingContextLong
	}
	rule := resolveBillingRule(mult.BillingRules, modelName, serviceTier, contextMode)
	effectiveMultiplier := DefaultEndpointMultiplier()
	effectiveMultiplier.DisableLongContextPricing = mult.DisableLongContextPricing
	effectiveMultiplier.ServiceTierSource = mult.ServiceTierSource
	effectiveMultiplier.CacheWriteTTLPricing = mult.CacheWriteTTLPricing
	effectiveMultiplier.BillingOutputTokenSource = NormalizeBillingOutputTokenSource(mult.BillingOutputTokenSource)
	if rule == nil {
		copyBillingGroupMultipliers(&effectiveMultiplier, mult)
		return pricing, effectiveMultiplier, nil
	}
	resolvedRule := *rule
	resolvedRule.GroupMultiplierMode = DefaultBillingRuleGroupMultiplierMode(rule.GroupMultiplierMode, rule.Model == "" && rule.ServiceTier == "" && rule.Context == "")
	rule = &resolvedRule
	if rule.GroupMultiplierMode == BillingGroupMultiplierInherit {
		copyBillingGroupMultipliers(&effectiveMultiplier, mult)
	}

	if rule.Mode == BillingRuleModeFixed {
		pricing.Input = rule.InputPrice
		pricing.Output = rule.OutputPrice
		pricing.CacheCreation = rule.CacheCreationPrice
		pricing.CacheCreation1h = rule.CacheCreationPrice
		pricing.CacheWritePrice30m = rule.CacheCreationPrice
		pricing.CacheWritePrice12h = rule.CacheCreationPrice
		pricing.LongContextCacheWritePrice12h = 0
		pricing.CacheRead = rule.CacheReadPrice
		pricing.LongContextThreshold = 0
		pricing.LongContextInputPrice = 0
		pricing.LongContextOutputPrice = 0
		pricing.LongContextCacheWritePrice = 0
		pricing.LongContextCacheReadPrice = 0
	} else {
		effectiveMultiplier.CostMultiplier *= ensurePositiveMultiplier(rule.CostMultiplier)
		effectiveMultiplier.InputCostMultiplier *= ensurePositiveMultiplier(rule.InputCostMultiplier)
		effectiveMultiplier.OutputCostMultiplier *= ensurePositiveMultiplier(rule.OutputCostMultiplier)
		effectiveMultiplier.CacheCreationCostMultiplier *= ensurePositiveMultiplier(rule.CacheCreationCostMultiplier)
		effectiveMultiplier.CacheCreationCostMultiplier1h *= ensurePositiveMultiplier(rule.CacheCreationCostMultiplier)
		effectiveMultiplier.CacheReadCostMultiplier *= ensurePositiveMultiplier(rule.CacheReadCostMultiplier)
	}

	return pricing, effectiveMultiplier, rule
}

func billingRuleKey(rule BillingRule) string {
	value := func(value string) string {
		if value == "" {
			return "*"
		}
		return value
	}
	return "model=" + value(rule.Model) + ";tier=" + value(rule.ServiceTier) + ";context=" + value(rule.Context)
}

// calculateRequestCost 计算成本并同时产出计费决策快照。快照无条件生成——
// 未配置组合规则的普通请求同样需要留下「命中哪行定价、走没走 long context、
// 单价与各层倍率是多少」的证据，否则详情页只能靠除法反推。
func calculateRequestCost(usage *TokenUsage, pricing ModelPricing, multiplier EndpointMultiplier, id billingIdentity) (CostBreakdown, EndpointMultiplier, *BillingDecisionSnapshot) {
	effectivePricing, effectiveMultiplier, rule := resolveBillingForRequest(pricing, multiplier, usage, id.requestModel, id.serviceTier)
	cost := CalculateCost(usage, &effectivePricing, &effectiveMultiplier)
	snapshot := buildBillingDecisionSnapshot(usage, effectivePricing, effectiveMultiplier, id, rule)
	if snapshot != nil && rule != nil {
		// Persist both the final factors and their sources. Request multiplier columns
		// keep the combined values for consumers that do not understand rule snapshots.
		snapshot.RuleGroupMultiplierMode = BillingGroupMultiplierOverride
		snapshot.GroupCostMultiplier = 1
		if rule.GroupMultiplierMode == BillingGroupMultiplierInherit {
			snapshot.RuleGroupMultiplierMode = BillingGroupMultiplierInherit
			snapshot.GroupCostMultiplier = ensurePositiveMultiplier(multiplier.CostMultiplier)
		}
		snapshot.RuleCostMultiplier = 1
		if rule.Mode == BillingRuleModeMultiplier {
			snapshot.RuleCostMultiplier = ensurePositiveMultiplier(rule.CostMultiplier)
		}
	}
	return cost, effectiveMultiplier, snapshot
}
