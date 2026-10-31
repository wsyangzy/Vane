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
	restored bool // persisted metadata has no parser continuation state
}

// Reader serializes refreshes, keeps append offsets and prunes removed files.
// Only metadata is cached; no goroutines or file handles survive a query.
type Reader struct {
	mu          sync.Mutex
	files       map[string]*fileState
	indexPath   string
	indexLoaded bool
	index       map[string]indexedFile
	statsMu     sync.Mutex
	stats       readerStats
	enabled     bool
	titles      titleIndex
}

func NewReader() *Reader { return &Reader{files: make(map[string]*fileState), enabled: true} }

func (r *Reader) Query(ctx context.Context, roots Roots, start, end time.Time, provider, model string) ([]Call, error) {
	result, err := r.Read(ctx, roots, start, end, provider, model)
	return result.Calls, err
}

func (r *Reader) Read(ctx context.Context, roots Roots, start, end time.Time, provider, model string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if provider != "" && provider != "claude" && provider != "codex" {
		return Snapshot{}, fmt.Errorf("unsupported local usage provider %q", provider)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	r.loadIndex()
	defer func() {
		if !r.enabled {
			r.files = make(map[string]*fileState)
			r.titles = titleIndex{}
		}
	}()
	seen := make(map[string]bool)
	calls := make(map[string]Call)
	activity := make(map[string]Activity)
	type job struct {
		path  string
		info  fs.FileInfo
		state *fileState
	}
	var jobs []job
	dirty := false
	for _, source := range []struct{ provider, path string }{
		{"claude", filepath.Join(roots.Claude, "projects")},
		{"codex", filepath.Join(roots.Codex, "sessions")},
		{"codex", filepath.Join(roots.Codex, "archived_sessions")},
	} {
		if provider != "" && provider != source.provider {
			continue
		}
		// Empty injected roots disable a source (useful for isolated tests).
		if source.provider == "claude" && roots.Claude == "" || source.provider == "codex" && roots.Codex == "" {
			continue
		}
		root, err := filepath.EvalSymlinks(source.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Snapshot{}, fmt.Errorf("读取 %s 会话目录失败: %w", source.provider, err)
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
			seen[path] = true
			info, err := entry.Info()
			if err != nil {
				return err
			}
			state := r.files[path]
			if state == nil && r.enabled {
				if cached, ok := r.index[indexKey(source.provider, path)]; ok && cached.Size == info.Size() && cached.Modified.Equal(info.ModTime()) {
					state = &fileState{info: info, parser: newParser(source.provider, path), restored: true}
					state.parser.calls, state.parser.activity = cached.Calls, cached.Activity
					state.parser.title = cached.Title
					r.files[path] = state
				}
			}
			unchanged := state != nil && state.info != nil && os.SameFile(state.info, info) && state.info.Size() == info.Size() && state.info.ModTime().Equal(info.ModTime())
			if unchanged {
				r.noteFile(true)
				return nil
			}
			if state == nil || state.restored || state.info != nil && (!os.SameFile(state.info, info) || info.Size() < state.offset || info.Size() == state.info.Size() && !info.ModTime().Equal(state.info.ModTime())) {
				state = &fileState{parser: newParser(source.provider, path)}
				r.files[path] = state
			}
			r.noteFile(false)
			jobs = append(jobs, job{path, info, state})
			return nil
		})
		if err != nil {
			return Snapshot{}, fmt.Errorf("读取 %s 本地用量失败: %w", source.provider, err)
		}
	}
	// Each parser belongs to one file. Bound concurrent IO/decoding so the first
	// index can use multiple cores without opening the whole archive at once.
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan job)
	var workers sync.WaitGroup
	var once sync.Once
	var readErr error
	for i := 0; i < min(4, runtime.GOMAXPROCS(0), len(jobs)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range queue {
				if err := readAppend(workCtx, item.path, item.state); err != nil {
					once.Do(func() { readErr = err; cancel() })
					continue
				}
				item.state.info = item.info
			}
		}()
	}
	for _, item := range jobs {
		if workCtx.Err() != nil {
			break
		}
		queue <- item
	}
	close(queue)
	workers.Wait()
	if readErr != nil {
		return Snapshot{}, fmt.Errorf("读取本地用量失败: %w", readErr)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if len(jobs) > 0 {
		dirty = true
	}
	var names map[string]string
	if provider == "" || provider == "codex" {
		names = r.codexTitles(ctx, roots.Codex)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	for path, state := range r.files {
		if provider != "" && state.parser.provider != provider {
			continue
		}
		if !seen[path] {
			delete(r.files, path)
			dirty = true
			continue
		}
		for _, call := range state.parser.calls {
			call.Title = state.parser.title
			if call.Provider == "codex" && names[call.Session] != "" {
				call.Title = names[call.Session]
			}
			if !call.At.Before(start) && call.At.Before(end) && (model == "" || strings.EqualFold(model, call.Model)) {
				if old, ok := calls[call.ID]; !ok || call.At.After(old.At) {
					calls[call.ID] = call
				}
			}
		}
		for key, item := range state.parser.activity {
			item.Title = state.parser.title
			if item.Provider == "codex" && names[item.Session] != "" {
				item.Title = names[item.Session]
			}
			if !item.At.Before(start) && item.At.Before(end) && (model == "" || strings.EqualFold(model, item.Model)) {
				// Live/archived copies may have different trailing lengths.
				// Keep the fullest observed minute rather than traversal order.
				if old, ok := activity[key]; ok {
					item.Milliseconds = max(old.Milliseconds, item.Milliseconds)
					item.Prompts = max(old.Prompts, item.Prompts)
				}
				activity[key] = item
			}
		}
	}
	r.updateIndex(seen, provider, dirty)
	result := Snapshot{Calls: make([]Call, 0, len(calls)), Activity: make([]Activity, 0, len(activity))}
	for _, call := range calls {
		result.Calls = append(result.Calls, call)
	}
	for _, item := range activity {
		result.Activity = append(result.Activity, item)
	}
	return result, nil
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
		line = line[:0]
	}
}
