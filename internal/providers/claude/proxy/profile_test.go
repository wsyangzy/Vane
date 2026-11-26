package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/middleware"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestClaudeProfileFailoverChargesOnlyTheActualSuccessfulGroup(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/a/") {
					w.WriteHeader(http.StatusServiceUnavailable)
					io.WriteString(w, `{"error":{"type":"api_error"}}`)
					return
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"test\",\"usage\":{\"input_tokens\":10}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"type":"message","model":"test","content":[],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`)
			}))
			t.Cleanup(server.Close)
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 3, MaxTotalDuration: time.Second * 3}}
			cfg.Endpoints = []config.EndpointConfig{
				{Name: "same-name", Channel: "A", CredentialGroupID: 11, CredentialGroupName: "same-group", URL: server.URL + "/a", SkipHealthCheck: true},
				{Name: "same-name", Channel: "B", CredentialGroupID: 22, CredentialGroupName: "same-group", URL: server.URL + "/b", SkipHealthCheck: true},
			}
			h, manager := profileClaudeHandler(t, cfg, 3, nil)
			tracker := newLifecycleTestTracker(t)
			tracker.UpdatePricing(map[string]tracking.ModelPricing{tracking.ModelPricingKey("claude", "test", ""): {Input: 10, Output: 20}})
			first, second := tracking.DefaultEndpointMultiplier(), tracking.DefaultEndpointMultiplier()
			first.CostMultiplier, second.CostMultiplier = 100, 0.1
			tracker.UpdateEndpointMultipliers(map[string]tracking.EndpointMultiplier{
				tracking.EndpointMultiplierGroupKey("claude", "A", "same-group", "same-name"): first,
				tracking.EndpointMultiplierGroupKey("claude", "B", "same-group", "same-name"): second,
			})
			h.SetUsageTracker(tracker)
			h.monitoringMiddleware = middleware.NewMonitoringMiddleware(manager)
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"messages":[],"max_tokens":16}`, streaming)))
			req = req.WithContext(context.WithValue(req.Context(), "conn_id", "failover-billing"))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if err := tracker.ForceFlush(); err != nil {
				t.Fatal(err)
			}
			for _, resource := range []struct {
				channel string
				cost    float64
			}{{"A", 0}, {"B", 0.000014}} {
				used, _, err := tracker.CredentialGroupDailyCost(context.Background(), "claude", resource.channel, "same-group")
				if err != nil || used < resource.cost-1e-12 || used > resource.cost+1e-12 {
					t.Fatalf("cost attributed to %s = %.12f, want %.12f (err=%v)", resource.channel, used, resource.cost, err)
				}
			}
		})
	}
}

func profileClaudeHandler(t *testing.T, cfg *config.Config, attempts int, trace func(routing.DispatchTrace), settlements ...func(routing.AttemptSettlement)) (*Handler, *endpoint.Manager) {
	t.Helper()
	manager := endpoint.NewManager(cfg)
	manager.SetRouteConfig(&endpoint.RouteConfig{ChannelNames: map[int64]string{1: "A", 2: "B"}})
	if err := manager.ManualActivateGroup("A"); err != nil {
		t.Fatal(err)
	}
	manager.UpdateChannelFailoverEnabled(map[string]bool{"B": false})
	legacyDisabled := false
	for _, ep := range manager.GetAllEndpoints() {
		ep.Config.FailoverEnabled = &legacyDisabled
	}
	profile := routing.DefaultProfile("claude")
	profile.ID, profile.Name, profile.IsDefault = 1, "test", true
	profile.Strategy, profile.CandidateMode = routing.StrategyOrder, routing.UnifiedCandidates
	profile.MaxAttempts, profile.MaxDuration = attempts, time.Second*2
	profile.Members = []routing.RoutingMember{{ChannelID: 1, Enabled: true}, {ChannelID: 2, Enabled: true}}
	candidates := []routing.DispatchCandidate{}
	for _, channel := range []struct {
		id   int64
		name string
	}{{1, "A"}, {2, "B"}} {
		for _, ep := range manager.GetAllEndpoints() {
			if endpoint.ChannelKey(ep) != channel.name {
				continue
			}
			candidates = append(candidates, routing.DispatchCandidate{Provider: "claude", ChannelID: channel.id, GroupID: ep.Config.CredentialGroupID, EndpointID: endpoint.EndpointKeyForConfig(ep.Config), Protocol: "messages", Enabled: true, Position: len(candidates), StateVersion: ep.RouteStateVersion()})
		}
	}
	accountConfig := routing.PoolConfig{}
	seen := map[[2]int64]bool{}
	for _, candidate := range candidates {
		key := [2]int64{candidate.ChannelID, candidate.GroupID}
		if !seen[key] {
			// Exercise real sends with legacy participation disabled.
			accountConfig.Accounts = append(accountConfig.Accounts, routing.AccountState{ChannelID: candidate.ChannelID, GroupID: candidate.GroupID, Enabled: false})
			seen[key] = true
		}
	}
	runtime := routing.NewPoolRuntime(accountConfig, nil, nil)
	runtime.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile}, Candidates: candidates, OnTrace: trace, OnSettlement: func(value routing.AttemptSettlement) {
		for _, settle := range settlements {
			settle(value)
		}
	}}))
	manager.SetPoolRuntime(runtime)
	return NewHandler(manager, cfg), manager
}

func TestClaudeProfileActualSendsUseExactCandidatesAndSharedBudget(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		status, budget int
		errorBody      string
		want           []string
	}{
		{"all_candidates", 503, 10, `{"error":{"type":"api_error"}}`, []string{"/a/v1/messages", "/b/v1/messages", "/c/v1/messages"}},
		{"model_scope", 404, 10, `{"error":{"code":"model_not_found"}}`, []string{"/a/v1/messages", "/c/v1/messages"}},
		{"credential_scope", 401, 10, `{"error":{"type":"authentication_error"}}`, []string{"/a/v1/messages", "/c/v1/messages"}},
		{"request_scope", 400, 10, `{"error":{"type":"invalid_request_error"}}`, []string{"/a/v1/messages"}},
		{"shared_budget", 503, 1, `{"error":{"type":"api_error"}}`, []string{"/a/v1/messages"}},
		{"global_budget", 503, 10, `{"error":{"type":"api_error"}}`, []string{"/a/v1/messages"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var paths []string
			raw := `{"model":"test-model","stream":false,"messages":[{"role":"user","content":"hello"}],"max_tokens":32,"tools":[]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				body, _ := io.ReadAll(r.Body)
				if string(body) != raw {
					t.Errorf("request changed: %s", body)
				}
				if r.Header.Get("User-Agent") != "claude-profile-test" || r.Header.Get("X-Relay-Key") != "" {
					t.Error("identity lost or local key leaked")
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(r.URL.Path, "/c/") {
					io.WriteString(w, `{"type":"message","content":[],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`)
					return
				}
				w.WriteHeader(scenario.status)
				io.WriteString(w, scenario.errorBody)
			}))
			defer server.Close()
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 5, MaxTotalAttempts: 10, MaxTotalDuration: time.Second * 3}, Failover: config.FailoverConfig{Enabled: true}}
			if scenario.name == "global_budget" {
				cfg.Retry.MaxTotalAttempts = 1
			}
			for i, name := range []string{"a", "b", "c", "outside"} {
				channel, group := "A", int64(11)
				if i == 2 {
					channel, group = "B", 22
				}
				if i == 3 {
					channel, group = "outside", 33
				}
				cfg.Endpoints = append(cfg.Endpoints, config.EndpointConfig{Name: name, Channel: channel, CredentialGroupID: group, URL: server.URL + "/" + name, Priority: i, SkipHealthCheck: true, Timeout: time.Second})
			}
			var trace routing.DispatchTrace
			h, manager := profileClaudeHandler(t, cfg, scenario.budget, func(value routing.DispatchTrace) { trace = value })
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(raw))
			r.Header.Set("User-Agent", "claude-profile-test")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if !reflect.DeepEqual(paths, scenario.want) {
				t.Fatalf("sends=%v want=%v response=%d %s", paths, scenario.want, rec.Code, rec.Body.String())
			}
			if manager.ActiveChannelName() != "A" {
				t.Fatal("global activation changed")
			}
			if trace.Attempts != len(paths) {
				t.Fatalf("trace attempts=%d sends=%v", trace.Attempts, paths)
			}
			if paths[len(paths)-1] == "/c/v1/messages" && (!trace.Successful || rec.Code != 200) {
				t.Fatalf("success missing: %+v response=%d", trace, rec.Code)
			}
		})
	}
}

func TestClaudeProfileStartedStreamCannotReplayOrReportSuccess(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n")
	}))
	defer server.Close()
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 3, MaxTotalAttempts: 10, MaxTotalDuration: time.Second}}
	for i, name := range []string{"a", "b"} {
		cfg.Endpoints = append(cfg.Endpoints, config.EndpointConfig{Name: name, Channel: "A", CredentialGroupID: int64(11 + i), URL: server.URL + "/" + name, Priority: i, SkipHealthCheck: true})
	}
	var trace routing.DispatchTrace
	h, _ := profileClaudeHandler(t, cfg, 10, func(value routing.DispatchTrace) { trace = value })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`)))
	if len(paths) != 1 || !trace.ResponseStarted || trace.Successful {
		t.Fatalf("stream replayed or incorrectly succeeded: sends=%v trace=%+v", paths, trace)
	}
}

func TestClaudeProfileMessageStopWithoutCompleteUsageIsNotSuccess(t *testing.T) {
	for _, start := range []string{"", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n"} {
		sends := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sends++
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, start+"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		}))
		cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 10, MaxTotalDuration: time.Second}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
		var trace routing.DispatchTrace
		h, _ := profileClaudeHandler(t, cfg, 10, func(value routing.DispatchTrace) { trace = value })
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`)))
		server.Close()
		if sends != 1 || !trace.ResponseStarted || trace.Successful {
			t.Fatalf("incomplete stream recorded as success: sends=%d trace=%+v", sends, trace)
		}
	}
}

func TestClaudeProfileConfigurationChangeSkipsStaleCandidate(t *testing.T) {
	var manager *endpoint.Manager
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Updating an endpoint also schedules a diagnostic GET. Count only
		// business requests when checking stale candidate admission.
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		paths = append(paths, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/a/") {
			for _, ep := range manager.GetAllEndpoints() {
				if ep.Config.Name == "b" {
					cfg := ep.Config
					cfg.ApiKey = "rebound-credential"
					if err := manager.UpdateEndpointConfig(endpoint.EndpointKeyForConfig(ep.Config), cfg); err != nil {
						t.Error(err)
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":{"type":"api_error"}}`)
	}))
	defer server.Close()
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 10, MaxTotalDuration: time.Second}, Endpoints: []config.EndpointConfig{
		{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL + "/a", Priority: 0, SkipHealthCheck: true},
		{Name: "b", Channel: "B", CredentialGroupID: 22, URL: server.URL + "/b", Priority: 1, SkipHealthCheck: true},
	}}
	var trace routing.DispatchTrace
	var h *Handler
	h, manager = profileClaudeHandler(t, cfg, 10, func(value routing.DispatchTrace) { trace = value })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","stream":false,"messages":[],"max_tokens":16}`)))
	if !reflect.DeepEqual(paths, []string{"/a/v1/messages"}) || trace.Attempts != 1 {
		t.Fatalf("stale candidate was sent: paths=%v trace=%+v", paths, trace)
	}
	found := false
	for _, event := range trace.Candidates {
		found = found || event.Reason == "candidate_configuration_changed"
	}
	if !found {
		t.Fatalf("configuration skip missing from trace: %+v", trace)
	}
}

func TestClaudeProfileSelectedCredentialsNeedNoPoolKey(t *testing.T) {
	for _, managed := range []bool{true, false} {
		t.Run(map[bool]string{true: "managed", false: "passthrough"}[managed], func(t *testing.T) {
			binding := subscription.Binding{Channel: "A", GroupID: 11, Version: 1, Enabled: true}
			wantAuth := "Bearer client-oauth"
			if managed {
				binding.AccountID = "account"
				wantAuth = "Bearer managed-oauth"
			}
			store := &poolSubscriptionStore{subscriptionIntegrationStore: &subscriptionIntegrationStore{}, bindings: []subscription.Binding{binding}}
			controller := subscription.NewController(store)
			controller.SetAccountResolver(func(context.Context, string) (string, string, error) { return "managed-oauth", "", nil }, func(context.Context, string) error { return nil })
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 10, MaxTotalDuration: time.Second}, Endpoints: []config.EndpointConfig{{Name: "official", Channel: "A", CredentialGroupID: 11, URL: subscription.OfficialOrigin, AuthMode: subscription.ClientPassthrough, SkipHealthCheck: true, Timeout: time.Second}}}
			h, _ := profileClaudeHandler(t, cfg, 10, nil)
			h.SetSubscriptionController(controller)
			client, err := h.forwarder.NewClient(time.Second)
			if err != nil {
				t.Fatal(err)
			}
			sends := 0
			client.Transport.(*http.Transport).RegisterProtocol("https", subscriptionIntegrationTransport(func(r *http.Request) (*http.Response, error) {
				sends++
				if r.Header.Get("Authorization") != wantAuth {
					t.Errorf("wrong auth %q", r.Header.Get("Authorization"))
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"message","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))}, nil
			}))
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","stream":false,"messages":[],"max_tokens":16}`))
			r.Header.Set("Authorization", "Bearer client-oauth")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if sends != 1 || rec.Code != 200 {
				t.Fatalf("send=%d response=%d %s", sends, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestClaudeProfileStreamingCompletionAndLearningContainOnlyUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.25")
		w.Header().Set("Set-Cookie", "private-cookie")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"test\",\"usage\":{\"input_tokens\":2,\"cache_read_input_tokens\":4}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"private-generated-text\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 10, MaxTotalDuration: time.Second}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
	var trace routing.DispatchTrace
	h, _ := profileClaudeHandler(t, cfg, 10, func(value routing.DispatchTrace) { trace = value })
	observations := 0
	h.SetProfileResponseObserver(func(candidate routing.DispatchCandidate, status int, headers http.Header, body []byte) {
		observations++
		if candidate.GroupID != 11 || status != 200 || headers.Get("Anthropic-Ratelimit-Unified-5h-Utilization") != "0.25" || headers.Get("Set-Cookie") != "" || strings.Contains(string(body), "private-generated-text") {
			t.Error("invalid response learning boundary")
		}
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`)))
	if !trace.Successful || !trace.ResponseStarted || trace.Attempts != 1 || observations != 3 {
		t.Fatalf("completion/learning failed: %+v observations=%d response=%s", trace, observations, rec.Body.String())
	}
}

func TestClaudeProfileResponseLearningExcludesErrorTextAndUnknownUsage(t *testing.T) {
	const raw = `{"type":"error","error":{"type":"quota_exceeded","message":"private-error-message","details":{"secret":"private-secret"}},"usage":{"input_tokens":2,"output_tokens":3,"private":"private-usage"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Authorization", "Bearer private-response-auth")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, raw)
	}))
	defer server.Close()
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 1, MaxTotalDuration: time.Second}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
	h, _ := profileClaudeHandler(t, cfg, 1, nil)
	sawError, sawUsage := false, false
	h.SetProfileResponseObserver(func(_ routing.DispatchCandidate, status int, headers http.Header, body []byte) {
		if status != http.StatusTooManyRequests || headers.Get("Retry-After") != "30" || headers.Get("Authorization") != "" || strings.Contains(string(body), "private") {
			t.Errorf("response learning leaked private fields: headers=%v body=%s", headers, body)
		}
		sawError = sawError || strings.Contains(string(body), `"type":"quota_exceeded"`)
		sawUsage = sawUsage || strings.Contains(string(body), `"input_tokens":2`)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","stream":false,"messages":[],"max_tokens":16}`)))
	if !sawError || !sawUsage || rec.Code != http.StatusTooManyRequests || rec.Body.String() != raw {
		t.Fatalf("learning or client response changed: error=%v usage=%v response=%d %s", sawError, sawUsage, rec.Code, rec.Body.String())
	}
}

func TestClaudeProfileWaitForRecoverySharesBudgetAndDoesNotSendEarly(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(map[bool]string{false: "reset", true: "budget"}[exhausted], func(t *testing.T) {
			reset := time.Now().Add(30 * time.Millisecond)
			sends := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends++
				if time.Now().Before(reset) {
					t.Error("quota request sent before reset")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"type":"message","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer server.Close()
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 2, MaxTotalAttempts: 10, MaxTotalDuration: time.Second}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
			h, manager := profileClaudeHandler(t, cfg, 10, nil)
			runtime := manager.PoolRuntime().ProfileRuntime()
			profile, _ := runtime.Profile(1)
			profile.WaitForRecovery = true
			if exhausted {
				profile.MaxDuration = 5 * time.Millisecond
			}
			candidates := runtime.ExpandTargets(profile, []routing.RouteTarget{{ProfileID: 1}}, "test")
			full := 100.0
			for i := range candidates {
				candidates[i].Quota.Windows = []routing.QuotaSnapshot{{Provider: "claude", ModelScope: "*", WindowName: "5h", UsedPercent: &full, ResetAt: &reset, ExpiresAt: reset.Add(time.Hour)}}
			}
			var trace routing.DispatchTrace
			runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile}, Candidates: candidates, OnTrace: func(value routing.DispatchTrace) { trace = value }})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","stream":false,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`)))
			if exhausted {
				if sends != 0 || trace.StopReason != routing.StopRecoveryOutsideBudget {
					t.Fatalf("budget not shared: sends=%d trace=%+v", sends, trace)
				}
			} else if sends != 1 || !trace.Successful || trace.WaitMS <= 0 {
				t.Fatalf("recovery missing: sends=%d trace=%+v response=%s", sends, trace, rec.Body.String())
			}
		})
	}
}

func TestClaudeUnifiedRuntimeWithoutDefaultNeverSendsToActiveChannel(t *testing.T) {
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends++ }))
	defer server.Close()
	cfg := &config.Config{Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL}}, Retry: config.RetryConfig{MaxAttempts: 1}}
	h, manager := profileClaudeHandler(t, cfg, 1, nil)
	manager.PoolRuntime().ProfileRuntime().Replace(routing.RuntimeConfig{Provider: "claude"})
	for _, streaming := range []bool{false, true} {
		body := `{"model":"test","max_tokens":16,"messages":[],"stream":false}`
		if streaming {
			body = strings.Replace(body, "false", "true", 1)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
		if rec.Code < 400 && !strings.Contains(rec.Body.String(), "routing_profile_missing") {
			t.Fatalf("missing default was not reported: %d %s", rec.Code, rec.Body.String())
		}
	}
	if sends != 0 || manager.ActiveChannelName() != "A" {
		t.Fatalf("missing default fell back to active channel: sends=%d", sends)
	}
}

func TestClaudeLocalFailureCooldownRecordsOnlyFirstIdenticalRequest(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			sends := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends++
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"test\",\"usage\":{\"input_tokens\":10}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"type":"message","model":"test","content":[],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`)
				}
			}))
			t.Cleanup(server.Close)
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1, DisableRecoveryWait: true}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
			h, manager := profileClaudeHandler(t, cfg, 1, nil)
			tracker := newLifecycleTestTracker(t)
			h.SetUsageTracker(tracker)
			h.monitoringMiddleware = middleware.NewMonitoringMiddleware(manager)
			runtime := manager.PoolRuntime().ProfileRuntime()
			runtime.Replace(routing.RuntimeConfig{Provider: "claude", LoadError: errors.New("SQL logic error: no such column: intent_id (1)")})
			body := fmt.Sprintf(`{"model":"test","stream":%t,"messages":[{"role":"user","content":"first"}],"max_tokens":16}`, streaming)
			sequence := 0
			call := func(body string) *httptest.ResponseRecorder {
				sequence++
				req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
				req = req.WithContext(context.WithValue(req.Context(), "conn_id", fmt.Sprintf("local-failure-%d", sequence)))
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return rec
			}
			for i := 0; i < 28; i++ {
				rec := call(body)
				if rec.Code != 400 || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), "no such column: intent_id") {
					t.Fatalf("missing local rejection: %d %s", rec.Code, rec.Body.String())
				}
				if i > 0 && !strings.Contains(rec.Body.String(), "首次记录") {
					t.Fatal("duplicate entered normal processing")
				}
			}
			rows, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 100})
			if err != nil || len(rows) != 1 || sends != 0 {
				t.Fatalf("retries recorded or sent: rows=%d sends=%d err=%v", len(rows), sends, err)
			}
			if rows[0].FailureReason != "routing_config_unavailable" || !strings.Contains(rows[0].LastFailureReason, "no such column: intent_id") || rows[0].HTTPStatusIsUpstream {
				t.Fatalf("cause lost: %+v", rows[0])
			}
			call(strings.Replace(body, "first", "different", 1))
			rows, _, err = tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 100})
			if err != nil || len(rows) != 2 {
				t.Fatalf("different request suppressed: rows=%d err=%v", len(rows), err)
			}
			profile := routing.DefaultProfile("claude")
			profile.ID, profile.IsDefault = 7, true
			profile.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}}
			ep := manager.GetAllEndpoints()[0]
			runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile}, Candidates: []routing.DispatchCandidate{{Provider: "claude", ChannelID: 1, GroupID: 11, EndpointID: endpoint.EndpointKeyForConfig(ep.Config), Protocol: "messages", Enabled: true}}})
			if rec := call(body); rec.Code != 200 || sends != 1 {
				t.Fatalf("repaired request blocked: %d %s sends=%d", rec.Code, rec.Body.String(), sends)
			}
			if err := tracker.ForceFlush(); err != nil {
				t.Fatal(err)
			}
			rows, err = tracker.QueryRequestDetails(context.Background(), &tracking.QueryOptions{Limit: 100})
			if err != nil || len(rows) != 3 {
				t.Fatalf("archive differs from visible records: rows=%d err=%v", len(rows), err)
			}
		})
	}
}

func TestClaudeUnsentHealthCooldownStopsIdenticalRetriesUntilRecovery(t *testing.T) {
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"type":"message","model":"test","content":[],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`)
	}))
	t.Cleanup(server.Close)
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1, DisableRecoveryWait: true}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
	h, manager := profileClaudeHandler(t, cfg, 1, nil)
	tracker := newLifecycleTestTracker(t)
	h.SetUsageTracker(tracker)
	h.monitoringMiddleware = middleware.NewMonitoringMiddleware(manager)
	runtime := manager.PoolRuntime().ProfileRuntime()
	profile, _ := runtime.Profile(1)
	profile.Members = profile.Members[:1]
	ep := manager.GetAllEndpoints()[0]
	blocked := true
	until := time.Now().Add(time.Minute)
	runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile}, Candidates: []routing.DispatchCandidate{{Provider: "claude", ChannelID: 1, GroupID: 11, EndpointID: endpoint.EndpointKeyForConfig(ep.Config), Protocol: "messages", Enabled: true}}, PrepareCandidate: func(c routing.DispatchCandidate, model, override string) routing.DispatchCandidate {
		c = routing.CopyCandidateForModel(c, model, override)
		c.Health = routing.HealthState{CoolingDown: blocked, Until: until}
		return c
	}})
	seq := 0
	call := func() *httptest.ResponseRecorder {
		seq++
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test","max_tokens":16,"messages":[]}`))
		req = req.WithContext(context.WithValue(req.Context(), "conn_id", fmt.Sprintf("cooldown-%d", seq)))
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 3; i++ {
		if rec := call(); rec.Code != 400 || !strings.Contains(rec.Body.String(), "route_unavailable") {
			t.Fatalf("cooldown response: %d %s", rec.Code, rec.Body.String())
		}
	}
	rows, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 10})
	if err != nil || len(rows) != 1 || sends != 0 {
		t.Fatalf("cooldown retries escaped: rows=%d sends=%d err=%v", len(rows), sends, err)
	}
	blocked = false
	if rec := call(); rec.Code != 200 || sends != 1 {
		t.Fatalf("recovery stayed blocked: %d %s", rec.Code, rec.Body.String())
	}
}

func TestClaudeCountTokensLocalFailureGatePreservesDisabledEstimation(t *testing.T) {
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1, DisableRecoveryWait: true}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: "http://127.0.0.1:1", SkipHealthCheck: true}}}
	cfg.TokenCounting.Enabled = true
	h, manager := profileClaudeHandler(t, cfg, 1, nil)
	tracker := newLifecycleTestTracker(t)
	h.SetUsageTracker(tracker)
	h.monitoringMiddleware = middleware.NewMonitoringMiddleware(manager)
	manager.PoolRuntime().ProfileRuntime().Replace(routing.RuntimeConfig{Provider: "claude", LoadError: errors.New("no such column: intent_id")})
	call := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(`{"model":"test","messages":[]}`))
		req = req.WithContext(context.WithValue(req.Context(), "conn_id", id))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, id := range []string{"count-first", "count-duplicate"} {
		if rec := call(id); rec.Code != 400 || !strings.Contains(rec.Body.String(), "intent_id") {
			t.Fatalf("missing auxiliary rejection: %d %s", rec.Code, rec.Body.String())
		}
	}
	rows, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("auxiliary duplicate was recorded: rows=%d err=%v", len(rows), err)
	}
	cfg.TokenCounting.Enabled = false
	if rec := call("count-estimated"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "input_tokens") {
		t.Fatalf("disabled counting must estimate: %d %s", rec.Code, rec.Body.String())
	}
}
