package codexproxy

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
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestRoutedCodexTriesDifferentModelOnSameEndpoint(t *testing.T) {
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
					_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","code":"model_not_found","param":"model","message":"model unavailable"}}`)
					return
				}
				response := `{"id":"resp-test","status":"completed","model":"model-y","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
					return
				}
				_, _ = io.WriteString(w, response)
			}))
			defer upstream.Close()
			a := &store.CodexChannelRecord{ID: 1, Name: "A", FailoverEnabled: true}
			b := &store.CodexChannelRecord{ID: 2, Name: "B", FailoverEnabled: true}
			ep := &CodexEndpoint{ID: 20, ChannelID: 2, CredentialGroupID: 7, Name: "target", BaseURL: upstream.URL, APIKey: "test-key", FailoverEnabled: true, Timeout: time.Second}
			manager := &EndpointManager{activeChannel: a, channelsByID: map[int64]*store.CodexChannelRecord{1: a, 2: b}, endpointsByChannel: map[int64][]*CodexEndpoint{2: {ep}}, unhealthyMap: make(map[int64]bool), failureDetails: make(map[int64]EndpointFailureDetail), logger: newTestLogger()}
			manager.SetModelRules([]routing.ModelRule{{ID: 1, SourceChannelID: 1, MatchModel: "original", Mode: routing.ModePrefer, Targets: []routing.Target{{ChannelID: 2, GroupID: 7, UpstreamModel: "model-x"}, {ChannelID: 2, GroupID: 7, UpstreamModel: "model-y"}}}})
			handler := NewHandler(manager, time.Second, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 4, MaxTotalDuration: time.Second}, config.FailoverConfig{}, nil, newTestLogger(), nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"original","stream":%t,"input":"hello"}`, streaming))))
			mu.Lock()
			got := append([]string(nil), models...)
			mu.Unlock()
			if !reflect.DeepEqual(got, []string{"model-x", "model-y"}) {
				t.Fatalf("model sends=%v want X then Y; status=%d body=%s", got, recorder.Code, recorder.Body.String())
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if manager.GetActiveChannel().ID != 1 || manager.preferredEndpointID != 0 {
				t.Fatal("request route mutated global activation/preference")
			}
		})
	}
}

func TestCodexSealedFunctionArgumentsKeepTheirIssuingCandidate(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/responses", nil)
	body := []byte(`{"input":[{"type":"function_call","encrypted_function_args":["sealed-argument"]}]}`)
	keys := codexStateKeys(r, body)
	if len(keys) != 1 {
		t.Fatal("sealed function arguments unprotected")
	}
	tracker := routing.NewStateTracker()
	owner := routing.Candidate{Provider: "codex", Credential: "account-a"}
	tracker.Observe(keys, "", owner)
	if got, ok := tracker.Preference(keys, ""); !ok || got != owner {
		t.Fatal("issuing account was not preferred")
	}
	if len(codexStateKeys(r, []byte(`{"input":[{"type":"function_call","encrypted_function_args":[]} ]}`))) != 0 {
		t.Fatal("unsealed function call treated as opaque")
	}
}

func newRoutedCodexExecutionFixture(t *testing.T, upstreamHandler http.HandlerFunc, retry config.RetryConfig, models ...string) *Handler {
	t.Helper()
	upstream := httptest.NewServer(upstreamHandler)
	t.Cleanup(upstream.Close)
	a := &store.CodexChannelRecord{ID: 1, Name: "A", FailoverEnabled: true}
	b := &store.CodexChannelRecord{ID: 2, Name: "B", FailoverEnabled: true}
	ep := &CodexEndpoint{ID: 20, ChannelID: 2, CredentialGroupID: 7, Name: "target", BaseURL: upstream.URL, APIKey: "test-key", FailoverEnabled: true, Timeout: time.Second}
	manager := &EndpointManager{activeChannel: a, channelsByID: map[int64]*store.CodexChannelRecord{1: a, 2: b}, endpointsByChannel: map[int64][]*CodexEndpoint{2: {ep}}, unhealthyMap: make(map[int64]bool), failureDetails: make(map[int64]EndpointFailureDetail), logger: newTestLogger()}
	var targets []routing.Target
	for _, model := range models {
		targets = append(targets, routing.Target{ChannelID: 2, GroupID: 7, UpstreamModel: model})
	}
	manager.SetModelRules([]routing.ModelRule{{ID: 1, SourceChannelID: 1, MatchModel: "original", Mode: routing.ModePrefer, Targets: targets}})
	return NewHandler(manager, time.Second, retry, config.FailoverConfig{}, nil, newTestLogger(), nil)
}

func TestRoutedCodexBudgetCoversInternalRetriesAndRetryAfter(t *testing.T) {
	for _, status := range []int{408, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var sends atomic.Int32
			h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				if status == 429 {
					w.Header().Set("Retry-After", "3600")
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"code":"temporary","message":"retry"}}`)
			}, config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: 2, MaxTotalDuration: 20 * time.Millisecond}, "x", "y")
			rec := httptest.NewRecorder()
			start := time.Now()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","input":"hello"}`)))
			if sends.Load() > 2 || sends.Load() == 0 {
				t.Fatalf("sends=%d", sends.Load())
			}
			if status == 429 && sends.Load() != 1 {
				t.Fatal("Retry-After sent again before budget")
			}
			if time.Since(start) > time.Second {
				t.Fatal("budget did not bound retry wait")
			}
			if rec.Code != status {
				t.Fatalf("lost real response: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRoutedCodexCancelWaitAndNeverReplayAfterOutput(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var sends atomic.Int32
		h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
			sends.Add(1)
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
			cancel()
		}, config.RetryConfig{MaxAttempts: 3, MaxTotalAttempts: 5, MaxTotalDuration: time.Hour}, "x", "y")
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original"}`)).WithContext(ctx)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if sends.Load() != 1 {
			t.Fatalf("sends after cancel=%d", sends.Load())
		}
	})
	t.Run("output", func(t *testing.T) {
		var sends atomic.Int32
		h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
			sends.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\nevent: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\"}}}\n\n")
		}, config.RetryConfig{MaxAttempts: 3, MaxTotalAttempts: 5}, "x", "y")
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","stream":true}`)))
		if sends.Load() != 1 {
			t.Fatalf("replayed after output, sends=%d", sends.Load())
		}
	})
}

func TestRoutedCodexSealedStateUsesActualWriterAndCaller(t *testing.T) {
	var sends atomic.Int32
	h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body["model"] == "x" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"code":"model_not_found"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"response-owned-by-y","status":"completed","output":[{"type":"reasoning","encrypted_content":"sealed"}]}`)
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 5}, "x", "y")
	for i, caller := range []string{"caller-a", "caller-a", "caller-b"} {
		body := `{"model":"original"}`
		if i > 0 {
			body = `{"model":"original","previous_response_id":"response-owned-by-y","input":[{"type":"reasoning","encrypted_content":"sealed"}]}`
		}
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		req = req.WithContext(routing.WithCallerScope(req.Context(), caller))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("writer should remain usable: %d %s", rec.Code, rec.Body.String())
		}
	}
	if sends.Load() != 5 {
		t.Fatalf("expected X,Y then only Y, then caller B's own X,Y; got %d", sends.Load())
	}
}

func TestRoutedCodexStreamingStateRoundTrip(t *testing.T) {
	var sends atomic.Int32
	h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "x" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"code":"model_not_found"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"stream-y\",\"status\":\"completed\",\"model\":\"y\",\"output\":[{\"type\":\"reasoning\",\"encrypted_content\":\"sealed-stream\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 5}, "x", "y")
	for _, body := range []string{
		`{"model":"original","stream":true}`,
		`{"model":"original","stream":true,"previous_response_id":"stream-y","input":[{"type":"reasoning","encrypted_content":"sealed-stream"}]}`,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("stream state rejected: %d %s", rec.Code, rec.Body.String())
		}
	}
	if sends.Load() != 3 {
		t.Fatalf("want X,Y,Y sends; got %d", sends.Load())
	}
}

func TestRoutedCodexLongAcceptedResponseOutlivesRetryBudget(t *testing.T) {
	var sends atomic.Int32
	h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(40 * time.Millisecond)
		_, _ = io.WriteString(w, `{"id":"long","status":"completed","model":"x","output":[{"type":"message","content":[{"type":"output_text","text":"finished"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 1, MaxTotalDuration: 20 * time.Millisecond}, "x", "y")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original"}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "finished") || sends.Load() != 1 {
		t.Fatalf("accepted response interrupted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRoutedCodexFinalExecutionSnapshotPersistsBeforeArchive(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(fmt.Sprint(exhausted), func(t *testing.T) {
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: ":memory:", BufferSize: 10, BatchSize: 10, FlushInterval: time.Hour, MaxRetry: 1, CleanupInterval: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tracker.Close() })
			h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if exhausted {
					w.Header().Set("Retry-After", "3600")
					w.WriteHeader(429)
					_, _ = io.WriteString(w, `{"error":{"code":"rate_limit"}}`)
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["model"] == "x" {
					w.WriteHeader(400)
					_, _ = io.WriteString(w, `{"error":{"code":"model_not_found"}}`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"trace","status":"completed","model":"y","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
			}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 2, MaxTotalDuration: 100 * time.Millisecond}, "x", "y")
			h.usageTracker = tracker
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original"}`)))
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
			if snapshot.Execution == nil || len(snapshot.Execution.Events) < 2 {
				t.Fatalf("missing trace: %+v", snapshot)
			}
			if exhausted {
				if snapshot.Attempts != 1 || snapshot.StopReason != "retry_budget_exhausted" || snapshot.WaitMs == 0 {
					t.Fatalf("lost final budget stats: %+v", snapshot)
				}
			} else if snapshot.Attempts != 2 || snapshot.StopReason != "" {
				t.Fatalf("success at limit mislabeled: %+v", snapshot)
			}
		})
	}
}

func TestRoutedCodexExplicitTurnPreferenceStaysInsideEligibleStage(t *testing.T) {
	h := newRoutedCodexExecutionFixture(t, func(http.ResponseWriter, *http.Request) {}, config.RetryConfig{}, "x")
	first := h.manager.endpointsByChannel[2][0]
	second := *first
	second.ID, second.Name, second.stateVersion = 21, "second", 0
	h.manager.endpointsByChannel[2] = []*CodexEndpoint{first, &second}
	stage := &codexRouteStage{channel: h.manager.channelsByID[2], upstreamModel: "x", turn: "known-turn", execution: &routing.Ledger{}}
	h.states.Observe(nil, stage.turn, stage.candidate(&second, "original"))
	ep, _, err := h.selectQuotaEligibleEndpointForModel(context.Background(), "B", "original", nil, nil, nil, true, stage)
	if err != nil || ep.ID != second.ID {
		t.Fatalf("turn preference ignored: endpoint=%v err=%v", ep, err)
	}
	stage.execution.Fail(stage.candidate(&second, "original"), routing.Failure{Scope: routing.ScopeCredential, Reason: "credential_rejected"})
	if _, _, err := h.selectQuotaEligibleEndpointForModel(context.Background(), "B", "original", nil, nil, nil, true, stage); err == nil {
		t.Fatal("turn affinity bypassed credential eligibility")
	}
}

func TestRoutedCodexEndpointAndCredentialFailuresDoNotRetryAsAnotherModel(t *testing.T) {
	for _, status := range []int{0, 400, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var sends atomic.Int32
			h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				if status == 0 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
					return
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"request rejected"}}`)
			}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 5}, "x", "y")
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original"}`)))
			if sends.Load() != 1 {
				t.Fatalf("retrying same failure with Y: sends=%d", sends.Load())
			}
		})
	}
}

func TestRoutedCodexAcceptedLongStreamFinishes(t *testing.T) {
	h := newRoutedCodexExecutionFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"begin\"}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"long\",\"status\":\"completed\",\"model\":\"x\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 1, MaxTotalDuration: 100 * time.Millisecond}, "x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","stream":true}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "response.completed") {
		t.Fatalf("long stream interrupted: %d %s", rec.Code, rec.Body.String())
	}
}
