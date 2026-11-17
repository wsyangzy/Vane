package codexproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestExplicitEndpointPathsMatchForwardingAndHealthAcrossModes(t *testing.T) {
	const raw = `{ "input":"unchanged", "stream":true, "tools":[], "reasoning":{"effort":"high"} }`
	for _, strategy := range []string{"base", "v1"} {
		for _, mode := range []string{"legacy", "pool", "profile"} {
			t.Run(strategy+"/"+mode, func(t *testing.T) {
				var paths []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					paths = append(paths, r.URL.RequestURI())
					if r.Method == http.MethodPost {
						b, _ := io.ReadAll(r.Body)
						if string(b) != raw || r.Header.Get("User-Agent") != "client" {
							t.Error("request payload or client identity changed")
						}
					}
					io.WriteString(w, `{"data":[]}`)
				}))
				defer server.Close()
				ep := &CodexEndpoint{ID: 1, ChannelID: 1, BaseURL: server.URL + "/custom", PathStrategy: strategy}
				r := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses?keep=a%2Fb", strings.NewReader(raw))
				r.Header.Set("User-Agent", "client")
				if mode == "pool" {
					r = r.WithContext(routing.WithPoolCaller(r.Context(), routing.PoolCaller{Provider: "codex", PoolID: 1}))
				}
				if mode == "profile" {
					r = r.WithContext(routing.WithProfileRequest(r.Context(), &routing.DispatchPlan{}))
				}
				resp, err := NewForwarder(time.Second, newTestLogger()).Forward(r, []byte(raw), ep)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				manager := NewEndpointManager(nil, nil, nil, newTestLogger())
				manager.endpointsByChannel = map[int64][]*CodexEndpoint{1: {ep}}
				NewHealthChecker(manager, &config.Config{Health: config.HealthConfig{Timeout: time.Second}}, newTestLogger()).checkEndpoint(ep)
				prefix := "/custom"
				if strategy == "v1" {
					prefix += "/v1"
				}
				if len(paths) != 2 || paths[0] != prefix+"/responses?keep=a%2Fb" || paths[1] != prefix+"/models" {
					t.Fatalf("forward/probe paths=%v", paths)
				}
			})
		}
	}
}
