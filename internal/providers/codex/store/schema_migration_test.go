package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestEnsureCodexChannelsSchemaAllowsDuplicateEndpointsAcrossCredentialGroups(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)

	if err := (&schemaMigrator{}).ensureCodexChannelsSchema(ctx, db); err != nil {
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

func TestEnsureCodexChannelsSchemaAddsCredentialGroupBillingMultipliers(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)

	if err := (&schemaMigrator{}).ensureCodexChannelsSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	channelID := insertCodexSchemaTestChannel(t, db, "billing-fields")
	groupID := insertCodexSchemaTestCredentialGroup(t, db, channelID, "default", true)
	var cacheCreationMultiplier, longContextMultiplier float64
	var longContextInputMultiplier, longContextOutputMultiplier float64
	var longContextCacheCreationMultiplier, longContextCacheReadMultiplier float64
	var disableLongContext, disableServiceTier int
	var serviceTierMultipliers string
	if err := db.QueryRowContext(ctx, `
		SELECT cache_creation_cost_multiplier, long_context_cost_multiplier,
			long_context_input_cost_multiplier, long_context_output_cost_multiplier,
			long_context_cache_creation_cost_multiplier, long_context_cache_read_cost_multiplier,
			disable_long_context_pricing, disable_service_tier_pricing, service_tier_multipliers
		FROM codex_credential_groups WHERE id = ?
	`, groupID).Scan(&cacheCreationMultiplier, &longContextMultiplier,
		&longContextInputMultiplier, &longContextOutputMultiplier,
		&longContextCacheCreationMultiplier, &longContextCacheReadMultiplier,
		&disableLongContext, &disableServiceTier, &serviceTierMultipliers); err != nil {
		t.Fatalf("read credential-group billing defaults: %v", err)
	}
	if cacheCreationMultiplier != 1 || longContextMultiplier != 1 ||
		longContextInputMultiplier != 1 || longContextOutputMultiplier != 1 ||
		longContextCacheCreationMultiplier != 1 || longContextCacheReadMultiplier != 1 ||
		disableLongContext != 1 || disableServiceTier != 1 || serviceTierMultipliers != "[]" {
		t.Fatalf("unexpected billing defaults: cache=%v long=%v disabled=%v", cacheCreationMultiplier, longContextMultiplier, disableLongContext)
	}

	groupStore := NewSQLiteCodexCredentialGroupStore(db)
	group, err := groupStore.GetByID(ctx, groupID)
	if err != nil {
		t.Fatalf("read credential group: %v", err)
	}
	group.CacheCreationCostMultiplier = 1.2
	group.LongContextCostMultiplier = 1.5
	group.LongContextInputCostMultiplier = 1.1
	group.LongContextOutputCostMultiplier = 1.2
	group.LongContextCacheCreationCostMultiplier = 1.3
	group.LongContextCacheReadCostMultiplier = 1.4
	group.DisableLongContextPricing = true
	group.BillingRules = []CodexBillingRuleRecord{{
		Mode: "fixed", InputPrice: 2, OutputPrice: 4, CacheCreationPrice: 1, CacheReadPrice: 0.5,
	}}
	if err := groupStore.Update(ctx, group); err != nil {
		t.Fatalf("update credential-group billing fields: %v", err)
	}
	updated, err := groupStore.GetByID(ctx, groupID)
	if err != nil {
		t.Fatalf("read updated credential group: %v", err)
	}
	if updated.CacheCreationCostMultiplier != 1.2 || updated.LongContextCostMultiplier != 1.5 ||
		updated.LongContextInputCostMultiplier != 1.1 || updated.LongContextOutputCostMultiplier != 1.2 ||
		updated.LongContextCacheCreationCostMultiplier != 1.3 || updated.LongContextCacheReadCostMultiplier != 1.4 ||
		!updated.DisableLongContextPricing ||
		len(updated.BillingRules) != 1 || updated.BillingRules[0].InputPrice != 2 {
		t.Fatalf("billing fields did not survive round trip: %+v", updated)
	}
}

func TestEnsureCodexChannelsSchemaMigratesEndpointUniquenessToCredentialGroupScope(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)

	createLegacyCodexEndpointSchema(t, db)
	channelID := insertCodexSchemaTestChannel(t, db, "anyrouter")
	insertLegacyCodexSchemaTestEndpoint(t, db, channelID, "主站直连", "https://anyrouter.top")

	if err := (&schemaMigrator{}).ensureCodexChannelsSchema(ctx, db); err != nil {
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

func TestArchiveCodexProvidersUsesStableLegacyTableName(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)

	createCodexProviderArchiveTestTable(t, db, "codex_providers")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO codex_providers (name, provider_type, base_url, api_key, model, model_reasoning_effort)
		VALUES ('legacy', 'openai-compatible', 'https://example.invalid/v1', 'sk-test', 'gpt-5', 'high')
	`); err != nil {
		t.Fatalf("insert old provider: %v", err)
	}

	if err := (&schemaMigrator{}).archiveCodexProvidersTable(ctx, db); err != nil {
		t.Fatalf("archive old provider table: %v", err)
	}

	if exists, err := sqliteTableExists(ctx, db, "codex_providers"); err != nil {
		t.Fatalf("check old table: %v", err)
	} else if exists {
		t.Fatalf("expected old codex_providers table to be archived")
	}
	if exists, err := sqliteTableExists(ctx, db, "codex_providers_legacy"); err != nil {
		t.Fatalf("check stable archive table: %v", err)
	} else if !exists {
		t.Fatalf("expected stable codex_providers_legacy archive table")
	}
	if exists, err := sqliteTableExists(ctx, db, "codex_providers_legacy_v7"); err != nil {
		t.Fatalf("check deprecated archive table: %v", err)
	} else if exists {
		t.Fatalf("expected deprecated version-suffixed archive table to be removed")
	}
}

func TestMigrateCodexProvidersAssignsUniqueZeroBasedChannelPriorities(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)

	if err := (&schemaMigrator{}).ensureCodexChannelsSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	createCodexProviderArchiveTestTable(t, db, "codex_providers")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO codex_providers (name, provider_type, base_url, api_key, model, model_reasoning_effort)
		VALUES
			('first', 'openai-compatible', 'https://first.invalid/v1', 'sk-first', 'gpt-5', 'high'),
			('second', 'openai-compatible', 'https://second.invalid/v1', 'sk-second', 'gpt-5', 'high')
	`); err != nil {
		t.Fatalf("insert old providers: %v", err)
	}

	if err := (&schemaMigrator{}).migrateCodexProvidersToChannels(ctx, db, true); err != nil {
		t.Fatalf("migrate providers: %v", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT priority FROM codex_channels ORDER BY id`)
	if err != nil {
		t.Fatalf("query priorities: %v", err)
	}
	defer rows.Close()
	var priorities []int
	for rows.Next() {
		var priority int
		if err := rows.Scan(&priority); err != nil {
			t.Fatalf("scan priority: %v", err)
		}
		priorities = append(priorities, priority)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate priorities: %v", err)
	}
	if len(priorities) != 2 || priorities[0] != 0 || priorities[1] != 1 {
		t.Fatalf("expected migrated priorities [0 1], got %v", priorities)
	}
}

func TestNormalizeCodexProviderArchiveTablesRemovesDeprecatedArchiveTable(t *testing.T) {
	ctx := context.Background()
	db := newCodexSchemaTestDB(t)

	createCodexProviderArchiveTestTable(t, db, "codex_providers_legacy_v7")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO codex_providers_legacy_v7 (name, provider_type, base_url, api_key, model, model_reasoning_effort)
		VALUES ('legacy', 'openai-compatible', 'https://example.invalid/v1', 'sk-test', 'gpt-5', 'high')
	`); err != nil {
		t.Fatalf("insert deprecated archive provider: %v", err)
	}

	if err := (&schemaMigrator{}).normalizeCodexProviderArchiveTables(ctx, db); err != nil {
		t.Fatalf("normalize archive tables: %v", err)
	}

	if exists, err := sqliteTableExists(ctx, db, "codex_providers_legacy"); err != nil {
		t.Fatalf("check stable archive table: %v", err)
	} else if !exists {
		t.Fatalf("expected stable codex_providers_legacy archive table")
	}
	if exists, err := sqliteTableExists(ctx, db, "codex_providers_legacy_v7"); err != nil {
		t.Fatalf("check deprecated archive table: %v", err)
	} else if exists {
		t.Fatalf("expected deprecated version-suffixed archive table to be removed")
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM codex_providers_legacy WHERE name = 'legacy'`).Scan(&count); err != nil {
		t.Fatalf("count normalized rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected normalized archive row to be preserved, got %d", count)
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

func createCodexProviderArchiveTestTable(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE ` + tableName + ` (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			provider_type TEXT NOT NULL DEFAULT 'openai-compatible',
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			model_reasoning_effort TEXT NOT NULL DEFAULT '',
			enable_1m_context INTEGER NOT NULL DEFAULT 0,
			website TEXT,
			is_active INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		t.Fatalf("create %s: %v", tableName, err)
	}
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
			priority INTEGER NOT NULL DEFAULT 0,
			failover_enabled INTEGER NOT NULL DEFAULT 1,
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
