package cache

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Options controls a single in-process cache instance.
type Options struct {
	Name         string
	TTL          time.Duration
	MaxEntries   int
	StaleOnError bool
	Enabled      bool
}

// Stats is a point-in-time snapshot for one cache instance.
type Stats struct {
	Name              string        `json:"name"`
	Enabled           bool          `json:"enabled"`
	TTL               time.Duration `json:"ttl"`
	MaxEntries        int           `json:"max_entries"`
	Size              int           `json:"size"`
	Hits              uint64        `json:"hits"`
	Misses            uint64        `json:"misses"`
	Expired           uint64        `json:"expired"`
	Evicted           uint64        `json:"evicted"`
	Invalidations     uint64        `json:"invalidations"`
	LastUpdatedAt     time.Time     `json:"last_updated_at"`
	LastInvalidatedAt time.Time     `json:"last_invalidated_at"`
}

// InvalidateOptions selects entries to remove from a cache.
type InvalidateOptions struct {
	Key    string
	Prefix string
	All    bool
}

type entry[T any] struct {
	value     T
	expiresAt time.Time
	updatedAt time.Time
}

// Cache is a small generic TTL cache for read-path aggregation data.
type Cache[T any] struct {
	mu                sync.RWMutex
	options           Options
	items             map[string]entry[T]
	hits              atomic.Uint64
	misses            atomic.Uint64
	expired           atomic.Uint64
	evicted           atomic.Uint64
	invalidations     atomic.Uint64
	lastUpdatedAt     time.Time
	lastInvalidatedAt time.Time
}

// New creates a cache with normalized defaults.
func New[T any](options Options) *Cache[T] {
	options = normalizeOptions(options, true)
	return &Cache[T]{
		options: options,
		items:   make(map[string]entry[T]),
	}
}

func normalizeOptions(options Options, defaultEnabled bool) Options {
	if options.Name == "" {
		options.Name = "cache"
	}
	if defaultEnabled && !options.Enabled {
		options.Enabled = true
	}
	return options
}

// Name returns the configured cache name.
func (c *Cache[T]) Name() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.options.Name
}

// Get returns a non-expired cached value.
func (c *Cache[T]) Get(key string) (T, bool) {
	now := time.Now()
	var zero T

	c.mu.RLock()
	if !c.options.Enabled {
		c.mu.RUnlock()
		c.misses.Add(1)
		return zero, false
	}
	item, ok := c.items[key]
	if !ok {
		c.mu.RUnlock()
		c.misses.Add(1)
		return zero, false
	}
	if c.isExpired(item, now) {
		c.mu.RUnlock()
		c.mu.Lock()
		if current, exists := c.items[key]; exists && c.isExpired(current, now) {
			delete(c.items, key)
			c.expired.Add(1)
		}
		c.mu.Unlock()
		c.misses.Add(1)
		return zero, false
	}
	c.mu.RUnlock()

	c.hits.Add(1)
	return item.value, true
}

// Set stores or replaces a value.
func (c *Cache[T]) Set(key string, value T) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.options.Enabled {
		return
	}
	c.items[key] = entry[T]{
		value:     value,
		expiresAt: c.expiresAt(now),
		updatedAt: now,
	}
	c.lastUpdatedAt = now
	c.enforceCapacityLocked(now)
}

// GetOrLoad returns a cached value or stores the loader result.
func (c *Cache[T]) GetOrLoad(key string, loader func() (T, error)) (T, error) {
	now := time.Now()
	var zero T
	var staleValue T
	var hasStale bool

	c.mu.RLock()
	enabled := c.options.Enabled
	staleOnError := c.options.StaleOnError
	if enabled {
		if item, ok := c.items[key]; ok {
			if !c.isExpired(item, now) {
				c.mu.RUnlock()
				c.hits.Add(1)
				return item.value, nil
			}
			if staleOnError {
				staleValue = item.value
				hasStale = true
			}
			c.expired.Add(1)
		}
	}
	c.mu.RUnlock()
	c.misses.Add(1)

	value, err := loader()
	if err != nil {
		if enabled && staleOnError && hasStale {
			return staleValue, nil
		}
		return zero, err
	}
	if enabled {
		c.Set(key, value)
	}
	return value, nil
}

// Invalidate removes matching entries and returns the number removed.
func (c *Cache[T]) Invalidate(options InvalidateOptions) int {
	if !options.All && options.Key == "" && options.Prefix == "" {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	switch {
	case options.All:
		removed = len(c.items)
		c.items = make(map[string]entry[T])
	case options.Key != "":
		if _, ok := c.items[options.Key]; ok {
			delete(c.items, options.Key)
			removed = 1
		}
	case options.Prefix != "":
		for key := range c.items {
			if strings.HasPrefix(key, options.Prefix) {
				delete(c.items, key)
				removed++
			}
		}
	}

	c.invalidations.Add(1)
	c.lastInvalidatedAt = time.Now()
	return removed
}

// SetOptions updates runtime cache settings and enforces the new capacity.
func (c *Cache[T]) SetOptions(options Options) {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentName := c.options.Name
	c.options = normalizeOptions(options, false)
	if c.options.Name == "cache" && currentName != "" {
		c.options.Name = currentName
	}
	if !c.options.Enabled {
		c.items = make(map[string]entry[T])
		c.lastInvalidatedAt = time.Now()
		c.invalidations.Add(1)
		return
	}
	c.enforceCapacityLocked(time.Now())
}

// Options returns a copy of the current options.
func (c *Cache[T]) Options() Options {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.options
}

// Stats returns a point-in-time status snapshot and lazily removes expired entries.
func (c *Cache[T]) Stats() Stats {
	now := time.Now()

	c.mu.Lock()
	c.purgeExpiredLocked(now)
	stats := Stats{
		Name:              c.options.Name,
		Enabled:           c.options.Enabled,
		TTL:               c.options.TTL,
		MaxEntries:        c.options.MaxEntries,
		Size:              len(c.items),
		Hits:              c.hits.Load(),
		Misses:            c.misses.Load(),
		Expired:           c.expired.Load(),
		Evicted:           c.evicted.Load(),
		Invalidations:     c.invalidations.Load(),
		LastUpdatedAt:     c.lastUpdatedAt,
		LastInvalidatedAt: c.lastInvalidatedAt,
	}
	c.mu.Unlock()

	return stats
}

func (c *Cache[T]) isExpired(item entry[T], now time.Time) bool {
	return !item.expiresAt.IsZero() && !now.Before(item.expiresAt)
}

func (c *Cache[T]) expiresAt(now time.Time) time.Time {
	if c.options.TTL <= 0 {
		return time.Time{}
	}
	return now.Add(c.options.TTL)
}

func (c *Cache[T]) purgeExpiredLocked(now time.Time) int {
	removed := 0
	for key, item := range c.items {
		if c.isExpired(item, now) {
			delete(c.items, key)
			removed++
		}
	}
	if removed > 0 {
		c.expired.Add(uint64(removed))
	}
	return removed
}

func (c *Cache[T]) enforceCapacityLocked(now time.Time) {
	maxEntries := c.options.MaxEntries
	if maxEntries <= 0 {
		c.purgeExpiredLocked(now)
		return
	}

	c.purgeExpiredLocked(now)
	for len(c.items) > maxEntries {
		oldestKey := ""
		var oldestUpdatedAt time.Time
		for key, item := range c.items {
			if oldestKey == "" || item.updatedAt.Before(oldestUpdatedAt) {
				oldestKey = key
				oldestUpdatedAt = item.updatedAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(c.items, oldestKey)
		c.evicted.Add(1)
	}
}
