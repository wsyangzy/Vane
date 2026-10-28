package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

func planClaudePool(runtime *routing.PoolRuntime, cfg *endpoint.RouteConfig, r *http.Request, body []byte, streaming bool) *routeRun {
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	var plan routing.PoolPlan
	if r.URL.Path == claudeGenerationPath {
		plan = runtime.Plan(r.Context(), "claude", model, streaming)
	} else {
		plan = runtime.PlanDefault(r.Context(), "claude")
	}
	decision := plan.Decision
	if !decision.Routed() {
		decision.Kind = routing.KindPool
	}
	run := &routeRun{pool: runtime, poolCtx: r.Context(), poolModel: model, kind: decision.Kind, snapshot: routing.NewSnapshot(decision, ""), turn: routing.ExplicitTurn(r.Context(), r.Header)}
	run.snapshot.PoolID, run.snapshot.PoolName = plan.Pool.ID, plan.Pool.Name
	for _, member := range plan.Stages {
		m := member
		channel := cfg.ChannelName(m.ChannelID)
		if channel != "" {
			run.stages = append(run.stages, routeStage{member: &m, channel: channel, groupID: m.GroupID, upstreamModel: m.UpstreamModel, index: m.Index})
		}
	}
	return run
}

func (f *Forwarder) poolChannelID(ep *endpoint.Endpoint) int64 {
	if cfg := f.endpointManager.RouteConfig(); cfg != nil {
		for id, name := range cfg.ChannelNames {
			if name == endpoint.ChannelKey(ep) {
				return id
			}
		}
	}
	return 0
}

func (f *Forwarder) admitPool(ctx context.Context, ep *endpoint.Endpoint, model string) (context.Context, error) {
	if routing.ProfileRequestFromContext(ctx) != nil {
		return f.admitProfile(ctx, ep, model)
	}
	runtime := f.endpointManager.PoolRuntime()
	if runtime == nil {
		return ctx, nil
	}
	if _, ok := routing.PoolCallerFromContext(ctx); ok && ep.Config.AuthMode == "client_passthrough" {
		if f.subscriptions == nil {
			return ctx, http.ErrNotSupported
		}
		var err error
		ctx, err = f.subscriptions.SelectPoolAccount(ctx, f.poolChannelID(ep), ep.Config.CredentialGroupID)
		if err != nil {
			return ctx, err
		}
	}
	ctx, _, err := routing.AdmitPool(ctx, runtime, f.poolChannelID(ep), ep.Config.CredentialGroupID, endpoint.EndpointKeyForConfig(ep.Config), model)
	return ctx, err
}

func observeClaudePool(resp *http.Response, ctx context.Context) {
	attempt := routing.PoolAttemptFromContext(ctx)
	if attempt == nil {
		return
	}
	status := resp.StatusCode
	retryAt := routing.RetryAfter(resp.Header, time.Now())
	stream := resp.Header.Get("Content-Type") == "text/event-stream" || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream;")
	resp.Body = routing.ObservePoolBody(resp.Body, attempt, stream, func(data []byte) (bool, routing.Failure) {
		if status == http.StatusTooManyRequests && subscription.FromContext(ctx) != nil {
			return false, routing.Failure{}
		}
		if status >= 400 {
			return false, claudeRouteFailure(&EndpointError{StatusCode: status, Body: data})
		}
		success := false
		var failure routing.Failure
		routing.JSONFrames(data, func(frame map[string]any) {
			typ, _ := frame["type"].(string)
			if subscription.FromContext(ctx) != nil && (typ == "error" || typ == "response.failed" || frame["error"] != nil) {
				quotaFrame := frame
				if response, ok := frame["response"].(map[string]any); ok {
					quotaFrame = response
				}
				raw, _ := json.Marshal(quotaFrame)
				if len(subscription.QuotaPauses(status, resp.Header, raw, subscription.FromContext(ctx).Binding, "", time.Now())) > 0 {
					return
				}
			}
			if typ == "error" || frame["error"] != nil {
				failure = claudeRouteFailure(&EndpointError{StatusCode: status, Body: data})
			}
			if typ == "message_stop" || typ == "message" && frame["stop_reason"] != nil || frame["input_tokens"] != nil {
				success = true
			}
		})
		return success, failure
	}, retryAt)
}

func (f *Forwarder) poolRecovery(ctx context.Context, member routing.PoolStage) (time.Time, bool) {
	account, ok := f.endpointManager.PoolRuntime().Account(member.ChannelID, member.GroupID)
	if !ok || !account.Enabled {
		return time.Time{}, false
	}
	if !account.Managed {
		return time.Time{}, true
	}
	if f.subscriptions == nil {
		return time.Time{}, false
	}
	binding, err := f.subscriptions.PoolBinding(ctx, member.ChannelID, member.GroupID)
	if err != nil {
		return time.Time{}, false
	}
	return f.subscriptions.RecoveryTime(ctx, binding, member.UpstreamModel)
}
