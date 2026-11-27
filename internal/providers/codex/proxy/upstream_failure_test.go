package codexproxy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCodexUpstreamFailureClassification(t *testing.T) {
	for _, test := range []struct {
		name, body, reason string
		status             int
		scope              routing.FailureScope
		terminal           bool
	}{
		{"token_limit_sse", `data: {"type":"error","error":{"type":"too_many_requests","code":"rate_limit_exceeded","message":"Your requests have exceeded token rate limit."}}` + "\n\n", "rate_limited", 200, routing.ScopeModel, false},
		{"nested_response_limit", `{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded"}}}`, "rate_limited", 200, routing.ScopeModel, false},
		{"rate_limit_type", `{"error":{"type":"rate_limit_error"}}`, "rate_limited", 429, routing.ScopeModel, false},
		{"unknown_429", `{"error":{"message":"try again"}}`, "rate_limited", 429, routing.ScopeCandidate, false},
		{"known_debt", `{"Code":"AccessDenied","Message":"Current user is in debt."}`, "balance_exhausted", 403, routing.ScopeCredential, false},
		{"payment_required", `{}`, "balance_exhausted", 402, routing.ScopeCredential, false},
		{"balance_code", `{"error":{"code":"insufficient_user_quota"}}`, "balance_exhausted", 403, routing.ScopeCredential, false},
		{"quota_code", `{"error":{"code":"usage_limit_exceeded"}}`, "quota_exhausted", 200, routing.ScopeModel, false},
		{"invalid_key", `{"error":{"code":"invalid_api_key"}}`, "credential_rejected", 403, routing.ScopeCredential, false},
		{"permission_code", `{"error":{"type":"permission_error"}}`, "permission_denied", 403, routing.ScopeCandidate, false},
		{"unknown_access_denied", `{"Code":"AccessDenied","Message":"Access denied by policy."}`, "permission_scope_unknown", 403, routing.ScopeCandidate, false},
		{"unstructured_debt", `{"error":{"message":"Current user is in debt."}}`, "permission_scope_unknown", 403, routing.ScopeCandidate, false},
		{"permission_mentions_quota", `{"error":{"message":"permission denied; quota not applicable"}}`, "permission_scope_unknown", 403, routing.ScopeCandidate, false},
		{"message_mentions_invalid_request", `{"error":{"type":"server_error","message":"backend invalid_request counter is unavailable"}}`, "upstream_unavailable", 200, routing.ScopeCandidate, false},
		{"invalid_request", `{"error":{"type":"invalid_request_error"}}`, "invalid_request", 200, routing.ScopeRequest, true},
		{"invalid_request_400", `{"error":{"message":"rate limit words in validation failure"}}`, "invalid_request", 400, routing.ScopeRequest, true},
		{"model_rejected", `{"error":{"code":"model_access_denied"}}`, "model_access_denied", 403, routing.ScopeModel, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := codexRouteFailure(test.status, []byte(test.body), nil)
			if got.Scope != test.scope || got.Reason != test.reason || got.Terminal != test.terminal {
				t.Fatalf("classification = %+v", got)
			}
		})
	}
	wrapped := fmt.Errorf("stream: %w", &UpstreamStreamError{Code: "rate_limit_exceeded", Type: "too_many_requests"})
	if got := codexRouteFailure(200, nil, wrapped); got.Scope != routing.ScopeModel || got.Reason != "rate_limited" {
		t.Fatalf("wrapped stream lost identity: %+v", got)
	}
	if got := codexRouteFailure(0, nil, errors.New("unexpected EOF")); got.Scope != routing.ScopeEndpoint || got.Reason != "network_error" {
		t.Fatalf("network classification = %+v", got)
	}
	if got := classifyStreamError(&UpstreamStreamError{Type: "server_error", Message: "backend invalid_request counter is unavailable"}); got != ActionFailover {
		t.Fatal("message text made a server error terminal")
	}
}

func TestCodexProfileUpstreamFailuresSkipSharedCredentialEndpoints(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, test := range []struct {
			name, body, reason string
			status             int
			repair             bool
		}{
			{"rate_limit", `{"error":{"type":"too_many_requests","code":"rate_limit_exceeded"}}`, "rate_limited", 429, false},
			{"debt", `{"Code":"AccessDenied","Message":"Current user is in debt."}`, "balance_exhausted", 403, true},
			{"sse_rate_limit", `{"type":"error","error":{"type":"too_many_requests","code":"rate_limit_exceeded"}}`, "rate_limited", 200, false},
		} {
			if !streaming && test.name == "sse_rate_limit" {
				continue
			}
			t.Run(fmt.Sprintf("%s/stream=%t", test.name, streaming), func(t *testing.T) {
				var paths []string
				raw := fmt.Sprintf(`{"model":"test","stream":%t,"input":[{"role":"user","content":"hello"}],"tools":[],"reasoning":{"effort":"high"}}`, streaming)
				h, runtime, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
					paths = append(paths, r.URL.Path)
					body, _ := io.ReadAll(r.Body)
					if string(body) != raw {
						t.Error("request body changed during failover")
					}
					if strings.HasPrefix(r.URL.Path, "/first/") {
						w.Header().Set("Retry-After", "120")
						if test.name == "sse_rate_limit" {
							w.Header().Set("Content-Type", "text/event-stream")
						}
						w.WriteHeader(test.status)
						if test.name == "sse_rate_limit" {
							io.WriteString(w, "data: "+test.body+"\n\n")
						} else {
							io.WriteString(w, test.body)
						}
						return
					}
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
					} else {
						io.WriteString(w, `{"status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
					}
				})
				sibling := *h.manager.endpointsByChannel[1][0]
				sibling.ID, sibling.Name = 3, "sibling"
				h.manager.endpointsByChannel[1] = append(h.manager.endpointsByChannel[1], &sibling)
				profile := routing.DefaultProfile("codex")
				profile.ID, profile.Name, profile.IsDefault, profile.Strategy = 1, "profile", true, routing.StrategyOrder
				profile.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}, {ChannelID: 2, GroupID: 22, Enabled: true}}
				runtime.Replace(routing.RuntimeConfig{
					Provider: "codex", Profiles: []routing.RoutingProfile{profile},
					Candidates: []routing.DispatchCandidate{
						{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true},
						{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "3", Protocol: "responses", Enabled: true},
						{Provider: "codex", ChannelID: 2, GroupID: 22, EndpointID: "2", Protocol: "responses", Enabled: true},
					},
					PrepareCandidate: h.manager.PrepareDispatchCandidate, Supports: h.manager.SupportsDispatchCandidate,
					OnTrace: func(value routing.DispatchTrace) { *trace = value },
				})
				for i := 0; i < 2; i++ {
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(raw)))
					if rec.Code != 200 || !trace.Successful {
						t.Fatalf("failover failed: %d %+v", rec.Code, trace)
					}
				}
				if !reflect.DeepEqual(paths, []string{"/first/v1/responses", "/second/v1/responses", "/second/v1/responses"}) {
					t.Fatalf("credential failure was retried: %v", paths)
				}
				cooldowns := h.manager.PoolRuntime().Cooldowns()
				if len(cooldowns) != 1 || cooldowns[0].Reason != test.reason || cooldowns[0].Repair != test.repair || cooldowns[0].Endpoint != "" {
					t.Fatalf("wrong recovery scope: %+v", cooldowns)
				}
			})
		}
	}
}
