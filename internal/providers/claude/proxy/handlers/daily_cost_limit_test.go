package handlers

import (
	"context"
	"testing"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
)

func TestFilterCredentialGroupDailyCostLimitsFailsClosedAndKeepsUnlimitedGroup(t *testing.T) {
	limited := &endpoint.Endpoint{Config: config.EndpointConfig{
		Name: "limited", Channel: "channel-a", CredentialGroupName: "group-a", DailyCostLimitUSD: 1,
	}}
	unlimited := &endpoint.Endpoint{Config: config.EndpointConfig{
		Name: "unlimited", Channel: "channel-a", CredentialGroupName: "group-b",
	}}

	filtered, blocked := filterCredentialGroupDailyCostLimits(context.Background(), nil, []*endpoint.Endpoint{limited, unlimited})
	if !blocked {
		t.Fatal("expected limited group to be blocked when usage tracker is unavailable")
	}
	if len(filtered) != 1 || filtered[0] != unlimited {
		t.Fatalf("filtered = %#v, want only unlimited endpoint", filtered)
	}
}

func TestFilterCredentialGroupDailyCostLimitsReportsAllLimitedGroupsBlocked(t *testing.T) {
	limited := &endpoint.Endpoint{Config: config.EndpointConfig{
		Name: "limited", Channel: "channel-a", CredentialGroupName: "group-a", DailyCostLimitUSD: 1,
	}}
	filtered, blocked := filterCredentialGroupDailyCostLimits(context.Background(), nil, []*endpoint.Endpoint{limited})
	if !blocked || len(filtered) != 0 {
		t.Fatalf("filtered=%d blocked=%v, want no candidates and blocked", len(filtered), blocked)
	}
}

func TestDailyCostReadFailureRecordsEverySkippedCandidate(t *testing.T) {
	first := &endpoint.Endpoint{Config: config.EndpointConfig{Name: "first", Channel: "channel", CredentialGroupName: "group", DailyCostLimitUSD: 1}}
	second := &endpoint.Endpoint{Config: config.EndpointConfig{Name: "second", Channel: "channel", CredentialGroupName: "group", DailyCostLimitUSD: 1}}
	var skipped []*endpoint.Endpoint
	filtered, blocked := filterCredentialGroupDailyCostLimits(context.Background(), nil, []*endpoint.Endpoint{first, second}, func(ep *endpoint.Endpoint, reason string) {
		skipped = append(skipped, ep)
		if reason != "daily_cost_state_unavailable" {
			t.Errorf("query failure was reported as %q", reason)
		}
	})
	if !blocked || len(filtered) != 0 || len(skipped) != 2 || skipped[0] != first || skipped[1] != second {
		t.Fatalf("filtered=%d blocked=%v skipped=%v", len(filtered), blocked, skipped)
	}
}
