package codexproxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var errCredentialGroupDailyCostLimitExceeded = errors.New("credential group daily cost limit exceeded")

func (h *Handler) selectQuotaEligibleEndpoint(
	ctx context.Context,
	channelName string,
	failedIDs []int64,
	baseSkipped []credentialGroupRef,
	quotaSkipped []credentialGroupRef,
	primary bool,
) (*CodexEndpoint, []credentialGroupRef, error) {
	blocked := false
	for {
		skipped := make([]credentialGroupRef, 0, len(baseSkipped)+len(quotaSkipped))
		skipped = append(skipped, baseSkipped...)
		skipped = append(skipped, quotaSkipped...)

		var endpoint *CodexEndpoint
		var err error
		if primary {
			endpoint, err = h.manager.GetPrimaryEndpoint(skipped...)
		} else {
			endpoint, err = h.manager.GetNextEndpoint(failedIDs, skipped...)
		}
		if err != nil {
			if blocked {
				return nil, quotaSkipped, fmt.Errorf("%w: channel %s", errCredentialGroupDailyCostLimitExceeded, channelName)
			}
			return nil, quotaSkipped, err
		}
		if endpoint.DailyCostLimitUSD <= 0 {
			return endpoint, quotaSkipped, nil
		}

		usedUSD, _, usageErr := h.usageTracker.CredentialGroupDailyCost(
			ctx,
			"codex",
			channelName,
			endpoint.CredentialGroupName,
		)
		if usageErr == nil && usedUSD < endpoint.DailyCostLimitUSD {
			return endpoint, quotaSkipped, nil
		}

		blocked = true
		group := credentialGroupRefFromEndpoint(endpoint)
		if !containsCredentialGroupRef(quotaSkipped, group) {
			quotaSkipped = append(quotaSkipped, group)
		}
		if usageErr != nil {
			h.logger.Warn("[Codex 凭证分组限额查询失败，跳过分组]",
				"provider", "codex",
				"channel", channelName,
				"credential_group", endpoint.CredentialGroupName,
				"limit_usd", endpoint.DailyCostLimitUSD,
				"error", usageErr,
			)
		} else {
			h.logger.Info("[Codex 凭证分组已达每日成本限额]",
				"provider", "codex",
				"channel", channelName,
				"credential_group", endpoint.CredentialGroupName,
				"used_usd", usedUSD,
				"limit_usd", endpoint.DailyCostLimitUSD,
			)
		}
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
