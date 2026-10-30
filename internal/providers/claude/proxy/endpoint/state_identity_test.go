package endpoint

import (
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
)

func TestClaudeSyncPreservesOnlyUnchangedBackendIdentity(t *testing.T) {
	for _, change := range []string{"unchanged", "policy", "url", "credential", "group", "auth", "headers"} {
		t.Run(change, func(t *testing.T) {
			cfg := config.EndpointConfig{Name: "same", Channel: "same", CredentialGroupID: 3, URL: "https://upstream.invalid", ApiKey: "key"}
			m := NewManager(&config.Config{Endpoints: []config.EndpointConfig{cfg}})
			version := m.GetAllEndpoints()[0].RouteStateVersion()
			switch change {
			case "policy":
				cfg.Priority, cfg.Timeout, cfg.DailyCostLimitUSD = 99, time.Minute, 20
			case "url":
				cfg.URL = "https://another.invalid"
			case "credential":
				cfg.ApiKey = "other-key"
			case "group":
				cfg.CredentialGroupID++
			case "auth":
				cfg.AuthMode = "client_passthrough"
			case "headers":
				cfg.Headers = map[string]string{"X-Custom-Auth": "new"}
			}
			m.SyncEndpoints([]config.EndpointConfig{cfg})
			got := m.GetAllEndpoints()[0].RouteStateVersion()
			if (got == version) != (change == "unchanged" || change == "policy") {
				t.Fatalf("backend version inheritance incorrect: change=%s", change)
			}
		})
	}
}
