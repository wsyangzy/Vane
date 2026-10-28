package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRoutingClassifierRequestSupportsManagedStreamingAndAPIResponses(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		body, err := RoutingClassifierRequest("gpt-fast", `{"current":"[redacted]"}`, streaming)
		if err != nil {
			t.Fatal(err)
		}
		var request struct {
			Model        string `json:"model"`
			Instructions string `json:"instructions"`
			Stream       bool   `json:"stream"`
			Store        *bool  `json:"store"`
			Input        []struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err = json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "gpt-fast" || request.Stream != streaming || request.Store == nil || *request.Store || len(request.Input) != 1 || request.Input[0].Role != "user" || request.Input[0].Content[0].Type != "input_text" || request.Input[0].Content[0].Text != `{"current":"[redacted]"}` || !strings.Contains(request.Instructions, "intent_id") {
			t.Fatalf("request contract differs: %s", body)
		}
	}
}

func classifierResponsesFixture(t *testing.T, text string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": "gpt-fast", "status": "completed", "output": []map[string]any{{"type": "reasoning"}, {"type": "message", "content": []map[string]string{{"type": "output_text", "text": text}}}}, "usage": map[string]any{"input_tokens": "100", "output_tokens": 20, "input_tokens_details": map[string]any{"cached_tokens": "60", "cache_write_tokens": 5}, "output_tokens_details": map[string]int{"reasoning_tokens": 9}}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestRoutingClassifierResponsesPreservesRawCachedAndReasoningSubset(t *testing.T) {
	result, usage, err := ParseRoutingClassifierResponse(200, http.Header{}, classifierResponsesFixture(t, `{"intent_id":"coding","confidence":0.85}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IntentID != "coding" || result.Confidence != 0.85 || usage == nil || usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.ReasoningTokens != 9 || usage.CacheReadTokens != 60 || usage.CacheCreationTokens != 5 || !usage.InputIncludesCacheRead {
		t.Fatalf("intent=%+v usage=%+v", result, usage)
	}
}

func TestRoutingClassifierSSERequiresCompletionAndSupportsCRLFMultilineData(t *testing.T) {
	response := classifierResponsesFixture(t, `{"intent_id":"coding","confidence":0.8}`)
	event, _ := json.Marshal(map[string]any{"type": "response.completed", "response": json.RawMessage(response)})
	body := []byte(": ping\r\n\r\nevent: response.completed\r\ndata: " + string(event) + "\r\n\r\ndata: [DONE]\r\n\r\n")
	result, usage, err := ParseRoutingClassifierResponse(200, http.Header{"Content-Type": []string{"text/event-stream"}}, body)
	if err != nil || result.IntentID != "coding" || usage == nil || usage.OutputTokens != 20 {
		t.Fatalf("completed SSE: %+v %+v %v", result, usage, err)
	}
	// The parser accepts standard multi-line data fields and an EOF without a
	// trailing blank line, while completion still comes from response.completed.
	multiline := []byte("event: response.completed\ndata: {\ndata: \"type\":\"response.completed\",\ndata: \"response\":" + string(response) + "\ndata: }")
	if _, _, err = ParseRoutingClassifierResponse(200, nil, multiline); err != nil {
		t.Fatalf("multiline SSE rejected: %v", err)
	}
	delta := `data: {"type":"response.output_text.delta","delta":"{\"intent_id\":\"coding\",\"confidence\":0.8}"}` + "\n\ndata: [DONE]\n\n"
	if _, _, err = ParseRoutingClassifierResponse(200, nil, []byte(delta)); err == nil || err.Error() != "routing_classifier_response_incomplete" {
		t.Fatalf("delta/DONE falsely completed: %v", err)
	}
	if _, _, err = ParseRoutingClassifierResponse(200, nil, []byte(delta+"data: {\"type\":\"response.completed\",\"response\":null}\n\n")); err == nil {
		t.Fatal("null completed response accepted")
	}
	failed := append(append([]byte{}, body...), []byte("data: {\"type\":\"error\",\"error\":{\"message\":\"secret-provider-error\"}}\n\n")...)
	_, usage, err = ParseRoutingClassifierResponse(200, nil, failed)
	if err == nil || err.Error() != "routing_classifier_upstream_failed" || usage == nil || usage.InputTokens != 100 {
		t.Fatalf("late error accepted or usage lost: %+v %v", usage, err)
	}
}

func TestRoutingClassifierSSECanUseTextDeltasAfterVerifiedCompletion(t *testing.T) {
	delta := `data: {"type":"response.output_text.delta","delta":"{\"intent_id\":\"coding\",\"confidence\":0.7}"}` + "\n\n"
	complete := `data: {"type":"response.completed","response":{"status":"completed","model":"gpt-fast","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n\n"
	result, usage, err := ParseRoutingClassifierResponse(200, nil, []byte(delta+complete))
	if err != nil || result.IntentID != "coding" || usage == nil || usage.InputTokens != 10 {
		t.Fatalf("verified delta fallback: %+v %+v %v", result, usage, err)
	}
}

func TestRoutingClassifierResponsesRejectsMalformedIntentWithoutBodyInErrors(t *testing.T) {
	for _, content := range []string{`{"intent_id":"coding"}`, `{"intent_id":"coding","confidence":2}`, `{"intent_id":"","confidence":0.8}`, "private-upstream-content"} {
		_, usage, err := ParseRoutingClassifierResponse(200, nil, classifierResponsesFixture(t, content))
		if err == nil || err.Error() != "routing_classifier_intent_invalid" || usage == nil || usage.OutputTokens != 20 {
			t.Fatalf("malformed result accepted or usage lost: %v %+v", err, usage)
		}
	}
	_, _, err := ParseRoutingClassifierResponse(403, nil, []byte(`{"error":"private-upstream-content"}`))
	if err == nil || err.Error() != "routing_classifier_upstream_failed" {
		t.Fatalf("upstream error accepted/leaked: %v", err)
	}
}
