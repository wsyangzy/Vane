// error_classifier.go - Codex 错误分类器
// 根据 HTTP 响应状态码或网络错误，决定是同端点重试、跨端点切换还是立即失败。

package codexproxy

import "net/http"

// ErrorAction 错误处理动作（三态）
type ErrorAction int

const (
	// ActionRetry 同端点重试：瞬时错误，可能下一次就成功
	// 适用：网络错误、408 Request Timeout、425 Too Early
	ActionRetry ErrorAction = iota

	// ActionFailover 跨端点切换：当前端点不可恢复但其他端点可能正常
	// 适用：5xx、401/403、404、409、410、451
	ActionFailover

	// ActionFailFast 立即失败：重试或切换均无意义
	// 适用：400（请求格式错误）、402（账户欠费）、429（限流/配额）、其他 4xx
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
//   - HTTP 408/425                     → ActionRetry（瞬时超时类）
//   - HTTP 5xx                         → ActionFailover（端点宕机，切换端点）
//   - HTTP 401/403/404/409/410/451     → ActionFailover（端点级问题，切换端点）
//   - HTTP 402/429                     → ActionFailFast（计费/限流，切换端点也无意义）
//   - HTTP 400 及其他 4xx              → ActionFailFast（客户端错误）
func (c *ErrorClassifier) Classify(resp *http.Response, err error) ErrorAction {
	if err != nil {
		return ActionRetry
	}
	if resp == nil {
		return ActionRetry
	}

	switch {
	case resp.StatusCode == http.StatusRequestTimeout, // 408
		resp.StatusCode == http.StatusTooEarly: // 425
		return ActionRetry

	case resp.StatusCode >= 500:
		return ActionFailover

	case resp.StatusCode == http.StatusUnauthorized, // 401
		resp.StatusCode == http.StatusForbidden, // 403
		resp.StatusCode == http.StatusNotFound, // 404
		resp.StatusCode == http.StatusConflict, // 409
		resp.StatusCode == http.StatusGone, // 410
		resp.StatusCode == http.StatusUnavailableForLegalReasons: // 451
		return ActionFailover

	case resp.StatusCode == http.StatusTooManyRequests, // 429（限流）
		resp.StatusCode == http.StatusPaymentRequired: // 402（欠费）
		return ActionFailFast

	default:
		// HTTP 400 及其他未列出的 4xx
		return ActionFailFast
	}
}
