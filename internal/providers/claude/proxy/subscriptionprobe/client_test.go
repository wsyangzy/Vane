package subscriptionprobe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt in with RELAY_T0_CLAUDE_BINARY. The normal client runs with sentinel
// authentication, an empty temporary configuration and a local mock only.
// --bare prevents keychain, hooks, plugins and workspace instruction loading.
// This verifies client configuration compatibility, never official OAuth.
func TestClaudeClientLocalConfiguration(t *testing.T) {
	binary := os.Getenv("RELAY_T0_CLAUDE_BINARY")
	if binary == "" {
		t.Skip("set RELAY_T0_CLAUDE_BINARY for isolated native client validation")
	}
	registry := NewRegistry()
	_, key, err := registry.Create("client-test")
	if err != nil {
		t.Fatal(err)
	}
	var received, forwarded atomic.Int32
	var betaQuery atomic.Bool
	probe := New(registry, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		forwarded.Add(1)
		if r.Header.Get("Authorization") != "Bearer local-auth-sentinel" || r.Header.Get("X-Relay-Key") != "" {
			t.Error("authentication/header contract changed")
		}
		if r.URL.Path == "/v1/messages/count_tokens" {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"input_tokens":1}`))}, nil
		}
		frames := []string{
			`{"type":"message_start","message":{"id":"msg-local","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`,
		}
		var body strings.Builder
		for _, frame := range frames {
			var event struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(frame), &event)
			body.WriteString("event: " + event.Type + "\ndata: " + frame + "\n\n")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}, nil
	}), nil, "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		betaQuery.Store(r.URL.RawQuery == "beta=true")
		probe.ServeHTTP(w, r)
	}))
	defer server.Close()
	if err := runIsolatedClaude(t, binary, server.URL, key, "", "Reply OK"); err != nil {
		t.Fatalf("isolated client failed: exit=%v received=%d forwarded=%d beta_query=%t", err, received.Load(), forwarded.Load(), betaQuery.Load())
	}
	if forwarded.Load() == 0 {
		t.Fatal("client did not reach the isolated probe")
	}
	t.Logf("local native client accepted: received=%d forwarded=%d beta_query=%t; no official calls", received.Load(), forwarded.Load(), betaQuery.Load())
}

func runIsolatedClaude(t *testing.T, binary, serverURL, key, tools, prompt string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"--bare", "--print", "--no-session-persistence", "--tools", tools, "--setting-sources", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--model", "claude-sonnet-4-5"}
	if tools != "" {
		args = append(args, "--allowedTools", tools, "--permission-mode", "dontAsk")
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = t.TempDir()
	overrides := map[string]string{
		"ANTHROPIC_AUTH_TOKEN":                     "local-auth-sentinel",
		"ANTHROPIC_API_KEY":                        "",
		"CLAUDE_CODE_OAUTH_TOKEN":                  "",
		"ANTHROPIC_BASE_URL":                       serverURL,
		"ANTHROPIC_CUSTOM_HEADERS":                 "X-Relay-Key: " + key,
		"CLAUDE_CONFIG_DIR":                        t.TempDir(),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"DISABLE_AUTOUPDATER":                      "1",
		"CLAUDE_CODE_MAX_RETRIES":                  "0",
	}
	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		if _, replace := overrides[strings.ToUpper(name)]; !replace {
			cmd.Env = append(cmd.Env, env)
		}
	}
	for name, value := range overrides {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	// Discard the client text: only nonsecret boolean/count evidence is needed.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run()
}

func TestClaudeClientLocalToolRoundTrip(t *testing.T) {
	binary := os.Getenv("RELAY_T0_CLAUDE_BINARY")
	if binary == "" {
		t.Skip("set RELAY_T0_CLAUDE_BINARY for isolated native client validation")
	}
	file := filepath.Join(t.TempDir(), "tool-fixture.txt")
	if err := os.WriteFile(file, []byte("local-tool-result-sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	_, key, _ := registry.Create("tool-client-test")
	var generations atomic.Int32
	var toolResult atomic.Bool
	probe := New(registry, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer local-auth-sentinel" || r.Header.Get("X-Relay-Key") != "" {
			t.Error("tool request authentication changed")
		}
		if r.URL.Path == "/v1/messages/count_tokens" {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"input_tokens":1}`))}, nil
		}
		body, _ := io.ReadAll(r.Body)
		var request struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &request)
		for _, message := range request.Messages {
			var blocks []struct {
				Type      string          `json:"type"`
				ToolUseID string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
			}
			_ = json.Unmarshal(message.Content, &blocks)
			for _, block := range blocks {
				if block.Type == "tool_result" && block.ToolUseID == "tool-local" && strings.Contains(string(block.Content), "local-tool-result-sentinel") {
					toolResult.Store(true)
				}
			}
		}
		call := generations.Add(1)
		content := map[string]any{"type": "text", "text": "OK"}
		stop := "end_turn"
		if call == 1 {
			content = map[string]any{"type": "tool_use", "id": "tool-local", "name": "Read", "input": map[string]any{}}
			stop = "tool_use"
		}
		frames := []map[string]any{
			{"type": "message_start", "message": map[string]any{"id": "msg-local-tool", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5", "content": []any{}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 0}}},
			{"type": "content_block_start", "index": 0, "content_block": content},
		}
		if call == 1 {
			input, _ := json.Marshal(map[string]string{"file_path": file})
			frames = append(frames, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
		}
		frames = append(frames,
			map[string]any{"type": "content_block_stop", "index": 0},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1}},
			map[string]any{"type": "message_stop"},
		)
		var response strings.Builder
		for _, frame := range frames {
			data, _ := json.Marshal(frame)
			response.WriteString("event: " + frame["type"].(string) + "\ndata: " + string(data) + "\n\n")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response.String()))}, nil
	}), nil, "", WithAttemptLimit(4))
	server := httptest.NewServer(probe)
	defer server.Close()
	if err := runIsolatedClaude(t, binary, server.URL, key, "Read", "Read the local fixture as requested and reply OK"); err != nil {
		t.Fatalf("isolated tool client failed: %v; generations=%d tool_result=%t", err, generations.Load(), toolResult.Load())
	}
	// The native client may make an additional auxiliary generation. The
	// contract is the observed Read result returning through Messages, with
	// every native send still bounded by the shared probe allowance.
	if generations.Load() < 2 || generations.Load() > 4 || !toolResult.Load() {
		t.Fatalf("tool round trip incomplete: generations=%d tool_result=%t", generations.Load(), toolResult.Load())
	}
	t.Logf("local native Read tool round trip: generations=%d tool_result=true; no official calls", generations.Load())
}
