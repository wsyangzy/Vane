package codexproxy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// codexRoute 一次请求的路由执行计划，只在本次请求内有效
type codexRoute struct {
	profile    *routing.DispatchPlan
	pool       *routing.PoolRuntime
	waitStages []routing.PoolStage
	kind       routing.Kind
	snapshot   routing.Snapshot
	stages     []codexRouteStage
}

// codexRouteStage 路由阶段：在 channel（groupID > 0 时只取该凭证分组）内跑一个端点周期
type codexRouteStage struct {
	member             *routing.PoolStage
	route              *codexRoute
	native             bool
	subscriptionStatus func() *routing.SubscriptionSnapshot
	execution          *routing.Ledger
	budget             *routing.Budget
	stateKeys          []string
	turn               string
	channel            *store.CodexChannelRecord
	groupID            int64
	upstreamModel      string
	index              int    // route_snapshot.target：0 为来源渠道
	snapshotJSON       string // 进入本阶段时写入 request_logs.route_snapshot
}

// isCodexGenerationPath 请求级路由只作用于生成路径
func isCodexGenerationPath(path string) bool {
	return path == "/v1/responses" || path == "/responses" || path == "/backend-api/codex/responses" || path == "/backend-api/codex/responses/compact" || path == "/v1/responses/compact" || path == "/responses/compact"
}

// planCodexRoute 在隐私脱敏之后做路由决策：模型规则 > 非流改道 > 默认流程。未命中返回 nil，行为不变。
func (h *Handler) planCodexRoute(r *http.Request, body []byte, streaming bool, requestID string) *codexRoute {
	if h.manager == nil || r == nil || r.URL == nil {
		return nil
	}
	if runtime := h.manager.PoolRuntime(); runtime != nil && runtime.ProfileRuntime() != nil {
		return h.planProfileRoute(r, body, streaming, requestID, runtime.ProfileRuntime())
	}
	if subscription.FromContext(r.Context()) != nil {
		return h.planSubscriptionRoute(r, body, streaming)
	}
	if runtime := h.manager.PoolRuntime(); runtime != nil {
		return h.planPoolRoute(r, body, streaming, runtime)
	}
	if !isCodexGenerationPath(r.URL.Path) {
		return nil
	}
	active := h.manager.GetActiveChannel()
	var nonStream *routing.NonStreamRoute
	var rules []routing.ModelRule
	sourceName := ""
	if active != nil {
		nonStream = active.NonStreamRoute
		sourceName = active.Name
		// 只取激活渠道自己的规则：别的渠道配置的改道与本次请求无关
		rules = h.manager.ModelRules(active.ID)
	}
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	decision := routing.Decide(rules, nonStream, strings.TrimSpace(model), streaming)
	if !decision.Routed() {
		return nil
	}

	route := &codexRoute{kind: decision.Kind, snapshot: routing.NewSnapshot(decision, sourceName)}
	for _, stage := range decision.Stages() {
		if stage.Target == nil {
			if active != nil {
				route.stages = append(route.stages, codexRouteStage{channel: active, snapshotJSON: route.snapshot.JSON(0)})
			}
			continue
		}
		channel := h.manager.ChannelByID(stage.Target.ChannelID)
		if channel == nil {
			h.logger.Warn("[Codex 路由目标渠道不存在，跳过]", "request_id", requestID, "kind", decision.Kind, "channel_id", stage.Target.ChannelID)
			continue
		}
		route.stages = append(route.stages, codexRouteStage{
			channel:       channel,
			groupID:       stage.Target.GroupID,
			upstreamModel: stage.Target.UpstreamModel,
			index:         stage.Index,
			snapshotJSON:  route.snapshot.JSON(stage.Index),
		})
	}

	attrs := []any{"provider", "codex", "request_id", requestID, "kind", decision.Kind, "mode", decision.Mode, "stages", len(route.stages), "source_channel", sourceName}
	if decision.Rule != nil {
		attrs = append(attrs, "rule_id", decision.Rule.ID, "rule", decision.Rule.Name)
	}
	h.logger.Info("[Codex 请求级路由命中]", attrs...)
	return route
}

// serveRouted 按阶段依次尝试：每个阶段只跑一个端点周期，不进入 loop_retry 轮次与挂起等待，
// 也不触发渠道间故障转移——全局激活渠道全程不读写。
func (h *Handler) serveRouted(w http.ResponseWriter, r *http.Request, body []byte, requestID string, startTime time.Time, isStreaming bool, route *codexRoute) {
	if route.profile != nil {
		h.serveProfileRouted(w, r, body, requestID, startTime, isStreaming, route.profile)
		return
	}
	ledger := &routing.Ledger{}
	cfg := h.retryConfig()
	attempts, duration := cfg.MaxTotalAttempts, cfg.MaxTotalDuration
	if route.pool != nil {
		if attempts <= 0 {
			attempts = 10
		}
		if duration <= 0 {
			duration = 60 * time.Second
		}
	}
	budget := routing.NewBudget(attempts, duration)
	r = r.WithContext(routing.WithBudget(r.Context(), budget))
	var currentStage *codexRouteStage
	defer func() {
		if currentStage != nil {
			h.recordRouteSnapshot(requestID, currentStage.executionSnapshot())
		}
	}()
	var endpointChain []chainNode
	attemptCount := 0
	var lastErr error
	var lastSummary string
	var lastUpstreamStatus int
	var lastUpstreamHeaders http.Header
	var lastUpstreamBody []byte
	quotaBlocked := false
	requestModel := h.tokenParser.ParseModelFromBody(body)

	for {
		for i := range route.stages {
			stage := &route.stages[i]
			stage.execution, stage.budget = ledger, budget
			ledger.Member = stage.member
			stage.stateKeys = codexStateKeys(r, body)
			stage.turn = routing.ExplicitTurn(r.Context(), r.Header)
			currentStage = stage
			var failedIDs []int64
			if err := r.Context().Err(); err != nil {
				h.recordRouteSnapshot(requestID, stage.executionSnapshot())
				h.recordFailure(requestID, startTime, "client_cancelled", "client_canceled", nil, requestModel)
				return
			}
			if err := budget.Check(); err != nil {
				budget.Stop()
				lastErr = err
				break
			}
			result := h.tryEndpointCycleForStage(
				w, r, body, requestID, startTime,
				isStreaming, stage.channel.Name, requestModel,
				&failedIDs, &endpointChain, &attemptCount, stage,
			)
			if result.completed {
				return
			}
			lastErr = result.lastErr
			lastSummary = result.lastSummary
			if errors.Is(result.lastErr, errCredentialGroupDailyCostLimitExceeded) {
				quotaBlocked = true
			}
			if result.lastUpstreamStatus > 0 {
				lastUpstreamStatus = result.lastUpstreamStatus
				lastUpstreamHeaders = result.lastUpstreamHeaders
				lastUpstreamBody = result.lastUpstreamBody
			}
			h.logger.Info("[Codex 路由阶段失败]", "request_id", requestID, "kind", route.kind, "target", stage.index, "channel", stage.channel.Name)
		}
		if route.pool == nil {
			break
		}
		if next, ok := route.pool.NextRecoveryWith(route.waitStages, requestModel, func(member routing.PoolStage) (time.Time, bool) { return h.poolRecovery(r.Context(), member) }); ok && budget.Check() == nil {
			ledger.Wait("waiting_for_account_recovery")
			if currentStage != nil {
				h.recordRouteSnapshot(requestID, currentStage.executionSnapshot())
			}
			if err := budget.Wait(r.Context(), time.Until(next)); err == nil {
				ledger.RetryRecovered()
				route.waitStages = nil
				continue
			} else {
				lastErr = err
			}
		}
		break
	}

	failureReason := route.kind.ExhaustedReason()
	if currentStage != nil {
		h.recordRouteSnapshot(requestID, currentStage.executionSnapshot())
	}
	if errors.Is(lastErr, routing.ErrBudgetExhausted) {
		failureReason = "retry_budget_exhausted"
	}
	detail := lastSummary
	if strings.TrimSpace(detail) == "" && lastErr != nil {
		detail = lastErr.Error()
	}
	if len(endpointChain) == 0 {
		if reason := ledger.LastSkipReason(); strings.HasPrefix(reason, "state_") {
			failureReason, detail = reason, reason
		}
	}
	h.logger.Error("[Codex 请求失败]",
		"provider", "codex",
		"request_id", requestID,
		"failure_reason", failureReason,
		"detail", detail,
		"duration_ms", time.Since(startTime).Milliseconds(),
		"endpoints_tried", len(endpointChain),
		"route_kind", route.kind,
	)
	if lastUpstreamStatus > 0 {
		// 有真实上游响应：原样透传最后一次响应，追踪记它的真实状态码
		if message := strings.TrimSpace(string(lastUpstreamBody)); message != "" {
			detail = message
		}
		h.recordUpstreamFailure(requestID, startTime, failureReason, detail, lastUpstreamStatus, nil, requestModel)
		h.passthroughWithBody(w, &http.Response{
			StatusCode: lastUpstreamStatus,
			Header:     lastUpstreamHeaders,
			Body:       io.NopCloser(bytes.NewReader(lastUpstreamBody)),
		}, lastUpstreamBody)
		return
	}

	// 上游从未给出 HTTP 响应：追踪不记状态码，给客户端的码只是协议必需
	statusCode := http.StatusServiceUnavailable
	switch {
	case errors.Is(lastErr, routing.ErrBudgetExhausted):
		statusCode = http.StatusGatewayTimeout
	case strings.HasPrefix(failureReason, "state_"):
		statusCode = http.StatusBadRequest
	case len(endpointChain) > 0:
		// 发出过请求但只有网络错误：沿用 Codex 现有的 503
	case quotaBlocked || subscription.FromContext(r.Context()) != nil:
		failureReason = "daily_cost_limit_exceeded"
		if sub := subscription.FromContext(r.Context()); sub != nil {
			failureReason = "subscription_unavailable"
			detail = failureReason
			w.Header().Set("Retry-After", h.subscriptionRetryAfter(r.Context(), sub.Binding))
		}
		statusCode = http.StatusTooManyRequests
	default:
		// 一次都没发出：路由配置当前无法服务这条请求，重试不会改变结果，回不可重试的 400
		statusCode = http.StatusBadRequest
		if detail == "" {
			detail = failureReason
		}
	}
	h.recordFailure(requestID, startTime, failureReason, detail, nil, requestModel)
	h.writeError(w, statusCode, detail)
}

// recordRouteSnapshot 记录请求级路由快照（只含规则标识与阶段序号，不含请求内容）
func (h *Handler) recordRouteSnapshot(requestID, snapshotJSON string) {
	if h.usageTracker == nil || requestID == "" || snapshotJSON == "" {
		return
	}
	h.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{RouteSnapshot: &snapshotJSON})
}
