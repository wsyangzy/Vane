package routing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// TurnInput contains request-scoped identity and continuation hints, never message text.
type TurnInput struct {
	ConversationID string `json:"-"`
	BranchID       string `json:"-"`
	TurnID         string `json:"-"`
	NewTurn        bool   `json:"-"`
	Continuation   bool   `json:"-"`
}

func routingHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// ResolveTurn reads only explicit user/assistant text blocks. Tool outputs,
// system messages, tool definitions, images and reasoning are never extracted.
func ResolveTurn(ctx context.Context, h http.Header, body []byte, provider string) TurnInput {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return TurnInput{}
	}
	t := TurnInput{ConversationID: h.Get("X-Session-Id"), BranchID: h.Get("X-Branch-Id")}
	for _, key := range []string{"X-Claude-Session-Id", "X-Codex-Session-Id", "Session_id", "Conversation_id"} {
		if t.ConversationID == "" {
			t.ConversationID = h.Get(key)
		}
	}
	if t.ConversationID == "" {
		if metadata, ok := root["metadata"].(map[string]any); ok {
			t.ConversationID, _ = metadata["conversation_id"].(string)
		}
	}
	var messages []any
	if provider == "claude" {
		messages, _ = root["messages"].([]any)
	} else {
		switch input := root["input"].(type) {
		case string:
			messages = []any{map[string]any{"role": "user", "content": input}}
		case []any:
			messages = input
		}
	}
	texts := func(content any) string {
		if s, ok := content.(string); ok {
			return s
		}
		var parts []string
		if blocks, ok := content.([]any); ok {
			for _, b := range blocks {
				obj, ok := b.(map[string]any)
				if !ok {
					continue
				}
				typ, _ := obj["type"].(string)
				if typ == "text" || typ == "input_text" || typ == "output_text" {
					s, _ := obj["text"].(string)
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	lastUser := -1
	var currentUserMessage string
	for i, item := range messages {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := obj["role"].(string)
		text := texts(obj["content"])
		if role == "user" && strings.TrimSpace(text) != "" && !strings.HasPrefix(strings.TrimSpace(text), "<system-reminder>") {
			lastUser = i
			currentUserMessage = text
		}
	}
	t.NewTurn = lastUser >= 0 && lastUser == len(messages)-1
	for _, key := range []string{"X-Claude-Turn-Id", "X-Claude-Prompt-Id", "X-Codex-Turn-Id", "X-Codex-Prompt-Id", "X-Claude-User-Message-ID", "X-Codex-User-Message-ID"} {
		if value := h.Get(key); value != "" {
			t.TurnID = routingHash(CallerScope(ctx), provider, t.ConversationID, t.BranchID, value)
			break
		}
	}
	if t.TurnID == "" && lastUser >= 0 && t.ConversationID != "" {
		raw, _ := json.Marshal(messages[:lastUser+1])
		t.TurnID = routingHash(CallerScope(ctx), provider, t.ConversationID, t.BranchID, string(raw))
	}
	current := strings.TrimSpace(currentUserMessage)
	t.Continuation = !t.NewTurn || len([]rune(current)) < 40 && (strings.Contains(current, "继续") || strings.Contains(current, "刚才") || current == "修复这个" || strings.EqualFold(current, "continue"))
	return t
}
