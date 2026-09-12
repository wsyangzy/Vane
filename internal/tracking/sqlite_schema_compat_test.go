package tracking

import (
	"context"
	"database/sql"
	"math"
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
	for _, c := range []string{"endpoint_chain", "cache_creation_5m_tokens", "cache_creation_1h_tokens", "cache_creation_5m_cost_usd", "cache_creation_1h_cost_usd", "billing_rule_snapshot"} {
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
		"daily_cost_limit_usd",
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
	if !sqliteColumnExists(t, adapter.GetReadDB(), "codex_credential_groups", "daily_cost_limit_usd") {
		t.Fatalf("expected codex_credential_groups.daily_cost_limit_usd to exist after InitSchema")
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

func TestEmbeddedSchemaCredentialGroupServiceTierColumns(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema, err := sqliteSchemaFS.ReadFile("schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("execute schema: %v", err)
	}

	for _, column := range []string{
		"disable_service_tier_pricing",
		"service_tier_multipliers",
		"billing_rules",
	} {
		if !sqliteColumnExists(t, db, "codex_credential_groups", column) {
			t.Fatalf("expected codex_credential_groups.%s in embedded schema", column)
		}
	}
	for _, column := range []string{
		"disable_service_tier_pricing",
		"service_tier_cost_multiplier",
		"service_tier_input_cost_multiplier",
		"service_tier_output_cost_multiplier",
		"service_tier_cache_read_cost_multiplier",
	} {
		if !sqliteColumnExists(t, db, "claude_credential_groups", column) {
			t.Fatalf("expected claude_credential_groups.%s in embedded schema", column)
		}
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

// TestSQLiteAdapter_InitSchema_RemovesClaudeLongContextPricing 验证 Claude 侧移除 Long context 后，
// 历史上被显式开启的分组会被强制关闭，且不得波及仍保留该能力的 Codex 分组。
func TestSQLiteAdapter_InitSchema_RemovesClaudeLongContextPricing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "claude-long-context-removal.db")
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

	// 模拟用户在 20260712 迁移之后手动开启了 Long context 的旧库。
	// 只删除本次迁移的版本记录，保证 20260712 不会重跑而把 Codex 也一并关掉，
	// 这样才能验证新迁移确实只作用于 Claude。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claude_credential_groups (channel, name, disable_long_context_pricing)
		VALUES ('legacy-claude', 'default', 0);
		INSERT INTO codex_channels (name, provider_type, api_key, model)
		VALUES ('legacy-codex', 'openai-compatible', '', 'gpt-5');
		INSERT INTO codex_credential_groups (channel_id, name, disable_long_context_pricing)
		SELECT id, 'default', 0 FROM codex_channels WHERE name = 'legacy-codex';
		DELETE FROM schema_versions WHERE version = '20260821_remove_claude_long_context_pricing';
	`); err != nil {
		t.Fatalf("seed credential groups: %v", err)
	}

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("migrating InitSchema: %v", err)
	}

	readDisabled := func(table string) int {
		t.Helper()
		var disabled int
		if err := adapter.GetReadDB().QueryRowContext(ctx,
			"SELECT disable_long_context_pricing FROM "+table+" LIMIT 1").Scan(&disabled); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		return disabled
	}

	if got := readDisabled("claude_credential_groups"); got != 1 {
		t.Fatalf("expected Claude group Long context forced off, got %d", got)
	}
	if got := readDisabled("codex_credential_groups"); got != 0 {
		t.Fatalf("Codex group must not be touched by the Claude-only migration, got %d", got)
	}

	var migrationCount int
	if err := adapter.GetReadDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM schema_versions
		WHERE version = '20260821_remove_claude_long_context_pricing'
	`).Scan(&migrationCount); err != nil {
		t.Fatalf("query migration version: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected exactly one migration record, got %d", migrationCount)
	}

	// 迁移是一次性的：重复 InitSchema 不应再次改写数据。
	if _, err := db.ExecContext(ctx,
		"UPDATE claude_credential_groups SET disable_long_context_pricing = 0"); err != nil {
		t.Fatalf("reset claude group: %v", err)
	}
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("idempotent InitSchema: %v", err)
	}
	if got := readDisabled("claude_credential_groups"); got != 0 {
		t.Fatalf("expected one-shot migration not to rerun, got %d", got)
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

func TestSQLiteAdapter_InitSchema_RepairsCrossProviderMultiplierCostOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cross-provider-multiplier.db")
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	// anyrouter: 两侧同名渠道/分组，Claude 配 1.0 而 Codex 配 0.4，旧的无 provider
	// key 让 Codex 段覆盖 Claude 段，Claude 请求落库成 0.4 —— 这是要修的污染。
	//
	// Right Code: Claude 侧分组倍率 0.3，但记录落库 1.0（分组建立前发出的请求）。
	// Codex 侧同名渠道叫 RightCode（无空格）匹配不上，因此不得被追溯改写。
	if _, err := adapter.GetWriteDB().ExecContext(ctx, `
		INSERT INTO claude_credential_groups (channel, name, is_default, cost_multiplier)
		VALUES ('anyrouter', '默认分组', 1, 1.0),
			   ('Right Code', 'aws', 1, 0.3);

		INSERT INTO codex_channels (name, provider_type, api_key, model)
		VALUES ('anyrouter', 'openai-compatible', 'k', 'gpt-5.6-sol'),
			   ('RightCode', 'openai-compatible', 'k', 'gpt-5.6-sol');

		INSERT INTO codex_credential_groups (channel_id, name, cost_multiplier)
		SELECT id, '默认分组', 0.4 FROM codex_channels WHERE name = 'anyrouter';

		INSERT INTO request_logs (
			request_id, start_time, provider, channel, group_name, endpoint_name,
			cost_multiplier, input_cost_usd, output_cost_usd, cache_read_cost_usd,
			cache_creation_cost_usd, cache_creation_5m_cost_usd, cache_creation_1h_cost_usd, total_cost_usd
		) VALUES
			('req-polluted', '2026-09-01 10:00:00', 'claude', 'anyrouter', '默认分组', '主站直连',
				0.4, 0.4, 0.8, 0.2, 0.1, 0.1, 0.0, 1.5),
			('req-clean', '2026-09-01 10:05:00', 'claude', 'anyrouter', '默认分组', '主站直连',
				1.0, 1.0, 2.0, 0.5, 0.0, 0.0, 0.0, 3.5),
			('req-codex', '2026-09-01 10:10:00', 'codex', 'anyrouter', '默认分组', '主站直连',
				0.4, 0.4, 0.8, 0.0, 0.0, 0.0, 0.0, 1.2),
			('req-preconfig', '2026-09-02 16:31:42', 'claude', 'Right Code', 'aws', 'AWSQ逆向渠道',
				1.0, 1.0, 2.0, 0.0, 0.0, 0.0, 0.0, 3.0);

		DELETE FROM schema_versions WHERE version = '20260902_repair_cross_provider_multiplier_cost';
	`); err != nil {
		t.Fatalf("seed polluted request logs: %v", err)
	}

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("repairing InitSchema: %v", err)
	}

	type costRow struct {
		multiplier float64
		input      float64
		output     float64
		cacheRead  float64
		total      float64
	}
	readCost := func(requestID string) costRow {
		t.Helper()
		var row costRow
		if err := adapter.GetReadDB().QueryRowContext(ctx, `
			SELECT cost_multiplier, input_cost_usd, output_cost_usd, cache_read_cost_usd, total_cost_usd
			FROM request_logs WHERE request_id = ?
		`, requestID).Scan(&row.multiplier, &row.input, &row.output, &row.cacheRead, &row.total); err != nil {
			t.Fatalf("query %s: %v", requestID, err)
		}
		return row
	}
	assertClose := func(label string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("%s = %.6f, want %.6f", label, got, want)
		}
	}

	// 0.4 → 1.0，比例 2.5 应用到每个成本列。
	repaired := readCost("req-polluted")
	assertClose("polluted multiplier", repaired.multiplier, 1.0)
	assertClose("polluted input cost", repaired.input, 1.0)
	assertClose("polluted output cost", repaired.output, 2.0)
	assertClose("polluted cache read cost", repaired.cacheRead, 0.5)
	assertClose("polluted total cost", repaired.total, 3.75)

	// 倍率本就与配置一致的记录不得被触碰。
	clean := readCost("req-clean")
	assertClose("clean multiplier", clean.multiplier, 1.0)
	assertClose("clean total cost", clean.total, 3.5)

	// Codex 记录归 Codex 配置管辖，本修复只针对 Claude。
	codex := readCost("req-codex")
	assertClose("codex multiplier", codex.multiplier, 0.4)
	assertClose("codex total cost", codex.total, 1.2)

	// 分组建立前发出的请求：与当前配置不同是正常的，Codex 侧无同名渠道，不得改写。
	preconfig := readCost("req-preconfig")
	assertClose("pre-config multiplier", preconfig.multiplier, 1.0)
	assertClose("pre-config total cost", preconfig.total, 3.0)

	// 幂等：再次 InitSchema 不得二次放大。
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("idempotent InitSchema: %v", err)
	}
	again := readCost("req-polluted")
	assertClose("multiplier after rerun", again.multiplier, 1.0)
	assertClose("total cost after rerun", again.total, 3.75)

	var migrationCount int
	if err := adapter.GetReadDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM schema_versions
		WHERE version = '20260902_repair_cross_provider_multiplier_cost'
	`).Scan(&migrationCount); err != nil {
		t.Fatalf("query migration version: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one cross-provider multiplier repair record, got %d", migrationCount)
	}
}

// 老库的 model_pricing 是 model_name 单列唯一，这既禁止两个 provider 配置同名模型的
// 不同定价，也无法为一个模型配置多个服务档位的定价行。
func TestSQLiteAdapter_InitSchema_RebuildsModelPricingUniqueToProviderTier(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "model-pricing-uniq.db")
	dsn := dbPath + "?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=60000"

	legacy, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 刻意建成老结构：model_name 单列唯一、且没有 service_tier 列
	if _, err := legacy.Exec(`
		CREATE TABLE model_pricing (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_name TEXT UNIQUE NOT NULL,
			provider TEXT NOT NULL DEFAULT 'claude',
			input_price REAL NOT NULL DEFAULT 3.0,
			output_price REAL NOT NULL DEFAULT 15.0,
			cache_creation_price_5m REAL DEFAULT 3.75,
			cache_creation_price_1h REAL DEFAULT 6.0,
			cache_read_price REAL DEFAULT 0.30,
			long_context_threshold INTEGER NOT NULL DEFAULT 0,
			long_context_input_price REAL NOT NULL DEFAULT 0,
			long_context_cache_read_price REAL NOT NULL DEFAULT 0,
			long_context_cache_write_price REAL NOT NULL DEFAULT 0,
			long_context_output_price REAL NOT NULL DEFAULT 0,
			display_name TEXT,
			description TEXT,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		);
		INSERT INTO model_pricing (model_name, provider, input_price, output_price, long_context_threshold, long_context_input_price)
		VALUES ('MiniMax-M2', 'claude', 3.0, 15.0, 272000, 6.0);
	`); err != nil {
		t.Fatalf("seed legacy model_pricing: %v", err)
	}
	_ = legacy.Close()

	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter: %v", err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatalf("adapter.Open: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	// 存量行必须完整保留，并落到标准档（service_tier=''）
	var provider, serviceTier string
	var inputPrice, longContextInput float64
	var threshold int64
	if err := adapter.GetReadDB().QueryRowContext(ctx, `
		SELECT provider, service_tier, input_price, long_context_threshold, long_context_input_price
		FROM model_pricing WHERE model_name = 'MiniMax-M2'
	`).Scan(&provider, &serviceTier, &inputPrice, &threshold, &longContextInput); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if provider != "claude" || serviceTier != "" || inputPrice != 3.0 || threshold != 272000 || longContextInput != 6.0 {
		t.Fatalf("migrated row = provider %q tier %q input %.2f threshold %d lc_input %.2f",
			provider, serviceTier, inputPrice, threshold, longContextInput)
	}

	// 同名模型在另一个 provider 下必须能独立配置定价
	if _, err := adapter.GetWriteDB().ExecContext(ctx, `
		INSERT INTO model_pricing (model_name, provider, service_tier, input_price, output_price)
		VALUES ('MiniMax-M2', 'codex', '', 7.0, 21.0)
	`); err != nil {
		t.Fatalf("same model under another provider must be insertable: %v", err)
	}

	// 同一模型可以有多个档位行
	if _, err := adapter.GetWriteDB().ExecContext(ctx, `
		INSERT INTO model_pricing (model_name, provider, service_tier, input_price, output_price)
		VALUES ('MiniMax-M2', 'codex', 'flex', 3.5, 10.5)
	`); err != nil {
		t.Fatalf("tier row must be insertable: %v", err)
	}

	// 三元组本身仍然唯一
	if _, err := adapter.GetWriteDB().ExecContext(ctx, `
		INSERT INTO model_pricing (model_name, provider, service_tier, input_price, output_price)
		VALUES ('MiniMax-M2', 'codex', 'flex', 9.9, 9.9)
	`); err == nil {
		t.Fatal("duplicate (provider, model_name, service_tier) must be rejected")
	}

	// 幂等：再次 InitSchema 不应重复重建或丢数据
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("idempotent InitSchema: %v", err)
	}
	var rowCount int
	if err := adapter.GetReadDB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM model_pricing WHERE model_name = 'MiniMax-M2'").Scan(&rowCount); err != nil {
		t.Fatalf("count rows after rerun: %v", err)
	}
	if rowCount != 3 {
		t.Fatalf("row count after rerun = %d, want 3", rowCount)
	}
}
