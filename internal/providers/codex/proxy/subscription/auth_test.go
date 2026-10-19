package subscription

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func clientRequest() *http.Request {
	r := httptest.NewRequest("POST", "http://127.0.0.1/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-test"}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Relay-Key", "local-key-sentinel")
	r.Header.Set("Authorization", "Bearer oauth-sentinel")
	r.Header.Set("ChatGPT-Account-ID", "workspace-sentinel")
	return r
}

func TestCodexAuthRequiresIndependentLoopbackCaller(t *testing.T) {
	c, _, _ := fixture()
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"valid", func(*http.Request) {}},
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "null") }},
		{"remote", func(r *http.Request) { r.RemoteAddr = "192.0.2.1:1" }},
		{"duplicate-key", func(r *http.Request) { r.Header["x-relay-key"] = []string{"local-key-sentinel"} }},
		{"duplicate-auth", func(r *http.Request) { r.Header.Add("Authorization", "Bearer other") }},
		{"duplicate-account", func(r *http.Request) { r.Header.Add("ChatGPT-Account-ID", "other") }},
		{"api-key", func(r *http.Request) { r.Header.Set("X-Api-Key", "other") }},
		{"missing-oauth", func(r *http.Request) { r.Header.Del("Authorization") }},
		{"path-traversal", func(r *http.Request) { r.URL.Path = "/backend-api/codex/../wham/usage" }},
		{"encoded-path", func(r *http.Request) { r.URL.RawPath = "/backend-api/codex/%72esponses" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := clientRequest()
			tc.mutate(r)
			ctx, handled, e := c.Authenticate(r)
			if !handled {
				t.Fatal("not handled")
			}
			if tc.name == "valid" {
				if e != nil || FromContext(ctx) == nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	r := clientRequest()
	r.Header.Del("X-Relay-Key")
	if _, handled, _ := c.Authenticate(r); handled {
		t.Fatal("legacy request intercepted")
	}
}

func TestCodexManagedAccountReplacesClientAuthAndKeepsBackupAvailable(t *testing.T) {
	c, s, b := fixture()
	s.bindings[0].AccountID = "managed-id"
	c.SetAccountResolver(func(context.Context, string) (string, string, error) {
		return "managed-token", "managed-workspace", nil
	}, func(context.Context, string) error { return nil })
	r := clientRequest()
	r.Header.Del("Authorization")
	ctx, _, e := c.Authenticate(r)
	if e != nil {
		t.Fatal(e)
	}
	r = r.WithContext(ctx)
	dst := httptest.NewRequest("POST", OfficialBaseURL+"/responses", nil)
	dst.Host = "chatgpt.com"
	dst.Header.Set("X-Relay-Key", "never-out")
	if e = AuthorizeOutbound(r, dst, ClientPassthrough, b.GroupID); e != nil {
		t.Fatal(e)
	}
	if dst.Header.Get("Authorization") != "Bearer managed-token" || dst.Header.Get("ChatGPT-Account-ID") != "managed-workspace" || dst.Header.Get("X-Relay-Key") != "" {
		t.Fatal("wrong managed authentication")
	}
	c.SetAccountResolver(func(context.Context, string) (string, string, error) { return "", "", errors.New("disabled") }, func(context.Context, string) error { return errors.New("disabled") })
	ctx, _, e = c.Authenticate(clientRequest())
	if e != nil {
		t.Fatal("local caller must still be able to use authorized backup")
	}
	b = FromContext(ctx).Binding
	if !c.Valid(ctx, b) {
		t.Fatal("disabled account invalidated local backup authorization")
	}
	if ok, _ := c.Eligible(ctx, b, "gpt-test"); ok {
		t.Fatal("disabled managed account eligible")
	}
}

func TestCodexAuthCannotSendOfficialCredentialToThirdParty(t *testing.T) {
	c, _, b := fixture()
	r := clientRequest()
	ctx, _, e := c.Authenticate(r)
	if e != nil {
		t.Fatal(e)
	}
	r = r.WithContext(ctx)
	for _, target := range []string{"https://evil.invalid/backend-api/codex/responses", "https://chatgpt.com:443/backend-api/codex/responses", "http://chatgpt.com/backend-api/codex/responses", "https://chatgpt.com/backend-api/wham/usage", "https://user@chatgpt.com/backend-api/codex/responses"} {
		dst := httptest.NewRequest("POST", target, nil)
		if e = AuthorizeOutbound(r, dst, ClientPassthrough, b.GroupID); e == nil {
			t.Fatalf("unsafe destination accepted: %s", target)
		}
	}
	dst := httptest.NewRequest("POST", "https://third.invalid/v1/responses", nil)
	dst.Header = r.Header.Clone()
	dst.Header.Set("Authorization", "Bearer backup-key")
	if e = AuthorizeOutbound(r, dst, GroupCredential, 2); e != nil {
		t.Fatal(e)
	}
	if dst.Header.Get("X-Relay-Key") != "" || dst.Header.Get("ChatGPT-Account-ID") != "" || dst.Header.Get("Authorization") != "Bearer backup-key" {
		t.Fatal("backup credential boundary failed")
	}
}

func TestCodexQuotaOnlyExhaustedWindowsPauseAndUseSeconds(t *testing.T) {
	_, _, b := fixture()
	now := time.Unix(1800000000, 0)
	h := http.Header{}
	h.Set("X-Codex-Primary-Used-Percent", "100")
	h.Set("X-Codex-Primary-Reset-At", "1800000300")
	h.Set("X-Codex-Secondary-Used-Percent", "20")
	h.Set("X-Codex-Secondary-Reset-At", "1800500000")
	p := QuotaPauses(429, h, nil, b, "gpt-test", now)
	if len(p) != 1 || p[0].Model != "*" || !p[0].ResetKnown || !p[0].ResumeNotBefore.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("wrong reset: %+v", p)
	}
	h.Set("X-Codex-Primary-Reset-At", "1800000300000")
	p = QuotaPauses(429, h, nil, b, "gpt-test", now)
	if len(p) != 1 || p[0].ResetKnown || !p[0].ResumeNotBefore.Equal(now.Add(time.Minute)) {
		t.Fatal("milliseconds treated as epoch seconds")
	}
	if len(QuotaPauses(429, nil, []byte(`{"error":{"type":"rate_limit_error"}}`), b, "gpt-test", now)) != 0 {
		t.Fatal("ordinary 429 paused subscription")
	}
	p = QuotaPauses(429, nil, []byte(`{"error":{"type":"usage_limit_reached","resets_in_seconds":120}}`), b, "gpt-test", now)
	if len(p) != 1 || p[0].Model != "*" || !p[0].ResetKnown || !p[0].ResumeNotBefore.Equal(now.Add(2*time.Minute)) {
		t.Fatal("structured quota reset not recognized")
	}
}
