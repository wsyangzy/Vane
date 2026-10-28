package routing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// CredentialRef identifies a configured account without making its credentials
// part of an API result, cache key, trace or persisted quota snapshot.
type CredentialRef struct {
	Provider      string `json:"provider"`
	ChannelID     int64  `json:"channel_id"`
	GroupID       int64  `json:"group_id"`
	AccountID     string `json:"-"`
	AccountIDHash string `json:"account_id_hash"`
}

func (r CredentialRef) IdentityHash() string {
	if r.AccountIDHash != "" {
		return r.AccountIDHash
	}
	sum := sha256.Sum256([]byte(r.Provider + "\x00" + r.AccountID))
	return hex.EncodeToString(sum[:])
}

type QuotaReader interface {
	Provider() string
	Read(context.Context, CredentialRef, string) ([]QuotaSnapshot, error)
}

// QuotaError values are safe to persist and display. Provider errors and CLI
// output are deliberately never wrapped into them.
type QuotaError string

func (e QuotaError) Error() string { return string(e) }

const (
	ErrQuotaUnknown        QuotaError = "quota_unknown"
	ErrQuotaUnavailable    QuotaError = "quota_refresh_failed"
	ErrQuotaCredentials    QuotaError = "quota_credentials_unavailable"
	ErrQuotaTimeout        QuotaError = "quota_timeout"
	ErrQuotaCLIUnavailable QuotaError = "quota_cli_unavailable"
	ErrQuotaCLIUnsupported QuotaError = "quota_response_learning_only"
	ErrQuotaIsolation      QuotaError = "quota_process_isolation_failed"
	ErrQuotaPersist        QuotaError = "quota_persist_failed"
	ErrQuotaBusy           QuotaError = "quota_refresh_busy"
)

func SafeQuotaError(err error) QuotaError {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrQuotaTimeout
	}
	var safe QuotaError
	if errors.As(err, &safe) {
		switch safe {
		case ErrQuotaUnknown, ErrQuotaUnavailable, ErrQuotaCredentials, ErrQuotaTimeout, ErrQuotaCLIUnavailable, ErrQuotaCLIUnsupported, ErrQuotaIsolation, ErrQuotaPersist, ErrQuotaBusy:
			return safe
		}
	}
	return ErrQuotaUnavailable
}

type quotaRefreshKey struct{ provider, account, model string }
type quotaRefreshEntry struct {
	snapshots []QuotaSnapshot
	attempted time.Time
	touched   time.Time
}
type quotaRefreshFlight struct {
	done      chan struct{}
	snapshots []QuotaSnapshot
	err       error
}

// QuotaRefresher merges reads per account/model. Refresh's active flag selects
// the 5-minute or 30-minute interval; force bypasses it but still joins a flight.
// It never changes business endpoint health or account cooling state.
type QuotaRefresher struct {
	mu      sync.Mutex
	readers map[string]QuotaReader
	cache   map[quotaRefreshKey]quotaRefreshEntry
	flights map[quotaRefreshKey]*quotaRefreshFlight
	persist func(context.Context, []QuotaSnapshot) error
	now     func() time.Time
}

func NewQuotaRefresher(readers []QuotaReader, persist func(context.Context, []QuotaSnapshot) error) *QuotaRefresher {
	r := &QuotaRefresher{readers: map[string]QuotaReader{}, cache: map[quotaRefreshKey]quotaRefreshEntry{}, flights: map[quotaRefreshKey]*quotaRefreshFlight{}, persist: persist, now: time.Now}
	for _, reader := range readers {
		if reader != nil {
			r.readers[reader.Provider()] = reader
		}
	}
	return r
}

func cloneQuota(snapshots []QuotaSnapshot) []QuotaSnapshot {
	data, _ := json.Marshal(snapshots)
	var result []QuotaSnapshot
	_ = json.Unmarshal(data, &result)
	return result
}

func quotaForRef(snapshots []QuotaSnapshot, ref CredentialRef) []QuotaSnapshot {
	result := cloneQuota(snapshots)
	for i := range result {
		result[i].Provider, result[i].ChannelID, result[i].GroupID = ref.Provider, ref.ChannelID, ref.GroupID
		result[i].AccountIDHash = ref.IdentityHash()
	}
	return result
}

// Seed restores only quota facts. No running requests or credentials survive a
// restart. Model-independent facts also satisfy a subsequent model-specific read.
func (r *QuotaRefresher) Seed(snapshots []QuotaSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, snapshot := range snapshots {
		if snapshot.AccountIDHash == "" || snapshot.Provider != "claude" && snapshot.Provider != "codex" {
			continue
		}
		key := quotaRefreshKey{snapshot.Provider, snapshot.AccountIDHash, snapshot.ModelScope}
		entry := r.cache[key]
		replaced := false
		for i, previous := range entry.snapshots {
			if previous.WindowName == snapshot.WindowName {
				if !previous.ObservedAt.After(snapshot.ObservedAt) {
					entry.snapshots[i] = cloneQuota([]QuotaSnapshot{snapshot})[0]
				}
				replaced = true
				break
			}
		}
		if !replaced && len(entry.snapshots) < 64 {
			entry.snapshots = append(entry.snapshots, cloneQuota([]QuotaSnapshot{snapshot})...)
		}
		if snapshot.ObservedAt.After(entry.attempted) {
			entry.attempted = snapshot.ObservedAt
		}
		entry.touched = r.now()
		r.cache[key] = entry
	}
	r.pruneLocked()
}

func (r *QuotaRefresher) Refresh(ctx context.Context, ref CredentialRef, model string, active, force bool) ([]QuotaSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if ref.AccountID == "" || ref.ChannelID <= 0 || ref.GroupID < 0 {
		return nil, ErrQuotaCredentials
	}
	key := quotaRefreshKey{ref.Provider, ref.IdentityHash(), model}
	r.mu.Lock()
	reader := r.readers[ref.Provider]
	if reader == nil {
		r.mu.Unlock()
		return nil, ErrQuotaUnknown
	}
	if flight := r.flights[key]; flight != nil {
		r.mu.Unlock()
		return waitQuotaRefresh(ctx, flight, ref)
	}
	now := r.now()
	entry, exists := r.cache[key]
	if !exists {
		entry, exists = r.cache[quotaRefreshKey{ref.Provider, ref.IdentityHash(), "*"}]
	}
	interval := 30 * time.Minute
	if active {
		interval = 5 * time.Minute
	}
	if !force && exists && now.Before(entry.attempted.Add(interval)) {
		entry.touched = now
		r.cache[key] = entry
		r.pruneLocked()
		r.mu.Unlock()
		return quotaForRef(entry.snapshots, ref), nil
	}
	if len(r.flights) >= 64 {
		r.mu.Unlock()
		return quotaForRef(entry.snapshots, ref), ErrQuotaBusy
	}
	flight := &quotaRefreshFlight{done: make(chan struct{})}
	r.flights[key] = flight
	r.mu.Unlock()
	go func() {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 35*time.Second)
		defer cancel()
		snapshots, err := reader.Read(readCtx, ref, model)
		now := r.now()
		if err != nil {
			err = SafeQuotaError(err)
			snapshots = quotaForRef(entry.snapshots, ref)
			for i := range snapshots {
				snapshots[i].Confidence, snapshots[i].Error = "stale", err.Error()
			}
			if len(snapshots) == 0 {
				snapshots = []QuotaSnapshot{{Provider: ref.Provider, ChannelID: ref.ChannelID, GroupID: ref.GroupID, AccountIDHash: ref.IdentityHash(), ModelScope: model, WindowName: "unknown", ObservedAt: now, ExpiresAt: now, Source: "provider_api", Confidence: "unknown", Error: err.Error()}}
			}
		} else {
			snapshots = quotaForRef(snapshots, ref)
		}
		if r.persist != nil && r.persist(readCtx, snapshots) != nil {
			err = ErrQuotaPersist
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.cache[key] = quotaRefreshEntry{cloneQuota(snapshots), now, now}
		r.pruneLocked()
		flight.snapshots, flight.err = snapshots, err
		delete(r.flights, key)
		close(flight.done)
	}()
	return waitQuotaRefresh(ctx, flight, ref)
}

func waitQuotaRefresh(ctx context.Context, flight *quotaRefreshFlight, ref CredentialRef) ([]QuotaSnapshot, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return quotaForRef(flight.snapshots, ref), flight.err
	}
}

func (r *QuotaRefresher) pruneLocked() {
	for len(r.cache) > 2048 {
		var oldest quotaRefreshKey
		var touched time.Time
		for key, entry := range r.cache {
			if touched.IsZero() || entry.touched.Before(touched) {
				oldest, touched = key, entry.touched
			}
		}
		delete(r.cache, oldest)
	}
}
