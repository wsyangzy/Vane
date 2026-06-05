package codexproxy

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

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
