package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/logging"
	"github.com/wsyangzy/Vane/internal/platform/middleware"
)

func TestLogFormatWritesStructuredJSONAndPreservesText(t *testing.T) {
	for _, jsonFormat := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "test.log")
		rotator, err := logging.NewFileRotator(path, 1024*1024, 1, false)
		if err != nil {
			t.Fatal(err)
		}
		handler := &SimpleHandler{jsonFormat: jsonFormat, fileRotator: rotator, suppressConsole: true}
		record := slog.NewRecord(time.Now(), slog.LevelWarn, "quoted \"message\"\nsecond line", 0)
		record.AddAttrs(slog.String("provider", "codex"), slog.Int("attempts", 2))
		if err := handler.Handle(context.Background(), record); err != nil {
			t.Fatal(err)
		}
		if err := handler.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		err = json.Unmarshal(data, &value)
		if jsonFormat && (err != nil || value["provider"] != "codex" || value["attempts"] != float64(2) || value["msg"] != record.Message) {
			t.Fatalf("JSON log lost structured data: %s, %v", data, err)
		}
		if !jsonFormat && err == nil {
			t.Fatal("text format unexpectedly wrote JSON")
		}
	}
}

func TestLoggingSettingsReloadExistingProviderLoggers(t *testing.T) {
	a, _ := newDispatchSettingsTestApp(t)
	a.config.Logging.FileEnabled = true
	a.config.Logging.FilePath = filepath.Join(t.TempDir(), "app.log")
	previousLogger := slog.Default()
	t.Cleanup(func() {
		if a.logEmitter != nil {
			a.logEmitter.Stop()
		}
		closeCurrentLogHandlersForTest()
		slog.SetDefault(previousLogger)
	})
	if err := a.settingsService.InitDefaults(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.setupLogger()
	a.logEmitter.Start(nil)
	originalLogger, originalEmitter := a.logger, a.logEmitter
	providerLoggers := map[string]*slog.Logger{
		"claude": a.logger.With("provider", "claude").With("component", "health"),
		"codex":  a.logger.With("provider", "codex").With("component", "health"),
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{
		{Category: "logging", Key: "level", Value: "warn"},
		{Category: "logging", Key: "format", Value: "json"},
	}}); err != nil {
		t.Fatal(err)
	}
	if a.logger != originalLogger || a.logEmitter != originalEmitter {
		t.Fatal("saving log settings replaced existing logger or live-event references")
	}
	for provider, logger := range providerLoggers {
		if logger.Enabled(context.Background(), slog.LevelInfo) {
			t.Fatal("retained logger still uses the old log level")
		}
		logger.Info("filtered-old-reference")
		logger.Warn("retained-logger-json")
		data, err := os.ReadFile(filepath.Join(filepath.Dir(a.config.Logging.FilePath), provider+".log"))
		if err != nil {
			t.Fatal(err)
		}
		var record map[string]any
		if err := json.Unmarshal(data, &record); err != nil || record["provider"] != provider || record["component"] != "health" || record["msg"] != "retained-logger-json" {
			t.Fatalf("retained %s logger did not reach the current JSON output: %s, %v", provider, data, err)
		}
	}
	if err := a.UpdateSetting(UpdateSettingInput{Category: "logging", Key: "file_enabled", Value: "false"}); err != nil {
		t.Fatal(err)
	}
	for provider, logger := range providerLoggers {
		path := filepath.Join(filepath.Dir(a.config.Logging.FilePath), provider+".log")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		logger.Warn("live-only-file-disabled")
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("disabled file logger wrote to disk: %v", err)
		}
	}
	if a.logger != originalLogger || a.logEmitter != originalEmitter {
		t.Fatal("file switch replaced retained logging references")
	}
	logs := a.logHandler.GetRecentLogs(10)
	found := false
	for _, entry := range logs {
		found = found || entry.Message == "live-only-file-disabled"
	}
	if !found {
		t.Fatal("disabling file output also disabled the live log buffer")
	}
	if err := a.ResetCategorySettings("logging"); err != nil {
		t.Fatal(err)
	}
	for provider, logger := range providerLoggers {
		if !logger.Enabled(context.Background(), slog.LevelInfo) {
			t.Fatal("retained logger did not follow the default level after reset")
		}
		logger.Info("retained-logger-text")
		data, err := os.ReadFile(filepath.Join(filepath.Dir(a.config.Logging.FilePath), provider+".log"))
		if err != nil || !strings.Contains(string(data), "[INFO] retained-logger-text provider="+provider) {
			t.Fatalf("retained %s logger did not follow the text format after reset: %s, %v", provider, data, err)
		}
	}
}

func TestVisibleSettingsPersistReachRuntimeAndReset(t *testing.T) {
	setBuildModeForTest(t, "release")
	a, store := newDispatchSettingsTestApp(t)
	runningPort := a.config.Server.Port
	a.config.Logging.FileEnabled = false
	previousLogger := slog.Default()
	t.Cleanup(func() {
		if a.logEmitter != nil {
			a.logEmitter.Stop()
		}
		for _, handler := range currentLogHandlers {
			_ = handler.Close()
		}
		currentLogHandlers = nil
		slog.SetDefault(previousLogger)
	})
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	a.authMiddleware = middleware.NewAuthMiddleware(a.config.Auth)
	var oldRequestLogs bytes.Buffer
	a.loggingMiddleware = middleware.NewLoggingMiddleware(slog.New(slog.NewTextHandler(&oldRequestLogs, nil)))
	inputs := []UpdateSettingInput{
		{Category: "server", Key: "preferred_port", Value: "9087"},
		{Category: "auth", Key: "token", Value: "settings-test-application-token"},
		{Category: "auth", Key: "enabled", Value: "true"},
		{Category: "health", Key: "check_interval", Value: "45s"},
		{Category: "health", Key: "timeout", Value: "7s"},
		{Category: "health", Key: "health_path", Value: "/healthz"},
		{Category: "health", Key: "active_probes_enabled", Value: "false"},
		{Category: "request", Key: "global_timeout", Value: "480s"},
		{Category: "request", Key: "response_header_timeout", Value: "240s"},
		{Category: "request", Key: "default_response_header_timeout", Value: "80s"},
		{Category: "retry", Key: "max_total_attempts", Value: "6"},
		{Category: "retry", Key: "max_total_duration", Value: "45s"},
		{Category: "token_counting", Key: "enabled", Value: "true"},
		{Category: "token_counting", Key: "estimation_ratio", Value: "2.5"},
		{Category: "retention", Key: "retention_days", Value: "7"},
		{Category: "cache", Key: "enabled", Value: "false"},
		{Category: "cache", Key: "fallback_ttl", Value: "2m"},
		{Category: "cache", Key: "max_entries", Value: "128"},
		{Category: "cache", Key: "stale_on_error", Value: "false"},
		{Category: "logging", Key: "level", Value: "warn"},
		{Category: "logging", Key: "format", Value: "json"},
		{Category: "logging", Key: "max_file_size", Value: "2MB"},
		{Category: "logging", Key: "max_files", Value: "3"},
		{Category: "logging", Key: "compress_rotated", Value: "false"},
		{Category: "logging", Key: "file_enabled", Value: "false"},
	}
	visible, err := a.GetAllSettings()
	if err != nil || len(visible) != len(inputs) {
		t.Fatalf("settings coverage mismatch: %d controls, %d cases, error=%v", len(visible), len(inputs), err)
	}
	for _, input := range inputs {
		t.Run(input.Category+"."+input.Key, func(t *testing.T) {
			if err := a.UpdateSetting(input); err != nil {
				t.Fatal(err)
			}
			persisted, err := store.Get(ctx, input.Category, input.Key)
			if err != nil || persisted == nil || persisted.Value != input.Value {
				t.Fatalf("saved value missing: %+v, %v", persisted, err)
			}
		})
	}
	if a.config.Server.Port != runningPort {
		t.Fatal("saving a restart-only port changed the running listener")
	}
	if a.config.Health != (config.HealthConfig{PauseActiveProbes: true, CheckInterval: 45 * time.Second, Timeout: 7 * time.Second, HealthPath: "/healthz"}) {
		t.Fatalf("health config was not applied: %+v", a.config.Health)
	}
	if a.config.GlobalTimeout != 480*time.Second || a.config.CodexServer.Timeout != a.config.GlobalTimeout ||
		a.config.Streaming.ResponseHeaderTimeout != 240*time.Second || a.config.Streaming.DefaultResponseHeaderTimeout != 80*time.Second {
		t.Fatal("request timeouts were not applied")
	}
	if a.config.Retry.MaxTotalAttempts != 6 || a.config.Retry.MaxTotalDuration != 45*time.Second ||
		!a.config.TokenCounting.Enabled || a.config.TokenCounting.EstimationRatio != 2.5 || a.config.UsageTracking.RetentionDays != 7 {
		t.Fatal("dispatch, estimation or retention settings were not applied")
	}
	for _, options := range []struct {
		enabled, stale bool
		ttl            time.Duration
		max            int
	}{
		{a.usageSummaryCache.Options().Enabled, a.usageSummaryCache.Options().StaleOnError, a.usageSummaryCache.Options().TTL, a.usageSummaryCache.Options().MaxEntries},
		{a.tokenUsageCache.Options().Enabled, a.tokenUsageCache.Options().StaleOnError, a.tokenUsageCache.Options().TTL, a.tokenUsageCache.Options().MaxEntries},
	} {
		if options.enabled || options.stale || options.ttl != 2*time.Minute || options.max != 128 {
			t.Fatalf("cache options were not applied: %+v", options)
		}
	}
	if a.logger.Enabled(ctx, slog.LevelInfo) || !a.logger.Enabled(ctx, slog.LevelWarn) || !currentLogHandlers[0].jsonFormat {
		t.Fatal("logging settings were not applied to the actual logger")
	}
	if a.config.Logging.FileEnabled || a.config.Logging.MaxFileSize != "2MB" || a.config.Logging.MaxFiles != 3 || a.config.Logging.CompressRotated {
		t.Fatalf("file logging settings were not applied: %+v", a.config.Logging)
	}
	path := filepath.Join(t.TempDir(), "claude.log")
	rotator, err := logging.NewFileRotator(path, 1024*1024, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	currentLogHandlers[1].fileRotator = rotator
	currentLogHandlers[1].suppressConsole = true
	a.loggingMiddleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/settings-logger-check", nil))
	data, err := os.ReadFile(path)
	var logRecord map[string]any
	if err != nil || json.Unmarshal(data, &logRecord) != nil || logRecord["path"] != "/settings-logger-check" || logRecord["provider"] != "claude" || oldRequestLogs.Len() != 0 {
		t.Fatalf("request middleware did not switch to the current JSON logger: %s, %v", data, err)
	}
	for _, provider := range []string{"claude", "codex"} {
		request := httptest.NewRequest("POST", "/", nil)
		if _, _, err := a.authenticateRoutingProfile(request, provider); err == nil {
			t.Fatal("enabled application authentication accepted a missing token")
		}
		request.Header.Set("X-Relay-Key", inputs[1].Value)
		if _, _, err := a.authenticateRoutingProfile(request, provider); err != nil {
			t.Fatal("saved application token was rejected")
		}
	}
	for _, category := range a.GetSettingCategories() {
		if err := a.ResetCategorySettings(category.Name); err != nil {
			t.Fatal(err)
		}
	}
	if a.config.Auth.Enabled || a.config.Auth.Token != "" {
		t.Fatal("resetting authentication retained an old token")
	}
	for _, defaults := range a.settingsService.GetAllDefaults() {
		if !isVisibleSetting(defaults.Category, defaults.Key) {
			continue
		}
		persisted, err := store.Get(ctx, defaults.Category, defaults.Key)
		if err != nil || persisted == nil || persisted.Value != defaults.Value {
			t.Fatalf("reset did not restore %s.%s", defaults.Category, defaults.Key)
		}
	}
}

func TestLoggingRotationSettingsControlFilesAndDeferredCleanup(t *testing.T) {
	setBuildModeForTest(t, "release")
	a, _ := newDispatchSettingsTestApp(t)
	if err := a.settingsService.InitDefaults(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.setupLogger()
	archiveRoot := filepath.Join(filepath.Dir(a.config.Logging.FilePath), "release", "claude")
	oldDir := filepath.Join(archiveRoot, "2020-01")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(oldDir, "claude-2020-01-01.log")
	if err := os.WriteFile(oldFile, []byte("retained-until-rotation"), 0600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{
		{Category: "logging", Key: "level", Value: "warn"},
		{Category: "logging", Key: "max_file_size", Value: "256B"},
		{Category: "logging", Key: "max_files", Value: "1"},
		{Category: "logging", Key: "compress_rotated", Value: "true"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatal("saving a lower archive limit immediately deleted historical files")
	}
	logger := a.logger.With("provider", "claude")
	logger.Warn("rotation-first-" + strings.Repeat("x", 160))
	logger.Warn("rotation-second-" + strings.Repeat("x", 160))
	deadline := time.Now().Add(3 * time.Second)
	for {
		files, err := filepath.Glob(filepath.Join(archiveRoot, "*", "*.log*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 1 && strings.HasSuffix(files[0], ".gz") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rotation/compression/archive limit not applied: %v", files)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("rotation did not clean the oldest archive across months")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(a.config.Logging.FilePath), "claude.log"))
	if err != nil || !strings.Contains(string(data), "rotation-second-") {
		t.Fatalf("rotation did not preserve the new active log: %s, %v", data, err)
	}
}

func TestInvalidSettingsCannotPartiallySaveOrLeaveStaleAuth(t *testing.T) {
	a, store := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	for _, input := range []UpdateSettingInput{
		{Category: "server", Key: "preferred_port", Value: "65536"},
		{Category: "server", Key: "preferred_port", Value: "1.5"},
		{Category: "health", Key: "check_interval", Value: "0s"},
		{Category: "health", Key: "timeout", Value: "broken"},
		{Category: "health", Key: "health_path", Value: "https://example.test/"},
		{Category: "request", Key: "global_timeout", Value: "-1s"},
		{Category: "request", Key: "response_header_timeout", Value: "0s"},
		{Category: "request", Key: "default_response_header_timeout", Value: "bad"},
		{Category: "auth", Key: "enabled", Value: "true"},
		{Category: "auth", Key: "token", Value: "has whitespace"},
		{Category: "token_counting", Key: "estimation_ratio", Value: "0"},
		{Category: "token_counting", Key: "estimation_ratio", Value: "NaN"},
		{Category: "token_counting", Key: "estimation_ratio", Value: "+Inf"},
		{Category: "retention", Key: "retention_days", Value: "-1"},
		{Category: "cache", Key: "enabled", Value: "invalid"},
		{Category: "cache", Key: "fallback_ttl", Value: "0s"},
		{Category: "cache", Key: "max_entries", Value: "0"},
		{Category: "logging", Key: "level", Value: "trace"},
		{Category: "logging", Key: "format", Value: "xml"},
		{Category: "health", Key: "active_probes_enabled", Value: "yes"},
		{Category: "logging", Key: "file_enabled", Value: "yes"},
		{Category: "logging", Key: "max_file_size", Value: "0MB"},
		{Category: "logging", Key: "max_file_size", Value: "-1MB"},
		{Category: "logging", Key: "max_file_size", Value: "invalid"},
		{Category: "logging", Key: "max_file_size", Value: "9007199254740993KB"},
		{Category: "logging", Key: "max_files", Value: "0"},
		{Category: "logging", Key: "compress_rotated", Value: "yes"},
	} {
		t.Run(input.Category+"."+input.Key+"/"+input.Value, func(t *testing.T) {
			batch := BatchUpdateSettingsInput{Settings: []UpdateSettingInput{{Category: "retry", Key: "max_total_attempts", Value: "8"}, input}}
			if err := a.BatchUpdateSettings(batch); err == nil {
				t.Fatal("invalid batch was accepted")
			}
			persisted, _ := store.Get(ctx, "retry", "max_total_attempts")
			if persisted.Value != "10" {
				t.Fatal("invalid batch partially changed settings")
			}
		})
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{
		{Category: "auth", Key: "enabled", Value: "true"},
		{Category: "auth", Key: "token", Value: "valid-test-token"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateSetting(UpdateSettingInput{Category: "auth", Key: "token", Value: ""}); err == nil {
		t.Fatal("clearing the token while authentication is enabled should be rejected")
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{
		{Category: "auth", Key: "token", Value: ""},
		{Category: "auth", Key: "enabled", Value: "false"},
	}}); err != nil || a.config.Auth.Token != "" || a.config.Auth.Enabled {
		t.Fatalf("disabled authentication did not clear the runtime token: %v", err)
	}
}

func TestDevPortSaveAndResetRemainIsolatedAndAtomic(t *testing.T) {
	setBuildModeForTest(t, "dev")
	a, store := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := store.Set(ctx, "server", "preferred_port", "9087"); err != nil {
		t.Fatal(err)
	}
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	batch := BatchUpdateSettingsInput{Settings: []UpdateSettingInput{
		{Category: "server", Key: "preferred_port", Value: "9000"},
		{Category: "retry", Key: "max_total_attempts", Value: "0"},
	}}
	if err := a.BatchUpdateSettings(batch); err == nil {
		t.Fatal("invalid dev settings batch was accepted")
	}
	port, _ := store.Get(ctx, "server", "preferred_port_dev")
	if port.Value != "9087" || !port.RequiresRestart {
		t.Fatalf("failed batch changed the dev port or its restart metadata: %+v", port)
	}
	if err := a.UpdateSetting(batch.Settings[0]); err != nil {
		t.Fatal(err)
	}
	port, _ = store.Get(ctx, "server", "preferred_port")
	if port.Value != "9087" {
		t.Fatal("dev save changed the release port")
	}
	if err := a.ResetCategorySettings("server"); err != nil {
		t.Fatal(err)
	}
	port, _ = store.Get(ctx, "server", "preferred_port_dev")
	if port.Value != "8087" || !port.RequiresRestart {
		t.Fatal("reset did not restore the restart-only dev port")
	}
}
