package proxy

import (
	"strings"
	"testing"
)

func TestSummarizeNonStreamRequest(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-5","max_tokens":1,"stream":false,
		"thinking":{"type":"disabled"},
		"system":[{"type":"text","text":"You are a helpful assistant.","cache_control":{"type":"ephemeral"}}],
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"assistant","content":[{"type":"text","text":"hi"}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"x"},{"type":"text","text":"客户张三 13800138000 Summarize"}]}
		],
		"tools":[{"name":"a"},{"name":"b"}],
		"metadata":{"user_id":"device-secret-u1"}
	}`)
	got := summarizeNonStreamRequest(body)
	for _, want := range []string{
		"model=claude-opus-5", "max_tokens=1", "messages=3", "roles=user,assistant,user", "tools=2",
		"metadata_keys=user_id",
		"system=[text:28]",
		"last_user=[tool_result,text:26]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
	// 摘要在隐私脱敏之前执行、会进日志文件与前端日志查看器：任何文本内容与 metadata 值都不得出现
	for _, leaked := range []string{"helpful assistant", "张三", "13800138000", "Summarize", "device-secret-u1", "hello"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("summary leaked request content %q:\n%s", leaked, got)
		}
	}

	if got := summarizeNonStreamRequest([]byte("not json")); !strings.Contains(got, "not a JSON object") {
		t.Fatalf("non-JSON body should be reported by length, got %s", got)
	}
	if got := contentShape([]byte(`"字字字"`)); got != "text:3" {
		t.Fatalf("string content should report rune count, got %s", got)
	}
}
