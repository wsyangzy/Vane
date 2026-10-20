package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/routing"

	_ "modernc.org/sqlite"
)

func newModelRouteTestStore(t *testing.T) *SQLiteModelRouteStore {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "model-routes.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE model_routes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    source_channel_id INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 100,
    match_model TEXT NOT NULL,
    mode TEXT NOT NULL,
    targets TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    UNIQUE(provider, name)
);`); err != nil {
		t.Fatalf("create model_routes: %v", err)
	}
	return NewSQLiteModelRouteStore(db)
}

func TestModelRouteStoreScopesRulesToSourceChannel(t *testing.T) {
	ctx := context.Background()
	routeStore := newModelRouteTestStore(t)
	for _, record := range []*ModelRouteRecord{
		{Provider: "claude", SourceChannelID: 1, Name: "a-haiku", Enabled: true, MatchModel: "claude-haiku-*", Mode: routing.ModePrefer, Targets: []routing.Target{{ChannelID: 3}}},
		{Provider: "claude", SourceChannelID: 2, Name: "b-haiku", Enabled: true, MatchModel: "claude-haiku-*", Mode: routing.ModePrefer, Targets: []routing.Target{{ChannelID: 3}}},
		{Provider: "codex", SourceChannelID: 1, Name: "codex-1", Enabled: true, MatchModel: "gpt-*", Mode: routing.ModePrefer, Targets: []routing.Target{{ChannelID: 4}}},
	} {
		if _, err := routeStore.Create(ctx, record); err != nil {
			t.Fatalf("create %s: %v", record.Name, err)
		}
	}

	// 同一 ID 的 Claude 渠道与 Codex 渠道互不影响
	claudeOne, err := routeStore.ListBySource(ctx, "claude", 1)
	if err != nil || len(claudeOne) != 1 || claudeOne[0].Name != "a-haiku" || claudeOne[0].SourceChannelID != 1 {
		t.Fatalf("claude channel 1 rules = %+v err=%v", claudeOne, err)
	}
	if err := routeStore.DeleteBySource(ctx, "claude", 1); err != nil {
		t.Fatalf("delete by source: %v", err)
	}
	if rest, _ := routeStore.List(ctx, "claude"); len(rest) != 1 || rest[0].Name != "b-haiku" {
		t.Fatalf("deleting channel 1 must keep channel 2 rules, got %+v", rest)
	}
	if codex, _ := routeStore.ListBySource(ctx, "codex", 1); len(codex) != 1 {
		t.Fatalf("deleting claude channel 1 must not touch codex channel 1, got %+v", codex)
	}
}

func TestModelRouteStoreRoundTripAndProviderIsolation(t *testing.T) {
	ctx := context.Background()
	routeStore := newModelRouteTestStore(t)

	claudeRule, err := routeStore.Create(ctx, &ModelRouteRecord{
		Provider: "claude", Name: "haiku 改道", Enabled: true, Priority: 10,
		MatchModel: "claude-haiku-*", Mode: routing.ModePrefer,
		Targets: []routing.Target{{ChannelID: 3, GroupID: 0, UpstreamModel: " deepseek-chat "}},
	})
	if err != nil {
		t.Fatalf("create claude rule: %v", err)
	}
	if claudeRule.ID == 0 || len(claudeRule.Targets) != 1 || claudeRule.Targets[0].UpstreamModel != "deepseek-chat" {
		t.Fatalf("unexpected created rule: %+v", claudeRule)
	}
	// 两个 provider 允许同名规则
	if _, err := routeStore.Create(ctx, &ModelRouteRecord{
		Provider: "codex", Name: "haiku 改道", Enabled: false, Priority: 10,
		MatchModel: "gpt-5*", Mode: routing.ModeFallback, Targets: []routing.Target{{ChannelID: 9}},
	}); err != nil {
		t.Fatalf("same name under another provider must be allowed: %v", err)
	}
	if _, err := routeStore.Create(ctx, &ModelRouteRecord{
		Provider: "claude", Name: "haiku 改道", MatchModel: "x", Mode: routing.ModePrefer, Targets: []routing.Target{{ChannelID: 1}},
	}); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("duplicate name in the same provider must be rejected, got %v", err)
	}

	claudeRules, err := routeStore.List(ctx, "claude")
	if err != nil || len(claudeRules) != 1 || claudeRules[0].Name != "haiku 改道" {
		t.Fatalf("claude list = %+v err=%v", claudeRules, err)
	}
	enabledCodex, err := routeStore.ListEnabled(ctx, "codex")
	if err != nil || len(enabledCodex) != 0 {
		t.Fatalf("disabled codex rule must not be listed as enabled: %+v err=%v", enabledCodex, err)
	}

	claudeRule.MatchModel = "claude-haiku-4-5"
	claudeRule.Targets = []routing.Target{{ChannelID: 4, GroupID: 2}}
	if err := routeStore.Update(ctx, claudeRule); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := routeStore.SetEnabled(ctx, claudeRule.ID, false); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	got, err := routeStore.GetByID(ctx, claudeRule.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if got.Enabled || got.MatchModel != "claude-haiku-4-5" || got.Targets[0] != (routing.Target{ChannelID: 4, GroupID: 2}) {
		t.Fatalf("unexpected updated rule: %+v", got)
	}

	if err := routeStore.Delete(ctx, claudeRule.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, err := routeStore.GetByID(ctx, claudeRule.ID); err != nil || got != nil {
		t.Fatalf("deleted rule must be gone: %+v err=%v", got, err)
	}
	if err := routeStore.Delete(ctx, claudeRule.ID); err == nil {
		t.Fatal("deleting a missing rule must fail")
	}
}
