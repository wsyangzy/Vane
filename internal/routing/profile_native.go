package routing

import "context"

// PlanNative uses the profile's members and scheduler without applying text
// generation rules, model overrides, intent classification or turn affinity.
// The provider adapter prepares protocol-specific credentials and capabilities.
func (r *ProfileRuntime) PlanNative(ctx context.Context, profileID int64, model, protocol string) DispatchPlan {
	return r.plan(ctx, profileID, model, false, TurnInput{}, false, &nativeDispatchPolicy{protocol: protocol})
}

type nativeDispatchPolicy struct {
	protocol string
}

func (p *nativeDispatchPolicy) apply(config RuntimeConfig) RuntimeConfig {
	if p == nil {
		return config
	}
	config.Profiles = append([]RoutingProfile(nil), config.Profiles...)
	for i := range config.Profiles {
		config.Profiles[i].Rules = nil
		config.Profiles[i].Intents = nil
		config.Profiles[i].Classifier = nil
		config.Profiles[i].AffinityMode = AffinityNone
	}
	config.RuleDefaults = nil
	prepare := config.PrepareCandidate
	config.PrepareCandidate = func(c DispatchCandidate, model, _ string) DispatchCandidate {
		c.Model, c.UpstreamModel, c.Protocol = model, "", p.protocol
		if prepare != nil {
			c = prepare(c, model, "")
		}
		c.Model, c.UpstreamModel, c.Protocol = model, "", p.protocol
		return c
	}
	return config
}
