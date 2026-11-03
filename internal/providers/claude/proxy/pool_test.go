package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

type poolSubscriptionStore struct {
	*subscriptionIntegrationStore
	bindings []subscription.Binding
}

func (s *poolSubscriptionStore) ListBindings(context.Context) ([]subscription.Binding, error) {
	return append([]subscription.Binding(nil), s.bindings...), nil
}

func TestClaudePoolManagedFallbackResolvesSelectedAccountOnEverySend(t *testing.T) {
	first := subscription.Binding{Channel: "first", GroupID: 11, AccountID: "first", Version: 1, Enabled: true}
	second := subscription.Binding{Channel: "second", GroupID: 22, AccountID: "second", Version: 1, Enabled: true}
	s := &poolSubscriptionStore{subscriptionIntegrationStore: &subscriptionIntegrationStore{}, bindings: []subscription.Binding{first, second}}
	c := subscription.NewController(s)
	c.SetAccountResolver(func(_ context.Context, id string) (string, string, error) { return "token-" + id, "", nil }, func(context.Context, string) error { return nil })
	digest := sha256.Sum256([]byte("pool-key"))
	runtime := routing.NewPoolRuntime(routing.PoolConfig{Pools: []routing.Pool{{ID: 1, Provider: "claude", Name: "managed", Default: true, Revision: 1, KeyHash: hex.EncodeToString(digest[:]), Strategy: routing.PoolSequential, Members: []routing.PoolMember{{ChannelID: 1, GroupID: 11, Tier: routing.TierPrimary}, {ChannelID: 2, GroupID: 22, Tier: routing.TierBackup}}}}, Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 11, Enabled: true, Managed: true}, {ChannelID: 2, GroupID: 22, Enabled: true, Managed: true}}}, nil, nil)
	c.SetPoolRuntime(runtime)
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 10, MaxTotalDuration: time.Second}}
	for _, b := range s.bindings {
		cfg.Endpoints = append(cfg.Endpoints, config.EndpointConfig{Name: b.Channel, Channel: b.Channel, CredentialGroupID: b.GroupID, URL: subscription.OfficialOrigin, AuthMode: subscription.ClientPassthrough, Timeout: time.Second})
	}
	manager := endpoint.NewManager(cfg)
	manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "first", 2: "second"}})
	manager.SetPoolRuntime(runtime)
	if err := manager.ManualActivateGroup("first"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(manager, cfg)
	h.SetSubscriptionController(c)
	client, err := h.forwarder.NewClient(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	client.Transport.(*http.Transport).RegisterProtocol("https", subscriptionIntegrationTransport(func(r *http.Request) (*http.Response, error) {
		auth := r.Header.Get("Authorization")
		sent = append(sent, auth)
		if r.Header.Get("X-Relay-Key") != "" || r.Header.Get("User-Agent") != "claude-test" {
			t.Fatal("local key leaked or client identity was lost")
		}
		if auth == "Bearer token-first" {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"120"}, "Content-Type": {"application/json"}, "Anthropic-Ratelimit-Unified-5h-Status": {"rejected"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_error"}}`))}, nil
		}
		if auth != "Bearer token-second" {
			t.Fatal("selected backup reused another account credential")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"message","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))}, nil
	}))
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","messages":[],"max_tokens":16,"stream":false}`))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("X-Relay-Key", "pool-key")
		r.Header.Set("User-Agent", "claude-test")
		r = r.WithContext(routing.WithPoolCaller(r.Context(), routing.PoolCaller{Provider: "claude", PoolID: 1, Revision: 1}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("fallback failed: %d %s", rec.Code, rec.Body.String())
		}
	}
	if !reflect.DeepEqual(sent, []string{"Bearer token-first", "Bearer token-second", "Bearer token-second"}) || manager.ActiveChannelName() != "first" {
		t.Fatalf("wrong account scheduling: %v", sent)
	}
	if len(runtime.Cooldowns()) != 0 || len(c.Status(context.Background(), first)) != 1 {
		t.Fatal("quota must stay in the subscription controller")
	}
}

func TestClaudePoolActualSendsStayBoundedAndNeverActivate(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		status, budget int
		disabled       bool
		groupsOff      bool
		want           []string
	}{
		{"channel_group_endpoint_fallback", 503, 10, false, false, []string{"/a/v1/messages", "/b/v1/messages", "/b2/v1/messages", "/c/v1/messages"}},
		{"invalid_request_is_terminal", 400, 10, false, false, []string{"/a/v1/messages"}},
		{"attempt_budget", 503, 2, false, false, []string{"/a/v1/messages", "/b/v1/messages"}},
		{"disabled_account", 503, 10, true, false, []string{"/b2/v1/messages", "/c/v1/messages"}},
		{"deselected_pool_groups", 503, 10, false, true, []string{"/c/v1/messages"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var paths []string
			raw := `{"model":"test-model","stream":false,"messages":[],"max_tokens":32,"tools":[]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				body, _ := io.ReadAll(r.Body)
				if string(body) != raw {
					t.Errorf("request body changed: %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				if !strings.HasPrefix(r.URL.Path, "/c/") {
					w.WriteHeader(scenario.status)
					io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"unavailable"}}`)
					return
				}
				io.WriteString(w, `{"id":"message","type":"message","model":"test-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer server.Close()
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: scenario.budget, MaxTotalDuration: time.Second}, Failover: config.FailoverConfig{Enabled: true}}
			for i, part := range []string{"a", "b", "b2", "c", "outside"} {
				channel, group := "A", int64(11)
				if i == 2 {
					group = 12
				} else if i == 3 {
					channel, group = "B", 22
				} else if i == 4 {
					channel, group = "outside", 33
				}
				cfg.Endpoints = append(cfg.Endpoints, config.EndpointConfig{Name: part, Channel: channel, CredentialGroupID: group, URL: server.URL + "/" + part, Priority: i, SkipHealthCheck: true, Timeout: time.Second})
			}
			manager := endpoint.NewManager(cfg)
			if err := manager.ManualActivateGroup("A"); err != nil {
				t.Fatal(err)
			}
			manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "A", 2: "B", 3: "outside"}})
			runtime := routing.NewPoolRuntime(routing.PoolConfig{Pools: []routing.Pool{{ID: 1, Provider: "claude", Name: "pool", Default: true, Strategy: routing.PoolSequential, Channels: []routing.PoolChannel{{ChannelID: 1, Tier: routing.TierPrimary}, {ChannelID: 2, Tier: routing.TierBackup}}, Members: []routing.PoolMember{{ChannelID: 1, GroupID: 11, Tier: routing.TierPrimary}, {ChannelID: 2, GroupID: 22, Tier: routing.TierBackup}, {ChannelID: 1, GroupID: 12, Tier: routing.TierPrimary}}}}, Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 11, Enabled: !scenario.disabled}, {ChannelID: 1, GroupID: 12, Enabled: true}, {ChannelID: 2, GroupID: 22, Enabled: true}, {ChannelID: 3, GroupID: 33, Enabled: true}}}, nil, nil)
			if scenario.groupsOff {
				poolCfg := runtime.Config()
				poolCfg.Pools[0].Members = []routing.PoolMember{{ChannelID: 2, GroupID: 22, Tier: routing.TierBackup}}
				runtime.Replace(poolCfg)
			}
			manager.SetPoolRuntime(runtime)
			rec := httptest.NewRecorder()
			NewHandler(manager, cfg).ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(raw)))
			if !reflect.DeepEqual(paths, scenario.want) {
				t.Fatalf("sends=%v want=%v; response=%d %s", paths, scenario.want, rec.Code, rec.Body.String())
			}
			if manager.ActiveChannelName() != "A" {
				t.Fatal("pool changed global activation")
			}
			if scenario.status == 400 && rec.Code != 400 {
				t.Fatalf("lost upstream status: %d", rec.Code)
			}
			if strings.HasSuffix(paths[len(paths)-1], "/c/v1/messages") && rec.Code != 200 {
				t.Fatalf("fallback failed: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
