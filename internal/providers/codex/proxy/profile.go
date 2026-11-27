package codexproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func (m *EndpointManager) dispatchEndpoint(c routing.DispatchCandidate) *CodexEndpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, endpoint := range m.endpointsByChannel[c.ChannelID] {
		if endpoint.CredentialGroupID == c.GroupID && fmt.Sprint(endpoint.ID) == c.EndpointID {
			return endpoint
		}
	}
	return nil
}

// PrepareDispatchCandidate resolves the same model policy as the actual
// forwarder. It retains the client's model separately from the effective model.
func (m *EndpointManager) PrepareDispatchCandidate(c routing.DispatchCandidate, requestModel, override string) routing.DispatchCandidate {
	c.Model = requestModel
	ep := m.dispatchEndpoint(c)
	if ep == nil {
		c.Enabled = false
		return c
	}
	c.StateVersion = routing.StateVersion(&ep.stateVersion)
	if c.Protocol == "images" {
		c.Enabled = c.Enabled && ep.AuthMode != subscription.ClientPassthrough
		return c
	}
	if c.Protocol == "search" {
		return c
	}
	if override != "" {
		c.UpstreamModel = override
	} else {
		c.UpstreamModel = effectiveCodexModel(ep, m.ChannelByID(c.ChannelID), requestModel)
	}
	return c
}

func (m *EndpointManager) SupportsDispatchCandidate(c routing.DispatchCandidate, actualModel string) bool {
	ep := m.dispatchEndpoint(c)
	return ep != nil && endpointDeclaresCodexModel(ep, actualModel)
}

func (h *HTTPHandler) SetProfileResponseObserver(observer func(context.Context, routing.DispatchCandidate, int, http.Header, []byte)) {
	h.handler.configMu.Lock()
	h.handler.profileResponse = observer
	h.handler.configMu.Unlock()
}

func (h *Handler) observeProfileResponse(ctx context.Context, candidate routing.DispatchCandidate, status int, headers http.Header, body []byte) {
	h.configMu.RLock()
	observer := h.profileResponse
	h.configMu.RUnlock()
	if observer != nil {
		quotaHeaders := http.Header{}
		for name, values := range headers {
			lower := strings.ToLower(name)
			if lower == "retry-after" || strings.HasPrefix(lower, "x-codex-") || strings.HasPrefix(lower, "x-ratelimit-") {
				quotaHeaders[name] = append([]string(nil), values...)
			}
		}
		observer(ctx, candidate, status, quotaHeaders, codexProfileLearningBody(body))
	}
}

func codexProfileLearningBody(body []byte) []byte {
	var source struct {
		Error *struct {
			Code     string `json:"code,omitempty"`
			Type     string `json:"type,omitempty"`
			ResetsAt int64  `json:"resets_at,omitempty"`
			ResetsIn int64  `json:"resets_in_seconds,omitempty"`
		} `json:"error,omitempty"`
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(body, &source) != nil {
		return nil
	}
	if len(source.Response) > 0 {
		return codexProfileLearningBody(source.Response)
	}
	result := map[string]any{}
	if source.Error != nil {
		result["error"] = source.Error
	}
	if usage := NewTokenParser().ParseNonStreaming(body); usage != nil {
		result["usage"] = map[string]any{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens, "input_tokens_details": map[string]int64{"cached_tokens": usage.CacheReadTokens}, "output_tokens_details": map[string]int64{"reasoning_tokens": usage.ReasoningTokens}}
	}
	if len(result) == 0 {
		return nil
	}
	data, _ := json.Marshal(result)
	return data
}

type codexProfileBeforeSendKey struct{}

func beginCodexProfileSend(ctx context.Context) error {
	if begin, ok := ctx.Value(codexProfileBeforeSendKey{}).(func() error); ok {
		return begin()
	}
	return nil
}

func (h *Handler) planProfileRoute(r *http.Request, body []byte, streaming bool, requestID string, runtime *routing.ProfileRuntime) *codexRoute {
	if isCodexSearchRequest(r) {
		plan := runtime.PlanNative(r.Context(), 0, h.requestModel(r, body), "search")
		plan.RequestID = requestID
		return &codexRoute{kind: routing.KindPool, profile: &plan}
	}
	if isCodexImageRequest(r) {
		plan := runtime.PlanNative(r.Context(), 0, h.requestModel(r, body), "images")
		plan.RequestID = requestID
		return &codexRoute{kind: routing.KindPool, profile: &plan}
	}
	model, _ := bodyrewrite.LookupTopLevelString(body, "model")
	turn := routing.ResolveTurn(r.Context(), r.Header, body, "codex")
	keys := codexStateKeys(r, body)
	ctx := routing.WithStatePreference(r.Context(), h.states, keys, routing.ExplicitTurn(r.Context(), r.Header))
	plan := runtime.Plan(routing.WithProfileStateKeys(ctx, keys), 0, model, streaming, turn, false)
	plan.RequestID = requestID
	if !subscription.AllowedPath(r) || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		plan.Error = "profile_path_rejected"
		plan.EligibleCandidates = nil
	}
	return &codexRoute{kind: routing.KindPool, profile: &plan}
}

type codexProfileWriter struct {
	http.ResponseWriter
	attempt *routing.DispatchReservation
	started bool
}

func (w *codexProfileWriter) WriteHeader(status int) {
	if !w.started {
		w.started = true
		w.attempt.ResponseStarted()
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *codexProfileWriter) Write(data []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *codexProfileWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// codexProfileStream observes bounded protocol frames without retaining the
// complete stream. State identifiers are hashed as soon as they are extracted.
type codexProfileStream struct {
	io.ReadCloser
	handler    *Handler
	request    *http.Request
	candidate  routing.DispatchCandidate
	status     int
	headers    http.Header
	line       []byte
	keys       []string
	usage      *TokenResult
	completed  bool
	failed     bool
	eof        bool
	errorFrame []byte
	decoder    codexSSEDecoder
}

func (b *codexProfileStream) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	for _, ch := range data[:n] {
		if ch == '\n' {
			b.frame(bytes.TrimSpace(b.line))
			b.line = b.line[:0]
		} else if len(b.line) < maxCodexStreamEventBytes {
			b.line = append(b.line, ch)
		} else {
			b.failed = true
		}
	}
	if err == io.EOF {
		b.eof = true
		if len(bytes.TrimSpace(b.line)) > 0 {
			b.frame(bytes.TrimSpace(b.line))
		}
		b.observeEvent(b.decoder.end())
	}
	return n, err
}

func (b *codexProfileStream) frame(line []byte) {
	b.observeEvent(b.decoder.line(line))
}

func (b *codexProfileStream) observeEvent(event *codexSSEEvent) {
	if event == nil {
		return
	}
	if result, _ := b.handler.tokenParser.ParseStreamingLine(append([]byte("data: "), event.data...)); result != nil {
		b.usage = result
	}
	if event.err != nil {
		b.failed = true
		b.errorFrame = append(b.errorFrame[:0], event.data...)
	}
	b.completed = b.completed || event.completed
	if (b.failed || b.completed) && b.request != nil && event.frame != nil {
		b.handler.observeProfileResponse(b.request.Context(), b.candidate, b.status, b.headers, event.data)
	}
	for _, key := range codexResponseStateKeys(b.request, event.frame) {
		if len(b.keys) >= 256 {
			break
		}
		if !slices.Contains(b.keys, key) {
			b.keys = append(b.keys, key)
		}
	}
}

func profileUsage(result *TokenResult) (tokens, cached int64) {
	if result == nil {
		return 0, 0
	}
	// Codex input already includes cached tokens; reasoning is an output
	// detail. Neither is added a second time to the local load signal.
	return result.InputTokens + result.OutputTokens, result.CacheReadTokens
}

func codexAttemptUsage(result *TokenResult, complete bool, requestedTier ...string) routing.AttemptUsage {
	if result == nil {
		return routing.AttemptUsage{Complete: complete}
	}
	tier := ""
	if len(requestedTier) > 0 {
		tier = requestedTier[0]
	}
	return routing.AttemptUsage{RequestedServiceTier: tier, CacheWrite30mTokens: result.CacheWrite30mTokens, CacheWrite12hTokens: result.CacheWrite12hTokens, Known: true, Complete: complete, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, ReasoningTokens: result.ReasoningTokens, CacheReadTokens: result.CacheReadTokens, CacheCreationTokens: result.CacheWriteTokens, InputIncludesCacheRead: true, ServiceTier: result.ServiceTier}
}

func codexProfileProtocolSuccess(path string, data []byte) bool {
	var frame map[string]any
	if json.Unmarshal(data, &frame) != nil || frame["error"] != nil {
		return false
	}
	if path == "/models" {
		_, models := frame["models"].([]any)
		_, list := frame["data"].([]any)
		return models || frame["object"] == "list" && list
	}
	if path == "/alpha/search" {
		_, ok := frame["output"].(string)
		return ok
	}
	if path == "/images/generations" || path == "/images/edits" {
		return codexImageProtocolSuccess(frame)
	}
	if path == "/responses/compact" {
		_, output := frame["output"].([]any)
		return output && (frame["object"] == "response.compaction" || frame["type"] == "response.compaction")
	}
	return frame["status"] == "completed"
}

func (h *Handler) serveProfileRouted(w http.ResponseWriter, r *http.Request, body []byte, requestID string, start time.Time, streaming bool, plan *routing.DispatchPlan) {
	runtime := h.manager.PoolRuntime().ProfileRuntime()
	globalBudget := h.retryConfig()
	// Snapshot the global policy for this request, ignoring the legacy profile flag.
	plan.Profile.WaitForRecovery = !globalBudget.DisableRecoveryWait
	plan.Budget.Limit(globalBudget.MaxTotalAttempts, globalBudget.MaxTotalDuration)
	r = r.WithContext(routing.WithProfileRequest(r.Context(), plan))
	var chain []chainNode
	publishProgress := func() {
		if h.usageTracker == nil {
			return
		}
		trace := plan.Trace()
		data, _ := json.Marshal(trace)
		snapshot := string(data)
		retries := max(0, trace.Attempts-1)
		opts := tracking.UpdateOptions{RouteSnapshot: &snapshot, RetryCount: &retries}
		if len(chain) > 0 {
			data, _ = json.Marshal(chain)
			chainJSON := string(data)
			opts.EndpointChain = &chainJSON
		}
		h.usageTracker.RecordRequestUpdate(requestID, opts)
	}
	// Publish before archival. A deferred update after handle* returns is lost
	// because the request has already left the active hot pool.
	publishProgress()
	if plan.Error != "" && !(plan.Error == "route_unavailable" && plan.Profile.WaitForRecovery) {
		plan.End(plan.Error)
		publishProgress()
		if h.trySearchFallback(w, r, body, requestID, start, chain) {
			return
		}
		rejection := plan.RejectLocal(plan.Error)
		h.recordFailure(requestID, start, plan.Error, plan.FailureDetail(), nil, plan.Model)
		if rejection != nil {
			rejection.Write(w)
		} else {
			h.writeError(w, http.StatusBadRequest, plan.Error)
		}
		return
	}
	var lastResponse *http.Response
	var lastResponseInvalidStream bool
	var lastBody []byte
	var lastUpstreamError string
	lastFailure := "route_unavailable"
	for {
		for _, candidate := range plan.EligibleCandidates {
			if err := r.Context().Err(); err != nil {
				plan.End("client_cancelled")
				return
			}
			ep := h.manager.dispatchEndpoint(candidate)
			if ep == nil || candidate.StateVersion != 0 && candidate.StateVersion != routing.StateVersion(&ep.stateVersion) {
				plan.Skip(candidate, "candidate_configuration_changed")
				continue
			}
			channel := h.manager.ChannelByID(candidate.ChannelID)
			if channel == nil {
				plan.Skip(candidate, "channel_missing")
				continue
			}
			if ep.DailyCostLimitUSD > 0 {
				if h.usageTracker == nil {
					plan.Skip(candidate, "daily_cost_state_unavailable")
					continue
				}
				used, _, err := h.usageTracker.CredentialGroupDailyCost(r.Context(), "codex", channel.Name, ep.CredentialGroupName)
				if err != nil {
					plan.Skip(candidate, "daily_cost_state_unavailable")
					continue
				}
				if used >= ep.DailyCostLimitUSD {
					plan.Skip(candidate, "daily_cost_limit_exceeded")
					continue
				}
			}
			attempt, err := runtime.Reserve(r.Context(), plan, candidate)
			if err != nil {
				if errors.Is(err, routing.ErrBudgetExhausted) {
					lastFailure = routing.BudgetStopReason(err)
					break
				}
				plan.Skip(candidate, err.Error())
				continue
			}
			availability := h.manager.PoolRuntime()
			releaseHealth, err := availability.AcquireHealth(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model)
			if err != nil {
				attempt.Finish(0, false, routing.Failure{})
				plan.Skip(candidate, err.Error())
				continue
			}
			sent, quotaFailure := false, false
			recovery := h.newResponseRecoveryState(r, requestID)
			finish := func(tokens int64, successful bool, failure routing.Failure, retryAt time.Time) routing.Failure {
				if recovery.rule.ID != "" && !successful {
					failure.Scope, failure.Terminal = routing.ScopeRequest, true
				}
				releaseHealth(successful)
				if sent && !successful && failure.Scope != "" && !quotaFailure && r.Context().Err() == nil {
					_ = availability.Fail(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model, failure, retryAt)
				}
				attempt.Finish(tokens, successful, failure)
				if sent {
					closeLastHop(chain, start)
				}
				return failure
			}
			sendRequest := r
			var lease *subscription.Lease
			if ep.AuthMode == subscription.ClientPassthrough {
				if h.subscriptions == nil {
					err = errors.New("subscription_state_unavailable")
				} else {
					var selected context.Context
					selected, err = h.subscriptions.SelectProfileAccount(r.Context(), ep.ChannelID, ep.CredentialGroupID, r.Header)
					if err == nil {
						binding := subscription.FromContext(selected).Binding
						lease, err = h.subscriptions.Acquire(selected, binding, candidate.Identity().Model)
						if err == nil {
							sendRequest = r.WithContext(context.WithValue(selected, subscriptionAdmissionKey{}, lease))
						}
					}
				}
			}
			if err != nil {
				finish(0, false, routing.Failure{}, time.Time{})
				lastFailure = "credential_unavailable"
				plan.Skip(candidate, lastFailure)
				continue
			}
			outbound := h.outboundBody(r, requestID, channel, body)
			rewrite := modelRewriteResult{Body: outbound}
			if !isCodexImageRequest(r) && !isCodexSearchRequest(r) {
				rewrite = rewriteModelForRoute(outbound, candidate.Identity().Model, h.logger, requestID)
			}
			recordSend := func() {
				sent = true
				chain = append(chain, chainNode{Hop: len(chain) + 1, Channel: channel.Name, CredentialGroup: ep.CredentialGroupName, Endpoint: ep.Name, Attempts: 1, StartMs: time.Since(start).Milliseconds(), RemovedReasoningItems: recovery.removedReasoningItems, RestoredReasoningItems: recovery.restoredReasoningItems})
				h.recordForwarding(requestID, ep.Name, channel.Name, candidate.Identity().Model, chain)
				publishProgress()
			}
			beforeSend := func() error {
				if err := attempt.Begin(sendRequest.Context()); err != nil {
					return err
				}
				recordSend()
				return nil
			}
			sendRequest = sendRequest.WithContext(context.WithValue(sendRequest.Context(), codexProfileBeforeSendKey{}, beforeSend))
			var rejectedResponse *http.Response
			recovery.onRejected = func(resp *http.Response, raw []byte) {
				attempt.SetUsage(codexAttemptUsage(h.tokenParser.ParseNonStreaming(raw), true, extractServiceTier(body)))
				h.observeProfileResponse(sendRequest.Context(), candidate, resp.StatusCode, resp.Header, raw)
				rejectedResponse = resp
				text := subscription.Redact(sendRequest.Context(), chainBodyText(raw, recovery.rule.ID))
				annotateLastChainError(&chain, resp.StatusCode, text, resp.Header, false)
				closeLastHop(chain, start)
				publishProgress()
			}
			recovery.beforeRetry = func(ctx context.Context) error {
				if err := attempt.Retry(ctx, recovery.rule.ID); err != nil {
					return err
				}
				recordSend()
				return nil
			}
			sendRequest = sendRequest.WithContext(withResponseRecoveryState(sendRequest.Context(), recovery))
			fwd := h.forwarder
			if streaming {
				fwd = h.streamFwd
			}
			resp, err := fwd.Forward(sendRequest, rewrite.Body, ep)
			if err != nil {
				failure := codexRouteFailure(0, nil, err)
				failure = finish(0, false, failure, time.Time{})
				if sent {
					lastUpstreamError = subscription.Redact(sendRequest.Context(), chainErrorText(err, "", failure.Reason))
					annotateLastChainError(&chain, 0, lastUpstreamError, nil, true)
					publishProgress()
				}
				lastFailure = failure.Reason
				if !sent {
					plan.Skip(candidate, "request_not_sent")
				}
				if failure.Terminal {
					break
				}
				continue
			}
			if resp.StatusCode >= 400 {
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				diagnosticBody := raw
				if recovery.rule.ID != "" && resp.Request != nil {
					if plain, decoded := decodeRecoveryErrorBody(resp, raw); decoded {
						diagnosticBody = plain
					}
					diagnosticBody = redactCodexErrorBody(resp.Request, diagnosticBody)
				}
				attempt.SetUsage(codexAttemptUsage(h.tokenParser.ParseNonStreaming(diagnosticBody), true, extractServiceTier(body)))
				if rejectedResponse != resp {
					h.observeProfileResponse(sendRequest.Context(), candidate, resp.StatusCode, resp.Header, diagnosticBody)
				}
				failure := codexRequestFailure(sendRequest, resp.StatusCode, diagnosticBody, nil)
				quotaFailure = codexProfileQuotaFailure(sendRequest.Context(), resp.StatusCode, resp.Header, diagnosticBody, candidate.Identity().Model)
				failure = finish(0, false, failure, routing.RetryAfter(resp.Header, time.Now()))
				lastUpstreamError = subscription.Redact(sendRequest.Context(), chainBodyText(diagnosticBody, failure.Reason))
				annotateLastChainError(&chain, resp.StatusCode, lastUpstreamError, resp.Header, false)
				publishProgress()
				lastFailure, lastResponse, lastBody = failure.Reason, resp, raw
				lastResponseInvalidStream = false
				if resp.StatusCode == http.StatusUnauthorized && h.subscriptions != nil {
					h.subscriptions.RejectCredential(sendRequest.Context())
				}
				if failure.Terminal {
					break
				}
				continue
			}
			writer := &codexProfileWriter{ResponseWriter: w, attempt: attempt}
			if len(chain) > 0 {
				chain[len(chain)-1].StatusCode = resp.StatusCode
			}
			publishProgress()
			if streaming {
				if err := decodeCodexSubscriptionResponse(resp); err != nil {
					resp.Body.Close()
					failure := finish(0, false, routing.Failure{Scope: routing.ScopeCandidate, Reason: "invalid_response_encoding"}, time.Time{})
					if failure.Terminal {
						lastFailure = failure.Reason
						break
					}
					continue
				}
				observed := &codexProfileStream{ReadCloser: resp.Body, handler: h, request: sendRequest, candidate: candidate, status: resp.StatusCode, headers: resp.Header.Clone()}
				resp.Body = observed
				settled, success := false, false
				var failure routing.Failure
				settleStream := func(streamErr *UpstreamStreamError, served bool) {
					if settled {
						return
					}
					settled = true
					tokens, cached := profileUsage(observed.usage)
					attempt.SetCachedTokens(cached)
					success = served && observed.completed && !observed.failed
					failure = routing.Failure{Scope: routing.ScopeCandidate, Reason: "incomplete_response"}
					if streamErr != nil {
						failure = codexRouteFailure(http.StatusOK, nil, streamErr)
						if streamErr.Type == "invalid_stream_response" {
							failure = routing.Failure{Scope: routing.ScopeCandidate, Reason: streamErr.Code}
						} else if streamErr.Type == "response_incomplete" {
							failure = routing.Failure{Scope: routing.ScopeCandidate, Reason: "incomplete_response"}
						}
						quotaFailure = codexProfileQuotaFailure(sendRequest.Context(), resp.StatusCode, resp.Header, observed.errorFrame, candidate.Identity().Model)
					}
					attempt.SetUsage(codexAttemptUsage(observed.usage, success, extractServiceTier(body)))
					failure = finish(tokens, success, failure, routing.RetryAfter(resp.Header, time.Now()))
					if streamErr != nil {
						lastUpstreamError = subscription.Redact(sendRequest.Context(), chainBodyText(observed.errorFrame, streamErr.Error()))
						annotateLastChainError(&chain, resp.StatusCode, lastUpstreamError, resp.Header, false)
					}
					publishProgress()
				}
				streamErr, served := h.handleStreaming(writer, resp, requestID, start, candidate.Identity().Model, ep.Name, ep.CredentialGroupName, channel.Name, plan.Trace().Attempts, settleStream)
				settleStream(streamErr, served)
				h.observeProfileResponse(sendRequest.Context(), candidate, resp.StatusCode, resp.Header, nil)
				if success && h.states != nil {
					h.states.Observe(observed.keys, routing.ExplicitTurn(sendRequest.Context(), sendRequest.Header), candidate.Identity())
					runtime.RememberResponseStates(plan, candidate, observed.keys)
				}
				if writer.started || success {
					return
				}
				lastFailure = failure.Reason
				if streamErr != nil {
					lastBody, _ = io.ReadAll(resp.Body)
					lastResponse = resp
					lastResponseInvalidStream = streamErr.InvalidProtocol
					if lastResponseInvalidStream {
						lastUpstreamError = subscription.Redact(sendRequest.Context(), chainBodyText(lastBody, streamErr.Error()))
						annotateLastChainError(&chain, resp.StatusCode, lastUpstreamError, resp.Header, false)
						publishProgress()
					}
				}
				if failure.Terminal {
					break
				}
				continue
			}
			if err := decodeCodexSubscriptionResponse(resp); err != nil {
				resp.Body.Close()
				failure := finish(0, false, routing.Failure{Scope: routing.ScopeCandidate, Reason: "invalid_response_encoding"}, time.Time{})
				if failure.Terminal {
					lastFailure = failure.Reason
					break
				}
				continue
			}
			raw, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			h.observeProfileResponse(sendRequest.Context(), candidate, resp.StatusCode, resp.Header, raw)
			success := readErr == nil && codexProfileProtocolSuccess(subscription.Path(sendRequest), raw)
			usage := h.tokenParser.ParseNonStreaming(raw)
			attempt.SetUsage(codexAttemptUsage(usage, success, extractServiceTier(body)))
			tokens, cached := profileUsage(usage)
			attempt.SetCachedTokens(cached)
			if !success {
				failure := codexRequestFailure(sendRequest, resp.StatusCode, raw, readErr)
				quotaFailure = codexProfileQuotaFailure(sendRequest.Context(), resp.StatusCode, resp.Header, raw, candidate.Identity().Model)
				failure = finish(tokens, false, failure, routing.RetryAfter(resp.Header, time.Now()))
				if resp.StatusCode >= 400 {
					lastUpstreamError = subscription.Redact(sendRequest.Context(), chainBodyText(raw, failure.Reason))
				} else {
					lastUpstreamError = failure.Reason
				}
				annotateLastChainError(&chain, resp.StatusCode, lastUpstreamError, resp.Header, false)
				publishProgress()
				lastFailure, lastResponse, lastBody = failure.Reason, resp, raw
				lastResponseInvalidStream = false
				if failure.Terminal {
					break
				}
				continue
			}
			resp.Body = io.NopCloser(bytes.NewReader(raw))
			closeLastHop(chain, start)
			publishProgress()
			h.handleNonStreaming(writer, resp, requestID, start, candidate.Identity().Model, ep.Name, ep.CredentialGroupName, channel.Name, plan.Trace().Attempts, func() {
				finish(tokens, true, routing.Failure{}, time.Time{})
				publishProgress()
			})
			if h.states != nil {
				stateKeys := h.observeRoutedState(sendRequest, candidate.Identity(), raw)
				runtime.RememberResponseStates(plan, candidate, stateKeys)
			}
			return
		}
		recovered, err := runtime.WaitForRecovery(r.Context(), plan, func(candidate routing.DispatchCandidate) (time.Time, bool) {
			availability := h.manager.PoolRuntime()
			ready, valid := availability.HealthReadyAt(candidate.ChannelID, candidate.GroupID, candidate.EndpointID, candidate.Identity().Model)
			if !valid {
				return time.Time{}, false
			}
			ep := h.manager.dispatchEndpoint(candidate)
			if ep == nil {
				return time.Time{}, false
			}
			if ep.AuthMode == subscription.ClientPassthrough {
				if h.subscriptions == nil {
					return time.Time{}, false
				}
				at, ok := h.subscriptions.ProfileRecoveryTime(r.Context(), candidate.ChannelID, candidate.GroupID, candidate.Identity().Model)
				if !ok {
					return time.Time{}, false
				}
				if at.After(ready) {
					ready = at
				}
			}
			return ready, true
		})
		if err != nil {
			if errors.Is(err, routing.ErrBudgetExhausted) {
				lastFailure = routing.BudgetStopReason(err)
			} else {
				lastFailure = "client_cancelled"
			}
			break
		}
		if !recovered {
			break
		}
	}
	if plan.Budget.Stopped() {
		lastFailure = plan.Budget.Reason()
	}
	plan.End(lastFailure)
	if routing.IsBudgetStopReason(lastFailure) {
		trace := plan.Trace()
		trace.StopReason = lastFailure
		if h.usageTracker != nil {
			data, _ := json.Marshal(trace)
			snapshot := string(data)
			chainJSON, _ := json.Marshal(chain)
			chainString := string(chainJSON)
			opts := tracking.UpdateOptions{RouteSnapshot: &snapshot, EndpointChain: &chainString}
			if lastUpstreamError != "" {
				opts.LastFailureReason = &lastUpstreamError
			}
			h.usageTracker.RecordRequestUpdate(requestID, opts)
		}
	} else {
		publishProgress()
	}
	if h.trySearchFallback(w, r, body, requestID, start, chain) {
		return
	}
	if lastResponse != nil {
		lastResponse.Body = io.NopCloser(bytes.NewReader(lastBody))
		detail := lastUpstreamError
		if detail == "" {
			detail = lastFailure
		}
		if streaming && lastResponseInvalidStream {
			detail = subscription.Redact(r.Context(), chainBodyText(lastBody, detail))
		}
		h.recordUpstreamFailure(requestID, start, lastFailure, detail, lastResponse.StatusCode, nil, plan.Model)
		if streaming && lastResponseInvalidStream {
			lastResponse.Body.Close()
			h.writeInvalidStreamFailure(w)
			return
		}
		h.passthroughWithBody(w, lastResponse, lastBody)
		return
	}
	status := http.StatusServiceUnavailable
	if routing.IsBudgetStopReason(lastFailure) {
		status = http.StatusGatewayTimeout
	} else if strings.HasPrefix(lastFailure, "state_") {
		status = http.StatusBadRequest
	}
	detail := lastUpstreamError
	if detail == "" {
		detail = lastFailure
	}
	if rejection := plan.RejectLocal(lastFailure); rejection != nil {
		h.recordFailure(requestID, start, lastFailure, detail, nil, plan.Model)
		rejection.Write(w)
		return
	}
	h.recordFailure(requestID, start, lastFailure, detail, nil, plan.Model)
	h.writeError(w, status, lastFailure)
}
