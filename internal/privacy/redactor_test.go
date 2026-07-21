package privacy

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestService(t *testing.T, enabled bool) *Service {
	t.Helper()
	clearSyntheticReplacementRegistry()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE privacy_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			category TEXT NOT NULL,
			pattern_or_value TEXT NOT NULL,
			replacement_mode TEXT NOT NULL DEFAULT 'placeholder',
			enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 100,
			created_at DATETIME DEFAULT '2026-01-01 00:00:00+08:00',
			updated_at DATETIME DEFAULT '2026-01-01 00:00:00+08:00'
		);
	`)
	if err != nil {
		t.Fatalf("create privacy_rules: %v", err)
	}
	store := NewStore(db)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return NewService(store, func(context.Context) bool { return enabled })
}

func mustCreateRule(t *testing.T, svc *Service, input PrivacyRuleInput) PrivacyRule {
	t.Helper()
	rule, err := svc.CreateRule(context.Background(), input)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	return rule
}

func testJWT() string {
	return "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.signature"
}

func testIDCard(base string) string {
	return base + cnIDCardCheckDigit(base)
}

func testBankCard(base string) string {
	return base + luhnCheckDigit(base)
}

func TestExactMatchRedactsValueNotJSONKeys(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "api token",
		Type:           RuleTypeExact,
		PatternOrValue: "sk-live-123",
		Enabled:        true,
	})

	body := []byte(`{"messages":[{"role":"user","content":"sk-live-123"}],"api_key":"sk-live-123","name":"token"}`)
	redacted, summary, changed, err := svc.Redact(body)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed || !summary.Changed {
		t.Fatalf("expected changed summary")
	}
	output := string(redacted)
	if strings.Count(output, "sk-live-123") != 1 {
		t.Fatalf("expected only non-context api_key value to remain: %s", output)
	}
	if !strings.Contains(output, `"token"`) || !strings.Contains(output, `"api_key"`) {
		t.Fatalf("json keys should not be modified: %s", output)
	}
	if !strings.Contains(output, `"api_key":"sk-live-123"`) {
		t.Fatalf("non-context protocol field should remain unchanged: %s", output)
	}
	if summary.Hits[0].Count != 1 {
		t.Fatalf("expected 1 hit, got %+v", summary.Hits)
	}
}

func TestRegexRulesRedactCommonSensitiveSamples(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "phone", Type: RuleTypeRegex, Category: CategoryPhone, PatternOrValue: `\b1[3-9]\d{9}\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "id", Type: RuleTypeRegex, Category: CategoryIDCard, PatternOrValue: `\b\d{17}[\dXx]\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "card", Type: RuleTypeRegex, Category: CategoryBankCard, PatternOrValue: `\b\d{16,19}\b`, Enabled: true})

	phone := "13900001111"
	idCard := testIDCard("11010519491231002")
	bankCard := testBankCard("411111111111111")
	input := []byte("phone=" + phone + " id=" + idCard + " card=" + bankCard)
	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected text redaction")
	}
	output := string(redacted)
	for _, leaked := range []string{phone, idCard, bankCard} {
		if strings.Contains(output, leaked) {
			t.Fatalf("leaked %s in %s", leaked, output)
		}
	}
	if len(summary.Hits) != 3 {
		t.Fatalf("expected 3 rule hits, got %+v", summary.Hits)
	}
}

func TestBankCardRegexRedactsValidBankCards(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "银行卡号",
		Type:           RuleTypeRegex,
		Category:       CategoryBankCard,
		PatternOrValue: `\b(?:[3-9]\d{12,18}|[3-9]\d{3}(?:[- ]\d{4}){2,3}(?:[- ]\d{1,3})?)\b`,
		Enabled:        true,
	})

	bankCardA := testBankCard("411111111111111")
	bankCardB := testBankCard("555555555555444")
	input := strings.Join([]string{
		"card=" + bankCardA,
		"same=" + bankCardB,
	}, " ")

	redacted, summary, changed, err := svc.Redact([]byte(input))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected valid bank card redaction")
	}
	output := string(redacted)
	for _, leaked := range []string{bankCardA, bankCardB} {
		if strings.Contains(output, leaked) {
			t.Fatalf("valid bank card leaked: %s", output)
		}
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 2 {
		t.Fatalf("expected 2 valid bank-card hits, got %+v", summary.Hits)
	}
	if len(summary.Matches) != 2 {
		t.Fatalf("expected 2 bank-card match details, got %+v", summary.Matches)
	}
}

func TestBankCardRegexDoesNotRedactDateZeroOrIDLikeNumbers(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "银行卡号",
		Type:           RuleTypeRegex,
		Category:       CategoryBankCard,
		PatternOrValue: `\b(?:[3-9]\d{12,18}|[3-9]\d{3}(?:[- ]\d{4}){2,3}(?:[- ]\d{1,3})?)\b`,
		Enabled:        true,
	})

	input := strings.Join([]string{
		"0000-000000000000",
		"20150711004518",
		"20180306012644",
		"20230129092748",
		"20230307220236",
		"20240102092130",
		"420822199901011040",
		"420822199901011016",
	}, " ")

	redacted, summary, changed, err := svc.Redact([]byte(input))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if changed || summary.Changed {
		t.Fatalf("expected no bank-card false positive redaction, got %s %+v", redacted, summary)
	}
	if string(redacted) != input {
		t.Fatalf("false positives should remain unchanged: %s", redacted)
	}
}

func TestTypedRegexRulesUseValidatorsForIDCardAndJWT(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "身份证号",
		Type:           RuleTypeRegex,
		Category:       CategoryIDCard,
		PatternOrValue: `\b[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]\b`,
		Enabled:        true,
	})
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "JWT",
		Type:           RuleTypeRegex,
		Category:       CategoryToken,
		PatternOrValue: `\b[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`,
		Enabled:        true,
	})

	validIDBase := "11010519491231002"
	validID := validIDBase + cnIDCardCheckDigit(validIDBase)
	invalidID := validIDBase + "0"
	if invalidID == validID {
		invalidID = validIDBase + "1"
	}

	input := "bad_id=" + invalidID + " good_id=" + validID + " bad_jwt=not.a.jwt good_jwt=" + testJWT()
	redacted, summary, changed, err := svc.Redact([]byte(input))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed || !summary.Changed {
		t.Fatalf("expected valid ID card and JWT redaction")
	}
	output := string(redacted)
	for _, kept := range []string{invalidID, "not.a.jwt"} {
		if !strings.Contains(output, kept) {
			t.Fatalf("invalid typed regex candidate %s should remain unchanged in %s", kept, output)
		}
	}
	for _, leaked := range []string{validID, testJWT()} {
		if strings.Contains(output, leaked) {
			t.Fatalf("valid typed regex candidate leaked in %s", output)
		}
	}
	if len(summary.Hits) != 2 {
		t.Fatalf("expected two typed regex hits, got %+v", summary.Hits)
	}
}

func TestJSONRedactionOnlyTouchesStringValues(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "name", Type: RuleTypeExact, Category: CategoryName, PatternOrValue: "张三", Enabled: true})

	input := []byte(`{"messages":[{"role":"user","content":"hello 张三"},{"role":"assistant","content":[{"type":"text","text":"张三"}]}],"profile_name":"张三","nested":{"items":[123,true,null],"张三":"key stays"}}`)
	redacted, _, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected json change")
	}
	output := string(redacted)
	if strings.Contains(output, `"hello 张三"`) || strings.Contains(output, `"text":"张三"`) {
		t.Fatalf("string value leaked: %s", output)
	}
	if !strings.Contains(output, `"profile_name":"张三"`) {
		t.Fatalf("non-context json field should remain unchanged: %s", output)
	}
	if !strings.Contains(output, `"张三":"key stays"`) {
		t.Fatalf("json key should remain unchanged: %s", output)
	}
	if !strings.Contains(output, `123`) || !strings.Contains(output, `true`) || !strings.Contains(output, `null`) {
		t.Fatalf("non-string json values should remain present: %s", output)
	}
}

func TestNonJSONTextRedaction(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret-value", Enabled: true})

	redacted, summary, changed, err := svc.Redact([]byte("prefix secret-value suffix"))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed || string(redacted) != "prefix fake_secret_0001 suffix" {
		t.Fatalf("unexpected redaction: %s %+v", redacted, summary)
	}
}

func TestOverlappingExactRulesRespectPriority(t *testing.T) {
	svc := newTestService(t, true)
	shortRule := mustCreateRule(t, svc, PrivacyRuleInput{Name: "short", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "abc", Enabled: true, Priority: 1})
	longRule := mustCreateRule(t, svc, PrivacyRuleInput{Name: "long", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "abcdef", Enabled: true, Priority: 999})

	redacted, summary, changed, err := svc.Redact([]byte("abcdef abc"))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected redaction")
	}
	if string(redacted) != "fake_token_0001def fake_token_0001" {
		t.Fatalf("unexpected order result: %s", redacted)
	}
	shortHit := false
	for _, hit := range summary.Hits {
		if hit.RuleID == shortRule.ID && hit.Count == 2 {
			shortHit = true
		}
		if hit.RuleID == longRule.ID {
			t.Fatalf("lower-priority long rule should not consume overlap: %+v", summary.Hits)
		}
	}
	if !shortHit {
		t.Fatalf("expected higher-priority short rule to run first, got %+v", summary.Hits)
	}
}

func TestInvalidRegexValidationAndDisabledGlobal(t *testing.T) {
	svc := newTestService(t, true)
	if _, err := svc.CreateRule(context.Background(), PrivacyRuleInput{Name: "bad", Type: RuleTypeRegex, Category: CategoryCustom, PatternOrValue: "(", Enabled: true}); err == nil {
		t.Fatalf("expected invalid enabled regex to fail")
	}
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "disabled bad", Type: RuleTypeRegex, Category: CategoryCustom, PatternOrValue: "(", Enabled: false})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "disabled exact", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret", Enabled: false})

	redacted, _, changed, err := svc.Redact([]byte("secret"))
	if err != nil {
		t.Fatalf("redact with disabled invalid regex: %v", err)
	}
	if changed || string(redacted) != "secret" {
		t.Fatalf("disabled rules should not redact: %s", redacted)
	}

	disabledSvc := newTestService(t, false)
	mustCreateRule(t, disabledSvc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret", Enabled: true})
	redacted, _, changed, err = disabledSvc.Redact([]byte("secret"))
	if err != nil {
		t.Fatalf("redact with deprecated global disabled: %v", err)
	}
	if !changed || string(redacted) != "fake_secret_0001" {
		t.Fatalf("deprecated global disabled should not suppress enabled rules: %s", redacted)
	}
}

func TestReplacementModePlaceholderDoesNotCreateCrossRequestMapping(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "token a", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "token-a", ReplacementMode: ReplacementModePlaceholder, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "token b", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "token-b", ReplacementMode: ReplacementModePlaceholder, Enabled: true})

	redacted, summary, changed, err := svc.Redact([]byte("token-a token-b token-a"))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected placeholder redaction")
	}
	if string(redacted) != "REDACTED_TOKEN REDACTED_TOKEN REDACTED_TOKEN" {
		t.Fatalf("unexpected placeholder output: %s", redacted)
	}
	if len(summary.Hits) != 2 || summary.Hits[0].Count+summary.Hits[1].Count != 3 {
		t.Fatalf("expected 3 placeholder hits, got %+v", summary.Hits)
	}
	records, err := svc.ListSyntheticValues(context.Background())
	if err != nil {
		t.Fatalf("list synthetic values: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("placeholder mode should not persist mappings: %+v", records)
	}
}

func TestBodyAuthenticationStyleTextIsRedacted(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "authorization token", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "sk-body-token", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "api key body", Type: RuleTypeExact, Category: CategoryCustom, PatternOrValue: "sk-body-api-key", Enabled: true})

	input := []byte("Authorization: Bearer sk-body-token\napi_key=sk-body-api-key")
	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected body authentication-style text redaction")
	}
	output := string(redacted)
	for _, leaked := range []string{"sk-body-token", "sk-body-api-key"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("body authentication-style value leaked: %s", output)
		}
	}
	for _, expected := range []string{"Authorization: Bearer fake_token_0001", "api_key=fake_api_key_0001"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in %s", expected, output)
		}
	}
	if len(summary.Hits) != 2 || summary.Hits[0].Count+summary.Hits[1].Count != 2 {
		t.Fatalf("expected 2 body auth-style hits, got %+v", summary.Hits)
	}
}

func TestRedactionSummaryKeepsDebugMatchesOutOfAuditJSON(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "privacy body token", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "sk-body-token", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "phone", Type: RuleTypeRegex, Category: CategoryPhone, PatternOrValue: `\b1[3-9]\d{9}\b`, Enabled: true})

	body := `{"input":[{"content":[{"text":"token sk-body-token phone 13900001111"}]}]}`
	redacted, summary, changed, err := svc.Redact([]byte(body))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed || !summary.Changed {
		t.Fatalf("expected changed summary")
	}
	output := string(redacted)
	for _, leaked := range []string{"sk-body-token", "13900001111"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("redacted body leaked %s in %s", leaked, output)
		}
	}
	if len(summary.Matches) != 2 {
		t.Fatalf("expected 2 debug matches, got %+v", summary.Matches)
	}

	debugJSON := DebugMatchesToJSON(summary)
	for _, expected := range []string{"privacy body token", "sk-body-token", "fake_token_0001", "13900001111", "13411110001"} {
		if !strings.Contains(debugJSON, expected) {
			t.Fatalf("expected debug matches JSON to contain %q, got %s", expected, debugJSON)
		}
	}

	if !strings.Contains(debugJSON, "raw_base64") {
		t.Fatalf("expected debug matches JSON to contain raw_base64, got %s", debugJSON)
	}

	auditJSON := SummaryToJSON(summary)
	for _, forbidden := range []string{"sk-body-token", "13900001111", "matches", "replacement"} {
		if strings.Contains(auditJSON, forbidden) {
			t.Fatalf("audit summary should not contain %q: %s", forbidden, auditJSON)
		}
	}
	if !strings.Contains(auditJSON, "body token") || !strings.Contains(auditJSON, "phone") {
		t.Fatalf("audit summary should keep rule names and counts: %s", auditJSON)
	}
	if strings.Contains(auditJSON, "raw_base64") {
		t.Fatalf("audit summary should not contain raw_base64: %s", auditJSON)
	}
}

func TestFieldRuleRedactsJSONFieldValues(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "fields",
		Type:           RuleTypeField,
		Category:       CategoryToken,
		PatternOrValue: "password,access_token",
		Enabled:        true,
	})

	input := []byte(`{"input":[{"password":"p@ssw0rd","auth":{"access_token":"tok_123","safe":"keep"}}],"access_token":"root stays","count":123}`)
	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected field redaction")
	}
	output := string(redacted)
	for _, leaked := range []string{"p@ssw0rd", "tok_123"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("field value leaked: %s", output)
		}
	}
	if !strings.Contains(output, `"access_token":"root stays"`) || !strings.Contains(output, `"count":123`) {
		t.Fatalf("unexpected json field mutation: %s", output)
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 2 {
		t.Fatalf("expected 2 field hits, got %+v", summary.Hits)
	}
}

func TestJSONRedactionPreservesProtocolFieldsAndRedactsToolDescriptions(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "validator",
		Type:           RuleTypeValidator,
		Category:       CategoryCustom,
		PatternOrValue: "jwt",
		Enabled:        true,
	})

	jwt := testJWT()
	input := []byte(`{
		"client_metadata":{"trace_token":"` + jwt + `"},
		"tools":[{"name":"` + jwt + `","description":"` + jwt + `","input_schema":{"type":"object","default":{"name":"` + jwt + `"}}}],
		"input":[
			{"content":[{"type":"input_text","text":"context ` + jwt + `"}]},
			{"type":"reasoning","encrypted_content":"` + jwt + `"}
		]
	}`)

	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected context json redaction")
	}
	output := string(redacted)
	if strings.Count(output, jwt) != 3 {
		t.Fatalf("expected metadata, encrypted content, and tool name to remain: %s", output)
	}
	if !strings.Contains(output, "fake_token_0001") {
		t.Fatalf("expected context jwt replacement in %s", output)
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 3 {
		t.Fatalf("expected tool description, nested default, and context hits, got %+v", summary.Hits)
	}
	if len(summary.Matches) != 3 || summary.Matches[0].RawBase64 == "" {
		t.Fatalf("expected three debug matches with raw_base64, got %+v", summary.Matches)
	}
}

func TestJSONRedactionIncludesSystemAndInstructionsRoots(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "body secret",
		Type:           RuleTypeExact,
		Category:       CategorySecret,
		PatternOrValue: "ctx-secret-value",
		Enabled:        true,
	})

	input := []byte(`{
		"system":"system ctx-secret-value",
		"instructions":"instructions ctx-secret-value",
		"input":[{"content":[{"type":"input_text","text":"input ctx-secret-value"}]}],
		"messages":[{"role":"user","content":"message ctx-secret-value"}],
		"prompt":"prompt ctx-secret-value"
	}`)

	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected context json redaction")
	}
	output := string(redacted)
	if strings.Contains(output, `"system ctx-secret-value"`) ||
		strings.Contains(output, `"instructions ctx-secret-value"`) ||
		strings.Contains(output, `"input ctx-secret-value"`) ||
		strings.Contains(output, `"message ctx-secret-value"`) ||
		strings.Contains(output, `"prompt":"prompt ctx-secret-value"`) {
		t.Fatalf("model context roots should be redacted: %s", output)
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 5 {
		t.Fatalf("expected 5 context hits, got %+v", summary.Hits)
	}
}

func TestJSONRedactionTreatsToolInputAsFreeformWithoutMutatingProtocolFields(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "tool input secret",
		Type:           RuleTypeExact,
		Category:       CategorySecret,
		PatternOrValue: "secret-value",
		Enabled:        true,
	})

	input := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"secret-value","name":"secret-value","input":{"command":"echo secret-value","name":"secret-value","metadata":{"token":"secret-value"},"tools":{"token":"secret-value"}}}]}]}`)
	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	output := string(redacted)
	if !changed || strings.Count(output, "secret-value") != 2 {
		t.Fatalf("tool protocol fields should remain while freeform input is redacted: %s", output)
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 4 {
		t.Fatalf("expected all freeform tool input fields to be redacted, got %+v", summary.Hits)
	}
}

func TestJSONRedactionPreservesLargeNumbers(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "body secret",
		Type:           RuleTypeExact,
		Category:       CategorySecret,
		PatternOrValue: "secret-value",
		Enabled:        true,
	})

	redacted, _, changed, err := svc.Redact([]byte(`{"messages":[{"role":"user","content":"secret-value"}],"database_id":9007199254740993}`))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed || !strings.Contains(string(redacted), `"database_id":9007199254740993`) {
		t.Fatalf("large integer changed during redaction: %s", redacted)
	}
}

func TestJSONRedactionSkipsTypedBinaryPayloads(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "entropy",
		Type:           RuleTypeEntropy,
		Category:       CategorySecret,
		PatternOrValue: "16",
		Enabled:        true,
	})
	payload := "AbCdEf0123456789+/AbCdEf0123456789+/"
	input := []byte(`{"input":[{"type":"input_image","image_url":"data:image/png;base64,` + payload + `"},{"type":"input_text","text":"` + payload + `"}],"messages":[{"content":[{"type":"image","source":{"type":"base64","data":"` + payload + `"}}]}]}`)

	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	output := string(redacted)
	if !changed || strings.Count(output, payload) != 2 {
		t.Fatalf("binary payloads should remain while text is redacted: %s", output)
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 1 {
		t.Fatalf("expected one text entropy hit, got %+v", summary.Hits)
	}
}

func TestJSONRedactionScansRemoteMediaURLsButPreservesInlineData(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "signed url token",
		Type:           RuleTypeExact,
		Category:       CategoryToken,
		PatternOrValue: "signed-secret-value",
		Enabled:        true,
	})

	inlineData := "data:image/png;base64,AbCdEf0123456789+/AbCdEf0123456789+/"
	input := []byte(`{"input":[{"type":"input_image","image_url":"` + inlineData + `"},{"type":"input_image","image_url":"https://images.example.test/private.png?token=signed-secret-value"}]}`)
	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	output := string(redacted)
	if !changed || !strings.Contains(output, inlineData) || strings.Contains(output, "signed-secret-value") {
		t.Fatalf("inline data should remain and remote URL token should be redacted: %s", output)
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 1 {
		t.Fatalf("expected one remote URL hit, got %+v", summary.Hits)
	}
}

func TestNonUTF8BodyFailsClosedWhenRulesAreEnabled(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret", Enabled: true})

	if _, _, _, err := svc.Redact([]byte{0xff, 0xfe, 0x00, 0x01}); err == nil {
		t.Fatal("expected binary body to be rejected")
	}
}

func TestRequestMediaValidationOnlyAppliesWhenRulesAreEnabled(t *testing.T) {
	svc := newTestService(t, true)
	body := []byte{0x1f, 0x8b, 0x08}
	if output, _, changed, err := svc.RedactRequestContext(context.Background(), body, "application/json", "gzip"); err != nil || changed || string(output) != string(body) {
		t.Fatalf("zero-rule request should pass through: output=%v changed=%v err=%v", output, changed, err)
	}
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret", Enabled: true})
	if _, _, _, err := svc.RedactRequestContext(context.Background(), body, "application/json", "gzip"); err == nil {
		t.Fatal("expected compressed request to fail closed")
	}
	if _, _, _, err := svc.RedactRequestContext(context.Background(), []byte("binary"), "application/octet-stream", ""); err == nil {
		t.Fatal("expected unsupported media type to fail closed")
	}
}

func TestRuleCacheInvalidatesAfterCreate(t *testing.T) {
	svc := newTestService(t, true)
	if output, _, changed, err := svc.Redact([]byte("secret-value")); err != nil || changed || string(output) != "secret-value" {
		t.Fatalf("unexpected initial redaction: %q changed=%v err=%v", output, changed, err)
	}
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret-value", Enabled: true})
	if output, _, changed, err := svc.Redact([]byte("secret-value")); err != nil || !changed || string(output) == "secret-value" {
		t.Fatalf("rule cache was not refreshed: %q changed=%v err=%v", output, changed, err)
	}
}

func TestRedactionMatchDetailsAreCapped(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret", Enabled: true})

	_, summary, changed, err := svc.Redact([]byte(strings.Repeat("secret ", 200)))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed || len(summary.Hits) != 1 || summary.Hits[0].Count != 200 {
		t.Fatalf("expected full hit count, got %+v", summary.Hits)
	}
	if len(summary.Matches) != maxRedactionMatchDetails {
		t.Fatalf("match details = %d, want %d", len(summary.Matches), maxRedactionMatchDetails)
	}
}

func TestUnavailablePrivacyServiceFailsClosed(t *testing.T) {
	svc := NewService(nil, nil)
	if _, _, _, err := svc.Redact([]byte(`{"messages":[{"content":"secret"}]}`)); err == nil {
		t.Fatal("expected unavailable privacy service to reject forwarding")
	}
}

func TestKeywordStructuredValidatorAndEntropyRules(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "keyword", Type: RuleTypeKeyword, Category: CategoryName, PatternOrValue: "姓名,token", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "structured", Type: RuleTypeStructured, Category: CategoryToken, PatternOrValue: "access_token,sessionid", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "validator", Type: RuleTypeValidator, Category: CategoryIDCard, PatternOrValue: "cn_id_card,jwt,bank_card_luhn", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "entropy", Type: RuleTypeEntropy, Category: CategorySecret, PatternOrValue: "32", Enabled: true})

	input := []byte(`姓名：张三 token=abc123 url=https://x.test/cb?access_token=url-token&safe=1 Cookie: sessionid=cookie-token id=11010519491231002X jwt=eyJhbGciOiJub25lIn0.eyJzdWIiOiIxMjMifQ.sig card=4111111111111111 secret=a8F3c0E91b7D4a9F8c2E6d7A1b9C0f3E`)
	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected redaction")
	}
	output := string(redacted)
	for _, leaked := range []string{
		"张三",
		"abc123",
		"url-token",
		"cookie-token",
		"11010519491231002X",
		"eyJhbGciOiJub25lIn0.eyJzdWIiOiIxMjMifQ.sig",
		"4111111111111111",
		"a8F3c0E91b7D4a9F8c2E6d7A1b9C0f3E",
	} {
		if strings.Contains(output, leaked) {
			t.Fatalf("leaked %s in %s", leaked, output)
		}
	}
	if len(summary.Hits) != 4 {
		t.Fatalf("expected 4 rule hit groups, got %+v", summary.Hits)
	}
}

func TestValidatorRulesUseTypedSyntheticPseudonyms(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:            "validator",
		Type:            RuleTypeValidator,
		Category:        CategoryCustom,
		PatternOrValue:  "cn_id_card,jwt,bank_card_luhn",
		ReplacementMode: ReplacementModePseudonym,
		Enabled:         true,
	})

	input := []byte(`id=11010519491231002X jwt=eyJhbGciOiJub25lIn0.eyJzdWIiOiIxMjMifQ.sig card=4111111111111111`)
	redacted, summary, changed, err := svc.Redact(input)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected validator redaction")
	}
	output := string(redacted)
	for _, expected := range []string{syntheticIDCard(1), "fake_token_0001", syntheticBankCard(1)} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %s in %s", expected, output)
		}
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 3 {
		t.Fatalf("expected 3 validator hits, got %+v", summary.Hits)
	}
}

func TestPseudonymReplacementUsesSyntheticValuesAndKeepsSameValueStable(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:            "names",
		Type:            RuleTypeRegex,
		Category:        CategoryName,
		PatternOrValue:  `真实甲|真实乙`,
		ReplacementMode: ReplacementModePseudonym,
		Enabled:         true,
	})

	redacted, summary, changed, err := svc.Redact([]byte("真实甲 真实甲 真实乙"))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected pseudonym redaction")
	}
	if string(redacted) != "张三 张三 李四" {
		t.Fatalf("unexpected pseudonym mapping: %s", redacted)
	}
	if len(summary.Hits) != 1 || summary.Hits[0].Count != 3 {
		t.Fatalf("expected 3 pseudonym hits, got %+v", summary.Hits)
	}
}

func TestPseudonymReplacementUsesNumberedFakeValuesAcrossRules(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "token a", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "token-a", ReplacementMode: ReplacementModePseudonym, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "token b", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "token-b", ReplacementMode: ReplacementModePseudonym, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "api key", Type: RuleTypeExact, Category: CategoryCustom, PatternOrValue: "key-a", ReplacementMode: ReplacementModePseudonym, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "secret-a", ReplacementMode: ReplacementModePseudonym, Enabled: true})

	redacted, summary, changed, err := svc.Redact([]byte("token-a token-b token-a key-a secret-a"))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected pseudonym redaction")
	}
	if string(redacted) != "fake_token_0001 fake_token_0002 fake_token_0001 fake_api_key_0001 fake_secret_0001" {
		t.Fatalf("unexpected pseudonym mapping across rules: %s", redacted)
	}
	if len(summary.Hits) != 4 {
		t.Fatalf("expected 4 rule hit groups, got %+v", summary.Hits)
	}
}

func TestSyntheticReplacementValuesAreNotRedactedAgain(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "phone", Type: RuleTypeRegex, Category: CategoryPhone, PatternOrValue: `\b1[3-9]\d{9}\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "id", Type: RuleTypeRegex, Category: CategoryIDCard, PatternOrValue: `\b\d{17}[\dXx]\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "card", Type: RuleTypeRegex, Category: CategoryBankCard, PatternOrValue: `\b\d{16,19}\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "token", Type: RuleTypeExact, Category: CategoryToken, PatternOrValue: "real-token", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "secret", Type: RuleTypeExact, Category: CategorySecret, PatternOrValue: "real-secret", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "api key", Type: RuleTypeExact, Category: CategoryCustom, PatternOrValue: "real-api-key", Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "custom", Type: RuleTypeExact, Category: CategoryCustom, PatternOrValue: "real-custom", Enabled: true})

	firstInput := "id=11010519491231002X token=real-token secret=real-secret api=real-api-key custom=real-custom"
	firstRedacted, _, changed, err := svc.Redact([]byte(firstInput))
	if err != nil {
		t.Fatalf("first redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected first redaction")
	}
	syntheticBody := string(firstRedacted)
	for _, expected := range []string{
		"fake_token_0001",
		"fake_secret_0001",
		"fake_api_key_0001",
		"fake_value_0001",
	} {
		if !strings.Contains(syntheticBody, expected) {
			t.Fatalf("expected synthetic replacement %s in %s", expected, syntheticBody)
		}
	}
	if strings.Contains(syntheticBody, "11010519491231002X") {
		t.Fatalf("original ID card should be redacted in %s", syntheticBody)
	}
	if !strings.Contains(syntheticBody, "42082219990101") {
		t.Fatalf("expected synthetic ID card in %s", syntheticBody)
	}

	secondRedacted, summary, changed, err := svc.Redact([]byte(syntheticBody))
	if err != nil {
		t.Fatalf("second redact: %v", err)
	}
	if changed || summary.Changed {
		t.Fatalf("synthetic replacements should not be redacted again: %s %+v", secondRedacted, summary)
	}
	if string(secondRedacted) != syntheticBody {
		t.Fatalf("synthetic replacements should remain unchanged: %s", secondRedacted)
	}
}

func TestSyntheticMappingKeepsSameRawStableAcrossRequests(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "id",
		Type:           RuleTypeRegex,
		Category:       CategoryIDCard,
		PatternOrValue: `\b\d{17}[\dXx]\b`,
		Enabled:        true,
	})

	idA := testIDCard("11010519491231002")
	idB := testIDCard("11010519491231003")
	firstRedacted, _, changed, err := svc.Redact([]byte("a=" + idA + " b=" + idB))
	if err != nil {
		t.Fatalf("first redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected first redaction")
	}
	firstOutput := string(firstRedacted)
	if !strings.Contains(firstOutput, syntheticIDCard(1)) || !strings.Contains(firstOutput, syntheticIDCard(2)) {
		t.Fatalf("expected two stable synthetic ID cards in %s", firstOutput)
	}

	clearSyntheticReplacementRegistry()
	secondRedacted, summary, changed, err := svc.Redact([]byte("b=" + idB))
	if err != nil {
		t.Fatalf("second redact: %v", err)
	}
	if !changed || !summary.Changed {
		t.Fatalf("expected second redaction through persisted mapping")
	}
	secondOutput := string(secondRedacted)
	if !strings.Contains(secondOutput, syntheticIDCard(2)) {
		t.Fatalf("expected persisted mapping to reuse %s in %s", syntheticIDCard(2), secondOutput)
	}
	if strings.Contains(secondOutput, syntheticIDCard(1)) {
		t.Fatalf("mapping should not fall back to reset counter value %s in %s", syntheticIDCard(1), secondOutput)
	}

	var mappingCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM privacy_synthetic_mappings WHERE kind = ?`, syntheticKindIDCard).Scan(&mappingCount); err != nil {
		t.Fatalf("query synthetic mappings: %v", err)
	}
	if mappingCount != 2 {
		t.Fatalf("expected 2 persisted synthetic mappings, got %d", mappingCount)
	}

	records, err := svc.ListSyntheticValues(context.Background())
	if err != nil {
		t.Fatalf("list synthetic mappings: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 synthetic mapping records, got %+v", records)
	}
	var foundRawB bool
	for _, record := range records {
		if record.RawValue == idB {
			foundRawB = true
			if record.Replacement != syntheticIDCard(2) || record.Value != syntheticIDCard(2) {
				t.Fatalf("expected idB mapping to reuse %s, got %+v", syntheticIDCard(2), record)
			}
			if record.ReuseCount != 1 {
				t.Fatalf("expected idB reuse count 1, got %+v", record)
			}
		}
	}
	if !foundRawB {
		t.Fatalf("expected raw value %s in synthetic mapping records: %+v", idB, records)
	}
}

func TestObservedSyntheticValuesReserveReplacementSlots(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{
		Name:           "id",
		Type:           RuleTypeRegex,
		Category:       CategoryIDCard,
		PatternOrValue: `\b\d{17}[\dXx]\b`,
		Enabled:        true,
	})
	if err := svc.store.RegisterSyntheticValue(context.Background(), syntheticIDCard(1), syntheticKindIDCard); err != nil {
		t.Fatalf("seed synthetic value: %v", err)
	}
	clearSyntheticReplacementRegistry()

	idCard := testIDCard("11010519491231002")
	redacted, _, changed, err := svc.Redact([]byte("id=" + idCard))
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected redaction")
	}
	output := string(redacted)
	if strings.Contains(output, syntheticIDCard(1)) {
		t.Fatalf("new mapping should not reuse observed synthetic value %s in %s", syntheticIDCard(1), output)
	}
	if !strings.Contains(output, syntheticIDCard(2)) {
		t.Fatalf("expected next available synthetic value %s in %s", syntheticIDCard(2), output)
	}
}

func TestUnregisteredSyntheticLookingValuesAreStillRedacted(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "phone", Type: RuleTypeRegex, Category: CategoryPhone, PatternOrValue: `\b1[3-9]\d{9}\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "id", Type: RuleTypeRegex, Category: CategoryIDCard, PatternOrValue: `\b\d{17}[\dXx]\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "card", Type: RuleTypeRegex, Category: CategoryBankCard, PatternOrValue: `\b\d{16,19}\b`, Enabled: true})
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "token", Type: RuleTypeRegex, Category: CategoryToken, PatternOrValue: `fake_token_\d{4}`, Enabled: true})

	clearSyntheticReplacementRegistry()
	body := "phone=" + syntheticPhone(1) + " id=" + syntheticIDCard(3) + " card=" + syntheticBankCard(1) + " token=fake_token_0001"
	redacted, summary, changed, err := svc.Redact([]byte(body))
	if err != nil {
		t.Fatalf("redact synthetic-looking values: %v", err)
	}
	if !changed || !summary.Changed {
		t.Fatalf("unregistered synthetic-looking values should still be redacted: %s %+v", redacted, summary)
	}
	output := string(redacted)
	for _, raw := range []string{syntheticPhone(1), syntheticIDCard(3), syntheticBankCard(1), "fake_token_0001"} {
		if strings.Contains(output, raw) {
			t.Fatalf("unregistered synthetic-looking raw value %s should not pass through unchanged: %s", raw, output)
		}
	}
	for _, replacement := range []string{syntheticPhone(2), syntheticIDCard(1), syntheticBankCard(2), "fake_token_0002"} {
		if !strings.Contains(output, replacement) {
			t.Fatalf("expected replacement %s in %s", replacement, output)
		}
	}
}

func TestSyntheticReplacementRegistryPersistsInStore(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "phone", Type: RuleTypeRegex, Category: CategoryPhone, PatternOrValue: `\b1[3-9]\d{9}\b`, Enabled: true})

	rawPhone := "13411110001"
	firstRedacted, _, changed, err := svc.Redact([]byte("phone=" + rawPhone))
	if err != nil {
		t.Fatalf("first redact: %v", err)
	}
	if !changed {
		t.Fatalf("expected first redaction")
	}
	syntheticBody := string(firstRedacted)
	if syntheticBody == "phone="+rawPhone {
		t.Fatalf("expected synthetic phone replacement")
	}

	records, err := svc.ListSyntheticValues(context.Background())
	if err != nil {
		t.Fatalf("list synthetic values: %v", err)
	}
	if len(records) != 1 || records[0].Kind != syntheticKindPhone {
		t.Fatalf("unexpected synthetic records: %+v", records)
	}
	if records[0].RawValue != rawPhone {
		t.Fatalf("expected raw value to be persisted, got %+v", records[0])
	}
	if !strings.Contains(syntheticBody, records[0].Replacement) {
		t.Fatalf("redacted body %q should contain persisted value %+v", syntheticBody, records[0])
	}

	clearSyntheticReplacementRegistry()
	secondRedacted, summary, changed, err := svc.Redact([]byte(syntheticBody))
	if err != nil {
		t.Fatalf("second redact: %v", err)
	}
	if changed || summary.Changed {
		t.Fatalf("persisted synthetic value should be skipped after memory clear: %s %+v", secondRedacted, summary)
	}

	records, err = svc.ListSyntheticValues(context.Background())
	if err != nil {
		t.Fatalf("list synthetic values after skip: %v", err)
	}
	if len(records) != 1 || records[0].HitCount != 1 {
		t.Fatalf("expected hit count to be updated, got %+v", records)
	}
}

func TestDeleteSyntheticValueRemovesSingleMappingAndReplacement(t *testing.T) {
	svc := newTestService(t, true)
	mustCreateRule(t, svc, PrivacyRuleInput{Name: "phone", Type: RuleTypeRegex, Category: CategoryPhone, PatternOrValue: `\b1[3-9]\d{9}\b`, Enabled: true})

	if _, _, changed, err := svc.Redact([]byte("phone=13900001111")); err != nil {
		t.Fatalf("redact: %v", err)
	} else if !changed {
		t.Fatalf("expected redaction")
	}

	records, err := svc.ListSyntheticValues(context.Background())
	if err != nil {
		t.Fatalf("list synthetic values: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one synthetic mapping, got %+v", records)
	}
	record := records[0]
	if record.RawHash == "" || record.RawValue != "13900001111" || record.Replacement == "" {
		t.Fatalf("unexpected synthetic mapping record: %+v", record)
	}

	if err := svc.DeleteSyntheticValue(context.Background(), record.Kind, record.RawHash); err != nil {
		t.Fatalf("delete synthetic value: %v", err)
	}

	records, err = svc.ListSyntheticValues(context.Background())
	if err != nil {
		t.Fatalf("list synthetic values after delete: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected synthetic mapping to be deleted, got %+v", records)
	}

	var valueCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM privacy_synthetic_values WHERE kind = ? AND value = ?`, record.Kind, record.Replacement).Scan(&valueCount); err != nil {
		t.Fatalf("query synthetic values after delete: %v", err)
	}
	if valueCount != 0 {
		t.Fatalf("expected replacement value to be deleted with mapping, got %d", valueCount)
	}
}
