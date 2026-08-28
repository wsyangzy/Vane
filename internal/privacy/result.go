package privacy

import (
	"fmt"
	"time"
)

// Request 描述一次出站请求的过滤上下文。
// MVP 不含端点/渠道维度：Scope 作用域后置，引入后需重新评估接入点位置。
type Request struct {
	RequestID    string `json:"request_id"`
	Path         string `json:"path"`
	Method       string `json:"method"`
	ProviderType string `json:"provider_type"`
	ContentType  string `json:"content_type"`
}

// RuleHit 单条规则的命中统计。
type RuleHit struct {
	RuleID   int64  `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Source   string `json:"source"`
	Action   string `json:"action"`
	Count    int    `json:"count"`
	// Matches 仅用于本地误判调试。json:"-" 从类型层面阻断序列化：
	// 禁止落库、禁止经 Wails API 回传前端，只允许进本地 slog。
	Matches []string `json:"-"`
}

// 跳过原因
const (
	SkippedNonTextBody     = "skipped_non_text_body"
	SkippedUnsupportedPath = "skipped_unsupported_path"
	SkippedScanTruncated   = "scan_truncated"
	SkippedEmptyBody       = "skipped_empty_body"
)

// ApplyResult 一次过滤的结果。Body 始终可直接转发：
// 零命中 / 仅检测 / 关闭时与输入 byte-identical。
type ApplyResult struct {
	Body            []byte        `json:"-"`
	Changed         bool          `json:"changed"`
	HitCount        int           `json:"hit_count"`
	RuleHits        []RuleHit     `json:"rule_hits"`
	Action          string        `json:"action"`
	ScanDuration    time.Duration `json:"scan_duration"`
	SkippedReason   string        `json:"skipped_reason"`
	Truncated       bool          `json:"truncated"`
	SnapshotVersion int64         `json:"snapshot_version"`
}

// PolicyError 本地隐私策略拒绝。调用方必须用 errors.As 识别并短路返回，
// 不允许进入端点重试、端点故障转移或端点冷却。
type PolicyError struct {
	StatusCode int
	Code       string
	Message    string
	Result     ApplyResult
}

func (e *PolicyError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("privacy policy rejected request: %s (%d): %s", e.Code, e.StatusCode, e.Message)
}

// 策略错误码
const (
	CodeScanBodyTooLarge = "privacy_scan_body_too_large"
	CodeScanFailed       = "privacy_scan_failed"
	CodeUnsupportedPath  = "privacy_unsupported_path"
)
