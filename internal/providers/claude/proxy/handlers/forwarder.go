package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/transport"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

// Forwarder 负责HTTP请求转发和头部处理
type Forwarder struct {
	config                  *config.Config
	endpointManager         *endpoint.Manager
	transport               *transport.Provider
	states                  *routing.StateTracker
	subscriptions           *subscription.Controller
	profileResponseObserver func(routing.DispatchCandidate, int, http.Header, []byte)
}

// NewForwarder 创建新的Forwarder实例
func NewForwarder(cfg *config.Config, endpointManager *endpoint.Manager) *Forwarder {
	return &Forwarder{
		config:          cfg,
		endpointManager: endpointManager,
		transport:       transport.NewProvider(cfg),
		states:          routing.NewStateTracker(),
	}
}

func (f *Forwarder) NewClient(timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 && f.config != nil {
		timeout = f.config.GlobalTimeout
	}
	tr, err := f.transport.Default()
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

// ForwardRequestToEndpoint 转发请求到指定端点
func (f *Forwarder) ForwardRequestToEndpoint(ctx context.Context, r *http.Request, bodyBytes []byte, ep *endpoint.Endpoint) (*http.Response, error) {
	// 创建目标URL
	targetURL := ep.Config.URL + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 复制和修改头部
	f.CopyHeaders(r, req, ep)

	httpTransport, err := f.transport.Streaming()
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	client := &http.Client{
		Timeout:       0, // 流式请求无超时
		Transport:     httpTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 执行请求
	resp, err := f.sendClaude(ctx, r, req, ep, bodyBytes, client)
	if err != nil {
		return nil, err
	}

	// 检查响应状态：保留原始响应体用于透传
	if resp.StatusCode >= 400 {
		return nil, newEndpointErrorFromResponse(resp, targetURL)
	}

	return resp, nil
}

// CopyHeaders 复制头部逻辑
func (f *Forwarder) CopyHeaders(src *http.Request, dst *http.Request, ep *endpoint.Endpoint) {
	// List of headers to skip/remove
	skipHeaders := map[string]bool{
		"host":          true, // We'll set this based on target endpoint
		"authorization": true, // We'll add our own if configured
		"x-api-key":     true, // Remove sensitive client API keys
		"x-relay-key":   true,
	}

	// Copy all headers except those we want to skip
	for key, values := range src.Header {
		if skipHeaders[strings.ToLower(key)] {
			continue
		}

		for _, value := range values {
			dst.Header.Add(key, value)
		}
	}

	// Set Host header based on target endpoint URL
	if u, err := url.Parse(ep.Config.URL); err == nil {
		dst.Header.Set("Host", u.Host)
		// Also set the Host field directly on the request for proper HTTP/1.1 behavior
		dst.Host = u.Host
	}

	// Add or override Authorization header with dynamically resolved token
	token := ep.Config.Token
	if subscription.FromContext(src.Context()) == nil {
		token = f.endpointManager.GetTokenForEndpoint(ep)
	}
	if token != "" {
		dst.Header.Set("Authorization", "Bearer "+token)
	}

	// Add or override X-Api-Key header with dynamically resolved api-key
	apiKey := ep.Config.ApiKey
	if subscription.FromContext(src.Context()) == nil {
		apiKey = f.endpointManager.GetApiKeyForEndpoint(ep)
	}
	if apiKey != "" {
		dst.Header.Set("X-Api-Key", apiKey)
	}

	// Add custom headers from endpoint configuration
	for key, value := range ep.Config.Headers {
		dst.Header.Set(key, value)
	}

	// Remove hop-by-hop headers
	hopByHopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, header := range hopByHopHeaders {
		dst.Header.Del(header)
	}
	for _, value := range src.Header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			dst.Header.Del(strings.TrimSpace(name))
		}
	}
}
