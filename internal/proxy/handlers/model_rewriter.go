package handlers

import (
	"encoding/json"
	"log/slog"
	"strings"

	"ai-relay-forwarder/internal/endpoint"
)

// ModelRewriteResult 描述一次请求体 model 改写结果。
type ModelRewriteResult struct {
	Body          []byte
	OriginalModel string
	NewModel      string
	Source        string
}

// RewriteModelForEndpoint 按端点所属渠道的模型映射改写请求体 model 字段。
// 映射只做精确匹配；任何解析或序列化异常都原样转发。
func (f *Forwarder) RewriteModelForEndpoint(body []byte, ep *endpoint.Endpoint, requestID string) ModelRewriteResult {
	result := ModelRewriteResult{Body: body}
	if f == nil || f.endpointManager == nil || ep == nil {
		return result
	}

	channel := strings.TrimSpace(ep.Config.Channel)
	if channel == "" {
		channel = strings.TrimSpace(ep.Config.Name)
	}
	modelMapping := f.endpointManager.GetChannelModelMapping(channel)
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
