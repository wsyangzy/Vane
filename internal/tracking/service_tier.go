package tracking

import "strings"

const (
	ServiceTierSourceResponse = "response"
	ServiceTierSourceRequest  = "request"
	ServiceTierSourceRules    = "rules"
	ServiceTierSourceStandard = "standard"
)

func NormalizeServiceTierSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ServiceTierSourceRules:
		return ServiceTierSourceRules
	case ServiceTierSourceStandard:
		return ServiceTierSourceStandard
	case ServiceTierSourceRequest:
		return ServiceTierSourceRequest
	}
	return ServiceTierSourceResponse
}

// Combination rules match tiers; traditional multipliers always use Standard.
func billingServiceTierSource(mult EndpointMultiplier) string {
	if len(mult.BillingRules) > 0 {
		return ServiceTierSourceRules
	}
	return ServiceTierSourceStandard
}

// ResolveBillingServiceTier uses an explicit request tier for combination
// rules. Auto and unknown request values defer to the response. Standard pricing
// keeps the original request and response evidence without using their prices.
func ResolveBillingServiceTier(requested, response, source string) (string, string) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	response = strings.ToLower(strings.TrimSpace(response))
	source = NormalizeServiceTierSource(source)
	if source == ServiceTierSourceStandard {
		return ServiceTierStandard, ServiceTierSourceStandard
	}
	if (source == ServiceTierSourceRequest || source == ServiceTierSourceRules) && requested != "" && NormalizeServiceTierForBilling(requested) != "" {
		return requested, ServiceTierSourceRequest
	}
	if response != "" {
		return response, ServiceTierSourceResponse
	}
	if requested != "" {
		return requested, ServiceTierSourceRequest
	}
	return "", "default"
}

func resolveActiveRequestServiceTier(req *ActiveRequest) {
	if req.Provider == "codex" {
		if len(req.BillingRules) > 0 {
			req.ServiceTierSource = ServiceTierSourceRules
		} else {
			req.ServiceTierSource = ServiceTierSourceStandard
		}
		req.ServiceTier, _ = ResolveBillingServiceTier(req.RequestedServiceTier, req.ResponseServiceTier, req.ServiceTierSource)
	}
}
