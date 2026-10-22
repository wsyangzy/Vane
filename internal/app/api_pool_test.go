package app

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudesub "github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexsub "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func poolTestApp(t *testing.T, provider string) (*App, []routing.PoolMember) {
	t.Helper()
	adapter, err := tracking.NewSQLiteAdapter(tracking.DatabaseConfig{DatabasePath: filepath.Join(t.TempDir(), "pool.db")})
	require.NoError(t, err)
	require.NoError(t, adapter.Open())
	t.Cleanup(func() { require.NoError(t, adapter.Close()) })
	require.NoError(t, adapter.InitSchema())
	cfg := config.NewDefaultConfig()
	cfg.Failover.Enabled, cfg.CodexFailover.Enabled = true, true
	cfg.Endpoints = []config.EndpointConfig{{Name: "first", Channel: "first", URL: "https://first.invalid", CredentialGroupID: 1}}
	a := &App{storeDB: adapter.GetWriteDB(), config: cfg, endpointManager: claudeendpoint.NewManager(cfg)}
	a.channelStore = claudestore.NewSQLiteChannelStore(a.storeDB)
	a.claudeCredentialGroupStore = claudestore.NewSQLiteClaudeCredentialGroupStore(a.storeDB)
	a.endpointStore = claudestore.NewSQLiteEndpointStore(a.storeDB)
	a.ensureCodexProviderService()
	require.NoError(t, a.endpointManager.ManualActivateGroup("first"))
	var members []routing.PoolMember
	for i, name := range []string{"first", "backup", "paused", "managed"} {
		ctx := context.Background()
		var channelID, groupID int64
		if provider == "claude" {
			ch, e := a.channelStore.Create(ctx, &claudestore.ChannelRecord{Name: name, Priority: i, FailoverEnabled: name != "paused"})
			require.NoError(t, e)
			g := &claudestore.ClaudeCredentialGroupRecord{Channel: name, Name: name, ApiKey: "test-api-key", IsDefault: true}
			if name == "managed" {
				g.AuthMode, g.ApiKey, g.Subscription = claudesub.ClientPassthrough, "", &claudesub.Binding{AccountID: "test-account", Version: 1, Enabled: true}
			}
			group, e := a.claudeCredentialGroupStore.Create(ctx, g)
			require.NoError(t, e)
			channelID, groupID = ch.ID, group.ID
			_, e = a.endpointStore.Create(ctx, &claudestore.EndpointRecord{Channel: name, CredentialGroupID: groupID, Name: name, URL: "https://" + name + ".invalid", FailoverEnabled: true})
			require.NoError(t, e)
		} else {
			ch, e := a.codexChannelStore.Create(ctx, &codexstore.CodexChannelRecord{Name: name, Model: "test", Priority: i, FailoverEnabled: name != "paused"})
			require.NoError(t, e)
			g := &codexstore.CodexCredentialGroupRecord{ChannelID: ch.ID, Name: name, ApiKey: "test-api-key", IsDefault: true}
			if name == "managed" {
				g.AuthMode, g.ApiKey, g.Subscription = codexsub.ClientPassthrough, "", &codexsub.Binding{AccountID: "test-account", Version: 1, Enabled: true}
			}
			group, e := a.codexCredentialGroupStore.Create(ctx, g)
			require.NoError(t, e)
			channelID, groupID = ch.ID, group.ID
			_, e = a.codexEndpointStore.Create(ctx, &codexstore.CodexEndpointRecord{ChannelID: channelID, CredentialGroupID: groupID, Name: name, BaseURL: "https://" + name + ".invalid/v1", FailoverEnabled: true})
			require.NoError(t, e)
			if i == 0 {
				require.NoError(t, a.codexChannelStore.SetActive(ctx, channelID))
			}
		}
		members = append(members, routing.PoolMember{ChannelID: channelID, GroupID: groupID, Tier: routing.TierPrimary})
	}
	return a, members
}

func TestAccountPoolMigrationBacksUpAndPreservesLegacyRules(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, members := poolTestApp(t, provider)
			ctx := context.Background()
			legacy := routingstore.NewSQLiteModelRouteStore(a.storeDB)
			_, err := legacy.Create(ctx, &routingstore.ModelRouteRecord{Provider: provider, SourceChannelID: members[0].ChannelID, Name: "disabled-rule", Enabled: false, MatchModel: "test-*", Mode: "prefer", Targets: []routing.Target{{ChannelID: members[1].ChannelID, GroupID: members[1].GroupID}}})
			require.NoError(t, err)
			state, err := a.ListAccountPools(provider)
			require.NoError(t, err)
			require.Len(t, state.Pools, 4)
			require.Len(t, state.Accounts, 4, "managed Codex accounts remain available for explicit pool membership")
			var first routing.Pool
			for _, pool := range state.Pools {
				if pool.Default {
					first = pool
				}
				for _, member := range pool.Members {
					require.NotEqual(t, members[3].GroupID, member.GroupID, "migration must not grant managed account access")
				}
			}
			require.Equal(t, "first", first.Name)
			require.Equal(t, []routing.PoolMember{members[0], {ChannelID: members[1].ChannelID, GroupID: members[1].GroupID, Tier: routing.TierBackup}}, first.Members)
			require.Len(t, first.Rules, 1)
			require.True(t, first.Rules[0].Disabled)
			require.False(t, state.Accounts[2].Enabled, "legacy paused channel becomes disabled accounts")
			again, err := a.ListAccountPools(provider)
			require.NoError(t, err)
			require.Equal(t, state.Pools, again.Pools)
			var path string
			require.NoError(t, a.storeDB.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path))
			backup, err := sql.Open("sqlite", path+".before-pools-"+provider+".bak")
			require.NoError(t, err)
			defer backup.Close()
			var count int
			require.NoError(t, backup.QueryRow(`SELECT COUNT(*) FROM model_routes WHERE name = 'disabled-rule'`).Scan(&count))
			require.Equal(t, 1, count)
			require.NoError(t, backup.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'account_pools'`).Scan(&count))
			require.Zero(t, count, "backup must precede pool schema changes")
		})
	}
}

func TestPoolConnectionPreviewRejectsStaleKeyAndRestoresConfig(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			a, _ := poolTestApp(t, provider)
			clientDir := t.TempDir()
			t.Setenv("CODEX_HOME", clientDir)
			t.Setenv("CLAUDE_CONFIG_DIR", clientDir)
			path, err := subscriptionConfigPath(provider)
			require.NoError(t, err)
			original := `{"env":{"KEEP":"existing-private-value"}}`
			if provider == "codex" {
				original = "model_reasoning_effort = 'high'\n"
			}
			require.NoError(t, os.WriteFile(path, []byte(original), 0600))
			state, err := a.ListAccountPools(provider)
			require.NoError(t, err)
			pool := state.Pools[0]
			key, err := a.RotateAccountPoolKey(provider, pool.ID)
			require.NoError(t, err)
			a.isRunning, a.proxyServer = true, &http.Server{}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			a.config.Server.Host, a.config.Server.Port = "127.0.0.1", listener.Addr().(*net.TCPAddr).Port
			input := SubscriptionConnectionCommand{Provider: provider, Action: "preview", PoolID: pool.ID, LocalKey: key.Key}
			preview, err := a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			require.Contains(t, preview.Snippet, key.Key)
			require.NotContains(t, preview.Snippet, "existing-private-value")
			current, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, string(current), "preview is read-only")
			input.Action, input.Revision, input.BindingVersion, input.ContentRevision = "apply", preview.Revision, preview.BindingVersion, "stale"
			_, err = a.ManageSubscriptionConnection(input)
			require.Error(t, err)
			input.ContentRevision = preview.ContentRevision
			applied, err := a.ManageSubscriptionConnection(input)
			require.NoError(t, err)
			require.True(t, applied.BackupAvailable)
			_, err = a.RotateAccountPoolKey(provider, pool.ID)
			require.NoError(t, err)
			input.Action = "preview"
			_, err = a.ManageSubscriptionConnection(input)
			require.ErrorContains(t, err, "密钥")
			_, err = a.ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: provider, Action: "restore", Revision: applied.Revision})
			require.NoError(t, err)
			current, err = os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSpace(original), strings.TrimSpace(string(current)))
		})
	}
}
