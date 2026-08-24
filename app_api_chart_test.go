package main

import (
	"testing"
	"time"

	"ai-relay-forwarder/internal/tracking"
)

func TestBuildEndpointAvailabilityDataFillsGapsAndWeightsLatency(t *testing.T) {
	firstSlice := time.Date(2026, 8, 24, 0, 0, 0, 0, time.Local)
	const hours = 24

	byKey := map[string]tracking.EndpointAvailabilityBucket{
		"2026-08-24 03": {Key: "2026-08-24 03", Total: 10, Success: 9, Failed: 1, AvgFirstByteMs: 100},
		"2026-08-24 07": {Key: "2026-08-24 07", Total: 90, Success: 90, Failed: 0, AvgFirstByteMs: 200},
	}

	data := buildEndpointAvailabilityData(byKey, firstSlice, hours)

	// 没有流量的小时必须补齐为空档：省略会让时间轴不再等距
	if len(data.Slices) != hours {
		t.Fatalf("slices len = %d, want %d", len(data.Slices), hours)
	}
	withData := 0
	for _, slice := range data.Slices {
		if slice.HasData {
			withData++
		}
	}
	if withData != 2 {
		t.Fatalf("slices with data = %d, want 2", withData)
	}
	if data.Slices[0].HasData || data.Slices[23].HasData {
		t.Fatalf("empty hours should stay empty: %+v / %+v", data.Slices[0], data.Slices[23])
	}

	if got := data.Slices[3].Availability; got != 90 {
		t.Fatalf("slice 3 availability = %v, want 90", got)
	}
	if got := data.Slices[7].Availability; got != 100 {
		t.Fatalf("slice 7 availability = %v, want 100", got)
	}
	if data.Slices[3].Label != "08/24 03:00" {
		t.Fatalf("slice 3 label = %q, want 08/24 03:00", data.Slices[3].Label)
	}

	if data.Total != 100 || data.Success != 99 || data.Failed != 1 {
		t.Fatalf("totals = %+v, want total=100 success=99 failed=1", data)
	}
	if data.Availability != 99 {
		t.Fatalf("window availability = %v, want 99", data.Availability)
	}
	// 按请求数加权：(100×10 + 200×90) / 100 = 190。
	// 若错误地对两个分片均值直接平均会得到 150。
	if diff := data.AvgFirstByteMs - 190; diff > 0.001 || diff < -0.001 {
		t.Fatalf("window AvgFirstByteMs = %v, want 190", data.AvgFirstByteMs)
	}
}

func TestBuildEndpointAvailabilityDataWithoutTrafficReportsNoData(t *testing.T) {
	firstSlice := time.Date(2026, 8, 24, 0, 0, 0, 0, time.Local)
	data := buildEndpointAvailabilityData(map[string]tracking.EndpointAvailabilityBucket{}, firstSlice, 24)

	if len(data.Slices) != 24 {
		t.Fatalf("slices len = %d, want 24", len(data.Slices))
	}
	if data.HasData || data.Total != 0 {
		t.Fatalf("empty window should report no data: %+v", data)
	}
	// 可用率保持 0 而不是 100：没有样本不等于全部成功
	if data.Availability != 0 {
		t.Fatalf("availability = %v, want 0", data.Availability)
	}
}
