package service

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type QuotaCredentialResolver interface {
	Resolve(context.Context, string, string) (accounts.Credential, error)
}

type RoutingQuotaReader struct {
	resolver QuotaCredentialResolver
	client   *http.Client
	mu       sync.Mutex
	flights  map[string]*codexQuotaFlight
}

type codexQuotaFlight struct {
	done      chan struct{}
	snapshots []routing.QuotaSnapshot
	err       error
}

func NewRoutingQuotaReader(resolver QuotaCredentialResolver, client *http.Client) *RoutingQuotaReader {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &RoutingQuotaReader{resolver: resolver, client: &copy, flights: map[string]*codexQuotaFlight{}}
}

func (*RoutingQuotaReader) Provider() string { return "codex" }

func (r *RoutingQuotaReader) Read(ctx context.Context, ref routing.CredentialRef, model string) ([]routing.QuotaSnapshot, error) {
	key := ref.IdentityHash()
	r.mu.Lock()
	f := r.flights[key]
	if f == nil {
		if len(r.flights) >= 64 {
			r.mu.Unlock()
			return nil, routing.ErrQuotaBusy
		}
		f = &codexQuotaFlight{done: make(chan struct{})}
		r.flights[key] = f
		go func() {
			readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			result, err := r.read(readCtx, ref, model)
			r.mu.Lock()
			defer r.mu.Unlock()
			f.snapshots, f.err = result, err
			delete(r.flights, key)
			close(f.done)
		}()
	}
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		data, _ := json.Marshal(f.snapshots)
		var result []routing.QuotaSnapshot
		_ = json.Unmarshal(data, &result)
		for i := range result {
			result[i].ChannelID, result[i].GroupID, result[i].AccountIDHash = ref.ChannelID, ref.GroupID, ref.IdentityHash()
		}
		return result, f.err
	}
}

func (r *RoutingQuotaReader) read(ctx context.Context, ref routing.CredentialRef, model string) ([]routing.QuotaSnapshot, error) {
	if r.resolver == nil || ref.Provider != "codex" || ref.AccountID == "" {
		return nil, routing.ErrQuotaCredentials
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	credential, err := r.resolver.Resolve(ctx, "codex", ref.AccountID)
	if err != nil || !accounts.ValidToken(credential.AccessToken) || !accounts.ValidToken(credential.AccountID) {
		return nil, routing.ErrQuotaCredentials
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, routing.ErrQuotaUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("ChatGPT-Account-Id", credential.AccountID)
	request.Header.Set("Accept", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, routing.SafeQuotaError(ctx.Err())
		}
		return nil, routing.ErrQuotaUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, routing.ErrQuotaUnknown
	}
	defer clear(body)
	now := time.Now().UTC()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, routing.ErrQuotaCredentials
	}
	if response.StatusCode != http.StatusOK {
		if snapshots := ParseResponseQuota(response.StatusCode, response.Header, body, ref, model, now); len(snapshots) > 0 {
			return snapshots, nil
		}
		return nil, routing.ErrQuotaUnavailable
	}
	snapshots := parseCodexUsage(body, ref, now)
	if len(snapshots) == 0 {
		return nil, routing.ErrQuotaUnknown
	}
	return snapshots, nil
}

type codexQuotaWindow struct {
	UsedPercent *float64 `json:"used_percent"`
	ResetAt     int64    `json:"reset_at"`
	ResetAfter  int64    `json:"reset_after_seconds"`
}
type codexQuotaLimits struct {
	Primary   *codexQuotaWindow `json:"primary_window"`
	Secondary *codexQuotaWindow `json:"secondary_window"`
}

func codexSnapshot(ref routing.CredentialRef, model, window, source, confidence string, used float64, reset *time.Time, now time.Time) routing.QuotaSnapshot {
	used = math.Min(used, 100)
	expires := now.Add(5 * time.Minute)
	if reset != nil && reset.Before(expires) {
		expires = *reset
	}
	return routing.QuotaSnapshot{Provider: "codex", ChannelID: ref.ChannelID, GroupID: ref.GroupID, AccountIDHash: ref.IdentityHash(), ModelScope: model, WindowName: window, UsedPercent: &used, ResetAt: reset, ObservedAt: now, ExpiresAt: expires, Source: source, Confidence: confidence}
}

func parseCodexUsage(body []byte, ref routing.CredentialRef, now time.Time) []routing.QuotaSnapshot {
	var payload struct {
		Limits     codexQuotaLimits `json:"rate_limit"`
		Additional []struct {
			Model   string           `json:"model"`
			Feature string           `json:"metered_feature"`
			Limits  codexQuotaLimits `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	var result []routing.QuotaSnapshot
	appendLimits := func(limits codexQuotaLimits, scope string) {
		for _, window := range []struct {
			name string
			data *codexQuotaWindow
		}{{"primary", limits.Primary}, {"secondary", limits.Secondary}} {
			q := window.data
			if q == nil || q.UsedPercent == nil || math.IsNaN(*q.UsedPercent) || math.IsInf(*q.UsedPercent, 0) || *q.UsedPercent < 0 {
				continue
			}
			var reset *time.Time
			at, known := subscription.ResetTime(strconv.FormatInt(q.ResetAt, 10), now, false)
			if !known && q.ResetAfter > 0 {
				at, known = subscription.ResetTime(strconv.FormatInt(q.ResetAfter, 10), now, true)
			}
			if known {
				reset = &at
			}
			result = append(result, codexSnapshot(ref, scope, window.name, "provider_api", "exact", *q.UsedPercent, reset, now))
		}
	}
	appendLimits(payload.Limits, "*")
	for _, additional := range payload.Additional {
		scope := additional.Model
		if scope == "" && additional.Feature != "" {
			// A feature label is not a model identifier. Keep its isolation rather
			// than applying an unfamiliar feature's limit to every model.
			scope = "feature:" + additional.Feature
		}
		if scope != "" {
			appendLimits(additional.Limits, scope)
		}
	}
	return result
}

// ParseResponseQuota learns only structured provider evidence. It never returns
// upstream messages, raw bodies, credentials or health/cooldown mutations.
func ParseResponseQuota(status int, headers http.Header, body []byte, ref routing.CredentialRef, model string, now time.Time) []routing.QuotaSnapshot {
	var result []routing.QuotaSnapshot
	for _, window := range []string{"primary", "secondary"} {
		prefix := "x-codex-" + window
		used, err := strconv.ParseFloat(headers.Get(prefix+"-used-percent"), 64)
		if err != nil || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 {
			continue
		}
		at, known := subscription.ResetTime(headers.Get(prefix+"-reset-at"), now, false)
		if !known {
			at, known = subscription.ResetTime(headers.Get(prefix+"-reset-after-seconds"), now, true)
		}
		if !known && used >= 100 {
			at, known = subscription.ResetTime(headers.Get("Retry-After"), now, true)
		}
		var reset *time.Time
		if known {
			reset = &at
		}
		result = append(result, codexSnapshot(ref, "*", window, "response_header", "learned", used, reset, now))
	}
	for _, pause := range subscription.QuotaPauses(status, headers, body, subscription.Binding{GroupID: ref.GroupID}, model, now) {
		if pause.Model == "" || !strings.Contains(pause.Source, "structured") {
			continue
		}
		var reset *time.Time
		if pause.ResetKnown {
			at := pause.ResumeNotBefore
			reset = &at
		}
		q := codexSnapshot(ref, pause.Model, "quota_error", "structured_error", "learned", 100, reset, now)
		if reset == nil {
			q.ExpiresAt = now.Add(time.Minute)
		}
		result = append(result, q)
	}
	return result
}
