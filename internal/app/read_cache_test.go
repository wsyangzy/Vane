package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
	pricingservice "github.com/wsyangzy/Vane/internal/pricing/service"
	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

func TestDashboardCacheSharesPagesAndSortingAndTracksProviderChanges(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()
	a := &App{usageTracker: tracker}
	a.setupCacheRegistry()
	tracker.SetUsageCacheInvalidationCallback(a.invalidateUsageQueryCaches)
	stamp := time.Now().Add(-time.Minute)
	insert := func(id string, input int) {
		t.Helper()
		_, err := tracker.GetDB().Exec(`INSERT INTO request_logs(request_id,start_time,status,provider,environment,model_name,input_tokens,output_tokens) VALUES(?,?,'completed','codex','release','model',?,20)`, id, stamp, input)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		insert(fmt.Sprint(i), 100*(i+1))
	}
	q := UsageDashboardQuery{Source: "proxy", Provider: "codex", Period: "all", PageSize: 1, Sort: "tokens"}
	first, err := a.GetUsageDashboard(q)
	if err != nil {
		t.Fatal(err)
	}
	q.Page = 2
	second, err := a.GetUsageDashboard(q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Rows[0].ID != "2" || second.Rows[0].ID != "1" || !reflect.DeepEqual(first.Summary, second.Summary) {
		t.Fatalf("pagination changed totals or order: %+v %+v", first, second)
	}
	var wait sync.WaitGroup
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			selected := q
			selected.Sort = []string{"recent", "cost", "active", "tokens"}[i%4]
			if _, err := a.GetUsageDashboard(selected); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wait.Wait()
	cache := a.ensureReadCaches().proxy
	if cache.Stats().Misses != 1 {
		t.Fatalf("pages reloaded the source: %+v", cache.Stats())
	}
	tracker.NotifyUsageDataChanged(appcache.Change{Providers: []string{"claude"}, Start: stamp, End: stamp})
	if cache.Stats().Size != 1 {
		t.Fatal("unrelated provider invalidated dashboard")
	}
	insert("new", 1000)
	tracker.NotifyUsageDataChanged(appcache.Change{Providers: []string{"codex"}, Start: stamp, End: stamp})
	updated, err := a.GetUsageDashboard(q)
	if err != nil || updated.Summary.Requests != 4 || cache.Stats().Misses != 2 {
		t.Fatalf("write did not refresh: %+v %v", updated.Summary, err)
	}
	cache.SetOptions(appcache.Options{Enabled: true, MaxBytes: 1})
	complete, err := a.GetUsageDashboard(q)
	if err != nil || complete.RowCount != 4 || cache.Stats().Size != 0 {
		t.Fatal("budget altered query completeness")
	}
}

func TestLocalDashboardRefreshAndPricingRevision(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "absent"))
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "sessions", "a.jsonl")
	stamp := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	line := func(id string) string {
		return fmt.Sprintf("{\"type\":\"token_usage_record\",\"timestamp\":%q,\"payload\":{\"response_id\":%q,\"usage\":{\"input_tokens\":100,\"cached_input_tokens\":70,\"output_tokens\":20}}}\n", stamp, id)
	}
	data := "{\"type\":\"session_meta\",\"payload\":{\"id\":\"session-a\",\"cwd\":\"D:/work/Vane\"}}\n{\"type\":\"turn_context\",\"payload\":{\"model\":\"model\"}}\n" + line("first")
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()
	pricing := pricingservice.NewModelPricingService(pricingstore.NewSQLiteModelPricingStore(tracker.GetDB()))
	price, err := pricing.CreatePricing(context.Background(), &pricingstore.ModelPricingRecord{ModelName: "model", Provider: "codex", InputPrice: 1, OutputPrice: 2, CacheReadPrice: .1})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{modelPricingService: pricing, localUsageReader: sessionusage.NewPersistentReader(filepath.Join(t.TempDir(), "usage.sqlite"))}
	a.setupCacheRegistry()
	q := UsageDashboardQuery{Source: "local", Period: "all"}
	first, err := a.GetUsageDashboard(q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetUsageDashboard(q); err != nil {
		t.Fatal(err)
	}
	if a.ensureReadCaches().local.Stats().Hits != 1 {
		t.Fatal("local snapshot not reused")
	}
	price.InputPrice = 10
	if err := pricing.UpdatePricing(context.Background(), price); err != nil {
		t.Fatal(err)
	}
	repriced, err := a.GetUsageDashboard(q)
	if err != nil || repriced.Summary.CostUSD <= first.Summary.CostUSD || repriced.Summary.Tokens != first.Summary.Tokens {
		t.Fatalf("pricing snapshot stale: %+v %v", repriced.Summary, err)
	}
	if err := os.WriteFile(file, []byte(data+line("second")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ClearCache(ClearCacheInput{Names: []string{cacheNameDashboardLocal}}); err != nil {
		t.Fatal(err)
	}
	updated, err := a.GetUsageDashboard(q)
	if err != nil || updated.Summary.Requests != 2 || updated.Summary.Tokens != 100 {
		t.Fatalf("manual refresh missed appended usage: %+v %v", updated.Summary, err)
	}
	if _, err := a.ClearCache(ClearCacheInput{Names: []string{sessionusage.CacheName}}); err != nil {
		t.Fatal(err)
	}
	if a.ensureReadCaches().local.Stats().Size != 0 {
		t.Fatal("index clearing retained dependent dashboard")
	}
}

func TestEndpointAvailabilityCacheInvalidatesWithRequestChanges(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()
	a := &App{usageTracker: tracker}
	a.setupCacheRegistry()
	tracker.SetUsageCacheInvalidationCallback(a.invalidateUsageQueryCaches)
	stamp := time.Now().Add(-time.Minute)
	insert := func(id, status string) {
		t.Helper()
		_, err := tracker.GetDB().Exec(`INSERT INTO request_logs(request_id,start_time,status,provider,environment,channel,endpoint_name) VALUES(?,?,?,'claude','release','channel','endpoint')`, id, stamp, status)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("a", "completed")
	first, err := a.GetEndpointAvailability("claude", "channel", "endpoint", 24)
	if err != nil || first.Total != 1 {
		t.Fatalf("initial=%+v %v", first, err)
	}
	first.Slices[0].Label = "caller mutation"
	second, err := a.GetEndpointAvailability("claude", "channel", "endpoint", 24)
	if err != nil || second.Slices[0].Label == "caller mutation" || a.ensureReadCaches().availability.Stats().Hits != 1 {
		t.Fatal("endpoint cache missed or exposed shared slices")
	}
	insert("b", "failed")
	tracker.NotifyUsageDataChanged(appcache.Change{Providers: []string{"claude"}, Start: stamp, End: stamp})
	updated, err := a.GetEndpointAvailability("claude", "channel", "endpoint", 24)
	if err != nil || updated.Total != 2 || updated.Success != 1 {
		t.Fatalf("availability stale: %+v %v", updated, err)
	}
}

func BenchmarkUsageDashboardCachedPagination(b *testing.B) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	q, start, end, _ := dashboardRange(UsageDashboardQuery{Source: "local", Period: "30d", Sort: "tokens"}, now)
	snapshot, err := buildUsageDashboardSnapshot(q, start, end, now, func(add func(dashboardRecord), _ func(sessionusage.Activity)) error {
		for i := 0; i < 20000; i++ {
			provider := "codex"
			if i%2 != 0 {
				provider = "claude"
			}
			add(dashboardRecord{Call: sessionusage.Call{
				ID: fmt.Sprintf("call-%d", i), Provider: provider, Model: "fixture-model",
				Session: fmt.Sprintf("session-%d", i%240), Project: "fixture-project", Title: "会话用量统计测试",
				At: start.Add(time.Duration(i%720) * time.Hour), Tokens: sessionusage.Tokens{Input: 1000, Output: int64(i%500 + 1), CacheRead: 500},
			}, channel: "fixture-channel", group: "fixture-group", status: "completed", priced: true, cost: .01, duration: 1000})
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	cache := appcache.NewSized(readCacheOptions(defaultCacheSettings())[0], func(v usageDashboardSnapshot) int64 { return estimateReadCacheBytes(v, dashboardCacheBytes) })
	cache.Set("query", snapshot)
	if cache.Stats().Size != 1 {
		b.Fatal("benchmark snapshot does not fit actual budget")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value, ok := cache.Get("query")
		if !ok {
			b.Fatal("cache miss")
		}
		q.Page = i%12 + 1
		_ = value.page(q, now.Location())
	}
}
