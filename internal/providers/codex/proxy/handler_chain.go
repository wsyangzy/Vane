package codexproxy

import (
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking"
)

// chainNode 端点链路节点（结构化存储）
// 序列化为 JSON 数组写入 tracking.UpdateOptions.EndpointChain，与 Claude 端
// internal/proxy.ChainNode 保持兼容（{hop, channel, credential_group, endpoint}），供前端统一解析。
type chainNode struct {
	Hop             int    `json:"hop"`
	Channel         string `json:"channel"`
	CredentialGroup string `json:"credential_group,omitempty"`
	Endpoint        string `json:"endpoint"`
	// Attempts 本跳内实际发出的请求次数（含同端点重试）；整请求累计 = retry_count + 1
	Attempts int `json:"attempts,omitempty"`
	// StartMs / DurationMs 与 Claude 端 ChainNode 同口径：距请求开始的毫秒数与本跳耗时
	StartMs    int64  `json:"start_ms"`
	DurationMs *int64 `json:"duration_ms,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	// Headers / Local 与 Claude 端 ChainNode 同口径：诊断头白名单；没拿到上游响应的本地失败
	Headers map[string]string `json:"headers,omitempty"`
	Local   bool              `json:"local,omitempty"`
}

// closeLastHop 结束最后一跳的计时（已结束的跳不重复写）
func closeLastHop(chain []chainNode, startTime time.Time) {
	if n := len(chain); n > 0 && chain[n-1].DurationMs == nil {
		duration := time.Since(startTime).Milliseconds() - chain[n-1].StartMs
		if duration < 0 {
			duration = 0
		}
		chain[n-1].DurationMs = &duration
	}
}

const maxPreOutputStreamBufferBytes = 256 * 1024

func latestCredentialGroupName(chain []chainNode) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1].CredentialGroup
}

// annotateLastChainError 在最后一跳记录失败信息。上游只回状态码不带正文时也记录，
// 并附上诊断头白名单。local 表示本跳没有拿到任何上游响应（连接失败、超时等）；
// 上游 SSE 错误事件状态码同为 0，但它来自上游，由调用方传 local=false。
func annotateLastChainError(chain *[]chainNode, statusCode int, errorText string, headers http.Header, local bool) {
	if chain == nil || len(*chain) == 0 {
		return
	}
	errorText = strings.TrimSpace(errorText)
	diagnostic := tracking.DiagnosticHeaders(headers)
	if errorText == "" && statusCode == 0 && len(diagnostic) == 0 {
		return
	}
	last := &(*chain)[len(*chain)-1]
	last.StatusCode = statusCode
	last.Error = errorText
	last.Headers = diagnostic
	last.Local = local
}

// maxChainErrorBodyBytes 链路里保存的上游错误原文上限，与 Claude 侧 maxUpstreamErrorBodyBytes 一致
const maxChainErrorBodyBytes = 64 * 1024

// chainBodyText 链路记录用的上游错误原文：完整保留（超长截断到 64KB），正文为空时回退 fallback。
// 无效 UTF-8 替换为 U+FFFD，否则写库或前端 JSON 解析会失败。
func chainBodyText(body []byte, fallback string) string {
	if len(strings.TrimSpace(string(body))) == 0 {
		return fallback
	}
	if len(body) > maxChainErrorBodyBytes {
		body = body[:maxChainErrorBodyBytes]
	}
	return strings.ToValidUTF8(string(body), "�")
}

func chainErrorText(err error, errSummary, fallback string) string {
	if err != nil {
		return err.Error()
	}
	if strings.TrimSpace(errSummary) != "" {
		return errSummary
	}
	return fallback
}
