package tracking

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestContextSummaryRecordsStructureWithoutPayload(t *testing.T) {
	body := []byte(`{"instructions":"private instruction","tools":[{"name":"private tool"}],"previous_response_id":"private link","input":[{"role":"developer","content":[{"type":"input_text","text":"<project_instructions>private path"},{"type":"input_text","text":"<skills_instructions>private skill"}]},{"role":"user","content":"private prompt"},{"type":"function_call_output","output":"private output"},{"type":"compaction","encrypted_content":"private summary"}]}`)
	raw := summarizeContext("responses", body)
	if strings.Contains(raw, "private") {
		t.Fatal("summary unexpectedly contains request content")
	}
	var got struct {
		Parts   map[string]contextPart
		Partial bool
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Partial || len(got.Parts) != 7 {
		t.Fatalf("expected seven groups and linked context, got %+v", got)
	}
	for _, kind := range []string{"system", "tools", "memory", "skills", "chat", "results", "summary"} {
		if part := got.Parts[kind]; part.Items != 1 || part.Tokens <= 0 {
			t.Errorf("%s: %+v", kind, part)
		}
	}
	if summarizeContext("responses", bytes.Repeat([]byte(" "), maxContextSummaryBytes+1)) != "" || summarizeContext("responses", []byte("{")) != "" {
		t.Fatal("oversized and invalid bodies should be skipped")
	}
}

func TestContextSummaryAcceptsLargePromptWithinBound(t *testing.T) {
	const payloadBytes = 600 << 10
	body, err := json.Marshal(struct {
		Input string `json:"input"`
	}{Input: strings.Repeat("x", payloadBytes)})
	if err != nil {
		t.Fatal(err)
	}
	raw := summarizeContext("responses", body)
	if raw == "" {
		t.Fatal("large but bounded request body should produce a context summary")
	}
	if strings.Contains(raw, strings.Repeat("x", 100)) {
		t.Fatal("context summary unexpectedly contains request text")
	}
	var summary contextRequestSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Parts["chat"].Tokens < payloadBytes/5 {
		t.Fatalf("large prompt was not represented in the summary: %+v", summary.Parts["chat"])
	}
}

func TestContextSummarySeparatesNamedPartsAndTurns(t *testing.T) {
	body := []byte(`{
	  "instructions":"Agent text\n# AGENTS.md instructions for D:\\private\\workspace\nDo not persist this text.\n<skills_instructions>\n<available_skills>\n- impeccable: design\n</available_skills>\n</skills_instructions>",
	  "tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"exec"}]},
	           {"type":"namespace","name":"mcp__collaboration","tools":[{},{}]},
	           {"type":"function","name":"read_file"}],
	  "input":[
	    {"role":"user","content":"<environment_context>secret cwd</environment_context>"},
	    {"role":"user","content":"first user message"},
	    {"type":"function_call","call_id":"private-call-id","name":"read_file","arguments":"{\"path\":\"D:\\\\private\\\\workspace\\\\README.md\"}"},
	    {"type":"function_call_output","call_id":"private-call-id","output":"raw output"},
	    {"role":"assistant","content":[{"type":"output_text","text":"raw answer"}]},
	    {"role":"user","content":[{"type":"input_text","text":"second user message"},{"type":"input_image","image_url":"private image"}]}
	  ]
	}`)
	raw := summarizeContext("responses", body)
	var summary contextRequestSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Version != 2 || summary.Turns != 2 || summary.Partial {
		t.Fatalf("unexpected summary: %s", raw)
	}
	for _, check := range []struct {
		group, kind, name string
		count, turn       int
	}{
		{"system", "prompt", "", 1, 0},
		{"system", "environment", "", 1, 0},
		{"memory", "instruction", "AGENTS.md", 1, 0},
		{"skills", "skill", "impeccable", 1, 0},
		{"tools", "tool", "exec", 1, 0},
		{"tools", "mcp", "collaboration", 2, 0},
		{"files", "file", "README.md", 1, 0},
		{"files", "image", "", 1, 0},
		{"chat", "turn", "", 3, 1},
		{"chat", "turn", "", 1, 2},
	} {
		found := false
		for _, entry := range summary.Parts[check.group].Breakdown {
			found = found || (entry.Kind == check.kind && entry.Name == check.name && entry.Turn == check.turn && entry.Count == check.count && entry.Tokens > 0)
		}
		if !found {
			t.Errorf("missing %+v in %s", check, raw)
		}
	}
	for _, fragment := range []string{"private", "raw output", "raw answer", "user message", "persist this text", "workspace"} {
		if strings.Contains(raw, fragment) {
			t.Errorf("unexpected request content in summary: %q", fragment)
		}
	}
}

func TestContextSummaryBoundsEntriesAndKeepsTotals(t *testing.T) {
	var tools []string
	for index := range 90 {
		tools = append(tools, fmt.Sprintf(`{"name":"tool_%02d","type":"function","description":"%s"}`, index, strings.Repeat("x", index)))
	}
	body := []byte(`{"tools":[` + strings.Join(tools, ",") + `]}`)
	var summary contextRequestSummary
	if err := json.Unmarshal([]byte(summarizeContext("responses", body)), &summary); err != nil {
		t.Fatal(err)
	}
	part := summary.Parts["tools"]
	if len(part.Breakdown) != maxContextEntries || part.Breakdown[0].Name < "tool_85" {
		t.Fatalf("incorrect retained entries: %+v", part)
	}
	if rest := part.Breakdown[len(part.Breakdown)-1]; rest.Kind != "other" || rest.Count != 90-maxContextEntries+1 {
		t.Fatalf("incorrect merged entries: %+v", rest)
	}
	total := 0
	for _, entry := range part.Breakdown {
		total += entry.Tokens
	}
	if total != part.Tokens || part.Items != len(tools) {
		t.Fatalf("merged summary lost totals: %+v", part)
	}
}

func BenchmarkContextSummary(b *testing.B) {
	payload := `{"input":[{"role":"user","content":%q}],"tools":[{"name":"exec","type":"function"}]}`
	for _, size := range []int{8 << 10, 128 << 10, 500 << 10, 520 << 10} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			body := []byte(fmt.Sprintf(payload, strings.Repeat("x", size)))
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				summarizeContext("responses", body)
			}
		})
	}
}

func TestContextSummaryPersistsAndLegacyRowsStillLoad(t *testing.T) {
	manager, adapter := newReplayTestManager(t, filepath.Join(t.TempDir(), "usage.db"))
	tracker := &UsageTracker{config: &Config{Enabled: true}, hotPoolEnabled: true, hotPool: NewHotPool(HotPoolConfig{MaxSize: 10})}
	t.Cleanup(func() { _ = tracker.hotPool.Close() })
	req := replayTestRequest("context-summary")
	if err := tracker.hotPool.Add(req); err != nil {
		t.Fatal(err)
	}
	tracker.RecordContextSummary(req.RequestID, "messages", []byte(`{"system":"system","tools":[{"name":"shell"}],"messages":[{"role":"user","content":[{"type":"text","text":"start"}]},{"role":"assistant","content":[{"type":"tool_use","id":"private-id","name":"shell","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"private-id","content":"result"},{"type":"text","text":"followup"}]}]}`))
	projected := tracker.ActiveRequestToDetail(req).ContextSummary
	var summary struct{ Parts map[string]contextPart }
	if err := json.Unmarshal([]byte(projected), &summary); err != nil {
		t.Fatal(err)
	}
	if got := []int{summary.Parts["system"].Items, summary.Parts["results"].Items, summary.Parts["chat"].Items}; !reflect.DeepEqual(got, []int{1, 1, 3}) {
		t.Fatalf("incorrect Claude context grouping: %v", got)
	}
	if result := summary.Parts["results"].Breakdown; len(result) != 1 || result[0].Name != "shell" {
		t.Fatalf("tool result lost its structural name: %+v", result)
	}
	if err := manager.batchInsert([]*ArchiveEvent{{Request: req}}); err != nil {
		t.Fatal(err)
	}
	read := func(want string) {
		t.Helper()
		rows, err := tracker.queryRequestDetailsFromSource(t.Context(), adapter.GetReadDB(), &QueryOptions{})
		if err != nil || len(rows) != 1 || rows[0].ContextSummary != want {
			t.Fatalf("query context summary: rows=%+v, err=%v", rows, err)
		}
	}
	read(projected)
	if _, err := adapter.GetWriteDB().Exec("ALTER TABLE request_logs DROP COLUMN context_summary"); err != nil {
		t.Fatal(err)
	}
	read("")
}
