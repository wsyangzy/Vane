package codexproxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/search"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

const nativeSearchBody = `{ "model" : "search-original", "commands":{"search_query":[{"q":"query","domains":["example.org"]}],"open":[{"ref_id":"turn0search0"}]},"settings":{"external_web_access":false},"encrypted_context":"opaque-context" }`
const nativeSearchResponse = `{ "encrypted_output":"opaque-result", "output":"Native search result", "results":[] }`

func attachSearchTracker(t *testing.T, h *Handler) func() *tracking.RequestDetail {
	t.Helper()
	tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tracker.Close() })
	h.usageTracker = tracker
	return func() *tracking.RequestDetail {
		rows, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 5})
		if err != nil || len(rows) != 1 {
			t.Fatalf("tracking rows=%d err=%v", len(rows), err)
		}
		return &rows[0]
	}
}

func TestSearchNativeSkipsTextRulesAndPreservesProtocolOnFailover(t *testing.T) {
	sends := 0
	h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends++
		got, _ := io.ReadAll(r.Body)
		if string(got) != nativeSearchBody || !strings.HasSuffix(r.URL.Path, "/v1/alpha/search") || r.Header.Get("X-Client-Identity") != "search-client" {
			t.Errorf("native request changed: path=%s body=%s", r.URL.Path, got)
		}
		if strings.HasPrefix(r.URL.Path, "/first") {
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"message":"search path unavailable"}}`)
			return
		}
		io.WriteString(w, nativeSearchResponse)
	})
	h.manager.channelsByID[1].Model, h.manager.channelsByID[2].Model = "wrong-model-1", "wrong-model-2"
	p := routing.DefaultProfile("codex")
	p.ID, p.IsDefault = 1, true
	p.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}, {ChannelID: 2, GroupID: 22, Enabled: true}}
	p.Rules = []routing.RoutingRule{{ID: 1, Enabled: true, Kind: "non_stream", Mode: "strict", Targets: []routing.RouteTarget{{ChannelID: 1, UpstreamModel: "wrong-model"}}}}
	p.Classifier = &routing.ClassifierConfig{Enabled: true}
	runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{p}, Candidates: []routing.DispatchCandidate{{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true}, {Provider: "codex", ChannelID: 2, GroupID: 22, EndpointID: "2", Protocol: "responses", Enabled: true}}, PrepareCandidate: h.manager.PrepareDispatchCandidate, Supports: h.manager.SupportsDispatchCandidate, Classify: func(context.Context, routing.RoutingProfile, routing.TurnInput) (routing.IntentResult, error) {
		t.Error("search invoked text classifier")
		return routing.IntentResult{}, nil
	}})
	row := attachSearchTracker(t, h)
	req := httptest.NewRequest("POST", "/v1/alpha/search/native", strings.NewReader(nativeSearchBody))
	req.Header.Set("X-Client-Identity", "search-client")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || sends != 2 || rec.Body.String() != nativeSearchResponse {
		t.Fatalf("native failover: code=%d sends=%d body=%s", rec.Code, sends, rec.Body.String())
	}
	got := row()
	var trace routing.DispatchTrace
	json.Unmarshal([]byte(got.RouteSnapshot), &trace)
	if got.Provider != "codex" || got.ModelName != "search-original" || got.ModelRewriteSource != "" || got.Status != "completed" || trace.Decision.Kind != "default" || !trace.Successful {
		t.Fatalf("search tracked as text: %+v trace=%+v", got, trace)
	}
	for _, event := range trace.Candidates {
		if event.Candidate.Protocol != "search" || event.Candidate.Model != "search-original" {
			t.Errorf("wrong native search identity: %+v", event)
		}
	}
	if allowed, reason := h.manager.PoolRuntime().HealthAllowed(1, 11, "1", "text-model"); !allowed {
		t.Fatalf("search 404 cooled text endpoint: %s", reason)
	}
}

func TestSearchOfficialSubscriptionPreservesIdentityAndOpaqueResponse(t *testing.T) {
	for _, path := range []string{"/v1/alpha/search", "/alpha/search/native", "/backend-api/codex/alpha/search/native"} {
		t.Run(path, func(t *testing.T) {
			calls := 0
			h, _, request := codexSubscriptionFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				got, _ := io.ReadAll(r.Body)
				if r.URL.Host != "chatgpt.com" || r.URL.Path != "/backend-api/codex/alpha/search" || string(got) != nativeSearchBody || r.Header.Get("Authorization") != "Bearer official-oauth-sentinel" || r.Header.Get("ChatGPT-Account-ID") != "workspace-sentinel" || r.Header.Get("X-Relay-Key") != "" {
					t.Errorf("official search boundary failed: %s %s", r.URL, got)
				}
				return codexSubscriptionResponseFor(200, nativeSearchResponse), nil
			})
			h.searchConfig = func(context.Context) (search.Config, error) {
				cfg := search.DefaultConfig()
				if strings.HasSuffix(path, "/native") {
					cfg.Mode = "service"
				}
				return cfg, nil
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, request(path, nativeSearchBody))
			if rec.Code != 200 || calls != 1 || rec.Body.String() != nativeSearchResponse {
				t.Fatalf("official search: status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
			}
		})
	}
}

func TestSearchServiceUsesOwnCredentialsAndTracksActualFailure(t *testing.T) {
	for _, status := range []int{200, 401, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if r.Header.Get("Authorization") != "Bearer search-only-key" || r.Header.Get("Cookie") != "" || r.Header.Get("ChatGPT-Account-ID") != "" || r.Header.Get("X-Relay-Key") != "" || strings.Contains(string(body), "opaque-context") || strings.Contains(string(body), "search-original") {
					t.Errorf("client state leaked into search adapter: headers=%v body=%s", r.Header, body)
				}
				w.WriteHeader(status)
				if status == 200 {
					io.WriteString(w, `{"results":[{"title":"Result","url":"https://example.org/","content":"snippet"}]}`)
				} else {
					io.WriteString(w, `{"detail":"search quota exhausted"}`)
				}
			}))
			defer server.Close()
			h, _, _ := newProfileCodexFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("adapter reached model endpoint") })
			h.searchConfig = func(context.Context) (search.Config, error) {
				cfg := search.DefaultConfig()
				cfg.Endpoint, cfg.APIKey = server.URL, "search-only-key"
				return cfg, nil
			}
			row := attachSearchTracker(t, h)
			req := httptest.NewRequest("POST", "/v1/alpha/search/service", strings.NewReader(strings.Replace(nativeSearchBody, `"external_web_access":false`, `"external_web_access":true`, 1)))
			req.Header.Set("Authorization", "Bearer client-secret")
			req.Header.Set("Cookie", "session=client-secret")
			req.Header.Set("ChatGPT-Account-ID", "account-secret")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			var reply search.Reply
			err := json.Unmarshal(rec.Body.Bytes(), &reply)
			if rec.Code != 200 || calls != 1 || err != nil || reply.EncryptedOutput != nil || reply.Output == "" {
				t.Fatalf("invalid tool envelope: code=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
			}
			got := row()
			wantStatus := "completed"
			if status != 200 {
				wantStatus = "failed"
				if got.LastFailureReason != `{"detail":"search quota exhausted"}` {
					t.Errorf("upstream error replaced: %q", got.LastFailureReason)
				}
			}
			if got.Status != wantStatus || got.HTTPStatusCode == nil || *got.HTTPStatusCode != status || got.Provider != "codex" || got.ModelName != "search-original" {
				t.Fatalf("actual service result was not tracked: %+v", got)
			}
		})
	}
}

func TestSearchServiceLocalFailuresHaveNoUpstreamStatus(t *testing.T) {
	for _, cached := range []bool{false, true} {
		h, _, _ := newProfileCodexFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("local failure reached network") })
		h.searchConfig = func(context.Context) (search.Config, error) {
			cfg := search.DefaultConfig()
			cfg.Mode = "service"
			return cfg, nil
		}
		row := attachSearchTracker(t, h)
		body := strings.Replace(nativeSearchBody, `"external_web_access":false`, `"external_web_access":true`, 1)
		reason := "search_not_configured"
		if cached {
			body, reason = nativeSearchBody, "search_cached_unsupported"
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(body)))
		got := row()
		if rec.Code != 200 || got.Status != "failed" || got.HTTPStatusCode != nil || got.FailureReason != reason || got.Channel != "" {
			t.Fatalf("local failure presented as upstream: %+v response=%s", got, rec.Body.String())
		}
	}
}

func TestSearchNativeProtocolRequiresToolOutput(t *testing.T) {
	for _, body := range []string{nativeSearchResponse, `{"output":"","encrypted_output":null}`} {
		if !codexProfileProtocolSuccess("/alpha/search", []byte(body)) {
			t.Errorf("valid search reply rejected: %s", body)
		}
	}
	for _, body := range []string{`{"output":[]}`, `{"models":[]}`, `{"output":"ok","error":{"message":"denied"}}`} {
		if codexProfileProtocolSuccess("/alpha/search", []byte(body)) {
			t.Errorf("text response accepted as search success: %s", body)
		}
	}
}

func TestSearchLegacyNativeRejectsWrongProtocolWithHTTP200(t *testing.T) {
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"models":[]}`)
	})
	h.manager.poolRuntime.Store(nil)
	ep := h.manager.endpointsByChannel[1][0]
	ep.FailoverEnabled = true
	h.manager.endpoints = []*CodexEndpoint{ep}
	h.failoverCfg.Enabled = false
	row := attachSearchTracker(t, h)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search/native", strings.NewReader(nativeSearchBody)))
	got := row()
	if rec.Code != http.StatusBadGateway || got.Status != "failed" || got.HTTPStatusCode == nil || *got.HTTPStatusCode != 200 || got.FailureReason != "response_read_error" {
		t.Fatalf("wrong native protocol counted as success: response=%d row=%+v", rec.Code, got)
	}
}

func TestSearchManagedSubscriptionUsesOfficialAccount(t *testing.T) {
	h, _, trace := newProfileCodexFixture(t, func(http.ResponseWriter, *http.Request) { t.Fatal("managed search reached third party") })
	s := &poolSubscriptionStore{codexSubscriptionStore: &codexSubscriptionStore{}, bindings: []subscription.Binding{{ChannelID: 1, GroupID: 11, Version: 1, Enabled: true, AccountID: "managed"}}}
	h.subscriptions = subscription.NewController(s)
	h.subscriptions.SetAccountResolver(func(context.Context, string) (string, string, error) {
		return "managed-search-token", "managed-workspace", nil
	}, func(context.Context, string) error { return nil })
	ep := h.manager.endpointsByChannel[1][0]
	ep.AuthMode, ep.BaseURL, ep.APIKey = subscription.ClientPassthrough, subscription.OfficialBaseURL, ""
	calls := 0
	h.forwarder.client.Transport = codexSubscriptionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "chatgpt.com" || r.URL.Path != "/backend-api/codex/alpha/search" || r.Header.Get("Authorization") != "Bearer managed-search-token" || r.Header.Get("ChatGPT-Account-ID") != "managed-workspace" {
			t.Error("managed account search identity changed")
		}
		return codexSubscriptionResponseFor(200, nativeSearchResponse), nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(nativeSearchBody)))
	if rec.Code != 200 || calls != 1 || !trace.Successful || rec.Body.String() != nativeSearchResponse {
		t.Fatalf("managed search failed: %d %s trace=%+v", rec.Code, rec.Body.String(), trace)
	}
}

func TestSearchNativeErrorPreservesActualResponse(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		raw := `{"error":{"message":"Invalid URL (POST /v1/alpha/search)","type":"invalid_request_error"}}`
		h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if string(body) != nativeSearchBody {
				t.Error("native search body rewritten")
			}
			w.WriteHeader(404)
			io.WriteString(w, raw)
		})
		if legacy {
			h.manager.poolRuntime.Store(nil)
			ep := h.manager.endpointsByChannel[1][0]
			ep.FailoverEnabled = true
			h.manager.endpoints = []*CodexEndpoint{ep}
			h.failoverCfg.Enabled = false
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(nativeSearchBody)))
		if rec.Code != 404 || rec.Body.String() != raw {
			t.Fatalf("search error replaced: legacy=%t code=%d body=%s", legacy, rec.Code, rec.Body.String())
		}
	}
}

func TestSearchServiceSettingsChangePerRequestAndDoNotFollowRedirects(t *testing.T) {
	redirectCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectCalls++ }))
	defer destination.Close()
	serviceCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serviceCalls++
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(307)
	}))
	defer server.Close()
	nativeCalls := 0
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) { nativeCalls++; io.WriteString(w, nativeSearchResponse) })
	cfg := search.DefaultConfig()
	cfg.Endpoint, cfg.APIKey = server.URL, "search-secret"
	h.searchConfig = func(context.Context) (search.Config, error) { return cfg, nil }
	body := strings.Replace(nativeSearchBody, `"external_web_access":false`, `"external_web_access":true`, 1)
	for _, mode := range []string{"native", "service"} {
		cfg.Mode = mode
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/alpha/search", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("mode=%s status=%d", mode, rec.Code)
		}
		if mode == "service" && !strings.Contains(rec.Body.String(), "Search service failed") {
			t.Error("redirect was treated as success")
		}
	}
	if nativeCalls != 1 || serviceCalls != 1 || redirectCalls != 0 {
		t.Fatalf("search switch or credential redirect boundary failed: native=%d service=%d redirects=%d", nativeCalls, serviceCalls, redirectCalls)
	}
}
