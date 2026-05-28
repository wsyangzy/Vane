package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	proxyresponse "ai-relay-forwarder/internal/proxy/response"
)

const (
	// 上游错误响应体最大读取长度
	maxUpstreamErrorBodyBytes = 64 * 1024
	// SSE 错误消息字段最大长度（注入到 message 字段前截断）
	// 避免大段 HTML/JSON 错误体导致单条 SSE 事件过大，部分客户端有事件大小限制
	maxErrorMessageBytes = 2 * 1024
	// 错误消息截断后的省略提示
	errorMessageTruncationSuffix = "...(truncated)"
)

// EndpointError 携带提供商原始响应信息的错误类型。
// 当端点返回 4xx/5xx 错误时，保留原始状态码、响应体和 Content-Type，
// 以便在所有重试耗尽后将提供商的原始错误透传给客户端。
type EndpointError struct {
	StatusCode  int
	Body        []byte
	ContentType string
	URL         string
}

func (e *EndpointError) Error() string {
	return fmt.Sprintf("endpoint returned error: %d, url: %s", e.StatusCode, e.URL)
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
		return nil, contentType
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
		// 🔧 [P1-S1 修复] 解压失败时返回明确的占位符，
		// 禁止把损坏的压缩字节注入后续 SSE/JSON 文本字段，避免客户端解析失败。
		return fmt.Appendf(nil, "[upstream error body unreadable: encoding=%s, raw_bytes=%d, decode_error=%v]",
			encoding, len(body), err)
	}
	defer decompressedReader.Close()

	decompressedBody, err := io.ReadAll(io.LimitReader(decompressedReader, maxUpstreamErrorBodyBytes))
	if err != nil {
		slog.Warn("Failed to decompress upstream error body", "content_encoding", encoding, "error", err)
		// 🔧 [P1-S1 修复] 同上：解压过程中读取失败时也返回占位符。
		return fmt.Appendf(nil, "[upstream error body decode failed: encoding=%s, raw_bytes=%d, read_error=%v]",
			encoding, len(body), err)
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

// GetStatusCodeFromError 从错误和响应中提取状态码
// 优先级：
// 1. 首先从 http.Response 获取状态码
// 2. 然后从错误信息字符串中解析状态码
// 3. 最后返回0表示网络错误或无法确定状态码的情况
func GetStatusCodeFromError(err error, resp *http.Response) int {
	// 优先从响应对象获取状态码
	if resp != nil {
		return resp.StatusCode
	}

	// 从 EndpointError 提取状态码（携带提供商原始信息）
	var epErr *EndpointError
	if errors.As(err, &epErr) {
		return epErr.StatusCode
	}

	// 如果没有响应对象，尝试从错误信息中提取状态码
	if err == nil {
		return 0
	}

	errorStr := err.Error()

	// 常见的HTTP状态码提取模式
	statusCodes := []int{400, 401, 403, 404, 405, 406, 408, 409, 410, 413, 414, 415, 416, 417, 429, 500, 501, 502, 503, 504, 505}

	for _, code := range statusCodes {
		codeStr := strconv.Itoa(code)
		if strings.Contains(errorStr, codeStr) {
			return code
		}
	}

	// 尝试匹配HTTP状态码模式 "HTTP status xxx"
	if strings.Contains(errorStr, "HTTP status") {
		parts := strings.Fields(errorStr)
		for i, part := range parts {
			if part == "status" && i+1 < len(parts) {
				if code, err := strconv.Atoi(parts[i+1]); err == nil {
					return code
				}
			}
		}
	}

	// 尝试匹配 "status code xxx" 模式
	if strings.Contains(errorStr, "status code") {
		parts := strings.Fields(errorStr)
		for i, part := range parts {
			if part == "code" && i+1 < len(parts) {
				if code, err := strconv.Atoi(parts[i+1]); err == nil {
					return code
				}
			}
		}
	}

	// 无法提取状态码，返回0表示网络错误或其他未知错误
	return 0
}

// WriteUpstreamErrorResponse 写入上游错误响应到客户端。
// 如果 err 携带提供商原始响应（EndpointError），则完整透传：原始状态码、Content-Type 和响应体。
// 对于非 EndpointError（网络超时等），回退到 http.Error 输出纯文本，使用 fallbackStatusCode。
//
// 注意：流式请求的 403 等问题已通过 sendStreamInterruptedMessage（SSE 包裹）解决，
// 此函数仅用于非流式请求的最终错误响应，直接透传提供商原始信息便于调试。
func WriteUpstreamErrorResponse(w http.ResponseWriter, err error, fallbackMsg string, fallbackStatusCode int) {
	var epErr *EndpointError
	if errors.As(err, &epErr) && len(epErr.Body) > 0 {
		if epErr.ContentType != "" {
			w.Header().Set("Content-Type", epErr.ContentType)
		}
		w.WriteHeader(epErr.StatusCode)
		w.Write(epErr.Body)
		return
	}
	http.Error(w, fallbackMsg, fallbackStatusCode)
}

// ExtractUpstreamErrorMessage 从错误中提取上游提供商的原始错误信息。
// 如果是 EndpointError 且携带原始响应体，返回响应体文本；
// 否则返回 error.Error() 原始错误描述（如网络超时信息）。
//
// 🔧 [P1-S2 修复] 截断超长消息并清洗无效 UTF-8，避免：
//   - 大段错误体导致单条 SSE 事件过大，部分客户端有事件大小限制
//   - 无效 UTF-8 字节序列（如截断的多字节字符）导致客户端 JSON 解析失败
func ExtractUpstreamErrorMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	var epErr *EndpointError
	if errors.As(err, &epErr) && len(epErr.Body) > 0 {
		return sanitizeErrorMessage(string(epErr.Body))
	}
	return sanitizeErrorMessage(err.Error())
}

// sanitizeErrorMessage 清洗错误消息以确保安全注入 JSON 字符串字段：
//  1. 替换无效 UTF-8 序列为 Unicode 替换字符 U+FFFD
//  2. 超过 maxErrorMessageBytes 时按 UTF-8 边界截断并追加省略提示
//
// 设计原则：保持调试可读性的前提下，避免污染下游 JSON/SSE 协议。
func sanitizeErrorMessage(s string) string {
	if s == "" {
		return s
	}
	// 修正无效 UTF-8：strings.ToValidUTF8 会用替换字符替换无效字节序列
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	// 限长：超出阈值时按 rune 边界截断，避免再次破坏 UTF-8
	if len(s) > maxErrorMessageBytes {
		truncated := s[:maxErrorMessageBytes]
		// 回退到最近的 rune 边界，避免半截多字节字符
		for !utf8.ValidString(truncated) && len(truncated) > 0 {
			truncated = truncated[:len(truncated)-1]
		}
		s = truncated + errorMessageTruncationSuffix
	}
	return s
}
