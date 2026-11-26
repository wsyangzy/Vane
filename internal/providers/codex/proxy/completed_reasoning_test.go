package codexproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

func reasoningTransitionStream(final string) string {
	item, _ := json.Marshal(map[string]any{"id": "rs-1", "type": "reasoning", "encrypted_content": final, "summary": []any{}, "status": "completed"})
	return "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"rs-1\",\"type\":\"reasoning\",\"summary\":[],\"encrypted_content\":\"partial\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":" + string(item) + "}\n\n"
}

func sameHistoryRequest(body string) *http.Request {
	r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	r.Header.Set("Session_id", "conversation")
	r.Header.Set("X-Codex-Turn-Id", "turn")
	return r
}

func TestCompletedReasoningRecoveryBeforeLossyFallback(t *testing.T) {
	for _, mode := range []string{"profile", "model-rule", "active"} {
		for _, outcome := range []string{"restored", "fallback", "fallback_disabled", "budget_exhausted"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				// This would previously hit the 256 KiB scanner/observer ceiling.
				final := strings.Repeat("sealed-final", 30000)
				wire := reasoningTransitionStream(final) + diagnosticCompletedSSE
				var bodies []string
				h, trace := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, r *http.Request) {
					raw, _ := io.ReadAll(r.Body)
					bodies = append(bodies, string(raw))
					if len(bodies) == 1 {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, wire)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if (outcome == "restored" && len(bodies) == 4) || (outcome == "fallback" && len(bodies) == 5) {
						io.WriteString(w, encryptedContentTestSuccess)
						return
					}
					w.WriteHeader(400)
					io.WriteString(w, encryptedContentTestError)
				})
				h.SetEncryptedHistoryRecovery(outcome != "fallback_disabled")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, sameHistoryRequest(`{"model":"original","stream":true,"input":"start"}`))
				if rec.Body.String() != wire || len(bodies) != 1 || len(h.completedHistory.entries) != 1 {
					t.Fatalf("completed item not preserved: bytes=%d want=%d sends=%d mappings=%d", rec.Body.Len(), len(wire), len(bodies), len(h.completedHistory.entries))
				}
				if trace != nil && !trace.Successful {
					t.Fatal("large stream rejected")
				}
				if outcome == "budget_exhausted" {
					cfg := h.retryConfig()
					cfg.MaxTotalAttempts = 2
					h.UpdateConfig(cfg, h.failoverConfig())
				}
				tracker := encryptedContentTestTracker(t, h)
				body := "{\"model\":\"original\",\"stream\":false,\"store\":false,\"input\": [ {\"id\":\"rs-1\", \"type\":\"reasoning\", \"summary\":[], \"encrypted_content\" : \"partial\"}," +
					`{"role":"user","content":"keep <>&"},{"type":"function_call","call_id":"call","name":"tool","arguments":"{}"},{"type":"function_call_output","call_id":"call","output":"keep"}], "tools":[], "reasoning":{"effort":"high"}, "large":123456789012345678901 }`
				rec = httptest.NewRecorder()
				h.ServeHTTP(rec, sameHistoryRequest(body))
				wantSends, wantStatus := 4, 200
				if outcome == "fallback" {
					wantSends = 5
				}
				if outcome == "fallback_disabled" {
					wantStatus = 400
				}
				if outcome == "budget_exhausted" {
					wantSends, wantStatus = 3, 400
				}
				if rec.Code != wantStatus || len(bodies) != wantSends {
					t.Fatalf("recovery order: status=%d sends=%d want=%d/%d", rec.Code, len(bodies), wantStatus, wantSends)
				}
				if bodies[1] != body || bodies[2] != body {
					t.Fatal("initial/unchanged retry modified history")
				}
				if len(bodies) >= 4 && bodies[3] != strings.Replace(body, `"partial"`, `"`+final+`"`, 1) {
					t.Fatal("restoration changed more than the proven ciphertext")
				}
				if outcome == "fallback" {
					fallback, _ := withoutEncryptedReasoning([]byte(body))
					if bodies[4] != string(fallback) {
						t.Fatal("fallback modified retained history")
					}
				}
				detail, chain := encryptedContentTestHistory(t, tracker)
				if detail.RetryCount != wantSends-2 || len(chain) != wantSends-1 {
					t.Fatalf("wrong attempt accounting: retries=%d chain=%d", detail.RetryCount, len(chain))
				}
				if len(chain) >= 3 && (chain[2].RestoredReasoningItems != 1 || chain[2].RemovedReasoningItems != 0) {
					t.Fatal("complete restoration marked as degradation")
				}
				if outcome == "fallback" && (chain[3].RestoredReasoningItems != 0 || chain[3].RemovedReasoningItems != 1) {
					t.Fatal("fallback evidence incorrect")
				}
				if strings.Contains(detail.EndpointChain, final) || strings.Contains(detail.EndpointChain, `"partial"`) {
					t.Fatal("opaque state leaked to tracking")
				}
			})
		}
	}
}

func TestCompletedReasoningRequiresSuccessfulStream(t *testing.T) {
	for _, ending := range []string{"", "data: [DONE]\n\n", "data: {\"type\":\"response.incomplete\"}\n\n", diagnosticCompletedSSE + "data: {\"type\":\"error\",\"code\":\"server_error\"}\n\n", "data: malformed\n\n" + diagnosticCompletedSSE} {
		t.Run(fmt.Sprint(len(ending)), func(t *testing.T) {
			h, _ := newEncryptedContentTestHandler(t, "profile", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, reasoningTransitionStream("final")+ending)
			})
			h.ServeHTTP(httptest.NewRecorder(), sameHistoryRequest(`{"model":"original","stream":true,"input":"start"}`))
			if len(h.completedHistory.entries) != 0 {
				t.Fatal("unfinished/failed stream poisoned repair cache")
			}
		})
	}
}

func TestCompletedReasoningScopeIsolation(t *testing.T) {
	r := sameHistoryRequest(`{}`)
	r.URL.Scheme, r.URL.Host = "https", "upstream.example"
	r.Header.Set("Authorization", "Bearer key")
	ep := &CodexEndpoint{ID: 1, ChannelID: 2, CredentialGroupID: 3}
	body := []byte(`{"model":"original"}`)
	scope := completedReasoningScope(r, r, ep, body)
	if scope == "" {
		t.Fatal("missing scope")
	}
	for _, name := range []string{"Session_id", "X-Branch-Id", "Authorization", "Cookie", "ChatGPT-Account-ID", "X-Api-Key", "OpenAI-Project", "OpenAI-Organization", "X-Relay-Key"} {
		other := r.Clone(r.Context())
		other.Header.Set(name, "different")
		if completedReasoningScope(other, other, ep, body) == scope {
			t.Fatalf("scope ignores %s", name)
		}
	}
	other := r.Clone(routing.WithCallerScope(context.Background(), "other"))
	if completedReasoningScope(other, other, ep, body) == scope {
		t.Fatal("scope ignores caller")
	}
	other = r.Clone(r.Context())
	other.Header.Del("Session_id")
	if completedReasoningScope(other, other, ep, body) != "" {
		t.Fatal("guessed a missing session")
	}
	other = r.Clone(r.Context())
	other.Header.Set("X-Codex-Turn-Id", "next-turn")
	if completedReasoningScope(other, other, ep, body) != scope {
		t.Fatal("cannot continue in the same session")
	}
	if completedReasoningScope(r, r, ep, []byte(`{"model":"different"}`)) == scope {
		t.Fatal("scope ignores model")
	}
	copyEP := *ep
	copyEP.stateVersion = 999999
	if completedReasoningScope(r, r, &copyEP, body) != scope {
		t.Fatal("scope cannot survive a process restart")
	}
	copyEP.AuthMode = "changed"
	if completedReasoningScope(r, r, &copyEP, body) == scope {
		t.Fatal("scope ignores authentication mode")
	}
	other = r.Clone(r.Context())
	other.URL.Host = "other.example"
	if completedReasoningScope(other, other, ep, body) == scope {
		t.Fatal("scope ignores destination")
	}
}

func TestCompletedReasoningNeverGuessesAndExpires(t *testing.T) {
	c := newCompletedReasoningCache()
	items := map[string]*completedReasoningItem{reasoningHash("rs-1"): {partial: reasoningHash("partial"), final: "final"}}
	c.remember("scope", items)
	valid := `{"input":[{"id":"rs-1","type":"reasoning","encrypted_content":"partial"},{"role":"user","content":"continue"}]}`
	for _, body := range []string{
		strings.Replace(valid, "partial", "unobserved", 1), strings.Replace(valid, "rs-1", "rs-2", 1),
		strings.Replace(valid, `"id":"rs-1",`, "", 1),
		strings.Replace(valid, `"id":"rs-1",`, `"id":"other","id":"rs-1",`, 1),
		strings.Replace(valid, `"input":`, `"previous_response_id":"old","input":`, 1),
		strings.Replace(valid, `"input":`, `"conversation":"old","input":`, 1),
		strings.Replace(valid, `"input":`, `"input":[],"input":`, 1),
		strings.Replace(valid, `"role":"user"`, `"type":"compaction"`, 1),
	} {
		got, count := c.restore("scope", []byte(body))
		if count != 0 || string(got) != body {
			t.Fatal("ambiguous history was changed")
		}
	}
	if _, count := c.restore("other-scope", []byte(valid)); count != 0 {
		t.Fatal("cross-scope restoration")
	}
	if got, count := c.restore("scope", []byte(valid)); count != 1 || string(got) != strings.Replace(valid, "partial", "final", 1) {
		t.Fatal("known completion not restored")
	}
	for key, entry := range c.entries {
		entry.expires = time.Now().Add(-time.Second)
		c.entries[key] = entry
	}
	if _, count := c.restore("scope", []byte(valid)); count != 0 || c.bytes != 0 {
		t.Fatal("expired completion restored")
	}
	c.remember("scope", items)
	items[reasoningHash("rs-1")].final = "conflicting"
	c.remember("scope", items)
	if _, count := c.restore("scope", []byte(valid)); count != 0 {
		t.Fatal("conflicting completion restored")
	}
	if c.bytes != 0 {
		t.Fatal("conflicting ciphertext retained")
	}
}

func TestCompletedReasoningCacheBounds(t *testing.T) {
	c := newCompletedReasoningCache()
	for i := 0; i < maxCompletedReasoningEntries+10; i++ {
		c.remember("scope", map[string]*completedReasoningItem{fmt.Sprint(i): {partial: "hash", final: strings.Repeat("x", 64<<10)}})
	}
	if len(c.entries) > maxCompletedReasoningEntries || c.bytes > maxCompletedReasoningBytes {
		t.Fatal("unbounded opaque cache")
	}
	var observed completedReasoningObservation
	data := []byte(`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs","encrypted_content":"partial"}}`)
	observed.observe(parseCodexSSEEvent("", data))
	data, _ = json.Marshal(map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "reasoning", "id": "rs", "encrypted_content": strings.Repeat("x", maxCompletedReasoningBytes+1)}})
	observed.observe(parseCodexSSEEvent("", data))
	if !observed.failed || len(observed.items) != 0 {
		t.Fatal("unbounded stream observation")
	}
}

func TestCompletedReasoningReadFailureDoesNotCommit(t *testing.T) {
	h := &Handler{tokenParser: NewTokenParser(), logger: newTestLogger()}
	cache := newCompletedReasoningCache()
	recovery := &responseRecoveryState{cfg: defaultResponseRecoveryConfig(), historyScope: "scope", completedHistory: cache}
	req := sameHistoryRequest(`{}`)
	req = req.WithContext(withResponseRecoveryState(req.Context(), recovery))
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Request: req,
		Body: &contextCanceledAfterReadCloser{reader: strings.NewReader(reasoningTransitionStream("final") + diagnosticCompletedSSE)}}
	h.handleStreaming(httptest.NewRecorder(), resp, "test", time.Now(), "original", "endpoint", "group", "channel", 1)
	if len(cache.entries) != 0 {
		t.Fatal("read failure committed repair state")
	}
}

type incompleteHistoryWriter struct{ *httptest.ResponseRecorder }

func (w incompleteHistoryWriter) Write(data []byte) (int, error) {
	return len(data) - 1, io.ErrClosedPipe
}

func TestCompletedReasoningDownstreamFailureDoesNotCommit(t *testing.T) {
	h := &Handler{tokenParser: NewTokenParser(), logger: newTestLogger()}
	cache := newCompletedReasoningCache()
	recovery := &responseRecoveryState{cfg: defaultResponseRecoveryConfig(), historyScope: "scope", completedHistory: cache}
	req := sameHistoryRequest(`{}`)
	req = req.WithContext(withResponseRecoveryState(req.Context(), recovery))
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Request: req,
		Body: io.NopCloser(strings.NewReader(reasoningTransitionStream("final") + diagnosticCompletedSSE))}
	h.handleStreaming(incompleteHistoryWriter{httptest.NewRecorder()}, resp, "test", time.Now(), "original", "endpoint", "group", "channel", 1)
	if len(cache.entries) != 0 {
		t.Fatal("incomplete downstream delivery committed repair state")
	}
}

func TestLargeCompletedItemAsFirstEventPreservesWireBytes(t *testing.T) {
	final := strings.Repeat("x", 300<<10)
	wire := "event: response.output_item.done\r\ndata: {\"item\":{\"type\":\"reasoning\",\"id\":\"rs\",\"summary\":[],\"encrypted_content\":\"" + final + "\"}}\r\n\r\n" + diagnosticCompletedSSE
	h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, wire)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sameHistoryRequest(`{"model":"original","stream":true,"input":"start"}`))
	if rec.Body.String() != wire || !trace.Successful {
		t.Fatalf("large first output was truncated: bytes=%d success=%v", rec.Body.Len(), trace.Successful)
	}
}

func TestCodexTransientReasoningIsNotACompletedRouteState(t *testing.T) {
	r := sameHistoryRequest(`{}`)
	for _, frame := range []string{
		`{"type":"response.created","response":{"status":"in_progress","output":[{"type":"reasoning","encrypted_content":"partial"}]}}`,
		`{"type":"response.in_progress","response":{"output":[{"type":"reasoning","encrypted_content":"partial"}]}}`,
		`{"type":"response.output_item.added","item":{"type":"reasoning","encrypted_content":"partial"}}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","status":"incomplete","encrypted_content":"partial"}}`,
	} {
		var data map[string]any
		if err := json.Unmarshal([]byte(frame), &data); err != nil {
			t.Fatal(err)
		}
		if keys := codexResponseStateKeys(r, data); len(keys) != 0 {
			t.Fatal("transient ciphertext registered as complete")
		}
	}
	var decoder codexSSEDecoder
	decoder.wireLine([]byte("event: response.output_item.done\n"))
	decoder.wireLine([]byte("data: {\"item\":{\"type\":\"reasoning\",\"encrypted_content\":\"final\"}}\n"))
	event := decoder.wireLine([]byte("\n"))
	if keys := codexResponseStateKeys(r, event.frame); len(keys) != 1 {
		t.Fatal("SSE event-name completion not recognized")
	}
}
