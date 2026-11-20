package codexproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/search"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
)

type searchFallbackKey struct{}

func isCodexSearchRequest(r *http.Request) bool {
	return subscription.Path(r) == "/alpha/search"
}

func (h *HTTPHandler) SetSearchConfigProvider(provider func(context.Context) (search.Config, error)) {
	h.handler.configMu.Lock()
	h.handler.searchConfig = provider
	h.handler.configMu.Unlock()
}

// Returns false only for native forwarding. An adapter result always follows
// the tool response envelope, including actionable local capability failures.
func (h *Handler) serveSearchAdapter(w http.ResponseWriter, r *http.Request, body []byte, requestID string, start time.Time) bool {
	model := h.requestModel(r, body)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.recordFailure(requestID, start, "search_request_invalid", "搜索接口仅支持 POST", nil, model)
		h.writeError(w, http.StatusMethodNotAllowed, "search requires POST")
		return true
	}
	_, err := search.Parse(body)
	if err != nil {
		h.recordFailure(requestID, start, "search_request_invalid", err.Error(), nil, model)
		h.writeError(w, http.StatusBadRequest, err.Error())
		return true
	}
	cfg := search.DefaultConfig()
	h.configMu.RLock()
	provider := h.searchConfig
	h.configMu.RUnlock()
	var configErr error
	if provider != nil {
		cfg, configErr = provider(r.Context())
	}
	if strings.HasSuffix(r.URL.Path, "/native") {
		return false
	}
	if strings.HasSuffix(r.URL.Path, "/service") {
		cfg.Mode = "service"
		cfg.Fallbacks = nil
	}
	if cfg.Mode == "native" || cfg.Mode == "auto" {
		if cfg.Mode == "auto" && configErr == nil {
			*r = *r.WithContext(context.WithValue(r.Context(), searchFallbackKey{}, cfg))
		}
		return false
	}
	return h.serveConfiguredSearch(w, r, body, requestID, start, cfg, configErr, nil)
}

func (h *Handler) trySearchFallback(w http.ResponseWriter, r *http.Request, body []byte, requestID string, start time.Time, chain []chainNode) bool {
	cfg, ok := r.Context().Value(searchFallbackKey{}).(search.Config)
	if !ok || r.Context().Err() != nil {
		return false
	}
	req, err := search.Parse(body)
	if err != nil || !req.AllowsLiveAccess() || !req.HasAdapterCommand(cfg) {
		return false
	}
	return h.serveConfiguredSearch(w, r, body, requestID, start, cfg, nil, chain)
}

func (h *Handler) serveConfiguredSearch(w http.ResponseWriter, r *http.Request, body []byte, requestID string, start time.Time, cfg search.Config, configErr error, nativeChain []chainNode) bool {
	model := h.requestModel(r, body)
	var err error
	var req search.Request
	var outcome search.Outcome
	if configErr != nil {
		outcome.Failure = &search.Failure{Reason: "search_not_configured", Detail: "搜索设置读取失败"}
		outcome.Reply.Output = "Search unavailable: 搜索设置读取失败"
	} else {
		client := *h.forwarder.client
		client.Timeout = 0 // Each candidate uses its configured timeout via context.
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		if h.forwarder.transportProvider != nil {
			client.Transport, err = h.forwarder.transportProvider.Default()
		}
		if err != nil {
			outcome.Failure = &search.Failure{Reason: "search_not_configured", Detail: "搜索服务网络配置不可用"}
			outcome.Reply.Output = "Search unavailable: 搜索服务网络配置不可用"
		} else {
			// SelectBody respects the existing privacy enforcement policy and records
			// its summary without persisting the query or conversation body.
			outbound := h.outboundBody(r, requestID, nil, body)
			req, err = search.Parse(outbound)
			if err != nil {
				outcome.Failure = &search.Failure{Reason: "search_request_invalid", Detail: err.Error()}
				outcome.Reply.Output = "Search unavailable: " + err.Error()
			} else {
				outcome = search.Run(r.Context(), &client, cfg, req, strings.TrimPrefix(requestID, "req-"))
			}
		}
	}
	chain := append([]chainNode(nil), nativeChain...)
	for _, attempt := range outcome.Attempts {
		if !attempt.Sent {
			continue
		}
		startMs := attempt.StartedAt.Sub(start).Milliseconds()
		if startMs < 0 {
			startMs = 0
		}
		endpoint := attempt.Service
		if attempt.Operation == "网页提取" {
			endpoint += " · " + attempt.Operation
		}
		chain = append(chain, chainNode{
			Hop: len(chain) + 1, Channel: "搜索服务", Endpoint: endpoint, Attempts: 1,
			StartMs: startMs, DurationMs: attempt.DurationMs, StatusCode: attempt.Status,
			UsageCredits: attempt.UsageCredits, Error: attempt.Error, Local: attempt.Status == 0,
		})
	}
	service := cfg.Name()
	if outcome.Service != "" {
		service = outcome.Service
	}
	if len(chain) > 0 {
		h.recordForwarding(requestID, service, "搜索服务", model, chain)
	}
	if outcome.Failure != nil {
		failure := outcome.Failure
		if failure.Status > 0 {
			h.recordUpstreamFailure(requestID, start, failure.Reason, failure.Detail, failure.Status, nil, model)
		} else {
			h.recordFailure(requestID, start, failure.Reason, failure.Detail, nil, model)
		}
	} else {
		h.recordSuccess(requestID, start, http.StatusOK, nil, model, service, "", "搜索服务")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(outcome.Reply)
	return true
}
