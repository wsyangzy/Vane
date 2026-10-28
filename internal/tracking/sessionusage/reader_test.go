package sessionusage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLog(t *testing.T, root, path, lines string) string {
	t.Helper()
	file := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func query(t *testing.T, r *Reader, roots Roots) []Call {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := r.Query(context.Background(), roots, start, start.AddDate(1, 0, 0), "", "")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func sum(calls []Call) Tokens {
	var result Tokens
	for _, c := range calls {
		result.Input += c.Input
		result.Output += c.Output
		result.CacheRead += c.CacheRead
		result.CacheWrite += c.CacheWrite
	}
	return result
}

func TestClaudeRevisionsSubagentsAndNoContentRetained(t *testing.T) {
	root := t.TempDir()
	line := func(at, id string, out int) string {
		return `{"type":"assistant","timestamp":"2026-10-27T` + at + `Z","sessionId":"s","requestId":"req","message":{"id":"` + id + `","model":"claude-sonnet","content":[{"text":"private-user-payload"}],"usage":{"input_tokens":100,"output_tokens":` + stringInt(out) + `,"cache_read_input_tokens":200,"cache_creation_input_tokens":30}}}` + "\n"
	}
	writeLog(t, root, "projects/p/s.jsonl", line("10:00:00", "m", 5)+line("10:00:01", "m", 10)+line("10:00:02", "m", 5))
	writeLog(t, root, "projects/p/s/subagents/agent-1.jsonl", line("10:01:00", "sub", 3))
	reader := NewReader()
	calls := query(t, reader, Roots{Claude: root})
	if len(calls) != 2 || sum(calls) != (Tokens{Input: 200, Output: 13, CacheRead: 400, CacheWrite: 60}) {
		t.Fatalf("calls=%+v", calls)
	}
	data, _ := json.Marshal(calls)
	if strings.Contains(string(data), "private-user-payload") {
		t.Fatal("retained transcript content")
	}
}

func stringInt(n int) string { b, _ := json.Marshal(n); return string(b) }

func count(at, total, last string) string {
	return `{"timestamp":"2026-10-27T` + at + `Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":` + total + `,"last_token_usage":` + last + `}}}` + "\n"
}
func record(at, id, usage string) string {
	return `{"timestamp":"2026-10-27T` + at + `Z","type":"token_usage_record","payload":{"response_id":"` + id + `","usage":` + usage + `}}` + "\n"
}

const meta = `{"type":"session_meta","payload":{"id":"codex-thread"}}` + "\n" + `{"type":"turn_context","payload":{"turn_id":"t","model":"gpt-codex"}}` + "\n"
const use1 = `{"input_tokens":100,"output_tokens":20,"cached_input_tokens":70,"reasoning_output_tokens":10}`
const use2 = `{"input_tokens":200,"output_tokens":40,"cached_input_tokens":140,"reasoning_output_tokens":20}`

func TestCodexPairedUsageCountersForkAndArchiveCopies(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		t.Run(stringInt(map[bool]int{false: 0, true: 1}[reversed]), func(t *testing.T) {
			root := t.TempDir()
			a, b := record("10:00:00", "r1", use1), count("10:00:01", use1, use1)
			if reversed {
				a, b = b, a
			}
			lines := meta + a + b + count("10:00:02", use1, use1) + record("10:01:00", "r2", use1) + count("10:01:01", use2, use1) + count("10:02:00", `{"input_tokens":500,"output_tokens":100}`, `{"input_tokens":0,"output_tokens":0}`)
			writeLog(t, root, "sessions/2026/10/27/a.jsonl", lines)
			writeLog(t, root, "archived_sessions/a.jsonl", lines)
			calls := query(t, NewReader(), Roots{Codex: root})
			if len(calls) != 2 || sum(calls) != (Tokens{Input: 60, Output: 40, CacheRead: 140}) {
				t.Fatalf("calls=%+v", calls)
			}
		})
	}
}

func TestCodexLegacyCumulativeNeverAddedAsPerCall(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1)+count("10:00:01", use1, use1)+count("10:01:00", use2, use1))
	calls := query(t, NewReader(), Roots{Codex: root})
	if len(calls) != 2 || sum(calls).Total() != 240 {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestCodexInputBoundaryPreservesDistinctEqualUsage(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "sessions/a.jsonl", meta+record("10:00:00", "r1", use1)+`{"type":"event_msg","payload":{"type":"user_message"}}`+"\n"+count("10:01:00", use2, use1))
	if calls := query(t, NewReader(), Roots{Codex: root}); len(calls) != 2 {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestReaderAppendPartialTruncateAndRemoval(t *testing.T) {
	root := t.TempDir()
	file := writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1)+`{"type":"event_msg"`)
	r := NewReader()
	if len(query(t, r, Roots{Codex: root})) != 1 {
		t.Fatal("initial read")
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("}\n" + count("10:01:00", use2, use1))
	f.Close()
	if len(query(t, r, Roots{Codex: root})) != 2 {
		t.Fatal("append read")
	}
	if len(query(t, r, Roots{Codex: root})) != 2 {
		t.Fatal("repeat read duplicates")
	}
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	if len(query(t, r, Roots{Codex: root})) != 1 {
		t.Fatal("truncated file not rebuilt")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if len(query(t, r, Roots{Codex: root})) != 0 || len(r.files) != 0 {
		t.Fatal("deleted file retained")
	}
}

func TestReaderFiltersCancellationMissingDirectories(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	r := NewReader()
	start := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		provider, model string
		start           time.Time
		want            int
	}{{"codex", "gpt-codex", start, 1}, {"claude", "", start, 0}, {"codex", "other", start, 0}, {"codex", "", start.Add(time.Second), 0}} {
		calls, err := r.Query(context.Background(), Roots{Codex: root}, c.start, start.Add(time.Hour), c.provider, c.model)
		if err != nil || len(calls) != c.want {
			t.Fatalf("%+v calls=%+v err=%v", c, calls, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Query(ctx, Roots{Codex: root}, start, start.Add(time.Hour), "", ""); err == nil {
		t.Fatal("cancellation ignored")
	}
	if calls := query(t, r, Roots{Claude: filepath.Join(root, "missing")}); len(calls) != 0 {
		t.Fatal("missing client should be empty")
	}
}

func TestCodexCounterResetDoesNotLoseRepeatedCheckpointValues(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1)+count("10:01:00", use2, use1)+count("10:02:00", use1, use1)+count("10:03:00", use2, use1))
	calls := query(t, NewReader(), Roots{Codex: root})
	if len(calls) != 4 || sum(calls).Total() != 480 {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestCodexCompactionUsageNotDoubleCountedWithNamedResponse(t *testing.T) {
	root := t.TempDir()
	compact := `{"timestamp":"2026-10-27T10:00:00Z","type":"compacted","payload":{"compaction_response_id":"compact","latest_token_usage_record":{"response_id":"compact","usage":` + use1 + `}}}` + "\n"
	writeLog(t, root, "sessions/a.jsonl", meta+compact+record("10:00:01", "compact", use1)+record("10:01:00", "next", use1)+count("10:01:01", use1, use1))
	calls := query(t, NewReader(), Roots{Codex: root})
	if len(calls) != 2 || sum(calls).Total() != 240 {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestClaudeOptionalRequestIDAdoptsEarlierRevision(t *testing.T) {
	p := newParser("claude", "synthetic")
	for _, line := range []string{
		`{"type":"assistant","timestamp":"2026-10-27T10:00:00Z","sessionId":"s","message":{"id":"m","model":"claude","usage":{"input_tokens":10,"output_tokens":1}}}`,
		`{"type":"assistant","timestamp":"2026-10-27T10:00:01Z","sessionId":"s","requestId":"r","message":{"id":"m","model":"claude","usage":{"input_tokens":10,"output_tokens":2}}}`,
		`{"type":"assistant","timestamp":"2026-10-27T10:00:02Z","sessionId":"s","requestId":null,"message":{"id":"m","model":"claude","usage":{"input_tokens":10,"output_tokens":2}}}`,
	} {
		p.read([]byte(line))
	}
	if len(p.calls) != 1 {
		t.Fatalf("calls=%+v", p.calls)
	}
	for _, call := range p.calls {
		if call.Total() != 12 {
			t.Fatalf("call=%+v", call)
		}
	}
}
