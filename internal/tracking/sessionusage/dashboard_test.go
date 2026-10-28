package sessionusage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDashboardMetadataTracksWorkspaceActivityAndNeverRetainsFullContent(t *testing.T) {
	root := t.TempDir()
	secret := "sk-proj-" + strings.Repeat("a", 32)
	prompt := "排查活动统计 key=" + secret + " " + strings.Repeat("正常内容", 80) + " secret-user-text"
	lines := `{"type":"session_meta","payload":{"id":"s","cwd":"D:\\work\\Vane"}}` + "\n" +
		`{"type":"turn_context","payload":{"model":"gpt","turn_id":"t"}}` + "\n" +
		`{"type":"event_msg","timestamp":"2026-10-27T10:00:00Z","payload":{"type":"user_message","message":"` + prompt + `"}}` + "\n" +
		`{"type":"response_item","timestamp":"2026-10-27T10:00:00Z","payload":{"role":"user","content":"` + prompt + `"}}` + "\n" +
		record("10:02:00", "r1", use1) + record("11:00:00", "r2", use1)
	writeLog(t, root, "sessions/a.jsonl", lines)
	writeLog(t, root, "archived_sessions/a.jsonl", lines)
	reader := NewReader()
	start := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		got, err := reader.Read(context.Background(), Roots{Codex: root}, start, start.AddDate(0, 0, 1), "", "")
		if err != nil {
			t.Fatal(err)
		}
		var active, prompts int64
		for _, item := range got.Activity {
			active += item.Milliseconds
			prompts += item.Prompts
		}
		if len(got.Calls) != 2 || active != 120000 || prompts != 1 {
			t.Fatalf("metadata=%+v active=%d prompts=%d", got, active, prompts)
		}
		if got.Calls[0].Project != "D:/work/Vane" || got.Calls[0].Session != "s" {
			t.Fatalf("workspace=%+v", got.Calls[0])
		}
		if !strings.HasPrefix(got.Calls[0].Title, "排查活动统计") || len([]rune(got.Calls[0].Title)) > titleLimit {
			t.Fatal("missing bounded display title")
		}
		data, _ := json.Marshal(got)
		if strings.Contains(string(data), "secret-user-text") || strings.Contains(string(data), secret) {
			t.Fatal("retained full transcript content or credentials")
		}
	}
}

func TestClaudeDashboardPreservesOneHourCacheSubsetAndPromptIdentity(t *testing.T) {
	p := newParser("claude", "a.jsonl")
	p.read([]byte(`{"type":"user","timestamp":"2026-10-27T10:00:00Z","sessionId":"s","cwd":"D:\\work\\Vane","uuid":"u","message":{"content":"secret"}}`))
	p.read([]byte(`{"type":"user","timestamp":"2026-10-27T10:00:01Z","sessionId":"s","uuid":"u","message":{"content":"secret"}}`))
	p.read([]byte(`{"type":"assistant","timestamp":"2026-10-27T10:01:00Z","sessionId":"s","message":{"id":"m","model":"sonnet","usage":{"input_tokens":100,"output_tokens":20,"cache_creation":{"ephemeral_5m_input_tokens":60,"ephemeral_1h_input_tokens":40}}}}`))
	var prompts int64
	for _, item := range p.activity {
		prompts += item.Prompts
	}
	if prompts != 1 {
		t.Fatalf("duplicate prompt count: %d", prompts)
	}
	for _, call := range p.calls {
		if call.CacheWrite != 100 || call.CacheWrite1h != 40 || call.Total() != 220 || call.Project != "D:/work/Vane" {
			t.Fatalf("cache/project=%+v", call)
		}
	}
}

func TestDashboardActivityUsesCompactMinuteBucketsAndExcludesSubagentWork(t *testing.T) {
	p := newParser("codex", "sessions/a.jsonl")
	p.session, p.project, p.model = "s", "D:/work", "gpt"
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		p.observe(at.Add(time.Duration(i)*time.Second), "")
	}
	if len(p.activity) != 1 {
		t.Fatalf("stored per-event activity instead of minute buckets: %d", len(p.activity))
	}
	for _, item := range p.activity {
		if item.Milliseconds != 59000 {
			t.Fatalf("minute work: %+v", item)
		}
	}
	sub := newParser("claude", "projects/p/s/subagents/agent-1.jsonl")
	sub.observe(at, "u")
	sub.observe(at.Add(time.Minute), "")
	if len(sub.activity) != 0 {
		t.Fatal("child activity must not inflate parent conversation active time")
	}
}
