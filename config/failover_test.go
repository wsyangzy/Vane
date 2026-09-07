package config

import (
	"testing"
	"time"
)

// TestFailoverConfig_SetDefaults 验证 SetDefaults 行为
func TestFailoverConfig_SetDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    FailoverConfig
		expected FailoverConfig
	}{
		{
			name:  "all zero values get defaults",
			input: FailoverConfig{},
			expected: FailoverConfig{
				MaxLoops:                3,
				SuspendTimeout:          120 * time.Second,
				LoopRetrySuspendTimeout: 120 * time.Second,
				MaxSuspendedRequests:    100,
			},
		},
		{
			name: "custom MaxLoops preserved",
			input: FailoverConfig{
				MaxLoops: 5,
			},
			expected: FailoverConfig{
				MaxLoops:                5,
				SuspendTimeout:          120 * time.Second,
				LoopRetrySuspendTimeout: 120 * time.Second,
				MaxSuspendedRequests:    100,
			},
		},
		{
			name: "LoopRetrySuspendTimeout falls back to SuspendTimeout",
			input: FailoverConfig{
				SuspendTimeout: 60 * time.Second,
			},
			expected: FailoverConfig{
				MaxLoops:                3,
				SuspendTimeout:          60 * time.Second,
				LoopRetrySuspendTimeout: 60 * time.Second,
				MaxSuspendedRequests:    100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.input
			cfg.SetDefaults()
			if cfg.MaxLoops != tt.expected.MaxLoops {
				t.Errorf("MaxLoops: got %d, want %d", cfg.MaxLoops, tt.expected.MaxLoops)
			}
			if cfg.SuspendTimeout != tt.expected.SuspendTimeout {
				t.Errorf("SuspendTimeout: got %v, want %v", cfg.SuspendTimeout, tt.expected.SuspendTimeout)
			}
			if cfg.LoopRetrySuspendTimeout != tt.expected.LoopRetrySuspendTimeout {
				t.Errorf("LoopRetrySuspendTimeout: got %v, want %v", cfg.LoopRetrySuspendTimeout, tt.expected.LoopRetrySuspendTimeout)
			}
			if cfg.MaxSuspendedRequests != tt.expected.MaxSuspendedRequests {
				t.Errorf("MaxSuspendedRequests: got %d, want %d", cfg.MaxSuspendedRequests, tt.expected.MaxSuspendedRequests)
			}
		})
	}
}

// TestFailoverConfig_Validate 验证 Validate 各种边界条件
func TestFailoverConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         FailoverConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid default config",
			cfg: FailoverConfig{
				LoopRetry:               false,
				MaxLoops:                3,
				SuspendTimeout:          120 * time.Second,
				LoopRetrySuspendTimeout: 120 * time.Second,
				MaxSuspendedRequests:    100,
			},
			expectError: false,
		},
		{
			name: "negative max_loops when loop_retry enabled",
			cfg: FailoverConfig{
				LoopRetry: true,
				MaxLoops:  -1,
			},
			expectError: true,
			errorMsg:    "max_loops cannot be negative",
		},
		{
			name: "max_loops too large",
			cfg: FailoverConfig{
				MaxLoops: 101,
			},
			expectError: true,
			errorMsg:    "max_loops cannot exceed 100",
		},
		{
			name: "negative suspend_timeout",
			cfg: FailoverConfig{
				SuspendTimeout: -1 * time.Second,
			},
			expectError: true,
			errorMsg:    "suspend_timeout cannot be negative",
		},
		{
			name: "negative loop_retry_suspend_timeout",
			cfg: FailoverConfig{
				LoopRetrySuspendTimeout: -1 * time.Second,
			},
			expectError: true,
			errorMsg:    "loop_retry_suspend_timeout cannot be negative",
		},
		{
			name: "negative max_suspended_requests",
			cfg: FailoverConfig{
				MaxSuspendedRequests: -1,
			},
			expectError: true,
			errorMsg:    "max_suspended_requests cannot be negative",
		},
		{
			name: "max_suspended_requests too large",
			cfg: FailoverConfig{
				MaxSuspendedRequests: 10001,
			},
			expectError: true,
			errorMsg:    "max_suspended_requests cannot exceed 10000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.errorMsg)
				}
				if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestFailoverConfig_ShouldSkipHealthCheck 验证 ShouldSkipHealthCheck 仅当 3 个开关全开时返回 true
func TestFailoverConfig_ShouldSkipHealthCheck(t *testing.T) {
	tests := []struct {
		name string
		cfg  FailoverConfig
		want bool
	}{
		{"all false", FailoverConfig{}, false},
		{"only enabled", FailoverConfig{Enabled: true}, false},
		{"enabled + loop_retry", FailoverConfig{Enabled: true, LoopRetry: true}, false},
		{"only skip", FailoverConfig{SkipHealthCheck: true}, false},
		{"all 3 true", FailoverConfig{Enabled: true, LoopRetry: true, SkipHealthCheck: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ShouldSkipHealthCheck(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFailoverConfig_GetSuspendTimeout 验证 GetSuspendTimeout 兜底逻辑
func TestFailoverConfig_GetSuspendTimeout(t *testing.T) {
	tests := []struct {
		name string
		cfg  *FailoverConfig
		want time.Duration
	}{
		{"nil cfg", nil, 120 * time.Second},
		{"zero value", &FailoverConfig{}, 120 * time.Second},
		{"explicit value", &FailoverConfig{SuspendTimeout: 60 * time.Second}, 60 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetSuspendTimeout(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFailoverConfig_GetLoopRetrySuspendTimeout 验证 fallback 链
func TestFailoverConfig_GetLoopRetrySuspendTimeout(t *testing.T) {
	tests := []struct {
		name string
		cfg  *FailoverConfig
		want time.Duration
	}{
		{"nil cfg", nil, 120 * time.Second},
		{"zero value", &FailoverConfig{}, 120 * time.Second},
		{"explicit LoopRetrySuspendTimeout", &FailoverConfig{LoopRetrySuspendTimeout: 90 * time.Second}, 90 * time.Second},
		{"fallback to SuspendTimeout", &FailoverConfig{SuspendTimeout: 45 * time.Second}, 45 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetLoopRetrySuspendTimeout(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// contains 简化的字符串包含检查（避免引入 strings 包冲突）
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
