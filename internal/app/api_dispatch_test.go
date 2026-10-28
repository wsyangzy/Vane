package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/privacy"
	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexproxy "github.com/wsyangzy/Vane/internal/providers/codex/proxy"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestDispatchAndRequestHistoryUseSameBuildEnvironment(t *testing.T) {
	for _, environment := range []string{"dev", "release"} {
		for _, provider := range []string{"claude", "codex"} {
			t.Run(environment+"/"+provider, func(t *testing.T) {
				setBuildModeForTest(t, environment)
				a := newStorageTestApp(t)
				a.config.UsageTracking.Enabled = true
				a.setupUsageTracker()
				a.setupStoreDB()
				require.NotNil(t, a.usageTracker)
				require.NotNil(t, a.storeDB)
				a.endpointManager = claudeendpoint.NewManager(a.config)
				a.endpointStore = claudestore.NewSQLiteEndpointStore(a.storeDB)
				a.claudeCredentialGroupStore = claudestore.NewSQLiteClaudeCredentialGroupStore(a.storeDB)
				ctx := context.Background()
				for _, source := range []string{"dev", "release"} {
					store := routingstore.NewProfileStore(a.storeDB, source)
					require.NoError(t, store.EnsureSchema(ctx))
					requestID := source + "-request"
					_, err := a.storeDB.ExecContext(ctx, `INSERT INTO request_logs(request_id,start_time,status,provider,environment) VALUES(?,?,'completed',?,?)`, requestID, time.Now().Format("2006-01-02 15:04:05"), provider, source)
					require.NoError(t, err)
					require.NoError(t, store.SaveTrace(ctx, routing.DispatchTrace{RequestID: requestID, Provider: provider, Successful: true}))
				}
				requests, err := a.GetRequests(RequestQueryParams{Provider: provider, Page: 1, PageSize: 20, StartDate: "2000-01-01", EndDate: "2100-01-01"})
				require.NoError(t, err)
				require.Len(t, requests.Requests, 1)
				traces, err := a.ListDispatchTraces(provider, 100)
				require.NoError(t, err)
				require.Len(t, traces, 1)
				require.Equal(t, environment+"-request", requests.Requests[0].RequestID)
				require.Equal(t, requests.Requests[0].RequestID, traces[0].RequestID)
				require.Equal(t, environment, buildinfo.BuildMode)
			})
		}
	}
}

func dispatchTestApp(t *testing.T, provider string) (*App, []routing.PoolMember) {
	t.Helper()
	a, members := poolTestApp(t, provider)
	a.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if provider == "claude" {
		cfg := config.NewDefaultConfig()
		for i, member := range members {
			name := []string{"first", "backup", "paused", "managed"}[i]
			cfg.Endpoints = append(cfg.Endpoints, config.EndpointConfig{Name: name, Channel: name, CredentialGroupID: member.GroupID, ApiKey: "test-key", URL: "https://" + name + ".invalid"})
		}
		a.endpointManager = claudeendpoint.NewManager(cfg)
	} else {
		a.codexEndpointMgr = codexproxy.NewEndpointManager(a.codexChannelStore, a.codexEndpointStore, a.codexCredentialGroupStore, a.logger)
		require.NoError(t, a.codexEndpointMgr.Reload())
	}
	return a, members
}

func TestDispatchHealthSeparatesDisabledRepairAndCooldown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		failure routing.Failure
	}{
		{name: "disabled"},
		{name: "healthy", enabled: true},
		{name: "repair", enabled: true, failure: routing.Failure{Scope: routing.ScopeCredential, Reason: "credential_rejected"}},
		{name: "cooldown", enabled: true, failure: routing.Failure{Scope: routing.ScopeEndpoint, Reason: "endpoint_unavailable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := routing.NewPoolRuntime(routing.PoolConfig{Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 2, Identity: "account", Enabled: tc.enabled}}}, nil, nil)
			if tc.failure.Reason != "" {
				require.NoError(t, pool.Fail(1, 2, "ep", "model", tc.failure, time.Time{}))
			}
			state := dispatchCandidateHealth(pool, routing.DispatchCandidate{ChannelID: 1, GroupID: 2, EndpointID: "ep", Model: "model"})
			require.Equal(t, tc.name == "cooldown", state.CoolingDown)
			require.Equal(t, tc.name == "repair", state.RepairRequired)
			if tc.name == "disabled" {
				require.Empty(t, state.Reason, "legacy participation is not a health failure")
			}
			if tc.name != "cooldown" {
				require.True(t, state.Until.IsZero(), "disabled or invalid credentials have no timed recovery")
			} else {
				require.True(t, state.Until.After(time.Now()))
			}
		})
	}
}

func TestUnifiedStartupPreservesLegacyDataAndNeedsManualDefault(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			ctx := context.Background()
			legacy, err := a.poolStore()
			require.NoError(t, err)
			require.NoError(t, legacy.EnsureSchema(ctx))
			old := routing.Pool{Provider: provider, Name: "historical", Default: true, KeyHash: "retained-key-hash", Strategy: routing.PoolSequential, Members: []routing.PoolMember{members[0]}}
			require.NoError(t, legacy.Save(ctx, &old))
			store, err := a.profileStore(ctx)
			require.NoError(t, err)
			_, err = a.storeDB.ExecContext(ctx, `INSERT INTO routing_runtime_state(provider,mode) VALUES(?,'legacy')`, provider)
			require.NoError(t, err)
			state, err := a.ListRoutingProfiles(provider)
			require.NoError(t, err)
			require.Empty(t, state.Profiles, "do not infer a profile from old configuration")
			runtime := a.poolRuntimes[provider]
			require.NotNil(t, runtime.ProfileRuntime())
			require.Empty(t, runtime.Config().Pools, "historical pools cannot serve requests")
			plan := runtime.ProfileRuntime().Plan(ctx, 0, "test", true, routing.TurnInput{}, false)
			require.Equal(t, "routing_profile_missing", plan.Error)
			record, err := store.Migration(ctx, provider)
			require.NoError(t, err)
			require.Equal(t, "unified_only", record.State)
			require.FileExists(t, record.BackupPath)
			backup, err := sql.Open("sqlite", record.BackupPath+"?mode=ro")
			require.NoError(t, err)
			defer backup.Close()
			var retained string
			require.NoError(t, backup.QueryRow(`SELECT key_hash FROM routing_pools WHERE id=?`, old.ID).Scan(&retained))
			require.Equal(t, old.KeyHash, retained)
			before, err := legacy.List(ctx, provider)
			require.NoError(t, err)
			profile := routing.DefaultProfile(provider)
			profile.Name, profile.IsDefault = "manual", true
			profile.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
			profile, err = a.SaveRoutingProfile(profile)
			require.NoError(t, err)
			plan = a.poolRuntimes[provider].ProfileRuntime().Plan(ctx, 0, "test", true, routing.TurnInput{}, true)
			require.Empty(t, plan.Error)
			require.Equal(t, profile.ID, plan.Profile.ID, "saving the default takes effect without a runtime switch")
			traces, err := a.ListDispatchTraces(provider, 100)
			require.NoError(t, err)
			require.Empty(t, traces)
			profile.Enabled = false
			_, err = a.SaveRoutingProfile(profile)
			require.Error(t, err)
			after, err := legacy.List(ctx, provider)
			require.NoError(t, err)
			require.Equal(t, before, after)
			again, err := store.Migration(ctx, provider)
			require.NoError(t, err)
			require.Equal(t, record, again, "refresh must not replace the upgrade snapshot")
			var historicalMode string
			require.NoError(t, a.storeDB.QueryRow(`SELECT mode FROM routing_runtime_state WHERE provider=?`, provider).Scan(&historicalMode))
			require.Equal(t, "legacy", historicalMode, "retained historical state cannot select the runtime")
		})
	}
}

func TestClosedRoutingRuntimeCannotFallBackToHistoricalPools(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, _ := dispatchTestApp(t, provider)
			a.closePoolRuntime(provider)
			runtime := a.poolRuntimes[provider]
			require.NotNil(t, runtime.ProfileRuntime())
			plan := runtime.ProfileRuntime().Plan(context.Background(), 0, "test", true, routing.TurnInput{}, false)
			require.Equal(t, "routing_profile_missing", plan.Error)
			require.Empty(t, runtime.Config().Pools)
		})
	}
}

func TestUnifiedApplicationAuthenticationSeparatesUpstreamCredentials(t *testing.T) {
	a := &App{config: config.NewDefaultConfig(), poolRuntimes: map[string]*routing.PoolRuntime{}}
	a.config.Auth = config.AuthConfig{Enabled: true, Token: "app-secret"}
	pool := routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
	pool.SetProfileRuntime(routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "codex"}))
	a.poolRuntimes["codex"] = pool
	for _, tc := range []struct {
		alias, auth, remaining string
		allowed                bool
	}{
		{"", "Bearer app-secret", "", true}, {"app-secret", "Bearer client-upstream", "Bearer client-upstream", true}, {"wrong", "Bearer app-secret", "", false}, {"", "Bearer vane_pool_old", "", false}, {"", "", "", false},
	} {
		t.Run(tc.alias+tc.auth, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/responses", nil)
			r.Header.Set("X-Relay-Key", tc.alias)
			r.Header.Set("Authorization", tc.auth)
			ctx, handled, err := a.authenticateRoutingProfile(r, "codex")
			require.True(t, handled)
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, "profile:codex", routing.CallerScope(ctx))
				require.Equal(t, tc.remaining, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("X-Relay-Key"))
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestRoutingConfigurationReportsInvalidAuthAndUnresolvableMember(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			profile := routing.DefaultProfile(provider)
			profile.Name, profile.IsDefault = "manual", true
			profile.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
			var err error
			profile, err = a.SaveRoutingProfile(profile)
			require.NoError(t, err)
			a.config.Auth = config.AuthConfig{Enabled: true}
			status, err := a.CheckRoutingConfiguration(provider)
			require.NoError(t, err)
			for _, check := range status.Checks {
				if check.Code == "application_auth" {
					require.False(t, check.Passed)
				}
			}
			a.config.Auth.Token = "valid-application-token"
			profile.Members = append(profile.Members, routing.RoutingMember{ChannelID: members[3].ChannelID, GroupID: members[3].GroupID, Enabled: true})
			_, err = a.SaveRoutingProfile(profile)
			require.NoError(t, err)
			status, err = a.CheckRoutingConfiguration(provider)
			require.NoError(t, err)
			require.False(t, status.Ready, "a valid first member cannot hide an invalid managed binding")
		})
	}
}

func TestRoutingProfilesUseMembersInsteadOfLegacyParticipation(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			ctx := context.Background()
			// Initialize legacy migration, then retain disabled channel/group/endpoint
			// flags exactly as a user upgrading from the old scheduler would have them.
			_, err := a.ListRoutingProfiles(provider)
			require.NoError(t, err)
			groupTable, endpointTable := "claude_credential_groups", "endpoints"
			if provider == "codex" {
				groupTable, endpointTable = "codex_credential_groups", "codex_endpoints"
			}
			_, err = a.storeDB.ExecContext(ctx, "UPDATE "+groupTable+" SET enabled=0")
			require.NoError(t, err)
			_, err = a.storeDB.ExecContext(ctx, "UPDATE "+endpointTable+" SET failover_enabled=0")
			require.NoError(t, err)
			if provider == "claude" {
				cfg := config.NewDefaultConfig()
				disabled := false
				for i, member := range members[:3] {
					name := []string{"first", "backup", "paused"}[i]
					cfg.Endpoints = append(cfg.Endpoints, config.EndpointConfig{Name: name, Channel: name, CredentialGroupID: member.GroupID, URL: "https://" + name + ".invalid", ApiKey: "test-key", FailoverEnabled: &disabled})
				}
				a.endpointManager = claudeendpoint.NewManager(cfg)
			} else {
				require.NoError(t, a.codexEndpointMgr.Reload())
			}
			p := routing.DefaultProfile(provider)
			p.Name, p.IsDefault = "manual", true
			p.Members = []routing.RoutingMember{
				{ChannelID: members[2].ChannelID, GroupID: members[2].GroupID, Enabled: true},
				{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: false},
			}
			p, err = a.SaveRoutingProfile(p)
			require.NoError(t, err)
			status, err := a.CheckRoutingConfiguration(provider)
			require.NoError(t, err)
			require.True(t, status.Ready, "%+v", status.Checks)
			preview, err := a.PreviewDispatch(DispatchInput{Provider: provider, ProfileID: p.ID, Model: "test", Streaming: true})
			require.NoError(t, err)
			require.Len(t, preview.EligibleCandidates, 1)
			require.Equal(t, members[2].GroupID, preview.EligibleCandidates[0].GroupID)
			state, err := a.ListRoutingProfiles(provider)
			require.NoError(t, err)
			require.True(t, state.Accounts[0].Enabled, "new profiles must not inherit legacy group switches")
			var historicalEnabled bool
			require.NoError(t, a.storeDB.QueryRowContext(ctx, "SELECT enabled FROM "+groupTable+" WHERE id=?", members[0].GroupID).Scan(&historicalEnabled))
			require.False(t, historicalEnabled, "historical participation flags must remain intact")
			ref, err := a.quotaCredentialRef(ctx, provider, members[2].ChannelID, members[2].GroupID)
			require.NoError(t, err)
			require.NotEmpty(t, ref.IdentityHash())
			var disabledCount int
			require.NoError(t, a.storeDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+endpointTable+" WHERE failover_enabled=0").Scan(&disabledCount))
			require.Equal(t, 4, disabledCount, "unified preview must not rewrite historical flags")
		})
	}
}

func TestRoutingQuotaLearningValidatesBindingAndRetainsFactsOnFailure(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := dispatchTestApp(t, provider)
			ctx := context.Background()
			ref, err := a.quotaCredentialRef(ctx, provider, members[0].ChannelID, members[0].GroupID)
			require.NoError(t, err)
			require.NotEmpty(t, ref.AccountIDHash)
			require.Empty(t, ref.AccountID)
			_, err = a.quotaCredentialRef(ctx, provider, members[1].ChannelID, members[0].GroupID)
			require.Error(t, err)
			now := time.Now()
			used := 42.0
			q := routing.QuotaSnapshot{Provider: provider, ChannelID: ref.ChannelID, GroupID: ref.GroupID, AccountIDHash: ref.AccountIDHash, WindowName: "test", ModelScope: "m", UsedPercent: &used, ObservedAt: now, ExpiresAt: now.Add(time.Hour), Confidence: "learned"}
			require.NoError(t, a.persistRoutingQuota(ctx, []routing.QuotaSnapshot{q}))
			q.AccountIDHash = "obsolete"
			q.ObservedAt = now.Add(time.Second)
			v := 99.0
			q.UsedPercent = &v
			require.NoError(t, a.persistRoutingQuota(ctx, []routing.QuotaSnapshot{q}))
			quota, err := a.ListRoutingQuota(provider)
			require.NoError(t, err)
			require.Len(t, quota, 1)
			require.Equal(t, 42.0, *quota[0].UsedPercent)
		})
	}
}

func TestRoutingClassifierProcessesAllContextBeforeNetwork(t *testing.T) {
	a, _ := dispatchTestApp(t, "claude")
	ctx := context.Background()
	store := privacy.NewStore(a.storeDB)
	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.SaveSettings(ctx, privacy.DefaultSettings()))
	_, err := store.CreateRule(ctx, privacy.Rule{Name: "private marker", Enabled: true, MatchType: privacy.MatchTypeLiteral, Pattern: "PRIVATE", Action: privacy.ActionRedact, ReplacementMode: privacy.ReplacementPlaceholder, Placeholder: "[redacted]", Source: privacy.SourceCustom})
	require.NoError(t, err)
	a.privacyService = privacy.NewService(store)
	require.NoError(t, a.privacyService.Reload(ctx))
	p := routing.DefaultProfile("claude")
	p.Intents = []routing.IntentRule{{ID: "coding", Description: "programming", Enabled: true}}
	prompt, err := a.classifierPrompt(ctx, p, routing.TurnInput{CurrentUserMessage: "current PRIVATE", PreviousUserMessage: "previous PRIVATE", PreviousAssistantText: "assistant PRIVATE"})
	require.NoError(t, err)
	require.NotContains(t, prompt, "PRIVATE")
	require.Equal(t, 3, strings.Count(prompt, "[redacted]"))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(prompt), &decoded))
	require.Len(t, decoded["input"], 3)
	// A match crossing the retained head boundary must be redacted before
	// truncation can split it into a string the privacy rule no longer matches.
	longMessage := strings.Repeat("前", 2998) + "PRIVATE" + strings.Repeat("中", 2000) + strings.Repeat("尾", 1000)
	body, err := json.Marshal(map[string]any{"input": longMessage})
	require.NoError(t, err)
	turn := routing.ResolveTurn(ctx, http.Header{"X-Codex-Turn-Id": {"test-turn"}}, body, "codex")
	prompt, err = a.classifierPrompt(ctx, p, turn)
	require.NoError(t, err)
	var limited struct {
		Input map[string]string `json:"input"`
	}
	require.NoError(t, json.Unmarshal([]byte(prompt), &limited))
	require.NotContains(t, limited.Input["current_user_message"], "PR")
	require.Contains(t, limited.Input["current_user_message"], "[context truncated]")
	require.True(t, strings.HasSuffix(limited.Input["current_user_message"], strings.Repeat("尾", 1000)))
	settings := privacy.DefaultSettings()
	settings.Mode = privacy.ModeDisabled
	require.NoError(t, store.SaveSettings(ctx, settings))
	require.NoError(t, a.privacyService.Reload(ctx))
	_, err = a.classifierPrompt(ctx, p, routing.TurnInput{CurrentUserMessage: "PRIVATE"})
	require.ErrorContains(t, err, "classifier_privacy_rejected")
}

func TestRoutingClassifierSendsRedactedInputWithSelectedMultiKeyCredential(t *testing.T) {
	a, members := dispatchTestApp(t, "claude")
	ctx := context.Background()
	store := privacy.NewStore(a.storeDB)
	require.NoError(t, store.EnsureSchema(ctx))
	a.privacyService = privacy.NewService(store)
	_, err := store.CreateRule(ctx, privacy.Rule{Name: "private", Enabled: true, MatchType: privacy.MatchTypeLiteral, Pattern: "PRIVATE", Action: privacy.ActionRedact, ReplacementMode: privacy.ReplacementPlaceholder, Placeholder: "[redacted]", Source: privacy.SourceCustom})
	require.NoError(t, err)
	require.NoError(t, a.privacyService.Reload(ctx))
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		require.Equal(t, "selected-key", r.Header.Get("X-Api-Key"))
		require.Empty(t, r.Header.Get("X-Relay-Key"))
		body, _ := io.ReadAll(r.Body)
		require.NotContains(t, string(body), "PRIVATE")
		require.Contains(t, string(body), "[redacted]")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"{\"intent_id\":\"coding\",\"confidence\":0.95}"}],"usage":{"input_tokens":12,"output_tokens":4,"cache_read_input_tokens":3}}`)
	}))
	defer server.Close()
	cfg := config.NewDefaultConfig()
	cfg.Endpoints = []config.EndpointConfig{{Name: "first", Channel: "first", CredentialGroupID: members[0].GroupID, URL: server.URL, SupportedModels: []string{"router"}, ApiKeys: []config.ApiKeyConfig{{Name: "multi", Value: "selected-key"}}, Headers: map[string]string{"X-Relay-Key": "local-key-must-not-leak"}}}
	a.endpointManager = claudeendpoint.NewManager(cfg)
	p := routing.DefaultProfile("claude")
	p.Name = "classifier"
	p.Classifier = &routing.ClassifierConfig{Enabled: true, Model: "router", Provider: "claude", ChannelID: members[0].ChannelID, GroupID: members[0].GroupID}
	p.Intents = []routing.IntentRule{{ID: "coding", Name: "coding", Description: "programming", Enabled: true}}
	a.usageTracker = &tracking.UsageTracker{}
	a.usageTracker.UpdatePricing(map[string]tracking.ModelPricing{tracking.ModelPricingKey("claude", "router", ""): {Input: 10, Output: 20, CacheRead: 1}})
	balanceInput := routing.ManualBalanceUpdate{Provider: "claude", ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Action: "calibrate", AmountUSD: "1", StopWhenExhausted: true, OperationID: "classifier-initial-balance"}
	_, err = a.UpdateManualBalance(balanceInput)
	require.NoError(t, err)
	result, err := a.callIntentClassifier(ctx, p, routing.TurnInput{CurrentUserMessage: "PRIVATE request", PreviousUserMessage: "PRIVATE context", PreviousAssistantText: "PRIVATE summary"})
	require.NoError(t, err)
	require.Equal(t, "coding", result.IntentID)
	require.EqualValues(t, 12, result.InputTokens)
	require.EqualValues(t, 4, result.OutputTokens)
	require.Equal(t, 1, sends)

	p.Classifier.Model = "unsupported"
	_, err = a.callIntentClassifier(ctx, p, routing.TurnInput{CurrentUserMessage: "request"})
	require.ErrorContains(t, err, "classifier_candidate_unavailable")
	require.Equal(t, 1, sends, "unsupported classifier models must be rejected before sending")
	p.Classifier.Model = "router"
	balances, err := a.ListManualBalances("claude")
	require.NoError(t, err)
	require.Len(t, balances, 1)
	require.Equal(t, "0.000203", balances[0].SpentUSD)
	balanceInput.AmountUSD, balanceInput.Revision, balanceInput.OperationID = "0", balances[0].Revision, "classifier-empty-balance"
	_, err = a.UpdateManualBalance(balanceInput)
	require.NoError(t, err)
	_, err = a.callIntentClassifier(ctx, p, routing.TurnInput{CurrentUserMessage: "request"})
	require.ErrorContains(t, err, "classifier_candidate_unavailable")
	require.Equal(t, 1, sends, "exhausted classifier balance must be rejected before sending")
}
