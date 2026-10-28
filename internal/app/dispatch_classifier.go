package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/transport"
	"github.com/wsyangzy/Vane/internal/privacy"
	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudesub "github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexsub "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func (a *App) classifierPrompt(ctx context.Context, p routing.RoutingProfile, turn routing.TurnInput) (string, error) {
	if a.privacyService == nil {
		return "", errors.New("classifier_privacy_unavailable")
	}
	limit := func(s string, head, tail int) string {
		r := []rune(s)
		if len(r) <= head+tail {
			return s
		}
		return string(r[:head]) + "\n[context truncated]\n" + string(r[len(r)-tail:])
	}
	if len(turn.CurrentUserMessage)+len(turn.PreviousUserMessage)+len(turn.PreviousAssistantText) > 1024*1024 {
		return "", errors.New("classifier_input_too_large")
	}
	data, err := json.Marshal(turn)
	if err != nil {
		return "", errors.New("classifier_input_invalid")
	}
	defer clear(data)
	filtered, err := a.privacyService.Apply(ctx, privacy.Request{Path: "/internal/routing/classify", Method: http.MethodPost, ProviderType: p.Provider, ContentType: "application/json"}, data)
	if err != nil || filtered.Action == privacy.ModeDisabled || filtered.SnapshotVersion == 0 || filtered.Truncated || filtered.SkippedReason != "" {
		return "", errors.New("classifier_privacy_rejected")
	}
	processed, _ := filtered.SelectBody(privacy.ChannelLevelThirdParty, data)
	var input map[string]string
	if json.Unmarshal(processed, &input) != nil {
		return "", errors.New("classifier_input_invalid")
	}
	input["current_user_message"] = limit(input["current_user_message"], 3000, 1000)
	input["previous_user_message"] = limit(input["previous_user_message"], 750, 250)
	input["previous_assistant_text"] = limit(input["previous_assistant_text"], 750, 250)
	type choice struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	choices := []choice{}
	for _, intent := range p.Intents {
		if intent.Enabled {
			choices = append(choices, choice{intent.ID, intent.Description})
		}
	}
	prompt, err := json.Marshal(struct {
		Intents []choice          `json:"intents"`
		Input   map[string]string `json:"input"`
	}{choices, input})
	return string(prompt), err
}

// Classification chooses an explicit resource and calls its protocol adapter
// directly. It cannot re-enter business routing or cool business candidates.
func (a *App) callIntentClassifier(ctx context.Context, p routing.RoutingProfile, turn routing.TurnInput) (result routing.IntentResult, callErr error) {
	cfg := p.Classifier
	if cfg == nil || !cfg.Enabled {
		return result, errors.New("classifier_disabled")
	}
	prompt, err := a.classifierPrompt(ctx, p, turn)
	if err != nil {
		return result, err
	}
	provider := cfg.Provider
	if provider == "" {
		provider = p.Provider
	}
	runtime, err := a.dispatchRuntime(ctx, provider)
	if err != nil {
		return result, errors.New("classifier_resources_unavailable")
	}
	classifierProfile := routing.DefaultProfile(provider)
	classifierProfile.ID = p.ID
	evaluations := runtime.PreviewTargets(ctx, classifierProfile, []routing.RouteTarget{{ChannelID: cfg.ChannelID, GroupID: cfg.GroupID}}, cfg.Model)
	var candidate routing.DispatchCandidate
	found := false
	for _, evaluation := range evaluations {
		if evaluation.Eligible && evaluation.Candidate.CredentialMode != "client_passthrough" {
			candidate = evaluation.Candidate
			found = true
			break
		}
	}
	if !found {
		return result, errors.New("classifier_candidate_unavailable")
	}
	plan := routing.DispatchPlan{Profile: classifierProfile, EligibleCandidates: []routing.DispatchCandidate{candidate}}
	ctx = routing.WithProfileRequest(ctx, &plan)
	managed := candidate.CredentialMode == "managed"
	var body []byte
	var target, endpointName, channelName, groupName string
	var claudeHeaders map[string]string
	headers := http.Header{"Content-Type": []string{"application/json"}}
	sourcePath := "/v1/messages"
	if provider == "claude" {
		body, err = claudeservice.RoutingClassifierRequest(candidate.UpstreamModel, prompt)
		for _, ep := range a.endpointManager.GetAllEndpoints() {
			if claudeendpoint.EndpointKeyForConfig(ep.Config) == candidate.EndpointID && ep.Config.CredentialGroupID == candidate.GroupID {
				claudeHeaders = ep.Config.Headers
				endpointName, channelName, groupName = ep.Config.Name, claudeendpoint.ChannelKey(ep), ep.Config.CredentialGroupName
				target = strings.TrimRight(ep.Config.URL, "/") + sourcePath
				for key, value := range ep.Config.Headers {
					headers.Set(key, value)
				}
				if token := a.endpointManager.GetTokenForEndpoint(ep); token != "" {
					headers.Set("Authorization", "Bearer "+token)
				}
				if key := a.endpointManager.GetApiKeyForEndpoint(ep); key != "" {
					headers.Set("X-Api-Key", key)
				}
				break
			}
		}
		headers.Set("Anthropic-Version", "2023-06-01")
		if managed {
			if a.claudeSubscriptions == nil {
				return result, errors.New("classifier_credentials_unavailable")
			}
			ctx, err = a.claudeSubscriptions.SelectProfileAccount(ctx, candidate.ChannelID, candidate.GroupID, nil)
		}
	} else {
		sourcePath = "/responses"
		body, err = codexservice.RoutingClassifierRequest(candidate.UpstreamModel, prompt, managed)
		for _, ep := range a.codexEndpointMgr.PoolEndpoints(candidate.ChannelID, candidate.GroupID) {
			if fmt.Sprint(ep.ID) == candidate.EndpointID {
				endpointName, groupName = ep.Name, ep.CredentialGroupName
				if channel := a.codexEndpointMgr.ChannelByID(candidate.ChannelID); channel != nil {
					channelName = channel.Name
				}
				base := strings.TrimRight(ep.BaseURL, "/")
				if strings.HasSuffix(base, "/responses") {
					target = base
				} else {
					target = base + sourcePath
				}
				headers.Set("Authorization", "Bearer "+ep.APIKey)
				break
			}
		}
		if managed {
			target = "https://chatgpt.com/backend-api/codex/responses"
			if a.codexSubscriptions == nil {
				return result, errors.New("classifier_credentials_unavailable")
			}
			ctx, err = a.codexSubscriptions.SelectProfileAccount(ctx, candidate.ChannelID, candidate.GroupID, nil)
		}
	}
	if err != nil || target == "" {
		return result, errors.New("classifier_credentials_unavailable")
	}
	defer clear(body)
	src, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost"+sourcePath, nil)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return result, errors.New("classifier_endpoint_invalid")
	}
	request.Header = headers
	mode := candidate.CredentialMode
	if managed {
		mode = "client_passthrough"
	}
	if provider == "claude" {
		err = claudesub.AuthorizeOutbound(ctx, src, request, mode, candidate.GroupID, claudeHeaders)
	} else {
		err = codexsub.AuthorizeOutbound(src, request, mode, candidate.GroupID)
	}
	if err != nil {
		return result, errors.New("classifier_egress_rejected")
	}
	start := time.Now()
	balanceStore, err := a.profileStore(ctx)
	if err != nil {
		return result, errors.New("classifier_balance_unavailable")
	}
	balance, err := currentManualBalance(balanceStore, candidate)
	if err != nil || balance.Exhausted() {
		return result, errors.New("classifier_balance_unavailable")
	}
	sent := false
	id := fmt.Sprintf("routing-classifier-%d", start.UnixNano())
	var usage *tracking.TokenUsage
	status := 0
	if a.usageTracker != nil {
		a.usageTracker.RecordRequestStart(id, "", "Vane routing classifier", http.MethodPost, "/internal/routing/classify", managed && provider == "codex")
		kind := `{"kind":"routing_classifier"}`
		model := candidate.UpstreamModel
		a.usageTracker.RecordRequestUpdate(id, tracking.UpdateOptions{Provider: &provider, Channel: &channelName, GroupName: &groupName, EndpointName: &endpointName, ModelName: &model, RouteSnapshot: &kind})
	}
	defer func() {
		if sent {
			settledUsage := routing.AttemptUsage{Known: usage != nil, Complete: usage != nil}
			if usage != nil {
				settledUsage.InputTokens, settledUsage.OutputTokens, settledUsage.ReasoningTokens = usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens
				settledUsage.CacheReadTokens, settledUsage.CacheCreationTokens = usage.CacheReadTokens, usage.CacheCreationTokens
				settledUsage.CacheCreation5mTokens, settledUsage.CacheCreation1hTokens = usage.CacheCreation5mTokens, usage.CacheCreation1hTokens
				settledUsage.InputIncludesCacheRead = usage.InputIncludesCacheRead
			}
			a.settleManualBalance(balanceStore, routing.AttemptSettlement{Candidate: candidate, ID: id, StartedAt: start, Usage: settledUsage})
		}
		if usage != nil {
			result.InputTokens, result.OutputTokens = usage.InputTokens, usage.OutputTokens
			if a.modelPricingService != nil {
				pricing := a.modelPricingService.ToTrackingPricing(a.modelPricingService.GetPricingOrDefault(ctx, candidate.UpstreamModel, provider))
				mult := tracking.DefaultEndpointMultiplier()
				if a.usageTracker != nil {
					mult = a.usageTracker.GetEndpointMultiplierForEndpointAndModelWithGroup(provider, channelName, groupName, endpointName, candidate.UpstreamModel)
				}
				result.Cost = tracking.CalculateCost(usage, &pricing, &mult).TotalCost
			}
		}
		if a.usageTracker != nil {
			if cost, known := a.usageTracker.CalculateAttemptCost(provider, channelName, groupName, endpointName, candidate.UpstreamModel, "", usage); known {
				result.Cost = cost
			}
			if callErr == nil {
				a.usageTracker.RecordRequestSuccessWithHTTPStatus(id, candidate.UpstreamModel, usage, time.Since(start), status)
			} else {
				a.usageTracker.RecordRequestFinalFailureWithUpstreamStatus(id, candidate.UpstreamModel, "error", "routing_classifier_failed", "", time.Since(start), status, usage)
			}
		}
	}()
	classifierTransport, err := transport.CreateTransport(a.config)
	if err != nil {
		return result, errors.New("classifier_transport_unavailable")
	}
	defer classifierTransport.CloseIdleConnections()
	client := &http.Client{Timeout: 30 * time.Second, Transport: classifierTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	sent = true
	response, err := client.Do(request)
	if err != nil {
		return result, errors.New("classifier_request_failed")
	}
	defer response.Body.Close()
	status = response.StatusCode
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	defer clear(data)
	if err != nil || len(data) > 1024*1024 {
		return result, errors.New("classifier_response_incomplete")
	}
	if provider == "claude" {
		result, usage, err = claudeservice.ParseRoutingClassifierResponse(status, response.Header, data)
	} else {
		result, usage, err = codexservice.ParseRoutingClassifierResponse(status, response.Header, data)
	}
	result.Model = candidate.UpstreamModel
	return result, err
}
