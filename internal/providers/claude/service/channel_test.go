package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/store"

	_ "modernc.org/sqlite"
)

func newChannelServiceTestStore(t *testing.T) (*ChannelService, *store.SQLiteChannelStore) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	const schema = `
CREATE TABLE channels (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT UNIQUE NOT NULL,
	website TEXT,
	priority INTEGER DEFAULT 1,
	failover_enabled INTEGER DEFAULT 1,
	model_mapping TEXT NOT NULL DEFAULT '{}',
	non_stream_route TEXT,
	privacy_level TEXT,
	created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
	updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create channels table: %v", err)
	}

	channelStore := store.NewSQLiteChannelStore(db)
	return NewChannelService(channelStore), channelStore
}

func TestCreateChannelAutoAssignsNextPriority(t *testing.T) {
	service, _ := newChannelServiceTestStore(t)
	ctx := context.Background()

	first, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "first"})
	if err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	second, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "second"})
	if err != nil {
		t.Fatalf("create second channel: %v", err)
	}

	if first.Priority != 1 || second.Priority != 2 {
		t.Fatalf("expected sequential priorities 1 and 2, got %d and %d", first.Priority, second.Priority)
	}
}

func TestCreateChannelMovesOccupiedPriorityToDefaultTail(t *testing.T) {
	service, channelStore := newChannelServiceTestStore(t)
	ctx := context.Background()

	if _, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "first", Priority: 1}); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	if _, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "second", Priority: 2}); err != nil {
		t.Fatalf("create second channel: %v", err)
	}
	created, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "inserted", Priority: 1})
	if err != nil {
		t.Fatalf("create inserted channel: %v", err)
	}

	moved, err := channelStore.Get(ctx, "first")
	if err != nil {
		t.Fatalf("get moved channel: %v", err)
	}
	unchanged, err := channelStore.Get(ctx, "second")
	if err != nil {
		t.Fatalf("get unchanged channel: %v", err)
	}
	if created.Priority != 1 || moved.Priority != 3 || unchanged.Priority != 2 {
		t.Fatalf("expected inserted=1, first=3, second=2; got inserted=%d first=%d second=%d", created.Priority, moved.Priority, unchanged.Priority)
	}
}

func TestCreateChannelChecksDuplicateNameBeforeMovingPriority(t *testing.T) {
	service, channelStore := newChannelServiceTestStore(t)
	ctx := context.Background()

	if _, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "first", Priority: 1}); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	if _, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "second", Priority: 2}); err != nil {
		t.Fatalf("create second channel: %v", err)
	}
	if _, err := service.CreateChannel(ctx, &store.ChannelRecord{Name: "second", Priority: 1}); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("expected duplicate-name error, got %v", err)
	}

	first, err := channelStore.Get(ctx, "first")
	if err != nil {
		t.Fatalf("get first channel: %v", err)
	}
	second, err := channelStore.Get(ctx, "second")
	if err != nil {
		t.Fatalf("get second channel: %v", err)
	}
	if first.Priority != 1 || second.Priority != 2 {
		t.Fatalf("duplicate create must not move priorities, got first=%d second=%d", first.Priority, second.Priority)
	}
}

func TestEnsureActiveChannelParticipatesResumesOnlyActiveChannel(t *testing.T) {
	channelService, channelStore := newChannelServiceTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"active", "paused-backup"} {
		if _, err := channelService.CreateChannel(ctx, &store.ChannelRecord{Name: name}); err != nil {
			t.Fatalf("create channel %s: %v", name, err)
		}
		if err := channelService.SetFailoverEnabled(ctx, name, false); err != nil {
			t.Fatalf("pause channel %s: %v", name, err)
		}
	}

	cfg := &config.Config{
		Endpoints: []config.EndpointConfig{
			{Name: "active-endpoint", URL: "http://active.invalid", Channel: "active", Timeout: time.Second},
			{Name: "backup-endpoint", URL: "http://backup.invalid", Channel: "paused-backup", Timeout: time.Second},
		},
	}
	manager := endpoint.NewManager(cfg)
	if err := manager.ManualActivateGroup("active"); err != nil {
		t.Fatalf("activate runtime channel: %v", err)
	}
	manager.SetSingleChannelFailoverEnabled("active", false)
	manager.SetSingleChannelFailoverEnabled("paused-backup", false)
	endpointService := NewEndpointService(nil, manager, cfg)

	resumed, err := endpointService.EnsureActiveChannelParticipates(ctx, channelService)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed || !manager.GetGroupManager().IsChannelFailoverEnabled("active") {
		t.Fatal("expected the active Claude channel to resume failover participation")
	}
	if manager.GetGroupManager().IsChannelFailoverEnabled("paused-backup") {
		t.Fatal("expected the inactive Claude channel to remain paused at runtime")
	}

	active, err := channelStore.Get(ctx, "active")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := channelStore.Get(ctx, "paused-backup")
	if err != nil {
		t.Fatal(err)
	}
	if !active.FailoverEnabled || backup.FailoverEnabled {
		t.Fatalf("expected only active channel resumed in SQLite, active=%v backup=%v", active.FailoverEnabled, backup.FailoverEnabled)
	}

	resumed, err = endpointService.EnsureActiveChannelParticipates(ctx, channelService)
	if err != nil || resumed {
		t.Fatalf("expected an idempotent no-op, resumed=%v err=%v", resumed, err)
	}
}
