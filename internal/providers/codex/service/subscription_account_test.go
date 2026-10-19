package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func codexAccountJWT(claims map[string]any) string {
	raw, _ := json.Marshal(claims)
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(raw) + ".test"
}

type accountTransport func(*http.Request) (*http.Response, error)

func (f accountTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCodexSubscriptionAccountIdentityImportAndRefresh(t *testing.T) {
	adapter := SubscriptionAccountAdapter{}
	expiry := time.Now().Add(time.Hour).Unix()
	access := codexAccountJWT(map[string]any{"exp": expiry})
	makeRaw := func(sub string) []byte {
		id := codexAccountJWT(map[string]any{"sub": sub, "email": "test@example.invalid", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "workspace", "chatgpt_plan_type": "plus"}})
		raw, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{"access_token": access, "refresh_token": "old-refresh", "id_token": id}})
		return raw
	}
	first, e := adapter.Parse(makeRaw("user-a"))
	if e != nil {
		t.Fatal(e)
	}
	second, e := adapter.Parse(makeRaw("user-b"))
	if e != nil {
		t.Fatal(e)
	}
	if first.Subject == second.Subject || first.AccountID != "workspace" || first.Plan != "plus" || first.ExpiresAt.Unix() != expiry {
		t.Fatal("user identity merged with workspace or metadata lost")
	}
	if _, e = adapter.Parse(makeRaw("")); e == nil {
		t.Fatal("workspace alone accepted as user identity")
	}
	if _, e = adapter.Parse([]byte(`{"OPENAI_API_KEY":"api-key"}`)); e == nil {
		t.Fatal("API key imported as subscription")
	}
	adapter.Client = &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://auth.openai.com/oauth/token" {
			t.Fatal("untrusted OAuth destination")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["client_id"] != "app_EMoamEEZ73f0CkXaXp7hrann" || body["refresh_token"] != "old-refresh" {
			t.Fatal("refresh request lost protocol fields")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600}`))}, nil
	})}
	next, e := adapter.Refresh(context.Background(), first)
	if e != nil {
		t.Fatal(e)
	}
	if next.RefreshToken != "rotated-refresh" || next.Subject != first.Subject || next.AccountID != first.AccountID || time.Until(next.ExpiresAt) < 59*time.Minute {
		t.Fatal("refresh identity/rotation lost")
	}
}
