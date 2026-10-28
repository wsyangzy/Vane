package privacy

import (
	"fmt"
	"time"
)

// Request 描述一次出站请求的过滤上下文。
// 扫描不含端点/渠道维度，只做一次；渠道级隐私级别在每次尝试选端点后，
// 由调用方用 ChannelLevel.SelectBody 从 ApplyResult 中挑选实际发送的正文。
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
	// RedactedBody 按脱敏语义替换后的正文，与全局模式无关；没有可替换的命中时为 nil。
	// 供渠道级隐私级别覆盖使用（三方中转在全局「仅检测」下仍强制脱敏）。
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

// 渠道隐私级别：覆盖全局模式在该渠道上的效果。全局模式为 disabled 时一律不生效（总开关）。
const (
	// ChannelLevelDefault 跟随全局模式
	ChannelLevelDefault = ""
	// ChannelLevelThirdParty 三方中转：全局为「仅检测」时也强制脱敏
	ChannelLevelThirdParty = "third_party"
	// ChannelLevelTrusted 可信渠道（官方直连等）：只检测不改写
	ChannelLevelTrusted = "trusted"
)

// NormalizeChannelLevel 规范化渠道隐私级别，未知取值按跟随全局处理
func NormalizeChannelLevel(level string) string {
	switch level {
	case ChannelLevelThirdParty, ChannelLevelTrusted:
		return level
	default:
		return ChannelLevelDefault
	}
}

// SelectBody 按渠道隐私级别挑选实际发送的正文。
// original 是扫描前的原始正文；返回的 redacted 表示发出的正文是否被改写。
func (r ApplyResult) SelectBody(level string, original []byte) (body []byte, redacted bool) {
	defer func() {
		if redacted {
			r.Restoration.enable()
			r.activity.update(true, 0, "")
		}
	}()
	switch NormalizeChannelLevel(level) {
	case ChannelLevelThirdParty:
		if r.Action != ModeDisabled && r.RedactedBody != nil {
			return r.RedactedBody, true
		}
	case ChannelLevelTrusted:
		return original, false
	}
	return r.Body, r.Changed
}

// 策略错误码
const (
	CodeScanBodyTooLarge = "privacy_scan_body_too_large"
	CodeScanFailed       = "privacy_scan_failed"
	CodeUnsupportedPath  = "privacy_unsupported_path"
)
