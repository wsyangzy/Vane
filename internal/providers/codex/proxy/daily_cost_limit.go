package codexproxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var errCredentialGroupDailyCostLimitExceeded = errors.New("credential group daily cost limit exceeded")
var errRouteCandidateIneligible = errors.New("route candidates excluded by execution or state eligibility")

func (h *Handler) selectQuotaEligibleEndpoint(
	ctx context.Context,
	channelName string,
	failedIDs []int64,
	baseSkipped []credentialGroupRef,
	quotaSkipped []credentialGroupRef,
	primary bool,
) (*CodexEndpoint, []credentialGroupRef, error) {
	return h.selectQuotaEligibleEndpointForModel(ctx, channelName, "", failedIDs, baseSkipped, quotaSkipped, primary, nil)
}

// selectQuotaEligibleEndpointForModel stage 非空时只在路由阶段的渠道内选端点（不读写激活渠道），
// 否则按激活渠道的现有流程选端点。
func (h *Handler) selectQuotaEligibleEndpointForModel(
	ctx context.Context,
	channelName string,
	requestedModel string,
	failedIDs []int64,
	baseSkipped []credentialGroupRef,
	quotaSkipped []credentialGroupRef,
	primary bool,
	stage *codexRouteStage,
) (*CodexEndpoint, []credentialGroupRef, error) {
	blocked := false
	locallyExcluded := false
	localReason := ""
	for {
		skipped := make([]credentialGroupRef, 0, len(baseSkipped)+len(quotaSkipped))
		skipped = append(skipped, baseSkipped...)
		skipped = append(skipped, quotaSkipped...)

		var endpoint *CodexEndpoint
		var err error
		switch {
		case stage != nil:
			endpoint, err = h.manager.getEndpointForChannel(stage.channel.ID, stage.groupID, requestedModel, stage.upstreamModel, failedIDs, func(ep *CodexEndpoint) bool {
				return h.states != nil && h.states.Preferred(stage.turn, stage.candidate(ep, requestedModel))
			}, skipped...)
		case primary:
			endpoint, err = h.manager.GetPrimaryEndpointForModel(requestedModel, skipped...)
		default:
			endpoint, err = h.manager.GetNextEndpointForModel(requestedModel, failedIDs, skipped...)
		}
		if err != nil {
			if blocked {
				return nil, quotaSkipped, fmt.Errorf("%w: channel %s", errCredentialGroupDailyCostLimitExceeded, channelName)
			}
			if locallyExcluded {
				return nil, quotaSkipped, fmt.Errorf("%w: %s", errRouteCandidateIneligible, localReason)
			}
			return nil, quotaSkipped, err
		}
		if endpoint.DailyCostLimitUSD > 0 {
			usedUSD, _, usageErr := h.usageTracker.CredentialGroupDailyCost(ctx, "codex", channelName, endpoint.CredentialGroupName)
			if usageErr != nil || usedUSD >= endpoint.DailyCostLimitUSD {
				blocked = true
				group := credentialGroupRefFromEndpoint(endpoint)
				if !containsCredentialGroupRef(quotaSkipped, group) {
					quotaSkipped = append(quotaSkipped, group)
				}
				continue
			}
		}
		model := requestedModel
		if stage != nil {
			model = stage.candidate(endpoint, requestedModel).Model
		}

		if stage != nil && stage.execution != nil {
			candidate := stage.candidate(endpoint, requestedModel)
			if h.states != nil {
				if allowed, reason := h.states.Allowed(stage.stateKeys, candidate); !allowed {
					stage.execution.Skip(candidate, reason)
					locallyExcluded = true
					localReason = reason
					failedIDs = appendUniqueInt64(failedIDs, endpoint.ID)
					continue
				}
			}
			eligible, eligibilityReason := h.subscriptionEligibility(ctx, endpoint, model)
			if !eligible && !(stage.route != nil && stage.route.pool != nil && eligibilityReason == "subscription_paused") {
				stage.execution.Skip(candidate, eligibilityReason)
				locallyExcluded = true
				localReason = eligibilityReason
				failedIDs = appendUniqueInt64(failedIDs, endpoint.ID)
				continue
			}
			if stage.member != nil && stage.route != nil {
				if stage.native && endpoint.AuthMode != "client_passthrough" {
					stage.execution.Skip(candidate, "native_subscription_required")
					failedIDs = appendUniqueInt64(failedIDs, endpoint.ID)
					locallyExcluded = true
					continue
				}
				member := *stage.member
				member.UpstreamModel = candidate.Model
				member.Endpoint = candidate.Endpoint
				stage.route.waitStages = append(stage.route.waitStages, member)
				if !eligible {
					stage.execution.Skip(candidate, eligibilityReason)
					locallyExcluded = true
					localReason = eligibilityReason
					failedIDs = appendUniqueInt64(failedIDs, endpoint.ID)
					continue
				}
				if allowed, reason := stage.route.pool.Allowed(stage.channel.ID, endpoint.CredentialGroupID, candidate.Endpoint, candidate.Model); !allowed {
					stage.execution.Skip(candidate, reason)
					failedIDs = appendUniqueInt64(failedIDs, endpoint.ID)
					locallyExcluded = true
					localReason = reason
					continue
				}
			}
			if allowed, reason := stage.execution.Allowed(candidate); !allowed {
				stage.execution.Skip(candidate, reason)
				locallyExcluded = true
				localReason = reason
				failedIDs = appendUniqueInt64(failedIDs, endpoint.ID)
				continue
			}
		}
		if stage == nil || stage.execution == nil {
			if allowed, reason := h.subscriptionEligibility(ctx, endpoint, model); !allowed {
				if stage != nil && stage.execution != nil {
					stage.execution.Skip(stage.candidate(endpoint, requestedModel), reason)
				}
				locallyExcluded = true
				localReason = reason
				failedIDs = appendUniqueInt64(failedIDs, endpoint.ID)
				continue
			}
		}
		return endpoint, quotaSkipped, nil
	}
}

func containsCredentialGroupRef(groups []credentialGroupRef, target credentialGroupRef) bool {
	for _, group := range groups {
		if group == target {
			return true
		}
	}
	return false
}

func endpointCycleLastError(selectionErr error, forwardingErr error, status int) error {
	if forwardingErr != nil || status > 0 {
		return forwardingErr
	}
	return selectionErr
}

func quotaAwareStatus(err error, fallback int) int {
	if fallback > 0 {
		return fallback
	}
	if errors.Is(err, errCredentialGroupDailyCostLimitExceeded) {
		return http.StatusTooManyRequests
	}
	return fallback
}
