package service

import (
	"context"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"

	"github.com/stretchr/testify/require"
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

func TestNormalizeCodexBillingRuleGroupRelationship(t *testing.T) {
	for _, mode := range []string{"", "override", " Inherit ", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			rules, err := normalizeCodexBillingRules([]store.CodexBillingRuleRecord{{Mode: tracking.BillingRuleModeFixed, GroupMultiplierMode: mode}})
			if mode == "invalid" {
				require.ErrorContains(t, err, "分组倍率关系")
				return
			}
			require.NoError(t, err)
			want := tracking.BillingGroupMultiplierOverride
			if mode == " Inherit " {
				want = tracking.BillingGroupMultiplierInherit
			}
			require.Equal(t, want, rules[0].GroupMultiplierMode)
		})
	}
}

func TestNormalizeCodexAddedRulesAlwaysUseGroupInheritance(t *testing.T) {
	rules, err := normalizeCodexBillingRules([]store.CodexBillingRuleRecord{
		{Mode: "fixed", InputPrice: 2},
		{Model: "gpt-6.1-sol", ServiceTier: "priority", Context: "normal", Mode: "fixed", InputPrice: 4},
		{Model: "gpt-6.1-sol", ServiceTier: "fast", Context: "long", Mode: "fixed", GroupMultiplierMode: "override", InputPrice: 8},
		{Model: "other", Mode: "fixed", GroupMultiplierMode: "override", InputPrice: 2},
	})
	require.NoError(t, err)
	require.Equal(t, tracking.BillingGroupMultiplierOverride, rules[0].GroupMultiplierMode)
	require.Equal(t, tracking.BillingGroupMultiplierInherit, rules[1].GroupMultiplierMode)
	require.Equal(t, tracking.BillingGroupMultiplierInherit, rules[2].GroupMultiplierMode)
	require.Equal(t, tracking.BillingGroupMultiplierInherit, rules[3].GroupMultiplierMode)
}

func TestCodexCredentialGroupDeletion(t *testing.T) {
	for _, tc := range []struct {
		name           string
		channelActive  bool
		empty          bool
		isDefault      bool
		participating  bool
		activeEndpoint bool
		failGroupWrite bool
		outerRollback  bool
		wantError      string
	}{
		{name: "disabled endpoints in active channel", channelActive: true},
		{name: "disabled endpoints in inactive channel"},
		{name: "empty group", empty: true},
		{name: "default group remains protected", isDefault: true, wantError: "默认凭证分组不能删除"},
		{name: "legacy failover participation does not block deletion", participating: true},
		{name: "legacy activation does not block deletion", activeEndpoint: true},
		{name: "group deletion failure restores both endpoints", failGroupWrite: true, wantError: "forced group failure"},
		{name: "joins caller transaction", outerRollback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newCodexEndpointTestDB(t)
			db.SetMaxOpenConns(1)
			channels := store.NewSQLiteCodexChannelStore(db)
			groups := store.NewSQLiteCodexCredentialGroupStore(db)
			endpoints := store.NewSQLiteCodexEndpointStore(db)
			service := NewCodexCredentialGroupService(groups, channels, endpoints)
			channel, err := channels.Create(ctx, &store.CodexChannelRecord{Name: "test-channel"})
			require.NoError(t, err)
			if tc.channelActive {
				require.NoError(t, channels.SetActive(ctx, channel.ID))
			}
			keptGroup, err := groups.Create(ctx, &store.CodexCredentialGroupRecord{
				ChannelID: channel.ID, Name: "kept", ApiKey: "test-kept-key", IsDefault: true,
			})
			require.NoError(t, err)
			targetGroup, err := groups.Create(ctx, &store.CodexCredentialGroupRecord{
				ChannelID: channel.ID, Name: "target", ApiKey: "test-target-key", IsDefault: tc.isDefault,
			})
			require.NoError(t, err)
			keptEndpoint, err := endpoints.Create(ctx, &store.CodexEndpointRecord{
				ChannelID: channel.ID, CredentialGroupID: keptGroup.ID, Name: "kept",
				BaseURL: "https://kept.example.invalid/v1", FailoverEnabled: true, IsActive: !tc.activeEndpoint,
			})
			require.NoError(t, err)
			var targetEndpoints []*store.CodexEndpointRecord
			if !tc.empty {
				for i, name := range []string{"first", "second"} {
					endpoint, err := endpoints.Create(ctx, &store.CodexEndpointRecord{
						ChannelID: channel.ID, CredentialGroupID: targetGroup.ID, Name: name,
						BaseURL:         "https://" + name + ".example.invalid/v1",
						FailoverEnabled: tc.participating && i == 1, IsActive: tc.activeEndpoint && i == 1,
					})
					require.NoError(t, err)
					targetEndpoints = append(targetEndpoints, endpoint)
				}
			}
			if tc.failGroupWrite {
				_, err := db.ExecContext(ctx, `CREATE TRIGGER fail_group_delete BEFORE DELETE ON codex_credential_groups
					BEGIN SELECT RAISE(ABORT, 'forced group failure'); END`)
				require.NoError(t, err)
			}

			if tc.outerRollback {
				tx, err := db.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer tx.Rollback()
				require.NoError(t, groups.WithTx(tx).DeleteByID(ctx, targetGroup.ID))
				require.NoError(t, tx.Rollback())
			} else {
				err = service.DeleteGroup(ctx, targetGroup.ID)
				if tc.wantError != "" {
					require.ErrorContains(t, err, tc.wantError)
				} else {
					require.NoError(t, err)
				}
			}

			shouldRemain := tc.wantError != "" || tc.outerRollback
			groupAfter, err := groups.GetByID(ctx, targetGroup.ID)
			require.NoError(t, err)
			require.Equal(t, shouldRemain, groupAfter != nil)
			for _, endpoint := range targetEndpoints {
				after, err := endpoints.GetByID(ctx, endpoint.ID)
				require.NoError(t, err)
				require.Equal(t, shouldRemain, after != nil)
				if after != nil {
					require.Equal(t, targetGroup.ID, after.CredentialGroupID)
					require.Equal(t, endpoint.FailoverEnabled, after.FailoverEnabled)
				}
			}
			keptAfter, err := endpoints.GetByID(ctx, keptEndpoint.ID)
			require.NoError(t, err)
			require.NotNil(t, keptAfter)
			require.True(t, keptAfter.FailoverEnabled)
			require.Equal(t, keptEndpoint.IsActive, keptAfter.IsActive)
			channelAfter, err := channels.GetByID(ctx, channel.ID)
			require.NoError(t, err)
			require.Equal(t, tc.channelActive, channelAfter.IsActive)
		})
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

func TestNormalizeCodexCredentialGroupModelPolicy(t *testing.T) {
	record := &store.CodexCredentialGroupRecord{
		ChannelID: 1, Name: "g", ApiKey: "key", ModelPolicy: " OVERRIDE ",
		ModelMapping:    map[string]string{" gpt-a ": " gpt-mini ", "": "ignored"},
		SupportedModels: []string{" gpt-a ", "gpt-a", ""},
	}
	// normalizeAndValidate reaches stores after policy normalization; use the
	// standalone normalization path through a minimal record validation setup.
	record.ModelPolicy = strings.ToLower(strings.TrimSpace(record.ModelPolicy))
	record.ModelMapping = normalizeCodexModelMapping(record.ModelMapping)
	record.SupportedModels = normalizeCodexSupportedModels(record.SupportedModels)
	if record.ModelPolicy != store.CodexModelPolicyOverride || record.ModelMapping["gpt-a"] != "gpt-mini" || len(record.SupportedModels) != 1 {
		t.Fatalf("unexpected normalized model policy: %+v", record)
	}
}
