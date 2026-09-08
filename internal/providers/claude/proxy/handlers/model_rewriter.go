package handlers

import (
	"encoding/json"
	"log/slog"
	"strings"

	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/store"
)

// ModelRewriteResult 描述一次请求体 model 改写结果。
type ModelRewriteResult struct {
	Body          []byte
	OriginalModel string
	NewModel      string
	Source        string
}

// RewriteModelForEndpoint 按端点所属分组策略改写请求体 model 字段。
// 映射只做精确匹配；任何解析或序列化异常都原样转发。
func (f *Forwarder) RewriteModelForEndpoint(body []byte, ep *endpoint.Endpoint, requestID string) ModelRewriteResult {
	result := ModelRewriteResult{Body: body}
	if f == nil || f.endpointManager == nil || ep == nil {
		return result
	}

	if ep.Config.ModelPolicy == store.ClaudeModelPolicyPassthrough {
		return result
	}

	channel := strings.TrimSpace(ep.Config.Channel)
	if channel == "" {
		channel = strings.TrimSpace(ep.Config.Name)
	}
	modelMapping := ep.Config.ModelMapping
	if ep.Config.ModelPolicy == "" || ep.Config.ModelPolicy == store.ClaudeModelPolicyInherit {
		modelMapping = f.endpointManager.GetChannelModelMapping(channel)
	}
	if len(modelMapping) == 0 {
		return result
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return result
	}

	modelRaw, hasModel := raw["model"]
	if !hasModel {
		return result
	}

	var originalModel string
	if err := json.Unmarshal(modelRaw, &originalModel); err != nil {
		return result
	}
	originalModel = strings.TrimSpace(originalModel)
	if originalModel == "" {
		return result
	}

	newModel := strings.TrimSpace(modelMapping[originalModel])
	if newModel == "" || newModel == originalModel {
		return result
	}

	newModelBytes, err := json.Marshal(newModel)
	if err != nil {
		slog.Warn("[Claude model 改写] 新 model 序列化失败，原样转发",
			"request_id", requestID,
			"channel", channel,
			"new_model", newModel,
			"error", err,
		)
		return result
	}
	raw["model"] = newModelBytes

	newBody, err := json.Marshal(raw)
	if err != nil {
		slog.Warn("[Claude model 改写] body 重新序列化失败，原样转发",
			"request_id", requestID,
			"channel", channel,
			"error", err,
		)
		return result
	}

	slog.Info("[Claude model 改写]",
		"request_id", requestID,
		"channel", channel,
		"original_model", originalModel,
		"new_model", newModel,
		"rewrite_source", "mapping",
	)
	return ModelRewriteResult{
		Body:          newBody,
		OriginalModel: originalModel,
		NewModel:      newModel,
		Source:        "mapping",
	}
}

// EffectiveClaudeModel resolves the model used by an endpoint without mutating the request.
func EffectiveClaudeModel(ep *endpoint.Endpoint, channelMapping map[string]string, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if ep == nil || ep.Config.ModelPolicy == store.ClaudeModelPolicyPassthrough {
		return requestedModel
	}
	mapping := ep.Config.ModelMapping
	if ep.Config.ModelPolicy == "" || ep.Config.ModelPolicy == store.ClaudeModelPolicyInherit {
		mapping = channelMapping
	}
	if mapped := strings.TrimSpace(mapping[requestedModel]); mapped != "" {
		return mapped
	}
	return requestedModel
}

// EndpointSupportsClaudeModel filters only groups that explicitly declare capabilities.
func EndpointSupportsClaudeModel(ep *endpoint.Endpoint, channelMapping map[string]string, requestedModel string) bool {
	if ep == nil || len(ep.Config.SupportedModels) == 0 || strings.TrimSpace(requestedModel) == "" {
		return true
	}
	effective := EffectiveClaudeModel(ep, channelMapping, requestedModel)
	for _, model := range ep.Config.SupportedModels {
		model = strings.TrimSpace(model)
		if model == requestedModel || model == effective {
			return true
		}
	}
	return false
}

func filterClaudeModelEndpoints(forwarder *Forwarder, endpoints []*endpoint.Endpoint, body []byte) []*endpoint.Endpoint {
	if forwarder == nil || forwarder.endpointManager == nil || len(endpoints) == 0 {
		return endpoints
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return endpoints
	}
	var requestedModel string
	if err := json.Unmarshal(raw["model"], &requestedModel); err != nil {
		return endpoints
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return endpoints
	}
	filtered := endpoints[:0:0]
	for _, ep := range endpoints {
		channel := strings.TrimSpace(ep.Config.Channel)
		if channel == "" {
			channel = strings.TrimSpace(ep.Config.Name)
		}
		if EndpointSupportsClaudeModel(ep, forwarder.endpointManager.GetChannelModelMapping(channel), requestedModel) {
			filtered = append(filtered, ep)
		}
	}
	return filtered
}

func rewriteBodyForEndpoint(forwarder *Forwarder, lifecycleManager RequestLifecycleManager, body []byte, ep *endpoint.Endpoint) []byte {
	requestID := ""
	if lifecycleManager != nil {
		requestID = lifecycleManager.GetRequestID()
	}

	rewrite := forwarder.RewriteModelForEndpoint(body, ep, requestID)
	if rewrite.Source != "" && lifecycleManager != nil {
		lifecycleManager.SetModelRewrite(rewrite.OriginalModel, rewrite.NewModel, rewrite.Source)
	}
	return rewrite.Body
}
