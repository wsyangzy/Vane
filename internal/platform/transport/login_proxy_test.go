package transport

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
)

func TestLoginProxyUsesDirectOrRedactedHTTPConfiguration(t *testing.T) {
	address, label, closeProxy, e := LoginProxy(&config.Config{})
	if e != nil || address != "" || label != "直连" || closeProxy != nil {
		t.Fatal("disabled proxy not direct")
	}
	cfg := &config.Config{Proxy: config.ProxyConfig{Enabled: true, Type: "http", URL: "http://127.0.0.1:7890", Username: "user-sentinel", Password: "password-sentinel"}}
	address, label, _, e = LoginProxy(cfg)
	if e != nil || !strings.Contains(address, "password-sentinel") || strings.Contains(label, "sentinel") || !strings.Contains(label, "127.0.0.1:7890") {
		t.Fatal("proxy auth lost or displayed")
	}
}

func TestLoginProxyBridgesSOCKSAndClosesActiveTunnels(t *testing.T) {
	target, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	go func() {
		c, e := target.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()
	socks, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer socks.Close()
	go func() {
		c, e := socks.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		header := make([]byte, 3)
		if _, e = io.ReadFull(c, header); e != nil {
			return
		}
		_, _ = c.Write([]byte{5, 0})
		request := make([]byte, 10)
		if _, e = io.ReadFull(c, request); e != nil {
			return
		}
		upstream, e := net.Dial("tcp", target.Addr().String())
		if e != nil {
			return
		}
		defer upstream.Close()
		_, _ = c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		go func() { _, _ = io.Copy(upstream, c); upstream.Close() }()
		_, _ = io.Copy(c, upstream)
	}()
	cfg := &config.Config{Proxy: config.ProxyConfig{Enabled: true, Type: "socks5", URL: "socks5://" + socks.Addr().String()}}
	address, _, closeBridge, e := LoginProxy(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer closeBridge()
	client, e := net.DialTimeout("tcp", strings.TrimPrefix(address, "http://"), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(client, "CONNECT "+target.Addr().String()+" HTTP/1.1\r\nHost: "+target.Addr().String()+"\r\n\r\n")
	reader := bufio.NewReader(client)
	status, e := reader.ReadString('\n')
	if e != nil || !strings.Contains(status, "200") {
		t.Fatal("CONNECT failed", e)
	}
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if line == "\r\n" {
			break
		}
	}
	_, _ = io.WriteString(client, "tunnel-sentinel\n")
	line, e := reader.ReadString('\n')
	if e != nil || line != "tunnel-sentinel\n" {
		t.Fatal("SOCKS tunnel not used", e)
	}
	closeBridge()
	if _, e = reader.ReadByte(); e == nil {
		t.Fatal("active tunnel remained after close")
	}
}
