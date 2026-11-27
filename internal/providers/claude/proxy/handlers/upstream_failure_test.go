package handlers

import (
	"testing"

	"github.com/wsyangzy/Vane/internal/routing"
)

func TestClaudeUpstreamFailureClassification(t *testing.T) {
	for _, test := range []struct {
		name, body, reason string
		status             int
		scope              routing.FailureScope
		terminal           bool
	}{
		{"token_limit_sse", `data: {"type":"error","error":{"type":"too_many_requests","code":"rate_limit_exceeded"}}` + "\n\n", "rate_limited", 200, routing.ScopeModel, false},
		{"rate_limit_type", `{"error":{"type":"rate_limit_error"}}`, "rate_limited", 429, routing.ScopeModel, false},
		{"unknown_429", `{}`, "rate_limited", 429, routing.ScopeCandidate, false},
		{"known_debt", `{"Code":"AccessDenied","Message":"Current user is in debt."}`, "balance_exhausted", 403, routing.ScopeCredential, false},
		{"payment_required", `{}`, "balance_exhausted", 402, routing.ScopeCredential, false},
		{"balance_code", `{"error":{"code":"insufficient_user_quota"}}`, "balance_exhausted", 403, routing.ScopeCredential, false},
		{"quota_code", `{"error":{"code":"quota_exceeded"}}`, "quota_exhausted", 200, routing.ScopeModel, false},
		{"invalid_key", `{"error":{"type":"authentication_error"}}`, "credential_rejected", 403, routing.ScopeCredential, false},
		{"permission_code", `{"error":{"type":"permission_error"}}`, "permission_denied", 403, routing.ScopeCandidate, false},
		{"unknown_access_denied", `{"Code":"AccessDenied","Message":"Access denied by policy."}`, "permission_scope_unknown", 403, routing.ScopeCandidate, false},
		{"unstructured_debt", `{"error":{"message":"Current user is in debt."}}`, "permission_scope_unknown", 403, routing.ScopeCandidate, false},
		{"permission_mentions_quota", `{"error":{"message":"permission denied; quota not applicable"}}`, "permission_scope_unknown", 403, routing.ScopeCandidate, false},
		{"invalid_request", `{"error":{"type":"invalid_request_error"}}`, "invalid_request", 200, routing.ScopeRequest, true},
		{"model_rejected", `{"error":{"code":"model_access_denied"}}`, "model_access_denied", 403, routing.ScopeModel, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := claudeRouteFailure(&EndpointError{StatusCode: test.status, Body: []byte(test.body)})
			if got.Scope != test.scope || got.Reason != test.reason || got.Terminal != test.terminal {
				t.Fatalf("classification = %+v", got)
			}
		})
	}
}
