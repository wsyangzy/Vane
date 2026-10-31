package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func (a *App) manualBalanceRef(ctx context.Context, provider string, channel, group int64) (routing.CredentialRef, error) {
	if provider != "claude" && provider != "codex" {
		return routing.CredentialRef{}, errors.New("provider_invalid")
	}
	ref, err := a.quotaCredentialRef(ctx, provider, channel, group)
	if err != nil {
		return ref, err
	}
	if ref.AccountID != "" {
		return ref, errors.New("managed_account_uses_official_quota")
	}
	if provider == "claude" {
		g, e := a.claudeCredentialGroupStore.GetByID(ctx, group)
		if e != nil {
			return ref, e
		}
		if g == nil || g.AuthMode == "client_passthrough" {
			return ref, errors.New("manual_balance_requires_fixed_credentials")
		}
	} else {
		g, e := a.codexCredentialGroupStore.GetByID(ctx, group)
		if e != nil {
			return ref, e
		}
		if g == nil || g.AuthMode == "client_passthrough" {
			return ref, errors.New("manual_balance_requires_fixed_credentials")
		}
	}
	return ref, nil
}

func (a *App) ListManualBalances(provider string) ([]routing.ManualBalance, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.profileStore(ctx)
	if err != nil {
		return nil, err
	}
	return a.listManualBalances(ctx, s, provider)
}

func (a *App) listManualBalances(ctx context.Context, s *routingstore.ProfileStore, provider string) ([]routing.ManualBalance, error) {
	if provider != "claude" && provider != "codex" {
		return nil, errors.New("provider_invalid")
	}
	catalog, err := a.poolCatalog(ctx, provider)
	if err != nil {
		return nil, err
	}
	accounts, _, err := a.poolAccounts(ctx, provider, catalog)
	if err != nil {
		return nil, err
	}
	result := []routing.ManualBalance{}
	for _, account := range accounts {
		b, e := s.ManualBalance(ctx, provider, account.ChannelID, account.GroupID)
		if e != nil {
			return nil, e
		}
		if b == nil {
			continue
		}
		ref, e := a.manualBalanceRef(ctx, provider, account.ChannelID, account.GroupID)
		if e == nil {
			if ref.IdentityHash() != b.AccountIDHash {
				b.Enabled, b.BindingChanged = false, true
			}
			result = append(result, *b)
		}
	}
	return result, nil
}

func (a *App) UpdateManualBalance(input routing.ManualBalanceUpdate) (*routing.ManualBalance, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	a.routingWriteMu.Lock()
	defer a.routingWriteMu.Unlock()
	ref, err := a.manualBalanceRef(ctx, input.Provider, input.ChannelID, input.GroupID)
	if err != nil {
		return nil, err
	}
	s, err := a.profileStore(ctx)
	if err != nil {
		return nil, err
	}
	b, err := s.UpdateManualBalance(ctx, input, ref.IdentityHash(), time.Now())
	if err != nil {
		return nil, err
	}
	if _, err = a.refreshPoolRuntime(ctx, input.Provider); err != nil {
		return nil, err
	}
	a.publishPoolRuntime(input.Provider)
	return b, nil
}

func (a *App) ListManualBalanceEntries(provider string, channel, group int64) ([]routing.BalanceEntry, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	if _, err := a.manualBalanceRef(ctx, provider, channel, group); err != nil {
		return nil, err
	}
	s, err := a.profileStore(ctx)
	if err != nil {
		return nil, err
	}
	return s.ManualBalanceEntries(ctx, provider, channel, group)
}

func currentManualBalance(s *routingstore.ProfileStore, c routing.DispatchCandidate) (*routing.ManualBalance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := s.ManualBalance(ctx, c.Provider, c.ChannelID, c.GroupID)
	if b != nil && b.AccountIDHash != c.AccountIDHash {
		b = nil
	}
	return b, err
}

func (a *App) settleManualBalance(s *routingstore.ProfileStore, settlement routing.AttemptSettlement) {
	if settlement.Usage.Exempt {
		return
	}
	c := settlement.Candidate
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := currentManualBalance(s, c)
	if err != nil || b == nil || !b.Enabled {
		return
	}
	channel, group, endpoint := "", "", ""
	if c.Provider == "claude" && a.endpointManager != nil {
		for _, ep := range a.endpointManager.GetAllEndpoints() {
			if ep.Config.CredentialGroupID == c.GroupID && claudeendpoint.EndpointKeyForConfig(ep.Config) == c.EndpointID {
				channel, group, endpoint = claudeendpoint.ChannelKey(ep), ep.Config.CredentialGroupName, ep.Config.Name
				break
			}
		}
	} else if c.Provider == "codex" && a.codexEndpointMgr != nil {
		catalog, e := a.poolCatalog(ctx, c.Provider)
		if e == nil {
			if ch := catalog.Channel(c.ChannelID); ch != nil {
				channel = ch.Name
			}
		}
		g, e := a.codexCredentialGroupStore.GetByID(ctx, c.GroupID)
		if e == nil && g != nil {
			group = g.Name
		}
		// Endpoint multiplier lookup is group-first, but preserve endpoint fallbacks.
		for _, ep := range a.codexEndpointMgr.GetAllEndpoints() {
			if ep.CredentialGroupID == c.GroupID && ep.ChannelID == c.ChannelID && fmt.Sprint(ep.ID) == c.EndpointID {
				endpoint = ep.Name
				break
			}
		}
	}
	u := settlement.Usage
	usage := &tracking.TokenUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, ReasoningTokens: u.ReasoningTokens, CacheReadTokens: u.CacheReadTokens, CacheCreationTokens: u.CacheCreationTokens, CacheWrite30mTokens: u.CacheWrite30mTokens, CacheWrite12hTokens: u.CacheWrite12hTokens, CacheCreation5mTokens: u.CacheCreation5mTokens, CacheCreation1hTokens: u.CacheCreation1hTokens, InputIncludesCacheRead: u.InputIncludesCacheRead}
	cost, priced := a.usageTracker.CalculateAttemptCost(c.Provider, channel, group, endpoint, c.Identity().Model, u.ServiceTier, usage, u.RequestedServiceTier)
	if _, err := routing.CostToUSD(cost); err != nil {
		cost, priced = 0, false
	}
	if err = s.SettleManualBalance(ctx, settlement, cost, priced && u.Known, time.Now()); err != nil && a.logger != nil {
		a.logger.Error("手动余额结算失败", "provider", c.Provider, "channel_id", c.ChannelID, "group_id", c.GroupID)
	}
}
