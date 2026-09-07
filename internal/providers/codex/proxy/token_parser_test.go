package codexproxy

import (
	"math"
	"strings"
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

func TestTokenParserCarriesResponsesCacheWriteTokensToBilling(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5.6-sol",
		"usage": {
			"input_tokens": 2048,
			"output_tokens": 64,
			"total_tokens": 2112,
			"input_tokens_details": {
				"cached_tokens": 0,
				"cache_write_tokens": 1024
			}
		}
	}`)

	result := NewTokenParser().ParseNonStreaming(body)
	if result == nil {
		t.Fatal("expected usage to be parsed")
	}
	usage := buildTokenUsage(result, tracking.BillingOutputTokenSourceOutput)
	if usage.CacheCreationTokens != 1024 {
		t.Fatalf("expected cache write tokens 1024, got %d", usage.CacheCreationTokens)
	}
}

func TestTokenParserCarriesResolvedServiceTier(t *testing.T) {
	body := []byte(`{
		"response": {
			"model": "gpt-5.6-sol",
			"service_tier": "priority",
			"usage": {"input_tokens": 10, "output_tokens": 2}
		}
	}`)

	result := NewTokenParser().ParseNonStreaming(body)
	if result == nil {
		t.Fatal("expected usage to be parsed")
	}
	if result.ServiceTier != "priority" {
		t.Fatalf("expected response service tier priority, got %q", result.ServiceTier)
	}
}

func TestTokenParserCarriesChatCacheWriteTokenStringsToBilling(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5.6-terra",
		"usage": {
			"prompt_tokens": "1536",
			"completion_tokens": "32",
			"total_tokens": "1568",
			"prompt_tokens_details": {
				"cached_tokens": "0",
				"cache_write_tokens": "512"
			}
		}
	}`)

	result := NewTokenParser().ParseNonStreaming(body)
	if result == nil {
		t.Fatal("expected usage to be parsed")
	}
	usage := buildTokenUsage(result, tracking.BillingOutputTokenSourceOutput)
	if usage.CacheCreationTokens != 512 {
		t.Fatalf("expected cache write tokens 512, got %d", usage.CacheCreationTokens)
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

func TestTokenParserParsesResponseFailedCapacityError(t *testing.T) {
	line := []byte(`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"Selected model is at capacity. Please try a different model."}}}`)

	streamErr := NewTokenParser().ParseStreamingErrorLine(line)
	if streamErr == nil {
		t.Fatal("expected stream error to be parsed")
	}
	// 不再断言状态码：上游这条响应的 HTTP 状态码是 200，错误只在 SSE 事件里，
	// 反代不该凭 message 关键字编一个 503 写进链路。改为断言处置动作。
	if !strings.Contains(streamErr.Message, "Selected model is at capacity") {
		t.Fatalf("unexpected message: %q", streamErr.Message)
	}
	if streamErr.Code != "server_error" {
		t.Fatalf("expected code server_error, got %q", streamErr.Code)
	}
	if action := NewErrorClassifier().Classify(nil, streamErr); action != ActionFailover {
		t.Fatalf("capacity 错误应切换端点，got %v", action)
	}
}

func TestTokenParserParsesTopLevelStreamingError(t *testing.T) {
	line := []byte(`data: {"error":{"type":"server_error","message":"Selected model is at capacity. Please try a different model."}}`)

	streamErr := NewTokenParser().ParseStreamingErrorLine(line)
	if streamErr == nil {
		t.Fatal("expected stream error to be parsed")
	}
	if streamErr.Type != "server_error" {
		t.Fatalf("expected type server_error, got %q", streamErr.Type)
	}
	if action := NewErrorClassifier().Classify(nil, streamErr); action != ActionFailover {
		t.Fatalf("capacity 错误应切换端点，got %v", action)
	}
}

// TestTokenParserDoesNotInventStatusCode 锁定：上游用 HTTP 200 + SSE error 事件报错时，
// 反代不得凭关键字造出提供商从未返回过的状态码——那会与真实的 429/401 在链路里混为一谈。
func TestTokenParserDoesNotInventStatusCode(t *testing.T) {
	lines := []string{
		`data: {"error":{"type":"rate_limit_error","message":"rate_limit exceeded"}}`,
		`data: {"error":{"type":"authentication_error","message":"unauthorized"}}`,
		`data: {"error":{"code":"insufficient_quota","message":"billing hard limit reached"}}`,
	}
	classifier := NewErrorClassifier()
	for _, line := range lines {
		streamErr := NewTokenParser().ParseStreamingErrorLine([]byte(line))
		if streamErr == nil {
			t.Fatalf("expected stream error for %s", line)
		}
		// 换端点仍然是对的处置，但不能因此把它记成一次 429/401 响应
		if action := classifier.Classify(nil, streamErr); action != ActionFailover {
			t.Fatalf("%s: expected ActionFailover, got %v", line, action)
		}
	}

	// 请求体本身不合法时换端点无意义，仍应立即失败
	badRequest := NewTokenParser().ParseStreamingErrorLine(
		[]byte(`data: {"error":{"type":"invalid_request_error","message":"unsupported parameter"}}`))
	if badRequest == nil {
		t.Fatal("expected stream error to be parsed")
	}
	if action := classifier.Classify(nil, badRequest); action != ActionFailFast {
		t.Fatalf("invalid_request 应快速失败，got %v", action)
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
