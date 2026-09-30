package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"unicode/utf8"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/tracking"
)

// CountTokensHandler 处理 /v1/messages/count_tokens 请求
// 策略：优先转发到标记了 supports_count_tokens 的端点，失败则本地估算
type CountTokensHandler struct {
	config          *config.Config
	endpointManager *endpoint.Manager
	forwarder       *Forwarder
	privacyFilter   PrivacyFilter
}

// NewCountTokensHandler 创建 CountTokensHandler
func NewCountTokensHandler(cfg *config.Config, em *endpoint.Manager, f *Forwarder, filter PrivacyFilter) *CountTokensHandler {
	return &CountTokensHandler{
		config:          cfg,
		endpointManager: em,
		forwarder:       f,
		privacyFilter:   filter,
	}
}

// CountTokensRequest 定义 count_tokens 请求结构
type CountTokensRequest struct {
	Model    string                   `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
	System   interface{}              `json:"system,omitempty"`
	Tools    []interface{}            `json:"tools,omitempty"`
}

// CountTokensResponse 定义响应结构
type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// Handle 处理count_tokens请求 - 极简逻辑
func (h *CountTokensHandler) Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, bodyBytes []byte, connID string, lifecycleManager RequestLifecycleManager) {
	slog.Info(fmt.Sprintf("🔢 [Token计数] [%s] 收到count_tokens请求", connID))

	// 关闭 TokenCounting 时仍由 Relay 本地处理，不能把客户端发来的路径落穿到上游。
	var supportedEndpoints []*endpoint.Endpoint
	if h.config != nil && h.config.TokenCounting.Enabled {
		candidates := h.getSupportedEndpoints()
		supportedEndpoints = filterClaudeModelEndpoints(h.forwarder, candidates, bodyBytes)
		if len(supportedEndpoints) == 0 && len(candidates) > 0 {
			// 激活渠道的 count_tokens 端点都不支持该模型：本次请求借用其它渠道，不切换全局激活渠道
			supportedEndpoints = h.countTokensEndpointsFromOtherChannels(bodyBytes)
		}
	}

	// 2. 如果有，尝试转发
	if len(supportedEndpoints) > 0 {
		result, ok, redactionErr := h.tryForward(ctx, r, bodyBytes, supportedEndpoints, connID, lifecycleManager)
		if redactionErr != nil {
			// 隐私策略拒绝：短路返回，既不重试也不降级到本地估算
			if policyErr := AsPrivacyPolicyError(redactionErr); policyErr != nil {
				if lifecycleManager != nil {
					lifecycleManager.FailRequest(PrivacyFailureReason(policyErr), policyErr.Message)
				}
				WritePrivacyPolicyErrorResponse(w, policyErr)
				return
			}
			if lifecycleManager != nil {
				lifecycleManager.FailRequest("privacy_filter_failed", redactionErr.Error())
			}
			http.Error(w, fmt.Sprintf("隐私防护处理失败: %v", redactionErr), http.StatusInternalServerError)
			return
		}
		if ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(result)
			h.completeCountTokensRequest(lifecycleManager, result)
			slog.Info(fmt.Sprintf("✅ [Token计数-转发] [%s] 转发成功", connID))
			return
		}
		// 转发失败，降级到估算
		slog.Warn(fmt.Sprintf("⚠️ [Token计数] [%s] 转发失败，降级到本地估算", connID))
	} else {
		slog.Info(fmt.Sprintf("🔍 [Token计数] [%s] 无支持端点，使用本地估算", connID))
	}

	// 3. 本地估算
	h.respondWithEstimation(w, bodyBytes, connID, lifecycleManager)
}

// getSupportedEndpoints 获取支持count_tokens的端点
func (h *CountTokensHandler) getSupportedEndpoints() []*endpoint.Endpoint {
	allEndpoints := h.endpointManager.GetHealthyEndpoints()
	var supported []*endpoint.Endpoint

	for _, ep := range allEndpoints {
		if ep.Config.SupportsCountTokens {
			supported = append(supported, ep)
		}
	}

	return supported
}

// countTokensEndpointsFromOtherChannels 按渠道优先级找第一个有 count_tokens 端点支持请求模型的渠道。
// 只在本次请求内使用；关闭渠道间故障转移时不借用其它渠道，退回本地估算。
func (h *CountTokensHandler) countTokensEndpointsFromOtherChannels(body []byte) []*endpoint.Endpoint {
	if h.endpointManager == nil || !h.endpointManager.IsFailoverEnabled() {
		return nil
	}
	active := h.endpointManager.ActiveChannelName()
	for _, channel := range h.endpointManager.RoutableChannelsByPriority() {
		if channel == active {
			continue
		}
		var supported []*endpoint.Endpoint
		for _, ep := range h.endpointManager.GetRoutableEndpointsForChannel(channel, 0, nil) {
			if ep.Config.SupportsCountTokens {
				supported = append(supported, ep)
			}
		}
		if filtered := filterClaudeModelEndpoints(h.forwarder, supported, body); len(filtered) > 0 {
			return filtered
		}
	}
	return nil
}

// tryForward 尝试转发到支持的端点
func (h *CountTokensHandler) tryForward(ctx context.Context, r *http.Request, bodyBytes []byte, endpoints []*endpoint.Endpoint, connID string, lifecycleManager RequestLifecycleManager) ([]byte, bool, error) {
	bodies, err := applyPrivacyFilter(ctx, h.privacyFilter, r, "claude", bodyBytes, lifecycleManager, privacyLevelsFor(h.endpointManager))
	if err != nil {
		return nil, false, err
	}
	for _, ep := range endpoints {
		// 转发前登记端点：否则 count_tokens 请求的渠道与链路在追踪里始终是空的，
		// 出问题时看不出这条请求到底打到了哪个端点
		if lifecycleManager != nil {
			lifecycleManager.SetEndpoint(ep.Config.Name, endpointDisplayGroupName(ep), ep.Config.Channel)
		}
		requestBody := h.forwarder.RewriteModelForEndpoint(bodies.forEndpoint(ep), ep, connID).Body
		targetURL := ep.Config.URL + "/v1/messages/count_tokens"
		req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(requestBody))
		if err != nil {
			continue
		}

		h.forwarder.CopyHeaders(r, req, ep)

		client, err := h.forwarder.NewClient(ep.Config.Timeout)
		if err != nil {
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Debug(fmt.Sprintf("❌ [转发失败] [%s] 端点: %s, 错误: %v", connID, ep.Config.Name, err))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			if bodyBytes, err := io.ReadAll(resp.Body); err == nil {
				slog.Info(fmt.Sprintf("✅ [转发成功] [%s] 端点: %s", connID, ep.Config.Name))
				return bodyBytes, true, nil
			}
		}
	}

	return nil, false, nil
}

// respondWithEstimation 返回本地估算结果
func (h *CountTokensHandler) respondWithEstimation(w http.ResponseWriter, bodyBytes []byte, connID string, lifecycleManager RequestLifecycleManager) {
	tokens, err := h.estimateTokens(bodyBytes)
	if err != nil {
		if lifecycleManager != nil {
			lifecycleManager.FailRequest("count_tokens_estimation_failed", err.Error())
		}
		http.Error(w, fmt.Sprintf("Failed to estimate tokens: %v", err), http.StatusBadRequest)
		return
	}

	response := CountTokensResponse{InputTokens: tokens}
	responseBytes, _ := json.Marshal(response)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Token-Estimation", "true") // 标记这是估算值
	w.WriteHeader(http.StatusOK)
	w.Write(responseBytes)
	if lifecycleManager != nil {
		lifecycleManager.CompleteRequest(&tracking.TokenUsage{InputTokens: int64(tokens)})
	}

	slog.Info(fmt.Sprintf("📊 [Token估算] [%s] 估算结果: %d tokens", connID, tokens))
}

func (h *CountTokensHandler) completeCountTokensRequest(lifecycleManager RequestLifecycleManager, body []byte) {
	if lifecycleManager == nil {
		return
	}
	// tryForward 只在上游返回 200 时才交出正文，这里记录的就是那次真实的上游状态。
	var response CountTokensResponse
	if err := json.Unmarshal(body, &response); err != nil {
		lifecycleManager.CompleteRequestWithUpstreamStatus(nil, http.StatusOK)
		return
	}
	lifecycleManager.CompleteRequestWithUpstreamStatus(&tracking.TokenUsage{InputTokens: int64(response.InputTokens)}, http.StatusOK)
}

// estimateTokens 本地估算token数量
func (h *CountTokensHandler) estimateTokens(bodyBytes []byte) (int, error) {
	var req CountTokensRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return 0, fmt.Errorf("invalid request body: %w", err)
	}

	totalChars := 0

	// 统计消息内容
	for _, msg := range req.Messages {
		if content, ok := msg["content"].(string); ok {
			totalChars += utf8.RuneCountInString(content)
		}
	}

	// 统计系统提示
	if req.System != nil {
		switch sys := req.System.(type) {
		case string:
			totalChars += utf8.RuneCountInString(sys)
		case []interface{}:
			for _, item := range sys {
				if str, ok := item.(string); ok {
					totalChars += utf8.RuneCountInString(str)
				}
			}
		}
	}

	// 工具定义开销 (每个工具约100 tokens)
	if len(req.Tools) > 0 {
		totalChars += len(req.Tools) * 400
	}

	// 应用估算比例
	estimatedTokens := int(float64(totalChars) / h.config.TokenCounting.EstimationRatio)

	// 基础开销
	estimatedTokens += 50

	return estimatedTokens, nil
}
