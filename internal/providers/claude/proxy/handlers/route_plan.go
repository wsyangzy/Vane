package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"ai-relay-forwarder/internal/bodyrewrite"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/routing"
	"ai-relay-forwarder/internal/tracking"
)

// claudeGenerationPath 请求级路由只作用于生成路径；count_tokens 等路径不参与模型规则与非流改道
const claudeGenerationPath = "/v1/messages"

// routeRun 一次请求的路由执行状态，只在本次请求内有效。
//
// 命中路由的请求按阶段依次尝试，每个阶段只在自己的渠道（可限定凭证分组）内换端点，
// 不调用任何跨渠道全局故障转移，也不读写全局激活渠道；阶段全部失败即结束。
type routeRun struct {
	subscription bool
	ledger       routing.Ledger
	budget       *routing.Budget
	turn         string
	onUpdate     func(string)
	refresh      func()
	lastResponse error
	kind         routing.Kind
	snapshot     routing.Snapshot
	stages       []routeStage
	index        int
	// quotaBlocked 有阶段因凭证分组每日限额被整体跳过；一次都没发出请求时据此回 429
	quotaBlocked bool
}

func (run *routeRun) publishSnapshot() {
	if run != nil && run.onUpdate != nil {
		run.onUpdate(run.stageSnapshotJSON())
	}
}

// routeStage 执行阶段：channel 为运行时渠道名，groupID 为 0 表示该渠道全部凭证分组
type routeStage struct {
	channel       string
	groupID       int64
	upstreamModel string
	index         int // route_snapshot.target：0 为来源渠道
}

func (run *routeRun) current() *routeStage {
	if run == nil || run.index >= len(run.stages) {
		return nil
	}
	return &run.stages[run.index]
}

// advance 进入下一阶段；没有下一阶段时返回 false
func (run *routeRun) advance() bool {
	if run == nil {
		return false
	}
	run.index++
	return run.index < len(run.stages)
}

func (run *routeRun) upstreamModel() string {
	if stage := run.current(); stage != nil {
		return stage.upstreamModel
	}
	return ""
}

func (run *routeRun) stageSnapshotJSON() string {
	if run.refresh != nil {
		run.refresh()
	}
	stage := run.current()
	if stage == nil && len(run.stages) > 0 {
		stage = &run.stages[len(run.stages)-1]
	}
	if stage == nil {
		return ""
	}
	run.snapshot.Execution = &run.ledger
	run.snapshot.Attempts, run.snapshot.WaitMs = run.budget.Stats()
	if run.budget.Stopped() {
		run.snapshot.StopReason = "retry_budget_exhausted"
	}
	return run.snapshot.JSON(stage.index)
}

// terminalFailure 全部阶段失败时的 failure_reason 与兜底状态码。
// 有上游响应时由调用方透传最后一次真实响应；一次都没发出请求说明路由配置当前无法服务这条请求，
// 回 400 而不是 5xx：Claude Code 会按 CLAUDE_CODE_MAX_RETRIES 重试 5xx，把一次调用放大成十几条记录。
func (run *routeRun) terminalFailure(lastErr error) (string, int) {
	if lastErr != nil {
		return run.kind.ExhaustedReason(), http.StatusBadGateway
	}
	if run.quotaBlocked {
		return "daily_cost_limit_exceeded", http.StatusTooManyRequests
	}
	if reason := run.ledger.LastSkipReason(); strings.HasPrefix(reason, "state_") {
		return reason, http.StatusBadRequest
	}
	if reason := run.ledger.LastSkipReason(); strings.HasPrefix(reason, "subscription_") {
		return reason, http.StatusTooManyRequests
	}
	if run.kind == routing.KindModelCapability {
		return "no_supported_model_endpoint", http.StatusBadRequest
	}
	return run.kind.ExhaustedReason(), http.StatusBadRequest
}

// planClaudeRoute 在隐私脱敏之后做路由决策：模型规则 > 非流改道 > 默认流程。
// 未命中时返回 nil，请求按现有流程处理，行为不变。
func planClaudeRoute(em *endpoint.Manager, r *http.Request, body []byte, streaming bool, requestID string) *routeRun {
	if em == nil || r == nil || r.URL == nil || r.URL.Path != claudeGenerationPath {
		return nil
	}
	cfg := em.RouteConfig()
	if cfg == nil {
		return nil
	}
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	source := em.ActiveChannelName()
	// 只取激活渠道自己的规则：别的渠道配置的改道与本次请求无关
	decision := routing.Decide(cfg.Rules(source), cfg.NonStreamRoute(source), strings.TrimSpace(model), streaming)
	if !decision.Routed() {
		return nil
	}

	run := &routeRun{kind: decision.Kind, snapshot: routing.NewSnapshot(decision, source), turn: routing.ExplicitTurn(r.Context(), r.Header)}
	for _, stage := range decision.Stages() {
		if stage.Target == nil {
			if source != "" {
				run.stages = append(run.stages, routeStage{channel: source})
			}
			continue
		}
		channel := cfg.ChannelName(stage.Target.ChannelID)
		if channel == "" {
			slog.Warn("[Claude 路由目标渠道不存在，跳过]",
				"request_id", requestID,
				"kind", decision.Kind,
				"channel_id", stage.Target.ChannelID,
			)
			continue
		}
		run.stages = append(run.stages, routeStage{
			channel:       channel,
			groupID:       stage.Target.GroupID,
			upstreamModel: stage.Target.UpstreamModel,
			index:         stage.Index,
		})
	}

	attrs := []any{"request_id", requestID, "kind", decision.Kind, "mode", decision.Mode, "stages", len(run.stages), "source_channel", source}
	if decision.Rule != nil {
		attrs = append(attrs, "rule_id", decision.Rule.ID, "rule", decision.Rule.Name)
	}
	slog.Info("[Claude 请求级路由命中]", attrs...)
	return run
}

// planModelCapabilityRoute 激活渠道的端点都不支持请求模型时，按渠道优先级挑出有端点支持该模型的渠道，
// 只在本次请求内使用，不切换全局激活渠道。关闭渠道间故障转移时不借用其它渠道，保持原有的 400。
func planModelCapabilityRoute(forwarder *Forwarder, em *endpoint.Manager, body []byte, sourceChannel string, triedChannels map[string]struct{}, triedEndpoints map[string]struct{}) *routeRun {
	if em == nil || !em.IsFailoverEnabled() {
		return nil
	}
	run := &routeRun{
		kind:     routing.KindModelCapability,
		snapshot: routing.NewSnapshot(routing.Decision{Kind: routing.KindModelCapability}, sourceChannel),
	}
	excluded := stringSetKeys(triedEndpoints)
	for _, channel := range em.RoutableChannelsByPriority() {
		if channel == sourceChannel {
			continue
		}
		if _, tried := triedChannels[channel]; tried {
			continue
		}
		candidates := em.GetRoutableEndpointsForChannel(channel, 0, excluded)
		if len(filterClaudeModelEndpoints(forwarder, candidates, body)) == 0 {
			continue
		}
		run.stages = append(run.stages, routeStage{channel: channel, index: len(run.stages) + 1})
	}
	if len(run.stages) == 0 {
		return nil
	}
	return run
}

// routeStageEndpoints 当前阶段经过模型能力与凭证分组每日限额过滤后的候选端点
func routeStageEndpoints(ctx context.Context, forwarder *Forwarder, usageTracker *tracking.UsageTracker, em *endpoint.Manager, run *routeRun, body []byte, triedEndpoints map[string]struct{}) ([]*endpoint.Endpoint, bool) {
	defer run.publishSnapshot()
	run.observeSubscription(ctx, forwarder)
	stage := run.current()
	if stage == nil || em == nil {
		return nil, false
	}
	candidates := em.GetRoutableEndpointsForChannel(stage.channel, stage.groupID, nil)
	eligible := candidates[:0:0]
	for _, ep := range candidates {
		candidate := run.candidate(forwarder, ep, body)
		if allowed, reason := forwarder.subscriptionEligibility(ctx, ep, candidate.Model); !allowed {
			run.ledger.Skip(candidate, reason)
			continue
		}
		if forwarder.states != nil {
			if allowed, reason := forwarder.states.Allowed(claudeStateKeys(ctx, body), candidate); !allowed {
				run.ledger.Skip(candidate, reason)
				continue
			}
		}
		if allowed, reason := run.ledger.Allowed(candidate); allowed {
			eligible = append(eligible, ep)
		} else {
			run.ledger.Skip(candidate, reason)
		}
	}
	candidates = eligible
	if forwarder.states != nil {
		turn := run.turn
		sort.SliceStable(candidates, func(i, j int) bool {
			return forwarder.states.Preferred(turn, run.candidate(forwarder, candidates[i], body)) && !forwarder.states.Preferred(turn, run.candidate(forwarder, candidates[j], body))
		})
	}
	var capable []*endpoint.Endpoint
	if stage.upstreamModel != "" {
		// 目标强制改写了上游模型：分组映射不再生效，按改写后的模型核对分组能力声明
		capable = filterEndpointsDeclaringModel(candidates, stage.upstreamModel)
	} else {
		capable = filterClaudeModelEndpoints(forwarder, candidates, body)
	}
	return filterCredentialGroupDailyCostLimits(ctx, usageTracker, capable)
}

func filterEndpointsDeclaringModel(endpoints []*endpoint.Endpoint, model string) []*endpoint.Endpoint {
	filtered := endpoints[:0:0]
	for _, ep := range endpoints {
		if ep == nil {
			continue
		}
		if len(ep.Config.SupportedModels) == 0 {
			filtered = append(filtered, ep)
			continue
		}
		for _, declared := range ep.Config.SupportedModels {
			if routing.MatchSupportedModel(declared, model) {
				filtered = append(filtered, ep)
				break
			}
		}
	}
	return filtered
}

// rewriteBodyForRoute 目标声明了上游模型时强制改写 model，优先级高于分组的模型映射；
// 否则沿用端点所属分组的模型策略。每次都从原始 body 重建。
func rewriteBodyForRoute(forwarder *Forwarder, lifecycleManager RequestLifecycleManager, body []byte, ep *endpoint.Endpoint, run *routeRun) []byte {
	upstreamModel := run.upstreamModel()
	if upstreamModel == "" {
		return rewriteBodyForEndpoint(forwarder, lifecycleManager, body, ep)
	}
	newBody, originalModel, ok := bodyrewrite.ReplaceTopLevelString(body, "model", upstreamModel)
	if !ok {
		return body
	}
	if originalModel != upstreamModel && lifecycleManager != nil {
		slog.Info("[Claude model 改写]",
			"request_id", lifecycleManager.GetRequestID(),
			"channel", endpointRouteName(ep),
			"original_model", originalModel,
			"new_model", upstreamModel,
			"rewrite_source", "route",
		)
		lifecycleManager.SetModelRewrite(originalModel, upstreamModel, "route")
	}
	return newBody
}

// checkRouteBudget 路由阶段之间共享全局重试预算，每次尝试前检查
func checkRouteBudget(run *routeRun, lifecycleManager RequestLifecycleManager) (bool, string) {
	if run == nil || lifecycleManager == nil {
		return false, ""
	}
	exhausted, reason := lifecycleManager.CheckRetryBudget()
	if exhausted {
		run.budget.Stop()
		run.publishSnapshot()
	}
	return exhausted, reason
}
