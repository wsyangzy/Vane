package sessionusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

func TestSessionTitlesPreferClaudeRenameAndSkipInstructionMessages(t *testing.T) {
	p := newParser("claude", "fixture")
	for _, line := range []string{
		`{"type":"user","isMeta":true,"message":{"content":"system instruction"}}`,
		`{"type":"user","message":{"content":"# AGENTS.md instructions"}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"not a prompt"},{"type":"text","text":"修复首次加载问题"}]}}`,
	} {
		p.read([]byte(line))
	}
	if p.title != "修复首次加载问题" {
		t.Fatalf("first title=%q", p.title)
	}
	p.read([]byte(`{"type":"summary","summary":"总结加载优化"}`))
	p.read([]byte(`{"type":"ai-title","aiTitle":"概览加载优化"}`))
	p.read([]byte(`{"type":"custom-title","customTitle":"我的性能排查"}`))
	p.read([]byte(`{"type":"ai-title","aiTitle":"后台自动改名"}`))
	if p.title != "我的性能排查" {
		t.Fatalf("rename priority=%q", p.title)
	}
	q := newParser("codex", "fixture")
	for _, line := range []string{
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"<environment_context>injected</environment_context>"}]}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_image","image_url":"not a title"},{"type":"input_text","text":"实现项目分组明细"}]}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"后续问题"}}`,
	} {
		q.read([]byte(line))
	}
	if q.title != "实现项目分组明细" {
		t.Fatalf("Codex title=%q", q.title)
	}
}

func TestTitleMetadataIsBoundedAndRedactsSecretsBeforePersistence(t *testing.T) {
	secret := "sk-proj-" + strings.Repeat("abc123", 12)
	prompt := "修复接口 key=" + secret + " " + strings.Repeat("正常标题 ", 80) + "body-not-retained"
	encoded, _ := json.Marshal(map[string]any{"type": "event_msg", "payload": map[string]string{"type": "user_message", "message": prompt}})
	p := newParser("codex", "fixture")
	p.read(encoded)
	if p.title == "" || strings.Contains(p.title, secret) || strings.Contains(p.title, "sk-proj-") || strings.Contains(p.title, "body-not-retained") || len([]rune(p.title)) > titleLimit {
		t.Fatalf("unsafe title=%q", p.title)
	}
	if got := displayTitle("  标题\n测试\t\u202E消息  "); got != "标题 测试 消息" {
		t.Fatalf("normalized=%q", got)
	}
	for _, kind := range []string{"PRIVATE KEY", "RSA PRIVATE KEY", "PGP PRIVATE KEY BLOCK"} {
		value := "排查密钥 -----BEGIN " + kind + "-----\n" + strings.Repeat("secretMaterial", 500) + "\n-----END " + kind + "-----"
		if got := displayTitle(value); got != "排查密钥 [私钥]" {
			t.Fatalf("partial %s retained: %q", kind, got)
		}
	}
	if got := displayTitle(strings.Repeat(" ", 4095) + "中"); got != "" {
		t.Fatalf("truncated UTF-8 retained: %q", got)
	}
}

func TestCodexRenameIndexRefreshesWithoutReparsingSessions(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	file := writeLog(t, root, "session_index.jsonl", `{"id":"codex-thread","thread_name":"实施统一路由"}`+"\n"+`{"id":"codex-thread","thread_name":"未完成`)
	r := NewReader()
	if calls := query(t, r, Roots{Codex: root}); calls[0].Title != "实施统一路由" {
		t.Fatalf("initial=%+v", calls)
	}
	states := make(map[string]*fileState)
	for path, state := range r.files {
		states[path] = state
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("标题\"}\n")
	f.Close()
	if calls := query(t, r, Roots{Codex: root}); calls[0].Title != "未完成标题" {
		t.Fatalf("appended=%+v", calls)
	}
	for path, state := range states {
		if r.files[path] != state {
			t.Fatal("a rename reparsed the transcript")
		}
	}
	writeLog(t, root, "session_index.jsonl", `{"id":"codex-thread","thread_name":"更换名称"}`+"\n")
	when := time.Now().Add(time.Second)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
	if calls := query(t, r, Roots{Codex: root}); calls[0].Title != "更换名称" {
		t.Fatalf("rewritten=%+v", calls)
	}
	if r.Stats().Size != 2 {
		t.Fatalf("title index not included in managed status: %+v", r.Stats())
	}
	r.Invalidate(appcache.InvalidateOptions{All: true})
	if r.titles.names != nil || r.Stats().Size != 0 {
		t.Fatal("title index survived clear")
	}
	if err := os.Remove(filepath.Join(root, "session_index.jsonl")); err != nil {
		t.Fatal(err)
	}
	if calls := query(t, r, Roots{Codex: root}); calls[0].Title != "" {
		t.Fatal("deleted rename retained")
	}
}
