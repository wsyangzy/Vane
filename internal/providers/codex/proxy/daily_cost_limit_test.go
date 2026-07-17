package codexproxy

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestSelectQuotaEligibleEndpointFailsClosedAndUsesNextGroup(t *testing.T) {
	limited := &CodexEndpoint{ID: 1, CredentialGroupID: 10, CredentialGroupName: "group-a", CredentialGroupOrder: 0, Name: "limited", FailoverEnabled: true, DailyCostLimitUSD: 1}
	unlimited := &CodexEndpoint{ID: 2, CredentialGroupID: 20, CredentialGroupName: "group-b", CredentialGroupOrder: 1, Name: "unlimited", FailoverEnabled: true}
	manager := &EndpointManager{
		endpoints:    []*CodexEndpoint{limited, unlimited},
		unhealthyMap: map[int64]bool{},
		latencies:    map[int64]int64{},
	}
	handler := &Handler{manager: manager, logger: slog.Default()}

	selected, skipped, err := handler.selectQuotaEligibleEndpoint(context.Background(), "channel-a", nil, nil, nil, true)
	if err != nil {
		t.Fatalf("selectQuotaEligibleEndpoint failed: %v", err)
	}
	if selected != unlimited {
		t.Fatalf("selected = %#v, want unlimited endpoint", selected)
	}
	if len(skipped) != 1 || skipped[0].ID != limited.CredentialGroupID {
		t.Fatalf("skipped = %#v, want limited credential group", skipped)
	}
	if manager.unhealthyMap[limited.ID] {
		t.Fatal("quota filtering must not mark endpoint unhealthy")
	}
}

func TestSelectQuotaEligibleEndpointReturnsDailyLimitSentinel(t *testing.T) {
	limited := &CodexEndpoint{ID: 1, CredentialGroupID: 10, CredentialGroupName: "group-a", Name: "limited", FailoverEnabled: true, DailyCostLimitUSD: 1}
	manager := &EndpointManager{
		endpoints:    []*CodexEndpoint{limited},
		unhealthyMap: map[int64]bool{},
		latencies:    map[int64]int64{},
	}
	handler := &Handler{manager: manager, logger: slog.Default()}

	_, _, err := handler.selectQuotaEligibleEndpoint(context.Background(), "channel-a", nil, nil, nil, true)
	if !errors.Is(err, errCredentialGroupDailyCostLimitExceeded) {
		t.Fatalf("error = %v, want daily cost limit sentinel", err)
	}
	if manager.unhealthyMap[limited.ID] {
		t.Fatal("quota filtering must not mark endpoint unhealthy")
	}
}
