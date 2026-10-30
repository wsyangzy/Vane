package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
)

func TestClaudeForwarderPreservesWireRequestWithoutRedirectReplay(t *testing.T) {
	const body = `{ "model":"unchanged", "stream":false, "messages":[], "tools":[], "thinking":{"type":"enabled","budget_tokens":1024}, "max_tokens":2048 }`
	for _, status := range []int{200, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			sends := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends++
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != body || r.URL.RequestURI() != "/v1/messages?keep=one%2Ftwo" || r.Header.Get("User-Agent") != "native-client" || r.Header.Get("X-Session-Id") != "native-session" || r.Header.Get("Anthropic-Beta") != "native-beta" {
					t.Error("wire request was changed")
				}
				for _, name := range []string{"X-Relay-Key", "Proxy-Authorization", "X-Connection-Secret", "Via", "Forwarded", "X-Forwarded-For"} {
					if r.Header.Get(name) != "" {
						t.Errorf("unexpected outbound header: %s", name)
					}
				}
				if r.Header.Get("X-Api-Key") != "selected-key" {
					t.Error("selected credential missing")
				}
				if status == 307 {
					w.Header().Set("Location", "/must-not-replay")
				}
				w.WriteHeader(status)
				io.WriteString(w, `{"type":"message","content":[],"stop_reason":"end_turn"}`)
			}))
			defer server.Close()
			cfg := &config.Config{}
			f := NewForwarder(cfg, endpoint.NewManager(cfg))
			r := httptest.NewRequest("POST", "/v1/messages?keep=one%2Ftwo", nil)
			for name, value := range map[string]string{"X-Api-Key": "local-key", "X-Relay-Key": "relay-key", "Proxy-Authorization": "proxy-key", "Connection": "X-Connection-Secret", "X-Connection-Secret": "do-not-forward", "User-Agent": "native-client", "X-Session-Id": "native-session", "Anthropic-Beta": "native-beta"} {
				r.Header.Set(name, value)
			}
			resp, err := f.ForwardRequestToEndpoint(context.Background(), r, []byte(body), &endpoint.Endpoint{Config: config.EndpointConfig{URL: server.URL, ApiKey: "selected-key", Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if sends != 1 || resp.StatusCode != status {
				t.Fatalf("unexpected replay/status: sends=%d status=%d", sends, resp.StatusCode)
			}
		})
	}
}
