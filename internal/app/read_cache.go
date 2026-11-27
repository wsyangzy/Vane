package app

import (
	"reflect"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
)

const (
	cacheNameDashboardLocal       = "usage:dashboard-local"
	cacheNameDashboardProxy       = "usage:dashboard-proxy"
	cacheNameDashboardPricing     = "usage:dashboard-pricing"
	cacheNameEndpointAvailability = "endpoint:availability"
	dashboardCacheBytes           = 4 << 20
	metadataCacheBytes            = 1 << 20
)

type readQueryCaches struct {
	local, proxy *appcache.Cache[usageDashboardSnapshot]
	pricing      *appcache.Cache[map[string]*pricingstore.ModelPricingRecord]
	availability *appcache.Cache[EndpointAvailabilityData]
}

func (a *App) ensureReadCaches() *readQueryCaches {
	if caches := a.readCaches.Load(); caches != nil {
		return caches
	}
	options := readCacheOptions(defaultCacheSettings())
	caches := &readQueryCaches{
		local: appcache.NewSized(options[0], func(v usageDashboardSnapshot) int64 { return estimateReadCacheBytes(v, dashboardCacheBytes) }),
		proxy: appcache.NewSized(options[1], func(v usageDashboardSnapshot) int64 { return estimateReadCacheBytes(v, dashboardCacheBytes) }),
		pricing: appcache.NewSized(options[2], func(v map[string]*pricingstore.ModelPricingRecord) int64 {
			return estimateReadCacheBytes(v, metadataCacheBytes)
		}),
		availability: appcache.NewSized(options[3], func(v EndpointAvailabilityData) int64 { return estimateReadCacheBytes(v, metadataCacheBytes) }),
	}
	a.readCaches.CompareAndSwap(nil, caches)
	return a.readCaches.Load()
}

func (c *readQueryCaches) managed() []appcache.ManagedCache {
	return []appcache.ManagedCache{c.local, c.proxy, c.pricing, c.availability}
}

func readCacheOptions(settings cacheSettings) []appcache.Options {
	names := []string{cacheNameDashboardLocal, cacheNameDashboardProxy, cacheNameDashboardPricing, cacheNameEndpointAvailability}
	result := make([]appcache.Options, len(names))
	for i, name := range names {
		option := cacheOptions(name, settings)
		option.MaxEntries = min(option.MaxEntries, 24)
		option.StaleOnError = false
		option.MaxBytes = metadataCacheBytes
		if i < 2 {
			option.MaxBytes = dashboardCacheBytes
		}
		if i != 2 && (option.TTL <= 0 || option.TTL > 30*time.Second) {
			option.TTL = 30 * time.Second
		}
		result[i] = option
	}
	return result
}

// These snapshots contain acyclic metadata only. Count backing arrays, map
// overhead and string payloads without allocating a serialized copy. Shared
// pointers are counted once; repeated strings deliberately overestimate usage.
func estimateReadCacheBytes(value any, ceiling int64) int64 {
	var bytes int64
	seen := make(map[uintptr]bool)
	var visit func(reflect.Value)
	visit = func(v reflect.Value) {
		if !v.IsValid() || bytes > ceiling {
			return
		}
		switch v.Kind() {
		case reflect.String:
			bytes += int64(v.Len())
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			bytes += int64(v.Type().Elem().Size())
			visit(v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				visit(v.Field(i))
			}
		case reflect.Slice:
			bytes += int64(v.Cap()) * int64(v.Type().Elem().Size())
			for i := 0; i < v.Len() && bytes <= ceiling; i++ {
				visit(v.Index(i))
			}
		case reflect.Map:
			bytes += 64 + int64(v.Len())*(48+int64(v.Type().Key().Size()+v.Type().Elem().Size()))
			it := v.MapRange()
			for bytes <= ceiling && it.Next() {
				visit(it.Key())
				visit(it.Value())
			}
		case reflect.Array:
			for i := 0; i < v.Len() && bytes <= ceiling; i++ {
				visit(v.Index(i))
			}
		}
	}
	root := reflect.ValueOf(value)
	if root.IsValid() {
		bytes = int64(root.Type().Size())
		visit(root)
	}
	return bytes
}
