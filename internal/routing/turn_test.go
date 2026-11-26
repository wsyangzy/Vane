package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTurnRecognitionKeepsProviderIdentityAndContinuationWithoutText(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			for _, message := range []string{"继续", "continue", strings.Repeat("前", 4000) + "继续"} {
				var body []byte
				if provider == "claude" {
					body, _ = json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": message}}})
				} else {
					body, _ = json.Marshal(map[string]any{"input": message})
				}
				headers := http.Header{"X-Session-Id": {"session"}, "X-Branch-Id": {"branch"}}
				turn := ResolveTurn(context.Background(), headers, body, provider)
				if !turn.NewTurn || turn.TurnID == "" || turn.ConversationID != "session" || turn.BranchID != "branch" || turn.Continuation != (len([]rune(message)) < 40) {
					t.Fatalf("turn recognition changed: %+v", turn)
				}
				raw, err := json.Marshal(turn)
				if err != nil || string(raw) != "{}" {
					t.Fatalf("turn metadata or message text leaked: %s %v", raw, err)
				}
				headers.Set("X-Branch-Id", "another")
				if other := ResolveTurn(context.Background(), headers, body, provider); other.TurnID == turn.TurnID {
					t.Fatal("different branches shared a turn")
				}
			}
		})
	}
}

func TestTurnExtractionExcludesToolsSystemAndReasoning(t *testing.T) {
	body := []byte(`{"system":"system-secret","tools":[{"description":"tool-secret"}],"messages":[{"role":"user","content":"最初的问题"},{"role":"assistant","content":[{"type":"thinking","thinking":"hidden-secret"},{"type":"text","text":"先修复测试"}]},{"role":"user","content":[{"type":"tool_result","content":"tool-result-secret"},{"type":"text","text":"继续"}]}]}`)
	h := http.Header{"X-Claude-Session-Id": []string{"s"}}
	t1 := ResolveTurn(context.Background(), h, body, "claude")
	if !t1.NewTurn || t1.TurnID == "" || !t1.Continuation {
		t.Fatalf("%+v", t1)
	}
	raw, _ := json.Marshal(t1)
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("private protocol context leaked: %s", raw)
	}
	tool := strings.TrimSuffix(string(body), "]}") + `,{"role":"assistant","content":[{"type":"tool_use","input":{"secret":"do not classify"}}]},{"role":"user","content":[{"type":"tool_result","content":"more-secret"}]}]}`
	t2 := ResolveTurn(context.Background(), h, []byte(tool), "claude")
	if t2.NewTurn || t2.TurnID != t1.TurnID {
		t.Fatalf("tool changed turn: %+v %+v", t1, t2)
	}
	h.Set("X-Claude-Session-Id", "other")
	t3 := ResolveTurn(context.Background(), h, body, "claude")
	if t3.TurnID == t1.TurnID {
		t.Fatal("conversation collision")
	}
	noID := ResolveTurn(context.Background(), http.Header{}, body, "claude")
	if !noID.NewTurn || noID.TurnID != "" {
		t.Fatal("unscoped user content must rotate without creating a persistent turn identity")
	}
}
