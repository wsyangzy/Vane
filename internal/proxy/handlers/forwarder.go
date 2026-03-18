package handlers

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andybalholm/brotli"

	"claude-request-forwarder/config"
	"claude-request-forwarder/internal/endpoint"
	"claude-request-forwarder/internal/transport"
)

// Forwarder 负责HTTP请求转发和头部处理
type Forwarder struct {
	config          *config.Config
	endpointManager *endpoint.Manager
	transport       *transport.Provider
}

// NewForwarder 创建新的Forwarder实例
func NewForwarder(cfg *config.Config, endpointManager *endpoint.Manager) *Forwarder {
	return &Forwarder{
		config:          cfg,
		endpointManager: endpointManager,
		transport:       transport.NewProvider(cfg),
	}
}

func (f *Forwarder) NewClient(timeout time.Duration) (*http.Client, error) {
	tr, err := f.transport.Default()
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
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
		Timeout:   0, // 流式请求无超时
		Transport: httpTransport,
	}

	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// 检查响应状态：保留原始响应体用于透传
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // 最多保留 64KB
		contentType := resp.Header.Get("Content-Type")
		resp.Body.Close()
		return nil, &EndpointError{
			StatusCode:  resp.StatusCode,
			Body:        bodyBytes,
			ContentType: contentType,
			URL:         targetURL,
		}
	}

	// 校验 Content-Type：拦截非 SSE 响应（CDN/网关拦截页、JSON 错误等）
	contentType := resp.Header.Get("Content-Type")
	if isNonStreamingContentType(contentType) {
		bodySnippet := readResponseBodySnippet(resp, 512)
		resp.Body.Close()
		slog.Warn(fmt.Sprintf("⚠️ [非SSE响应] endpoint=%s content_type=%s url=%s body=%s",
			ep.Config.Name, contentType, targetURL, bodySnippet))
		return nil, fmt.Errorf("endpoint returned non-SSE response: content_type=%s, body: %s, url: %s", contentType, bodySnippet, targetURL)
	}

	return resp, nil
}

// isNonStreamingContentType 判断响应 Content-Type 是否明确不是 SSE 流。
// 空 Content-Type 视为可接受（部分提供商不设置该头部）。
func isNonStreamingContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	// 取 media-type 部分，忽略 charset 等参数
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	switch ct {
	case "text/html",
		"application/json",
		"text/plain",
		"text/xml",
		"application/xml",
		"application/problem+json":
		return true
	}
	return false
}

// readResponseBodySnippet 从 HTTP 响应中读取可读的文本片段用于诊断日志。
// 自动处理 Content-Encoding（gzip/br/deflate）解压，
// 并过滤非 UTF-8 内容和控制字符，确保日志输出安全。
func readResponseBodySnippet(resp *http.Response, maxBytes int64) string {
	if resp == nil || resp.Body == nil {
		return "(empty)"
	}

	body := resp.Body

	// 根据 Content-Encoding 包装解压 reader
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "gzip":
		gr, err := gzip.NewReader(body)
		if err != nil {
			return fmt.Sprintf("(gzip decode error: %v)", err)
		}
		defer gr.Close()
		body = io.NopCloser(gr)
	case "br":
		body = io.NopCloser(brotli.NewReader(body))
	case "deflate":
		body = io.NopCloser(flate.NewReader(body))
	}

	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return fmt.Sprintf("(read error: %v)", err)
	}
	if len(data) == 0 {
		return "(empty)"
	}

	// 非 UTF-8 内容视为二进制，不输出原始字节
	if !utf8.Valid(data) {
		return fmt.Sprintf("(binary content, %d bytes)", len(data))
	}

	snippet := string(data)
	if int64(len(data)) > maxBytes {
		snippet = snippet[:maxBytes] + "..."
	}

	// 将换行/回车替换为空格，保证日志单行输出
	snippet = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(snippet)

	return snippet
}

// CopyHeaders 复制头部逻辑
func (f *Forwarder) CopyHeaders(src *http.Request, dst *http.Request, ep *endpoint.Endpoint) {
	// List of headers to skip/remove
	skipHeaders := map[string]bool{
		"host":          true, // We'll set this based on target endpoint
		"authorization": true, // We'll add our own if configured
		"x-api-key":     true, // Remove sensitive client API keys
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
	token := f.endpointManager.GetTokenForEndpoint(ep)
	if token != "" {
		dst.Header.Set("Authorization", "Bearer "+token)
	}

	// Add or override X-Api-Key header with dynamically resolved api-key
	apiKey := f.endpointManager.GetApiKeyForEndpoint(ep)
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
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, header := range hopByHopHeaders {
		dst.Header.Del(header)
	}
}
