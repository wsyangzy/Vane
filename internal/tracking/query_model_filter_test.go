package tracking

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestMultipleModelFiltersAcrossActiveAndArchive(t *testing.T) {
	tracker, _ := newHistoryArchiveTestTracker(t)
	ctx := context.Background()
	oldTime := previousHistoryMonthStart(tracker.now()).Add(12 * time.Hour)
	currentTime := tracker.now()
	for _, when := range []time.Time{oldTime, currentTime} {
		for i, model := range []string{"model-a", "model-b", "other", "custom,model"} {
			id := fmt.Sprintf("models-%s-%d", when.Format("2006-01"), i)
			insertHistoryRequest(t, tracker, id, when.Format("2006-01-02 15:04:05"), "completed", "codex", 0)
			tier := "default"
			if when.Equal(currentTime) {
				tier = "priority"
			}
			if _, err := tracker.GetWriteDB().Exec("UPDATE request_logs SET model_name = ?, service_tier = ? WHERE request_id = ?", model, tier, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, moved, err := tracker.archiveOldestClosedMonth(ctx); err != nil || !moved {
		t.Fatalf("archive moved=%v err=%v", moved, err)
	}
	start, end := oldTime.Add(-time.Hour), currentTime.Add(time.Hour)
	for _, tc := range []struct {
		name string
		opts QueryOptions
		want int
	}{
		{"multiple", QueryOptions{ModelNames: []string{" model-b ", "model-a", "model-a", ""}}, 4},
		{"legacy", QueryOptions{ModelName: "model-a"}, 2},
		{"empty", QueryOptions{ModelNames: []string{"", " "}}, 8},
		{"precedence", QueryOptions{ModelName: "other", ModelNames: []string{"model-a", "model-b"}}, 4},
		{"exact comma", QueryOptions{ModelNames: []string{"custom,model"}}, 2},
		{"missing", QueryOptions{ModelNames: []string{"unlisted"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.StartDate, opts.EndDate = &start, &end
			details, err := tracker.QueryRequestDetails(ctx, &opts)
			if err != nil || len(details) != tc.want {
				t.Fatalf("details len=%d want=%d err=%v", len(details), tc.want, err)
			}
			count, err := tracker.CountRequestDetails(ctx, &opts)
			if err != nil || count != tc.want {
				t.Fatalf("count=%d want=%d err=%v", count, tc.want, err)
			}
			totals, err := tracker.QueryUsageStatsTotals(ctx, &opts)
			if err != nil || totals.TotalRequests != int64(tc.want) || totals.TotalTokens != int64(tc.want*15) {
				t.Fatalf("totals=%+v want=%d err=%v", totals, tc.want, err)
			}
			summaries, err := tracker.QueryUsageSummary(ctx, &opts)
			summaryCount := 0
			for _, summary := range summaries {
				summaryCount += summary.RequestCount
			}
			if err != nil || summaryCount != tc.want/2 {
				t.Fatalf("archive summaries=%d want=%d err=%v", summaryCount, tc.want/2, err)
			}
			if tc.want > 0 {
				opts.Limit, opts.Offset = 1, 1
				page, total, err := tracker.QueryRequestDetailsWithHotPool(ctx, &opts)
				if err != nil || len(page) != 1 || total != int64(tc.want) || page[0].RequestID != details[1].RequestID {
					t.Fatalf("page=%+v total=%d want=%d err=%v", page, total, tc.want, err)
				}
			}
		})
	}
}

func TestMultipleModelFiltersHotPoolAndCacheIsolation(t *testing.T) {
	opts := &QueryOptions{ModelNames: []string{"model-a", "model-b"}, Provider: "codex"}
	for _, tc := range []struct {
		model, provider string
		match           bool
	}{
		{"model-a", "codex", true}, {"model-b", "codex", true}, {"other", "codex", false}, {"model-a", "claude", false},
	} {
		if got := activeRequestMatchesFilters(&ActiveRequest{ModelName: tc.model, Provider: tc.provider}, opts); got != tc.match {
			t.Fatalf("hot pool model=%s provider=%s got=%v", tc.model, tc.provider, got)
		}
	}
	a := &QueryOptions{ModelNames: []string{"model-a", "model-b"}}
	b := &QueryOptions{ModelNames: []string{"model-b", "model-a", "model-b"}}
	c := &QueryOptions{ModelNames: []string{"model-a,model-b"}}
	if queryOptsCacheKey(a) != queryOptsCacheKey(b) || queryOptsCacheKey(a) == queryOptsCacheKey(c) {
		t.Fatal("count cache must normalize sets and distinguish exact names from multiple names")
	}
	if !reflect.DeepEqual(a.ModelNames, []string{"model-a", "model-b"}) {
		t.Fatal("cache key must not mutate caller selection")
	}
	tracker, _ := newHistoryArchiveTestTracker(t)
	month := previousHistoryMonthStart(tracker.now()).Format("2006-01")
	if tracker.archiveScopedCacheKey(month, a) != tracker.archiveScopedCacheKey(month, b) || tracker.archiveScopedCacheKey(month, a) == tracker.archiveScopedCacheKey(month, c) {
		t.Fatal("archive stats cache must isolate different model selections")
	}
	counter := NewDBRequestCounter()
	if err := counter.Init(context.Background(), tracker.GetReadDB()); err != nil {
		t.Fatal(err)
	}
	if _, ok := counter.Get(a); ok {
		t.Fatal("unfiltered counter cannot answer a model filter")
	}
}
