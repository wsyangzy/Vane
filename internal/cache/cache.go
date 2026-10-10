package cache

import (
	"slices"
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
	Fixed             bool          `json:"fixed"` // 不受统一开关控制的常驻缓存
	TTL               time.Duration `json:"ttl"`
	MaxEntries        int           `json:"max_entries"`
	Size              int           `json:"size"`
	Hits              uint64        `json:"hits"`
	Misses            uint64        `json:"misses"`
	Expired           uint64        `json:"expired"`
	Evicted           uint64        `json:"evicted"`
	Invalidations     uint64        `json:"invalidations"`
	StaleServed       uint64        `json:"stale_served"`
	LastUpdatedAt     time.Time     `json:"last_updated_at"`
	LastInvalidatedAt time.Time     `json:"last_invalidated_at"`
}

// Scope 描述一条缓存值依赖的数据范围。零值表示依赖全部数据，任何变更都会使其失效。
type Scope struct {
	Provider string    // 空串表示不区分 provider
	Start    time.Time // 零值表示无下界
	End      time.Time // 零值表示无上界
}

// Change 描述一次数据变更影响的范围，按 provider 与记录时间定位受影响的缓存。
type Change struct {
	All       bool      // 影响范围未知或为全量
	Providers []string  // 为空表示所有 provider
	Start     time.Time // 受影响记录的最早时间，零值表示无下界
	End       time.Time // 受影响记录的最晚时间，零值表示无上界
}

// Affected 判断变更是否落入该范围；边界相等按受影响处理，宁可多清不可漏清。
func (s Scope) Affected(c Change) bool {
	if c.All {
		return true
	}
	if s.Provider != "" && len(c.Providers) > 0 && !slices.Contains(c.Providers, s.Provider) {
		return false
	}
	if !s.End.IsZero() && !c.Start.IsZero() && c.Start.After(s.End) {
		return false
	}
	if !s.Start.IsZero() && !c.End.IsZero() && c.End.Before(s.Start) {
		return false
	}
	return true
}

// InvalidateOptions selects entries to remove from a cache.
type InvalidateOptions struct {
	Key    string
	Prefix string
	All    bool
	Change *Change // 按数据范围失效，仅移除 Scope 受影响的条目
}

type entry[T any] struct {
	value     T
	scope     Scope
	expiresAt time.Time
	updatedAt time.Time
}

// call 合并同一 key 的并发加载，避免缓存未命中时多个调用方同时查库。
type call[T any] struct {
	done  chan struct{}
	value T
	err   error
	stale bool
}

// Cache is a small generic TTL cache for read-path aggregation data.
type Cache[T any] struct {
	mu       sync.Mutex
	options  Options
	items    map[string]entry[T]
	inflight map[string]*call[T]
	// epoch 在每次失效时递增。加载开始后若发生过失效，加载结果可能基于失效前的数据，
	// 只返回给调用方而不入缓存，避免旧值在兜底 TTL 内一直残留。
	epoch uint64

	hits              atomic.Uint64
	misses            atomic.Uint64
	expired           atomic.Uint64
	evicted           atomic.Uint64
	invalidations     atomic.Uint64
	staleServed       atomic.Uint64
	lastUpdatedAt     time.Time
	lastInvalidatedAt time.Time
}

// New creates a cache with normalized defaults.
func New[T any](options Options) *Cache[T] {
	options = normalizeOptions(options, true)
	return &Cache[T]{
		options:  options,
		items:    make(map[string]entry[T]),
		inflight: make(map[string]*call[T]),
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
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.options.Name
}

// Get returns a non-expired cached value.
func (c *Cache[T]) Get(key string) (T, bool) {
	now := time.Now()
	var zero T

	c.mu.Lock()
	if !c.options.Enabled {
		c.mu.Unlock()
		c.misses.Add(1)
		return zero, false
	}
	item, ok := c.items[key]
	if !ok {
		c.mu.Unlock()
		c.misses.Add(1)
		return zero, false
	}
	if c.isExpired(item, now) {
		delete(c.items, key)
		c.expired.Add(1)
		c.mu.Unlock()
		c.misses.Add(1)
		return zero, false
	}
	c.mu.Unlock()

	c.hits.Add(1)
	return item.value, true
}

// Set stores or replaces a value that depends on all data.
func (c *Cache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.options.Enabled {
		return
	}
	c.setLocked(key, Scope{}, value, time.Now())
}

func (c *Cache[T]) setLocked(key string, scope Scope, value T, now time.Time) {
	if existing, ok := c.items[key]; ok && c.isExpired(existing, now) {
		c.expired.Add(1)
	}
	c.items[key] = entry[T]{
		value:     value,
		scope:     scope,
		expiresAt: c.expiresAt(now),
		updatedAt: now,
	}
	c.lastUpdatedAt = now
	c.enforceCapacityLocked(now)
}

// GetOrLoad returns a cached value or stores the loader result.
func (c *Cache[T]) GetOrLoad(key string, loader func() (T, error)) (T, error) {
	return c.GetOrLoadScoped(key, Scope{}, loader)
}

// GetOrLoadScoped 与 GetOrLoad 相同，额外记录值依赖的数据范围，供 Change 失效精确定位。
func (c *Cache[T]) GetOrLoadScoped(key string, scope Scope, loader func() (T, error)) (T, error) {
	now := time.Now()

	c.mu.Lock()
	if !c.options.Enabled {
		c.mu.Unlock()
		c.misses.Add(1)
		return loader()
	}
	item, hasItem := c.items[key]
	if hasItem && !c.isExpired(item, now) {
		c.mu.Unlock()
		c.hits.Add(1)
		return item.value, nil
	}
	if pending, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-pending.done
		c.hits.Add(1)
		if pending.stale {
			c.staleServed.Add(1)
		}
		return pending.value, pending.err
	}

	current := &call[T]{done: make(chan struct{})}
	c.inflight[key] = current
	staleOnError := c.options.StaleOnError
	epoch := c.epoch
	c.mu.Unlock()
	c.misses.Add(1)

	// defer 保证 loader panic 时也会唤醒等待者并清理 inflight
	defer func() {
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		close(current.done)
	}()

	current.value, current.err = loader()

	c.mu.Lock()
	if current.err == nil {
		if c.options.Enabled && c.epoch == epoch {
			c.setLocked(key, scope, current.value, time.Now())
		}
	} else if staleOnError && hasItem {
		current.value, current.err, current.stale = item.value, nil, true
	}
	c.mu.Unlock()

	if current.stale {
		c.staleServed.Add(1)
	}
	return current.value, current.err
}

// Invalidate removes matching entries and returns the number removed.
func (c *Cache[T]) Invalidate(options InvalidateOptions) int {
	if !options.All && options.Key == "" && options.Prefix == "" && options.Change == nil {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.epoch++
	removed := 0
	switch {
	case options.All:
		removed = len(c.items)
		c.items = make(map[string]entry[T])
	case options.Change != nil:
		for key, item := range c.items {
			if item.scope.Affected(*options.Change) {
				delete(c.items, key)
				removed++
			}
		}
		// 按范围失效由每次写入触发，未命中任何条目时不计数，保持「失效」统计有意义
		if removed == 0 {
			return 0
		}
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
		c.epoch++
		c.lastInvalidatedAt = time.Now()
		c.invalidations.Add(1)
		return
	}
	c.enforceCapacityLocked(time.Now())
}

// Options returns a copy of the current options.
func (c *Cache[T]) Options() Options {
	c.mu.Lock()
	defer c.mu.Unlock()
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
		StaleServed:       c.staleServed.Load(),
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
