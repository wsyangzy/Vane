package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecoveryStrategyIsValid(t *testing.T) {
	tests := []struct {
		name     string
		strategy RecoveryStrategy
		expected bool
	}{
		{"auto_wait is valid", RecoveryStrategyAutoWait, true},
		{"loop_retry is valid", RecoveryStrategyLoopRetry, true},
		{"fail_fast is valid", RecoveryStrategyFailFast, true},
		{"empty string is valid for migration", RecoveryStrategy(""), true},
		{"random string is invalid", RecoveryStrategy("random"), false},
		{"AUTO_WAIT uppercase is invalid", RecoveryStrategy("AUTO_WAIT"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.strategy.IsValid(); got != tt.expected {
				t.Errorf("IsValid()=%v, expected=%v", got, tt.expected)
			}
		})
	}
}

func TestRecoveryStrategyString(t *testing.T) {
	tests := []struct {
		name     string
		strategy RecoveryStrategy
		expected string
	}{
		{"auto_wait", RecoveryStrategyAutoWait, "智能等待恢复（已废弃）"},
		{"loop_retry", RecoveryStrategyLoopRetry, "循环重试"},
		{"fail_fast", RecoveryStrategyFailFast, "快速失败"},
		{"unknown", RecoveryStrategy("unknown"), "未知策略"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.strategy.String(); got != tt.expected {
				t.Errorf("String()=%q, expected=%q", got, tt.expected)
			}
		})
	}
}

func TestRecoveryStrategyShouldWaitForRecovery(t *testing.T) {
	tests := []RecoveryStrategy{RecoveryStrategyAutoWait, RecoveryStrategyLoopRetry, RecoveryStrategyFailFast}
	for _, strategy := range tests {
		if strategy.ShouldWaitForRecovery() {
			t.Errorf("ShouldWaitForRecovery() for %q should be false", strategy)
		}
	}
}

func TestRecoveryStrategyShouldLoopRetry(t *testing.T) {
	tests := []struct {
		strategy RecoveryStrategy
		expected bool
	}{
		{RecoveryStrategyLoopRetry, true},
		{RecoveryStrategyAutoWait, false},
		{RecoveryStrategyFailFast, false},
	}

	for _, tt := range tests {
		if got := tt.strategy.ShouldLoopRetry(); got != tt.expected {
			t.Errorf("ShouldLoopRetry(%q)=%v, expected=%v", tt.strategy, got, tt.expected)
		}
	}
}

func TestFailoverConfigV2Validate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         FailoverConfigV2
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid default config",
			cfg: FailoverConfigV2{
				LoopRetry:               false,
				MaxLoops:                3,
				SuspendTimeout:          120 * time.Second,
				LoopRetrySuspendTimeout: 120 * time.Second,
			},
			expectError: false,
		},
		{
			name: "invalid negative max_loops when loop_retry enabled",
			cfg: FailoverConfigV2{
				LoopRetry: true,
				MaxLoops:  -1,
			},
			expectError: true,
			errorMsg:    "max_loops cannot be negative",
		},
		{
			name: "invalid too large max_loops",
			cfg: FailoverConfigV2{
				MaxLoops: 101,
			},
			expectError: true,
			errorMsg:    "max_loops cannot exceed 100",
		},
		{
			name: "invalid negative suspend_timeout",
			cfg: FailoverConfigV2{
				SuspendTimeout: -1 * time.Second,
			},
			expectError: true,
			errorMsg:    "suspend_timeout cannot be negative",
		},
		{
			name: "invalid negative loop_retry_suspend_timeout",
			cfg: FailoverConfigV2{
				LoopRetrySuspendTimeout: -1 * time.Second,
			},
			expectError: true,
			errorMsg:    "loop_retry_suspend_timeout cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errorMsg)
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestFailoverConfigV2SetDefaults(t *testing.T) {
	tests := []struct {
		name                     string
		cfg                      FailoverConfigV2
		expectedMaxLoops         int
		expectedSuspendTimeout   time.Duration
		expectedLoopRetryTimeout time.Duration
		expectedRecoveryStrategy RecoveryStrategy
	}{
		{
			name:                     "empty config sets defaults",
			cfg:                      FailoverConfigV2{},
			expectedMaxLoops:         3,
			expectedSuspendTimeout:   120 * time.Second,
			expectedLoopRetryTimeout: 120 * time.Second,
			expectedRecoveryStrategy: "",
		},
		{
			name: "auto_wait_timeout migrates to suspend timeouts",
			cfg: FailoverConfigV2{
				AutoWaitTimeout: 45 * time.Second,
			},
			expectedMaxLoops:         3,
			expectedSuspendTimeout:   45 * time.Second,
			expectedLoopRetryTimeout: 45 * time.Second,
			expectedRecoveryStrategy: "",
		},
		{
			name: "suspend_timeout populates loop_retry_suspend_timeout",
			cfg: FailoverConfigV2{
				MaxLoops:       8,
				SuspendTimeout: 30 * time.Second,
			},
			expectedMaxLoops:         8,
			expectedSuspendTimeout:   30 * time.Second,
			expectedLoopRetryTimeout: 30 * time.Second,
			expectedRecoveryStrategy: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.SetDefaults()

			if cfg.MaxLoops != tt.expectedMaxLoops {
				t.Fatalf("MaxLoops=%d, expected=%d", cfg.MaxLoops, tt.expectedMaxLoops)
			}
			if cfg.SuspendTimeout != tt.expectedSuspendTimeout {
				t.Fatalf("SuspendTimeout=%v, expected=%v", cfg.SuspendTimeout, tt.expectedSuspendTimeout)
			}
			if cfg.LoopRetrySuspendTimeout != tt.expectedLoopRetryTimeout {
				t.Fatalf("LoopRetrySuspendTimeout=%v, expected=%v", cfg.LoopRetrySuspendTimeout, tt.expectedLoopRetryTimeout)
			}
			if cfg.RecoveryStrategy != tt.expectedRecoveryStrategy {
				t.Fatalf("RecoveryStrategy=%q, expected=%q", cfg.RecoveryStrategy, tt.expectedRecoveryStrategy)
			}
		})
	}
}

func TestFailoverConfigV2MigrateFromLegacy(t *testing.T) {
	tests := []struct {
		name                     string
		oldFailover              *FailoverConfig
		oldSuspend               *RequestSuspendConfig
		expectedLoopRetry        bool
		expectedSuspendTimeout   time.Duration
		expectedLoopRetryTimeout time.Duration
	}{
		{
			name: "allow_revisit true migrates to loop_retry true",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				AllowRevisitAfterExhausted: true,
			},
			oldSuspend:               nil,
			expectedLoopRetry:        true,
			expectedSuspendTimeout:   0,
			expectedLoopRetryTimeout: 0,
		},
		{
			name: "allow_revisit false migrates to loop_retry false",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				AllowRevisitAfterExhausted: false,
			},
			oldSuspend:               nil,
			expectedLoopRetry:        false,
			expectedSuspendTimeout:   0,
			expectedLoopRetryTimeout: 0,
		},
		{
			name: "legacy suspend timeout migrates to new timeout fields",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				AllowRevisitAfterExhausted: false,
			},
			oldSuspend: &RequestSuspendConfig{
				Enabled: true,
				Timeout: 75 * time.Second,
			},
			expectedLoopRetry:        false,
			expectedSuspendTimeout:   75 * time.Second,
			expectedLoopRetryTimeout: 75 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &FailoverConfigV2{}
			fc.MigrateFromLegacy(tt.oldFailover, tt.oldSuspend)

			if fc.LoopRetry != tt.expectedLoopRetry {
				t.Fatalf("LoopRetry=%v, expected=%v", fc.LoopRetry, tt.expectedLoopRetry)
			}
			if fc.SuspendTimeout != tt.expectedSuspendTimeout {
				t.Fatalf("SuspendTimeout=%v, expected=%v", fc.SuspendTimeout, tt.expectedSuspendTimeout)
			}
			if fc.LoopRetrySuspendTimeout != tt.expectedLoopRetryTimeout {
				t.Fatalf("LoopRetrySuspendTimeout=%v, expected=%v", fc.LoopRetrySuspendTimeout, tt.expectedLoopRetryTimeout)
			}
			if fc.Enabled != tt.oldFailover.Enabled {
				t.Fatalf("Enabled=%v, expected=%v", fc.Enabled, tt.oldFailover.Enabled)
			}
		})
	}
}

func TestLoadConfigWithV60Format(t *testing.T) {
	configContent := `
proxy:
  host: "127.0.0.1"
  port: 9090

endpoints:
  - name: "primary"
    url: "https://api.anthropic.com"
    priority: 1
    token: "sk-ant-xxx"

failover:
  enabled: true
  default_cooldown: "10m"
  allow_revisit_after_exhausted: false

request_suspend:
  enabled: true
  timeout: "300s"
  max_suspended_requests: 100
`

	tmpFile, err := os.CreateTemp("", "test-v60-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !cfg.FailoverV2.Enabled {
		t.Fatalf("expected FailoverV2.Enabled=true")
	}
	if cfg.FailoverV2.LoopRetry {
		t.Fatalf("expected LoopRetry=false after migration")
	}
	if cfg.FailoverV2.SuspendTimeout != 300*time.Second {
		t.Fatalf("expected SuspendTimeout=300s, got %v", cfg.FailoverV2.SuspendTimeout)
	}
	if cfg.FailoverV2.LoopRetrySuspendTimeout != 300*time.Second {
		t.Fatalf("expected LoopRetrySuspendTimeout=300s, got %v", cfg.FailoverV2.LoopRetrySuspendTimeout)
	}
	if cfg.FailoverV2.MaxLoops != 3 {
		t.Fatalf("expected MaxLoops=3, got %d", cfg.FailoverV2.MaxLoops)
	}
}

func TestLoadConfigWithV61Format(t *testing.T) {
	configContent := `
proxy:
  host: "127.0.0.1"
  port: 9090

endpoints:
  - name: "primary"
    url: "https://api.anthropic.com"
    priority: 1
    token: "sk-ant-xxx"

failover_v2:
  enabled: true
  default_cooldown: "10m"
  recovery_strategy: "loop_retry"
`

	tmpFile, err := os.CreateTemp("", "test-v61-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !cfg.FailoverV2.LoopRetry {
		t.Fatalf("expected LoopRetry=true")
	}
	if cfg.FailoverV2.RecoveryStrategy != "" {
		t.Fatalf("expected RecoveryStrategy to be cleared after migration, got %q", cfg.FailoverV2.RecoveryStrategy)
	}
	if cfg.FailoverV2.SuspendTimeout != 120*time.Second {
		t.Fatalf("expected default SuspendTimeout=120s, got %v", cfg.FailoverV2.SuspendTimeout)
	}
	if cfg.FailoverV2.LoopRetrySuspendTimeout != 120*time.Second {
		t.Fatalf("expected default LoopRetrySuspendTimeout=120s, got %v", cfg.FailoverV2.LoopRetrySuspendTimeout)
	}
}

func TestLoadConfigWithInvalidStrategy(t *testing.T) {
	configContent := `
proxy:
  host: "127.0.0.1"
  port: 9090

endpoints:
  - name: "primary"
    url: "https://api.anthropic.com"
    priority: 1
    token: "sk-ant-xxx"

failover_v2:
  enabled: true
  recovery_strategy: "invalid_strategy"
`

	tmpFile, err := os.CreateTemp("", "test-invalid-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("expected invalid legacy strategy to be tolerated, got error: %v", err)
	}

	if cfg.FailoverV2.RecoveryStrategy != "" {
		t.Fatalf("expected RecoveryStrategy cleared after migration, got %q", cfg.FailoverV2.RecoveryStrategy)
	}
	if cfg.FailoverV2.LoopRetry {
		t.Fatalf("expected LoopRetry=false for unknown legacy strategy")
	}
}
