package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	codexconfig "github.com/wsyangzy/Vane/internal/providers/codex/clientconfig"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
)

func TestBuildCodexTopLevelConfigValuesLeavesReasoningEffortClientManaged(t *testing.T) {
	record := &codexstore.CodexProviderRecord{
		Name:            "relay",
		Model:           "gpt-5.6",
		ReasoningEffort: "high",
	}

	values := codexconfig.BuildCodexTopLevelConfigValues(record)
	if _, exists := values["model_reasoning_effort"]; exists {
		t.Fatal("model_reasoning_effort must not be managed by provider sync")
	}

	updated, err := codexconfig.UpsertCodexTopLevelConfig(`model_reasoning_effort = "minimal"`, values)
	if err != nil {
		t.Fatalf("upsert top-level config: %v", err)
	}
	if !strings.Contains(updated, `model_reasoning_effort = "minimal"`) {
		t.Fatalf("client-managed reasoning effort changed:\n%s", updated)
	}
}

func TestRemoveCodexProviderConfigPreservesClientManagedReasoningEffort(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(strings.Join([]string{
		`model = "gpt-5.6"`,
		`model_reasoning_effort = "minimal"`,
		`model_provider = "relay"`,
		``,
		`[model_providers.relay]`,
		`base_url = "https://relay.example.invalid/v1"`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	if err := (&App{}).removeCodexProviderConfig("relay", nil); err != nil {
		t.Fatalf("remove provider config: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `model_reasoning_effort = "minimal"`) {
		t.Fatalf("client-managed reasoning effort should be preserved:\n%s", text)
	}
	if strings.Contains(text, `model = "gpt-5.6"`) || strings.Contains(text, `model_provider = "relay"`) {
		t.Fatalf("Relay-managed top-level config should be removed:\n%s", text)
	}
}

func TestCodexProviderSectionHeaderUsesQuotedKeyForUnicodeName(t *testing.T) {
	if got, want := codexconfig.CodexProviderSectionHeader("custom_1"), "[model_providers.custom_1]"; got != want {
		t.Fatalf("ASCII header mismatch: got %q, want %q", got, want)
	}

	if got, want := codexconfig.CodexProviderSectionHeader("ark api"), `[model_providers."ark api"]`; got != want {
		t.Fatalf("space-containing header mismatch: got %q, want %q", got, want)
	}

	if got, want := codexconfig.CodexProviderSectionHeader("主渠道"), `[model_providers."主渠道"]`; got != want {
		t.Fatalf("Unicode header mismatch: got %q, want %q", got, want)
	}
}

func TestUpsertCodexProviderSectionUsesQuotedUnicodeHeader(t *testing.T) {
	record := &codexstore.CodexProviderRecord{
		Name:            "主渠道",
		BaseURL:         "https://example.invalid/codex/v1",
		ApiKey:          "test-key",
		Model:           "gpt-5",
		ReasoningEffort: "high",
	}

	got, err := codexconfig.UpsertCodexProviderSection("", record)
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

	got := codexconfig.DeleteCodexProviderSection(content, "主渠道")
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
