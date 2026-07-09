package codexproxy

import "testing"

func TestEndpointManagerUsesEndpointIDForDuplicateNamesAcrossCredentialGroups(t *testing.T) {
	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{
				ID:                  1,
				CredentialGroupID:   10,
				CredentialGroupName: "github",
				Name:                "大陆网络优化",
				FailoverEnabled:     true,
				Priority:            1,
			},
			{
				ID:                  2,
				CredentialGroupID:   20,
				CredentialGroupName: "默认分组",
				Name:                "大陆网络优化",
				FailoverEnabled:     true,
				Priority:            2,
			},
			{
				ID:                  3,
				CredentialGroupID:   30,
				CredentialGroupName: "backup",
				Name:                "jp-node",
				FailoverEnabled:     true,
				Priority:            3,
			},
		},
		unhealthyMap:   make(map[int64]bool),
		failureDetails: make(map[int64]EndpointFailureDetail),
		logger:         newTestLogger(),
	}

	first, err := manager.GetPrimaryEndpoint()
	if err != nil {
		t.Fatalf("GetPrimaryEndpoint() error = %v", err)
	}
	if first.ID != 1 {
		t.Fatalf("GetPrimaryEndpoint() ID = %d, want 1", first.ID)
	}

	manager.MarkUnhealthyWithDetail(first.ID, 502, "first group failed")

	next, err := manager.GetNextEndpoint([]int64{first.ID})
	if err != nil {
		t.Fatalf("GetNextEndpoint() error = %v", err)
	}
	if next.ID != 2 {
		t.Fatalf("GetNextEndpoint() ID = %d, want duplicate-name endpoint ID 2", next.ID)
	}
	if next.Name != first.Name {
		t.Fatalf("expected duplicate endpoint name %q, got %q", first.Name, next.Name)
	}
	if next.CredentialGroupID == first.CredentialGroupID {
		t.Fatalf("expected next endpoint from a different credential group")
	}

	if !manager.IsHealthy(next.ID) {
		t.Fatalf("duplicate-name endpoint ID %d should remain healthy", next.ID)
	}
	if manager.IsHealthy(first.ID) {
		t.Fatalf("failed endpoint ID %d should be unhealthy", first.ID)
	}
}

func TestEndpointManagerFastestStrategyUsesRecordedLatencyWithinCredentialGroup(t *testing.T) {
	manager := &EndpointManager{
		strategyType: "fastest",
		endpoints: []*CodexEndpoint{
			{
				ID:                   1,
				CredentialGroupOrder: 0,
				Name:                 "priority-first",
				FailoverEnabled:      true,
				Priority:             1,
			},
			{
				ID:                   2,
				CredentialGroupOrder: 0,
				Name:                 "fastest",
				FailoverEnabled:      true,
				Priority:             2,
			},
			{
				ID:                   3,
				CredentialGroupOrder: 1,
				Name:                 "other-group-fastest",
				FailoverEnabled:      true,
				Priority:             1,
			},
		},
		unhealthyMap:   make(map[int64]bool),
		failureDetails: make(map[int64]EndpointFailureDetail),
		latencies: map[int64]int64{
			1: 120,
			2: 20,
			3: 1,
		},
		logger: newTestLogger(),
	}

	first, err := manager.GetPrimaryEndpoint()
	if err != nil {
		t.Fatalf("GetPrimaryEndpoint() error = %v", err)
	}
	if first.ID != 2 {
		t.Fatalf("GetPrimaryEndpoint() ID = %d, want fastest endpoint ID 2", first.ID)
	}
}

func TestEndpointManagerPrimaryUsesCredentialGroupBeforeActiveEndpoint(t *testing.T) {
	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{
				ID:                   1,
				CredentialGroupOrder: 0,
				Name:                 "old-group-p0",
				FailoverEnabled:      true,
				Priority:             0,
			},
			{
				ID:                   2,
				CredentialGroupOrder: 2,
				Name:                 "active-new-group",
				IsActive:             true,
				FailoverEnabled:      true,
				Priority:             0,
			},
		},
		unhealthyMap:   make(map[int64]bool),
		failureDetails: make(map[int64]EndpointFailureDetail),
		logger:         newTestLogger(),
	}

	first, err := manager.GetPrimaryEndpoint()
	if err != nil {
		t.Fatalf("GetPrimaryEndpoint() error = %v", err)
	}
	if first.ID != 1 {
		t.Fatalf("GetPrimaryEndpoint() ID = %d, want credential-group-priority endpoint ID 1", first.ID)
	}
}

func TestEndpointManagerSkipHealthCheckBypassesHealthFilter(t *testing.T) {
	manager := &EndpointManager{
		endpoints: []*CodexEndpoint{
			{
				ID:                   1,
				CredentialGroupOrder: 0,
				Name:                 "regular-unhealthy",
				IsActive:             true,
				FailoverEnabled:      true,
				Priority:             0,
			},
			{
				ID:                   2,
				CredentialGroupOrder: 0,
				Name:                 "skip-health",
				FailoverEnabled:      true,
				SkipHealthCheck:      true,
				Priority:             1,
			},
		},
		unhealthyMap: map[int64]bool{
			1: true,
			2: true,
		},
		failureDetails: make(map[int64]EndpointFailureDetail),
		logger:         newTestLogger(),
	}

	first, err := manager.GetPrimaryEndpoint()
	if err != nil {
		t.Fatalf("GetPrimaryEndpoint() error = %v", err)
	}
	if first.ID != 2 {
		t.Fatalf("GetPrimaryEndpoint() ID = %d, want skip-health endpoint ID 2", first.ID)
	}

	next, err := manager.GetNextEndpoint(nil)
	if err != nil {
		t.Fatalf("GetNextEndpoint() error = %v", err)
	}
	if next.ID != 2 {
		t.Fatalf("GetNextEndpoint() ID = %d, want skip-health endpoint ID 2", next.ID)
	}
}

func TestSortCodexEndpointsKeepsCredentialGroupsContiguous(t *testing.T) {
	endpoints := []*CodexEndpoint{
		{
			ID:                   1,
			CredentialGroupOrder: 1,
			CredentialGroupName:  "github",
			Name:                 "github-p0",
			FailoverEnabled:      true,
			Priority:             0,
		},
		{
			ID:                   2,
			CredentialGroupOrder: 0,
			CredentialGroupName:  "默认分组",
			Name:                 "default-p2",
			FailoverEnabled:      true,
			Priority:             2,
		},
		{
			ID:                   3,
			CredentialGroupOrder: 2,
			CredentialGroupName:  "linuxdo",
			Name:                 "linuxdo-p0",
			FailoverEnabled:      true,
			Priority:             0,
		},
		{
			ID:                   4,
			CredentialGroupOrder: 0,
			CredentialGroupName:  "默认分组",
			Name:                 "default-p1",
			FailoverEnabled:      true,
			Priority:             1,
		},
		{
			ID:                   5,
			CredentialGroupOrder: 1,
			CredentialGroupName:  "github",
			Name:                 "github-p1",
			FailoverEnabled:      true,
			Priority:             1,
		},
	}

	sortCodexEndpoints(endpoints)

	got := make([]int64, 0, len(endpoints))
	for _, endpoint := range endpoints {
		got = append(got, endpoint.ID)
	}
	want := []int64{4, 2, 1, 5, 3}
	if len(got) != len(want) {
		t.Fatalf("sorted endpoint count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted IDs = %v, want %v", got, want)
		}
	}
}
