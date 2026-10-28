package codexproxy

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

	"github.com/wsyangzy/Vane/internal/privacy"
)

func TestCodexPrivacyRoundTripAcrossProfileFailoverAndBlock(t *testing.T) {
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
			settings.RestoreResponses = true
			if err := svc.UpdateSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			term, err := svc.CreateRule(ctx, privacy.Rule{Name: "业务脱敏词", Enabled: true, MatchType: privacy.MatchTypeLiteral, Pattern: "client-secret", Action: privacy.ActionRedact, ReplacementMode: privacy.ReplacementPlaceholder, Placeholder: "[脱敏词]", Priority: -100})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			token := ""
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				data, _ := io.ReadAll(r.Body)
				var body map[string]any
				json.Unmarshal(data, &body)
				value, _ := body["input"].(string)
				if value == "client-secret" || !strings.HasPrefix(value, "__VANE_") {
					t.Errorf("upstream received unsafe body: %s", data)
				}
				if token != "" && token != value {
					t.Error("replacement changed during failover")
				}
				token = value
				if body["stream"] != stream || body["max_output_tokens"] != float64(17) || r.Header.Get("User-Agent") != "codex-client" {
					t.Error("request fidelity lost")
				}
				if strings.HasPrefix(r.URL.Path, "/first") {
					w.WriteHeader(503)
					io.WriteString(w, `{"error":{"code":"overloaded"}}`)
					return
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					for _, part := range []string{token[:13], token[13:]} {
						event, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "item_id": "msg-1", "output_index": 0, "content_index": 0, "delta": part})
						fmt.Fprintf(w, "data: %s\n\n", event)
						w.(http.Flusher).Flush()
					}
					io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"done\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"id": "done", "status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": token}}}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
				}
			})
			h.privacyService = svc
			raw := fmt.Sprintf(`{"model":"test","stream":%t,"input":"client-secret","max_output_tokens":17}`, stream)
			req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(raw))
			req.Header.Set("X-Session-Id", "privacy-test")
			req.Header.Set("User-Agent", "codex-client")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if calls != 2 || rec.Code != 200 || !trace.Successful || !strings.Contains(rec.Body.String(), "client-secret") || strings.Contains(rec.Body.String(), "__VANE_") {
				t.Fatalf("round trip: calls=%d trace=%+v response=%d %s", calls, trace, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), `"input_tokens":10`) || h.manager.GetActiveChannel().ID != 1 {
				t.Fatal("usage or global activation changed")
			}
			term.Action = privacy.ActionBlock
			if _, err := svc.UpdateRule(ctx, term.ID, term.Rule); err != nil {
				t.Fatal(err)
			}
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(raw)))
			if rec.Code != 403 || calls != 2 || strings.Contains(rec.Body.String(), "client-secret") {
				t.Fatalf("block did not short-circuit: %d calls=%d %s", rec.Code, calls, rec.Body)
			}
		})
	}
}
