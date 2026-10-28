package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestClaudeProfileSettlesFullCacheUsageAcrossFailover(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			settled := []routing.AttemptSettlement{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/a/") {
					w.WriteHeader(503)
					io.WriteString(w, `{"error":{"type":"api_error"},"usage":{"input_tokens":4,"output_tokens":1}}`)
					return
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"cache_read_input_tokens\":70,\"cache_creation_input_tokens\":30,\"cache_creation\":{\"ephemeral_5m_input_tokens\":10,\"ephemeral_1h_input_tokens\":20}}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":20}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"type":"message","content":[],"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":70,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}}}`)
				}
			}))
			defer server.Close()
			cfg := &config.Config{Retry: config.RetryConfig{MaxAttempts: 3, MaxTotalAttempts: 5, MaxTotalDuration: time.Second * 3}, Failover: config.FailoverConfig{Enabled: true}}
			for i, name := range []string{"a", "b"} {
				cfg.Endpoints = append(cfg.Endpoints, config.EndpointConfig{Name: name, Channel: "A", CredentialGroupID: int64(11 + i), URL: server.URL + "/" + name, Priority: i, SkipHealthCheck: true})
			}
			h, _ := profileClaudeHandler(t, cfg, 5, nil, func(s routing.AttemptSettlement) { settled = append(settled, s) })
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"messages":[{"role":"user","content":"hello"}],"max_tokens":32}`, streaming))))
			require.Equal(t, 200, rec.Code)
			require.Len(t, settled, 2)
			require.EqualValues(t, 4, settled[0].Usage.InputTokens)
			u := settled[1].Usage
			require.True(t, u.Known)
			require.True(t, u.Complete)
			require.EqualValues(t, 100, u.InputTokens)
			require.EqualValues(t, 20, u.OutputTokens)
			require.EqualValues(t, 70, u.CacheReadTokens)
			require.EqualValues(t, 10, u.CacheCreation5mTokens)
			require.EqualValues(t, 20, u.CacheCreation1hTokens)
			require.NotEqual(t, settled[0].ID, settled[1].ID)
		})
	}
}
