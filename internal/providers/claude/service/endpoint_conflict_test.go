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
	"ai-relay-forwarder/internal/providers/claude/store"

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
	priority INTEGER DEFAULT 0,
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
	long_context_cost_multiplier REAL DEFAULT 1.0,
	long_context_input_cost_multiplier REAL DEFAULT 1.0,
	long_context_output_cost_multiplier REAL DEFAULT 1.0,
	long_context_cache_creation_cost_multiplier REAL DEFAULT 1.0,
	long_context_cache_read_cost_multiplier REAL DEFAULT 1.0,
	disable_long_context_pricing INTEGER DEFAULT 1,
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

func TestEndpointService_UpdateEndpoint_SwapsPriorityConflict(t *testing.T) {
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
	svc := NewEndpointService(storeImpl, manager, cfg)
	ctx := context.Background()

	first, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "first",
		URL:             "https://first.example.invalid",
		FailoverEnabled: true,
		TimeoutSeconds:  1,
		Priority:        1,
	})
	if err != nil {
		t.Fatalf("创建第一个端点失败: %v", err)
	}
	second, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "second",
		URL:             "https://second.example.invalid",
		FailoverEnabled: true,
		TimeoutSeconds:  1,
		Priority:        5,
	})
	if err != nil {
		t.Fatalf("创建第二个端点失败: %v", err)
	}

	second.Priority = first.Priority
	if err := svc.UpdateEndpoint(ctx, second); err != nil {
		t.Fatalf("更新第二个端点优先级失败: %v", err)
	}

	updatedFirst, err := storeImpl.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("重新读取第一个端点失败: %v", err)
	}
	updatedSecond, err := storeImpl.GetByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("重新读取第二个端点失败: %v", err)
	}
	if updatedFirst.Priority != 5 || updatedSecond.Priority != 1 {
		t.Fatalf("预期优先级冲突时交换，got first=%d second=%d", updatedFirst.Priority, updatedSecond.Priority)
	}
}

func TestEndpointService_UpdateEndpoint_ChannelMoveAssignsNextPriority(t *testing.T) {
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
	svc := NewEndpointService(storeImpl, manager, cfg)
	ctx := context.Background()

	if _, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "existing",
		URL:             "https://existing.example.invalid",
		FailoverEnabled: true,
		TimeoutSeconds:  1,
		Priority:        1,
	}); err != nil {
		t.Fatalf("创建目标渠道已有端点失败: %v", err)
	}
	moving, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-b",
		Name:            "moving",
		URL:             "https://moving.example.invalid",
		FailoverEnabled: true,
		TimeoutSeconds:  1,
		Priority:        1,
	})
	if err != nil {
		t.Fatalf("创建待移动端点失败: %v", err)
	}

	moving.Channel = "ch-a"
	if err := svc.UpdateEndpoint(ctx, moving); err != nil {
		t.Fatalf("移动端点失败: %v", err)
	}

	updatedMoving, err := storeImpl.GetByID(ctx, moving.ID)
	if err != nil {
		t.Fatalf("重新读取移动端点失败: %v", err)
	}
	if updatedMoving.Priority != 2 {
		t.Fatalf("预期跨渠道移动自动排到目标渠道末尾，got %d", updatedMoving.Priority)
	}
}

func TestEndpointService_CreateEndpointPriorityIsScopedToCredentialGroup(t *testing.T) {
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

	defaultGroup, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel:   "ch-a",
		Name:      "default",
		Token:     "token-a",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("创建默认分组失败: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel: "ch-a",
		Name:    "github",
		Token:   "token-b",
	})
	if err != nil {
		t.Fatalf("创建其他分组失败: %v", err)
	}

	defaultEndpoint, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:           "ch-a",
		CredentialGroupID: defaultGroup.ID,
		Name:              "default-first",
		URL:               "https://default-first.example.invalid",
		FailoverEnabled:   true,
		TimeoutSeconds:    1,
		Priority:          -1,
	})
	if err != nil {
		t.Fatalf("创建默认分组端点失败: %v", err)
	}
	otherEndpoint, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:           "ch-a",
		CredentialGroupID: otherGroup.ID,
		Name:              "other-first",
		URL:               "https://other-first.example.invalid",
		FailoverEnabled:   true,
		TimeoutSeconds:    1,
		Priority:          -1,
	})
	if err != nil {
		t.Fatalf("创建其他分组端点失败: %v", err)
	}

	if defaultEndpoint.Priority != 0 || otherEndpoint.Priority != 0 {
		t.Fatalf("预期各凭证分组独立从优先级 0 开始，got default=%d other=%d", defaultEndpoint.Priority, otherEndpoint.Priority)
	}
}

func TestEndpointService_CreateEndpointKeepsExplicitZeroPriority(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()

	cfg := &config.Config{
		Health: config.HealthConfig{
			CheckInterval: 10 * time.Second,
			Timeout:       time.Second,
		},
	}
	storeRepo := store.NewSQLiteEndpointStore(db)
	manager := endpoint.NewManager(cfg)
	svc := NewEndpointService(storeRepo, manager, cfg)

	created, err := svc.CreateEndpoint(context.Background(), &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "first",
		URL:             "https://first.example.invalid",
		FailoverEnabled: true,
		TimeoutSeconds:  1,
		Priority:        0,
	})
	if err != nil {
		t.Fatalf("创建端点失败: %v", err)
	}
	if created.Priority != 0 {
		t.Fatalf("expected explicit priority 0, got %d", created.Priority)
	}
}

func TestEndpointService_CreateEndpointMovesExplicitPriorityConflictToNextPriority(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()

	cfg := &config.Config{
		Health: config.HealthConfig{
			CheckInterval: 10 * time.Second,
			Timeout:       time.Second,
		},
	}
	storeRepo := store.NewSQLiteEndpointStore(db)
	manager := endpoint.NewManager(cfg)
	svc := NewEndpointService(storeRepo, manager, cfg)
	ctx := context.Background()

	first, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "first",
		URL:             "https://first.example.invalid",
		FailoverEnabled: true,
		TimeoutSeconds:  1,
		Priority:        0,
	})
	if err != nil {
		t.Fatalf("创建第一个端点失败: %v", err)
	}
	second, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "second",
		URL:             "https://second.example.invalid",
		FailoverEnabled: true,
		TimeoutSeconds:  1,
		Priority:        0,
	})
	if err != nil {
		t.Fatalf("创建第二个端点失败: %v", err)
	}

	updatedFirst, err := storeRepo.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("重新读取第一个端点失败: %v", err)
	}
	if second.Priority != 0 || updatedFirst.Priority != 1 {
		t.Fatalf("预期新端点占用 P0 且旧端点移动到 P1，got second=%d first=%d", second.Priority, updatedFirst.Priority)
	}
}

func TestEndpointService_UpdateEndpointPriorityConflictIsScopedToCredentialGroup(t *testing.T) {
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

	defaultGroup, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel:   "ch-a",
		Name:      "default",
		Token:     "token-a",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("创建默认分组失败: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel: "ch-a",
		Name:    "github",
		Token:   "token-b",
	})
	if err != nil {
		t.Fatalf("创建其他分组失败: %v", err)
	}

	defaultEndpoint, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:           "ch-a",
		CredentialGroupID: defaultGroup.ID,
		Name:              "default-first",
		URL:               "https://default-first.example.invalid",
		FailoverEnabled:   true,
		TimeoutSeconds:    1,
		Priority:          1,
	})
	if err != nil {
		t.Fatalf("创建默认分组端点失败: %v", err)
	}
	otherEndpoint, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:           "ch-a",
		CredentialGroupID: otherGroup.ID,
		Name:              "other-first",
		URL:               "https://other-first.example.invalid",
		FailoverEnabled:   true,
		TimeoutSeconds:    1,
		Priority:          5,
	})
	if err != nil {
		t.Fatalf("创建其他分组端点失败: %v", err)
	}

	otherEndpoint.Priority = defaultEndpoint.Priority
	if err := svc.UpdateEndpoint(ctx, otherEndpoint); err != nil {
		t.Fatalf("更新其他分组端点优先级失败: %v", err)
	}

	updatedDefault, err := storeImpl.GetByID(ctx, defaultEndpoint.ID)
	if err != nil {
		t.Fatalf("重新读取默认分组端点失败: %v", err)
	}
	updatedOther, err := storeImpl.GetByID(ctx, otherEndpoint.ID)
	if err != nil {
		t.Fatalf("重新读取其他分组端点失败: %v", err)
	}
	if updatedDefault.Priority != 1 || updatedOther.Priority != 1 {
		t.Fatalf("预期不同分组相同优先级不交换，got default=%d other=%d", updatedDefault.Priority, updatedOther.Priority)
	}
}

func TestEndpointService_UpdateEndpointCredentialGroupMoveAssignsNextPriority(t *testing.T) {
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

	defaultGroup, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel:   "ch-a",
		Name:      "default",
		Token:     "token-a",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("创建默认分组失败: %v", err)
	}
	otherGroup, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel: "ch-a",
		Name:    "github",
		Token:   "token-b",
	})
	if err != nil {
		t.Fatalf("创建其他分组失败: %v", err)
	}

	if _, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:           "ch-a",
		CredentialGroupID: defaultGroup.ID,
		Name:              "target-existing",
		URL:               "https://target-existing.example.invalid",
		FailoverEnabled:   true,
		TimeoutSeconds:    1,
		Priority:          1,
	}); err != nil {
		t.Fatalf("创建目标分组已有端点失败: %v", err)
	}
	moving, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:           "ch-a",
		CredentialGroupID: otherGroup.ID,
		Name:              "moving",
		URL:               "https://moving.example.invalid",
		FailoverEnabled:   true,
		TimeoutSeconds:    1,
		Priority:          1,
	})
	if err != nil {
		t.Fatalf("创建待移动端点失败: %v", err)
	}

	moving.CredentialGroupID = defaultGroup.ID
	if err := svc.UpdateEndpoint(ctx, moving); err != nil {
		t.Fatalf("移动端点到默认分组失败: %v", err)
	}

	updatedMoving, err := storeImpl.GetByID(ctx, moving.ID)
	if err != nil {
		t.Fatalf("重新读取移动端点失败: %v", err)
	}
	if updatedMoving.Priority != 2 {
		t.Fatalf("预期移动端点排到目标分组末尾，got %d", updatedMoving.Priority)
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

func TestEndpointService_UpdateEndpointDisablingActiveFailoverSwitchesToNextCandidate(t *testing.T) {
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
	svc := NewEndpointService(storeImpl, manager, cfg)

	ctx := context.Background()
	record, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "active",
		URL:             "https://example.invalid",
		FailoverEnabled: true,
		Enabled:         true,
		TimeoutSeconds:  1,
		Priority:        2,
	})
	if err != nil {
		t.Fatalf("创建端点失败: %v", err)
	}
	standby, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "standby",
		URL:             "https://standby.example.invalid",
		FailoverEnabled: true,
		Enabled:         false,
		TimeoutSeconds:  1,
		Priority:        1,
	})
	if err != nil {
		t.Fatalf("创建备用端点失败: %v", err)
	}
	laterStandby, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "later-standby",
		URL:             "https://later-standby.example.invalid",
		FailoverEnabled: true,
		Enabled:         false,
		TimeoutSeconds:  1,
		Priority:        3,
	})
	if err != nil {
		t.Fatalf("创建后添加备用端点失败: %v", err)
	}

	record.FailoverEnabled = false
	if err := svc.UpdateEndpoint(ctx, record); err != nil {
		t.Fatalf("关闭当前路由端点故障转移失败: %v", err)
	}

	updatedActive, err := storeImpl.GetByID(ctx, record.ID)
	if err != nil {
		t.Fatalf("重新读取当前端点失败: %v", err)
	}
	updatedStandby, err := storeImpl.GetByID(ctx, standby.ID)
	if err != nil {
		t.Fatalf("重新读取备用端点失败: %v", err)
	}
	if updatedActive.FailoverEnabled || updatedActive.Enabled {
		t.Fatalf("预期原当前端点被关闭参与并取消当前状态，got enabled=%v failover=%v", updatedActive.Enabled, updatedActive.FailoverEnabled)
	}
	if !updatedStandby.Enabled {
		t.Fatalf("预期备用端点成为当前路由端点")
	}
	updatedLaterStandby, err := storeImpl.GetByID(ctx, laterStandby.ID)
	if err != nil {
		t.Fatalf("重新读取后添加备用端点失败: %v", err)
	}
	if updatedLaterStandby.Enabled {
		t.Fatalf("预期更低优先级的备用端点不被选中")
	}
}

func TestEndpointService_UpdateEndpointDisablingActiveFailoverRequiresCandidate(t *testing.T) {
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
	svc := NewEndpointService(storeImpl, manager, cfg)

	ctx := context.Background()
	record, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            "active",
		URL:             "https://example.invalid",
		FailoverEnabled: true,
		Enabled:         true,
		TimeoutSeconds:  1,
		Priority:        1,
	})
	if err != nil {
		t.Fatalf("创建端点失败: %v", err)
	}

	record.FailoverEnabled = false
	err = svc.UpdateEndpoint(ctx, record)
	if err == nil || !strings.Contains(err.Error(), "没有其他参与渠道内故障转移的端点") {
		t.Fatalf("预期缺少备用端点时报错，got: %v", err)
	}

	updatedActive, err := storeImpl.GetByID(ctx, record.ID)
	if err != nil {
		t.Fatalf("重新读取当前端点失败: %v", err)
	}
	if !updatedActive.FailoverEnabled || !updatedActive.Enabled {
		t.Fatalf("预期当前端点保持活跃并参与转移，got enabled=%v failover=%v", updatedActive.Enabled, updatedActive.FailoverEnabled)
	}
}
