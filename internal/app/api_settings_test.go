package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/config"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestSettingsUseBeijingTimezoneWithLegacyDatabase(t *testing.T) {
	adapter, err := tracking.NewSQLiteAdapter(tracking.DatabaseConfig{DatabasePath: filepath.Join(t.TempDir(), "settings.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.InitSchema(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := settingsstore.NewSQLiteSettingsStore(adapter.GetWriteDB())
	if err := store.Set(ctx, settingsservice.CategoryTimezone, "global", "America/New_York"); err != nil {
		t.Fatal(err)
	}
	svc := settingsservice.NewSettingsService(store)
	if err := svc.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	a := &App{config: config.NewDefaultConfig(), settingsService: svc, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if a.config.Timezone != config.BeijingTimezone {
		t.Fatalf("default timezone = %q", a.config.Timezone)
	}
	a.config.Timezone = "UTC"
	if err := a.applySettingsToConfig(false); err != nil {
		t.Fatal(err)
	}
	if a.config.Timezone != config.BeijingTimezone {
		t.Fatalf("legacy setting changed runtime timezone to %q", a.config.Timezone)
	}
	for _, category := range a.GetSettingCategories() {
		if category.Name == settingsservice.CategoryTimezone {
			t.Fatal("timezone should not be a configurable category")
		}
	}
	settings, err := a.GetAllSettings()
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range settings {
		if setting.Category == settingsservice.CategoryTimezone {
			t.Fatal("legacy timezone should not be exposed to the settings page")
		}
	}
	if err := a.UpdateSetting(UpdateSettingInput{Category: settingsservice.CategoryTimezone, Key: "global", Value: "UTC"}); err == nil {
		t.Fatal("timezone updates should be rejected")
	}
	legacy, err := store.Get(ctx, settingsservice.CategoryTimezone, "global")
	if err != nil || legacy == nil || legacy.Value != "America/New_York" {
		t.Fatalf("legacy setting should be preserved without affecting runtime: %+v, %v", legacy, err)
	}
}

func TestPreferredPortSettingIsScopedWithoutLeakingToUI(t *testing.T) {
	base := &settingsstore.SettingRecord{
		Provider: "global", Category: settingsservice.CategoryServer, Key: "preferred_port",
		Value: "8087", ValueType: "int", Label: "首选端口",
	}
	dev := &settingsstore.SettingRecord{
		Provider: "global", Category: settingsservice.CategoryServer, Key: "preferred_port_dev",
		Value: "9000",
	}

	setBuildModeForTest(t, "dev")
	visible := visibleSettingRecords([]*settingsstore.SettingRecord{base, dev})
	if len(visible) != 1 || visible[0].Key != "preferred_port" || visible[0].Value != "9000" {
		t.Fatalf("expected dev override under stable UI key, got %#v", visible)
	}
	if visible[0].Label != base.Label || visible[0].ValueType != base.ValueType {
		t.Fatalf("expected dev override to inherit base metadata, got %#v", visible[0])
	}

	buildinfo.BuildMode = "release"
	visible = visibleSettingRecords([]*settingsstore.SettingRecord{base, dev})
	if len(visible) != 1 || visible[0].Key != "preferred_port" || visible[0].Value != "8087" {
		t.Fatalf("expected release to hide dev override, got %#v", visible)
	}
}

func TestScopedSettingKeyUsesDevPortKeyOnlyInDev(t *testing.T) {
	setBuildModeForTest(t, "dev")
	if got := scopedSettingKey(settingsservice.CategoryServer, "preferred_port"); got != "preferred_port_dev" {
		t.Fatalf("dev preferred port key = %q", got)
	}
	buildinfo.BuildMode = "release"
	if got := scopedSettingKey(settingsservice.CategoryServer, "preferred_port"); got != "preferred_port" {
		t.Fatalf("release preferred port key = %q", got)
	}
}
