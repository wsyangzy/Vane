package subscriptionprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProbeEvidencePreservesProtocolUncertaintyAndNeverIncludesSecrets(t *testing.T) {
	registry := NewRegistry()
	_, key, _ := registry.Create("caller-secret-sentinel")
	for _, tc := range []struct {
		name, response string
		status         int
		observed, ok   bool
	}{
		{"message", `{"type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"response-secret-sentinel"}]}`, 200, true, true},
		{"stream", "data: {\"type\":\"message_stop\"}\n\n", 200, true, true},
		{"incomplete_stream", "data: {\"type\":\"content_block_delta\"}\n\n", 200, false, false},
		{"protocol_failure", "data: {\"type\":\"error\",\"error\":{\"message\":\"response-secret-sentinel\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n", 200, true, false},
		{"unauthorized", `{"error":{"type":"authentication_error","message":"response-secret-sentinel"}}`, 401, true, false},
		{"bounded_observation", strings.Repeat("x", 1024*1024+1), 200, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got Evidence
			probe := New(registry, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			}), nil, "", WithEvidence(func(e Evidence) { got = e }))
			body := `{"stream":true,"messages":[{"content":"request-secret-sentinel"},{"content":[{"type":"tool_result","content":"tool-secret-sentinel"}]}],"model":"model-secret-sentinel"}`
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
			req.Header.Set("X-Relay-Key", key)
			req.Header.Set("Authorization", "Bearer oauth-secret-sentinel")
			rec := httptest.NewRecorder()
			probe.ServeHTTP(rec, req)
			if rec.Code != tc.status || rec.Body.String() != tc.response {
				t.Fatal("evidence observation changed upstream response")
			}
			if !got.OfficialAttempt || !got.ResponseComplete || got.ProtocolObserved != tc.observed || got.ProtocolSuccess != tc.ok || !got.Streaming || !got.ToolResult || got.UpstreamStatus != tc.status || got.ObservedAt.IsZero() {
				t.Fatalf("incorrect evidence: %+v", got)
			}
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "secret-sentinel") || strings.Contains(string(data), key) {
				t.Fatal("evidence leaked credentials or content")
			}
		})
	}
}

func TestProbeAttemptLimitIsGlobalConcurrentAndDoesNotCountLocalRejections(t *testing.T) {
	registry := NewRegistry()
	_, keyA, _ := registry.Create("a")
	_, keyB, _ := registry.Create("b")
	var sends, refused atomic.Int32
	probe := New(registry, roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"input_tokens":1}`))}, nil
	}), nil, "", WithAttemptLimit(3), WithEvidence(func(e Evidence) {
		if e.Outcome == "probe_send_limit" {
			refused.Add(1)
			if e.OfficialAttempt || e.UpstreamStatus != 0 || e.ProtocolSuccess {
				t.Error("local send limit was represented as an upstream failure")
			}
		}
	}))
	// Invalid clients must not consume the bounded allowance.
	probe.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`)))
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(`{}`))
			req.Header.Set("X-Relay-Key", []string{keyA, keyB}[i%2])
			req.Header.Set("Authorization", "Bearer sentinel")
			probe.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
	if sends.Load() != 3 || refused.Load() != 21 {
		t.Fatalf("sends=%d refused=%d", sends.Load(), refused.Load())
	}
}

func TestProbeEvidenceKeepsTransportFailureAndCancelledRequestLocal(t *testing.T) {
	registry := NewRegistry()
	_, key, _ := registry.Create("caller")
	var got Evidence
	var sends int
	probe := New(registry, roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends++
		return nil, errors.New("transport-secret-sentinel")
	}), nil, "", WithEvidence(func(e Evidence) { got = e }), WithAttemptLimit(1))
	request := func(ctx context.Context) *http.Request {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`)).WithContext(ctx)
		req.Header.Set("X-Relay-Key", key)
		req.Header.Set("Authorization", "Bearer sentinel")
		return req
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe.ServeHTTP(httptest.NewRecorder(), request(ctx))
	if sends != 0 || got.Outcome != "client_cancelled" || got.OfficialAttempt {
		t.Fatalf("cancelled request sent: %+v", got)
	}
	probe.ServeHTTP(httptest.NewRecorder(), request(context.Background()))
	if sends != 1 || got.UpstreamStatus != 0 || got.ProtocolObserved || got.Outcome != "upstream_transport_failed" {
		t.Fatalf("transport failure fabricated upstream status: %+v", got)
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "secret-sentinel") {
		t.Fatal("transport error leaked into evidence")
	}
}
