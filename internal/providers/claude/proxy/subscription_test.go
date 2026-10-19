package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/proxy/subscription"
	"ai-relay-forwarder/internal/routing"
)

type subscriptionIntegrationStore struct {
	mu      sync.Mutex
	binding subscription.Binding
	pauses  []subscription.Pause
}

func (s *subscriptionIntegrationStore) ListBindings(context.Context) ([]subscription.Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []subscription.Binding{s.binding}, nil
}
func (s *subscriptionIntegrationStore) ListPauses(context.Context) ([]subscription.Pause, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]subscription.Pause(nil), s.pauses...), nil
}
func (s *subscriptionIntegrationStore) PutPause(_ context.Context, p subscription.Pause) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pauses = []subscription.Pause{p}
	return nil
}
func (s *subscriptionIntegrationStore) DeletePause(context.Context, subscription.Pause) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pauses = nil
	return nil
}

type subscriptionIntegrationTransport func(*http.Request) (*http.Response, error)

func (f subscriptionIntegrationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newSubscriptionIntegrationFixture(t *testing.T, backup http.HandlerFunc) (*Handler, *subscriptionIntegrationStore) {
	t.Helper()
	server := httptest.NewServer(backup)
	t.Cleanup(server.Close)
	hash := sha256.Sum256([]byte("local-key-sentinel"))
	s := &subscriptionIntegrationStore{binding: subscription.Binding{GroupID: 1, Channel: "official", Version: 1, Enabled: true, KeyHash: hex.EncodeToString(hash[:]), BackupTargets: []routing.Target{{ChannelID: 2, GroupID: 2, UpstreamModel: "backup-model"}}}}
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 3, MaxTotalAttempts: 4, MaxTotalDuration: time.Second}, Endpoints: []config.EndpointConfig{
		{Name: "official-a", URL: subscription.OfficialOrigin, Channel: "official", CredentialGroupID: 1, AuthMode: subscription.ClientPassthrough, Timeout: time.Second},
		{Name: "official-b", URL: subscription.OfficialOrigin, Channel: "official", CredentialGroupID: 1, AuthMode: subscription.ClientPassthrough, Timeout: time.Second},
		{Name: "backup", URL: server.URL, Channel: "backup", CredentialGroupID: 2, Token: "backup-key", SkipHealthCheck: true, SupportsCountTokens: true, Timeout: time.Second},
	}}
	manager := endpoint.NewManager(cfg)
	manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "official", 2: "backup"}})
	if err := manager.ManualActivateGroup("backup"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(manager, cfg)
	h.SetSubscriptionController(subscription.NewController(s))
	return h, s
}
func subscriptionRequest(path string, stream bool) *http.Request {
	r := httptest.NewRequest("POST", path+"?beta=true", strings.NewReader(fmt.Sprintf(`{"model":"original","stream":%t,"max_tokens":16,"messages":[],"tools":[{"input_schema":{"model":"nested"}}]}`, stream)))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Relay-Key", "local-key-sentinel")
	r.Header.Set("Authorization", "Bearer oauth-sentinel")
	return r
}

func TestSubscriptionHandlerPausedOfficialUsesAuthorizedBackupForEveryPath(t *testing.T) {
	for _, tc := range []struct {
		path   string
		stream bool
	}{{"/v1/messages", false}, {"/v1/messages", true}, {"/v1/messages/count_tokens", false}} {
		t.Run(fmt.Sprintf("%s/%t", tc.path, tc.stream), func(t *testing.T) {
			var sends atomic.Int32
			h, s := newSubscriptionIntegrationFixture(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Header.Get("Authorization") != "Bearer backup-key" || r.Header.Get("X-Relay-Key") != "" || strings.Contains(string(body), "oauth-sentinel") || !strings.Contains(string(body), `"model":"backup-model"`) {
					t.Error("backup auth/model boundary failed")
				}
				if r.URL.Path == "/v1/messages/count_tokens" {
					io.WriteString(w, `{"input_tokens":5}`)
				} else if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"backup-model\",\"usage\":{\"input_tokens\":1}}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					io.WriteString(w, `{"type":"message","model":"backup-model","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
				}
			})
			s.pauses = []subscription.Pause{{GroupID: 1, Version: 1, Model: "*", Revision: 1, ResumeNotBefore: time.Now().Add(time.Hour)}}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, subscriptionRequest(tc.path, tc.stream))
			if rec.Code != 200 || sends.Load() != 1 {
				t.Fatalf("status=%d sends=%d body=%s", rec.Code, sends.Load(), rec.Body.String())
			}
			if h.endpointManager.ActiveChannelName() != "backup" {
				t.Fatal("request changed global active channel")
			}
		})
	}
}

func TestSubscriptionHandlerOfficial401ReturnsToClientAndRefreshCanSucceed(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		t.Run(path, func(t *testing.T) {
			var backups atomic.Int32
			var official atomic.Int32
			h, _ := newSubscriptionIntegrationFixture(t, func(w http.ResponseWriter, r *http.Request) { backups.Add(1); w.WriteHeader(500) })
			client, err := h.forwarder.NewClient(time.Second)
			if err != nil {
				t.Fatal(err)
			}
			tr := client.Transport.(*http.Transport)
			// RegisterProtocol replaces only this fixture's https transport. No official
			// network request, system certificate change or real credential is involved.
			tr.RegisterProtocol("https", subscriptionIntegrationTransport(func(r *http.Request) (*http.Response, error) {
				official.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.URL.Host != "api.anthropic.com" || r.Header.Get("Authorization") != "Bearer oauth-sentinel" || r.Header.Get("X-Relay-Key") != "" || !strings.Contains(string(body), `"model":"original"`) {
					t.Error("official boundary failed")
				}
				status, payload := 401, `{"type":"error","error":{"type":"authentication_error","message":"refresh required"}}`
				if official.Load() > 1 {
					status = 200
					payload = `{"type":"message","model":"original","stop_reason":"end_turn","content":[],"input_tokens":2,"usage":{"input_tokens":2,"output_tokens":1}}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
			}))
			first := httptest.NewRecorder()
			h.ServeHTTP(first, subscriptionRequest(path, false))
			if first.Code != 401 || backups.Load() != 0 || official.Load() != 1 || !strings.Contains(first.Body.String(), "refresh required") {
				t.Fatalf("401 hidden: %d %s sends=%d/%d", first.Code, first.Body.String(), official.Load(), backups.Load())
			}
			second := httptest.NewRecorder()
			h.ServeHTTP(second, subscriptionRequest(path, false))
			if second.Code != 200 || official.Load() != 2 || backups.Load() != 0 {
				t.Fatalf("refreshed request failed: %d %s", second.Code, second.Body.String())
			}
		})
	}
}

func TestSubscriptionHandlerAllPausedRejectsWithoutUpstream(t *testing.T) {
	h, s := newSubscriptionIntegrationFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected backup send") })
	s.binding.BackupTargets = nil
	s.pauses = []subscription.Pause{{GroupID: 1, Version: 1, Model: "*", Revision: 1, ResumeNotBefore: time.Now().Add(time.Hour)}}
	for _, tc := range []struct {
		path   string
		stream bool
	}{{"/v1/messages", false}, {"/v1/messages", true}, {"/v1/messages/count_tokens", false}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, subscriptionRequest(tc.path, tc.stream))
		if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("paused request: %d %s", rec.Code, rec.Body.String())
		}
	}

}

func TestSubscriptionHandlerQuotaSkipsOtherURLs(t *testing.T) {
	var backups, official atomic.Int32
	h, s := newSubscriptionIntegrationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		backups.Add(1)
		io.WriteString(w, `{"type":"message","model":"backup-model","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	client, err := h.forwarder.NewClient(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.Transport.(*http.Transport).RegisterProtocol("https", subscriptionIntegrationTransport(func(r *http.Request) (*http.Response, error) {
		official.Add(1)
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"60"}, "Anthropic-Ratelimit-Unified-5h-Status": []string{"rejected"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_error"}}`))}, nil
	}))
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, subscriptionRequest("/v1/messages", false))
		if rec.Code != 200 {
			t.Fatalf("quota fallback %d: %s", rec.Code, rec.Body.String())
		}
	}
	if official.Load() != 1 || backups.Load() != 2 {
		t.Fatalf("same credential retried via URL: official=%d backup=%d", official.Load(), backups.Load())
	}
	state := h.subscriptions.Status(context.Background(), s.binding)
	if len(state) != 1 || state[0].Model != "*" || state[0].State != "paused" {
		t.Fatalf("quota state missing: %+v", state)
	}
}
