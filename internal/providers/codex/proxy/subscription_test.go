package codexproxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/codex/proxy/subscription"
	"ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/routing"
)

type codexSubscriptionStore struct {
	mu      sync.Mutex
	binding subscription.Binding
	pauses  []subscription.Pause
}

func (s *codexSubscriptionStore) ListBindings(context.Context) ([]subscription.Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []subscription.Binding{s.binding}, nil
}
func (s *codexSubscriptionStore) ListPauses(context.Context) ([]subscription.Pause, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]subscription.Pause(nil), s.pauses...), nil
}
func (s *codexSubscriptionStore) PutPause(_ context.Context, p subscription.Pause) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.pauses {
		if old.GroupID == p.GroupID && old.Version == p.Version && old.Model == p.Model {
			if p.Revision > old.Revision {
				s.pauses[i] = p
			}
			return nil
		}
	}
	s.pauses = append(s.pauses, p)
	return nil
}
func (s *codexSubscriptionStore) DeletePause(_ context.Context, p subscription.Pause) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.pauses {
		if old.GroupID == p.GroupID && old.Version == p.Version && old.Model == p.Model && old.Revision == p.Revision {
			s.pauses = append(s.pauses[:i], s.pauses[i+1:]...)
			break
		}
	}
	return nil
}

type codexSubscriptionTransport func(*http.Request) (*http.Response, error)

func TestCodexSubscriptionSSEQuotaPersistsWithoutChangingHTTPStatusOrBytes(t *testing.T) {
	for _, tc := range []struct {
		code   string
		paused bool
	}{{"usage_limit_reached", true}, {"rate_limit_error", false}} {
		t.Run(tc.code, func(t *testing.T) {
			s := &codexSubscriptionStore{binding: subscription.Binding{GroupID: 1, Version: 1, Enabled: true}}
			c := subscription.NewController(s)
			lease, e := c.Acquire(context.Background(), s.binding, "gpt-test")
			if e != nil {
				t.Fatal(e)
			}
			body := `data: {"type":"response.failed","response":{"status":"failed","error":{"type":"` + tc.code + `","resets_in_seconds":120}}}` + "\n\n"
			resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
			observeCodexSubscriptionResponse(resp, lease, "/responses")
			raw, e := io.ReadAll(resp.Body)
			resp.Body.Close()
			if e != nil || string(raw) != body || resp.StatusCode != 200 {
				t.Fatal("quota observation changed wire response")
			}
			if got := c.Status(context.Background(), s.binding); (len(got) > 0) != tc.paused {
				t.Fatalf("wrong protocol quota classification: %+v", got)
			}
		})
	}
}

func (f codexSubscriptionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func codexSubscriptionFixture(t *testing.T, roundTrip codexSubscriptionTransport) (*Handler, *codexSubscriptionStore, func(string, string) *http.Request) {
	t.Helper()
	sum := sha256.Sum256([]byte("local-key-sentinel"))
	s := &codexSubscriptionStore{binding: subscription.Binding{GroupID: 1, ChannelID: 1, Channel: "official", Version: 1, Enabled: true, KeyHash: hex.EncodeToString(sum[:]), BackupTargets: []routing.Target{{ChannelID: 2, GroupID: 2}}}}
	c := subscription.NewController(s)
	official := &store.CodexChannelRecord{ID: 1, Name: "official", FailoverEnabled: true}
	backup := &store.CodexChannelRecord{ID: 2, Name: "backup", FailoverEnabled: true}
	global := &store.CodexChannelRecord{ID: 3, Name: "global", FailoverEnabled: true}
	manager := &EndpointManager{activeChannel: global, channelsByID: map[int64]*store.CodexChannelRecord{1: official, 2: backup, 3: global}, endpointsByChannel: map[int64][]*CodexEndpoint{1: {{ID: 1, ChannelID: 1, CredentialGroupID: 1, AuthMode: subscription.ClientPassthrough, Name: "official", BaseURL: subscription.OfficialBaseURL, FailoverEnabled: true}}, 2: {{ID: 2, ChannelID: 2, CredentialGroupID: 2, Name: "backup", BaseURL: "https://backup.invalid/v1", APIKey: "backup-key-sentinel", FailoverEnabled: true}}}, unhealthyMap: map[int64]bool{}, failureDetails: map[int64]EndpointFailureDetail{}, logger: newTestLogger()}
	h := NewHandler(manager, time.Second, config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 4, MaxTotalDuration: time.Second}, config.FailoverConfig{}, nil, newTestLogger(), nil)
	h.subscriptions = c
	h.forwarder.client = &http.Client{Transport: roundTrip}
	h.streamFwd.client = &http.Client{Transport: roundTrip}
	request := func(path, body string) *http.Request {
		method := "POST"
		if strings.Contains(path, "/models") {
			method = "GET"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("X-Relay-Key", "local-key-sentinel")
		r.Header.Set("Authorization", "Bearer official-oauth-sentinel")
		r.Header.Set("ChatGPT-Account-ID", "workspace-sentinel")
		r.Header.Set("User-Agent", "codex-test-client")
		ctx, handled, e := c.Authenticate(r)
		if !handled || e != nil {
			t.Fatalf("authenticate: %v", e)
		}
		return r.WithContext(ctx)
	}
	return h, s, request
}
func codexSubscriptionResponseFor(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestSubscriptionCodexOfficialPathsPreserveBodiesAndIdentity(t *testing.T) {
	for _, path := range []string{"/backend-api/codex/responses", "/backend-api/codex/responses/compact", "/backend-api/codex/models?client_version=1.0", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			body := `{ "model" : "gpt-test", "stream":false,"input":[{"role":"user","content":"hello"}],"tools":[{"type":"function","name":"read","parameters":{"type":"object"}}],"reasoning":{"effort":"high"},"max_output_tokens":1024 }`
			calls := 0
			h, _, request := codexSubscriptionFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != body {
					t.Fatal("request body changed")
				}
				if r.URL.Scheme != "https" || r.URL.Host != "chatgpt.com" || r.Host != "chatgpt.com" || !strings.HasPrefix(r.URL.Path, "/backend-api/codex/") {
					t.Fatal("wrong official destination")
				}
				if r.Header.Get("Authorization") != "Bearer official-oauth-sentinel" || r.Header.Get("ChatGPT-Account-ID") != "workspace-sentinel" || r.Header.Get("X-Relay-Key") != "" || r.Header.Get("User-Agent") != "codex-test-client" {
					t.Fatal("official identity/header boundary failed")
				}
				if strings.HasSuffix(r.URL.Path, "/models") {
					if r.URL.RawQuery != "client_version=1.0" {
						t.Fatal("model query changed")
					}
					return codexSubscriptionResponseFor(200, `{"models":[]}`), nil
				}
				if strings.HasSuffix(r.URL.Path, "/compact") {
					return codexSubscriptionResponseFor(200, `{"object":"response.compaction","output":[]}`), nil
				}
				return codexSubscriptionResponseFor(200, `{"status":"completed","output":[]}`), nil
			})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, request(path, body))
			if rec.Code != 200 || calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
			}
			if h.manager.GetActiveChannel().ID != 3 {
				t.Fatal("global active channel changed")
			}
		})
	}
}

func TestSubscriptionCodexQuotaFallbackAndAllPausedSendNothing(t *testing.T) {
	var hosts []string
	h, s, request := codexSubscriptionFixture(t, func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Host)
		if r.URL.Host == "chatgpt.com" {
			resp := codexSubscriptionResponseFor(429, `{"error":{"type":"usage_limit_reached","resets_in_seconds":3600}}`)
			resp.Header.Set("Retry-After", "3600")
			return resp, nil
		}
		if r.Header.Get("Authorization") != "Bearer backup-key-sentinel" || r.Header.Get("ChatGPT-Account-ID") != "" || r.Header.Get("X-Relay-Key") != "" {
			t.Fatal("official/local credential leaked to backup")
		}
		if r.URL.Path != "/v1/responses" {
			t.Fatal("backend path leaked to API backup")
		}
		return codexSubscriptionResponseFor(200, `{"status":"completed","output":[]}`), nil
	})
	for range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request("/backend-api/codex/responses", `{"model":"gpt-test","input":[]}`))
		if rec.Code != 200 {
			t.Fatalf("fallback failed: %d %s", rec.Code, rec.Body.String())
		}
	}
	if fmt.Sprint(hosts) != "[chatgpt.com backup.invalid backup.invalid]" {
		t.Fatalf("unexpected sends: %v", hosts)
	}
	s.mu.Lock()
	s.binding.BackupTargets = nil
	s.binding.Version++
	s.pauses = []subscription.Pause{{GroupID: 1, Version: s.binding.Version, Model: "*", Revision: 1, ResumeNotBefore: time.Now().Add(time.Hour)}}
	s.mu.Unlock()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request("/backend-api/codex/responses", `{"model":"gpt-test"}`))
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" || len(hosts) != 3 {
		t.Fatalf("all paused: status=%d sends=%d", rec.Code, len(hosts))
	}
}

func TestSubscriptionCodexUnauthorizedAndRedirectNeverReplayToBackup(t *testing.T) {
	for _, status := range []int{401, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			h, s, request := codexSubscriptionFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				resp := codexSubscriptionResponseFor(status, `{"error":{"message":"official-oauth-sentinel workspace-sentinel"}}`)
				resp.Header.Set("Authorization", "echo-secret")
				resp.Header.Set("X-Relay-Key", "echo-key")
				if status == 302 {
					resp.Header.Set("Location", "https://evil.invalid")
				}
				return resp, nil
			})
			if status == 302 {
				s.mu.Lock()
				s.binding.BackupTargets = nil
				s.mu.Unlock()
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, request("/backend-api/codex/responses", `{"model":"gpt-test"}`))
			if calls != 1 {
				t.Fatal("unauthorized or redirect replayed")
			}
			if status == 401 && rec.Code != 401 {
				t.Fatal("401 not returned")
			}
			if rec.Header().Get("Location") != "" || rec.Header().Get("Authorization") != "" || rec.Header().Get("X-Relay-Key") != "" || strings.Contains(rec.Body.String(), "-sentinel") {
				t.Fatal("credentials/redirect reflected")
			}
		})
	}
}

func TestSubscriptionCodexModelsNeverUseGenericBackupAndWebSocketFallsBack(t *testing.T) {
	calls := 0
	h, s, request := codexSubscriptionFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return codexSubscriptionResponseFor(200, `{"models":[]}`), nil
	})
	s.mu.Lock()
	s.binding.Enabled = false
	s.mu.Unlock()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request("/backend-api/codex/models", ""))
	if rec.Code != 429 || calls != 0 {
		t.Fatal("models used generic API fallback")
	}
	r := request("/backend-api/codex/responses", `{"model":"gpt-test"}`)
	r.Method = "GET"
	r.Header.Set("Upgrade", "websocket")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 426 || calls != 0 {
		t.Fatal("WebSocket did not ask Codex to use HTTP")
	}
}

func TestSubscriptionCodexRecoveryRequiresCompleteProtocolEOF(t *testing.T) {
	for _, kind := range []string{"json", "sse", "long-sse", "truncated-sse", "gzip", "truncated-gzip", "stream-error", "compact", "models"} {
		t.Run(kind, func(t *testing.T) {
			s := &codexSubscriptionStore{binding: subscription.Binding{GroupID: 1, ChannelID: 1, Version: 1, Enabled: true}, pauses: []subscription.Pause{{GroupID: 1, Version: 1, Model: "*", Revision: 1, ResumeNotBefore: time.Now().Add(-time.Second)}}}
			c := subscription.NewController(s)
			lease, e := c.Acquire(context.Background(), s.binding, "gpt-test")
			if e != nil {
				t.Fatal(e)
			}
			body := []byte(`{"status":"completed","output":[]}`)
			path := "/responses"
			if strings.Contains(kind, "sse") || kind == "stream-error" {
				body = []byte("data: {\"type\":\"response.created\"}\n\n")
				if kind == "long-sse" {
					for range 3500 {
						body = append(body, []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\""+strings.Repeat("x", 1024)+"\"}\n\n")...)
					}
				}
				if kind == "stream-error" {
					body = append(body, []byte("data: {\"type\":\"response.failed\"}\n\n")...)
				} else if kind != "truncated-sse" {
					body = append(body, []byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")...)
				}
			}
			if kind == "compact" {
				path = "/responses/compact"
				body = []byte(`{"object":"response.compaction","output":[]}`)
			}
			if kind == "models" {
				path = "/models"
				body = []byte(`{"models":[]}`)
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}
			if strings.Contains(kind, "gzip") {
				var compressed bytes.Buffer
				w := gzip.NewWriter(&compressed)
				_, _ = w.Write(body)
				_ = w.Close()
				body = compressed.Bytes()
				if kind == "truncated-gzip" {
					body = body[:len(body)-4]
				}
				resp.Body = io.NopCloser(bytes.NewReader(body))
				resp.Header.Set("Content-Encoding", "gzip")
			}
			observeCodexSubscriptionResponse(resp, lease, path)
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			status := c.Status(context.Background(), s.binding)
			wantPaused := strings.HasPrefix(kind, "truncated") || kind == "stream-error"
			if (len(status) > 0) != wantPaused {
				t.Fatalf("recovery state wrong: %+v", status)
			}
		})
	}
}
