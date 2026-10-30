package codexproxy

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/transport"
)

func TestHTTPSettingsApplyToNewClientsWithoutChangingInflightClients(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.GlobalTimeout = 12 * time.Second
	h := NewHTTPHandler(nil, ServerConfig{Timeout: time.Second, Config: cfg}, slog.Default(), nil)
	old, err := h.handler.forwarder.clientForEndpoint(nil)
	if err != nil || old.Timeout != 12*time.Second {
		t.Fatalf("initial default timeout = %v, %v", old, err)
	}
	cfg.GlobalTimeout = 24 * time.Second
	cfg.Streaming.DefaultResponseHeaderTimeout = 8 * time.Second
	cfg.Streaming.ResponseHeaderTimeout = 16 * time.Second
	next, err := h.handler.forwarder.clientForEndpoint(nil)
	if err != nil || next.Timeout != 24*time.Second || old.Timeout != 12*time.Second {
		t.Fatal("live update did not isolate new and existing clients")
	}
	if next.Transport.(*http.Transport).ResponseHeaderTimeout != 8*time.Second {
		t.Fatal("non-streaming response header setting was not applied")
	}
	explicit, err := h.handler.forwarder.clientForEndpoint(&CodexEndpoint{Timeout: time.Second})
	if err != nil || explicit.Timeout != time.Second {
		t.Fatal("endpoint timeout must take precedence over the default")
	}
	stream, err := h.handler.streamFwd.clientForEndpoint(&CodexEndpoint{Timeout: time.Second})
	if err != nil || stream.Timeout != 0 || stream.Transport.(*http.Transport).ResponseHeaderTimeout != 16*time.Second {
		t.Fatal("streaming header timeout changed the stream duration limit")
	}
}

func TestDefaultTimeoutActuallyStopsWaitingForUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	cfg := config.NewDefaultConfig()
	cfg.GlobalTimeout = 20 * time.Millisecond
	h := NewHTTPHandler(nil, ServerConfig{Config: cfg}, slog.Default(), nil)
	client, err := h.handler.forwarder.clientForEndpoint(nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("configured non-streaming timeout did not stop the request")
	}
}

func TestForwarderUsesConfiguredTransport(t *testing.T) {
	proxyURL, err := url.Parse("http://127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Proxy: config.ProxyConfig{Enabled: true, Type: "http", URL: proxyURL.String()}}
	forwarder := NewForwarder(time.Second, slog.Default(), transport.NewProvider(cfg))

	client, err := forwarder.clientForEndpoint(nil)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("Codex forwarder did not use the configured HTTP transport")
	}
	got, err := tr.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "api.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.String() != proxyURL.String() {
		t.Fatalf("proxy = %v, want %s", got, proxyURL)
	}
}

func TestStreamingForwarderUsesStreamingTransport(t *testing.T) {
	cfg := &config.Config{Streaming: config.StreamingConfig{ResponseHeaderTimeout: 7 * time.Minute}}
	forwarder := NewStreamingForwarder(slog.Default(), transport.NewProvider(cfg))

	client, err := forwarder.clientForEndpoint(nil)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.ResponseHeaderTimeout != 7*time.Minute {
		t.Fatalf("streaming forwarder did not use Streaming.ResponseHeaderTimeout: %+v", client.Transport)
	}
}
