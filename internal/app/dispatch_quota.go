package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/routing"
)

func quotaActivityKey(provider string, channel, group int64) string {
	return fmt.Sprintf("%s/%d/%d", provider, channel, group)
}

func (a *App) observeRoutingQuota(candidate routing.DispatchCandidate, status int, headers http.Header, body []byte) {
	a.poolMu.Lock()
	if a.quotaActivity == nil {
		a.quotaActivity = map[string]time.Time{}
	}
	a.quotaActivity[quotaActivityKey(candidate.Provider, candidate.ChannelID, candidate.GroupID)] = time.Now()
	a.poolMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ref, err := a.quotaCredentialRef(ctx, candidate.Provider, candidate.ChannelID, candidate.GroupID)
	if err != nil || ref.AccountID == "" || candidate.AccountIDHash == "" || ref.IdentityHash() != candidate.AccountIDHash {
		return
	}
	var snapshots []routing.QuotaSnapshot
	if candidate.Provider == "claude" {
		snapshots = claudeservice.ParseResponseQuota(status, headers, body, ref, candidate.Identity().Model, time.Now())
	} else {
		snapshots = codexservice.ParseResponseQuota(status, headers, body, ref, candidate.Identity().Model, time.Now())
	}
	if len(snapshots) == 0 {
		return
	}
	if a.persistRoutingQuota(ctx, snapshots) != nil {
		return
	}
	if _, err := a.refreshPoolRuntime(ctx, candidate.Provider); err == nil {
		a.publishPoolRuntime(candidate.Provider)
	}
}

// One bounded worker handles periodic reads. A request only learns from its
// response; it never waits for an external quota query or a CLI invocation.
func (a *App) startRoutingQuotaRefresh() {
	ctx, cancel := context.WithCancel(context.Background())
	a.quotaRefreshCancel = cancel
	a.quotaRefreshDone = make(chan struct{})
	go func() {
		defer close(a.quotaRefreshDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.refreshIdleRoutingQuota(ctx)
			}
		}
	}()
}

func (a *App) refreshIdleRoutingQuota(ctx context.Context) {
	for _, provider := range []string{"claude", "codex"} {
		a.poolMu.Lock()
		pool := a.poolRuntimes[provider]
		refresher := a.quotaRefresher
		a.poolMu.Unlock()
		if pool == nil || pool.ProfileRuntime() == nil || refresher == nil {
			continue
		}
		catalog, err := a.poolCatalog(ctx, provider)
		if err != nil {
			continue
		}
		accounts, _, err := a.poolAccounts(ctx, provider, catalog)
		if err != nil {
			continue
		}
		for _, account := range accounts {
			if ctx.Err() != nil {
				return
			}
			if !account.Managed {
				continue
			}
			ref, err := a.quotaCredentialRef(ctx, provider, account.ChannelID, account.GroupID)
			if err != nil {
				continue
			}
			a.poolMu.Lock()
			last := a.quotaActivity[quotaActivityKey(provider, account.ChannelID, account.GroupID)]
			a.poolMu.Unlock()
			readCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
			_, _ = refresher.Refresh(readCtx, ref, "", time.Since(last) < 10*time.Minute, false)
			cancel()
		}
		if _, err := a.refreshPoolRuntime(ctx, provider); err == nil {
			a.publishPoolRuntime(provider)
		}
	}
}
