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
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"x"},{"type":"text","text":"Summarize this <b>conversation</b>"}]}
		],
		"tools":[{"name":"a"},{"name":"b"}],
		"metadata":{"user_id":"u1"}
	}`)
	got := summarizeNonStreamRequest(body)
	for _, want := range []string{
		"model=claude-opus-5", "max_tokens=1", "messages=3", "tools=2",
		`metadata={"user_id":"u1"}`,
		`system="You are a helpful assistant."`,
		`last_user="<tool_result> | Summarize this <b>conversation</b>"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}

	if got := summarizeNonStreamRequest([]byte("not json")); !strings.Contains(got, "not a JSON object") {
		t.Fatalf("non-JSON body should be reported by length, got %s", got)
	}

	long := strings.Repeat("字", 400)
	if got := clip(long); len([]rune(got)) != nonStreamSummaryTextLimit+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("clip should cut to %d runes plus ellipsis, got %d", nonStreamSummaryTextLimit, len([]rune(got)))
	}
}
