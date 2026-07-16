package tracking

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// 回归测试：旧库缺少后续新增列时，InitSchema 应能通过 migrateSchema 补齐关键列，避免启动失败。
func TestSQLiteAdapter_InitSchema_LegacyDBMissingColumns(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	dsn := dbPath + "?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=60000"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	// 1) 造一个“旧版本”数据库：request_logs/endpoints/channels 存在，但缺少后续新增列（5m/1h 缓存字段、端点超时、渠道优先级等）。
	// 注意：旧表存在时，schema.sql 的 CREATE TABLE IF NOT EXISTS 不会补列，需要 migrateSchema 补齐。
	_, err = db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT UNIQUE NOT NULL,
    client_ip TEXT,
    user_agent TEXT,
    method TEXT DEFAULT 'POST',
    path TEXT DEFAULT '/v1/messages',
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    duration_ms INTEGER,
    channel TEXT DEFAULT '',
    endpoint_name TEXT,
    group_name TEXT,
    model_name TEXT,
    is_streaming BOOLEAN DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'pending',
    http_status_code INTEGER,
    retry_count INTEGER DEFAULT 0,
    failure_reason TEXT,
    last_failure_reason TEXT,
    cancel_reason TEXT,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cache_creation_tokens INTEGER DEFAULT 0,
    cache_read_tokens INTEGER DEFAULT 0,
    input_cost_usd REAL DEFAULT 0,
    output_cost_usd REAL DEFAULT 0,
    cache_creation_cost_usd REAL DEFAULT 0,
    cache_read_cost_usd REAL DEFAULT 0,
    total_cost_usd REAL DEFAULT 0,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

CREATE TABLE IF NOT EXISTS endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel TEXT NOT NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    token TEXT,
    api_key TEXT,
    headers TEXT,
    priority INTEGER DEFAULT 1,
    failover_enabled INTEGER DEFAULT 1,
    cooldown_seconds INTEGER,
    enabled INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    website TEXT,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

CREATE TABLE IF NOT EXISTS model_pricing (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model_name TEXT UNIQUE NOT NULL,
    provider TEXT NOT NULL DEFAULT 'claude',
    input_price REAL NOT NULL DEFAULT 3.0,
    output_price REAL NOT NULL DEFAULT 15.0,
    cache_creation_price_5m REAL DEFAULT 3.75,
    cache_creation_price_1h REAL DEFAULT 6.0,
    cache_read_price REAL DEFAULT 0.30,
    display_name TEXT,
    description TEXT,
    is_default INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

INSERT INTO model_pricing (
    model_name, provider, input_price, output_price,
    cache_creation_price_5m, cache_creation_price_1h, cache_read_price
) VALUES ('legacy-model', 'codex', 2.0, 8.0, 2.5, 4.0, 0.2);
`)
	if err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	adapter, err := NewSQLiteAdapter(DatabaseConfig{
		DatabasePath: dbPath,
		Timezone:     "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter: %v", err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatalf("adapter.Open: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	// 2) InitSchema 需要自动补列，并确保 schema.sql 可顺利执行（包括索引/触发器）。
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("adapter.InitSchema: %v", err)
	}

	// 3) 验证关键列已存在（由 migrateSchema 补齐）
	for _, c := range []string{"endpoint_chain", "cache_creation_5m_tokens", "cache_creation_1h_tokens", "cache_creation_5m_cost_usd", "cache_creation_1h_cost_usd"} {
		if !sqliteColumnExists(t, adapter.GetReadDB(), "request_logs", c) {
			t.Fatalf("expected request_logs.%s to exist after InitSchema", c)
		}
	}
	for _, c := range []string{"timeout_seconds", "supports_count_tokens"} {
		if !sqliteColumnExists(t, adapter.GetReadDB(), "endpoints", c) {
			t.Fatalf("expected endpoints.%s to exist after InitSchema", c)
		}
	}
	if !sqliteColumnExists(t, adapter.GetReadDB(), "channels", "priority") {
		t.Fatalf("expected channels.priority to exist after InitSchema")
	}
	if !sqliteColumnExists(t, adapter.GetReadDB(), "channels", "failover_enabled") {
		t.Fatalf("expected channels.failover_enabled to exist after InitSchema")
	}
	for _, c := range []string{
		"long_context_cost_multiplier",
		"long_context_input_cost_multiplier",
		"long_context_output_cost_multiplier",
		"long_context_cache_creation_cost_multiplier",
		"long_context_cache_read_cost_multiplier",
		"disable_long_context_pricing",
	} {
		if !sqliteColumnExists(t, adapter.GetReadDB(), "claude_credential_groups", c) {
			t.Fatalf("expected claude_credential_groups.%s to exist after InitSchema", c)
		}
	}

	longContextColumns := []string{
		"long_context_threshold",
		"long_context_input_price",
		"long_context_cache_read_price",
		"long_context_cache_write_price",
		"long_context_output_price",
	}
	for _, column := range longContextColumns {
		if !sqliteColumnExists(t, adapter.GetReadDB(), "model_pricing", column) {
			t.Fatalf("expected model_pricing.%s to exist after InitSchema", column)
		}
	}

	var (
		threshold                                                 int64
		inputPrice                                                float64
		longInput, longCacheRead, longCacheWrite, longOutputPrice float64
	)
	if err := adapter.GetReadDB().QueryRowContext(ctx, `
		SELECT input_price, long_context_threshold, long_context_input_price,
			long_context_cache_read_price, long_context_cache_write_price,
			long_context_output_price
		FROM model_pricing WHERE model_name = 'legacy-model'
	`).Scan(&inputPrice, &threshold, &longInput, &longCacheRead, &longCacheWrite, &longOutputPrice); err != nil {
		t.Fatalf("query migrated model pricing: %v", err)
	}
	if inputPrice != 2.0 {
		t.Fatalf("expected legacy input price to be preserved, got %f", inputPrice)
	}
	if threshold != 0 || longInput != 0 || longCacheRead != 0 || longCacheWrite != 0 || longOutputPrice != 0 {
		t.Fatalf("expected migrated long-context pricing to default to zero")
	}
}

func TestSQLiteAdapter_InitSchema_DisablesLegacyLongContextPricingOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "long-context-default.db")
	adapter, err := NewSQLiteAdapter(DatabaseConfig{
		DatabasePath: dbPath,
		Timezone:     "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter: %v", err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatalf("adapter.Open: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("initial InitSchema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	db := adapter.GetWriteDB()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claude_credential_groups (channel, name, disable_long_context_pricing)
		VALUES ('legacy-claude', 'default', 0);
		INSERT INTO codex_channels (name, provider_type, api_key, model)
		VALUES ('legacy-codex', 'openai-compatible', '', 'gpt-5');
		INSERT INTO codex_credential_groups (channel_id, name, disable_long_context_pricing)
		SELECT id, 'default', 0 FROM codex_channels WHERE name = 'legacy-codex';
		DELETE FROM schema_versions WHERE version = '20260712_default_disable_long_context_pricing';
	`); err != nil {
		t.Fatalf("seed legacy credential groups: %v", err)
	}

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("migrating InitSchema: %v", err)
	}

	assertLongContextDisabled := func(table string) {
		t.Helper()
		var disabled int
		if err := adapter.GetReadDB().QueryRowContext(ctx, "SELECT disable_long_context_pricing FROM "+table+" LIMIT 1").Scan(&disabled); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if disabled != 1 {
			t.Fatalf("expected %s legacy group to default Long context off, got %d", table, disabled)
		}
	}
	assertLongContextDisabled("claude_credential_groups")
	assertLongContextDisabled("codex_credential_groups")

	var migrationCount int
	if err := adapter.GetReadDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM schema_versions
		WHERE version = '20260712_default_disable_long_context_pricing'
	`).Scan(&migrationCount); err != nil {
		t.Fatalf("query migration version: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one Long context default migration record, got %d", migrationCount)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE claude_credential_groups SET disable_long_context_pricing = 0;
		UPDATE codex_credential_groups SET disable_long_context_pricing = 0;
	`); err != nil {
		t.Fatalf("simulate explicit enable: %v", err)
	}
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("idempotent InitSchema: %v", err)
	}

	for _, table := range []string{"claude_credential_groups", "codex_credential_groups"} {
		var disabled int
		if err := adapter.GetReadDB().QueryRowContext(ctx, "SELECT disable_long_context_pricing FROM "+table+" LIMIT 1").Scan(&disabled); err != nil {
			t.Fatalf("query preserved %s state: %v", table, err)
		}
		if disabled != 0 {
			t.Fatalf("expected repeated InitSchema to preserve explicit %s state, got %d", table, disabled)
		}
	}
}

func TestSQLiteAdapter_InitSchema_ScopesLegacySettingsByProvider(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "settings-provider.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			category TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			value_type TEXT NOT NULL DEFAULT 'string',
			label TEXT,
			description TEXT,
			display_order INTEGER NOT NULL DEFAULT 0,
			requires_restart INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(category, key)
		);
		INSERT INTO settings (category, key, value) VALUES
			('failover', 'enabled', 'true'),
			('health', 'timeout', '5s');
	`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	if err := adapter.InitSchema(); err != nil {
		t.Fatal(err)
	}

	assertSetting := func(provider, category, key, want string) {
		t.Helper()
		var got string
		if err := adapter.GetReadDB().QueryRow(
			`SELECT value FROM settings WHERE provider = ? AND category = ? AND key = ?`,
			provider, category, key,
		).Scan(&got); err != nil {
			t.Fatalf("query %s/%s/%s: %v", provider, category, key, err)
		}
		if got != want {
			t.Fatalf("%s/%s/%s = %q, want %q", provider, category, key, got, want)
		}
	}
	assertSetting("claude", "failover", "enabled", "true")
	assertSetting("global", "health", "timeout", "5s")

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("idempotent InitSchema: %v", err)
	}
}

func TestSQLiteAdapter_InitSchema_NormalizesPausedCodexChannelPriorities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex-channel-priority.db")
	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	if err := adapter.InitSchema(); err != nil {
		t.Fatal(err)
	}

	if _, err := adapter.GetWriteDB().Exec(`
		INSERT INTO codex_channels (name, provider_type, api_key, model, model_reasoning_effort, priority, failover_enabled)
		VALUES
			('active', 'openai-compatible', '', '', 'xhigh', 0, 1),
			('paused', 'openai-compatible', '', '', 'xhigh', 1, 0),
			('standby', 'openai-compatible', '', '', 'xhigh', 1, 1)
	`); err != nil {
		t.Fatal(err)
	}
	if err := adapter.InitSchema(); err != nil {
		t.Fatal(err)
	}

	rows, err := adapter.GetReadDB().Query(`SELECT priority FROM codex_channels ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var priorities []int
	for rows.Next() {
		var priority int
		if err := rows.Scan(&priority); err != nil {
			t.Fatal(err)
		}
		priorities = append(priorities, priority)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(priorities) != 3 || priorities[0] != 0 || priorities[1] != 1 || priorities[2] != 2 {
		t.Fatalf("expected priorities [0 1 2] across participating and paused channels, got %v", priorities)
	}
}

func sqliteColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()

	var (
		cid     int
		name    string
		typ     string
		notnull int
		dflt    any
		pk      int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma rows: %v", err)
	}
	return false
}
