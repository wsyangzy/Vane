package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/middleware"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestRoutedClaudeStreamingStateRoundTrip(t *testing.T) {
	var sends atomic.Int32
	h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "x" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"code":"model_not_found"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range []string{
			`{"type":"message_start","message":{"id":"m","model":"y","usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sealed-"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"stream"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		}
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 5})
	for _, body := range []string{
		`{"model":"original","stream":true,"messages":[]}`,
		`{"model":"original","stream":true,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"sealed-stream"}]}]}`,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("stream state rejected: %d %s", rec.Code, rec.Body.String())
		}
	}
	if sends.Load() != 3 {
		t.Fatalf("want X,Y,Y sends; got %d", sends.Load())
	}
}

func TestClaudeAuxiliaryFailureAndAdmissionEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		status    int
		blocked   bool
		streaming bool
	}{{"unsupported_path", 404, false, false}, {"unsupported_method", 405, false, false}, {"admission_cooldown", 0, true, false}, {"admission_cooldown_streaming", 0, true, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			var sends atomic.Int32
			raw := `{"error":{"message":"Invalid URL (POST /v1/alpha/search)","type":"invalid_request_error"}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(scenario.status)
				_, _ = io.WriteString(w, raw)
			}))
			defer server.Close()
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 5}, Endpoints: []config.EndpointConfig{{Name: "source", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true, Timeout: time.Second}}}
			manager := endpoint.NewManager(cfg)
			manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "A"}})
			if err := manager.ManualActivateGroup("A"); err != nil {
				t.Fatal(err)
			}
			pool := routing.NewPoolRuntime(routing.PoolConfig{Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 11, Enabled: true}}}, nil, nil)
			manager.SetPoolRuntime(pool)
			if scenario.blocked {
				if err := pool.Fail(1, 11, "", "", routing.Failure{Scope: routing.ScopeEndpoint, Reason: "network_error"}, time.Time{}); err != nil {
					t.Fatal(err)
				}
			}
			tracker := newLifecycleTestTracker(t)
			h := NewHandler(manager, cfg)
			h.SetUsageTracker(tracker)
			h.monitoringMiddleware = middleware.NewMonitoringMiddleware(manager)
			for i := 0; i < 2; i++ {
				req := httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(fmt.Sprintf(`{"stream":%t,"commands":{"search_query":[{"q":"test"}]}}`, scenario.streaming)))
				req = req.WithContext(context.WithValue(req.Context(), "conn_id", fmt.Sprintf("auxiliary-%s-%d", scenario.name, i)))
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if scenario.blocked {
					if rec.Code != 502 || sends.Load() != 0 {
						t.Fatalf("blocked request sent: status=%d sends=%d", rec.Code, sends.Load())
					}
				} else if rec.Code != scenario.status || rec.Body.String() != raw || sends.Load() != int32(i+1) || len(pool.Cooldowns()) != 0 {
					t.Fatalf("auxiliary response changed or cooled endpoint: status=%d sends=%d cooldowns=%+v body=%s", rec.Code, sends.Load(), pool.Cooldowns(), rec.Body.String())
				}
			}
			if err := tracker.ForceFlush(); err != nil {
				t.Fatal(err)
			}
			details, err := tracker.QueryRequestDetails(context.Background(), &tracking.QueryOptions{Limit: 10})
			if err != nil || len(details) != 2 {
				t.Fatalf("details=%d err=%v", len(details), err)
			}
			for _, detail := range details {
				var hops []ChainNode
				if err := json.Unmarshal([]byte(detail.EndpointChain), &hops); err != nil {
					t.Fatal(err)
				}
				if len(hops) != 1 {
					t.Fatalf("unexpected hops: %+v", hops)
				}
				if scenario.blocked {
					if detail.FailureReason != "account_cooling_down" || detail.HTTPStatusIsUpstream || hops[0].Attempts != 0 || hops[0].Error != "account_cooling_down" {
						t.Fatalf("admission evidence missing: detail=%+v hops=%+v", detail, hops)
					}
				} else if detail.FailureReason != "auxiliary_endpoint_unsupported" || !detail.HTTPStatusIsUpstream || detail.LastFailureReason != raw || hops[0].Attempts != 1 {
					t.Fatalf("upstream evidence missing: %+v hops=%+v", detail, hops)
				}
			}
		})
	}
}

func TestRoutedClaudeCancelAndOutputStopReplay(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var sends atomic.Int32
		h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
			sends.Add(1)
			w.WriteHeader(503)
			cancel()
		}, config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: 5})
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original"}`)).WithContext(ctx))
		if sends.Load() != 1 {
			t.Fatalf("sent after cancellation: %d", sends.Load())
		}
	})
	t.Run("output", func(t *testing.T) {
		var sends atomic.Int32
		h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
			sends.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"x\",\"usage\":{\"input_tokens\":1}}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"overload\"}}\n\n")
		}, config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: 5})
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original","stream":true}`)))
		if sends.Load() != 1 {
			t.Fatalf("replayed committed output: %d", sends.Load())
		}
	})
}

func TestRoutedClaudeLongAcceptedResponseOutlivesRetryBudget(t *testing.T) {
	var sends atomic.Int32
	h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(40 * time.Millisecond)
		_, _ = io.WriteString(w, `{"type":"message","model":"x","content":[{"type":"text","text":"finished"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 1, MaxTotalDuration: 20 * time.Millisecond})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original"}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "finished") || sends.Load() != 1 {
		t.Fatalf("accepted response interrupted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRoutedClaudeFinalExecutionPersistsBeforeArchive(t *testing.T) {
	tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: ":memory:", BufferSize: 10, BatchSize: 10, FlushInterval: time.Hour, MaxRetry: 1, CleanupInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tracker.Close() })
	h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "x" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"code":"model_not_found"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","model":"y","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 2})
	h.SetUsageTracker(tracker)
	h.monitoringMiddleware = middleware.NewMonitoringMiddleware(h.endpointManager)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original"}`))
	req = req.WithContext(context.WithValue(req.Context(), "conn_id", "claude-execution-trace"))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if err := tracker.ForceFlush(); err != nil {
		t.Fatal(err)
	}
	details, err := tracker.QueryRequestDetails(context.Background(), &tracking.QueryOptions{Limit: 10})
	if err != nil || len(details) != 1 {
		t.Fatalf("details=%d err=%v", len(details), err)
	}
	var snapshot routing.Snapshot
	if err := json.Unmarshal([]byte(details[0].RouteSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Attempts != 2 || snapshot.StopReason != "" || snapshot.Execution == nil || len(snapshot.Execution.Events) != 3 {
		t.Fatalf("execution missing before archive: %+v", snapshot)
	}
}

func TestRoutedClaudeEndpointAndCredentialFailuresDoNotRetryAsAnotherModel(t *testing.T) {
	for _, status := range []int{0, 400, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var mu sync.Mutex
			var models []string
			h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				mu.Lock()
				models = append(models, fmt.Sprint(body["model"]))
				mu.Unlock()
				if status == 0 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
					return
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"type":"api_error","message":"request rejected"}}`)
			}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 5})
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original"}`)))
			mu.Lock()
			defer mu.Unlock()
			for _, model := range models {
				if model == "y" {
					t.Fatalf("failure retried as another model: %v", models)
				}
			}
			want := []string{"x", "original"}
			if status == 400 {
				want = []string{"x"}
			}
			if !reflect.DeepEqual(models, want) {
				t.Fatalf("models=%v want=%v", models, want)
			}
		})
	}
}

func TestRoutedClaudeAcceptedLongStreamFinishes(t *testing.T) {
	h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"x\",\"usage\":{\"input_tokens\":1}}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"begin\"}}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 1, MaxTotalDuration: 100 * time.Millisecond})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original","stream":true}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "message_stop") {
		t.Fatalf("long stream interrupted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRoutedClaudeKeepsRealResponseAcrossLaterNetworkFailures(t *testing.T) {
	const upstreamBody = `{"error":{"code":"model_not_found","message":"model X unavailable"}}`
	h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "x" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = io.WriteString(w, upstreamBody)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 5})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original"}`)))
	if rec.Code != 400 || rec.Body.String() != upstreamBody {
		t.Fatalf("real response overwritten: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRoutedClaudeTriesDifferentModelOnSameEndpoint(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			var mu sync.Mutex
			var models []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct{ Model string }
				_ = json.NewDecoder(r.Body).Decode(&payload)
				mu.Lock()
				models = append(models, payload.Model)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if payload.Model == "model-x" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"not_found_error","code":"model_not_found","param":"model","message":"model unavailable"}}`)
					return
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-test\",\"model\":\"model-y\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
					return
				}
				_, _ = io.WriteString(w, `{"id":"msg-test","type":"message","role":"assistant","model":"model-y","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer upstream.Close()
			cfg := &config.Config{
				Retry: config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 4, MaxTotalDuration: time.Second},
				Endpoints: []config.EndpointConfig{
					{Name: "source", URL: upstream.URL, Channel: "A", SkipHealthCheck: true, Timeout: time.Second},
					{Name: "target", URL: upstream.URL, Channel: "B", CredentialGroupID: 7, SkipHealthCheck: true, Timeout: time.Second},
				},
			}
			manager := endpoint.NewManager(cfg)
			if err := manager.ManualActivateGroup("A"); err != nil {
				t.Fatal(err)
			}
			manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "A", 2: "B"}, ModelRules: map[string][]routing.ModelRule{"A": {{ID: 1, MatchModel: "original", Mode: routing.ModePrefer, Targets: []routing.Target{{ChannelID: 2, GroupID: 7, UpstreamModel: "model-x"}, {ChannelID: 2, GroupID: 7, UpstreamModel: "model-y"}}}}}})
			handler := NewHandler(manager, cfg)
			body := fmt.Sprintf(`{"model":"original","stream":%t,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`, streaming)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
			mu.Lock()
			got := append([]string(nil), models...)
			mu.Unlock()
			if !reflect.DeepEqual(got, []string{"model-x", "model-y"}) {
				t.Fatalf("model sends = %v; want X then Y; status=%d body=%s", got, recorder.Code, recorder.Body.String())
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if manager.ActiveChannelName() != "A" {
				t.Fatal("request route changed global activation")
			}
		})
	}
}

func newRoutedClaudeExecutionFixture(t *testing.T, upstreamHandler http.HandlerFunc, retry config.RetryConfig) *Handler {
	t.Helper()
	upstream := httptest.NewServer(upstreamHandler)
	t.Cleanup(upstream.Close)
	cfg := &config.Config{Retry: retry, Endpoints: []config.EndpointConfig{{Name: "source", URL: upstream.URL, Channel: "A", SkipHealthCheck: true, Timeout: time.Second}, {Name: "target", URL: upstream.URL, Channel: "B", CredentialGroupID: 7, SkipHealthCheck: true, Timeout: time.Second}}}
	manager := endpoint.NewManager(cfg)
	if err := manager.ManualActivateGroup("A"); err != nil {
		t.Fatal(err)
	}
	manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "A", 2: "B"}, ModelRules: map[string][]routing.ModelRule{"A": {{ID: 1, MatchModel: "original", Mode: routing.ModePrefer, Targets: []routing.Target{{ChannelID: 2, GroupID: 7, UpstreamModel: "x"}, {ChannelID: 2, GroupID: 7, UpstreamModel: "y"}}}}}})
	return NewHandler(manager, cfg)
}

func TestRoutedClaudeInternalRetriesShareAttemptBudget(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			var sends atomic.Int32
			h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"unavailable"}}`)
			}, config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: 2, MaxTotalDuration: time.Second})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(fmt.Sprintf(`{"model":"original","stream":%t,"max_tokens":16,"messages":[]}`, streaming))))
			if sends.Load() > 2 || sends.Load() == 0 {
				t.Fatalf("sends=%d", sends.Load())
			}
		})
	}
}

func TestRoutedClaudeSealedStateKeepsActualWriter(t *testing.T) {
	var sends atomic.Int32
	h := newRoutedClaudeExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body["model"] == "x" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"type":"not_found_error","code":"model_not_found"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"m","type":"message","model":"y","content":[{"type":"thinking","thinking":"reason","signature":"sealed"},{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 5})
	for i, caller := range []string{"a", "a", "b"} {
		body := `{"model":"original","messages":[]}`
		if i > 0 {
			body = `{"model":"original","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"sealed"}]}]}`
		}
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req = req.WithContext(routing.WithCallerScope(req.Context(), caller))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("writer rejected: %d %s", rec.Code, rec.Body.String())
		}
	}
	if sends.Load() != 5 {
		t.Fatalf("expected X,Y,Y then caller B's own X,Y; got %d sends", sends.Load())
	}
}
