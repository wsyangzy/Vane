package codexproxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// passthrough 透传上游响应（含错误响应）
func (h *Handler) passthrough(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// passthroughWithBody 直接透传给定 body（用于已经读取了 body 用于错误日志的场景）
func (h *Handler) passthroughWithBody(w http.ResponseWriter, resp *http.Response, body []byte) {
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	// 重写 Content-Length（如果有）
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// readErrorBody 读取完整上游错误响应体；summary 仅用于日志，客户端收到原始 body。
func (h *Handler) readErrorBody(resp *http.Response) (body []byte, summary string) {
	if resp == nil || resp.Body == nil {
		return nil, ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Sprintf("(读取响应体失败: %v)", err)
	}
	summary = strings.TrimSpace(string(body))
	// 单行化，避免日志多行
	summary = strings.ReplaceAll(summary, "\n", " ")
	summary = strings.ReplaceAll(summary, "\r", "")
	if len(summary) > 500 {
		summary = summary[:500] + "..."
	}
	return body, summary
}

// writeError writes only the original local/transport error text. It must not
// manufacture a Relay JSON envelope around an error that did not come from upstream.
func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if message != "" {
		_, _ = io.WriteString(w, message)
	}
}

// writeInvalidStreamFailure terminates only responses rejected by stream
// validation before output. Upstream evidence is recorded separately; arbitrary
// HTML/JSON must not become the final response to a streaming client.
func (h *Handler) writeInvalidStreamFailure(w http.ResponseWriter) {
	w.Header().Del("Content-Encoding")
	w.Header().Del("Content-Length")
	w.Header().Del("Transfer-Encoding")
	w.Header().Del("Connection")
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"code\":\"invalid_stream_response\",\"message\":\"上游未返回有效的流式响应，请稍后重试。\"}}\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
