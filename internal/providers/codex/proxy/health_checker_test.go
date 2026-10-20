package codexproxy

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
)

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
