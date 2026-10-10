package main

import (
	"context"
	"path/filepath"
	"testing"

	codexproxy "ai-relay-forwarder/internal/providers/codex/proxy"
	codexstore "ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/tracking"

	"github.com/stretchr/testify/require"
)

func TestDeleteCodexCredentialGroupRefreshesRuntimeWithoutChangingActiveChannel(t *testing.T) {
	adapter, err := tracking.NewSQLiteAdapter(tracking.DatabaseConfig{
		DatabasePath: filepath.Join(t.TempDir(), "usage.db"),
	})
	require.NoError(t, err)
	require.NoError(t, adapter.Open())
	defer adapter.Close()
	require.NoError(t, adapter.InitSchema())

	app := &App{storeDB: adapter.GetWriteDB()}
	app.ensureCodexProviderService()
	ctx := context.Background()
	channel, err := app.codexChannelStore.Create(ctx, &codexstore.CodexChannelRecord{
		Name: "active-channel", Model: "gpt-test", FailoverEnabled: true,
	})
	require.NoError(t, err)
	require.NoError(t, app.codexChannelStore.SetActive(ctx, channel.ID))
	keptGroup, err := app.codexCredentialGroupStore.Create(ctx, &codexstore.CodexCredentialGroupRecord{
		ChannelID: channel.ID, Name: "default", ApiKey: "test-default-key", IsDefault: true,
	})
	require.NoError(t, err)
	targetGroup, err := app.codexCredentialGroupStore.Create(ctx, &codexstore.CodexCredentialGroupRecord{
		ChannelID: channel.ID, Name: "target", ApiKey: "test-target-key",
	})
	require.NoError(t, err)
	keptEndpoint, err := app.codexEndpointStore.Create(ctx, &codexstore.CodexEndpointRecord{
		ChannelID: channel.ID, CredentialGroupID: keptGroup.ID, Name: "kept",
		BaseURL: "https://kept.example.invalid/v1", FailoverEnabled: true, IsActive: true, SkipHealthCheck: true,
	})
	require.NoError(t, err)
	for _, name := range []string{"first", "second"} {
		_, err := app.codexEndpointStore.Create(ctx, &codexstore.CodexEndpointRecord{
			ChannelID: channel.ID, CredentialGroupID: targetGroup.ID, Name: name,
			BaseURL: "https://" + name + ".example.invalid/v1", FailoverEnabled: false,
		})
		require.NoError(t, err)
	}
	app.codexEndpointMgr = codexproxy.NewEndpointManager(app.codexChannelStore, app.codexEndpointStore, app.codexCredentialGroupStore, nil)
	require.NoError(t, app.codexEndpointMgr.Reload())
	require.Len(t, app.codexEndpointMgr.GetAllEndpoints(), 3)

	require.NoError(t, app.DeleteCodexCredentialGroupByID(targetGroup.ID))
	groupAfter, err := app.codexCredentialGroupStore.GetByID(ctx, targetGroup.ID)
	require.NoError(t, err)
	require.Nil(t, groupAfter)
	remaining, err := app.codexEndpointStore.ListByChannelID(ctx, channel.ID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, keptEndpoint.ID, remaining[0].ID)
	require.Equal(t, channel.ID, app.codexEndpointMgr.GetActiveChannel().ID)
	require.Len(t, app.codexEndpointMgr.GetAllEndpoints(), 1)
	primary, err := app.codexEndpointMgr.GetPrimaryEndpoint()
	require.NoError(t, err)
	require.Equal(t, keptEndpoint.ID, primary.ID)
}
