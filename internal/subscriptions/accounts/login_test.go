package accounts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type loginTestAdapter struct {
	testAdapter
	spec LoginSpec
}

func (a loginTestAdapter) Login() LoginSpec { return a.spec }

func TestMain(m *testing.M) {
	if CaptureLoginBrowser(os.Args[1:]) {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A child copy of this test binary stands in for an installed official CLI.
// It touches only the directory supplied through the isolated login environment.
func TestAccountLoginCLIHelper(t *testing.T) {
	if os.Getenv("RELAY_LOGIN_TEST_HELPER") != "1" {
		return
	}
	dir := os.Getenv("RELAY_LOGIN_TEST_DIR")
	if dir == "" || os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" {
		os.Exit(2)
	}
	if os.Getenv("RELAY_LOGIN_TEST_FAIL") == "1" {
		fmt.Println("private-access-secret-sentinel private-refresh-secret-sentinel")
		os.Exit(7)
	}
	if os.Getenv("RELAY_LOGIN_TEST_CAPTURE") == "1" {
		returned := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" || r.URL.Query().Get("state") != "helper-state" || r.URL.Query().Get("code") != "helper-code" {
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte("accepted"))
			w.(http.Flusher).Flush()
			close(returned)
		}))
		defer srv.Close()
		fmt.Println("https://platform.claude.com/oauth/authorize?state=helper-state")
		link := "https://claude.com/cai/oauth/authorize?" + url.Values{"redirect_uri": {srv.URL + "/callback"}, "state": {"helper-state"}}.Encode()
		if e := exec.Command(os.Getenv("BROWSER"), link).Run(); e != nil {
			os.Exit(4)
		}
		<-returned
	}
	fmt.Println("Sign in at https://auth.openai.com/device")
	fmt.Println("ABCD-EFGH")
	if os.Getenv("RELAY_LOGIN_TEST_HOLD") == "1" {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	raw, _ := json.Marshal(Credential{AccessToken: "helper-access-sentinel", RefreshToken: "helper-refresh-sentinel", Subject: "helper-user", ExpiresAt: time.Now().Add(time.Hour)})
	if os.WriteFile(filepath.Join(dir, "auth.json"), raw, 0600) != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestCLILoginCapturesActualBrowserAndForwardsOnlyMatchingCallback(t *testing.T) {
	t.Setenv("RELAY_LOGIN_TEST_HELPER", "1")
	t.Setenv("RELAY_LOGIN_TEST_CAPTURE", "1")
	exe, _ := os.Executable()
	m, _ := newAccountFixture(t, loginTestAdapter{spec: LoginSpec{Command: exe, Args: []string{"-test.run=^TestAccountLoginCLIHelper$"}, DirectoryEnv: []string{"RELAY_LOGIN_TEST_DIR"}, CredentialFile: "auth.json", AllowedHosts: []string{"claude.com", "platform.claude.com"}, CaptureBrowser: true}})
	login, e := m.BeginLogin("claude", "captured")
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	var pending LoginInfo
	for time.Now().Before(deadline) {
		pending, _ = m.LoginStatus(login.ID)
		if pending.PasteCallback {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.HasPrefix(pending.URL, "https://claude.com/") || !pending.PasteCallback || pending.PasteCode {
		t.Fatalf("actual browser URL not captured: %+v", pending)
	}
	auth, _ := url.Parse(pending.URL)
	callback := auth.Query().Get("redirect_uri")
	if e = m.CompleteLogin(login.ID, callback+"?state=foreign&code=helper-code"); e == nil {
		t.Fatal("foreign callback forwarded")
	}
	if e = m.CompleteLogin(login.ID, callback+"?state=helper-state&code=helper-code"); e != nil {
		t.Fatal(e)
	}
	for time.Now().Before(deadline) {
		status, _ := m.LoginStatus(login.ID)
		if status.State == "complete" {
			return
		}
		if status.State == "failed" {
			t.Fatal(status.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("captured login did not complete")
}

func TestCLILoginFailureReportsExitCodeWithoutRawOutput(t *testing.T) {
	t.Setenv("RELAY_LOGIN_TEST_HELPER", "1")
	t.Setenv("RELAY_LOGIN_TEST_FAIL", "1")
	exe, _ := os.Executable()
	m, _ := newAccountFixture(t, loginTestAdapter{spec: LoginSpec{Command: exe, Args: []string{"-test.run=^TestAccountLoginCLIHelper$"}, DirectoryEnv: []string{"RELAY_LOGIN_TEST_DIR"}}})
	login, e := m.BeginLogin("codex", "")
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := m.LoginStatus(login.ID)
		if status.State == "failed" {
			if strings.Contains(status.Error, "sentinel") || !strings.Contains(status.Error, "7") {
				t.Fatal("unsafe or incomplete error")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("failed CLI did not terminate")
}

func TestLoginEnvironmentUsesSelectedProxyAndIsolatesCredentials(t *testing.T) {
	env := loginEnv([]string{"HTTPS_PROXY=http://old.invalid", "all_proxy=socks5://old.invalid", "NO_PROXY=*", "CODEX_ACCESS_TOKEN=secret", "BROWSER=old", "KEEP=ok"}, LoginSpec{DirectoryEnv: []string{"CODEX_HOME"}}, "isolated", "http://selected.invalid")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "old.invalid") || strings.Contains(joined, "secret") || strings.Contains(joined, "BROWSER=") || !strings.Contains(joined, "HTTPS_PROXY=http://selected.invalid") || !strings.Contains(joined, "NO_PROXY=localhost,127.0.0.1,::1") || !strings.Contains(joined, "KEEP=ok") {
		t.Fatal("login environment not isolated")
	}
}

func TestAccountLoginUsesIsolatedCLIImportsAndCleansItsDirectory(t *testing.T) {
	t.Setenv("RELAY_LOGIN_TEST_HELPER", "1")
	t.Setenv("RELAY_LOGIN_TEST_HOLD", "1")
	t.Setenv("ANTHROPIC_API_KEY", "user-original-auth")
	t.Setenv("OPENAI_API_KEY", "user-original-key")
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	m, _ := newAccountFixture(t, loginTestAdapter{spec: LoginSpec{Command: exe, Args: []string{"-test.run=^TestAccountLoginCLIHelper$"}, DirectoryEnv: []string{"RELAY_LOGIN_TEST_DIR"}, CredentialFile: "auth.json", AllowedHosts: []string{"auth.openai.com"}}})
	login, e := m.BeginLogin("codex", "CLI account")
	if e != nil {
		t.Fatal(e)
	}
	m.loginMu.Lock()
	flow := m.logins[login.ID]
	m.loginMu.Unlock()
	waitLogin := func(state string) LoginInfo {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			v, e := m.LoginStatus(login.ID)
			if e != nil {
				t.Fatal(e)
			}
			if v.State == state && (state != "pending" || v.URL != "") {
				return v
			}
			if v.State == "failed" {
				t.Fatalf("login failed: %s", v.Error)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("login timeout")
		return LoginInfo{}
	}
	pending := waitLogin("pending")
	if pending.URL != "https://auth.openai.com/device" || pending.Code != "ABCD-EFGH" {
		t.Fatalf("official login URL/code not captured: %+v", pending)
	}
	if _, e = m.BeginLogin("claude", "second"); e == nil {
		t.Fatal("overlapping login admitted")
	}
	if e = m.CompleteLogin(login.ID, "test-code"); e != nil {
		t.Fatal(e)
	}
	complete := waitLogin("complete")
	if complete.Account == nil || complete.URL != "" || complete.Code != "" {
		t.Fatal("login completion metadata wrong")
	}
	<-flow.done
	if _, err := os.Stat(flow.directory); !os.IsNotExist(err) {
		t.Fatal("temporary credential directory remained after login")
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "user-original-auth" || os.Getenv("OPENAI_API_KEY") != "user-original-key" {
		t.Fatal("parent client environment modified")
	}
	metadata, _ := json.Marshal(complete)
	if strings.Contains(string(metadata), "-sentinel") {
		t.Fatal("CLI credential leaked")
	}
}

func TestAccountLoginCancelDoesNotImportOrLeaveCLI(t *testing.T) {
	t.Setenv("RELAY_LOGIN_TEST_HELPER", "1")
	t.Setenv("RELAY_LOGIN_TEST_HOLD", "1")
	exe, _ := os.Executable()
	m, _ := newAccountFixture(t, loginTestAdapter{spec: LoginSpec{Command: exe, Args: []string{"-test.run=^TestAccountLoginCLIHelper$"}, DirectoryEnv: []string{"RELAY_LOGIN_TEST_DIR"}, CredentialFile: "auth.json", AllowedHosts: []string{"auth.openai.com"}}})
	login, e := m.BeginLogin("claude", "cancelled")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.CancelLogin(login.ID); e != nil {
		t.Fatal(e)
	}
	m.loginMu.Lock()
	flow := m.logins[login.ID]
	m.loginMu.Unlock()
	select {
	case <-flow.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled CLI remained alive")
	}
	status, _ := m.LoginStatus(login.ID)
	if _, err := os.Stat(flow.directory); !os.IsNotExist(err) {
		t.Fatal("temporary credential directory remained after cancellation")
	}
	if status.State != "cancelled" {
		t.Fatal("cancel overwritten by late completion")
	}
	list, e := m.List(t.Context(), "claude")
	if e != nil || len(list) != 0 {
		t.Fatal("cancelled login imported account")
	}
}
