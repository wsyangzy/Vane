package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/store"

	_ "modernc.org/sqlite"
)

func TestCodexEndpointAllowsDuplicateNameAndURLAcrossCredentialGroups(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "github",
		ApiKey:    "sk-github",
	})
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}

	first, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "大陆网络优化",
		BaseURL:           "https://a-ocnfniawgw.cn-shanghai.fcapp.run",
		FailoverEnabled:   true,
	})
	if err != nil {
		t.Fatalf("create first endpoint: %v", err)
	}
	second, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: otherGroup.ID,
		Name:              first.Name,
		BaseURL:           first.BaseURL,
		FailoverEnabled:   true,
	})
	if err != nil {
		t.Fatalf("expected same name/base_url in another credential group to be allowed, got: %v", err)
	}
	if second.CredentialGroupID != otherGroup.ID {
		t.Fatalf("expected second endpoint group %d, got %d", otherGroup.ID, second.CredentialGroupID)
	}

	_, err = endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: otherGroup.ID,
		Name:              first.Name,
		BaseURL:           "https://another.example.invalid/codex/v1",
		FailoverEnabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "相同的端点名") {
		t.Fatalf("expected duplicate name in same credential group to be rejected, got: %v", err)
	}
}

func TestNormalizeAndValidateCodexEndpointAllowsInternalSpaces(t *testing.T) {
	cases := []string{
		"主站直连",
		"主站 直连",
		"ark api",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			record := &store.CodexEndpointRecord{
				ChannelID:       1,
				Name:            name,
				BaseURL:         "https://example.invalid/codex/v1",
				FailoverEnabled: true,
			}
			if err := normalizeAndValidateCodexEndpoint(record); err != nil {
				t.Fatalf("expected endpoint name %q to be valid, got: %v", name, err)
			}
		})
	}
}

func TestCodexEndpointRejectsDisablingActiveEndpointFailover(t *testing.T) {
	ctx := context.Background()
	db := newCodexEndpointTestDB(t)
	channelStore := store.NewSQLiteCodexChannelStore(db)
	groupStore := store.NewSQLiteCodexCredentialGroupStore(db)
	endpointStore := store.NewSQLiteCodexEndpointStore(db)
	endpointService := NewCodexEndpointService(endpointStore, channelStore, groupStore)

	channel, err := channelStore.Create(ctx, &store.CodexChannelRecord{
		Name:         "anyrouter",
		ProviderType: "openai-compatible",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defaultGroup, err := groupStore.Create(ctx, &store.CodexCredentialGroupRecord{
		ChannelID: channel.ID,
		Name:      "default",
		ApiKey:    "sk-default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create default group: %v", err)
	}
	active, err := endpointService.CreateEndpoint(ctx, &store.CodexEndpointRecord{
		ChannelID:         channel.ID,
		CredentialGroupID: defaultGroup.ID,
		Name:              "active",
		BaseURL:           "https://example.invalid/codex/v1",
		FailoverEnabled:   true,
	})
	if err != nil {
		t.Fatalf("create active endpoint: %v", err)
	}
	if !active.IsActive {
		t.Fatalf("expected first endpoint to be active")
	}

	err = endpointService.UpdateEndpointByID(
		ctx,
		active.ID,
		active.Name,
		active.BaseURL,
		active.CredentialGroupID,
		active.ApiKey,
		false,
		active.SkipHealthCheck,
		active.Priority,
		active.TimeoutSeconds,
		active.CostMultiplier,
		active.InputCostMultiplier,
		active.OutputCostMultiplier,
		active.CacheReadCostMultiplier,
		active.BillingOutputTokenSource,
		active.ModelMultipliers,
	)
	if err == nil {
		t.Fatalf("expected active endpoint failover disable to be rejected")
	}
	if !strings.Contains(err.Error(), "当前激活端点必须参与故障转移") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeAndValidateCodexEndpointRejectsControlWhitespaceInNames(t *testing.T) {
	cases := []string{
		"主站\t直连",
		"主站\n直连",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			record := &store.CodexEndpointRecord{
				ChannelID:       1,
				Name:            name,
				BaseURL:         "https://example.invalid/codex/v1",
				FailoverEnabled: true,
			}
			if err := normalizeAndValidateCodexEndpoint(record); err == nil {
				t.Fatalf("expected endpoint name %q to be invalid", name)
			}
		})
	}
}

func newCodexEndpointTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	statements := []string{
		`CREATE TABLE codex_channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			provider_type TEXT NOT NULL DEFAULT 'openai-compatible',
			api_key TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			model_mapping TEXT NOT NULL DEFAULT '{}',
			model_reasoning_effort TEXT NOT NULL DEFAULT '',
			enable_1m_context INTEGER NOT NULL DEFAULT 0,
			website TEXT,
			is_active INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE codex_credential_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			api_key TEXT NOT NULL DEFAULT '',
			is_default INTEGER NOT NULL DEFAULT 0,
			cost_multiplier REAL NOT NULL DEFAULT 1,
			input_cost_multiplier REAL NOT NULL DEFAULT 1,
			output_cost_multiplier REAL NOT NULL DEFAULT 1,
			cache_read_cost_multiplier REAL NOT NULL DEFAULT 1,
			billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
			model_multipliers TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE codex_endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL,
			credential_group_id INTEGER,
			api_key TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0,
			failover_enabled INTEGER NOT NULL DEFAULT 1,
			skip_health_check INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
			cost_multiplier REAL NOT NULL DEFAULT 1,
			input_cost_multiplier REAL NOT NULL DEFAULT 1,
			output_cost_multiplier REAL NOT NULL DEFAULT 1,
			cache_read_cost_multiplier REAL NOT NULL DEFAULT 1,
			billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
			model_multipliers TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create test schema: %v", err)
		}
	}
	return db
}
