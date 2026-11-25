package routing

import (
	"context"
	"strings"
	"time"
)

type profileRecoveryState struct {
	next    time.Time
	ready   bool
	probing bool
}

// NextRecovery reports only a known recovery time. An in-flight health probe
// has no known completion time and is handled by WaitForRecovery instead.
func (r *ProfileRuntime) NextRecovery(plan *DispatchPlan, external func(DispatchCandidate) (time.Time, bool)) (time.Time, bool) {
	state := r.recoveryState(plan, external)
	return state.next, !state.next.IsZero()
}

func (r *ProfileRuntime) recoveryState(plan *DispatchPlan, external func(DispatchCandidate) (time.Time, bool)) profileRecoveryState {
	var result profileRecoveryState
	if plan == nil || plan.Preview || plan.state == nil || plan.state.runtime != r || !plan.Profile.WaitForRecovery || plan.Budget.Check() != nil {
		return result
	}
	plan.state.mu.Lock()
	defer plan.state.mu.Unlock()
	if plan.state.reserved || plan.state.responseStarted || plan.state.successful || plan.state.stopped != "" {
		return result
	}
	config := plan.state.native.apply(r.snapshot())
	if _, exists := runtimeProfile(config, plan.Profile.ID); !exists {
		return result
	}
	now := r.now()
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
		if candidate.Balance.Exhausted() || evaluation.Blockage != nil && (evaluation.Blockage.Recovery == "manual" || evaluation.Blockage.Recovery == "unknown") {
			continue
		}
		var ready time.Time
		// Health is refreshed by PrepareCandidate and the provider callback.
		// A captured cooldown deadline may already have been cleared or extended.
		if evaluation.Blockage != nil && evaluation.Blockage.Recovery == "automatic" && evaluation.Blockage.RecoverAt != nil && evaluation.Reason != "cooldown" && evaluation.Reason != "account_cooling_down" {
			ready = *evaluation.Blockage.RecoverAt
		}
		probing := candidate.Health.Reason == "account_probe_in_progress"
		if candidate.Health.CoolingDown && !probing {
			if candidate.Health.Until.IsZero() {
				continue
			}
			if candidate.Health.Until.After(ready) {
				ready = candidate.Health.Until
			}
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
			if !ok && !probing {
				continue
			}
			if ok {
				probing = false
				if at.After(ready) {
					ready = at
				}
			}
		}
		if ready.After(now) {
			if result.next.IsZero() || ready.Before(result.next) {
				result.next = ready
			}
		} else if probing {
			result.probing = true
		} else if !evaluation.Eligible || !ready.IsZero() {
			// Recheck an opened gate, rather than replaying an already-tried
			// candidate with no evidence of a recoverable condition.
			result.ready = true
		}
	}
	return result
}

// WaitForRecovery keeps the admitted request while timed health/quota gates or
// another request's recovery probe can still open within the shared budget.
// Probe polling uses Wait, so it never fabricates an endpoint recovery time.
func (r *ProfileRuntime) WaitForRecovery(ctx context.Context, plan *DispatchPlan, external func(DispatchCandidate) (time.Time, bool)) (bool, error) {
	if plan == nil || plan.Preview || plan.state == nil || plan.state.runtime != r || !plan.Profile.WaitForRecovery {
		return false, nil
	}
	waited, recorded, rechecked := false, false, false
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := plan.Budget.Check(); err != nil {
			return false, err
		}
		state := r.recoveryState(plan, external)
		if (waited || !rechecked) && state.ready || waited && !state.probing && state.next.IsZero() {
			rechecked = true
			if r.Recover(ctx, plan) {
				return true, nil
			}
			waited = false
			continue
		}
		if !state.probing && state.next.IsZero() {
			return false, nil
		}
		if !recorded {
			plan.state.mu.Lock()
			plan.state.appendEvent(DispatchTraceEvent{Action: "wait", Reason: "waiting_for_account_recovery"})
			plan.state.mu.Unlock()
			recorded = true
		}
		var err error
		if state.probing {
			delay := 100 * time.Millisecond
			if !state.next.IsZero() && time.Until(state.next) < delay {
				delay = time.Until(state.next)
			}
			err = plan.Budget.Wait(ctx, delay)
		} else {
			err = plan.Budget.WaitRecovery(ctx, time.Until(state.next))
		}
		if err != nil {
			return false, err
		}
		waited = true
	}
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
	config := state.native.apply(r.snapshot())
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
	r.populateDispatchPlan(ctx, config, plan, state.previous, r.now())
	return len(plan.EligibleCandidates) > 0
}
