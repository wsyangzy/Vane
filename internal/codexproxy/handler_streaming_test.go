package codexproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleStreamingReturnsCapacityErrorBeforeClientOutput(t *testing.T) {
	handler := &Handler{
		tokenParser: NewTokenParser(),
		logger:      newTestLogger(),
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(
			"event: response.failed\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"Selected model is at capacity. Please try a different model.\"}}}\n\n",
		)),
	}
	recorder := httptest.NewRecorder()

	streamErr := handler.handleStreaming(recorder, resp, "req-capacity", time.Now(), "gpt-5.4", "primary", "CCTQ", 1)
	if streamErr == nil {
		t.Fatal("expected capacity stream error")
	}
	if streamErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", streamErr.StatusCode)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("expected no client output before failover, got %q", recorder.Body.String())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected recorder to remain unwritten default 200, got %d", recorder.Code)
	}
}
