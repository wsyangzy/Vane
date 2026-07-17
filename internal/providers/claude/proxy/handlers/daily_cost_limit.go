package handlers

import (
	"context"
	"log/slog"

	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/tracking"
)

type credentialGroupDailyLimitDecision struct {
	eligible bool
}

func filterCredentialGroupDailyCostLimits(
	ctx context.Context,
	usageTracker *tracking.UsageTracker,
	endpoints []*endpoint.Endpoint,
) ([]*endpoint.Endpoint, bool) {
	if len(endpoints) == 0 {
		return endpoints, false
	}

	filtered := make([]*endpoint.Endpoint, 0, len(endpoints))
	decisions := make(map[string]credentialGroupDailyLimitDecision)
	blocked := false
	for _, candidate := range endpoints {
		if candidate == nil || candidate.Config.DailyCostLimitUSD <= 0 {
			filtered = append(filtered, candidate)
			continue
		}

		channel := endpointRouteName(candidate)
		groupName := candidate.Config.CredentialGroupName
		key := channel + "\x00" + groupName
		decision, ok := decisions[key]
		if !ok {
			usedUSD, _, err := usageTracker.CredentialGroupDailyCost(ctx, "claude", channel, groupName)
			decision.eligible = err == nil && usedUSD < candidate.Config.DailyCostLimitUSD
			decisions[key] = decision
			if err != nil {
				slog.Warn("[Claude 凭证分组限额查询失败，跳过分组]",
					"provider", "claude",
					"channel", channel,
					"credential_group", groupName,
					"limit_usd", candidate.Config.DailyCostLimitUSD,
					"error", err,
				)
			} else if !decision.eligible {
				slog.Info("[Claude 凭证分组已达每日成本限额]",
					"provider", "claude",
					"channel", channel,
					"credential_group", groupName,
					"used_usd", usedUSD,
					"limit_usd", candidate.Config.DailyCostLimitUSD,
				)
			}
		}
		if decision.eligible {
			filtered = append(filtered, candidate)
		} else {
			blocked = true
		}
	}
	return filtered, blocked
}
