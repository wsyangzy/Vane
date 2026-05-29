// backoff.go - 指数退避计算
// 用于 Codex 同端点重试时的等待时长计算。

package codexproxy

import "time"

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
