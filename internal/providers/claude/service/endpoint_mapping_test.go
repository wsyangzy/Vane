package service

import (
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
)

func TestEndpointTimeoutMappingKeepsInheritanceAndExplicitDefaults(t *testing.T) {
	service := &EndpointService{}
	for _, seconds := range []int{0, 300, 45} {
		cfg := config.EndpointConfig{Name: "timeout", Channel: "test", URL: "https://example.invalid", Timeout: time.Duration(seconds) * time.Second}
		record := service.configToRecord(cfg)
		if record.TimeoutSeconds != seconds {
			t.Fatalf("config timeout %d changed to %d", seconds, record.TimeoutSeconds)
		}
		if mapped := service.recordToConfig(record); mapped.Timeout != cfg.Timeout {
			t.Fatalf("record timeout %d mapped to %v", seconds, mapped.Timeout)
		}
	}
}
