package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func browserFixture(t *testing.T, exchange func(context.Context, string) ([]byte, error), blocked bool) (*Manager, LoginInfo, string) {
	t.Helper()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	redirect := "http://localhost:" + fmt.Sprint(ln.Addr().(*net.TCPAddr).Port) + "/auth/callback"
	if blocked {
		t.Cleanup(func() { ln.Close() })
	} else {
		ln.Close()
	}
	m, _ := newAccountFixture(t, loginTestAdapter{spec: LoginSpec{Command: "not-an-installed-cli", BrowserLogin: func() (BrowserLogin, error) {
		return BrowserLogin{URL: "https://auth.openai.com/oauth/authorize?state=audit-state", RedirectURL: redirect, State: "audit-state", Exchange: exchange}, nil
	}}})
	login, e := m.BeginLogin("codex", "browser account")
	if e != nil {
		t.Fatal(e)
	}
	return m, login, redirect
}
func browserCredential() []byte {
	b, _ := json.Marshal(Credential{AccessToken: "browser-secret-sentinel", RefreshToken: "refresh-secret-sentinel", Subject: "browser-user"})
	return b
}
func TestBrowserLoginAutomaticCallbackPersistsBeforeReportingCompletion(t *testing.T) {
	var calls atomic.Int32
	m, login, redirect := browserFixture(t, func(_ context.Context, code string) ([]byte, error) {
		calls.Add(1)
		if code != "official-code" {
			t.Error("wrong code")
		}
		return browserCredential(), nil
	}, false)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, e := client.Get(redirect + "?state=audit-state&code=official-code")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(response.Body)
	response.Body.Close()
	client.CloseIdleConnections()
	if response.StatusCode != 200 || !strings.Contains(string(b), "登录完成") {
		t.Fatalf("callback failed: %d", response.StatusCode)
	}
	status, _ := m.LoginStatus(login.ID)
	list, e := m.List(t.Context(), "codex")
	if e != nil || status.State != "complete" || status.Account == nil || len(list) != 1 || calls.Load() != 1 {
		t.Fatalf("completion without stored account: %+v %v", status, e)
	}
	out, _ := json.Marshal(status)
	if strings.Contains(string(out), "secret-sentinel") {
		t.Fatal("token exposed in login metadata")
	}
}
func TestBrowserLoginBlockedPortAcceptsOnlyItsOwnPastedCallback(t *testing.T) {
	var calls atomic.Int32
	m, login, redirect := browserFixture(t, func(context.Context, string) ([]byte, error) { calls.Add(1); return browserCredential(), nil }, true)
	if !login.PasteCallback || login.CallbackWarning == "" {
		t.Fatal("blocked port prevented fallback")
	}
	for _, bad := range []string{
		"https://evil.invalid/auth/callback?state=audit-state&code=x",
		strings.Replace(redirect, "localhost", "evil.invalid", 1) + "?state=audit-state&code=x",
		strings.Replace(redirect, "localhost", "user@localhost", 1) + "?state=audit-state&code=x",
		redirect + "?state=other&code=x", redirect + "?state=audit-state&state=audit-state&code=x",
		redirect + "?state=audit-state&code=x&code=y", redirect + "/wrong?state=audit-state&code=x",
		redirect + "?state=audit-state&code=x#fragment",
	} {
		if e := m.CompleteLogin(login.ID, bad); e == nil {
			t.Fatalf("accepted foreign callback: %s", bad)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid callback exchanged a code")
	}
	if e := m.CompleteLogin(login.ID, redirect+"?state=audit-state&code=official-code"); e != nil {
		t.Fatal(e)
	}
	if e := m.CompleteLogin(login.ID, redirect+"?state=audit-state&code=official-code"); e == nil || calls.Load() != 1 {
		t.Fatal("code exchanged more than once")
	}
}
func TestBrowserLoginCancelDuringExchangeNeverImports(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	m, login, redirect := browserFixture(t, func(context.Context, string) ([]byte, error) {
		close(started)
		<-release
		return browserCredential(), nil
	}, true)
	done := make(chan error, 1)
	go func() { done <- m.CompleteLogin(login.ID, redirect+"?state=audit-state&code=x") }()
	<-started
	if e := m.CancelLogin(login.ID); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e == nil {
		t.Fatal("canceled exchange reported success")
	}
	list, e := m.List(t.Context(), "codex")
	status, _ := m.LoginStatus(login.ID)
	if e != nil || len(list) != 0 || status.State != "cancelled" {
		t.Fatal("canceled login saved an account")
	}
}
