package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudesub "github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
	codexsub "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
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

func TestRetiredPoolConnectionCannotReadOrRewriteClientConfig(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CODEX_HOME", dir)
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			path, err := subscriptionConfigPath(provider)
			require.NoError(t, err)
			original := []byte("retained-client-configuration")
			require.NoError(t, os.WriteFile(path, original, 0600))
			for _, action := range []string{"status", "preview", "apply", "prepare_endpoint", "restore"} {
				_, err = (&App{}).ManageSubscriptionConnection(SubscriptionConnectionCommand{Provider: provider, PoolID: 1, Action: action, LocalKey: "vane_pool_old"})
				require.ErrorContains(t, err, "legacy_pool_entry_retired")
				actual, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, original, actual)
			}
		})
	}
}
