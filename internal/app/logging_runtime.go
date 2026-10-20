package app

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking/debuglog"
)

func (a *App) setupLogger() {
	logger, broadcastHandler := setupLogger(a.config.Logging)
	a.logger = logger
	slog.SetDefault(logger)

	// 存储日志处理器和发射器引用
	a.logHandler = broadcastHandler
	a.logEmitter = broadcastHandler.Emitter

	// 初始化调试配置
	debuglog.SetDebugConfig(a.config)

	a.logger.Info("✅ 日志系统初始化完成",
		"level", a.config.Logging.Level,
		"file_enabled", a.config.Logging.FileEnabled)
}

func (a *App) applyPersistedLoggingConfigBeforeLogger() {
	if a.config == nil {
		return
	}
	dbPath := strings.TrimSpace(a.config.UsageTracking.DatabasePath)
	if a.config.UsageTracking.Database != nil && strings.TrimSpace(a.config.UsageTracking.Database.Path) != "" {
		dbPath = strings.TrimSpace(a.config.UsageTracking.Database.Path)
	}
	if dbPath == "" || dbPath == ":memory:" {
		return
	}
	if _, err := os.Stat(dbPath); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", sqliteReadOnlyStartupDSN(dbPath))
	if err != nil {
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	rows, err := db.QueryContext(ctx, `
		SELECT key, value
		FROM settings
		WHERE category = 'logging'
		  AND key IN ('level', 'format')
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return
		}
		value = strings.TrimSpace(value)
		switch key {
		case "level":
			if isValidLoggingLevel(value) {
				a.config.Logging.Level = strings.ToLower(value)
			}
		case "format":
			if isValidLoggingFormat(value) {
				a.config.Logging.Format = strings.ToLower(value)
			}
		}
	}
}

func sqliteReadOnlyStartupDSN(dbPath string) string {
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	return dbPath + separator + "mode=ro&_busy_timeout=2000"
}

func isValidLoggingLevel(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

func isValidLoggingFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text", "json":
		return true
	default:
		return false
	}
}

func (a *App) applyRuntimeLoggingConfig() {
	if a.config == nil {
		return
	}
	if a.logEmitter != nil {
		a.logEmitter.Stop()
	}

	logger, broadcastHandler := setupLogger(a.config.Logging)
	slog.SetDefault(logger)
	a.logger = logger
	a.logHandler = broadcastHandler
	a.logEmitter = broadcastHandler.Emitter

	debuglog.SetDebugConfig(a.config)
	if a.codexHTTPHandler != nil {
		a.codexHTTPHandler.UpdateLogger(logger)
	}
	a.logger.Info("✅ 日志设置已应用",
		"level", a.config.Logging.Level,
		"file_enabled", a.config.Logging.FileEnabled)
}
