// Package privacy 提供出站请求隐私保护的纯规则引擎。
// 本包不依赖 proxy/store/service，只做规则编译、文本扫描与脱敏替换。
//
// 三条不可妥协的约束：
//  1. 命中原文和还原映射仅存内存，禁止写日志、落库或经 API 回传前端；
//  2. 未命中的请求体字节必须与客户端原始请求 byte-identical；
//  3. 扫描异常放行，按配置决定是否记录，不主动拒发或触发额外重试。
package privacy

import (
	"fmt"
	"strings"
)

// 全局运行模式（privacy_settings.mode 唯一真值来源）
const (
	ModeDisabled = "disabled"
	ModeDetect   = "detect"
	ModeRedact   = "redact"
)

// 扫描出错策略。fail_closed 仅用于读取旧配置，运行时统一放行。
const (
	OnErrorFailOpen    = "fail_open"
	OnErrorFailClosed  = "fail_closed"
	OnErrorPassthrough = "passthrough"
)

// 超出扫描上限策略。旧 fail_closed 配置统一迁移为扫描前缀。
const (
	OverLimitScanPrefix = "scan_prefix"
	OverLimitFailClosed = "fail_closed"
	OverLimitSkipScan   = "skip_scan"
)

// 未覆盖路径统一放行，warn 记录防护摘要，passthrough 不记录；fail_closed 仅兼容旧配置。
const (
	UnsupportedPathWarn        = "warn"
	UnsupportedPathFailClosed  = "fail_closed"
	UnsupportedPathPassthrough = "passthrough"
)

// 规则匹配方式
const (
	MatchTypeLiteral = "literal"
	MatchTypeRegex   = "regex"
	MatchTypeBuiltin = "builtin"
	MatchTypePrefix  = "prefix"
)

// 规则用途只区分普通规则与额外放行。detect / block 兼容旧数据，编译时归为普通规则。
const (
	ActionDetect = "detect"
	ActionRedact = "redact"
	ActionBlock  = "block"
	ActionAllow  = "allow"
)

// reversible / mask_only 均使用稳定编码，后者禁止回复还原。
// *_key 保留密钥格式；placeholder / pseudonym 兼容旧配置。
const (
	ReplacementPlaceholder   = "placeholder"
	ReplacementPseudonym     = "pseudonym"
	ReplacementReversible    = "reversible"
	ReplacementReversibleKey = "reversible_key"
	ReplacementMaskOnly      = "mask_only"
	ReplacementMaskOnlyKey   = "mask_only_key"
)

// 规则来源
const (
	SourceCustom  = "custom"
	SourcePreset  = "preset"
	SourceBuiltin = "builtin"
)

// DefaultScanMaxBytes 单请求累计扫描文本字节上限默认值（4MB）
const DefaultScanMaxBytes = 4 * 1024 * 1024

// DefaultPlaceholder 默认脱敏占位符
const DefaultPlaceholder = "[已脱敏]"

// Rule 单条隐私规则
type Rule struct {
	ID              int64  `json:"id"`
	Enabled         bool   `json:"enabled"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Priority        int    `json:"priority"`
	MatchType       string `json:"match_type"`
	Pattern         string `json:"pattern"`
	Action          string `json:"action"`
	ReplacementMode string `json:"replacement_mode"`
	Placeholder     string `json:"placeholder"`
	PseudonymKind   string `json:"pseudonym_kind"`
	Source          string `json:"source"`
}

// Settings 引擎运行配置（来自 privacy_settings 表）
type Settings struct {
	Mode                  string `json:"mode"`
	ScanMaxBytes          int64  `json:"scan_max_bytes"`
	OverLimitAction       string `json:"over_limit_action"`
	OnError               string `json:"on_error"`
	UnsupportedPathAction string `json:"unsupported_path_action"`
	// Deprecated matching options are read only to migrate existing databases.
	// Runtime matching and actions come exclusively from Rule.
	Categories   map[string]bool   `json:"categories"`
	Words        []string          `json:"words"`
	WordsAnyCase bool              `json:"words_any_case"`
	Allow        []string          `json:"allow"`
	Actions      map[string]string `json:"actions"`
	// RestoreResponses controls response restoration independently of outbound masking.
	RestoreResponses bool `json:"restore_responses"`
	KeepKeyFormat    bool `json:"keep_key_format"`
}

// DefaultSettings 返回新装默认配置。
// 默认 detect 而非 disabled：默认关闭等于装了没装，用户不会知道自己在裸奔；
// 默认 redact 又会让未经验证的规则直接改写生产流量。detect 首次即呈现命中统计，
// 零改写风险，且命中数本身就是最强的启用动机。
func DefaultSettings() Settings {
	return Settings{
		Mode:                  ModeDetect,
		RestoreResponses:      true,
		ScanMaxBytes:          DefaultScanMaxBytes,
		OverLimitAction:       OverLimitScanPrefix,
		OnError:               OnErrorFailOpen,
		UnsupportedPathAction: UnsupportedPathWarn,
	}
}

// ValidateSettings 校验引擎配置字段
func ValidateSettings(s Settings) error {
	if hasLegacyMatchingOptions(s) || s.KeepKeyFormat {
		return fmt.Errorf("匹配配置已整合到防护规则，请在规则列表中修改")
	}
	switch s.Mode {
	case ModeDisabled, ModeDetect, ModeRedact:
	default:
		return fmt.Errorf("invalid mode: %q", s.Mode)
	}
	if s.ScanMaxBytes <= 0 {
		return fmt.Errorf("scan_max_bytes must be positive, got %d", s.ScanMaxBytes)
	}
	switch s.OverLimitAction {
	case OverLimitScanPrefix, OverLimitSkipScan, OverLimitFailClosed:
	default:
		return fmt.Errorf("invalid over_limit_action: %q", s.OverLimitAction)
	}
	switch s.OnError {
	case OnErrorFailOpen, OnErrorPassthrough, OnErrorFailClosed:
	default:
		return fmt.Errorf("invalid on_error: %q", s.OnError)
	}
	switch s.UnsupportedPathAction {
	case UnsupportedPathWarn, UnsupportedPathPassthrough, UnsupportedPathFailClosed:
	default:
		return fmt.Errorf("invalid unsupported_path_action: %q", s.UnsupportedPathAction)
	}
	return nil
}

func normalizeScanPolicies(s Settings) Settings {
	if s.OnError == OnErrorFailClosed {
		s.OnError = OnErrorFailOpen
	}
	if s.OverLimitAction == OverLimitFailClosed {
		s.OverLimitAction = OverLimitScanPrefix
	}
	if s.UnsupportedPathAction == UnsupportedPathFailClosed {
		s.UnsupportedPathAction = UnsupportedPathWarn
	}
	return s
}

// ValidateRule 校验单条规则字段（不含正则编译，编译交给 CompileRule）
func ValidateRule(r Rule) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("rule name is required")
	}
	if r.Pattern == "" {
		return fmt.Errorf("rule pattern is required")
	}
	switch r.MatchType {
	case MatchTypeLiteral, MatchTypeRegex, MatchTypeBuiltin, MatchTypePrefix:
	default:
		return fmt.Errorf("invalid match_type: %q", r.MatchType)
	}
	switch r.Action {
	case ActionDetect, ActionRedact, ActionBlock, ActionAllow:
	default:
		return fmt.Errorf("invalid action: %q", r.Action)
	}
	if r.Action != ActionRedact {
		return nil
	}
	switch r.ReplacementMode {
	case ReplacementPlaceholder:
		if r.Placeholder == "" {
			return fmt.Errorf("placeholder is required for placeholder replacement mode")
		}
	case ReplacementPseudonym:
		if !IsValidPseudonymKind(r.PseudonymKind) {
			return fmt.Errorf("invalid pseudonym_kind: %q", r.PseudonymKind)
		}
	case ReplacementReversible, ReplacementReversibleKey, ReplacementMaskOnly, ReplacementMaskOnlyKey:
	default:
		return fmt.Errorf("invalid replacement_mode: %q", r.ReplacementMode)
	}
	return nil
}

func (r Rule) usesTokens() bool {
	return r.Action == ActionRedact && (r.restoresResponses() || r.ReplacementMode == ReplacementMaskOnly || r.ReplacementMode == ReplacementMaskOnlyKey)
}

func (r Rule) restoresResponses() bool {
	return r.ReplacementMode == ReplacementReversible || r.ReplacementMode == ReplacementReversibleKey
}
