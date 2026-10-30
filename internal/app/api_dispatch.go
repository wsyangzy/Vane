package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/platform/appdir"
	"github.com/wsyangzy/Vane/internal/platform/transport"
	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/handlers"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

type RoutingProfileState struct {
	Profiles   []routing.RoutingProfile    `json:"profiles"`
	Channels   []PoolChannelInfo           `json:"channels"`
	Accounts   []PoolAccountInfo           `json:"accounts"`
	Candidates []routing.DispatchCandidate `json:"candidates"`
	Quota      []routing.QuotaSnapshot     `json:"quota"`
	Balances   []routing.ManualBalance     `json:"balances"`
}

type DispatchInput struct {
	Provider              string `json:"provider"`
	ProfileID             int64  `json:"profile_id"`
	Model                 string `json:"model"`
	Streaming             bool   `json:"streaming"`
	ConversationID        string `json:"conversation_id"`
	BranchID              string `json:"branch_id"`
	TurnID                string `json:"turn_id"`
	NewTurn               bool   `json:"new_turn"`
	CurrentUserMessage    string `json:"current_user_message"`
	PreviousUserMessage   string `json:"previous_user_message"`
	PreviousAssistantText string `json:"previous_assistant_text"`
}

func (i DispatchInput) turn() routing.TurnInput {
	return routing.TurnInput{ConversationID: i.ConversationID, BranchID: i.BranchID, TurnID: i.TurnID, NewTurn: i.NewTurn, CurrentUserMessage: i.CurrentUserMessage, PreviousUserMessage: i.PreviousUserMessage, PreviousAssistantText: i.PreviousAssistantText}
}

func (a *App) profileStore(ctx context.Context) (*routingstore.ProfileStore, error) {
	a.mu.RLock()
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()
	if db == nil {
		return nil, errors.New("routing_database_unavailable")
	}
	s := routingstore.NewProfileStore(db, buildinfo.BuildMode)
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Called while poolMu is held. Resource snapshots never contain credentials.
func (a *App) refreshRoutingRuntime(ctx context.Context, provider string, pool *routing.PoolRuntime, catalog routing.Catalog, accounts []routing.AccountState) error {
	s, err := a.profileStore(ctx)
	if err != nil {
		return err
	}
	profiles, err := s.List(ctx, provider)
	if err != nil {
		return err
	}
	quota, err := s.ListQuota(ctx, provider)
	if err != nil {
		return err
	}
	candidates := a.dispatchCandidates(provider, pool, catalog, accounts, quota)
	cfg := routing.RuntimeConfig{Provider: provider, Profiles: profiles, Candidates: candidates, Classify: a.callIntentClassifier}
	cfg.OnSettlement = func(settlement routing.AttemptSettlement) { a.settleManualBalance(s, settlement) }
	cfg.CheckAdmission = func(c routing.DispatchCandidate) string {
		b, err := currentManualBalance(s, c)
		if err != nil {
			return "manual_balance_unavailable"
		}
		if b.Exhausted() {
			return "manual_balance_exhausted"
		}
		return ""
	}
	prepareHealth := func(c routing.DispatchCandidate) routing.DispatchCandidate {
		c.Health = dispatchCandidateHealth(pool, c)
		c.Balance, _ = currentManualBalance(s, c)
		if provider == "claude" {
			c.Quota.Windows = claudeservice.ProjectRoutingQuotaForModel(c.Quota.Windows, c.Identity().Model)
		}
		return c
	}
	cfg.PrepareCandidate = func(c routing.DispatchCandidate, requested, override string) routing.DispatchCandidate {
		if provider == "codex" && a.codexEndpointMgr != nil {
			return prepareHealth(a.codexEndpointMgr.PrepareDispatchCandidate(c, requested, override))
		}
		c.Model, c.UpstreamModel = requested, override
		if provider == "claude" && a.endpointManager != nil {
			for _, ep := range a.endpointManager.GetAllEndpoints() {
				if claudeendpoint.EndpointKeyForConfig(ep.Config) == c.EndpointID && ep.Config.CredentialGroupID == c.GroupID {
					if override == "" {
						c.UpstreamModel = handlers.EffectiveClaudeModel(ep, a.endpointManager.GetChannelModelMapping(claudeendpoint.ChannelKey(ep)), requested)
					}
					c.StateVersion = ep.RouteStateVersion()
					break
				}
			}
		}
		return prepareHealth(c)
	}
	cfg.Supports = func(c routing.DispatchCandidate, requested string) bool {
		if requested == "" {
			return true
		}
		if provider == "codex" && a.codexEndpointMgr != nil {
			return a.codexEndpointMgr.SupportsDispatchCandidate(c, requested)
		}
		if provider == "claude" && a.endpointManager != nil {
			for _, ep := range a.endpointManager.GetAllEndpoints() {
				if claudeendpoint.EndpointKeyForConfig(ep.Config) == c.EndpointID && ep.Config.CredentialGroupID == c.GroupID {
					return handlers.EndpointSupportsClaudeModel(ep, nil, c.UpstreamModel)
				}
			}
		}
		return false
	}
	cfg.OnTrace = func(trace routing.DispatchTrace) {
		persistCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.SaveTrace(persistCtx, trace); err != nil && a.logger != nil {
			a.logger.Warn("调度记录保存失败", "provider", provider)
		}
		a.routingAffinityMu.Lock()
		defer a.routingAffinityMu.Unlock()
		a.poolMu.Lock()
		current := a.profileRuntimes[provider]
		a.poolMu.Unlock()
		if current != nil {
			_ = s.SaveAffinity(persistCtx, provider, current.ExportSessionAffinity())
		}
	}
	if a.profileRuntimes == nil {
		a.profileRuntimes = map[string]*routing.ProfileRuntime{}
	}
	runtime := a.profileRuntimes[provider]
	if runtime == nil {
		runtime = routing.NewProfileRuntime(cfg)
		affinity, err := s.LoadAffinity(ctx, provider)
		if err != nil {
			return err
		}
		runtime.SeedSessionAffinity(affinity)
		a.profileRuntimes[provider] = runtime
	} else {
		runtime.Replace(cfg)
	}
	pool.SetProfileRuntime(runtime)
	if a.quotaRefresher == nil {
		quotaTransport, err := transport.CreateTransport(a.config)
		if err != nil {
			return err
		}
		a.routingQuotaTransport = quotaTransport
		var resolver claudeservice.QuotaCredentialResolver
		if a.subscriptionAccounts != nil {
			resolver = a.subscriptionAccounts
		}
		claudeQuotaReader := claudeservice.NewRoutingQuotaReader(resolver, appdir.GetDataDir())
		claudeQuotaReader.Proxy = a.routingCLIProxy
		a.quotaRefresher = routing.NewQuotaRefresher([]routing.QuotaReader{
			codexservice.NewRoutingQuotaReader(resolver, &http.Client{Timeout: 30 * time.Second, Transport: quotaTransport}),
			claudeQuotaReader,
		}, a.persistRoutingQuota)
	}
	a.quotaRefresher.Seed(quota)
	return nil
}

func (a *App) dispatchCandidates(provider string, pool *routing.PoolRuntime, catalog routing.Catalog, accounts []routing.AccountState, quota []routing.QuotaSnapshot) []routing.DispatchCandidate {
	result := []routing.DispatchCandidate{}
	for _, ch := range catalog {
		for _, account := range accounts {
			if account.ChannelID != ch.ID {
				continue
			}
			mode := "api_key"
			if account.Managed {
				mode = "managed"
			}
			// Resources retain legacy participation flags for rollback only.
			// Profile members own participation in the unified runtime.
			base := routing.DispatchCandidate{Provider: provider, ChannelID: ch.ID, GroupID: account.GroupID, Enabled: true, Weight: 1, Quota: routing.QuotaView{Learns: account.Managed}}
			if account.Managed {
				base.AccountIDHash = (routing.CredentialRef{Provider: provider, AccountID: strings.TrimPrefix(account.Identity, "account/")}).IdentityHash()
			} else if ref, err := a.quotaCredentialRef(context.Background(), provider, ch.ID, account.GroupID); err == nil {
				base.AccountIDHash = ref.IdentityHash()
			}
			for _, q := range quota {
				if account.Managed && q.AccountIDHash != "" && q.AccountIDHash == base.AccountIDHash {
					base.Quota.Windows = append(base.Quota.Windows, q)
				}
			}
			add := func(c routing.DispatchCandidate) {
				c.Health = dispatchCandidateHealth(pool, c)
				result = append(result, c)
			}
			if provider == "claude" && a.endpointManager != nil {
				for _, ep := range a.endpointManager.GetAllEndpoints() {
					if claudeendpoint.ChannelKey(ep) != ch.Name || ep.Config.CredentialGroupID != account.GroupID {
						continue
					}
					c := base
					c.EndpointID = claudeendpoint.EndpointKeyForConfig(ep.Config)
					c.Protocol = "messages"
					c.StateVersion = ep.RouteStateVersion()
					c.Position = ep.Config.Priority
					c.CredentialMode = mode
					if mode == "api_key" && ep.Config.AuthMode != "client_passthrough" && ep.Config.Token == "" && ep.Config.ApiKey == "" && len(ep.Config.Tokens) == 0 && len(ep.Config.ApiKeys) == 0 {
						c.Enabled = false
					}
					if !account.Managed && ep.Config.AuthMode == "client_passthrough" {
						c.CredentialMode = "client_passthrough"
					}
					add(c)
				}
			} else if provider == "codex" && a.codexEndpointMgr != nil {
				for _, ep := range a.codexEndpointMgr.PoolEndpoints(ch.ID, account.GroupID) {
					c := base
					c.EndpointID = strconv.FormatInt(ep.ID, 10)
					c.Protocol = "responses"
					c.Position = ep.Priority
					c.CredentialMode = mode
					if mode == "api_key" && ep.AuthMode != "client_passthrough" && ep.APIKey == "" {
						c.Enabled = false
					}
					if !account.Managed && ep.AuthMode == "client_passthrough" {
						c.CredentialMode = "client_passthrough"
					}
					add(c)
				}
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		x, y := result[i], result[j]
		if x.ChannelID != y.ChannelID {
			return x.ChannelID < y.ChannelID
		}
		if x.GroupID != y.GroupID {
			return x.GroupID < y.GroupID
		}
		if x.Position != y.Position {
			return x.Position < y.Position
		}
		return x.EndpointID < y.EndpointID
	})
	return result
}

func dispatchCandidateHealth(pool *routing.PoolRuntime, c routing.DispatchCandidate) routing.HealthState {
	state := routing.HealthState{}
	if allowed, reason := pool.HealthAllowed(c.ChannelID, c.GroupID, c.EndpointID, c.Identity().Model); !allowed {
		state.Reason = reason
		switch reason {
		case "account_missing":
			return state
		case "account_repair_required":
			state.RepairRequired = true
		case "account_cooling_down", "account_probe_in_progress":
			state.CoolingDown = true
		}
	}
	account, _ := pool.Account(c.ChannelID, c.GroupID)
	for _, cooldown := range pool.Cooldowns() {
		if cooldown.Identity != account.Identity || cooldown.Endpoint != "" && cooldown.Endpoint != c.EndpointID || cooldown.Model != "" && cooldown.Model != c.Identity().Model {
			continue
		}
		state.RepairRequired = state.RepairRequired || cooldown.Repair
		if state.CoolingDown && !cooldown.Repair && cooldown.Until.After(state.Until) {
			state.Until = cooldown.Until
		}
	}
	return state
}

func (a *App) ListRoutingProfiles(provider string) (RoutingProfileState, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return RoutingProfileState{}, err
	}
	a.routingWriteMu.Lock()
	defer a.routingWriteMu.Unlock()
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.profileStore(ctx)
	if err != nil {
		return RoutingProfileState{}, err
	}
	catalog, err := a.poolCatalog(ctx, provider)
	if err != nil {
		return RoutingProfileState{}, err
	}
	if err := s.PruneMissingCredentialGroupReferences(ctx, provider, catalog); err != nil {
		return RoutingProfileState{}, err
	}
	pool, err := a.refreshPoolRuntime(ctx, provider)
	if err != nil {
		return RoutingProfileState{}, err
	}
	a.publishPoolRuntime(provider)
	profiles, err := s.List(ctx, provider)
	if err != nil {
		return RoutingProfileState{}, err
	}
	accounts, infos, err := a.poolAccounts(ctx, provider, catalog)
	if err != nil {
		return RoutingProfileState{}, err
	}
	for i := range infos {
		infos[i].Enabled = true // Legacy global switches do not seed profile membership.
	}
	quota, err := s.ListQuota(ctx, provider)
	if err != nil {
		return RoutingProfileState{}, err
	}
	state := RoutingProfileState{Profiles: profiles, Accounts: infos, Quota: quota, Channels: []PoolChannelInfo{}}
	state.Balances, err = a.listManualBalances(ctx, s, provider)
	if err != nil {
		return RoutingProfileState{}, err
	}
	for _, ch := range catalog {
		state.Channels = append(state.Channels, PoolChannelInfo{ID: ch.ID, Name: ch.Name})
	}
	state.Candidates = a.dispatchCandidates(provider, pool, catalog, accounts, quota)
	for i := range state.Candidates {
		state.Candidates[i].Balance, _ = currentManualBalance(s, state.Candidates[i])
	}
	a.poolMu.Lock()
	runtime := a.profileRuntimes[provider]
	a.poolMu.Unlock()
	if runtime != nil {
		for i := range state.Candidates {
			state.Candidates[i].Load = runtime.CandidateLoad(state.Candidates[i])
		}
	}
	return state, nil
}

func (a *App) SaveRoutingProfile(input routing.RoutingProfile) (routing.RoutingProfile, error) {
	a.routingWriteMu.Lock()
	defer a.routingWriteMu.Unlock()
	ctx, cancel := routeCtx()
	defer cancel()
	if err := routing.ValidateProfile(&input); err != nil {
		return input, err
	}
	catalog, err := a.poolCatalog(ctx, input.Provider)
	if err != nil {
		return input, err
	}
	for _, member := range input.Members {
		if err := catalog.Validate([]routing.Target{{ChannelID: member.ChannelID, GroupID: member.GroupID}}); err != nil {
			return input, err
		}
	}
	for _, rule := range input.Rules {
		for _, target := range rule.Targets {
			if target.ProfileID == 0 {
				if err := catalog.Validate([]routing.Target{{ChannelID: target.ChannelID, GroupID: target.GroupID}}); err != nil {
					return input, err
				}
			}
		}
	}
	for _, intent := range input.Intents {
		for _, target := range intent.Targets {
			if target.ProfileID == 0 {
				if err := catalog.Validate([]routing.Target{{ChannelID: target.ChannelID, GroupID: target.GroupID}}); err != nil {
					return input, err
				}
			}
		}
	}
	if input.Classifier != nil && input.Classifier.Enabled {
		classifierProvider := input.Classifier.Provider
		if classifierProvider == "" {
			classifierProvider = input.Provider
		}
		cc, err := a.poolCatalog(ctx, classifierProvider)
		if err != nil {
			return input, err
		}
		if err := cc.Validate([]routing.Target{{ChannelID: input.Classifier.ChannelID, GroupID: input.Classifier.GroupID}}); err != nil {
			return input, err
		}
	}
	s, err := a.profileStore(ctx)
	if err != nil {
		return input, err
	}
	if err = s.Save(ctx, &input); err != nil {
		return input, err
	}
	_, err = a.refreshPoolRuntime(ctx, input.Provider)
	if err == nil {
		a.publishPoolRuntime(input.Provider)
		a.emitPoolHealthUpdate(input.Provider)
	}
	return input, err
}

func (a *App) DeleteRoutingProfile(provider string, id int64) error {
	a.routingWriteMu.Lock()
	defer a.routingWriteMu.Unlock()
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.profileStore(ctx)
	if err != nil {
		return err
	}
	if err = s.Delete(ctx, provider, id); err != nil {
		return err
	}
	_, err = a.refreshPoolRuntime(ctx, provider)
	if err == nil {
		a.publishPoolRuntime(provider)
		a.emitPoolHealthUpdate(provider)
	}
	return err
}
func (a *App) SetDefaultRoutingProfile(provider string, id int64) error {
	a.routingWriteMu.Lock()
	defer a.routingWriteMu.Unlock()
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.profileStore(ctx)
	if err != nil {
		return err
	}
	if err = s.SetDefault(ctx, provider, id); err != nil {
		return err
	}
	_, err = a.refreshPoolRuntime(ctx, provider)
	if err == nil {
		a.publishPoolRuntime(provider)
		a.emitPoolHealthUpdate(provider)
	}
	return err
}
func (a *App) dispatchRuntime(ctx context.Context, provider string) (*routing.ProfileRuntime, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return nil, err
	}
	if _, err = a.refreshPoolRuntime(ctx, provider); err != nil {
		return nil, err
	}
	a.poolMu.Lock()
	defer a.poolMu.Unlock()
	return a.profileRuntimes[provider], nil
}
func (a *App) PreviewDispatch(input DispatchInput) (routing.DispatchPlan, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	runtime, err := a.dispatchRuntime(ctx, input.Provider)
	if err != nil {
		return routing.DispatchPlan{}, err
	}
	return runtime.Plan(ctx, input.ProfileID, input.Model, input.Streaming, input.turn(), true), nil
}
func (a *App) ClassifyIntent(input DispatchInput) (routing.IntentResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runtime, err := a.dispatchRuntime(ctx, input.Provider)
	if err != nil {
		return routing.IntentResult{}, err
	}
	turn := input.turn()
	turn.NewTurn = true
	if turn.ConversationID == "" {
		turn.ConversationID = "preview"
	}
	if turn.TurnID == "" {
		turn.TurnID = fmt.Sprint(time.Now().UnixNano())
	}
	return runtime.ClassifyIntent(ctx, input.ProfileID, turn), nil
}
func (a *App) ListRoutingQuota(provider string) ([]routing.QuotaSnapshot, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.profileStore(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListQuota(ctx, provider)
}
func (a *App) ListDispatchTraces(provider string, limit int) ([]routing.DispatchTrace, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.profileStore(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListTraces(ctx, provider, limit)
}
func (a *App) quotaCredentialRef(ctx context.Context, provider string, channelID, groupID int64) (routing.CredentialRef, error) {
	ref := routing.CredentialRef{Provider: provider, ChannelID: channelID, GroupID: groupID}
	if provider == "claude" && a.claudeCredentialGroupStore != nil {
		group, err := a.claudeCredentialGroupStore.GetByID(ctx, groupID)
		if err != nil {
			return ref, err
		}
		catalog, err := a.poolCatalog(ctx, provider)
		if err != nil {
			return ref, err
		}
		channel := catalog.Channel(channelID)
		if group != nil && channel != nil && group.Channel == channel.Name {
			if group.Subscription != nil {
				ref.AccountID = group.Subscription.AccountID
			}
			if ref.AccountID == "" {
				ref.AccountIDHash = (routing.CredentialRef{Provider: provider, AccountID: fmt.Sprintf("group/%d/%s/%s/%s", group.ID, group.AuthMode, group.Token, group.ApiKey)}).IdentityHash()
			}
		}
	}
	if provider == "codex" && a.codexCredentialGroupStore != nil {
		group, err := a.codexCredentialGroupStore.GetByID(ctx, groupID)
		if err != nil {
			return ref, err
		}
		if group != nil && group.ChannelID == channelID {
			if group.Subscription != nil {
				ref.AccountID = group.Subscription.AccountID
			}
			if ref.AccountID == "" {
				ref.AccountIDHash = (routing.CredentialRef{Provider: provider, AccountID: fmt.Sprintf("group/%d/%s/%s", group.ID, group.AuthMode, group.ApiKey)}).IdentityHash()
			}
		}
	}
	if ref.AccountID == "" && ref.AccountIDHash == "" {
		return ref, routing.ErrQuotaCredentials
	}
	ref.AccountIDHash = ref.IdentityHash()
	return ref, nil
}
func (a *App) persistRoutingQuota(ctx context.Context, snapshots []routing.QuotaSnapshot) error {
	valid := []routing.QuotaSnapshot{}
	for _, snapshot := range snapshots {
		ref, err := a.quotaCredentialRef(ctx, snapshot.Provider, snapshot.ChannelID, snapshot.GroupID)
		if err == nil && ref.IdentityHash() == snapshot.AccountIDHash {
			valid = append(valid, snapshot)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	s, err := a.profileStore(ctx)
	if err != nil {
		return err
	}
	return s.SaveQuota(ctx, valid)
}
func (a *App) RefreshRoutingQuota(provider string, channelID, groupID int64, model string) ([]routing.QuotaSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if _, err := a.dispatchRuntime(ctx, provider); err != nil {
		return nil, err
	}
	ref, err := a.quotaCredentialRef(ctx, provider, channelID, groupID)
	if err != nil {
		return nil, err
	}
	if ref.AccountID == "" {
		return nil, errors.New("quota_query_unsupported")
	}
	a.poolMu.Lock()
	refresher := a.quotaRefresher
	a.poolMu.Unlock()
	result, refreshErr := refresher.Refresh(ctx, ref, strings.TrimSpace(model), true, true)
	if current, err := a.quotaCredentialRef(ctx, provider, channelID, groupID); err != nil || current.IdentityHash() != ref.IdentityHash() {
		return nil, errors.New("quota_binding_changed")
	}
	if _, err := a.refreshPoolRuntime(ctx, provider); err == nil {
		a.publishPoolRuntime(provider)
	}
	return result, refreshErr
}

// Historical channel policies are retained for inspection only.
func (a *App) requireLegacyRouting(_ context.Context, _ string) error {
	return errors.New("legacy_routing_retired: 请在调度方案中配置规则")
}

func (a *App) GetRoutingProfile(provider string, id int64) (*routing.RoutingProfile, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	s, err := a.profileStore(ctx)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, provider, id)
}
