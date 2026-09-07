package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

const maxTurnFingerprintTextBytes = 8192

func requestTurnFailureCacheKey(r *http.Request, body []byte) string {
	if r != nil {
		for _, header := range []string{
			"X-Claude-Turn-ID",
			"X-Claude-Prompt-ID",
			"X-Claude-User-Message-ID",
			"X-Anthropic-Turn-ID",
			"Anthropic-Turn-ID",
			"X-Codex-Turn-ID",
			"X-Codex-Prompt-ID",
			"X-Codex-User-Message-ID",
		} {
			if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
				return hashTurnCacheKey(strings.ToLower(header), value)
			}
		}
	}

	var payload any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if value := explicitTurnIDFromPayload(obj); value != "" {
		return hashTurnCacheKey("explicit", value)
	}
	if value := lastUserInputText(obj); value != "" {
		return hashTurnCacheKey("user", value)
	}
	return ""
}

func explicitTurnIDFromPayload(obj map[string]any) string {
	for _, key := range []string{"turn_id", "turnId", "prompt_id", "promptId", "user_message_id", "userMessageId"} {
		if value := turnStringValue(obj[key]); value != "" {
			return key + ":" + value
		}
	}
	if metadata, ok := obj["metadata"].(map[string]any); ok {
		for _, key := range []string{"turn_id", "turnId", "prompt_id", "promptId", "user_message_id", "userMessageId"} {
			if value := turnStringValue(metadata[key]); value != "" {
				return "metadata." + key + ":" + value
			}
		}
	}
	return ""
}

func lastUserInputText(obj map[string]any) string {
	for _, key := range []string{"messages", "input"} {
		if text := lastUserTextFromValue(obj[key]); text != "" {
			return text
		}
	}
	return ""
}

func lastUserTextFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for i := len(typed) - 1; i >= 0; i-- {
			item, ok := typed[i].(map[string]any)
			if !ok {
				continue
			}
			role := strings.ToLower(turnStringValue(item["role"]))
			if role != "user" {
				continue
			}
			if text := collectTurnContentText(item["content"]); text != "" {
				return text
			}
		}
	}
	return ""
}

func collectTurnContentText(value any) string {
	var builder strings.Builder
	appendTurnContentText(&builder, value)
	return strings.TrimSpace(builder.String())
}

func appendTurnContentText(builder *strings.Builder, value any) {
	if builder.Len() >= maxTurnFingerprintTextBytes {
		return
	}
	switch typed := value.(type) {
	case string:
		appendTurnText(builder, typed)
	case []any:
		for _, item := range typed {
			appendTurnContentText(builder, item)
			if builder.Len() >= maxTurnFingerprintTextBytes {
				return
			}
		}
	case map[string]any:
		if text := turnStringValue(typed["text"]); text != "" {
			appendTurnText(builder, text)
			return
		}
		if text := turnStringValue(typed["input_text"]); text != "" {
			appendTurnText(builder, text)
			return
		}
		if content, ok := typed["content"]; ok {
			appendTurnContentText(builder, content)
		}
	}
}

func appendTurnText(builder *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" || builder.Len() >= maxTurnFingerprintTextBytes {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	remaining := maxTurnFingerprintTextBytes - builder.Len()
	if len(text) > remaining {
		text = text[:remaining]
	}
	builder.WriteString(text)
}

func turnStringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func hashTurnCacheKey(prefix, value string) string {
	sum := sha256.Sum256([]byte(prefix + "\x00" + value))
	return prefix + ":" + hex.EncodeToString(sum[:12])
}
