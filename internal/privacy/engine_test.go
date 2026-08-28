package privacy

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func newTestSnapshot(t *testing.T, settings Settings, rules ...Rule) *Snapshot {
	t.Helper()
	compiled, err := CompileRules(rules)
	if err != nil {
		t.Fatalf("CompileRules failed: %v", err)
	}
	return &Snapshot{Version: 1, Settings: settings, Rules: compiled}
}

func redactSettings() Settings {
	s := DefaultSettings()
	s.Mode = ModeRedact
	return s
}

func phoneRule() Rule {
	return Rule{
		ID: 1, Enabled: true, Name: "手机号", Priority: 10,
		MatchType: MatchTypeBuiltin, Pattern: BuiltinCNMobile,
		Action: ActionRedact, ReplacementMode: ReplacementPlaceholder,
		Placeholder: "[手机号]", Source: SourceBuiltin,
	}
}

// 核心不变量：命中字段之外的每一个字节都必须原样保留。
// map[string]any + json.Marshal 的往返会重排键序、把 < > & 转义成 < 一类形式、
// 改变大数字精度——那等于伪造客户端的原始请求。
func TestEngineRedactPreservesUnmatchedBytesExactly(t *testing.T) {
	// 键刻意非字典序，并混入 HTML 特殊字符、超 float64 精度的整数、已转义序列
	body := []byte(`{"model":"claude-3","stream":true,"metadata":{"z_last":1,"a_first":2},` +
		`"max_tokens":12345678901234567890123,"tools":[{"description":"compare a < b && c > d"}],` +
		`"messages":[{"role":"user","content":"打给 13800138000 说 x<y&z"}]}`)

	snapshot := newTestSnapshot(t, redactSettings(), phoneRule())
	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected body to be changed")
	}

	got := string(result.Body)
	want := strings.Replace(string(body), "13800138000", "[手机号]", 1)
	if got != want {
		t.Fatalf("only the matched span may change\n got: %s\nwant: %s", got, want)
	}

	// 逐项确认最容易被 Marshal 破坏的部分
	for _, fragment := range []string{
		`"model":"claude-3","stream":true`,       // 键序
		`"z_last":1,"a_first":2`,                 // 嵌套对象键序
		`"max_tokens":12345678901234567890123`,   // 超精度整数
		`"description":"compare a < b && c > d"`, // tools 内的 HTML 字符未被转义也未被扫描
		`说 x<y&z`,                                // 命中字段内未命中部分的 HTML 字符
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("fragment mutated or missing: %s", fragment)
		}
	}
}

func TestEngineZeroHitReturnsIdenticalBody(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"今天天气不错"}],"model":"claude-3"}`)
	snapshot := newTestSnapshot(t, redactSettings(), phoneRule())

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Changed || result.HitCount != 0 {
		t.Fatalf("expected no change, got changed=%v hits=%d", result.Changed, result.HitCount)
	}
	if !bytes.Equal(result.Body, body) {
		t.Fatalf("body must be byte-identical on zero hit")
	}
}

func TestEngineDetectModeCountsWithoutRewriting(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"我的号码 13800138000"}]}`)
	settings := DefaultSettings() // 默认即 detect
	snapshot := newTestSnapshot(t, settings, phoneRule())

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Action != ModeDetect {
		t.Fatalf("expected detect action, got %q", result.Action)
	}
	if result.HitCount != 1 {
		t.Fatalf("detect mode must still count hits, got %d", result.HitCount)
	}
	if result.Changed || !bytes.Equal(result.Body, body) {
		t.Fatal("detect mode must not rewrite the body")
	}
}

func TestEngineRuleHitsNeverSerializeRawMatches(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"号码 13800138000"}]}`)
	snapshot := newTestSnapshot(t, redactSettings(), phoneRule())

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(result.RuleHits) != 1 || len(result.RuleHits[0].Matches) != 1 {
		t.Fatalf("expected one hit carrying the raw match for local logging")
	}

	encoded, err := json.Marshal(result.RuleHits)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(encoded), "13800138000") {
		t.Fatalf("raw match leaked through JSON serialization: %s", encoded)
	}
}

func TestEngineUnsupportedPathWarnsOrFailsClosed(t *testing.T) {
	body := []byte(`{"prompt":"号码 13800138000"}`)

	warn := newTestSnapshot(t, redactSettings(), phoneRule())
	result, err := warn.Apply(Request{Path: "/v1/unknown"}, body)
	if err != nil {
		t.Fatalf("warn mode must not reject: %v", err)
	}
	if result.SkippedReason != SkippedUnsupportedPath {
		t.Fatalf("expected skipped_unsupported_path, got %q", result.SkippedReason)
	}
	if !bytes.Equal(result.Body, body) {
		t.Fatal("warn mode must pass the body through untouched")
	}

	strictSettings := redactSettings()
	strictSettings.UnsupportedPathAction = UnsupportedPathFailClosed
	strict := newTestSnapshot(t, strictSettings, phoneRule())
	if _, err = strict.Apply(Request{Path: "/v1/unknown"}, body); err == nil {
		t.Fatal("fail_closed must reject unsupported paths")
	}
	var policyErr *PolicyError
	if !errors.As(err, &policyErr) || policyErr.Code != CodeUnsupportedPath {
		t.Fatalf("expected PolicyError with unsupported-path code, got %v", err)
	}
}

func TestEngineScanErrorHonoursOnErrorPolicy(t *testing.T) {
	broken := []byte(`{"messages":[{"role":"user","content":"未闭合`)

	open := newTestSnapshot(t, redactSettings(), phoneRule())
	result, err := open.Apply(Request{Path: "/v1/messages"}, broken)
	if err != nil {
		t.Fatalf("fail_open must not return an error: %v", err)
	}
	if !bytes.Equal(result.Body, broken) {
		t.Fatal("fail_open must pass the original body through")
	}

	closedSettings := redactSettings()
	closedSettings.OnError = OnErrorFailClosed
	closed := newTestSnapshot(t, closedSettings, phoneRule())
	_, err = closed.Apply(Request{Path: "/v1/messages"}, broken)
	var policyErr *PolicyError
	if !errors.As(err, &policyErr) || policyErr.Code != CodeScanFailed {
		t.Fatalf("expected scan-failed PolicyError, got %v", err)
	}
}

// Anthropic 的 key 同时满足 OpenAI 的宽松模式，靠优先级 + 重叠丢弃保证命中更精确的那条。
func TestEnginePriorityDropsOverlappingLowerRule(t *testing.T) {
	var anthropic, openai Rule
	for _, rule := range Presets()[1].Rules {
		switch rule.Name {
		case "Anthropic API Key":
			anthropic = rule
			anthropic.ID = 2
		case "OpenAI API Key":
			openai = rule
			openai.ID = 3
		}
	}
	if anthropic.Pattern == "" || openai.Pattern == "" {
		t.Fatal("preset rules not found")
	}

	// 故意先传 OpenAI，验证排序按 priority 而非入参顺序
	snapshot := newTestSnapshot(t, redactSettings(), openai, anthropic)
	body := []byte(`{"messages":[{"role":"user","content":"key=sk-ant-abcdefghijklmnopqrstuvwxyz01"}]}`)

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(result.RuleHits) != 1 {
		t.Fatalf("expected exactly one surviving hit, got %d", len(result.RuleHits))
	}
	if result.RuleHits[0].RuleName != "Anthropic API Key" {
		t.Fatalf("higher-priority rule must win, got %q", result.RuleHits[0].RuleName)
	}
	if !strings.Contains(string(result.Body), "[Anthropic密钥]") {
		t.Fatalf("unexpected replacement: %s", result.Body)
	}
}

// 命名捕获组 redact 只替换组内区间，保留字段名。
func TestEngineRedactCaptureGroupKeepsFieldName(t *testing.T) {
	rule := Rule{
		ID: 1, Enabled: true, Name: "密码字段", Priority: 10,
		MatchType: MatchTypeRegex, Pattern: `(?i)password\s*=\s*(?P<redact>\S+)`,
		Action: ActionRedact, ReplacementMode: ReplacementPlaceholder,
		Placeholder: "[密码]", Source: SourcePreset,
	}
	snapshot := newTestSnapshot(t, redactSettings(), rule)
	body := []byte(`{"messages":[{"role":"user","content":"password = hunter2000"}]}`)

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got := string(result.Body)
	if !strings.Contains(got, `password = [密码]`) {
		t.Fatalf("field name must survive, got: %s", got)
	}
	if strings.Contains(got, "hunter2000") {
		t.Fatalf("secret leaked: %s", got)
	}
}

func TestEngineScanBudgetTruncatesOrFailsClosed(t *testing.T) {
	long := strings.Repeat("x", 400) + "13800138000"
	body := []byte(`{"messages":[{"role":"user","content":"` + long + `"}]}`)

	prefixSettings := redactSettings()
	prefixSettings.ScanMaxBytes = 100
	prefix := newTestSnapshot(t, prefixSettings, phoneRule())
	result, err := prefix.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("scan_prefix must not reject: %v", err)
	}
	if !result.Truncated || result.SkippedReason != SkippedScanTruncated {
		t.Fatalf("expected truncation, got truncated=%v reason=%q", result.Truncated, result.SkippedReason)
	}
	if !bytes.Contains(result.Body, []byte("13800138000")) {
		t.Fatal("text beyond the budget must pass through unscanned")
	}

	closedSettings := redactSettings()
	closedSettings.ScanMaxBytes = 100
	closedSettings.OverLimitAction = OverLimitFailClosed
	closed := newTestSnapshot(t, closedSettings, phoneRule())
	_, err = closed.Apply(Request{Path: "/v1/messages"}, body)
	var policyErr *PolicyError
	if !errors.As(err, &policyErr) || policyErr.Code != CodeScanBodyTooLarge {
		t.Fatalf("expected body-too-large PolicyError, got %v", err)
	}
}

func TestEngineDisabledModeIsPassthrough(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"13800138000"}]}`)
	settings := DefaultSettings()
	settings.Mode = ModeDisabled
	snapshot := newTestSnapshot(t, settings, phoneRule())

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.HitCount != 0 || !bytes.Equal(result.Body, body) {
		t.Fatal("disabled mode must neither scan nor rewrite")
	}
}
