package codexproxy

import (
	"net/http"
	"testing"
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
