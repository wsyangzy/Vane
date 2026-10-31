package codexproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

type poolSubscriptionStore struct {
	*codexSubscriptionStore
	bindings []subscription.Binding
}

func (s *poolSubscriptionStore) ListBindings(context.Context) ([]subscription.Binding, error) {
	return append([]subscription.Binding(nil), s.bindings...), nil
}

func TestCodexPoolManagedFallbackResolvesSelectedAccountOnEverySend(t *testing.T) {
	first := subscription.Binding{ChannelID: 1, Channel: "first", GroupID: 11, AccountID: "first", Version: 1, Enabled: true}
	second := subscription.Binding{ChannelID: 2, Channel: "second", GroupID: 22, AccountID: "second", Version: 1, Enabled: true}
	s := &poolSubscriptionStore{codexSubscriptionStore: &codexSubscriptionStore{}, bindings: []subscription.Binding{first, second}}
	c := subscription.NewController(s)
	c.SetAccountResolver(func(_ context.Context, id string) (string, string, error) {
		return "token-" + id, "workspace-" + id, nil
	}, func(context.Context, string) error { return nil })
	digest := sha256.Sum256([]byte("pool-key"))
	runtime := routing.NewPoolRuntime(routing.PoolConfig{Pools: []routing.Pool{{ID: 1, Provider: "codex", Name: "managed", Default: true, Revision: 1, KeyHash: hex.EncodeToString(digest[:]), Strategy: routing.PoolSequential, Members: []routing.PoolMember{{ChannelID: 1, GroupID: 11, Tier: routing.TierPrimary}, {ChannelID: 2, GroupID: 22, Tier: routing.TierBackup}}}}, Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 11, Enabled: true, Managed: true}, {ChannelID: 2, GroupID: 22, Enabled: true, Managed: true}}}, nil, nil)
	c.SetPoolRuntime(runtime)
	channels := map[int64]*store.CodexChannelRecord{1: {ID: 1, Name: "first"}, 2: {ID: 2, Name: "second"}}
	manager := &EndpointManager{activeChannel: channels[1], channelsByID: channels, endpointsByChannel: map[int64][]*CodexEndpoint{}, unhealthyMap: map[int64]bool{}, failureDetails: map[int64]EndpointFailureDetail{}, logger: newTestLogger()}
	for _, b := range s.bindings {
		manager.endpointsByChannel[b.ChannelID] = []*CodexEndpoint{{ID: b.GroupID, ChannelID: b.ChannelID, CredentialGroupID: b.GroupID, AuthMode: subscription.ClientPassthrough, Name: b.Channel, BaseURL: subscription.OfficialBaseURL, FailoverEnabled: true}}
	}
	manager.SetPoolRuntime(runtime)
	h := NewHandler(manager, time.Second, config.RetryConfig{MaxTotalAttempts: 10, MaxTotalDuration: time.Second}, config.FailoverConfig{}, nil, newTestLogger(), nil)
	h.subscriptions = c
	var sent []string
	h.forwarder.client.Transport = codexSubscriptionTransport(func(r *http.Request) (*http.Response, error) {
		auth := r.Header.Get("Authorization")
		sent = append(sent, auth)
		if r.Header.Get("X-Relay-Key") != "" || r.Header.Get("User-Agent") != "codex-test" {
			t.Fatal("local key leaked or client identity was lost")
		}
		if auth == "Bearer token-first" {
			response := codexSubscriptionResponseFor(429, `{"error":{"code":"usage_limit_reached","resets_in_seconds":120}}`)
			response.Header.Set("Retry-After", "120")
			return response, nil
		}
		if auth != "Bearer token-second" || r.Header.Get("ChatGPT-Account-ID") != "workspace-second" {
			t.Fatal("selected backup reused another account credential")
		}
		return codexSubscriptionResponseFor(200, `{"status":"completed","output":[]}`), nil
	})
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","input":"hello","stream":false}`))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("X-Relay-Key", "pool-key")
		r.Header.Set("User-Agent", "codex-test")
		ctx := routing.WithPoolCaller(r.Context(), routing.PoolCaller{Provider: "codex", PoolID: 1, Revision: 1})
		r = r.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("fallback failed: %d %s", rec.Code, rec.Body.String())
		}
	}
	if !reflect.DeepEqual(sent, []string{"Bearer token-first", "Bearer token-second", "Bearer token-second"}) || manager.GetActiveChannel().ID != 1 {
		t.Fatalf("wrong account scheduling: %v", sent)
	}
	if len(runtime.Cooldowns()) != 0 || len(c.Status(context.Background(), first)) != 1 {
		t.Fatal("quota must stay in the subscription controller")
	}
}

func TestCodexPoolActualSendsStayBoundedAndNeverActivate(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		status, budget int
		disabled       bool
		groupsOff      bool
		want           []string
	}{
		{"channel_group_endpoint_fallback", 503, 10, false, false, []string{"/a/v1/responses", "/b/v1/responses", "/b2/v1/responses", "/c/v1/responses"}},
		{"invalid_request_is_terminal", 400, 10, false, false, []string{"/a/v1/responses"}},
		{"attempt_budget", 503, 2, false, false, []string{"/a/v1/responses", "/b/v1/responses"}},
		{"disabled_account", 503, 10, true, false, []string{"/b2/v1/responses", "/c/v1/responses"}},
		{"deselected_pool_groups", 503, 10, false, true, []string{"/c/v1/responses"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var paths []string
			raw := `{"model":"test-model","stream":false,"input":"hello","tools":[],"reasoning":{"effort":"high"}}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				body, _ := io.ReadAll(r.Body)
				if string(body) != raw {
					t.Errorf("request body changed: %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				if !strings.HasPrefix(r.URL.Path, "/c/") {
					w.WriteHeader(scenario.status)
					_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"unavailable"}}`)
					return
				}
				_, _ = io.WriteString(w, `{"id":"response","status":"completed","model":"test-model","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer upstream.Close()
			a := &store.CodexChannelRecord{ID: 1, Name: "A", FailoverEnabled: true}
			b := &store.CodexChannelRecord{ID: 2, Name: "B", FailoverEnabled: true}
			var eps []*CodexEndpoint
			for i, part := range []string{"a", "b", "b2", "c", "outside"} {
				ch, group := int64(1), int64(11)
				if i == 2 {
					group = 12
				} else if i == 3 {
					ch, group = 2, 22
				} else if i == 4 {
					ch, group = 3, 33
				}
				eps = append(eps, &CodexEndpoint{ID: int64(i + 1), ChannelID: ch, CredentialGroupID: group, Name: part, BaseURL: upstream.URL + "/" + part + "/v1", APIKey: part, FailoverEnabled: true, Timeout: time.Second, Priority: i})
			}
			manager := &EndpointManager{activeChannel: a, channelsByID: map[int64]*store.CodexChannelRecord{1: a, 2: b, 3: {ID: 3, Name: "outside", FailoverEnabled: true}}, endpointsByChannel: map[int64][]*CodexEndpoint{1: eps[:3], 2: eps[3:4], 3: eps[4:]}, unhealthyMap: map[int64]bool{}, failureDetails: map[int64]EndpointFailureDetail{}, logger: newTestLogger()}
			runtime := routing.NewPoolRuntime(routing.PoolConfig{Pools: []routing.Pool{{ID: 1, Provider: "codex", Name: "pool", Default: true, Strategy: routing.PoolSequential, Channels: []routing.PoolChannel{{1, routing.TierPrimary}, {2, routing.TierBackup}}, Members: []routing.PoolMember{{ChannelID: 1, GroupID: 11, Tier: routing.TierPrimary}, {ChannelID: 2, GroupID: 22, Tier: routing.TierBackup}, {ChannelID: 1, GroupID: 12, Tier: routing.TierPrimary}}}}, Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 11, Enabled: !scenario.disabled}, {ChannelID: 1, GroupID: 12, Enabled: true}, {ChannelID: 2, GroupID: 22, Enabled: true}, {ChannelID: 3, GroupID: 33, Enabled: true}}}, nil, nil)
			if scenario.groupsOff {
				cfg := runtime.Config()
				cfg.Pools[0].Members = []routing.PoolMember{{ChannelID: 2, GroupID: 22, Tier: routing.TierBackup}}
				runtime.Replace(cfg)
			}
			manager.SetPoolRuntime(runtime)
			h := NewHandler(manager, time.Second, config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: scenario.budget, MaxTotalDuration: time.Second}, config.FailoverConfig{Enabled: true}, nil, newTestLogger(), nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(raw)))
			if !reflect.DeepEqual(paths, scenario.want) {
				t.Fatalf("sends=%v want=%v; response=%d %s", paths, scenario.want, rec.Code, rec.Body.String())
			}
			if manager.GetActiveChannel().ID != 1 || manager.preferredEndpointID != 0 {
				t.Fatal("pool mutated global activation")
			}
			if strings.HasSuffix(paths[len(paths)-1], "/c/v1/responses") && rec.Code != 200 {
				t.Fatalf("fallback failed: %d %s", rec.Code, rec.Body.String())
			}
			if scenario.status == 400 && rec.Code != 400 {
				t.Fatalf("lost upstream status: %d", rec.Code)
			}
			if !scenario.groupsOff && len(runtime.Cooldowns()) == 0 && scenario.status == 503 {
				t.Fatal("failed sends did not register cooldown")
			}
		})
	}
}

func TestCodexPoolRecoveryCompletesOnlyAfterReadingProtocol(t *testing.T) {
	runtime := routing.NewPoolRuntime(routing.PoolConfig{Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 1, Enabled: true}}}, []routing.Cooldown{{Identity: "1/1", Endpoint: "9", Reason: "network_error", Until: time.Now().Add(-time.Second)}}, nil)
	ctx, _, err := routing.AdmitPool(httptest.NewRequest("POST", "/v1/responses", nil).Context(), runtime, 1, 1, "9", "test")
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","output":[]}`)
	}))
	defer upstream.Close()
	resp, err := NewForwarder(time.Second, newTestLogger()).Forward(httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx), []byte(`{}`), &CodexEndpoint{ID: 9, BaseURL: upstream.URL, FailoverEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.Cooldowns()) != 1 {
		t.Fatal("recovered before consuming response")
	}
	_, err = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.Cooldowns()) != 0 {
		t.Fatal("complete response did not recover")
	}
}

func TestCodexPoolAuxiliaryResponsesRecoverOnlyOnValidCompleteProtocol(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		recovered  bool
	}{
		{"/models", `{"models":[]}`, true},
		{"/models", `{"models":null}`, false},
		{"/responses/compact", `{"object":"response.compaction","output":[]}`, true},
		{"/responses/compact", `{"object":"response.compaction"}`, false},
		{"/responses/compact", `{"error":{"code":"model_not_found"}}`, false},
	} {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			runtime := routing.NewPoolRuntime(routing.PoolConfig{Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 1, Enabled: true}}}, []routing.Cooldown{{Identity: "1/1", Endpoint: "9", Reason: "network_error", Until: time.Now().Add(-time.Second)}}, nil)
			ctx, _, err := routing.AdmitPool(context.Background(), runtime, 1, 1, "9", "test")
			if err != nil {
				t.Fatal(err)
			}
			resp := codexSubscriptionResponseFor(200, tc.body)
			observeCodexPool(resp, ctx, tc.path)
			if len(runtime.Cooldowns()) == 0 {
				t.Fatal("recovered before reading the response")
			}
			if _, err = io.Copy(io.Discard, resp.Body); err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if (len(runtime.Cooldowns()) == 0) != tc.recovered {
				t.Fatalf("wrong recovery state: %+v", runtime.Cooldowns())
			}
		})
	}
}
