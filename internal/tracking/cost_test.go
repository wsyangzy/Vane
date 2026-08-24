package tracking

import (
	"math"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCalculateCost_Basic 测试基础成本计算
func TestCalculateCost_Basic(t *testing.T) {
	pricing := &ModelPricing{
		Input:           3.0,  // $3/1M tokens
		Output:          15.0, // $15/1M tokens
		CacheCreation:   3.75, // $3.75/1M tokens (5m cache = 1.25x input)
		CacheCreation1h: 6.0,  // $6/1M tokens (1h cache = 2x input)
		CacheRead:       0.30, // $0.30/1M tokens
	}

	usage := &TokenUsage{
		InputTokens:  1000000,
		OutputTokens: 100000,
	}

	result := CalculateCost(usage, pricing, nil)

	// Input: 1M tokens * $3/1M = $3.00
	if math.Abs(result.InputCost-3.0) > 0.001 {
		t.Errorf("Expected InputCost $3.00, got $%f", result.InputCost)
	}

	// Output: 0.1M tokens * $15/1M = $1.50
	if math.Abs(result.OutputCost-1.5) > 0.001 {
		t.Errorf("Expected OutputCost $1.50, got $%f", result.OutputCost)
	}

	// Total: $3.00 + $1.50 = $4.50
	if math.Abs(result.TotalCost-4.5) > 0.001 {
		t.Errorf("Expected TotalCost $4.50, got $%f", result.TotalCost)
	}
}

func TestResolveCodexBillingRuleUsesSpecificityThenSelectorPriority(t *testing.T) {
	pricing := ModelPricing{
		Input:                  10,
		Output:                 20,
		LongContextThreshold:   100,
		LongContextInputPrice:  30,
		LongContextOutputPrice: 40,
	}
	multiplier := DefaultEndpointMultiplier()
	multiplier.CostMultiplier = 9
	multiplier.InputCostMultiplier = 8
	multiplier.DisableServiceTierPricing = false
	multiplier.ServiceTierMultipliers = map[string]ServiceTierMultiplier{
		ServiceTierFast: {CostMultiplier: 7, InputCostMultiplier: 6, OutputCostMultiplier: 5, CacheCreationMultiplier: 4, CacheReadMultiplier: 3},
	}
	multiplier.ModelMultipliers = []ModelMultiplier{{Model: "gpt-test", CostMultiplier: 10, InputCostMultiplier: 10, OutputCostMultiplier: 10, CacheReadCostMultiplier: 10}}
	multiplier.BillingRules = []BillingRule{
		{Mode: BillingRuleModeMultiplier, CostMultiplier: 1, InputCostMultiplier: 1, OutputCostMultiplier: 1, CacheCreationCostMultiplier: 1, CacheReadCostMultiplier: 1},
		{Model: "gpt-test", Mode: BillingRuleModeMultiplier, CostMultiplier: 2, InputCostMultiplier: 1, OutputCostMultiplier: 1, CacheCreationCostMultiplier: 1, CacheReadCostMultiplier: 1},
		{ServiceTier: ServiceTierFast, Mode: BillingRuleModeMultiplier, CostMultiplier: 3, InputCostMultiplier: 1, OutputCostMultiplier: 1, CacheCreationCostMultiplier: 1, CacheReadCostMultiplier: 1},
		{Model: "gpt-test", ServiceTier: ServiceTierFast, Context: BillingContextLong, Mode: BillingRuleModeFixed, InputPrice: 7, OutputPrice: 11, CacheCreationPrice: 2, CacheReadPrice: 0.5},
	}

	_, normalMultiplier, normalSnapshot := resolveBillingForRequest(pricing, multiplier, &TokenUsage{InputTokens: 100}, "gpt-test", "priority")
	if normalSnapshot == nil || normalSnapshot.SelectorModel != "gpt-test" || normalMultiplier.CostMultiplier != 2 {
		t.Fatalf("expected model selector to win equal-specificity tie, snapshot=%+v multiplier=%+v", normalSnapshot, normalMultiplier)
	}

	cost, _, snapshotJSON := calculateRequestCost(&TokenUsage{
		InputTokens:            3_000_000,
		OutputTokens:           1_000_000,
		CacheCreationTokens:    1_000_000,
		CacheReadTokens:        1_000_000,
		InputIncludesCacheRead: true,
	}, pricing, multiplier, "gpt-test", "priority")
	if math.Abs(cost.TotalCost-20.5) > 1e-9 {
		t.Fatalf("expected one fixed tier+context rule to replace all multipliers, got %f", cost.TotalCost)
	}
	if !strings.Contains(snapshotJSON, `"mode":"fixed"`) || !strings.Contains(snapshotJSON, `"selector_context":"long"`) {
		t.Fatalf("expected fixed long-context audit snapshot, got %s", snapshotJSON)
	}
}

func TestCalculateCost_InputIncludesCacheRead(t *testing.T) {
	pricing := &ModelPricing{
		Input:     5,
		Output:    30,
		CacheRead: 0.5,
	}
	multiplier := &EndpointMultiplier{
		CostMultiplier:          0.4,
		InputCostMultiplier:     1,
		OutputCostMultiplier:    1,
		CacheReadCostMultiplier: 1,
	}
	usage := &TokenUsage{
		InputTokens:            146783,
		OutputTokens:           544,
		CacheReadTokens:        145280,
		InputIncludesCacheRead: true,
	}

	result := CalculateCost(usage, pricing, multiplier)

	if math.Abs(result.TotalCost-0.038590) > 0.000001 {
		t.Fatalf("Expected TotalCost $0.038590, got $%.6f", result.TotalCost)
	}
}

func TestCalculateCost_InputIncludesCacheReadAndWrite(t *testing.T) {
	pricing := &ModelPricing{
		Input:         10,
		CacheCreation: 12.5,
		CacheRead:     1,
	}
	usage := &TokenUsage{
		InputTokens:            1_000_000,
		CacheCreationTokens:    300_000,
		CacheReadTokens:        200_000,
		InputIncludesCacheRead: true,
	}

	result := CalculateCost(usage, pricing, nil)

	if math.Abs(result.InputCost-5.0) > 0.000001 {
		t.Fatalf("expected ordinary input cost $5.000000 after cache read/write deduction, got $%.6f", result.InputCost)
	}
	if math.Abs(result.CacheCreationCost-3.75) > 0.000001 {
		t.Fatalf("expected cache write cost $3.750000, got $%.6f", result.CacheCreationCost)
	}
	if math.Abs(result.CacheReadCost-0.2) > 0.000001 {
		t.Fatalf("expected cache read cost $0.200000, got $%.6f", result.CacheReadCost)
	}
}

func TestCalculateCost_LongContextThreshold(t *testing.T) {
	var pricing ModelPricing
	if err := yaml.Unmarshal([]byte(`
input: 2
output: 4
cache_creation: 2.5
cache_read: 0.2
long_context_threshold: 1000000
long_context_input_price: 10
long_context_output_price: 20
long_context_cache_write_price: 12.5
long_context_cache_read_price: 1
`), &pricing); err != nil {
		t.Fatalf("decode long-context pricing: %v", err)
	}

	t.Run("below threshold uses standard prices", func(t *testing.T) {
		usage := &TokenUsage{
			InputTokens:            900_000,
			OutputTokens:           50_000,
			CacheCreationTokens:    200_000,
			CacheReadTokens:        100_000,
			InputIncludesCacheRead: true,
		}

		result := CalculateCost(usage, &pricing, nil)

		if math.Abs(result.InputCost-1.2) > 0.000001 ||
			math.Abs(result.OutputCost-0.2) > 0.000001 ||
			math.Abs(result.CacheCreationCost-0.5) > 0.000001 ||
			math.Abs(result.CacheReadCost-0.02) > 0.000001 {
			t.Fatalf("expected standard prices below threshold, got %+v", result)
		}
	})

	t.Run("threshold itself still uses standard prices", func(t *testing.T) {
		usage := &TokenUsage{
			InputTokens:  1_000_000,
			OutputTokens: 50_000,
		}

		result := CalculateCost(usage, &pricing, nil)

		if math.Abs(result.InputCost-2.0) > 0.000001 || math.Abs(result.OutputCost-0.2) > 0.000001 {
			t.Fatalf("expected standard prices at the strict threshold, got %+v", result)
		}
	})

	t.Run("above threshold uses long-context prices and multiplier", func(t *testing.T) {
		usage := &TokenUsage{
			InputTokens:            1_000_001,
			OutputTokens:           50_000,
			CacheCreationTokens:    200_000,
			CacheReadTokens:        100_000,
			InputIncludesCacheRead: true,
		}
		multiplier := &EndpointMultiplier{
			CostMultiplier:                         0.5,
			InputCostMultiplier:                    2,
			OutputCostMultiplier:                   3,
			CacheCreationCostMultiplier:            4,
			CacheReadCostMultiplier:                5,
			ModelCostMultiplier:                    1.5,
			ModelInputCostMultiplier:               1.25,
			ModelOutputCostMultiplier:              1.2,
			ModelCacheReadCostMultiplier:           1.1,
			LongContextCostMultiplier:              2,
			LongContextInputCostMultiplier:         1.1,
			LongContextOutputCostMultiplier:        1.2,
			LongContextCacheCreationCostMultiplier: 1.3,
			LongContextCacheReadCostMultiplier:     1.4,
		}

		result := CalculateCost(usage, &pricing, multiplier)

		if math.Abs(result.InputCost-28.87504125) > 0.000001 {
			t.Fatalf("expected long-context input cost $26.2500375, got $%.6f", result.InputCost)
		}
		if math.Abs(result.OutputCost-6.48) > 0.000001 {
			t.Fatalf("expected long-context output cost $5.400000, got $%.6f", result.OutputCost)
		}
		if math.Abs(result.CacheCreationCost-19.5) > 0.000001 {
			t.Fatalf("expected long-context cache write cost $15.000000, got $%.6f", result.CacheCreationCost)
		}
		if math.Abs(result.CacheReadCost-1.155) > 0.000001 {
			t.Fatalf("expected long-context cache read cost $0.825000, got $%.6f", result.CacheReadCost)
		}
		if math.Abs(result.TotalCost-56.01004125) > 0.000001 {
			t.Fatalf("expected long-context total cost $47.4750375, got $%.6f", result.TotalCost)
		}
	})

	t.Run("disabled long-context pricing keeps standard prices", func(t *testing.T) {
		usage := &TokenUsage{InputTokens: 1_000_001, OutputTokens: 50_000}
		multiplier := &EndpointMultiplier{
			CostMultiplier:            1,
			InputCostMultiplier:       1,
			OutputCostMultiplier:      1,
			LongContextCostMultiplier: 3,
			DisableLongContextPricing: true,
		}

		result := CalculateCost(usage, &pricing, multiplier)

		if math.Abs(result.InputCost-2.000002) > 0.000001 || math.Abs(result.OutputCost-0.2) > 0.000001 {
			t.Fatalf("expected standard prices when long-context pricing is disabled, got %+v", result)
		}
	})

	t.Run("incomplete long-context prices fall back to standard prices", func(t *testing.T) {
		incomplete := pricing
		incomplete.LongContextOutputPrice = 0
		usage := &TokenUsage{InputTokens: 1_000_001, OutputTokens: 50_000}
		multiplier := &EndpointMultiplier{LongContextCostMultiplier: 4}

		result := CalculateCost(usage, &incomplete, multiplier)

		if math.Abs(result.InputCost-2.000002) > 0.000001 || math.Abs(result.OutputCost-0.2) > 0.000001 {
			t.Fatalf("expected standard-price fallback for incomplete long-context config, got %+v", result)
		}
	})
}

func TestDisplayTotalTokens_CodexRawAndLegacyInput(t *testing.T) {
	rawTotal := DisplayTotalTokens("codex", 128742, 313, 0, 128384)
	if rawTotal != 129055 {
		t.Fatalf("Expected raw Codex total 129055, got %d", rawTotal)
	}

	legacyTotal := DisplayTotalTokens("codex", 358, 313, 0, 128384)
	if legacyTotal != 129055 {
		t.Fatalf("Expected legacy Codex total 129055, got %d", legacyTotal)
	}

	claudeTotal := DisplayTotalTokens("claude", 100, 20, 30, 40)
	if claudeTotal != 190 {
		t.Fatalf("Expected Claude total 190, got %d", claudeTotal)
	}
}

// TestCalculateCost_Separate5mAnd1hCache 测试分开的 5m/1h 缓存定价
func TestCalculateCost_Separate5mAnd1hCache(t *testing.T) {
	// Claude Opus 4.5 定价
	pricing := &ModelPricing{
		Input:           15.0,  // $15/1M tokens
		Output:          75.0,  // $75/1M tokens
		CacheCreation:   18.75, // $18.75/1M tokens (5m = 1.25x input)
		CacheCreation1h: 30.0,  // $30/1M tokens (1h = 2x input)
		CacheRead:       1.50,  // $1.50/1M tokens
	}

	usage := &TokenUsage{
		InputTokens:           10000,
		OutputTokens:          5000,
		CacheCreation5mTokens: 0,     // 无 5 分钟缓存
		CacheCreation1hTokens: 1000,  // 1000 个 1 小时缓存 tokens
		CacheCreationTokens:   1000,  // 总数（向后兼容）
		CacheReadTokens:       50000, // 50000 个缓存读取 tokens
	}

	result := CalculateCost(usage, pricing, nil)

	// Input: 10000 tokens * $15/1M = $0.15
	expectedInput := 10000.0 * 15.0 / 1_000_000
	if math.Abs(result.InputCost-expectedInput) > 0.0001 {
		t.Errorf("Expected InputCost $%f, got $%f", expectedInput, result.InputCost)
	}

	// Output: 5000 tokens * $75/1M = $0.375
	expectedOutput := 5000.0 * 75.0 / 1_000_000
	if math.Abs(result.OutputCost-expectedOutput) > 0.0001 {
		t.Errorf("Expected OutputCost $%f, got $%f", expectedOutput, result.OutputCost)
	}

	// 5m Cache: 0 tokens = $0.00
	if math.Abs(result.CacheCreation5mCost-0) > 0.0001 {
		t.Errorf("Expected CacheCreation5mCost $0.00, got $%f", result.CacheCreation5mCost)
	}

	// 1h Cache: 1000 tokens * $30/1M = $0.03
	expectedCache1h := 1000.0 * 30.0 / 1_000_000
	if math.Abs(result.CacheCreation1hCost-expectedCache1h) > 0.0001 {
		t.Errorf("Expected CacheCreation1hCost $%f, got $%f", expectedCache1h, result.CacheCreation1hCost)
	}

	// Total Cache: $0.03
	if math.Abs(result.CacheCreationCost-expectedCache1h) > 0.0001 {
		t.Errorf("Expected CacheCreationCost $%f, got $%f", expectedCache1h, result.CacheCreationCost)
	}

	// Cache Read: 50000 tokens * $1.50/1M = $0.075
	expectedCacheRead := 50000.0 * 1.50 / 1_000_000
	if math.Abs(result.CacheReadCost-expectedCacheRead) > 0.0001 {
		t.Errorf("Expected CacheReadCost $%f, got $%f", expectedCacheRead, result.CacheReadCost)
	}

	// Total: $0.15 + $0.375 + $0.03 + $0.075 = $0.63
	expectedTotal := expectedInput + expectedOutput + expectedCache1h + expectedCacheRead
	if math.Abs(result.TotalCost-expectedTotal) > 0.0001 {
		t.Errorf("Expected TotalCost $%f, got $%f", expectedTotal, result.TotalCost)
	}
}

// TestCalculateCost_BackwardCompatibility 测试向后兼容（只有总缓存数）
func TestCalculateCost_BackwardCompatibility(t *testing.T) {
	pricing := &ModelPricing{
		Input:           3.0,
		Output:          15.0,
		CacheCreation:   3.75, // 5m 价格
		CacheCreation1h: 6.0,  // 1h 价格
		CacheRead:       0.30,
	}

	// 只有 CacheCreationTokens 总数，没有分开的 5m/1h
	usage := &TokenUsage{
		InputTokens:           100000,
		OutputTokens:          50000,
		CacheCreationTokens:   10000, // 总数
		CacheCreation5mTokens: 0,     // 无分开数据
		CacheCreation1hTokens: 0,     // 无分开数据
		CacheReadTokens:       20000,
	}

	result := CalculateCost(usage, pricing, nil)

	// 向后兼容：使用 5m 定价
	expectedCache5m := 10000.0 * 3.75 / 1_000_000
	if math.Abs(result.CacheCreation5mCost-expectedCache5m) > 0.0001 {
		t.Errorf("Backward compatible: Expected CacheCreation5mCost $%f, got $%f", expectedCache5m, result.CacheCreation5mCost)
	}

	if math.Abs(result.CacheCreation1hCost-0) > 0.0001 {
		t.Errorf("Backward compatible: Expected CacheCreation1hCost $0, got $%f", result.CacheCreation1hCost)
	}
}

// TestCalculateCost_Mixed5mAnd1hCache 测试混合 5m 和 1h 缓存
func TestCalculateCost_Mixed5mAnd1hCache(t *testing.T) {
	pricing := &ModelPricing{
		Input:           3.0,
		Output:          15.0,
		CacheCreation:   3.75, // 5m = 1.25x
		CacheCreation1h: 6.0,  // 1h = 2x
		CacheRead:       0.30,
	}

	usage := &TokenUsage{
		InputTokens:           100000,
		OutputTokens:          50000,
		CacheCreation5mTokens: 5000, // 5000 个 5 分钟缓存
		CacheCreation1hTokens: 3000, // 3000 个 1 小时缓存
		CacheCreationTokens:   8000, // 总数
		CacheReadTokens:       20000,
	}

	result := CalculateCost(usage, pricing, nil)

	// 5m Cache: 5000 * $3.75/1M = $0.01875
	expectedCache5m := 5000.0 * 3.75 / 1_000_000
	if math.Abs(result.CacheCreation5mCost-expectedCache5m) > 0.0001 {
		t.Errorf("Expected CacheCreation5mCost $%f, got $%f", expectedCache5m, result.CacheCreation5mCost)
	}

	// 1h Cache: 3000 * $6/1M = $0.018
	expectedCache1h := 3000.0 * 6.0 / 1_000_000
	if math.Abs(result.CacheCreation1hCost-expectedCache1h) > 0.0001 {
		t.Errorf("Expected CacheCreation1hCost $%f, got $%f", expectedCache1h, result.CacheCreation1hCost)
	}

	// Total Cache: $0.01875 + $0.018 = $0.03675
	expectedTotalCache := expectedCache5m + expectedCache1h
	if math.Abs(result.CacheCreationCost-expectedTotalCache) > 0.0001 {
		t.Errorf("Expected CacheCreationCost $%f, got $%f", expectedTotalCache, result.CacheCreationCost)
	}
}

// TestCalculateCost_WithEndpointMultiplier 测试端点倍率（叠加模式）
// 验证：细分倍率应用于各类型成本，与总倍率叠加
func TestCalculateCost_WithEndpointMultiplier(t *testing.T) {
	pricing := &ModelPricing{
		Input:           3.0,
		Output:          15.0,
		CacheCreation:   3.75,
		CacheCreation1h: 6.0,
		CacheRead:       0.30,
	}

	usage := &TokenUsage{
		InputTokens:           100000,
		OutputTokens:          50000,
		CacheCreation5mTokens: 5000,
		CacheCreation1hTokens: 3000,
		CacheReadTokens:       20000,
	}

	// 叠加模式：细分倍率应用，总倍率保持 1.0（不叠加）
	multiplier := &EndpointMultiplier{
		CostMultiplier:                1.0, // 总倍率 1.0（不影响）
		InputCostMultiplier:           1.5, // 输入 1.5x
		OutputCostMultiplier:          1.2, // 输出 1.2x
		CacheCreationCostMultiplier:   1.0, // 5m 缓存 1.0x
		CacheCreationCostMultiplier1h: 2.0, // 1h 缓存 2.0x
		CacheReadCostMultiplier:       1.0, // 读取 1.0x
	}

	result := CalculateCost(usage, pricing, multiplier)

	// Input: 100000 * $3/1M * 1.5 = $0.45
	expectedInput := 100000.0 * 3.0 / 1_000_000 * 1.5
	if math.Abs(result.InputCost-expectedInput) > 0.0001 {
		t.Errorf("With multiplier: Expected InputCost $%f, got $%f", expectedInput, result.InputCost)
	}

	// 1h Cache: 3000 * $6/1M * 2.0 = $0.036
	expectedCache1h := 3000.0 * 6.0 / 1_000_000 * 2.0
	if math.Abs(result.CacheCreation1hCost-expectedCache1h) > 0.0001 {
		t.Errorf("With multiplier: Expected CacheCreation1hCost $%f, got $%f", expectedCache1h, result.CacheCreation1hCost)
	}
}

// TestCalculateCost_TotalMultiplier 测试总倍率模式（叠加在所有细分倍率之上）
func TestCalculateCost_TotalMultiplier(t *testing.T) {
	pricing := &ModelPricing{
		Input:           3.0,
		Output:          15.0,
		CacheCreation:   3.75,
		CacheCreation1h: 6.0,
		CacheRead:       0.30,
	}

	usage := &TokenUsage{
		InputTokens:           100000,
		OutputTokens:          50000,
		CacheCreation5mTokens: 5000,
		CacheCreation1hTokens: 3000,
		CacheReadTokens:       20000,
	}

	// 总倍率叠加模式：所有细分倍率 1.0，仅总倍率 1.5
	multiplier := &EndpointMultiplier{
		CostMultiplier:                1.5,
		InputCostMultiplier:           1.0,
		OutputCostMultiplier:          1.0,
		CacheCreationCostMultiplier:   1.0,
		CacheCreationCostMultiplier1h: 1.0,
		CacheReadCostMultiplier:       1.0,
	}

	result := CalculateCost(usage, pricing, multiplier)

	// 先计算无倍率的成本
	baseResult := CalculateCost(usage, pricing, nil)

	// 验证所有成本都乘以 1.5
	if math.Abs(result.InputCost-baseResult.InputCost*1.5) > 0.0001 {
		t.Errorf("Total multiplier: InputCost mismatch")
	}
	if math.Abs(result.OutputCost-baseResult.OutputCost*1.5) > 0.0001 {
		t.Errorf("Total multiplier: OutputCost mismatch")
	}
	if math.Abs(result.CacheCreation5mCost-baseResult.CacheCreation5mCost*1.5) > 0.0001 {
		t.Errorf("Total multiplier: CacheCreation5mCost mismatch")
	}
	if math.Abs(result.CacheCreation1hCost-baseResult.CacheCreation1hCost*1.5) > 0.0001 {
		t.Errorf("Total multiplier: CacheCreation1hCost mismatch")
	}
	if math.Abs(result.TotalCost-baseResult.TotalCost*1.5) > 0.0001 {
		t.Errorf("Total multiplier: TotalCost mismatch, expected $%f, got $%f", baseResult.TotalCost*1.5, result.TotalCost)
	}
}

// TestCalculateCost_StackedMultiplier 测试叠加倍率：细分 * 总倍率
// 这是新逻辑的关键场景：用户期望细分倍率与总倍率"叠加"而不是"二选一"
func TestCalculateCost_StackedMultiplier(t *testing.T) {
	pricing := &ModelPricing{
		Input:  3.0,
		Output: 15.0,
	}

	usage := &TokenUsage{
		InputTokens:  100000,
		OutputTokens: 50000,
	}

	// 输入 2.0x + 总倍率 1.5x → 输入实际倍率 3.0x
	multiplier := &EndpointMultiplier{
		CostMultiplier:                1.5,
		InputCostMultiplier:           2.0,
		OutputCostMultiplier:          1.0,
		CacheCreationCostMultiplier:   1.0,
		CacheCreationCostMultiplier1h: 1.0,
		CacheReadCostMultiplier:       1.0,
	}

	result := CalculateCost(usage, pricing, multiplier)

	// Input: 100000 * $3/1M * 2.0 * 1.5 = $0.90
	expectedInput := 100000.0 * 3.0 / 1_000_000 * 2.0 * 1.5
	if math.Abs(result.InputCost-expectedInput) > 0.0001 {
		t.Errorf("Stacked multiplier: Expected InputCost $%f, got $%f", expectedInput, result.InputCost)
	}

	// Output: 50000 * $15/1M * 1.0 * 1.5 = $1.125
	expectedOutput := 50000.0 * 15.0 / 1_000_000 * 1.0 * 1.5
	if math.Abs(result.OutputCost-expectedOutput) > 0.0001 {
		t.Errorf("Stacked multiplier: Expected OutputCost $%f, got $%f", expectedOutput, result.OutputCost)
	}
}

func TestCalculateCost_WithModelMultiplier(t *testing.T) {
	pricing := &ModelPricing{
		Input:     30.0,
		Output:    60.0,
		CacheRead: 3.0,
	}

	usage := &TokenUsage{
		InputTokens:     1_000_000,
		OutputTokens:    1_000_000,
		CacheReadTokens: 1_000_000,
	}

	resolved := ResolveEndpointMultiplierForModel(EndpointMultiplier{
		CostMultiplier:          0.4,
		InputCostMultiplier:     1.0,
		OutputCostMultiplier:    1.0,
		CacheReadCostMultiplier: 1.0,
		ModelMultipliers: []ModelMultiplier{
			{
				Model:                   "gpt-5.5",
				CostMultiplier:          4.0 / 3.0,
				InputCostMultiplier:     1.0,
				OutputCostMultiplier:    2.0,
				CacheReadCostMultiplier: 1.5,
			},
		},
	}, "gpt-5.5")

	if resolved.CostMultiplier != 0.4 {
		t.Fatalf("endpoint total multiplier should stay separated, got %f", resolved.CostMultiplier)
	}
	if math.Abs(resolved.ModelCostMultiplier-4.0/3.0) > 0.0001 {
		t.Fatalf("model total multiplier mismatch, got %f", resolved.ModelCostMultiplier)
	}

	result := CalculateCost(usage, pricing, &resolved)

	expectedInput := 30.0 * 0.4 * (4.0 / 3.0)
	expectedOutput := 60.0 * 2.0 * 0.4 * (4.0 / 3.0)
	expectedCacheRead := 3.0 * 1.5 * 0.4 * (4.0 / 3.0)
	if math.Abs(result.InputCost-expectedInput) > 0.0001 {
		t.Errorf("Model multiplier: Expected InputCost $%f, got $%f", expectedInput, result.InputCost)
	}
	if math.Abs(result.OutputCost-expectedOutput) > 0.0001 {
		t.Errorf("Model multiplier: Expected OutputCost $%f, got $%f", expectedOutput, result.OutputCost)
	}
	if math.Abs(result.CacheReadCost-expectedCacheRead) > 0.0001 {
		t.Errorf("Model multiplier: Expected CacheReadCost $%f, got $%f", expectedCacheRead, result.CacheReadCost)
	}
}

func TestResolveEndpointMultiplierForServiceTier(t *testing.T) {
	base := EndpointMultiplier{
		CostMultiplier:                0.5,
		InputCostMultiplier:           2,
		OutputCostMultiplier:          3,
		CacheCreationCostMultiplier:   4,
		CacheCreationCostMultiplier1h: 5,
		CacheReadCostMultiplier:       6,
		ServiceTierMultipliers: map[string]ServiceTierMultiplier{
			ServiceTierFast: {
				CostMultiplier:          1.5,
				InputCostMultiplier:     2,
				OutputCostMultiplier:    2.5,
				CacheCreationMultiplier: 3,
				CacheReadMultiplier:     4,
			},
		},
	}
	resolved := ResolveEndpointMultiplierForServiceTier(base, "priority")
	if resolved.CostMultiplier != 0.75 || resolved.InputCostMultiplier != 4 || resolved.OutputCostMultiplier != 7.5 {
		t.Fatalf("fast tier did not compose with ordinary multipliers: %#v", resolved)
	}
	if resolved.CacheCreationCostMultiplier != 12 || resolved.CacheCreationCostMultiplier1h != 15 || resolved.CacheReadCostMultiplier != 24 {
		t.Fatalf("fast tier cache multipliers were not composed: %#v", resolved)
	}
	auto := ResolveEndpointMultiplierForServiceTier(base, "auto")
	if auto.CostMultiplier != base.CostMultiplier || auto.InputCostMultiplier != base.InputCostMultiplier {
		t.Fatalf("auto tier must stay unresolved: %#v", auto)
	}
}

// TestCalculateCost_NilInputs 测试空输入
func TestCalculateCost_NilInputs(t *testing.T) {
	pricing := &ModelPricing{
		Input:  3.0,
		Output: 15.0,
	}

	// nil usage
	result := CalculateCost(nil, pricing, nil)
	if result.TotalCost != 0 {
		t.Errorf("nil usage: Expected TotalCost $0, got $%f", result.TotalCost)
	}

	// nil pricing
	usage := &TokenUsage{InputTokens: 1000}
	result = CalculateCost(usage, nil, nil)
	if result.TotalCost != 0 {
		t.Errorf("nil pricing: Expected TotalCost $0, got $%f", result.TotalCost)
	}
}

// TestCalculateCost_Default1hPrice 测试 1h 定价默认值
func TestCalculateCost_Default1hPrice(t *testing.T) {
	// 不设置 CacheCreation1h，应默认使用 2x input
	pricing := &ModelPricing{
		Input:           10.0,
		Output:          50.0,
		CacheCreation:   12.5, // 1.25x input
		CacheCreation1h: 0,    // 未设置，应默认 2x input = 20.0
		CacheRead:       1.0,
	}

	usage := &TokenUsage{
		CacheCreation1hTokens: 10000,
	}

	result := CalculateCost(usage, pricing, nil)

	// 1h Cache: 10000 * $20/1M = $0.20 (因为默认 2x input = 2 * 10 = 20)
	expectedCache1h := 10000.0 * 20.0 / 1_000_000
	if math.Abs(result.CacheCreation1hCost-expectedCache1h) > 0.0001 {
		t.Errorf("Default 1h price: Expected CacheCreation1hCost $%f, got $%f", expectedCache1h, result.CacheCreation1hCost)
	}
}

// TestCalculateCost_RealWorldScenario 测试真实场景
// 模拟请求 req-c2a06ef3: claude-opus-4-5-20251101
func TestCalculateCost_RealWorldScenario(t *testing.T) {
	// Claude Opus 4.5 官方定价
	pricing := &ModelPricing{
		Input:           15.0,  // $15/1M tokens
		Output:          75.0,  // $75/1M tokens
		CacheCreation:   18.75, // $18.75/1M (5m = 1.25x input)
		CacheCreation1h: 30.0,  // $30/1M (1h = 2x input)
		CacheRead:       1.50,  // $1.50/1M
	}

	// 真实请求数据（从之前的调查）
	usage := &TokenUsage{
		InputTokens:           5000,
		OutputTokens:          800,
		CacheCreation1hTokens: 2000,  // 使用 1 小时缓存
		CacheCreation5mTokens: 0,     // 无 5 分钟缓存
		CacheCreationTokens:   2000,  // 总数
		CacheReadTokens:       10000, // 缓存读取
	}

	result := CalculateCost(usage, pricing, nil)

	// 手动计算预期成本
	// Input: 5000 * $15/1M = $0.075
	expectedInput := 5000.0 * 15.0 / 1_000_000
	// Output: 800 * $75/1M = $0.06
	expectedOutput := 800.0 * 75.0 / 1_000_000
	// 1h Cache: 2000 * $30/1M = $0.06
	expectedCache1h := 2000.0 * 30.0 / 1_000_000
	// Cache Read: 10000 * $1.50/1M = $0.015
	expectedCacheRead := 10000.0 * 1.50 / 1_000_000

	expectedTotal := expectedInput + expectedOutput + expectedCache1h + expectedCacheRead

	t.Logf("Real-world scenario cost breakdown:")
	t.Logf("  Input:       $%.6f (expected $%.6f)", result.InputCost, expectedInput)
	t.Logf("  Output:      $%.6f (expected $%.6f)", result.OutputCost, expectedOutput)
	t.Logf("  Cache 5m:    $%.6f (expected $0.000000)", result.CacheCreation5mCost)
	t.Logf("  Cache 1h:    $%.6f (expected $%.6f)", result.CacheCreation1hCost, expectedCache1h)
	t.Logf("  Cache Read:  $%.6f (expected $%.6f)", result.CacheReadCost, expectedCacheRead)
	t.Logf("  Total:       $%.6f (expected $%.6f)", result.TotalCost, expectedTotal)

	if math.Abs(result.TotalCost-expectedTotal) > 0.0001 {
		t.Errorf("Real-world: Expected TotalCost $%f, got $%f", expectedTotal, result.TotalCost)
	}
}

// TestCalculateCost_BackwardCompatible 测试旧版 CalculateCost 向后兼容
func TestCalculateCost_BackwardCompatible(t *testing.T) {
	pricing := &ModelPricing{
		Input:           3.0,
		Output:          15.0,
		CacheCreation:   3.75,
		CacheCreation1h: 6.0,
		CacheRead:       0.30,
	}

	// 使用旧版 CalculateCostFromTokens，use1hCache = false
	result5m := CalculateCostFromTokens(100000, 50000, 10000, 20000, pricing, nil, false)

	// 缓存应该使用 5m 定价
	expectedCache5m := 10000.0 * 3.75 / 1_000_000
	if math.Abs(result5m.CacheCreation5mCost-expectedCache5m) > 0.0001 {
		t.Errorf("CalculateCost(use1hCache=false): Expected CacheCreation5mCost $%f, got $%f", expectedCache5m, result5m.CacheCreation5mCost)
	}
	if math.Abs(result5m.CacheCreation1hCost-0) > 0.0001 {
		t.Errorf("CalculateCost(use1hCache=false): Expected CacheCreation1hCost $0, got $%f", result5m.CacheCreation1hCost)
	}

	// 使用旧版 CalculateCostFromTokens，use1hCache = true
	result1h := CalculateCostFromTokens(100000, 50000, 10000, 20000, pricing, nil, true)

	// 缓存应该使用 1h 定价
	expectedCache1h := 10000.0 * 6.0 / 1_000_000
	if math.Abs(result1h.CacheCreation1hCost-expectedCache1h) > 0.0001 {
		t.Errorf("CalculateCost(use1hCache=true): Expected CacheCreation1hCost $%f, got $%f", expectedCache1h, result1h.CacheCreation1hCost)
	}
	if math.Abs(result1h.CacheCreation5mCost-0) > 0.0001 {
		t.Errorf("CalculateCost(use1hCache=true): Expected CacheCreation5mCost $0, got $%f", result1h.CacheCreation5mCost)
	}
}
