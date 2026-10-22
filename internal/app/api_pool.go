package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
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
type AccountPoolState struct {
	Pools     []routing.Pool     `json:"pools"`
	Accounts  []PoolAccountInfo  `json:"accounts"`
	Cooldowns []PoolCooldownInfo `json:"cooldowns"`
}
type PoolCooldownInfo struct {
	ChannelID int64  `json:"channel_id"`
	GroupID   int64  `json:"group_id"`
	Endpoint  string `json:"endpoint"`
	Model     string `json:"model"`
	Reason    string `json:"reason"`
	Until     string `json:"until"`
	Repair    bool   `json:"repair"`
	Probing   bool   `json:"probing"`
}
type PoolKeyResult struct {
	Key    string `json:"key"`
	Header string `json:"header"`
}

func (a *App) poolStore() (*routingstore.PoolStore, error) {
	a.mu.RLock()
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()
	if db == nil {
		return nil, errors.New("账号池管理数据库未就绪")
	}
	return routingstore.NewPoolStore(db), nil
}

// Legacy channel rules intentionally exclude managed Codex groups. Pool editing
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
	if err = a.backupBeforePoolMigration(ctx, provider); err != nil {
		return nil, err
	}
	if err = s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	catalog, err := a.poolCatalog(ctx, provider)
	if err != nil {
		return nil, err
	}
	accounts, infos, err := a.poolAccounts(ctx, provider, catalog)
	if err != nil {
		return nil, err
	}
	needed, err := s.NeedsMigration(ctx, provider)
	if err != nil {
		return nil, err
	}
	if needed {
		// VACUUM INTO includes committed WAL data and never overwrites a backup.
		if err = a.backupBeforePoolMigration(ctx, provider); err != nil {
			return nil, err
		}
		var pools []routing.Pool
		svc, err := a.modelRouteSvc()
		if err != nil {
			return nil, err
		}
		records, err := svc.List(ctx, provider)
		if err != nil {
			return nil, err
		}
		var rules []routing.ModelRule
		for _, record := range records {
			rules = append(rules, routing.ModelRule{ID: record.ID, SourceChannelID: record.SourceChannelID, Name: record.Name, Disabled: !record.Enabled, Priority: record.Priority, MatchModel: record.MatchModel, Mode: record.Mode, Targets: record.Targets})
		}
		bySource := routing.RulesBySource(rules)
		failover := a.config != nil && a.config.Failover.Enabled
		if provider == "codex" {
			failover = a.config != nil && a.config.CodexFailover.Enabled
		}
		for _, ch := range catalog {
			p := routing.Pool{Provider: provider, Name: ch.Name, Strategy: routing.PoolSequential, Default: ch.Active, Rules: bySource[ch.ID], NonStream: ch.NonStream}
			for _, info := range infos {
				if info.ChannelID == ch.ID && info.AuthMode != "client_passthrough" {
					p.Members = append(p.Members, routing.PoolMember{ChannelID: ch.ID, GroupID: info.GroupID, Tier: routing.TierPrimary})
				}
			}
			if failover {
				for _, other := range catalog {
					if other.ID == ch.ID || other.Paused {
						continue
					}
					for _, info := range infos {
						if info.ChannelID == other.ID && info.AuthMode != "client_passthrough" {
							p.Members = append(p.Members, routing.PoolMember{ChannelID: other.ID, GroupID: info.GroupID, Tier: routing.TierBackup})
						}
					}
				}
			}
			pools = append(pools, p)
		}
		if len(pools) > 0 {
			hasDefault := false
			for _, p := range pools {
				hasDefault = hasDefault || p.Default
			}
			if !hasDefault {
				pools[0].Default = true
			}
		}
		var disabled []int64
		for i, account := range accounts {
			if ch := catalog.Channel(account.ChannelID); ch != nil && ch.Paused && !ch.Active {
				disabled = append(disabled, account.GroupID)
				accounts[i].Enabled = false
			}
		}
		if err = s.Migrate(ctx, provider, pools, disabled...); err != nil {
			return nil, err
		}
	}
	pools, err := s.List(ctx, provider)
	if err != nil {
		return nil, err
	}
	cfg := routing.PoolConfig{Pools: pools, Accounts: accounts}
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
	return runtime, nil
}

func (a *App) backupBeforePoolMigration(ctx context.Context, provider string) error {
	a.mu.RLock()
	db := a.storeDB
	a.mu.RUnlock()
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return err
	}
	var path string
	for rows.Next() {
		var seq int
		var name, file string
		if err = rows.Scan(&seq, &name, &file); err != nil {
			rows.Close()
			return err
		}
		if name == "main" {
			path = file
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	backup := path + ".before-pools-" + provider + ".bak"
	if _, err = os.Stat(backup); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(backup, "'", "''")+`'`)
	return err
}

func (a *App) ListAccountPools(provider string) (AccountPoolState, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return AccountPoolState{}, err
	}
	ctx, cancel := routeCtx()
	defer cancel()
	runtime, err := a.refreshPoolRuntime(ctx, provider)
	if err != nil {
		return AccountPoolState{}, err
	}
	cfg := runtime.Config()
	catalog, err := a.poolCatalog(ctx, provider)
	if err != nil {
		return AccountPoolState{}, err
	}
	_, infos, err := a.poolAccounts(ctx, provider, catalog)
	if err != nil {
		return AccountPoolState{}, err
	}
	result := AccountPoolState{Pools: cfg.Pools, Accounts: infos, Cooldowns: []PoolCooldownInfo{}}
	for _, c := range runtime.Cooldowns() {
		for _, account := range cfg.Accounts {
			if account.Identity == c.Identity {
				result.Cooldowns = append(result.Cooldowns, PoolCooldownInfo{account.ChannelID, account.GroupID, c.Endpoint, c.Model, c.Reason, c.Until.Format(time.RFC3339), c.Repair, c.Probing})
			}
		}
	}
	// Display the quota controller's persisted state; pools do not own a second quota store.
	for _, info := range infos {
		if !info.Managed {
			continue
		}
		if provider == "claude" && a.claudeSubscriptions != nil {
			record, e := a.claudeCredentialGroupStore.GetByID(ctx, info.GroupID)
			if e == nil && record != nil && record.Subscription != nil {
				for _, pause := range a.claudeSubscriptions.Status(ctx, *record.Subscription) {
					result.Cooldowns = append(result.Cooldowns, PoolCooldownInfo{ChannelID: info.ChannelID, GroupID: info.GroupID, Model: pause.Model, Reason: pause.Reason, Until: pause.ResumeNotBefore.Format(time.RFC3339), Probing: pause.State == "probing"})
				}
			}
		} else if provider == "codex" && a.codexSubscriptions != nil {
			record, e := a.codexCredentialGroupStore.GetByID(ctx, info.GroupID)
			if e == nil && record != nil && record.Subscription != nil {
				for _, pause := range a.codexSubscriptions.Status(ctx, *record.Subscription) {
					result.Cooldowns = append(result.Cooldowns, PoolCooldownInfo{ChannelID: info.ChannelID, GroupID: info.GroupID, Model: pause.Model, Reason: pause.Reason, Until: pause.ResumeNotBefore.Format(time.RFC3339), Probing: pause.State == "probing"})
				}
			}
		}
	}
	return result, nil
}
func (a *App) SaveAccountPool(input routing.Pool) (int64, error) {
	provider, err := normalizeProviderParam(input.Provider)
	if err != nil {
		return 0, err
	}
	input.Provider = provider
	ctx, cancel := routeCtx()
	defer cancel()
	runtime, err := a.refreshPoolRuntime(ctx, provider)
	if err != nil {
		return 0, err
	}
	cfg := runtime.Config()
	catalog, err := a.poolCatalog(ctx, provider)
	if err != nil {
		return 0, err
	}
	if err = routing.ValidatePool(&input, catalog, cfg.Pools); err != nil {
		return 0, err
	}
	if len(cfg.Pools) == 0 {
		input.Default = true
	}
	s, err := a.poolStore()
	if err != nil {
		return 0, err
	}
	if err = s.Save(ctx, &input); err != nil {
		return 0, err
	}
	_, err = a.refreshPoolRuntime(ctx, provider)
	a.publishPoolRuntime(provider)
	if err == nil {
		a.notifyPoolChange(provider)
	}
	return input.ID, err
}
func (a *App) DeleteAccountPool(provider string, id int64) error {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return err
	}
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.poolStore()
	if err != nil {
		return err
	}
	if err = s.Delete(ctx, provider, id); err != nil {
		return err
	}
	_, err = a.refreshPoolRuntime(ctx, provider)
	a.publishPoolRuntime(provider)
	if err == nil {
		a.notifyPoolChange(provider)
	}
	return err
}
func (a *App) RotateAccountPoolKey(provider string, id int64) (PoolKeyResult, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return PoolKeyResult{}, err
	}
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.poolStore()
	if err != nil {
		return PoolKeyResult{}, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return PoolKeyResult{}, err
	}
	key := "vane_pool_" + hex.EncodeToString(secret)
	hash := sha256.Sum256([]byte(key))
	if err = s.RotateKey(ctx, provider, id, hex.EncodeToString(hash[:])); err != nil {
		return PoolKeyResult{}, err
	}
	_, err = a.refreshPoolRuntime(ctx, provider)
	return PoolKeyResult{key, "X-Relay-Key"}, err
}
func (a *App) SetPoolAccountEnabled(provider string, channelID, groupID int64, enabled bool) error {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return err
	}
	ctx, cancel := routeCtx()
	defer cancel()
	catalog, err := a.poolCatalog(ctx, provider)
	if err != nil {
		return err
	}
	if err = catalog.Validate([]routing.Target{{ChannelID: channelID, GroupID: groupID}}); err != nil {
		return err
	}
	if groupID <= 0 {
		return errors.New("请选择具体账号")
	}
	a.mu.RLock()
	db := a.storeDB
	a.mu.RUnlock()
	if db == nil {
		return errors.New("数据库未就绪")
	}
	table := "claude_credential_groups"
	if provider == "codex" {
		table = "codex_credential_groups"
	}
	result, err := db.ExecContext(ctx, `UPDATE `+table+` SET enabled=? WHERE id=?`, enabled, groupID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	_, err = a.refreshPoolRuntime(ctx, provider)
	a.publishPoolRuntime(provider)
	if err == nil {
		a.notifyPoolChange(provider)
	}
	return err
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

// Pool keys use the same local-only request boundary as subscription keys.
func (a *App) authenticatePool(r *http.Request, provider string) (context.Context, bool, error) {
	var key []string
	headers := 0
	for name, values := range r.Header {
		if strings.EqualFold(name, "X-Relay-Key") {
			key = append(key, values...)
			headers++
		}
	}
	ok := headers > 0
	if headers > 1 || len(key) > 1 {
		return r.Context(), true, errors.New("relay_authentication_rejected")
	}
	if !ok {
		return r.Context(), false, nil
	}
	a.poolMu.Lock()
	runtime := a.poolRuntimes[provider]
	a.poolMu.Unlock()
	if runtime == nil {
		return r.Context(), false, nil
	}
	if len(key) != 1 {
		return r.Context(), true, errors.New("relay_authentication_rejected")
	}
	caller, err := runtime.Authenticate(provider, strings.TrimSpace(key[0]))
	if err != nil {
		return r.Context(), false, nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return r.Context(), true, errors.New("pool_requires_loopback")
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Origin") {
			return r.Context(), true, errors.New("pool_request_rejected")
		}
	}
	return routing.WithPoolCaller(r.Context(), caller), true, nil
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
