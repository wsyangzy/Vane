package service

import (
	"context"
	"strings"
	"testing"

	"ai-relay-forwarder/internal/providers/codex/store"
	"ai-relay-forwarder/internal/tracking"
)

func TestNormalizeCodexBillingRulesRequiresDefaultAndNormalizesSelectors(t *testing.T) {
	rules, err := normalizeCodexBillingRules([]store.CodexBillingRuleRecord{
		{Mode: tracking.BillingRuleModeMultiplier, CostMultiplier: 1, InputCostMultiplier: 1, OutputCostMultiplier: 1, CacheCreationCostMultiplier: 1, CacheReadCostMultiplier: 1},
		{ServiceTier: "priority", Context: "LONG", Mode: tracking.BillingRuleModeFixed, InputPrice: 2, OutputPrice: 4, CacheCreationPrice: 1, CacheReadPrice: 0.5},
	})
	if err != nil {
		t.Fatalf("normalizeCodexBillingRules returned error: %v", err)
	}
	if rules[1].ServiceTier != tracking.ServiceTierFast || rules[1].Context != tracking.BillingContextLong {
		t.Fatalf("expected normalized selectors, got %+v", rules[1])
	}

	_, err = normalizeCodexBillingRules([]store.CodexBillingRuleRecord{{Model: "gpt-test", Mode: tracking.BillingRuleModeFixed}})
	if err == nil || !strings.Contains(err.Error(), "默认规则") {
		t.Fatalf("expected missing default rule error, got %v", err)
	}
}

func TestEnsureDefaultGroupSkipsChannelWithoutAPIKey(t *testing.T) {
	service := &CodexCredentialGroupService{}
	group, err := service.EnsureDefaultGroup(context.Background(), &store.CodexChannelRecord{ID: 1, Name: "empty"})
	if err != nil {
		t.Fatalf("EnsureDefaultGroup returned error: %v", err)
	}
	if group != nil {
		t.Fatalf("group = %#v, want nil", group)
	}
}
