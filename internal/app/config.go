package app

import (
	"log/slog"
	"path/filepath"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/appdir"
)

func (a *App) loadConfig() {
	tempLogger := slog.Default()

	// 确保应用目录存在
	if err := appdir.EnsureAppDirs(); err != nil {
		tempLogger.Warn("⚠️ 无法创建应用目录", "error", err)
	} else {
		tempLogger.Info("📁 应用目录已就绪",
			"appdir", appdir.GetAppDataDir(),
			"data", appdir.GetDataDir(),
			"logs", appdir.GetLogDir())
	}

	// 配置基线来自代码，端点、渠道和可调设置随后从 SQLite 加载。
	cfg := config.NewDefaultConfig()
	cfg.Logging.FilePath = filepath.Join(appdir.GetLogDir(), "app.log")
	cfg.UsageTracking.DatabasePath = filepath.Join(appdir.GetDataDir(), "usage.db")

	a.config = cfg

	tempLogger.Info("✅ 代码默认配置已加载",
		"log_path", a.config.Logging.FilePath,
		"db_path", a.config.UsageTracking.DatabasePath,
		"appdir", appdir.GetAppDataDir())
}
