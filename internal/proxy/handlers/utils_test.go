package handlers

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestIsSuccessStatus(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		expected   bool
	}{
		// 成功状态码 (2xx)
		{"200 OK", 200, true},
		{"201 Created", 201, true},
		{"202 Accepted", 202, true},
		{"204 No Content", 204, true},
		{"299 边界值", 299, true},

		// 重定向状态码 (3xx) - 也被视为成功
		{"300 Multiple Choices", 300, true},
		{"301 Moved Permanently", 301, true},
		{"302 Found", 302, true},
		{"304 Not Modified", 304, true},
		{"399 边界值", 399, true},

		// 客户端错误 (4xx) - 失败
		{"400 Bad Request", 400, false},
		{"401 Unauthorized", 401, false},
		{"403 Forbidden", 403, false},
		{"404 Not Found", 404, false},
		{"429 Too Many Requests", 429, false},

		// 服务器错误 (5xx) - 失败
		{"500 Internal Server Error", 500, false},
		{"502 Bad Gateway", 502, false},
		{"503 Service Unavailable", 503, false},
		{"504 Gateway Timeout", 504, false},

		// 边界值测试
		{"199 边界值", 199, false},
		{"400 边界值", 400, false},

		// 异常值
		{"0 异常值", 0, false},
		{"100 信息状态码", 100, false},
		{"600 超出范围", 600, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsSuccessStatus(tc.statusCode)
			if result != tc.expected {
				t.Errorf("IsSuccessStatus(%d) = %v, expected %v", tc.statusCode, result, tc.expected)
			}
		})
	}
}

func TestIsRetryableStatus(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		expected   bool
	}{
		// 可重试的4xx错误
		{"400 Bad Request - 可重试", 400, true},
		{"403 Forbidden - 可重试", 403, true},
		{"429 Too Many Requests - 可重试", 429, true},

		// 不可重试的4xx错误
		{"401 Unauthorized - 不可重试", 401, false},
		{"404 Not Found - 不可重试", 404, false},
		{"410 Gone - 不可重试", 410, false},

		// 其他4xx错误（默认不可重试）
		{"402 Payment Required", 402, false},
		{"405 Method Not Allowed", 405, false},
		{"406 Not Acceptable", 406, false},
		{"408 Request Timeout", 408, false},
		{"409 Conflict", 409, false},
		{"413 Payload Too Large", 413, false},
		{"414 URI Too Long", 414, false},
		{"415 Unsupported Media Type", 415, false},
		{"416 Range Not Satisfiable", 416, false},
		{"417 Expectation Failed", 417, false},

		// 5xx错误（通常可重试）
		{"500 Internal Server Error", 500, true},
		{"501 Not Implemented", 501, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"504 Gateway Timeout", 504, true},
		{"505 HTTP Version Not Supported", 505, true},

		// 成功状态码（不需要重试）
		{"200 OK", 200, false},
		{"201 Created", 201, false},
		{"301 Moved Permanently", 301, false},

		// 边界值和异常值
		{"0 异常值", 0, false},
		{"100 信息状态码", 100, false},
		{"199 边界值", 199, false},
		{"600 超出范围", 600, true}, // 按照默认规则，>=500被视为可重试
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsRetryableStatus(tc.statusCode)
			if result != tc.expected {
				t.Errorf("IsRetryableStatus(%d) = %v, expected %v", tc.statusCode, result, tc.expected)
			}
		})
	}
}

func TestGetStatusCodeFromError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		resp     *http.Response
		expected int
	}{
		// 优先从响应对象获取状态码
		{
			name:     "从Response获取状态码",
			err:      errors.New("some error"),
			resp:     &http.Response{StatusCode: 404},
			expected: 404,
		},
		{
			name:     "Response优先级高于错误信息",
			err:      errors.New("HTTP status 500"),
			resp:     &http.Response{StatusCode: 200},
			expected: 200,
		},

		// 从错误信息中提取状态码
		{
			name:     "错误信息包含404",
			err:      errors.New("404 not found"),
			resp:     nil,
			expected: 404,
		},
		{
			name:     "错误信息包含500",
			err:      errors.New("internal server error 500"),
			resp:     nil,
			expected: 500,
		},
		{
			name:     "错误信息包含401",
			err:      errors.New("unauthorized 401"),
			resp:     nil,
			expected: 401,
		},
		{
			name:     "错误信息包含429",
			err:      errors.New("too many requests 429"),
			resp:     nil,
			expected: 429,
		},

		// HTTP状态码模式匹配
		{
			name:     "HTTP status模式",
			err:      errors.New("HTTP status 502"),
			resp:     nil,
			expected: 502,
		},
		{
			name:     "HTTP status模式 - 多字段",
			err:      errors.New("request failed with HTTP status 503 bad gateway"),
			resp:     nil,
			expected: 503,
		},

		// status code模式匹配
		{
			name:     "status code模式",
			err:      errors.New("request failed with status code 504"),
			resp:     nil,
			expected: 504,
		},
		{
			name:     "status code模式 - 多字段",
			err:      errors.New("error: status code 400 bad request"),
			resp:     nil,
			expected: 400,
		},

		// 无法提取状态码的情况
		{
			name:     "纯网络错误",
			err:      errors.New("connection refused"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "DNS错误",
			err:      errors.New("no such host"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "超时错误",
			err:      errors.New("timeout"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "无错误和响应",
			err:      nil,
			resp:     nil,
			expected: 0,
		},
		{
			name:     "空Response",
			err:      errors.New("some error"),
			resp:     nil,
			expected: 0,
		},

		// 边界情况
		{
			name:     "错误信息包含多个状态码-取第一个",
			err:      errors.New("error 404 then redirected with 301"),
			resp:     nil,
			expected: 404, // 应该匹配第一个找到的状态码
		},
		{
			name:     "错误信息包含部分匹配",
			err:      errors.New("error code 2000"), // 不是有效的HTTP状态码
			resp:     nil,
			expected: 0,
		},

		// 测试所有预定义的状态码
		{
			name:     "状态码403",
			err:      errors.New("forbidden 403"),
			resp:     nil,
			expected: 403,
		},
		{
			name:     "状态码405",
			err:      errors.New("method not allowed 405"),
			resp:     nil,
			expected: 405,
		},
		{
			name:     "状态码408",
			err:      errors.New("request timeout 408"),
			resp:     nil,
			expected: 408,
		},
		{
			name:     "状态码413",
			err:      errors.New("payload too large 413"),
			resp:     nil,
			expected: 413,
		},
		{
			name:     "状态码501",
			err:      errors.New("not implemented 501"),
			resp:     nil,
			expected: 501,
		},
		{
			name:     "状态码505",
			err:      errors.New("HTTP version not supported 505"),
			resp:     nil,
			expected: 505,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GetStatusCodeFromError(tc.err, tc.resp)
			if result != tc.expected {
				t.Errorf("GetStatusCodeFromError(%v, %v) = %d, expected %d", tc.err, tc.resp, result, tc.expected)
			}
		})
	}
}

func TestNewEndpointErrorFromResponse_DecompressesCompressedBody(t *testing.T) {
	testCases := []struct {
		name            string
		contentEncoding string
		buildBody       func(t *testing.T, payload string) []byte
	}{
		{
			name:            "gzip",
			contentEncoding: "gzip",
			buildBody: func(t *testing.T, payload string) []byte {
				t.Helper()

				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				if _, err := writer.Write([]byte(payload)); err != nil {
					t.Fatalf("gzip write failed: %v", err)
				}
				if err := writer.Close(); err != nil {
					t.Fatalf("gzip close failed: %v", err)
				}
				return compressed.Bytes()
			},
		},
		{
			name:            "brotli",
			contentEncoding: "br",
			buildBody: func(t *testing.T, payload string) []byte {
				t.Helper()

				var compressed bytes.Buffer
				writer := brotli.NewWriter(&compressed)
				if _, err := writer.Write([]byte(payload)); err != nil {
					t.Fatalf("brotli write failed: %v", err)
				}
				if err := writer.Close(); err != nil {
					t.Fatalf("brotli close failed: %v", err)
				}
				return compressed.Bytes()
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expectedBody := `{"error":{"message":"请在CC客户端中使用"}}`
			resp := &http.Response{
				StatusCode: http.StatusForbidden,
				Header: http.Header{
					"Content-Type":     []string{"application/json"},
					"Content-Encoding": []string{tc.contentEncoding},
				},
				Body: io.NopCloser(bytes.NewReader(tc.buildBody(t, expectedBody))),
			}

			err := newEndpointErrorFromResponse(resp, "https://example.com/v1/messages")

			if err.StatusCode != http.StatusForbidden {
				t.Fatalf("expected status 403, got %d", err.StatusCode)
			}
			if string(err.Body) != expectedBody {
				t.Fatalf("expected decoded body %q, got %q", expectedBody, string(err.Body))
			}
			if got := ExtractUpstreamErrorMessage(err); got != expectedBody {
				t.Fatalf("expected extracted message %q, got %q", expectedBody, got)
			}
		})
	}
}

func TestNewEndpointErrorFromResponse_FallsBackToRawBodyWhenDecompressFails(t *testing.T) {
	rawBody := []byte("not-a-valid-gzip-stream")
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header: http.Header{
			"Content-Type":     []string{"application/json"},
			"Content-Encoding": []string{"gzip"},
		},
		Body: io.NopCloser(bytes.NewReader(rawBody)),
	}

	err := newEndpointErrorFromResponse(resp, "https://example.com/v1/messages")

	// 🔧 [P1-S1 修复] 解压失败时不再回退到原始二进制字节，
	// 改为返回明确的占位符消息，避免二进制污染下游 SSE/JSON 文本字段
	bodyStr := string(err.Body)
	if !strings.HasPrefix(bodyStr, "[upstream error body unreadable:") {
		t.Fatalf("expected placeholder prefix, got %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "encoding=gzip") {
		t.Fatalf("expected placeholder to contain encoding info, got %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "raw_bytes=23") {
		t.Fatalf("expected placeholder to contain raw byte length, got %q", bodyStr)
	}
	// 占位符应通过 ExtractUpstreamErrorMessage 完整可读（不被 sanitize 破坏）
	if !strings.Contains(ExtractUpstreamErrorMessage(err), "[upstream error body unreadable:") {
		t.Fatalf("expected placeholder to remain readable via ExtractUpstreamErrorMessage, got %q", ExtractUpstreamErrorMessage(err))
	}
}

func TestWriteUpstreamErrorResponsePassesThroughEndpointError(t *testing.T) {
	recorder := httptest.NewRecorder()
	upstreamBody := []byte(`{"error":{"type":"rate_limit_error","message":"quota exceeded"}}`)
	err := &EndpointError{
		StatusCode:  http.StatusTooManyRequests,
		Body:        upstreamBody,
		ContentType: "application/json",
		URL:         "https://upstream.example/v1/messages",
	}

	WriteUpstreamErrorResponse(recorder, err, "fallback should not be used", http.StatusBadGateway)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := recorder.Body.String(); got != string(upstreamBody) {
		t.Fatalf("body = %q, want %q", got, upstreamBody)
	}
}

func TestWriteUpstreamErrorResponseFallsBackForNetworkError(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteUpstreamErrorResponse(recorder, errors.New("connection refused"), "all endpoints failed", http.StatusBadGateway)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if !strings.Contains(recorder.Body.String(), "all endpoints failed") {
		t.Fatalf("body %q does not contain fallback message", recorder.Body.String())
	}
}

func TestSendStreamInterruptedMessageUsesRetryableSSEError(t *testing.T) {
	recorder := httptest.NewRecorder()

	sendStreamInterruptedMessage(recorder, recorder, "upstream 403 body", "ignored-model")

	body := recorder.Body.String()
	if !strings.HasPrefix(body, "event: error\n") {
		t.Fatalf("body should start with SSE error event, got %q", body)
	}
	if !strings.Contains(body, "\ndata: ") {
		t.Fatalf("body should contain data line, got %q", body)
	}

	payload := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(body, "data: ", 2)[1], ""))
	var decoded struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("unmarshal SSE payload: %v, payload=%q", err, payload)
	}
	if decoded.Type != "error" {
		t.Fatalf("payload type = %q, want error", decoded.Type)
	}
	if decoded.Error.Type != "overloaded_error" {
		t.Fatalf("error type = %q, want overloaded_error", decoded.Error.Type)
	}
	if decoded.Error.Message != "upstream 403 body" {
		t.Fatalf("message = %q, want upstream 403 body", decoded.Error.Message)
	}
}

// 基准测试
func BenchmarkIsSuccessStatus(b *testing.B) {
	statusCodes := []int{200, 404, 500, 301, 429}
	for i := 0; i < b.N; i++ {
		for _, code := range statusCodes {
			IsSuccessStatus(code)
		}
	}
}

func BenchmarkIsRetryableStatus(b *testing.B) {
	statusCodes := []int{400, 401, 403, 404, 429, 500, 502, 503}
	for i := 0; i < b.N; i++ {
		for _, code := range statusCodes {
			IsRetryableStatus(code)
		}
	}
}

func BenchmarkGetStatusCodeFromError(b *testing.B) {
	testError := errors.New("HTTP status 404 not found")
	testResp := &http.Response{StatusCode: 200}

	b.Run("WithResponse", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			GetStatusCodeFromError(testError, testResp)
		}
	})

	b.Run("WithoutResponse", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			GetStatusCodeFromError(testError, nil)
		}
	})
}
