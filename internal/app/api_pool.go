package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

type PoolAccountInfo struct {
	ChannelID   int64  `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	GroupID     int64  `json:"group_id"`
	GroupName   string `json:"group_name"`
	Enabled     bool   `json:"enabled"`
	Managed     bool   `json:"managed"`
	AuthMode    string `json:"auth_mode"`
}
type PoolChannelInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (a *App) poolStore() (*routingstore.PoolStore, error) {
	a.mu.RLock()
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()
	if db == nil {
		return nil, errors.New("调度资源数据库未就绪")
	}
	return routingstore.NewPoolStore(db), nil
}

// Legacy channel rules intentionally exclude managed Codex groups. Dispatch
// needs the full account catalogue without widening that legacy API contract.
func (a *App) poolCatalog(ctx context.Context, provider string) (routing.Catalog, error) {
	catalog, err := a.routeCatalog(ctx, provider)
	if err != nil || provider != "codex" {
		return catalog, err
	}
	for i := range catalog {
		groups, e := a.codexCredentialGroupStore.ListByChannelID(ctx, catalog[i].ID)
		if e != nil {
			return nil, e
		}
		seen := map[int64]bool{}
		for _, group := range catalog[i].Groups {
			seen[group.ID] = true
		}
		for _, group := range groups {
			if group != nil && !seen[group.ID] {
				catalog[i].Groups = append(catalog[i].Groups, routing.CatalogGroup{ID: group.ID, Name: group.Name})
			}
		}
	}
	return catalog, nil
}

func (a *App) poolAccounts(ctx context.Context, provider string, catalog routing.Catalog) ([]routing.AccountState, []PoolAccountInfo, error) {
	var accounts []routing.AccountState
	var infos []PoolAccountInfo
	for _, ch := range catalog {
		for _, g := range ch.Groups {
			state := routing.AccountState{ChannelID: ch.ID, GroupID: g.ID}
			info := PoolAccountInfo{ChannelID: ch.ID, ChannelName: ch.Name, GroupID: g.ID, GroupName: g.Name}
			if provider == "claude" {
				record, err := a.claudeCredentialGroupStore.GetByID(ctx, g.ID)
				if err != nil {
					return nil, nil, err
				}
				if record == nil {
					continue
				}
				state.Enabled = record.Enabled
				info.AuthMode = record.AuthMode
				if record.Subscription != nil && record.Subscription.AccountID != "" {
					state.Managed = true
					state.Identity = "account/" + record.Subscription.AccountID
				}
			} else {
				record, err := a.codexCredentialGroupStore.GetByID(ctx, g.ID)
				if err != nil {
					return nil, nil, err
				}
				if record == nil {
					continue
				}
				state.Enabled = record.Enabled
				info.AuthMode = record.AuthMode
				if record.Subscription != nil && record.Subscription.AccountID != "" {
					state.Managed = true
					state.Identity = "account/" + record.Subscription.AccountID
				}
			}
			if state.Identity == "" {
				state.Identity = fmt.Sprintf("group/%d", g.ID)
			}
			info.Enabled, info.Managed = state.Enabled, state.Managed
			accounts = append(accounts, state)
			infos = append(infos, info)
		}
	}
	return accounts, infos, nil
}

func (a *App) refreshPoolRuntime(ctx context.Context, provider string) (*routing.PoolRuntime, error) {
	a.poolMu.Lock()
	defer a.poolMu.Unlock()
	s, err := a.poolStore()
	if err != nil {
		return nil, err
	}
	if err = s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	if err = a.prepareUnifiedRouting(ctx, provider); err != nil {
		return nil, err
	}
	catalog, err := a.poolCatalog(ctx, provider)
	if err != nil {
		return nil, err
	}
	accounts, _, err := a.poolAccounts(ctx, provider, catalog)
	if err != nil {
		return nil, err
	}
	// Legacy pools remain in SQLite; only resource identities and health are
	// loaded into the application runtime. Profiles own all participation.
	cfg := routing.PoolConfig{Accounts: accounts}
	if a.poolRuntimes == nil {
		a.poolRuntimes = map[string]*routing.PoolRuntime{}
	}
	runtime := a.poolRuntimes[provider]
	if runtime == nil {
		saved, err := s.LoadCooldowns(ctx, provider)
		if err != nil {
			return nil, err
		}
		runtime = routing.NewPoolRuntime(cfg, saved, func(state []routing.Cooldown) error {
			persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := s.SaveCooldowns(persistCtx, provider, state)
			if err != nil && a.logger != nil {
				a.logger.Warn("账号池冷却状态保存失败", "provider", provider, "error", err)
			}
			return err
		})
		a.poolRuntimes[provider] = runtime
	} else {
		runtime.Replace(cfg)
	}
	if err := a.refreshRoutingRuntime(ctx, provider, runtime, catalog, accounts); err != nil {
		return nil, err
	}
	return runtime, nil
}

func (a *App) publishPoolRuntime(provider string) {
	a.poolMu.Lock()
	runtime := a.poolRuntimes[provider]
	a.poolMu.Unlock()
	if provider == "claude" && a.claudeSubscriptions != nil {
		a.claudeSubscriptions.SetPoolRuntime(runtime)
	}
	if provider == "codex" && a.codexSubscriptions != nil {
		a.codexSubscriptions.SetPoolRuntime(runtime)
	}
	if provider == "claude" && a.endpointManager != nil {
		a.endpointManager.SetPoolRuntime(runtime)
	}
	if provider == "codex" && a.codexEndpointMgr != nil {
		a.codexEndpointMgr.SetPoolRuntime(runtime)
	}
}

// Never fall through to the legacy globally mutating scheduler when pool loading fails.
func (a *App) closePoolRuntime(provider string) {
	a.poolMu.Lock()
	if a.poolRuntimes == nil {
		a.poolRuntimes = map[string]*routing.PoolRuntime{}
	}
	runtime := a.poolRuntimes[provider]
	if runtime == nil {
		runtime = routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
		a.poolRuntimes[provider] = runtime
	} else {
		runtime.Replace(routing.PoolConfig{})
	}
	runtime.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{Provider: provider}))
	a.poolMu.Unlock()
	a.publishPoolRuntime(provider)
}

func (a *App) notifyPoolChange(provider string) {
	if provider == "codex" {
		a.emitCodexProviderUpdate()
	} else {
		a.emitEndpointUpdate()
	}
}

// Diagnostic probes never clear quota or account recovery state.
func (a *App) CheckPoolEndpoint(provider string, id int64) error {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return err
	}
	if provider == "codex" {
		if a.codexHealthCheck == nil {
			return errors.New("健康检测服务未就绪")
		}
		err = a.codexHealthCheck.CheckOne(id)
	} else {
		if a.endpointStore == nil || a.endpointManager == nil {
			return errors.New("健康检测服务未就绪")
		}
		ctx, cancel := routeCtx()
		defer cancel()
		record, e := a.endpointStore.GetByID(ctx, id)
		if e != nil {
			return e
		}
		if record == nil {
			return errors.New("端点不存在")
		}
		err = a.endpointManager.CheckPoolEndpoint(record.Channel, record.CredentialGroupID, record.Name)
	}
	if err == nil {
		a.notifyPoolChange(provider)
	}
	return err
}

func (a *App) repairPoolGroup(provider string, group int64) {
	a.poolMu.Lock()
	runtime := a.poolRuntimes[provider]
	a.poolMu.Unlock()
	if runtime == nil {
		return
	}
	for _, account := range runtime.Config().Accounts {
		if account.GroupID == group {
			if err := runtime.ClearRepair(account.Identity); err != nil && a.logger != nil {
				a.logger.Warn("账号修复状态保存失败", "provider", provider, "error", err)
			}
			return
		}
	}
}
