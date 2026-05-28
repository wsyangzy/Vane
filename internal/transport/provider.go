package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ai-relay-forwarder/config"
)

// Provider 复用 http.Transport，避免在高频路径反复创建 Transport 导致连接复用失效与资源抖动。
//
// 说明：
// - http.Transport 适合被多个 http.Client 共享（并发安全）
// - http.Client 本身也可复用，但此处优先解决“Transport 反复创建”这一高影响问题
type Provider struct {
	mu sync.Mutex

	cfg *config.Config

	defaultKey       string
	defaultTransport *http.Transport

	streamingKey       string
	streamingTransport *http.Transport
}

func NewProvider(cfg *config.Config) *Provider {
	return &Provider{cfg: cfg}
}

func (p *Provider) Default() (*http.Transport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := buildTransportKey(p.cfg, "default")
	if p.defaultTransport != nil && p.defaultKey == key {
		return p.defaultTransport, nil
	}

	tr, err := CreateTransport(p.cfg)
	if err != nil {
		return nil, err
	}

	// 🔧 [P0.8 修复] Default Transport 也从配置读取 ResponseHeaderTimeout，
	// 不再硬编码 60s，统一配置源（避免与 Streaming 行为分裂、与 settings.go 默认值漂移）。
	// 默认 60s 适合非流式 API 请求；如需更长（如非流式长 thinking），可在配置层调整。
	// 与 Streaming 一致的回退顺序：config > 60s 兜底
	responseHeaderTimeout := time.Duration(0)
	if p.cfg != nil {
		responseHeaderTimeout = p.cfg.Streaming.DefaultResponseHeaderTimeout
	}
	if responseHeaderTimeout == 0 {
		responseHeaderTimeout = 60 * time.Second
	}
	tr.ResponseHeaderTimeout = responseHeaderTimeout

	p.defaultTransport = tr
	p.defaultKey = key
	return tr, nil
}

func (p *Provider) Streaming() (*http.Transport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := buildTransportKey(p.cfg, "streaming")
	if p.streamingTransport != nil && p.streamingKey == key {
		return p.streamingTransport, nil
	}

	tr, err := CreateTransport(p.cfg)
	if err != nil {
		return nil, err
	}

	// Streaming 优化：在 Transport 创建时一次性设置，避免每次请求都新建或修改。
	// 注意：不要把 IdleConnTimeout 设为 0（永不回收），长期运行会导致资源压力。
	tr.DisableCompression = true
	tr.WriteBufferSize = 4096
	tr.ReadBufferSize = 4096

	// 从配置中读取响应头超时时间，默认 120 秒
	// 🔧 [P2-S1 修复] 默认从 60s 提升到 120s，覆盖 Claude Opus thinking 模式典型首字节延迟
	// （部分长 prompt + 复杂工具调用场景的首字节可在 60s-100s 之间）
	responseHeaderTimeout := time.Duration(0)
	if p.cfg != nil {
		responseHeaderTimeout = p.cfg.Streaming.ResponseHeaderTimeout
	}
	if responseHeaderTimeout == 0 {
		responseHeaderTimeout = 120 * time.Second
	}
	tr.ResponseHeaderTimeout = responseHeaderTimeout

	// 保持较小的空闲连接池，避免流式场景下长期积累。
	tr.MaxIdleConns = 10
	tr.MaxIdleConnsPerHost = 2

	p.streamingTransport = tr
	p.streamingKey = key
	return tr, nil
}

func buildTransportKey(cfg *config.Config, kind string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})

	if cfg == nil {
		return hex.EncodeToString(h.Sum(nil))
	}

	// Proxy 配置变化会影响 Dial/认证等，需重建 Transport。
	proxy := cfg.Proxy
	_, _ = h.Write([]byte(fmt.Sprintf(
		"proxy:%v:%s:%s:%s:%d:%s:%s",
		proxy.Enabled,
		proxy.Type,
		proxy.URL,
		proxy.Host,
		proxy.Port,
		proxy.Username,
		proxy.Password,
	)))

	// 🔧 [P0.8] Default Transport 的 DefaultResponseHeaderTimeout 变化也需重建对应 Transport
	if kind == "default" {
		_, _ = h.Write([]byte(fmt.Sprintf(":drht:%d", cfg.Streaming.DefaultResponseHeaderTimeout)))
	}

	// Streaming 的 ResponseHeaderTimeout 变化也需重建对应 Transport。
	if kind == "streaming" {
		_, _ = h.Write([]byte(fmt.Sprintf(":rht:%d", cfg.Streaming.ResponseHeaderTimeout)))
	}

	return hex.EncodeToString(h.Sum(nil))
}
