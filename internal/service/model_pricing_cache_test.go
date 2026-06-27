package service

import (
	"testing"

	"ai-relay-forwarder/internal/store"
)

func TestModelPricingCacheStatsAndInvalidate(t *testing.T) {
	service := NewModelPricingService(nil)
	service.updateCache(&store.ModelPricingRecord{ModelName: "model-a", IsDefault: true})

	stats := service.CacheStats()
	if stats.Name != "model-pricing" {
		t.Fatalf("expected model-pricing cache name, got %q", stats.Name)
	}
	if stats.Size != 1 {
		t.Fatalf("expected cache size 1, got %d", stats.Size)
	}

	removed := service.InvalidateCache()
	if removed != 1 {
		t.Fatalf("expected 1 invalidated entry, got %d", removed)
	}
	stats = service.CacheStats()
	if stats.Size != 0 {
		t.Fatalf("expected empty cache, got %d", stats.Size)
	}
	if stats.Invalidations != 1 {
		t.Fatalf("expected 1 invalidation, got %d", stats.Invalidations)
	}
}
