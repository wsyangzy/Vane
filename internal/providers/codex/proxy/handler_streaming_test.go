package codexproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-relay-forwarder/internal/tracking"
)

type contextCanceledAfterReadCloser struct {
	reader *strings.Reader
}

func (r *contextCanceledAfterReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		return n, context.Canceled
	}
	return n, err
}

func (r *contextCanceledAfterReadCloser) Close() error {
	return nil
}

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

func TestHandleStreamingRecordsSuccessWhenContextCanceledAfterCompletion(t *testing.T) {
	tracker, err := tracking.NewUsageTracker(&tracking.Config{
		Enabled:         true,
		DatabasePath:    filepath.Join(t.TempDir(), "usage.db"),
		BufferSize:      50,
		BatchSize:       5,
		FlushInterval:   10 * time.Millisecond,
		MaxRetry:        3,
		CleanupInterval: time.Hour,
		RetentionDays:   1,
		DefaultPricing: tracking.ModelPricing{
			Input:  1,
			Output: 1,
		},
	})
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	defer tracker.Close()

	handler := &Handler{
		tokenParser:  NewTokenParser(),
		logger:       newTestLogger(),
		usageTracker: tracker,
	}
	requestID := "req-stream-completed-cancel"
	startTime := time.Now().Add(-time.Second)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true}`))
	handler.recordStart(requestID, req, true, "anyrouter", "gpt-5.5", modelRewriteResult{})

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: &contextCanceledAfterReadCloser{reader: strings.NewReader(
			"event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.5\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3,\"total_tokens\":15}}}\n\n",
		)},
	}
	recorder := httptest.NewRecorder()

	streamErr := handler.handleStreaming(recorder, resp, requestID, startTime, "gpt-5.5", "jp-node", "anyrouter", 1)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}

	details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 1})
	if err != nil {
		t.Fatalf("query request details: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 request detail, got %d", len(details))
	}
	detail := details[0]
	if detail.Status != "completed" {
		t.Fatalf("expected completed status, got %q with failure_reason=%q", detail.Status, detail.FailureReason)
	}
	if detail.FailureReason != "" {
		t.Fatalf("expected empty failure_reason, got %q", detail.FailureReason)
	}
	if detail.InputTokens != 12 || detail.OutputTokens != 3 {
		t.Fatalf("expected parsed tokens 12/3, got %d/%d", detail.InputTokens, detail.OutputTokens)
	}
}
