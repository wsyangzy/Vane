package endpoint

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claude-request-forwarder/config"
)

func TestHealthCheck_SkipHealthCheck_RecordsLatencyButKeepsNeverChecked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{}
	cfg.Health.Timeout = 2 * time.Second
	cfg.Health.HealthPath = "/v1/models"

	manager := NewManager(cfg)

	ep := &Endpoint{
		Config: config.EndpointConfig{
			Name:            "skip-ep",
			URL:             server.URL,
			SkipHealthCheck: true,
		},
		Status: EndpointStatus{
			Healthy:      false,
			NeverChecked: true,
		},
	}

	manager.checkEndpointHealth(ep)

	ep.mutex.RLock()
	defer ep.mutex.RUnlock()

	if !ep.Status.NeverChecked {
		t.Fatalf("expected NeverChecked to remain true for skip health check endpoint")
	}
	if !ep.Status.Healthy {
		t.Fatalf("expected skip health check endpoint to be treated as healthy")
	}
	if ep.Status.ResponseTime <= 0 {
		t.Fatalf("expected ResponseTime to be recorded, got %v", ep.Status.ResponseTime)
	}
	if ep.Status.LastCheck.IsZero() {
		t.Fatalf("expected LastCheck to be set")
	}
}

