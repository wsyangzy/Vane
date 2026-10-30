package codexproxy

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestActiveProbeSwitchPreservesManualChecksAndRequestFeedback(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	manager := NewEndpointManager(nil, nil, nil, nil)
	manager.endpointsByChannel = map[int64][]*CodexEndpoint{1: {{ID: 1, ChannelID: 1, BaseURL: server.URL}}}
	pool := routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
	pool.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "codex",
		Profiles:   []routing.RoutingProfile{{Provider: "codex", Enabled: true, IsDefault: true, Members: []routing.RoutingMember{{ChannelID: 1, Enabled: true}}}},
		Candidates: []routing.DispatchCandidate{{Provider: "codex", ChannelID: 1, EndpointID: "1", Enabled: true}},
	}))
	manager.SetPoolRuntime(pool)
	cfg := &config.Config{Health: config.HealthConfig{PauseActiveProbes: true, Timeout: time.Second}}
	manager.SetConfig(cfg)
	checker := NewHealthChecker(manager, cfg, nil)
	checker.Start()
	checker.Stop()
	checker.checkAll()
	if manager.IsHealthCheckEnabled(1) || requests.Load() != 0 {
		t.Fatal("disabled startup or scheduled checks sent an upstream request")
	}
	if err := checker.CheckOne(1); err != nil || requests.Load() != 1 {
		t.Fatalf("manual detection failed while automatic checks were disabled: %v", err)
	}
	manager.MarkUnhealthy(1)
	if manager.IsHealthy(1) {
		t.Fatal("global probe switch disabled request failure feedback")
	}
	manager.MarkHealthy(1)
	if !manager.IsHealthy(1) {
		t.Fatal("global probe switch disabled request recovery feedback")
	}
	cfg.Health.PauseActiveProbes = false
	manager.SetConfig(cfg)
	checker.checkAll()
	if !manager.IsHealthCheckEnabled(1) || requests.Load() != 2 {
		t.Fatal("automatic checks did not resume")
	}
	cfg.Health.PauseActiveProbes = true
	manager.SetConfig(cfg)
	checker.checkAll()
	if requests.Load() != 2 {
		t.Fatal("automatic checks continued after disabling again")
	}
}

func TestDefaultProfileLimitsAutomaticHealthRequests(t *testing.T) {
	var requests [5]atomic.Int32
	manager := NewEndpointManager(nil, nil, nil, slog.Default())
	manager.endpointsByChannel = make(map[int64][]*CodexEndpoint)
	var candidates []routing.DispatchCandidate
	for i := range requests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests[i].Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)
		groupID, channelID := int64(11), int64(1)
		if i == 1 {
			groupID = 12
		}
		if i == 2 {
			channelID = 2
		}
		ep := &CodexEndpoint{ID: int64(i + 1), Name: "same-name", ChannelID: channelID, CredentialGroupID: groupID, BaseURL: server.URL, SkipHealthCheck: i == 1}
		if i == 3 {
			ep.AuthMode = "client_passthrough"
		}
		manager.endpointsByChannel[channelID] = append(manager.endpointsByChannel[channelID], ep)
		candidates = append(candidates, routing.DispatchCandidate{Provider: "codex", ChannelID: channelID, GroupID: groupID, EndpointID: strconv.FormatInt(ep.ID, 10), Enabled: i != 4})
	}
	runtime := routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "codex"})
	pool := routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
	pool.SetProfileRuntime(runtime)
	manager.SetPoolRuntime(pool)
	checker := NewHealthChecker(manager, &config.Config{Health: config.HealthConfig{Timeout: time.Second}}, nil)
	assertCounts := func(want [5]int32) {
		t.Helper()
		for i := range requests {
			if got := requests[i].Load(); got != want[i] {
				t.Fatalf("resource %d received %d requests, want %d", i, got, want[i])
			}
		}
	}
	checker.checkAll()
	assertCounts([5]int32{})
	profile := routing.RoutingProfile{Provider: "codex", Enabled: true, IsDefault: true, Members: []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}}}
	other := routing.RoutingProfile{Provider: "codex", Enabled: true, Members: []routing.RoutingMember{{ChannelID: 2, Enabled: true}}}
	runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile, other}, Candidates: candidates})
	checker.checkAll()
	assertCounts([5]int32{1})
	profile.Members[0].GroupID = 12
	runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile, other}, Candidates: candidates})
	checker.checkAll()
	assertCounts([5]int32{1, 1})
	profile.Enabled = false
	runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile, other}, Candidates: candidates})
	checker.checkAll()
	assertCounts([5]int32{1, 1})
	if err := checker.CheckOne(1); err != nil {
		t.Fatal(err)
	}
	assertCounts([5]int32{2, 1})
	if err := checker.CheckOne(4); err != nil {
		t.Fatal(err)
	}
	checker.checkAll()
	assertCounts([5]int32{2, 1})
}

func TestBuildProbeURL(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		healthPath string
		want       string
	}{
		// ===== 默认路径 /v1/models =====
		{
			name:       "default path - plain host",
			baseURL:    "https://right.codes/codex",
			healthPath: "/v1/models",
			want:       "https://right.codes/codex/v1/models",
		},
		{
			name:       "default path - already has v1",
			baseURL:    "https://right.codes/codex/v1",
			healthPath: "/v1/models",
			want:       "https://right.codes/codex/v1/models",
		},
		{
			name:       "default path - already has v1 trailing slash",
			baseURL:    "https://right.codes/codex/v1/",
			healthPath: "/v1/models",
			want:       "https://right.codes/codex/v1/models",
		},
		{
			name:       "default path - root v1",
			baseURL:    "https://api.openai.com/v1",
			healthPath: "/v1/models",
			want:       "https://api.openai.com/v1/models",
		},
		{
			name:       "default path - root no v1",
			baseURL:    "https://api.openai.com",
			healthPath: "/v1/models",
			want:       "https://api.openai.com/v1/models",
		},

		// ===== 自定义路径 =====
		{
			name:       "custom path - /health",
			baseURL:    "https://api.example.com",
			healthPath: "/health",
			want:       "https://api.example.com/health",
		},
		{
			name:       "custom path - /ping with v1 in base",
			baseURL:    "https://api.example.com/v1",
			healthPath: "/ping",
			want:       "https://api.example.com/v1/ping",
		},
		{
			name:       "custom path - without leading slash",
			baseURL:    "https://api.example.com",
			healthPath: "status",
			want:       "https://api.example.com/status",
		},

		// ===== 兜底（空 healthPath）=====
		{
			name:       "empty healthPath fallback to /v1/models",
			baseURL:    "https://api.example.com",
			healthPath: "",
			want:       "https://api.example.com/v1/models",
		},
		{
			name:       "whitespace healthPath fallback to /v1/models",
			baseURL:    "https://api.example.com/v1",
			healthPath: "   ",
			want:       "https://api.example.com/v1/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildProbeURL(tt.baseURL, tt.healthPath); got != tt.want {
				t.Fatalf("buildProbeURL(%q, %q) = %q, want %q", tt.baseURL, tt.healthPath, got, tt.want)
			}
		})
	}
}

func TestHealthCheckerSkippedEndpointStillRecordsLatencyWithoutChangingHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	manager := NewEndpointManager(nil, nil, nil, slog.Default())
	const endpointID int64 = 42
	manager.MarkUnhealthy(endpointID)

	checker := NewHealthChecker(manager, &config.Config{
		Health: config.HealthConfig{
			HealthPath: "/health",
		},
	}, slog.Default())

	checker.checkEndpoint(&CodexEndpoint{
		ID:              endpointID,
		Name:            "skipped",
		BaseURL:         server.URL,
		SkipHealthCheck: true,
	})

	latencies := checker.GetLatencies()
	if got := latencies[endpointID]; got <= 0 {
		t.Fatalf("expected skipped endpoint latency to be recorded, got %d", got)
	}
	if checker.GetLastChecks()[endpointID].IsZero() {
		t.Fatalf("expected skipped endpoint last check time to be recorded")
	}
	if manager.IsHealthy(endpointID) {
		t.Fatalf("expected skipped endpoint probe not to change routing health")
	}
}
