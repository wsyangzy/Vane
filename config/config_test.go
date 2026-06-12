package config

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestDynamicTokenResolution(t *testing.T) {
	configContent := `
server:
  host: "localhost"
  port: 8080

strategy:
  type: "priority"

endpoints:
  # Group 1: main - first endpoint defines the group token
  - name: "main-primary"
    url: "https://api1.example.com"
    group: "main"
    group-priority: 1
    priority: 1
    token: "sk-main-token"
    timeout: "30s"

  - name: "main-backup"
    url: "https://api2.example.com"
    priority: 2
    # No token defined - should dynamically resolve to "sk-main-token" at runtime

  # Group 2: backup - first endpoint defines the group token  
  - name: "backup-primary"
    url: "https://api3.example.com"
    group: "backup"
    group-priority: 2
    priority: 1
    token: "sk-backup-token"
    timeout: "45s"

  - name: "backup-secondary"
    url: "https://api4.example.com"
    priority: 2
    # No token defined - should dynamically resolve to "sk-backup-token" at runtime

  # Group 3: with explicit token override
  - name: "override-primary"
    url: "https://api5.example.com"
    group: "override"
    group-priority: 3
    priority: 1
    token: "sk-group-token"

  - name: "override-custom"
    url: "https://api6.example.com"
    priority: 2
    token: "sk-custom-token"
    # Has its own token - should use its own, not inherit from group
`

	tmpFile, err := os.CreateTemp("", "test-dynamic-config-*.yaml")
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

	// Verify that tokens are NOT inherited during config parsing (dynamic resolution)
	// Group 1: main
	mainPrimary := config.Endpoints[0]
	if mainPrimary.Token != "sk-main-token" {
		t.Errorf("main-primary: expected own token 'sk-main-token', got '%s'", mainPrimary.Token)
	}

	mainBackup := config.Endpoints[1]
	if mainBackup.Token != "" {
		t.Errorf("main-backup: expected no static token (for dynamic resolution), got '%s'", mainBackup.Token)
	}

	// Group 2: backup
	backupPrimary := config.Endpoints[2]
	if backupPrimary.Token != "sk-backup-token" {
		t.Errorf("backup-primary: expected own token 'sk-backup-token', got '%s'", backupPrimary.Token)
	}

	backupSecondary := config.Endpoints[3]
	if backupSecondary.Token != "" {
		t.Errorf("backup-secondary: expected no static token (for dynamic resolution), got '%s'", backupSecondary.Token)
	}

	// Group 3: override scenarios
	overridePrimary := config.Endpoints[4]
	if overridePrimary.Token != "sk-group-token" {
		t.Errorf("override-primary: expected own token 'sk-group-token', got '%s'", overridePrimary.Token)
	}

	overrideCustom := config.Endpoints[5]
	if overrideCustom.Token != "sk-custom-token" {
		t.Errorf("override-custom: expected own token 'sk-custom-token', got '%s'", overrideCustom.Token)
	}

	// Verify group assignments are still working
	if mainBackup.Group != "main" {
		t.Errorf("main-backup: expected group 'main', got '%s'", mainBackup.Group)
	}
	if backupSecondary.Group != "backup" {
		t.Errorf("backup-secondary: expected group 'backup', got '%s'", backupSecondary.Group)
	}
	if overrideCustom.Group != "override" {
		t.Errorf("override-custom: expected group 'override', got '%s'", overrideCustom.Group)
	}
}

func TestFullParameterInheritance(t *testing.T) {
	configContent := `
server:
  host: "localhost"
  port: 8080

strategy:
  type: "priority"

endpoints:
  - name: "primary"
    url: "https://api1.example.com"
    priority: 1
    timeout: "45s"
    token: "primary-token"
    headers:
      X-API-Version: "v1"
      User-Agent: "Claude-Forwarder/1.0"
      Authorization-Fallback: "Bearer fallback"

  - name: "inherits-all"
    url: "https://api2.example.com"
    priority: 2
    # Should inherit timeout and all headers, but NOT token (dynamic resolution)

  - name: "partial-override" 
    url: "https://api3.example.com"
    priority: 3
    timeout: "60s"
    headers:
      X-API-Version: "v2"
      X-Custom: "custom-value"

  - name: "full-custom"
    url: "https://api4.example.com"
    priority: 4
    timeout: "30s"
    token: "custom-token"
    headers:
      X-Different: "different-value"
`

	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
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

	// Test full inheritance endpoint (timeout and headers, but not token)
	inheritsAll := config.Endpoints[1]
	if inheritsAll.Timeout != 45*time.Second {
		t.Errorf("Expected inherited timeout 45s, got %v", inheritsAll.Timeout)
	}
	// Token should NOT be inherited in the new dynamic system
	if inheritsAll.Token != "" {
		t.Errorf("Expected no static token (dynamic resolution), got '%s'", inheritsAll.Token)
	}
	if len(inheritsAll.Headers) != 3 {
		t.Errorf("Expected 3 inherited headers, got %d", len(inheritsAll.Headers))
	}
	if inheritsAll.Headers["X-API-Version"] != "v1" {
		t.Errorf("Expected inherited header X-API-Version=v1, got '%s'", inheritsAll.Headers["X-API-Version"])
	}

	// Test partial override endpoint (timeout and headers, but not token)
	partialOverride := config.Endpoints[2]
	if partialOverride.Timeout != 60*time.Second {
		t.Errorf("Expected override timeout 60s, got %v", partialOverride.Timeout)
	}
	// Token should NOT be inherited in the new dynamic system
	if partialOverride.Token != "" {
		t.Errorf("Expected no static token (dynamic resolution), got '%s'", partialOverride.Token)
	}
	if partialOverride.Headers["X-API-Version"] != "v2" {
		t.Errorf("Expected override header X-API-Version=v2, got '%s'", partialOverride.Headers["X-API-Version"])
	}
	if partialOverride.Headers["User-Agent"] != "Claude-Forwarder/1.0" {
		t.Errorf("Expected inherited User-Agent, got '%s'", partialOverride.Headers["User-Agent"])
	}
	if partialOverride.Headers["X-Custom"] != "custom-value" {
		t.Errorf("Expected new header X-Custom=custom-value, got '%s'", partialOverride.Headers["X-Custom"])
	}

	// Test full custom with merging
	fullCustom := config.Endpoints[3]
	if fullCustom.Timeout != 30*time.Second {
		t.Errorf("Expected custom timeout 30s, got %v", fullCustom.Timeout)
	}
	if fullCustom.Token != "custom-token" {
		t.Errorf("Expected custom token 'custom-token', got '%s'", fullCustom.Token)
	}
	if fullCustom.Headers["X-Different"] != "different-value" {
		t.Errorf("Expected custom header X-Different=different-value, got '%s'", fullCustom.Headers["X-Different"])
	}
	if fullCustom.Headers["User-Agent"] != "Claude-Forwarder/1.0" {
		t.Errorf("Expected inherited User-Agent, got '%s'", fullCustom.Headers["User-Agent"])
	}
}

func TestLoadConfigAppliesCoreDefaults(t *testing.T) {
	configContent := `
endpoints:
  - name: "primary"
    url: "https://api1.example.com"
    priority: 1
    headers:
      X-Base: "base"
  - name: "secondary"
    url: "https://api2.example.com"
    headers:
      X-Base: "override"
      X-Extra: "extra"
`

	tmpFile, err := os.CreateTemp("", "test-defaults-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp config: %v", err)
	}

	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if config.Server.Host != "localhost" {
		t.Fatalf("Server.Host = %q, want localhost", config.Server.Host)
	}
	if config.Server.Port != 8080 {
		t.Fatalf("Server.Port = %d, want 8080", config.Server.Port)
	}
	if config.Strategy.Type != "priority" {
		t.Fatalf("Strategy.Type = %q, want priority", config.Strategy.Type)
	}
	if config.Retry.MaxAttempts != 3 {
		t.Fatalf("Retry.MaxAttempts = %d, want 3", config.Retry.MaxAttempts)
	}
	if config.Retry.BaseDelay != time.Second {
		t.Fatalf("Retry.BaseDelay = %v, want 1s", config.Retry.BaseDelay)
	}
	if config.Health.HealthPath != "/v1/models" {
		t.Fatalf("Health.HealthPath = %q, want /v1/models", config.Health.HealthPath)
	}
	if config.Streaming.ResponseHeaderTimeout != 120*time.Second {
		t.Fatalf("Streaming.ResponseHeaderTimeout = %v, want 120s", config.Streaming.ResponseHeaderTimeout)
	}
	if config.Streaming.DefaultResponseHeaderTimeout != 60*time.Second {
		t.Fatalf("Streaming.DefaultResponseHeaderTimeout = %v, want 60s", config.Streaming.DefaultResponseHeaderTimeout)
	}
	if config.GlobalTimeout != 300*time.Second {
		t.Fatalf("GlobalTimeout = %v, want 300s", config.GlobalTimeout)
	}
	if config.Timezone != "Asia/Shanghai" {
		t.Fatalf("Timezone = %q, want Asia/Shanghai", config.Timezone)
	}
	if config.EndpointsStorage.Type != "yaml" {
		t.Fatalf("EndpointsStorage.Type = %q, want yaml", config.EndpointsStorage.Type)
	}
	if !config.CodexServer.Enabled {
		t.Fatal("CodexServer.Enabled = false, want true")
	}
	if config.Endpoints[1].Group != "Default" {
		t.Fatalf("secondary group = %q, want Default", config.Endpoints[1].Group)
	}
	if config.Endpoints[1].Timeout != config.GlobalTimeout {
		t.Fatalf("secondary timeout = %v, want global timeout %v", config.Endpoints[1].Timeout, config.GlobalTimeout)
	}
	if got := config.Endpoints[1].Headers["X-Base"]; got != "override" {
		t.Fatalf("secondary X-Base = %q, want override", got)
	}
	if got := config.Endpoints[1].Headers["X-Extra"]; got != "extra" {
		t.Fatalf("secondary X-Extra = %q, want extra", got)
	}
}

func TestNoTokenInheritance(t *testing.T) {
	// Test case where first endpoint has no token
	configContent := `
server:
  host: "localhost"
  port: 8080

strategy:
  type: "priority"

endpoints:
  - name: "first"
    url: "https://api1.example.com"
    priority: 1
    # No token specified

  - name: "second"
    url: "https://api2.example.com"
    priority: 2
    # No token specified

  - name: "third"
    url: "https://api3.example.com"
    priority: 3
    token: "third-token"
`

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	// Load configuration
	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify no token inheritance when first endpoint has no token
	if config.Endpoints[0].Token != "" {
		t.Errorf("First endpoint token: expected empty, got '%s'", config.Endpoints[0].Token)
	}

	if config.Endpoints[1].Token != "" {
		t.Errorf("Second endpoint token: expected empty, got '%s'", config.Endpoints[1].Token)
	}

	if config.Endpoints[2].Token != "third-token" {
		t.Errorf("Third endpoint token: expected 'third-token', got '%s'", config.Endpoints[2].Token)
	}
}

func TestApplyPrimaryEndpoint(t *testing.T) {
	tests := []struct {
		name             string
		config           *Config
		primaryEndpoint  string
		expectError      bool
		expectedPriority int
		expectedAdjusted int
	}{
		{
			name: "Valid endpoint name",
			config: &Config{
				Endpoints: []EndpointConfig{
					{Name: "endpoint1", Priority: 5},
					{Name: "endpoint2", Priority: 1},
					{Name: "endpoint3", Priority: 3},
				},
			},
			primaryEndpoint:  "endpoint3",
			expectError:      false,
			expectedPriority: 1,
			expectedAdjusted: 1, // only endpoint2 has priority <= 1
		},
		{
			name: "Invalid endpoint name",
			config: &Config{
				Endpoints: []EndpointConfig{
					{Name: "endpoint1", Priority: 1},
					{Name: "endpoint2", Priority: 2},
				},
			},
			primaryEndpoint: "nonexistent",
			expectError:     true,
		},
		{
			name: "Empty primary endpoint",
			config: &Config{
				Endpoints: []EndpointConfig{
					{Name: "endpoint1", Priority: 1},
				},
			},
			primaryEndpoint: "",
			expectError:     false,
		},
		{
			name: "Multiple endpoints need adjustment",
			config: &Config{
				Endpoints: []EndpointConfig{
					{Name: "endpoint1", Priority: 1},
					{Name: "endpoint2", Priority: 0},
					{Name: "endpoint3", Priority: -1},
					{Name: "endpoint4", Priority: 5},
				},
			},
			primaryEndpoint:  "endpoint4",
			expectError:      false,
			expectedPriority: 1,
			expectedAdjusted: 3, // endpoints 1, 2, 3 all have priority <= 1
		},
		{
			name: "No adjustments needed",
			config: &Config{
				Endpoints: []EndpointConfig{
					{Name: "endpoint1", Priority: 5},
					{Name: "endpoint2", Priority: 3},
					{Name: "endpoint3", Priority: 2},
				},
			},
			primaryEndpoint:  "endpoint2",
			expectError:      false,
			expectedPriority: 1,
			expectedAdjusted: 0, // no endpoints have priority <= 1 except the primary
		},
	}

	// Create a null logger for testing
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the primary endpoint
			tt.config.PrimaryEndpoint = tt.primaryEndpoint

			// Apply the primary endpoint
			err := tt.config.ApplyPrimaryEndpoint(logger)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected an error, but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, but got: %v", err)
			}

			// If no error expected, check the results
			if !tt.expectError && tt.primaryEndpoint != "" {
				// Find the primary endpoint and check its priority
				primaryIndex := tt.config.findEndpointIndex(tt.primaryEndpoint)
				if primaryIndex != -1 {
					if tt.config.Endpoints[primaryIndex].Priority != tt.expectedPriority {
						t.Errorf("Expected primary endpoint priority %d, got %d",
							tt.expectedPriority, tt.config.Endpoints[primaryIndex].Priority)
					}
				}

				// Count endpoints that were adjusted (now have priority > original priority)
				adjustedCount := 0
				for i := range tt.config.Endpoints {
					if i != primaryIndex {
						// Check if this endpoint was adjusted (has priority > 1)
						switch tt.name {
						case "Valid endpoint name":
							if i == 1 && tt.config.Endpoints[i].Priority == 3 { // endpoint2: 1 + 2 = 3
								adjustedCount++
							}
						case "Multiple endpoints need adjustment":
							if (i == 0 && tt.config.Endpoints[i].Priority == 3) || // endpoint1: 1 + 2 = 3
								(i == 1 && tt.config.Endpoints[i].Priority == 2) || // endpoint2: 0 + 2 = 2
								(i == 2 && tt.config.Endpoints[i].Priority == 1) { // endpoint3: -1 + 2 = 1
								adjustedCount++
							}
						}
					}
				}

				if adjustedCount != tt.expectedAdjusted {
					t.Errorf("Expected %d endpoints to be adjusted, got %d",
						tt.expectedAdjusted, adjustedCount)
				}
			}
		})
	}
}

func TestFindEndpointIndex(t *testing.T) {
	config := &Config{
		Endpoints: []EndpointConfig{
			{Name: "endpoint1"},
			{Name: "endpoint2"},
			{Name: "endpoint3"},
		},
	}

	tests := []struct {
		name     string
		endpoint string
		expected int
	}{
		{"First endpoint", "endpoint1", 0},
		{"Middle endpoint", "endpoint2", 1},
		{"Last endpoint", "endpoint3", 2},
		{"Non-existent endpoint", "nonexistent", -1},
		{"Empty string", "", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.findEndpointIndex(tt.endpoint)
			if result != tt.expected {
				t.Errorf("Expected index %d, got %d", tt.expected, result)
			}
		})
	}
}
