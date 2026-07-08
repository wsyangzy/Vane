// error_classifier.go - Codex 错误分类器
// 根据 HTTP 响应状态码或网络错误，决定是同端点重试、跨端点切换还是立即失败。

package codexproxy

import (
	"errors"
	"net/http"
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
//   - HTTP 408/425                     → ActionRetry（瞬时超时类）
//   - HTTP 5xx                         → ActionFailover（端点宕机，切换端点）
//   - HTTP 401/402/403/404/409/410/429/451 → ActionFailover（端点或凭证分组级问题，切换端点/分组）
//   - HTTP 400 及其他 4xx              → ActionFailFast（客户端错误）
func (c *ErrorClassifier) Classify(resp *http.Response, err error) ErrorAction {
	if err != nil {
		var streamErr *UpstreamStreamError
		if errors.As(err, &streamErr) {
			return c.classifyStatus(streamErr.StatusCode)
		}
		return ActionRetry
	}
	if resp == nil {
		return ActionRetry
	}

	return c.classifyStatus(resp.StatusCode)
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
