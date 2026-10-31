package app

import (
	"testing"

	"github.com/wsyangzy/Vane/internal/config"
	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	codexproxy "github.com/wsyangzy/Vane/internal/providers/codex/proxy"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

func healthScopeRuntime(provider, endpointID string) *routing.PoolRuntime {
	pool := routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
	pool.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{
		Provider:   provider,
		Profiles:   []routing.RoutingProfile{{Provider: provider, Enabled: true, IsDefault: true, Members: []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}}}},
		Candidates: []routing.DispatchCandidate{{Provider: provider, ChannelID: 1, GroupID: 11, EndpointID: endpointID, Enabled: true}},
	}))
	return pool
}

func TestClaudeHealthProjectionHidesExcludedProbeState(t *testing.T) {
	manager := claudeendpoint.NewManager(&config.Config{Endpoints: []config.EndpointConfig{
		{Name: "selected", Channel: "A"}, {Name: "outside", Channel: "B", SkipHealthCheck: true},
	}})
	manager.SetPoolRuntime(healthScopeRuntime("claude", claudeendpoint.EndpointKey("A", "selected")))
	info := EndpointRecordInfo{Healthy: true, SkipHealthCheck: true, HealthState: "skipped", LastCheck: "old", ResponseTimeMs: 268, LastFailCode: 503, LastFailReason: "old"}
	applyClaudeHealthScope(&info, manager, claudeendpoint.EndpointKey("B", "outside"))
	if info.HealthState != "disabled" || info.Healthy || !info.NeverChecked || info.ResponseTimeMs != 0 || info.LastCheck != "" || info.LastFailCode != 0 || info.LastFailReason != "" {
		t.Fatalf("excluded endpoint retained old health state: %+v", info)
	}
	if !info.SkipHealthCheck {
		t.Fatal("projection changed stored health preference")
	}
	selected := EndpointRecordInfo{Healthy: true, HealthState: "healthy", ResponseTimeMs: 268}
	applyClaudeHealthScope(&selected, manager, claudeendpoint.EndpointKey("A", "selected"))
	if selected.HealthState != "healthy" || selected.ResponseTimeMs != 268 {
		t.Fatal("selected endpoint lost health state")
	}
	app := &App{endpointManager: manager}
	for _, endpoint := range app.GetEndpoints() {
		if endpoint.Name == "outside" && (endpoint.HealthState != "disabled" || endpoint.ResponseTimeMs != 0 || endpoint.LastCheck != "") {
			t.Fatalf("event endpoint projection retained old state: %+v", endpoint)
		}
	}
}

func TestCodexHealthProjectionUsesDefaultProfile(t *testing.T) {
	manager := codexproxy.NewEndpointManager(nil, nil, nil, nil)
	manager.SetPoolRuntime(healthScopeRuntime("codex", "1"))
	// Legacy activation and failover flags cannot exclude a default member.
	channel := &codexstore.CodexChannelRecord{ID: 1}
	selected := &codexstore.CodexEndpointRecord{ID: 1}
	healthy, state, inScope, latency := codexEndpointRuntimeHealth(channel, selected, manager, 999, false, map[int64]int64{1: 268})
	if !healthy || state != "healthy" || !inScope || latency != 268 {
		t.Fatalf("default member projection = %v, %s, %v, %d", healthy, state, inScope, latency)
	}
	outside := &codexstore.CodexEndpointRecord{ID: 2, FailoverEnabled: true, SkipHealthCheck: true}
	healthy, state, inScope, latency = codexEndpointRuntimeHealth(channel, outside, manager, 1, true, map[int64]int64{2: 268})
	if healthy || state != "disabled" || inScope || latency != 0 {
		t.Fatalf("excluded member projection = %v, %s, %v, %d", healthy, state, inScope, latency)
	}
	manager.SetConfig(&config.Config{Health: config.HealthConfig{PauseActiveProbes: true}})
	healthy, state, inScope, latency = codexEndpointRuntimeHealth(channel, selected, manager, 1, true, map[int64]int64{1: 268})
	if healthy || state != "disabled" || inScope || latency != 0 {
		t.Fatal("global switch left stale Codex health or latency visible")
	}
}

func TestGlobalSwitchHidesClaudeHealthWithAndWithoutProfile(t *testing.T) {
	for _, withProfile := range []bool{false, true} {
		cfg := &config.Config{Health: config.HealthConfig{PauseActiveProbes: true}, Endpoints: []config.EndpointConfig{{Name: "selected", Channel: "A"}}}
		manager := claudeendpoint.NewManager(cfg)
		key := claudeendpoint.EndpointKey("A", "selected")
		if withProfile {
			manager.SetPoolRuntime(healthScopeRuntime("claude", key))
		}
		info := EndpointRecordInfo{Healthy: true, HealthState: "healthy", LastCheck: "old", ResponseTimeMs: 268}
		applyClaudeHealthScope(&info, manager, key)
		if info.HealthState != "disabled" || info.Healthy || !info.NeverChecked || info.ResponseTimeMs != 0 || info.LastCheck != "" {
			t.Fatalf("global switch retained old Claude health state: %+v", info)
		}
		app := &App{endpointManager: manager}
		for _, endpoint := range app.GetEndpoints() {
			if endpoint.HealthState != "disabled" || endpoint.ResponseTimeMs != 0 || endpoint.LastCheck != "" {
				t.Fatalf("event projection retained old state: %+v", endpoint)
			}
		}
	}
}
