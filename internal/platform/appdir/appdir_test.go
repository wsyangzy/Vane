package appdir

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	for _, key := range []string{"APPDATA", "USERPROFILE", "HOME", "XDG_DATA_HOME"} {
		t.Setenv(key, tmp)
	}
	return tmp
}

func TestVaneDirectoryIgnoresOtherApplicationData(t *testing.T) {
	tmp := isolate(t)
	other := filepath.Join(tmp, "OtherApplication", "data")
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(other, "usage.db")
	if err := os.WriteFile(path, []byte("existing data"), 0600); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, Name)
	if runtime.GOOS == "darwin" {
		want = filepath.Join(tmp, "Library", "Application Support", Name)
	}
	if got := Dir(); got != want {
		t.Fatalf("directory = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("resolving created a directory: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "existing data" {
		t.Fatalf("unrelated data changed: %q, %v", data, err)
	}
}

func TestEnsureAppDirsPreservesExistingData(t *testing.T) {
	isolate(t)
	if err := EnsureAppDirs(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(GetDataDir(), "usage.db")
	if err := os.WriteFile(path, []byte("existing Vane data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAppDirs(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "existing Vane data" {
		t.Fatalf("existing data changed: %q, %v", data, err)
	}
	for _, dir := range []string{GetAppDataDir(), GetDataDir(), GetLogDir()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatalf("not a directory: %q", dir)
		}
	}
}

func TestPlatformEnvironmentFallback(t *testing.T) {
	tmp := isolate(t)
	var want string
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", "")
		want = filepath.Join(tmp, "AppData", "Roaming", Name)
	case "linux":
		t.Setenv("XDG_DATA_HOME", "")
		want = filepath.Join(tmp, ".local", "share", Name)
	default:
		t.Skip("no optional environment override")
	}
	if got := Dir(); got != want {
		t.Fatalf("directory = %q, want %q", got, want)
	}
}
