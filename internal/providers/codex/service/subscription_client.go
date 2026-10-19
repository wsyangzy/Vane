package service

import (
	"bytes"
	"errors"

	"github.com/pelletier/go-toml/v2"
)

const SubscriptionClientProvider = "relay_subscription"

// RenderSubscriptionClient preserves unknown settings as TOML values. Formatting
// is normalized; the file layer keeps an encrypted byte-exact rollback copy.
func RenderSubscriptionClient(original []byte, baseURL, key string, managed bool) (content, snippet []byte, command string, err error) {
	bom := bytes.HasPrefix(original, []byte{0xef, 0xbb, 0xbf})
	crlf := bytes.Contains(original, []byte("\r\n"))
	original = bytes.TrimPrefix(original, []byte{0xef, 0xbb, 0xbf})
	config := map[string]any{}
	if len(original) > 0 && toml.Unmarshal(original, &config) != nil {
		return nil, nil, "", errors.New("Codex config.toml 无效，未修改")
	}
	provider := map[string]any{"name": "Relay Subscription", "base_url": baseURL, "wire_api": "responses", "requires_openai_auth": !managed, "supports_websockets": false, "http_headers": map[string]any{"X-Relay-Key": key}}
	if managed {
		provider["experimental_bearer_token"] = "relay-managed"
	}
	providers, ok := config["model_providers"].(map[string]any)
	if config["model_providers"] != nil && !ok {
		return nil, nil, "", errors.New("Codex model_providers 必须是表")
	}
	if providers == nil {
		providers = map[string]any{}
	}
	providers[SubscriptionClientProvider] = provider
	config["model_providers"] = providers
	config["model_provider"] = SubscriptionClientProvider
	// Keep the currently selected profile from silently overriding the new route.
	if profile, ok := config["profile"].(string); ok && profile != "" {
		if profiles, ok := config["profiles"].(map[string]any); ok {
			if selected, ok := profiles[profile].(map[string]any); ok {
				selected["model_provider"] = SubscriptionClientProvider
			}
		}
	}
	// An existing forced method must agree with the selected credential source.
	if _, ok := config["forced_login_method"]; ok {
		if managed {
			config["forced_login_method"] = "api"
		} else {
			config["forced_login_method"] = "chatgpt"
		}
	}
	content, err = toml.Marshal(config)
	if err != nil {
		return nil, nil, "", errors.New("生成 Codex 接入配置失败")
	}
	if crlf {
		content = bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
	}
	if bom {
		content = append([]byte{0xef, 0xbb, 0xbf}, content...)
	}
	snippet, _ = toml.Marshal(map[string]any{"model_provider": SubscriptionClientProvider, "model_providers": map[string]any{SubscriptionClientProvider: provider}})
	command = "# 先将下方 TOML 应用到显示的 config.toml，再运行\ncodex"
	return
}
