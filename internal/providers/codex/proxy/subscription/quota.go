package subscription

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ResetTime accepts an epoch in seconds or an HTTP date. Milliseconds,
// expired/extreme values and malformed input remain unknown, never guessed.
func ResetTime(raw string, now time.Time, retryAfter bool) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsInf(seconds, 0) && !math.IsNaN(seconds) && seconds >= 0 {
		var value time.Time
		if retryAfter {
			if seconds > 31*24*3600 {
				return time.Time{}, false
			}
			value = now.Add(time.Duration(seconds * float64(time.Second)))
		} else {
			if seconds > 1e11 {
				return time.Time{}, false
			}
			value = time.Unix(int64(seconds), 0)
		}
		return value, value.After(now) && value.Before(now.Add(31*24*time.Hour))
	}
	value, err := http.ParseTime(raw)
	return value, err == nil && value.After(now) && value.Before(now.Add(31*24*time.Hour))
}

func QuotaPauses(status int, h http.Header, body []byte, b Binding, model string, now time.Time) []Pause {
	// A protocol error inside HTTP 200 is still quota evidence. Keep the HTTP
	// status untouched elsewhere; successful 200 headers alone cannot pause.
	if status == http.StatusOK {
		var frame struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &frame)
		code := frame.Error.Code
		if code == "" {
			code = frame.Error.Type
		}
		switch code {
		case "usage_limit_reached", "usage_limit_exceeded", "quota_exceeded", "insufficient_quota", "model_usage_limit_reached":
			status = http.StatusTooManyRequests
		default:
			return nil
		}
	}
	if status != http.StatusTooManyRequests {
		return nil
	}
	var result []Pause
	for _, window := range []string{"primary", "secondary"} {
		prefix := "x-codex-" + window
		used, e := strconv.ParseFloat(h.Get(prefix+"-used-percent"), 64)
		if e != nil || math.IsNaN(used) || math.IsInf(used, 0) || used < 100 {
			continue
		}
		resume, known := ResetTime(h.Get(prefix+"-reset-at"), now, false)
		if !known {
			resume, known = ResetTime(h.Get(prefix+"-reset-after-seconds"), now, true)
		}
		if !known {
			resume, known = ResetTime(h.Get("Retry-After"), now, true)
		}
		if !known {
			resume = now.Add(time.Minute)
		}
		result = append(result, Pause{GroupID: b.GroupID, Version: b.Version, Model: "*", Reason: "quota_exhausted", Source: prefix, ObservedAt: now, ResumeNotBefore: resume, ResetKnown: known, Revision: 1})
	}
	if len(result) > 0 {
		return mergePauses(result)
	}
	var p struct {
		Error struct {
			Code     string `json:"code"`
			Type     string `json:"type"`
			ResetsAt int64  `json:"resets_at"`
			ResetsIn int64  `json:"resets_in_seconds"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &p)
	code := p.Error.Code
	if code == "" {
		code = p.Error.Type
	}
	scope := model
	switch code {
	case "usage_limit_reached", "usage_limit_exceeded", "quota_exceeded", "insufficient_quota":
		scope = "*"
	case "model_usage_limit_reached":
	default:
		return nil
	}
	resume, known := ResetTime(h.Get("Retry-After"), now, true)
	if !known && p.Error.ResetsAt > 0 {
		resume, known = ResetTime(strconv.FormatInt(p.Error.ResetsAt, 10), now, false)
	}
	if !known && p.Error.ResetsIn > 0 {
		resume, known = ResetTime(strconv.FormatInt(p.Error.ResetsIn, 10), now, true)
	}
	if !known {
		resume = now.Add(time.Minute)
	}
	return []Pause{{GroupID: b.GroupID, Version: b.Version, Model: scope, Reason: code, Source: "structured_quota_error", ObservedAt: now, ResumeNotBefore: resume, ResetKnown: known, Revision: 1}}
}

func mergePauses(pauses []Pause) []Pause {
	byModel := map[string]Pause{}
	for _, p := range pauses {
		prior, exists := byModel[p.Model]
		if !exists || p.ResumeNotBefore.After(prior.ResumeNotBefore) {
			byModel[p.Model] = p
		}
	}
	result := make([]Pause, 0, len(byModel))
	for _, p := range byModel {
		result = append(result, p)
	}
	return result
}
