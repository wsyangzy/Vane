package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

type profileHeadersKey struct{}
type profileAttemptKey struct{}
type profileHealthKey struct{}
type profileBillingExemptKey struct{}
type profileHealth struct {
	once      sync.Once
	pool      *routing.PoolRuntime
	candidate routing.DispatchCandidate
	release   func(bool)
	sent      bool
	ctx       context.Context
	response  *profileResponseBody
}

func (h *profileHealth) finish(success bool, failure routing.Failure, quota bool, retry time.Time) {
	if h == nil {
		return
	}
	h.once.Do(func() {
		h.release(success)
		if h.sent && !success && !quota && h.ctx.Err() == nil && failure.Scope != "" {
			_ = h.pool.Fail(h.candidate.ChannelID, h.candidate.GroupID, h.candidate.EndpointID, h.candidate.Identity().Model, failure, retry)
		}
	})
}

func planClaudeProfile(runtime *routing.ProfileRuntime, cfg *endpoint.RouteConfig, r *http.Request, body []byte, streaming bool, requestID string) *routeRun {
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	turn := routing.ResolveTurn(r.Context(), r.Header, body, "claude")
	if r.URL.Path != claudeGenerationPath {
		// Auxiliary requests never advance the ordinary selection cursor.
		turn = routing.TurnInput{}
	}
	plan := runtime.Plan(r.Context(), 0, model, streaming, turn, false)
	if requestID != "" {
		plan.RequestID = requestID
	}
	run := &routeRun{profile: &plan, profileRuntime: runtime, profileChannels: cfg, budget: plan.Budget, kind: routing.KindPool, turn: routing.ExplicitTurn(r.Context(), r.Header)}
	run.stages = claudeProfileStages(&plan, cfg)
	return run
}

func claudeProfileStages(plan *routing.DispatchPlan, cfg *endpoint.RouteConfig) []routeStage {
	stages := []routeStage{}
	for _, stage := range plan.Stages {
		for _, candidate := range stage.EligibleCandidates {
			c := candidate
			channel := ""
			if cfg != nil {
				channel = cfg.ChannelName(c.ChannelID)
			}
			if channel == "" {
				plan.Skip(c, "channel_unavailable")
				continue
			}
			stages = append(stages, routeStage{candidate: &c, channel: channel, groupID: c.GroupID, upstreamModel: c.UpstreamModel, index: stage.Index})
		}
	}
	return stages
}

func profileRouteContext(ctx context.Context, run *routeRun, r *http.Request) context.Context {
	if run == nil || run.profile == nil {
		return ctx
	}
	ctx = routing.WithProfileRequest(ctx, run.profile)
	ctx = context.WithValue(ctx, profileBillingExemptKey{}, r.URL.Path != claudeGenerationPath)
	run.poolCtx = ctx
	return context.WithValue(ctx, profileHeadersKey{}, r.Header.Clone())
}

func (run *routeRun) endProfile() {
	if run != nil && run.profile != nil {
		reason := run.profile.Trace().StopReason
		if run.budget.Stopped() {
			reason = "retry_budget_exhausted"
		}
		if reason == "" {
			reason = run.ledger.LastSkipReason()
		}
		if reason == "" {
			reason = "route_unavailable"
		}
		run.profile.End(reason)
	}
}

func profileAttempt(ctx context.Context) *routing.DispatchReservation {
	reservation, _ := ctx.Value(profileAttemptKey{}).(*routing.DispatchReservation)
	return reservation
}

func finishProfileAttempt(ctx context.Context, err error) {
	if reservation := profileAttempt(ctx); reservation != nil {
		failure := routing.Failure{Scope: routing.ScopeCandidate, Reason: "response_incomplete"}
		if err != nil {
			failure = claudeRouteFailure(err)
		}
		reservation.Finish(0, false, failure)
		health, _ := ctx.Value(profileHealthKey{}).(*profileHealth)
		health.finish(false, failure, false, time.Time{})
	}
}

func finishProfileStream(ctx context.Context, err error) {
	health, _ := ctx.Value(profileHealthKey{}).(*profileHealth)
	if health == nil || health.response == nil {
		return
	}
	// The protocol processor checks completeness after closing the body. Wait
	// for its verdict before updating successful affinity or account health.
	health.response.finish(err == nil && health.response.complete)
}

func profileCandidate(ctx context.Context, channel, group int64, endpointID, model string) (routing.DispatchCandidate, bool) {
	plan := routing.ProfileRequestFromContext(ctx)
	if plan == nil || plan.Preview || plan.Error != "" || plan.Profile.Provider != "claude" {
		return routing.DispatchCandidate{}, false
	}
	for _, c := range plan.EligibleCandidates {
		if c.Provider == "claude" && c.ChannelID == channel && c.GroupID == group && c.EndpointID == endpointID && c.Identity().Model == model {
			return c, true
		}
	}
	return routing.DispatchCandidate{}, false
}

func (f *Forwarder) profileCandidateCurrent(candidate routing.DispatchCandidate, ep *endpoint.Endpoint) bool {
	if candidate.StateVersion != 0 && candidate.StateVersion != ep.RouteStateVersion() {
		return false
	}
	for _, current := range f.endpointManager.GetAllEndpoints() {
		if endpoint.EndpointKeyForConfig(current.Config) == candidate.EndpointID && current.Config.CredentialGroupID == candidate.GroupID && f.poolChannelID(current) == candidate.ChannelID {
			return candidate.StateVersion == 0 || candidate.StateVersion == current.RouteStateVersion()
		}
	}
	return false
}

func (f *Forwarder) admitProfile(ctx context.Context, ep *endpoint.Endpoint, model string) (context.Context, error) {
	plan := routing.ProfileRequestFromContext(ctx)
	candidate, ok := profileCandidate(ctx, f.poolChannelID(ep), ep.Config.CredentialGroupID, endpoint.EndpointKeyForConfig(ep.Config), model)
	if !ok {
		return ctx, errors.New("profile_candidate_not_authorized")
	}
	if !f.profileCandidateCurrent(candidate, ep) {
		plan.Skip(candidate, "candidate_configuration_changed")
		return ctx, errors.New("candidate_configuration_changed")
	}
	if ep.Config.AuthMode == subscription.ClientPassthrough {
		if f.subscriptions == nil {
			return ctx, errors.New("subscription_state_unavailable")
		}
		headers, _ := ctx.Value(profileHeadersKey{}).(http.Header)
		var err error
		ctx, err = f.subscriptions.SelectProfileAccount(ctx, candidate.ChannelID, candidate.GroupID, headers)
		if err != nil {
			return ctx, err
		}
	}
	pool := f.endpointManager.PoolRuntime()
	if pool == nil || pool.ProfileRuntime() == nil {
		return ctx, errors.New("profile_runtime_unavailable")
	}
	runtime := pool.ProfileRuntime()
	reservation, err := runtime.Reserve(ctx, plan, candidate)
	if err != nil {
		return ctx, err
	}
	release, err := pool.AcquireHealth(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model)
	if err != nil {
		reservation.Finish(0, false, routing.Failure{})
		return ctx, err
	}
	ctx = context.WithValue(ctx, profileHealthKey{}, &profileHealth{pool: pool, candidate: candidate, release: release, ctx: ctx})
	exempt, _ := ctx.Value(profileBillingExemptKey{}).(bool)
	reservation.SetUsage(routing.AttemptUsage{Exempt: exempt})
	return context.WithValue(ctx, profileAttemptKey{}, reservation), nil
}

func (f *Forwarder) SetProfileResponseObserver(observer func(routing.DispatchCandidate, int, http.Header, []byte)) {
	f.profileResponseObserver = observer
}

type profileResponseWriter struct {
	http.ResponseWriter
	reservation *routing.DispatchReservation
}

func (w *profileResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if n > 0 {
		w.reservation.ResponseStarted()
	}
	return n, err
}

func (f *Forwarder) observeClaudeProfile(resp *http.Response, ctx context.Context) {
	reservation := profileAttempt(ctx)
	if reservation == nil {
		return
	}
	var observer func(routing.DispatchCandidate, int, http.Header, []byte)
	if callback := f.profileResponseObserver; callback != nil {
		observer = func(candidate routing.DispatchCandidate, status int, headers http.Header, body []byte) {
			quotaHeaders := http.Header{}
			for name, values := range headers {
				lower := strings.ToLower(name)
				if lower == "retry-after" || strings.HasPrefix(lower, "anthropic-ratelimit-") || strings.HasPrefix(lower, "x-ratelimit-") {
					quotaHeaders[name] = append([]string(nil), values...)
				}
			}
			callback(candidate, status, quotaHeaders, claudeProfileLearningBody(body))
		}
	}
	if observer != nil {
		observer(reservation.Candidate(), resp.StatusCode, resp.Header.Clone(), nil)
	}
	health, _ := ctx.Value(profileHealthKey{}).(*profileHealth)
	body := &profileResponseBody{ReadCloser: resp.Body, reservation: reservation, health: health, status: resp.StatusCode, headers: resp.Header.Clone(), stream: strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"), observer: observer}
	body.usage.Exempt, _ = ctx.Value(profileBillingExemptKey{}).(bool)
	if health != nil {
		health.response = body
	}
	resp.Body = body
}

// Decode into an allowlist so error messages, generated text and unexpected
// fields nested in usage cannot cross the response-learning boundary.
func claudeProfileLearningBody(body []byte) []byte {
	var source struct {
		Error *struct {
			Code string `json:"code,omitempty"`
			Type string `json:"type,omitempty"`
		} `json:"error,omitempty"`
		Usage map[string]json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &source) != nil {
		return nil
	}
	result := map[string]any{}
	if source.Error != nil {
		result["error"] = source.Error
	}
	usage := map[string]int64{}
	for _, name := range []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
		var value int64
		if raw, exists := source.Usage[name]; exists && json.Unmarshal(raw, &value) == nil && value >= 0 {
			usage[name] = value
		}
	}
	if len(usage) > 0 {
		result["usage"] = usage
	}
	if len(result) == 0 {
		return nil
	}
	data, _ := json.Marshal(result)
	return data
}

// Only one bounded JSON message or SSE frame is retained. The observer emits
// usage/error fields, never generated content, to the quota persistence layer.
type profileResponseBody struct {
	io.ReadCloser
	reservation                    *routing.DispatchReservation
	health                         *profileHealth
	quota                          bool
	status                         int
	headers                        http.Header
	stream                         bool
	observer                       func(routing.DispatchCandidate, int, http.Header, []byte)
	buffer                         []byte
	overflow                       bool
	complete                       bool
	success                        bool
	failure                        routing.Failure
	input, output, cached, created int64
	once                           sync.Once
	usage                          routing.AttemptUsage
}

func (b *profileResponseBody) inspect(data []byte) {
	if len(subscription.QuotaPauses(b.status, b.headers, data, subscription.Binding{GroupID: b.reservation.Candidate().GroupID}, b.reservation.Candidate().Identity().Model, time.Now())) > 0 {
		b.quota = true
	}
	if b.status >= 400 {
		b.failure = claudeRouteFailure(&EndpointError{StatusCode: b.status, Body: data})
	}
	routing.JSONFrames(data, func(frame map[string]any) {
		typ, _ := frame["type"].(string)
		if typ == "error" || frame["error"] != nil {
			raw, _ := json.Marshal(map[string]any{"type": typ, "error": frame["error"]})
			b.failure = claudeRouteFailure(&EndpointError{StatusCode: b.status, Body: raw})
			if b.observer != nil {
				b.observer(b.reservation.Candidate(), b.status, b.headers.Clone(), raw)
			}
		}
		if typ == "message_stop" || typ == "message" && frame["stop_reason"] != nil || frame["input_tokens"] != nil {
			b.success = true
		}
		usage, _ := frame["usage"].(map[string]any)
		if message, ok := frame["message"].(map[string]any); ok {
			if value, ok := message["usage"].(map[string]any); ok {
				usage = value
			}
		}
		if usage != nil {
			b.usage.Known = true
			value := func(key string) int64 { v, _ := usage[key].(float64); return max(int64(v), 0) }
			b.input, b.output = max(b.input, value("input_tokens")), max(b.output, value("output_tokens"))
			b.cached, b.created = max(b.cached, value("cache_read_input_tokens")), max(b.created, value("cache_creation_input_tokens"))
			if cache, ok := usage["cache_creation"].(map[string]any); ok {
				five, _ := cache["ephemeral_5m_input_tokens"].(float64)
				hour, _ := cache["ephemeral_1h_input_tokens"].(float64)
				b.usage.CacheCreation5mTokens = max(b.usage.CacheCreation5mTokens, int64(five))
				b.usage.CacheCreation1hTokens = max(b.usage.CacheCreation1hTokens, int64(hour))
			}
			if tier, ok := usage["service_tier"].(string); ok {
				b.usage.ServiceTier = tier
			}
			if b.observer != nil {
				raw, _ := json.Marshal(map[string]any{"type": typ, "usage": usage})
				b.observer(b.reservation.Candidate(), b.status, b.headers.Clone(), raw)
			}
		}
	})
}

func (b *profileResponseBody) finish(complete bool) {
	b.once.Do(func() {
		if b.failure.Reason == "" && (!complete || !b.success || b.overflow) {
			b.failure = routing.Failure{Scope: routing.ScopeCandidate, Reason: "response_incomplete"}
		}
		b.reservation.SetCachedTokens(b.cached)
		success := complete && b.success && !b.overflow && b.failure.Reason == ""
		b.usage.InputTokens, b.usage.OutputTokens = b.input, b.output
		b.usage.CacheReadTokens, b.usage.CacheCreationTokens = b.cached, b.created
		b.usage.Complete = success
		b.reservation.SetUsage(b.usage)
		b.health.finish(success, b.failure, b.quota, routing.RetryAfter(b.headers, time.Now()))
		b.reservation.Finish(b.input+b.output+b.cached+b.created, success, b.failure)
		clear(b.buffer)
		b.buffer = nil
	})
}

func (b *profileResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if !b.overflow {
		b.buffer = append(b.buffer, p[:n]...)
		if b.stream {
			for {
				i := bytes.IndexByte(b.buffer, '\n')
				if i < 0 {
					break
				}
				b.inspect(b.buffer[:i])
				b.buffer = b.buffer[i+1:]
			}
		}
		if len(b.buffer) > 1<<20 {
			clear(b.buffer)
			b.buffer, b.overflow = nil, true
		}
	}
	if err == io.EOF {
		if !b.overflow {
			b.inspect(b.buffer)
		}
		b.complete = true
		if !b.stream {
			b.finish(true)
		}
	} else if err != nil {
		if !b.stream {
			b.finish(false)
		}
	}
	return n, err
}

func (b *profileResponseBody) Close() error {
	if !b.stream || !b.complete || !b.success || b.status >= 400 || b.failure.Reason != "" {
		b.finish(b.complete)
	}
	return b.ReadCloser.Close()
}
