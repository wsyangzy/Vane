package endpoint

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestActiveProbeSwitchPreservesManualChecks(t *testing.T) {
	for _, withProfile := range []bool{false, true} {
		t.Run(fmt.Sprintf("profile=%v", withProfile), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			cfg := &config.Config{Health: config.HealthConfig{PauseActiveProbes: true, Timeout: time.Second},
				Strategy:  config.StrategyConfig{FastTestEnabled: true, FastTestTimeout: time.Second},
				Endpoints: []config.EndpointConfig{{Name: "selected", Channel: "A", URL: server.URL}}}
			manager := NewManager(cfg)
			manager.healthThrottleInterval = 0
			ep := manager.GetAllEndpoints()[0]
			key := EndpointKeyForConfig(ep.Config)
			if withProfile {
				pool := routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
				pool.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "claude",
					Profiles:   []routing.RoutingProfile{{Provider: "claude", Enabled: true, IsDefault: true, Members: []routing.RoutingMember{{ChannelID: 1, Enabled: true}}}},
					Candidates: []routing.DispatchCandidate{{Provider: "claude", ChannelID: 1, EndpointID: key, Enabled: true}},
				}))
				manager.SetPoolRuntime(pool)
			}
			manager.performHealthChecks()
			manager.checkEndpointHealth(ep)
			_, _, _ = manager.BatchHealthCheckAll()
			_, _ = manager.fastTester.TestEndpointsParallel(context.Background(), []*Endpoint{ep})
			if manager.IsHealthCheckEnabled(key) || requests.Load() != 0 {
				t.Fatal("disabled automatic probes sent an upstream request")
			}
			manager.checkEndpointHealthForced(ep)
			if requests.Load() != 1 {
				t.Fatal("disabled automatic probes blocked a manual check")
			}
			cfg.Health.PauseActiveProbes = false
			manager.UpdateConfig(cfg)
			manager.checkEndpointHealth(ep)
			if !manager.IsHealthCheckEnabled(key) || requests.Load() != 2 {
				t.Fatal("automatic checks did not resume after enabling")
			}
			cfg.Health.PauseActiveProbes = true
			manager.UpdateConfig(cfg)
			manager.checkEndpointHealth(ep)
			if requests.Load() != 2 {
				t.Fatal("automatic checks continued after disabling again")
			}
		})
	}
}

func TestDefaultProfileLimitsAutomaticHealthRequests(t *testing.T) {
	for _, mode := range []string{"scheduled", "batch", "crud"} {
		t.Run(mode, func(t *testing.T) {
			var requests [5]atomic.Int32
			cfg := &config.Config{Health: config.HealthConfig{Timeout: time.Second, HealthPath: "/health"}}
			var candidates []routing.DispatchCandidate
			for i := range requests {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests[i].Add(1)
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(server.Close)
				groupID := int64(11)
				channel, channelID := "A", int64(1)
				if i == 1 {
					groupID = 12
				}
				if i == 2 {
					channel, channelID = "B", 2
				}
				ep := config.EndpointConfig{Name: "same-name", Channel: channel, CredentialGroupID: groupID, RuntimeKey: fmt.Sprintf("resource-%d", i), URL: server.URL, Timeout: time.Second, SkipHealthCheck: i == 1}
				if i == 3 {
					ep.AuthMode = "client_passthrough"
				}
				cfg.Endpoints = append(cfg.Endpoints, ep)
				candidates = append(candidates, routing.DispatchCandidate{Provider: "claude", ChannelID: channelID, GroupID: groupID, EndpointID: ep.RuntimeKey, Enabled: i != 4})
			}
			manager := NewManager(cfg)
			manager.healthThrottleInterval = 0
			runtime := routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "claude"})
			pool := routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
			pool.SetProfileRuntime(runtime)
			manager.SetPoolRuntime(pool)
			probe := func() {
				switch mode {
				case "scheduled":
					manager.performHealthChecks()
				case "batch":
					_, _, _ = manager.BatchHealthCheckAll()
				case "crud":
					// AddEndpoint and UpdateEndpointConfig use this same guarded entry.
					for _, ep := range manager.GetAllEndpoints() {
						manager.checkEndpointHealth(ep)
					}
				}
			}
			assertCounts := func(want [5]int32) {
				t.Helper()
				for i := range requests {
					if got := requests[i].Load(); got != want[i] {
						t.Fatalf("resource %d received %d requests, want %d", i, got, want[i])
					}
				}
			}
			probe()
			assertCounts([5]int32{})
			profile := routing.RoutingProfile{Provider: "claude", Enabled: true, IsDefault: true, Members: []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}}}
			other := routing.RoutingProfile{Provider: "claude", Enabled: true, Members: []routing.RoutingMember{{ChannelID: 2, Enabled: true}}}
			runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile, other}, Candidates: candidates})
			probe()
			assertCounts([5]int32{1})
			profile.Members[0].GroupID = 12
			runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile, other}, Candidates: candidates})
			probe()
			assertCounts([5]int32{1, 1})
			profile.Enabled = false
			runtime.Replace(routing.RuntimeConfig{Provider: "claude", Profiles: []routing.RoutingProfile{profile, other}, Candidates: candidates})
			probe()
			assertCounts([5]int32{1, 1})
			manager.checkEndpointHealthForced(manager.GetAllEndpoints()[0])
			assertCounts([5]int32{2, 1})
			probe()
			assertCounts([5]int32{2, 1})
		})
	}
}

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
			pool.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{
				Provider:   "claude",
				Profiles:   []routing.RoutingProfile{{Provider: "claude", Enabled: true, IsDefault: true, Members: []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}}}},
				Candidates: []routing.DispatchCandidate{{Provider: "claude", ChannelID: 1, GroupID: 11, EndpointID: EndpointKeyForConfig(cfg.Endpoints[0]), Enabled: true}},
			}))
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
