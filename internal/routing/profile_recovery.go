package routing

import (
	"context"
	"strings"
	"time"
)

// NextRecovery waits only for a timed condition. Disabled resources, credential
// repair, unsupported models and unknown quota never create a retry loop.
func (r *ProfileRuntime) NextRecovery(plan *DispatchPlan, external func(DispatchCandidate) (time.Time, bool)) (time.Time, bool) {
	if plan == nil || plan.Preview || plan.state == nil || plan.state.runtime != r || !plan.Profile.WaitForRecovery || plan.Budget.Check() != nil {
		return time.Time{}, false
	}
	plan.state.mu.Lock()
	defer plan.state.mu.Unlock()
	if plan.state.reserved || plan.state.responseStarted || plan.state.successful || plan.state.stopped != "" {
		return time.Time{}, false
	}
	config := r.snapshot()
	now := r.now()
	var next time.Time
	for _, evaluation := range plan.Evaluations {
		if strings.HasPrefix(evaluation.Reason, "state_") || plan.state.stateRejected[evaluation.Candidate.Identity()] != "" {
			continue
		}
		previous := evaluation.Candidate
		candidate, exists := recoveryResource(config, previous)
		if !exists || !candidate.Enabled || candidate.Health.RepairRequired || config.Supports != nil && !config.Supports(candidate, candidate.Identity().Model) {
			continue
		}
		if hardFailure(plan.state.ledger, candidate.Identity()) {
			continue
		}
		var ready time.Time
		if candidate.Health.CoolingDown {
			if candidate.Health.Until.IsZero() {
				continue
			}
			ready = candidate.Health.Until
		}
		blockedUnknown := false
		for _, q := range candidate.Quota.Windows {
			// Projecting one window uses the same binding/model/confidence rules
			// as scheduler admission. Independent exhausted windows all recover.
			window := candidate
			window.Quota.Windows = []QuotaSnapshot{q}
			quota := applicableQuota(window, now)
			if quota.known && quota.used >= 100 {
				if q.ResetAt == nil || q.ResetAt.IsZero() {
					blockedUnknown = true
					break
				}
				if q.ResetAt.After(ready) {
					ready = *q.ResetAt
				}
			}
		}
		if blockedUnknown {
			continue
		}
		if external != nil {
			at, ok := external(candidate)
			if !ok {
				continue
			}
			if at.After(ready) {
				ready = at
			}
		}
		if ready.After(now) && (next.IsZero() || ready.Before(next)) {
			next = ready
		}
	}
	return next, !next.IsZero()
}

func recoveryResource(config RuntimeConfig, previous DispatchCandidate) (DispatchCandidate, bool) {
	for _, resource := range config.Candidates {
		if resource.Provider == previous.Provider && resource.ChannelID == previous.ChannelID && resource.GroupID == previous.GroupID && resource.EndpointID == previous.EndpointID {
			prepare := config.PrepareCandidate
			if prepare == nil {
				prepare = CopyCandidateForModel
			}
			resource = prepare(resource, previous.Model, previous.UpstreamModel)
			resource.ProfileID, resource.Weight, resource.Position = previous.ProfileID, previous.Weight, previous.Position
			return resource, true
		}
	}
	return DispatchCandidate{}, false
}

func hardFailure(ledger Ledger, candidate Candidate) bool {
	for key, failure := range ledger.failures {
		if failure.Scope == ScopeRequest || failure.Scope == ScopeCredential || failure.Scope == ScopeModel {
			copy := Ledger{failures: map[failureKey]Failure{key: failure}}
			if allowed, _ := copy.Allowed(candidate); !allowed {
				return true
			}
		}
	}
	return false
}

// Recover rechecks the original decision without classifying again or resetting
// the execution budget, selection cursor ownership, or deterministic failures.
func (r *ProfileRuntime) Recover(ctx context.Context, plan *DispatchPlan) bool {
	if ctx.Err() != nil || plan == nil || plan.Preview || plan.state == nil || plan.state.runtime != r || !plan.Profile.WaitForRecovery || plan.Budget.Check() != nil {
		return false
	}
	state := plan.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.reserved || state.responseStarted || state.successful || state.stopped != "" {
		return false
	}
	config := r.snapshot()
	_, exists := runtimeProfile(config, plan.Profile.ID)
	if !exists {
		return false
	}
	// The request keeps the policy it admitted with. Resource eligibility and
	// state compatibility are refreshed against the latest runtime snapshot.
	for key, failure := range state.ledger.failures {
		if failure.Scope == ScopeCandidate || failure.Scope == ScopeEndpoint {
			delete(state.ledger.failures, key)
		}
	}
	state.ledger.tried = nil
	state.appendEvent(DispatchTraceEvent{Action: "wait", Reason: "waiting_for_account_recovery"})
	r.populateDispatchPlan(ctx, config, plan, state.previous, r.now())
	return len(plan.EligibleCandidates) > 0
}
