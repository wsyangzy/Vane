package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestSubscriptionClientConfigPreservesModelAndProfileAndAuthSource(t *testing.T) {
	original := []byte("model = 'my-model'\nmodel_reasoning_effort = 'high'\nprofile = 'work'\nforced_login_method = 'api'\n[profiles.work]\nmodel_provider = 'old'\n[mcp_servers.local]\ncommand = 'keep-me'\n[model_providers.other]\nbase_url = 'https://other.example'\nexperimental_bearer_token = 'private-other-token'\n[model_providers.relay_subscription.auth]\ncommand = 'old-auth-helper'\n")
	for _, managed := range []bool{false, true} {
		content, snippet, _, err := RenderClientConfig(original, "http://[::1]:8899/backend-api/codex", "relay_new", managed)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err = toml.Unmarshal(content, &got); err != nil {
			t.Fatal(err)
		}
		if got["model_provider"] != "OpenAI" || got["model"] != "my-model" || got["model_reasoning_effort"] != "high" || got["mcp_servers"] == nil {
			t.Fatal("client settings lost")
		}
		providers := got["model_providers"].(map[string]any)
		p := providers[ClientProviderID].(map[string]any)
		if p["name"] != "OpenAI" || p["requires_openai_auth"] != !managed || p["auth"] != nil || p["supports_websockets"] != false {
			t.Fatal("auth or transport conflict")
		}
		if (p["experimental_bearer_token"] == "relay_new") != managed {
			t.Fatal("managed application token wrong")
		}
		for _, rendered := range [][]byte{content, snippet} {
			var config map[string]any
			if err := toml.Unmarshal(rendered, &config); err != nil {
				t.Fatal(err)
			}
			headers := config["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)["http_headers"].(map[string]any)
			if managed && headers["X-Relay-Key"] != nil {
				t.Fatal("managed credentials must not duplicate the application bearer token in X-Relay-Key")
			}
			if !managed && headers["X-Relay-Key"] != "relay_new" {
				t.Fatal("official login passthrough needs separate local proxy authentication")
			}
		}
		if !strings.Contains(string(content), "private-other-token") || strings.Contains(string(snippet), "private-other-token") {
			t.Fatal("other provider lost or leaked to preview")
		}
		profile := got["profiles"].(map[string]any)["work"].(map[string]any)
		if profile["model_provider"] != ClientProviderID {
			t.Fatal("active profile still overrides connection")
		}
	}
	if _, _, _, err := RenderClientConfig([]byte("broken = ["), "url", "key", false); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}

func TestSubscriptionClientPreservesWindowsEncodingStyle(t *testing.T) {
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("model = 'my-model'\r\n")...)
	content, _, _, err := RenderClientConfig(original, "http://127.0.0.1:8899/backend-api/codex", "key", false)
	if err != nil || !bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf}) || !bytes.Contains(content, []byte("\r\n")) {
		t.Fatalf("Windows style lost: %v", err)
	}
}

func TestClientConfigRemovesStaleRelayHeaderWhenReturningToManagedCredentials(t *testing.T) {
	content := []byte(`model_provider = "OpenAI"
[model_providers.OpenAI]
requires_openai_auth = false
experimental_bearer_token = "old-local-token"
[model_providers.OpenAI.http_headers]
X-Relay-Key = "old-local-token"
`)
	for _, managed := range []bool{true, false, true} {
		updated, snippet, _, err := RenderClientConfig(content, "http://127.0.0.1:8087/v1", "current-local-token", managed)
		if err != nil {
			t.Fatal(err)
		}
		for _, rendered := range [][]byte{updated, snippet} {
			var config map[string]any
			if err := toml.Unmarshal(rendered, &config); err != nil {
				t.Fatal(err)
			}
			provider := config["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)
			headers := provider["http_headers"].(map[string]any)
			if provider["requires_openai_auth"] != !managed {
				t.Fatal("login requirement disagrees with selected credential source")
			}
			if managed {
				if headers["X-Relay-Key"] != nil || provider["experimental_bearer_token"] != "current-local-token" {
					t.Fatal("managed config retained a redundant relay header or lost its bearer token")
				}
			} else if headers["X-Relay-Key"] != "current-local-token" || provider["experimental_bearer_token"] != nil {
				t.Fatal("official login passthrough has conflicting or missing local authentication")
			}
		}
		content = updated
	}
}

func TestClientConfigMigratesExistingProviderWithoutLosingTransportOptions(t *testing.T) {
	original := []byte(`model_provider = "Custom"
[model_providers.Custom]
request_max_retries = 7
stream_idle_timeout_ms = 123456
env_key = "UPSTREAM_API_KEY"
env_key_instructions = "Upstream credentials"
env_http_headers = { Authorization = "UPSTREAM_HEADER" }
experimental_bearer_token = "old-token"
http_headers = { Authorization = "old-secret" }
[model_providers.Custom.auth]
command = "upstream-helper"
`)
	for _, managed := range []bool{true, false} {
		content, snippet, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/v1", "local-token", managed)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := toml.Unmarshal(content, &config); err != nil {
			t.Fatal(err)
		}
		provider := config["model_providers"].(map[string]any)["OpenAI"].(map[string]any)
		if provider["request_max_retries"] != int64(7) || provider["stream_idle_timeout_ms"] != int64(123456) {
			t.Fatal("existing provider transport options were lost")
		}
		for _, field := range []string{"auth", "env_key", "env_key_instructions", "env_http_headers"} {
			if provider[field] != nil {
				t.Fatalf("upstream authentication field %s survived migration", field)
			}
		}
		if provider["requires_openai_auth"] != !managed || (provider["experimental_bearer_token"] == "local-token") != managed {
			t.Fatal("credential source disagrees with selected routing mode")
		}
		if (provider["http_headers"].(map[string]any)[imageExtensionHeader] == imageExtensionValue) != managed {
			t.Fatal("mode two must add the image extension even if the old provider had no marker")
		}
		if strings.Contains(string(snippet), "old-secret") || strings.Contains(string(snippet), "upstream-helper") {
			t.Fatal("upstream credentials exposed in preview")
		}
	}
}

func TestSubscriptionClientConnectsRemoteCatalogAndKeepsExtensionHeaders(t *testing.T) {
	original := []byte(`model_provider = "vendor"
[model_providers.vendor]
base_url = "https://vendor.invalid/v1"
experimental_bearer_token = "upstream-secret"
http_headers = { "x-openai-actor-authorization" = "local-image-extension", "Authorization" = "private-header", "X-Relay-Key" = "old-local-key", "X-Vendor-Secret" = "hidden-provider-secret" }
[features]
image_generation = true
goals = true
api_key_model_discovery = false
`)
	content, snippet, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/backend-api/codex", "application-token", true)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := toml.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	provider := config["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)
	headers := provider["http_headers"].(map[string]any)
	if provider["requires_openai_auth"] != false || provider["model_catalog_url"] != "http://127.0.0.1:8087/backend-api/codex/models" || headers["x-openai-actor-authorization"] != "local-image-extension" || headers["X-Relay-Key"] != nil || headers["Authorization"] != nil {
		t.Fatalf("wrong local provider: %v", provider)
	}
	features := config["features"].(map[string]any)
	if features["image_generation"] != true || features["goals"] != true || features["api_key_model_discovery"] != true || strings.Contains(string(snippet), "upstream-secret") || strings.Contains(string(snippet), "private-header") || strings.Contains(string(snippet), "hidden-provider-secret") {
		t.Fatal("features lost or upstream credentials exposed in connection preview")
	}
}

func TestSubscriptionClientKeepsAndValidatesLocalCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-models.json")
	raw := []byte(`{"models":[{"slug":"local-model","base_instructions":"Local model instructions"}]}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	original, _ := toml.Marshal(map[string]any{"model_catalog_json": path, "features": map[string]any{"goals": true}})
	content, snippet, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/v1", "application-token", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{content, snippet} {
		var parsed map[string]any
		if err := toml.Unmarshal(data, &parsed); err != nil || parsed["model_catalog_json"] != path {
			t.Fatalf("local catalog preference lost: %s, %v", data, err)
		}
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, raw) {
		t.Fatal("local model catalog was modified")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/v1", "application-token", true); err == nil {
		t.Fatal("missing local catalog accepted before configuration apply")
	}
}

func TestSubscriptionClientLocalCatalogFollowsSelectedProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile-models.json")
	if err := os.WriteFile(path, []byte(`{"models":[{"slug":"profile-model","base_instructions":"Profile instructions"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := toml.Marshal(map[string]any{
		"profile": "work", "model_provider": "inactive", "model_catalog_json": "inactive-missing.json",
		"profiles":        map[string]any{"work": map[string]any{"model_provider": "vendor", "model_catalog_json": path}},
		"model_providers": map[string]any{"vendor": map[string]any{"http_headers": map[string]any{"x-openai-actor-authorization": "local-image-extension"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, snippet, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/v1", "application-token", true)
	if err != nil {
		t.Fatal(err)
	}
	var parsed, preview map[string]any
	if err := toml.Unmarshal(content, &parsed); err != nil {
		t.Fatal(err)
	}
	if err := toml.Unmarshal(snippet, &preview); err != nil {
		t.Fatal(err)
	}
	profile := parsed["profiles"].(map[string]any)["work"].(map[string]any)
	provider := parsed["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)
	if profile["model_provider"] != ClientProviderID || profile["model_catalog_json"] != path || preview["model_catalog_json"] != path || provider["http_headers"].(map[string]any)["x-openai-actor-authorization"] != "local-image-extension" {
		t.Fatal("selected profile did not retain its catalog and image extension")
	}
}

func TestClientCatalogChoiceSwitchesBothWaysWithoutActiveProfileOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex-models.json")
	if err := os.WriteFile(path, []byte(`{"models":[{"slug":"gpt-6.1-sol","base_instructions":"Model instructions"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	original, _ := toml.Marshal(map[string]any{
		"profile": "work", "model_catalog_json": "missing-root.json",
		"profiles": map[string]any{
			"work":  map[string]any{"model": "my-model", "model_catalog_json": "missing-profile.json"},
			"other": map[string]any{"model_catalog_json": "keep-inactive.json"},
		},
	})
	for _, mode := range []string{"local", "remote", "local"} {
		content, snippet, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/v1", "local-token", true, ClientModelCatalog{Mode: mode, Path: path})
		if err != nil {
			t.Fatal(err)
		}
		for _, data := range [][]byte{content, snippet} {
			var parsed map[string]any
			if err := toml.Unmarshal(data, &parsed); err != nil {
				t.Fatal(err)
			}
			provider := parsed["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)
			if mode == "local" {
				if parsed["model_catalog_json"] != path || provider["model_catalog_url"] != nil {
					t.Fatal("local catalog must replace the remote URL")
				}
			} else if parsed["model_catalog_json"] != nil || provider["model_catalog_url"] != "http://127.0.0.1:8087/v1/models" {
				t.Fatal("remote catalog must replace the local catalog")
			}
			if parsed["features"].(map[string]any)["api_key_model_discovery"] != (mode == "remote") {
				t.Fatal("model discovery disagrees with the catalog choice")
			}
		}
		var parsed map[string]any
		_ = toml.Unmarshal(content, &parsed)
		profiles := parsed["profiles"].(map[string]any)
		if profiles["work"].(map[string]any)["model_catalog_json"] != nil || profiles["other"].(map[string]any)["model_catalog_json"] != "keep-inactive.json" {
			t.Fatal("active override survived or an inactive profile was changed")
		}
		choice, err := ClientModelCatalogFromConfig(content)
		if err != nil || choice.Mode != mode || (mode == "local" && choice.Path != path) {
			t.Fatal("status disagrees with generated config", err)
		}
		// Ordinary settings must keep the selected mode and local file.
		original, _, _, err = RenderClientConfig(content, "http://127.0.0.1:19087/v1", "local-token", true)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, choice := range []ClientModelCatalog{{Mode: "other"}, {Mode: "local"}, {Mode: "local", Path: "relative.json"}} {
		if _, _, _, err := RenderClientConfig(nil, "http://127.0.0.1:8087/v1", "local-token", true, choice); err == nil {
			t.Fatal("invalid catalog choice accepted", choice)
		}
	}
}

func TestClientConfigTemplateFillsMissingFieldsAndPreservesPreferences(t *testing.T) {
	for _, original := range []string{"", "model = 'my-model'\nreview_model = 'my-review'\nweb_search = 'disabled'\ndisable_response_storage = false\nnetwork_access = 'restricted'\nwindows_wsl_setup_acknowledged = false\n[features]\nimage_generation = false\ngoals = false\n"} {
		content, snippet, _, err := RenderClientConfig([]byte(original), "http://127.0.0.1:8087/v1", "local-token", true)
		if err != nil {
			t.Fatal(err)
		}
		for _, data := range [][]byte{content, snippet} {
			var parsed map[string]any
			_ = toml.Unmarshal(data, &parsed)
			expectedModel, expectedReview, expectedSearch, expectedNetwork := "gpt-6.1-sol", "gpt-6.1-sol", "live", "enabled"
			defaults := original == ""
			if !defaults {
				expectedModel, expectedReview, expectedSearch, expectedNetwork = "my-model", "my-review", "disabled", "restricted"
			}
			features := parsed["features"].(map[string]any)
			if parsed["model"] != expectedModel || parsed["review_model"] != expectedReview || parsed["web_search"] != expectedSearch || parsed["network_access"] != expectedNetwork || parsed["disable_response_storage"] != defaults || parsed["windows_wsl_setup_acknowledged"] != defaults || features["image_generation"] != defaults || features["goals"] != defaults {
				t.Fatal("template defaults or existing preferences were lost")
			}
		}
	}
}
