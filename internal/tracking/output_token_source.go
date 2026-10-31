package tracking

import "strings"

const (
	BillingOutputTokenSourceOutput    = "output_tokens"
	BillingOutputTokenSourceReasoning = "reasoning_tokens"
	BillingOutputTokenSourceVisible   = "visible_output_tokens"
)

func NormalizeBillingOutputTokenSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case BillingOutputTokenSourceReasoning:
		return BillingOutputTokenSourceReasoning
	case BillingOutputTokenSourceVisible:
		return BillingOutputTokenSourceVisible
	default:
		return BillingOutputTokenSourceOutput
	}
}

func VisibleOutputTokens(outputTokens, reasoningTokens int64) int64 {
	if reasoningTokens <= 0 {
		return outputTokens
	}
	if reasoningTokens >= outputTokens {
		return 0
	}
	return outputTokens - reasoningTokens
}

func BillableOutputTokens(outputTokens, reasoningTokens int64, source string) int64 {
	switch NormalizeBillingOutputTokenSource(source) {
	case BillingOutputTokenSourceReasoning:
		if reasoningTokens < 0 {
			return 0
		}
		return reasoningTokens
	case BillingOutputTokenSourceVisible:
		return VisibleOutputTokens(outputTokens, reasoningTokens)
	default:
		if outputTokens < 0 {
			return 0
		}
		return outputTokens
	}
}

func ResolveBillableOutputTokens(usage *TokenUsage) int64 {
	if usage == nil {
		return 0
	}
	source := NormalizeBillingOutputTokenSource(usage.BillingOutputTokenSource)
	if usage.BillableOutputTokens > 0 && source != BillingOutputTokenSourceOutput {
		return usage.BillableOutputTokens
	}
	return BillableOutputTokens(usage.OutputTokens, usage.ReasoningTokens, source)
}

func applyTokenUsageToActiveRequest(req *ActiveRequest, tokens *TokenUsage) {
	if req == nil || tokens == nil {
		return
	}

	source := req.BillingOutputTokenSource
	if tokens.BillingOutputTokenSource != "" {
		source = tokens.BillingOutputTokenSource
	}
	source = NormalizeBillingOutputTokenSource(source)

	req.InputTokens = tokens.InputTokens
	req.OutputTokens = tokens.OutputTokens
	req.ReasoningTokens = tokens.ReasoningTokens
	req.BillingOutputTokenSource = source
	req.BillableOutputTokens = BillableOutputTokens(tokens.OutputTokens, tokens.ReasoningTokens, source)
	req.CacheCreationTokens = tokens.CacheCreationTokens
	req.CacheWrite30mTokens = tokens.CacheWrite30mTokens
	req.CacheWrite12hTokens = tokens.CacheWrite12hTokens
	req.CacheCreation5mTokens = tokens.CacheCreation5mTokens
	req.CacheCreation1hTokens = tokens.CacheCreation1hTokens
	req.CacheReadTokens = tokens.CacheReadTokens
	req.InputIncludesCacheRead = tokens.InputIncludesCacheRead
}

func tokenUsageFromCompleteData(data RequestCompleteData) *TokenUsage {
	return &TokenUsage{
		InputTokens:              data.InputTokens,
		OutputTokens:             data.OutputTokens,
		ReasoningTokens:          data.ReasoningTokens,
		BillableOutputTokens:     data.BillableOutputTokens,
		BillingOutputTokenSource: data.BillingOutputTokenSource,
		CacheCreationTokens:      data.CacheCreationTokens,
		CacheCreation5mTokens:    data.CacheCreation5mTokens,
		CacheCreation1hTokens:    data.CacheCreation1hTokens,
		CacheReadTokens:          data.CacheReadTokens,
		InputIncludesCacheRead:   data.InputIncludesCacheRead,
	}
}
