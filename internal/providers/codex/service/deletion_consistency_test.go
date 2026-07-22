package service

import (
	"context"
	"testing"

	"ai-relay-forwarder/internal/providers/codex/store"
)

func TestCodexSetActiveMissingTargetPreservesCurrentActive(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)

	activeChannel := mustCreateCodexTestChannel(t, ctx, channelStore, "active", 0, true, true)
	if err := channelStore.SetActive(ctx, activeChannel.ID+1000); err == nil {
		t.Fatal("expected missing channel activation to fail")
	}
	currentChannel, err := channelStore.GetActive(ctx)
	if err != nil {
		t.Fatalf("read active channel: %v", err)
	}
	if currentChannel == nil || currentChannel.ID != activeChannel.ID {
		t.Fatalf("missing target must not clear active channel, got %#v", currentChannel)
	}

	activeEndpoint := mustCreateCodexTestEndpoint(t, ctx, endpointStore, activeChannel.ID, "active", 0, true, true)
	if err := endpointStore.SetActive(ctx, activeEndpoint.ID+1000); err == nil {
		t.Fatal("expected missing endpoint activation to fail")
	}
	currentEndpoint, err := endpointStore.GetActiveByChannelID(ctx, activeChannel.ID)
	if err != nil {
		t.Fatalf("read active endpoint: %v", err)
	}
	if currentEndpoint == nil || currentEndpoint.ID != activeEndpoint.ID {
		t.Fatalf("missing target must not clear active endpoint, got %#v", currentEndpoint)
	}
}

func TestCodexChannelDeletionSuccessorSkipsPausedCandidatesAndUsesPriority(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	channelService := NewCodexChannelService(channelStore, endpointStore)

	target := mustCreateCodexTestChannel(t, ctx, channelStore, "target", 0, true, true)
	pausedChannel := mustCreateCodexTestChannel(t, ctx, channelStore, "paused-channel", 1, false, false)
	blockedChannel := mustCreateCodexTestChannel(t, ctx, channelStore, "blocked-channel", 2, true, false)
	selectedChannel := mustCreateCodexTestChannel(t, ctx, channelStore, "selected-channel", 3, true, false)
	laterChannel := mustCreateCodexTestChannel(t, ctx, channelStore, "later-channel", 4, true, false)

	mustCreateCodexTestEndpoint(t, ctx, endpointStore, pausedChannel.ID, "paused-channel-eligible", 0, true, true)
	mustCreateCodexTestEndpoint(t, ctx, endpointStore, blockedChannel.ID, "blocked-channel-paused", 0, false, true)
	mustCreateCodexTestEndpoint(t, ctx, endpointStore, selectedChannel.ID, "selected-paused", 0, false, true)
	selectedEndpoint := mustCreateCodexTestEndpoint(t, ctx, endpointStore, selectedChannel.ID, "selected-eligible", 1, true, false)
	mustCreateCodexTestEndpoint(t, ctx, endpointStore, laterChannel.ID, "later-eligible", 0, true, true)

	nextChannel, nextEndpoint, err := channelService.GetChannelDeletionSuccessor(ctx, target.ID)
	if err != nil {
		t.Fatalf("select channel deletion successor: %v", err)
	}
	if nextChannel == nil || nextChannel.ID != selectedChannel.ID {
		t.Fatalf("selected channel = %#v, want %d", nextChannel, selectedChannel.ID)
	}
	if nextEndpoint == nil || nextEndpoint.ID != selectedEndpoint.ID {
		t.Fatalf("selected endpoint = %#v, want %d", nextEndpoint, selectedEndpoint.ID)
	}

	if err := channelService.PrepareChannelDeletion(ctx, target.ID, selectedChannel.ID, selectedEndpoint.ID); err != nil {
		t.Fatalf("prepare channel deletion: %v", err)
	}
	stagedTarget, err := channelStore.GetByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("read staged channel: %v", err)
	}
	if stagedTarget == nil || stagedTarget.FailoverEnabled || stagedTarget.IsActive {
		t.Fatalf("target channel must be inactive and excluded from failover, got %#v", stagedTarget)
	}
	activeChannel, err := channelStore.GetActive(ctx)
	if err != nil {
		t.Fatalf("read active channel: %v", err)
	}
	if activeChannel == nil || activeChannel.ID != selectedChannel.ID {
		t.Fatalf("active channel = %#v, want %d", activeChannel, selectedChannel.ID)
	}
	activeEndpoint, err := endpointStore.GetActiveByChannelID(ctx, selectedChannel.ID)
	if err != nil {
		t.Fatalf("read active endpoint: %v", err)
	}
	if activeEndpoint == nil || activeEndpoint.ID != selectedEndpoint.ID {
		t.Fatalf("active endpoint = %#v, want %d", activeEndpoint, selectedEndpoint.ID)
	}
}

func TestCodexPrepareEndpointDeletionSkipsPausedCandidateAndMakesTargetInert(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, nil)

	channel := mustCreateCodexTestChannel(t, ctx, channelStore, "endpoint-deletion", 0, true, true)
	target := mustCreateCodexTestEndpoint(t, ctx, endpointStore, channel.ID, "target", 0, true, true)
	mustCreateCodexTestEndpoint(t, ctx, endpointStore, channel.ID, "paused", 1, false, false)
	selected := mustCreateCodexTestEndpoint(t, ctx, endpointStore, channel.ID, "selected", 2, true, false)
	mustCreateCodexTestEndpoint(t, ctx, endpointStore, channel.ID, "later", 3, true, false)

	next, err := endpointService.GetEndpointDeletionSuccessor(ctx, target.ID)
	if err != nil {
		t.Fatalf("select endpoint deletion successor: %v", err)
	}
	if next == nil || next.ID != selected.ID {
		t.Fatalf("selected endpoint = %#v, want %d", next, selected.ID)
	}
	if err := endpointService.PrepareEndpointDeletion(ctx, target.ID, selected.ID); err != nil {
		t.Fatalf("prepare endpoint deletion: %v", err)
	}

	stagedTarget, err := endpointStore.GetByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("read staged endpoint: %v", err)
	}
	if stagedTarget == nil || stagedTarget.FailoverEnabled || stagedTarget.IsActive {
		t.Fatalf("target must be inactive and excluded from failover, got %#v", stagedTarget)
	}
	activeEndpoint, err := endpointStore.GetActiveByChannelID(ctx, channel.ID)
	if err != nil {
		t.Fatalf("read active endpoint: %v", err)
	}
	if activeEndpoint == nil || activeEndpoint.ID != selected.ID {
		t.Fatalf("active endpoint = %#v, want %d", activeEndpoint, selected.ID)
	}
}

func mustCreateCodexTestChannel(t *testing.T, ctx context.Context, channelStore *store.SQLiteCodexChannelStore, name string, priority int, failoverEnabled, isActive bool) *store.CodexChannelRecord {
	t.Helper()
	record, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:            name,
		ProviderType:    "openai-compatible",
		Priority:        priority,
		FailoverEnabled: failoverEnabled,
		IsActive:        isActive,
	})
	if err != nil {
		t.Fatalf("create channel %q: %v", name, err)
	}
	return record
}

func mustCreateCodexTestEndpoint(t *testing.T, ctx context.Context, endpointStore *store.SQLiteCodexEndpointStore, channelID int64, name string, priority int, failoverEnabled, isActive bool) *store.CodexEndpointRecord {
	t.Helper()
	record, err := endpointStore.Create(ctx, &store.CodexEndpointRecord{
		ChannelID:       channelID,
		Name:            name,
		BaseURL:         "https://" + name + ".example.invalid/v1",
		Priority:        priority,
		FailoverEnabled: failoverEnabled,
		IsActive:        isActive,
	})
	if err != nil {
		t.Fatalf("create endpoint %q: %v", name, err)
	}
	return record
}
