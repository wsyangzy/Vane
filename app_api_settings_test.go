package main

import (
	"testing"

	"ai-relay-forwarder/internal/service"
	"ai-relay-forwarder/internal/store"
)

func TestPreferredPortSettingIsScopedWithoutLeakingToUI(t *testing.T) {
	base := &store.SettingRecord{
		Provider: "global", Category: service.CategoryServer, Key: "preferred_port",
		Value: "8087", ValueType: "int", Label: "首选端口",
	}
	dev := &store.SettingRecord{
		Provider: "global", Category: service.CategoryServer, Key: "preferred_port_dev",
		Value: "9000",
	}

	setBuildModeForTest(t, "dev")
	visible := visibleSettingRecords([]*store.SettingRecord{base, dev})
	if len(visible) != 1 || visible[0].Key != "preferred_port" || visible[0].Value != "9000" {
		t.Fatalf("expected dev override under stable UI key, got %#v", visible)
	}
	if visible[0].Label != base.Label || visible[0].ValueType != base.ValueType {
		t.Fatalf("expected dev override to inherit base metadata, got %#v", visible[0])
	}

	BuildMode = "release"
	visible = visibleSettingRecords([]*store.SettingRecord{base, dev})
	if len(visible) != 1 || visible[0].Key != "preferred_port" || visible[0].Value != "8087" {
		t.Fatalf("expected release to hide dev override, got %#v", visible)
	}
}

func TestScopedSettingKeyUsesDevPortKeyOnlyInDev(t *testing.T) {
	setBuildModeForTest(t, "dev")
	if got := scopedSettingKey(service.CategoryServer, "preferred_port"); got != "preferred_port_dev" {
		t.Fatalf("dev preferred port key = %q", got)
	}
	BuildMode = "release"
	if got := scopedSettingKey(service.CategoryServer, "preferred_port"); got != "preferred_port" {
		t.Fatalf("release preferred port key = %q", got)
	}
}
