package handlers

import (
	"testing"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/store"
)

func TestEndpointSupportsClaudeModelUsesMappingAndPolicy(t *testing.T) {
	ep := &endpoint.Endpoint{Config: config.EndpointConfig{
		ModelPolicy:     store.ClaudeModelPolicyOverride,
		ModelMapping:    map[string]string{"claude-small": "claude-haiku"},
		SupportedModels: []string{"claude-haiku"},
	}}
	if !EndpointSupportsClaudeModel(ep, map[string]string{"claude-small": "claude-sonnet"}, "claude-small") {
		t.Fatal("override mapping target should satisfy the group capability")
	}
	ep.Config.ModelPolicy = store.ClaudeModelPolicyPassthrough
	if EndpointSupportsClaudeModel(ep, nil, "claude-small") {
		t.Fatal("passthrough should not match a capability list through a mapping")
	}
	if got := EffectiveClaudeModel(ep, map[string]string{"claude-small": "claude-sonnet"}, "claude-small"); got != "claude-small" {
		t.Fatalf("passthrough should preserve the requested model, got %q", got)
	}
	ep.Config.ModelPolicy = store.ClaudeModelPolicyOverride
	if got := EffectiveClaudeModel(ep, map[string]string{"claude-small": "claude-sonnet"}, "claude-small"); got != "claude-haiku" {
		t.Fatalf("override should use the group mapping, got %q", got)
	}
	ep.Config.ModelPolicy = store.ClaudeModelPolicyInherit
	if got := EffectiveClaudeModel(ep, map[string]string{"claude-small": "claude-sonnet"}, "claude-small"); got != "claude-sonnet" {
		t.Fatalf("inherit should use the channel mapping, got %q", got)
	}
}

func TestEndpointSupportsClaudeModelEmptyListDoesNotFilter(t *testing.T) {
	ep := &endpoint.Endpoint{Config: config.EndpointConfig{
		ModelPolicy:     store.ClaudeModelPolicyInherit,
		SupportedModels: nil,
	}}
	if !EndpointSupportsClaudeModel(ep, nil, "any-model") {
		t.Fatal("an empty capability list should leave the endpoint eligible")
	}
}
