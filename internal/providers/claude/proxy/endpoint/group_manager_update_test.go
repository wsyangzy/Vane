package endpoint

import (
	"testing"

	"ai-relay-forwarder/config"
)

func TestUpdateGroupsPreservesSQLiteActiveAndAppliesChannelState(t *testing.T) {
	cfg := &config.Config{
		EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"},
	}
	gm := NewGroupManager(cfg)

	failoverDisabled := false
	endpoints := []*Endpoint{
		testGroupEndpoint("alpha-a", "alpha", 5, nil),
		testGroupEndpoint("beta-a", "beta", 3, nil),
		testGroupEndpoint("gamma-a", "gamma", 7, &failoverDisabled),
	}

	gm.UpdateGroups(endpoints)
	if gm.groups["alpha"] == nil {
		t.Fatal("expected alpha group")
	}
	gm.groups["alpha"].IsActive = true

	gm.UpdateChannelPriorities(map[string]int{
		"alpha": 4,
		"beta":  1,
	})
	gm.UpdateChannelFailoverEnabled(map[string]bool{
		"beta": false,
	})

	updated := []*Endpoint{
		testGroupEndpoint("alpha-a", "alpha", 5, nil),
		testGroupEndpoint("alpha-b", "alpha", 2, nil),
		testGroupEndpoint("beta-a", "beta", 3, nil),
		testGroupEndpoint("gamma-a", "gamma", 7, &failoverDisabled),
		testGroupEndpoint("gamma-b", "gamma", 8, &failoverDisabled),
	}
	gm.UpdateGroups(updated)

	alpha := gm.groups["alpha"]
	if alpha == nil {
		t.Fatal("expected alpha group after update")
	}
	if !alpha.IsActive {
		t.Fatal("expected sqlite mode to preserve active alpha group")
	}
	if alpha.Priority != 4 {
		t.Fatalf("expected alpha channel priority override 4, got %d", alpha.Priority)
	}
	if len(alpha.Endpoints) != 2 {
		t.Fatalf("expected alpha to contain 2 endpoints, got %d", len(alpha.Endpoints))
	}
	if alpha.ManuallyPaused {
		t.Fatal("expected alpha to remain unpaused")
	}

	beta := gm.groups["beta"]
	if beta == nil {
		t.Fatal("expected beta group after update")
	}
	if beta.Priority != 1 {
		t.Fatalf("expected beta channel priority override 1, got %d", beta.Priority)
	}
	if !beta.ManuallyPaused {
		t.Fatal("expected beta to be paused by channel failover setting")
	}

	gamma := gm.groups["gamma"]
	if gamma == nil {
		t.Fatal("expected gamma group after update")
	}
	if !gamma.ManuallyPaused {
		t.Fatal("expected gamma to be paused when all endpoints disable failover")
	}
}

func TestDisabledFailoverNeverAutoActivatesAnotherYAMLChannel(t *testing.T) {
	cfg := &config.Config{
		Failover: config.FailoverConfig{
			Enabled:            false,
			SuspendOnExhausted: false,
		},
		EndpointsStorage: config.EndpointsStorageConfig{Type: "yaml"},
	}
	gm := NewGroupManager(cfg)
	gm.UpdateGroups([]*Endpoint{
		testGroupEndpoint("alpha-a", "alpha", 1, nil),
		testGroupEndpoint("beta-a", "beta", 2, nil),
	})

	active := gm.GetActiveGroups()
	if len(active) != 1 || active[0].Name != "alpha" {
		t.Fatalf("startup active groups = %#v, want alpha", active)
	}
	if err := gm.ManualPauseGroup("alpha", 0); err != nil {
		t.Fatalf("pause alpha: %v", err)
	}
	active = gm.GetActiveGroups()
	if len(active) != 0 {
		t.Fatalf("disabled failover activated another runtime channel: %#v", active)
	}
}

func testGroupEndpoint(name, channel string, priority int, failoverEnabled *bool) *Endpoint {
	return &Endpoint{
		Config: config.EndpointConfig{
			Name:            name,
			Channel:         channel,
			Priority:        priority,
			FailoverEnabled: failoverEnabled,
		},
		Status: EndpointStatus{Healthy: true},
	}
}
