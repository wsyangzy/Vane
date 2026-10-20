// Package appdir owns Vane's application data paths.
package appdir

import (
	"os"
	"path/filepath"
	"runtime"
)

const Name = "Vane"

// Dir resolves Vane's directory without creating or migrating user files.
func Dir() string {
	home, _ := os.UserHomeDir()
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support")
	case "linux":
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
	default:
		return filepath.Join(home, ".vane")
	}
	return filepath.Join(base, Name)
}

func GetAppDataDir() string { return Dir() }

func GetDataDir() string { return filepath.Join(Dir(), "data") }

func GetLogDir() string { return filepath.Join(Dir(), "logs") }

func EnsureAppDirs() error {
	for _, dir := range []string{Dir(), GetDataDir(), GetLogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
