package codexproxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCodexProfileSettlesEachActualAttemptWithFullUsage(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			settled := []routing.AttemptSettlement{}
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/first") {
					w.WriteHeader(503)
					io.WriteString(w, `{"error":{"code":"overloaded"},"usage":{"input_tokens":4,"output_tokens":1}}`)
					return
				}
				body := `{"id":"done","status":"completed","output":[],"service_tier":"flex","usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":70,"cache_write_tokens":5},"output_tokens_details":{"reasoning_tokens":12}}}`
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":"+body+"}\n\n")
				} else {
					io.WriteString(w, body)
				}
			}, func(s routing.AttemptSettlement) { settled = append(settled, s) })
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"input":"hello"}`, streaming))))
			require.Equal(t, 200, rec.Code)
			require.True(t, trace.Successful)
			require.Len(t, settled, 2)
			require.EqualValues(t, 4, settled[0].Usage.InputTokens)
			require.EqualValues(t, 100, settled[1].Usage.InputTokens)
			require.EqualValues(t, 70, settled[1].Usage.CacheReadTokens)
			require.EqualValues(t, 12, settled[1].Usage.ReasoningTokens)
			require.True(t, settled[1].Usage.InputIncludesCacheRead)
			require.True(t, settled[1].Usage.Complete)
			require.NotEqual(t, settled[0].ID, settled[1].ID)
			require.NotEqual(t, settled[0].Candidate.GroupID, settled[1].Candidate.GroupID)
		})
	}
}

func TestCodexIncompleteStreamKeepsObservedUsageForSettlement(t *testing.T) {
	settled := []routing.AttemptSettlement{}
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n")
	}, func(s routing.AttemptSettlement) { settled = append(settled, s) })
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true,"input":"hello"}`)))
	for _, s := range settled {
		require.False(t, s.Usage.Complete)
		if s.Usage.Known {
			require.EqualValues(t, 10, s.Usage.InputTokens)
		}
	}
	require.NotEmpty(t, settled)
}
