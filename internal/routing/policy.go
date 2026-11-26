package routing

import (
	"math"
	"time"
)

// CooldownPolicy controls temporary failures; credential repair and request
// errors retain their own admission rules.
type CooldownPolicy struct {
	Initial    time.Duration
	Maximum    time.Duration
	Multiplier float64
}

func DefaultCooldownPolicy() CooldownPolicy {
	return CooldownPolicy{Initial: 30 * time.Second, Maximum: 5 * time.Minute, Multiplier: 2}
}

func (p CooldownPolicy) normalized() CooldownPolicy {
	defaults := DefaultCooldownPolicy()
	if p.Initial <= 0 {
		p.Initial = defaults.Initial
	}
	if p.Maximum <= 0 {
		p.Maximum = defaults.Maximum
	}
	if p.Multiplier < 1 || math.IsNaN(p.Multiplier) || math.IsInf(p.Multiplier, 0) {
		p.Multiplier = defaults.Multiplier
	}
	return p
}

func (p CooldownPolicy) delay(failures int) time.Duration {
	p = p.normalized()
	value := float64(p.Initial) * math.Pow(p.Multiplier, float64(max(0, failures-1)))
	if math.IsInf(value, 1) || value >= float64(p.Maximum) {
		return p.Maximum
	}
	return time.Duration(value)
}

type RuleDefaults struct {
	FallbackToDefault bool
}

func DefaultRuleDefaults() RuleDefaults {
	return RuleDefaults{FallbackToDefault: true}
}

// Only inherited values change. Reused turns keep their selected rule, and
// each admitted request keeps its resolved policy during recovery.
func applyRuleDefaults(p RoutingProfile, decision DispatchDecision, defaults *RuleDefaults) DispatchDecision {
	if defaults == nil || decision.Kind == "default" {
		return decision
	}
	for _, rule := range p.Rules {
		if rule.ID == decision.RuleID && rule.Kind == decision.Kind {
			decision.FallbackToDefault = boolDefault(rule.FallbackToDefault, defaults.FallbackToDefault)
			return decision
		}
	}
	return decision
}

func (r *PoolRuntime) SetCooldownPolicy(policy CooldownPolicy) {
	r.mu.Lock()
	r.config.CooldownPolicy = policy.normalized()
	r.mu.Unlock()
	if profile := r.ProfileRuntime(); profile != nil {
		profile.mu.Lock()
		profile.config.CooldownPolicy = policy.normalized()
		profile.mu.Unlock()
	}
}

func (r *ProfileRuntime) SetRuleDefaults(defaults RuleDefaults) {
	r.mu.Lock()
	defer r.mu.Unlock()
	config := r.config
	config.RuleDefaults = &defaults
	r.updateLocalFailureConfigLocked(&config)
	r.config = config
}
