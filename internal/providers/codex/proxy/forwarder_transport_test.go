package codexproxy

import (
	"log/slog"
	"net/http"
	"net/url"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/transport"
)

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
