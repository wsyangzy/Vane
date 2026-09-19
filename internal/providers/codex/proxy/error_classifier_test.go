package codexproxy

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestErrorClassifierFailsOverCapacityStreamError(t *testing.T) {
	classifier := NewErrorClassifier()
	action := classifier.Classify(nil, &UpstreamStreamError{
		Type:    "server_error",
		Code:    "server_error",
		Message: "Selected model is at capacity. Please try a different model.",
	})

	if action != ActionFailover {
		t.Fatalf("expected ActionFailover, got %v", action)
	}
}

func TestErrorClassifierFailsFastOnResponseHeaderTimeout(t *testing.T) {
	classifier := NewErrorClassifier()
	err := &url.Error{Op: "Post", URL: "https://example.com/v1/responses", Err: errors.New("http2: timeout awaiting response headers")}
	if action := classifier.Classify(nil, err); action != ActionFailFast {
		t.Fatalf("expected ActionFailFast, got %v", action)
	}
	if action := classifier.Classify(nil, errors.New("dial tcp: i/o timeout")); action != ActionRetry {
		t.Fatalf("dial timeout: expected ActionRetry, got %v", action)
	}
}

func TestRetryAfterHint(t *testing.T) {
	cases := []struct {
		header string
		text   string
		want   time.Duration
	}{
		{"", `{"error":{"message":"Codex accounts are temporarily throttled, retry after 9 seconds"}}`, 9 * time.Second},
		{"", "retry after 1 seconds", time.Second},
		{"", "Codex account usage window limit reached", 0},
		{"5", "", 5 * time.Second},
		{"", "retry after 3600 seconds", maxRetryAfterWait},
	}
	for _, c := range cases {
		resp := &http.Response{Header: http.Header{}}
		if c.header != "" {
			resp.Header.Set("Retry-After", c.header)
		}
		if got := retryAfterHint(resp, c.text); got != c.want {
			t.Fatalf("retryAfterHint(%q, %q) = %v, want %v", c.header, c.text, got, c.want)
		}
	}
}

func TestErrorClassifierFailsOverCredentialGroupErrors(t *testing.T) {
	classifier := NewErrorClassifier()
	for _, statusCode := range []int{
		http.StatusPaymentRequired,
		http.StatusTooManyRequests,
	} {
		resp := &http.Response{StatusCode: statusCode}
		if action := classifier.Classify(resp, nil); action != ActionFailover {
			t.Fatalf("status %d: expected ActionFailover, got %v", statusCode, action)
		}
	}
}
