package codexproxy

import (
	"math"
	"testing"

	"ai-relay-forwarder/internal/tracking"
)

func TestTokenParserPreservesProviderUsageAndBillsCachedInputOnce(t *testing.T) {
	body := []byte(`{
		"model": "gpt5.5",
		"usage": {
			"input_tokens": 146783,
			"total_tokens": 147327,
			"output_tokens": 544,
			"input_tokens_details": {
				"cached_tokens": 145280
			},
			"output_tokens_details": {
				"reasoning_tokens": 137
			}
		}
	}`)

	result := NewTokenParser().ParseNonStreaming(body)
	if result == nil {
		t.Fatal("expected usage to be parsed")
	}
	if result.Model != "gpt5.5" {
		t.Fatalf("expected model gpt5.5, got %q", result.Model)
	}
	if result.InputTokens != 146783 {
		t.Fatalf("expected provider input tokens 146783, got %d", result.InputTokens)
	}
	if result.OutputTokens != 544 {
		t.Fatalf("expected output tokens 544, got %d", result.OutputTokens)
	}
	if result.CacheReadTokens != 145280 {
		t.Fatalf("expected cache read tokens 145280, got %d", result.CacheReadTokens)
	}
	if result.TotalTokens != 147327 {
		t.Fatalf("expected total tokens 147327, got %d", result.TotalTokens)
	}

	cost := tracking.CalculateCost(buildTokenUsage(result), &tracking.ModelPricing{
		Input:     5,
		Output:    30,
		CacheRead: 0.5,
	}, &tracking.EndpointMultiplier{
		CostMultiplier:          0.4,
		InputCostMultiplier:     1,
		OutputCostMultiplier:    1,
		CacheReadCostMultiplier: 1,
	})

	if math.Abs(cost.TotalCost-0.038590) > 0.000001 {
		t.Fatalf("expected total cost 0.038590, got %.6f", cost.TotalCost)
	}
}
