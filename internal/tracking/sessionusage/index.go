package sessionusage

import (
	"bufio"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

const CacheName = "usage:local-index"
const indexVersion = 2 // includes bounded display titles

// The on-disk allowlist excludes transcripts and parser event payloads. Changed
// files are parsed afresh after restart; live files then resume incrementally.
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

type readerStats struct {
	appcache.Stats
	disabled bool
}

func NewPersistentReader(path string) *Reader {
	r := NewReader()
	r.indexPath = path
	return r
}

func indexKey(provider, path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(provider+"\x00"+path)))
}

// Caller holds r.mu. Cache corruption and IO failures fall back to the source.
func (r *Reader) loadIndex() {
	if r.indexLoaded || !r.enabled {
		return
	}
	r.indexLoaded = true
	r.index = make(map[string]indexedFile)
	if r.indexPath == "" {
		return
	}
	f, err := os.Open(r.indexPath)
	if err != nil {
		return
	}
	defer f.Close()
	var index diskIndex
	if gob.NewDecoder(io.LimitReader(f, 128<<20)).Decode(&index) == nil && index.Version == indexVersion && index.Files != nil {
		r.index = index.Files
		r.statsMu.Lock()
		r.stats.Size = len(index.Files)
		if info, err := f.Stat(); err == nil {
			r.stats.LastUpdatedAt = info.ModTime()
		}
		r.statsMu.Unlock()
	}
}

func (r *Reader) updateIndex(seen map[string]bool, provider string, dirty bool) {
	if !r.enabled {
		return
	}
	active := make(map[string]bool, len(seen))
	for path := range seen {
		state := r.files[path]
		if state == nil || state.info == nil {
			continue
		}
		key := indexKey(state.parser.provider, path)
		active[key] = true
		r.index[key] = indexedFile{Provider: state.parser.provider, Title: state.parser.title, Size: state.info.Size(), Modified: state.info.ModTime(), Calls: state.parser.calls, Activity: state.parser.activity}
	}
	for key, file := range r.index {
		if (provider == "" || provider == file.Provider) && !active[key] {
			delete(r.index, key)
			dirty = true
		}
	}
	r.statsMu.Lock()
	r.stats.Size = len(r.index)
	if r.titles.info != nil {
		r.stats.Size++
	}
	if dirty {
		r.stats.LastUpdatedAt = time.Now()
	}
	r.statsMu.Unlock()
	if !dirty || r.indexPath == "" {
		return
	}
	// Atomic replacement makes an interrupted write a cache miss on next launch.
	// Saving is best effort: unavailable cache storage must not hide usage data.
	if err := os.MkdirAll(filepath.Dir(r.indexPath), 0700); err != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(r.indexPath), ".local-usage-*")
	if err != nil {
		return
	}
	name := f.Name()
	defer os.Remove(name)
	w := bufio.NewWriter(f)
	if err := gob.NewEncoder(w).Encode(diskIndex{indexVersion, r.index}); err != nil {
		f.Close()
		return
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return
	}
	if err := f.Close(); err != nil {
		return
	}
	_ = os.Rename(name, r.indexPath)
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

func (r *Reader) Name() string { return CacheName }

func (r *Reader) Stats() appcache.Stats {
	// Status reads never wait for transcript IO/decoding to finish.
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	s := r.stats.Stats
	s.Name, s.Enabled = CacheName, !r.stats.disabled
	return s
}

func (r *Reader) Options() appcache.Options {
	return appcache.Options{Name: CacheName, Enabled: r.Stats().Enabled}
}

func (r *Reader) SetOptions(options appcache.Options) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.enabled == options.Enabled {
		return
	}
	r.enabled = options.Enabled
	r.clearIndex()
	r.statsMu.Lock()
	r.stats.disabled = !options.Enabled
	r.statsMu.Unlock()
}

func (r *Reader) Invalidate(options appcache.InvalidateOptions) int {
	// Proxy request-ledger writes do not invalidate native transcript metadata.
	if !options.All {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadIndex()
	return r.clearIndex()
}

func (r *Reader) clearIndex() int {
	size := max(len(r.index), len(r.files))
	if r.titles.info != nil {
		size++
	}
	r.files = make(map[string]*fileState)
	r.index = make(map[string]indexedFile)
	r.indexLoaded = true
	r.titles = titleIndex{}
	if r.indexPath != "" {
		_ = os.Remove(r.indexPath)
	}
	r.statsMu.Lock()
	r.stats.Size = 0
	r.stats.Invalidations++
	r.stats.LastInvalidatedAt = time.Now()
	r.statsMu.Unlock()
	return size
}
