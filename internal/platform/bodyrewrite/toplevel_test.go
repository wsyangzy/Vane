package bodyrewrite

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReplaceTopLevelStringOnlyTouchesModelToken(t *testing.T) {
	// 键序非字母序、带缩进空白、HTML 字符原文与转义形式、超出 float64 的大数字、
	// 嵌套对象里的同名 model——这些都必须原样保留。
	body := []byte("{\n  \"stream\": true,\n  \"model\" :  \"claude-opus-5\",\n" +
		"  \"system\": \"a < b && c > d \\u003ctag\\u003e\",\n" +
		"  \"max_tokens\": 123456789012345678901234567890,\n" +
		"  \"metadata\": {\"model\": \"nested-keep\"},\n" +
		"  \"messages\": [{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"model\"}]}]\n}")
	want := []byte("{\n  \"stream\": true,\n  \"model\" :  \"claude-sonnet-5\",\n" +
		"  \"system\": \"a < b && c > d \\u003ctag\\u003e\",\n" +
		"  \"max_tokens\": 123456789012345678901234567890,\n" +
		"  \"metadata\": {\"model\": \"nested-keep\"},\n" +
		"  \"messages\": [{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"model\"}]}]\n}")

	got, old, ok := ReplaceTopLevelString(body, "model", "claude-sonnet-5")
	if !ok {
		t.Fatal("expected replacement to succeed")
	}
	if old != "claude-opus-5" {
		t.Fatalf("old = %q, want claude-opus-5", old)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected body\n got: %s\nwant: %s", got, want)
	}
}

func TestReplaceTopLevelStringDoesNotHTMLEscapeValue(t *testing.T) {
	body := []byte(`{"model":"a","x":1}`)
	got, _, ok := ReplaceTopLevelString(body, "model", `m<1>&"q"`)
	if !ok {
		t.Fatal("expected replacement to succeed")
	}
	want := `{"model":"m<1>&\"q\"","x":1}`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	var decoded struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(got, &decoded); err != nil || decoded.Model != `m<1>&"q"` {
		t.Fatalf("result must stay valid JSON with the new value, decoded=%q err=%v", decoded.Model, err)
	}
}

func TestReplaceTopLevelStringDecodesEscapedOldValue(t *testing.T) {
	// 原值使用 JSON 转义书写（\\u002d 即连字符），old 应返回解码后的字符串
	body := []byte("{\"model\":\"claude\\u002dhaiku\"}")
	got, old, ok := ReplaceTopLevelString(body, "model", "claude-sonnet")
	if !ok || old != "claude-haiku" {
		t.Fatalf("ok=%v old=%q", ok, old)
	}
	if string(got) != `{"model":"claude-sonnet"}` {
		t.Fatalf("got %s", got)
	}
}

func TestReplaceTopLevelStringKeepsBytesWhenValueUnchanged(t *testing.T) {
	// 原值已等于目标值时不重新编码，保留客户端原有的转义写法
	body := []byte("{\"model\":\"claude\\u002dhaiku\"}")
	got, old, ok := ReplaceTopLevelString(body, "model", "claude-haiku")
	if !ok || old != "claude-haiku" {
		t.Fatalf("ok=%v old=%q", ok, old)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body must stay byte-identical, got %s", got)
	}
}

func TestReplaceTopLevelStringReplacesDuplicateKeys(t *testing.T) {
	body := []byte(`{"model":"a","x":[1,2],"model":"b"}`)
	got, old, ok := ReplaceTopLevelString(body, "model", "c")
	if !ok {
		t.Fatal("expected replacement to succeed")
	}
	if old != "b" {
		t.Fatalf("old should follow encoding/json last-wins semantics, got %q", old)
	}
	if string(got) != `{"model":"c","x":[1,2],"model":"c"}` {
		t.Fatalf("got %s", got)
	}
}

func TestReplaceTopLevelStringReturnsBodyUnchangedWhenNotApplicable(t *testing.T) {
	cases := map[string]string{
		"not json":          `not a json`,
		"top-level array":   `[{"model":"a"}]`,
		"missing model":     `{"input":[],"metadata":{"model":"nested"}}`,
		"model is number":   `{"model":123}`,
		"model is null":     `{"model":null}`,
		"model is object":   `{"model":{"name":"a"}}`,
		"truncated":         `{"model":"a","messages":[`,
		"trailing value":    `{"model":"a"}{"model":"b"}`,
		"trailing garbage":  `{"model":"a"} x`,
		"empty body":        ``,
		"duplicate non-str": `{"model":"a","model":1}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			body := []byte(input)
			got, old, ok := ReplaceTopLevelString(body, "model", "new-model")
			if ok {
				t.Fatalf("expected ok=false, got old=%q body=%s", old, got)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("body must be returned unchanged, got %s", got)
			}
		})
	}
}

func TestReplaceTopLevelStringAllowsTrailingWhitespace(t *testing.T) {
	body := []byte("{\"model\":\"a\"}\r\n")
	got, _, ok := ReplaceTopLevelString(body, "model", "b")
	if !ok || string(got) != "{\"model\":\"b\"}\r\n" {
		t.Fatalf("ok=%v got=%q", ok, got)
	}
}

func TestLookupTopLevelString(t *testing.T) {
	if value, ok := LookupTopLevelString([]byte(`{"metadata":{"model":"x"},"model":"gpt-5"}`), "model"); !ok || value != "gpt-5" {
		t.Fatalf("value=%q ok=%v", value, ok)
	}
	if _, ok := LookupTopLevelString([]byte(`{"metadata":{"model":"x"}}`), "model"); ok {
		t.Fatal("nested model must not be treated as top-level")
	}
	if _, ok := LookupTopLevelString([]byte(`{"model":["a"]}`), "model"); ok {
		t.Fatal("non-string model must not be returned")
	}
}
