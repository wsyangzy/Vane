package handlers

import (
	"testing"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/store"
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

func newModelRewriteTestForwarder(mapping map[string]string) (*Forwarder, *endpoint.Endpoint) {
	cfg := &config.Config{}
	manager := endpoint.NewManager(cfg)
	manager.UpdateChannelModelMappings(map[string]map[string]string{"ch-a": mapping})
	ep := &endpoint.Endpoint{Config: config.EndpointConfig{Name: "ep-a", Channel: "ch-a", ModelPolicy: store.ClaudeModelPolicyInherit}}
	return NewForwarder(cfg, manager), ep
}

func TestModelRewriteKeepsEveryByteOutsideModelToken(t *testing.T) {
	forwarder, ep := newModelRewriteTestForwarder(map[string]string{"claude-opus-5": "claude-sonnet-5"})
	// 键序非字母序、HTML 字符、超出 float64 的大数字、嵌套同名 model 都必须原样保留
	body := []byte("{\"stream\":false, \"model\": \"claude-opus-5\",\"system\":\"<b> & \\u003ci\\u003e\"," +
		"\"max_tokens\":123456789012345678901,\"metadata\":{\"model\":\"claude-opus-5\"}}")
	want := "{\"stream\":false, \"model\": \"claude-sonnet-5\",\"system\":\"<b> & \\u003ci\\u003e\"," +
		"\"max_tokens\":123456789012345678901,\"metadata\":{\"model\":\"claude-opus-5\"}}"

	result := forwarder.RewriteModelForEndpoint(body, ep, "req-test")
	if string(result.Body) != want {
		t.Fatalf("unexpected body\n got: %s\nwant: %s", result.Body, want)
	}
	if result.OriginalModel != "claude-opus-5" || result.NewModel != "claude-sonnet-5" || result.Source != "mapping" {
		t.Fatalf("unexpected rewrite metadata: %#v", result)
	}
}

func TestModelRewriteLeavesBodyUntouchedWithoutMatch(t *testing.T) {
	forwarder, ep := newModelRewriteTestForwarder(map[string]string{"claude-opus-5": "claude-sonnet-5"})
	for _, body := range []string{
		`{"model":"claude-haiku-5","messages":[]}`,
		`{"model":123}`,
		`{"messages":[],"metadata":{"model":"claude-opus-5"}}`,
		`not json`,
	} {
		result := forwarder.RewriteModelForEndpoint([]byte(body), ep, "req-test")
		if string(result.Body) != body || result.Source != "" {
			t.Fatalf("body %q must pass through untouched, got %q source=%q", body, result.Body, result.Source)
		}
	}
}
