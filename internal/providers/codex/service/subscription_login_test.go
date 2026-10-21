package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCodexBrowserLoginGeneratesPKCEAndExchangesAtOfficialEndpoint(t *testing.T) {
	var authorization url.Values
	a := SubscriptionAccountAdapter{Client: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://auth.openai.com/oauth/token" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("unexpected token endpoint or encoding")
		}
		if e := r.ParseForm(); e != nil {
			t.Fatal(e)
		}
		challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("code") != "browser-code" || r.Form.Get("client_id") != codexOAuthClient || r.Form.Get("redirect_uri") != authorization.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(challenge[:]) != authorization.Get("code_challenge") {
			t.Fatal("PKCE exchange mismatch")
		}
		id := codexAccountJWT(map[string]any{"sub": "user", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "workspace"}})
		access := codexAccountJWT(map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
		body, _ := json.Marshal(map[string]any{"access_token": access, "refresh_token": "refresh-sentinel", "id_token": id})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}}
	first, e := a.Login().BrowserLogin()
	if e != nil {
		t.Fatal(e)
	}
	second, e := a.Login().BrowserLogin()
	if e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(first.URL)
	if e != nil {
		t.Fatal(e)
	}
	authorization = u.Query()
	if u.Host != "auth.openai.com" || u.Path != "/oauth/authorize" || first.State == second.State || first.State == "" || first.RedirectURL != "http://localhost:1455/auth/callback" || authorization.Get("code_challenge_method") != "S256" {
		t.Fatal("invalid authorization session")
	}
	raw, e := first.Exchange(context.Background(), "browser-code")
	if e != nil {
		t.Fatal(e)
	}
	c, e := a.Parse(raw)
	if e != nil || c.Subject != "user" || c.AccountID != "workspace" {
		t.Fatal("OAuth result not importable", e)
	}
}
func TestCodexImportsNativeAndCLIProxyJSON(t *testing.T) {
	a := SubscriptionAccountAdapter{}
	id := codexAccountJWT(map[string]any{"sub": "user", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "workspace"}})
	flat, _ := json.Marshal(map[string]any{"type": "codex", "access_token": "access-token", "refresh_token": "refresh-token", "id_token": id, "account_id": "workspace"})
	c, e := a.Parse(flat)
	if e != nil || c.Subject != "user" {
		t.Fatal("flat import failed", e)
	}
	bad := strings.Replace(string(flat), `"type":"codex"`, `"type":"claude"`, 1)
	if _, e = a.Parse([]byte(bad)); e == nil {
		t.Fatal("wrong provider accepted")
	}
}
