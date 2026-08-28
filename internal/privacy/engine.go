package privacy

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// CompiledRule 编译后的规则。literal 规则 regex 为 nil。
type CompiledRule struct {
	Rule
	regex       *regexp.Regexp
	redactGroup int
	builtin     builtinMatcher
}

// Snapshot 编译后的规则快照，热路径只读。
type Snapshot struct {
	Version  int64
	Settings Settings
	Rules    []CompiledRule
	LoadedAt time.Time
}

// CompileRule 编译单条规则（regex 使用 Go RE2，无回溯，天然免疫 ReDoS）
func CompileRule(rule Rule) (CompiledRule, error) {
	if err := ValidateRule(rule); err != nil {
		return CompiledRule{}, err
	}
	compiled := CompiledRule{Rule: rule}
	switch rule.MatchType {
	case MatchTypeRegex:
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return CompiledRule{}, fmt.Errorf("compile rule %q failed: %w", rule.Name, err)
		}
		compiled.regex = re
		compiled.redactGroup = redactCaptureGroup(re)
	case MatchTypeBuiltin:
		matcher, err := compileBuiltinMatcher(rule.Pattern)
		if err != nil {
			return CompiledRule{}, fmt.Errorf("compile builtin rule %q failed: %w", rule.Name, err)
		}
		compiled.builtin = matcher
	}
	return compiled, nil
}

// redactCaptureGroup 返回名为 redact 的捕获组下标。
// 有该组时只替换组内区间，用于「保留字段名、只脱敏值」。
func redactCaptureGroup(re *regexp.Regexp) int {
	for i, name := range re.SubexpNames() {
		if i > 0 && name == "redact" {
			return i
		}
	}
	return 0
}

// CompileRules 编译规则集并按 priority 升序（同优先级按 ID）排序。
// 任一规则编译失败立即返回错误，调用方不得替换当前有效快照——
// 半可用的规则集会造成用户以为受保护、实则漏扫。
func CompileRules(rules []Rule) ([]CompiledRule, error) {
	compiled := make([]CompiledRule, 0, len(rules))
	for _, rule := range rules {
		c, err := CompileRule(rule)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, c)
	}
	sort.SliceStable(compiled, func(i, j int) bool {
		if compiled[i].Priority != compiled[j].Priority {
			return compiled[i].Priority < compiled[j].Priority
		}
		return compiled[i].ID < compiled[j].ID
	})
	return compiled, nil
}

// matchSpan 单个命中区间（解码后字符串内的字节偏移）
type matchSpan struct {
	start   int
	end     int
	ruleIdx int
}

// Apply 对请求 body 执行隐私过滤。
//
// 返回的 error 只会是 *PolicyError（fail_closed 短路），调用方必须用
// errors.As 识别并短路返回，不得进入重试 / 故障转移 / 端点冷却；
// 其余内部错误按 Settings.OnError 处理，fail_open 时记录跳过原因并透传原 body。
func (s *Snapshot) Apply(req Request, body []byte) (ApplyResult, error) {
	result := ApplyResult{Body: body, Action: ModeDisabled}
	if s == nil {
		return result, nil
	}
	result.SnapshotVersion = s.Version
	if s.Settings.Mode == ModeDisabled || s.Settings.Mode == "" {
		return result, nil
	}
	result.Action = s.Settings.Mode

	started := time.Now()
	candidates := s.candidateRules()
	if len(candidates) == 0 {
		result.ScanDuration = time.Since(started)
		return result, nil
	}
	if len(body) == 0 {
		result.SkippedReason = SkippedEmptyBody
		result.ScanDuration = time.Since(started)
		return result, nil
	}

	matcher := walkerForPath(req.Path)
	if matcher == nil {
		// 未覆盖路径无法安全定位文本字段。不静默跳过：
		// 漏扫是静默失败，必须让用户看见或直接拒发。
		result.SkippedReason = SkippedUnsupportedPath
		result.ScanDuration = time.Since(started)
		if s.Settings.UnsupportedPathAction == UnsupportedPathFailClosed {
			return result, &PolicyError{
				StatusCode: http.StatusUnprocessableEntity,
				Code:       CodeUnsupportedPath,
				Message:    fmt.Sprintf("path %q has no privacy field whitelist and unsupported_path_action is fail_closed", req.Path),
				Result:     result,
			}
		}
		return result, nil
	}

	segments, err := collectJSONTextSegments(body, matcher)
	if err != nil {
		return s.handleScanError(result, started, err)
	}
	if len(segments) == 0 {
		result.ScanDuration = time.Since(started)
		return result, nil
	}

	scanned, policyErr := s.scanSegments(&result, candidates, segments)
	if policyErr != nil {
		result.ScanDuration = time.Since(started)
		policyErr.Result = result
		return result, policyErr
	}

	if s.Settings.Mode == ModeRedact && len(scanned) > 0 {
		result.Body = replaceSegments(body, segments, scanned)
		result.Changed = true
	}
	result.ScanDuration = time.Since(started)
	return result, nil
}

// candidateRules 返回启用规则下标。MVP 无 Scope 维度，全部启用规则都是候选。
func (s *Snapshot) candidateRules() []int {
	candidates := make([]int, 0, len(s.Rules))
	for i := range s.Rules {
		if s.Rules[i].Enabled {
			candidates = append(candidates, i)
		}
	}
	return candidates
}

// scanSegments 对文本段应用候选规则，返回需替换的段（段下标 -> 替换后文本）。
// 超出 scan_max_bytes 时按 over_limit_action 处理。
func (s *Snapshot) scanSegments(result *ApplyResult, candidates []int, segments []textSegment) (map[int]string, *PolicyError) {
	limit := s.Settings.ScanMaxBytes
	if limit <= 0 {
		limit = DefaultScanMaxBytes
	}

	hits := make(map[int64]*RuleHit)
	replacements := make(map[int]string)
	// 假名分配器随请求创建、随请求销毁：请求内一致，不留任何持久化痕迹
	pseudo := newPseudonymizer()
	var scannedBytes int64

	for segIdx, seg := range segments {
		if result.Truncated {
			break
		}
		text := seg.Value
		remaining := limit - scannedBytes
		if remaining <= 0 {
			if s.Settings.OverLimitAction == OverLimitFailClosed {
				return nil, s.newOverLimitError()
			}
			result.Truncated = true
			result.SkippedReason = SkippedScanTruncated
			break
		}
		if int64(len(text)) > remaining {
			if s.Settings.OverLimitAction == OverLimitFailClosed {
				return nil, s.newOverLimitError()
			}
			// 只扫描剩余预算内的前缀，其余文本透传
			text = text[:remaining]
			result.Truncated = true
			result.SkippedReason = SkippedScanTruncated
		}
		scannedBytes += int64(len(text))

		spans := s.collectMatchSpans(candidates, text)
		if len(spans) == 0 {
			continue
		}

		var redactSpans []matchSpan
		for _, span := range spans {
			rule := &s.Rules[span.ruleIdx]
			hit := hits[rule.ID]
			if hit == nil {
				hit = &RuleHit{RuleID: rule.ID, RuleName: rule.Name, Source: rule.Source, Action: rule.Action}
				hits[rule.ID] = hit
			}
			hit.Count++
			hit.Matches = append(hit.Matches, text[span.start:span.end])
			result.HitCount++
			if s.Settings.Mode == ModeRedact && rule.Action == ActionRedact {
				redactSpans = append(redactSpans, span)
			}
		}
		if len(redactSpans) > 0 {
			replacements[segIdx] = s.applySpans(seg.Value, redactSpans, pseudo)
		}
	}

	result.RuleHits = sortedRuleHits(hits)
	return replacements, nil
}

// collectMatchSpans 按规则优先级收集命中区间；与更高优先级规则重叠的命中被丢弃。
func (s *Snapshot) collectMatchSpans(candidates []int, text string) []matchSpan {
	var accepted []matchSpan
	for _, ruleIdx := range candidates {
		rule := &s.Rules[ruleIdx]
		for _, loc := range findRuleMatches(rule, text) {
			// 已经是本引擎生成的替身值，不再二次替换：
			// 会话历史会反复重发，二次替换会让同一实体在上下文里出现多个假名
			if looksLikePseudonym(text[loc[0]:loc[1]]) {
				continue
			}
			span := matchSpan{start: loc[0], end: loc[1], ruleIdx: ruleIdx}
			if overlapsAny(accepted, span) {
				continue
			}
			accepted = append(accepted, span)
		}
	}
	sort.Slice(accepted, func(i, j int) bool { return accepted[i].start < accepted[j].start })
	return accepted
}

// findRuleMatches 返回规则在文本中的所有命中区间（字节偏移）
func findRuleMatches(rule *CompiledRule, text string) [][2]int {
	if rule.builtin != nil {
		return rule.builtin(text)
	}
	if rule.regex != nil {
		if rule.redactGroup > 0 {
			locs := rule.regex.FindAllStringSubmatchIndex(text, -1)
			out := make([][2]int, 0, len(locs))
			for _, loc := range locs {
				groupStart := rule.redactGroup * 2
				groupEnd := groupStart + 1
				if len(loc) <= groupEnd {
					continue
				}
				if loc[groupStart] >= 0 && loc[groupEnd] > loc[groupStart] {
					out = append(out, [2]int{loc[groupStart], loc[groupEnd]})
				}
			}
			return out
		}
		locs := rule.regex.FindAllStringIndex(text, -1)
		out := make([][2]int, 0, len(locs))
		for _, loc := range locs {
			if loc[1] > loc[0] { // 跳过空匹配
				out = append(out, [2]int{loc[0], loc[1]})
			}
		}
		return out
	}
	if rule.Pattern == "" {
		return nil
	}
	var out [][2]int
	offset := 0
	for {
		idx := strings.Index(text[offset:], rule.Pattern)
		if idx < 0 {
			break
		}
		start := offset + idx
		out = append(out, [2]int{start, start + len(rule.Pattern)})
		offset = start + len(rule.Pattern)
	}
	return out
}

func overlapsAny(spans []matchSpan, candidate matchSpan) bool {
	for _, span := range spans {
		if candidate.start < span.end && span.start < candidate.end {
			return true
		}
	}
	return false
}

// applySpans 将命中区间替换为占位符或假名
func (s *Snapshot) applySpans(text string, spans []matchSpan, pseudo *pseudonymizer) string {
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var builder strings.Builder
	builder.Grow(len(text))
	cursor := 0
	for _, span := range spans {
		builder.WriteString(text[cursor:span.start])
		builder.WriteString(s.replacementFor(&s.Rules[span.ruleIdx], text[span.start:span.end], pseudo))
		cursor = span.end
	}
	builder.WriteString(text[cursor:])
	return builder.String()
}

func (s *Snapshot) replacementFor(rule *CompiledRule, raw string, pseudo *pseudonymizer) string {
	if rule.ReplacementMode == ReplacementPseudonym {
		return pseudo.replacementFor(rule.PseudonymKind, raw)
	}
	if rule.Placeholder == "" {
		return DefaultPlaceholder
	}
	return rule.Placeholder
}

func (s *Snapshot) newOverLimitError() *PolicyError {
	return &PolicyError{
		StatusCode: http.StatusRequestEntityTooLarge,
		Code:       CodeScanBodyTooLarge,
		Message: fmt.Sprintf("scannable text exceeds scan_max_bytes (%d) and over_limit_action is fail_closed",
			s.Settings.ScanMaxBytes),
	}
}

// handleScanError 按 on_error 策略处理内部扫描错误。错误信息不含命中原文。
func (s *Snapshot) handleScanError(result ApplyResult, started time.Time, err error) (ApplyResult, error) {
	result.ScanDuration = time.Since(started)
	if s.Settings.OnError == OnErrorFailClosed {
		result.SkippedReason = CodeScanFailed
		return result, &PolicyError{
			StatusCode: http.StatusUnprocessableEntity,
			Code:       CodeScanFailed,
			Message:    fmt.Sprintf("privacy scan failed and on_error is fail_closed: %v", err),
			Result:     result,
		}
	}
	result.SkippedReason = fmt.Sprintf("scan_error: %v", err)
	return result, nil
}

func sortedRuleHits(hits map[int64]*RuleHit) []RuleHit {
	if len(hits) == 0 {
		return nil
	}
	out := make([]RuleHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, *hit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RuleID < out[j].RuleID })
	return out
}

// ApplyToText 对一段裸文本执行规则（规则测试面板用）。
// 始终按 redact 语义返回替换后文本，但不会越过 mode=disabled 的快照。
func (s *Snapshot) ApplyToText(text string) ApplyResult {
	result := ApplyResult{Action: ModeRedact}
	if s == nil {
		return result
	}
	result.SnapshotVersion = s.Version
	started := time.Now()

	pseudo := newPseudonymizer()
	hits := make(map[int64]*RuleHit)
	var redactSpans []matchSpan
	for _, span := range s.collectMatchSpans(s.candidateRules(), text) {
		rule := &s.Rules[span.ruleIdx]
		hit := hits[rule.ID]
		if hit == nil {
			hit = &RuleHit{RuleID: rule.ID, RuleName: rule.Name, Source: rule.Source, Action: rule.Action}
			hits[rule.ID] = hit
		}
		hit.Count++
		hit.Matches = append(hit.Matches, text[span.start:span.end])
		result.HitCount++
		if rule.Action == ActionRedact {
			redactSpans = append(redactSpans, span)
		}
	}
	replaced := text
	if len(redactSpans) > 0 {
		replaced = s.applySpans(text, redactSpans, pseudo)
		result.Changed = true
	}
	result.Body = []byte(replaced)
	result.RuleHits = sortedRuleHits(hits)
	result.ScanDuration = time.Since(started)
	return result
}
