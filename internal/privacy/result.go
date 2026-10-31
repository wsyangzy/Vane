package privacy

import (
	"fmt"
	"time"
)

// Request 描述一次出站请求的过滤上下文。
// 扫描和处理只做一次，全部渠道及故障转移尝试共用同一结果。
type Request struct {
	SessionKey   string `json:"-"`
	RequestID    string `json:"request_id"`
	Path         string `json:"path"`
	Method       string `json:"method"`
	ProviderType string `json:"provider_type"`
	ContentType  string `json:"content_type"`
}

// RuleHit 单条规则的命中统计。
type RuleHit struct {
	Kind     string `json:"kind,omitempty"`
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
	SkippedScanLimit       = "skipped_scan_limit"
	SkippedEmptyBody       = "skipped_empty_body"
	SkippedNoRules         = "skipped_no_rules"
	SkippedNoText          = "skipped_no_text"
)

// ApplyResult 一次过滤的结果。Body 始终可直接转发：
// 零命中 / 仅检测 / 关闭时与输入 byte-identical。
type ApplyResult struct {
	Restoration *Restoration `json:"-"`
	activity    *activityHandle
	Body        []byte `json:"-"`
	// RedactedBody 是脱敏模式下替换后的正文；仅检测和关闭时为 nil。
	RedactedBody    []byte        `json:"-"`
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

// SelectBody 返回全局策略确定的正文，并在实际发送替身后启用回复还原。
// original 是扫描前的原始正文；返回的 redacted 表示发出的正文是否被改写。
func (r ApplyResult) SelectBody(original []byte) (body []byte, redacted bool) {
	defer func() {
		// Conversation history may already contain tokens even when this request
		// does not introduce a new replacement.
		if r.Restoration != nil && len(r.Restoration.values) > 0 {
			r.Restoration.enable()
		}
		if redacted {
			r.activity.update(true, 0, "")
		}
	}()
	if r.Body == nil {
		return original, false
	}
	return r.Body, r.Changed
}

// 策略错误码
const (
	CodeScanBodyTooLarge = "privacy_scan_body_too_large"
	CodeScanFailed       = "privacy_scan_failed"
	CodeUnsupportedPath  = "privacy_unsupported_path"
	CodeRestoreCapacity  = "privacy_restore_capacity"
)
