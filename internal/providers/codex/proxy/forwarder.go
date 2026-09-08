// forwarder.go - Codex HTTP 转发器
// 将请求转发到指定端点，处理 URL 拼接、认证头替换和 hop-by-hop 头清理。

package codexproxy

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-relay-forwarder/internal/transport"
)

// Forwarder 执行实际的 HTTP 转发
type Forwarder struct {
	client             *http.Client
	transportProvider  *transport.Provider
	timeout            time.Duration
	useEndpointTimeout bool
	logger             *slog.Logger
}

// NewForwarder 创建转发器实例
func NewForwarder(timeout time.Duration, logger *slog.Logger, providers ...*transport.Provider) *Forwarder {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Forwarder{
		client: &http.Client{
			Timeout: timeout,
		},
		transportProvider:  firstTransportProvider(providers...),
		timeout:            timeout,
		useEndpointTimeout: true,
		logger:             logger,
	}
}

// NewStreamingForwarder 创建流式转发器（无超时）
func NewStreamingForwarder(logger *slog.Logger, providers ...*transport.Provider) *Forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Forwarder{
		client: &http.Client{
			Timeout: 0, // 流式请求不设超时
		},
		transportProvider:  firstTransportProvider(providers...),
		timeout:            0,
		useEndpointTimeout: false,
		logger:             logger,
	}
}

// UpdateLogger updates the logger used by the forwarder.
func (f *Forwarder) UpdateLogger(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	f.logger = logger
}

// Forward 将请求转发到指定端点
func (f *Forwarder) Forward(r *http.Request, body []byte, endpoint *CodexEndpoint) (*http.Response, error) {
	// 构建目标 URL
	targetURL := buildTargetURL(endpoint.BaseURL, r.URL)

	// 创建新请求
	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建转发请求失败: %w", err)
	}

	// 复制原始请求 headers
	copyHeaders(r, req)

	// 设置认证头
	if endpoint.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}

	// 设置 Host
	if u, err := url.Parse(endpoint.BaseURL); err == nil {
		req.Host = u.Host
	}

	// 移除 hop-by-hop headers
	removeHopByHopHeaders(req)

	// 执行转发
	client, err := f.clientForEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("创建转发客户端失败: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (f *Forwarder) clientForEndpoint(endpoint *CodexEndpoint) (*http.Client, error) {
	client := f.client
	if f.transportProvider != nil {
		tr, err := f.transportProvider.Default()
		if err != nil {
			return nil, err
		}
		client = &http.Client{Timeout: f.client.Timeout, Transport: tr}
	}
	if !f.useEndpointTimeout || endpoint == nil || endpoint.Timeout <= 0 || endpoint.Timeout == f.timeout {
		return client, nil
	}
	clientCopy := *client
	clientCopy.Timeout = endpoint.Timeout
	return &clientCopy, nil
}

func firstTransportProvider(providers ...*transport.Provider) *transport.Provider {
	if len(providers) > 0 && providers[0] != nil {
		return providers[0]
	}
	return nil
}

// buildTargetURL 构建目标 URL: endpoint.BaseURL + request path + query
// 智能处理路径前缀：如果请求路径以 /v1/ 开头且 baseURL 已包含 /v1，
// 则去掉请求路径中的 /v1 前缀以避免重复（如 .../v1/v1/responses）。
func buildTargetURL(baseURL string, reqURL *url.URL) string {
	base := strings.TrimRight(baseURL, "/")
	path := reqURL.Path

	// 避免路径前缀重复：如果 base 以 /v1 结尾且 path 以 /v1/ 开头，去掉 path 的 /v1
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
		path = path[3:] // "/v1/responses" → "/responses"
	}

	target := base + path
	if reqURL.RawQuery != "" {
		target += "?" + reqURL.RawQuery
	}
	return target
}

// copyHeaders 复制请求头（排除 Host, Authorization）
func copyHeaders(src, dst *http.Request) {
	skipHeaders := map[string]bool{
		"host":          true,
		"authorization": true,
	}

	for key, values := range src.Header {
		if skipHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			dst.Header.Add(key, value)
		}
	}
}

// removeHopByHopHeaders 移除 hop-by-hop headers
func removeHopByHopHeaders(req *http.Request) {
	hopByHop := []string{
		"Connection",
		"Keep-Alive",
		"Transfer-Encoding",
		"TE",
		"Trailer",
		"Upgrade",
	}
	for _, h := range hopByHop {
		req.Header.Del(h)
	}
}
