package service

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/store"

	_ "modernc.org/sqlite"
)

func createEndpointServiceTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "endpoint_service_test_*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("打开数据库失败: %v", err)
	}

	// 故意不加 UNIQUE(channel,name)，让测试验证“服务层校验”能阻止同渠道同名。
	schema := `
CREATE TABLE IF NOT EXISTS endpoints (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
		channel TEXT NOT NULL,
		credential_group_id INTEGER,
		name TEXT NOT NULL,
	url TEXT NOT NULL,
	token TEXT,
	api_key TEXT,
	headers TEXT,
	priority INTEGER DEFAULT 1,
	failover_enabled INTEGER DEFAULT 1,
	cooldown_seconds INTEGER,
	timeout_seconds INTEGER DEFAULT 300,
	skip_health_check INTEGER DEFAULT 0,
	supports_count_tokens INTEGER DEFAULT 0,
	cost_multiplier REAL DEFAULT 1.0,
	input_cost_multiplier REAL DEFAULT 1.0,
	output_cost_multiplier REAL DEFAULT 1.0,
	cache_creation_cost_multiplier REAL DEFAULT 1.0,
	cache_creation_cost_multiplier_1h REAL DEFAULT 1.0,
	cache_read_cost_multiplier REAL DEFAULT 1.0,
	model_multipliers TEXT NOT NULL DEFAULT '[]',
	enabled INTEGER DEFAULT 1,
	created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
	updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);
CREATE TABLE IF NOT EXISTS claude_credential_groups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	channel TEXT NOT NULL,
	name TEXT NOT NULL,
	token TEXT,
	api_key TEXT,
	is_default INTEGER DEFAULT 0,
	cost_multiplier REAL DEFAULT 1.0,
	input_cost_multiplier REAL DEFAULT 1.0,
	output_cost_multiplier REAL DEFAULT 1.0,
	cache_creation_cost_multiplier REAL DEFAULT 1.0,
	cache_creation_cost_multiplier_1h REAL DEFAULT 1.0,
	cache_read_cost_multiplier REAL DEFAULT 1.0,
	model_multipliers TEXT NOT NULL DEFAULT '[]',
	created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
	updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("创建表失败: %v", err)
	}

	return db, func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
}

func TestEndpointService_UpdateEndpoint_ConflictSameChannelSameName(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Health: config.HealthConfig{
			Timeout:    50 * time.Millisecond,
			HealthPath: "",
		},
		EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"},
	}
	manager := endpoint.NewManager(cfg)
	storeImpl := store.NewSQLiteEndpointStore(db)
	svc := NewEndpointService(storeImpl, manager, cfg)

	ctx := context.Background()

	ep1, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "dup",
		URL:             ts.URL,
		FailoverEnabled: true,
		TimeoutSeconds:  1,
	})
	if err != nil {
		t.Fatalf("创建端点1失败: %v", err)
	}

	ep2, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-b",
		Name:            "dup",
		URL:             ts.URL,
		FailoverEnabled: true,
		TimeoutSeconds:  1,
	})
	if err != nil {
		t.Fatalf("创建端点2失败: %v", err)
	}

	// 允许不同渠道同名：将 ep2 移动到 ch-a 后应被阻拦，并提示“同凭证分组同名”原因。
	ep2.Channel = ep1.Channel
	if err := svc.UpdateEndpoint(ctx, ep2); err == nil {
		t.Fatalf("预期同渠道同名更新失败，但实际成功")
	} else if !strings.Contains(err.Error(), "同一凭证分组内端点名称必须唯一") {
		t.Fatalf("错误原因不够明确: %v", err)
	}
}

func TestEndpointService_RecordToConfigUsesDefaultCredentialGroup(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()

	cfg := &config.Config{
		Health: config.HealthConfig{
			Timeout:    50 * time.Millisecond,
			HealthPath: "",
		},
		EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"},
	}
	manager := endpoint.NewManager(cfg)
	storeImpl := store.NewSQLiteEndpointStore(db)
	groupStore := store.NewSQLiteClaudeCredentialGroupStore(db)
	svc := NewEndpointService(storeImpl, manager, cfg, groupStore)

	ctx := context.Background()
	group, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel:   "ch-a",
		Name:      "默认分组",
		Token:     "group-token",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("创建默认分组失败: %v", err)
	}

	record, err := storeImpl.Create(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "ep-a",
		URL:             "https://example.invalid",
		FailoverEnabled: true,
		Enabled:         true,
		TimeoutSeconds:  1,
	})
	if err != nil {
		t.Fatalf("创建端点失败: %v", err)
	}

	runtimeConfig := svc.recordToConfig(record)
	if runtimeConfig.CredentialGroupID != group.ID {
		t.Fatalf("credential group id = %d, want %d", runtimeConfig.CredentialGroupID, group.ID)
	}
	if runtimeConfig.CredentialGroupName != "默认分组" {
		t.Fatalf("credential group name = %q, want 默认分组", runtimeConfig.CredentialGroupName)
	}
	if runtimeConfig.Token != "group-token" {
		t.Fatalf("token = %q, want group-token", runtimeConfig.Token)
	}
}
