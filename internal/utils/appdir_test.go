package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyAppDir_NewInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	migrated, err := MigrateLegacyAppDir()
	if err != nil || migrated {
		t.Fatalf("expected no migration on new install, got migrated=%v err=%v", migrated, err)
	}
}

func TestMigrateLegacyAppDir_SuccessfulMigration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	legacyDir := filepath.Join(tmp, LegacyAppName)
	os.MkdirAll(filepath.Join(legacyDir, "data"), 0755)
	os.WriteFile(filepath.Join(legacyDir, "data", "test.db"), []byte("data"), 0644)

	migrated, err := MigrateLegacyAppDir()
	if err != nil || !migrated {
		t.Fatalf("expected successful migration, got migrated=%v err=%v", migrated, err)
	}
	newDir := filepath.Join(tmp, AppName)
	if _, err := os.Stat(filepath.Join(newDir, "data", "test.db")); err != nil {
		t.Fatal("migrated data file not found in new dir")
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatal("legacy dir should not exist after migration")
	}
}

func TestMigrateLegacyAppDir_AlreadyMigrated(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	os.MkdirAll(filepath.Join(tmp, AppName), 0755)
	os.MkdirAll(filepath.Join(tmp, LegacyAppName), 0755)

	migrated, err := MigrateLegacyAppDir()
	if err != nil || migrated {
		t.Fatalf("should skip when new dir exists, got migrated=%v err=%v", migrated, err)
	}
}

func TestGetAppDataDir_Fallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	legacyDir := filepath.Join(tmp, LegacyAppName)
	os.MkdirAll(legacyDir, 0755)

	got := GetAppDataDir()
	if got != legacyDir {
		t.Fatalf("expected fallback to legacy dir %s, got %s", legacyDir, got)
	}
}

func TestGetAppDataDir_PrefersNewDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	newDir := filepath.Join(tmp, AppName)
	os.MkdirAll(newDir, 0755)
	os.MkdirAll(filepath.Join(tmp, LegacyAppName), 0755)

	got := GetAppDataDir()
	if got != newDir {
		t.Fatalf("expected new dir %s, got %s", newDir, got)
	}
}
