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

// Only inherited values change. Reused turns keep their selected rule and
// intent, and each admitted request keeps its resolved policy during recovery.
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
	if decision.Kind == "intent" {
		for _, intent := range p.Intents {
			if intent.ID == decision.IntentID {
				decision.FallbackToDefault = boolDefault(intent.FallbackToDefault, defaults.FallbackToDefault)
			}
		}
		decision.FallbackIntents = append([]IntentRoute(nil), decision.FallbackIntents...)
		for i := range decision.FallbackIntents {
			for _, intent := range p.Intents {
				if intent.ID == decision.FallbackIntents[i].IntentID {
					decision.FallbackIntents[i].FallbackToDefault = boolDefault(intent.FallbackToDefault, defaults.FallbackToDefault)
				}
			}
		}
	}
	return decision
}

func (r *PoolRuntime) SetCooldownPolicy(policy CooldownPolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config.CooldownPolicy = policy.normalized()
}

func (r *ProfileRuntime) SetRuleDefaults(defaults RuleDefaults) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config.RuleDefaults = &defaults
}
