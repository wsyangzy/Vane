// Package privacy 提供出站请求隐私保护的纯规则引擎。
// 本包不依赖 proxy/store/service，只做规则编译、文本扫描与脱敏替换。
//
// 三条不可妥协的约束：
//  1. 命中原文只允许出现在本地日志，禁止落库、禁止经 API 回传前端；
//  2. 未命中的请求体字节必须与客户端原始请求 byte-identical；
//  3. 策略拒绝必须短路，不得进入重试 / 故障转移 / 端点冷却。
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

// 扫描出错策略
const (
	OnErrorFailOpen   = "fail_open"
	OnErrorFailClosed = "fail_closed"
)

// 超出扫描上限策略
const (
	OverLimitScanPrefix = "scan_prefix"
	OverLimitFailClosed = "fail_closed"
)

// 未覆盖路径策略。白名单之外的路径无法安全扫描，
// warn 放行并记录（前端可见），fail_closed 直接拒发。
const (
	UnsupportedPathWarn       = "warn"
	UnsupportedPathFailClosed = "fail_closed"
)

// 规则匹配方式
const (
	MatchTypeLiteral = "literal"
	MatchTypeRegex   = "regex"
	MatchTypeBuiltin = "builtin"
)

// 规则动作
const (
	ActionDetect = "detect"
	ActionRedact = "redact"
)

// 替换模式。pseudonym 生成格式合法但虚构的替身值，
// 使下游 AI 仍能区分多个实体；placeholder 使用固定占位符。
const (
	ReplacementPlaceholder = "placeholder"
	ReplacementPseudonym   = "pseudonym"
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
}

// DefaultSettings 返回新装默认配置。
// 默认 detect 而非 disabled：默认关闭等于装了没装，用户不会知道自己在裸奔；
// 默认 redact 又会让未经验证的规则直接改写生产流量。detect 首次即呈现命中统计，
// 零改写风险，且命中数本身就是最强的启用动机。
func DefaultSettings() Settings {
	return Settings{
		Mode:                  ModeDetect,
		ScanMaxBytes:          DefaultScanMaxBytes,
		OverLimitAction:       OverLimitScanPrefix,
		OnError:               OnErrorFailOpen,
		UnsupportedPathAction: UnsupportedPathWarn,
	}
}

// ValidateSettings 校验引擎配置字段
func ValidateSettings(s Settings) error {
	switch s.Mode {
	case ModeDisabled, ModeDetect, ModeRedact:
	default:
		return fmt.Errorf("invalid mode: %q", s.Mode)
	}
	if s.ScanMaxBytes <= 0 {
		return fmt.Errorf("scan_max_bytes must be positive, got %d", s.ScanMaxBytes)
	}
	switch s.OverLimitAction {
	case OverLimitScanPrefix, OverLimitFailClosed:
	default:
		return fmt.Errorf("invalid over_limit_action: %q", s.OverLimitAction)
	}
	switch s.OnError {
	case OnErrorFailOpen, OnErrorFailClosed:
	default:
		return fmt.Errorf("invalid on_error: %q", s.OnError)
	}
	switch s.UnsupportedPathAction {
	case UnsupportedPathWarn, UnsupportedPathFailClosed:
	default:
		return fmt.Errorf("invalid unsupported_path_action: %q", s.UnsupportedPathAction)
	}
	return nil
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
	case MatchTypeLiteral, MatchTypeRegex, MatchTypeBuiltin:
	default:
		return fmt.Errorf("invalid match_type: %q", r.MatchType)
	}
	switch r.Action {
	case ActionDetect, ActionRedact:
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
	default:
		return fmt.Errorf("invalid replacement_mode: %q", r.ReplacementMode)
	}
	return nil
}
