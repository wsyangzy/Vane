package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

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

func newDispatchSettingsTestApp(t *testing.T) (*App, settingsstore.SettingsStore) {
	t.Helper()
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
	store := settingsstore.NewSQLiteSettingsStore(adapter.GetWriteDB())
	svc := settingsservice.NewSettingsService(store)
	a := &App{config: config.NewDefaultConfig(), settingsService: svc, settingsStore: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.config.Logging.FilePath = filepath.Join(t.TempDir(), "app.log")
	previousLogger := slog.Default()
	t.Cleanup(func() {
		if a.logEmitter != nil {
			a.logEmitter.Stop()
		}
		closeCurrentLogHandlersForTest()
		slog.SetDefault(previousLogger)
	})
	svc.SetOnChangeCallback(func() error { return a.applySettingsToConfig(false) })
	return a, store
}

func TestSettingsAdaptLegacyRowsWithoutReinterpretingRetryValues(t *testing.T) {
	a, store := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	legacy := []struct{ category, key, value string }{
		{"strategy", "type", "fastest"},
		{"retry", "max_attempts", "7"},
		{"retry", "base_delay", "5s"},
		{"retry", "max_delay", "40s"},
		{"retry", "multiplier", "3"},
		{"request", "eof_retry_hint", "true"},
		{"request", "suspend_wait_timeout", "90s"},
	}
	for _, old := range legacy {
		if err := store.Set(ctx, old.category, old.key, old.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.applySettingsToConfig(false); err != nil {
		t.Fatal(err)
	}
	if a.config.Retry.MaxAttempts != 7 || a.config.Retry.MaxTotalAttempts != 10 || a.config.Retry.MaxTotalDuration != time.Minute {
		t.Fatalf("legacy per-endpoint values must not become dispatch budgets: %+v", a.config.Retry)
	}
	for _, category := range a.GetSettingCategories() {
		if category.Name == "strategy" || category.Name == "failover" {
			t.Fatalf("legacy category exposed: %+v", category)
		}
		if category.Name == "retry" && category.Label != "调度保护" {
			t.Fatalf("retry category was not adapted: %+v", category)
		}
	}
	records, err := a.GetAllSettings()
	if err != nil {
		t.Fatal(err)
	}
	budgets := 0
	for _, record := range records {
		for _, old := range legacy {
			if record.Category == old.category && record.Key == old.key {
				t.Fatalf("legacy control still visible: %s.%s", old.category, old.key)
			}
		}
		if record.Category == "failover" {
			t.Fatalf("legacy provider controls still visible: %+v", record)
		}
		if record.Category == "retry" {
			budgets++
		}
	}
	if budgets != 2 {
		t.Fatalf("expected both dispatch budget controls, got %d", budgets)
	}
	for _, old := range legacy {
		record, err := store.Get(ctx, old.category, old.key)
		if err != nil || record == nil || record.Value != old.value {
			t.Fatalf("compatibility value changed: %s.%s = %+v, %v", old.category, old.key, record, err)
		}
	}
}

func TestDispatchBudgetSettingsApplyAtomicallyAndReset(t *testing.T) {
	a, store := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	update := BatchUpdateSettingsInput{Settings: []UpdateSettingInput{
		{Provider: "global", Category: "retry", Key: "max_total_attempts", Value: "4"},
		{Provider: "global", Category: "retry", Key: "max_total_duration", Value: "15s"},
	}}
	if err := a.BatchUpdateSettings(update); err != nil {
		t.Fatal(err)
	}
	if a.config.Retry.MaxTotalAttempts != 4 || a.config.Retry.MaxTotalDuration != 15*time.Second {
		t.Fatalf("saved dispatch budgets did not reach runtime: %+v", a.config.Retry)
	}
	for _, input := range []UpdateSettingInput{
		{Provider: "global", Category: "retry", Key: "max_total_attempts", Value: "0"},
		{Provider: "global", Category: "retry", Key: "max_total_attempts", Value: "101"},
		{Provider: "global", Category: "retry", Key: "max_total_attempts", Value: "1.5"},
		{Provider: "global", Category: "retry", Key: "max_total_duration", Value: "500ms"},
		{Provider: "global", Category: "retry", Key: "max_total_duration", Value: "11m"},
		{Provider: "global", Category: "retry", Key: "max_total_duration", Value: "invalid"},
		{Provider: "codex", Category: "retry", Key: "max_total_attempts", Value: "2"},
	} {
		if err := a.UpdateSetting(input); err == nil {
			t.Fatalf("invalid dispatch setting accepted: %+v", input)
		}
	}
	update.Settings[0].Value = "6"
	update.Settings[1].Value = "0s"
	if err := a.BatchUpdateSettings(update); err == nil {
		t.Fatal("invalid batch should fail before writing any settings")
	}
	persisted, err := store.Get(ctx, "retry", "max_total_attempts")
	if err != nil || persisted == nil || persisted.Value != "4" || a.config.Retry.MaxTotalAttempts != 4 {
		t.Fatalf("failed batch changed existing budget: %+v, %v", persisted, err)
	}
	if err := a.ResetCategorySettings("retry"); err != nil {
		t.Fatal(err)
	}
	if a.config.Retry.MaxTotalAttempts != 10 || a.config.Retry.MaxTotalDuration != time.Minute {
		t.Fatalf("reset did not restore dispatch budget defaults: %+v", a.config.Retry)
	}
}
