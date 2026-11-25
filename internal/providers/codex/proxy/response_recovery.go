package codexproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

func defaultResponseRecoveryConfig() routing.ResponseRecoveryConfig {
	return routing.ResponseRecoveryConfig{Enabled: true, Rules: []routing.ResponseRecoveryRule{{
		ID: "invalid_encrypted_content", Enabled: true, Action: routing.ResponseRecoveryRetrySameTarget,
		StatusCodes: []int{http.StatusBadRequest}, ErrorCodes: []string{"invalid_encrypted_content"},
		Methods: []string{http.MethodPost}, Paths: []string{"/responses", "/responses/compact"},
		MaxRetries: 1, DelayMS: 250,
	}}}
}

// These Go configuration methods are intentionally not registered as Wails
// services or HTTP routes. Future settings/UI adapters can reuse the validated
// configuration contract and supply persistence at the application boundary.
func (h *Handler) GetResponseRecoveryConfig() routing.ResponseRecoveryConfig {
	h.configMu.RLock()
	defer h.configMu.RUnlock()
	if h.responseRecoveryCfg == nil {
		return defaultResponseRecoveryConfig()
	}
	return h.responseRecoveryCfg.Clone()
}

func (h *Handler) UpdateResponseRecoveryConfig(cfg routing.ResponseRecoveryConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	snapshot := cfg.Clone()
	h.configMu.Lock()
	h.responseRecoveryCfg = &snapshot
	h.configMu.Unlock()
	return nil
}

func (h *HTTPHandler) GetResponseRecoveryConfig() routing.ResponseRecoveryConfig {
	if h.handler == nil {
		return defaultResponseRecoveryConfig()
	}
	return h.handler.GetResponseRecoveryConfig()
}

func (h *HTTPHandler) UpdateResponseRecoveryConfig(cfg routing.ResponseRecoveryConfig) error {
	if h.handler == nil {
		return errors.New("codex_handler_unavailable")
	}
	return h.handler.UpdateResponseRecoveryConfig(cfg)
}

type responseRecoveryConfigKey struct{}
type responseRecoveryStateKey struct{}

func (h *Handler) withResponseRecoveryConfig(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), responseRecoveryConfigKey{}, h.GetResponseRecoveryConfig()))
}

func (h *Handler) newResponseRecoveryState(r *http.Request, requestID string) *responseRecoveryState {
	cfg, ok := r.Context().Value(responseRecoveryConfigKey{}).(routing.ResponseRecoveryConfig)
	if !ok {
		cfg = h.GetResponseRecoveryConfig()
	}
	return &responseRecoveryState{requestID: requestID, cfg: cfg, attempts: 1}
}

// Recovery stays inside the selected destination's admission. The caller
// records every send and ends the request when the selected rule is exhausted.
type responseRecoveryState struct {
	requestID   string
	cfg         routing.ResponseRecoveryConfig
	rule        routing.ResponseRecoveryRule
	attempts    int
	retries     int
	onRejected  func(*http.Response, []byte)
	beforeRetry func(context.Context) error
}

func (r *responseRecoveryState) retried() bool { return r.retries > 0 }

func withResponseRecoveryState(ctx context.Context, recovery *responseRecoveryState) context.Context {
	return context.WithValue(ctx, responseRecoveryStateKey{}, recovery)
}

func responseRecoveryStateFromContext(ctx context.Context) *responseRecoveryState {
	recovery, _ := ctx.Value(responseRecoveryStateKey{}).(*responseRecoveryState)
	return recovery
}

// doWithResponseRecovery runs before any response observers or downstream
// output. Replaying the prepared request keeps the URL, credentials, headers,
// model and encrypted items identical, including for managed subscriptions.
func (f *Forwarder) doWithResponseRecovery(client *http.Client, req, source *http.Request) (*http.Response, error) {
	recovery := responseRecoveryStateFromContext(req.Context())
	if recovery == nil || !recovery.cfg.Enabled || recovery.beforeRetry == nil || req.GetBody == nil || req.Body == nil || req.Body == http.NoBody {
		return client.Do(req)
	}
	metadata := routing.ResponseRecoveryMatch{Method: source.Method, Path: subscription.Path(source)}
	// Snapshot the prepared destination and headers before observers run.
	template, getBody := req.Clone(req.Context()), req.GetBody
	resp, err := client.Do(req)
	for err == nil {
		if recovery.rule.ID != "" && recovery.retries >= recovery.rule.MaxRetries {
			break
		}
		match, raw, readable := inspectRecoveryResponse(resp, metadata, recovery.cfg)
		if !readable {
			break
		}
		if recovery.rule.ID == "" {
			var matched bool
			recovery.rule, matched = recovery.cfg.Match(match)
			if !matched {
				break
			}
		} else if !recovery.rule.Matches(match) {
			// A different error does not start another policy's retry budget.
			break
		}
		switch recovery.rule.Action {
		case routing.ResponseRecoveryRetrySameTarget:
			if resp.Request == nil {
				resp.Request = req
			}
			if recovery.onRejected != nil {
				recovery.onRejected(resp, redactCodexErrorBody(template, raw))
			}
			f.logger.Info("[Codex 响应恢复等待]", "request_id", recovery.requestID,
				"policy_id", recovery.rule.ID, "delay_ms", recovery.rule.DelayMS,
				"retry", recovery.retries+1, "max_retries", recovery.rule.MaxRetries)
			if waitErr := routing.BudgetFromContext(req.Context()).Wait(req.Context(), recovery.rule.Delay()); waitErr != nil {
				return resp, nil
			}
			replay := template.Clone(template.Context())
			replay.Body, err = getBody()
			if err != nil {
				return resp, nil
			}
			// The policy owns the retry count; transport auto-replay must not
			// send additional requests outside that count or the shared budget.
			replay.GetBody = nil
			if err := recovery.beforeRetry(replay.Context()); err != nil {
				replay.Body.Close()
				return resp, nil
			}
			recovery.retries++
			recovery.attempts++
			resp.Body.Close()
			req = replay
			resp, err = client.Do(replay)
		default:
			return resp, err
		}
	}
	return resp, err
}

type recoveryResponseBody struct {
	io.Reader
	io.Closer
}

type recoveryResponseReadError struct{ err error }

func (r recoveryResponseReadError) Read([]byte) (int, error) { return 0, r.err }

// Inspect only eligible, bounded HTTP error bodies. Restore every byte and any
// read failure so other errors retain their existing passthrough behavior.
func inspectRecoveryResponse(resp *http.Response, match routing.ResponseRecoveryMatch, cfg routing.ResponseRecoveryConfig) (routing.ResponseRecoveryMatch, []byte, bool) {
	if resp == nil || resp.StatusCode < 400 || resp.Body == nil {
		return match, nil, false
	}
	match.StatusCode = resp.StatusCode
	if !cfg.MayMatch(match) {
		return match, nil, false
	}
	original := resp.Body
	raw, err := io.ReadAll(io.LimitReader(original, maxChainErrorBodyBytes+1))
	var remainder io.Reader = original
	if err != nil {
		remainder = recoveryResponseReadError{err: err}
	}
	resp.Body = &recoveryResponseBody{Reader: io.MultiReader(bytes.NewReader(raw), remainder), Closer: original}
	if err != nil || len(raw) > maxChainErrorBodyBytes {
		return match, nil, false
	}
	plain, decoded := decodeRecoveryErrorBody(resp, raw)
	if !decoded {
		return match, nil, false
	}
	match.ErrorCode, match.ErrorType, _ = routing.ErrorIdentity(plain)
	return match, plain, true
}

// Decode a copy for inspection and history while preserving the wire response.
func decodeRecoveryErrorBody(resp *http.Response, raw []byte) ([]byte, bool) {
	decoded := *resp
	decoded.Header = resp.Header.Clone()
	decoded.Body = io.NopCloser(bytes.NewReader(raw))
	if err := decodeCodexSubscriptionResponse(&decoded); err != nil {
		decoded.Body.Close()
		return nil, false
	}
	plain, readErr := io.ReadAll(io.LimitReader(decoded.Body, maxChainErrorBodyBytes+1))
	decoded.Body.Close()
	if readErr != nil || len(plain) > maxChainErrorBodyBytes {
		return nil, false
	}
	return plain, true
}

func redactCodexErrorBody(req *http.Request, raw []byte) []byte {
	text := subscription.Redact(req.Context(), string(raw))
	for _, name := range []string{"Authorization", "X-Api-Key"} {
		value := req.Header.Get(name)
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, "[REDACTED]")
		if token := strings.TrimPrefix(value, "Bearer "); token != "" {
			text = strings.ReplaceAll(text, token, "[REDACTED]")
		}
	}
	return []byte(text)
}
