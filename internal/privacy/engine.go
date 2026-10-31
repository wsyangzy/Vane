package privacy

import (
	"fmt"
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
	Version     int64
	Settings    Settings
	Rules       []CompiledRule
	LoadedAt    time.Time
	restoration *Restoration
}

func (s *Snapshot) requiresMappings() bool {
	for _, rule := range s.Rules {
		if rule.Enabled && rule.Rule.usesTokens() {
			return true
		}
	}
	return false
}

// CompileRule 编译单条规则（regex 使用 Go RE2，无回溯，天然免疫 ReDoS）
func CompileRule(rule Rule) (CompiledRule, error) {
	if err := ValidateRule(rule); err != nil {
		return CompiledRule{}, err
	}
	if rule.Action == ActionDetect || rule.Action == ActionBlock {
		rule.Action = ActionRedact
	}
	compiled := CompiledRule{Rule: rule}
	switch rule.MatchType {
	case MatchTypePrefix:
		if len(rule.Pattern) < 3 || len(rule.Pattern) > 64 {
			return CompiledRule{}, fmt.Errorf("密钥前缀须为 3–64 字节")
		}
		compiled.regex = regexp.MustCompile(regexp.QuoteMeta(rule.Pattern) + `[A-Za-z0-9_-]{8,}`)
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
// 扫描异常、未覆盖路径和超限不拒发；防护摘要由 Service 按配置记录。
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
		result.SkippedReason = SkippedNoRules
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
		// 无法安全定位文本字段时保留原文，并通过防护记录提示漏扫。
		result.SkippedReason = SkippedUnsupportedPath
		result.ScanDuration = time.Since(started)
		return result, nil
	}

	segments, err := collectJSONTextSegments(body, matcher)
	if err != nil {
		result.SkippedReason = "scan_error"
		result.ScanDuration = time.Since(started)
		return result, nil
	}
	if len(segments) == 0 {
		result.SkippedReason = SkippedNoText
		result.ScanDuration = time.Since(started)
		return result, nil
	}

	scanned := s.scanSegments(&result, candidates, segments)
	if s.restoration != nil && s.restoration.skipped != "" {
		// 无法保存完整映射时原文放行，避免发送无法还原或混淆实体的占位符。
		result.SkippedReason = "mapping_capacity"
		result.ScanDuration = time.Since(started)
		return result, nil
	}

	// 仅脱敏模式生成替身；仅检测模式不改写、不创建还原映射。
	if len(scanned) > 0 {
		result.RedactedBody = replaceSegments(body, segments, scanned)
		if s.Settings.Mode == ModeRedact {
			result.Body = result.RedactedBody
			result.Changed = true
		}
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
// 超出 scan_max_bytes 时按配置扫描前缀或跳过整次扫描，其余文本原样放行。
func (s *Snapshot) scanSegments(result *ApplyResult, candidates []int, segments []textSegment) map[int]string {
	limit := s.Settings.ScanMaxBytes
	if limit <= 0 {
		limit = DefaultScanMaxBytes
	}
	if s.Settings.OverLimitAction == OverLimitSkipScan {
		// Check the whole budget before replacing any segment, so skip_scan
		// always sends the original request rather than a partially masked body.
		remaining := limit
		for _, segment := range segments {
			if int64(len(segment.Value)) > remaining {
				result.SkippedReason = SkippedScanLimit
				return nil
			}
			remaining -= int64(len(segment.Value))
		}
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
			result.Truncated = true
			result.SkippedReason = SkippedScanTruncated
			break
		}
		if int64(len(text)) > remaining {
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
			action := rule.Action
			if s.Settings.Mode == ModeDetect {
				action = ActionDetect
			}
			hit := hits[rule.ID]
			if hit == nil {
				hit = &RuleHit{RuleID: rule.ID, RuleName: rule.Name, Source: rule.Source, Action: action, Kind: RuleKind(rule.Rule)}
				hits[rule.ID] = hit
			}
			hit.Count++
			hit.Matches = append(hit.Matches, text[span.start:span.end])
			result.HitCount++
			if action == ActionRedact {
				redactSpans = append(redactSpans, span)
			}
		}
		if len(redactSpans) > 0 {
			replacements[segIdx] = s.applySpans(seg.Value, redactSpans, pseudo)
		}
	}

	result.RuleHits = sortedRuleHits(hits)
	return replacements
}

// collectMatchSpans 按规则优先级收集命中区间；与更高优先级规则重叠的命中被丢弃。
func (s *Snapshot) collectMatchSpans(candidates []int, text string) []matchSpan {
	var accepted []matchSpan
	var ordered, allowRules []int
	for _, index := range candidates {
		if s.Rules[index].Action == ActionAllow {
			allowRules = append(allowRules, index)
		} else {
			ordered = append(ordered, index)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		rank := func(index int) int {
			if RuleKind(s.Rules[index].Rule) == "private_key" {
				return 1
			}
			return 2
		}
		return rank(ordered[i]) < rank(ordered[j])
	})
	for _, ruleIdx := range ordered {
		rule := &s.Rules[ruleIdx]
		locations := findRuleMatches(rule, text)
		if s.restoration != nil && rule.Rule.usesTokens() {
			locations = append(locations, s.restoration.knownMatches(rule.ID, text)...)
		}
		for _, loc := range locations {
			if s.allowsMatch(allowRules, text[loc[0]:loc[1]]) {
				continue
			}
			// 已经是本引擎生成的替身值，不再二次替换：
			// 会话历史会反复重发，二次替换会让同一实体在上下文里出现多个假名
			if looksLikePseudonym(text[loc[0]:loc[1]]) || s.restoration != nil && s.restoration.isReplacement(text[loc[0]:loc[1]]) {
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

// 放行仅豁免完整命中值；匹配其中一部分不能放行整个敏感值。
// 字面量放行忽略大小写，与旧例外名单保持一致。
func (s *Snapshot) allowsMatch(candidates []int, raw string) bool {
	for _, index := range candidates {
		rule := &s.Rules[index]
		if rule.MatchType == MatchTypeLiteral {
			if strings.EqualFold(rule.Pattern, raw) {
				return true
			}
			continue
		}
		for _, loc := range findRuleMatches(rule, raw) {
			if loc[0] == 0 && loc[1] == len(raw) {
				return true
			}
		}
	}
	return false
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
	if rule.Rule.usesTokens() {
		if s.restoration != nil {
			return s.restoration.replaceForRule(rule.Rule, raw)
		}
		return DefaultPlaceholder
	}
	if rule.ReplacementMode == ReplacementPseudonym {
		return pseudo.replacementFor(rule.PseudonymKind, raw)
	}
	if rule.Placeholder == "" {
		return DefaultPlaceholder
	}
	return rule.Placeholder
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
// 始终按 redact 语义推演，用于本地预览，不受运行模式影响。
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
			hit = &RuleHit{RuleID: rule.ID, RuleName: rule.Name, Source: rule.Source, Action: rule.Action, Kind: RuleKind(rule.Rule)}
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
	if s.restoration != nil && s.restoration.skipped != "" {
		replaced = text
		result.Changed = false
		result.SkippedReason = "mapping_capacity"
	}
	result.Body = []byte(replaced)
	result.RuleHits = sortedRuleHits(hits)
	result.ScanDuration = time.Since(started)
	return result
}
