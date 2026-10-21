package accounts

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	Command                        string
	Args, DeviceArgs, DirectoryEnv []string
	CredentialFile, ProfileFile    string
	PrepareImport                  func([]byte, []byte) ([]byte, error)
	ConfigFile, ConfigContent      string
	AllowedHosts                   []string
	BrowserLogin                   func() (BrowserLogin, error)
	CaptureBrowser                 bool
	ValidateCode                   func(string, string) error
}

type LoginInfo struct {
	ID              string `json:"id"`
	Provider        string `json:"provider"`
	State           string `json:"state"`
	URL             string `json:"url"`
	Code            string `json:"code"`
	Error           string `json:"error"`
	Method          string `json:"method"`
	PasteCallback   bool   `json:"paste_callback"`
	PasteCode       bool   `json:"paste_code"`
	CallbackWarning string `json:"callback_warning"`
	Proxy           string `json:"proxy"`
	Account         *Info  `json:"account,omitempty"`
}

type loginFlow struct {
	mu        sync.Mutex
	info      LoginInfo
	ctx       context.Context
	input     io.WriteCloser
	cancel    context.CancelFunc
	done      chan struct{}
	directory string
	complete  func(string) error
	claimed   bool
}

const openedURLEnv = "VANE_LOGIN_BROWSER_FILE"

var loginBrowserExecutable = os.Executable
var loginURL = regexp.MustCompile(`https://[^\s\x1b<>"']+`)
var deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)
var loginANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// CaptureLoginBrowser runs before the desktop starts when Claude Code invokes
// Vane as its browser. Only the child process receives this private output path.
func CaptureLoginBrowser(args []string) bool {
	file := os.Getenv(openedURLEnv)
	if file == "" || len(args) != 1 || !strings.HasPrefix(args[0], "https://") {
		return false
	}
	u, err := url.Parse(args[0])
	if err != nil || u.Scheme != "https" || u.User != nil || len(args[0]) > 16384 {
		return true
	}
	_ = os.WriteFile(file, []byte(args[0]), 0600)
	return true
}

// ConfigureLoginProxy uses the same proxy as the provider HTTP client. The
// optional closer belongs to the manager's application lifetime.
func (m *Manager) ConfigureLoginProxy(address, label string, closeProxy func()) {
	m.cliProxy, m.proxyLabel, m.closeProxy = address, label, closeProxy
}

func loginEnv(env []string, spec LoginSpec, dir, proxy string) []string {
	out := []string{}
	for _, v := range env {
		key, _, _ := strings.Cut(v, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") || key == "CLAUDECODE" || strings.HasPrefix(key, "OPENAI_") || key == "CODEX_HOME" || key == "CODEX_API_KEY" || key == "CODEX_ACCESS_TOKEN" || key == "CODEX_MANAGED_CONFIG_REQUIREMENTS_PATH" || key == "BROWSER" || key == openedURLEnv || key == "HTTP_PROXY" || key == "HTTPS_PROXY" || key == "ALL_PROXY" || key == "NO_PROXY" {
			continue
		}
		out = append(out, v)
	}
	for _, key := range spec.DirectoryEnv {
		out = append(out, key+"="+dir)
	}
	out = append(out, "NO_PROXY=localhost,127.0.0.1,::1")
	if proxy != "" {
		out = append(out, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "ALL_PROXY="+proxy)
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
	if method == "" {
		method = "browser"
	}
	spec := a.Login()
	if method == "device" && len(spec.DeviceArgs) > 0 {
		spec.Args = spec.DeviceArgs
	} else if method != "browser" {
		return LoginInfo{}, errors.New("unsupported_login_method")
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	f := &loginFlow{ctx: ctx, cancel: cancel, done: make(chan struct{}), info: LoginInfo{ID: uuid.NewString(), Provider: provider, State: "pending", Method: method, Proxy: m.proxyLabel}}
	if f.info.Proxy == "" {
		f.info.Proxy = "直连"
	}
	var err error
	if method == "browser" && spec.BrowserLogin != nil {
		var browser BrowserLogin
		browser, err = spec.BrowserLogin()
		if err == nil {
			err = m.startBrowserLogin(f, browser, name)
		}
	} else {
		err = m.startCLILogin(f, spec, name)
	}
	if err != nil {
		cancel()
		return LoginInfo{}, err
	}
	// Bound terminal history without retaining CLI output or credentials.
	if len(m.logins) > 16 {
		for id, old := range m.logins {
			old.mu.Lock()
			terminal := old.info.State != "pending"
			old.mu.Unlock()
			if terminal {
				delete(m.logins, id)
			}
		}
	}
	m.logins[f.info.ID] = f
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.info, nil
}

func (f *loginFlow) fail(message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.info.State == "pending" {
		f.info.State = "failed"
		f.info.Error = message
		f.info.URL = ""
		f.info.Code = ""
		f.cancel()
	}
}
func (m *Manager) finishLogin(f *loginFlow, name string, raw []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.info.State != "pending" || f.ctx.Err() != nil {
		return errors.New("登录已取消或超时，请重新开始")
	}
	// Serialize cancellation with persistence: a canceled flow never writes an account.
	info, err := m.Import(f.ctx, f.info.Provider, name, raw)
	if err != nil {
		f.info.State = "failed"
		f.info.Error = "登录凭证或账号身份读取失败，请重新登录"
		f.cancel()
		return errors.New(f.info.Error)
	}
	f.info.State = "complete"
	f.info.URL = ""
	f.info.Code = ""
	f.info.Account = &info
	f.cancel()
	return nil
}
func allowedLoginURL(raw string, spec LoginSpec) string {
	u, e := url.Parse(strings.TrimRight(raw, ".,);"))
	if e != nil || u.Scheme != "https" || u.User != nil {
		return ""
	}
	for _, host := range spec.AllowedHosts {
		if strings.EqualFold(u.Host, host) {
			return u.String()
		}
	}
	return ""
}
func (f *loginFlow) setCLILink(raw string, spec LoginSpec, opened bool) {
	link := allowedLoginURL(raw, spec)
	if link == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.info.State != "pending" {
		return
	}
	if f.info.PasteCallback && !opened {
		return
	}
	f.info.URL = link
	if spec.CaptureBrowser {
		u, _ := url.Parse(link)
		redirect, _ := url.Parse(u.Query().Get("redirect_uri"))
		f.info.PasteCallback = opened && redirect != nil && redirect.Scheme == "http" && loopback(redirect.Hostname()) && redirect.Port() != ""
		f.info.PasteCode = !f.info.PasteCallback
	}
}
func cliFailure(line string) string {
	s := strings.ToLower(line)
	switch {
	case strings.Contains(s, "address already in use"), strings.Contains(s, "10013"), strings.Contains(s, "os error 10048"):
		return "登录回调端口不可用，请关闭其他登录窗口或改用设备码登录"
	case strings.Contains(s, "certificate"), strings.Contains(s, "tls"):
		return "官方 CLI 的 TLS 连接失败，请检查证书与代理设置"
	case strings.Contains(s, "proxy"), strings.Contains(s, "econnrefused"):
		return "官方 CLI 无法连接代理，请检查 Vane 的网络设置"
	case strings.Contains(s, "not recognized"), strings.Contains(s, "cannot find module"), strings.Contains(s, "unknown option"):
		return "官方 CLI 安装或参数不兼容，请检查 CLI 版本"
	case strings.Contains(s, "timed out"), strings.Contains(s, "timeout"), strings.Contains(s, "fetch failed"):
		return "官方 CLI 网络请求失败，请检查网络与代理设置"
	}
	return ""
}

func (m *Manager) startCLILogin(f *loginFlow, spec LoginSpec, name string) error {
	path, err := exec.LookPath(spec.Command)
	if err != nil {
		path, err = exec.LookPath(spec.Command + ".cmd")
	}
	if err != nil {
		return errors.New("请先安装对应的官方 CLI，或使用凭证文件导入")
	}
	dir, err := os.MkdirTemp("", "vane-login-")
	if err != nil {
		return errors.New("login_directory_failed")
	}
	f.directory = dir
	cleanup := func() { _ = os.RemoveAll(dir) }
	if spec.ConfigFile != "" {
		if err = os.WriteFile(filepath.Join(dir, spec.ConfigFile), []byte(spec.ConfigContent), 0600); err != nil {
			cleanup()
			return err
		}
	}
	cmd := loginCommand(f.ctx, path, spec.Args)
	cmd.Dir = dir
	cmd.Env = loginEnv(os.Environ(), spec, dir, m.cliProxy)
	opened := filepath.Join(dir, ".opened-url")
	if spec.CaptureBrowser {
		executable, e := loginBrowserExecutable()
		if e != nil {
			cleanup()
			return errors.New("login_browser_capture_failed")
		}
		cmd.Env = append(cmd.Env, "BROWSER="+executable, openedURLEnv+"="+opened)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cleanup()
		return err
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		return err
	}
	f.input = input
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		cleanup()
		return errors.New("官方 CLI 启动失败，请检查安装路径")
	}
	stopProcess, err := trackLoginProcess(cmd)
	if err != nil {
		f.cancel()
		_ = cmd.Wait()
		cleanup()
		return errors.New("official_cli_process_isolation_failed")
	}
	stopCancellation := context.AfterFunc(f.ctx, stopProcess)
	f.complete = func(raw string) error {
		f.mu.Lock()
		if f.info.State != "pending" || f.claimed {
			f.mu.Unlock()
			return errors.New("这次登录已提交或结束")
		}
		link, pasteCallback, pasteCode := f.info.URL, f.info.PasteCallback, f.info.PasteCode
		f.mu.Unlock()
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
			if !pasteCallback {
				return errors.New("请粘贴登录页面显示的授权码")
			}
			auth, _ := url.Parse(link)
			redirect := auth.Query().Get("redirect_uri")
			callback, e := callbackURL(raw, redirect, auth.Query().Get("state"))
			if e != nil {
				return e
			}
			callback.Host = netLoopbackHost(callback.Port())
			f.mu.Lock()
			if f.info.State != "pending" || f.claimed {
				f.mu.Unlock()
				return errors.New("这次登录已提交或结束")
			}
			f.claimed = true
			f.mu.Unlock()
			request, e := http.NewRequestWithContext(f.ctx, http.MethodGet, callback.String(), nil)
			if e != nil {
				return errors.New("invalid_login_callback")
			}
			client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			defer client.CloseIdleConnections()
			response, e := client.Do(request)
			if e != nil {
				return errors.New("CLI 回调提交未确认，请等待登录结果；若仍未完成，请取消后重新登录")
			}
			response.Body.Close()
			if response.StatusCode >= 400 {
				f.mu.Lock()
				f.claimed = false
				f.mu.Unlock()
				return errors.New("CLI 未接受回调，请检查是否为本次登录的地址")
			}
			return nil
		}
		if spec.CaptureBrowser && !pasteCode {
			return errors.New("请粘贴浏览器地址栏中的完整回调 URL")
		}
		if len(raw) > 2048 {
			return errors.New("授权码过长，请检查粘贴内容")
		}
		if spec.ValidateCode != nil {
			if e := spec.ValidateCode(raw, link); e != nil {
				return e
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.info.State != "pending" || f.claimed {
			return errors.New("这次登录已提交或结束")
		}
		if _, e := io.WriteString(input, raw+"\n"); e != nil {
			return errors.New("官方 CLI 已结束，请重新登录")
		}
		f.claimed = true
		return nil
	}
	var readers sync.WaitGroup
	var failure string
	read := func(r io.Reader) {
		defer readers.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			line := loginANSI.ReplaceAllString(scanner.Text(), "")
			for _, raw := range loginURL.FindAllString(line, 4) {
				if spec.CaptureBrowser {
					continue
				}
				f.setCLILink(raw, spec, false)
			}
			f.mu.Lock()
			if reason := cliFailure(line); reason != "" {
				failure = reason
			}
			if f.info.State == "pending" {
				if code := deviceCode.FindString(line); code != "" {
					f.info.Code = code
				}
			}
			f.mu.Unlock()
			if spec.CaptureBrowser {
				for _, raw := range loginURL.FindAllString(line, 4) {
					if candidate := allowedLoginURL(raw, spec); candidate != "" {
						// Give the CLI's actual browser link priority over its printed code fallback.
						go func() {
							select {
							case <-time.After(2 * time.Second):
								f.setCLILink(candidate, spec, false)
							case <-f.ctx.Done():
							}
						}()
					}
				}
			}
		}
		if scanner.Err() != nil {
			f.fail("官方 CLI 登录输出读取失败，请重新登录")
		}
	}
	readers.Add(2)
	go read(stdout)
	go read(stderr)
	if spec.CaptureBrowser {
		go func() {
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-f.ctx.Done():
					return
				case <-ticker.C:
					if b, e := os.ReadFile(opened); e == nil && len(b) > 0 && len(b) < 16384 {
						f.setCLILink(strings.TrimSpace(string(b)), spec, true)
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(f.done)
		defer func() { stopCancellation(); stopProcess(); _ = input.Close(); cleanup(); f.cancel() }()
		readers.Wait()
		waitErr := cmd.Wait()
		f.mu.Lock()
		pending := f.info.State == "pending"
		reason := failure
		f.mu.Unlock()
		if !pending {
			return
		}
		if f.ctx.Err() != nil {
			f.fail("登录已超时，请重新开始")
			return
		}
		if waitErr != nil {
			if reason == "" {
				reason = fmt.Sprintf("官方 CLI 登录失败（退出码 %d），请重新登录或导入凭证", cmd.ProcessState.ExitCode())
			}
			f.fail(reason)
			return
		}
		raw, e := os.ReadFile(filepath.Join(dir, spec.CredentialFile))
		if e != nil {
			f.fail("官方 CLI 未保存可导入凭证，请使用文件导入")
			return
		}
		defer clear(raw)
		if spec.PrepareImport != nil {
			profile, _ := os.ReadFile(filepath.Join(dir, spec.ProfileFile))
			prepared, e := spec.PrepareImport(raw, profile)
			clear(profile)
			if e != nil {
				f.fail("登录身份读取失败，请重新登录")
				return
			}
			raw = prepared
			defer clear(prepared)
		}
		_ = m.finishLogin(f, name, raw)
	}()
	return nil
}
func netLoopbackHost(port string) string { return "127.0.0.1:" + port }
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
	if code == "" || len(code) > 16384 || strings.ContainsAny(code, "\r\n\x00") {
		return errors.New("invalid_login_code")
	}
	return f.complete(code)
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
	if m.closeProxy != nil {
		defer m.closeProxy()
	}
	m.loginMu.Lock()
	var pending []*loginFlow
	for _, f := range m.logins {
		f.mu.Lock()
		if f.info.State == "pending" {
			f.info.State = "cancelled"
			f.info.URL = ""
			f.info.Code = ""
		}
		f.cancel()
		f.mu.Unlock()
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
