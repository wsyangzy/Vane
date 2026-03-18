package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

// WriteErrorResponse 写入错误响应到客户端。
// 如果 err 是 EndpointError 且携带原始响应体，则透传提供商原始响应（保留 Content-Type 和 Body）；
// 否则回退到 http.Error 发送纯文本错误。
func WriteErrorResponse(w http.ResponseWriter, err error, fallbackMsg string, statusCode int) {
	var epErr *EndpointError
	if errors.As(err, &epErr) && len(epErr.Body) > 0 {
		if epErr.ContentType != "" {
			w.Header().Set("Content-Type", epErr.ContentType)
		}
		w.WriteHeader(epErr.StatusCode)
		w.Write(epErr.Body)
		return
	}
	http.Error(w, fallbackMsg, statusCode)
}