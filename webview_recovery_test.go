package main

import (
	"testing"
	"time"
)

// TestWebviewIdleTrackerThreshold 阈值判定：未达阈值不打扰用户页面状态，达到则重建。
func TestWebviewIdleTrackerThreshold(t *testing.T) {
	const threshold = 10 * time.Minute
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		hidden time.Duration
		want   bool
	}{
		{"隐藏 1 秒", time.Second, false},
		{"隐藏 9 分钟", 9 * time.Minute, false},
		{"恰好达到阈值", threshold, true},
		{"隐藏 1 小时", time.Hour, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &webviewIdleTracker{threshold: threshold}
			tracker.markHidden(base)
			if got := tracker.shouldReload(base.Add(tc.hidden)); got != tc.want {
				t.Errorf("隐藏 %v 后 shouldReload() = %v, want %v", tc.hidden, got, tc.want)
			}
		})
	}
}

// TestWebviewIdleTrackerIdempotent 一次隐藏只触发一次重建。
// 从托盘恢复与取消最小化可能连续派发多个事件，重复重建会白白丢失页面状态。
func TestWebviewIdleTrackerIdempotent(t *testing.T) {
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	tracker := &webviewIdleTracker{threshold: 10 * time.Minute}

	tracker.markHidden(base)
	restoredAt := base.Add(time.Hour)

	if !tracker.shouldReload(restoredAt) {
		t.Fatal("首次恢复应触发重建")
	}
	if tracker.shouldReload(restoredAt) {
		t.Error("同一次隐藏的第二个恢复事件不应重复触发重建")
	}
}

// TestWebviewIdleTrackerNotHidden 未曾隐藏时恢复事件不应触发重建。
func TestWebviewIdleTrackerNotHidden(t *testing.T) {
	tracker := &webviewIdleTracker{threshold: 10 * time.Minute}
	if tracker.shouldReload(time.Now()) {
		t.Error("从未进入隐藏态时不应触发重建")
	}
}

// TestWebviewIdleTrackerKeepsEarliestHidden 连续的隐藏事件应保留最早的时刻。
// 隐藏到托盘与最小化会各派发一个事件，若被后到的事件覆盖则会低估隐藏时长。
func TestWebviewIdleTrackerKeepsEarliestHidden(t *testing.T) {
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	tracker := &webviewIdleTracker{threshold: 10 * time.Minute}

	tracker.markHidden(base)
	tracker.markHidden(base.Add(20 * time.Minute)) // 后到的事件不应覆盖

	// 以最早时刻计算，隐藏了 15 分钟，超过阈值
	if !tracker.shouldReload(base.Add(15 * time.Minute)) {
		t.Error("应以最早的隐藏时刻计算时长")
	}
}

// TestWebviewIdleTrackerReusable 恢复后再次隐藏应能重新计时。
func TestWebviewIdleTrackerReusable(t *testing.T) {
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	tracker := &webviewIdleTracker{threshold: 10 * time.Minute}

	// 第一轮：短暂隐藏，不触发
	tracker.markHidden(base)
	if tracker.shouldReload(base.Add(time.Minute)) {
		t.Fatal("短暂隐藏不应触发重建")
	}

	// 第二轮：长时间隐藏，应触发
	second := base.Add(2 * time.Minute)
	tracker.markHidden(second)
	if !tracker.shouldReload(second.Add(time.Hour)) {
		t.Error("恢复后再次长时间隐藏应重新计时并触发重建")
	}
}
