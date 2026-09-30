package tracking

import (
	"net/http"
	"sort"
	"strings"
)

// diagnosticHeaderAllowlist 失败响应里值得留存的上游响应头：只用于判断错误来源，
// 例如 520 是 Cloudflare 还是中转站自己给的、请求 ID、限流窗口。
//
// 刻意用白名单：响应头里可能有 Set-Cookie、鉴权回显、内部路由信息，
// 全量保存等于把上游的会话凭据写进追踪库。
var diagnosticHeaderAllowlist = []string{
	"Server",
	"Content-Type",
	"Content-Length",
	"Cf-Ray",
	"Cf-Cache-Status",
	"Via",
	"X-Request-Id",
	"Request-Id",
	"X-Oneapi-Request-Id",
	"X-Amzn-Requestid",
	"Anthropic-Organization-Id",
	"Openai-Processing-Ms",
	"Retry-After",
	"X-Ratelimit-Limit-Requests",
	"X-Ratelimit-Remaining-Requests",
	"X-Ratelimit-Reset-Requests",
	"X-Ratelimit-Limit-Tokens",
	"X-Ratelimit-Remaining-Tokens",
	"X-Ratelimit-Reset-Tokens",
	"Anthropic-Ratelimit-Requests-Remaining",
	"Anthropic-Ratelimit-Requests-Reset",
	"Anthropic-Ratelimit-Tokens-Remaining",
	"Anthropic-Ratelimit-Tokens-Reset",
	"X-Should-Retry",
}

// maxDiagnosticHeaderValue 单个头值上限：诊断用途不需要长值，防止异常上游塞入大段内容
const maxDiagnosticHeaderValue = 256

// DiagnosticHeaders 从上游响应头里提取白名单内的诊断头；没有可保存的头时返回 nil。
// 键统一为小写，便于前端展示与比对。
func DiagnosticHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string)
	for _, name := range diagnosticHeaderAllowlist {
		value := strings.TrimSpace(headers.Get(name))
		if value == "" {
			continue
		}
		if len(value) > maxDiagnosticHeaderValue {
			value = value[:maxDiagnosticHeaderValue]
		}
		out[strings.ToLower(name)] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DiagnosticHeaderNames 白名单（小写、排序），供测试与文档使用
func DiagnosticHeaderNames() []string {
	names := make([]string, 0, len(diagnosticHeaderAllowlist))
	for _, name := range diagnosticHeaderAllowlist {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	return names
}
