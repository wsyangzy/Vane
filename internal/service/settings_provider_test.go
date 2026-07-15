package service

import (
	"context"
	"database/sql"
	"testing"

	"ai-relay-forwarder/internal/store"
	_ "modernc.org/sqlite"
)

func newSettingsServiceTestStore(t *testing.T) (*SettingsService, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider TEXT NOT NULL,
		category TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		value_type TEXT DEFAULT 'string',
		label TEXT, description TEXT, display_order INTEGER DEFAULT 0,
		requires_restart INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(provider, category, key)
	)`)
	if err != nil {
		t.Fatal(err)
	}
	return NewSettingsService(store.NewSQLiteSettingsStore(db)), db
}

func TestSettingsServiceScopesFailoverByProvider(t *testing.T) {
	svc, _ := newSettingsServiceTestStore(t)

	ctx := context.Background()
	if err := svc.SetScoped(ctx, ProviderClaude, CategoryFailover, "enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetScoped(ctx, ProviderCodex, CategoryFailover, "enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if !svc.GetBoolScoped(ctx, ProviderClaude, CategoryFailover, "enabled", false) {
		t.Fatal("Claude failover should be enabled")
	}
	if svc.GetBoolScoped(ctx, ProviderCodex, CategoryFailover, "enabled", true) {
		t.Fatal("Codex failover should be disabled")
	}
	if err := svc.SetScoped(ctx, SettingsProvider("unknown"), CategoryFailover, "enabled", "true"); err == nil {
		t.Fatal("invalid provider should be rejected")
	}
}

func TestRetentionSettingsExposeOnlyRetentionDays(t *testing.T) {
	svc, db := newSettingsServiceTestStore(t)
	ctx := context.Background()
	if _, err := db.Exec(`
		INSERT INTO settings (provider, category, key, value)
		VALUES ('global', 'retention', 'cleanup_interval', '24h'),
		       ('global', 'retention', 'max_active_database_size_mb', '50')
	`); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitDefaults(ctx); err != nil {
		t.Fatalf("initialize defaults: %v", err)
	}

	settings, err := svc.GetByCategoryScoped(ctx, ProviderGlobal, CategoryRetention)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != 1 || settings[0].Key != "retention_days" {
		t.Fatalf("unexpected retention settings: %+v", settings)
	}

	if err := svc.UpdateAndApply(ctx, []*store.SettingRecord{{
		Provider: string(ProviderGlobal), Category: CategoryRetention, Key: "retention_days", Value: "30",
	}}); err != nil {
		t.Fatalf("retention days update rejected: %v", err)
	}
}

func TestSettingsServiceDefaultsKeepCodexFailoverDisabled(t *testing.T) {
	svc := NewSettingsService(nil)
	defaults := svc.GetAllDefaults()
	values := map[string]string{}
	for _, record := range defaults {
		if record.Category == CategoryFailover {
			values[record.Provider+"/"+record.Key] = record.Value
		}
	}
	if values[string(ProviderClaude)+"/enabled"] != "true" {
		t.Fatalf("Claude enabled default = %q", values[string(ProviderClaude)+"/enabled"])
	}
	if values[string(ProviderCodex)+"/enabled"] != "false" {
		t.Fatalf("Codex enabled default = %q", values[string(ProviderCodex)+"/enabled"])
	}
}
