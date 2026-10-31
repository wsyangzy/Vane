package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRoutingClassifierRequestUsesSeparateNonStreamingMessages(t *testing.T) {
	body, err := RoutingClassifierRequest("claude-fast", `{"current":"[redacted]","intents":["coding"]}`)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Model     string `json:"model"`
		System    string `json:"system"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err = json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if request.Model != "claude-fast" || request.Stream || request.MaxTokens != 256 || len(request.Messages) != 1 || request.Messages[0].Content != `{"current":"[redacted]","intents":["coding"]}` || !strings.Contains(request.System, "intent_id") {
		t.Fatalf("request contract differs: %s", body)
	}
	if _, err = RoutingClassifierRequest("", "prompt"); err == nil {
		t.Fatal("empty classifier model accepted")
	}
}

func TestRoutingClassifierMessagesResponsePreservesCacheUsage(t *testing.T) {
	body := []byte(`{"type":"message","model":"claude-fast","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"ignore"},{"type":"text","text":"{\"intent_id\":\"coding-v2\",\"confidence\":0.9}"}],"usage":{"input_tokens":20,"output_tokens":15,"cache_read_input_tokens":70,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":10}}}`)
	result, usage, err := ParseRoutingClassifierResponse(200, http.Header{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if result.IntentID != "coding-v2" || result.Confidence != 0.9 || result.Model != "claude-fast" || usage == nil || usage.InputTokens != 20 || usage.OutputTokens != 15 || usage.CacheReadTokens != 70 || usage.CacheCreationTokens != 15 || usage.CacheCreation5mTokens != 5 || usage.CacheCreation1hTokens != 10 || usage.InputIncludesCacheRead {
		t.Fatalf("intent=%+v usage=%+v", result, usage)
	}
}

func TestRoutingClassifierMessagesFailuresStaySafeAndKeepBilledUsage(t *testing.T) {
	for _, tc := range []struct{ name, content, stop string }{
		{"missing-confidence", `{"intent_id":"coding"}`, "end_turn"},
		{"null-confidence", `{"intent_id":"coding","confidence":null}`, "end_turn"},
		{"bad-confidence", `{"intent_id":"coding","confidence":1.1}`, "end_turn"},
		{"truncated", `{"intent_id":"coding","confidence":0.8}`, "max_tokens"},
		{"private-text", "secret-upstream-content", "end_turn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"type": "message", "stop_reason": tc.stop, "content": []map[string]string{{"type": "text", "text": tc.content}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5}})
			_, usage, err := ParseRoutingClassifierResponse(200, nil, body)
			if err == nil || !strings.HasPrefix(err.Error(), "routing_classifier_") || strings.Contains(err.Error(), tc.content) || usage == nil || usage.OutputTokens != 5 {
				t.Fatalf("unsafe error or missing billed usage: %v %+v", err, usage)
			}
		})
	}
	_, _, err := ParseRoutingClassifierResponse(401, nil, []byte(`{"error":"secret-api-key"}`))
	if err == nil || err.Error() != "routing_classifier_upstream_failed" {
		t.Fatalf("upstream error leaked or accepted: %v", err)
	}
}
