package sessionusage

import (
	"bytes"
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

func TestPreparedVersionCoalescesScansAndRefreshesChangedFiles(t *testing.T) {
	root := t.TempDir()
	file := writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	r := NewPersistentReader(filepath.Join(t.TempDir(), "prepared.sqlite"))
	roots := Roots{Codex: root}
	first, err := r.PrepareVersion(context.Background(), roots, "codex")
	if err != nil {
		t.Fatal(err)
	}
	before := r.Stats()
	second, err := r.PrepareVersion(context.Background(), roots, "codex")
	if err != nil || first != second || r.Stats().Hits != before.Hits {
		t.Fatal("prepared query repeated filesystem scan")
	}
	if calls := query(t, r, roots); len(calls) != 1 {
		t.Fatal("prepared index omitted calls")
	}
	writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1)+count("10:01:00", use2, use1))
	r.InvalidateRefresh()
	third, err := r.PrepareVersion(context.Background(), roots, "codex")
	if err != nil || third <= second {
		t.Fatal("manual refresh did not advance revision")
	}
	if calls := query(t, r, roots); len(calls) != 2 {
		t.Fatal("refresh omitted appended usage")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	r.refreshAt = time.Now().Add(-3 * time.Second)
	fourth, err := r.PrepareVersion(context.Background(), roots, "codex")
	if err != nil || fourth <= third {
		t.Fatal("expired scan did not see deleted file")
	}
	if calls := query(t, r, roots); len(calls) != 0 {
		t.Fatal("deleted calls stayed in index")
	}
}

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
	if stats := r.Stats(); stats.Hits != 1 || stats.Misses != 0 || stats.Size != 0 || stats.LastUpdatedAt.IsZero() {
		t.Fatalf("stats=%+v", stats)
	}
	for _, state := range r.files {
		if !state.restored {
			t.Fatal("unchanged transcript was parsed again")
		}
	}
	data, err := os.ReadFile(indexStorePath(path))
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
	cache, err := openIndexStore(context.Background(), indexStorePath(path))
	if err != nil {
		t.Fatal(err)
	}
	_, err = cache.db.Exec("PRAGMA user_version = 2")
	cache.db.Close()
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

func TestLegacyIndexImportsOnceAndPreservesRollbackFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	file := writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	p := newParser("codex", file)
	for _, line := range strings.Split(strings.TrimSpace(meta+count("10:00:00", use1, use1)), "\n") {
		p.read([]byte(line))
	}
	legacy := diskIndex{Version: indexVersion, Files: map[string]indexedFile{
		indexKey("codex", file): {Provider: "codex", Size: info.Size(), Modified: info.ModTime(), Calls: p.calls, Activity: p.activity},
	}}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = gob.NewEncoder(f).Encode(legacy)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := NewPersistentReader(path)
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 1 || sum(calls).Total() != 120 || calls[0].Session != "codex-thread" || calls[0].Model != "gpt-codex" {
		t.Fatalf("legacy calls=%+v", calls)
	}
	if r.Stats().Misses != 0 || r.MemoryStats().LegacyImports != 1 || r.MemoryStats().ResidentParsers != 0 {
		t.Fatal("legacy index was reparsed or pinned in memory")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("legacy rollback file changed")
	}
	restarted := NewPersistentReader(path)
	query(t, restarted, Roots{Codex: root})
	if restarted.MemoryStats().LegacyImports != 0 || restarted.Stats().Misses != 0 {
		t.Fatal("legacy index was imported again")
	}
}

func TestParserEvictionKeepsCompleteQueriesAndRestartedHistory(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			root := t.TempDir()
			for i := 0; i < 12; i++ {
				writeLog(t, root, fmt.Sprintf("sessions/%02d.jsonl", i), meta+record("10:00:00", fmt.Sprintf("r%d", i), use1))
			}
			r := NewReader()
			if persistent {
				r = NewPersistentReader(filepath.Join(t.TempDir(), "index.gob"))
			}
			r.SetOptions(appcache.Options{Enabled: true, MaxEntries: 2})
			for i := 0; i < 3; i++ {
				if calls := query(t, r, Roots{Codex: root}); len(calls) != 12 || sum(calls).Total() != 1440 {
					t.Fatalf("query %d lost evicted history: %d calls", i, len(calls))
				}
				if memory := r.MemoryStats(); memory.ResidentParsers > 2 || memory.ParserBytes+memory.TitleBytes > memory.BudgetBytes {
					t.Fatalf("unbounded cache: %+v", memory)
				}
			}
			if persistent {
				if calls := query(t, NewPersistentReader(r.indexPath), Roots{Codex: root}); len(calls) != 12 {
					t.Fatal("restart lost evicted history")
				}
			}
		})
	}
}

func TestIndexLiveAppendWritesOnlyChangedCallIDs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	file := writeLog(t, root, "sessions/a.jsonl", meta+record("10:00:00", "one", use1)+record("10:01:00", "two", use1))
	r := NewPersistentReader(path)
	query(t, r, Roots{Codex: root})
	cache, err := openIndexStore(context.Background(), indexStorePath(path))
	if err != nil {
		t.Fatal(err)
	}
	_, err = cache.db.Exec(`CREATE TABLE write_probe(id TEXT); CREATE TRIGGER track_call_insert AFTER INSERT ON calls BEGIN INSERT INTO write_probe VALUES (new.id); END`)
	cache.db.Close()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(record("10:02:00", "three", use1))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 3 {
		t.Fatalf("appended calls=%+v", calls)
	}
	cache, err = openIndexStore(context.Background(), indexStorePath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer cache.db.Close()
	var writes int
	if err := cache.db.QueryRow("SELECT COUNT(*) FROM write_probe").Scan(&writes); err != nil || writes != 1 {
		t.Fatalf("append rewrote %d call rows: %v", writes, err)
	}
}

func TestParserTTLReleasesMemoryWithoutDeletingIndexedHistory(t *testing.T) {
	root := t.TempDir()
	r := NewPersistentReader(filepath.Join(t.TempDir(), "index.gob"))
	writeLog(t, root, "sessions/a.jsonl", meta+record("10:00:00", "one", use1))
	query(t, r, Roots{Codex: root})
	for _, state := range r.resident {
		state.accessed = time.Now().Add(-defaultParserTTL - time.Second)
	}
	if memory := r.MemoryStats(); memory.ResidentParsers != 0 || memory.ParserBytes != 0 || memory.IndexedFiles != 1 {
		t.Fatalf("idle cache=%+v", memory)
	}
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 1 {
		t.Fatal("TTL removed historical data")
	}
}

func TestCorruptSQLiteIndexRebuildsWithoutLosingSourceHistory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	writeLog(t, root, "sessions/a.jsonl", meta+record("10:00:00", "one", use1))
	query(t, NewPersistentReader(path), Roots{Codex: root})
	if err := os.WriteFile(indexStorePath(path), []byte("truncated-cache"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewPersistentReader(path)
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 1 || sum(calls).Total() != 120 {
		t.Fatal("corrupt SQLite cache hid source history")
	}
	if r.MemoryStats().LastStorageError != "" {
		t.Fatal("corrupt cache failed to recover")
	}
	restarted := NewPersistentReader(path)
	query(t, restarted, Roots{Codex: root})
	if restarted.Stats().Misses != 0 {
		t.Fatal("rebuilt cache was not reusable")
	}
}

func TestOversizedParserIsEvictedAndItsFullHistoryStaysQueryable(t *testing.T) {
	root := t.TempDir()
	var data strings.Builder
	data.WriteString(meta)
	for i := 0; i < 500; i++ {
		data.WriteString(record("10:00:00", fmt.Sprintf("r%d-%s", i, strings.Repeat("x", 12000)), use1))
	}
	writeLog(t, root, "sessions/a.jsonl", data.String())
	r := NewPersistentReader(filepath.Join(t.TempDir(), "index.gob"))
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 500 {
		t.Fatal("oversized parser truncated history")
	}
	if memory := r.MemoryStats(); memory.ResidentParsers != 0 || memory.ParserBytes != 0 {
		t.Fatalf("oversized parser retained: %+v", memory)
	}
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 500 {
		t.Fatal("evicted parser could not be read from disk")
	}
}

func TestPersistentModelFilterUsesEqualFoldBeforeDuplicateSelection(t *testing.T) {
	root := t.TempDir()
	oldModel := strings.ReplaceAll(meta, "gpt-codex", "K-Σ-model")
	newModel := strings.ReplaceAll(meta, "gpt-codex", "other-model")
	writeLog(t, root, "sessions/a.jsonl", oldModel+record("10:00:00", "same", use1))
	writeLog(t, root, "archived_sessions/a.jsonl", newModel+record("10:01:00", "same", use2))
	r := NewPersistentReader(filepath.Join(t.TempDir(), "index.gob"))
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calls, err := r.Query(context.Background(), Roots{Codex: root}, start, start.AddDate(1, 0, 0), "codex", "k-σ-model")
	if err != nil || len(calls) != 1 || calls[0].Model != "K-Σ-model" {
		t.Fatalf("model filter lost the matching copy: %d calls, %v", len(calls), err)
	}
}

func TestPersistentAppendRemovesReplacedAnonymousUsage(t *testing.T) {
	root := t.TempDir()
	file := writeLog(t, root, "sessions/a.jsonl", meta+count("10:00:00", use1, use1))
	r := NewPersistentReader(filepath.Join(t.TempDir(), "index.gob"))
	query(t, r, Roots{Codex: root})
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(record("10:00:01", "complete", use1))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 1 || !strings.HasSuffix(calls[0].ID, "/response/complete") || sum(calls).Total() != 120 {
		t.Fatal("stored anonymous usage was not replaced by the completed response")
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

func TestPersistentActivityOnlyVisitSkipsCalls(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "index.gob")
	writeLog(t, root, "sessions/a.jsonl", meta+`{"timestamp":"2026-10-27T09:59:00Z","type":"event_msg","payload":{"type":"user_message","message":"查看活动统计"}}`+"\n"+record("10:00:00", "one", use1))
	query(t, NewPersistentReader(path), Roots{Codex: root})
	store, err := openIndexStore(context.Background(), indexStorePath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	if _, err := store.db.Exec("DROP TABLE calls"); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var count int
	err = store.visit(context.Background(), start, start.AddDate(1, 0, 0), "codex", "", nil, func(item Activity) {
		count++
		if item.Title != "查看活动统计" {
			t.Fatalf("activity title=%q", item.Title)
		}
	})
	if err != nil || count == 0 {
		t.Fatalf("activity-only visit queried calls or lost activity: count=%d, err=%v", count, err)
	}
}

func TestReaderDoesNotParseAliasedArchiveRootConcurrently(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "sessions/a.jsonl", meta+record("10:00:00", "one", use1))
	if err := os.Symlink(filepath.Join(root, "sessions"), filepath.Join(root, "archived_sessions")); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	r := NewReader()
	if calls := query(t, r, Roots{Codex: root}); len(calls) != 1 || sum(calls).Total() != 120 || r.Stats().Misses != 1 {
		t.Fatal("the same resolved transcript was scheduled more than once")
	}
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
