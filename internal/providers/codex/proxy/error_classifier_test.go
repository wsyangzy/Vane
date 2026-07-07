package codexproxy

import (
	"net/http"
	"testing"
)

func TestErrorClassifierFailsOverCapacityStreamError(t *testing.T) {
	classifier := NewErrorClassifier()
	action := classifier.Classify(nil, &UpstreamStreamError{
		Type:       "server_error",
		Code:       "server_error",
		Message:    "Selected model is at capacity. Please try a different model.",
		StatusCode: http.StatusServiceUnavailable,
	})

	if action != ActionFailover {
		t.Fatalf("expected ActionFailover, got %v", action)
	}
}
