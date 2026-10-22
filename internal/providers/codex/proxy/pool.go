package codexproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (m *EndpointManager) SetPoolRuntime(runtime *routing.PoolRuntime) { m.poolRuntime.Store(runtime) }
func (m *EndpointManager) PoolRuntime() *routing.PoolRuntime {
	if m == nil {
		return nil
	}
	return m.poolRuntime.Load()
}

func (m *EndpointManager) PoolEndpoints(channelID, groupID int64) []*CodexEndpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*CodexEndpoint
	for id, endpoints := range m.endpointsByChannel {
		if channelID != 0 && channelID != id {
			continue
		}
		for _, ep := range endpoints {
			if groupID == 0 || groupID == ep.CredentialGroupID {
				result = append(result, ep)
			}
		}
	}
	return result
}

func (h *Handler) planPoolRoute(r *http.Request, body []byte, streaming bool, runtime *routing.PoolRuntime) *codexRoute {
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	native := !isCodexGenerationPath(r.URL.Path) || strings.HasSuffix(r.URL.Path, "/compact")
	if native {
		model = ""
		streaming = true
	}
	var plan routing.PoolPlan
	if native {
		plan = runtime.PlanDefault(r.Context(), "codex")
	} else {
		plan = runtime.Plan(r.Context(), "codex", model, streaming)
	}
	decision := plan.Decision
	if !decision.Routed() {
		decision.Kind = routing.KindPool
	}
	route := &codexRoute{kind: decision.Kind, pool: runtime, snapshot: routing.NewSnapshot(decision, "")}
	route.snapshot.PoolID, route.snapshot.PoolName = plan.Pool.ID, plan.Pool.Name
	for _, member := range plan.Stages {
		m := member
		channel := h.manager.ChannelByID(m.ChannelID)
		if channel == nil {
			continue
		}
		snapshot := route.snapshot
		snapshot.Member = &m
		route.stages = append(route.stages, codexRouteStage{member: &m, route: route, native: false, channel: channel, groupID: m.GroupID, upstreamModel: m.UpstreamModel, index: m.Index, snapshotJSON: snapshot.JSON(m.Index)})
	}
	if h.states != nil {
		sort.SliceStable(route.stages, func(i, j int) bool {
			a, b := &route.stages[i], &route.stages[j]
			if a.index != b.index || a.member.Tier != b.member.Tier {
				return false
			}
			preferred := func(s *codexRouteStage) bool {
				for _, ep := range h.manager.PoolEndpoints(s.channel.ID, s.groupID) {
					if ep.ChannelID == s.channel.ID && ep.CredentialGroupID == s.groupID && h.states.Preferred(routing.ExplicitTurn(r.Context(), r.Header), s.candidate(ep, model)) {
						return true
					}
				}
				return false
			}
			return preferred(a) && !preferred(b)
		})
	}
	return route
}

func (h *Handler) admitPool(r *http.Request, ep *CodexEndpoint, model string) (*http.Request, error) {
	runtime := h.manager.PoolRuntime()
	if runtime == nil {
		return r, nil
	}
	ctx := r.Context()
	if _, ok := routing.PoolCallerFromContext(ctx); ok && ep.AuthMode == "client_passthrough" {
		if h.subscriptions == nil {
			return r, http.ErrNotSupported
		}
		var err error
		ctx, err = h.subscriptions.SelectPoolAccount(ctx, ep.ChannelID, ep.CredentialGroupID)
		if err != nil {
			return r, err
		}
	}
	ctx, _, err := routing.AdmitPool(ctx, runtime, ep.ChannelID, ep.CredentialGroupID, fmt.Sprint(ep.ID), model)
	return r.WithContext(ctx), err
}

func observeCodexPool(resp *http.Response, ctx context.Context, path string) {
	attempt := routing.PoolAttemptFromContext(ctx)
	if attempt == nil {
		return
	}
	status := resp.StatusCode
	resp.Body = routing.ObservePoolBody(resp.Body, attempt, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"), func(data []byte) (bool, routing.Failure) {
		if status == http.StatusTooManyRequests && subscription.FromContext(ctx) != nil {
			return false, routing.Failure{}
		}
		if status >= 400 {
			return false, codexRouteFailure(status, data, nil)
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
			if typ == "error" || typ == "response.failed" || frame["error"] != nil {
				failure = codexRouteFailure(status, data, nil)
			}
			response := frame
			if obj, ok := frame["response"].(map[string]any); ok {
				response = obj
			}
			if response["status"] == "completed" || typ == "response.completed" || response["object"] == "list" || typ == "compaction" {
				success = true
			}
			if path == "/models" {
				_, success = response["models"].([]any)
			} else if path == "/responses/compact" {
				_, output := response["output"].([]any)
				success = output && (response["object"] == "response.compaction" || typ == "response.compaction")
			}
		})
		return success, failure
	}, routing.RetryAfter(resp.Header, time.Now()))
}

func (h *Handler) poolRecovery(ctx context.Context, member routing.PoolStage) (time.Time, bool) {
	account, ok := h.manager.PoolRuntime().Account(member.ChannelID, member.GroupID)
	if !ok || !account.Enabled {
		return time.Time{}, false
	}
	if !account.Managed {
		return time.Time{}, true
	}
	if h.subscriptions == nil {
		return time.Time{}, false
	}
	binding, err := h.subscriptions.PoolBinding(ctx, member.ChannelID, member.GroupID)
	if err != nil {
		return time.Time{}, false
	}
	return h.subscriptions.RecoveryTime(ctx, binding, member.UpstreamModel)
}
