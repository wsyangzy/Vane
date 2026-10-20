package service

import (
	"context"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

// memoryModelRouteStore 只实现 Save 路径用到的方法
type memoryModelRouteStore struct {
	routingstore.ModelRouteStore
	records map[int64]*routingstore.ModelRouteRecord
	nextID  int64
}

func (m *memoryModelRouteStore) Create(_ context.Context, record *routingstore.ModelRouteRecord) (*routingstore.ModelRouteRecord, error) {
	m.nextID++
	copied := *record
	copied.ID = m.nextID
	m.records[copied.ID] = &copied
	return &copied, nil
}

func (m *memoryModelRouteStore) GetByID(_ context.Context, id int64) (*routingstore.ModelRouteRecord, error) {
	return m.records[id], nil
}

func (m *memoryModelRouteStore) Update(_ context.Context, record *routingstore.ModelRouteRecord) error {
	copied := *record
	m.records[record.ID] = &copied
	return nil
}

func TestModelRouteSaveRequiresSourceChannelAndRejectsSelfTarget(t *testing.T) {
	ctx := context.Background()
	routeStore := &memoryModelRouteStore{records: map[int64]*routingstore.ModelRouteRecord{}}
	svc := NewModelRouteService(routeStore, nil)
	catalog := routing.Catalog{{ID: 1, Name: "anyrouter"}, {ID: 2, Name: "backup"}}
	base := routingstore.ModelRouteRecord{
		Provider: "claude", Name: "haiku", MatchModel: "claude-haiku-*", Mode: routing.ModePrefer,
		Targets: []routing.Target{{ChannelID: 2}},
	}

	missing := base
	if _, _, err := svc.Save(ctx, &missing, catalog); err == nil || !strings.Contains(err.Error(), "来源渠道") {
		t.Fatalf("rule without a source channel must be rejected, got %v", err)
	}

	self := base
	self.SourceChannelID = 2
	if _, _, err := svc.Save(ctx, &self, catalog); err == nil || !strings.Contains(err.Error(), "来源渠道自身") {
		t.Fatalf("targeting the source channel itself must be rejected, got %v", err)
	}

	valid := base
	valid.SourceChannelID = 1
	saved, _, err := svc.Save(ctx, &valid, catalog)
	if err != nil || saved.SourceChannelID != 1 {
		t.Fatalf("valid rule: saved=%+v err=%v", saved, err)
	}

	moved := *saved
	moved.SourceChannelID = 2
	moved.Targets = []routing.Target{{ChannelID: 1}}
	if _, _, err := svc.Save(ctx, &moved, catalog); err == nil || !strings.Contains(err.Error(), "来源渠道") {
		t.Fatalf("moving a rule to another source channel must be rejected, got %v", err)
	}
}
