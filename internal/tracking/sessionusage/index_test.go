package sessionusage

import (
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

func TestPersistentIndexReusesMetadataAfterRestartWithoutRetainingBodies(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	writeLog(t, root, "sessions/a.jsonl", meta+`{"timestamp":"2026-10-27T09:59:00Z","type":"event_msg","payload":{"type":"user_message","message":"修复会话列表 `+strings.Repeat("示例", 120)+` private-transcript-payload"}}`+"\n"+count("10:00:00", use1, use1))
	first := query(t, NewPersistentReader(path), Roots{Codex: root})
	r := NewPersistentReader(path)
	second := query(t, r, Roots{Codex: root})
	if len(second) != len(first) || sum(second) != sum(first) {
		t.Fatalf("restored=%+v first=%+v", second, first)
	}
	if !strings.HasPrefix(second[0].Title, "修复会话列表") || len([]rune(second[0].Title)) > titleLimit {
		t.Fatal("bounded title did not survive restart")
	}
	if stats := r.Stats(); stats.Hits != 1 || stats.Misses != 0 || stats.Size != 1 || stats.LastUpdatedAt.IsZero() {
		t.Fatalf("stats=%+v", stats)
	}
	for _, state := range r.files {
		if !state.restored {
			t.Fatal("unchanged transcript was parsed again")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-transcript-payload") {
		t.Fatal("persisted message content")
	}
}

func TestPersistentIndexRebuildsChangedFileAndThenResumesIncrementalParsing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	file := writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	query(t, NewPersistentReader(path), Roots{Codex: root})
	r := NewPersistentReader(path)
	query(t, r, Roots{Codex: root})
	appendLine := func(value string) {
		t.Helper()
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(value)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	appendLine(count("10:01:00", use2, use1))
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 2 || sum(calls).Total() != 240 {
		t.Fatalf("append=%+v", calls)
	}
	state := r.files[file]
	appendLine(count("10:02:00", `{"input_tokens":300,"output_tokens":60,"cached_input_tokens":210}`, use1))
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 3 || sum(calls).Total() != 360 {
		t.Fatalf("incremental=%+v", calls)
	}
	if r.files[file] != state {
		t.Fatal("live parser unnecessarily rebuilt")
	}
	// Same-size replacement with a changed modification time must invalidate.
	data := strings.ReplaceAll(meta+count("10:00:00", use1, use1), "100", "900")
	writeLog(t, root, "sessions/a.jsonl", data)
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatal(err)
	}
	if calls := query(t, NewPersistentReader(path), Roots{Codex: root}); len(calls) != 1 || sum(calls).Input != 830 {
		t.Fatalf("replacement=%+v", calls)
	}
}

func TestPersistentIndexCorruptionVersionMismatchAndDeletedFilesRebuild(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	file := writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	if err := os.WriteFile(path, []byte("interrupted-cache"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewPersistentReader(path)
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 1 || r.Stats().Misses != 1 {
		t.Fatal("corrupt index did not rebuild")
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = gob.NewEncoder(f).Encode(diskIndex{Version: indexVersion + 1, Files: r.index})
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	r = NewPersistentReader(path)
	query(t, r, Roots{Codex: root})
	if r.Stats().Misses != 1 {
		t.Fatal("incompatible parser version reused")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	r = NewPersistentReader(path)
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 0 || r.Stats().Size != 0 {
		t.Fatal("deleted file retained")
	}
}

func TestPersistentIndexClearAndDisableAffectDiskAndMemory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	query(t, NewPersistentReader(path), Roots{Codex: root})
	r := NewPersistentReader(path)
	if removed := r.Invalidate(appcache.InvalidateOptions{All: true}); removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("index still present: %v", err)
	}
	query(t, r, Roots{Codex: root})
	if r.Stats().Misses != 1 {
		t.Fatal("clear failed to force parsing")
	}
	r.SetOptions(appcache.Options{Enabled: false})
	query(t, r, Roots{Codex: root})
	query(t, r, Roots{Codex: root})
	if s := r.Stats(); s.Enabled || s.Size != 0 || s.Misses != 3 {
		t.Fatalf("disabled stats=%+v", s)
	}
	if len(r.files) != 0 {
		t.Fatal("disabled reader retained metadata")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("disabled reader wrote index")
	}
	r.SetOptions(appcache.Options{Enabled: true})
	query(t, r, Roots{Codex: root})
	query(t, r, Roots{Codex: root})
	if s := r.Stats(); !s.Enabled || s.Hits != 1 || s.Size != 1 {
		t.Fatalf("reenabled=%+v", s)
	}
}

func TestPersistentIndexProviderFiltersAndStatusDuringRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	writeLog(t, root, "projects/p/a.jsonl", `{"type":"assistant","timestamp":"2026-10-27T10:00:00Z","sessionId":"s","message":{"id":"m","model":"claude","usage":{"input_tokens":10}}}`+"\n")
	roots := Roots{Codex: root, Claude: root}
	query(t, NewPersistentReader(path), roots)
	r := NewPersistentReader(path)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if calls, err := r.Query(context.Background(), roots, start, start.AddDate(1, 0, 0), "codex", ""); err != nil || len(calls) != 1 {
		t.Fatalf("provider filter: %v %+v", err, calls)
	}
	if calls := query(t, NewPersistentReader(path), roots); len(calls) != 2 {
		t.Fatal("one provider refresh lost another provider's persisted metadata")
	}
	r.mu.Lock()
	done := make(chan struct{})
	go func() { r.Stats(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		r.mu.Unlock()
		t.Fatal("status waits for transcript indexing")
	}
	r.mu.Unlock()
}

// A reproducible local fixture, not a claim about a user's machine or storage.
func BenchmarkLocalUsageIndex(b *testing.B) {
	root, cacheDir := b.TempDir(), b.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		b.Fatal(err)
	}
	var fixture strings.Builder
	fixture.WriteString(meta)
	for i := 0; i < 96; i++ {
		fixture.WriteString(`{"timestamp":"2026-10-27T10:00:00Z","type":"response_item","payload":{"role":"assistant","content":"` + strings.Repeat("synthetic-data-", 2048) + `"}}` + "\n")
		fixture.WriteString(record(fmt.Sprintf("10:%02d:00", i%60), fmt.Sprintf("r%d", i), use1))
	}
	for i := 0; i < 16; i++ {
		if err := os.WriteFile(filepath.Join(root, "sessions", fmt.Sprintf("%02d.jsonl", i)), []byte(fixture.String()), 0600); err != nil {
			b.Fatal(err)
		}
	}
	path := filepath.Join(cacheDir, "index.gob")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	read := func(reader *Reader) {
		if _, err := reader.Read(context.Background(), Roots{Codex: root}, start, start.AddDate(1, 0, 0), "", ""); err != nil {
			b.Fatal(err)
		}
	}
	read(NewPersistentReader(path))
	b.Run("cold_serial", func(b *testing.B) {
		previous := runtime.GOMAXPROCS(1)
		defer runtime.GOMAXPROCS(previous)
		for i := 0; i < b.N; i++ {
			read(NewReader())
		}
	})
	b.Run("cold", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			read(NewReader())
		}
	})
	b.Run("restart_with_index", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			read(NewPersistentReader(path))
		}
	})
}
