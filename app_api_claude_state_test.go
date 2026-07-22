package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"ai-relay-forwarder/config"
	claudeendpoint "ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	claudeservice "ai-relay-forwarder/internal/providers/claude/service"
	claudestore "ai-relay-forwarder/internal/providers/claude/store"
)

type claudeStateTestChannelStore struct {
	mu                  sync.Mutex
	records             map[string]*claudestore.ChannelRecord
	listErr             error
	updateContexts      []context.Context
	updateContextErrors []error
}

func (s *claudeStateTestChannelStore) Create(_ context.Context, record *claudestore.ChannelRecord) (*claudestore.ChannelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *record
	s.records[record.Name] = &copy
	return &copy, nil
}

func (s *claudeStateTestChannelStore) Get(_ context.Context, name string) (*claudestore.ChannelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.records[name]
	if record == nil {
		return nil, nil
	}
	copy := *record
	return &copy, nil
}

func (s *claudeStateTestChannelStore) GetByPriority(_ context.Context, priority int) (*claudestore.ChannelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.Priority == priority {
			copy := *record
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *claudeStateTestChannelStore) GetMaxPriority(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	maxPriority := 0
	for _, record := range s.records {
		if record.Priority > maxPriority {
			maxPriority = record.Priority
		}
	}
	return maxPriority, nil
}

func (s *claudeStateTestChannelStore) List(context.Context) ([]*claudestore.ChannelRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	records := make([]*claudestore.ChannelRecord, 0, len(s.records))
	for _, record := range s.records {
		copy := *record
		records = append(records, &copy)
	}
	return records, nil
}

func (s *claudeStateTestChannelStore) Update(ctx context.Context, record *claudestore.ChannelRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *record
	s.records[record.Name] = &copy
	s.updateContexts = append(s.updateContexts, ctx)
	s.updateContextErrors = append(s.updateContextErrors, ctx.Err())
	return nil
}

func (s *claudeStateTestChannelStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, name)
	return nil
}

func (s *claudeStateTestChannelStore) WithTx(*sql.Tx) claudestore.ChannelStore {
	return s
}

type claudeStateTestEndpointStore struct {
	claudestore.EndpointStore
	records     []*claudestore.EndpointRecord
	failChannel string
}

func (s *claudeStateTestEndpointStore) List(context.Context) ([]*claudestore.EndpointRecord, error) {
	return cloneClaudeStateEndpointRecords(s.records), nil
}

func (s *claudeStateTestEndpointStore) ListEnabled(context.Context) ([]*claudestore.EndpointRecord, error) {
	result := make([]*claudestore.EndpointRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.Enabled {
			copy := *record
			result = append(result, &copy)
		}
	}
	return result, nil
}

func (s *claudeStateTestEndpointStore) ListByChannel(_ context.Context, channel string) ([]*claudestore.EndpointRecord, error) {
	result := make([]*claudestore.EndpointRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.Channel == channel {
			copy := *record
			result = append(result, &copy)
		}
	}
	return result, nil
}

func (s *claudeStateTestEndpointStore) SetActiveChannel(_ context.Context, channel string) error {
	if channel == s.failChannel {
		return fmt.Errorf("forced channel activation failure")
	}
	for _, record := range s.records {
		record.Enabled = record.Channel == channel
	}
	return nil
}

func cloneClaudeStateEndpointRecords(records []*claudestore.EndpointRecord) []*claudestore.EndpointRecord {
	result := make([]*claudestore.EndpointRecord, 0, len(records))
	for _, record := range records {
		copy := *record
		result = append(result, &copy)
	}
	return result
}

func TestPauseGroupRestoresParticipationWhenAutomaticActivationFails(t *testing.T) {
	channelStore := &claudeStateTestChannelStore{records: map[string]*claudestore.ChannelRecord{
		"A": {Name: "A", Priority: 1, FailoverEnabled: true},
		"B": {Name: "B", Priority: 2, FailoverEnabled: true},
	}}
	channelService := claudeservice.NewChannelService(channelStore)
	endpointStore := &claudeStateTestEndpointStore{
		records: []*claudestore.EndpointRecord{
			{ID: 1, Channel: "A", Name: "a", URL: "https://a.example.invalid", Priority: 1, FailoverEnabled: true, SkipHealthCheck: true, Enabled: true},
			{ID: 2, Channel: "B", Name: "b", URL: "https://b.example.invalid", Priority: 1, FailoverEnabled: true, SkipHealthCheck: true},
		},
		failChannel: "B",
	}
	cfg := &config.Config{
		Failover:         config.FailoverConfig{Enabled: true},
		EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"},
	}
	manager := claudeendpoint.NewManager(cfg)
	t.Cleanup(manager.Stop)
	failoverEnabled := true
	runtimeEnabled := true
	manager.SyncEndpoints([]config.EndpointConfig{
		{Name: "a", Channel: "A", URL: "https://a.example.invalid", Priority: 1, FailoverEnabled: &failoverEnabled, SkipHealthCheck: true, Enabled: &runtimeEnabled},
		{Name: "b", Channel: "B", URL: "https://b.example.invalid", Priority: 1, FailoverEnabled: &failoverEnabled, SkipHealthCheck: true},
	})
	manager.UpdateChannelFailoverEnabled(map[string]bool{"A": true, "B": true})
	if err := manager.ManualActivateGroup("A"); err != nil {
		t.Fatalf("激活测试渠道失败: %v", err)
	}

	app := &App{
		endpointManager: manager,
		channelService:  channelService,
		endpointService: claudeservice.NewEndpointService(endpointStore, manager, cfg),
	}
	err := app.PauseGroup("A")
	if err == nil || !strings.Contains(err.Error(), "forced channel activation failure") {
		t.Fatalf("预期自动切换失败，got: %v", err)
	}

	channelStore.mu.Lock()
	defer channelStore.mu.Unlock()
	if !channelStore.records["A"].FailoverEnabled {
		t.Fatal("自动切换失败后必须恢复原渠道数据库参与状态")
	}
	if len(channelStore.updateContexts) != 2 {
		t.Fatalf("预期暂停和恢复各写入一次，got %d", len(channelStore.updateContexts))
	}
	if channelStore.updateContexts[0] == channelStore.updateContexts[1] {
		t.Fatal("恢复参与状态必须使用独立 context")
	}
	if channelStore.updateContextErrors[1] != nil {
		t.Fatalf("恢复 context 在写入时已失效: %v", channelStore.updateContextErrors[1])
	}
	if !manager.GetGroupManager().IsChannelFailoverEnabled("A") {
		t.Fatal("自动切换失败后必须恢复原渠道运行时参与状态")
	}
}

func TestUpdateChannelReturnsPriorityRuntimeSyncFailure(t *testing.T) {
	channelStore := &claudeStateTestChannelStore{
		records: map[string]*claudestore.ChannelRecord{
			"A": {Name: "A", Priority: 1, FailoverEnabled: true},
		},
		listErr: fmt.Errorf("forced channel list failure"),
	}
	cfg := &config.Config{EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"}}
	manager := claudeendpoint.NewManager(cfg)
	t.Cleanup(manager.Stop)
	app := &App{
		endpointManager: manager,
		channelStore:    channelStore,
		channelService:  claudeservice.NewChannelService(channelStore),
	}

	err := app.UpdateChannel(UpdateChannelInput{Name: "A", Priority: 1})
	if err == nil || !strings.Contains(err.Error(), "同步渠道优先级失败") || !strings.Contains(err.Error(), "forced channel list failure") {
		t.Fatalf("预期渠道优先级同步错误向上传播，got: %v", err)
	}
}

func TestSyncChannelFailoverEnabledReturnsChannelListFailure(t *testing.T) {
	channelStore := &claudeStateTestChannelStore{
		records: map[string]*claudestore.ChannelRecord{},
		listErr: fmt.Errorf("forced participation list failure"),
	}
	manager := claudeendpoint.NewManager(&config.Config{})
	t.Cleanup(manager.Stop)
	app := &App{endpointManager: manager, channelStore: channelStore}

	err := app.syncChannelFailoverEnabledToEndpointManager(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forced participation list failure") {
		t.Fatalf("预期渠道参与状态同步错误向上传播，got: %v", err)
	}
}

func TestCreateChannelDoesNotReportFailureAfterPersistentRuntimeSyncError(t *testing.T) {
	channelStore := &claudeStateTestChannelStore{
		records: map[string]*claudestore.ChannelRecord{},
		listErr: fmt.Errorf("forced channel list failure"),
	}
	cfg := &config.Config{EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"}}
	manager := claudeendpoint.NewManager(cfg)
	t.Cleanup(manager.Stop)
	app := &App{
		endpointManager: manager,
		channelStore:    channelStore,
		channelService:  claudeservice.NewChannelService(channelStore),
	}

	if err := app.CreateChannel(CreateChannelInput{Name: "new-channel", Priority: 1}); err != nil {
		t.Fatalf("渠道已落库后不应把运行时同步错误报告为创建失败: %v", err)
	}
	created, err := channelStore.Get(context.Background(), "new-channel")
	if err != nil || created == nil {
		t.Fatalf("预期渠道已创建: channel=%+v err=%v", created, err)
	}
}

var _ claudestore.ChannelStore = (*claudeStateTestChannelStore)(nil)
var _ claudestore.EndpointStore = (*claudeStateTestEndpointStore)(nil)
