package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"

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
	return NewSettingsService(settingsstore.NewSQLiteSettingsStore(db)), db
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

	if err := svc.UpdateAndApply(ctx, []*settingsstore.SettingRecord{{
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

func TestSettingsServiceSetScopedRestoresDatabaseWhenApplyFails(t *testing.T) {
	svc, _ := newSettingsServiceTestStore(t)
	if err := svc.SetScoped(context.Background(), ProviderCodex, CategoryFailover, "enabled", "false"); err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	var cancelApply context.CancelFunc
	svc.SetOnChangeCallback(func() error {
		cancelApply()
		return errors.New("forced apply failure")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancelApply = cancel
	err := svc.SetScoped(ctx, ProviderCodex, CategoryFailover, "enabled", "true")
	if err == nil || !strings.Contains(err.Error(), "forced apply failure") {
		t.Fatalf("expected apply failure, got %v", err)
	}
	if svc.GetBoolScoped(context.Background(), ProviderCodex, CategoryFailover, "enabled", true) {
		t.Fatal("failed hot apply must restore the previous persisted value")
	}

	ctx, cancel = context.WithCancel(context.Background())
	cancelApply = cancel
	err = svc.SetScoped(ctx, ProviderCodex, CategoryFailover, "temporary_test_key", "true")
	if err == nil {
		t.Fatal("expected apply failure for a newly inserted setting")
	}
	record, getErr := svc.GetScoped(context.Background(), ProviderCodex, CategoryFailover, "temporary_test_key")
	if getErr != nil {
		t.Fatalf("read rolled-back setting: %v", getErr)
	}
	if record != nil {
		t.Fatalf("failed hot apply must delete a previously absent setting, got %#v", record)
	}
}

func TestSettingsServiceGetBoolStrict(t *testing.T) {
	svc, db := newSettingsServiceTestStore(t)
	if _, err := db.Exec(`INSERT INTO settings (provider, category, key, value) VALUES
		('claude', 'failover', 'strict_true', 'true'),
		('claude', 'failover', 'strict_false', 'false'),
		('claude', 'failover', 'strict_empty', ''),
		('claude', 'failover', 'strict_invalid', 'not-a-bool'),
		('global', 'failover', 'strict_lookup', 'true'),
		('claude', 'failover', 'strict_lookup', 'false')`); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name       string
		key        string
		defaultVal bool
		want       bool
	}{
		{name: "missing uses default", key: "strict_missing", defaultVal: true, want: true},
		{name: "empty uses default", key: "strict_empty", defaultVal: true, want: true},
		{name: "true", key: "strict_true", want: true},
		{name: "false", key: "strict_false", defaultVal: true, want: false},
		{name: "failover uses Claude scope", key: "strict_lookup", defaultVal: true, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.GetBoolStrict(context.Background(), CategoryFailover, tt.key, tt.defaultVal)
			if err != nil {
				t.Fatalf("GetBoolStrict() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("GetBoolStrict() = %v, want %v", got, tt.want)
			}
		})
	}

	if _, err := svc.GetBoolStrict(context.Background(), CategoryFailover, "strict_invalid", false); err == nil {
		t.Fatal("invalid bool must return an error")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.GetBoolStrict(canceled, CategoryFailover, "strict_true", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v, want context.Canceled", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetBoolStrict(context.Background(), CategoryFailover, "strict_true", false); err == nil {
		t.Fatal("store read failure must return an error")
	}
}

func TestSettingsServiceGetBoolScopedStrict(t *testing.T) {
	svc, db := newSettingsServiceTestStore(t)
	if _, err := db.Exec(`INSERT INTO settings (provider, category, key, value) VALUES
		('codex', 'failover', 'strict_true', 'true'),
		('codex', 'failover', 'strict_false', 'false'),
		('codex', 'failover', 'strict_empty', ''),
		('codex', 'failover', 'strict_invalid', 'not-a-bool')`); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name       string
		key        string
		defaultVal bool
		want       bool
	}{
		{name: "missing uses default", key: "strict_missing", defaultVal: true, want: true},
		{name: "empty uses default", key: "strict_empty", defaultVal: true, want: true},
		{name: "true", key: "strict_true", want: true},
		{name: "false", key: "strict_false", defaultVal: true, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.GetBoolScopedStrict(context.Background(), ProviderCodex, CategoryFailover, tt.key, tt.defaultVal)
			if err != nil {
				t.Fatalf("GetBoolScopedStrict() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("GetBoolScopedStrict() = %v, want %v", got, tt.want)
			}
		})
	}

	if _, err := svc.GetBoolScopedStrict(context.Background(), ProviderCodex, CategoryFailover, "strict_invalid", false); err == nil {
		t.Fatal("invalid bool must return an error")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.GetBoolScopedStrict(canceled, ProviderCodex, CategoryFailover, "strict_true", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v, want context.Canceled", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetBoolScopedStrict(context.Background(), ProviderCodex, CategoryFailover, "strict_true", false); err == nil {
		t.Fatal("store read failure must return an error")
	}
}
