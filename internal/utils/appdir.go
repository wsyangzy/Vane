package utils

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

const (
	AppName       = "AI-Relay-Forwarder"
	LegacyAppName = "Claude-Request-Forwarder"
)

// MigrateLegacyAppDir 首次启动时将旧数据目录重命名为新目录。
// 返回 (migrated bool, err error)。迁移失败时调用方应记录告警并退回旧目录。
func MigrateLegacyAppDir() (bool, error) {
	newDir := computeAppDir(AppName)
	legacyDir := computeAppDir(LegacyAppName)
	if newDir == "" || legacyDir == "" {
		return false, nil
	}
	if dirExists(newDir) {
		return false, nil
	}
	if !dirExists(legacyDir) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0755); err != nil {
		return false, err
	}
	if err := os.Rename(legacyDir, newDir); err != nil {
		return false, err
	}
	return true, nil
}

// GetAppDataDir 获取应用数据目录（跨平台），迁移失败时 fallback 到旧目录
func GetAppDataDir() string {
	newDir := computeAppDir(AppName)
	if dirExists(newDir) {
		return newDir
	}
	legacyDir := computeAppDir(LegacyAppName)
	if dirExists(legacyDir) {
		return legacyDir
	}
	return newDir
}

// GetDataDir 获取数据库目录
func GetDataDir() string { return filepath.Join(GetAppDataDir(), "data") }

// GetLogDir 获取日志目录
func GetLogDir() string { return filepath.Join(GetAppDataDir(), "logs") }

// EnsureAppDirs 确保应用所需的所有目录存在
func EnsureAppDirs() error {
	for _, dir := range []string{GetAppDataDir(), GetDataDir(), GetLogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// LogMigrationResult 记录迁移结果日志
func LogMigrationResult(migrated bool, err error) {
	if err != nil {
		slog.Warn("legacy app dir migration failed, falling back", "err", err)
	} else if migrated {
		slog.Info("migrated legacy app dir", "from", LegacyAppName, "to", AppName)
	}
}

func computeAppDir(name string) string {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(base, name)
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", name)
	default:
		home, _ := os.UserHomeDir()
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, name)
		}
		return filepath.Join(home, ".local", "share", name)
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
