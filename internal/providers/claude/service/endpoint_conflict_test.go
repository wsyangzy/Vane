package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
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
	daily_cost_limit_usd REAL NOT NULL DEFAULT 0,
	model_policy TEXT NOT NULL DEFAULT 'inherit',
	model_mapping TEXT NOT NULL DEFAULT '{}',
	supported_models TEXT NOT NULL DEFAULT '[]',
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
	disable_service_tier_pricing INTEGER DEFAULT 1,
	service_tier_cost_multiplier REAL DEFAULT 1.0,
	service_tier_input_cost_multiplier REAL DEFAULT 1.0,
	service_tier_output_cost_multiplier REAL DEFAULT 1.0,
	service_tier_cache_read_cost_multiplier REAL DEFAULT 1.0,
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

func TestEndpointService_FailoverActivationPreservesNewerManualChannel(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()

	cfg := &config.Config{
		Health: config.HealthConfig{
			Timeout: 20 * time.Millisecond,
		},
		Failover: config.FailoverConfig{Enabled: true},
	}
	manager := endpoint.NewManager(cfg)
	storeImpl := store.NewSQLiteEndpointStore(db)
	svc := NewEndpointService(storeImpl, manager, cfg)
	ctx := context.Background()

	createdByChannel := make(map[string]*store.EndpointRecord)
	for i, channel := range []string{"A", "B", "C"} {
		created, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
			Channel:         channel,
			Name:            strings.ToLower(channel) + "1",
			URL:             "http://127.0.0.1:" + fmt.Sprint(61000+i),
			Priority:        0,
			FailoverEnabled: true,
			TimeoutSeconds:  1,
		})
		if err != nil {
			t.Fatalf("create channel %s endpoint: %v", channel, err)
		}
		createdByChannel[channel] = created
	}

	if err := svc.ActivateChannel(ctx, "A"); err != nil {
		t.Fatalf("activate A: %v", err)
	}
	decisionStarted := time.Now()
	_, revisionA := manager.GetGroupManager().ActiveGroupSnapshot()
	_, revisionB, activated, err := manager.GetGroupManager().ActivateGroupForFailover("B", "A", revisionA, false)
	if err != nil {
		t.Fatalf("activate runtime failover B: %v", err)
	} else if !activated {
		t.Fatal("expected runtime failover activation to B")
	}
	applied, err := svc.ActivateChannelForFailover(ctx, "B", "A", revisionB)
	if err != nil {
		t.Fatalf("persist failover B: %v", err)
	}
	if !applied {
		t.Fatal("expected current failover activation to be persisted")
	}
	if manager.GetGroupManager().WasGroupManuallyActivatedSince("B", decisionStarted) {
		t.Fatal("automatic failover must not be recorded as a manual activation")
	}

	if err := svc.ActivateChannel(ctx, "C"); err != nil {
		t.Fatalf("manual activate C: %v", err)
	}
	applied, err = svc.ActivateChannelForFailover(ctx, "A", "B", revisionB)
	if err != nil {
		t.Fatalf("persist stale failover A: %v", err)
	}
	if applied {
		t.Fatal("stale automatic persistence must not replace the newer manual channel")
	}

	enabled, err := storeImpl.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("list enabled endpoints: %v", err)
	}
	if len(enabled) != 1 || enabled[0].Channel != "C" {
		t.Fatalf("enabled endpoints = %#v, want only channel C", enabled)
	}
	activeGroups := manager.GetGroupManager().GetActiveGroups()
	if len(activeGroups) != 1 || activeGroups[0].Name != "C" {
		t.Fatalf("active groups = %#v, want only C", activeGroups)
	}

	for i := 0; i < 10; i++ {
		errCh := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			errCh <- svc.ActivateChannel(ctx, "A")
		}()
		go func() {
			defer wg.Done()
			_, err := svc.ActivateEndpointByID(ctx, createdByChannel["B"].ID)
			errCh <- err
		}()
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				t.Fatalf("concurrent activation: %v", err)
			}
		}

		enabled, err = storeImpl.ListEnabled(ctx)
		if err != nil {
			t.Fatalf("list enabled endpoints after concurrent activation: %v", err)
		}
		if len(enabled) != 1 {
			t.Fatalf("enabled endpoint count = %d, want 1", len(enabled))
		}
		activeGroups = manager.GetGroupManager().GetActiveGroups()
		if len(activeGroups) != 1 || activeGroups[0].Name != enabled[0].Channel {
			t.Fatalf("runtime active = %#v, persisted active = %s", activeGroups, enabled[0].Channel)
		}
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

func TestEndpointService_CreateEndpointRequiresUsableCredentialGroup(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()

	cfg := &config.Config{}
	endpointStore := store.NewSQLiteEndpointStore(db)
	groupStore := store.NewSQLiteClaudeCredentialGroupStore(db)
	svc := NewEndpointService(endpointStore, endpoint.NewManager(cfg), cfg, groupStore)
	ctx := context.Background()

	_, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel: "ch-a", Name: "without-group", URL: "https://without-group.example.invalid", FailoverEnabled: true,
	})
	if err == nil || !strings.Contains(err.Error(), "尚未配置凭证分组") {
		t.Fatalf("expected missing-group error, got %v", err)
	}

	group, err := groupStore.Create(ctx, &store.ClaudeCredentialGroupRecord{
		Channel: "ch-a", Name: "default", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create empty credential group: %v", err)
	}
	_, err = svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel: "ch-a", CredentialGroupID: group.ID, Name: "empty-group", URL: "https://empty-group.example.invalid", FailoverEnabled: true,
	})
	if err == nil || !strings.Contains(err.Error(), "Token 或 API Key") {
		t.Fatalf("expected empty-credential error, got %v", err)
	}
	_, err = svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel: "ch-a", CredentialGroupID: group.ID, Name: "endpoint-override", URL: "https://endpoint-override.example.invalid", Token: "legacy-token", FailoverEnabled: true,
	})
	if err != nil {
		t.Fatalf("endpoint-level credential override should allow an empty group: %v", err)
	}

	count, err := endpointStore.Count(ctx)
	if err != nil {
		t.Fatalf("count endpoints: %v", err)
	}
	if count != 1 {
		t.Fatalf("only the endpoint-level credential override should create a record, count=%d", count)
	}
}

func TestEndpointService_CreateEndpointPriorityConflictRollsBackOnInsertFailure(t *testing.T) {
	db, cleanup := createEndpointServiceTestDB(t)
	defer cleanup()

	if _, err := db.Exec(`CREATE TRIGGER fail_target_endpoint_insert BEFORE INSERT ON endpoints WHEN NEW.name = 'target' BEGIN SELECT RAISE(ABORT, 'forced endpoint insert failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	cfg := &config.Config{}
	endpointStore := store.NewSQLiteEndpointStore(db)
	svc := NewEndpointService(endpointStore, endpoint.NewManager(cfg), cfg)
	ctx := context.Background()

	conflicting, err := svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel: "ch-a", Name: "existing", URL: "https://existing.example.invalid", FailoverEnabled: true, Priority: 0,
	})
	if err != nil {
		t.Fatalf("create existing endpoint: %v", err)
	}
	_, err = svc.CreateEndpoint(ctx, &store.EndpointRecord{
		Channel: "ch-a", Name: "target", URL: "https://target.example.invalid", FailoverEnabled: true, Priority: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "保存端点到数据库失败") {
		t.Fatalf("expected insert failure, got %v", err)
	}
	reloaded, err := endpointStore.GetByID(ctx, conflicting.ID)
	if err != nil {
		t.Fatalf("reload conflicting endpoint: %v", err)
	}
	if reloaded == nil || reloaded.Priority != 0 {
		t.Fatalf("priority move must roll back, got %#v", reloaded)
	}
	count, err := endpointStore.Count(ctx)
	if err != nil {
		t.Fatalf("count endpoints: %v", err)
	}
	if count != 1 {
		t.Fatalf("failed create must not leave target endpoint, count=%d", count)
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

type endpointFailureStore struct {
	store.EndpointStore

	mu                    sync.Mutex
	failUpdateID          int64
	failDelete            bool
	failList              bool
	failListAfterUpdate   bool
	failListAfterActiveID int64
	cancelAfterActiveID   int64
	cancelActive          context.CancelFunc
	activeContexts        []context.Context
	activeContextErrors   []error
}

func (s *endpointFailureStore) UpdateByID(ctx context.Context, record *store.EndpointRecord) error {
	s.mu.Lock()
	fail := record != nil && record.ID == s.failUpdateID
	failListAfterUpdate := s.failListAfterUpdate
	s.mu.Unlock()
	if fail {
		return fmt.Errorf("forced endpoint update failure")
	}
	if err := s.EndpointStore.UpdateByID(ctx, record); err != nil {
		return err
	}
	if failListAfterUpdate {
		s.mu.Lock()
		s.failList = true
		s.mu.Unlock()
	}
	return nil
}

func (s *endpointFailureStore) DeleteByID(ctx context.Context, id int64) error {
	s.mu.Lock()
	fail := s.failDelete
	s.mu.Unlock()
	if fail {
		return fmt.Errorf("forced endpoint delete failure")
	}
	return s.EndpointStore.DeleteByID(ctx, id)
}

func (s *endpointFailureStore) List(ctx context.Context) ([]*store.EndpointRecord, error) {
	s.mu.Lock()
	fail := s.failList
	s.mu.Unlock()
	if fail {
		return nil, fmt.Errorf("forced endpoint list failure")
	}
	return s.EndpointStore.List(ctx)
}

func (s *endpointFailureStore) SetActiveEndpoint(ctx context.Context, id int64) error {
	s.mu.Lock()
	s.activeContexts = append(s.activeContexts, ctx)
	s.activeContextErrors = append(s.activeContextErrors, ctx.Err())
	s.mu.Unlock()
	if err := s.EndpointStore.SetActiveEndpoint(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	if id == s.failListAfterActiveID {
		s.failList = true
	}
	var cancel context.CancelFunc
	if id == s.cancelAfterActiveID {
		cancel = s.cancelActive
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func newEndpointRecoveryTestService(t *testing.T) (*EndpointService, *store.SQLiteEndpointStore, *endpointFailureStore, *endpoint.Manager) {
	t.Helper()
	db, cleanup := createEndpointServiceTestDB(t)
	t.Cleanup(cleanup)

	cfg := &config.Config{
		Health: config.HealthConfig{Timeout: 20 * time.Millisecond},
	}
	manager := endpoint.NewManager(cfg)
	t.Cleanup(manager.Stop)
	baseStore := store.NewSQLiteEndpointStore(db)
	failureStore := &endpointFailureStore{EndpointStore: baseStore}
	return NewEndpointService(failureStore, manager, cfg), baseStore, failureStore, manager
}

func createEndpointForRecoveryTest(t *testing.T, svc *EndpointService, name string, priority int, enabled bool) *store.EndpointRecord {
	t.Helper()
	record, err := svc.CreateEndpoint(context.Background(), &store.EndpointRecord{
		Channel:         "ch-a",
		Name:            name,
		URL:             "https://" + name + ".example.invalid",
		Priority:        priority,
		FailoverEnabled: true,
		SkipHealthCheck: true,
		TimeoutSeconds:  1,
		Enabled:         enabled,
	})
	if err != nil {
		t.Fatalf("创建测试端点 %s 失败: %v", name, err)
	}
	return record
}

func TestEndpointService_UpdateEndpointRestoresActiveEndpointWhenTransactionFails(t *testing.T) {
	svc, baseStore, failureStore, _ := newEndpointRecoveryTestService(t)
	active := createEndpointForRecoveryTest(t, svc, "active-rollback", 1, true)
	standby := createEndpointForRecoveryTest(t, svc, "standby-rollback", 2, false)

	failureStore.mu.Lock()
	failureStore.failUpdateID = active.ID
	failureStore.mu.Unlock()
	active.FailoverEnabled = false
	err := svc.UpdateEndpoint(context.Background(), active)
	if err == nil || !strings.Contains(err.Error(), "forced endpoint update failure") {
		t.Fatalf("预期事务更新失败，got: %v", err)
	}

	enabled, err := baseStore.ListEnabled(context.Background())
	if err != nil {
		t.Fatalf("读取活跃端点失败: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != active.ID {
		t.Fatalf("预期恢复原活跃端点 %d，got: %+v", active.ID, enabled)
	}
	updatedStandby, err := baseStore.GetByID(context.Background(), standby.ID)
	if err != nil {
		t.Fatalf("读取备用端点失败: %v", err)
	}
	if updatedStandby.Enabled {
		t.Fatal("事务失败后备用端点不应保持活跃")
	}

	failureStore.mu.Lock()
	defer failureStore.mu.Unlock()
	if len(failureStore.activeContexts) != 2 {
		t.Fatalf("预期切换和回滚各写入一次 active，got %d", len(failureStore.activeContexts))
	}
	if failureStore.activeContexts[0] == failureStore.activeContexts[1] {
		t.Fatal("回滚必须使用独立 context")
	}
	if failureStore.activeContextErrors[1] != nil {
		t.Fatalf("回滚 context 在写入时已失效: %v", failureStore.activeContextErrors[1])
	}
}

func TestEndpointService_UpdateEndpointReloadsRuntimeAfterIncrementalFailure(t *testing.T) {
	svc, _, _, manager := newEndpointRecoveryTestService(t)
	record := createEndpointForRecoveryTest(t, svc, "reload-success", 1, true)
	key := EndpointRuntimeKey(record)
	if err := manager.RemoveEndpoint(key); err != nil {
		t.Fatalf("移除测试运行时端点失败: %v", err)
	}

	record.TimeoutSeconds = 2
	if err := svc.UpdateEndpoint(context.Background(), record); err != nil {
		t.Fatalf("全量重载可恢复时更新不应失败: %v", err)
	}
	runtimeEndpoint := manager.GetEndpointByNameAny(key)
	if runtimeEndpoint == nil || runtimeEndpoint.Config.Timeout != 2*time.Second {
		t.Fatalf("预期全量重载恢复端点及超时配置，got: %+v", runtimeEndpoint)
	}
}

func TestEndpointService_UpdateEndpointReturnsReloadFailure(t *testing.T) {
	svc, baseStore, failureStore, manager := newEndpointRecoveryTestService(t)
	record := createEndpointForRecoveryTest(t, svc, "reload-failure", 1, true)
	key := EndpointRuntimeKey(record)
	if err := manager.RemoveEndpoint(key); err != nil {
		t.Fatalf("移除测试运行时端点失败: %v", err)
	}

	failureStore.mu.Lock()
	failureStore.failListAfterUpdate = true
	failureStore.mu.Unlock()
	record.TimeoutSeconds = 3
	err := svc.UpdateEndpoint(context.Background(), record)
	if err == nil || !strings.Contains(err.Error(), "从数据库重新同步运行时失败") {
		t.Fatalf("预期全量重载失败向上传播，got: %v", err)
	}
	failureStore.mu.Lock()
	failureStore.failList = false
	failureStore.mu.Unlock()

	persisted, readErr := baseStore.GetByID(context.Background(), record.ID)
	if readErr != nil {
		t.Fatalf("读取已提交端点失败: %v", readErr)
	}
	if persisted.TimeoutSeconds != 3 {
		t.Fatalf("预期数据库更新已提交，got timeout=%d", persisted.TimeoutSeconds)
	}
}

func TestEndpointService_ActivateEndpointReloadsRuntimeAfterIncrementalFailure(t *testing.T) {
	svc, baseStore, _, manager := newEndpointRecoveryTestService(t)
	active := createEndpointForRecoveryTest(t, svc, "activate-current", 1, true)
	target := createEndpointForRecoveryTest(t, svc, "activate-target", 2, false)
	if err := manager.RemoveEndpoint(EndpointRuntimeKey(target)); err != nil {
		t.Fatalf("移除目标运行时端点失败: %v", err)
	}

	activated, err := svc.ActivateEndpointByID(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("全量重载可恢复时激活不应失败: %v", err)
	}
	if activated == nil || activated.ID != target.ID {
		t.Fatalf("预期激活目标端点 %d，got: %+v", target.ID, activated)
	}
	if manager.GetEndpointByNameAny(EndpointRuntimeKey(target)) == nil {
		t.Fatal("预期全量重载恢复目标运行时端点")
	}
	enabled, err := baseStore.ListEnabled(context.Background())
	if err != nil {
		t.Fatalf("读取活跃端点失败: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != target.ID {
		t.Fatalf("预期数据库只激活目标端点 %d，got: %+v", target.ID, enabled)
	}
	persistedActive, err := baseStore.GetByID(context.Background(), active.ID)
	if err != nil || persistedActive.Enabled {
		t.Fatalf("原端点不应继续激活: record=%+v err=%v", persistedActive, err)
	}
}

func TestEndpointService_ActivateEndpointRestoresWithIndependentContextWhenReloadFails(t *testing.T) {
	svc, baseStore, failureStore, manager := newEndpointRecoveryTestService(t)
	active := createEndpointForRecoveryTest(t, svc, "activate-rollback-current", 1, true)
	target := createEndpointForRecoveryTest(t, svc, "activate-rollback-target", 2, false)
	if err := manager.RemoveEndpoint(EndpointRuntimeKey(target)); err != nil {
		t.Fatalf("移除目标运行时端点失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	failureStore.mu.Lock()
	failureStore.failListAfterActiveID = target.ID
	failureStore.cancelAfterActiveID = target.ID
	failureStore.cancelActive = cancel
	failureStore.mu.Unlock()
	_, err := svc.ActivateEndpointByID(ctx, target.ID)
	if err == nil || !strings.Contains(err.Error(), "从数据库重新同步运行时失败") {
		t.Fatalf("预期全量重载失败向上传播，got: %v", err)
	}

	enabled, readErr := baseStore.ListEnabled(context.Background())
	if readErr != nil {
		t.Fatalf("读取补偿后的活跃端点失败: %v", readErr)
	}
	if len(enabled) != 1 || enabled[0].ID != active.ID {
		t.Fatalf("预期恢复原活跃端点 %d，got: %+v", active.ID, enabled)
	}
	failureStore.mu.Lock()
	defer failureStore.mu.Unlock()
	if len(failureStore.activeContexts) != 2 {
		t.Fatalf("预期激活和补偿各写入一次 active，got %d", len(failureStore.activeContexts))
	}
	if failureStore.activeContexts[0] == failureStore.activeContexts[1] {
		t.Fatal("激活补偿必须使用独立 context")
	}
	if failureStore.activeContextErrors[1] != nil {
		t.Fatalf("补偿 context 在写入时已失效: %v", failureStore.activeContextErrors[1])
	}
}

func TestEndpointService_DeleteActiveEndpointSelectsEligibleSuccessor(t *testing.T) {
	svc, baseStore, _, manager := newEndpointRecoveryTestService(t)
	active := createEndpointForRecoveryTest(t, svc, "delete-active", 1, true)
	paused := createEndpointForRecoveryTest(t, svc, "delete-paused", 2, false)
	eligible := createEndpointForRecoveryTest(t, svc, "delete-eligible", 3, false)
	paused.FailoverEnabled = false
	if err := svc.UpdateEndpoint(context.Background(), paused); err != nil {
		t.Fatalf("暂停候选端点失败: %v", err)
	}

	if err := svc.DeleteEndpointByID(context.Background(), active.ID); err != nil {
		t.Fatalf("删除当前路由端点失败: %v", err)
	}
	enabled, err := baseStore.ListEnabled(context.Background())
	if err != nil {
		t.Fatalf("读取后续活跃端点失败: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != eligible.ID {
		t.Fatalf("预期跳过暂停端点并激活 %d，got: %+v", eligible.ID, enabled)
	}
	if manager.GetEndpointByNameAny(EndpointRuntimeKey(active)) != nil {
		t.Fatal("已删除端点不应保留在运行时")
	}
}

func TestEndpointService_DeleteKeepsRuntimeWhenDatabaseDeleteFails(t *testing.T) {
	svc, baseStore, failureStore, manager := newEndpointRecoveryTestService(t)
	record := createEndpointForRecoveryTest(t, svc, "delete-rollback", 1, true)
	_ = createEndpointForRecoveryTest(t, svc, "delete-rollback-standby", 2, false)
	key := EndpointRuntimeKey(record)
	failureStore.mu.Lock()
	failureStore.failDelete = true
	failureStore.mu.Unlock()

	for _, deleteEndpoint := range []struct {
		name string
		call func() error
	}{
		{name: "name", call: func() error { return svc.DeleteEndpoint(context.Background(), record.Name) }},
		{name: "id", call: func() error { return svc.DeleteEndpointByID(context.Background(), record.ID) }},
	} {
		t.Run(deleteEndpoint.name, func(t *testing.T) {
			err := deleteEndpoint.call()
			if err == nil || !strings.Contains(err.Error(), "forced endpoint delete failure") {
				t.Fatalf("预期数据库删除失败，got: %v", err)
			}
			if manager.GetEndpointByNameAny(key) == nil {
				t.Fatal("数据库删除失败时运行时端点必须保留")
			}
			persisted, readErr := baseStore.GetByID(context.Background(), record.ID)
			if readErr != nil || persisted == nil {
				t.Fatalf("数据库删除失败时记录必须保留: record=%+v err=%v", persisted, readErr)
			}
			enabled, readErr := baseStore.ListEnabled(context.Background())
			if readErr != nil || len(enabled) != 1 || enabled[0].ID != record.ID {
				t.Fatalf("数据库删除失败时必须恢复原活跃端点: enabled=%+v err=%v", enabled, readErr)
			}
		})
	}
}
