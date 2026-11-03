package service

import (
	"context"
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

func (s *SettingsService) RoutingPolicy(ctx context.Context) (routing.CooldownPolicy, routing.RuleDefaults) {
	cooldown, rules := routing.DefaultCooldownPolicy(), routing.DefaultRuleDefaults()
	if s == nil {
		return cooldown, rules
	}
	cooldown.Initial = s.GetDuration(ctx, CategoryRetry, "cooldown_initial", cooldown.Initial)
	cooldown.Maximum = s.GetDuration(ctx, CategoryRetry, "cooldown_max", cooldown.Maximum)
	cooldown.Multiplier = s.GetFloat(ctx, CategoryRetry, "cooldown_multiplier", cooldown.Multiplier)
	rules.FallbackToDefault = s.GetBool(ctx, CategoryRetry, "rule_fallback_to_default", rules.FallbackToDefault)
	return cooldown, rules
}

func IsCurrentRetrySetting(key string) bool {
	switch key {
	case "max_total_attempts", "max_total_duration", "wait_for_recovery", "cooldown_initial", "cooldown_max", "cooldown_multiplier",
		"rule_fallback_to_default":
		return true
	}
	return false
}

func (s *SettingsService) validateCooldownUpdate(ctx context.Context, records []*settingsstore.SettingRecord) error {
	policy, _ := s.RoutingPolicy(ctx)
	changed := false
	for _, record := range records {
		if record.Category != CategoryRetry {
			continue
		}
		switch record.Key {
		case "cooldown_initial":
			policy.Initial, _ = time.ParseDuration(record.Value)
			changed = true
		case "cooldown_max":
			policy.Maximum, _ = time.ParseDuration(record.Value)
			changed = true
		}
	}
	if changed && policy.Initial > policy.Maximum {
		return fmt.Errorf("最长冷却时间不能小于首次冷却时间")
	}
	return nil
}
