package privacy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	store       *Store
	enabledFunc func(context.Context) bool
	logger      *slog.Logger
}

func NewService(store *Store, enabledFunc func(context.Context) bool, logger ...*slog.Logger) *Service {
	var selectedLogger *slog.Logger
	if len(logger) > 0 {
		selectedLogger = logger[0]
	}
	return &Service{
		store:       store,
		enabledFunc: enabledFunc,
		logger:      selectedLogger,
	}
}

func (s *Service) ListRules(ctx context.Context) ([]PrivacyRule, error) {
	return s.store.ListRules(ctx)
}

func (s *Service) ListSyntheticValues(ctx context.Context) ([]SyntheticValueRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("privacy store not initialized")
	}
	return s.store.ListSyntheticValues(ctx)
}

func (s *Service) ClearSyntheticValues(ctx context.Context) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	return s.store.ClearSyntheticValues(ctx)
}

func (s *Service) DeleteSyntheticValue(ctx context.Context, kind, rawHash string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	return s.store.DeleteSyntheticValue(ctx, kind, rawHash)
}

func (s *Service) CreateRule(ctx context.Context, input PrivacyRuleInput) (PrivacyRule, error) {
	normalized, err := NormalizeInput(input)
	if err != nil {
		return PrivacyRule{}, err
	}
	return s.store.CreateRule(ctx, normalized)
}

func (s *Service) UpdateRule(ctx context.Context, id int64, input PrivacyRuleInput) (PrivacyRule, error) {
	normalized, err := NormalizeInputForUpdate(input)
	if err != nil {
		return PrivacyRule{}, err
	}
	if strings.TrimSpace(normalized.PatternOrValue) == "" {
		existing, err := s.store.GetRuleRaw(ctx, id)
		if err != nil {
			return PrivacyRule{}, err
		}
		normalized.PatternOrValue = existing.PatternOrValue
	}
	if normalized.Type == RuleTypeRegex && normalized.Enabled {
		if _, err := regexp.Compile(normalized.PatternOrValue); err != nil {
			return PrivacyRule{}, fmt.Errorf("正则表达式无效: %w", err)
		}
	}
	return s.store.UpdateRule(ctx, id, normalized)
}

func (s *Service) DeleteRule(ctx context.Context, id int64) error {
	return s.store.DeleteRule(ctx, id)
}

func (s *Service) Redact(body []byte) ([]byte, RedactionSummary, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s == nil || s.store == nil {
		return body, RedactionSummary{}, false, nil
	}

	rules, err := s.store.ListEnabledRules(ctx)
	if err != nil {
		return nil, RedactionSummary{}, false, err
	}
	compiled, err := compileRuntimeRules(rules)
	if err != nil {
		return nil, RedactionSummary{}, false, err
	}
	if len(compiled) == 0 {
		return body, RedactionSummary{}, false, nil
	}

	tracker := &serviceSyntheticReplacementTracker{
		ctx:    ctx,
		store:  s.store,
		logger: s.logger,
	}
	redacted, summary, changed, err := redactWithRules(body, compiled, tracker)
	if err != nil {
		return nil, RedactionSummary{}, false, err
	}
	return redacted, summary, changed, nil
}

func (s *Service) Preview(ctx context.Context, input string) (PreviewResult, error) {
	if s == nil || s.store == nil {
		return PreviewResult{Output: input}, nil
	}
	rules, err := s.store.ListEnabledRules(ctx)
	if err != nil {
		return PreviewResult{}, err
	}
	compiled, err := compileRuntimeRules(rules)
	if err != nil {
		return PreviewResult{}, err
	}
	output, summary, changed, err := RedactWithRules([]byte(input), compiled)
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Output: string(output), Changed: changed, Hits: summary.Hits}, nil
}

func NormalizeInput(input PrivacyRuleInput) (PrivacyRuleInput, error) {
	return normalizeInput(input, false)
}

func NormalizeInputForUpdate(input PrivacyRuleInput) (PrivacyRuleInput, error) {
	return normalizeInput(input, true)
}

func normalizeInput(input PrivacyRuleInput, allowEmptyPattern bool) (PrivacyRuleInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.TrimSpace(strings.ToLower(input.Type))
	input.Category = strings.TrimSpace(strings.ToLower(input.Category))
	input.ReplacementMode = strings.TrimSpace(strings.ToLower(input.ReplacementMode))

	if input.Name == "" {
		return PrivacyRuleInput{}, fmt.Errorf("规则名称不能为空")
	}
	if !isValidRuleType(input.Type) {
		return PrivacyRuleInput{}, fmt.Errorf("规则类型无效")
	}
	if input.Category == "" {
		input.Category = CategoryCustom
	}
	if input.ReplacementMode == "" {
		input.ReplacementMode = ReplacementModePseudonym
	}
	if !isValidReplacementMode(input.ReplacementMode) {
		return PrivacyRuleInput{}, fmt.Errorf("替换方式无效")
	}
	if !isValidCategory(input.Category) {
		return PrivacyRuleInput{}, fmt.Errorf("规则类别无效")
	}
	if input.Priority <= 0 {
		input.Priority = 100
	}
	if !allowEmptyPattern && strings.TrimSpace(input.PatternOrValue) == "" {
		return PrivacyRuleInput{}, fmt.Errorf("匹配内容不能为空")
	}
	if strings.TrimSpace(input.PatternOrValue) != "" && input.Type == RuleTypeRegex && input.Enabled {
		if _, err := regexp.Compile(input.PatternOrValue); err != nil {
			return PrivacyRuleInput{}, fmt.Errorf("正则表达式无效: %w", err)
		}
	}
	if strings.TrimSpace(input.PatternOrValue) != "" && input.Type == RuleTypeEntropy {
		minLength, err := strconv.Atoi(strings.TrimSpace(input.PatternOrValue))
		if err != nil || minLength < 8 {
			return PrivacyRuleInput{}, fmt.Errorf("高熵识别最小长度必须是不小于 8 的数字")
		}
	}
	if strings.TrimSpace(input.PatternOrValue) != "" && input.Type == RuleTypeValidator {
		for _, validator := range parseRuleList(input.PatternOrValue) {
			if !isValidValidator(validator) {
				return PrivacyRuleInput{}, fmt.Errorf("校验算法无效: %s", validator)
			}
		}
	}
	return input, nil
}

func SummaryToJSON(summary RedactionSummary) string {
	if !summary.Changed || len(summary.Hits) == 0 {
		return ""
	}
	data, err := json.Marshal(struct {
		Changed bool           `json:"changed"`
		Hits    []RedactionHit `json:"hits"`
	}{
		Changed: summary.Changed,
		Hits:    summary.Hits,
	})
	if err != nil {
		return ""
	}
	return string(data)
}

func DebugMatchesToJSON(summary RedactionSummary) string {
	if !summary.Changed || len(summary.Matches) == 0 {
		return ""
	}
	data, err := json.Marshal(summary.Matches)
	if err != nil {
		return ""
	}
	return string(data)
}

type serviceSyntheticReplacementTracker struct {
	ctx    context.Context
	store  *Store
	logger *slog.Logger
}

func (t *serviceSyntheticReplacementTracker) LookupSyntheticMapping(raw, kind string) (string, bool) {
	if t == nil || t.store == nil || raw == "" {
		return "", false
	}
	replacement, found, err := t.store.LookupSyntheticMapping(t.ctx, raw, kind)
	if err != nil {
		if t.logger != nil {
			t.logger.Debug("[隐私防护] 查询模拟值映射失败", "kind", kind, "error", err)
		}
		return "", false
	}
	if found && t.logger != nil {
		t.logger.Debug("[隐私防护] 命中模拟值映射", "kind", kind, "replacement", replacement)
	}
	return replacement, found
}

func (t *serviceSyntheticReplacementTracker) RegisterSyntheticMapping(raw, kind, replacement string) {
	if t == nil || t.store == nil || raw == "" || replacement == "" {
		return
	}
	if err := t.store.RegisterSyntheticMapping(t.ctx, raw, kind, replacement); err != nil {
		if t.logger != nil {
			t.logger.Debug("[隐私防护] 模拟值映射登记失败", "kind", kind, "replacement", replacement, "error", err)
		}
		return
	}
	if t.logger != nil {
		t.logger.Debug("[隐私防护] 已登记模拟值映射", "kind", kind, "replacement", replacement)
	}
}

func (t *serviceSyntheticReplacementTracker) RegisterSyntheticValue(value, kind string) {
	if t == nil || t.store == nil || value == "" {
		return
	}
	if err := t.store.RegisterSyntheticValue(t.ctx, value, kind); err != nil {
		if t.logger != nil {
			t.logger.Debug("[隐私防护] 模拟值登记失败", "kind", kind, "value", value, "error", err)
		}
		return
	}
	if t.logger != nil {
		t.logger.Debug("[隐私防护] 已登记模拟值", "kind", kind, "value", value)
	}
}

func (t *serviceSyntheticReplacementTracker) SyntheticReplacementInUse(kind, replacement string) bool {
	if t == nil || t.store == nil || replacement == "" {
		return false
	}
	found, err := t.store.SyntheticReplacementInUse(t.ctx, kind, replacement)
	if err != nil {
		if t.logger != nil {
			t.logger.Debug("[隐私防护] 查询模拟值占用失败", "kind", kind, "replacement", replacement, "error", err)
		}
		return false
	}
	return found
}

func (t *serviceSyntheticReplacementTracker) TouchSyntheticValue(value string) bool {
	if t == nil || t.store == nil || value == "" {
		return false
	}
	found, err := t.store.TouchSyntheticValue(t.ctx, value)
	if err != nil {
		if t.logger != nil {
			t.logger.Debug("[隐私防护] 查询模拟值失败", "value", value, "error", err)
		}
		return false
	}
	if found {
		t.LogSyntheticValueSkip(value)
	}
	return found
}

func (t *serviceSyntheticReplacementTracker) LogSyntheticValueSkip(value string) {
	if t == nil || t.logger == nil || value == "" {
		return
	}
	t.logger.Debug("[隐私防护] 跳过已登记模拟值", "value", value)
}

func isValidRuleType(value string) bool {
	switch value {
	case RuleTypeExact, RuleTypeRegex, RuleTypeField, RuleTypeKeyword, RuleTypeValidator, RuleTypeEntropy, RuleTypeStructured:
		return true
	default:
		return false
	}
}

func isValidReplacementMode(value string) bool {
	switch value {
	case ReplacementModePlaceholder, ReplacementModePseudonym:
		return true
	default:
		return false
	}
}

func isValidValidator(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bank_card_luhn", "cn_id_card", "jwt":
		return true
	default:
		return false
	}
}

func isValidCategory(value string) bool {
	switch value {
	case CategorySecret, CategoryToken, CategoryName, CategoryPhone, CategoryBankCard, CategoryIDCard, CategoryCustom:
		return true
	default:
		return false
	}
}
