package utils

import (
	"os"
	"path/filepath"
	"runtime"
)

// AppName 应用目录名（各平台统一使用此常量）
const AppName = "Claude-Request-Forwarder"

// GetAppDataDir 获取应用数据目录（跨平台）
// Windows: %APPDATA%\Claude-Request-Forwarder
// macOS: ~/Library/Application Support/Claude-Request-Forwarder
// Linux: ~/.local/share/claude-request-forwarder
func GetAppDataDir() string {
	var baseDir string

	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			baseDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(baseDir, AppName)

	case "darwin":
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "Library", "Application Support", AppName)

	case "linux":
		homeDir, _ := os.UserHomeDir()
		xdgDataHome := os.Getenv("XDG_DATA_HOME")
		if xdgDataHome != "" {
			return filepath.Join(xdgDataHome, "claude-request-forwarder")
		}
		return filepath.Join(homeDir, ".local", "share", "claude-request-forwarder")

	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".claude-request-forwarder")
	}
}

// GetDataDir 获取数据库目录
func GetDataDir() string {
	return filepath.Join(GetAppDataDir(), "data")
}

// GetLogDir 获取日志目录
func GetLogDir() string {
	return filepath.Join(GetAppDataDir(), "logs")
}

// EnsureAppDirs 确保应用所需的所有目录存在
func EnsureAppDirs() error {
	dirs := []string{
		GetAppDataDir(),
		GetDataDir(),
		GetLogDir(),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}
