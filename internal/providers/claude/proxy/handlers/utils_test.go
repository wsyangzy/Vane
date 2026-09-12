package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/tracking"

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

// TestLocalFailureBodyFallsBackToReason 没有上游正文时，客户端只收到 Relay 的失败分类标识，不收编造的说明。
func TestLocalFailureBodyFallsBackToReason(t *testing.T) {
	if got := localFailureBody("no_supported_model_endpoint", ""); got != "no_supported_model_endpoint" {
		t.Fatalf("localFailureBody without upstream message = %q, want reason token", got)
	}
	if got := localFailureBody("endpoint_exhausted", "dial tcp: i/o timeout"); got != "dial tcp: i/o timeout" {
		t.Fatalf("localFailureBody must keep the real transport error, got %q", got)
	}

	recorder := httptest.NewRecorder()
	WriteUpstreamErrorResponse(recorder, nil, localFailureBody("no_supported_model_endpoint", ""), http.StatusBadRequest)
	if recorder.Code != http.StatusBadRequest || recorder.Body.String() != "no_supported_model_endpoint" {
		t.Fatalf("local rejection = %d %q, want non-retryable 400 with reason token", recorder.Code, recorder.Body.String())
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

		// 普通错误文本不携带可信的 HTTP 响应状态
		{
			name:     "错误信息包含404",
			err:      errors.New("404 not found"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "错误信息包含500",
			err:      errors.New("internal server error 500"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "错误信息包含401",
			err:      errors.New("unauthorized 401"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "错误信息包含429",
			err:      errors.New("too many requests 429"),
			resp:     nil,
			expected: 0,
		},

		// 文本中的 HTTP/status 标签也不作为真实响应状态
		{
			name:     "HTTP status模式",
			err:      errors.New("HTTP status 502"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "HTTP status模式 - 多字段",
			err:      errors.New("request failed with HTTP status 503 bad gateway"),
			resp:     nil,
			expected: 0,
		},

		// status code 文本不作为真实响应状态
		{
			name:     "status code模式",
			err:      errors.New("request failed with status code 504"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "status code模式 - 多字段",
			err:      errors.New("error: status code 400 bad request"),
			resp:     nil,
			expected: 0,
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
			expected: 0,
		},
		{
			name:     "错误信息包含部分匹配",
			err:      errors.New("error code 2000"), // 不是有效的HTTP状态码
			resp:     nil,
			expected: 0,
		},
		// 纯网络错误里的数字不是状态码：反代不能凭子串编造一个提供商从未返回过的码
		{
			name:     "端口号 4001 不应被读成 400",
			err:      errors.New("dial tcp 10.0.0.1:4001: connect: connection refused"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "URL 端口 8404 不应被读成 404",
			err:      errors.New(`Get "http://api.example.com:8404/v1/messages": context deadline exceeded`),
			resp:     nil,
			expected: 0,
		},

		// 测试所有预定义的状态码
		{
			name:     "状态码403",
			err:      errors.New("forbidden 403"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "状态码405",
			err:      errors.New("method not allowed 405"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "状态码408",
			err:      errors.New("request timeout 408"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "状态码413",
			err:      errors.New("payload too large 413"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "状态码501",
			err:      errors.New("not implemented 501"),
			resp:     nil,
			expected: 0,
		},
		{
			name:     "状态码505",
			err:      errors.New("HTTP version not supported 505"),
			resp:     nil,
			expected: 0,
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
			wantMessage := expectedBody
			if got := ExtractUpstreamErrorMessage(err); got != wantMessage {
				t.Fatalf("expected extracted message %q, got %q", wantMessage, got)
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

	if !bytes.Equal(err.Body, rawBody) {
		t.Fatalf("body = %q, want original bytes %q", err.Body, rawBody)
	}
	wantMessage := string(rawBody)
	if got := ExtractUpstreamErrorMessage(err); got != wantMessage {
		t.Fatalf("extracted body = %q, want %q", got, wantMessage)
	}
}

func TestWriteUpstreamErrorResponsePassesThroughEndpointError(t *testing.T) {
	recorder := httptest.NewRecorder()
	upstreamBody := []byte(`{"error":{"type":"rate_limit_error","message":"quota exceeded"}}`)
	err := &EndpointError{
		StatusCode:  http.StatusTooManyRequests,
		Body:        upstreamBody,
		ContentType: "application/json",
		Headers: http.Header{
			"Retry-After":      []string{"15"},
			"X-Upstream-Trace": []string{"trace-123"},
		},
		URL: "https://upstream.example/v1/messages",
	}

	WriteUpstreamErrorResponse(recorder, err, "fallback should not be used", http.StatusBadGateway)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := recorder.Header().Get("Retry-After"); got != "15" {
		t.Fatalf("Retry-After = %q, want 15", got)
	}
	if got := recorder.Header().Get("X-Upstream-Trace"); got != "trace-123" {
		t.Fatalf("X-Upstream-Trace = %q, want trace-123", got)
	}
	if got := recorder.Body.String(); got != string(upstreamBody) {
		t.Fatalf("body = %q, want %q", got, upstreamBody)
	}
}

func TestWriteUpstreamErrorResponseKeepsStatusWhenBodyIsEmpty(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteUpstreamErrorResponse(recorder, &EndpointError{StatusCode: http.StatusServiceUnavailable}, "relay fallback", http.StatusBadGateway)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(recorder.Body.String(), "relay fallback") {
		t.Fatalf("empty upstream body must not use relay fallback: %q", recorder.Body.String())
	}
}

func TestWriteUpstreamErrorResponseWritesNetworkErrorVerbatim(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteUpstreamErrorResponse(recorder, errors.New("connection refused"), "connection refused", http.StatusBadGateway)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if got := recorder.Body.String(); got != "connection refused" {
		t.Fatalf("body = %q, want original transport error", got)
	}
}

// TestExtractUpstreamErrorMessageDoesNotTruncate 服务商的原文不得被截断改写。
func TestExtractUpstreamErrorMessageDoesNotTruncate(t *testing.T) {
	longBody := []byte(`{"error":{"message":"` + strings.Repeat("x", 8*1024) + `"}}`)

	got := ExtractUpstreamErrorMessage(&EndpointError{StatusCode: 500, Body: longBody})

	want := string(longBody)
	if got != want {
		t.Fatalf("expected complete provider error (len %d), got len %d", len(want), len(got))
	}
	if strings.Contains(got, "truncated") {
		t.Fatalf("message should not carry a truncation suffix, got tail %q", got[len(got)-40:])
	}
}

type bufferedBodyStub struct{ body []byte }

func (s bufferedBodyStub) ProcessStreamWithRetry(context.Context, *http.Response) (*tracking.TokenUsage, string, error) {
	return nil, "", nil
}
func (s bufferedBodyStub) HasSentContent() bool         { return false }
func (s bufferedBodyStub) HasWrittenResponse() bool     { return false }
func (s bufferedBodyStub) BufferedUpstreamBody() []byte { return s.body }

// TestUpstreamStreamFailurePassesThroughRawSSEVerbatim
// 上游 200 + SSE error 先于任何输出到达：耗尽后客户端收到的必须是那次真实的 200、头和原始事件字节。
func TestUpstreamStreamFailurePassesThroughRawSSEVerbatim(t *testing.T) {
	raw := []byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Upstream-Marker": []string{"sse-200"}},
	}

	err := upstreamStreamFailure(resp, bufferedBodyStub{body: raw}, "https://upstream.example/v1/messages")
	recorder := httptest.NewRecorder()
	WriteUpstreamErrorResponse(recorder, err, "relay fallback", http.StatusBadGateway)

	if GetStatusCodeFromError(err, nil) != http.StatusOK || recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (tracking %d), want upstream 200", recorder.Code, GetStatusCodeFromError(err, nil))
	}
	if recorder.Header().Get("X-Upstream-Marker") != "sse-200" || recorder.Body.String() != string(raw) {
		t.Fatalf("expected upstream headers and raw SSE verbatim, got %q body=%q", recorder.Header(), recorder.Body.String())
	}
	if got, want := ExtractUpstreamErrorMessage(err), string(raw); got != want {
		t.Fatalf("tracking detail must preserve raw upstream body, got %q want %q", got, want)
	}
}

func TestExtractUpstreamErrorMessagePreservesRawBody(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "empty upstream body",
			err:  &EndpointError{StatusCode: http.StatusForbidden},
			want: "",
		},
		{
			name: "legacy status prefix is not duplicated",
			err:  &EndpointError{StatusCode: http.StatusForbidden, Body: []byte("HTTP 403: access denied")},
			want: "HTTP 403: access denied",
		},
		{
			name: "ordinary error keeps original text",
			err:  errors.New("dial tcp 127.0.0.1:4031: connect: connection refused"),
			want: "dial tcp 127.0.0.1:4031: connect: connection refused",
		},
		{
			name: "endpoint without a real status keeps body",
			err:  &EndpointError{Body: []byte("transport wrapper")},
			want: "transport wrapper",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractUpstreamErrorMessage(tc.err); got != tc.want {
				t.Fatalf("ExtractUpstreamErrorMessage() = %q, want %q", got, tc.want)
			}
		})
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
