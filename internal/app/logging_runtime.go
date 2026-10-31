package app

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/logging"
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
		WHERE provider = 'global' AND category = 'logging'
		  AND key IN ('level', 'format', 'file_enabled', 'max_file_size', 'max_files', 'compress_rotated')
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
		case "file_enabled", "compress_rotated":
			if value == "true" || value == "false" {
				if key == "file_enabled" {
					a.config.Logging.FileEnabled = value == "true"
				} else {
					a.config.Logging.CompressRotated = value == "true"
				}
			}
		case "max_file_size":
			if size, err := logging.ParseSize(value); err == nil && size > 0 {
				a.config.Logging.MaxFileSize = value
			}
		case "max_files":
			if count, err := strconv.Atoi(value); err == nil && count > 0 {
				a.config.Logging.MaxFiles = count
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
	previousHandlers := currentLogHandlers
	logger, broadcastHandler := buildLogger(a.config.Logging)
	if a.logEmitter != nil {
		broadcastHandler.SetEventEmitter(a.logEmitter)
	}
	if a.logger != nil {
		if handler, ok := a.logger.Handler().(*logging.ReloadableHandler); ok {
			handler.Update(broadcastHandler)
			logger = a.logger
		}
	}
	// Every existing logger now targets the new output. In-flight log writes
	// have finished before the old files are closed.
	for _, handler := range previousHandlers {
		if handler != nil {
			_ = handler.Close()
		}
	}
	slog.SetDefault(logger)
	a.logger = logger
	a.logHandler = broadcastHandler
	a.logEmitter = broadcastHandler.Emitter

	debuglog.SetDebugConfig(a.config)
	if a.loggingMiddleware != nil {
		a.loggingMiddleware.UpdateLogger(logger.With("provider", "claude"))
	}
	if a.codexHTTPHandler != nil {
		a.codexHTTPHandler.UpdateLogger(logger)
	}
	a.logger.Info("✅ 日志设置已应用",
		"level", a.config.Logging.Level,
		"file_enabled", a.config.Logging.FileEnabled)
}
