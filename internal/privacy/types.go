package privacy

import "time"

const (
	RuleTypeExact      = "exact"
	RuleTypeRegex      = "regex"
	RuleTypeField      = "field"
	RuleTypeKeyword    = "keyword"
	RuleTypeValidator  = "validator"
	RuleTypeEntropy    = "entropy"
	RuleTypeStructured = "structured"

	ReplacementModePlaceholder = "placeholder"
	ReplacementModePseudonym   = "pseudonym"

	CategorySecret   = "secret"
	CategoryToken    = "token"
	CategoryName     = "name"
	CategoryPhone    = "phone"
	CategoryBankCard = "bank_card"
	CategoryIDCard   = "id_card"
	CategoryCustom   = "custom"
)

type PrivacyRuleInput struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Category        string `json:"category"`
	PatternOrValue  string `json:"pattern_or_value"`
	ReplacementMode string `json:"replacement_mode"`
	Enabled         bool   `json:"enabled"`
	Priority        int    `json:"priority"`
}

type PrivacyRule struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Category        string    `json:"category"`
	PatternOrValue  string    `json:"pattern_or_value,omitempty"`
	PatternMasked   string    `json:"pattern_masked,omitempty"`
	ValueLength     int       `json:"value_length,omitempty"`
	ReplacementMode string    `json:"replacement_mode"`
	Enabled         bool      `json:"enabled"`
	Priority        int       `json:"priority"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type RedactionHit struct {
	RuleID   int64  `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Type     string `json:"type"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type RedactionMatch struct {
	RuleID      int64  `json:"rule_id"`
	RuleName    string `json:"rule_name"`
	Type        string `json:"type"`
	Category    string `json:"category"`
	Raw         string `json:"raw"`
	RawBase64   string `json:"raw_base64"`
	Replacement string `json:"replacement"`
}

type RedactionSummary struct {
	Changed bool             `json:"changed"`
	Hits    []RedactionHit   `json:"hits"`
	Matches []RedactionMatch `json:"matches,omitempty"`
}

type PrivacyConfig struct {
	Enabled bool          `json:"enabled"`
	Rules   []PrivacyRule `json:"rules"`
}

type SyntheticValueRecord struct {
	RawHash     string    `json:"raw_hash"`
	RawValue    string    `json:"raw_value"`
	Replacement string    `json:"replacement"`
	Value       string    `json:"value"`
	Kind        string    `json:"kind"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	LastSkipAt  time.Time `json:"last_skip_at"`
	ReuseCount  int       `json:"reuse_count"`
	SkipCount   int       `json:"skip_count"`
	HitCount    int       `json:"hit_count"`
}

type PreviewResult struct {
	Output  string         `json:"output"`
	Changed bool           `json:"changed"`
	Hits    []RedactionHit `json:"hits"`
}
