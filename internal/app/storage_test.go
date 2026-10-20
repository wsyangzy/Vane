package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/appdir"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func newStorageTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"APPDATA", "USERPROFILE", "HOME", "XDG_DATA_HOME"} {
		t.Setenv(key, root)
	}
	if err := appdir.EnsureAppDirs(); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.config = config.NewDefaultConfig()
	a.config.UsageTracking.DatabasePath = filepath.Join(appdir.GetDataDir(), "usage.db")
	a.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Cleanup(func() {
		if a.usageTracker != nil {
			if err := a.usageTracker.Close(); err != nil {
				t.Error(err)
			}
		}
		if a.storeDB != nil {
			if err := a.storeDB.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	return a
}

func seedHistoricalStorage(t *testing.T, path string) {
	t.Helper()
	adapter, err := tracking.NewSQLiteAdapter(tracking.DatabaseConfig{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	if err := adapter.InitSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.GetWriteDB().Exec(`INSERT INTO channels (name) VALUES ('historical-channel')`); err != nil {
		t.Fatal(err)
	}
}

func TestVaneStorageStartsIndependentlyOfHistoricalDatabases(t *testing.T) {
	for _, trackingEnabled := range []bool{false, true} {
		name := "without-tracking"
		if trackingEnabled {
			name = "with-tracking"
		}
		t.Run(name, func(t *testing.T) {
			a := newStorageTestApp(t)
			seedHistoricalStorage(t, filepath.Join(appdir.GetDataDir(), "usage-dev.db"))
			seedHistoricalStorage(t, filepath.Join(appdir.GetDataDir(), "usage-v2.db"))
			a.config.UsageTracking.Enabled = trackingEnabled
			a.setupUsageTracker()
			a.setupStoreDB()
			if a.storeDB == nil {
				t.Fatal("Vane management database was not initialized")
			}
			if trackingEnabled && a.usageTracker == nil {
				t.Fatal("Vane usage tracker was not initialized")
			}
			var channels int
			if err := a.storeDB.QueryRow("SELECT COUNT(*) FROM channels").Scan(&channels); err != nil {
				t.Fatal(err)
			}
			if channels != 0 {
				t.Fatalf("new Vane database imported %d historical channels", channels)
			}
		})
	}
}

func TestVaneStoragePreservesCorruptDatabaseWithoutImportingHistory(t *testing.T) {
	a := newStorageTestApp(t)
	seedHistoricalStorage(t, filepath.Join(appdir.GetDataDir(), "usage-dev.db"))
	path := a.getEffectiveUsageDBPath()
	contents := "invalid database retained for explicit recovery"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	a.setupUsageTracker()
	a.setupStoreDB()
	if a.usageTracker != nil || a.storeDB != nil {
		t.Fatal("corrupt database must not be treated as a ready store")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != contents {
		t.Fatalf("corrupt database was overwritten: %v", err)
	}
}
