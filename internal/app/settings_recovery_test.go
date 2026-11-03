package app

import (
	"context"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestGlobalRecoveryWaitDefaultsPersistsAndResets(t *testing.T) {
	a, _ := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.applySettingsToConfig(false); err != nil {
		t.Fatal(err)
	}
	setting, err := a.GetSetting("retry", "wait_for_recovery")
	if err != nil || setting.Provider != "global" || setting.Value != "true" || a.config.Retry.DisableRecoveryWait {
		t.Fatalf("recovery waiting must default on globally: setting=%+v runtime=%+v err=%v", setting, a.config.Retry, err)
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{{Provider: "global", Category: "retry", Key: "wait_for_recovery", Value: "false"}}}); err != nil {
		t.Fatal(err)
	}
	if !a.config.Retry.DisableRecoveryWait {
		t.Fatal("disabling recovery waiting did not reach the runtime")
	}
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	a.config.Retry = config.NewDefaultConfig().Retry
	if err := a.applySettingsToConfig(false); err != nil {
		t.Fatal(err)
	}
	if !a.config.Retry.DisableRecoveryWait {
		t.Fatal("reinitialization lost the saved disabled policy")
	}
	for _, input := range []UpdateSettingInput{
		{Provider: "claude", Category: "retry", Key: "wait_for_recovery", Value: "true"},
		{Provider: "codex", Category: "retry", Key: "wait_for_recovery", Value: "true"},
		{Provider: "global", Category: "retry", Key: "wait_for_recovery", Value: "invalid"},
	} {
		if err := a.UpdateSetting(input); err == nil {
			t.Fatalf("invalid global recovery policy accepted: %+v", input)
		}
	}
	if err := a.ResetCategorySettings("retry"); err != nil {
		t.Fatal(err)
	}
	if a.config.Retry.DisableRecoveryWait {
		t.Fatal("reset must restore recovery waiting")
	}
}

func TestRoutingPolicySettingsPersistApplyToBothProvidersAndRejectInvalidBatch(t *testing.T) {
	a, _ := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	visible, err := a.GetSettingsByCategory("retry")
	if err != nil || len(visible) != 7 {
		t.Fatalf("routing policy must be exposed by the settings API: %d %v", len(visible), err)
	}
	a.poolRuntimes = map[string]*routing.PoolRuntime{}
	a.profileRuntimes = map[string]*routing.ProfileRuntime{}
	for _, provider := range []string{"claude", "codex"} {
		a.poolRuntimes[provider] = routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
		profile := routing.DefaultProfile(provider)
		profile.ID, profile.Name, profile.IsDefault = 1, "test", true
		profile.Rules = []routing.RoutingRule{{ID: 1, Enabled: true, Kind: "model", MatchModel: "*", Targets: []routing.RouteTarget{{ChannelID: 2}}}}
		a.profileRuntimes[provider] = routing.NewProfileRuntime(routing.RuntimeConfig{Provider: provider, Profiles: []routing.RoutingProfile{profile}})
	}
	updates := []UpdateSettingInput{
		{Provider: "global", Category: "retry", Key: "cooldown_initial", Value: "4s"},
		{Provider: "global", Category: "retry", Key: "cooldown_max", Value: "20s"},
		{Provider: "global", Category: "retry", Key: "cooldown_multiplier", Value: "1.5"},
		{Provider: "global", Category: "retry", Key: "rule_fallback_to_default", Value: "false"},
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: updates}); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"claude", "codex"} {
		policy := a.poolRuntimes[provider].Config().CooldownPolicy
		if policy.Initial != 4*time.Second || policy.Maximum != 20*time.Second || policy.Multiplier != 1.5 {
			t.Fatalf("%s did not apply saved policy: %+v", provider, policy)
		}
		plan := a.profileRuntimes[provider].Preview(ctx, 1, "test-model", true, routing.TurnInput{})
		if plan.Decision.FallbackToDefault {
			t.Fatalf("%s did not apply inherited model fallback", provider)
		}
	}
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	for _, update := range updates {
		saved, err := a.GetSetting(update.Category, update.Key)
		if err != nil || saved.Value != update.Value {
			t.Fatalf("initialization overwrote saved policy: %+v %v", saved, err)
		}
	}
	for _, bad := range []UpdateSettingInput{
		{Provider: "global", Category: "retry", Key: "cooldown_initial", Value: "21s"},
		{Provider: "global", Category: "retry", Key: "cooldown_max", Value: "1ms"},
		{Provider: "global", Category: "retry", Key: "cooldown_multiplier", Value: "NaN"},
		{Provider: "global", Category: "retry", Key: "cooldown_multiplier", Value: "0.5"},
		{Provider: "codex", Category: "retry", Key: "rule_fallback_to_default", Value: "true"},
		{Provider: "global", Category: "retry", Key: "rule_fallback_to_default", Value: "bad"},
	} {
		if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{{Provider: "global", Category: "retry", Key: "rule_fallback_to_default", Value: "true"}, bad}}); err == nil {
			t.Fatalf("invalid batch accepted: %+v", bad)
		}
		saved, _ := a.GetSetting("retry", "rule_fallback_to_default")
		if saved.Value != "false" {
			t.Fatal("invalid batch partially updated settings")
		}
	}
	if err := a.ResetCategorySettings("retry"); err != nil {
		t.Fatal(err)
	}
	policy, rules := a.settingsService.RoutingPolicy(ctx)
	if policy != routing.DefaultCooldownPolicy() || rules != routing.DefaultRuleDefaults() {
		t.Fatal("reset failed to restore routing policy defaults")
	}
	for _, provider := range []string{"claude", "codex"} {
		plan := a.profileRuntimes[provider].Preview(ctx, 1, "test-model", true, routing.TurnInput{})
		if !plan.Decision.FallbackToDefault || a.poolRuntimes[provider].Config().CooldownPolicy != routing.DefaultCooldownPolicy() {
			t.Fatalf("reset did not reach %s runtime", provider)
		}
	}
}
