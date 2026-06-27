package cache

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCacheSetGetHitAndExpiry(t *testing.T) {
	c := New[string](Options{Name: "test", TTL: 20 * time.Millisecond, MaxEntries: 10})

	c.Set("alpha", "one")

	value, ok := c.Get("alpha")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if value != "one" {
		t.Fatalf("expected value one, got %q", value)
	}

	time.Sleep(30 * time.Millisecond)

	if _, ok := c.Get("alpha"); ok {
		t.Fatal("expected expired entry to miss")
	}

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Expired != 1 {
		t.Fatalf("expected 1 expired entry, got %d", stats.Expired)
	}
}

func TestCacheEvictsExpiredThenOldestUpdatedEntry(t *testing.T) {
	c := New[string](Options{Name: "bounded", TTL: time.Hour, MaxEntries: 2})

	c.Set("first", "1")
	time.Sleep(time.Millisecond)
	c.Set("second", "2")
	time.Sleep(time.Millisecond)
	c.Set("third", "3")

	if _, ok := c.Get("first"); ok {
		t.Fatal("expected oldest updated entry to be evicted")
	}
	if _, ok := c.Get("second"); !ok {
		t.Fatal("expected second entry to remain")
	}
	if _, ok := c.Get("third"); !ok {
		t.Fatal("expected third entry to remain")
	}
	if got := c.Stats().Evicted; got != 1 {
		t.Fatalf("expected 1 eviction, got %d", got)
	}

	short := New[string](Options{Name: "expired-first", TTL: 10 * time.Millisecond, MaxEntries: 2})
	short.Set("expired", "old")
	time.Sleep(15 * time.Millisecond)
	short.Set("fresh", "new")
	short.Set("another", "newer")

	if _, ok := short.Get("expired"); ok {
		t.Fatal("expected expired entry to be removed before capacity eviction")
	}
	if got := short.Stats().Evicted; got != 0 {
		t.Fatalf("expected no live entry eviction, got %d", got)
	}
}

func TestCacheInvalidationByKeyPrefixAndAll(t *testing.T) {
	c := New[string](Options{Name: "invalidate", TTL: time.Hour, MaxEntries: 10})
	c.Set("usage:today", "a")
	c.Set("usage:yesterday", "b")
	c.Set("settings:cache", "c")

	removed := c.Invalidate(InvalidateOptions{Key: "usage:today"})
	if removed != 1 {
		t.Fatalf("expected 1 key removed, got %d", removed)
	}
	if _, ok := c.Get("usage:today"); ok {
		t.Fatal("expected usage:today to be invalidated")
	}

	removed = c.Invalidate(InvalidateOptions{Prefix: "usage:"})
	if removed != 1 {
		t.Fatalf("expected 1 prefix match removed, got %d", removed)
	}
	if _, ok := c.Get("usage:yesterday"); ok {
		t.Fatal("expected usage:yesterday to be invalidated")
	}

	removed = c.Invalidate(InvalidateOptions{All: true})
	if removed != 1 {
		t.Fatalf("expected remaining entry removed, got %d", removed)
	}
	if c.Stats().Size != 0 {
		t.Fatalf("expected empty cache, got size %d", c.Stats().Size)
	}
	if got := c.Stats().Invalidations; got != 3 {
		t.Fatalf("expected 3 invalidation calls, got %d", got)
	}
}

func TestGetOrLoadCachesValueAndSupportsStaleOnError(t *testing.T) {
	c := New[string](Options{Name: "loader", TTL: 10 * time.Millisecond, MaxEntries: 10, StaleOnError: true})

	loads := 0
	value, err := c.GetOrLoad("answer", func() (string, error) {
		loads++
		return "fresh", nil
	})
	if err != nil {
		t.Fatalf("unexpected loader error: %v", err)
	}
	if value != "fresh" {
		t.Fatalf("expected fresh value, got %q", value)
	}

	value, err = c.GetOrLoad("answer", func() (string, error) {
		loads++
		return "unexpected", nil
	})
	if err != nil {
		t.Fatalf("unexpected cached error: %v", err)
	}
	if value != "fresh" {
		t.Fatalf("expected cached value, got %q", value)
	}
	if loads != 1 {
		t.Fatalf("expected loader to run once, got %d", loads)
	}

	time.Sleep(15 * time.Millisecond)
	value, err = c.GetOrLoad("answer", func() (string, error) {
		return "", errors.New("database busy")
	})
	if err != nil {
		t.Fatalf("expected stale value without error when stale_on_error is enabled, got %v", err)
	}
	if value != "fresh" {
		t.Fatalf("expected stale value, got %q", value)
	}

	withoutStale := New[string](Options{Name: "loader-error", TTL: time.Hour, MaxEntries: 10})
	_, err = withoutStale.GetOrLoad("missing", func() (string, error) {
		return "", errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected loader error")
	}
	if _, ok := withoutStale.Get("missing"); ok {
		t.Fatal("loader errors must not populate cache")
	}
}

func TestCacheCanBeDisabledViaRuntimeOptions(t *testing.T) {
	c := New[string](Options{Name: "disabled", TTL: time.Hour, MaxEntries: 10})
	c.Set("key", "value")

	c.SetOptions(Options{Name: "disabled", TTL: time.Hour, MaxEntries: 10, Enabled: false})

	if _, ok := c.Get("key"); ok {
		t.Fatal("expected disabled cache to miss")
	}
	if stats := c.Stats(); stats.Enabled {
		t.Fatal("expected cache status to show disabled")
	}
	c.Set("key", "new-value")
	if stats := c.Stats(); stats.Size != 0 {
		t.Fatalf("expected disabled cache to stay empty, got size %d", stats.Size)
	}
}

func TestCacheConcurrentReadWrite(t *testing.T) {
	const workers = 16
	const iterations = 100
	c := New[int](Options{Name: "concurrent", TTL: time.Hour, MaxEntries: workers * iterations})

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := fmt.Sprintf("worker:%d:%d", worker, i)
				c.Set(key, i)
				if value, ok := c.Get(key); !ok || value != i {
					t.Errorf("expected %s=%d, got %d hit=%v", key, i, value, ok)
				}
			}
		}(worker)
	}
	wg.Wait()

	if c.Stats().Size > workers*iterations {
		t.Fatalf("expected cache size capped at %d, got %d", workers*iterations, c.Stats().Size)
	}
}

func TestRegistryStatusAndClear(t *testing.T) {
	registry := NewRegistry()
	usage := New[string](Options{Name: "usage", TTL: time.Minute, MaxEntries: 10})
	status := New[string](Options{Name: "status", TTL: time.Minute, MaxEntries: 10})
	usage.Set("usage:today", "value")
	status.Set("status:port", "value")

	registry.Register(usage)
	registry.Register(status)

	stats := registry.Status()
	if len(stats) != 2 {
		t.Fatalf("expected 2 cache stats, got %d", len(stats))
	}

	result := registry.Clear(ClearOptions{Names: []string{"usage"}})
	if result.ClearedEntries != 1 {
		t.Fatalf("expected 1 cleared entry, got %d", result.ClearedEntries)
	}
	if _, ok := usage.Get("usage:today"); ok {
		t.Fatal("expected usage cache cleared")
	}
	if _, ok := status.Get("status:port"); !ok {
		t.Fatal("expected status cache to remain")
	}

	result = registry.Clear(ClearOptions{})
	if result.ClearedCaches != 2 {
		t.Fatalf("expected all registered caches to be cleared, got %d", result.ClearedCaches)
	}
}
