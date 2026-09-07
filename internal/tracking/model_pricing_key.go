package tracking

import "strings"

const modelPricingKeySeparator = "::"

// normalizeServiceTierForPricing 把请求档位映射到定价行的 service_tier 取值。
//
// 定价表里 service_tier 为空串的行既是标准档定价，也是其余档位的兜底，因此标准档
// （含 default / standard_only）与 auto / 未知档一律落到空串：auto 意味着上游
// 实际选用的档位无法从请求判定，套标准价是保守且可解释的做法。
func normalizeServiceTierForPricing(value string) string {
	tier := NormalizeServiceTierForBilling(value)
	if tier == ServiceTierStandard {
		return ""
	}
	return tier
}

// ModelPricingKey 构造定价缓存 key，形如 "codex::gpt-5.6-sol::flex"。
//
// 与倍率 key 同理：Claude 与 Codex 共用一张定价 map，而两侧的模型名空间天然会重叠
// （同一中转商常同时提供两种协议；Claude provider 下已存在 MiniMax-M2 这类非 Anthropic
// 模型）。key 不含 provider 时，后写入的一侧会静默覆盖另一侧——不报错、不告警，只是
// 账单悄悄算错。档位维度则让同一模型可以配置多套单价。
func ModelPricingKey(provider, modelName, serviceTier string) string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return ""
	}
	return defaultProvider(strings.TrimSpace(provider)) +
		modelPricingKeySeparator + modelName +
		modelPricingKeySeparator + normalizeServiceTierForPricing(serviceTier)
}

// modelPricingLookupKeys 返回由精确到宽泛的定价查找顺序：
//
//	provider::model::tier   命中该档位的专属定价
//	provider::model::       回退到标准档/兜底行
//	model                   legacy，兼容直接以模型名构造 map 的调用点
func modelPricingLookupKeys(provider, modelName, serviceTier string) []string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}
	scoped := ModelPricingKey(provider, modelName, serviceTier)
	fallback := ModelPricingKey(provider, modelName, "")

	keys := make([]string, 0, 3)
	seen := map[string]struct{}{}
	for _, key := range []string{scoped, fallback, modelName} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

// defaultModelPricingKey 返回 provider 对应的兜底定价 key（模型完全未配置时使用）。
//
// 兜底定价本身也是 model_pricing 里的普通记录（"_default" 归 claude，"_codex_default"
// 归 codex，见 app.go 的 defaultPricings），因此同样经 ModelPricingKey 加前缀入缓存，
// 这里必须用相同方式构造，否则查找会落空。
func defaultModelPricingKey(provider string) string {
	if defaultProvider(strings.TrimSpace(provider)) == "codex" {
		return ModelPricingKey("codex", "_codex_default", "")
	}
	return ModelPricingKey("claude", "_default", "")
}
