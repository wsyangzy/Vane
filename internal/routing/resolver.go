// Package routing 实现请求级路由的纯函数部分：模型路由规则的匹配与排序、
// 渠道级非流改道的触发判断，以及执行阶段的编排。
//
// 这里只决定"按什么顺序尝试哪些目标"，不读写任何全局激活状态，
// 也不涉及具体 provider 的端点与凭证；Claude 与 Codex 共用同一套规则语义。
package routing

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 路由模式
const (
	// ModePrefer 先依次尝试目标。模型规则的目标全部失败后回到激活渠道；
	// 非流改道不回退——来源渠道已声明不接非流请求，再试只会多消耗重试预算。
	ModePrefer = "prefer"
	// ModeFallback 先在激活渠道内尝试，最终失败后再依次尝试目标。
	ModeFallback = "fallback"
)

// Kind 路由类型，同时是 request_logs.route_snapshot 的 kind 取值。
type Kind string

const (
	KindNone      Kind = ""
	KindModelRule Kind = "model_rule"
	KindNonStream Kind = "non_stream"
	// KindModelCapability 激活渠道的凭证分组都不支持请求模型时，
	// 只在本次请求内借用其它渠道，不切换全局激活渠道。
	KindModelCapability Kind = "model_capability"
)

// Target 路由目标。用 ID 引用渠道与凭证分组，渠道改名不会让配置失效。
type Target struct {
	ChannelID int64 `json:"channel_id"`
	// GroupID 为 0 表示该渠道下的全部凭证分组，按分组现有顺序尝试
	GroupID int64 `json:"group_id"`
	// UpstreamModel 非空时强制改写请求 model，优先级高于分组的模型映射
	UpstreamModel string `json:"upstream_model"`
}

// ModelRule 已启用的模型路由规则（运行时快照）。
//
// 规则挂在来源渠道上：只有该渠道是当前激活渠道时才参与匹配，语义是
// "本渠道不适合处理模型 X，把 X 送到这些目标"。不同渠道的规则互不影响。
type ModelRule struct {
	ID              int64
	SourceChannelID int64
	Name            string
	Priority        int
	MatchModel      string
	Mode            string
	Targets         []Target
}

// RulesBySource 按来源渠道分组；来源渠道为 0 的旧版全局规则不参与运行时匹配。
func RulesBySource(rules []ModelRule) map[int64][]ModelRule {
	grouped := make(map[int64][]ModelRule)
	for _, rule := range rules {
		if rule.SourceChannelID <= 0 {
			continue
		}
		grouped[rule.SourceChannelID] = append(grouped[rule.SourceChannelID], rule)
	}
	return grouped
}

// NonStreamRoute 渠道级非流改道配置，对应 channels / codex_channels 的 non_stream_route 列。
type NonStreamRoute struct {
	Mode    string   `json:"mode"`
	Targets []Target `json:"targets"`
}

// Decision 一次请求的路由决策。Kind 为 KindNone 时走现有流程，行为不变。
type Decision struct {
	Kind    Kind
	Mode    string
	Targets []Target
	Rule    *ModelRule // KindModelRule 时有值
}

// Routed 报告请求是否命中路由
func (d Decision) Routed() bool { return d.Kind != KindNone }

// Stage 执行阶段。Target 为 nil 表示来源渠道（请求发起时的激活渠道）。
type Stage struct {
	Target *Target
	// Index 写入 route_snapshot.target：0 为来源渠道，1 起为第几个目标
	Index int
}

// NormalizeMode 规范化路由模式，非法取值返回空串
func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModePrefer:
		return ModePrefer
	case ModeFallback:
		return ModeFallback
	default:
		return ""
	}
}

// MatchModel 按模型路由规则的语义匹配：大小写不敏感，支持 * 通配（匹配任意长度）。
func MatchModel(pattern, model string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	model = strings.ToLower(strings.TrimSpace(model))
	if pattern == "" || model == "" {
		return false
	}
	return globMatch(pattern, model)
}

// MatchSupportedModel 按凭证分组 supported_models 的语义匹配：
// 含 * 的条目大小写不敏感地通配；不含 * 的条目保持原有的精确匹配。
func MatchSupportedModel(entry, model string) bool {
	entry = strings.TrimSpace(entry)
	model = strings.TrimSpace(model)
	if entry == "" || model == "" {
		return false
	}
	if !strings.Contains(entry, "*") {
		return entry == model
	}
	return MatchModel(entry, model)
}

// globMatch 只支持 *，贪心匹配并在失配时回溯到最近一个 *
func globMatch(pattern, s string) bool {
	p, i := 0, 0
	star, mark := -1, 0
	for i < len(s) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, i
			p++
		case p < len(pattern) && pattern[p] == s[i]:
			p++
			i++
		case star >= 0:
			mark++
			p, i = star+1, mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// specificity 规则特异度：tier 0 精确、1 通配、2 纯 *；prefix 为通配前的字面前缀长度
func specificity(pattern string) (tier, prefix int) {
	pattern = strings.TrimSpace(pattern)
	star := strings.IndexByte(pattern, '*')
	if star < 0 {
		return 0, len(pattern)
	}
	if strings.Trim(pattern, "*") == "" {
		return 2, 0
	}
	return 1, star
}

// RuleLess 规则排序：精确 > 通配（字面前缀越长越优先）> 纯 *，其次 priority 小者优先，最后 id。
func RuleLess(a, b *ModelRule) bool {
	tierA, prefixA := specificity(a.MatchModel)
	tierB, prefixB := specificity(b.MatchModel)
	if tierA != tierB {
		return tierA < tierB
	}
	if tierA == 1 && prefixA != prefixB {
		return prefixA > prefixB
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	return a.ID < b.ID
}

// ResolveModelRule 返回请求模型命中的规则；一个请求只命中一条。
func ResolveModelRule(rules []ModelRule, model string) *ModelRule {
	var best *ModelRule
	for i := range rules {
		rule := &rules[i]
		if len(rule.Targets) == 0 || !MatchModel(rule.MatchModel, model) {
			continue
		}
		if best == nil || RuleLess(rule, best) {
			best = rule
		}
	}
	return best
}

// Decide 决策优先级：模型规则 > 非流改道 > 默认流程。
// 模型规则是针对具体模型的显式配置，比渠道级的通用兜底更具体；
// 流式请求永不触发非流改道。
func Decide(rules []ModelRule, nonStream *NonStreamRoute, model string, streaming bool) Decision {
	if rule := ResolveModelRule(rules, model); rule != nil {
		mode := NormalizeMode(rule.Mode)
		if mode == "" {
			mode = ModePrefer
		}
		return Decision{Kind: KindModelRule, Mode: mode, Targets: rule.Targets, Rule: rule}
	}
	if !streaming && nonStream != nil && len(nonStream.Targets) > 0 {
		if mode := NormalizeMode(nonStream.Mode); mode != "" {
			return Decision{Kind: KindNonStream, Mode: mode, Targets: nonStream.Targets}
		}
	}
	return Decision{}
}

// Stages 按决策编排执行阶段
func (d Decision) Stages() []Stage {
	if !d.Routed() {
		return nil
	}
	stages := make([]Stage, 0, len(d.Targets)+1)
	source := Stage{Index: 0}
	if d.Mode == ModeFallback {
		stages = append(stages, source)
	}
	for i := range d.Targets {
		stages = append(stages, Stage{Target: &d.Targets[i], Index: i + 1})
	}
	if d.Mode != ModeFallback && d.Kind == KindModelRule {
		stages = append(stages, source)
	}
	return stages
}

// ExhaustedReason 路由全部阶段失败时写入 failure_reason 的取值
func (k Kind) ExhaustedReason() string {
	switch k {
	case KindModelRule:
		return "route_exhausted"
	case KindNonStream:
		return "non_stream_route_exhausted"
	default:
		return "endpoint_exhausted"
	}
}

// Snapshot request_logs.route_snapshot 的内容。只记录规则标识与阶段序号，不含请求内容。
type Snapshot struct {
	Kind Kind   `json:"kind"`
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// Channel 非流改道 / 模型能力改道的来源渠道
	Channel string `json:"channel,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Target  int    `json:"target"`
}

// NewSnapshot 按决策构造快照；sourceChannel 为请求发起时的激活渠道名。
// 模型规则也记来源渠道：规则按渠道隔离，只有规则名不足以定位是哪个渠道的配置。
func NewSnapshot(d Decision, sourceChannel string) Snapshot {
	snapshot := Snapshot{Kind: d.Kind, Mode: d.Mode, Channel: sourceChannel}
	if d.Rule != nil {
		snapshot.ID = d.Rule.ID
		snapshot.Name = d.Rule.Name
	}
	return snapshot
}

// JSON 序列化快照；Target 为当前所处阶段
func (s Snapshot) JSON(target int) string {
	s.Target = target
	encoded, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// NormalizeTargets 规整目标列表：去空白、丢弃无效项、按出现顺序去重
func NormalizeTargets(targets []Target) []Target {
	normalized := make([]Target, 0, len(targets))
	seen := make(map[Target]struct{}, len(targets))
	for _, target := range targets {
		target.UpstreamModel = strings.TrimSpace(target.UpstreamModel)
		if target.ChannelID <= 0 || target.GroupID < 0 {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		normalized = append(normalized, target)
	}
	return normalized
}

// ParseTargets 解析 JSON 目标数组；空串返回 nil
func ParseTargets(raw string) ([]Target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	var targets []Target
	if err := json.Unmarshal([]byte(raw), &targets); err != nil {
		return nil, fmt.Errorf("解析路由目标失败: %w", err)
	}
	return NormalizeTargets(targets), nil
}

// EncodeTargets 序列化目标数组
func EncodeTargets(targets []Target) (string, error) {
	normalized := NormalizeTargets(targets)
	if normalized == nil {
		normalized = []Target{}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("序列化路由目标失败: %w", err)
	}
	return string(encoded), nil
}

// ParseNonStreamRoute 解析 non_stream_route 列；NULL/空串表示关闭，返回 nil。
func ParseNonStreamRoute(raw string) (*NonStreamRoute, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	var route NonStreamRoute
	if err := json.Unmarshal([]byte(raw), &route); err != nil {
		return nil, fmt.Errorf("解析非流改道配置失败: %w", err)
	}
	route.Mode = NormalizeMode(route.Mode)
	route.Targets = NormalizeTargets(route.Targets)
	if route.Mode == "" || len(route.Targets) == 0 {
		return nil, nil
	}
	return &route, nil
}

// EncodeNonStreamRoute 序列化非流改道配置；nil 或无目标时返回空串（落库为 NULL，表示关闭）。
func EncodeNonStreamRoute(route *NonStreamRoute) (string, error) {
	if route == nil {
		return "", nil
	}
	normalized := NonStreamRoute{Mode: NormalizeMode(route.Mode), Targets: NormalizeTargets(route.Targets)}
	if len(normalized.Targets) == 0 {
		return "", nil
	}
	if normalized.Mode == "" {
		return "", fmt.Errorf("非流改道模式无效: %q", route.Mode)
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("序列化非流改道配置失败: %w", err)
	}
	return string(encoded), nil
}
