package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/service"
	"ai-relay-forwarder/internal/store"

	_ "modernc.org/sqlite"
)

func TestUpdateCodexGlobalConfigPersistsDefaultsForNextRead(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			category TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			value_type TEXT DEFAULT 'string',
			label TEXT,
			description TEXT,
			display_order INTEGER DEFAULT 0,
			requires_restart INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT '',
			updated_at DATETIME DEFAULT '',
			UNIQUE(category, key)
		)
	`); err != nil {
		t.Fatalf("create settings table: %v", err)
	}

	settingsService := service.NewSettingsService(store.NewSQLiteSettingsStore(db))
	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultModelKey, "gpt-old"); err != nil {
		t.Fatalf("seed model setting: %v", err)
	}
	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultReasoningEffortKey, "xhigh"); err != nil {
		t.Fatalf("seed effort setting: %v", err)
	}
	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultEnable1MContextKey, "true"); err != nil {
		t.Fatalf("seed 1m setting: %v", err)
	}

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(strings.Join([]string{
		`model = "gpt-old"`,
		`model_context_window = 1000000`,
		`model_reasoning_effort = "xhigh"`,
		`model_provider = "anyrouter"`,
		``,
		`[model_providers.anyrouter]`,
		`base_url = "https://old.example.invalid/v1"`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	app := &App{
		config: &config.Config{
			Server: config.ServerConfig{Host: "127.0.0.1", Port: 18087},
		},
		settingsService: settingsService,
	}

	if err := app.UpdateCodexGlobalConfig(CodexGlobalConfigInput{
		Model:           "gpt-new",
		ReasoningEffort: "low",
		Enable1MContext: false,
	}); err != nil {
		t.Fatalf("update global config: %v", err)
	}

	got, err := app.GetCodexGlobalConfig()
	if err != nil {
		t.Fatalf("get global config: %v", err)
	}
	if got.Model != "gpt-new" || got.ReasoningEffort != "low" || got.Enable1MContext {
		t.Fatalf("global config = {model:%q effort:%q 1m:%v}, want {gpt-new low false}",
			got.Model, got.ReasoningEffort, got.Enable1MContext)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `model = "gpt-new"`) ||
		!strings.Contains(text, `model_reasoning_effort = "low"`) ||
		strings.Contains(text, "model_context_window") {
		t.Fatalf("unexpected config.toml after update:\n%s", text)
	}
}

func TestUpdateCodexGlobalConfigNormalizesUnsupportedReasoningEffort(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(strings.Join([]string{
		`model = "gpt-5.6-sol"`,
		`model_reasoning_effort = "minimal"`,
		`model_provider = "anyrouter"`,
		``,
		`[model_providers.anyrouter]`,
		`base_url = "https://old.example.invalid/v1"`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	app := &App{
		config: &config.Config{
			Server: config.ServerConfig{Host: "127.0.0.1", Port: 18087},
		},
	}
	if err := app.UpdateCodexGlobalConfig(CodexGlobalConfigInput{
		Model:           "gpt-5.6-sol",
		ReasoningEffort: "minimal",
	}); err != nil {
		t.Fatalf("update global config: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !strings.Contains(string(content), `model_reasoning_effort = "xhigh"`) {
		t.Fatalf("unsupported reasoning effort should be normalized to xhigh:\n%s", content)
	}
}
