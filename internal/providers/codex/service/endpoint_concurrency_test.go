package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"

	"github.com/stretchr/testify/require"
)

func TestCodexEndpointDisableAndDeleteGroupWhileReadingRequestHistory(t *testing.T) {
	adapter, err := tracking.NewSQLiteAdapter(tracking.DatabaseConfig{
		DatabasePath: filepath.Join(t.TempDir(), "usage.db"),
	})
	require.NoError(t, err)
	require.NoError(t, adapter.Open())
	defer adapter.Close()
	require.NoError(t, adapter.InitSchema())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := adapter.GetWriteDB()
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	service := NewCodexEndpointService(endpointStore, channelStore, groupStore)
	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{Name: "active-channel"})
	require.NoError(t, err)
	require.NoError(t, channelStore.SetActive(ctx, channel.ID))
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID, Name: "default", ApiKey: "test-default-key", IsDefault: true,
	})
	require.NoError(t, err)
	targetGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID, Name: "target", ApiKey: "test-target-key",
	})
	require.NoError(t, err)

	createEndpoint := func(groupID int64, name string) *store.CodexEndpointRecord {
		t.Helper()
		record, err := service.CreateEndpoint(ctx, &store.CodexEndpointRecord{
			ChannelID: channel.ID, CredentialGroupID: groupID, Name: name,
			BaseURL: "https://" + name + ".example.invalid/v1", FailoverEnabled: true,
		})
		require.NoError(t, err)
		return record
	}
	first := createEndpoint(targetGroup.ID, "first")
	second := createEndpoint(targetGroup.ID, "second")
	survivor := createEndpoint(defaultGroup.ID, "survivor")
	require.True(t, first.IsActive)
	_, err = db.ExecContext(ctx, `INSERT INTO request_logs (request_id, start_time, status, provider) VALUES ('request-before-toggle', ?, 'completed', 'codex')`, time.Now())
	require.NoError(t, err)

	// Keep a request-history read transaction open across both configuration
	// updates, as can happen when the tracking list and proxy are in use.
	reader, err := adapter.GetReadDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer reader.Rollback()
	var count int
	require.NoError(t, reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs").Scan(&count))
	require.Equal(t, 1, count)
	for _, endpoint := range []*store.CodexEndpointRecord{first, second} {
		require.NoError(t, service.UpdateEndpointByID(ctx, endpoint.ID, endpoint.Name, endpoint.BaseURL, endpoint.CredentialGroupID, endpoint.ApiKey, false, endpoint.SkipHealthCheck, endpoint.Priority, endpoint.TimeoutSeconds, endpoint.CostMultiplier, endpoint.InputCostMultiplier, endpoint.OutputCostMultiplier, endpoint.CacheReadCostMultiplier, endpoint.BillingOutputTokenSource, endpoint.ModelMultipliers))
	}
	for _, endpoint := range []*store.CodexEndpointRecord{first, second} {
		updated, err := endpointStore.GetByID(ctx, endpoint.ID)
		require.NoError(t, err)
		require.False(t, updated.FailoverEnabled)
		require.False(t, updated.IsActive)
	}
	active, err := endpointStore.GetActiveByChannelID(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, survivor.ID, active.ID)
	activeChannel, err := channelStore.GetActive(ctx)
	require.NoError(t, err)
	require.Equal(t, channel.ID, activeChannel.ID)
	groupService := NewCodexCredentialGroupService(groupStore, channelStore, endpointStore)
	require.NoError(t, groupService.DeleteGroup(ctx, targetGroup.ID))
	deletedGroup, err := groupStore.GetByID(ctx, targetGroup.ID)
	require.NoError(t, err)
	require.Nil(t, deletedGroup)
	remaining, err := endpointStore.ListByChannelID(ctx, channel.ID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, survivor.ID, remaining[0].ID)
	require.True(t, remaining[0].IsActive)
	require.NoError(t, reader.Rollback())

	// The same write pool must still accept the next request's tracking data.
	next, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer next.Rollback()
	_, err = next.ExecContext(ctx, `INSERT INTO request_logs (request_id, start_time, status, provider) VALUES ('request-after-toggle', ?, 'completed', 'codex')`, time.Now())
	require.NoError(t, err)
	require.NoError(t, next.Commit())
	require.NoError(t, adapter.GetReadDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs").Scan(&count))
	require.Equal(t, 2, count)
}
