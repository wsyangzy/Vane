package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestClaudePrivacyRoundTripAcrossFailoverLegacyRulesAndRestorationToggle(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			store := privacy.NewStore(db)
			ctx := context.Background()
			if err := store.EnsureSchema(ctx); err != nil {
				t.Fatal(err)
			}
			svc := privacy.NewService(store)
			settings := privacy.DefaultSettings()
			settings.Mode = privacy.ModeRedact
			if err := svc.UpdateSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			term, err := svc.CreateRule(ctx, privacy.Rule{Name: "业务脱敏词", Enabled: true, MatchType: privacy.MatchTypeLiteral, Pattern: "client-secret", Action: privacy.ActionRedact, ReplacementMode: privacy.ReplacementReversible, Priority: -100})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			token := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				data, _ := io.ReadAll(r.Body)
				var body struct {
					Stream   bool `json:"stream"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
					MaxTokens int `json:"max_tokens"`
				}
				json.Unmarshal(data, &body)
				value := body.Messages[0].Content
				if value == "client-secret" || !strings.HasPrefix(value, "__REDACTED_") {
					t.Errorf("upstream received unsafe body: %s", data)
				}
				if token != "" && token != value {
					t.Error("replacement changed during failover")
				}
				token = value
				if body.Stream != stream || body.MaxTokens != 17 || r.Header.Get("User-Agent") != "claude-client" {
					t.Error("request fidelity lost")
				}
				if strings.HasPrefix(r.URL.Path, "/a/") {
					w.WriteHeader(503)
					io.WriteString(w, `{"error":{"type":"api_error"}}`)
					return
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n")
					for _, part := range []string{token[:13], token[13:]} {
						event, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": part}})
						fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", event)
						w.(http.Flusher).Flush()
					}
					io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"type": "message", "content": []any{map[string]string{"type": "text", "text": token}}, "stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
				}
			}))
			defer server.Close()
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 1, MaxTotalAttempts: 3, MaxTotalDuration: 3 * time.Second}, Failover: config.FailoverConfig{Enabled: true}, Endpoints: []config.EndpointConfig{{Name: "a", Channel: "A", CredentialGroupID: 11, URL: server.URL + "/a", Priority: 0, SkipHealthCheck: true, Timeout: time.Second}, {Name: "b", Channel: "B", CredentialGroupID: 22, URL: server.URL + "/b", Priority: 1, SkipHealthCheck: true, Timeout: time.Second}}}
			var trace routing.DispatchTrace
			h, manager := profileClaudeHandler(t, cfg, 3, func(value routing.DispatchTrace) { trace = value })
			h.SetPrivacyService(svc)
			raw := fmt.Sprintf(`{"model":"test","stream":%t,"messages":[{"role":"user","content":"client-secret"}],"max_tokens":17}`, stream)
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(raw))
			req.Header.Set("X-Session-Id", "privacy-test")
			req.Header.Set("User-Agent", "claude-client")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if calls != 2 || rec.Code != 200 || !trace.Successful || !strings.Contains(rec.Body.String(), "client-secret") || strings.Contains(rec.Body.String(), "__REDACTED_") {
				t.Fatalf("round trip: calls=%d trace=%+v response=%d %s", calls, trace, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), `"input_tokens":10`) || manager.ActiveChannelName() != "A" {
				t.Fatal("usage or global activation changed")
			}
			term.Action = privacy.ActionBlock
			if _, err := svc.UpdateRule(ctx, term.ID, term.Rule); err != nil {
				t.Fatal(err)
			}

			rec = httptest.NewRecorder()
			followup := req.Clone(ctx)
			followup.Body = io.NopCloser(strings.NewReader(raw))
			h.ServeHTTP(rec, followup)
			if rec.Code != 200 || calls != 3 || !strings.Contains(rec.Body.String(), "client-secret") || strings.Contains(rec.Body.String(), "__REDACTED_") {
				t.Fatalf("legacy blocking rule interrupted conversation: %d calls=%d %s", rec.Code, calls, rec.Body)
			}
			settings.RestoreResponses = false
			if err := svc.UpdateSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			rec = httptest.NewRecorder()
			followup = req.Clone(ctx)
			followup.Body = io.NopCloser(strings.NewReader(raw))
			h.ServeHTTP(rec, followup)
			if rec.Code != 200 || calls != 4 || strings.Contains(rec.Body.String(), "client-secret") {
				t.Fatalf("disabled restoration changed request forwarding: %d calls=%d %s", rec.Code, calls, rec.Body)
			}
			compact := strings.ReplaceAll(rec.Body.String(), "\n", "")
			if !stream && !strings.Contains(compact, token) || stream && !strings.Contains(compact, token[13:]) {
				t.Fatal("disabled restoration did not preserve upstream tokens")
			}

			settings.RestoreResponses = true
			if err := svc.UpdateSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			term.ReplacementMode = privacy.ReplacementMaskOnly
			if _, err := svc.UpdateRule(ctx, term.ID, term.Rule); err != nil {
				t.Fatal(err)
			}
			rec = httptest.NewRecorder()
			followup = req.Clone(ctx)
			followup.Body = io.NopCloser(strings.NewReader(raw))
			h.ServeHTTP(rec, followup)
			if rec.Code != 200 || calls != 5 || strings.Contains(rec.Body.String(), "client-secret") {
				t.Fatalf("per-rule masking or restoration changed forwarding: %d calls=%d %s", rec.Code, calls, rec.Body)
			}
			compact = strings.ReplaceAll(rec.Body.String(), "\n", "")
			if !stream && !strings.Contains(compact, token) || stream && !strings.Contains(compact, token[13:]) {
				t.Fatal("per-rule mask-only response was restored")
			}

		})
	}
}
