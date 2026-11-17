package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	codexproxy "github.com/wsyangzy/Vane/internal/providers/codex/proxy"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/pathpolicy"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
)

type EndpointProbeInput struct {
	ChannelID         int64  `json:"channel_id"`
	CredentialGroupID int64  `json:"credential_group_id"`
	EndpointID        int64  `json:"endpoint_id"`
	BaseURL           string `json:"base_url"`
	APIKey            string `json:"api_key,omitempty"`
	Model             string `json:"model"`
	PathStrategy      string `json:"path_strategy"`
	VerifyResponse    bool   `json:"verify_response"`
}

type EndpointProbeCandidate struct {
	PathStrategy string   `json:"path_strategy"`
	URL          string   `json:"url"`
	HTTPStatus   int      `json:"http_status"`
	Result       string   `json:"result"`
	Verified     bool     `json:"verified"`
	Models       []string `json:"models"`
}

type EndpointProbeResult struct {
	Candidates          []EndpointProbeCandidate `json:"candidates"`
	RecommendedStrategy string                   `json:"recommended_strategy"`
	ResponseVerified    bool                     `json:"response_verified"`
}

// ProbeEndpoint never persists a policy or sends conversation data. Basic
// probes are read-only model requests; generation requires the explicit flag.
func (s *CodexEndpointService) ProbeEndpoint(ctx context.Context, input EndpointProbeInput, client *http.Client) (EndpointProbeResult, error) {
	result := EndpointProbeResult{Candidates: []EndpointProbeCandidate{}}
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	if err := validateBaseURL(input.BaseURL); err != nil {
		return result, err
	}
	u, err := url.Parse(input.BaseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return result, fmt.Errorf("端点地址不能包含凭证、查询参数或片段")
	}
	key := strings.TrimSpace(input.APIKey)
	if input.ChannelID > 0 {
		channel, err := s.channelStore.GetByID(ctx, input.ChannelID)
		if err != nil {
			return result, err
		}
		if channel == nil {
			return result, fmt.Errorf("Codex 渠道不存在")
		}
		if input.Model == "" {
			input.Model = channel.Model
		}
		key = channel.ApiKey
		var endpoint *store.CodexEndpointRecord
		if input.EndpointID > 0 {
			endpoint, err = s.endpointStore.GetByID(ctx, input.EndpointID)
			if err != nil {
				return result, err
			}
			if endpoint == nil || endpoint.ChannelID != input.ChannelID {
				return result, fmt.Errorf("端点不属于当前渠道")
			}
			if input.CredentialGroupID <= 0 {
				input.CredentialGroupID = endpoint.CredentialGroupID
			}
		}
		if s.credentialGroupStore == nil && input.CredentialGroupID > 0 {
			return result, fmt.Errorf("凭证分组服务未就绪")
		}
		if s.credentialGroupStore != nil {
			var group *store.CodexCredentialGroupRecord
			if input.CredentialGroupID > 0 {
				group, err = s.credentialGroupStore.GetByID(ctx, input.CredentialGroupID)
			} else {
				group, err = s.credentialGroupStore.GetDefaultByChannelID(ctx, input.ChannelID)
			}
			if err != nil {
				return result, err
			}
			if input.CredentialGroupID > 0 && (group == nil || group.ChannelID != input.ChannelID) {
				return result, fmt.Errorf("凭证分组不属于当前渠道")
			}
			if group != nil {
				if group.AuthMode == subscription.ClientPassthrough {
					return result, fmt.Errorf("官方订阅使用固定路径，无需检测第三方路径")
				}
				input.CredentialGroupID = group.ID
				if group.ApiKey != "" {
					key = group.ApiKey
				}
			}
		}
		if endpoint != nil && (endpoint.CredentialGroupID <= 0 || endpoint.CredentialGroupID == input.CredentialGroupID) && endpoint.ApiKey != "" {
			key = endpoint.ApiKey
		}
	}
	if key == "" {
		return result, fmt.Errorf("请先配置所选凭证分组的 API Key")
	}
	if input.VerifyResponse && (strings.TrimSpace(input.Model) == "" || (input.PathStrategy != pathpolicy.Base && input.PathStrategy != pathpolicy.V1)) {
		return result, fmt.Errorf("请先选择明确的路径策略和验证模型")
	}
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.Timeout = 20 * time.Second
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	strategies := []string{pathpolicy.Base, pathpolicy.V1}
	if input.VerifyResponse {
		strategies = []string{input.PathStrategy}
	}
	seen := map[string]bool{}
	for _, strategy := range strategies {
		path, method := "/models", http.MethodGet
		var body io.Reader
		if input.VerifyResponse {
			path, method = "/responses", http.MethodPost
			raw, _ := json.Marshal(map[string]any{"model": strings.TrimSpace(input.Model), "stream": true, "input": "Reply with OK.", "max_output_tokens": 128})
			body = bytes.NewReader(raw)
		}
		target := pathpolicy.Resolve(input.BaseURL, &url.URL{Path: path}, strategy, false)
		if seen[target] {
			continue
		}
		seen[target] = true
		candidate := EndpointProbeCandidate{PathStrategy: strategy, URL: target, Models: []string{}}
		probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		req, err := http.NewRequestWithContext(probeCtx, method, target, body)
		if err != nil {
			cancel()
			return result, fmt.Errorf("检测地址无效")
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		if input.VerifyResponse {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
		}
		resp, err := copyClient.Do(req)
		if err != nil {
			candidate.Result = "network_error"
		} else {
			candidate.HTTPStatus = resp.StatusCode
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
			resp.Body.Close()
			switch {
			case len(data) > 256<<10:
				candidate.Result = "response_too_large"
			case resp.StatusCode == 401 || resp.StatusCode == 403:
				candidate.Result = "authentication_failed"
			case resp.StatusCode == 429:
				candidate.Result = "rate_limited"
			case resp.StatusCode >= 300:
				candidate.Result = "http_error"
			case input.VerifyResponse:
				candidate.Verified, candidate.Result = codexproxy.ValidateDiagnosticStream(data)
				if candidate.Verified {
					candidate.Result = "response_verified"
					result.ResponseVerified = true
				} else if readErr != nil {
					candidate.Result = "response_read_error"
				}
			case readErr != nil:
				candidate.Result = "response_read_error"
			default:
				var envelope struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
					Models []struct {
						ID string `json:"id"`
					} `json:"models"`
				}
				if json.Unmarshal(data, &envelope) == nil && (envelope.Data != nil || envelope.Models != nil) {
					candidate.Result, candidate.Verified = "models_verified", true
					for _, model := range append(envelope.Data, envelope.Models...) {
						if model.ID != "" && len(candidate.Models) < 100 {
							candidate.Models = append(candidate.Models, model.ID)
						}
					}
				} else {
					candidate.Result = "invalid_response"
				}
			}
		}
		cancel()
		result.Candidates = append(result.Candidates, candidate)
	}
	verified := 0
	for _, candidate := range result.Candidates {
		if candidate.Verified {
			verified++
			result.RecommendedStrategy = candidate.PathStrategy
		}
	}
	if verified != 1 {
		result.RecommendedStrategy = ""
	}
	return result, nil
}
