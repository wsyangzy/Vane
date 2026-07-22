package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/providers/codex/store"
)

type channelFailoverStore struct{ channels []*store.CodexChannelRecord }

func (s *channelFailoverStore) GetByID(_ context.Context, id int64) (*store.CodexChannelRecord, error) {
	for _, channel := range s.channels {
		if channel.ID == id {
			return channel, nil
		}
	}
	return nil, nil
}
func (s *channelFailoverStore) List(context.Context) ([]*store.CodexChannelRecord, error) {
	return s.channels, nil
}
func (s *channelFailoverStore) SetFailoverEnabled(_ context.Context, id int64, enabled bool) error {
	for _, channel := range s.channels {
		if channel.ID == id {
			channel.FailoverEnabled = enabled
		}
	}
	return nil
}
func (s *channelFailoverStore) Create(_ context.Context, record *store.CodexChannelRecord) (*store.CodexChannelRecord, error) {
	created := *record
	created.ID = int64(len(s.channels) + 1)
	s.channels = append(s.channels, &created)
	return &created, nil
}
func (s *channelFailoverStore) GetByName(_ context.Context, name string) (*store.CodexChannelRecord, error) {
	for _, channel := range s.channels {
		if channel.Name == name {
			return channel, nil
		}
	}
	return nil, nil
}
func (s *channelFailoverStore) GetActive(context.Context) (*store.CodexChannelRecord, error) {
	for _, channel := range s.channels {
		if channel.IsActive {
			return channel, nil
		}
	}
	return nil, nil
}
func (s *channelFailoverStore) UpdateByID(_ context.Context, id int64, record *store.CodexChannelRecord) error {
	for index, channel := range s.channels {
		if channel.ID == id {
			updated := *record
			updated.ID = id
			updated.IsActive = channel.IsActive
			s.channels[index] = &updated
			return nil
		}
	}
	return nil
}
func (s *channelFailoverStore) DeleteByID(context.Context, int64) error { return nil }
func (s *channelFailoverStore) SetActive(_ context.Context, id int64) error {
	for _, channel := range s.channels {
		channel.IsActive = channel.ID == id
	}
	return nil
}
func (s *channelFailoverStore) WithTx(*sql.Tx) store.CodexChannelStore { return s }

type channelFailoverEndpointStore struct {
	store.CodexEndpointStore
	endpoints map[int64][]*store.CodexEndpointRecord
}

func (s *channelFailoverEndpointStore) ListByChannelID(_ context.Context, channelID int64) ([]*store.CodexEndpointRecord, error) {
	return s.endpoints[channelID], nil
}

func TestSetFailoverEnabledValidatesPriorityOnlyWhenResuming(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 1, FailoverEnabled: true},
		{ID: 2, Name: "paused", Priority: 1, FailoverEnabled: false},
	}}
	service := NewCodexChannelService(channelStore, nil)

	if err := service.SetFailoverEnabled(context.Background(), 2, false); err != nil {
		t.Fatalf("pausing must ignore duplicate priority: %v", err)
	}
	if err := service.SetFailoverEnabled(context.Background(), 2, true); err == nil || !strings.Contains(err.Error(), "优先级 P1") {
		t.Fatalf("resuming must reject duplicate priority, got: %v", err)
	}
}

func TestSetFailoverEnabledRejectsPausingActiveChannel(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: true, IsActive: true},
	}}
	service := NewCodexChannelService(channelStore, nil)
	if err := service.SetFailoverEnabled(context.Background(), 1, false); err == nil || !strings.Contains(err.Error(), "当前接管渠道不能暂停") {
		t.Fatalf("pausing active channel must be rejected, got: %v", err)
	}
}

func TestEnsureActiveChannelParticipatesResumesOnlyActiveChannel(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: false, IsActive: true},
		// A paused channel may temporarily retain the active channel's priority.
		// Enabling global failover must still resume the active channel only.
		{ID: 2, Name: "paused-backup", Priority: 0, FailoverEnabled: false},
	}}
	service := NewCodexChannelService(channelStore, nil)

	resumed, err := service.EnsureActiveChannelParticipates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !resumed || !channelStore.channels[0].FailoverEnabled {
		t.Fatal("expected the active channel to resume failover participation")
	}
	if channelStore.channels[1].FailoverEnabled {
		t.Fatal("expected an inactive paused channel to remain paused")
	}
	resumed, err = service.EnsureActiveChannelParticipates(context.Background())
	if err != nil || resumed {
		t.Fatalf("expected an idempotent no-op, resumed=%v err=%v", resumed, err)
	}
}

func TestCreateChannelAutoAssignsPriorityAfterPausedChannels(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: true, IsActive: true},
		{ID: 2, Name: "paused", Priority: 3, FailoverEnabled: false},
	}}
	service := NewCodexChannelService(channelStore, nil)

	created, err := service.CreateChannel(context.Background(), &store.CodexChannelRecord{
		Name:            "new",
		Priority:        -1,
		FailoverEnabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if created.Priority != 4 {
		t.Fatalf("expected new channel priority P4 after every existing channel, got P%d", created.Priority)
	}
}

func TestCreateChannelMovesOccupiedPriorityToDefaultTail(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: true, IsActive: true},
		{ID: 2, Name: "occupied", Priority: 1, FailoverEnabled: true},
	}}
	service := NewCodexChannelService(channelStore, nil)

	created, err := service.CreateChannel(context.Background(), &store.CodexChannelRecord{
		Name:            "inserted",
		Priority:        1,
		FailoverEnabled: true,
	})
	if err != nil {
		t.Fatalf("create channel at occupied priority: %v", err)
	}
	if created.Priority != 1 || channelStore.channels[1].Priority != 2 {
		t.Fatalf("expected inserted=P1 and occupied=P2, got inserted=P%d occupied=P%d", created.Priority, channelStore.channels[1].Priority)
	}
}

func TestUpdateChannelSwapsPriorityWithPausedChannel(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: true, IsActive: true},
		{ID: 2, Name: "paused", Priority: 1, FailoverEnabled: false},
	}}
	service := NewCodexChannelService(channelStore, nil)
	updated := *channelStore.channels[0]
	updated.Priority = 1

	if err := service.UpdateChannelByID(context.Background(), updated.ID, &updated); err != nil {
		t.Fatalf("update channel priority: %v", err)
	}
	if channelStore.channels[0].Priority != 1 || channelStore.channels[1].Priority != 0 {
		t.Fatalf("expected priorities to swap across paused state, got active=P%d paused=P%d", channelStore.channels[0].Priority, channelStore.channels[1].Priority)
	}
}

func TestUpdateChannelRejectsPausingActiveChannel(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: true, IsActive: true},
	}}
	service := NewCodexChannelService(channelStore, nil)
	updated := *channelStore.channels[0]
	updated.FailoverEnabled = false

	if err := service.UpdateChannelByID(context.Background(), updated.ID, &updated); err == nil || !strings.Contains(err.Error(), "当前接管渠道不能暂停") {
		t.Fatalf("editing active channel must not bypass pause guard, got: %v", err)
	}
}

func TestGetNextFailoverChannelUsesPriorityAndSkipsIneligibleChannels(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: true, IsActive: true},
		{ID: 2, Name: "paused", Priority: 1, FailoverEnabled: false},
		{ID: 3, Name: "empty", Priority: 2, FailoverEnabled: true},
		{ID: 4, Name: "backup", Priority: 3, FailoverEnabled: true},
	}}
	endpointStore := &channelFailoverEndpointStore{endpoints: map[int64][]*store.CodexEndpointRecord{
		2: {{ID: 20, ChannelID: 2, FailoverEnabled: true}},
		3: {{ID: 30, ChannelID: 3, FailoverEnabled: false}},
		4: {{ID: 40, ChannelID: 4, FailoverEnabled: true}},
	}}
	service := NewCodexChannelService(channelStore, endpointStore)

	next, err := service.GetNextFailoverChannel(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != 4 {
		t.Fatalf("next channel = %d, want 4", next.ID)
	}
}

func TestActivateChannelRejectsPausedTargetOnlyWhenFailoverIsRequired(t *testing.T) {
	channelStore := &channelFailoverStore{channels: []*store.CodexChannelRecord{
		{ID: 1, Name: "active", Priority: 0, FailoverEnabled: true, IsActive: true},
		{ID: 2, Name: "paused", Priority: 1, FailoverEnabled: false},
	}}
	service := NewCodexChannelService(channelStore, nil)

	if err := service.ActivateChannel(context.Background(), 2, true); err == nil || !strings.Contains(err.Error(), "已暂停") {
		t.Fatalf("expected paused activation rejection, got %v", err)
	}
	if err := service.ActivateChannel(context.Background(), 2, false); err != nil {
		t.Fatalf("activation while global failover is disabled: %v", err)
	}
	if !channelStore.channels[1].IsActive {
		t.Fatal("expected paused channel to become active when participation is not required")
	}
}
