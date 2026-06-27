package cache

import (
	"sort"
	"sync"
)

// ManagedCache is the registry-facing subset implemented by Cache[T].
type ManagedCache interface {
	Name() string
	Stats() Stats
	Invalidate(InvalidateOptions) int
	SetOptions(Options)
	Options() Options
}

// ClearOptions selects which registered caches to clear.
type ClearOptions struct {
	Names []string
}

// ClearResult summarizes a registry clear operation.
type ClearResult struct {
	ClearedCaches  int      `json:"cleared_caches"`
	ClearedEntries int      `json:"cleared_entries"`
	UnknownNames   []string `json:"unknown_names,omitempty"`
}

// Registry owns status and clear operations for cache instances.
type Registry struct {
	mu     sync.RWMutex
	caches map[string]ManagedCache
}

// NewRegistry creates an empty cache registry.
func NewRegistry() *Registry {
	return &Registry{caches: make(map[string]ManagedCache)}
}

// Register adds or replaces a managed cache by name.
func (r *Registry) Register(cache ManagedCache) {
	if cache == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.caches[cache.Name()] = cache
}

// Get returns a registered cache by name.
func (r *Registry) Get(name string) (ManagedCache, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cache, ok := r.caches[name]
	return cache, ok
}

// Status returns stats for all registered caches in stable name order.
func (r *Registry) Status() []Stats {
	r.mu.RLock()
	caches := make([]ManagedCache, 0, len(r.caches))
	for _, cache := range r.caches {
		caches = append(caches, cache)
	}
	r.mu.RUnlock()

	stats := make([]Stats, 0, len(caches))
	for _, cache := range caches {
		stats = append(stats, cache.Stats())
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Name < stats[j].Name
	})
	return stats
}

// Clear removes entries from selected caches. Empty names means all caches.
func (r *Registry) Clear(options ClearOptions) ClearResult {
	r.mu.RLock()
	caches := make(map[string]ManagedCache, len(r.caches))
	for name, cache := range r.caches {
		caches[name] = cache
	}
	r.mu.RUnlock()

	result := ClearResult{}
	if len(options.Names) == 0 {
		names := make([]string, 0, len(caches))
		for name := range caches {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			result.ClearedEntries += caches[name].Invalidate(InvalidateOptions{All: true})
			result.ClearedCaches++
		}
		return result
	}

	seen := make(map[string]struct{}, len(options.Names))
	for _, name := range options.Names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cache, ok := caches[name]
		if !ok {
			result.UnknownNames = append(result.UnknownNames, name)
			continue
		}
		result.ClearedEntries += cache.Invalidate(InvalidateOptions{All: true})
		result.ClearedCaches++
	}
	sort.Strings(result.UnknownNames)
	return result
}
