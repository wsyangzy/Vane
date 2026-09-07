package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestRequestTurnFailureCacheKeyUsesExplicitHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-Claude-Turn-ID", "turn-1")
	if got := requestTurnFailureCacheKey(req, nil); got == "" {
		t.Fatal("expected an explicit turn header to produce a cache key")
	}
}

func TestRequestTurnFailureCacheKeyIsEmptyWithoutTurnIdentity(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	if got := requestTurnFailureCacheKey(req, []byte(`{"model":"claude"}`)); got != "" {
		t.Fatalf("expected no cache key without a turn identity, got %q", got)
	}
}
