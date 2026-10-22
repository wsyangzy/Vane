package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

func (f *Forwarder) SetSubscriptionController(c *subscription.Controller) { f.subscriptions = c }

type subscriptionAdmissionKey struct{}

// Admission precedes the execution budget: losing a recovery race is a local
// skip, not an upstream attempt. The same lease reaches the actual send.
func (f *Forwarder) admitSubscription(ctx context.Context, ep *endpoint.Endpoint, model string) (context.Context, *subscription.Lease, error) {
	var poolErr error
	ctx, poolErr = f.admitPool(ctx, ep, model)
	if poolErr != nil {
		return ctx, nil, poolErr
	}
	if ep.Config.AuthMode != subscription.ClientPassthrough {
		return ctx, nil, nil
	}
	request := subscription.FromContext(ctx)
	if request == nil || f.subscriptions == nil {
		return ctx, nil, errors.New("subscription_caller_required")
	}
	lease, err := f.subscriptions.Acquire(ctx, request.Binding, model)
	if err != nil {
		routing.PoolAttemptFromContext(ctx).Finish(false)
		return ctx, nil, err
	}
	return context.WithValue(ctx, subscriptionAdmissionKey{}, lease), lease, nil
}

func planSubscriptionRoute(em *endpoint.Manager, r *http.Request, body []byte) *routeRun {
	request := subscription.FromContext(r.Context())
	if request == nil {
		return nil
	}
	b := request.Binding
	decision := routing.Decision{Kind: routing.KindSubscription, Mode: routing.ModeFallback}
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	cfg := em.RouteConfig()
	if cfg != nil && em.PoolRuntime() == nil && r.URL.Path == claudeGenerationPath {
		streaming := false
		var object struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &object)
		streaming = object.Stream
		selected := routing.Decide(cfg.Rules(b.Channel), cfg.NonStreamRoute(b.Channel), model, streaming)
		if selected.Routed() {
			decision = selected
		}
	}
	run := &routeRun{kind: decision.Kind, subscription: true, snapshot: routing.NewSnapshot(decision, b.Channel), turn: routing.ExplicitTurn(r.Context(), r.Header)}
	if decision.Routed() && decision.Kind != routing.KindSubscription {
		for _, stage := range decision.Stages() {
			if stage.Target == nil {
				run.stages = append(run.stages, routeStage{channel: b.Channel, groupID: b.GroupID})
				continue
			}
			for _, allowed := range b.BackupTargets {
				if allowed.ChannelID == stage.Target.ChannelID && (stage.Target.GroupID == 0 || allowed.GroupID == stage.Target.GroupID) && (allowed.UpstreamModel == "" || allowed.UpstreamModel == stage.Target.UpstreamModel) {
					run.stages = append(run.stages, routeStage{channel: cfg.ChannelName(allowed.ChannelID), groupID: allowed.GroupID, upstreamModel: stage.Target.UpstreamModel, index: stage.Index})
				}
			}
		}
		return run
	}
	run.stages = append(run.stages, routeStage{channel: b.Channel, groupID: b.GroupID})
	for _, target := range b.BackupTargets {
		if cfg != nil {
			if channel := cfg.ChannelName(target.ChannelID); channel != "" {
				run.stages = append(run.stages, routeStage{channel: channel, groupID: target.GroupID, upstreamModel: target.UpstreamModel, index: len(run.stages)})
			}
		}
	}
	return run
}

func (f *Forwarder) subscriptionEligibility(ctx context.Context, ep *endpoint.Endpoint, model string) (bool, string) {
	if runtime := f.endpointManager.PoolRuntime(); runtime != nil {
		if account, ok := runtime.Account(f.poolChannelID(ep), ep.Config.CredentialGroupID); !ok || !account.Enabled {
			return false, "account_disabled"
		}
	}
	if _, ok := routing.PoolCallerFromContext(ctx); ok && ep.Config.AuthMode == subscription.ClientPassthrough {
		if f.subscriptions == nil {
			return false, "subscription_state_unavailable"
		}
		b, err := f.subscriptions.PoolBinding(ctx, f.poolChannelID(ep), ep.Config.CredentialGroupID)
		if err != nil {
			return false, "managed_account_not_authorized"
		}
		return f.subscriptions.Eligible(ctx, b, model)
	}
	request := subscription.FromContext(ctx)
	if request != nil && (f.subscriptions == nil || !f.subscriptions.Valid(ctx, request.Binding)) {
		return false, "subscription_binding_changed"
	}
	if ep.Config.AuthMode != subscription.ClientPassthrough {
		if request == nil {
			return true, ""
		}
		for _, target := range request.Binding.BackupTargets {
			if target.GroupID == ep.Config.CredentialGroupID && f.endpointManager.RouteConfig().ChannelName(target.ChannelID) == endpoint.ChannelKey(ep) && (target.UpstreamModel == "" || target.UpstreamModel == model) {
				return true, ""
			}
		}
		return false, "subscription_backup_not_authorized"
	}
	if request == nil || request.Binding.GroupID != ep.Config.CredentialGroupID {
		return false, "subscription_caller_required"
	}
	if f.subscriptions == nil {
		return false, "subscription_state_unavailable"
	}
	return f.subscriptions.Eligible(ctx, request.Binding, model)
}

// sendClaude is used by all Messages and count_tokens sends. Destination and
// binding checks run at the actual send boundary, after custom headers.
func (f *Forwarder) sendClaude(ctx context.Context, src, dst *http.Request, ep *endpoint.Endpoint, body []byte, client *http.Client) (*http.Response, error) {
	if err := subscription.AuthorizeOutbound(ctx, src, dst, ep.Config.AuthMode, ep.Config.CredentialGroupID, ep.Config.Headers); err != nil {
		return nil, err
	}
	request := subscription.FromContext(ctx)
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	lease, _ := ctx.Value(subscriptionAdmissionKey{}).(*subscription.Lease)
	if lease == nil {
		if ok, reason := f.subscriptionEligibility(ctx, ep, model); !ok {
			return nil, errors.New(reason)
		}
	} else if request == nil || !f.subscriptions.Valid(ctx, request.Binding) {
		lease.Finish(0, nil, nil, false)
		return nil, subscription.ErrBindingChanged
	}
	// Credential-bearing redirects are never followed, even for backup hops.
	if request != nil {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	}
	if ep.Config.AuthMode == subscription.ClientPassthrough && lease == nil {
		var err error
		lease, err = f.subscriptions.Acquire(ctx, request.Binding, model)
		if err != nil {
			return nil, err
		}
	}
	resp, err := client.Do(dst)
	if err != nil {
		lease.Finish(0, nil, nil, false)
		if ctx.Err() == nil {
			routing.PoolAttemptFromContext(ctx).Fail(claudeRouteFailure(err), time.Time{})
		} else {
			routing.PoolAttemptFromContext(ctx).Finish(false)
		}
		if request != nil {
			return nil, errors.New("subscription_transport_failed")
		}
		return nil, err
	}
	if request != nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		lease.Finish(resp.StatusCode, nil, nil, false)
		return nil, errors.New("subscription_redirect_rejected")
	}
	if request != nil && resp.StatusCode == http.StatusUnauthorized && ep.Config.AuthMode == subscription.ClientPassthrough {
		f.subscriptions.RejectCredential(ctx)
	}
	if request != nil && resp.StatusCode >= 400 {
		// Error details enter traces and Wails. Reuse the existing bounded
		// decoder, then remove exact credential echoes before that boundary.
		data, _ := readUpstreamErrorBody(resp)
		for _, name := range []string{"Authorization", "X-Api-Key"} {
			secret := dst.Header.Get(name)
			for _, value := range []string{secret, strings.TrimPrefix(secret, "Bearer ")} {
				if value != "" {
					data = bytes.ReplaceAll(data, []byte(value), []byte("[REDACTED]"))
				}
			}
		}
		resp.Body = io.NopCloser(bytes.NewReader(data))
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
	}
	if request != nil || routing.PoolAttemptFromContext(ctx) != nil {
		// Existing response processors forward decoded bytes. Decode before
		// recovery observation as well, including the count_tokens path.
		if err := decodeSubscriptionResponse(resp); err != nil {
			resp.Body.Close()
			lease.Finish(resp.StatusCode, resp.Header, nil, false)
			return nil, errors.New("subscription_response_decode_failed")
		}
	}
	if lease != nil {
		resp.Body = &subscriptionResponse{ReadCloser: resp.Body, lease: lease, status: resp.StatusCode, headers: resp.Header.Clone(), countTokens: src.URL.Path == "/v1/messages/count_tokens"}
	}
	// Client-controlled auth cannot return via response headers.
	if request != nil {
		for name := range resp.Header {
			if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Api-Key") || strings.EqualFold(name, "X-Relay-Key") {
				delete(resp.Header, name)
			}
		}
	}
	observeClaudePool(resp, ctx)
	return resp, nil
}

// The auxiliary path uses the same caller, credentials and bounded route as
// Messages. In particular a 401 must reach the client instead of an estimate.
func (h *CountTokensHandler) handleSubscription(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte, connID string, lifecycle RequestLifecycleManager) {
	bodies, err := applyPrivacyFilter(ctx, h.privacyFilter, r, "claude", body, lifecycle, privacyLevelsFor(h.endpointManager))
	if err != nil {
		if policy := AsPrivacyPolicyError(err); policy != nil {
			WritePrivacyPolicyErrorResponse(w, policy)
		} else {
			http.Error(w, "Privacy filter failed", http.StatusInternalServerError)
		}
		return
	}
	run := planSubscriptionRoute(h.endpointManager, r, body)
	if run == nil {
		run = planClaudePool(h.endpointManager.PoolRuntime(), h.endpointManager.RouteConfig(), r, body, true)
	}
	run.initBudget(h.config.Retry)
	run.watchSubscription(ctx, h.forwarder)
	if lifecycle != nil {
		run.onUpdate = lifecycle.SetRouteSnapshot
	}
	var lastErr error
	for run.current() != nil {
		candidates, _ := routeStageEndpoints(ctx, h.forwarder, nil, h.endpointManager, run, body, nil)
		for _, ep := range candidates {
			if ep.Config.AuthMode != subscription.ClientPassthrough && !ep.Config.SupportsCountTokens {
				continue
			}
			attemptCtx, admission, admitErr := h.forwarder.admitSubscription(ctx, ep, run.candidate(h.forwarder, ep, body).Model)
			if admitErr != nil {
				run.ledger.Skip(run.candidate(h.forwarder, ep, body), admitErr.Error())
				continue
			}
			defer admission.Finish(0, nil, nil, false)
			defer routing.PoolAttemptFromContext(attemptCtx).Finish(false)
			if err := beginRouteAttempt(ctx, run); err != nil {
				lastErr = err
				break
			}
			candidate := run.candidate(h.forwarder, ep, body)
			run.ledger.Begin(candidate)
			if lifecycle != nil {
				lifecycle.IncrementAttempt()
				lifecycle.SetEndpoint(ep.Config.Name, endpointDisplayGroupName(ep), ep.Config.Channel)
			}
			requestBody := rewriteBodyForRoute(h.forwarder, lifecycle, bodies.forEndpoint(ep), ep, run)
			client, err := h.forwarder.NewClient(ep.Config.Timeout)
			if err != nil {
				lastErr = err
				continue
			}
			url := ep.Config.URL + r.URL.Path
			if r.URL.RawQuery != "" {
				url += "?" + r.URL.RawQuery
			}
			req, err := http.NewRequestWithContext(ctx, r.Method, url, bytes.NewReader(requestBody))
			if err != nil {
				lastErr = err
				continue
			}
			h.forwarder.CopyHeaders(r, req, ep)
			resp, err := h.forwarder.sendClaude(attemptCtx, r, req, ep, requestBody, client)
			if err == nil && resp != nil {
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
					resp.Body.Close()
					if readErr == nil {
						copySubscriptionResponseHeaders(w, resp.Header)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(resp.StatusCode)
						w.Write(data)
						h.completeCountTokensRequest(lifecycle, data)
						return
					}
					err = readErr
				} else {
					err = newEndpointErrorFromResponse(resp, url)
				}
			}
			lastErr = err
			run.rememberResponse(err)
			run.ledger.Fail(candidate, claudeRouteFailure(err))
			run.publishSnapshot()
			if run.pool == nil && ep.Config.AuthMode == subscription.ClientPassthrough && GetStatusCodeFromError(err, nil) == http.StatusUnauthorized {
				break
			}
			if ctx.Err() != nil || claudeRouteFailure(err).Terminal {
				break
			}
		}
		if run.pool == nil && GetStatusCodeFromError(lastErr, nil) == http.StatusUnauthorized || ctx.Err() != nil || run.budget.Stopped() || claudeRouteFailure(lastErr).Terminal {
			break
		}
		if !run.advance() {
			break
		}
	}
	reason, status := run.terminalFailure(lastErr)
	h.forwarder.subscriptionRetryAfter(w, r, run)
	lastErr = run.responseEvidence(lastErr)
	if upstream := GetStatusCodeFromError(lastErr, nil); upstream != 0 {
		status = upstream
	}
	if ctx.Err() != nil {
		reason = "client_cancelled"
		status = 499
	}
	run.publishSnapshot()
	if lifecycle != nil {
		failRequest(lifecycle, reason, "", lastErr, nil)
	}
	*r = *r.WithContext(withFinalStatusCode(r.Context(), status))
	WriteUpstreamErrorResponse(w, lastErr, reason, status)
}

func copySubscriptionResponseHeaders(w http.ResponseWriter, headers http.Header) {
	for name, values := range headers {
		lower := strings.ToLower(name)
		if lower == "request-id" || lower == "x-request-id" || lower == "retry-after" || strings.HasPrefix(lower, "anthropic-ratelimit-") {
			w.Header()[name] = append([]string(nil), values...)
		}
	}
}

func (f *Forwarder) subscriptionRetryAfter(w http.ResponseWriter, r *http.Request, run *routeRun) {
	request := subscription.FromContext(r.Context())
	if request == nil || f.subscriptions == nil || run == nil {
		return
	}
	model := ""
	if count := len(run.ledger.Events); count > 0 {
		model = run.ledger.Events[count-1].Candidate.Model
	}
	resume := time.Time{}
	for _, pause := range f.subscriptions.Status(r.Context(), request.Binding) {
		if (pause.Model == "*" || pause.Model == model || strings.HasPrefix(pause.Model, "family:") && strings.Contains(strings.ToLower(model), strings.TrimPrefix(pause.Model, "family:"))) && pause.ResumeNotBefore.After(resume) {
			resume = pause.ResumeNotBefore
		}
	}
	if remaining := time.Until(resume); remaining > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64(math.Ceil(remaining.Seconds())), 10))
	}
}
