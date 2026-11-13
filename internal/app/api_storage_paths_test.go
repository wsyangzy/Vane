package app

import (
	"context"
	"path/filepath"
	"testing"

	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
)

func TestArchivePathSettingCopiesAndPersistsAcrossTrackerRestart(t *testing.T) {
	a := newStorageTestApp(t)
	a.setupUsageTracker()
	a.setupStoreDB()
	a.setupSettingsStore()
	root := filepath.Join(t.TempDir(), "历史数据")
	ctx := context.Background()
	if err := a.settingsService.Set(ctx, settingsservice.CategoryRetention, "archive_directory", root); err != nil {
		t.Fatal(err)
	}
	paths := a.GetDataStoragePaths()
	if paths.ArchiveDirectory != filepath.Join(root, historyEnvironment()) || paths.DatabasePath != a.getEffectiveUsageDBPath() {
		t.Fatalf("wrong paths: %+v", paths)
	}
	if saved, err := savedArchiveRoot(ctx, paths.DatabasePath); err != nil || saved != root {
		t.Fatalf("bootstrap lookup: %q %v", saved, err)
	}
	if err := a.usageTracker.Close(); err != nil {
		t.Fatal(err)
	}
	a.usageTracker = nil
	a.setupUsageTracker()
	if a.usageTracker == nil || a.usageTracker.ArchiveDirectory() != paths.ArchiveDirectory {
		t.Fatal("restart ignored saved archive directory")
	}
	if err := a.settingsService.Set(ctx, settingsservice.CategoryRetention, "archive_directory", "relative/path"); err == nil {
		t.Fatal("relative directory accepted")
	}
}
