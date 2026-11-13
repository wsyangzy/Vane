package service

import (
	"bytes"
	"errors"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/wsyangzy/Vane/internal/providers/codex/clientconfig"
)

// The capitalized ID is a custom provider; Codex reserves lowercase openai.
const ClientProviderID = "OpenAI"

const imageExtensionHeader = "x-openai-actor-authorization"
const imageExtensionValue = "local-image-extension"

type ClientModelCatalog struct {
	Mode string
	Path string
}

// ClientModelCatalogFromConfig reports the effective choice without opening the
// catalog, so a missing file can still be repaired from the connection panel.
func ClientModelCatalogFromConfig(original []byte) (ClientModelCatalog, error) {
	config := map[string]any{}
	if err := toml.Unmarshal(bytes.TrimPrefix(original, []byte{0xef, 0xbb, 0xbf}), &config); err != nil {
		return ClientModelCatalog{}, errors.New("Codex config.toml 无效，未修改")
	}
	return clientModelCatalog(config), nil
}

func activeClientProfile(config map[string]any) map[string]any {
	name, _ := config["profile"].(string)
	profiles, _ := config["profiles"].(map[string]any)
	profile, _ := profiles[name].(map[string]any)
	return profile
}

func clientModelCatalog(config map[string]any) ClientModelCatalog {
	source := config
	if profile := activeClientProfile(config); profile != nil {
		if _, exists := profile["model_catalog_json"]; exists {
			source = profile
		}
	}
	if path, exists := source["model_catalog_json"]; exists {
		value, _ := path.(string)
		return ClientModelCatalog{Mode: "local", Path: value}
	}
	return ClientModelCatalog{Mode: "remote"}
}

// RenderClientConfig is shared by ordinary Codex settings and client takeover.
// Omitting catalog keeps the effective choice when ordinary settings are saved.
func RenderClientConfig(original []byte, baseURL, key string, managed bool, catalogs ...ClientModelCatalog) (content, snippet []byte, command string, err error) {
	bom := bytes.HasPrefix(original, []byte{0xef, 0xbb, 0xbf})
	crlf := bytes.Contains(original, []byte("\r\n"))
	original = bytes.TrimPrefix(original, []byte{0xef, 0xbb, 0xbf})
	config := map[string]any{}
	if len(original) > 0 && toml.Unmarshal(original, &config) != nil {
		return nil, nil, "", errors.New("Codex config.toml 无效，未修改")
	}
	headers := map[string]any{}
	provider := map[string]any{"name": ClientProviderID, "base_url": baseURL, "model_catalog_url": strings.TrimRight(baseURL, "/") + "/models", "wire_api": "responses", "requires_openai_auth": !managed, "supports_websockets": false, "http_headers": headers}
	if managed {
		provider["experimental_bearer_token"] = key
		headers[imageExtensionHeader] = imageExtensionValue
	} else {
		// Official login uses Authorization; keep local proxy authentication separate.
		headers["X-Relay-Key"] = key
	}
	providers, ok := config["model_providers"].(map[string]any)
	if config["model_providers"] != nil && !ok {
		return nil, nil, "", errors.New("Codex model_providers 必须是表")
	}
	if providers == nil {
		providers = map[string]any{}
	}
	selected, _ := config["model_provider"].(string)
	if profile, ok := config["profile"].(string); ok {
		if profiles, ok := config["profiles"].(map[string]any); ok {
			if active, ok := profiles[profile].(map[string]any); ok {
				if value, ok := active["model_provider"].(string); ok {
					selected = value
				}
			}
		}
	}
	previous, _ := providers[selected].(map[string]any)
	// Keep an explicitly configured marker when using official login passthrough.
	if !managed {
		if originalHeaders, ok := previous["http_headers"].(map[string]any); ok {
			for name, value := range originalHeaders {
				if strings.EqualFold(name, imageExtensionHeader) && value == imageExtensionValue {
					headers[imageExtensionHeader] = value
				}
			}
		}
	}
	features, ok := config["features"].(map[string]any)
	if config["features"] != nil && !ok {
		return nil, nil, "", errors.New("Codex features 必须是表")
	}
	if features == nil {
		features = map[string]any{}
	}
	for _, field := range []string{"image_generation", "goals"} {
		if _, exists := features[field]; !exists {
			features[field] = true
		}
	}
	config["features"] = features
	selection := clientModelCatalog(config)
	if len(catalogs) > 0 && catalogs[0].Mode != "" {
		selection = catalogs[0]
		switch selection.Mode {
		case "local":
			config["model_catalog_json"] = selection.Path
		case "remote":
			delete(config, "model_catalog_json")
		default:
			return nil, nil, "", errors.New("模型目录方式必须为 local 或 remote")
		}
		// The explicit panel choice must also replace an active profile override.
		delete(activeClientProfile(config), "model_catalog_json")
	}
	catalogConfig := config
	if profile := activeClientProfile(config); profile != nil {
		if _, overrides := profile["model_catalog_json"]; overrides {
			catalogConfig = profile
		}
	}
	if err := resolveClientModelCatalog(catalogConfig); err != nil {
		return nil, nil, "", err
	}
	features["api_key_model_discovery"] = selection.Mode == "remote"
	if selection.Mode == "local" {
		delete(provider, "model_catalog_url")
	}
	active := activeClientProfile(config)
	defaults := map[string]any{
		"model": "gpt-6.1-sol", "web_search": "live", "disable_response_storage": true,
		"network_access": "enabled", "windows_wsl_setup_acknowledged": true,
	}
	for field, value := range defaults {
		if _, exists := config[field]; !exists {
			if override, exists := active[field]; exists {
				value = override
			}
			config[field] = value
		}
	}
	if _, exists := config["review_model"]; !exists {
		config["review_model"] = config["model"]
		if value, exists := active["model"]; exists {
			config["review_model"] = value
		}
		if value, exists := active["review_model"]; exists {
			config["review_model"] = value
		}
	}
	// Carry client transport preferences across the provider rename. Gateway
	// auth, query parameters and headers belong to the old route.
	if existing, ok := providers[ClientProviderID].(map[string]any); ok {
		previous = existing
	}
	for _, field := range []string{"request_max_retries", "stream_max_retries", "stream_idle_timeout_ms"} {
		if value, ok := previous[field]; ok {
			provider[field] = value
		}
	}
	providers[ClientProviderID] = provider
	config["model_providers"] = providers
	config["model_provider"] = ClientProviderID
	// Keep the currently selected profile from silently overriding the new route.
	if profile, ok := config["profile"].(string); ok && profile != "" {
		if profiles, ok := config["profiles"].(map[string]any); ok {
			if selected, ok := profiles[profile].(map[string]any); ok {
				selected["model_provider"] = ClientProviderID
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
	preview := map[string]any{"model_provider": ClientProviderID, "model_providers": map[string]any{ClientProviderID: provider}, "features": map[string]any{
		"api_key_model_discovery": features["api_key_model_discovery"], "image_generation": features["image_generation"], "goals": features["goals"],
	}}
	for _, field := range []string{"model", "review_model", "web_search", "disable_response_storage", "network_access", "windows_wsl_setup_acknowledged"} {
		preview[field] = config[field]
		if value, exists := active[field]; exists {
			preview[field] = value
		}
	}
	if path, ok := catalogConfig["model_catalog_json"]; ok {
		preview["model_catalog_json"] = path
	}
	snippet, _ = toml.Marshal(preview)
	command = "# 先将下方 TOML 应用到显示的 config.toml，再运行\ncodex"
	return
}

func resolveClientModelCatalog(config map[string]any) error {
	value, exists := config["model_catalog_json"]
	if !exists {
		return nil
	}
	path, ok := value.(string)
	if !ok || strings.TrimSpace(path) == "" {
		return errors.New("model_catalog_json 必须是非空文件路径")
	}
	resolved, _, err := clientconfig.ReadModelCatalog(path)
	if err != nil {
		return err
	}
	config["model_catalog_json"] = resolved
	return nil
}
