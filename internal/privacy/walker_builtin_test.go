package privacy

import (
	"strings"
	"testing"
)

func literalRuleFor(id int64, pattern, placeholder string) Rule {
	return Rule{
		ID: id, Enabled: true, Name: "literal-" + pattern, Priority: 10,
		MatchType: MatchTypeLiteral, Pattern: pattern,
		Action: ActionRedact, ReplacementMode: ReplacementPlaceholder,
		Placeholder: placeholder, Source: SourceCustom,
	}
}

// 协议字段必须天然不匹配：白名单只锁定对话文本，
// 改写 model/stream/tools 会破坏请求语义甚至触发上游指纹检测。
func TestWalkerClaudeScansTextFieldsOnly(t *testing.T) {
	body := []byte(`{"model":"SECRET","stream":true,` +
		`"system":"SECRET in system",` +
		`"tools":[{"name":"SECRET","description":"SECRET"}],` +
		`"metadata":{"user_id":"SECRET"},` +
		`"messages":[` +
		`{"role":"user","content":"SECRET plain"},` +
		`{"role":"assistant","content":[{"type":"text","text":"SECRET block"}]},` +
		`{"role":"user","content":[{"type":"tool_result","content":"SECRET tool"}]},` +
		`{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"SECRET nested"}]}]}` +
		`]}`)

	snapshot := newTestSnapshot(t, redactSettings(), literalRuleFor(1, "SECRET", "[X]"))
	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	got := string(result.Body)
	// 应被扫描的 5 处
	for _, want := range []string{
		`"system":"[X] in system"`,
		`"content":"[X] plain"`,
		`"text":"[X] block"`,
		`"content":"[X] tool"`,
		`"text":"[X] nested"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected scanned field to be redacted: %s\ngot: %s", want, got)
		}
	}
	// 不应被触碰的协议字段
	for _, want := range []string{
		`"model":"SECRET"`,
		`"name":"SECRET"`,
		`"description":"SECRET"`,
		`"user_id":"SECRET"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("protocol field must not be rewritten: %s\ngot: %s", want, got)
		}
	}
}

func TestWalkerCodexScansTextFieldsOnly(t *testing.T) {
	body := []byte(`{"model":"SECRET","stream":true,"reasoning":{"effort":"SECRET"},` +
		`"instructions":"SECRET inst",` +
		`"input":[` +
		`{"role":"user","content":"SECRET plain"},` +
		`{"role":"user","content":[{"type":"input_text","text":"SECRET block"}]},` +
		`{"type":"function_call_output","output":"SECRET output"}` +
		`]}`)

	snapshot := newTestSnapshot(t, redactSettings(), literalRuleFor(1, "SECRET", "[X]"))
	result, err := snapshot.Apply(Request{Path: "/v1/responses"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	got := string(result.Body)
	for _, want := range []string{
		`"instructions":"[X] inst"`,
		`"content":"[X] plain"`,
		`"text":"[X] block"`,
		`"output":"[X] output"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected scanned field to be redacted: %s\ngot: %s", want, got)
		}
	}
	for _, want := range []string{`"model":"SECRET"`, `"effort":"SECRET"`} {
		if !strings.Contains(got, want) {
			t.Errorf("protocol field must not be rewritten: %s\ngot: %s", want, got)
		}
	}
}

func TestWalkerEscapesReplacementWithoutHTMLEncoding(t *testing.T) {
	rule := literalRuleFor(1, "TARGET", `a<b & c>d "quoted" back\slash`)
	snapshot := newTestSnapshot(t, redactSettings(), rule)
	body := []byte(`{"messages":[{"role":"user","content":"TARGET"}]}`)

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got := string(result.Body)
	// 引号和反斜杠必须转义，尖括号和和号必须保持原样
	if !strings.Contains(got, `a<b & c>d \"quoted\" back\\slash`) {
		t.Fatalf("unexpected escaping: %s", got)
	}
	// encoding/json 默认会把尖括号和和号转成 Unicode 转义形式。
	// 替换值全为可打印 ASCII，正常输出不该出现任何 \u00XX 序列。
	unicodeEscapePrefix := `\` + "u00"
	if strings.Contains(got, unicodeEscapePrefix) {
		t.Fatalf("replacement must not be unicode-escaped: %s", got)
	}
	// 结果仍须是合法 JSON
	if _, err := collectJSONTextSegments(result.Body, matchClaudeMessagesTextPath); err != nil {
		t.Fatalf("redacted body is not valid json: %v", err)
	}
}

// 内置匹配器的价值在于低误报：把订单号、时间戳、版本号当 PII 会让用户直接关掉整个功能。
func TestBuiltinMatchersRejectNonPIIDigits(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"订单号", "订单 20240115093012345678 已创建"},
		{"毫秒时间戳", "ts=1705294212345"},
		{"连续序号", "1234567890123456"},
		{"更长数字串中的片段", "id=1341111000312345678901"},
		{"身份证校验位错误", "110101199001011239"},
		{"身份证省份码非法", "990101199001011237"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if hits := matchBuiltinCNMobile(tc.text); len(hits) > 0 {
				t.Errorf("mobile false positive: %q", tc.text[hits[0][0]:hits[0][1]])
			}
			if hits := matchBuiltinCNIDCard(tc.text); len(hits) > 0 {
				t.Errorf("id-card false positive: %q", tc.text[hits[0][0]:hits[0][1]])
			}
			if hits := matchBuiltinBankCardLuhn(tc.text); len(hits) > 0 {
				t.Errorf("bank-card false positive: %q", tc.text[hits[0][0]:hits[0][1]])
			}
		})
	}
}

func TestBuiltinMatchersAcceptRealPIIShapes(t *testing.T) {
	if hits := matchBuiltinCNMobile("联系 13800138000"); len(hits) != 1 {
		t.Errorf("expected one mobile hit, got %d", len(hits))
	}
	// 合法校验位的身份证（校验位由算法反推）
	idBase := "42082219990101101"
	id := idBase + string(cnIDCheckChars[cnIDWeightedSum(idBase)%11])
	if hits := matchBuiltinCNIDCard("身份证 " + id); len(hits) != 1 {
		t.Errorf("expected one id-card hit for %s, got %d", id, len(hits))
	}
	// UnionPay 前缀 + Luhn 合法
	card := pseudonymBankCard(1)
	if hits := matchBuiltinBankCardLuhn("卡号 " + card); len(hits) != 1 {
		t.Errorf("expected one bank-card hit for %s, got %d", card, len(hits))
	}
}

func TestBuiltinEmailSkipsCommandAndAssetContexts(t *testing.T) {
	if hits := matchBuiltinEmail("git@github.com:owner/repo.git"); len(hits) > 0 {
		t.Error("git remote must not be treated as an email")
	}
	if hits := matchBuiltinEmail("引用 logo@2x.png"); len(hits) > 0 {
		t.Error("image asset name must not be treated as an email")
	}
	if hits := matchBuiltinEmail("联系 someone@example.com"); len(hits) != 1 {
		t.Error("expected a real email hit")
	}
}

// 伪名化生成的值必须格式合法，否则上游会因校验失败而拒绝请求。
func TestPseudonymGeneratesStructurallyValidValues(t *testing.T) {
	for i := 1; i <= 50; i++ {
		id := pseudonymIDCard(i)
		if !validCNIDCard(id) {
			t.Errorf("generated id card %q fails checksum validation", id)
		}
		card := pseudonymBankCard(i)
		if !validLuhn(card) {
			t.Errorf("generated bank card %q fails Luhn validation", card)
		}
		if !matchesKnownPaymentCardProfile(card) {
			t.Errorf("generated bank card %q does not match any card profile", card)
		}
		phone := pseudonymPhone(i)
		if len(matchBuiltinCNMobile(phone)) != 1 {
			t.Errorf("generated phone %q is not a well-formed mobile number", phone)
		}
	}
}

func TestPseudonymIsStableWithinRequestAndStateless(t *testing.T) {
	rule := Rule{
		ID: 1, Enabled: true, Name: "手机号", Priority: 10,
		MatchType: MatchTypeBuiltin, Pattern: BuiltinCNMobile,
		Action: ActionRedact, ReplacementMode: ReplacementPseudonym,
		PseudonymKind: pseudonymKindPhone, Source: SourceBuiltin,
	}
	snapshot := newTestSnapshot(t, redactSettings(), rule)
	// 同一号码出现两次，另一号码出现一次
	body := []byte(`{"messages":[{"role":"user","content":"A=13800138000 B=13900139000 A=13800138000"}]}`)

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got := string(result.Body)
	if strings.Contains(got, "13800138000") || strings.Contains(got, "13900139000") {
		t.Fatalf("real numbers leaked: %s", got)
	}
	first, second := pseudonymPhone(1), pseudonymPhone(2)
	if strings.Count(got, first) != 2 {
		t.Errorf("same value must map to the same pseudonym twice, got: %s", got)
	}
	if !strings.Contains(got, second) {
		t.Errorf("distinct values must map to distinct pseudonyms, got: %s", got)
	}

	// 无状态：同一输入重复执行结果一致，且没有任何跨请求存储被写入
	again, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if string(again.Body) != got {
		t.Fatal("stateless pseudonymization must be deterministic across requests")
	}
}

// 会话历史反复重发：已经是替身值的内容不能被再次替换，
// 否则同一实体在上下文里会出现多个假名，AI 无法追踪指代。
func TestPseudonymValuesAreNotReplacedAgain(t *testing.T) {
	rule := Rule{
		ID: 1, Enabled: true, Name: "手机号", Priority: 10,
		MatchType: MatchTypeBuiltin, Pattern: BuiltinCNMobile,
		Action: ActionRedact, ReplacementMode: ReplacementPseudonym,
		PseudonymKind: pseudonymKindPhone, Source: SourceBuiltin,
	}
	snapshot := newTestSnapshot(t, redactSettings(), rule)
	existing := pseudonymPhone(1)
	body := []byte(`{"messages":[{"role":"assistant","content":"之前说的 ` + existing + ` 对吗"}]}`)

	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Changed || result.HitCount != 0 {
		t.Fatalf("existing pseudonym must be left alone, got: %s", result.Body)
	}
}

func TestPresetsCompile(t *testing.T) {
	for _, preset := range Presets() {
		if len(preset.Rules) == 0 {
			t.Errorf("preset %q has no rules", preset.ID)
		}
		if _, err := CompileRules(preset.Rules); err != nil {
			t.Errorf("preset %q failed to compile: %v", preset.ID, err)
		}
	}
	if _, ok := FindPreset(PresetAIAPIKeys); !ok {
		t.Error("FindPreset must locate the api-keys preset")
	}
}
