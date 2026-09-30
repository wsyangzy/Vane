package privacy

import (
	"bytes"
	"strings"
	"testing"
)

// 贴近 Claude Code 真实请求的形态：system 数组、thinking 块带签名、tool_use 参数对象、
// 嵌套 tool_result、cache_control、metadata.user_id、tools 定义。
const claudeCodeSample = `{"model":"claude-opus-5","max_tokens":32000,"stream":true,` +
	`"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude.","cache_control":{"type":"ephemeral"}}],` +
	`"messages":[` +
	`{"role":"user","content":[{"type":"text","text":"客户 SECRET 的订单"}]},` +
	`{"role":"assistant","content":[` +
	`{"type":"thinking","thinking":"用户提到 SECRET","signature":"EqQBCkYIBxgCKkCSECRET"},` +
	`{"type":"tool_use","id":"toolu_01SECRET","name":"Bash","input":{"command":"grep SECRET orders.csv","description":"查 SECRET"}}]},` +
	`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01SECRET","content":[{"type":"text","text":"SECRET,13800138000"}]}]}` +
	`],` +
	`"tools":[{"name":"Bash","description":"Run SECRET","input_schema":{"type":"object","properties":{"command":{"type":"string","description":"SECRET"}}}}],` +
	`"metadata":{"user_id":"user_SECRET_account"},` +
	`"thinking":{"type":"enabled","budget_tokens":31999}}`

func TestWalkerClaudeCoversToolArgumentsAndResults(t *testing.T) {
	snapshot := newTestSnapshot(t, redactSettings(), literalRuleFor(1, "SECRET", "[X]"))
	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, []byte(claudeCodeSample))
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got := string(result.Body)

	// 对话文本、工具调用参数、工具结果都必须被扫描
	for _, want := range []string{
		`"text":"客户 [X] 的订单"`,
		`"command":"grep [X] orders.csv"`,
		`"description":"查 [X]"`,
		`"text":"[X],13800138000"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected content field to be redacted: %s\ngot: %s", want, got)
		}
	}
	// 协议字段一个字节都不能动：签名受上游校验、ID 用于工具调用配对、
	// tools 定义与 metadata 改写会让模型调用错工具或被当成伪造身份
	for _, want := range []string{
		`"thinking":"用户提到 SECRET"`,
		`"signature":"EqQBCkYIBxgCKkCSECRET"`,
		`"id":"toolu_01SECRET"`,
		`"tool_use_id":"toolu_01SECRET"`,
		`"name":"Bash"`,
		`"description":"Run SECRET"`,
		`"description":"SECRET"}`,
		`"user_id":"user_SECRET_account"`,
		`"model":"claude-opus-5"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("protocol field must not be rewritten: %s\ngot: %s", want, got)
		}
	}
}

// 未命中时整个请求必须 byte-identical：任何改动都等于伪造客户端请求
func TestWalkerRealisticSamplesAreByteIdenticalWithoutHits(t *testing.T) {
	snapshot := newTestSnapshot(t, redactSettings(), literalRuleFor(1, "NEVER_PRESENT_TOKEN", "[X]"))
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/v1/messages", claudeCodeSample},
		{"/v1/responses", codexSample},
		{"/responses", codexSample},
		{"/v1/chat/completions", chatCompletionsSample},
	} {
		result, err := snapshot.Apply(Request{Path: tc.path}, []byte(tc.body))
		if err != nil {
			t.Fatalf("%s: Apply returned error: %v", tc.path, err)
		}
		if result.Changed || !bytes.Equal(result.Body, []byte(tc.body)) {
			t.Fatalf("%s: zero-hit request must be byte-identical", tc.path)
		}
		if result.SkippedReason != "" {
			t.Fatalf("%s: path must be scanned, got skipped=%q", tc.path, result.SkippedReason)
		}
	}
}

const codexSample = `{"model":"gpt-5.5","instructions":"You are Codex. SECRET policy","stream":true,` +
	`"reasoning":{"effort":"high","summary":"SECRET"},` +
	`"input":[` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"看看 SECRET"}]},` +
	`{"type":"reasoning","id":"rs_SECRET","summary":[{"type":"summary_text","text":"想到 SECRET"}],"encrypted_content":"gAAA_SECRET"},` +
	`{"type":"function_call","call_id":"call_SECRET","name":"shell","arguments":"{\"command\":[\"cat\",\"SECRET.txt\"]}"},` +
	`{"type":"function_call_output","call_id":"call_SECRET","output":[{"type":"input_text","text":"SECRET 内容"}]},` +
	`{"type":"custom_tool_call","call_id":"call_2","name":"apply_patch","input":"*** SECRET patch"}` +
	`],` +
	`"tools":[{"type":"function","name":"shell","description":"SECRET tool"}],` +
	`"prompt_cache_key":"SECRET-cache"}`

func TestWalkerCodexCoversFunctionCallsAndReasoningSummaries(t *testing.T) {
	snapshot := newTestSnapshot(t, redactSettings(), literalRuleFor(1, "SECRET", "[X]"))
	result, err := snapshot.Apply(Request{Path: "/responses"}, []byte(codexSample))
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got := string(result.Body)
	for _, want := range []string{
		`"instructions":"You are Codex. [X] policy"`,
		`"text":"看看 [X]"`,
		`"text":"想到 [X]"`,
		// arguments 是 JSON 字符串：按文本扫描，结果仍须是合法 JSON 转义
		`"arguments":"{\"command\":[\"cat\",\"[X].txt\"]}"`,
		`"text":"[X] 内容"`,
		`"input":"*** [X] patch"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected content field to be redacted: %s\ngot: %s", want, got)
		}
	}
	for _, want := range []string{
		`"id":"rs_SECRET"`,
		`"encrypted_content":"gAAA_SECRET"`,
		`"call_id":"call_SECRET"`,
		`"summary":"SECRET"}`, // reasoning 配置不是对话内容
		`"description":"SECRET tool"`,
		`"prompt_cache_key":"SECRET-cache"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("protocol field must not be rewritten: %s\ngot: %s", want, got)
		}
	}
	if _, err := collectJSONTextSegments(result.Body, matchCodexResponsesTextPath); err != nil {
		t.Fatalf("redacted body must stay valid JSON: %v", err)
	}
}

const chatCompletionsSample = `{"model":"gpt-4o","messages":[` +
	`{"role":"system","content":"系统 SECRET"},` +
	`{"role":"user","content":[{"type":"text","text":"问 SECRET"},{"type":"image_url","image_url":{"url":"https://x/SECRET.png"}}]},` +
	`{"role":"assistant","tool_calls":[{"id":"call_SECRET","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"SECRET\"}"}}]},` +
	`{"role":"tool","tool_call_id":"call_SECRET","content":"结果 SECRET"}` +
	`],"user":"SECRET-user"}`

func TestWalkerChatCompletionsIsScanned(t *testing.T) {
	snapshot := newTestSnapshot(t, redactSettings(), literalRuleFor(1, "SECRET", "[X]"))
	result, err := snapshot.Apply(Request{Path: "/v1/chat/completions"}, []byte(chatCompletionsSample))
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	got := string(result.Body)
	for _, want := range []string{`"content":"系统 [X]"`, `"text":"问 [X]"`, `"arguments":"{\"q\":\"[X]\"}"`, `"content":"结果 [X]"`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected content field to be redacted: %s\ngot: %s", want, got)
		}
	}
	for _, want := range []string{`"url":"https://x/SECRET.png"`, `"id":"call_SECRET"`, `"tool_call_id":"call_SECRET"`, `"user":"SECRET-user"`, `"name":"lookup"`} {
		if !strings.Contains(got, want) {
			t.Errorf("protocol field must not be rewritten: %s\ngot: %s", want, got)
		}
	}
}

func TestScanPathNormalization(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens", "/v1/responses", "/responses", "/responses/compact", "/v1/responses?x=1", "/chat/completions", "/v1/chat/completions/"} {
		if !IsScannablePath(path) {
			t.Errorf("%s must be scannable", path)
		}
	}
	for _, path := range []string{"/v1/models", "/v1/alpha/search", "/v1/embeddings", ""} {
		if IsScannablePath(path) {
			t.Errorf("%s must not be scannable", path)
		}
	}
}

// 真实值恰好以替身前缀开头时必须照常脱敏：只看前缀会让它们绕过隐私防护
func TestLooksLikePseudonymRejectsRealValuesSharingThePrefix(t *testing.T) {
	for i := 1; i <= 20; i++ {
		for _, value := range []string{pseudonymPhone(i), pseudonymIDCard(i), pseudonymBankCard(i)} {
			if !looksLikePseudonym(value) {
				t.Errorf("generated pseudonym %q must be recognised", value)
			}
		}
	}
	for _, value := range []string{"fake_secret_0001", "fake_api_key_0042"} {
		if !looksLikePseudonym(value) {
			t.Errorf("%q must be recognised", value)
		}
	}
	realIDBase := "42082219990101055" // 序号 055 不在生成器范围（101~999）
	realID := realIDBase + string(cnIDCheckChars[cnIDWeightedSum(realIDBase)%11])
	for _, value := range []string{
		"fake_prod_key_abcdef", // 以 fake_ 开头的真实密钥
		"fake_secret_12345",    // 序号位数不符
		"13411110000",          // 序号 0000 不在生成器范围
		realID,
	} {
		if looksLikePseudonym(value) {
			t.Errorf("real value %q must not be treated as a pseudonym", value)
		}
	}
}

func TestNewBuiltinMatchers(t *testing.T) {
	cases := []struct {
		name    string
		matcher builtinMatcher
		hit     []string
		miss    []string
	}{
		{
			name:    "landline",
			matcher: matchBuiltinCNLandline,
			hit:     []string{"电话 010-88886666", "(0755) 26668888", "021 62345678-123"},
			miss:    []string{"01088886666", "版本 0.12-3456789", "010-12345678901"},
		},
		{
			name:    "passport",
			matcher: matchBuiltinCNPassport,
			hit:     []string{"护照号码 E12345678", "passport: EA1234567", "护照 G12345678"},
			miss:    []string{"工单 E12345678", "E12345678"},
		},
		{
			name:    "credit code",
			matcher: matchBuiltinCNCreditCode,
			hit:     []string{"统一社会信用代码 91350100M000100Y43"},
			miss:    []string{"91350100M000100Y44", "编号 ABCDEFGHJKLMNPQRTU"},
		},
		{
			name:    "public ipv4",
			matcher: matchBuiltinPublicIPv4,
			hit:     []string{"服务器 8.8.8.8", "连到 203.0.113.9:443", "ip=114.114.114.114"},
			miss:    []string{"127.0.0.1", "10.0.0.5", "192.168.1.1", "172.16.0.1", "version 1.2.3.4.5", "v1.2.3.4", "0.0.0.0", "100.64.0.1"},
		},
	}
	for _, tc := range cases {
		for _, text := range tc.hit {
			if len(tc.matcher(text)) != 1 {
				t.Errorf("%s: expected one hit in %q, got %v", tc.name, text, tc.matcher(text))
			}
		}
		for _, text := range tc.miss {
			if hits := tc.matcher(text); len(hits) != 0 {
				t.Errorf("%s: false positive %q in %q", tc.name, text[hits[0][0]:hits[0][1]], text)
			}
		}
	}
	// 每个内置匹配器都能编译，且都能生成可新建的规则
	for _, info := range BuiltinMatchers() {
		if _, err := compileBuiltinMatcher(info.Pattern); err != nil {
			t.Errorf("builtin %s must compile: %v", info.Pattern, err)
		}
		if _, ok := BuiltinRuleFor(info.Pattern); !ok {
			t.Errorf("builtin %s must produce a creatable rule", info.Pattern)
		}
	}
}

// 渠道隐私级别：全局仅检测时三方中转仍拿到脱敏正文；可信渠道拿原文；全局关闭是总开关
func TestSelectBodyHonoursChannelLevel(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"SECRET"}]}`)
	detect := DefaultSettings()
	snapshot := newTestSnapshot(t, detect, literalRuleFor(1, "SECRET", "[X]"))
	result, err := snapshot.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Changed || !bytes.Equal(result.Body, body) {
		t.Fatal("detect mode must keep the original body as the default")
	}
	if got, redacted := result.SelectBody(ChannelLevelThirdParty, body); !redacted || !strings.Contains(string(got), `"content":"[X]"`) {
		t.Fatalf("third-party relays must receive the redacted body, got %s", got)
	}
	if got, redacted := result.SelectBody(ChannelLevelTrusted, body); redacted || !bytes.Equal(got, body) {
		t.Fatalf("trusted channels must receive the original body, got %s", got)
	}
	if got, redacted := result.SelectBody(ChannelLevelDefault, body); redacted || !bytes.Equal(got, body) {
		t.Fatalf("default channels follow the global detect mode, got %s", got)
	}

	disabled := DefaultSettings()
	disabled.Mode = ModeDisabled
	off, _ := newTestSnapshot(t, disabled, literalRuleFor(1, "SECRET", "[X]")).Apply(Request{Path: "/v1/messages"}, body)
	if got, redacted := off.SelectBody(ChannelLevelThirdParty, body); redacted || !bytes.Equal(got, body) {
		t.Fatalf("a disabled global mode must win over the channel level, got %s", got)
	}
}

func TestPreviewRequestListsScannedFieldsWithoutPlaintextHits(t *testing.T) {
	svc := &Service{snapshot: newTestSnapshot(t, DefaultSettings(), literalRuleFor(1, "SECRET", "[X]"))}
	preview := svc.PreviewRequest("/v1/messages", []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"SECRET"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}]}`))
	if !preview.Supported || preview.Error != "" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	// 全局是仅检测，测试面板仍按脱敏推演
	if !preview.Changed || !strings.Contains(preview.Output, `"text":"[X]"`) {
		t.Fatalf("preview must show the redacted output, got %s", preview.Output)
	}
	paths := make([]string, 0, len(preview.Fields))
	for _, field := range preview.Fields {
		paths = append(paths, field.Path)
	}
	joined := strings.Join(paths, ",")
	if joined != "messages[0].content[0].text,messages[0].content[1].input.command" {
		t.Fatalf("scanned fields = %s", joined)
	}
	if !preview.Fields[0].Redacted || preview.Fields[1].Redacted {
		t.Fatalf("redacted flags wrong: %+v", preview.Fields)
	}

	if unsupported := svc.PreviewRequest("/v1/models", []byte(`{}`)); unsupported.Supported {
		t.Fatal("unsupported path must be reported")
	}
	if broken := svc.PreviewRequest("/v1/messages", []byte(`{"messages":[`)); broken.Error == "" {
		t.Fatal("unparsable body must report an error")
	}
}
