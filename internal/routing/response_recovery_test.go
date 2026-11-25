package routing

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func responseRecoveryTestConfig() ResponseRecoveryConfig {
	return ResponseRecoveryConfig{Enabled: true, Rules: []ResponseRecoveryRule{{
		ID: "transient_response", Enabled: true, Action: ResponseRecoveryRetrySameTarget,
		StatusCodes: []int{400, 503}, ErrorCodes: []string{"state_rejected", "temporary_error"},
		ErrorTypes: []string{"invalid_request_error", "server_error"}, Methods: []string{"POST", "PUT"},
		Paths: []string{"/responses", "/responses/compact"}, MaxRetries: 2, DelayMS: 25,
	}}}
}

func TestResponseRecoveryRulesMatchMetadataAndExactErrorIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ResponseRecoveryConfig, *ResponseRecoveryMatch)
		want   bool
	}{
		{"exact", func(*ResponseRecoveryConfig, *ResponseRecoveryMatch) {}, true},
		{"alternate-values", func(_ *ResponseRecoveryConfig, m *ResponseRecoveryMatch) {
			m.StatusCode, m.ErrorCode, m.ErrorType, m.Method, m.Path = 503, "temporary_error", "server_error", "PUT", "/responses/compact"
		}, true},
		{"global-disabled", func(c *ResponseRecoveryConfig, _ *ResponseRecoveryMatch) { c.Enabled = false }, false},
		{"rule-disabled", func(c *ResponseRecoveryConfig, _ *ResponseRecoveryMatch) { c.Rules[0].Enabled = false }, false},
		{"wrong-status", func(_ *ResponseRecoveryConfig, m *ResponseRecoveryMatch) { m.StatusCode = 429 }, false},
		{"wrong-code", func(_ *ResponseRecoveryConfig, m *ResponseRecoveryMatch) { m.ErrorCode = "other_error" }, false},
		{"code-case-is-significant", func(_ *ResponseRecoveryConfig, m *ResponseRecoveryMatch) { m.ErrorCode = "STATE_REJECTED" }, false},
		{"wrong-type", func(_ *ResponseRecoveryConfig, m *ResponseRecoveryMatch) { m.ErrorType = "other_type" }, false},
		{"wrong-method", func(_ *ResponseRecoveryConfig, m *ResponseRecoveryMatch) { m.Method = "GET" }, false},
		{"wrong-path", func(_ *ResponseRecoveryConfig, m *ResponseRecoveryMatch) { m.Path = "/other" }, false},
		{"status-only", func(c *ResponseRecoveryConfig, m *ResponseRecoveryMatch) {
			c.Rules[0].ErrorCodes, c.Rules[0].ErrorTypes = nil, nil
			m.ErrorCode, m.ErrorType = "", ""
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := responseRecoveryTestConfig()
			match := ResponseRecoveryMatch{StatusCode: 400, ErrorCode: "state_rejected", ErrorType: "invalid_request_error", Method: "POST", Path: "/responses"}
			tc.change(&cfg, &match)
			rule, matched := cfg.Match(match)
			if matched != tc.want || matched && rule.ID != "transient_response" {
				t.Fatalf("match=%t rule=%+v want=%t", matched, rule, tc.want)
			}
		})
	}
	metadata := ResponseRecoveryMatch{StatusCode: 400, Method: "POST", Path: "/responses"}
	if !responseRecoveryTestConfig().MayMatch(metadata) {
		t.Fatal("body-dependent matching discarded eligible metadata")
	}
}

func TestResponseRecoveryRulesRetainConfiguredOrder(t *testing.T) {
	cfg := responseRecoveryTestConfig()
	second := cfg.Clone().Rules[0]
	second.ID = "second_policy"
	cfg.Rules = append(cfg.Rules, second)
	match := ResponseRecoveryMatch{StatusCode: 400, ErrorCode: "state_rejected", ErrorType: "server_error", Method: "POST", Path: "/responses"}
	if rule, matched := cfg.Match(match); !matched || rule.ID != "transient_response" {
		t.Fatalf("first configured match lost priority: %+v matched=%t", rule, matched)
	}
	cfg.Rules[0].Enabled = false
	if rule, matched := cfg.Match(match); !matched || rule.ID != "second_policy" {
		t.Fatalf("disabled rule hid the next enabled match: %+v matched=%t", rule, matched)
	}
}

func TestResponseRecoveryConfigRejectsInvalidPolicyControls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ResponseRecoveryConfig)
	}{
		{"missing-id", func(c *ResponseRecoveryConfig) { c.Rules[0].ID = "" }},
		{"unsafe-id", func(c *ResponseRecoveryConfig) { c.Rules[0].ID = "PRIVATE UPSTREAM TEXT" }},
		{"duplicate-id", func(c *ResponseRecoveryConfig) { c.Rules = append(c.Rules, c.Rules[0]) }},
		{"unknown-action", func(c *ResponseRecoveryConfig) { c.Rules[0].Action = "rewrite_request" }},
		{"no-retry-budget", func(c *ResponseRecoveryConfig) { c.Rules[0].MaxRetries = 0 }},
		{"unbounded-retries", func(c *ResponseRecoveryConfig) { c.Rules[0].MaxRetries = 11 }},
		{"negative-delay", func(c *ResponseRecoveryConfig) { c.Rules[0].DelayMS = -1 }},
		{"excessive-delay", func(c *ResponseRecoveryConfig) { c.Rules[0].DelayMS = 60_001 }},
		{"missing-status", func(c *ResponseRecoveryConfig) { c.Rules[0].StatusCodes = nil }},
		{"success-status", func(c *ResponseRecoveryConfig) { c.Rules[0].StatusCodes = []int{200} }},
		{"empty-error-filter", func(c *ResponseRecoveryConfig) { c.Rules[0].ErrorCodes = []string{""} }},
		{"padded-method", func(c *ResponseRecoveryConfig) { c.Rules[0].Methods = []string{" POST"} }},
		{"relative-path", func(c *ResponseRecoveryConfig) { c.Rules[0].Paths = []string{"responses"} }},
		{"query-in-path", func(c *ResponseRecoveryConfig) { c.Rules[0].Paths = []string{"/responses?secret=1"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := responseRecoveryTestConfig()
			tc.change(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid response recovery configuration accepted")
			}
		})
	}
	if err := responseRecoveryTestConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestResponseRecoveryConfigCloneAndSerializationKeepControlValues(t *testing.T) {
	cfg := responseRecoveryTestConfig()
	snapshot := cfg.Clone()
	cfg.Rules[0].StatusCodes[0], cfg.Rules[0].ErrorCodes[0], cfg.Rules[0].ErrorTypes[0] = 429, "changed", "changed"
	cfg.Rules[0].Methods[0], cfg.Rules[0].Paths[0] = "GET", "/changed"
	if !reflect.DeepEqual(snapshot, responseRecoveryTestConfig()) {
		t.Fatalf("request snapshot shared mutable match filters: %+v", snapshot)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ResponseRecoveryConfig
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(snapshot, decoded) || decoded.Rules[0].Delay() != 25*time.Millisecond {
		t.Fatalf("configuration adapter lost control values: decoded=%+v err=%v", decoded, err)
	}
}
