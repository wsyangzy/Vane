package tracking

import (
	"encoding/json"
	"strings"
)

// BillingDecisionSnapshot 是一次请求的完整计费决策快照：命中了哪一行定价、走的是
// 标准还是 long context 单价、四类 token 的实际生效单价、以及各层倍率。
//
// 它无条件生成并随 request_logs 落库（列名沿用 billing_rule_snapshot，免迁移），
// 因此后续修改定价或倍率配置都不会改写历史账单；前端也不必再用「成本 ÷ token ÷ 倍率」
// 反推单价——那种反推会把 long context 倍率之类未落库的层默默吸收进「基础单价」里。
//
// RuleKey / RuleMode 仅在命中凭证分组的组合计费规则时有值。
type BillingDecisionSnapshot struct {
	RequestedServiceTier string  `json:"requested_service_tier,omitempty"`
	ResponseServiceTier  string  `json:"response_service_tier,omitempty"`
	BillingServiceTier   string  `json:"billing_service_tier,omitempty"`
	ServiceTierSource    string  `json:"service_tier_source,omitempty"`
	ServiceTierPolicy    string  `json:"service_tier_policy,omitempty"`
	PricingFallback      string  `json:"pricing_fallback,omitempty"`
	CacheWritePricing    string  `json:"cache_write_pricing,omitempty"`
	CacheWrite30mTokens  int64   `json:"cache_write_30m_tokens,omitempty"`
	CacheWrite12hTokens  int64   `json:"cache_write_12h_tokens,omitempty"`
	CacheWrite30mPrice   float64 `json:"cache_write_30m_price,omitempty"`
	CacheWrite12hPrice   float64 `json:"cache_write_12h_price,omitempty"`
	CacheWrite30mCost    float64 `json:"cache_write_30m_cost,omitempty"`
	CacheWrite12hCost    float64 `json:"cache_write_12h_cost,omitempty"`
	// 命中的定价行标识。三者构成 model_pricing 的唯一键；PricingModel 可能是兜底行
	// （_default / _codex_default），此时如实记录兜底行名而非请求模型名。
	PricingProvider    string `json:"pricing_provider,omitempty"`
	PricingModel       string `json:"pricing_model,omitempty"`
	PricingServiceTier string `json:"pricing_service_tier"`

	// 上下文模式与判定依据。EffectiveInputTokens 是与阈值比较的原始输入 token
	// （未扣除缓存读写），与 CalculateCost 的判定口径一致。
	ContextMode          string `json:"context_mode"`
	LongContextThreshold int64  `json:"long_context_threshold,omitempty"`
	EffectiveInputTokens int64  `json:"effective_input_tokens"`

	// 实际生效单价（每百万 token）。long context 生效时即 long context 四价。
	InputPrice           float64 `json:"input_price"`
	OutputPrice          float64 `json:"output_price"`
	CacheCreationPrice   float64 `json:"cache_creation_price"`
	CacheCreation1hPrice float64 `json:"cache_creation_1h_price"`
	CacheReadPrice       float64 `json:"cache_read_price"`

	// 生效分项倍率：已合并分组/端点、模型、long context 三层。
	// tokens × 单价 ÷ 1e6 × 分项倍率 × TotalCostMultiplier 即为该分项成本。
	InputCostMultiplier           float64 `json:"input_cost_multiplier"`
	OutputCostMultiplier          float64 `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier   float64 `json:"cache_creation_cost_multiplier"`
	CacheCreation1hCostMultiplier float64 `json:"cache_creation_1h_cost_multiplier"`
	CacheReadCostMultiplier       float64 `json:"cache_read_cost_multiplier"`
	TotalCostMultiplier           float64 `json:"total_cost_multiplier"`

	// 总倍率的来源分层：TotalCostMultiplier = GroupCostMultiplier × ModelCostMultiplier × RuleCostMultiplier。
	// LongContextCostMultiplier 已折进分项倍率，仅在 long context 生效时记录来源。
	GroupCostMultiplier       float64 `json:"group_cost_multiplier"`
	ModelCostMultiplier       float64 `json:"model_cost_multiplier"`
	LongContextCostMultiplier float64 `json:"long_context_cost_multiplier,omitempty"`

	RuleKey                 string  `json:"rule_key,omitempty"`
	RuleMode                string  `json:"rule_mode,omitempty"`
	RuleGroupMultiplierMode string  `json:"rule_group_multiplier_mode,omitempty"`
	RuleCostMultiplier      float64 `json:"rule_cost_multiplier,omitempty"`
}

// billingIdentity 标识本次计费的请求归属与命中的定价行。
type billingIdentity struct {
	requestedTier string
	responseTier  string
	tierSource    string
	provider      string // 请求 provider（claude / codex）
	requestModel  string // 请求实际使用的模型，组合规则选择器与模型倍率按它匹配
	serviceTier   string // 请求档位（未归一化）
	pricingModel  string // 实际命中的定价行 model_name
	pricingTier   string // 实际命中的定价行 service_tier，空串 = 标准档/兜底行
}

// billingFactors 是一次成本计算实际选用的单价与合并后的倍率。
// CalculateCost 与计费快照共用它，避免 long context 判定出现两份会漂移的实现。
type billingFactors struct {
	inputPrice           float64
	outputPrice          float64
	cacheCreationPrice   float64
	cacheCreation1hPrice float64
	cacheReadPrice       float64

	inputMult           float64
	outputMult          float64
	cacheCreationMult   float64
	cacheCreation1hMult float64
	cacheReadMult       float64
	totalMult           float64

	longContextActive bool
}

// resolveBillingFactors 选出本次实际生效的单价，并把分组/端点、模型、long context
// 三层分项倍率合并成每类 token 一个乘数。
//
// long context 判定是严格 >：阈值必须为正、原始输入 token 严格大于阈值、且 long
// context 的输入/输出价都为正；缺失的 long context 缓存价回退到标准缓存价。
func resolveBillingFactors(usage *TokenUsage, pricing *ModelPricing, m *EndpointMultiplier) billingFactors {
	f := billingFactors{
		inputPrice:         pricing.Input,
		outputPrice:        pricing.Output,
		cacheCreationPrice: pricing.CacheCreation,
		cacheReadPrice:     pricing.CacheRead,
		longContextActive: !m.DisableLongContextPricing &&
			pricing.LongContextThreshold > 0 &&
			usage.InputTokens > pricing.LongContextThreshold &&
			pricing.LongContextInputPrice > 0 &&
			pricing.LongContextOutputPrice > 0,
	}
	if pricing.CodexCachePricing {
		f.cacheCreationPrice = pricing.CacheWritePrice30m
	}
	if f.longContextActive {
		f.inputPrice = pricing.LongContextInputPrice
		f.outputPrice = pricing.LongContextOutputPrice
		if pricing.LongContextCacheWritePrice > 0 {
			f.cacheCreationPrice = pricing.LongContextCacheWritePrice
		}
		if pricing.LongContextCacheReadPrice > 0 {
			f.cacheReadPrice = pricing.LongContextCacheReadPrice
		}
	}

	// 1h 缓存价未配置时按当前输入价的 2 倍兜底
	f.cacheCreation1hPrice = pricing.CacheCreation1h
	if f.longContextActive && pricing.LongContextCacheWritePrice > 0 {
		f.cacheCreation1hPrice = pricing.LongContextCacheWritePrice
	} else if f.cacheCreation1hPrice <= 0 {
		f.cacheCreation1hPrice = f.inputPrice * 2.0
	}

	f.inputMult = ensurePositiveMultiplier(m.InputCostMultiplier) * ensurePositiveMultiplier(m.ModelInputCostMultiplier)
	f.outputMult = ensurePositiveMultiplier(m.OutputCostMultiplier) * ensurePositiveMultiplier(m.ModelOutputCostMultiplier)
	f.cacheReadMult = ensurePositiveMultiplier(m.CacheReadCostMultiplier) * ensurePositiveMultiplier(m.ModelCacheReadCostMultiplier)
	f.cacheCreationMult = ensurePositiveMultiplier(m.CacheCreationCostMultiplier)
	f.cacheCreation1hMult = ensurePositiveMultiplier(m.CacheCreationCostMultiplier1h)
	if f.longContextActive {
		longMult := ensurePositiveMultiplier(m.LongContextCostMultiplier)
		f.inputMult *= ensurePositiveMultiplier(m.LongContextInputCostMultiplier) * longMult
		f.outputMult *= ensurePositiveMultiplier(m.LongContextOutputCostMultiplier) * longMult
		f.cacheReadMult *= ensurePositiveMultiplier(m.LongContextCacheReadCostMultiplier) * longMult
		f.cacheCreationMult *= ensurePositiveMultiplier(m.LongContextCacheCreationCostMultiplier) * longMult
		f.cacheCreation1hMult *= ensurePositiveMultiplier(m.LongContextCacheCreationCostMultiplier) * longMult
	}
	f.totalMult = ensurePositiveMultiplier(m.CostMultiplier) * ensurePositiveMultiplier(m.ModelCostMultiplier)
	return f
}

// buildBillingDecisionSnapshot 由「已应用组合规则后的」定价与倍率生成快照，
// 因此快照里的单价与上下文模式就是本次真正计费所用的值。
func buildBillingDecisionSnapshot(usage *TokenUsage, pricing ModelPricing, mult EndpointMultiplier, id billingIdentity, rule *BillingRule) *BillingDecisionSnapshot {
	if usage == nil {
		return nil
	}
	f := resolveBillingFactors(usage, &pricing, &mult)
	contextMode := BillingContextNormal
	if f.longContextActive {
		contextMode = BillingContextLong
	}

	snapshot := &BillingDecisionSnapshot{
		RequestedServiceTier: id.requestedTier,
		ResponseServiceTier:  id.responseTier,
		BillingServiceTier:   id.serviceTier,
		PricingProvider:      defaultProvider(strings.TrimSpace(id.provider)),
		PricingModel:         id.pricingModel,
		PricingServiceTier:   id.pricingTier,
		ContextMode:          contextMode,
		LongContextThreshold: pricing.LongContextThreshold,
		EffectiveInputTokens: usage.InputTokens,

		InputPrice:           f.inputPrice,
		OutputPrice:          f.outputPrice,
		CacheCreationPrice:   f.cacheCreationPrice,
		CacheCreation1hPrice: f.cacheCreation1hPrice,
		CacheReadPrice:       f.cacheReadPrice,

		InputCostMultiplier:           f.inputMult,
		OutputCostMultiplier:          f.outputMult,
		CacheCreationCostMultiplier:   f.cacheCreationMult,
		CacheCreation1hCostMultiplier: f.cacheCreation1hMult,
		CacheReadCostMultiplier:       f.cacheReadMult,
		TotalCostMultiplier:           f.totalMult,

		GroupCostMultiplier: ensurePositiveMultiplier(mult.CostMultiplier),
		ModelCostMultiplier: ensurePositiveMultiplier(mult.ModelCostMultiplier),
	}
	_, snapshot.ServiceTierSource = ResolveBillingServiceTier(id.requestedTier, id.responseTier, id.tierSource)
	if id.provider == "codex" {
		snapshot.ServiceTierPolicy = NormalizeServiceTierSource(id.tierSource)
	}
	if id.pricingModel != id.requestModel {
		snapshot.PricingFallback = "provider_default"
	} else if normalizeServiceTierForPricing(id.serviceTier) != id.pricingTier {
		snapshot.PricingFallback = "standard_price"
	} else if id.provider == "codex" && strings.TrimSpace(id.serviceTier) != "" && NormalizeServiceTierForBilling(id.serviceTier) == "" {
		snapshot.PricingFallback = "unresolved_tier"
	}
	if pricing.CodexCachePricing {
		allocation := resolveCacheWriteAllocation(usage, &pricing, &mult, f)
		snapshot.CacheWritePricing = allocation.reason
		snapshot.CacheWrite30mTokens, snapshot.CacheWrite12hTokens = allocation.tokens30m, allocation.tokens12h
		snapshot.CacheWrite30mPrice, snapshot.CacheWrite12hPrice = f.cacheCreationPrice, allocation.price12h
		snapshot.CacheWrite30mCost = float64(allocation.tokens30m) * f.cacheCreationPrice / 1e6 * f.cacheCreationMult * f.totalMult
		snapshot.CacheWrite12hCost = float64(allocation.tokens12h) * allocation.price12h / 1e6 * f.cacheCreationMult * f.totalMult
	}
	if f.longContextActive {
		snapshot.LongContextCostMultiplier = ensurePositiveMultiplier(mult.LongContextCostMultiplier)
	}
	if rule != nil {
		snapshot.RuleKey = billingRuleKey(*rule)
		snapshot.RuleMode = rule.Mode
	}
	return snapshot
}

func billingDecisionSnapshotJSON(snapshot *BillingDecisionSnapshot) string {
	if snapshot == nil {
		return ""
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(data)
}

// Tier evidence must survive even when the model has no configured price.
func unpricedBillingDecisionSnapshot(id billingIdentity) *BillingDecisionSnapshot {
	if id.provider != "codex" {
		return nil
	}
	_, source := ResolveBillingServiceTier(id.requestedTier, id.responseTier, id.tierSource)
	return &BillingDecisionSnapshot{
		RequestedServiceTier: id.requestedTier,
		ResponseServiceTier:  id.responseTier,
		BillingServiceTier:   id.serviceTier,
		ServiceTierSource:    source,
		ServiceTierPolicy:    NormalizeServiceTierSource(id.tierSource),
		PricingProvider:      id.provider,
		PricingFallback:      "missing_price",
	}
}

// lookupModelPricing 按 provider::model::tier → provider::model:: → legacy 裸模型名
// → provider 兜底行的顺序查找定价，并返回实际命中的定价行标识。
//
// 兜底价本身也是 model_pricing 的普通记录（_default 归 claude、_codex_default 归 codex），
// 所以命中兜底行时快照记录的是兜底行名，而不是请求模型名。
func lookupModelPricing(pricingMap map[string]ModelPricing, provider, modelName, serviceTier string) (ModelPricing, string, string, bool) {
	if pricingMap == nil {
		return ModelPricing{}, "", "", false
	}
	keys := modelPricingLookupKeys(provider, modelName, serviceTier)
	keys = append(keys, defaultModelPricingKey(provider))
	for _, key := range keys {
		if candidate, ok := pricingMap[key]; ok {
			pricingModel, pricingTier := splitModelPricingKey(key)
			return candidate, pricingModel, pricingTier, true
		}
	}
	return ModelPricing{}, "", "", false
}

// splitModelPricingKey 从 provider::model::tier 取出定价行标识。
// legacy 裸模型名 key 只有模型名一段，档位视为标准档。
func splitModelPricingKey(key string) (string, string) {
	parts := strings.Split(key, modelPricingKeySeparator)
	if len(parts) < 3 {
		return key, ""
	}
	// 模型名本身理论上可能含分隔符，取首段为 provider、末段为 tier，中间全部为模型名
	return strings.Join(parts[1:len(parts)-1], modelPricingKeySeparator), parts[len(parts)-1]
}
