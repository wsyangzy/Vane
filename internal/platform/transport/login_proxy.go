package transport

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
)

// LoginProxy supplies CLI logins with the provider's configured exit. Bun's
// browser login accepts HTTP proxies, so a SOCKS transport gets a local CONNECT
// bridge. The browser itself still uses the user's browser network settings.
func LoginProxy(cfg *config.Config) (string, string, func(), error) {
	if cfg == nil || !cfg.Proxy.Enabled {
		return "", "直连", nil, nil
	}
	tr, err := CreateTransport(cfg)
	if err != nil {
		return "", "", nil, err
	}
	if tr.Proxy != nil {
		p, e := tr.Proxy(&http.Request{})
		if e != nil {
			return "", "", nil, e
		}
		return p.String(), "全局代理（" + p.Scheme + "://" + p.Host + "）", nil, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, err
	}
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	closed := false
	track := func(c net.Conn) bool {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			c.Close()
			return false
		}
		connections[c] = true
		return true
	}
	untrack := func(c net.Conn) { mu.Lock(); delete(connections, c); mu.Unlock(); c.Close() }
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "HTTPS CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		if _, _, e := net.SplitHostPort(r.Host); e != nil {
			http.Error(w, "invalid destination", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		upstream, e := tr.DialContext(ctx, "tcp", r.Host)
		cancel()
		if e != nil {
			http.Error(w, "proxy connection failed", http.StatusBadGateway)
			return
		}
		if !track(upstream) {
			return
		}
		defer untrack(upstream)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "unsupported connection", http.StatusInternalServerError)
			return
		}
		client, buffer, e := hijacker.Hijack()
		if e != nil {
			return
		}
		if !track(client) {
			return
		}
		defer untrack(client)
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if buffer.Flush() != nil {
			return
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, buffer); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
	})}
	go func() { _ = srv.Serve(ln) }()
	var once sync.Once
	closeBridge := func() {
		once.Do(func() {
			mu.Lock()
			closed = true
			for c := range connections {
				_ = c.Close()
			}
			mu.Unlock()
			_ = srv.Close()
			_ = ln.Close()
			tr.CloseIdleConnections()
		})
	}
	return "http://" + ln.Addr().String(), "全局代理（SOCKS5）", closeBridge, nil
}
