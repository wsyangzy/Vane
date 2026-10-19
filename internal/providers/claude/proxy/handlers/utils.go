package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	proxyresponse "ai-relay-forwarder/internal/providers/claude/proxy/response"
)

const (
	// 上游错误响应体最大读取长度
	maxUpstreamErrorBodyBytes = 64 * 1024
)

// EndpointError 携带提供商原始响应信息的错误类型。
// 当端点返回 4xx/5xx 错误时，保留原始状态码、响应体和 Content-Type，
// 以便在所有重试耗尽后将提供商的原始错误透传给客户端。
type EndpointError struct {
	StatusCode  int
	Body        []byte
	ContentType string
	Headers     http.Header
	URL         string
}

func (e *EndpointError) Error() string {
	return fmt.Sprintf("endpoint returned error: %d, url: %s", e.StatusCode, e.URL)
}

// HTTPStatus exposes the status actually returned by the upstream response.
// Callers must not infer a provider status from arbitrary error text.
func (e *EndpointError) HTTPStatus() int {
	if e == nil {
		return 0
	}
	return e.StatusCode
}

func newEndpointErrorFromResponse(resp *http.Response, targetURL string) *EndpointError {
	if resp == nil {
		return &EndpointError{URL: targetURL}
	}

	bodyBytes, contentType := readUpstreamErrorBody(resp)
	return &EndpointError{
		StatusCode:  resp.StatusCode,
		Body:        bodyBytes,
		ContentType: contentType,
		Headers:     resp.Header.Clone(),
		URL:         targetURL,
	}
}

func readUpstreamErrorBody(resp *http.Response) ([]byte, string) {
	if resp == nil {
		return nil, ""
	}

	contentType := resp.Header.Get("Content-Type")
	if resp.Body == nil {
		return nil, contentType
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamErrorBodyBytes))
	if err != nil {
		slog.Warn("Failed to read upstream error response body", "status_code", resp.StatusCode, "error", err)
		return rawBody, contentType
	}

	return decodeUpstreamErrorBody(rawBody, resp.Header), contentType
}

func decodeUpstreamErrorBody(body []byte, headers http.Header) []byte {
	if len(body) == 0 {
		return body
	}

	encoding := strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding")))
	if encoding == "" || encoding == "identity" {
		return body
	}

	processor := proxyresponse.NewProcessor()
	clonedResp := &http.Response{
		Header: headers.Clone(),
		Body:   io.NopCloser(bytes.NewReader(body)),
	}

	decompressedReader, err := processor.DecompressStreamReader(clonedResp)
	if err != nil {
		slog.Warn("Failed to create upstream error decompressor", "content_encoding", encoding, "error", err)
		return body
	}
	defer decompressedReader.Close()

	decompressedBody, err := io.ReadAll(io.LimitReader(decompressedReader, maxUpstreamErrorBodyBytes))
	if err != nil {
		slog.Warn("Failed to decompress upstream error body", "content_encoding", encoding, "error", err)
		return body
	}

	return decompressedBody
}

// IsSuccessStatus 判断是否为成功状态码
// 成功状态码范围：200-399 (2xx-3xx)
func IsSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 400
}

// IsRetryableStatus 判断是否为可重试状态码
// 可重试的状态码：
// - 400, 403, 429: 特定的4xx错误可以重试
// - 401, 404, 410: 特定的4xx错误不应重试
// - 5xx: 服务器错误通常可以重试
func IsRetryableStatus(statusCode int) bool {
	switch statusCode {
	case 400, 403, 429: // 可重试的4xx
		return true
	case 401, 404, 410: // 不可重试的4xx
		return false
	default:
		return statusCode >= 500 // 5xx通常可重试
	}
}

// upstreamStreamFailure 上游 2xx 后、首字节写出前流失败（SSE error / 截断）：
// 把缓冲的原始正文连同状态、头一起装进 EndpointError，耗尽后与 HTTP 错误走同一条透传通道。
func upstreamStreamFailure(resp *http.Response, processor StreamProcessor, targetURL string) *EndpointError {
	return &EndpointError{
		StatusCode:  resp.StatusCode,
		Body:        processor.BufferedUpstreamBody(),
		ContentType: resp.Header.Get("Content-Type"),
		Headers:     resp.Header.Clone(),
		URL:         targetURL,
	}
}

// UpstreamErrorHeaders 返回上游错误响应的原始响应头；err 不携带上游响应时返回 nil。
// 调用方落库前必须经 tracking.DiagnosticHeaders 过滤白名单。
func UpstreamErrorHeaders(err error) http.Header {
	var epErr *EndpointError
	if errors.As(err, &epErr) && epErr != nil {
		return epErr.Headers
	}
	return nil
}

// GetStatusCodeFromError 只返回 HTTP 层实际提供的状态码。
// 普通 error 文本不携带可信的提供商响应，不能从其中猜测状态码。
func GetStatusCodeFromError(err error, resp *http.Response) int {
	if resp != nil {
		return resp.StatusCode
	}

	var epErr *EndpointError
	if errors.As(err, &epErr) {
		return epErr.HTTPStatus()
	}
	return 0
}

// WriteUpstreamErrorResponse 写入上游错误响应到客户端。
// 如果 err 携带提供商原始响应（EndpointError），完整透传状态码、Content-Type 和响应体，
// 即使响应体为空也不能改成 Relay 自己的状态或文案。
func WriteUpstreamErrorResponse(w http.ResponseWriter, err error, fallbackMsg string, fallbackStatusCode int) {
	var epErr *EndpointError
	if errors.As(err, &epErr) {
		for key, values := range epErr.Headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		if epErr.ContentType != "" && w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", epErr.ContentType)
		}
		// Body may have been decoded for readable tracking; stale wire metadata
		// must not describe different bytes. Other upstream headers are retained.
		w.Header().Del("Content-Encoding")
		w.Header().Del("Content-Length")
		w.Header().Del("Transfer-Encoding")
		w.Header().Del("Connection")
		w.WriteHeader(epErr.StatusCode)
		if len(epErr.Body) > 0 {
			_, _ = w.Write(epErr.Body)
		}
		return
	}
	if fallbackStatusCode <= 0 {
		fallbackStatusCode = http.StatusBadGateway
	}
	w.WriteHeader(fallbackStatusCode)
	if fallbackMsg != "" {
		_, _ = io.WriteString(w, fallbackMsg)
	}
}

// ExtractUpstreamErrorMessage 从错误中提取上游提供商的完整原始错误信息。
// HTTP 状态码单独由 EndpointError.StatusCode 传递；这里不拼接 Relay 自己的文本，
// 以免把生成的 "HTTP nnn:" 前缀误当成提供商原文。
//
// 长度上限由读取侧的 maxUpstreamErrorBodyBytes 兜底，这里不再二次裁剪。
func ExtractUpstreamErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var epErr *EndpointError
	if errors.As(err, &epErr) {
		if epErr == nil {
			return ""
		}

		body := toStorableText(string(epErr.Body))
		return body
	}
	return toStorableText(err.Error())
}

// toStorableText 只做一件事：把无效 UTF-8 字节序列换成 U+FFFD。
//
// 这不是对内容的加工，而是编码层的最低要求——错误原文会写进 SQLite TEXT 列
// 并经 JSON 序列化下发，无效 UTF-8 会让写库或客户端解析直接失败。
// 合法 UTF-8 的输入在这里原样返回，一个字节都不动。
func toStorableText(s string) string {
	if s == "" || utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}
