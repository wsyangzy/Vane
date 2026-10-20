package app

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/config"
)

func closeCurrentLogHandlersForTest() {
	for _, h := range currentLogHandlers {
		if h != nil {
			_ = h.Close()
		}
	}
	currentLogHandlers = nil
}

func setBuildModeForTest(t *testing.T, mode string) {
	t.Helper()
	originalBuildMode := buildinfo.BuildMode
	buildinfo.BuildMode = mode
	t.Cleanup(func() { buildinfo.BuildMode = originalBuildMode })
}

func TestSetupLoggerHonorsDebugLevelInReleaseBuild(t *testing.T) {
	setBuildModeForTest(t, "release")
	t.Cleanup(func() {
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

func TestSetupLoggerKeepsActiveFilesAtRootAndDedicatedDebugFile(t *testing.T) {
	setBuildModeForTest(t, "dev")
	t.Cleanup(closeCurrentLogHandlersForTest)

	logDir := t.TempDir()
	logger, _ := setupLogger(config.LoggingConfig{
		Level:       "debug",
		FileEnabled: true,
		FilePath:    filepath.Join(logDir, "app.log"),
		MaxFileSize: "10MB",
		MaxFiles:    5,
	})

	logger.Info("info log for active file")
	logger.Debug("debug log for dedicated file", "privacy_redacted_body", `{"token":"fake_token_0001"}`)
	closeCurrentLogHandlersForTest()

	appPath := filepath.Join(logDir, "app.log")
	debugPath := filepath.Join(logDir, "debug.log")
	for _, filename := range []string{"app.log", "claude.log", "codex.log", "debug.log"} {
		if _, err := os.Stat(filepath.Join(logDir, filename)); err != nil {
			t.Fatalf("active log %s should stay in the root log directory: %v", filename, err)
		}
	}

	appBytes, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read active app log: %v", err)
	}
	if !strings.Contains(string(appBytes), "info log for active file") {
		t.Fatalf("active app log missing info entry: %s", appBytes)
	}

	debugBytes, err := os.ReadFile(debugPath)
	if err != nil {
		t.Fatalf("read active debug log: %v", err)
	}
	debugText := string(debugBytes)
	for _, expected := range []string{"debug log for dedicated file", "privacy_redacted_body", "fake_token_0001"} {
		if !strings.Contains(debugText, expected) {
			t.Fatalf("debug log missing %q: %s", expected, debugText)
		}
	}
}

func TestSetupLoggerInfoLevelDoesNotCreateDebugFileOrExposeDebugLogs(t *testing.T) {
	setBuildModeForTest(t, "dev")
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

	appPath := filepath.Join(logDir, "app.log")
	debugPath := filepath.Join(logDir, "debug.log")

	if _, err := os.Stat(debugPath); !os.IsNotExist(err) {
		t.Fatalf("debug log file should not be created for info level, stat err=%v", err)
	}

	appBytes, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read active app log: %v", err)
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

func TestSetupLoggerKeepsReleaseActiveFileAtRoot(t *testing.T) {
	setBuildModeForTest(t, "release")
	t.Cleanup(closeCurrentLogHandlersForTest)

	logDir := t.TempDir()
	logger, _ := setupLogger(config.LoggingConfig{
		Level:       "info",
		FileEnabled: true,
		FilePath:    filepath.Join(logDir, "app.log"),
		MaxFileSize: "10MB",
		MaxFiles:    5,
	})
	logger.Info("release environment log")
	closeCurrentLogHandlersForTest()

	appPath := filepath.Join(logDir, "app.log")
	appBytes, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read release app log: %v", err)
	}
	if !strings.Contains(string(appBytes), "release environment log") {
		t.Fatalf("release app log missing entry: %s", appBytes)
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
			provider TEXT NOT NULL,
			category TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			UNIQUE(provider, category, key)
		);
		INSERT INTO settings (provider, category, key, value) VALUES ('global', 'logging', 'level', 'info');
		INSERT INTO settings (provider, category, key, value) VALUES ('global', 'logging', 'format', 'json');
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
