package accounts

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BrowserLogin contains provider-owned OAuth parameters. The manager only owns
// the local callback transport and account lifecycle.
type BrowserLogin struct {
	URL, RedirectURL, State string
	Exchange                func(context.Context, string) ([]byte, error)
}

func loopback(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

func callbackURL(raw, redirect, state string) (*url.URL, error) {
	u, err := url.Parse(raw)
	want, e := url.Parse(redirect)
	if err != nil || e != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" || !loopback(u.Hostname()) || u.Port() == "" || u.Port() != want.Port() || u.Path != want.Path {
		return nil, errors.New("请粘贴本次登录结束时浏览器地址栏中的完整回调 URL")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || state == "" || len(q["state"]) != 1 || q.Get("state") != state || len(q["code"]) > 1 || len(q["error"]) > 1 || (q.Get("code") == "" && q.Get("error") == "") {
		return nil, errors.New("回调与本次登录不匹配，请检查地址是否完整或重新登录")
	}
	return u, nil
}

func (m *Manager) startBrowserLogin(f *loginFlow, spec BrowserLogin, name string) error {
	u, err := url.Parse(spec.RedirectURL)
	if err != nil || u.Scheme != "http" || !loopback(u.Hostname()) || u.Port() == "" || spec.State == "" || spec.Exchange == nil {
		return errors.New("invalid_browser_login_configuration")
	}
	f.info.URL, f.info.PasteCallback = spec.URL, true
	f.complete = func(raw string) error {
		callback, err := callbackURL(raw, spec.RedirectURL, spec.State)
		if err != nil {
			return err
		}
		f.mu.Lock()
		if f.info.State != "pending" || f.claimed {
			f.mu.Unlock()
			return errors.New("这次登录已提交或结束，请等待结果或重新登录")
		}
		f.claimed = true
		f.mu.Unlock()
		if callback.Query().Get("error") != "" {
			f.fail("官方授权未完成或已取消，请重新登录")
			return errors.New("官方授权未完成或已取消，请重新登录")
		}
		ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
		defer cancel()
		rawCredential, err := spec.Exchange(ctx, callback.Query().Get("code"))
		defer clear(rawCredential)
		if err != nil {
			f.fail("授权码交换失败，请检查网络后重新登录")
			return errors.New("授权码交换失败，请检查网络后重新登录")
		}
		return m.finishLogin(f, name, rawCredential)
	}
	ln, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", u.Port()))
	if listenErr != nil {
		f.info.CallbackWarning = "本机回调端口不可用。完成浏览器授权后，请复制地址栏中的完整回调 URL，在下方完成登录。"
	}
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet || r.URL.Path != u.Path {
			http.NotFound(w, r)
			return
		}
		// The state, path and port are checked by the same code used for pasted URLs.
		if err := f.complete(spec.RedirectURL + "?" + r.URL.RawQuery); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		_, _ = w.Write([]byte("登录完成，账号已保存到 Vane。可以关闭此页面。"))
	})}
	go func() {
		defer close(f.done)
		if ln != nil {
			go func() { _ = srv.Serve(ln) }()
		}
		<-f.ctx.Done()
		// Let an automatic callback finish writing its success page before
		// releasing the server; closing it immediately can cut that response off.
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if srv.Shutdown(shutdown) != nil {
			_ = srv.Close()
		}
		if ln != nil {
			_ = ln.Close()
		}
		f.fail("登录已超时，请重新开始")
	}()
	return nil
}
