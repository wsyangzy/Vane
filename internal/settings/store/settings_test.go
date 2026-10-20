package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSettingsStoreScopesSameKeyByProvider(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			category TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			value_type TEXT NOT NULL DEFAULT 'string',
			label TEXT,
			description TEXT,
			display_order INTEGER NOT NULL DEFAULT 0,
			requires_restart INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider, category, key)
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	store := NewSQLiteSettingsStore(db)
	if err := store.SetScoped(ctx, "claude", "failover", "enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetScoped(ctx, "codex", "failover", "enabled", "false"); err != nil {
		t.Fatal(err)
	}

	claude, err := store.GetScoped(ctx, "claude", "failover", "enabled")
	if err != nil {
		t.Fatal(err)
	}
	codex, err := store.GetScoped(ctx, "codex", "failover", "enabled")
	if err != nil {
		t.Fatal(err)
	}
	if claude == nil || claude.Provider != "claude" || claude.Value != "true" {
		t.Fatalf("Claude setting = %#v", claude)
	}
	if codex == nil || codex.Provider != "codex" || codex.Value != "false" {
		t.Fatalf("Codex setting = %#v", codex)
	}
}
