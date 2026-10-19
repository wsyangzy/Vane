package accounts

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type oauthTransport func(*http.Request) (*http.Response, error)

func (f oauthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOAuthExchangeDoesNotFollowRedirectOrExposeSecrets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		signIn  bool
		success bool
	}{
		{"rotation", 200, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`, false, true},
		{"revoked", 400, `{"error":"invalid_grant","error_description":"refresh-secret"}`, true, false},
		{"network-status", 503, `refresh-secret`, false, false},
		{"redirect", 307, `refresh-secret`, false, false},
		{"invalid-token", 200, `{"access_token":"bad token"}`, false, false},
		{"invalid-rotation", 200, `{"access_token":"new-access","refresh_token":"bad\ntoken"}`, false, false},
		{"oversized", 200, strings.Repeat("x", (1<<20)+1), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: oauthTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "auth.openai.com" || r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatal("unexpected refresh destination")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://evil.invalid"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}
			result, err := Exchange(context.Background(), client, "https://auth.openai.com/oauth/token", map[string]any{"refresh_token": "refresh-secret"})
			if (err == nil) != tc.success || errors.Is(err, ErrSignInRequired) != tc.signIn {
				t.Fatalf("unexpected exchange outcome: %v", err)
			}
			if calls != 1 {
				t.Fatal("refresh followed redirect")
			}
			if err != nil && strings.Contains(err.Error(), "refresh-secret") {
				t.Fatal("refresh secret escaped")
			}
			if tc.success && (result.AccessToken != "new-access" || result.RefreshToken != "new-refresh") {
				t.Fatal("rotation discarded")
			}
		})
	}
}
