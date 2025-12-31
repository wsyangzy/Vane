package monitor

import (
	"testing"
	"time"
)

func TestMetrics_GetChartDataForRequestHistoryWithBaseline_IncludesBaseline(t *testing.T) {
	m := NewMetrics()

	now := time.Now()
	m.RequestHistory = []RequestDataPoint{
		{Timestamp: now.Add(-12 * time.Minute), Total: 10, Successful: 9, Failed: 1},
		{Timestamp: now.Add(-5 * time.Minute), Total: 15, Successful: 13, Failed: 2},
		{Timestamp: now.Add(-1 * time.Minute), Total: 16, Successful: 14, Failed: 2},
	}

	// minutes=10 应返回：baseline(-12m) + (-5m) + (-1m)
	got := m.GetChartDataForRequestHistoryWithBaseline(10)
	if len(got) != 3 {
		t.Fatalf("expected 3 points, got %d", len(got))
	}
	if got[0].Total != 10 || got[1].Total != 15 || got[2].Total != 16 {
		t.Fatalf("unexpected totals: %+v", []int64{got[0].Total, got[1].Total, got[2].Total})
	}
}
