package tracking

import (
	"strings"
)

const totalTokensSQLExpression = `CASE
	WHEN LOWER(COALESCE(provider, 'claude')) IN ('codex', 'openai') THEN
		COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0) +
		CASE
			WHEN COALESCE(cache_read_tokens, 0) > COALESCE(input_tokens, 0) THEN COALESCE(cache_read_tokens, 0)
			ELSE 0
		END
	ELSE
		COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0) +
		COALESCE(cache_creation_tokens, 0) + COALESCE(cache_read_tokens, 0)
END`

const distributionInputTokensSQLExpression = `CASE
	WHEN LOWER(COALESCE(provider, 'claude')) IN ('codex', 'openai') THEN
		CASE
			WHEN COALESCE(cache_read_tokens, 0) > COALESCE(input_tokens, 0) THEN COALESCE(input_tokens, 0)
			ELSE COALESCE(input_tokens, 0) - COALESCE(cache_read_tokens, 0)
		END
	ELSE
		COALESCE(input_tokens, 0)
END`

// TotalTokensSQLExpression returns the request_logs expression for display/usage total tokens.
// Codex/OpenAI input_tokens already include cached_tokens. Very old Codex rows stored only
// billable input, detected by cache_read_tokens > input_tokens and expanded for display totals.
func TotalTokensSQLExpression() string {
	return totalTokensSQLExpression
}

// DistributionInputTokensSQLExpression returns input-token buckets for token distribution charts.
// Codex/OpenAI cache reads are rendered as a separate bucket, so cached input is removed here.
func DistributionInputTokensSQLExpression() string {
	return distributionInputTokensSQLExpression
}

// DisplayInputTokens returns provider-facing input tokens for UI/statistics display.
func DisplayInputTokens(provider string, inputTokens, cacheReadTokens int64) int64 {
	if isInputCacheInclusiveProvider(provider) && cacheReadTokens > inputTokens {
		return inputTokens + cacheReadTokens
	}
	return inputTokens
}

// DisplayTotalTokens returns provider-facing total tokens for request display/statistics.
func DisplayTotalTokens(provider string, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) int64 {
	if isInputCacheInclusiveProvider(provider) {
		return DisplayInputTokens(provider, inputTokens, cacheReadTokens) + outputTokens
	}
	return inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
}

func isInputCacheInclusiveProvider(provider string) bool {
	switch strings.ToLower(provider) {
	case "codex", "openai":
		return true
	default:
		return false
	}
}

func cacheHitRate(numerator, denominator int64) *float64 {
	if denominator <= 0 {
		return nil
	}
	rate := float64(numerator) / float64(denominator)
	return &rate
}

// 缓存命中率按 provider 分别计算（09-25 评审修正 3）：
//
//	Claude：input_tokens 不含缓存 → cache_read / (input + cache_creation + cache_read)
//	Codex ：input_tokens 已含 cached_tokens → cache_read / input
//
// Codex 极旧的行只存了计费输入（cache_read > input），分母按展示口径补回 cache_read，
// 与 totalTokensSQLExpression 的处理一致，避免命中率超过 100%。
const cacheHitNumeratorSQLExpression = `COALESCE(cache_read_tokens, 0)`

const cacheHitDenominatorSQLExpression = `CASE
	WHEN LOWER(COALESCE(provider, 'claude')) IN ('codex', 'openai') THEN
		CASE
			WHEN COALESCE(cache_read_tokens, 0) > COALESCE(input_tokens, 0) THEN COALESCE(input_tokens, 0) + COALESCE(cache_read_tokens, 0)
			ELSE COALESCE(input_tokens, 0)
		END
	ELSE
		COALESCE(input_tokens, 0) + COALESCE(cache_creation_tokens, 0) + COALESCE(cache_read_tokens, 0)
END`

// cacheHitParts 内存侧（热池、端点统计）使用的同一口径
func cacheHitParts(provider string, inputTokens, cacheCreationTokens, cacheReadTokens int64) (numerator, denominator int64) {
	if isInputCacheInclusiveProvider(provider) {
		return cacheReadTokens, DisplayInputTokens(provider, inputTokens, cacheReadTokens)
	}
	return cacheReadTokens, inputTokens + cacheCreationTokens + cacheReadTokens
}
