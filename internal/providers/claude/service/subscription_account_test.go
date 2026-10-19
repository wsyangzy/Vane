package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type claudeAccountTransport func(*http.Request) (*http.Response, error)

func (f claudeAccountTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClaudeSubscriptionAccountOfficialProfileAndRefresh(t *testing.T) {
	raw := []byte(`{"claudeAiOauth":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":1800000000123,"scopes":["user:inference"],"subscriptionType":"max"}}`)
	prepared, e := prepareClaudeLoginImport(raw, []byte(`{"oauthAccount":{"accountUuid":"user-a","emailAddress":"test@example.invalid"}}`))
	if e != nil {
		t.Fatal(e)
	}
	adapter := SubscriptionAccountAdapter{}
	c, e := adapter.Parse(prepared)
	if e != nil {
		t.Fatal(e)
	}
	if c.Subject != "user-a" || c.ExpiresAt.UnixMilli() != 1800000000123 || c.Plan != "max" || c.Email != "test@example.invalid" {
		t.Fatal("official metadata or millisecond expiry lost")
	}
	fallback, e := adapter.Parse(raw)
	if e != nil || !strings.HasPrefix(fallback.Subject, "credential:") {
		t.Fatal("explicit import without profile not supported")
	}
	if _, e = adapter.Parse([]byte(`{"api_key":"api-key"}`)); e == nil {
		t.Fatal("API key imported as subscription")
	}
	adapter.Client = &http.Client{Transport: claudeAccountTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://platform.claude.com/v1/oauth/token" {
			t.Fatal("untrusted OAuth destination")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["client_id"] != "9d1c250a-e61b-44d9-88ed-5944d1962f5e" || body["refresh_token"] != "old-refresh" || body["scope"] != "user:inference" {
			t.Fatal("refresh protocol mismatch")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600,"scope":"user:inference user:profile"}`))}, nil
	})}
	next, e := adapter.Refresh(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	if next.Subject != c.Subject || next.RefreshToken != "rotated-refresh" || len(next.Scopes) != 2 || time.Until(next.ExpiresAt) < 59*time.Minute {
		t.Fatal("refresh rotation or identity lost")
	}
}
