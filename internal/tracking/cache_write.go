package tracking

type cacheWriteAllocation struct {
	tokens30m, tokens12h int64
	price12h             float64
	reason               string
}

func resolveCacheWriteAllocation(usage *TokenUsage, pricing *ModelPricing, mult *EndpointMultiplier, factors billingFactors) cacheWriteAllocation {
	total := max(usage.CacheCreationTokens, 0)
	a := cacheWriteAllocation{tokens30m: total, price12h: pricing.CacheWritePrice12h, reason: "30m_default"}
	if factors.longContextActive {
		a.price12h = pricing.LongContextCacheWritePrice12h
	}
	if !mult.CacheWriteTTLPricing {
		return a
	}
	if a.price12h <= 0 {
		a.reason = "30m_missing_12h_price"
		return a
	}
	// Inconsistent or partial upstream counters cannot invent expensive writes.
	if usage.CacheWrite30mTokens < 0 || usage.CacheWrite12hTokens < 0 || usage.CacheWrite30mTokens > total || usage.CacheWrite12hTokens != total-usage.CacheWrite30mTokens {
		a.reason = "30m_missing_ttl_usage"
		return a
	}
	a.tokens30m, a.tokens12h = usage.CacheWrite30mTokens, usage.CacheWrite12hTokens
	a.reason = "reported_ttl"
	return a
}
