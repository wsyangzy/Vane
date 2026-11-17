package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/routing"
)

// DispatchRecoveryInput identifies resources only. Checking or reopening a gate
// never replays a request, changes the active group, or transmits a probe.
type DispatchRecoveryInput struct {
	Provider   string `json:"provider"`
	ChannelID  int64  `json:"channel_id"`
	GroupID    int64  `json:"group_id"`
	EndpointID string `json:"endpoint_id"`
	Model      string `json:"model"`
}

type DispatchCandidateRecovery struct {
	Blockage *routing.Blockage `json:"blockage"`
	CanRetry bool              `json:"can_retry"`
}

func healthRetryReason(reason string) bool {
	switch reason {
	case "cooldown", "cooling_down", "account_cooling_down", "account_repair_required", "credential_repair_required":
		return true
	}
	return false
}

func (a *App) dispatchRecovery(ctx context.Context, input DispatchRecoveryInput) (*routing.PoolRuntime, []routing.CandidateEvaluation, error) {
	provider, err := normalizeProviderParam(input.Provider)
	if err != nil {
		return nil, nil, err
	}
	if input.ChannelID <= 0 || input.GroupID <= 0 {
		return nil, nil, errors.New("请先选择有效的渠道和凭证分组")
	}
	pool, err := a.refreshPoolRuntime(ctx, provider)
	if err != nil {
		return nil, nil, err
	}
	runtime := pool.ProfileRuntime()
	if runtime == nil {
		return nil, nil, errors.New("调度运行时尚未就绪")
	}
	profile := routing.DefaultProfile(provider)
	evaluations := runtime.PreviewTargets(ctx, profile, []routing.RouteTarget{{ChannelID: input.ChannelID, GroupID: input.GroupID, UpstreamModel: input.Model}}, input.Model)
	selected := []routing.CandidateEvaluation{}
	for _, evaluation := range evaluations {
		if input.EndpointID == "" || evaluation.Candidate.EndpointID == input.EndpointID {
			if evaluation.Eligible || healthRetryReason(evaluation.Reason) {
				if reason := a.dispatchManagedAccountAdmission(ctx, pool, evaluation.Candidate); reason != "" {
					evaluation.Eligible, evaluation.Reason = false, reason
					evaluation.Blockage = a.dispatchBlockage(pool, evaluation.Candidate, reason)
				}
			}
			selected = append(selected, evaluation)
		}
	}
	if len(selected) == 0 {
		return nil, nil, errors.New("此渠道、分组或端点已不存在，请检查当前渠道资源")
	}
	return pool, selected, nil
}

// Read account metadata only. Refreshing credentials can contact the provider
// and belongs to the existing account repair flow, not a status query.
func (a *App) dispatchManagedAccountAdmission(ctx context.Context, pool *routing.PoolRuntime, candidate routing.DispatchCandidate) string {
	account, exists := pool.Account(candidate.ChannelID, candidate.GroupID)
	if !exists || !account.Managed {
		return ""
	}
	if a.subscriptionAccounts == nil {
		return "subscription_state_unavailable"
	}
	infos, err := a.subscriptionAccounts.List(ctx, candidate.Provider)
	if err != nil {
		return "subscription_state_unavailable"
	}
	for _, info := range infos {
		if info.ID != strings.TrimPrefix(account.Identity, "account/") {
			continue
		}
		if !info.Enabled {
			return "subscription_account_disabled"
		}
		if info.Status == "sign_in_required" {
			return "subscription_authentication_required"
		}
		return ""
	}
	return "subscription_account_unavailable"
}

func recoveryRetryAllowed(evaluation routing.CandidateEvaluation) bool {
	if !healthRetryReason(evaluation.Reason) || evaluation.Blockage == nil || evaluation.Blockage.Recovery == "probing" {
		return false
	}
	// Health is only one admission condition. Removing it must not bypass a
	// missing credential, exhausted quota/balance or a concurrency restriction.
	candidate := evaluation.Candidate
	candidate.Health = routing.HealthState{}
	candidate.Load = evaluation.Load
	checks := routing.NewScheduler().Preview(routing.DefaultProfile(candidate.Provider), []routing.DispatchCandidate{candidate}, nil, time.Now())
	return len(checks) == 1 && checks[0].Eligible
}

func (a *App) GetDispatchRecovery(input DispatchRecoveryInput) ([]DispatchCandidateRecovery, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	pool, evaluations, err := a.dispatchRecovery(ctx, input)
	if err != nil {
		return nil, err
	}
	result := make([]DispatchCandidateRecovery, 0, len(evaluations))
	for _, evaluation := range evaluations {
		state := evaluation.Blockage
		if state == nil {
			reason := evaluation.Reason
			if evaluation.Eligible {
				// Eligible ordering may explain a quota fallback. That is not a
				// resource admission failure or an unknown recovery condition.
				reason = ""
			}
			state = a.dispatchBlockage(pool, evaluation.Candidate, reason)
		}
		result = append(result, DispatchCandidateRecovery{Blockage: state, CanRetry: recoveryRetryAllowed(evaluation)})
	}
	return result, nil
}

func (a *App) RetryDispatchCandidate(input DispatchRecoveryInput) ([]DispatchCandidateRecovery, error) {
	if input.EndpointID == "" {
		return nil, errors.New("请选择需要重新尝试的端点")
	}
	ctx, cancel := routeCtx()
	defer cancel()
	pool, evaluations, err := a.dispatchRecovery(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(evaluations) != 1 || !recoveryRetryAllowed(evaluations[0]) {
		return nil, errors.New("当前无法解除此阻塞，请刷新状态并按提示处理限额、余额、凭证或正在验证的请求")
	}
	candidate := evaluations[0].Candidate
	if err := pool.RequestRetry(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model); err != nil {
		return nil, err
	}
	a.emitPoolHealthUpdate(candidate.Provider)
	return a.GetDispatchRecovery(input)
}

func (a *App) dispatchResource(c routing.DispatchCandidate) (channel, group, endpoint string, limit float64) {
	if c.Provider == "codex" && a.codexEndpointMgr != nil {
		for _, ep := range a.codexEndpointMgr.PoolEndpoints(c.ChannelID, c.GroupID) {
			if strconv.FormatInt(ep.ID, 10) == c.EndpointID {
				if ch := a.codexEndpointMgr.ChannelByID(c.ChannelID); ch != nil {
					channel = ch.Name
				}
				return channel, ep.CredentialGroupName, ep.Name, ep.DailyCostLimitUSD
			}
		}
	} else if c.Provider == "claude" && a.endpointManager != nil {
		for _, ep := range a.endpointManager.GetAllEndpoints() {
			if ep.Config.CredentialGroupID == c.GroupID && claudeendpoint.EndpointKeyForConfig(ep.Config) == c.EndpointID {
				return claudeendpoint.ChannelKey(ep), ep.Config.CredentialGroupName, ep.Config.Name, ep.Config.DailyCostLimitUSD
			}
		}
	}
	return
}

func (a *App) dispatchBlockage(pool *routing.PoolRuntime, c routing.DispatchCandidate, reason string) *routing.Blockage {
	channel, group, endpoint, limit := a.dispatchResource(c)
	state := &routing.Blockage{Provider: c.Provider, ChannelID: c.ChannelID, GroupID: c.GroupID, EndpointID: c.EndpointID,
		ChannelName: channel, GroupName: group, EndpointName: endpoint, Model: c.Identity().Model,
		Reason: reason, Recovery: "unknown", ObservedAt: time.Now()}
	if reason == "" {
		state.Recovery = "ready"
	}
	switch reason {
	case "account_disabled", "candidate_disabled", "account_missing", "credential_missing", "model_unsupported", "model_unavailable", "protocol_incompatible", "state_backend_incompatible", "state_origin_unknown", "manual_balance_exhausted", "profile_disabled", "subscription_account_disabled", "subscription_account_unavailable", "subscription_binding_unavailable", "subscription_authentication_required", "subscription_authentication_rejected", "subscription_sign_in_required", "subscription_account_header_rejected":
		state.Recovery = "manual"
	case "daily_cost_limit_exceeded", "daily_cost_state_unavailable":
		state.DailyLimitUSD = &limit
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		used, reset, err := a.usageTracker.CredentialGroupDailyCost(ctx, c.Provider, channel, group)
		if err == nil {
			state.DailyUsedUSD = &used
			if reason == "daily_cost_limit_exceeded" {
				state.RecoverAt = &reset
				state.Recovery = "automatic"
			}
		} else {
			state.Recovery = "unknown"
		}
	case "cooldown", "cooling_down", "account_cooling_down", "account_repair_required", "credential_repair_required", "account_probe_in_progress":
		if pool == nil {
			state.Recovery = "unknown"
			break
		}
		account, _ := pool.Account(c.ChannelID, c.GroupID)
		for _, cooldown := range pool.Cooldowns() {
			if cooldown.Identity != account.Identity || cooldown.Endpoint != "" && cooldown.Endpoint != c.EndpointID || cooldown.Model != "" && cooldown.Model != c.Identity().Model {
				continue
			}
			state.Cause = cooldown.Reason
			if cooldown.Repair {
				state.Recovery, state.RecoverAt = "manual", nil
				break
			}
			if cooldown.Probing {
				state.Recovery, state.RecoverAt = "probing", nil
				break
			}
			state.Recovery = "automatic"
			if state.RecoverAt == nil || cooldown.Until.After(*state.RecoverAt) {
				at := cooldown.Until
				state.RecoverAt = &at
			}
		}
	case "quota_exhausted":
		if at, known := routing.QuotaRecoveryAt(c, state.ObservedAt); known {
			state.RecoverAt, state.Recovery = &at, "automatic"
		}
	case "concurrency_limit", "candidate_busy":
		state.Recovery = "pending"
	case "manual_balance_unavailable":
		state.Recovery = "unknown"
	default:
		// A failed attempt can be the event that creates a health gate. Save
		// that gate alongside its trigger, even when no later candidate skips it.
		if reason != "" && pool != nil {
			if allowed, gate := pool.HealthAllowed(c.ChannelID, c.GroupID, c.EndpointID, c.Identity().Model); !allowed && gate != reason {
				return a.dispatchBlockage(pool, c, gate)
			}
		}
	}
	return state
}
