package handlers

import (
	"context"
	"testing"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
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
