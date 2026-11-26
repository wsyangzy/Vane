package codexproxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const encryptedContentTestError = `{"error":{"message":"bad response status code 400 (request id: upstream-first)","type":"invalid_request_error","param":"","code":"invalid_encrypted_content"}}`
const encryptedContentTestSuccess = `{"id":"resp-recovered","status":"completed","model":"original","output":[],"usage":{"input_tokens":2,"output_tokens":3}}`

func newEncryptedContentTestHandler(t *testing.T, mode string, upstream http.HandlerFunc) (*Handler, *routing.DispatchTrace) {
	t.Helper()
	var h *Handler
	var trace *routing.DispatchTrace
	if mode == "profile" {
		h, _, trace = newProfileCodexFixture(t, upstream)
	} else {
		h = newRoutedCodexExecutionFixture(t, upstream, config.RetryConfig{}, "original", "fallback-model")
		if mode == "active" {
			h.manager.SetModelRules(nil)
			h.manager.activeChannel = h.manager.channelsByID[2]
			h.manager.endpoints = h.manager.endpointsByChannel[2]
		}
	}
	h.UpdateConfig(config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: 8, MaxTotalDuration: 2 * time.Second, DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
	return h, trace
}

func encryptedContentTestTracker(t *testing.T, h *Handler) *tracking.UsageTracker {
	t.Helper()
	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"),
		BufferSize: 20, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tracker.Close() })
	h.usageTracker = tracker
	return tracker
}

func encryptedContentTestHistory(t *testing.T, tracker *tracking.UsageTracker) (tracking.RequestDetail, []chainNode) {
	t.Helper()
	if err := tracker.ForceFlush(); err != nil {
		t.Fatal(err)
	}
	details, err := tracker.QueryRequestDetails(context.Background(), &tracking.QueryOptions{Limit: 1})
	if err != nil || len(details) != 1 {
		t.Fatalf("request history=%+v error=%v", details, err)
	}
	var chain []chainNode
	if err := json.Unmarshal([]byte(details[0].EndpointChain), &chain); err != nil {
		t.Fatal(err)
	}
	return details[0], chain
}

func TestEncryptedContentRecoveryPreservesRequestAndAttemptHistory(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode, stream), func(t *testing.T) {
				type observedRequest struct {
					method, url, body string
					headers           http.Header
				}
				received := make(chan observedRequest, 8)
				var sends atomic.Int32
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, r *http.Request) {
					raw, _ := io.ReadAll(r.Body)
					received <- observedRequest{r.Method, r.URL.String(), string(raw), r.Header.Clone()}
					w.Header().Set("Content-Type", "application/json")
					if sends.Add(1) == 1 {
						w.Header().Set("X-Request-Id", "upstream-first")
						w.WriteHeader(http.StatusBadRequest)
						io.WriteString(w, encryptedContentTestError)
						return
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", encryptedContentTestSuccess)
						return
					}
					io.WriteString(w, encryptedContentTestSuccess)
				})
				cfg := h.retryConfig()
				cfg.MaxAttempts = 1
				h.UpdateConfig(cfg, h.failoverConfig())
				tracker := encryptedContentTestTracker(t, h)
				body := fmt.Sprintf(`{ "model":"original", "stream":%t, "store":false, "previous_response_id":"resp-origin", "input":[{"type":"reasoning","encrypted_content":"sealed-origin"},{"role":"user","content":"continue"}], "tools":[{"type":"function","name":"probe","parameters":{"type":"object"}}], "reasoning":{"effort":"high"}, "max_output_tokens":17 }`, stream)
				req := httptest.NewRequest(http.MethodPost, "/v1/responses?trace=unchanged", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Session_id", "same-session")
				req.Header.Set("X-Codex-Turn-Id", "same-turn")
				req.Header.Set("User-Agent", "native-codex")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if sends.Load() != 2 || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "resp-recovered") {
					t.Fatalf("recovery failed: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
				}
				first, second := <-received, <-received
				if first.body != body || second.body != body || !reflect.DeepEqual(first, second) {
					t.Fatalf("request changed during recovery: first=%+v second=%+v", first, second)
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != 1 || len(chain) != 2 || chain[0].Attempts != 1 || chain[1].Attempts != 1 || chain[0].StatusCode != 400 || chain[1].StatusCode != 200 || chain[0].Error != encryptedContentTestError || chain[0].Headers["x-request-id"] != "upstream-first" {
					t.Fatalf("lost attempt history: retries=%d chain=%+v", detail.RetryCount, chain)
				}
				if chain[0].Channel != chain[1].Channel || chain[0].CredentialGroup != chain[1].CredentialGroup || chain[0].Endpoint != chain[1].Endpoint {
					t.Fatalf("recovery changed the selected resource: %+v", chain)
				}
				if trace != nil && (trace.Attempts != 2 || !trace.Successful || trace.StopReason != "") {
					t.Fatalf("recovery was mistaken for a route failure: %+v", trace)
				}
			})
		}
	}
}

func TestEncryptedContentRecoveryStopsAfterSecondFailure(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, status := range []int{400, 408, 503} {
			t.Run(fmt.Sprintf("%s/status=%d", mode, status), func(t *testing.T) {
				var sends atomic.Int32
				secondError := fmt.Sprintf(`{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","message":"second-verdict-%d"}}`, status)
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if sends.Add(1) == 1 {
						w.WriteHeader(http.StatusBadRequest)
						io.WriteString(w, encryptedContentTestError)
						return
					}
					w.WriteHeader(status)
					io.WriteString(w, secondError)
				})
				tracker := encryptedContentTestTracker(t, h)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":[{"type":"reasoning","encrypted_content":"sealed-origin"}]}`)))
				if sends.Load() != 2 || w.Code != status || w.Body.String() != secondError {
					t.Fatalf("recovery replayed or replaced the error: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != 1 || len(chain) != 2 || chain[0].Error != encryptedContentTestError || chain[1].Error != secondError || chain[1].StatusCode != status {
					t.Fatalf("failure evidence lost: retries=%d chain=%+v", detail.RetryCount, chain)
				}
				if trace != nil && (trace.Attempts != 2 || trace.Successful || trace.StopReason == "") {
					t.Fatalf("failed recovery left the route open: %+v", trace)
				}
			})
		}
	}
}

func TestEncryptedContentRecoveryRespectsRequestBudget(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, limit := range []string{"attempts", "duration"} {
			t.Run(mode+"/"+limit, func(t *testing.T) {
				var sends atomic.Int32
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, _ *http.Request) {
					sends.Add(1)
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, encryptedContentTestError)
				})
				tracker := encryptedContentTestTracker(t, h)
				cfg := config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: 1, MaxTotalDuration: time.Second}
				wantStop := routing.StopRetryAttemptsExhausted
				if limit == "duration" {
					cfg.MaxTotalAttempts, cfg.MaxTotalDuration = 8, 40*time.Millisecond
					wantStop = routing.StopRetryDeadlineExceeded
				}
				h.UpdateConfig(cfg, config.FailoverConfig{Enabled: true})
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"continue"}`)))
				if sends.Load() != 1 || w.Code != 400 || w.Body.String() != encryptedContentTestError {
					t.Fatalf("budget did not retain the real upstream failure: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != 0 || len(chain) != 1 || chain[0].Attempts != 1 || chain[0].Error != encryptedContentTestError {
					t.Fatalf("unsent recovery counted as an attempt: retries=%d chain=%+v", detail.RetryCount, chain)
				}
				if trace != nil && (trace.Attempts != 1 || trace.StopReason != wantStop) {
					t.Fatalf("wrong budget stop: %+v", trace)
				}
			})
		}
	}
}

func TestEncryptedContentRecoveryDoesNotReplayAfterStreamOutput(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule"} {
		t.Run(mode, func(t *testing.T) {
			var sends atomic.Int32
			h, _ := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, _ *http.Request) {
				sends.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-started\"}}\n\n")
				io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"already-sent\"}\n\n")
				fmt.Fprintf(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_encrypted_content\",\"message\":\"late-failure\"}}\n\n")
			})
			w := httptest.NewRecorder()
			h.SetEncryptedHistoryRecovery(true)
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","stream":true,"input":"continue"}`)))
			if sends.Load() != 1 || !strings.Contains(w.Body.String(), "already-sent") {
				t.Fatalf("output stream was replayed: sends=%d body=%s", sends.Load(), w.Body.String())
			}
		})
	}
}

func TestEncryptedContentRecoveryPinsPreparedRequestAndHonorsCancellation(t *testing.T) {
	for _, cancelRecovery := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelRecovery), func(t *testing.T) {
			var sends atomic.Int32
			received := make(chan string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				received <- r.URL.String() + "\n" + r.Header.Get("Authorization") + "\n" + r.Header.Get("Session_id") + "\n" + string(body)
				if sends.Add(1) == 1 {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, encryptedContentTestError)
					return
				}
				io.WriteString(w, encryptedContentTestSuccess)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget := routing.NewBudget(2, time.Second)
			if err := budget.Begin(ctx); err != nil {
				t.Fatal(err)
			}
			ep := &CodexEndpoint{BaseURL: server.URL, APIKey: "original-key"}
			retry := &responseRecoveryState{requestID: "req-pinned", cfg: defaultResponseRecoveryConfig(), beforeRetry: func(ctx context.Context) error { return budget.Begin(ctx) }}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses?original=1", nil)
			req.Header.Set("Session_id", "original-session")
			req = req.WithContext(withResponseRecoveryState(routing.WithBudget(ctx, budget), retry))
			retry.onRejected = func(*http.Response, []byte) {
				// Simulate configuration edits while recovery waits. The resend must
				// use the fully prepared first request, rather than rebuild it.
				ep.APIKey, ep.BaseURL = "changed-key", "https://example.invalid"
				req.Header.Set("Session_id", "changed-session")
				if cancelRecovery {
					cancel()
				}
			}
			resp, err := NewForwarder(time.Second, newTestLogger()).Forward(req, []byte(`{"model":"original","input":"unchanged"}`), ep)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if cancelRecovery {
				if sends.Load() != 1 || retry.retried() || resp.StatusCode != 400 {
					t.Fatalf("cancelled recovery was sent: sends=%d retry=%+v status=%d", sends.Load(), retry, resp.StatusCode)
				}
				return
			}
			first, second := <-received, <-received
			if sends.Load() != 2 || first != second || !retry.retried() {
				t.Fatalf("prepared request was not pinned: sends=%d first=%q second=%q", sends.Load(), first, second)
			}
		})
	}
}

func TestEncryptedContentErrorInspectionPreservesOtherResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"different-code", `{"error":{"code":"invalid_responses_request","message":"invalid_encrypted_content"}}`},
		{"message-only", `{"error":{"message":"invalid_encrypted_content"}}`},
		{"malformed", `{"error":{"code":"invalid_encrypted_content"}`},
		{"oversized", encryptedContentTestError + strings.Repeat(" ", maxChainErrorBodyBytes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(tc.body))}
			cfg := defaultResponseRecoveryConfig()
			match, _, readable := inspectRecoveryResponse(resp, routing.ResponseRecoveryMatch{Method: http.MethodPost, Path: "/responses"}, cfg)
			if _, matched := cfg.Match(match); readable && matched {
				t.Fatal("response unexpectedly qualified for recovery")
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(raw) != tc.body {
				t.Fatalf("passthrough body changed: error=%v body=%q", err, raw)
			}
		})
	}
	readErr := errors.New("upstream body interrupted")
	resp := &http.Response{StatusCode: 400, Body: io.NopCloser(io.MultiReader(strings.NewReader(encryptedContentTestError), recoveryResponseReadError{err: readErr}))}
	if _, _, readable := inspectRecoveryResponse(resp, routing.ResponseRecoveryMatch{Method: http.MethodPost, Path: "/responses"}, defaultResponseRecoveryConfig()); readable {
		t.Fatal("incomplete upstream error qualified for recovery")
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !errors.Is(err, readErr) || string(raw) != encryptedContentTestError {
		t.Fatalf("response read failure disappeared: error=%v body=%q", err, raw)
	}
}

func TestEncryptedContentRecoveryDecodesErrorsAndRedactsHistoryWithoutChangingPassthrough(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, finalStatus := range []int{200, 400} {
			t.Run(fmt.Sprintf("%s/status=%d", mode, finalStatus), func(t *testing.T) {
				key := "test-key"
				if mode == "profile" {
					key = "key-1"
				}
				firstBody := fmt.Sprintf(`{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","message":"first rejection echoed %s"}}`, key)
				finalBody := fmt.Sprintf(`{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","message":"second rejection echoed %s"}}`, key)
				var compressed bytes.Buffer
				encoder := gzip.NewWriter(&compressed)
				if _, err := io.WriteString(encoder, firstBody); err != nil {
					t.Fatal(err)
				}
				if err := encoder.Close(); err != nil {
					t.Fatal(err)
				}
				var finalCompressed bytes.Buffer
				encoder = gzip.NewWriter(&finalCompressed)
				if _, err := io.WriteString(encoder, finalBody); err != nil {
					t.Fatal(err)
				}
				if err := encoder.Close(); err != nil {
					t.Fatal(err)
				}
				var sends atomic.Int32
				h, _ := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if sends.Add(1) == 1 {
						w.Header().Set("Content-Encoding", "gzip")
						w.WriteHeader(400)
						w.Write(compressed.Bytes())
						return
					}
					if finalStatus == 200 {
						w.WriteHeader(finalStatus)
						io.WriteString(w, encryptedContentTestSuccess)
						return
					}
					w.Header().Set("Content-Encoding", "gzip")
					w.WriteHeader(finalStatus)
					w.Write(finalCompressed.Bytes())
				})
				tracker := encryptedContentTestTracker(t, h)
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"continue"}`))
				// Explicit negotiation prevents net/http's automatic gzip decoding.
				req.Header.Set("Accept-Encoding", "gzip")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if sends.Load() != 2 || w.Code != finalStatus || finalStatus == 400 && (!bytes.Equal(w.Body.Bytes(), finalCompressed.Bytes()) || w.Header().Get("Content-Encoding") != "gzip") {
					t.Fatalf("encoded recovery or passthrough failed: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
				}
				_, chain := encryptedContentTestHistory(t, tracker)
				if len(chain) != 2 || !strings.Contains(chain[0].Error, "invalid_encrypted_content") || !strings.Contains(chain[0].Error, "[REDACTED]") {
					t.Fatalf("encoded failure evidence was lost: %+v", chain)
				}
				for _, node := range chain {
					if strings.Contains(node.Error, key) {
						t.Fatalf("request history retained a credential: %+v", chain)
					}
				}
			})
		}
	}
}

func TestEncryptedContentRecoveryEndsAfterTransportFailure(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule"} {
		t.Run(mode, func(t *testing.T) {
			var sends atomic.Int32
			h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				if sends.Add(1) == 1 {
					w.WriteHeader(400)
					io.WriteString(w, encryptedContentTestError)
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
			})
			tracker := encryptedContentTestTracker(t, h)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"continue"}`)))
			if sends.Load() != 2 || w.Code < 400 {
				t.Fatalf("transport failure restarted recovery: sends=%d status=%d", sends.Load(), w.Code)
			}
			detail, chain := encryptedContentTestHistory(t, tracker)
			if detail.RetryCount != 1 || len(chain) != 2 || chain[0].Error != encryptedContentTestError || chain[1].Attempts != 1 || !chain[1].Local || chain[1].Error == "" {
				t.Fatalf("transport failure hid the original rejection: retries=%d chain=%+v", detail.RetryCount, chain)
			}
			if trace != nil && trace.Attempts != 2 {
				t.Fatalf("transport failure lost a send: %+v", trace)
			}
		})
	}
}

func TestResponseRecoveryConfigurationIsValidatedAndIsolated(t *testing.T) {
	h := &Handler{}
	facade := &HTTPHandler{handler: h}
	want := defaultResponseRecoveryConfig()
	if err := want.Validate(); err != nil {
		t.Fatal(err)
	}
	fromGetter := facade.GetResponseRecoveryConfig()
	fromGetter.Rules[0].ErrorCodes[0] = "changed"
	if !reflect.DeepEqual(h.GetResponseRecoveryConfig(), want) {
		t.Fatal("getter modified provider defaults")
	}
	want.Rules[0].ErrorTypes = []string{"invalid_request_error"}
	cfg := want.Clone()
	if err := facade.UpdateResponseRecoveryConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.Rules[0].Enabled = false, false
	cfg.Rules[0].StatusCodes[0], cfg.Rules[0].ErrorCodes[0], cfg.Rules[0].ErrorTypes[0] = 503, "changed", "changed"
	cfg.Rules[0].Methods[0], cfg.Rules[0].Paths[0] = "GET", "/changed"
	fromGetter = facade.GetResponseRecoveryConfig()
	if !reflect.DeepEqual(fromGetter, want) {
		t.Fatalf("caller mutation changed active configuration: %+v", fromGetter)
	}
	fromGetter.Rules[0].StatusCodes[0], fromGetter.Rules[0].ErrorCodes[0], fromGetter.Rules[0].ErrorTypes[0] = 502, "changed", "changed"
	fromGetter.Rules[0].Methods[0], fromGetter.Rules[0].Paths[0] = "GET", "/changed"
	invalid := want.Clone()
	invalid.Rules[0].MaxRetries = 0
	if err := facade.UpdateResponseRecoveryConfig(invalid); err == nil {
		t.Fatal("invalid update accepted")
	}
	if !reflect.DeepEqual(h.GetResponseRecoveryConfig(), want) {
		t.Fatal("getter mutation or invalid update replaced active configuration")
	}
	if err := facade.UpdateResponseRecoveryConfig(routing.ResponseRecoveryConfig{}); err != nil {
		t.Fatal(err)
	}
	if cfg := facade.GetResponseRecoveryConfig(); cfg.Enabled || len(cfg.Rules) != 0 {
		t.Fatalf("explicit disable reverted to provider defaults: %+v", cfg)
	}
	unavailable := &HTTPHandler{}
	if err := unavailable.UpdateResponseRecoveryConfig(want); err == nil || err.Error() != "codex_handler_unavailable" {
		t.Fatalf("missing handler accepted configuration: %v", err)
	}
}

func TestResponseRecoveryControlsDisableDefaultRule(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, control := range []string{"global", "rule", "empty"} {
			t.Run(mode+"/"+control, func(t *testing.T) {
				var sends atomic.Int32
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, _ *http.Request) {
					sends.Add(1)
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, encryptedContentTestError)
				})
				cfg := h.GetResponseRecoveryConfig()
				switch control {
				case "global":
					cfg.Enabled = false
				case "rule":
					cfg.Rules[0].Enabled = false
				case "empty":
					cfg.Rules = nil
				}
				if err := h.UpdateResponseRecoveryConfig(cfg); err != nil {
					t.Fatal(err)
				}
				tracker := encryptedContentTestTracker(t, h)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"continue"}`)))
				if sends.Load() != 1 || w.Code != 400 || w.Body.String() != encryptedContentTestError {
					t.Fatalf("disabled recovery sent or replaced the response: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != 0 || len(chain) != 1 || chain[0].Attempts != 1 {
					t.Fatalf("disabled recovery created retry evidence: retries=%d chain=%+v", detail.RetryCount, chain)
				}
				if trace != nil && trace.Attempts != 1 {
					t.Fatalf("disabled recovery changed trace attempts: %+v", trace)
				}
			})
		}
	}
}

func TestResponseRecoveryCustomRulesReplaySameTarget(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, tc := range []struct {
			name              string
			status            int
			body              string
			codes, errorTypes []string
		}{
			{"different-client-error", 400, `{"error":{"code":"transient_session_state","type":"invalid_request_error"}}`, []string{"transient_session_state"}, []string{"invalid_request_error"}},
			{"server-error", 503, `{"error":{"code":"transient_upstream_error","type":"server_error"}}`, []string{"transient_upstream_error"}, nil},
			{"type-only", 503, `{"error":{"type":"server_error"}}`, nil, []string{"server_error"}},
			{"status-only", 502, "temporary gateway failure", nil, nil},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				type observedRequest struct {
					method, url, body string
					headers           http.Header
				}
				received := make(chan observedRequest, 8)
				var sends atomic.Int32
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, r *http.Request) {
					raw, _ := io.ReadAll(r.Body)
					received <- observedRequest{r.Method, r.URL.String(), string(raw), r.Header.Clone()}
					w.Header().Set("X-Request-Id", fmt.Sprintf("upstream-%d", sends.Add(1)))
					if sends.Load() <= 2 {
						w.WriteHeader(tc.status)
						io.WriteString(w, tc.body)
						return
					}
					io.WriteString(w, encryptedContentTestSuccess)
				})
				cfg := routing.ResponseRecoveryConfig{Enabled: true, Rules: []routing.ResponseRecoveryRule{{
					ID: "custom_response", Enabled: true, Action: routing.ResponseRecoveryRetrySameTarget,
					StatusCodes: []int{tc.status}, ErrorCodes: tc.codes, ErrorTypes: tc.errorTypes,
					Methods: []string{http.MethodPost}, Paths: []string{"/responses"}, MaxRetries: 2,
				}}}
				if err := h.UpdateResponseRecoveryConfig(cfg); err != nil {
					t.Fatal(err)
				}
				retryCfg := h.retryConfig()
				retryCfg.MaxAttempts = 1
				h.UpdateConfig(retryCfg, h.failoverConfig())
				tracker := encryptedContentTestTracker(t, h)
				body := `{ "model":"original", "stream":false, "input":"unchanged", "tools":[{"type":"function","name":"probe","parameters":{"type":"object"}}], "reasoning":{"effort":"high"}, "max_output_tokens":17 }`
				req := httptest.NewRequest(http.MethodPost, "/v1/responses?unchanged=1", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Session_id", "same-session")
				req.Header.Set("User-Agent", "native-codex")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if sends.Load() != 3 || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "resp-recovered") {
					t.Fatalf("custom rule did not recover: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
				}
				first := <-received
				if first.body != body {
					t.Fatalf("request body changed: %q", first.body)
				}
				for i := 0; i < 2; i++ {
					if next := <-received; !reflect.DeepEqual(first, next) {
						t.Fatalf("custom recovery changed prepared request: first=%+v next=%+v", first, next)
					}
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != 2 || len(chain) != 3 {
					t.Fatalf("multiple retries lost send evidence: retries=%d chain=%+v", detail.RetryCount, chain)
				}
				for i, node := range chain {
					wantStatus := tc.status
					if i == 2 {
						wantStatus = http.StatusOK
					}
					if node.Attempts != 1 || node.StatusCode != wantStatus || node.Channel != chain[0].Channel || node.CredentialGroup != chain[0].CredentialGroup || node.Endpoint != chain[0].Endpoint || i < 2 && (node.Error != tc.body || node.Headers["x-request-id"] != fmt.Sprintf("upstream-%d", i+1)) {
						t.Fatalf("retry evidence or destination changed: %+v", chain)
					}
				}
				if trace != nil && (trace.Attempts != 3 || !trace.Successful || trace.StopReason != "") {
					t.Fatalf("multiple recovery attempts left a route failure: %+v", trace)
				}
			})
		}
	}
}

func TestResponseRecoveryRuleAndSharedBudgetBoundConfiguredRetries(t *testing.T) {
	const failure = `{"error":{"code":"transient_upstream_error","type":"server_error"}}`
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, maxAttempts := range []int{1, 2, 3, 8} {
			t.Run(fmt.Sprintf("%s/attempts=%d", mode, maxAttempts), func(t *testing.T) {
				var sends, observations atomic.Int32
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, _ *http.Request) {
					sends.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
					io.WriteString(w, failure)
				})
				facade := &HTTPHandler{handler: h}
				facade.SetProfileResponseObserver(func(context.Context, routing.DispatchCandidate, int, http.Header, []byte) {
					observations.Add(1)
				})
				if err := facade.UpdateResponseRecoveryConfig(routing.ResponseRecoveryConfig{Enabled: true, Rules: []routing.ResponseRecoveryRule{{
					ID: "custom_response", Enabled: true, Action: routing.ResponseRecoveryRetrySameTarget,
					StatusCodes: []int{503}, ErrorCodes: []string{"transient_upstream_error"}, MaxRetries: 3,
				}}}); err != nil {
					t.Fatal(err)
				}
				h.UpdateConfig(config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: maxAttempts, MaxTotalDuration: 2 * time.Second, DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
				tracker := encryptedContentTestTracker(t, h)
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"continue"}`))
				if maxAttempts == 2 {
					// An existing caller budget must also retain the stricter global ceiling.
					req = req.WithContext(routing.WithBudget(req.Context(), routing.NewBudget(8, 2*time.Second)))
				}
				h.ServeHTTP(w, req)
				wantSends := min(maxAttempts, 4)
				if sends.Load() != int32(wantSends) || w.Code != 503 || w.Body.String() != failure {
					t.Fatalf("configured retries escaped shared budget: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != wantSends-1 || len(chain) != wantSends {
					t.Fatalf("unsent retry entered history: retries=%d chain=%+v", detail.RetryCount, chain)
				}
				for _, node := range chain {
					if node.Attempts != 1 || node.StatusCode != 503 || node.Error != failure {
						t.Fatalf("budget stop lost actual upstream failure: %+v", chain)
					}
				}
				if trace != nil && (trace.Attempts != wantSends || trace.StopReason == "" || maxAttempts < 4 && trace.StopReason != routing.StopRetryAttemptsExhausted || observations.Load() != int32(wantSends)) {
					t.Fatalf("budget stop duplicated response observation or lost its stop reason: observations=%d trace=%+v", observations.Load(), trace)
				}
			})
		}
	}
}

func TestResponseRecoveryCompressedFailureKeepsAttemptUsage(t *testing.T) {
	const failure = `{"error":{"code":"invalid_encrypted_content","type":"invalid_request_error"},"usage":{"input_tokens":2,"output_tokens":3}}`
	var compressed bytes.Buffer
	encoder := gzip.NewWriter(&compressed)
	if _, err := io.WriteString(encoder, failure); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	for _, maxAttempts := range []int{1, 8} {
		t.Run(fmt.Sprint(maxAttempts), func(t *testing.T) {
			var sends atomic.Int32
			settlements := make(chan routing.AttemptSettlement, 8)
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				sends.Add(1)
				w.Header().Set("Content-Encoding", "gzip")
				w.WriteHeader(http.StatusBadRequest)
				w.Write(compressed.Bytes())
			}, func(settlement routing.AttemptSettlement) { settlements <- settlement })
			h.UpdateConfig(config.RetryConfig{MaxTotalAttempts: maxAttempts, MaxTotalDuration: 2 * time.Second, DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"continue"}`))
			req.Header.Set("Accept-Encoding", "gzip")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			wantSends := min(maxAttempts, 2)
			if sends.Load() != int32(wantSends) || trace.Attempts != wantSends || w.Code != 400 || !bytes.Equal(w.Body.Bytes(), compressed.Bytes()) || w.Header().Get("Content-Encoding") != "gzip" || len(settlements) != wantSends {
				t.Fatalf("compressed failure changed response or lost settlement: sends=%d settlements=%d status=%d trace=%+v", sends.Load(), len(settlements), w.Code, trace)
			}
			for i := 0; i < wantSends; i++ {
				usage := (<-settlements).Usage
				if !usage.Known || !usage.Complete || usage.InputTokens != 2 || usage.OutputTokens != 3 {
					t.Fatalf("compressed response overwrote attempt usage: %+v", usage)
				}
			}
		})
	}
}

func TestResponseRecoveryDoesNotChainDifferentRules(t *testing.T) {
	const firstError = `{"error":{"code":"first_error","type":"invalid_request_error"}}`
	const secondError = `{"error":{"code":"second_error","type":"server_error"}}`
	for _, mode := range []string{"profile", "model-rule", "active"} {
		t.Run(mode, func(t *testing.T) {
			var sends atomic.Int32
			h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, _ *http.Request) {
				if sends.Add(1) == 1 {
					w.WriteHeader(400)
					io.WriteString(w, firstError)
					return
				}
				w.WriteHeader(503)
				io.WriteString(w, secondError)
			})
			if err := h.UpdateResponseRecoveryConfig(routing.ResponseRecoveryConfig{Enabled: true, Rules: []routing.ResponseRecoveryRule{
				{ID: "first_policy", Enabled: true, Action: routing.ResponseRecoveryRetrySameTarget, StatusCodes: []int{400}, ErrorCodes: []string{"first_error"}, MaxRetries: 2},
				{ID: "second_policy", Enabled: true, Action: routing.ResponseRecoveryRetrySameTarget, StatusCodes: []int{503}, ErrorCodes: []string{"second_error"}, MaxRetries: 2},
			}}); err != nil {
				t.Fatal(err)
			}
			tracker := encryptedContentTestTracker(t, h)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"continue"}`)))
			if sends.Load() != 2 || w.Code != 503 || w.Body.String() != secondError {
				t.Fatalf("changed error restarted another rule or route: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
			}
			detail, chain := encryptedContentTestHistory(t, tracker)
			if detail.RetryCount != 1 || len(chain) != 2 || chain[0].Error != firstError || chain[1].Error != secondError {
				t.Fatalf("policy transition lost the final failure: retries=%d chain=%+v", detail.RetryCount, chain)
			}
			if trace != nil && (trace.Attempts != 2 || trace.Successful || trace.StopReason == "") {
				t.Fatalf("policy transition left routing open: %+v", trace)
			}
		})
	}
}

func TestResponseRecoveryRequestKeepsConfigurationSnapshot(t *testing.T) {
	var sends atomic.Int32
	h, trace := newEncryptedContentTestHandler(t, "profile", func(w http.ResponseWriter, _ *http.Request) {
		switch sends.Add(1) {
		case 1:
			w.WriteHeader(503)
			io.WriteString(w, `{"error":{"code":"overloaded"}}`)
		case 2, 4:
			w.WriteHeader(400)
			io.WriteString(w, encryptedContentTestError)
		default:
			io.WriteString(w, encryptedContentTestSuccess)
		}
	})
	(&HTTPHandler{handler: h}).SetProfileResponseObserver(func(_ context.Context, _ routing.DispatchCandidate, status int, _ http.Header, _ []byte) {
		if status == 503 {
			if err := h.UpdateResponseRecoveryConfig(routing.ResponseRecoveryConfig{}); err != nil {
				t.Error(err)
			}
		}
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"first"}`)))
	if sends.Load() != 3 || w.Code != 200 || !trace.Successful || trace.Attempts != 3 || h.GetResponseRecoveryConfig().Enabled {
		t.Fatalf("live update changed an in-flight request: sends=%d status=%d trace=%+v", sends.Load(), w.Code, trace)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"original","input":"next"}`)))
	if sends.Load() != 4 || w.Code != 400 || w.Body.String() != encryptedContentTestError || trace.Attempts != 1 {
		t.Fatalf("next request missed updated configuration: sends=%d status=%d trace=%+v", sends.Load(), w.Code, trace)
	}
}
