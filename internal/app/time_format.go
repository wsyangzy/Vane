package app

import (
	"fmt"
	"time"
)

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func parseTimeWithLocation(s string, loc *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	// 尝试各种格式
	formats := []struct {
		layout string
		useLoc bool // 是否使用 loc
	}{
		{"2006-01-02T15:04:05Z07:00", false}, // RFC3339
		{"2006-01-02T15:04:05+08:00", false}, // 带固定时区
		{"2006-01-02T15:04:05", true},        // 不带时区
		{"2006-01-02T15:04", true},           // 不带秒
		{"2006-01-02 15:04:05", true},        // 简单格式
		{"2006-01-02 15:04", true},           // 简单格式不带秒
		{"2006-01-02", true},                 // 仅日期
	}

	for _, f := range formats {
		var t time.Time
		var err error

		if f.useLoc && loc != nil {
			t, err = time.ParseInLocation(f.layout, s, loc)
		} else {
			t, err = time.Parse(f.layout, s)
		}

		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}
