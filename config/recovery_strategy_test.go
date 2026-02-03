package config

import (
	"os"
	"testing"
	"time"
)

// TestRecoveryStrategyIsValid 测试恢复策略验证
func TestRecoveryStrategyIsValid(t *testing.T) {
	tests := []struct {
		name     string
		strategy RecoveryStrategy
		expected bool
	}{
		{"auto_wait is valid", RecoveryStrategyAutoWait, true},
		{"loop_retry is valid", RecoveryStrategyLoopRetry, true},
		{"fail_fast is valid", RecoveryStrategyFailFast, true},
		{"empty string is invalid", RecoveryStrategy(""), false},
		{"random string is invalid", RecoveryStrategy("random"), false},
		{"AUTO_WAIT uppercase is invalid", RecoveryStrategy("AUTO_WAIT"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.strategy.IsValid()
			if result != tt.expected {
				t.Errorf("Expected IsValid()=%v, got %v", tt.expected, result)
			}
		})
	}
}

// TestRecoveryStrategyString 测试恢复策略可读名称
func TestRecoveryStrategyString(t *testing.T) {
	tests := []struct {
		name     string
		strategy RecoveryStrategy
		expected string
	}{
		{"auto_wait", RecoveryStrategyAutoWait, "智能等待恢复"},
		{"loop_retry", RecoveryStrategyLoopRetry, "循环重试"},
		{"fail_fast", RecoveryStrategyFailFast, "快速失败"},
		{"unknown", RecoveryStrategy("unknown"), "未知策略"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.strategy.String()
			if result != tt.expected {
				t.Errorf("Expected String()=%s, got %s", tt.expected, result)
			}
		})
	}
}

// TestRecoveryStrategyShouldWaitForRecovery 测试是否应等待恢复判断
func TestRecoveryStrategyShouldWaitForRecovery(t *testing.T) {
	tests := []struct {
		name     string
		strategy RecoveryStrategy
		expected bool
	}{
		{"auto_wait should wait", RecoveryStrategyAutoWait, true},
		{"loop_retry should not wait", RecoveryStrategyLoopRetry, false},
		{"fail_fast should not wait", RecoveryStrategyFailFast, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.strategy.ShouldWaitForRecovery()
			if result != tt.expected {
				t.Errorf("Expected ShouldWaitForRecovery()=%v, got %v", tt.expected, result)
			}
		})
	}
}

// TestRecoveryStrategyShouldLoopRetry 测试是否应循环重试判断
func TestRecoveryStrategyShouldLoopRetry(t *testing.T) {
	tests := []struct {
		name     string
		strategy RecoveryStrategy
		expected bool
	}{
		{"loop_retry should loop", RecoveryStrategyLoopRetry, true},
		{"auto_wait should not loop", RecoveryStrategyAutoWait, false},
		{"fail_fast should not loop", RecoveryStrategyFailFast, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.strategy.ShouldLoopRetry()
			if result != tt.expected {
				t.Errorf("Expected ShouldLoopRetry()=%v, got %v", tt.expected, result)
			}
		})
	}
}

// TestFailoverConfigV2Validate 测试 FailoverConfigV2 验证
func TestFailoverConfigV2Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      *FailoverConfigV2
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid auto_wait config",
			config: &FailoverConfigV2{
				RecoveryStrategy:    RecoveryStrategyAutoWait,
				AutoWaitTimeout:     300 * time.Second,
				AutoWaitMaxRequests: 100,
			},
			expectError: false,
		},
		{
			name: "valid loop_retry config",
			config: &FailoverConfigV2{
				RecoveryStrategy: RecoveryStrategyLoopRetry,
			},
			expectError: false,
		},
		{
			name: "valid fail_fast config",
			config: &FailoverConfigV2{
				RecoveryStrategy: RecoveryStrategyFailFast,
			},
			expectError: false,
		},
		{
			name: "invalid strategy",
			config: &FailoverConfigV2{
				RecoveryStrategy: RecoveryStrategy("invalid"),
			},
			expectError: true,
			errorMsg:    "invalid recovery_strategy",
		},
		{
			name: "auto_wait with zero timeout",
			config: &FailoverConfigV2{
				RecoveryStrategy:    RecoveryStrategyAutoWait,
				AutoWaitTimeout:     0,
				AutoWaitMaxRequests: 100,
			},
			expectError: true,
			errorMsg:    "auto_wait_timeout must be greater than 0",
		},
		{
			name: "auto_wait with zero max requests",
			config: &FailoverConfigV2{
				RecoveryStrategy:    RecoveryStrategyAutoWait,
				AutoWaitTimeout:     300 * time.Second,
				AutoWaitMaxRequests: 0,
			},
			expectError: true,
			errorMsg:    "auto_wait_max_requests must be greater than 0",
		},
		{
			name: "auto_wait with excessive max requests",
			config: &FailoverConfigV2{
				RecoveryStrategy:    RecoveryStrategyAutoWait,
				AutoWaitTimeout:     300 * time.Second,
				AutoWaitMaxRequests: 20000,
			},
			expectError: true,
			errorMsg:    "auto_wait_max_requests cannot exceed 10000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got '%v'", err)
				}
			}
		})
	}
}

// TestFailoverConfigV2SetDefaults 测试默认值设置
func TestFailoverConfigV2SetDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   *FailoverConfigV2
		expected *FailoverConfigV2
	}{
		{
			name:   "empty config sets all defaults",
			config: &FailoverConfigV2{},
			expected: &FailoverConfigV2{
				DefaultCooldown:     10 * time.Minute,
				RecoveryStrategy:    RecoveryStrategyAutoWait,
				AutoWaitTimeout:     300 * time.Second,
				AutoWaitMaxRequests: 100,
			},
		},
		{
			name: "partial config sets missing defaults",
			config: &FailoverConfigV2{
				RecoveryStrategy: RecoveryStrategyLoopRetry,
			},
			expected: &FailoverConfigV2{
				DefaultCooldown:     10 * time.Minute,
				RecoveryStrategy:    RecoveryStrategyLoopRetry,
				AutoWaitTimeout:     300 * time.Second,
				AutoWaitMaxRequests: 100,
			},
		},
		{
			name: "full config preserves all values",
			config: &FailoverConfigV2{
				DefaultCooldown:     5 * time.Minute,
				RecoveryStrategy:    RecoveryStrategyFailFast,
				AutoWaitTimeout:     600 * time.Second,
				AutoWaitMaxRequests: 200,
			},
			expected: &FailoverConfigV2{
				DefaultCooldown:     5 * time.Minute,
				RecoveryStrategy:    RecoveryStrategyFailFast,
				AutoWaitTimeout:     600 * time.Second,
				AutoWaitMaxRequests: 200,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.SetDefaults()
			if tt.config.DefaultCooldown != tt.expected.DefaultCooldown {
				t.Errorf("DefaultCooldown: expected %v, got %v", tt.expected.DefaultCooldown, tt.config.DefaultCooldown)
			}
			if tt.config.RecoveryStrategy != tt.expected.RecoveryStrategy {
				t.Errorf("RecoveryStrategy: expected %v, got %v", tt.expected.RecoveryStrategy, tt.config.RecoveryStrategy)
			}
			if tt.config.AutoWaitTimeout != tt.expected.AutoWaitTimeout {
				t.Errorf("AutoWaitTimeout: expected %v, got %v", tt.expected.AutoWaitTimeout, tt.config.AutoWaitTimeout)
			}
			if tt.config.AutoWaitMaxRequests != tt.expected.AutoWaitMaxRequests {
				t.Errorf("AutoWaitMaxRequests: expected %v, got %v", tt.expected.AutoWaitMaxRequests, tt.config.AutoWaitMaxRequests)
			}
		})
	}
}

// TestFailoverConfigV2MigrateFromLegacy 测试旧配置迁移
func TestFailoverConfigV2MigrateFromLegacy(t *testing.T) {
	tests := []struct {
		name                string
		oldFailover         *FailoverConfig
		oldSuspend          *RequestSuspendConfig
		expectedStrategy    RecoveryStrategy
		expectedTimeout     time.Duration
		expectedMaxRequests int
	}{
		{
			name: "suspend enabled → auto_wait",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				DefaultCooldown:            10 * time.Minute,
				AllowRevisitAfterExhausted: false,
			},
			oldSuspend: &RequestSuspendConfig{
				Enabled:              true,
				Timeout:              600 * time.Second,
				MaxSuspendedRequests: 200,
			},
			expectedStrategy:    RecoveryStrategyAutoWait,
			expectedTimeout:     600 * time.Second,
			expectedMaxRequests: 200,
		},
		{
			name: "suspend disabled + revisit enabled → loop_retry",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				DefaultCooldown:            10 * time.Minute,
				AllowRevisitAfterExhausted: true,
			},
			oldSuspend: &RequestSuspendConfig{
				Enabled: false,
			},
			expectedStrategy:    RecoveryStrategyLoopRetry,
			expectedTimeout:     0,
			expectedMaxRequests: 0,
		},
		{
			name: "both disabled → fail_fast",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				DefaultCooldown:            10 * time.Minute,
				AllowRevisitAfterExhausted: false,
			},
			oldSuspend: &RequestSuspendConfig{
				Enabled: false,
			},
			expectedStrategy:    RecoveryStrategyFailFast,
			expectedTimeout:     0,
			expectedMaxRequests: 0,
		},
		{
			name: "suspend nil + revisit true → loop_retry",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				DefaultCooldown:            10 * time.Minute,
				AllowRevisitAfterExhausted: true,
			},
			oldSuspend:          nil,
			expectedStrategy:    RecoveryStrategyLoopRetry,
			expectedTimeout:     0,
			expectedMaxRequests: 0,
		},
		{
			name: "suspend nil + revisit false → fail_fast",
			oldFailover: &FailoverConfig{
				Enabled:                    true,
				DefaultCooldown:            10 * time.Minute,
				AllowRevisitAfterExhausted: false,
			},
			oldSuspend:          nil,
			expectedStrategy:    RecoveryStrategyFailFast,
			expectedTimeout:     0,
			expectedMaxRequests: 0,
		},
		{
			name: "suspend enabled with custom values → auto_wait with values",
			oldFailover: &FailoverConfig{
				Enabled:         true,
				DefaultCooldown: 5 * time.Minute,
			},
			oldSuspend: &RequestSuspendConfig{
				Enabled:              true,
				Timeout:              120 * time.Second,
				MaxSuspendedRequests: 50,
			},
			expectedStrategy:    RecoveryStrategyAutoWait,
			expectedTimeout:     120 * time.Second,
			expectedMaxRequests: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &FailoverConfigV2{}
			fc.MigrateFromLegacy(tt.oldFailover, tt.oldSuspend)

			if fc.RecoveryStrategy != tt.expectedStrategy {
				t.Errorf("RecoveryStrategy: expected %v, got %v", tt.expectedStrategy, fc.RecoveryStrategy)
			}
			if fc.AutoWaitTimeout != tt.expectedTimeout {
				t.Errorf("AutoWaitTimeout: expected %v, got %v", tt.expectedTimeout, fc.AutoWaitTimeout)
			}
			if fc.AutoWaitMaxRequests != tt.expectedMaxRequests {
				t.Errorf("AutoWaitMaxRequests: expected %v, got %v", tt.expectedMaxRequests, fc.AutoWaitMaxRequests)
			}
			if fc.Enabled != tt.oldFailover.Enabled {
				t.Errorf("Enabled: expected %v, got %v", tt.oldFailover.Enabled, fc.Enabled)
			}
			if fc.DefaultCooldown != tt.oldFailover.DefaultCooldown {
				t.Errorf("DefaultCooldown: expected %v, got %v", tt.oldFailover.DefaultCooldown, fc.DefaultCooldown)
			}
		})
	}
}

// TestLoadConfigWithV60Format 测试加载 v6.0 格式配置（旧格式）
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
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证自动迁移到 v6.1 格式
	if config.FailoverV2.RecoveryStrategy != RecoveryStrategyAutoWait {
		t.Errorf("Expected RecoveryStrategy=auto_wait after migration, got %v", config.FailoverV2.RecoveryStrategy)
	}
	if config.FailoverV2.AutoWaitTimeout != 300*time.Second {
		t.Errorf("Expected AutoWaitTimeout=300s after migration, got %v", config.FailoverV2.AutoWaitTimeout)
	}
	if config.FailoverV2.AutoWaitMaxRequests != 100 {
		t.Errorf("Expected AutoWaitMaxRequests=100 after migration, got %v", config.FailoverV2.AutoWaitMaxRequests)
	}
	if config.FailoverV2.DefaultCooldown != 10*time.Minute {
		t.Errorf("Expected DefaultCooldown=10m after migration, got %v", config.FailoverV2.DefaultCooldown)
	}
}

// TestLoadConfigWithV61Format 测试加载 v6.1 格式配置（新格式）
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
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证直接加载 v6.1 格式
	if config.FailoverV2.RecoveryStrategy != RecoveryStrategyLoopRetry {
		t.Errorf("Expected RecoveryStrategy=loop_retry, got %v", config.FailoverV2.RecoveryStrategy)
	}
	if config.FailoverV2.DefaultCooldown != 10*time.Minute {
		t.Errorf("Expected DefaultCooldown=10m, got %v", config.FailoverV2.DefaultCooldown)
	}
	// 默认值应被设置
	if config.FailoverV2.AutoWaitTimeout != 300*time.Second {
		t.Errorf("Expected AutoWaitTimeout=300s (default), got %v", config.FailoverV2.AutoWaitTimeout)
	}
	if config.FailoverV2.AutoWaitMaxRequests != 100 {
		t.Errorf("Expected AutoWaitMaxRequests=100 (default), got %v", config.FailoverV2.AutoWaitMaxRequests)
	}
}

// TestLoadConfigWithInvalidStrategy 测试加载无效策略的配置
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
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	if err == nil {
		t.Errorf("Expected error when loading config with invalid strategy, got nil")
	}
	if !contains(err.Error(), "invalid recovery_strategy") {
		t.Errorf("Expected error message to contain 'invalid recovery_strategy', got: %v", err)
	}
}

// 辅助函数：检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
