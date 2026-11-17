package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/providers/codex/store"
)

func TestEndpointPathProbeOnlyReadsModelsAndDoesNotPersist(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer draft-test-key" {
			t.Error("probe method or authentication")
		}
		if r.URL.Path == "/models" {
			io.WriteString(w, "<html>challenge</html>")
			return
		}
		io.WriteString(w, `{"data":[{"id":"test-model"}]}`)
	}))
	defer server.Close()
	svc := NewCodexEndpointService(nil, nil)
	result, err := svc.ProbeEndpoint(context.Background(), EndpointProbeInput{BaseURL: server.URL, APIKey: "draft-test-key"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || strings.Join(paths, ",") != "/models,/v1/models" || result.RecommendedStrategy != "v1" || result.ResponseVerified || result.Candidates[0].Verified || !result.Candidates[1].Verified {
		t.Fatalf("paths=%v result=%+v", paths, result)
	}
}

func TestEndpointResponseVerificationUsesOneSelectedPathAndValidatesProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"valid-wrong-mime", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n", true},
		{"valid-shutdown-error", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n", true},
		{"html", "<html>challenge</html>", false},
		{"json", `{"status":"completed","output":[]}`, false},
		{"empty", "", false},
		{"heartbeat", ": keepalive\n\n", false},
		{"incomplete", "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/v1/responses" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var body struct {
					Model, Input string
					Stream       bool
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "test-model" || !body.Stream || body.Input == "" {
					t.Error("invalid diagnostic payload")
				}
				w.Header().Set("Content-Type", "text/plain")
				if tc.name == "valid-shutdown-error" {
					w.Header().Set("Content-Length", strconv.Itoa(len(tc.body)+100))
				}
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			result, err := NewCodexEndpointService(nil, nil).ProbeEndpoint(context.Background(), EndpointProbeInput{BaseURL: server.URL, APIKey: "test", Model: "test-model", PathStrategy: "v1", VerifyResponse: true}, server.Client())
			if err != nil || calls != 1 || len(result.Candidates) != 1 || result.ResponseVerified != tc.ok {
				t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
			}
		})
	}
}

func TestEndpointProbeDoesNotFollowRedirectOrConfirmAuthenticationFailures(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(status)
				io.WriteString(w, `{"data":[]}`)
			}))
			defer server.Close()
			result, err := NewCodexEndpointService(nil, nil).ProbeEndpoint(context.Background(), EndpointProbeInput{BaseURL: server.URL, APIKey: "test"}, server.Client())
			if err != nil || len(paths) != 2 || result.RecommendedStrategy != "" || result.Candidates[0].Verified || result.Candidates[1].Verified {
				t.Fatalf("paths=%v result=%+v err=%v", paths, result, err)
			}
		})
	}
}

func TestEndpointProbeResolvesSavedCredentialWithoutChangingEndpoint(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channels := store.NewSQLiteCodexChannelStore(db)
	groups := store.NewSQLiteCodexCredentialGroupStore(db)
	endpoints := store.NewSQLiteCodexEndpointStore(db)
	channel, err := channels.Create(ctx, &store.CodexChannelRecord{Name: "probe-test", ApiKey: "channel-key", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := groups.Create(ctx, &store.CodexCredentialGroupRecord{ChannelID: channel.ID, Name: "default", ApiKey: "group-key", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer endpoint-key" {
			t.Error("endpoint override not used")
		}
		io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	svc := NewCodexEndpointService(endpoints, channels, groups)
	ep, err := svc.CreateEndpoint(ctx, &store.CodexEndpointRecord{ChannelID: channel.ID, CredentialGroupID: group.ID, Name: "endpoint", BaseURL: server.URL, ApiKey: "endpoint-key", PathStrategy: "legacy", FailoverEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ProbeEndpoint(ctx, EndpointProbeInput{ChannelID: channel.ID, CredentialGroupID: group.ID, EndpointID: ep.ID, BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ProbeEndpoint(ctx, EndpointProbeInput{ChannelID: channel.ID, EndpointID: ep.ID, BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := endpoints.GetByID(ctx, ep.ID)
	if err != nil || stored.PathStrategy != "legacy" || stored.BaseURL != ep.BaseURL || stored.IsActive != ep.IsActive {
		t.Fatalf("detection persisted a change: %+v %v", stored, err)
	}
}

func TestEndpointProbeUsesDefaultGroupBeforeChannelCredential(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channels := store.NewSQLiteCodexChannelStore(db)
	groups := store.NewSQLiteCodexCredentialGroupStore(db)
	channel, err := channels.Create(ctx, &store.CodexChannelRecord{Name: "default-probe", ApiKey: "channel-key"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = groups.Create(ctx, &store.CodexCredentialGroupRecord{ChannelID: channel.ID, Name: "default", ApiKey: "group-key", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer group-key" {
			t.Error("default group credential not used")
		}
		io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	_, err = NewCodexEndpointService(nil, channels, groups).ProbeEndpoint(ctx, EndpointProbeInput{ChannelID: channel.ID, BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
}

func TestEndpointPathPolicyPersistsAndSurvivesUnrelatedUpdatesAndRestore(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channels := store.NewSQLiteCodexChannelStore(db)
	endpoints := store.NewSQLiteCodexEndpointStore(db)
	channel, err := channels.Create(ctx, &store.CodexChannelRecord{Name: "path-roundtrip", ApiKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewCodexEndpointService(endpoints, channels)
	ep, err := svc.CreateEndpoint(ctx, &store.CodexEndpointRecord{ChannelID: channel.ID, Name: "roundtrip", BaseURL: "https://root.invalid", PathStrategy: "base", FailoverEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	update := func(strategies ...string) error {
		return svc.UpdateEndpointByID(ctx, ep.ID, ep.Name, ep.BaseURL, ep.CredentialGroupID, ep.ApiKey, ep.FailoverEnabled, true, ep.Priority, ep.TimeoutSeconds, ep.CostMultiplier, ep.InputCostMultiplier, ep.OutputCostMultiplier, ep.CacheReadCostMultiplier, ep.BillingOutputTokenSource, ep.ModelMultipliers, strategies...)
	}
	assertStrategy := func(want string) {
		t.Helper()
		record, err := endpoints.GetByID(ctx, ep.ID)
		if err != nil || record == nil || record.PathStrategy != want {
			t.Fatalf("want policy %q: record=%+v err=%v", want, record, err)
		}
	}
	assertStrategy("base")
	if err := update(); err != nil {
		t.Fatal(err)
	}
	assertStrategy("base")
	if err := update("v1"); err != nil {
		t.Fatal(err)
	}
	assertStrategy("v1")
	if err := update("unknown"); err == nil {
		t.Fatal("invalid policy accepted")
	}
	assertStrategy("v1")
	if err := svc.RestoreEndpoint(ctx, ep); err != nil {
		t.Fatal(err)
	}
	assertStrategy("base")
	if err := endpoints.DeleteByID(ctx, ep.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RestoreDeletedEndpoint(ctx, ep); err != nil {
		t.Fatal(err)
	}
	assertStrategy("base")
}
