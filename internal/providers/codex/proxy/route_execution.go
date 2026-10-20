package codexproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wsyangzy/Vane/internal/routing"
)

func (s *codexRouteStage) candidate(ep *CodexEndpoint, requestedModel string) routing.Candidate {
	model := s.upstreamModel
	if model == "" {
		model = effectiveCodexModel(ep, s.channel, requestedModel)
	}
	credential := fmt.Sprintf("%d/%d", s.channel.ID, ep.CredentialGroupID)
	if ep.CredentialGroupID == 0 {
		credential = fmt.Sprintf("endpoint/%d", ep.ID)
	}
	return routing.Candidate{Provider: "codex", Protocol: "responses", Channel: s.channel.Name, Credential: credential, Endpoint: fmt.Sprint(ep.ID), Model: model, StateVersion: routing.StateVersion(&ep.stateVersion)}
}

func (s *codexRouteStage) executionSnapshot() string {
	var snapshot routing.Snapshot
	_ = json.Unmarshal([]byte(s.snapshotJSON), &snapshot)
	if s.subscriptionStatus != nil {
		snapshot.Subscription = s.subscriptionStatus()
	}
	snapshot.Execution = s.execution
	snapshot.Attempts, snapshot.WaitMs = s.budget.Stats()
	if s.budget.Stopped() {
		snapshot.StopReason = "retry_budget_exhausted"
	}
	return snapshot.JSON(s.index)
}

func codexRouteFailure(status int, body []byte, err error) routing.Failure {
	if status == http.StatusUnauthorized {
		return routing.Failure{Scope: routing.ScopeCredential, Reason: "credential_rejected"}
	}
	code, kind, _ := routing.ErrorIdentity(body)
	if streamErr, ok := err.(*UpstreamStreamError); ok {
		code, kind = streamErr.Code, streamErr.Type
	}
	code = strings.ToLower(code)
	kind = strings.ToLower(kind)
	switch code {
	case "model_not_found", "model_not_available", "model_not_supported", "unsupported_model", "model_access_denied", "context_length_exceeded", "input_too_long", "streaming_not_supported", "unsupported_stream":
		return routing.Failure{Scope: routing.ScopeModel, Reason: code}
	}
	if errors.Is(err, routing.ErrBudgetExhausted) {
		return routing.Failure{Scope: routing.ScopeRequest, Reason: "retry_budget_exhausted", Terminal: true}
	}
	if status == 400 || (status == 0 || status == 200) && (kind == "invalid_request_error" || kind == "bad_request") {
		return routing.Failure{Scope: routing.ScopeRequest, Reason: "invalid_request", Terminal: true}
	}
	if status == 403 {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: "permission_scope_unknown"}
	}
	if status == 429 || status == 200 {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: "upstream_unavailable"}
	}
	if status == 0 && err != nil && strings.Contains(err.Error(), "timeout awaiting response headers") {
		return routing.Failure{Scope: routing.ScopeRequest, Reason: "response_header_timeout", Terminal: true}
	}
	return routing.Failure{Scope: routing.ScopeEndpoint, Reason: "endpoint_unavailable"}
}

func codexStateKeys(r *http.Request, body []byte) []string {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	var keys []string
	if id, ok := payload["previous_response_id"].(string); ok && id != "" {
		keys = append(keys, routing.StateKey(r.Context(), "response_id", id))
	}
	if items, ok := payload["input"].([]any); ok {
		keys = append(keys, codexItemStateKeys(r, items)...)
	}
	return keys
}

func codexItemStateKeys(r *http.Request, items []any) []string {
	var keys []string
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := obj["type"].(string)
		if kind == "function_call" {
			// Official sealed tool arguments belong to their issuing account.
			// An empty array is the client's explicit unsealed representation.
			switch sealed := obj["encrypted_function_args"].(type) {
			case string:
				if sealed != "" {
					keys = append(keys, routing.StateKey(r.Context(), "encrypted_function_args", sealed))
				}
			case []any:
				if len(sealed) > 0 {
					raw, _ := json.Marshal(sealed)
					keys = append(keys, routing.StateKey(r.Context(), "encrypted_function_args", string(raw)))
				}
			}
		}
		if kind != "reasoning" && kind != "compaction" {
			continue
		}
		if value, ok := obj["encrypted_content"].(string); ok && value != "" {
			keys = append(keys, routing.StateKey(r.Context(), "encrypted_content", value))
		}
	}
	return keys
}

func (h *Handler) observeRoutedState(r *http.Request, c routing.Candidate, body []byte) {
	var keys []string
	success, failed := false, false
	routing.JSONFrames(body, func(frame map[string]any) {
		typ, _ := frame["type"].(string)
		if typ == "response.failed" || typ == "error" || frame["error"] != nil {
			failed = true
		}
		response := frame
		if obj, ok := frame["response"].(map[string]any); ok {
			response = obj
		}
		if status, _ := response["status"].(string); status == "completed" || typ == "response.completed" {
			success = true
		}
		if id, ok := response["id"].(string); ok && id != "" {
			keys = append(keys, routing.StateKey(r.Context(), "response_id", id))
		}
		if items, ok := response["output"].([]any); ok {
			keys = append(keys, codexItemStateKeys(r, items)...)
		}
	})
	if success && !failed {
		h.states.Observe(keys, routing.ExplicitTurn(r.Context(), r.Header), c)
	}
}
