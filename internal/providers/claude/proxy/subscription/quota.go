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
	// HTTP 200 may carry an explicit SSE/JSON quota error. This only changes
	// availability classification, never the recorded or forwarded HTTP status.
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
		case "quota_exceeded", "usage_limit_exceeded", "credit_balance_exhausted":
			status = http.StatusTooManyRequests
		default:
			return nil
		}
	}
	if status != http.StatusTooManyRequests {
		return nil
	}
	var result []Pause
	for _, window := range []struct{ name, model string }{{"5h", "*"}, {"7d", "*"}, {"7d-opus", "opus"}, {"7d-sonnet", "sonnet"}} {
		if window.model != "*" && !strings.Contains(strings.ToLower(model), window.model) {
			continue
		}
		prefix := "anthropic-ratelimit-unified-" + window.name
		used, usedErr := strconv.ParseFloat(h.Get(prefix+"-utilization"), 64)
		if strings.ToLower(h.Get(prefix+"-status")) != "rejected" && !(usedErr == nil && !math.IsNaN(used) && !math.IsInf(used, 0) && used >= 1) {
			continue
		}
		resume, known := ResetTime(h.Get(prefix+"-reset"), now, false)
		if !known {
			resume, known = ResetTime(h.Get("Retry-After"), now, true)
		}
		if !known {
			resume = now.Add(time.Minute)
		}
		scopeModel := "*"
		if window.model != "*" {
			scopeModel = "family:" + window.model
		}
		result = append(result, Pause{GroupID: b.GroupID, Version: b.Version, Model: scopeModel, Reason: "quota_exhausted", Source: prefix, ObservedAt: now, ResumeNotBefore: resume, ResetKnown: known, Revision: 1})
	}
	if len(result) > 0 {
		return mergePauses(result)
	}
	// Some clients expose only the rejected representative window. Unknown
	// claims stay unclassified instead of extending another model's quota.
	if strings.EqualFold(h.Get("anthropic-ratelimit-unified-status"), "rejected") {
		claim := h.Get("anthropic-ratelimit-unified-representative-claim")
		for _, known := range []string{"5h", "7d", "7d-opus", "7d-sonnet"} {
			if claim == known {
				copy := h.Clone()
				copy.Del("anthropic-ratelimit-unified-status")
				copy.Set("anthropic-ratelimit-unified-"+claim+"-status", "rejected")
				copy.Set("anthropic-ratelimit-unified-"+claim+"-reset", h.Get("anthropic-ratelimit-unified-reset"))
				return QuotaPauses(status, copy, body, b, model, now)
			}
		}
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	code := payload.Error.Code
	if code == "" {
		code = payload.Error.Type
	}
	switch code {
	case "quota_exceeded", "usage_limit_exceeded", "credit_balance_exhausted":
		resume, known := ResetTime(h.Get("Retry-After"), now, true)
		if !known {
			resume = now.Add(time.Minute)
		}
		return []Pause{{GroupID: b.GroupID, Version: b.Version, Model: model, Reason: code, Source: "structured_quota_error", ObservedAt: now, ResumeNotBefore: resume, ResetKnown: known, Revision: 1}}
	}
	return nil
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
