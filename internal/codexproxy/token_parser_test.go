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
	if result.ReasoningTokens != 137 {
		t.Fatalf("expected reasoning tokens 137, got %d", result.ReasoningTokens)
	}
	if result.CacheReadTokens != 145280 {
		t.Fatalf("expected cache read tokens 145280, got %d", result.CacheReadTokens)
	}
	if result.TotalTokens != 147327 {
		t.Fatalf("expected total tokens 147327, got %d", result.TotalTokens)
	}

	cost := tracking.CalculateCost(buildTokenUsage(result, tracking.BillingOutputTokenSourceOutput), &tracking.ModelPricing{
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

func TestTokenParserAcceptsProviderUsageVariants(t *testing.T) {
	body := []byte(`{
		"model": "gpt5.5",
		"usage": {
			"input_tokens": "131969",
			"total_tokens": "132099",
			"output_tokens": "130",
			"input_token_details": {
				"cached_tokens": "131072"
			},
			"output_token_details": {
				"reasoning_tokens": "22"
			}
		}
	}`)

	result := NewTokenParser().ParseNonStreaming(body)
	if result == nil {
		t.Fatal("expected usage to be parsed")
	}
	if result.InputTokens != 131969 {
		t.Fatalf("expected input tokens 131969, got %d", result.InputTokens)
	}
	if result.OutputTokens != 130 {
		t.Fatalf("expected output tokens 130, got %d", result.OutputTokens)
	}
	if result.ReasoningTokens != 22 {
		t.Fatalf("expected reasoning tokens 22, got %d", result.ReasoningTokens)
	}
	if result.CacheReadTokens != 131072 {
		t.Fatalf("expected cache read tokens 131072, got %d", result.CacheReadTokens)
	}
}

func TestTokenParserAcceptsTopLevelReasoningTokens(t *testing.T) {
	body := []byte(`{
		"model": "gpt5.5",
		"usage": {
			"input_tokens": 131315,
			"total_tokens": 131682,
			"output_tokens": 367,
			"reasoning_tokens": 30,
			"input_tokens_details": {
				"cached_tokens": 118272
			}
		}
	}`)

	result := NewTokenParser().ParseNonStreaming(body)
	if result == nil {
		t.Fatal("expected usage to be parsed")
	}
	if result.ReasoningTokens != 30 {
		t.Fatalf("expected reasoning tokens 30, got %d", result.ReasoningTokens)
	}
}

func TestTokenParserAcceptsSSEDataWithoutSpace(t *testing.T) {
	line := []byte(`data:{"type":"response.completed","response":{"model":"gpt5.5","usage":{"input_tokens":"131969","output_tokens":"130","output_tokens_details":{"reasoning_tokens":"22"}}}}`)

	result, done := NewTokenParser().ParseStreamingLine(line)
	if done {
		t.Fatal("did not expect stream to be done")
	}
	if result == nil {
		t.Fatal("expected usage to be parsed")
	}
	if result.Model != "gpt5.5" {
		t.Fatalf("expected model gpt5.5, got %q", result.Model)
	}
	if result.OutputTokens != 130 {
		t.Fatalf("expected output tokens 130, got %d", result.OutputTokens)
	}
	if result.ReasoningTokens != 22 {
		t.Fatalf("expected reasoning tokens 22, got %d", result.ReasoningTokens)
	}
}

func TestCodexCostCanUseReasoningOrVisibleOutputTokens(t *testing.T) {
	pricing := &tracking.ModelPricing{
		Input:     0,
		Output:    30,
		CacheRead: 0,
	}

	reasoningCost := tracking.CalculateCost(&tracking.TokenUsage{
		OutputTokens:             130,
		ReasoningTokens:          22,
		BillingOutputTokenSource: tracking.BillingOutputTokenSourceReasoning,
	}, pricing, nil)
	if math.Abs(reasoningCost.OutputCost-0.000660) > 0.000001 {
		t.Fatalf("expected reasoning output cost 0.000660, got %.6f", reasoningCost.OutputCost)
	}

	visibleCost := tracking.CalculateCost(&tracking.TokenUsage{
		OutputTokens:             130,
		ReasoningTokens:          22,
		BillingOutputTokenSource: tracking.BillingOutputTokenSourceVisible,
	}, pricing, nil)
	if math.Abs(visibleCost.OutputCost-0.003240) > 0.000001 {
		t.Fatalf("expected visible output cost 0.003240, got %.6f", visibleCost.OutputCost)
	}
}
