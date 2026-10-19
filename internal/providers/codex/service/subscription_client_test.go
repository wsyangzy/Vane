package service

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestSubscriptionClientConfigPreservesModelAndProfileAndAuthSource(t *testing.T) {
	original := []byte("model = 'my-model'\nmodel_reasoning_effort = 'high'\nprofile = 'work'\nforced_login_method = 'api'\n[profiles.work]\nmodel_provider = 'old'\n[mcp_servers.local]\ncommand = 'keep-me'\n[model_providers.other]\nbase_url = 'https://other.example'\nexperimental_bearer_token = 'private-other-token'\n[model_providers.relay_subscription.auth]\ncommand = 'old-auth-helper'\n")
	for _, managed := range []bool{false, true} {
		content, snippet, _, err := RenderSubscriptionClient(original, "http://[::1]:8899/backend-api/codex", "relay_new", managed)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err = toml.Unmarshal(content, &got); err != nil {
			t.Fatal(err)
		}
		if got["model"] != "my-model" || got["model_reasoning_effort"] != "high" || got["mcp_servers"] == nil {
			t.Fatal("client settings lost")
		}
		providers := got["model_providers"].(map[string]any)
		p := providers[SubscriptionClientProvider].(map[string]any)
		if p["requires_openai_auth"] != !managed || p["auth"] != nil || p["supports_websockets"] != false {
			t.Fatal("auth or transport conflict")
		}
		if (p["experimental_bearer_token"] == "relay-managed") != managed {
			t.Fatal("managed placeholder wrong")
		}
		if !strings.Contains(string(content), "private-other-token") || strings.Contains(string(snippet), "private-other-token") {
			t.Fatal("other provider lost or leaked to preview")
		}
		profile := got["profiles"].(map[string]any)["work"].(map[string]any)
		if profile["model_provider"] != SubscriptionClientProvider {
			t.Fatal("active profile still overrides connection")
		}
	}
	if _, _, _, err := RenderSubscriptionClient([]byte("broken = ["), "url", "key", false); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}

func TestSubscriptionClientPreservesWindowsEncodingStyle(t *testing.T) {
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("model = 'my-model'\r\n")...)
	content, _, _, err := RenderSubscriptionClient(original, "http://127.0.0.1:8899/backend-api/codex", "key", false)
	if err != nil || !bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf}) || !bytes.Contains(content, []byte("\r\n")) {
		t.Fatalf("Windows style lost: %v", err)
	}
}
