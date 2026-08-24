package handlers

import (
	"net/http"
	"testing"
)

// TestIsCredentialGroupFailure 覆盖凭证分组级失效判定。
// 回归点：裸 429（端点级限流/中转过载）不得判死整个凭证分组，否则
// rememberCredentialGroupFailureForTurn 会把同组其余端点批量塞进 triedEndpoints
// 并将整组写入 turn 级黑名单，导致渠道内转移每个分组只试一个节点。
func TestIsCredentialGroupFailure(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		summary    string
		want       bool
	}{
		// 402：同组共享账户余额，无条件整组失效
		{"402 无文本", http.StatusPaymentRequired, "", true},
		{"402 任意文本", http.StatusPaymentRequired, "Payment Required", true},

		// 429：裸限流不得整组失效（本次修复的核心）
		{"429 裸限流", http.StatusTooManyRequests, "Too Many Requests", false},
		{"429 空文本", http.StatusTooManyRequests, "", false},
		{"429 过载文本", http.StatusTooManyRequests, "rate limit exceeded, please retry", false},
		{"429 余额耗尽", http.StatusTooManyRequests, "insufficient_quota", true},
		{"429 账单耗尽", http.StatusTooManyRequests, "Billing hard limit reached", true},
		{"429 额度耗尽", http.StatusTooManyRequests, "credits exhausted", true},

		// 403：必须命中文本
		{"403 裸拒绝", http.StatusForbidden, "Forbidden", false},
		{"403 用户额度", http.StatusForbidden, "insufficient_user_quota", true},
		{"403 账户余额", http.StatusForbidden, "insufficient account balance", true},
		{"403 available quota", http.StatusForbidden, "no available quota left", true},

		// 其他状态码一律不属于分组级失效
		{"503 服务不可用", http.StatusServiceUnavailable, "Service Unavailable", false},
		{"500 内部错误", http.StatusInternalServerError, "insufficient_quota", false},
		{"401 未授权", http.StatusUnauthorized, "insufficient balance", false},
		{"200 成功", http.StatusOK, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCredentialGroupFailure(tc.statusCode, tc.summary); got != tc.want {
				t.Errorf("isCredentialGroupFailure(%d, %q) = %v, want %v",
					tc.statusCode, tc.summary, got, tc.want)
			}
		})
	}
}

// TestIsCredentialGroupFailureCaseInsensitive 上游错误文本大小写不固定，判定需大小写无关。
func TestIsCredentialGroupFailureCaseInsensitive(t *testing.T) {
	for _, summary := range []string{
		"INSUFFICIENT_QUOTA",
		"Insufficient Account Balance",
		"Quota Exhausted",
		"BALANCE EXHAUSTED",
	} {
		if !isCredentialGroupFailure(http.StatusTooManyRequests, summary) {
			t.Errorf("summary %q 应判定为凭证分组级失效", summary)
		}
	}
}
