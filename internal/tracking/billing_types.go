package tracking

import (
	_ "modernc.org/sqlite"
)

// ModelPricing 模型定价配置
type ModelPricing struct {
	Input                      float64 // per 1M tokens
	Output                     float64 // per 1M tokens
	CacheCreation              float64 // per 1M tokens (5分钟缓存创建，1.25x input)
	CacheCreation1h            float64 // per 1M tokens (1小时缓存创建，2x input)
	CacheRead                  float64 // per 1M tokens (缓存读取)
	LongContextThreshold       int64   // 原始输入 tokens 达到该值时启用，0 表示禁用
	LongContextInputPrice      float64 // per 1M tokens
	LongContextOutputPrice     float64 // per 1M tokens
	LongContextCacheWritePrice float64 // per 1M tokens
	LongContextCacheReadPrice  float64 // per 1M tokens
}

type ModelMultiplier struct {
	Model                   string
	CostMultiplier          float64
	InputCostMultiplier     float64
	OutputCostMultiplier    float64
	CacheReadCostMultiplier float64
}

const (
	ServiceTierStandard = "standard"
	ServiceTierBatch    = "batch"
	ServiceTierFlex     = "flex"
	ServiceTierFast     = "fast"
)

const (
	BillingRuleModeMultiplier = "multiplier"
	BillingRuleModeFixed      = "fixed"
	BillingContextNormal      = "normal"
	BillingContextLong        = "long"
)

// BillingRule is a Codex credential-group billing policy. Empty selectors
// match any value; validation guarantees one catch-all rule when rules exist.
type BillingRule struct {
	Model                       string
	ServiceTier                 string
	Context                     string
	Mode                        string
	CostMultiplier              float64
	InputCostMultiplier         float64
	OutputCostMultiplier        float64
	CacheCreationCostMultiplier float64
	CacheReadCostMultiplier     float64
	InputPrice                  float64
	OutputPrice                 float64
	CacheCreationPrice          float64
	CacheReadPrice              float64
}

// EndpointMultiplier 端点成本倍率（支持端点级别的成本调整）
// 成本计算公式：模型基础定价 * 细分倍率 * 总倍率
// 倍率叠加模式（不再是 either-or）：
//  1. 细分倍率：在每个分项成本计算时相乘（默认 1.0 = 不影响）
//  2. 总倍率：所有分项成本计算完成后，作为整体乘数应用（默认 1.0 = 不影响）
type EndpointMultiplier struct {
	CostMultiplier                         float64 // 总体倍率（默认 1.0，叠加在所有分项后）
	InputCostMultiplier                    float64 // 输入成本倍率（默认 1.0）
	OutputCostMultiplier                   float64 // 输出成本倍率（默认 1.0）
	CacheCreationCostMultiplier            float64 // 5分钟缓存创建成本倍率（默认 1.0）
	CacheCreationCostMultiplier1h          float64 // 1小时缓存创建成本倍率（默认 1.0）
	CacheReadCostMultiplier                float64 // 缓存读取成本倍率（默认 1.0）
	LongContextCostMultiplier              float64 // Long context 附加倍率（默认 1.0，仅命中 Long context 定价时应用）
	LongContextInputCostMultiplier         float64
	LongContextOutputCostMultiplier        float64
	LongContextCacheCreationCostMultiplier float64
	LongContextCacheReadCostMultiplier     float64
	DisableLongContextPricing              bool    // 禁用 Long context 定价，默认 false（跟随模型）
	BillingOutputTokenSource               string  // Codex 输出计费口径，默认 output_tokens
	ModelCostMultiplier                    float64 // 命中模型附加总倍率（默认 1.0）
	ModelInputCostMultiplier               float64 // 命中模型附加输入倍率（默认 1.0）
	ModelOutputCostMultiplier              float64 // 命中模型附加输出倍率（默认 1.0）
	ModelCacheReadCostMultiplier           float64 // 命中模型附加缓存读取倍率（默认 1.0）
	ModelMultipliers                       []ModelMultiplier
	BillingRules                           []BillingRule
}
