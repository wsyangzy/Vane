// Package sessionusage reads usage metadata from native client transcripts.
// Only usage metadata and bounded display titles are retained; transcripts are
// never logged or added to Vane's request ledger.
package sessionusage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Tokens struct {
	Input, Output, CacheRead, CacheWrite int64
	CacheWrite1h                         int64 // subset of CacheWrite
}

func (t Tokens) Total() int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

type Call struct {
	ID, Provider, Model string
	Session, Project    string
	Title               string
	At                  time.Time
	Tokens
}

// Activity contains timestamps, workspace metadata and a bounded display title.
type Activity struct {
	Session, Project, Provider, Model string
	Title                             string
	At                                time.Time
	Milliseconds                      int64
	Prompts                           int64
}

type Snapshot struct {
	Calls    []Call
	Activity []Activity
}

type Roots struct{ Claude, Codex string }

// NativeRoots honors the clients' directory overrides without opening auth files.
func NativeRoots() (Roots, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Roots{}, err
	}
	claude, codex := os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv("CODEX_HOME")
	if strings.TrimSpace(claude) == "" {
		claude = filepath.Join(home, ".claude")
	}
	if strings.TrimSpace(codex) == "" {
		codex = filepath.Join(home, ".codex")
	}
	return Roots{Claude: claude, Codex: codex}, nil
}

type fileState struct {
	info     fs.FileInfo
	offset   int64
	parser   *parser
	provider string
	restored bool // persisted metadata has no parser continuation state
	bytes    int64
	lastUsed uint64
	accessed time.Time
}

// Reader serializes refreshes and retains only a bounded set of live parsers.
// File manifests contain metadata; historical calls and activity live on disk.
type Reader struct {
	mu              sync.Mutex
	files           map[string]*fileState
	resident        map[string]*fileState
	indexPath       string
	index           map[string]indexedFile
	parserBytes     int64
	clock           uint64
	cacheEntries    int
	cacheTTL        time.Duration
	statsMu         sync.Mutex
	stats           readerStats
	enabled         bool
	titles          titleIndex
	titleAccessed   time.Time
	refreshManaged  bool
	refreshRoots    Roots
	refreshProvider string
	refreshAt       time.Time
	revision        uint64
}

func NewReader() *Reader {
	r := &Reader{
		files: make(map[string]*fileState), resident: make(map[string]*fileState),
		index: make(map[string]indexedFile), enabled: true,
		cacheEntries: defaultParserEntries, cacheTTL: defaultParserTTL,
	}
	r.stats.TTL, r.stats.MaxEntries = defaultParserTTL, defaultParserEntries
	r.stats.memory.BudgetBytes = defaultParserBytes
	return r
}

func (r *Reader) Query(ctx context.Context, roots Roots, start, end time.Time, provider, model string) ([]Call, error) {
	calls := make([]Call, 0)
	err := r.Visit(ctx, roots, start, end, provider, model, func(call Call) { calls = append(calls, call) }, nil)
	if err != nil {
		return nil, err
	}
	return calls, err
}

func (r *Reader) Read(ctx context.Context, roots Roots, start, end time.Time, provider, model string) (Snapshot, error) {
	result := Snapshot{Calls: []Call{}, Activity: []Activity{}}
	err := r.Visit(ctx, roots, start, end, provider, model,
		func(call Call) { result.Calls = append(result.Calls, call) },
		func(item Activity) { result.Activity = append(result.Activity, item) })
	if err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

// Visit lets aggregators consume metadata without retaining another complete
// snapshot. Callbacks run synchronously; nil callbacks skip the corresponding output.
func (r *Reader) Visit(ctx context.Context, roots Roots, start, end time.Time, provider, model string, call func(Call), activity func(Activity)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if provider != "" && provider != "claude" && provider != "codex" {
		return fmt.Errorf("unsupported local usage provider %q", provider)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.trimParsers()
	defer r.updateMemoryStats()
	defer func() {
		if !r.enabled {
			r.files, r.resident = make(map[string]*fileState), make(map[string]*fileState)
			r.parserBytes = 0
			r.titles = titleIndex{}
		}
	}()
	s := r.openStore(ctx)
	if s != nil {
		defer s.db.Close()
	}
	snapshot, names, err := r.refresh(ctx, roots, start, end, provider, model, s, activity != nil)
	var storageErr *indexWriteError
	if errors.As(err, &storageErr) && ctx.Err() == nil {
		r.noteStorageError(storageErr.err)
		s.db.Close()
		s = nil
		snapshot, names, err = r.refresh(ctx, roots, start, end, provider, model, nil, activity != nil)
	}
	if err != nil {
		return err
	}
	var emitCall func(Call)
	if call != nil {
		emitCall = func(item Call) {
			if item.Provider == "codex" && names[item.Session] != "" {
				item.Title = names[item.Session]
			}
			if model == "" || strings.EqualFold(model, item.Model) {
				call(item)
			}
		}
	}
	var emitActivity func(Activity)
	if activity != nil {
		emitActivity = func(item Activity) {
			if item.Provider == "codex" && names[item.Session] != "" {
				item.Title = names[item.Session]
			}
			if model == "" || strings.EqualFold(model, item.Model) {
				activity(item)
			}
		}
	}
	if s != nil {
		return s.visit(ctx, start, end, provider, model, emitCall, emitActivity)
	}
	if emitCall != nil {
		for _, item := range snapshot.Calls {
			emitCall(item)
		}
	}
	if emitActivity != nil {
		for _, item := range snapshot.Activity {
			emitActivity(item)
		}
	}
	return nil
}

type indexWriteError struct{ err error }

func (e *indexWriteError) Error() string { return e.err.Error() }
func (e *indexWriteError) Unwrap() error { return e.err }

type readJob struct {
	path    string
	info    fs.FileInfo
	state   *fileState
	replace bool
}

func unchangedFile(state *fileState, info fs.FileInfo) bool {
	return state != nil && state.info != nil && os.SameFile(state.info, info) &&
		state.info.Size() == info.Size() && state.info.ModTime().Equal(info.ModTime())
}

func (r *Reader) refresh(ctx context.Context, roots Roots, start, end time.Time, provider, model string, s *indexStore, withActivity bool) (Snapshot, map[string]string, error) {
	if s != nil && r.refreshManaged && roots == r.refreshRoots && provider == r.refreshProvider && time.Since(r.refreshAt) < 2*time.Second {
		return Snapshot{}, r.titles.names, nil
	}
	oldTitles := r.titles.info
	changed := roots != r.refreshRoots
	seen, seenKeys := make(map[string]bool), make(map[string]bool)
	var jobs []readJob
	var unchanged []string
	for _, source := range []struct{ provider, path, root string }{
		{"claude", filepath.Join(roots.Claude, "projects"), roots.Claude},
		{"codex", filepath.Join(roots.Codex, "sessions"), roots.Codex},
		{"codex", filepath.Join(roots.Codex, "archived_sessions"), roots.Codex},
	} {
		if source.root == "" || provider != "" && provider != source.provider {
			continue
		}
		root, err := filepath.EvalSymlinks(source.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Snapshot{}, nil, fmt.Errorf("读取 %s 会话目录失败: %w", source.provider, err)
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".jsonl") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			key := indexKey(source.provider, path)
			seenKeys[key] = true
			cached, indexed := r.index[key]
			cachedFile := s != nil && indexed && cached.Size == info.Size() && cached.Modified.Equal(info.ModTime())
			state := r.files[path]
			if state == nil && cachedFile {
				state = &fileState{provider: source.provider, info: info, restored: true}
				r.files[path] = state
			}
			if unchangedFile(state, info) && (cachedFile || s == nil && state.parser != nil) {
				r.noteFile(true)
				if s == nil {
					unchanged = append(unchanged, path)
				}
				return nil
			}
			replace := false
			if state == nil || state.parser == nil || state.provider != source.provider ||
				state.info != nil && (!os.SameFile(state.info, info) || info.Size() < state.offset ||
					info.Size() == state.info.Size() && !info.ModTime().Equal(state.info.ModTime())) {
				state = &fileState{provider: source.provider, parser: newParser(source.provider, path)}
				r.files[path] = state
				replace = true
			}
			if !state.parser.trackChanges || s != nil && !indexed {
				replace = true
			}
			r.forgetParser(path) // workers own these parsers until completion
			r.noteFile(false)
			jobs = append(jobs, readJob{path: path, info: info, state: state, replace: replace})
			return nil
		})
		if err != nil {
			return Snapshot{}, nil, fmt.Errorf("读取 %s 本地用量失败: %w", source.provider, err)
		}
	}
	builder := newSnapshotBuilder(start, end, model, withActivity)
	// Consume cached files before parsing can evict them from the resident LRU.
	for _, path := range unchanged {
		state := r.files[path]
		builder.addFile(state.parser)
		r.clock++
		state.lastUsed, state.accessed = r.clock, time.Now()
	}
	if err := r.parseJobs(ctx, jobs, s, func(path string, state *fileState) {
		if s == nil {
			builder.addFile(state.parser)
		}
	}); err != nil {
		return Snapshot{}, nil, err
	}
	for path, state := range r.files {
		if provider != "" && state.provider != provider {
			continue
		}
		if !seen[path] {
			changed = true
			r.forgetParser(path)
			delete(r.files, path)
			continue
		}
	}
	if s != nil {
		before := len(r.index)
		if err := s.prune(ctx, r.index, seenKeys, provider); err != nil {
			return Snapshot{}, nil, &indexWriteError{err}
		}
		changed = changed || len(r.index) != before
	}
	var names map[string]string
	if provider == "" || provider == "codex" {
		names = r.codexTitles(ctx, roots.Codex)
		r.titleAccessed = time.Now()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, nil, err
	}
	if changed || len(jobs) > 0 || !sameTitleFile(oldTitles, r.titles.info) {
		r.revision++
	}
	r.refreshRoots, r.refreshProvider, r.refreshAt = roots, provider, time.Now()
	return builder.Snapshot, names, nil
}

func sameTitleFile(a, b fs.FileInfo) bool {
	return a == nil && b == nil || a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// PrepareVersion refreshes persisted metadata before a dashboard cache lookup.
// A short window coalesces scans across pages; manual refresh bypasses it.
// In-memory/fallback readers never reuse a prepared version.
func (r *Reader) PrepareVersion(ctx context.Context, roots Roots, provider string) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.trimParsers()
	defer r.updateMemoryStats()
	s := r.openStore(ctx)
	if s == nil {
		r.revision++
		return r.revision, nil
	}
	defer s.db.Close()
	r.refreshManaged = true
	_, _, err := r.refresh(ctx, roots, time.Time{}, time.Time{}, provider, "", s, false)
	var storageErr *indexWriteError
	if errors.As(err, &storageErr) && ctx.Err() == nil {
		// Visit owns the established fallback path. Never let the preparation
		// optimization make a recoverable persistence failure break statistics.
		r.noteStorageError(storageErr.err)
		return r.revision, nil
	}
	return r.revision, err
}

func (r *Reader) InvalidateRefresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshAt = time.Time{}
	r.revision++
}

// Persist or consume each completed file immediately. The worker bound also
// bounds cold-index parser memory; completed archives do not accumulate in jobs.
func (r *Reader) parseJobs(ctx context.Context, jobs []readJob, s *indexStore, consume func(string, *fileState)) error {
	count := min(4, runtime.GOMAXPROCS(0), len(jobs))
	if count == 0 {
		return nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan readJob)
	type completion struct {
		job readJob
		err error
	}
	done := make(chan completion, count)
	var workers sync.WaitGroup
	for i := 0; i < count; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range queue {
				done <- completion{item, readAppend(workCtx, item.path, item.state)}
			}
		}()
	}
	go func() {
		defer close(queue)
		for _, item := range jobs {
			select {
			case queue <- item:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(done) }()
	defer func() {
		// Failed/cancelled jobs have no completed cache entry. Drop partial
		// parser state after every worker exits; the next read reparses safely.
		for _, item := range jobs {
			if r.resident[item.path] != item.state {
				item.state.parser, item.state.restored = nil, true
			}
		}
	}()
	var firstErr error
	for item := range done {
		if firstErr != nil {
			continue
		}
		if item.err != nil {
			firstErr = fmt.Errorf("读取本地用量失败: %w", item.err)
			cancel()
			continue
		}
		state := item.job.state
		state.info = item.job.info
		if s != nil {
			key := indexKey(state.provider, item.job.path)
			file := indexedFile{Provider: state.provider, Title: state.parser.title, Size: state.info.Size(), Modified: state.info.ModTime()}
			if err := s.saveFile(ctx, key, file, state.parser, item.job.replace); err != nil {
				firstErr = &indexWriteError{err}
				cancel()
				continue
			}
			state.parser.markStored()
			r.index[key] = file
			r.statsMu.Lock()
			r.stats.memory.FileWrites++
			r.stats.LastUpdatedAt = time.Now()
			r.statsMu.Unlock()
		}
		consume(item.job.path, state)
		r.retainParser(item.job.path, state)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return firstErr
}

type snapshotBuilder struct {
	Snapshot
	start, end   time.Time
	model        string
	withActivity bool
	calls        map[string]int
	activity     map[string]int
}

func newSnapshotBuilder(start, end time.Time, model string, withActivity bool) *snapshotBuilder {
	return &snapshotBuilder{
		Snapshot: Snapshot{Calls: []Call{}, Activity: []Activity{}},
		start:    start, end: end, model: model, withActivity: withActivity,
		calls: make(map[string]int), activity: make(map[string]int),
	}
}

func (b *snapshotBuilder) addFile(p *parser) {
	for _, item := range p.calls {
		if item.At.Before(b.start) || !item.At.Before(b.end) || b.model != "" && !strings.EqualFold(b.model, item.Model) {
			continue
		}
		item.Title = p.title
		if i, found := b.calls[item.ID]; found {
			if item.At.After(b.Calls[i].At) {
				b.Calls[i] = item
			}
		} else {
			b.calls[item.ID] = len(b.Calls)
			b.Calls = append(b.Calls, item)
		}
	}
	if !b.withActivity {
		return
	}
	for key, item := range p.activity {
		if item.At.Before(b.start) || !item.At.Before(b.end) || b.model != "" && !strings.EqualFold(b.model, item.Model) {
			continue
		}
		item.Title = p.title
		if i, found := b.activity[key]; found {
			item.Milliseconds = max(b.Activity[i].Milliseconds, item.Milliseconds)
			item.Prompts = max(b.Activity[i].Prompts, item.Prompts)
			b.Activity[i] = item
		} else {
			b.activity[key] = len(b.Activity)
			b.Activity = append(b.Activity, item)
		}
	}
}

func readAppend(ctx context.Context, path string, state *fileState) error {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) { // A client may archive a live file during discovery.
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(state.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(file, 128*1024)
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fragment, err := reader.ReadSlice('\n')
		if len(line) == 0 && err == nil {
			state.parser.read(fragment)
			state.offset += int64(len(fragment))
			continue
		}
		if len(line)+len(fragment) > 32*1024*1024 {
			return fmt.Errorf("会话包含超过 32 MiB 的单行，未将不完整结果计入统计")
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			// A partial final line is retried after the client completes it.
			return nil
		}
		if err != nil {
			return err
		}
		state.parser.read(line)
		state.offset += int64(len(line))
		if cap(line) > 256*1024 {
			line = nil
		} else {
			line = line[:0]
		}
	}
}
