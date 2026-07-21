package privacy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

type runtimeRule struct {
	PrivacyRule
	rawPattern     string
	regex          *regexp.Regexp
	patterns       []string
	keywordRegexes []*regexp.Regexp
	structuredREs  []*regexp.Regexp
	entropyRegex   *regexp.Regexp
	minEntropyLen  int
}

const maxRedactionMatchDetails = 100

var (
	bankCardValidatorRegex = regexp.MustCompile(`\b\d{13,19}\b`)
	cnIDCardValidatorRegex = regexp.MustCompile(`\b[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]\b`)
	jwtValidatorRegex      = regexp.MustCompile(`\b[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
)

type redactionSession struct {
	values    map[string]string
	counters  map[string]int
	generated map[string]struct{}
	tracker   syntheticReplacementTracker
}

type redactionCollector struct {
	hits    map[int64]*RedactionHit
	matches []RedactionMatch
}

type syntheticReplacementTracker interface {
	LookupSyntheticMapping(raw, kind string) (string, bool)
	RegisterSyntheticMapping(raw, kind, replacement string)
	RegisterSyntheticValue(value, kind string)
	SyntheticReplacementInUse(kind, replacement string) bool
	TouchSyntheticValue(value string) bool
	LogSyntheticValueSkip(value string)
}

type syntheticAllocationLocker interface {
	LockSyntheticAllocation()
	UnlockSyntheticAllocation()
}

func newRedactionSession(tracker syntheticReplacementTracker) *redactionSession {
	return &redactionSession{
		values:    make(map[string]string),
		counters:  make(map[string]int),
		generated: make(map[string]struct{}),
		tracker:   tracker,
	}
}

func newRedactionCollector() *redactionCollector {
	return &redactionCollector{
		hits: make(map[int64]*RedactionHit),
	}
}

func compileRuntimeRules(rules []PrivacyRule) ([]runtimeRule, error) {
	compiled := make([]runtimeRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		raw := strings.TrimSpace(rule.PatternOrValue)
		if raw == "" {
			return nil, fmt.Errorf("隐私规则 %q 缺少匹配内容", rule.Name)
		}
		if rule.ReplacementMode == "" {
			rule.ReplacementMode = ReplacementModePlaceholder
		}
		rt := runtimeRule{PrivacyRule: rule, rawPattern: raw}

		switch rule.Type {
		case RuleTypeRegex:
			re, err := regexp.Compile(raw)
			if err != nil {
				return nil, fmt.Errorf("隐私规则 %q 正则无效: %w", rule.Name, err)
			}
			rt.regex = re
		case RuleTypeField, RuleTypeStructured:
			rt.patterns = parseRuleList(raw)
			if len(rt.patterns) == 0 {
				return nil, fmt.Errorf("隐私规则 %q 缺少字段名", rule.Name)
			}
			if rule.Type == RuleTypeStructured {
				for _, key := range rt.patterns {
					rt.structuredREs = append(rt.structuredREs, regexp.MustCompile(`(?i)(^|[?&;\s])(`+regexp.QuoteMeta(key)+`)(\s*[=:]\s*)([^&;\s,，;；]+)`))
				}
			}
		case RuleTypeKeyword:
			rt.patterns = parseRuleList(raw)
			if len(rt.patterns) == 0 {
				return nil, fmt.Errorf("隐私规则 %q 缺少关键词", rule.Name)
			}
			for _, keyword := range rt.patterns {
				rt.keywordRegexes = append(rt.keywordRegexes, regexp.MustCompile(`(?i)(`+regexp.QuoteMeta(keyword)+`)(\s*[:：=]\s*)([^\s,，;；&"'<>{}\[\]()`+"`"+`]+)`))
			}
		case RuleTypeEntropy:
			minLength, err := strconv.Atoi(raw)
			if err != nil || minLength < 8 {
				return nil, fmt.Errorf("隐私规则 %q 高熵最小长度无效", rule.Name)
			}
			rt.minEntropyLen = minLength
			rt.entropyRegex = regexp.MustCompile(`[A-Za-z0-9_\-+/=]{` + strconv.Itoa(minLength) + `,}`)
		case RuleTypeValidator:
			rt.patterns = parseRuleList(raw)
			if len(rt.patterns) == 0 {
				return nil, fmt.Errorf("隐私规则 %q 缺少校验算法", rule.Name)
			}
		}

		compiled = append(compiled, rt)
	}
	sortRuntimeRules(compiled)
	return compiled, nil
}

func parseRuleList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '，'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func sortRuntimeRules(rules []runtimeRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		left, right := rules[i], rules[j]
		if left.Priority != right.Priority {
			return left.Priority < right.Priority
		}
		if left.Type == RuleTypeExact && right.Type == RuleTypeExact {
			if len([]rune(left.rawPattern)) != len([]rune(right.rawPattern)) {
				return len([]rune(left.rawPattern)) > len([]rune(right.rawPattern))
			}
		}
		return left.ID < right.ID
	})
}

func RedactWithRules(body []byte, rules []runtimeRule) ([]byte, RedactionSummary, bool, error) {
	return redactWithRules(body, rules, nil)
}

func redactWithRules(body []byte, rules []runtimeRule, tracker syntheticReplacementTracker) ([]byte, RedactionSummary, bool, error) {
	if len(body) == 0 || len(rules) == 0 {
		return body, RedactionSummary{}, false, nil
	}

	session := newRedactionSession(tracker)
	trimmed := bytes.TrimSpace(body)
	if value, ok := decodeJSONValue(trimmed); ok {
		collector := newRedactionCollector()
		changedValue, changed := redactJSONValue(value, nil, false, rules, collector, session)
		if !changed {
			return body, RedactionSummary{}, false, nil
		}
		redacted, err := json.Marshal(changedValue)
		if err != nil {
			return nil, RedactionSummary{}, false, err
		}
		summary := collector.summary()
		return redacted, summary, true, nil
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return nil, RedactionSummary{}, false, fmt.Errorf("隐私防护不支持非 UTF-8 或二进制请求体")
	}

	collector := newRedactionCollector()
	output, changed := redactString(string(body), rules, collector, session)
	if !changed {
		return body, RedactionSummary{}, false, nil
	}
	summary := collector.summary()
	return []byte(output), summary, true, nil
}

func decodeJSONValue(body []byte) (any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	return value, true
}

func redactJSONValue(value any, path []string, inContext bool, rules []runtimeRule, collector *redactionCollector, session *redactionSession) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for key, child := range typed {
			nextPath := appendPath(path, key)
			if shouldSkipBinaryPayloadField(typed, key, child, nextPath) {
				continue
			}
			nextContext := nextJSONContextState(nextPath, inContext)
			if text, ok := child.(string); ok && nextContext {
				replaced := false
				for _, rule := range rules {
					if rule.Type != RuleTypeField || !fieldRuleMatches(rule, key, nextPath) {
						continue
					}
					replacement := session.replacementFor(rule, text)
					if replacement == text {
						continue
					}
					typed[key] = replacement
					collector.addHit(rule, 1)
					collector.addMatch(rule, text, replacement)
					changed = true
					replaced = true
					break
				}
				if replaced {
					continue
				}
			}
			next, childChanged := redactJSONValue(child, nextPath, nextContext, rules, collector, session)
			if childChanged {
				typed[key] = next
				changed = true
			}
		}
		return typed, changed
	case []any:
		changed := false
		for i, child := range typed {
			next, childChanged := redactJSONValue(child, path, inContext, rules, collector, session)
			if childChanged {
				typed[i] = next
				changed = true
			}
		}
		return typed, changed
	case string:
		if !inContext {
			return value, false
		}
		return redactString(typed, rules, collector, session)
	default:
		return value, false
	}
}

func nextJSONContextState(path []string, parentInContext bool) bool {
	if len(path) == 0 {
		return parentInContext
	}
	key := strings.ToLower(path[len(path)-1])
	freeformPayload := pathContainsJSONFreeformPayload(path)
	if !freeformPayload && pathContainsKey(path, "tools") {
		return pathContainsToolTextPayload(path)
	}
	if !freeformPayload && (isJSONProtocolKey(key) || pathContainsJSONProtocolRoot(path)) {
		return false
	}
	if freeformPayload {
		return true
	}
	if isJSONContextRoot(path[0]) {
		return true
	}
	return parentInContext && isJSONContextTextKey(key)
}

func isJSONContextRoot(key string) bool {
	switch strings.ToLower(key) {
	case "input", "instructions", "messages", "prompt", "system":
		return true
	default:
		return false
	}
}

func isJSONContextTextKey(key string) bool {
	switch strings.ToLower(key) {
	case "content", "text", "output", "arguments", "input", "prompt":
		return true
	default:
		return false
	}
}

func isJSONProtocolKey(key string) bool {
	switch strings.ToLower(key) {
	case "call_id", "client_metadata", "encrypted_content", "id", "include", "metadata", "model", "name", "previous_response_id", "reasoning", "response_id", "role", "status", "stream", "tools", "type":
		return true
	default:
		return false
	}
}

func pathContainsJSONProtocolRoot(path []string) bool {
	if len(path) == 0 {
		return false
	}
	for _, part := range path {
		key := strings.ToLower(part)
		if key == "client_metadata" || key == "metadata" || key == "encrypted_content" || key == "reasoning" {
			return true
		}
	}
	return false
}

func pathContainsKey(path []string, expected string) bool {
	for _, part := range path {
		if strings.EqualFold(part, expected) {
			return true
		}
	}
	return false
}

func pathContainsJSONFreeformPayload(path []string) bool {
	for index, part := range path {
		switch strings.ToLower(part) {
		case "arguments", "output":
			return true
		case "input":
			if index > 0 && strings.EqualFold(path[index-1], "content") {
				return true
			}
		}
	}
	return false
}

func pathContainsToolTextPayload(path []string) bool {
	insideTools := false
	for _, part := range path {
		key := strings.ToLower(part)
		if key == "tools" {
			insideTools = true
			continue
		}
		if insideTools && (key == "description" || key == "default" || key == "example" || key == "examples") {
			return true
		}
	}
	return false
}

func shouldSkipBinaryPayloadField(object map[string]any, key string, child any, path []string) bool {
	// ponytail: binary payloads stay opaque; add typed text-attachment decoding only when provider formats require it.
	typeName, _ := object["type"].(string)
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	key = strings.ToLower(key)
	switch typeName {
	case "image", "image_url", "input_image", "computer_screenshot":
		if key == "data" {
			return true
		}
		if key == "source" || key == "image_url" || key == "url" {
			return isInlineBinaryPayload(child)
		}
	case "audio", "input_audio":
		if key == "data" {
			return true
		}
		if key == "audio" || key == "input_audio" {
			return isInlineBinaryPayload(child)
		}
	case "document", "file", "input_file":
		if key == "data" || key == "file_data" {
			return true
		}
		if key == "source" {
			return isInlineBinaryPayload(child)
		}
	case "base64":
		return key == "data"
	}
	if key == "data" && (pathContainsKey(path, "audio") || pathContainsKey(path, "input_audio")) {
		return true
	}
	if key == "image_url" || key == "url" {
		return isInlineBinaryPayload(child)
	}
	return false
}

func isInlineBinaryPayload(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "data:")
}

func appendPath(path []string, key string) []string {
	next := make([]string, 0, len(path)+1)
	next = append(next, path...)
	next = append(next, key)
	return next
}

func fieldRuleMatches(rule runtimeRule, key string, path []string) bool {
	pathValue := "$." + strings.Join(path, ".")
	for _, pattern := range rule.patterns {
		if strings.HasPrefix(pattern, "$.") {
			if pattern == pathValue {
				return true
			}
			continue
		}
		if pattern == key {
			return true
		}
	}
	return false
}

func redactString(input string, rules []runtimeRule, collector *redactionCollector, session *redactionSession) (string, bool) {
	output := input
	changed := false
	for _, rule := range rules {
		count := 0
		switch rule.Type {
		case RuleTypeExact:
			if rule.rawPattern == "" {
				continue
			}
			count = strings.Count(output, rule.rawPattern)
			if count > 0 {
				replacement := session.replacementFor(rule, rule.rawPattern)
				if replacement == rule.rawPattern {
					count = 0
					continue
				}
				output = strings.ReplaceAll(output, rule.rawPattern, replacement)
				collector.addMatches(rule, rule.rawPattern, replacement, count)
			}
		case RuleTypeRegex:
			if rule.regex == nil {
				continue
			}
			output, count = replaceFullMatches(output, rule.regex, func(raw string) string {
				category, replacementKey, ok := regexReplacementContext(rule, raw, session)
				if !ok {
					return raw
				}
				replacement := session.replacementForCategory(rule, category, replacementKey)
				if replacement != raw {
					collector.addMatchWithCategory(rule, category, raw, replacement)
				}
				return replacement
			})
		case RuleTypeKeyword:
			output, count = redactKeywordString(output, rule, collector, session)
		case RuleTypeValidator:
			output, count = redactValidatorString(output, rule, collector, session)
		case RuleTypeEntropy:
			output, count = redactEntropyString(output, rule, collector, session)
		case RuleTypeStructured:
			output, count = redactStructuredString(output, rule, collector, session)
		}
		if count > 0 {
			changed = true
			collector.addHit(rule, count)
		}
	}
	return output, changed
}

func regexReplacementContext(rule runtimeRule, raw string, session *redactionSession) (category string, replacementKey string, ok bool) {
	if session != nil && session.isSyntheticReplacementValue(raw) {
		return "", "", false
	}
	if isBankCardRegexRule(rule) {
		digits, valid := bankCardCandidateDigits(raw)
		if !valid {
			return "", "", false
		}
		return CategoryBankCard, digits, true
	}
	if isIDCardRegexRule(rule) {
		if !cnIDCardValid(raw) {
			return "", "", false
		}
		return CategoryIDCard, raw, true
	}
	if isJWTRegexRule(rule) {
		if !jwtStructureValid(raw) {
			return "", "", false
		}
		return CategoryToken, raw, true
	}
	return rule.Category, raw, true
}

func isBankCardRegexRule(rule runtimeRule) bool {
	if rule.Category == CategoryBankCard {
		return true
	}
	text := strings.ToLower(rule.Name + " " + rule.rawPattern)
	return containsAny(text, "bank_card", "bankcard", "银行卡")
}

func isIDCardRegexRule(rule runtimeRule) bool {
	if rule.Category == CategoryIDCard {
		return true
	}
	text := strings.ToLower(rule.Name + " " + rule.rawPattern)
	return containsAny(text, "id_card", "idcard", "身份证")
}

func isJWTRegexRule(rule runtimeRule) bool {
	text := strings.ToLower(rule.Name + " " + rule.rawPattern)
	return containsAny(text, "jwt", "json web token")
}

func replaceFullMatches(input string, re *regexp.Regexp, replacement func(string) string) (string, int) {
	count := 0
	output := re.ReplaceAllStringFunc(input, func(raw string) string {
		next := replacement(raw)
		if next != raw {
			count++
		}
		return next
	})
	return output, count
}

func replaceGroupMatches(input string, re *regexp.Regexp, groupIndex int, replacement func(string) string) (string, int) {
	matches := re.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, 0
	}

	var builder strings.Builder
	last := 0
	count := 0
	for _, match := range matches {
		startIndex := groupIndex * 2
		if len(match) <= startIndex+1 || match[startIndex] < 0 || match[startIndex+1] < 0 {
			continue
		}
		start, end := match[startIndex], match[startIndex+1]
		raw := input[start:end]
		next := replacement(raw)
		builder.WriteString(input[last:start])
		builder.WriteString(next)
		last = end
		if next != raw {
			count++
		}
	}
	builder.WriteString(input[last:])
	return builder.String(), count
}

func redactKeywordString(input string, rule runtimeRule, collector *redactionCollector, session *redactionSession) (string, int) {
	output := input
	total := 0
	for _, re := range rule.keywordRegexes {
		next, count := replaceGroupMatches(output, re, 3, func(raw string) string {
			replacement := session.replacementFor(rule, raw)
			if replacement != raw {
				collector.addMatch(rule, raw, replacement)
			}
			return replacement
		})
		output = next
		total += count
	}
	return output, total
}

func redactStructuredString(input string, rule runtimeRule, collector *redactionCollector, session *redactionSession) (string, int) {
	output := input
	total := 0
	for _, re := range rule.structuredREs {
		next, count := replaceGroupMatches(output, re, 4, func(raw string) string {
			replacement := session.replacementFor(rule, raw)
			if replacement != raw {
				collector.addMatch(rule, raw, replacement)
			}
			return replacement
		})
		output = next
		total += count
	}
	return output, total
}

func redactEntropyString(input string, rule runtimeRule, collector *redactionCollector, session *redactionSession) (string, int) {
	minLength := rule.minEntropyLen
	if minLength <= 0 {
		minLength = 32
	}
	re := rule.entropyRegex
	if re == nil {
		re = regexp.MustCompile(`[A-Za-z0-9_\-+/=]{` + strconv.Itoa(minLength) + `,}`)
	}
	return replaceFullMatches(input, re, func(raw string) string {
		if !isHighEntropyToken(raw, minLength) {
			return raw
		}
		replacement := session.replacementFor(rule, raw)
		if replacement != raw {
			collector.addMatch(rule, raw, replacement)
		}
		return replacement
	})
}

func isHighEntropyToken(value string, minLength int) bool {
	if len(value) < minLength || strings.HasPrefix(value, "REDACTED_") {
		return false
	}
	var hasLetter, hasDigit, hasSymbol bool
	frequencies := make(map[rune]int)
	var total int
	for _, r := range value {
		total++
		frequencies[r]++
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			hasLetter = true
		default:
			hasSymbol = true
		}
	}
	if total == 0 || !hasLetter || (!hasDigit && !hasSymbol) {
		return false
	}
	var entropy float64
	for _, count := range frequencies {
		p := float64(count) / float64(total)
		entropy -= p * math.Log2(p)
	}
	return entropy >= 3.5
}

func redactValidatorString(input string, rule runtimeRule, collector *redactionCollector, session *redactionSession) (string, int) {
	output := input
	total := 0
	for _, validator := range rule.patterns {
		switch strings.ToLower(validator) {
		case "bank_card_luhn":
			next, count := replaceFullMatches(output, bankCardValidatorRegex, func(raw string) string {
				if !luhnValid(raw) {
					return raw
				}
				replacement := session.replacementForCategory(rule, CategoryBankCard, raw)
				if replacement != raw {
					collector.addMatchWithCategory(rule, CategoryBankCard, raw, replacement)
				}
				return replacement
			})
			output = next
			total += count
		case "cn_id_card":
			next, count := replaceFullMatches(output, cnIDCardValidatorRegex, func(raw string) string {
				if !cnIDCardValid(raw) {
					return raw
				}
				replacement := session.replacementForCategory(rule, CategoryIDCard, raw)
				if replacement != raw {
					collector.addMatchWithCategory(rule, CategoryIDCard, raw, replacement)
				}
				return replacement
			})
			output = next
			total += count
		case "jwt":
			next, count := replaceFullMatches(output, jwtValidatorRegex, func(raw string) string {
				if !jwtStructureValid(raw) {
					return raw
				}
				replacement := session.replacementForCategory(rule, CategoryToken, raw)
				if replacement != raw {
					collector.addMatchWithCategory(rule, CategoryToken, raw, replacement)
				}
				return replacement
			})
			output = next
			total += count
		}
	}
	return output, total
}

func luhnValid(value string) bool {
	var sum int
	double := false
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
		n := int(value[i] - '0')
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	return sum > 0 && sum%10 == 0
}

func bankCardCandidateDigits(value string) (string, bool) {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == ' ' || r == '\t':
			continue
		default:
			return "", false
		}
	}
	digits := builder.String()
	if len(digits) < 13 || len(digits) > 19 {
		return "", false
	}
	if allDigitsSame(digits) || looksLikeCompactDateTime(digits) || looksLikeCNIDCardNumber(digits) {
		return "", false
	}
	if !luhnValid(digits) {
		return "", false
	}
	return digits, true
}

func allDigitsSame(value string) bool {
	if value == "" {
		return false
	}
	first := value[0]
	for i := 1; i < len(value); i++ {
		if value[i] != first {
			return false
		}
	}
	return true
}

func looksLikeCompactDateTime(value string) bool {
	if len(value) != 14 {
		return false
	}
	year, ok := parseFixedDigits(value[0:4])
	if !ok || year < 1900 || year > 2099 {
		return false
	}
	month, ok := parseFixedDigits(value[4:6])
	if !ok {
		return false
	}
	day, ok := parseFixedDigits(value[6:8])
	if !ok || !validMonthDay(year, month, day) {
		return false
	}
	hour, ok := parseFixedDigits(value[8:10])
	if !ok || hour > 23 {
		return false
	}
	minute, ok := parseFixedDigits(value[10:12])
	if !ok || minute > 59 {
		return false
	}
	second, ok := parseFixedDigits(value[12:14])
	if !ok || second > 59 {
		return false
	}
	return true
}

func looksLikeCNIDCardNumber(value string) bool {
	if len(value) != 18 || value[0] == '0' {
		return false
	}
	year, ok := parseFixedDigits(value[6:10])
	if !ok || year < 1900 || year > 2099 {
		return false
	}
	month, ok := parseFixedDigits(value[10:12])
	if !ok {
		return false
	}
	day, ok := parseFixedDigits(value[12:14])
	if !ok {
		return false
	}
	return validMonthDay(year, month, day)
}

func parseFixedDigits(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	result := 0
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
		result = result*10 + int(value[i]-'0')
	}
	return result, true
}

func validMonthDay(year, month, day int) bool {
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	daysInMonth := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	maxDay := daysInMonth[month-1]
	if month == 2 && isLeapYear(year) {
		maxDay = 29
	}
	return day <= maxDay
}

func isLeapYear(year int) bool {
	return year%400 == 0 || (year%4 == 0 && year%100 != 0)
}

func cnIDCardValid(value string) bool {
	if len(value) != 18 {
		return false
	}
	weights := []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	checks := []byte{'1', '0', 'X', '9', '8', '7', '6', '5', '4', '3', '2'}
	var sum int
	for i := 0; i < 17; i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
		sum += int(value[i]-'0') * weights[i]
	}
	actual := value[17]
	if actual >= 'a' && actual <= 'z' {
		actual -= 'a' - 'A'
	}
	return actual == checks[sum%11]
}

func jwtStructureValid(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return false
	}
	for i := 0; i < 2; i++ {
		if _, err := base64.RawURLEncoding.DecodeString(parts[i]); err != nil {
			return false
		}
	}
	return true
}

func (s *redactionSession) replacementFor(rule runtimeRule, raw string) string {
	return s.replacementForCategory(rule, rule.Category, raw)
}

func (s *redactionSession) replacementForCategory(rule runtimeRule, category string, raw string) string {
	if s.isSyntheticReplacementValue(raw) {
		return raw
	}
	if existing := s.values[raw]; existing != "" {
		return existing
	}
	kind := syntheticKindForRule(rule, category)
	if rule.ReplacementMode == ReplacementModePlaceholder {
		replacement := placeholderValueForKind(kind)
		s.values[raw] = replacement
		s.generated[replacement] = struct{}{}
		return replacement
	}
	if s.tracker != nil {
		if existing, ok := s.tracker.LookupSyntheticMapping(raw, kind); ok {
			s.values[raw] = existing
			s.generated[existing] = struct{}{}
			rememberSyntheticReplacementValue(existing)
			return existing
		}
	}
	if locker, ok := s.tracker.(syntheticAllocationLocker); ok {
		locker.LockSyntheticAllocation()
		defer locker.UnlockSyntheticAllocation()
		if existing, found := s.tracker.LookupSyntheticMapping(raw, kind); found {
			s.values[raw] = existing
			s.generated[existing] = struct{}{}
			rememberSyntheticReplacementValue(existing)
			return existing
		}
	}
	var replacement string
	maxAttempts := maxSyntheticGenerationAttempts(kind)
	for attempts := 0; ; attempts++ {
		s.counters[kind]++
		replacement = syntheticValueForKind(kind, s.counters[kind], raw)
		inUse := replacement == raw
		if s.tracker != nil && s.tracker.SyntheticReplacementInUse(kind, replacement) {
			inUse = true
		}
		if !inUse {
			break
		}
		if attempts >= maxAttempts {
			break
		}
	}
	s.values[raw] = replacement
	s.generated[replacement] = struct{}{}
	rememberSyntheticReplacementValue(replacement)
	if s.tracker != nil {
		s.tracker.RegisterSyntheticMapping(raw, kind, replacement)
		s.tracker.RegisterSyntheticValue(replacement, kind)
	}
	return replacement
}

func (s *redactionSession) isSyntheticReplacementValue(value string) bool {
	if value == "" {
		return false
	}
	if s != nil {
		if _, ok := s.generated[value]; ok {
			if s.tracker != nil {
				s.tracker.LogSyntheticValueSkip(value)
			}
			return true
		}
	}
	if s != nil && s.tracker != nil && s.tracker.TouchSyntheticValue(value) {
		return true
	}
	return false
}

const (
	syntheticKindAPIKey   = "api_key"
	syntheticKindBankCard = "bank_card"
	syntheticKindCustom   = "custom"
	syntheticKindIDCard   = "id_card"
	syntheticKindName     = "name"
	syntheticKindPhone    = "phone"
	syntheticKindSecret   = "secret"
	syntheticKindToken    = "token"
)

var syntheticNames = []string{
	"张三",
	"李四",
	"王五",
	"赵六",
	"钱七",
	"孙八",
	"周九",
	"吴十",
	"郑十一",
	"王十二",
}

func syntheticKindForRule(rule runtimeRule, category string) string {
	text := strings.ToLower(rule.Name + " " + rule.rawPattern + " " + strings.Join(rule.patterns, " "))
	if containsAny(text, "api_key", "apikey", "api-key", "x-api-key", "api key") {
		return syntheticKindAPIKey
	}
	if kind := syntheticKindFromCategory(category); kind != syntheticKindCustom {
		return kind
	}
	switch {
	case containsAny(text, "token", "jwt", "bearer", "session"):
		return syntheticKindToken
	case containsAny(text, "secret", "password", "passwd", "pwd", "private_key", "client_secret", "密钥", "密码"):
		return syntheticKindSecret
	case containsAny(text, "name", "姓名", "联系人"):
		return syntheticKindName
	case containsAny(text, "phone", "mobile", "tel", "手机号", "电话", "手机"):
		return syntheticKindPhone
	case containsAny(text, "id_card", "idcard", "身份证"):
		return syntheticKindIDCard
	case containsAny(text, "bank_card", "bankcard", "银行卡", "卡号"):
		return syntheticKindBankCard
	default:
		return syntheticKindCustom
	}
}

func syntheticKindFromCategory(category string) string {
	switch category {
	case CategorySecret:
		return syntheticKindSecret
	case CategoryToken:
		return syntheticKindToken
	case CategoryName:
		return syntheticKindName
	case CategoryPhone:
		return syntheticKindPhone
	case CategoryBankCard:
		return syntheticKindBankCard
	case CategoryIDCard:
		return syntheticKindIDCard
	default:
		return syntheticKindCustom
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func syntheticValueForKind(kind string, index int, raw string) string {
	sequence := formatSequence(index)
	switch kind {
	case syntheticKindAPIKey:
		return "fake_api_key_" + sequence
	case syntheticKindBankCard:
		return syntheticBankCard(index)
	case syntheticKindIDCard:
		return syntheticIDCard(index)
	case syntheticKindName:
		return syntheticName(index, raw)
	case syntheticKindPhone:
		return syntheticPhone(index)
	case syntheticKindSecret:
		return "fake_secret_" + sequence
	case syntheticKindToken:
		return "fake_token_" + sequence
	default:
		return "fake_value_" + sequence
	}
}

func placeholderValueForKind(kind string) string {
	switch kind {
	case syntheticKindAPIKey:
		return "REDACTED_API_KEY"
	case syntheticKindBankCard:
		return "REDACTED_BANK_CARD"
	case syntheticKindIDCard:
		return "REDACTED_ID_CARD"
	case syntheticKindName:
		return "REDACTED_NAME"
	case syntheticKindPhone:
		return "REDACTED_PHONE"
	case syntheticKindSecret:
		return "REDACTED_SECRET"
	case syntheticKindToken:
		return "REDACTED_TOKEN"
	default:
		return "REDACTED_VALUE"
	}
}

func maxSyntheticGenerationAttempts(kind string) int {
	switch kind {
	case syntheticKindIDCard:
		return 899
	case syntheticKindName:
		return len(syntheticNames) + 10000
	default:
		return 10000
	}
}

func isSyntheticReplacementValue(value string) bool {
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "REDACTED_") {
		return true
	}
	if isRememberedSyntheticReplacementValue(value) {
		return true
	}
	if _, ok := knownSyntheticReplacementKind(value); ok {
		return true
	}
	return false
}

func knownSyntheticReplacementKind(value string) (string, bool) {
	if kind, ok := syntheticPrefixedValueKind(value); ok {
		return kind, true
	}
	if reservedSyntheticPhone(value) {
		return syntheticKindPhone, true
	}
	if reservedSyntheticIDCard(value) {
		return syntheticKindIDCard, true
	}
	if reservedSyntheticBankCard(value) {
		return syntheticKindBankCard, true
	}
	return "", false
}

var syntheticReplacementRegistry = struct {
	sync.RWMutex
	values map[string]struct{}
}{
	values: make(map[string]struct{}),
}

func rememberSyntheticReplacementValue(value string) {
	if value == "" {
		return
	}
	syntheticReplacementRegistry.Lock()
	syntheticReplacementRegistry.values[value] = struct{}{}
	syntheticReplacementRegistry.Unlock()
}

func isRememberedSyntheticReplacementValue(value string) bool {
	syntheticReplacementRegistry.RLock()
	_, ok := syntheticReplacementRegistry.values[value]
	syntheticReplacementRegistry.RUnlock()
	return ok
}

func clearSyntheticReplacementRegistry() {
	syntheticReplacementRegistry.Lock()
	syntheticReplacementRegistry.values = make(map[string]struct{})
	syntheticReplacementRegistry.Unlock()
}

func syntheticPrefixedValueKind(value string) (string, bool) {
	prefixes := map[string]string{
		"fake_api_key_": syntheticKindAPIKey,
		"fake_secret_":  syntheticKindSecret,
		"fake_token_":   syntheticKindToken,
		"fake_value_":   syntheticKindCustom,
	}
	for prefix, kind := range prefixes {
		if strings.HasPrefix(value, prefix) {
			seq := strings.TrimPrefix(value, prefix)
			return kind, len(seq) == 4 && allASCIIBytesDigits(seq)
		}
	}
	return "", false
}

func reservedSyntheticPhone(value string) bool {
	if len(value) != 11 || !strings.HasPrefix(value, "1341111") {
		return false
	}
	serial, ok := parseFixedDigits(value[7:])
	return ok && serial >= 1 && serial <= 9999
}

func reservedSyntheticIDCard(value string) bool {
	if len(value) != 18 || !strings.HasPrefix(value, "42082219990101") {
		return false
	}
	serial, ok := parseFixedDigits(value[14:17])
	return ok && serial >= 101 && serial <= 999 && cnIDCardValid(value)
}

func reservedSyntheticBankCard(value string) bool {
	if len(value) != 16 || !strings.HasPrefix(value, "62220200000") {
		return false
	}
	serial, ok := parseFixedDigits(value[11:15])
	return ok && serial >= 1 && serial <= 9999 && luhnValid(value)
}

func allASCIIBytesDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func formatSequence(index int) string {
	return fmt.Sprintf("%04d", index)
}

func syntheticName(index int, raw string) string {
	if len(syntheticNames) == 0 {
		return "用户" + fmt.Sprintf("%04d", index)
	}
	if index <= len(syntheticNames) {
		start := (index - 1) % len(syntheticNames)
		for offset := 0; offset < len(syntheticNames); offset++ {
			candidate := syntheticNames[(start+offset)%len(syntheticNames)]
			if candidate != raw {
				return candidate
			}
		}
	}
	for sequence := index - len(syntheticNames); ; sequence++ {
		candidate := "用户" + fmt.Sprintf("%04d", sequence)
		if candidate != raw {
			return candidate
		}
	}
}

func syntheticPhone(index int) string {
	serial := ((index - 1) % 10000) + 1
	return fmt.Sprintf("1341111%04d", serial)
}

func syntheticIDCard(index int) string {
	serial := 101 + ((index - 1) % 899)
	base := fmt.Sprintf("42082219990101%03d", serial)
	return base + cnIDCardCheckDigit(base)
}

func cnIDCardCheckDigit(base string) string {
	if len(base) != 17 {
		return "0"
	}
	weights := []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	checks := []byte{'1', '0', 'X', '9', '8', '7', '6', '5', '4', '3', '2'}
	var sum int
	for i := 0; i < 17; i++ {
		if base[i] < '0' || base[i] > '9' {
			return "0"
		}
		sum += int(base[i]-'0') * weights[i]
	}
	return string(checks[sum%11])
}

func syntheticBankCard(index int) string {
	serial := ((index - 1) % 10000) + 1
	base := fmt.Sprintf("62220200000%04d", serial)
	return base + luhnCheckDigit(base)
}

func luhnCheckDigit(base string) string {
	for digit := byte('0'); digit <= '9'; digit++ {
		candidate := base + string(digit)
		if luhnValid(candidate) {
			return string(digit)
		}
	}
	return "0"
}

func (c *redactionCollector) addHit(rule runtimeRule, count int) {
	if c == nil || count <= 0 {
		return
	}
	hit, ok := c.hits[rule.ID]
	if !ok {
		hit = &RedactionHit{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Type:     rule.Type,
			Category: rule.Category,
		}
		c.hits[rule.ID] = hit
	}
	hit.Count += count
}

func (c *redactionCollector) addMatch(rule runtimeRule, raw, replacement string) {
	c.addMatchWithCategory(rule, rule.Category, raw, replacement)
}

func (c *redactionCollector) addMatches(rule runtimeRule, raw, replacement string, count int) {
	if c == nil || count <= 0 {
		return
	}
	remaining := maxRedactionMatchDetails - len(c.matches)
	if count > remaining {
		count = remaining
	}
	for i := 0; i < count; i++ {
		c.addMatch(rule, raw, replacement)
	}
}

func (c *redactionCollector) addMatchWithCategory(rule runtimeRule, category, raw, replacement string) {
	if c == nil || len(c.matches) >= maxRedactionMatchDetails || raw == "" || replacement == "" || raw == replacement {
		return
	}
	c.matches = append(c.matches, RedactionMatch{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Type:        rule.Type,
		Category:    category,
		Raw:         raw,
		RawBase64:   base64.StdEncoding.EncodeToString([]byte(raw)),
		Replacement: replacement,
	})
}

func (c *redactionCollector) summary() RedactionSummary {
	if c == nil || len(c.hits) == 0 {
		return RedactionSummary{}
	}
	result := RedactionSummary{
		Changed: true,
		Hits:    make([]RedactionHit, 0, len(c.hits)),
		Matches: append([]RedactionMatch(nil), c.matches...),
	}
	for _, hit := range c.hits {
		result.Hits = append(result.Hits, *hit)
	}
	sort.Slice(result.Hits, func(i, j int) bool {
		if result.Hits[i].RuleID != result.Hits[j].RuleID {
			return result.Hits[i].RuleID < result.Hits[j].RuleID
		}
		return result.Hits[i].RuleName < result.Hits[j].RuleName
	})
	sort.SliceStable(result.Matches, func(i, j int) bool {
		if result.Matches[i].RuleID != result.Matches[j].RuleID {
			return result.Matches[i].RuleID < result.Matches[j].RuleID
		}
		if result.Matches[i].Raw != result.Matches[j].Raw {
			return result.Matches[i].Raw < result.Matches[j].Raw
		}
		return result.Matches[i].Replacement < result.Matches[j].Replacement
	})
	return result
}
