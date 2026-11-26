package codexproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

const historyInput = `[{"role":"user", "content":"keep <>& \\ text"},{"type":"reasoning","encrypted_content":"sealed"},{"type":"function_call","call_id":"call-1","name":"test","arguments":"{}","encrypted_function_args":["keep"]},{"type":"function_call_output","call_id":"call-1","output":"done"},{"type":"reasoning","encrypted_content":null,"summary":[]}]`
const recoveredHistoryInput = `[{"role":"user", "content":"keep <>& \\ text"},{"type":"function_call","call_id":"call-1","name":"test","arguments":"{}","encrypted_function_args":["keep"]},{"type":"function_call_output","call_id":"call-1","output":"done"},{"type":"reasoning","encrypted_content":null,"summary":[]}]`

func TestEncryptedHistoryOnlyRemovesSealedReasoning(t *testing.T) {
	prefix := "{ \"model\":\"original\",\"large\":123456789012345678901234567890, \"input\" : "
	suffix := `, "tools":[{"type":"custom","name":"keep"}], "reasoning":{"effort":"high"},"max_output_tokens":19 }` + "\n"
	body := prefix + historyInput + suffix
	got, removed := withoutEncryptedReasoning([]byte(body))
	if removed != 1 || string(got) != prefix+recoveredHistoryInput+suffix {
		t.Fatalf("unexpected edit: removed=%d body=%s", removed, got)
	}
	for _, body := range []string{
		`{"input":"hello"}`, `{"input":[{"role":"user","content":"hello"}]}`,
		`{"input":[{"type":"reasoning","encrypted_content":"only-item"}]}`,
		`{"input":` + historyInput + `,"previous_response_id":"resp-old"}`,
		`{"input":` + historyInput + `,"conversation":{"id":"conv-old"}}`,
		`{"input":` + historyInput + `,"input":[]}`,
		`{"input":[{"type":"compaction","encrypted_content":"summary"},{"type":"reasoning","encrypted_content":"sealed"},{"role":"user","content":"hello"}]}`,
		`{"input":` + historyInput + `} trailing`,
		`{"input":[{"type":"reasoning","encrypted_content":123}]}`,
	} {
		got, removed := withoutEncryptedReasoning([]byte(body))
		if removed != 0 || string(got) != body {
			t.Fatalf("unsafe history modified: %s -> %s", body, got)
		}
	}
}

func TestEncryptedHistoryFallbackContinuesSameSession(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode, stream), func(t *testing.T) {
				var sends atomic.Int32
				type observed struct {
					body, path string
					headers    http.Header
				}
				received := make(chan observed, 6)
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, r *http.Request) {
					raw, _ := io.ReadAll(r.Body)
					headers := r.Header.Clone()
					headers.Del("Content-Length")
					received <- observed{string(raw), r.URL.String(), headers}
					w.Header().Set("Content-Type", "application/json")
					if sends.Add(1)%3 != 0 {
						w.WriteHeader(400)
						io.WriteString(w, encryptedContentTestError)
						return
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", encryptedContentTestSuccess)
					} else {
						io.WriteString(w, encryptedContentTestSuccess)
					}
				})
				(&HTTPHandler{handler: h}).SetEncryptedHistoryRecovery(true)
				tracker := encryptedContentTestTracker(t, h)
				for turn := 0; turn < 2; turn++ {
					body := fmt.Sprintf(`{ "model":"original", "stream":%t,"input":%s,"store":false,"turn":%d }`, stream, historyInput, turn)
					req := httptest.NewRequest("POST", "/v1/responses?keep=query", strings.NewReader(body))
					req.Header.Set("Session_id", "same-session")
					req.Header.Set("X-Codex-Turn-Id", fmt.Sprint(turn))
					req.Header.Set("User-Agent", "codex-client")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, req)
					if w.Code != 200 || sends.Load() != int32((turn+1)*3) || !strings.Contains(w.Body.String(), "resp-recovered") {
						t.Fatalf("turn %d failed: sends=%d status=%d body=%s", turn, sends.Load(), w.Code, w.Body.String())
					}
					first, second, third := <-received, <-received, <-received
					if first.body != body || !reflect.DeepEqual(first, second) || third.body != strings.Replace(body, historyInput, recoveredHistoryInput, 1) || third.path != first.path || !reflect.DeepEqual(third.headers, first.headers) {
						t.Fatalf("unexpected recovery request: first=%+v second=%+v third=%+v", first, second, third)
					}
					if trace != nil && (!trace.Successful || trace.Attempts != 3) {
						t.Fatalf("bad trace: %+v", trace)
					}
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != 2 || len(chain) != 3 || chain[2].RemovedReasoningItems != 1 || chain[0].Error != encryptedContentTestError || chain[1].Error != encryptedContentTestError {
					t.Fatalf("recovery evidence missing: %+v %+v", detail, chain)
				}
			})
		}
	}
}

func TestEncryptedHistoryFallbackBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		enabled                  bool
		secondStatus             int
		secondError, path, extra string
		limit, want              int
	}{
		{"off", false, 400, encryptedContentTestError, "/responses", "", 8, 2},
		{"persistent", true, 400, encryptedContentTestError, "/responses", "", 8, 3},
		{"different-error", true, 400, `{"error":{"code":"invalid_request_error"}}`, "/responses", "", 8, 2},
		{"different-status", true, 503, encryptedContentTestError, "/responses", "", 8, 2},
		{"compact-route", true, 400, encryptedContentTestError, "/responses/compact", "", 8, 2},
		{"server-state", true, 400, encryptedContentTestError, "/responses", `,"previous_response_id":"resp-old"`, 8, 2},
		{"attempt-limit", true, 400, encryptedContentTestError, "/responses", "", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sends atomic.Int32
			h, trace := newEncryptedContentTestHandler(t, "profile", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if sends.Add(1) == 1 {
					w.WriteHeader(400)
					io.WriteString(w, encryptedContentTestError)
					return
				}
				w.WriteHeader(tc.secondStatus)
				io.WriteString(w, tc.secondError)
			})
			h.SetEncryptedHistoryRecovery(tc.enabled)
			h.UpdateConfig(config.RetryConfig{MaxTotalAttempts: tc.limit, MaxTotalDuration: 3 * time.Second, DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
			tracker := encryptedContentTestTracker(t, h)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(`{"model":"original","input":`+historyInput+tc.extra+`}`)))
			if int(sends.Load()) != tc.want || w.Code != tc.secondStatus || w.Body.String() != tc.secondError {
				t.Fatalf("unexpected result: sends=%d status=%d body=%s", sends.Load(), w.Code, w.Body.String())
			}
			detail, chain := encryptedContentTestHistory(t, tracker)
			if len(chain) != tc.want || (chain[len(chain)-1].RemovedReasoningItems > 0) != (tc.want == 3) {
				t.Fatalf("phantom recovery: %+v", chain)
			}
			if tc.name == "attempt-limit" && (detail.FailureReason != "invalid_encrypted_content" || trace.StopReason != routing.StopRetryAttemptsExhausted) {
				t.Fatalf("cause and stop confused: %+v %+v", detail, trace)
			}
		})
	}
}

func TestEncryptedHistoryRecoverySnapshotAndCancellation(t *testing.T) {
	var sends atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _ := newEncryptedContentTestHandler(t, "profile", func(w http.ResponseWriter, r *http.Request) {
		if sends.Add(1) == 2 {
			cancel()
		}
		w.WriteHeader(400)
		io.WriteString(w, encryptedContentTestError)
	})
	req := h.withResponseRecoveryConfig(httptest.NewRequest("POST", "/responses", nil))
	h.SetEncryptedHistoryRecovery(true)
	if h.newResponseRecoveryState(req, "old").historyFallback || !h.newResponseRecoveryState(httptest.NewRequest("POST", "/responses", nil), "new").historyFallback {
		t.Fatal("in-flight setting snapshot changed")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"original","input":`+historyInput+`}`)).WithContext(ctx))
	if sends.Load() != 2 {
		t.Fatalf("cancelled request made %d sends", sends.Load())
	}
}

func TestEncryptedHistoryRecoveryPreservesCauseAfterDeadline(t *testing.T) {
	var sends atomic.Int32
	h, trace := newEncryptedContentTestHandler(t, "profile", func(w http.ResponseWriter, r *http.Request) {
		if sends.Add(1) == 2 {
			time.Sleep(80 * time.Millisecond)
		}
		w.WriteHeader(400)
		io.WriteString(w, encryptedContentTestError)
	})
	h.SetEncryptedHistoryRecovery(true)
	cfg := h.GetResponseRecoveryConfig()
	cfg.Rules[0].DelayMS = 0
	if err := h.UpdateResponseRecoveryConfig(cfg); err != nil {
		t.Fatal(err)
	}
	h.UpdateConfig(config.RetryConfig{MaxTotalAttempts: 8, MaxTotalDuration: 50 * time.Millisecond}, config.FailoverConfig{Enabled: true})
	tracker := encryptedContentTestTracker(t, h)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"original","input":`+historyInput+`}`)))
	detail, chain := encryptedContentTestHistory(t, tracker)
	if sends.Load() != 2 || w.Code != 400 || w.Body.String() != encryptedContentTestError || detail.FailureReason != "invalid_encrypted_content" || trace.StopReason != routing.StopRetryDeadlineExceeded || chain[len(chain)-1].RemovedReasoningItems != 0 {
		t.Fatalf("deadline lost the cause or allowed fallback: sends=%d detail=%+v trace=%+v", sends.Load(), detail, trace)
	}
}
