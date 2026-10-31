package app

import (
	"testing"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

func TestLocalUsageIndexParticipatesInCacheStatusAndClear(t *testing.T) {
	a := &App{}
	status, err := a.GetCacheStatus()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range status.Caches {
		if item.Name == sessionusage.CacheName {
			found = true
			if !item.Enabled || item.Fixed {
				t.Fatalf("index status=%+v", item)
			}
		}
	}
	if !found {
		t.Fatal("local index absent from managed caches")
	}
	before := a.usageReader().Stats().Invalidations
	result, err := a.ClearCache(ClearCacheInput{Names: []string{sessionusage.CacheName}})
	if err != nil || result.ClearedCaches != 1 || len(result.UnknownNames) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if a.usageReader().Stats().Invalidations != before+1 {
		t.Fatal("registry did not reach reader")
	}
	a.usageReader().SetOptions(appcache.Options{Enabled: false})
	status, _ = a.GetCacheStatus()
	for _, item := range status.Caches {
		if item.Name == sessionusage.CacheName && item.Enabled {
			t.Fatal("disabled index reported enabled")
		}
	}
}
