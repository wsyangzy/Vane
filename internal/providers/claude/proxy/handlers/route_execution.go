package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

func (run *routeRun) initBudget(cfg config.RetryConfig) {
	if run != nil && run.profile != nil {
		if !run.recoveryPolicySet {
			run.profile.Profile.WaitForRecovery = !cfg.DisableRecoveryWait
			run.recoveryPolicySet = true
		}
		run.budget.Limit(cfg.MaxTotalAttempts, cfg.MaxTotalDuration)
	}
	if run != nil && run.budget == nil {
		attempts, duration := cfg.MaxTotalAttempts, cfg.MaxTotalDuration
		if run.pool != nil {
			if attempts <= 0 {
				attempts = 10
			}
			if duration <= 0 {
				duration = 60 * time.Second
			}
		}
		run.budget = routing.NewBudget(attempts, duration)
	}
}

func (run *routeRun) candidate(f *Forwarder, ep *endpoint.Endpoint, body []byte) routing.Candidate {
	if run != nil && run.current() != nil && run.current().candidate != nil {
		return run.current().candidate.Identity()
	}
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	if run.upstreamModel() != "" {
		model = run.upstreamModel()
	} else {
		model = EffectiveClaudeModel(ep, f.endpointManager.GetChannelModelMapping(endpoint.ChannelKey(ep)), model)
	}
	credential := fmt.Sprintf("%s/%d", endpoint.ChannelKey(ep), ep.Config.CredentialGroupID)
	if ep.Config.CredentialGroupID == 0 {
		credential = endpoint.EndpointKeyForConfig(ep.Config)
	}
	return routing.Candidate{Provider: "claude", Protocol: "messages", Channel: endpoint.ChannelKey(ep), Credential: credential, Endpoint: endpoint.EndpointKeyForConfig(ep.Config), Model: model, StateVersion: ep.RouteStateVersion()}
}

func (run *routeRun) observeSubscription(ctx context.Context, f *Forwarder) {
	request := subscription.FromContext(ctx)
	if run == nil || request == nil {
		return
	}
	b := request.Binding
	state := &routing.SubscriptionSnapshot{GroupID: b.GroupID, BindingVersion: b.Version, OfficialEnabled: b.Enabled, OfficialVerification: "not_verified"}
	if f.subscriptions != nil {
		for _, p := range f.subscriptions.Status(ctx, b) {
			state.Availability = append(state.Availability, routing.AvailabilitySnapshot{Model: p.Model, State: p.State, Reason: p.Reason, Source: p.Source, ResumeNotBefore: p.ResumeNotBefore.Format(time.RFC3339), ResetKnown: p.ResetKnown})
		}
	}
	run.snapshot.Subscription = state
}

func (run *routeRun) watchSubscription(ctx context.Context, f *Forwarder) {
	if run != nil && run.profile != nil {
		run.profileRecovery = func(candidate routing.DispatchCandidate) (time.Time, bool) {
			ready, valid := f.endpointManager.PoolRuntime().HealthReadyAt(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model)
			if !valid {
				return time.Time{}, false
			}
			if candidate.CredentialMode != "managed" && candidate.CredentialMode != subscription.ClientPassthrough {
				return ready, true
			}
			if f.subscriptions == nil {
				return time.Time{}, false
			}
			quotaReady, ok := f.subscriptions.ProfileRecoveryTime(ctx, candidate.ChannelID, candidate.GroupID, candidate.Identity().Model)
			if quotaReady.After(ready) {
				ready = quotaReady
			}
			return ready, ok
		}
	}
	if run != nil && run.subscription {
		run.refresh = func() { run.observeSubscription(ctx, f) }
	}
}

func claudeRouteFailure(err error) routing.Failure {
	if subscription.IsLocalError(err) {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: err.Error()}
	}
	var response *EndpointError
	if err != nil && strings.Contains(err.Error(), "timeout awaiting response headers") {
		return routing.Failure{Scope: routing.ScopeRequest, Reason: "response_header_timeout", Terminal: true}
	}
	if !errors.As(err, &response) {
		return routing.Failure{Scope: routing.ScopeEndpoint, Reason: "network_error"}
	}
	if response.StatusCode == http.StatusUnauthorized {
		return routing.Failure{Scope: routing.ScopeCredential, Reason: "credential_rejected"}
	}
	code, kind, _ := routing.ErrorIdentity(response.Body)
	code = strings.ToLower(code)
	kind = strings.ToLower(kind)
	switch code {
	case "model_not_found", "model_not_available", "model_not_supported", "unsupported_model", "model_access_denied", "context_length_exceeded", "input_too_long", "streaming_not_supported", "unsupported_stream":
		return routing.Failure{Scope: routing.ScopeModel, Reason: code}
	}
	if kind == "not_found_error" && response.StatusCode == 404 {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: "resource_not_found"}
	}
	if response.StatusCode == 400 || (response.StatusCode == 0 || response.StatusCode == 200) && kind == "invalid_request_error" {
		return routing.Failure{Scope: routing.ScopeRequest, Reason: "invalid_request", Terminal: true}
	}
	if response.StatusCode == 403 {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: "permission_scope_unknown"}
	}
	if response.StatusCode == 429 || response.StatusCode == 200 {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: "upstream_unavailable"}
	}
	return routing.Failure{Scope: routing.ScopeEndpoint, Reason: "endpoint_unavailable"}
}

func (run *routeRun) failureDecision(f *Forwarder, ep *endpoint.Endpoint, body []byte, err error, decision RetryDecision) RetryDecision {
	if run == nil {
		return decision
	}
	failure := claudeRouteFailure(err)
	run.rememberResponse(err)
	run.ledger.Fail(run.candidate(f, ep, body), failure)
	run.publishSnapshot()
	if failure.Terminal {
		return RetryDecision{FinalStatus: "error", Reason: failure.Reason}
	}
	if run.subscription || run.pool != nil || run.profile != nil {
		return RetryDecision{SwitchEndpoint: true, Reason: failure.Reason}
	}
	// Network/overload backoff retains the existing bounded policy. Known
	// deterministic incompatibility must not enter retry or suspension loops.
	if failure.Scope == routing.ScopeModel || failure.Scope == routing.ScopeCredential {
		return RetryDecision{SwitchEndpoint: true, Reason: failure.Reason}
	}
	decision.SuspendRequest = false
	return decision
}

func (run *routeRun) rememberResponse(err error) {
	var response *EndpointError
	if run != nil && errors.As(err, &response) && response.StatusCode > 0 {
		run.lastResponse = err
	}
}

func (run *routeRun) responseEvidence(err error) error {
	if run != nil && run.lastResponse != nil && GetStatusCodeFromError(err, nil) == 0 {
		return run.lastResponse
	}
	return err
}

func beginRouteAttempt(ctx context.Context, run *routeRun) error {
	defer run.publishSnapshot()
	if err := ctx.Err(); err != nil {
		return err
	}
	if run == nil {
		return nil
	}
	if run.profile != nil {
		return run.budget.Check()
	}
	return run.budget.Begin(ctx)
}

func waitRouteRetry(ctx context.Context, run *routeRun, delay time.Duration) error {
	defer run.publishSnapshot()
	if run == nil {
		return (*routing.Budget)(nil).Wait(ctx, delay)
	}
	return run.budget.Wait(ctx, delay)
}

func claudeStateKeys(ctx context.Context, body []byte) []string {
	// String content is legal and carries no signed state; decode per message
	// so a string in one message does not suppress signed blocks in another.
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return nil
	}
	var messages []struct {
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(obj["messages"], &messages)
	var keys []string
	for _, message := range messages {
		var blocks []map[string]any
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		keys = append(keys, claudeBlockKeys(ctx, blocks)...)
	}
	return keys
}

func claudeBlockKeys(ctx context.Context, blocks []map[string]any) []string {
	var keys []string
	for _, block := range blocks {
		kind, _ := block["type"].(string)
		field := ""
		if kind == "thinking" {
			field = "signature"
		} else if kind == "redacted_thinking" {
			field = "data"
		}
		if field != "" {
			if value, ok := block[field].(string); ok && value != "" {
				keys = append(keys, routing.StateKey(ctx, kind, value))
			}
		}
	}
	return keys
}

func (f *Forwarder) observeRoutedState(ctx context.Context, r *http.Request, c routing.Candidate, body []byte) {
	var keys []string
	signatures := map[float64]string{}
	success, failed := false, false
	routing.JSONFrames(body, func(frame map[string]any) {
		typ, _ := frame["type"].(string)
		if typ == "error" || frame["error"] != nil {
			failed = true
		}
		if typ == "message_stop" || typ == "message" && frame["stop_reason"] != nil {
			success = true
		}
		if blocks, ok := frame["content"].([]any); ok {
			var selected []map[string]any
			for _, block := range blocks {
				if b, ok := block.(map[string]any); ok {
					selected = append(selected, b)
				}
			}
			keys = append(keys, claudeBlockKeys(ctx, selected)...)
		}
		if block, ok := frame["content_block"].(map[string]any); ok {
			keys = append(keys, claudeBlockKeys(ctx, []map[string]any{block})...)
		}
		if delta, ok := frame["delta"].(map[string]any); ok && delta["type"] == "signature_delta" {
			index, _ := frame["index"].(float64)
			value, _ := delta["signature"].(string)
			signatures[index] += value
		}
	})
	if success && !failed {
		for _, value := range signatures {
			if value != "" {
				keys = append(keys, routing.StateKey(ctx, "thinking", value))
			}
		}
		f.states.Observe(keys, routing.ExplicitTurn(ctx, r.Header), c)
	}
}
