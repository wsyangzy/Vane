package endpoint

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestProfileResourcesAndHealthIgnoreLegacyParticipation(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "scheduled"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)
			legacyDisabled := false
			cfg := &config.Config{
				Health:    config.HealthConfig{Timeout: time.Second, HealthPath: "/v1/models"},
				Endpoints: []config.EndpointConfig{{Name: "resource", Channel: "A", CredentialGroupID: 11, URL: server.URL, Timeout: time.Second, FailoverEnabled: &legacyDisabled}},
			}
			manager := NewManager(cfg)
			manager.UpdateChannelFailoverEnabled(map[string]bool{"A": false})
			pool := routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
			manager.SetPoolRuntime(pool)
			manager.performHealthChecks()
			if requests.Load() != 0 || len(manager.PoolEndpoints("A", 11)) != 0 {
				t.Fatal("legacy disabled endpoint became available")
			}
			pool.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "claude"}))
			if len(manager.ProfileEndpoints("A", 11)) != 1 || len(manager.ProfileEndpoints("A", 12)) != 0 || len(manager.ProfileEndpoints("B", 11)) != 0 {
				t.Fatal("profile resource resolution lost channel/group isolation")
			}
			if batch {
				healthy, unhealthy, err := manager.BatchHealthCheckAll()
				if err != nil || healthy != 1 || unhealthy != 0 {
					t.Fatalf("batch health check = (%d, %d, %v)", healthy, unhealthy, err)
				}
			} else {
				manager.performHealthChecks()
			}
			if requests.Load() != 1 {
				t.Fatalf("unified health requests = %d, want 1", requests.Load())
			}
			if manager.ActiveChannelName() != "" || len(manager.PoolEndpoints("A", 11)) != 0 || *manager.GetAllEndpoints()[0].Config.FailoverEnabled {
				t.Fatal("profile health changed legacy activation or participation")
			}
		})
	}
}

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
