package codexproxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCodexSessionContinuesAfterProfileAndResourceChanges(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, change := range []string{"profile", "reload", "remove-original-group", "model", "restart"} {
			t.Run(fmt.Sprintf("%s/stream=%t", change, stream), func(t *testing.T) {
				var received []string
				h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					received = append(received, string(body))
					if r.Header.Get("Session_id") != "same-session" || r.Header.Get("User-Agent") != "native-codex" || r.Header.Get("X-Codex-Turn-Id") != "same-turn" {
						t.Error("client identity/session headers changed")
					}
					response := fmt.Sprintf(`{"id":"resp-%d","status":"completed","output":[{"type":"reasoning","encrypted_content":"sealed-%d"}]}`, len(received), len(received))
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
					} else {
						w.Header().Set("Content-Type", "application/json")
						io.WriteString(w, response)
					}
				})
				send := func(body string) {
					r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
					r.Header.Set("Session_id", "same-session")
					r.Header.Set("X-Codex-Turn-Id", "same-turn")
					r.Header.Set("User-Agent", "native-codex")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != 200 || strings.Contains(w.Body.String(), "state_backend_incompatible") {
						t.Fatalf("continuation failed: %d %s", w.Code, w.Body.String())
					}
				}
				send(fmt.Sprintf(`{"model":"original","stream":%t,"input":"first"}`, stream))
				profile, _ := runtime.Profile(1)
				switch change {
				case "profile":
					profile.Name, profile.Revision, profile.Strategy = "edited", profile.Revision+1, routing.StrategyRotate
				case "reload":
					next := map[int64][]*CodexEndpoint{}
					for id, endpoints := range h.manager.endpointsByChannel {
						copy := *endpoints[0]
						copy.stateVersion, copy.Priority, copy.Name = 0, 99, "renamed"
						next[id] = []*CodexEndpoint{&copy}
					}
					h.manager.inheritStateVersionsLocked(next)
					h.manager.endpointsByChannel = next
				case "remove-original-group":
					profile.Members = profile.Members[1:]
				case "model":
					for _, endpoints := range h.manager.endpointsByChannel {
						endpoints[0].ModelMapping = map[string]string{"original": "mapped"}
					}
				case "restart":
					h.states = routing.NewStateTracker()
				}
				cs := []routing.DispatchCandidate{{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true}, {Provider: "codex", ChannelID: 2, GroupID: 22, EndpointID: "2", Protocol: "responses", Enabled: true}}
				runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile}, Candidates: cs, PrepareCandidate: h.manager.PrepareDispatchCandidate})
				for i := 0; i < 3; i++ {
					body := fmt.Sprintf(`{ "model":"original", "stream":%t, "previous_response_id":"resp-1", "input":[{"type":"reasoning","encrypted_content":"sealed-1"},{"type":"reasoning","encrypted_content":"external-history"}], "tools":[], "reasoning":{"effort":"high"}, "max_output_tokens":17 }`, stream)
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

func TestCodexUnknownStateUsesUpstreamVerdictWithoutRetry(t *testing.T) {
	sends := 0
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","message":"upstream-state-verdict"}}`)
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","previous_response_id":"unknown","input":[{"type":"reasoning","encrypted_content":"external"}]}`)))
	if sends != 1 || w.Code != 400 || !strings.Contains(w.Body.String(), "upstream-state-verdict") {
		t.Fatalf("upstream verdict replaced or retried: sends=%d status=%d body=%s", sends, w.Code, w.Body.String())
	}
}
