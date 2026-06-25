package handlers

import (
	"net/http/httptest"
	"testing"

	"ai-relay-forwarder/internal/privacy"
)

type staticPrivacyRedactor struct {
	output []byte
}

func (r staticPrivacyRedactor) Redact(body []byte) ([]byte, privacy.RedactionSummary, bool, error) {
	return r.output, privacy.RedactionSummary{Changed: true}, true, nil
}

func TestRedactBodyBeforeForwardDoesNotTouchTransportMetadata(t *testing.T) {
	req := httptest.NewRequest("POST", "https://provider.example/v1/messages?api_key=query-auth-token", nil)
	req.Header.Set("Authorization", "Bearer upstream-auth-token")
	req.Header.Set("X-Api-Key", "upstream-api-key")
	req.Header.Set("X-Custom-Auth", "custom-endpoint-auth")
	req.Header.Set("Content-Type", "application/json")

	originalURL := req.URL.String()
	originalAuthorization := req.Header.Get("Authorization")
	originalAPIKey := req.Header.Get("X-Api-Key")
	originalCustomAuth := req.Header.Get("X-Custom-Auth")

	body := []byte(`{"message":"sk-body-token"}`)
	redacted, err := redactBodyBeforeForward(
		staticPrivacyRedactor{output: []byte(`{"message":"fake_token_0001"}`)},
		body,
		nil,
	)
	if err != nil {
		t.Fatalf("redact body before forward: %v", err)
	}
	if string(redacted) != `{"message":"fake_token_0001"}` {
		t.Fatalf("unexpected redacted body: %s", redacted)
	}
	if req.URL.String() != originalURL {
		t.Fatalf("transport URL changed: %s", req.URL.String())
	}
	if req.Header.Get("Authorization") != originalAuthorization {
		t.Fatalf("Authorization header changed: %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Api-Key") != originalAPIKey {
		t.Fatalf("X-Api-Key header changed: %q", req.Header.Get("X-Api-Key"))
	}
	if req.Header.Get("X-Custom-Auth") != originalCustomAuth {
		t.Fatalf("custom auth header changed: %q", req.Header.Get("X-Custom-Auth"))
	}
}
