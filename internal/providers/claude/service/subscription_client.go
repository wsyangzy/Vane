package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// RenderSubscriptionClient changes only env fields used by this connection.
// Official login files are never read or written.
func RenderSubscriptionClient(original []byte, baseURL, key string, managed bool) (content, snippet []byte, command string, err error) {
	bom := bytes.HasPrefix(original, []byte{0xef, 0xbb, 0xbf})
	crlf := bytes.Contains(original, []byte("\r\n"))
	original = bytes.TrimPrefix(original, []byte{0xef, 0xbb, 0xbf})
	settings := map[string]json.RawMessage{}
	if len(original) > 0 && (json.Unmarshal(original, &settings) != nil || settings == nil) {
		return nil, nil, "", errors.New("Claude settings.json 不是有效 JSON 对象，未修改")
	}
	env := map[string]json.RawMessage{}
	if raw, ok := settings["env"]; ok && (json.Unmarshal(raw, &env) != nil || env == nil) {
		return nil, nil, "", errors.New("Claude settings.json 的 env 必须是对象")
	}
	var headers string
	if raw, ok := env["ANTHROPIC_CUSTOM_HEADERS"]; ok && json.Unmarshal(raw, &headers) != nil {
		return nil, nil, "", errors.New("ANTHROPIC_CUSTOM_HEADERS 必须是字符串")
	}
	var kept []string
	for _, line := range strings.Split(strings.ReplaceAll(headers, "\r\n", "\n"), "\n") {
		name, _, _ := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if strings.TrimSpace(line) != "" && !strings.EqualFold(name, "X-Relay-Key") && !strings.EqualFold(name, "Authorization") && !strings.EqualFold(name, "X-Api-Key") {
			kept = append(kept, line)
		}
	}
	kept = append(kept, "X-Relay-Key: "+key)
	set := func(k, v string) { env[k], _ = json.Marshal(v) }
	set("ANTHROPIC_BASE_URL", baseURL)
	set("ANTHROPIC_CUSTOM_HEADERS", strings.Join(kept, "\n"))
	delete(env, "ANTHROPIC_API_KEY")
	delete(env, "ANTHROPIC_AUTH_TOKEN")
	delete(env, "CLAUDE_CODE_OAUTH_TOKEN")
	delete(settings, "apiKeyHelper")
	if managed {
		set("ANTHROPIC_AUTH_TOKEN", "relay-managed")
	}
	settings["env"], _ = json.Marshal(env)
	content, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, nil, "", errors.New("生成 Claude 接入配置失败")
	}
	content = append(content, '\n')
	if crlf {
		content = bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
	}
	if bom {
		content = append([]byte{0xef, 0xbb, 0xbf}, content...)
	}
	// Only generated values are exposed in previews; existing secrets stay local.
	previewEnv := map[string]string{"ANTHROPIC_BASE_URL": baseURL, "ANTHROPIC_CUSTOM_HEADERS": "X-Relay-Key: " + key}
	if managed {
		previewEnv["ANTHROPIC_AUTH_TOKEN"] = "relay-managed"
	}
	snippet, _ = json.MarshalIndent(map[string]any{"env": previewEnv}, "", "  ")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	command = "Remove-Item Env:ANTHROPIC_API_KEY, Env:ANTHROPIC_AUTH_TOKEN, Env:CLAUDE_CODE_OAUTH_TOKEN -ErrorAction SilentlyContinue\n$env:ANTHROPIC_BASE_URL = " + quote(baseURL) + "\n$env:ANTHROPIC_CUSTOM_HEADERS = " + quote("X-Relay-Key: "+key)
	if managed {
		command += "\n$env:ANTHROPIC_AUTH_TOKEN = 'relay-managed'"
	}
	command += "\nclaude"
	return
}
