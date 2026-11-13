package clientconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CatalogLimit matches the limit used by Codex clients for explicit catalog URLs.
const CatalogLimit = 1 << 20

const localCatalogLimit = 16 << 20

// ReadModelCatalog resolves a user-supplied catalog without changing its file.
// Codex configuration uses an absolute path; a shell does not expand TOML values.
func ReadModelCatalog(path string) (string, []byte, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil, errors.New("无法解析模型目录的用户路径")
		}
		path = filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
	}
	if !filepath.IsAbs(path) {
		return "", nil, errors.New("model_catalog_json 必须使用绝对路径或 ~/ 路径")
	}
	path = filepath.Clean(path)
	file, err := os.Open(path)
	if err != nil {
		return "", nil, fmt.Errorf("读取 model_catalog_json 失败: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, localCatalogLimit+1))
	if err != nil || len(data) > localCatalogLimit {
		return "", nil, errors.New("模型目录不可读或超过 16 MiB")
	}
	if _, err := catalogEntries(data); err != nil {
		return "", nil, fmt.Errorf("model_catalog_json 无效: %w", err)
	}
	return path, data, nil
}

func catalogEntries(data []byte) ([]map[string]json.RawMessage, error) {
	var root struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &root) != nil || root.Models == nil {
		return nil, errors.New("需要包含 models 数组的 Codex 模型目录")
	}
	seen := map[string]bool{}
	for _, model := range root.Models {
		var slug, instructions string
		_ = json.Unmarshal(model["slug"], &slug)
		_ = json.Unmarshal(model["base_instructions"], &instructions)
		if instructions == "" {
			var messages struct {
				Instructions string `json:"instructions_template"`
			}
			_ = json.Unmarshal(model["model_messages"], &messages)
			instructions = messages.Instructions
		}
		if strings.TrimSpace(slug) == "" || seen[slug] || strings.TrimSpace(instructions) == "" {
			return nil, errors.New("模型需要唯一的非空 slug 和模型指令")
		}
		seen[slug] = true
	}
	return root.Models, nil
}

// AdaptModelCatalog retains native metadata. A public OpenAI ID list needs
// Codex metadata instead; matching cached client entries preserve their prompts
// and capabilities, while unknown IDs use the minimal documented gateway entry.
func AdaptModelCatalog(data, templates []byte) ([]byte, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || root == nil || len(root["error"]) > 0 && string(root["error"]) != "null" {
		return nil, errors.New("无效的上游模型列表")
	}
	if _, native := root["models"]; native {
		if _, err := catalogEntries(data); err != nil {
			return nil, err
		}
		return data, nil
	}
	var list []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(root["data"], &list) != nil || list == nil {
		return nil, errors.New("上游未返回 models 或 data 模型数组")
	}
	known := map[string]map[string]json.RawMessage{}
	if entries, err := catalogEntries(templates); err == nil {
		for _, entry := range entries {
			var slug string
			_ = json.Unmarshal(entry["slug"], &slug)
			known[slug] = entry
		}
	}
	models := make([]map[string]json.RawMessage, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return nil, errors.New("上游模型 ID 不能为空")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		entry := known[id]
		if entry != nil {
			copy := make(map[string]json.RawMessage, len(entry))
			for name, value := range entry {
				copy[name] = value
			}
			entry = copy
			// Availability comes from this provider, rather than the account
			// whose cached metadata supplies the model's instructions.
			entry["supported_in_api"] = []byte(`true`)
			entry["visibility"] = []byte(`"list"`)
		} else {
			entry = map[string]json.RawMessage{
				"base_instructions":            []byte(`"Help the user with their coding tasks."`),
				"supported_reasoning_levels":   []byte(`[]`),
				"shell_type":                   []byte(`"shell_command"`),
				"visibility":                   []byte(`"list"`),
				"supported_in_api":             []byte(`true`),
				"priority":                     []byte(`0`),
				"support_verbosity":            []byte(`false`),
				"truncation_policy":            []byte(`{"mode":"tokens","limit":10000}`),
				"experimental_supported_tools": []byte(`[]`),
				"input_modalities":             []byte(`["text"]`),
			}
			entry["slug"], _ = json.Marshal(id)
			entry["display_name"], _ = json.Marshal(id)
		}
		models = append(models, entry)
	}
	return json.Marshal(struct {
		Models []map[string]json.RawMessage `json:"models"`
	}{models})
}

// CachedModelCatalog is only a metadata template, never the source of available
// model IDs or credentials. Missing or incompatible client caches are optional.
func CachedModelCatalog() []byte {
	home, err := GetCodexHomeDir()
	if err != nil {
		return nil
	}
	_, data, err := ReadModelCatalog(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return nil
	}
	return data
}
