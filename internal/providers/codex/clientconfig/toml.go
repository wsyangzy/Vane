package clientconfig

import (
	"encoding/json"
	"fmt"
	"strings"

	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
)

const WireAPI = "responses"

const ContextWindow1M = "1000000"

type TopLevelValue struct {
	Value  string
	Quoted bool
}

func UpsertCodexAuthConfig(content string, apiKey string) ([]byte, error) {
	authConfig := make(map[string]any)

	if trimmed := strings.TrimSpace(content); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &authConfig); err != nil {
			return nil, fmt.Errorf("解析 Codex auth.json 失败: %w", err)
		}
	}

	authConfig["OPENAI_API_KEY"] = apiKey

	updated, err := json.MarshalIndent(authConfig, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成 Codex auth.json 失败: %w", err)
	}

	return append(updated, '\n'), nil
}

func BuildCodexTopLevelConfigValues(record *codexstore.CodexProviderRecord) map[string]TopLevelValue {
	values := map[string]TopLevelValue{
		"model": {
			Value:  record.Model,
			Quoted: true,
		},
		"model_provider": {
			Value:  record.Name,
			Quoted: true,
		},
	}

	if record.Enable1MContext {
		values["model_context_window"] = TopLevelValue{
			Value:  ContextWindow1M,
			Quoted: false,
		}
	}

	return values
}

func UpsertCodexTopLevelConfig(content string, values map[string]TopLevelValue) (string, error) {
	order := []string{"model", "model_context_window", "model_reasoning_effort", "model_provider"}
	lines := SplitCodexLines(content)
	found := make(map[string]bool, len(values))
	firstSectionIndex := -1
	inSection := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if firstSectionIndex == -1 {
				firstSectionIndex = i
			}
			inSection = true
			continue
		}

		if inSection {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		for _, key := range order {
			value, ok := values[key]
			if !ok {
				continue
			}
			if !found[key] && IsTopLevelTomlAssignment(trimmed, key) {
				lines[i] = fmt.Sprintf("%s = %s", key, RenderCodexTopLevelValue(value))
				found[key] = true
			}
		}
	}

	missing := make([]string, 0, len(order))
	for _, key := range order {
		value, ok := values[key]
		if !ok {
			continue
		}
		if !found[key] {
			missing = append(missing, fmt.Sprintf("%s = %s", key, RenderCodexTopLevelValue(value)))
		}
	}

	if len(missing) > 0 {
		insertAt := firstSectionIndex
		if insertAt < 0 {
			insertAt = len(lines)
		}
		insertion := append(missing, "")
		lines = append(lines[:insertAt], append(insertion, lines[insertAt:]...)...)
	}

	return JoinCodexLines(lines), nil
}

func RenderCodexTopLevelValue(value TopLevelValue) string {
	if value.Quoted {
		return fmt.Sprintf("\"%s\"", EscapeTomlString(value.Value))
	}
	return value.Value
}

func RemoveCodexTopLevelConfig(content string, keys []string) string {
	lines := SplitCodexLines(content)
	inSection := false
	filtered := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = true
			filtered = append(filtered, line)
			continue
		}

		if inSection {
			filtered = append(filtered, line)
			continue
		}

		shouldSkip := false
		for _, key := range keys {
			if IsTopLevelTomlAssignment(trimmed, key) {
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			continue
		}

		filtered = append(filtered, line)
	}

	return JoinCodexLines(filtered)
}

func UpsertCodexProviderSection(content string, record *codexstore.CodexProviderRecord) (string, error) {
	if record == nil {
		return "", fmt.Errorf("Codex 提供商记录为空")
	}

	updated := DeleteCodexProviderSection(content, record.Name)
	lines := SplitCodexLines(updated)

	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}

	lines = append(lines, RenderCodexProviderSection(record)...)
	lines = append(lines, "")
	return JoinCodexLines(lines), nil
}

func DeleteCodexProviderSection(content, name string) string {
	lines := SplitCodexLines(content)
	headers := CodexProviderSectionHeaders(name)

	start := -1
	end := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start == -1 {
			if ContainsString(headers, trimmed) {
				start = i
			}
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			end = i
			break
		}
	}

	if start == -1 {
		return JoinCodexLines(lines)
	}
	if end == -1 {
		end = len(lines)
	}

	lines = append(lines[:start], lines[end:]...)
	return JoinCodexLines(lines)
}

func RenderCodexProviderSection(record *codexstore.CodexProviderRecord) []string {
	return []string{
		CodexProviderSectionHeader(record.Name),
		fmt.Sprintf("name = \"%s\"", EscapeTomlString(record.Name)),
		fmt.Sprintf("base_url = \"%s\"", EscapeTomlString(record.BaseURL)),
		fmt.Sprintf("wire_api = \"%s\"", WireAPI),
		"requires_openai_auth = true",
	}
}

func CodexProviderSectionHeader(name string) string {
	if IsTomlBareKey(name) {
		return fmt.Sprintf("[model_providers.%s]", name)
	}
	return fmt.Sprintf("[model_providers.\"%s\"]", EscapeTomlString(name))
}

func CodexProviderSectionHeaders(name string) []string {
	formatted := CodexProviderSectionHeader(name)
	legacy := fmt.Sprintf("[model_providers.%s]", name)
	if formatted == legacy {
		return []string{formatted}
	}
	return []string{formatted, legacy}
}

func IsTomlBareKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func ContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func SplitCodexLines(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if normalized == "" {
		return []string{}
	}
	return strings.Split(normalized, "\n")
}

func JoinCodexLines(lines []string) string {
	updated := strings.Join(lines, "\n")
	updated = strings.TrimRight(updated, "\r\n")
	if updated == "" {
		return ""
	}
	return updated + "\n"
}

func IsTopLevelTomlAssignment(trimmedLine, key string) bool {
	parts := strings.SplitN(trimmedLine, "=", 2)
	if len(parts) != 2 {
		return false
	}
	return strings.TrimSpace(parts[0]) == key
}

func EscapeTomlString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", `"`, `\"`)
	return replacer.Replace(value)
}
