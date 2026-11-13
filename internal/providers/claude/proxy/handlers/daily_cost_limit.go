package handlers

import (
	"context"
	"log/slog"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/tracking"
)

type credentialGroupDailyLimitDecision struct {
	eligible bool
	reason   string
}

func filterCredentialGroupDailyCostLimits(
	ctx context.Context,
	usageTracker *tracking.UsageTracker,
	endpoints []*endpoint.Endpoint,
	onSkip ...func(*endpoint.Endpoint, string),
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
			if err != nil {
				decision.reason = "daily_cost_state_unavailable"
				slog.Warn("[Claude 凭证分组限额查询失败，跳过分组]",
					"provider", "claude",
					"channel", channel,
					"credential_group", groupName,
					"limit_usd", candidate.Config.DailyCostLimitUSD,
					"error", err,
				)
			} else if !decision.eligible {
				decision.reason = "daily_cost_limit_exceeded"
				slog.Info("[Claude 凭证分组已达每日成本限额]",
					"provider", "claude",
					"channel", channel,
					"credential_group", groupName,
					"used_usd", usedUSD,
					"limit_usd", candidate.Config.DailyCostLimitUSD,
				)
			}
			decisions[key] = decision
		}
		if decision.eligible {
			filtered = append(filtered, candidate)
		} else {
			blocked = true
			for _, skipped := range onSkip {
				skipped(candidate, decision.reason)
			}
		}
	}
	return filtered, blocked
}
