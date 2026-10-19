package codexproxy

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/providers/codex/store"
)

func TestRewriteModelForEndpointUsesGroupPolicy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	channel := &store.CodexChannelRecord{Model: "channel-model", ModelMapping: map[string]string{"agent-mini": "channel-mini"}}
	body := []byte(`{"model":"agent-mini","input":[]}`)

	result := rewriteModelForEndpoint(body, &CodexEndpoint{ModelPolicy: store.CodexModelPolicyInherit}, channel, logger, "test")
	if result.NewModel != "channel-mini" || result.Source != "mapping" {
		t.Fatalf("inherit result = %#v", result)
	}
	override := rewriteModelForEndpoint(body, &CodexEndpoint{ModelPolicy: store.CodexModelPolicyOverride, ModelMapping: map[string]string{"agent-mini": "group-mini"}}, channel, logger, "test")
	if override.NewModel != "group-mini" || override.Source != "mapping" {
		t.Fatalf("override result = %#v", override)
	}
	passthrough := rewriteModelForEndpoint(body, &CodexEndpoint{ModelPolicy: store.CodexModelPolicyPassthrough}, channel, logger, "test")
	if string(passthrough.Body) != string(body) || passthrough.Source != "" {
		t.Fatalf("passthrough result = %#v", passthrough)
	}
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
}

func TestRewriteModelIfNeeded_EmptyChannelModel(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[]}`)
	got := rewriteModelIfNeeded(body, "", newTestLogger(), "req-test")
	if !bytes.Equal(got, body) {
		t.Errorf("expected body unchanged when channelModel is empty, got %s", string(got))
	}
}

func TestRewriteModelIfNeeded_NonJSONBody(t *testing.T) {
	body := []byte("not a json")
	got := rewriteModelIfNeeded(body, "gpt-5-codex", newTestLogger(), "req-test")
	if !bytes.Equal(got, body) {
		t.Errorf("expected body unchanged for non-JSON, got %s", string(got))
	}
}

func TestRewriteModelIfNeeded_NoModelField(t *testing.T) {
	body := []byte(`{"input":[],"reasoning":{"effort":"high"}}`)
	got := rewriteModelIfNeeded(body, "gpt-5-codex", newTestLogger(), "req-test")
	if !bytes.Equal(got, body) {
		t.Errorf("expected body unchanged when no model field, got %s", string(got))
	}
}

func TestRewriteModelIfNeeded_NormalRewrite(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[]}`)
	got := rewriteModelIfNeeded(body, "gpt-5-codex", newTestLogger(), "req-test")
	if bytes.Equal(got, body) {
		t.Fatalf("expected body changed, got unchanged: %s", string(got))
	}
	if !bytes.Contains(got, []byte(`"model":"gpt-5-codex"`)) {
		t.Errorf("expected new body contains gpt-5-codex, got %s", string(got))
	}
	if bytes.Contains(got, []byte(`"gpt-5"`)) && !bytes.Contains(got, []byte(`"gpt-5-codex"`)) {
		t.Errorf("expected old model removed, got %s", string(got))
	}
}

func TestRewriteModelWithMappingIfNeeded_MappingPriority(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4-mini","input":[]}`)
	got := rewriteModelWithMappingIfNeeded(
		body,
		map[string]string{"gpt-5.4-mini": "gpt-5-mini"},
		"gpt-5-codex",
		newTestLogger(),
		"req-test",
	)
	if !bytes.Contains(got, []byte(`"model":"gpt-5-mini"`)) {
		t.Errorf("expected mapped model gpt-5-mini, got %s", string(got))
	}
	if bytes.Contains(got, []byte(`"model":"gpt-5-codex"`)) {
		t.Errorf("expected mapping to take priority over channelModel, got %s", string(got))
	}
}

func TestRewriteModelWithMappingIfNeeded_MappingMissWithoutFallback(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[]}`)
	got := rewriteModelWithMappingIfNeeded(
		body,
		map[string]string{"gpt-5.4-mini": "gpt-5-mini"},
		"",
		newTestLogger(),
		"req-test",
	)
	if !bytes.Equal(got, body) {
		t.Errorf("expected body unchanged when mapping misses and channelModel is empty, got %s", string(got))
	}
}

func TestRewriteModelWithMappingIfNeeded_MappingMissFallsBackToChannelModel(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[]}`)
	got := rewriteModelWithMappingIfNeeded(
		body,
		map[string]string{"gpt-5.4-mini": "gpt-5-mini"},
		"gpt-5-codex",
		newTestLogger(),
		"req-test",
	)
	if !bytes.Contains(got, []byte(`"model":"gpt-5-codex"`)) {
		t.Errorf("expected fallback channel model, got %s", string(got))
	}
}

func TestRewriteModelWithMappingIfNeeded_MappingTargetAlreadyEqual(t *testing.T) {
	body := []byte(`{"model":"gpt-5-mini","input":[]}`)
	got := rewriteModelWithMappingIfNeeded(
		body,
		map[string]string{"gpt-5-mini": "gpt-5-mini"},
		"",
		newTestLogger(),
		"req-test",
	)
	if !bytes.Equal(got, body) {
		t.Errorf("expected body unchanged when mapped target equals original model, got %s", string(got))
	}
}

func TestRewriteModelIfNeeded_AlreadyEqual(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","input":[]}`)
	got := rewriteModelIfNeeded(body, "gpt-5-codex", newTestLogger(), "req-test")
	if !bytes.Equal(got, body) {
		t.Errorf("expected body unchanged when model already equals channelModel, got %s", string(got))
	}
}

func TestRewriteModelIfNeeded_PreserveIntegerPrecision(t *testing.T) {
	body := []byte(`{"model":"gpt-5","max_tokens":1024,"temperature":0.7}`)
	got := rewriteModelIfNeeded(body, "gpt-5-codex", newTestLogger(), "req-test")
	if !bytes.Contains(got, []byte(`"max_tokens":1024`)) {
		t.Errorf("expected max_tokens preserved as 1024, got %s", string(got))
	}
	if !bytes.Contains(got, []byte(`"temperature":0.7`)) {
		t.Errorf("expected temperature preserved as 0.7, got %s", string(got))
	}
}

func TestRewriteModelIfNeeded_PreserveNestedObjects(t *testing.T) {
	body := []byte(`{"model":"gpt-5","reasoning":{"effort":"high"},"input":[{"type":"message","role":"user"}]}`)
	got := rewriteModelIfNeeded(body, "gpt-5-codex", newTestLogger(), "req-test")
	if !bytes.Contains(got, []byte(`"reasoning":{"effort":"high"}`)) {
		t.Errorf("expected reasoning object preserved, got %s", string(got))
	}
	if !bytes.Contains(got, []byte(`"input":[{"type":"message","role":"user"}]`)) {
		t.Errorf("expected input array preserved, got %s", string(got))
	}
}

func TestRewriteModelKeepsEveryByteOutsideModelToken(t *testing.T) {
	// 旧实现走 map 往返：键序被字母序重排、< > & 被转成 \u003c 形式。现在只替换 model 区间。
	body := []byte("{\"stream\":true, \"model\" : \"gpt-5\",\"instructions\":\"<tag> & \\u003e\"," +
		"\"input\":[{\"type\":\"message\",\"model\":\"gpt-5\"}],\"max_output_tokens\":123456789012345678901}")
	want := "{\"stream\":true, \"model\" : \"gpt-5-codex\",\"instructions\":\"<tag> & \\u003e\"," +
		"\"input\":[{\"type\":\"message\",\"model\":\"gpt-5\"}],\"max_output_tokens\":123456789012345678901}"

	got := rewriteModelWithMappingDetails(body, map[string]string{"gpt-5": "gpt-5-codex"}, "fallback", newTestLogger(), "req-test")
	if string(got.Body) != want {
		t.Fatalf("unexpected body\n got: %s\nwant: %s", got.Body, want)
	}
	if got.OriginalModel != "gpt-5" || got.NewModel != "gpt-5-codex" || got.Source != "mapping" {
		t.Fatalf("unexpected rewrite metadata: %#v", got)
	}
}

func TestRewriteModelIfNeeded_NonStringModelPassesThrough(t *testing.T) {
	body := []byte(`{"model":{"name":"gpt-5"},"input":[]}`)
	got := rewriteModelIfNeeded(body, "gpt-5-codex", newTestLogger(), "req-test")
	if !bytes.Equal(got, body) {
		t.Errorf("expected body unchanged when model is not a string, got %s", string(got))
	}
}
