package tracking

// CalculateAttemptCost uses the same pricing, provider fallback, cache rules,
// tier and multipliers as request accounting. The caller persists the result
// once, so later repricing or log retention cannot change a wallet balance.
func (ut *UsageTracker) CalculateAttemptCost(provider, channel, group, endpoint, model, tier string, usage *TokenUsage) (float64, bool) {
	if ut == nil || usage == nil {
		return 0, false
	}
	ut.mu.RLock()
	pricing, pricingModel, pricingTier, found := lookupModelPricing(ut.pricing, provider, model, tier)
	multiplier := DefaultEndpointMultiplier()
	for _, key := range endpointMultiplierLookupKeys(provider, channel, group, endpoint) {
		if value, ok := ut.endpointMu[key]; ok {
			multiplier = value
			break
		}
	}
	ut.mu.RUnlock()
	if !found {
		return 0, false
	}
	copyUsage := *usage
	copyUsage.BillingOutputTokenSource = multiplier.BillingOutputTokenSource
	usage = &copyUsage
	cost, _, _ := calculateRequestCost(usage, pricing, multiplier, billingIdentity{provider: provider, requestModel: model, serviceTier: tier, pricingModel: pricingModel, pricingTier: pricingTier})
	return cost.TotalCost, true
}
