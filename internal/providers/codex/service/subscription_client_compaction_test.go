package service

import (
	"bytes"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestClientConfigCompactionPreservedAcrossOrdinarySaves(t *testing.T) {
	original := []byte("model_provider = 'old'\n[model_providers.old.capabilities]\nremote_compaction = 'unsupported'\nexternal_web_access = false\n")
	for i := 0; i < 2; i++ {
		content, snippet, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/v1", "local-test", true)
		if err != nil {
			t.Fatal(err)
		}
		for _, rendered := range [][]byte{content, snippet} {
			var config map[string]any
			if err := toml.Unmarshal(rendered, &config); err != nil {
				t.Fatal(err)
			}
			provider := config["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)
			capabilities := provider["capabilities"].(map[string]any)
			if capabilities["remote_compaction"] != "unsupported" || capabilities["external_web_access"] != false {
				t.Fatal("ordinary client save lost the configured fallback or another provider capability")
			}
		}
		original = content
	}
}

func TestClientConfigExplicitCompactionChoicePreservesClientPreferences(t *testing.T) {
	original := []byte("model_auto_compact_token_limit = 272000\nmodel_context_window = 300000\n[model_providers.OpenAI.capabilities]\nremote_compaction = 'v2'\nexternal_web_access = false\n")
	for mode, want := range map[string]string{"native": "v2", "summary": "unsupported", "preserve": "v2", "": "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			content, snippet, _, err := RenderClientConfigWithOptions(original, "http://127.0.0.1:8087/v1", "local-test", true, ClientConfigOptions{CompactionMode: mode})
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := toml.Unmarshal(content, &config); err != nil {
				t.Fatal(err)
			}
			capabilities := config["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)["capabilities"].(map[string]any)
			if capabilities["remote_compaction"] != want || capabilities["external_web_access"] != false || config["model_auto_compact_token_limit"] != int64(272000) || config["model_context_window"] != int64(300000) {
				t.Fatal("explicit mode changed unrelated client preferences")
			}
			actual, err := ClientCompactionFromConfig(snippet)
			if err != nil || actual.RemoteCompaction != want {
				t.Fatal("preview capability disagrees with generated config", err)
			}
		})
	}
	before := append([]byte(nil), original...)
	content, snippet, command, err := RenderClientConfigWithOptions(original, "http://127.0.0.1:8087/v1", "local-test", true, ClientConfigOptions{CompactionMode: "invalid"})
	if err == nil || len(content) != 0 || len(snippet) != 0 || command != "" || !bytes.Equal(original, before) {
		t.Fatal("invalid mode must fail without generating a replacement")
	}
}

func TestClientConfigCompactionIgnoresLegacyProfileAndPreservesOtherCapabilities(t *testing.T) {
	original := []byte("profile = 'work'\nmodel_provider = 'current'\n[profiles.work]\nmodel_provider = 'gateway'\n[model_providers.current.capabilities]\nremote_compaction = 'v2'\nexternal_web_access = true\nfuture_capability = false\n[model_providers.gateway.capabilities]\nremote_compaction = 'unsupported'\nexternal_web_access = false\n")
	for _, managed := range []bool{false, true} {
		content, _, _, err := RenderClientConfig(original, "http://127.0.0.1:8087/v1", "local-test", managed)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := toml.Unmarshal(content, &config); err != nil {
			t.Fatal(err)
		}
		capabilities := config["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)["capabilities"].(map[string]any)
		if capabilities["remote_compaction"] != "v2" || capabilities["external_web_access"] != true || capabilities["future_capability"] != false {
			t.Fatal("legacy profile changed compaction or current provider capabilities were lost")
		}
	}
	for _, mode := range []string{"preserve", "native", "summary"} {
		content, _, _, err := RenderClientConfigWithOptions([]byte("model_provider = 'OpenAI'\n[model_providers.OpenAI]\ncapabilities = 'invalid'\n"), "url", "key", true, ClientConfigOptions{CompactionMode: mode})
		if err == nil || len(content) != 0 {
			t.Fatal("malformed capabilities must not be silently overwritten")
		}
	}
}

func TestClientCompactionFromConfigReportsConfiguredCapability(t *testing.T) {
	for _, tc := range []struct {
		name, original, mode, remote string
	}{
		{"empty", "", "preserve", ""},
		{"no_capability", "model_provider = 'OpenAI'\n[model_providers.OpenAI]\nbase_url = 'http://localhost/v1'\n", "preserve", ""},
		{"summary", "model_provider = 'OpenAI'\n[model_providers.OpenAI.capabilities]\nremote_compaction = 'unsupported'\n", "summary", "unsupported"},
		{"native", "model_provider = 'custom'\n[model_providers.custom.capabilities]\nremote_compaction = 'v2'\n", "native", "v2"},
		{"unknown", "[model_providers.OpenAI.capabilities]\nremote_compaction = 'future-version'\n", "preserve", "future-version"},
		{"legacy_profile_is_not_source", "profile = 'work'\nmodel_provider = 'current'\n[profiles.work]\nmodel_provider = 'gateway'\n[model_providers.gateway.capabilities]\nremote_compaction = 'unsupported'\n[model_providers.current.capabilities]\nremote_compaction = 'v2'\n", "native", "v2"},
		{"legacy_profile_only", "profile = 'work'\n[profiles.work]\nmodel_provider = 'gateway'\n[model_providers.gateway.capabilities]\nremote_compaction = 'unsupported'\n", "preserve", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []byte(tc.original)
			before := append([]byte(nil), original...)
			choice, err := ClientCompactionFromConfig(original)
			if err != nil || choice.Mode != tc.mode || choice.RemoteCompaction != tc.remote {
				t.Fatalf("incorrect configured capability: %+v, %v", choice, err)
			}
			if !bytes.Equal(original, before) {
				t.Fatal("status modified the input")
			}
		})
	}
	for _, original := range []string{"broken = [", "model_providers = 'invalid'", "[model_providers.OpenAI]\ncapabilities = 'invalid'", "[model_providers.OpenAI.capabilities]\nremote_compaction = false"} {
		if _, err := ClientCompactionFromConfig([]byte(original)); err == nil {
			t.Fatal("malformed configuration accepted", original)
		}
	}
}

func TestClientConfigPreserveDoesNotInventCompactionCapability(t *testing.T) {
	for _, original := range []string{
		"",
		"profile = 'work'\n[profiles.work]\nmodel_provider = 'gateway'\n[model_providers.gateway.capabilities]\nremote_compaction = 'v2'\nfuture_capability = true\n",
		"model_provider = 'OpenAI'\n[model_providers.OpenAI.capabilities]\nexternal_web_access = false\n",
	} {
		content, snippet, _, err := RenderClientConfigWithOptions([]byte(original), "http://127.0.0.1:8087/v1", "local-test", true, ClientConfigOptions{CompactionMode: "preserve"})
		if err != nil {
			t.Fatal(err)
		}
		for _, data := range [][]byte{content, snippet} {
			choice, err := ClientCompactionFromConfig(data)
			if err != nil || choice.RemoteCompaction != "" || choice.Mode != "preserve" {
				t.Fatal("preserve invented a capability", choice, err)
			}
			if bytes.Contains([]byte(original), []byte("future_capability")) {
				var config map[string]any
				if err := toml.Unmarshal(data, &config); err != nil {
					t.Fatal(err)
				}
				capabilities := config["model_providers"].(map[string]any)[ClientProviderID].(map[string]any)["capabilities"].(map[string]any)
				if capabilities["future_capability"] != true {
					t.Fatal("unrelated legacy capability was lost")
				}
			}
		}
	}
}
