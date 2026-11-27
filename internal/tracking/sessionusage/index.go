package sessionusage

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

const (
	CacheName            = "usage:local-index"
	indexVersion         = 2 // legacy gob format, imported once and kept for rollback
	defaultParserEntries = 128
	defaultParserBytes   = 8 << 20
	defaultParserTTL     = 5 * time.Minute
)

// The legacy allowlist contains no transcript bodies or parser event payloads.
// In the new store these maps live on disk; r.index retains only file metadata.
type indexedFile struct {
	Provider string
	Title    string
	Size     int64
	Modified time.Time
	Calls    map[string]Call
	Activity map[string]Activity
}

type diskIndex struct {
	Version int
	Files   map[string]indexedFile
}

type MemoryStats struct {
	IndexedFiles     int
	ResidentParsers  int
	ParserBytes      int64 // conservative estimate, not runtime heap accounting
	MetadataBytes    int64
	TitleBytes       int64
	BudgetBytes      int64
	LegacyImports    uint64
	FileWrites       uint64
	LastStorageError string
}

type readerStats struct {
	appcache.Stats
	disabled bool
	memory   MemoryStats
}

func NewPersistentReader(path string) *Reader {
	r := NewReader()
	r.indexPath = path
	return r
}

func indexKey(provider, path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(provider+"\x00"+path)))
}

func (r *Reader) openStore(ctx context.Context) *indexStore {
	if !r.enabled || r.indexPath == "" {
		return nil
	}
	s, err := openIndexStore(ctx, indexStorePath(r.indexPath))
	if isCorruptIndex(err) {
		if repairErr := rebuildCorruptIndex(ctx, indexStorePath(r.indexPath)); repairErr == nil {
			s, err = openIndexStore(ctx, indexStorePath(r.indexPath))
		}
	}
	if err != nil {
		r.noteStorageError(err)
		return nil
	}
	imported, err := s.importedLegacy(ctx)
	if err == nil && !imported {
		err = r.importLegacy(ctx, s)
	}
	if err == nil {
		r.index, err = s.manifest(ctx)
	}
	if err != nil {
		s.db.Close()
		r.noteStorageError(err)
		return nil
	}
	r.statsMu.Lock()
	r.stats.memory.LastStorageError = ""
	if info, err := os.Stat(indexStorePath(r.indexPath)); err == nil {
		r.stats.LastUpdatedAt = info.ModTime()
	}
	r.statsMu.Unlock()
	return s
}

func (r *Reader) importLegacy(ctx context.Context, s *indexStore) error {
	if !strings.EqualFold(filepath.Ext(r.indexPath), ".gob") {
		return s.markLegacyImported(ctx)
	}
	f, err := os.Open(r.indexPath)
	if os.IsNotExist(err) {
		return s.markLegacyImported(ctx)
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var legacy diskIndex
	if gob.NewDecoder(io.LimitReader(f, 128<<20)).Decode(&legacy) != nil || legacy.Version != indexVersion {
		return s.markLegacyImported(ctx)
	}
	for key, file := range legacy.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		file.Title = displayTitle(file.Title)
		p := &parser{calls: file.Calls, activity: file.Activity}
		if err := s.saveFile(ctx, key, file, p, true); err != nil {
			return err
		}
		delete(legacy.Files, key)
	}
	if err := s.markLegacyImported(ctx); err != nil {
		return err
	}
	r.statsMu.Lock()
	r.stats.memory.LegacyImports++
	r.statsMu.Unlock()
	return nil
}

func (r *Reader) noteStorageError(err error) {
	r.refreshAt = time.Time{}
	r.revision++
	r.statsMu.Lock()
	r.stats.memory.LastStorageError = err.Error()
	r.statsMu.Unlock()
}

func (r *Reader) noteFile(hit bool) {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	if hit {
		r.stats.Hits++
	} else {
		r.stats.Misses++
	}
}

func (r *Reader) forgetParser(path string) {
	if state := r.resident[path]; state != nil {
		r.parserBytes -= state.bytes
		delete(r.resident, path)
	}
}

func (r *Reader) retainParser(path string, state *fileState) {
	r.forgetParser(path)
	if !r.enabled || state.parser == nil {
		state.parser = nil
		state.restored = true
		return
	}
	state.bytes = state.parser.memoryBytes()
	if state.bytes > defaultParserBytes {
		state.parser = nil
		state.restored = true
		r.statsMu.Lock()
		r.stats.Evicted++
		r.statsMu.Unlock()
		return
	}
	r.clock++
	state.lastUsed, state.accessed = r.clock, time.Now()
	r.resident[path] = state
	r.parserBytes += state.bytes
	r.trimParsers()
}

func (r *Reader) trimParsers() {
	now := time.Now()
	var evicted, expired uint64
	if r.titles.info != nil && r.cacheTTL > 0 && now.Sub(r.titleAccessed) >= r.cacheTTL {
		r.refreshAt = time.Time{}
		r.titles = titleIndex{}
		expired++
	}
	for path, state := range r.resident {
		if r.cacheTTL > 0 && now.Sub(state.accessed) >= r.cacheTTL {
			r.forgetParser(path)
			state.parser, state.restored = nil, true
			expired++
		}
	}
	budget := max(int64(0), int64(defaultParserBytes)-r.titleMemoryBytes())
	for len(r.resident) > r.cacheEntries || r.parserBytes > budget {
		var oldest string
		var order uint64
		for path, state := range r.resident {
			if oldest == "" || state.lastUsed < order {
				oldest, order = path, state.lastUsed
			}
		}
		state := r.resident[oldest]
		r.forgetParser(oldest)
		state.parser, state.restored = nil, true
		evicted++
	}
	if evicted+expired > 0 {
		r.statsMu.Lock()
		r.stats.Evicted += evicted
		r.stats.Expired += expired
		r.statsMu.Unlock()
	}
}

func (r *Reader) titleMemoryBytes() int64 {
	var size int64
	for key, title := range r.titles.names {
		size += int64(len(key)+len(title)) + 64
	}
	return size
}

func (r *Reader) updateMemoryStats() {
	var metadata int64
	for path, state := range r.files {
		metadata += int64(unsafe.Sizeof(*state)) + int64(len(path)+len(state.provider)) + 64
	}
	for key, file := range r.index {
		metadata += int64(unsafe.Sizeof(file)) + int64(len(key)+len(file.Provider)+len(file.Title)) + 64
	}
	titles := r.titleMemoryBytes()
	// Large rename indexes remain available to the current query, but are not
	// pinned between queries. Reloading keeps all displayed titles complete.
	if titles > defaultParserBytes {
		r.refreshAt = time.Time{}
		r.titles = titleIndex{}
		titles = 0
	}
	r.trimParsers()
	r.statsMu.Lock()
	// Size/MaxEntries describe resident parsers. Disk file count is separate
	// so an unlimited historical index is not shown as exceeding its RAM limit.
	r.stats.Size = len(r.resident)
	r.stats.memory.IndexedFiles = len(r.index)
	r.stats.memory.ResidentParsers = len(r.resident)
	r.stats.memory.ParserBytes, r.stats.memory.MetadataBytes, r.stats.memory.TitleBytes = r.parserBytes, metadata, titles
	r.stats.memory.BudgetBytes = defaultParserBytes
	r.statsMu.Unlock()
}

func (r *Reader) Name() string { return CacheName }

func (r *Reader) Stats() appcache.Stats {
	if r.mu.TryLock() {
		r.trimParsers()
		r.updateMemoryStats()
		r.mu.Unlock()
	}
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	s := r.stats.Stats
	s.Name, s.Enabled = CacheName, !r.stats.disabled
	s.HasByteEstimate = true
	s.EstimatedBytes = r.stats.memory.ParserBytes + r.stats.memory.TitleBytes
	s.MaxBytes = r.stats.memory.BudgetBytes
	return s
}

func (r *Reader) MemoryStats() MemoryStats {
	if r.mu.TryLock() {
		r.trimParsers()
		r.updateMemoryStats()
		r.mu.Unlock()
	}
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	return r.stats.memory
}

func (r *Reader) Options() appcache.Options {
	stats := r.Stats()
	return appcache.Options{Name: CacheName, Enabled: stats.Enabled, TTL: stats.TTL, MaxEntries: stats.MaxEntries}
}

func (r *Reader) SetOptions(options appcache.Options) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.enabled != options.Enabled {
		r.enabled = options.Enabled
		r.clearIndex()
	}
	r.cacheTTL = defaultParserTTL
	if options.TTL > 0 {
		r.cacheTTL = options.TTL
	}
	r.cacheEntries = defaultParserEntries
	if options.MaxEntries > 0 {
		r.cacheEntries = min(defaultParserEntries, options.MaxEntries)
	}
	r.trimParsers()
	r.statsMu.Lock()
	r.stats.disabled = !r.enabled
	r.stats.TTL, r.stats.MaxEntries = r.cacheTTL, r.cacheEntries
	r.statsMu.Unlock()
	r.updateMemoryStats()
}

func (r *Reader) Invalidate(options appcache.InvalidateOptions) int {
	if !options.All {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Count an existing disk manifest without decoding/importing the legacy
	// index just to delete it. Clearing a cold cache must not rebuild history.
	if r.indexPath != "" {
		path := indexStorePath(r.indexPath)
		if _, err := os.Stat(path); err == nil {
			if s, err := openIndexStore(ctx, path); err == nil {
				if manifest, err := s.manifest(ctx); err == nil {
					r.index = manifest
				}
				s.db.Close()
			}
		}
	}
	return r.clearIndex()
}

func (r *Reader) clearIndex() int {
	r.refreshAt = time.Time{}
	r.revision++
	size := max(len(r.index), len(r.files))
	if r.titles.info != nil {
		size++
	}
	r.files, r.resident = make(map[string]*fileState), make(map[string]*fileState)
	r.index = make(map[string]indexedFile)
	r.parserBytes = 0
	r.titles = titleIndex{}
	if r.indexPath != "" {
		path := indexStorePath(r.indexPath)
		for _, file := range []string{path, path + "-wal", path + "-shm"} {
			_ = os.Remove(file)
		}
		if strings.EqualFold(filepath.Ext(r.indexPath), ".gob") {
			_ = os.Remove(r.indexPath)
		}
	}
	r.statsMu.Lock()
	r.stats.Size = 0
	r.stats.Invalidations++
	r.stats.LastInvalidatedAt = time.Now()
	r.stats.memory = MemoryStats{BudgetBytes: defaultParserBytes}
	r.statsMu.Unlock()
	return size
}
