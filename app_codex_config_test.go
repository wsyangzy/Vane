package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/store"

	_ "modernc.org/sqlite"
)

func TestCodexProviderSectionHeaderUsesQuotedKeyForUnicodeName(t *testing.T) {
	if got, want := codexProviderSectionHeader("custom_1"), "[model_providers.custom_1]"; got != want {
		t.Fatalf("ASCII header mismatch: got %q, want %q", got, want)
	}

	if got, want := codexProviderSectionHeader("ark api"), `[model_providers."ark api"]`; got != want {
		t.Fatalf("space-containing header mismatch: got %q, want %q", got, want)
	}

	if got, want := codexProviderSectionHeader("主渠道"), `[model_providers."主渠道"]`; got != want {
		t.Fatalf("Unicode header mismatch: got %q, want %q", got, want)
	}
}

func TestUpsertCodexProviderSectionUsesQuotedUnicodeHeader(t *testing.T) {
	record := &store.CodexProviderRecord{
		Name:            "主渠道",
		BaseURL:         "https://example.invalid/codex/v1",
		ApiKey:          "test-key",
		Model:           "gpt-5",
		ReasoningEffort: "high",
	}

	got, err := upsertCodexProviderSection("", record)
	if err != nil {
		t.Fatalf("upsertCodexProviderSection returned error: %v", err)
	}

	if !strings.Contains(got, `[model_providers."主渠道"]`) {
		t.Fatalf("expected quoted Unicode section header, got:\n%s", got)
	}
	if strings.Contains(got, "[model_providers.主渠道]") {
		t.Fatalf("unexpected bare Unicode section header, got:\n%s", got)
	}
}

func TestDeleteCodexProviderSectionHandlesQuotedUnicodeHeader(t *testing.T) {
	content := strings.Join([]string{
		`model = "gpt-5"`,
		``,
		`[model_providers."主渠道"]`,
		`name = "主渠道"`,
		`base_url = "https://example.invalid/codex/v1"`,
		``,
		`[model_providers.backup]`,
		`name = "backup"`,
		`base_url = "https://backup.example.invalid/v1"`,
		``,
	}, "\n")

	got := deleteCodexProviderSection(content, "主渠道")
	if strings.Contains(got, "主渠道") {
		t.Fatalf("expected Unicode section to be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "[model_providers.backup]") {
		t.Fatalf("expected unrelated section to remain, got:\n%s", got)
	}
}

func TestUpsertCodexProviderBaseURLNormalizesUnicodeHeader(t *testing.T) {
	content := strings.Join([]string{
		`[model_providers.主渠道]`,
		`name = "主渠道"`,
		`base_url = "https://old.example.invalid/v1"`,
		``,
	}, "\n")

	got, err := upsertCodexProviderBaseURL(content, "主渠道", "https://example.invalid/codex/v1")
	if err != nil {
		t.Fatalf("upsertCodexProviderBaseURL returned error: %v", err)
	}

	if !strings.Contains(got, `[model_providers."主渠道"]`) {
		t.Fatalf("expected quoted Unicode section header, got:\n%s", got)
	}
	if strings.Contains(got, "[model_providers.主渠道]") {
		t.Fatalf("expected bare Unicode header to be normalized, got:\n%s", got)
	}
	if !strings.Contains(got, `base_url = "https://example.invalid/codex/v1"`) {
		t.Fatalf("expected base_url to be updated, got:\n%s", got)
	}
}

func TestEnsureCodexChannelsSchemaAllowsDuplicateEndpointsAcrossCredentialGroups(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)
	app := &App{}

	if err := app.ensureCodexChannelsSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	channelID := insertCodexSchemaTestChannel(t, db, "anyrouter")
	defaultGroupID := insertCodexSchemaTestCredentialGroup(t, db, channelID, "default", true)
	githubGroupID := insertCodexSchemaTestCredentialGroup(t, db, channelID, "github", false)
	insertCodexSchemaTestEndpoint(t, db, channelID, defaultGroupID, "主站直连", "https://anyrouter.top")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO codex_endpoints (channel_id, credential_group_id, name, base_url)
		VALUES (?, ?, ?, ?)
	`, channelID, githubGroupID, "主站直连", "https://anyrouter.top"); err != nil {
		t.Fatalf("expected duplicate endpoint name/base_url across credential groups to be allowed: %v", err)
	}
}

func TestEnsureCodexChannelsSchemaMigratesEndpointUniquenessToCredentialGroupScope(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)
	app := &App{}

	createLegacyCodexEndpointSchema(t, db)
	channelID := insertCodexSchemaTestChannel(t, db, "anyrouter")
	insertLegacyCodexSchemaTestEndpoint(t, db, channelID, "主站直连", "https://anyrouter.top")

	if err := app.ensureCodexChannelsSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	githubGroupID := insertCodexSchemaTestCredentialGroup(t, db, channelID, "github", false)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO codex_endpoints (channel_id, credential_group_id, name, base_url)
		VALUES (?, ?, ?, ?)
	`, channelID, githubGroupID, "主站直连", "https://anyrouter.top"); err != nil {
		t.Fatalf("expected migrated schema to allow duplicate endpoint name/base_url across credential groups: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO codex_endpoints (channel_id, credential_group_id, name, base_url)
		VALUES (?, ?, ?, ?)
	`, channelID, githubGroupID, "主站直连", "https://another.example.invalid"); err == nil {
		t.Fatalf("expected duplicate endpoint name inside the same credential group to be rejected")
	}
}

func newCodexSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func createLegacyCodexEndpointSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE codex_channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			provider_type TEXT NOT NULL DEFAULT 'openai-compatible',
			api_key TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			model_mapping TEXT NOT NULL DEFAULT '{}',
			model_reasoning_effort TEXT NOT NULL DEFAULT 'xhigh',
			enable_1m_context INTEGER NOT NULL DEFAULT 0,
			website TEXT,
			is_active INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT '',
			updated_at DATETIME DEFAULT ''
		)`,
		`CREATE TABLE codex_endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0,
			failover_enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
			cost_multiplier REAL NOT NULL DEFAULT 1.0,
			input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
			output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
			cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
			billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
			model_multipliers TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME DEFAULT '',
			updated_at DATETIME DEFAULT '',
			UNIQUE(channel_id, base_url),
			UNIQUE(channel_id, name)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
}

func insertCodexSchemaTestChannel(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	result, err := db.Exec(`
		INSERT INTO codex_channels (name, provider_type, api_key, model, model_reasoning_effort)
		VALUES (?, 'openai-compatible', '', '', 'xhigh')
	`, name)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read channel id: %v", err)
	}
	return id
}

func insertCodexSchemaTestCredentialGroup(t *testing.T, db *sql.DB, channelID int64, name string, isDefault bool) int64 {
	t.Helper()
	defaultValue := 0
	if isDefault {
		defaultValue = 1
	}
	result, err := db.Exec(`
		INSERT INTO codex_credential_groups (channel_id, name, api_key, is_default)
		VALUES (?, ?, ?, ?)
	`, channelID, name, "sk-test", defaultValue)
	if err != nil {
		t.Fatalf("insert credential group: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read group id: %v", err)
	}
	return id
}

func insertCodexSchemaTestEndpoint(t *testing.T, db *sql.DB, channelID, groupID int64, name, baseURL string) int64 {
	t.Helper()
	result, err := db.Exec(`
		INSERT INTO codex_endpoints (channel_id, credential_group_id, name, base_url)
		VALUES (?, ?, ?, ?)
	`, channelID, groupID, name, baseURL)
	if err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read endpoint id: %v", err)
	}
	return id
}

func insertLegacyCodexSchemaTestEndpoint(t *testing.T, db *sql.DB, channelID int64, name, baseURL string) int64 {
	t.Helper()
	result, err := db.Exec(`
		INSERT INTO codex_endpoints (channel_id, name, base_url)
		VALUES (?, ?, ?)
	`, channelID, name, baseURL)
	if err != nil {
		t.Fatalf("insert legacy endpoint: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read legacy endpoint id: %v", err)
	}
	return id
}
