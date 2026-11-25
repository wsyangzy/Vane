package routing

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type ResponseRecoveryAction string

const ResponseRecoveryRetrySameTarget ResponseRecoveryAction = "retry_same_target"

// ResponseRecoveryConfig contains metadata-only rules. Provider adapters own
// their defaults, response decoding and execution; configuration/UI adapters
// can reuse this contract without adding provider semantics to routing.
type ResponseRecoveryConfig struct {
	Enabled bool                   `json:"enabled"`
	Rules   []ResponseRecoveryRule `json:"rules"`
}

// ResponseRecoveryRule combines populated filters with AND, values with OR;
// empty code/type/method/path filters match any value. MaxRetries counts extra
// sends and remains subject to the caller's shared request budget.
type ResponseRecoveryRule struct {
	ID          string                 `json:"id"`
	Enabled     bool                   `json:"enabled"`
	Action      ResponseRecoveryAction `json:"action"`
	StatusCodes []int                  `json:"status_codes"`
	ErrorCodes  []string               `json:"error_codes,omitempty"`
	ErrorTypes  []string               `json:"error_types,omitempty"`
	Methods     []string               `json:"methods,omitempty"`
	Paths       []string               `json:"paths,omitempty"`
	MaxRetries  int                    `json:"max_retries"`
	DelayMS     int64                  `json:"delay_ms"`
}

type ResponseRecoveryMatch struct {
	StatusCode int
	ErrorCode  string
	ErrorType  string
	Method     string
	Path       string
}

func (c ResponseRecoveryConfig) Clone() ResponseRecoveryConfig {
	c.Rules = slices.Clone(c.Rules)
	for i := range c.Rules {
		c.Rules[i].StatusCodes = slices.Clone(c.Rules[i].StatusCodes)
		c.Rules[i].ErrorCodes = slices.Clone(c.Rules[i].ErrorCodes)
		c.Rules[i].ErrorTypes = slices.Clone(c.Rules[i].ErrorTypes)
		c.Rules[i].Methods = slices.Clone(c.Rules[i].Methods)
		c.Rules[i].Paths = slices.Clone(c.Rules[i].Paths)
	}
	return c
}

func (c ResponseRecoveryConfig) Validate() error {
	ids := make(map[string]bool, len(c.Rules))
	for _, rule := range c.Rules {
		if rule.ID == "" || len(rule.ID) > 64 || safeDispatchReason(rule.ID, "") != rule.ID {
			return fmt.Errorf("invalid response recovery rule id %q", rule.ID)
		}
		if ids[rule.ID] {
			return fmt.Errorf("duplicate response recovery rule id %q", rule.ID)
		}
		ids[rule.ID] = true
		if rule.Action != ResponseRecoveryRetrySameTarget {
			return fmt.Errorf("response recovery rule %q has unsupported action %q", rule.ID, rule.Action)
		}
		if rule.MaxRetries < 1 || rule.MaxRetries > 10 {
			return fmt.Errorf("response recovery rule %q max_retries must be between 1 and 10", rule.ID)
		}
		if rule.DelayMS < 0 || rule.DelayMS > 60_000 {
			return fmt.Errorf("response recovery rule %q delay_ms must be between 0 and 60000", rule.ID)
		}
		if len(rule.StatusCodes) == 0 {
			return fmt.Errorf("response recovery rule %q requires explicit HTTP error statuses", rule.ID)
		}
		for _, status := range rule.StatusCodes {
			if status < 400 || status > 599 {
				return fmt.Errorf("response recovery rule %q has invalid HTTP error status %d", rule.ID, status)
			}
		}
		for _, values := range [][]string{rule.ErrorCodes, rule.ErrorTypes, rule.Methods, rule.Paths} {
			for _, value := range values {
				if value == "" || value != strings.TrimSpace(value) {
					return fmt.Errorf("response recovery rule %q has an empty or padded match value", rule.ID)
				}
			}
		}
		for _, path := range rule.Paths {
			if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
				return fmt.Errorf("response recovery rule %q requires paths without query or fragment", rule.ID)
			}
		}
	}
	return nil
}

func (r ResponseRecoveryRule) matchesMetadata(m ResponseRecoveryMatch) bool {
	return r.Enabled && slices.Contains(r.StatusCodes, m.StatusCode) &&
		(len(r.Methods) == 0 || slices.Contains(r.Methods, m.Method)) &&
		(len(r.Paths) == 0 || slices.Contains(r.Paths, m.Path))
}

func (r ResponseRecoveryRule) Matches(m ResponseRecoveryMatch) bool {
	return r.matchesMetadata(m) &&
		(len(r.ErrorCodes) == 0 || slices.Contains(r.ErrorCodes, m.ErrorCode)) &&
		(len(r.ErrorTypes) == 0 || slices.Contains(r.ErrorTypes, m.ErrorType))
}

// MayMatch lets adapters reject unrelated responses before reading their body.
func (c ResponseRecoveryConfig) MayMatch(m ResponseRecoveryMatch) bool {
	if c.Enabled {
		for _, rule := range c.Rules {
			if rule.matchesMetadata(m) {
				return true
			}
		}
	}
	return false
}

// Match uses configured order. One request retains its first selected rule;
// adapters must not chain different rules into additional recovery budgets.
func (c ResponseRecoveryConfig) Match(m ResponseRecoveryMatch) (ResponseRecoveryRule, bool) {
	if c.Enabled {
		for _, rule := range c.Rules {
			if rule.Matches(m) {
				return rule, true
			}
		}
	}
	return ResponseRecoveryRule{}, false
}

func (r ResponseRecoveryRule) Delay() time.Duration {
	return time.Duration(r.DelayMS) * time.Millisecond
}
