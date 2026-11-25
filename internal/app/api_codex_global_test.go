package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	_ "modernc.org/sqlite"

	"github.com/wsyangzy/Vane/internal/config"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

func TestUpdateCodexGlobalConfigPersistsOwnedDefaultsAndPreservesReasoningEffort(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
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
			UNIQUE(provider, category, key)
		)
	`); err != nil {
		t.Fatalf("create settings table: %v", err)
	}

	settingsService := settingsservice.NewSettingsService(settingsstore.NewSQLiteSettingsStore(db))
	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultModelKey, "gpt-old"); err != nil {
		t.Fatalf("seed model setting: %v", err)
	}
	if err := settingsService.Set(ctx, codexGlobalSettingsCategory, codexGlobalDefaultEnable1MContextKey, "true"); err != nil {
		t.Fatalf("seed 1m setting: %v", err)
	}

	codexHome := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())
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
	if got.Model != "gpt-new" || got.ReasoningEffort != "xhigh" || got.Enable1MContext {
		t.Fatalf("global config = {model:%q effort:%q 1m:%v}, want {gpt-new xhigh false}",
			got.Model, got.ReasoningEffort, got.Enable1MContext)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	text := string(content)
	if readCodexTopLevelString(text, "model") != "gpt-new" ||
		readCodexTopLevelString(text, "model_reasoning_effort") != "xhigh" ||
		strings.Contains(text, "model_context_window") {
		t.Fatalf("unexpected config.toml after update:\n%s", text)
	}
}

func TestUpdateCodexGlobalConfigPreservesClientManagedReasoningEffort(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())
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
	if readCodexTopLevelString(string(content), "model_reasoning_effort") != "minimal" {
		t.Fatalf("client-managed reasoning effort should be preserved:\n%s", content)
	}
}

func TestCodexGlobalConfigKeepsCatalogOnLocalListenerAndPreservesModeTwo(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	path := filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")
	catalogPath := filepath.Join(os.Getenv("CODEX_HOME"), "catalog.json")
	if err := os.WriteFile(catalogPath, []byte(`{"models":[{"slug":"model","base_instructions":"Keep model instructions"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	original := `model = "model"
model_provider = "vendor"
model_catalog_json = "` + filepath.ToSlash(catalogPath) + `"
[model_providers.vendor]
base_url = "http://127.0.0.1:8087/v1"
model_catalog_url = "https://vendor.invalid/v1/models"
requires_openai_auth = false
experimental_bearer_token = "existing-local-token"
http_headers = { "x-openai-actor-authorization" = "local-image-extension" }
[features]
image_generation = true
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{config: &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: 19087}}}
	if err := app.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "model"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	p := parsed["model_providers"].(map[string]any)["OpenAI"].(map[string]any)
	if p["base_url"] != "http://127.0.0.1:19087/v1" || p["model_catalog_url"] != nil || p["requires_openai_auth"] != false || p["experimental_bearer_token"] != "vane-local" || p["http_headers"] == nil || parsed["model_catalog_json"] != catalogPath {
		t.Fatalf("existing mode two or catalog choice changed: %s", raw)
	}
}

func TestCodexGlobalConfigMigratesExistingCompatibilityProviderWithoutWritingAuth(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	path := filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")
	original := `model = "keep-model"
model_reasoning_effort = "high"
model_provider = "Custom"
[model_providers.Custom]
base_url = "http://127.0.0.1:8087"
requires_openai_auth = true
env_key = "OLD_ENV_KEY"
[mcp_servers.keep]
command = "keep-command"
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{config: &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: 8087}, Auth: config.AuthConfig{Enabled: true, Token: "application-token"}}}
	if err := a.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "keep-model"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	p := parsed["model_providers"].(map[string]any)["OpenAI"].(map[string]any)
	if p["base_url"] != "http://127.0.0.1:8087/v1" || p["requires_openai_auth"] != false || p["experimental_bearer_token"] != "application-token" || p["env_key"] != nil || parsed["model_reasoning_effort"] != "high" || parsed["mcp_servers"] == nil {
		t.Fatal("existing ordinary provider was not migrated safely")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")); !os.IsNotExist(err) {
		t.Fatal("migration modified auth.json")
	}
}

func TestCodexGlobalConfigCreatesModeTwoUsingApplicationToken(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	app := &App{config: &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: 19087}, Auth: config.AuthConfig{Enabled: true, Token: "application-token"}}}
	if err := app.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "model"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	p := parsed["model_providers"].(map[string]any)["OpenAI"].(map[string]any)
	if parsed["model_provider"] != "OpenAI" || p["name"] != "OpenAI" || p["http_headers"].(map[string]any)["x-openai-actor-authorization"] != "local-image-extension" || p["requires_openai_auth"] != false || p["experimental_bearer_token"] != "application-token" || p["model_catalog_url"] != "http://127.0.0.1:19087/v1/models" {
		t.Fatal("new client provider does not use local mode two authentication")
	}
	if p["http_headers"].(map[string]any)["X-Relay-Key"] != nil {
		t.Fatal("managed client config duplicates the application bearer token")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("CODEX_HOME"), "auth.json")); !os.IsNotExist(err) {
		t.Fatal("mode two must not write an auth.json")
	}
}
