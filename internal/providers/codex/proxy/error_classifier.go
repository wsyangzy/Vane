// error_classifier.go - Codex 错误分类器
// 根据 HTTP 响应状态码或网络错误，决定是同端点重试、跨端点切换还是立即失败。

package codexproxy

import (
	"errors"
	"net/http"
	"strings"
)

// ErrorAction 错误处理动作（三态）
type ErrorAction int

const (
	// ActionRetry 同端点重试：瞬时错误，可能下一次就成功
	// 适用：网络错误、408 Request Timeout、425 Too Early
	ActionRetry ErrorAction = iota

	// ActionFailover 跨端点切换：当前端点不可恢复但其他端点可能正常
	// 适用：5xx、401/402/403/404、409/410、429、451
	ActionFailover

	// ActionFailFast 立即失败：重试或切换均无意义
	// 适用：400（请求格式错误）、其他 4xx
	ActionFailFast
)

// ErrorClassifier 分类 HTTP 错误
type ErrorClassifier struct{}

// NewErrorClassifier 创建错误分类器
func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{}
}

// Classify 根据 HTTP 响应或网络错误决定处理动作
//
// 三态规则：
//   - 网络错误（连接超时、DNS 失败等） → ActionRetry（瞬时，同端点重试）
//   - 响应头超时                       → ActionFailFast（请求已被上游接收，重放只会重复计费）
//   - HTTP 408/425                     → ActionRetry（瞬时超时类）
//   - HTTP 5xx                         → ActionFailover（端点宕机，切换端点）
//   - HTTP 401/402/403/404/409/410/429/451 → ActionFailover（端点或凭证分组级问题，切换端点/分组）
//   - HTTP 400 及其他 4xx              → ActionFailFast（客户端错误）
func (c *ErrorClassifier) Classify(resp *http.Response, err error) ErrorAction {
	if err != nil {
		var streamErr *UpstreamStreamError
		if errors.As(err, &streamErr) {
			return classifyStreamError(streamErr)
		}
		// 响应头超时说明请求体已被上游完整接收并开始处理（额度已消耗），只是首包没在
		// 期限内到达。重放同一份请求只会再消耗一次额度并加速触发上游限流，交给客户端
		// 自己决定是否重试。HTTP/1 与 HTTP/2 的错误文本都含这一段。
		if strings.Contains(err.Error(), "timeout awaiting response headers") {
			return ActionFailFast
		}
		return ActionRetry
	}
	if resp == nil {
		return ActionRetry
	}

	return c.classifyStatus(resp.StatusCode)
}

// classifyStreamError 处理上游以 HTTP 200 SSE 流返回的错误事件。
//
// 只读上游自己给出的 error type/code/message 文本，不再把关键字折算成 HTTP 状态码：
// 这条响应的真实状态码就是 200，编一个 429/401 出来既会写进转移链路冒充提供商的响应，
// 又和真正的限流/鉴权失败无法区分。
//
// 折算表原本 8 个分支，实际只有一个语义边界——请求体本身不合法时换端点也没用，
// 其余（限流、额度、鉴权、容量、未知）都值得换端点再试。
func classifyStreamError(streamErr *UpstreamStreamError) ErrorAction {
	combined := strings.ToLower(strings.Join([]string{streamErr.Type, streamErr.Code, streamErr.Message}, " "))
	if strings.Contains(combined, "invalid_request") || strings.Contains(combined, "bad_request") {
		return ActionFailFast
	}
	return ActionFailover
}

func (c *ErrorClassifier) classifyStatus(statusCode int) ErrorAction {
	switch {
	case statusCode == http.StatusRequestTimeout, // 408
		statusCode == http.StatusTooEarly: // 425
		return ActionRetry

	case statusCode >= 500:
		return ActionFailover

	case statusCode == http.StatusUnauthorized, // 401
		statusCode == http.StatusPaymentRequired,            // 402
		statusCode == http.StatusForbidden,                  // 403
		statusCode == http.StatusNotFound,                   // 404
		statusCode == http.StatusConflict,                   // 409
		statusCode == http.StatusGone,                       // 410
		statusCode == http.StatusTooManyRequests,            // 429
		statusCode == http.StatusUnavailableForLegalReasons: // 451
		return ActionFailover

	default:
		// HTTP 400 及其他未列出的 4xx
		return ActionFailFast
	}
}
