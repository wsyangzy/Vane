// error_classifier.go - Codex 错误分类器
// 根据 HTTP 响应状态码或网络错误决定是否可重试。

package codexproxy

import "net/http"

// ErrorAction 错误处理动作
type ErrorAction int

const (
	ActionRetry    ErrorAction = iota // 可重试（切换端点）
	ActionFailFast                    // 不可重试，直接返回
)

// ErrorClassifier 分类 HTTP 错误
type ErrorClassifier struct{}

// NewErrorClassifier 创建错误分类器
func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{}
}

// Classify 根据 HTTP 响应或网络错误决定处理动作
// 规则:
// - 网络错误（连接超时、DNS 失败等）→ ActionRetry
// - HTTP 5xx → ActionRetry
// - HTTP 429 (Rate Limited) → ActionRetry
// - HTTP 401/403 (鉴权失败) → ActionRetry（不同端点可能配置了不同 key）
// - HTTP 404 (路由不存在) → ActionRetry（不同端点可能 base_url 不同）
// - HTTP 408/409/410/425/451 → ActionRetry（端点特定的可恢复错误）
// - HTTP 400 (Bad Request) → ActionFailFast（请求体格式错误，重试无意义）
// - 其他 4xx → ActionFailFast
func (c *ErrorClassifier) Classify(resp *http.Response, err error) ErrorAction {
	// 网络错误一律可重试
	if err != nil {
		return ActionRetry
	}

	if resp == nil {
		return ActionRetry
	}

	switch {
	case resp.StatusCode >= 500:
		return ActionRetry
	case resp.StatusCode == http.StatusTooManyRequests: // 429
		return ActionRetry
	case resp.StatusCode == http.StatusUnauthorized, // 401
		resp.StatusCode == http.StatusForbidden,        // 403
		resp.StatusCode == http.StatusNotFound,         // 404
		resp.StatusCode == http.StatusRequestTimeout,   // 408
		resp.StatusCode == http.StatusConflict,         // 409
		resp.StatusCode == http.StatusGone,             // 410
		resp.StatusCode == http.StatusTooEarly,         // 425
		resp.StatusCode == http.StatusUnavailableForLegalReasons: // 451
		return ActionRetry
	case resp.StatusCode == http.StatusBadRequest: // 400 - 客户端错误，重试无意义
		return ActionFailFast
	default:
		return ActionFailFast
	}
}
