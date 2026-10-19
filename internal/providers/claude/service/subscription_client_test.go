package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionClientConfigPreservesUnrelatedFieldsAndSeparatesAuth(t *testing.T) {
	original := []byte(`{"permissions":{"allow":["Read"]},"env":{"KEEP":"value","ANTHROPIC_CUSTOM_HEADERS":"Other: preserve\nx-relay-key: old","ANTHROPIC_API_KEY":"private-api-key","ANTHROPIC_AUTH_TOKEN":"private-token","CLAUDE_CODE_OAUTH_TOKEN":"private-oauth"}}`)
	for _, managed := range []bool{false, true} {
		content, snippet, command, err := RenderSubscriptionClient(original, "http://127.0.0.1:8899", "relay_new", managed)
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Permissions map[string]any
			Env         map[string]string
		}
		if err = json.Unmarshal(content, &config); err != nil {
			t.Fatal(err)
		}
		if config.Env["KEEP"] != "value" || config.Permissions["allow"] == nil {
			t.Fatal("unrelated fields lost")
		}
		if config.Env["ANTHROPIC_CUSTOM_HEADERS"] != "Other: preserve\nX-Relay-Key: relay_new" {
			t.Fatal("headers not merged")
		}
		if strings.Contains(string(content), "private-") || strings.Contains(string(snippet), "preserve") || strings.Contains(string(snippet), "private-") {
			t.Fatal("conflicting auth or preview secret escaped")
		}
		if (config.Env["ANTHROPIC_AUTH_TOKEN"] == "relay-managed") != managed {
			t.Fatal("wrong auth source")
		}
		if !strings.Contains(command, "Remove-Item Env:ANTHROPIC_API_KEY") || !strings.HasSuffix(command, "\nclaude") {
			t.Fatal("launch command incomplete")
		}
	}
	if _, _, _, err := RenderSubscriptionClient([]byte(`{"env":null}`), "url", "key", false); err == nil {
		t.Fatal("invalid env accepted")
	}
}

func TestSubscriptionClientPreservesWindowsEncodingStyle(t *testing.T) {
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("{\r\n  \"permissions\": {}\r\n}\r\n")...)
	content, _, _, err := RenderSubscriptionClient(original, "http://127.0.0.1:8899", "key", false)
	if err != nil || !bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf}) || !bytes.Contains(content, []byte("\r\n")) {
		t.Fatalf("Windows style lost: %v", err)
	}
}
