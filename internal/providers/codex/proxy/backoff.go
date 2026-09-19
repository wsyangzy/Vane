// backoff.go - 指数退避计算
// 用于 Codex 同端点重试时的等待时长计算。

package codexproxy

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxRetryAfterWait 上游建议等待时长的上限，防止异常大值把请求挂死。
const maxRetryAfterWait = 30 * time.Second

// retryAfterPattern 匹配上游错误文本里的 "retry after 9 seconds" 之类提示。
var retryAfterPattern = regexp.MustCompile(`(?i)retry after (\d+) ?s(?:ec(?:ond)?s?)?\b`)

// retryAfterHint 从上游响应头 Retry-After 或错误文本中提取建议等待时长，没有则返回 0。
//
// 服务商在 SSE 里返回 "Codex accounts are temporarily throttled, retry after 9 seconds"
// 时，立即重发只会再吃一次限流；按它说的等一等，下一次尝试才有意义。
func retryAfterHint(resp *http.Response, errText string) time.Duration {
	seconds := 0
	if resp != nil {
		if v, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil {
			seconds = v
		}
	}
	if seconds <= 0 {
		if m := retryAfterPattern.FindStringSubmatch(errText); m != nil {
			seconds, _ = strconv.Atoi(m[1])
		}
	}
	if seconds <= 0 {
		return 0
	}
	if d := time.Duration(seconds) * time.Second; d < maxRetryAfterWait {
		return d
	}
	return maxRetryAfterWait
}

// computeBackoff 计算第 attempt 次重试前的等待时长（指数退避）
//
// 公式: base * multiplier^attempt（attempt 从 0 开始）
// 封顶: maxDelay
//
// 参数:
//   - attempt:    第几次重试（0 = 第一次重试前的等待，无重试时调用方应跳过）
//   - baseDelay:  基础延迟（默认 1s）
//   - maxDelay:   延迟上限（默认 30s）
//   - multiplier: 倍增系数（默认 2.0）
//
// 默认值兜底：传入 <=0 的参数会自动套用默认值。
func computeBackoff(attempt int, baseDelay, maxDelay time.Duration, multiplier float64) time.Duration {
	if baseDelay <= 0 {
		baseDelay = time.Second
	}
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	if multiplier <= 0 {
		multiplier = 2.0
	}
	if attempt < 0 {
		attempt = 0
	}

	// 指数计算（float64 防止 int 溢出）
	delay := float64(baseDelay)
	for i := 0; i < attempt; i++ {
		delay *= multiplier
		if delay >= float64(maxDelay) {
			return maxDelay
		}
	}

	result := time.Duration(delay)
	if result > maxDelay {
		return maxDelay
	}
	return result
}
