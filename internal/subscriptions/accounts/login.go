package accounts

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type LoginSpec struct {
	Command        string
	Args           []string
	DeviceArgs     []string
	DirectoryEnv   []string
	CredentialFile string
	ProfileFile    string
	PrepareImport  func([]byte, []byte) ([]byte, error)
	ConfigFile     string
	ConfigContent  string
	AllowedHosts   []string
}

type LoginInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	State    string `json:"state"`
	URL      string `json:"url"`
	Code     string `json:"code"`
	Error    string `json:"error"`
	Account  *Info  `json:"account,omitempty"`
}

type loginFlow struct {
	mu        sync.Mutex
	info      LoginInfo
	input     io.WriteCloser
	cancel    context.CancelFunc
	done      chan struct{}
	directory string
}

var loginURL = regexp.MustCompile(`https://[^\s\x1b<>"']+`)
var deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)

func loginEnv(env []string, spec LoginSpec, dir string) []string {
	out := []string{}
	for _, v := range env {
		key, _, _ := strings.Cut(v, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") || key == "CLAUDECODE" || strings.HasPrefix(key, "OPENAI_") || key == "CODEX_HOME" || key == "CODEX_API_KEY" || key == "CODEX_MANAGED_CONFIG_REQUIREMENTS_PATH" || key == "BROWSER" {
			continue
		}
		out = append(out, v)
	}
	for _, key := range spec.DirectoryEnv {
		out = append(out, key+"="+dir)
	}
	return out
}

func (m *Manager) BeginLogin(provider, name string) (LoginInfo, error) {
	return m.BeginLoginWithMethod(provider, name, "")
}

func (m *Manager) BeginLoginWithMethod(provider, name, method string) (LoginInfo, error) {
	if len(strings.TrimSpace(name)) > 256 {
		return LoginInfo{}, errors.New("account_name_too_long")
	}
	a, ok := m.adapters[provider]
	if !ok {
		return LoginInfo{}, errors.New("unsupported_subscription_provider")
	}
	spec := a.Login()
	if method == "device" && len(spec.DeviceArgs) > 0 {
		spec.Args = spec.DeviceArgs
	} else if method != "" && method != "browser" {
		return LoginInfo{}, errors.New("unsupported_login_method")
	}
	path, err := exec.LookPath(spec.Command)
	if err != nil {
		path, err = exec.LookPath(spec.Command + ".cmd")
	}
	if err != nil {
		return LoginInfo{}, errors.New("请先安装对应的官方 CLI，或使用凭证文件导入")
	}
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	for _, old := range m.logins {
		old.mu.Lock()
		running := old.info.State == "pending"
		old.mu.Unlock()
		if running {
			return LoginInfo{}, errors.New("请先完成或取消当前登录")
		}
	}
	dir, err := os.MkdirTemp("", "ai-relay-login-")
	if err != nil {
		return LoginInfo{}, errors.New("login_directory_failed")
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if spec.ConfigFile != "" {
		if err = os.WriteFile(filepath.Join(dir, spec.ConfigFile), []byte(spec.ConfigContent), 0600); err != nil {
			cleanup()
			return LoginInfo{}, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cmd := loginCommand(ctx, path, spec.Args)
	cmd.Dir = dir
	cmd.Env = loginEnv(os.Environ(), spec, dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cleanup()
		return LoginInfo{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		cleanup()
		return LoginInfo{}, err
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		cleanup()
		return LoginInfo{}, err
	}
	f := &loginFlow{info: LoginInfo{ID: uuid.NewString(), Provider: provider, State: "pending"}, input: input, cancel: cancel, done: make(chan struct{}), directory: dir}
	if err = cmd.Start(); err != nil {
		cancel()
		cleanup()
		return LoginInfo{}, errors.New("official_cli_start_failed")
	}
	stopProcess, err := trackLoginProcess(cmd)
	if err != nil {
		cancel()
		_ = cmd.Wait()
		cleanup()
		return LoginInfo{}, errors.New("official_cli_process_isolation_failed")
	}
	stopCancellation := context.AfterFunc(ctx, stopProcess)
	// Keep only a bounded set of terminal flows. Never retain CLI output or tokens.
	if len(m.logins) > 16 {
		for id, old := range m.logins {
			old.mu.Lock()
			done := old.info.State != "pending"
			old.mu.Unlock()
			if done {
				delete(m.logins, id)
			}
		}
	}
	m.logins[f.info.ID] = f
	var readers sync.WaitGroup
	read := func(r io.Reader) {
		defer readers.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			line := scanner.Text()
			f.mu.Lock()
			for _, raw := range loginURL.FindAllString(line, 4) {
				u, e := url.Parse(strings.TrimRight(raw, ".,);"))
				if e == nil && u.Scheme == "https" && u.User == nil {
					for _, host := range spec.AllowedHosts {
						if u.Host == host {
							f.info.URL = u.String()
						}
					}
				}
			}
			if code := deviceCode.FindString(line); code != "" {
				f.info.Code = code
			}
			f.mu.Unlock()
		}
	}
	readers.Add(2)
	go read(stdout)
	go read(stderr)
	go func() {
		defer close(f.done)
		defer func() { stopCancellation(); stopProcess(); cleanup(); cancel() }()
		readers.Wait()
		waitErr := cmd.Wait()
		_ = input.Close()
		f.mu.Lock()
		cancelled := f.info.State == "cancelled"
		f.mu.Unlock()
		if cancelled {
			return
		}
		if waitErr != nil {
			f.mu.Lock()
			f.info.State = "failed"
			f.info.Error = "官方 CLI 登录未完成，请重新登录或导入凭证文件"
			f.mu.Unlock()
			return
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, spec.CredentialFile))
		if readErr != nil {
			f.mu.Lock()
			f.info.State = "failed"
			f.info.Error = "官方 CLI 未写入可导入凭证，请使用文件导入"
			f.mu.Unlock()
			return
		}
		defer clear(raw)
		if spec.PrepareImport != nil {
			profile, _ := os.ReadFile(filepath.Join(dir, spec.ProfileFile))
			prepared, e := spec.PrepareImport(raw, profile)
			clear(profile)
			if e != nil {
				f.mu.Lock()
				f.info.State = "failed"
				f.info.Error = "登录身份读取失败，请重新登录"
				f.mu.Unlock()
				return
			}
			raw = prepared
			defer clear(prepared)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.info.State == "cancelled" {
			return
		}
		info, e := m.Import(context.Background(), provider, name, raw)
		if e != nil {
			f.info.State = "failed"
			f.info.Error = "登录凭证导入失败，请重新登录或导入文件"
			return
		}
		f.info.State = "complete"
		f.info.URL = ""
		f.info.Code = ""
		f.info.Account = &info
	}()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.info, nil
}

func (m *Manager) PendingLogin(provider string) *LoginInfo {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	for _, f := range m.logins {
		f.mu.Lock()
		i := f.info
		f.mu.Unlock()
		if i.Provider == provider && i.State == "pending" {
			return &i
		}
	}
	return nil
}

func (m *Manager) LoginStatus(id string) (LoginInfo, error) {
	m.loginMu.Lock()
	f := m.logins[id]
	m.loginMu.Unlock()
	if f == nil {
		return LoginInfo{}, errors.New("login_not_found")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.info, nil
}
func (m *Manager) CompleteLogin(id, code string) error {
	m.loginMu.Lock()
	f := m.logins[id]
	m.loginMu.Unlock()
	if f == nil {
		return errors.New("login_not_found")
	}
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 2048 || strings.ContainsAny(code, "\r\n\x00") {
		return errors.New("invalid_login_code")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.info.State != "pending" {
		return errors.New("login_not_pending")
	}
	_, err := io.WriteString(f.input, code+"\n")
	return err
}
func (m *Manager) CancelLogin(id string) error {
	m.loginMu.Lock()
	f := m.logins[id]
	m.loginMu.Unlock()
	if f == nil {
		return nil
	}
	f.mu.Lock()
	if f.info.State == "pending" {
		f.info.State = "cancelled"
		f.info.URL = ""
		f.info.Code = ""
		f.cancel()
	}
	f.mu.Unlock()
	return nil
}
func (m *Manager) Close() {
	m.loginMu.Lock()
	var pending []*loginFlow
	for _, f := range m.logins {
		f.cancel()
		pending = append(pending, f)
	}
	m.loginMu.Unlock()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for _, f := range pending {
		select {
		case <-f.done:
		case <-deadline.C:
			return
		}
	}
}
