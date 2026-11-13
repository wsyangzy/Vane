package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wsyangzy/Vane/internal/config"
	codexproxy "github.com/wsyangzy/Vane/internal/providers/codex/proxy"
)

func TestImagePathsUseCodexInsteadOfClaudeFallback(t *testing.T) {
	a := &App{config: &config.Config{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), codexEndpointMgr: &codexproxy.EndpointManager{}}
	mux := http.NewServeMux()
	a.registerCodexRoutes(mux)
	mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) {})
	for _, path := range []string{"/v1/images/generations", "/images/generations", "/v1/images/edits", "/images/edits"} {
		_, pattern := mux.Handler(httptest.NewRequest("POST", path, nil))
		if pattern != "/v1/images/" && pattern != "/images/" {
			t.Fatalf("image request fell through to Claude: path=%s pattern=%s", path, pattern)
		}
	}
	_, pattern := mux.Handler(httptest.NewRequest("POST", "/v1/messages", nil))
	if pattern != "/" {
		t.Fatalf("Claude messages routing changed: %s", pattern)
	}
}

func TestSearchPathsUseCodexInsteadOfClaudeFallback(t *testing.T) {
	a := &App{config: &config.Config{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), codexEndpointMgr: &codexproxy.EndpointManager{}}
	mux := http.NewServeMux()
	a.registerCodexRoutes(mux)
	mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) {})
	for _, prefix := range []string{"/v1/alpha/search", "/alpha/search", "/backend-api/codex/alpha/search"} {
		for _, suffix := range []string{"", "/native", "/service"} {
			path := prefix + suffix
			_, pattern := mux.Handler(httptest.NewRequest("POST", path, nil))
			if pattern == "/" {
				t.Fatalf("search request routed to Claude: %s", path)
			}
		}
	}
}
