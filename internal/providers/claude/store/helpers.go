package store

import (
	"strings"
	"time"
)

func parseSQLiteDateTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t
	}

	layouts := []string{
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}

	if strings.Contains(value, " ") {
		tail := ""
		if len(value) > 19 {
			tail = value[19:]
		}
		if strings.Contains(tail, "+") || strings.Contains(tail, "-") || strings.Contains(tail, "Z") {
			if t, err := time.Parse(time.RFC3339Nano, strings.Replace(value, " ", "T", 1)); err == nil {
				return t
			}
		}
	}

	return time.Time{}
}

func nullInt64IfZero(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func positiveInt64OrZero(value int64) int64 {
	if value <= 0 {
		return 0
	}
	return value
}
