package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/wsyangzy/Vane/internal/routing"
)

func claudeFailureIdentity(body []byte) (code, kind, message string) {
	routing.JSONFrames(body, func(frame map[string]any) {
		raw, _ := json.Marshal(frame)
		c, k, _ := routing.ErrorIdentity(raw)
		if c == "" && k == "" {
			return
		}
		code, kind = strings.ToLower(c), strings.ToLower(k)
		var envelope struct {
			Message string `json:"message"`
			Error   *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &envelope)
		message = envelope.Message
		if envelope.Error != nil {
			message = envelope.Error.Message
		}
	})
	return
}

func claudeStructuredFailure(status int, code, kind, message string) (routing.Failure, bool) {
	// Only the known debt response identifies billing. Generic AccessDenied
	// remains a candidate-level permission error.
	if status == http.StatusPaymentRequired || code == "accessdenied" && strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(message), "."), "Current user is in debt") {
		return routing.Failure{Scope: routing.ScopeCredential, Reason: "balance_exhausted"}, true
	}
	for _, identifier := range []string{code, kind} {
		switch identifier {
		case "invalid_api_key", "invalid_access_token", "invalid_token", "authentication_error":
			return routing.Failure{Scope: routing.ScopeCredential, Reason: "credential_rejected"}, true
		case "insufficient_quota", "insufficient_user_quota", "credit_balance_exhausted", "billing_hard_limit_reached", "balance_exhausted":
			return routing.Failure{Scope: routing.ScopeCredential, Reason: "balance_exhausted"}, true
		case "rate_limit_exceeded", "rate_limit_error", "too_many_requests", "rate_limit":
			return routing.Failure{Scope: routing.ScopeModel, Reason: "rate_limited"}, true
		case "usage_limit_reached", "usage_limit_exceeded", "quota_exceeded", "model_usage_limit_reached":
			return routing.Failure{Scope: routing.ScopeModel, Reason: "quota_exhausted"}, true
		case "permission_denied", "permission_error", "insufficient_permissions":
			return routing.Failure{Scope: routing.ScopeCandidate, Reason: "permission_denied"}, true
		}
	}
	return routing.Failure{}, false
}
