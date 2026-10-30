package codexproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCodexForwarderPreservesWireRequestWithoutCredentialLeakOrRedirectReplay(t *testing.T) {
	const body = `{ "model":"unchanged", "stream":false, "input":[], "tools":[], "reasoning":{"effort":"high"}, "max_output_tokens":17 }`
	for _, status := range []int{200, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			sends := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends++
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != body || r.URL.RequestURI() != "/v1/responses?keep=one%2Ftwo" || r.Header.Get("User-Agent") != "native-client" || r.Header.Get("Session_id") != "native-session" || r.Header.Get("Originator") != "codex_cli_rs" {
					t.Error("wire request was changed")
				}
				for _, name := range []string{"X-Api-Key", "X-Relay-Key", "Proxy-Authorization", "X-Connection-Secret", "Via", "Forwarded", "X-Forwarded-For"} {
					if r.Header.Get(name) != "" {
						t.Errorf("unexpected outbound header: %s", name)
					}
				}
				if r.Header.Get("Authorization") != "Bearer selected-key" {
					t.Error("selected credential missing")
				}
				if status == 307 {
					w.Header().Set("Location", "/must-not-replay")
				}
				w.WriteHeader(status)
				io.WriteString(w, `{"status":"completed","output":[]}`)
			}))
			defer server.Close()
			r := httptest.NewRequest("POST", "/v1/responses?keep=one%2Ftwo", nil)
			for name, value := range map[string]string{"Authorization": "Bearer local-key", "X-Api-Key": "local-api-key", "X-Relay-Key": "relay-key", "Proxy-Authorization": "proxy-key", "Connection": "X-Connection-Secret", "X-Connection-Secret": "do-not-forward", "User-Agent": "native-client", "Session_id": "native-session", "Originator": "codex_cli_rs"} {
				r.Header.Set(name, value)
			}
			f := NewForwarder(time.Second, newTestLogger())
			resp, err := f.Forward(r, []byte(body), &CodexEndpoint{BaseURL: server.URL, APIKey: "selected-key"})
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
