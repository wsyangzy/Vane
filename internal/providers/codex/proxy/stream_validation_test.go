package codexproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/tracking"
)

const diagnosticCompletedSSE = "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":8,\"output_tokens\":2}}}\n\n"

func TestCodexInvalidStreamBeforeOutputFallsBackWithoutLeakingPrelude(t *testing.T) {
	cases := []struct{ name, mime, body string }{
		{"html", "text/html", "<html>verify access</html>"},
		{"json_error", "application/json", `{"error":{"type":"server_error","message":"maintenance"}}`},
		{"flat_json_error", "application/json", `{"type":"error","code":"server_error","message":"maintenance"}`},
		{"json_completion", "application/json", `{"status":"completed","output":[]}`},
		{"plain_text", "text/plain", "temporarily unavailable"},
		{"empty", "text/event-stream", ""},
		{"heartbeat_only", "text/event-stream", ": heartbeat\n\n"},
		{"malformed_data", "text/event-stream", "data: not-json\n\n"},
		{"unknown_only", "text/event-stream", "data: {\"type\":\"vendor.extension\",\"message\":\"response.completed\"}\n\n"},
		{"created_only", "text/event-stream", "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\"}}\n\n"},
		{"completion_word", "text/event-stream", "data: {\"message\":\"response.completed\"}\n\n"},
		{"event_without_json", "text/event-stream", "event: response.completed\ndata: malformed\n\n"},
		{"failed", "text/event-stream", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n"},
		{"incomplete", "text/event-stream", "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n"},
		{"prelude_limit", "text/event-stream", strings.Repeat(": heartbeat\n\n", maxPreOutputStreamBufferBytes/10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if strings.HasPrefix(r.URL.Path, "/first/") {
					w.Header().Set("Content-Type", tc.mime)
					io.WriteString(w, tc.body)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, diagnosticCompletedSSE)
			})
			h.UpdateConfig(config.RetryConfig{DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`)))
			if calls != 2 || !trace.Successful || rec.Body.String() != diagnosticCompletedSSE || rec.Code != 200 {
				t.Fatalf("invalid prelude reached client or prevented fallback: calls=%d trace=%+v code=%d body=%q", calls, trace, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestValidateDiagnosticStreamReturnsStructuredFailureReason(t *testing.T) {
	for _, tc := range []struct{ body, reason string }{
		{`{"error":{"type":"server_error","message":"busy"}}`, "server_error"},
		{`{"type":"error","code":"insufficient_quota","message":"quota exhausted"}`, "insufficient_quota"},
		{"data: {\"type\":\"error\",\"code\":\"model_not_found\",\"message\":\"unknown model\"}\n\n", "model_not_found"},
		{"data: not-json\n\n", "invalid_stream_event"},
		{"", "empty_stream"},
		{": heartbeat\n\n", "incomplete_stream"},
	} {
		t.Run(tc.reason+tc.body, func(t *testing.T) {
			complete, failure := ValidateDiagnosticStream([]byte(tc.body))
			if complete || failure != tc.reason {
				t.Fatalf("wrong diagnostic failure: complete=%t failure=%q want=%q", complete, failure, tc.reason)
			}
		})
	}
	streamErr := NewTokenParser().ParseStreamingErrorLine([]byte(`data: {"type":"error","code":"model_not_found","message":"unknown model"}`))
	if streamErr == nil || streamErr.Code != "model_not_found" || streamErr.Type != "error" {
		t.Fatalf("flat Responses error identity lost: %+v", streamErr)
	}
	if streamErr.InvalidProtocol {
		t.Fatal("valid upstream SSE error marked as invalid protocol")
	}
	jsonErr := codexStreamPreludeError([]byte(`{"error":{"type":"server_error","code":"provider_busy","message":"busy"}}`))
	if !jsonErr.InvalidProtocol || jsonErr.Type != "server_error" || jsonErr.Code != "provider_busy" {
		t.Fatalf("JSON protocol failure lost provider identity: %+v", jsonErr)
	}
}

func TestCodexStreamProtocolCompatibilityAndExactForwarding(t *testing.T) {
	cases := []struct{ name, mime, body string }{
		{"wrong_content_type", "text/plain", diagnosticCompletedSSE},
		{"missing_content_type", "", diagnosticCompletedSSE},
		{"multiline_data", "text/event-stream", ": heartbeat\n\nevent: response.completed\ndata: {\"response\":\ndata: {\"status\":\"completed\",\"output\":[]}}\n\n"},
		{"unknown_before_valid", "text/event-stream", "data: {\"type\":\"vendor.extension\",\"message\":\"output_text delta\"}\n\n" + diagnosticCompletedSSE},
		{"crlf", "text/event-stream", strings.ReplaceAll(diagnosticCompletedSSE, "\n", "\r\n")},
		{"no_trailing_blank_line", "text/event-stream", strings.TrimRight(diagnosticCompletedSSE, "\n")},
		{"chat_completions", "text/event-stream", "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if tc.mime != "" {
					w.Header().Set("Content-Type", tc.mime)
				}
				io.WriteString(w, tc.body)
			})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`)))
			if calls != 1 || !trace.Successful || rec.Body.String() != tc.body {
				t.Fatalf("accepted protocol changed: calls=%d trace=%+v body=%q", calls, trace, rec.Body.String())
			}
			if complete, failure := ValidateDiagnosticStream([]byte(tc.body)); !complete || failure != "" {
				t.Fatalf("probe disagrees with live stream: complete=%t failure=%s", complete, failure)
			}
		})
	}
}

func TestCodexInvalidStreamExhaustionKeepsSourceAndReturnsSSE(t *testing.T) {
	for _, raw := range []string{"<html>maintenance</html>", `{"status":"completed","output":[]}`, `{"error":{"type":"server_error","message":"busy"}}`, "", "data: not-json\r\n\r\n"} {
		t.Run(raw, func(t *testing.T) {
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { tracker.Close() })
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Header().Set("X-Upstream-Marker", "invalid-protocol")
				io.WriteString(w, raw)
			})
			h.usageTracker = tracker
			h.UpdateConfig(config.RetryConfig{DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`)))
			assertCodexInvalidStreamTerminal(t, rec)
			if trace.Successful || trace.ResponseStarted || trace.Attempts != 2 {
				t.Fatalf("upstream response provenance changed: trace=%+v code=%d body=%q", trace, rec.Code, rec.Body.String())
			}
			details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 10})
			if err != nil || len(details) != 1 {
				t.Fatalf("request missing: %v", err)
			}
			var chain []chainNode
			if err = json.Unmarshal([]byte(details[0].EndpointChain), &chain); err != nil {
				t.Fatal(err)
			}
			if !details[0].HTTPStatusIsUpstream || details[0].HTTPStatusCode == nil || *details[0].HTTPStatusCode != 200 || len(chain) != 2 || chain[0].StatusCode != 200 || chain[1].StatusCode != 200 || chain[1].Headers["content-type"] != "text/plain" {
				t.Fatalf("actual upstream status lost: detail=%+v chain=%+v", details[0], chain)
			}
			if raw != "" && (chain[1].Error != strings.TrimSpace(raw) || details[0].LastFailureReason != raw) {
				t.Fatalf("raw source changed: want=%q chain=%q detail=%q", raw, chain[1].Error, details[0].LastFailureReason)
			}
		})
	}
}

func assertCodexInvalidStreamTerminal(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadGateway || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("invalid stream leaked to client: code=%d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
	}
	var envelope struct {
		Type  string `json:"type"`
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	data := strings.TrimSpace(strings.TrimPrefix(rec.Body.String(), "event: error\ndata: "))
	if err := json.Unmarshal([]byte(data), &envelope); err != nil || envelope.Type != "error" || envelope.Error.Type != "overloaded_error" || envelope.Error.Code != "invalid_stream_response" {
		t.Fatalf("terminal response is not a complete SSE error: body=%q err=%v", rec.Body.String(), err)
	}
}

func newCodexStreamTerminalFixture(t *testing.T, kind string, upstream http.HandlerFunc, retry config.RetryConfig) *Handler {
	t.Helper()
	if kind == "profile" {
		h, _, _ := newProfileCodexFixture(t, upstream)
		h.UpdateConfig(retry, config.FailoverConfig{Enabled: true})
		return h
	}
	if kind == "routed" {
		return newRoutedCodexExecutionFixture(t, upstream, retry, "x", "y")
	}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{ID: 1, Name: "first", CredentialGroupName: "default", BaseURL: server.URL + "/first/v1", APIKey: "test", FailoverEnabled: true, SkipHealthCheck: true},
			{ID: 2, Name: "second", CredentialGroupName: "default", BaseURL: server.URL + "/second/v1", APIKey: "test", FailoverEnabled: true, SkipHealthCheck: true},
		},
		unhealthyMap: map[int64]bool{}, failureDetails: map[int64]EndpointFailureDetail{}, logger: newTestLogger(),
	}
	return NewHandler(manager, time.Second, retry, config.FailoverConfig{}, nil, newTestLogger(), nil)
}

func TestCodexInvalidStreamTerminalAcrossDispatchPaths(t *testing.T) {
	for _, kind := range []string{"profile", "legacy", "routed"} {
		for _, raw := range []string{"<html>verification</html>", `{"status":"completed","output":[]}`, `{"error":{"type":"server_error","message":"busy"}}`, "", "data: broken\n\n"} {
			t.Run(kind+raw, func(t *testing.T) {
				calls := 0
				h := newCodexStreamTerminalFixture(t, kind, func(w http.ResponseWriter, _ *http.Request) {
					calls++
					w.Header().Set("Content-Type", "text/plain")
					io.WriteString(w, raw)
				}, config.RetryConfig{MaxAttempts: 1, DisableRecoveryWait: true})
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","stream":true}`)))
				assertCodexInvalidStreamTerminal(t, rec)
				if calls != 2 {
					t.Fatalf("expected both candidates before terminal response, calls=%d", calls)
				}
			})
		}
	}
}

func TestCodexInvalidStreamTerminalKeepsSourceAcrossBudgetAndNetworkFailure(t *testing.T) {
	const raw = "<html>original upstream body</html>"
	for _, kind := range []string{"profile", "legacy", "routed"} {
		for _, mode := range []string{"network", "budget"} {
			if kind == "legacy" && mode == "budget" {
				continue // Legacy requests have no request-scoped dispatch budget.
			}
			t.Run(kind+mode, func(t *testing.T) {
				tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { tracker.Close() })
				calls := 0
				retry := config.RetryConfig{MaxAttempts: 1, DisableRecoveryWait: true}
				if mode == "budget" {
					retry.MaxTotalAttempts = 1
				}
				h := newCodexStreamTerminalFixture(t, kind, func(w http.ResponseWriter, _ *http.Request) {
					calls++
					if calls > 1 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							conn.Close()
						}
						return
					}
					w.Header().Set("X-Upstream-Marker", "source")
					w.Header().Set("Connection", "close")
					io.WriteString(w, raw)
				}, retry)
				h.usageTracker = tracker
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","stream":true}`)))
				assertCodexInvalidStreamTerminal(t, rec)
				details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 10})
				if err != nil || len(details) != 1 {
					t.Fatalf("request missing: %v", err)
				}
				var chain []chainNode
				if err := json.Unmarshal([]byte(details[0].EndpointChain), &chain); err != nil {
					t.Fatal(err)
				}
				if !details[0].HTTPStatusIsUpstream || details[0].HTTPStatusCode == nil || *details[0].HTTPStatusCode != 200 || len(chain) == 0 || chain[0].StatusCode != 200 || chain[0].Error != raw || details[0].LastFailureReason != raw {
					t.Fatalf("last real upstream response lost: calls=%d detail=%+v chain=%+v", calls, details[0], chain)
				}
				if mode == "budget" && (calls != 1 || details[0].FailureReason != "retry_attempts_exhausted") {
					t.Fatalf("budget classification replaced source or allowed another send: calls=%d detail=%+v", calls, details[0])
				}
				if mode == "network" && calls != 2 {
					t.Fatalf("network failure scenario did not try second candidate: calls=%d", calls)
				}
			})
		}
	}
}

func TestCodexStreamTerminalPreservesSSEAndNonStreamingResponses(t *testing.T) {
	const rawSSE = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"message\":\"busy\"}}\n\n"
	for _, kind := range []string{"profile", "legacy", "routed"} {
		for _, tc := range []struct {
			name, body, mime string
			streaming        bool
			status           int
		}{
			{"sse", rawSSE, "text/event-stream", true, 200},
			{"invalid_before_sse", rawSSE, "text/event-stream", true, 200},
			{"json_error", `{"error":{"type":"server_error","message":"busy"}}`, "application/json", false, 503},
			{"json_success", `{"status":"completed","output":[]}`, "application/json", false, 200},
		} {
			t.Run(kind+tc.name, func(t *testing.T) {
				calls := 0
				h := newCodexStreamTerminalFixture(t, kind, func(w http.ResponseWriter, _ *http.Request) {
					calls++
					if calls == 1 && tc.name == "invalid_before_sse" {
						io.WriteString(w, "<html>bad</html>")
						return
					}
					w.Header().Set("Content-Type", tc.mime)
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.body)
				}, config.RetryConfig{MaxAttempts: 1, DisableRecoveryWait: true})
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"original","stream":%t}`, tc.streaming))))
				if rec.Code != tc.status || rec.Body.String() != tc.body || rec.Header().Get("Content-Type") != tc.mime {
					t.Fatalf("existing response fidelity changed: code=%d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
				}
			})
		}
	}
}

func TestCodexStreamFailureAfterOutputNeverReplays(t *testing.T) {
	const delta = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
	for _, ending := range []string{"", "data: not-json\n\n", "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n"} {
		t.Run(ending, func(t *testing.T) {
			calls := 0
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				io.WriteString(w, delta+ending)
			})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`)))
			if calls != 1 || trace.Successful || !trace.ResponseStarted || rec.Body.String() != delta+ending {
				t.Fatalf("started request replayed or changed: calls=%d trace=%+v body=%q", calls, trace, rec.Body.String())
			}
		})
	}
}

func TestCodexStreamFailureAfterOutputKeepsObservedUsage(t *testing.T) {
	tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tracker.Close() })
	calls := 0
	h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"+
			"data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"model\":\"test\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n")
	})
	h.usageTracker = tracker
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`)))
	details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 10})
	if err != nil || len(details) != 1 {
		t.Fatalf("request missing: %v", err)
	}
	if calls != 1 || trace.Successful || details[0].InputTokens != 10 || details[0].OutputTokens != 2 || details[0].Status != "failed" {
		t.Fatalf("stream error lost observed usage or replayed: calls=%d trace=%+v detail=%+v", calls, trace, details[0])
	}
}
