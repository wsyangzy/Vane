package routing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testQuotaReader struct {
	provider string
	read     func(context.Context, CredentialRef, string) ([]QuotaSnapshot, error)
}

func (r testQuotaReader) Provider() string { return r.provider }
func (r testQuotaReader) Read(ctx context.Context, ref CredentialRef, model string) ([]QuotaSnapshot, error) {
	return r.read(ctx, ref, model)
}

func TestQuotaRefreshSeedIsIdempotentAndKeepsNewestWindow(t *testing.T) {
	r := NewQuotaRefresher(nil, nil)
	now := time.Now()
	ref := CredentialRef{Provider: "codex", ChannelID: 1, GroupID: 2, AccountID: "account"}
	snapshot := QuotaSnapshot{Provider: "codex", AccountIDHash: ref.IdentityHash(), ModelScope: "*", WindowName: "primary", ObservedAt: now}
	for i := 0; i < 100; i++ {
		r.Seed([]QuotaSnapshot{snapshot})
	}
	older := snapshot
	older.ObservedAt = now.Add(-time.Minute)
	r.Seed([]QuotaSnapshot{older})
	entry := r.cache[quotaRefreshKey{"codex", ref.IdentityHash(), "*"}]
	if len(entry.snapshots) != 1 || !entry.snapshots[0].ObservedAt.Equal(now) {
		t.Fatalf("duplicate or stale seed: %+v", entry)
	}
	snapshot.ObservedAt = now.Add(time.Minute)
	r.Seed([]QuotaSnapshot{snapshot})
	entry = r.cache[quotaRefreshKey{"codex", ref.IdentityHash(), "*"}]
	if len(entry.snapshots) != 1 || !entry.snapshots[0].ObservedAt.Equal(snapshot.ObservedAt) {
		t.Fatalf("new seed not replaced: %+v", entry)
	}
}

func TestQuotaRefreshSingleflightIntervalsManualAndAccountIsolation(t *testing.T) {
	now := time.Now().UTC()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	reader := testQuotaReader{"codex", func(context.Context, CredentialRef, string) ([]QuotaSnapshot, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		used := 25.0
		return []QuotaSnapshot{{WindowName: "primary", ModelScope: "*", UsedPercent: &used, ObservedAt: now, ExpiresAt: now.Add(time.Hour), Confidence: "exact"}}, nil
	}}
	r := NewQuotaRefresher([]QuotaReader{reader}, nil)
	r.now = func() time.Time { return now }
	ref := CredentialRef{Provider: "codex", ChannelID: 1, GroupID: 2, AccountID: "account-a"}
	var wg sync.WaitGroup
	results := make([][]QuotaSnapshot, 16)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], _ = r.Refresh(context.Background(), ref, "gpt-a", true, false) }()
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent reads=%d", calls.Load())
	}
	*results[0][0].UsedPercent = 99
	if *results[1][0].UsedPercent != 25 {
		t.Fatal("callers share mutable quota pointers")
	}
	now = now.Add(6 * time.Minute)
	if _, err := r.Refresh(context.Background(), ref, "gpt-a", false, false); err != nil || calls.Load() != 1 {
		t.Fatalf("idle interval: %d %v", calls.Load(), err)
	}
	if _, err := r.Refresh(context.Background(), ref, "gpt-a", true, false); err != nil || calls.Load() != 2 {
		t.Fatalf("active interval: %d %v", calls.Load(), err)
	}
	if _, err := r.Refresh(context.Background(), ref, "gpt-a", true, true); err != nil || calls.Load() != 3 {
		t.Fatalf("manual interval: %d %v", calls.Load(), err)
	}
	ref.AccountID = "account-b"
	if _, err := r.Refresh(context.Background(), ref, "gpt-a", true, false); err != nil || calls.Load() != 4 {
		t.Fatalf("account identity cache collision: %d %v", calls.Load(), err)
	}
}

func TestQuotaRefreshFailurePreservesFactsAndPersistsOnlySafeErrors(t *testing.T) {
	now := time.Now().UTC()
	used := 42.0
	ref := CredentialRef{Provider: "claude", ChannelID: 1, GroupID: 2, AccountID: "account"}
	var persisted []QuotaSnapshot
	r := NewQuotaRefresher([]QuotaReader{testQuotaReader{"claude", func(context.Context, CredentialRef, string) ([]QuotaSnapshot, error) {
		return nil, errors.New("token-secret raw stderr")
	}}}, func(_ context.Context, s []QuotaSnapshot) error { persisted = cloneQuota(s); return nil })
	r.Seed([]QuotaSnapshot{{Provider: "claude", ChannelID: 1, GroupID: 2, AccountIDHash: ref.IdentityHash(), ModelScope: "*", WindowName: "5h", UsedPercent: &used, ObservedAt: now, ExpiresAt: now.Add(time.Hour), Confidence: "exact"}})
	got, err := r.Refresh(context.Background(), ref, "claude-test", true, true)
	if !errors.Is(err, ErrQuotaUnavailable) || len(got) != 1 || *got[0].UsedPercent != 42 || got[0].Confidence != "stale" || !got[0].ObservedAt.Equal(now) {
		t.Fatalf("old fact lost: %+v %v", got, err)
	}
	if len(persisted) != 1 || persisted[0].Error != "quota_refresh_failed" || strings.Contains(persisted[0].Error, "secret") {
		t.Fatalf("unsafe error persisted: %+v", persisted)
	}
}

func TestQuotaRefreshCallerCancellationDoesNotCancelSharedRead(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	r := NewQuotaRefresher([]QuotaReader{testQuotaReader{"codex", func(ctx context.Context, _ CredentialRef, _ string) ([]QuotaSnapshot, error) {
		close(entered)
		select {
		case <-release:
			return []QuotaSnapshot{{WindowName: "primary", ObservedAt: time.Now()}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}, nil)
	ref := CredentialRef{Provider: "codex", ChannelID: 1, GroupID: 1, AccountID: "a"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := r.Refresh(ctx, ref, "model", true, false); done <- err }()
	<-entered
	cancel()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("caller cancellation ignored")
	}
	close(release)
	if got, err := r.Refresh(context.Background(), ref, "model", true, false); err != nil || len(got) != 1 {
		t.Fatalf("shared read cancelled: %+v %v", got, err)
	}
}
