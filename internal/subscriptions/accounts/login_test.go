package accounts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
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
