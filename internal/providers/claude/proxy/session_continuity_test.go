package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestClaudeSessionContinuesAfterProfileAndResourceChanges(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, change := range []string{"profile", "reload", "remove-original-group", "model"} {
			t.Run(fmt.Sprintf("%s/stream=%t", change, stream), func(t *testing.T) {
				var received []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					received = append(received, string(body))
					if r.Header.Get("X-Session-Id") != "same-session" || r.Header.Get("User-Agent") != "native-claude" {
						t.Error("client identity/session headers changed")
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"signed-history\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						io.WriteString(w, `{"type":"message","content":[{"type":"thinking","thinking":"reason","signature":"signed-history"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
					}
				}))
				defer server.Close()
				cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}, {Name: "b", Channel: "B", CredentialGroupID: 22, URL: server.URL, SkipHealthCheck: true}}}
				h, manager := profileClaudeHandler(t, cfg, 3, nil)
				send := func(body string) {
					r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
					r.Header.Set("X-Session-Id", "same-session")
					r.Header.Set("User-Agent", "native-claude")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != 200 || strings.Contains(w.Body.String(), "state_backend_incompatible") {
						t.Fatalf("continuation failed: %d %s", w.Code, w.Body.String())
					}
				}
				send(fmt.Sprintf(`{"model":"original","stream":%t,"messages":[{"role":"user","content":"first"}]}`, stream))
				runtime := manager.PoolRuntime().ProfileRuntime()
				profile, _ := runtime.Profile(1)
				switch change {
				case "profile":
					profile.Name, profile.Strategy = "edited", routing.StrategyRotate
				case "reload":
					configs := []config.EndpointConfig{}
					for _, ep := range manager.GetAllEndpoints() {
						copy := ep.Config
						copy.Priority, copy.Timeout = 99, time.Minute
						configs = append(configs, copy)
					}
					manager.SyncEndpoints(configs)
				case "remove-original-group":
					profile.Members = profile.Members[1:]
				case "model":
					manager.UpdateChannelModelMappings(map[string]map[string]string{"A": {"original": "mapped"}})
				}
				cs := []routing.DispatchCandidate{}
				for _, ep := range manager.GetAllEndpoints() {
					channelID := int64(1)
					if ep.Config.Channel == "B" {
						channelID = 2
					}
					cs = append(cs, routing.DispatchCandidate{Provider: "claude", ChannelID: channelID, GroupID: ep.Config.CredentialGroupID, EndpointID: endpoint.EndpointKeyForConfig(ep.Config), Protocol: "messages", Enabled: true, StateVersion: ep.RouteStateVersion()})
				}
				runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile}, Candidates: cs})
				for i := 0; i < 3; i++ {
					body := fmt.Sprintf(`{ "model":"original", "stream":%t, "messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"signed-history"},{"type":"redacted_thinking","data":"external-history"}]},{"role":"user","content":"continue"}], "tools":[], "max_tokens":17 }`, stream)
					send(body)
					if change != "model" && received[len(received)-1] != body {
						t.Fatal("request body changed")
					}
				}
				if len(received) != 4 {
					t.Fatalf("successful continuations caused duplicate sends: %d", len(received))
				}
			})
		}
	}
}

func TestClaudeUnknownStateUsesUpstreamVerdictWithoutRetry(t *testing.T) {
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"upstream-state-verdict"}}`)
	}))
	defer server.Close()
	cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL, SkipHealthCheck: true}}}
	h, _ := profileClaudeHandler(t, cfg, 3, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"original","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"external"}]}]}`)))
	if sends != 1 || w.Code != 400 || !strings.Contains(w.Body.String(), "upstream-state-verdict") {
		t.Fatalf("upstream verdict replaced or retried: sends=%d status=%d body=%s", sends, w.Code, w.Body.String())
	}
}
