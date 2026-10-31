// forwarder.go - Codex HTTP 转发器
// 将请求转发到指定端点，处理 URL 拼接、认证头替换和 hop-by-hop 头清理。

package codexproxy

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/transport"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

// Forwarder 执行实际的 HTTP 转发
type Forwarder struct {
	client             *http.Client
	transportProvider  *transport.Provider
	timeout            time.Duration
	useEndpointTimeout bool
	// Read the current fallback for each new request; existing clients keep
	// their timeout when settings are saved during an active response.
	defaultTimeout func() time.Duration
	// streaming 为 true 时使用 Streaming Transport，其响应头超时来自设置页
	// 「请求控制 → 流式响应头超时」。Default Transport 固定 60s，长上下文 + 高推理
	// 档位的首包经常超过它；超时后重放同一请求只会重复消耗上游额度。
	streaming bool
	logger    *slog.Logger
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
		streaming:          true,
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
	lease := leaseFromContext(r.Context())
	poolAttempt := routing.PoolAttemptFromContext(r.Context())
	handedOff := false
	defer func() {
		if !handedOff {
			lease.Finish(0, nil, nil, false)
			poolAttempt.Finish(false)
		}
	}()
	if endpoint.AuthMode == subscription.ClientPassthrough {
		if err := subscription.ValidateEndpoint(endpoint.BaseURL); err != nil {
			return nil, err
		}
	}
	// 构建目标 URL
	targetURL := buildTargetURL(endpoint.BaseURL, r.URL)
	if endpoint.AuthMode == subscription.ClientPassthrough {
		targetURL = subscription.TargetURL(r, endpoint.BaseURL, true)
	} else if _, poolCaller := routing.PoolCallerFromContext(r.Context()); subscription.FromContext(r.Context()) != nil || poolCaller || routing.ProfileRequestFromContext(r.Context()) != nil {
		copyURL := *r.URL
		copyURL.Path = subscription.Path(r)
		targetURL = buildTargetURL(endpoint.BaseURL, &copyURL)
	}

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

	if err := subscription.AuthorizeOutbound(r, req, endpoint.AuthMode, endpoint.CredentialGroupID); err != nil {
		return nil, err
	}

	// 执行转发
	client, err := f.clientForEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("创建转发客户端失败: %w", err)
	}
	// Route selection owns the destination; a redirect must not silently replay
	// the request (and its credentials) to another backend.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &copyClient
	if err := beginCodexProfileSend(r.Context()); err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		leaseFromContext(r.Context()).Finish(0, nil, nil, false)
		if r.Context().Err() == nil {
			poolAttempt.Fail(codexRouteFailure(0, nil, err), time.Time{})
		}
		if subscription.FromContext(r.Context()) != nil {
			return nil, fmt.Errorf("subscription_transport_failed")
		}
		return nil, err
	}
	if resp.Request == nil {
		resp.Request = req
	}
	if subscription.FromContext(r.Context()) != nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("subscription_redirect_rejected")
	}
	if endpoint.AuthMode == subscription.ClientPassthrough {
		observeCodexSubscriptionResponse(resp, leaseFromContext(r.Context()), subscription.Path(r))
	}

	if subscription.FromContext(r.Context()) != nil {
		if resp.StatusCode >= 400 {
			if endpoint.AuthMode != subscription.ClientPassthrough {
				if err := decodeCodexSubscriptionResponse(resp); err != nil {
					resp.Body.Close()
					return nil, err
				}
			}
			raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
			resp.Body.Close()
			if readErr != nil || len(raw) > 1<<20 {
				return nil, fmt.Errorf("subscription_error_response_unavailable")
			}
			text := subscription.Redact(r.Context(), string(raw))
			for _, name := range []string{"Authorization", "X-Api-Key"} {
				value := req.Header.Get(name)
				if value != "" {
					text = strings.ReplaceAll(text, value, "[REDACTED]")
					text = strings.ReplaceAll(text, strings.TrimPrefix(value, "Bearer "), "[REDACTED]")
				}
			}
			resp.Body = io.NopCloser(strings.NewReader(text))
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
		}
		for name := range resp.Header {
			if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "X-Relay-Key") || strings.EqualFold(name, "ChatGPT-Account-ID") {
				delete(resp.Header, name)
			}
		}
	}
	if poolAttempt != nil {
		if err := decodeCodexSubscriptionResponse(resp); err != nil {
			resp.Body.Close()
			return nil, err
		}
	}
	observeCodexPool(resp, r.Context(), subscription.Path(r))
	handedOff = true
	return resp, nil
}

func (f *Forwarder) clientForEndpoint(endpoint *CodexEndpoint) (*http.Client, error) {
	client := f.client
	timeout := client.Timeout
	if f.useEndpointTimeout && f.defaultTimeout != nil {
		if current := f.defaultTimeout(); current > 0 {
			timeout = current
		}
	}
	if f.transportProvider != nil {
		tr, err := f.transport()
		if err != nil {
			return nil, err
		}
		client = &http.Client{Timeout: timeout, Transport: tr}
	} else if timeout != client.Timeout {
		clientCopy := *client
		clientCopy.Timeout = timeout
		client = &clientCopy
	}
	if !f.useEndpointTimeout || endpoint == nil || endpoint.Timeout <= 0 || endpoint.Timeout == client.Timeout {
		return client, nil
	}
	clientCopy := *client
	clientCopy.Timeout = endpoint.Timeout
	return &clientCopy, nil
}

func (f *Forwarder) transport() (*http.Transport, error) {
	if f.streaming {
		return f.transportProvider.Streaming()
	}
	return f.transportProvider.Default()
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
		"host":               true,
		"authorization":      true,
		"x-api-key":          true,
		"x-relay-key":        true,
		"chatgpt-account-id": true,
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
	connections := req.Header.Values("Connection")
	hopByHop := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Transfer-Encoding",
		"TE",
		"Trailer",
		"Upgrade",
	}
	for _, h := range hopByHop {
		req.Header.Del(h)
	}
	for _, value := range connections {
		for _, name := range strings.Split(value, ",") {
			req.Header.Del(strings.TrimSpace(name))
		}
	}
}
