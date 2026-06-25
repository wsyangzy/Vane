package main

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-relay-forwarder/config"
)

func closeCurrentLogHandlersForTest() {
	for _, h := range currentLogHandlers {
		if h != nil {
			_ = h.Close()
		}
	}
	currentLogHandlers = nil
}

func TestSetupLoggerHonorsDebugLevelInReleaseBuild(t *testing.T) {
	originalBuildMode := BuildMode
	BuildMode = "release"
	t.Cleanup(func() {
		BuildMode = originalBuildMode
		closeCurrentLogHandlersForTest()
	})

	logger, handler := setupLogger(config.LoggingConfig{
		Level:       "debug",
		FileEnabled: false,
	})

	logger.Debug("debug level should be visible in release")

	logs := handler.GetRecentLogs(10)
	for _, entry := range logs {
		if entry.Level == slog.LevelDebug.String() && entry.Message == "debug level should be visible in release" {
			return
		}
	}

	t.Fatalf("expected debug log in release mode, got %+v", logs)
}

func TestSetupLoggerUsesDatedFilesAndDedicatedDebugFile(t *testing.T) {
	t.Cleanup(closeCurrentLogHandlersForTest)

	logDir := t.TempDir()
	logger, _ := setupLogger(config.LoggingConfig{
		Level:       "debug",
		FileEnabled: true,
		FilePath:    filepath.Join(logDir, "app.log"),
		MaxFileSize: "10MB",
		MaxFiles:    5,
	})

	logger.Info("info log for dated file")
	logger.Debug("debug log for dedicated file", "privacy_redacted_body", `{"token":"fake_token_0001"}`)
	closeCurrentLogHandlersForTest()

	date := time.Now().Format("2006-01-02")
	appPath := filepath.Join(logDir, "app-"+date+".log")
	debugPath := filepath.Join(logDir, "debug-"+date+".log")

	appBytes, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read dated app log: %v", err)
	}
	if !strings.Contains(string(appBytes), "info log for dated file") {
		t.Fatalf("dated app log missing info entry: %s", appBytes)
	}

	debugBytes, err := os.ReadFile(debugPath)
	if err != nil {
		t.Fatalf("read dated debug log: %v", err)
	}
	debugText := string(debugBytes)
	for _, expected := range []string{"debug log for dedicated file", "privacy_redacted_body", "fake_token_0001"} {
		if !strings.Contains(debugText, expected) {
			t.Fatalf("debug log missing %q: %s", expected, debugText)
		}
	}
}

func TestSetupLoggerInfoLevelDoesNotCreateDebugFileOrExposeDebugLogs(t *testing.T) {
	t.Cleanup(closeCurrentLogHandlersForTest)

	logDir := t.TempDir()
	logger, handler := setupLogger(config.LoggingConfig{
		Level:       "info",
		FileEnabled: true,
		FilePath:    filepath.Join(logDir, "app.log"),
		MaxFileSize: "10MB",
		MaxFiles:    5,
	})

	logger.Debug("debug log should be filtered")
	logger.Info("info log should be visible")
	closeCurrentLogHandlersForTest()

	date := time.Now().Format("2006-01-02")
	appPath := filepath.Join(logDir, "app-"+date+".log")
	debugPath := filepath.Join(logDir, "debug-"+date+".log")

	if _, err := os.Stat(debugPath); !os.IsNotExist(err) {
		t.Fatalf("debug log file should not be created for info level, stat err=%v", err)
	}

	appBytes, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read dated app log: %v", err)
	}
	appText := string(appBytes)
	if strings.Contains(appText, "debug log should be filtered") {
		t.Fatalf("app log should not contain debug entry: %s", appText)
	}
	if !strings.Contains(appText, "info log should be visible") {
		t.Fatalf("app log missing info entry: %s", appText)
	}

	for _, entry := range handler.GetRecentLogs(10) {
		if entry.Level == slog.LevelDebug.String() || entry.Message == "debug log should be filtered" {
			t.Fatalf("recent logs should not expose debug entries at info level: %+v", handler.GetRecentLogs(10))
		}
	}
}

func TestApplyPersistedLoggingConfigBeforeLogger(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE settings (
			category TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			UNIQUE(category, key)
		);
		INSERT INTO settings (category, key, value) VALUES ('logging', 'level', 'info');
		INSERT INTO settings (category, key, value) VALUES ('logging', 'format', 'json');
	`)
	if closeErr := db.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("seed settings db: %v", err)
	}

	app := &App{
		config: &config.Config{
			Logging: config.LoggingConfig{
				Level:  "debug",
				Format: "text",
			},
			UsageTracking: config.UsageTrackingConfig{
				DatabasePath: dbPath,
			},
		},
	}

	app.applyPersistedLoggingConfigBeforeLogger()

	if app.config.Logging.Level != "info" {
		t.Fatalf("expected persisted logging level info, got %q", app.config.Logging.Level)
	}
	if app.config.Logging.Format != "json" {
		t.Fatalf("expected persisted logging format json, got %q", app.config.Logging.Format)
	}
}
