package app

import (
	"testing"
	"time"
)

func TestCompletionQueriesDoNotApplyTodayStartDate(t *testing.T) {
	opts := buildRequestQueryOptions(RequestQueryParams{CompletedSince: "2026-11-17T00:46:12Z", CompletedBefore: "2026-11-17T00:46:14Z", Status: "failed"}, "Asia/Hong_Kong", 100, 0)
	if opts.StartDate != nil || opts.EndDate != nil {
		t.Fatal("completion query must include requests started on earlier days")
	}
	if opts.CompletedSince == nil || opts.CompletedBefore == nil || opts.CompletedBefore.Sub(*opts.CompletedSince) != 2*time.Second {
		t.Fatalf("invalid completion window: %#v", opts)
	}
}

func TestCompletionQueryRejectsInvalidOrReversedCursors(t *testing.T) {
	for _, params := range []RequestQueryParams{
		{CompletedSince: "invalid"},
		{CompletedSince: "2026-11-17T00:00:00.123Z"},
		{CompletedSince: "2026-11-17T00:00:02Z", CompletedBefore: "2026-11-17T00:00:01Z"},
	} {
		if _, err := (&App{}).GetRequests(params); err == nil {
			t.Fatalf("accepted invalid completion cursor: %#v", params)
		}
	}
}
