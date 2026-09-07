package tracking

import (
	"reflect"
	"testing"
)

// Claude 与 Codex 共用一张定价 map，模型名空间天然会重叠
// （Claude provider 下已存在 MiniMax-M2 这类非 Anthropic 模型）。
// key 不含 provider 时，后写入的一侧会静默覆盖另一侧。
func TestModelPricingKeyIsolatesProviders(t *testing.T) {
	claudeKey := ModelPricingKey("claude", "MiniMax-M2", "")
	codexKey := ModelPricingKey("codex", "MiniMax-M2", "")

	if claudeKey == codexKey {
		t.Fatalf("same model name across providers must not share a key, got %q for both", claudeKey)
	}

	pricing := map[string]ModelPricing{
		claudeKey: {Input: 3.0},
		codexKey:  {Input: 7.0},
	}
	if got := pricing[claudeKey].Input; got != 3.0 {
		t.Fatalf("claude pricing input = %.2f, want 3.00", got)
	}
	if got := pricing[codexKey].Input; got != 7.0 {
		t.Fatalf("codex pricing input = %.2f, want 7.00", got)
	}
}

// 空 provider 归一化为 claude，未传 provider 的旧调用点因此落在 Claude 域。
func TestModelPricingKeyDefaultsToClaude(t *testing.T) {
	if ModelPricingKey("", "gpt-5.6-sol", "") != ModelPricingKey("claude", "gpt-5.6-sol", "") {
		t.Fatal("empty provider must normalize to claude")
	}
	if ModelPricingKey("codex", "", "flex") != "" {
		t.Fatal("empty model name must yield an empty key")
	}
}

// 标准档与 auto / 未知档都落到空档位——空档位行既是标准档定价，也是其余档位的兜底。
func TestNormalizeServiceTierForPricingCollapsesStandardAndAuto(t *testing.T) {
	for _, tier := range []string{"", "default", "standard", "standard_only", "auto", "totally-unknown"} {
		if got := normalizeServiceTierForPricing(tier); got != "" {
			t.Fatalf("normalizeServiceTierForPricing(%q) = %q, want empty", tier, got)
		}
	}
	cases := map[string]string{
		"flex":     ServiceTierFlex,
		"batch":    ServiceTierBatch,
		"fast":     ServiceTierFast,
		"priority": ServiceTierFast,
	}
	for input, want := range cases {
		if got := normalizeServiceTierForPricing(input); got != want {
			t.Fatalf("normalizeServiceTierForPricing(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestModelPricingLookupKeysFallsBackToStandardRow(t *testing.T) {
	got := modelPricingLookupKeys("codex", "gpt-5.6-sol", "flex")
	want := []string{
		"codex::gpt-5.6-sol::flex",
		"codex::gpt-5.6-sol::",
		"gpt-5.6-sol",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lookup keys = %#v, want %#v", got, want)
	}

	// 标准档请求不该产生重复的档位 key
	standard := modelPricingLookupKeys("codex", "gpt-5.6-sol", "standard")
	if !reflect.DeepEqual(standard, []string{"codex::gpt-5.6-sol::", "gpt-5.6-sol"}) {
		t.Fatalf("standard tier lookup keys = %#v", standard)
	}

	if modelPricingLookupKeys("codex", "  ", "flex") != nil {
		t.Fatal("blank model name must yield no lookup keys")
	}
}

// 档位专属定价优先，缺失时回退标准档，模型完全未配置时才用 provider 自己的兜底价。
func TestModelPricingLookupResolutionOrder(t *testing.T) {
	pricing := map[string]ModelPricing{
		ModelPricingKey("codex", "gpt-5.6-sol", ""):     {Input: 5.0},
		ModelPricingKey("codex", "gpt-5.6-sol", "flex"): {Input: 2.5},
		defaultModelPricingKey("codex"):                 {Input: 2.0},
		defaultModelPricingKey("claude"):                {Input: 3.0},
	}
	resolve := func(provider, model, tier string) float64 {
		for _, key := range modelPricingLookupKeys(provider, model, tier) {
			if p, ok := pricing[key]; ok {
				return p.Input
			}
		}
		return pricing[defaultModelPricingKey(provider)].Input
	}

	if got := resolve("codex", "gpt-5.6-sol", "flex"); got != 2.5 {
		t.Fatalf("flex tier input = %.2f, want 2.50 (tier row)", got)
	}
	if got := resolve("codex", "gpt-5.6-sol", "batch"); got != 5.0 {
		t.Fatalf("batch tier input = %.2f, want 5.00 (standard row fallback)", got)
	}
	if got := resolve("codex", "gpt-5.6-sol", "auto"); got != 5.0 {
		t.Fatalf("auto tier input = %.2f, want 5.00 (standard row)", got)
	}
	// Codex 未知模型必须用 Codex 自己的兜底价，不得落到 Claude 的 3.0
	if got := resolve("codex", "never-configured", ""); got != 2.0 {
		t.Fatalf("unknown codex model input = %.2f, want 2.00", got)
	}
}

// 兜底定价本身也是 model_pricing 的普通记录，入缓存时同样经 ModelPricingKey 加前缀。
func TestDefaultModelPricingKeyMatchesWriteSide(t *testing.T) {
	if got, want := defaultModelPricingKey("claude"), ModelPricingKey("claude", "_default", ""); got != want {
		t.Fatalf("claude default key = %q, want %q", got, want)
	}
	if got, want := defaultModelPricingKey("codex"), ModelPricingKey("codex", "_codex_default", ""); got != want {
		t.Fatalf("codex default key = %q, want %q", got, want)
	}
	if defaultModelPricingKey("") != defaultModelPricingKey("claude") {
		t.Fatal("empty provider must fall back to the claude default key")
	}
}
