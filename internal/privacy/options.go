package privacy

import (
	"fmt"
	"regexp"
	"strings"
)

const privateKeyPattern = `-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----[\s\S]+?-----END (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`

var kindLabels = map[string]string{"api_key": "API 密钥", "private_key": "私钥", "token": "Token", "password": "密码", "secret": "机密", "phone": "手机号", "email": "邮箱", "id_card": "身份证", "bank_card": "银行卡", "personal": "其他个人信息", "term": "脱敏词", "custom": "自定义规则"}

var presetKinds = func() map[string]string {
	out := map[string]string{}
	for _, preset := range Presets() {
		for _, rule := range preset.Rules {
			out[rule.Pattern] = kindFromName(rule.Name)
		}
	}
	return out
}()

// RuleKind classifies existing rules without changing their saved identity.
func RuleKind(rule Rule) string {
	if rule.Action == ActionAllow {
		return "allow"
	}
	kind := legacyRuleKind(rule)
	if rule.MatchType == MatchTypeLiteral && kind == "custom" {
		return "term"
	}
	return kind
}

// Keep the original category calculation for the one-time settings migration.
func legacyRuleKind(rule Rule) string {
	if rule.Pattern == privateKeyPattern {
		return "private_key"
	}
	if rule.MatchType == MatchTypePrefix {
		return "api_key"
	}
	switch rule.Pattern {
	case "builtin:cn_mobile", "builtin:cn_landline":
		return "phone"
	case "builtin:email":
		return "email"
	case "builtin:cn_id_card":
		return "id_card"
	case "builtin:bank_card_luhn":
		return "bank_card"
	}
	if rule.MatchType == MatchTypeBuiltin {
		return "personal"
	}
	if rule.ID < 0 {
		return "term"
	}
	if _, ok := kindLabels[rule.PseudonymKind]; ok {
		return rule.PseudonymKind
	}
	// Imported presets keep their category when a user renames them.
	if rule.Source == SourcePreset {
		if kind := presetKinds[rule.Pattern]; kind != "" {
			return kind
		}
	}
	return kindFromName(rule.Name)
}

func kindFromName(name string) string {
	name = strings.ToLower(name)
	switch {
	case strings.Contains(name, "私钥"):
		return "private_key"
	case strings.Contains(name, "密码"), strings.Contains(name, "连接串"):
		return "password"
	case strings.Contains(name, "token"), strings.Contains(name, "jwt"):
		return "token"
	case strings.Contains(name, "api key"), strings.Contains(name, "access key"):
		return "api_key"
	case strings.Contains(name, "密钥"):
		return "secret"
	}
	return "custom"
}

func ruleCategory(rule Rule) string {
	switch legacyRuleKind(rule) {
	case "phone", "email", "id_card", "bank_card", "personal":
		return "personal"
	case "term":
		return "terms"
	default:
		return "secrets"
	}
}

func hasLegacyMatchingOptions(s Settings) bool {
	return len(s.Categories) > 0 || len(s.Words) > 0 || s.WordsAnyCase || len(s.Allow) > 0 || len(s.Actions) > 0
}

func normalizedLegacyOptions(s Settings) (Settings, error) {
	clean := func(values []string, anyCase bool) ([]string, error) {
		if len(values) > 200 {
			return nil, fmt.Errorf("最多配置 200 个词或例外值")
		}
		out, seen := []string{}, map[string]bool{}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if len(value) > 512 {
				return nil, fmt.Errorf("单个词或例外值不能超过 512 字节")
			}
			key := value
			if anyCase {
				key = strings.ToLower(value)
			}
			if !seen[key] {
				out = append(out, value)
				seen[key] = true
			}
		}
		return out, nil
	}
	var err error
	if s.Words, err = clean(s.Words, s.WordsAnyCase); err != nil {
		return s, err
	}
	if s.Allow, err = clean(s.Allow, true); err != nil {
		return s, err
	}
	for kind, action := range s.Actions {
		if _, ok := kindLabels[kind]; !ok {
			return s, fmt.Errorf("未知敏感类型")
		}
		if action != ActionDetect && action != ActionRedact && action != ActionBlock {
			return s, fmt.Errorf("无效敏感内容处理方式")
		}
	}
	for category := range s.Categories {
		if category != "secrets" && category != "personal" && category != "terms" {
			return s, fmt.Errorf("未知保护类别")
		}
	}
	return s, nil
}

func legacyWordRules(settings Settings) []Rule {
	var rules []Rule
	for i, word := range settings.Words {
		if strings.TrimSpace(word) == "" {
			continue
		}
		rule := Rule{ID: -int64(i + 1), Name: "业务脱敏词", Enabled: true, Priority: -100, MatchType: MatchTypeLiteral, Pattern: word, Action: ActionRedact, ReplacementMode: ReplacementPlaceholder, Placeholder: "[脱敏词]", Source: SourceCustom}
		if settings.WordsAnyCase {
			rule.MatchType = MatchTypeRegex
			rule.Pattern = "(?i)" + regexp.QuoteMeta(word)
		}
		rules = append(rules, rule)
	}
	return rules
}
