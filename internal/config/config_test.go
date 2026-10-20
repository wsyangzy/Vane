package config

import (
	"log/slog"
	"os"
	"testing"
)

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
