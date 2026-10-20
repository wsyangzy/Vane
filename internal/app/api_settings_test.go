package app

import (
	"testing"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

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
