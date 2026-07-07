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
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/tracking"
)

// CountTokensHandler 处理 /v1/messages/count_tokens 请求
// 策略：优先转发到标记了 supports_count_tokens 的端点，失败则本地估算
type CountTokensHandler struct {
	config          *config.Config
	endpointManager *endpoint.Manager
	forwarder       *Forwarder
	privacyRedactor PrivacyRedactor
}

// NewCountTokensHandler 创建 CountTokensHandler
func NewCountTokensHandler(cfg *config.Config, em *endpoint.Manager, f *Forwarder, redactor PrivacyRedactor) *CountTokensHandler {
	return &CountTokensHandler{
		config:          cfg,
		endpointManager: em,
		forwarder:       f,
		privacyRedactor: redactor,
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

	// 1. 找配置了 supports_count_tokens: true 的端点
	supportedEndpoints := h.getSupportedEndpoints()

	// 2. 如果有，尝试转发
	if len(supportedEndpoints) > 0 {
		result, ok, redactionErr := h.tryForward(ctx, r, bodyBytes, supportedEndpoints, connID, lifecycleManager)
		if redactionErr != nil {
			if lifecycleManager != nil {
				lifecycleManager.FailRequest("privacy_redaction_failed", redactionErr.Error(), http.StatusInternalServerError)
			}
			http.Error(w, fmt.Sprintf("隐私脱敏失败: %v", redactionErr), http.StatusInternalServerError)
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

// tryForward 尝试转发到支持的端点
func (h *CountTokensHandler) tryForward(ctx context.Context, r *http.Request, bodyBytes []byte, endpoints []*endpoint.Endpoint, connID string, lifecycleManager RequestLifecycleManager) ([]byte, bool, error) {
	for _, ep := range endpoints {
		requestBody := h.forwarder.RewriteModelForEndpoint(bodyBytes, ep, connID).Body
		var err error
		requestBody, err = redactBodyBeforeForward(h.privacyRedactor, requestBody, lifecycleManager)
		if err != nil {
			return nil, false, err
		}
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
			lifecycleManager.FailRequest("count_tokens_estimation_failed", err.Error(), http.StatusBadRequest)
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
	var response CountTokensResponse
	if err := json.Unmarshal(body, &response); err != nil {
		lifecycleManager.CompleteRequest(nil)
		return
	}
	lifecycleManager.CompleteRequest(&tracking.TokenUsage{InputTokens: int64(response.InputTokens)})
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
