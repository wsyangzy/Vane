// model_rewriter.go - 请求体 model 字段改写器
// 反代根据当前激活渠道的 model 配置，决定是否改写请求体中的 model 字段。
// 设计原则：改写是增强而非强制，任何异常都降级为原样转发，不阻断请求。

package codexproxy

import (
	"log/slog"
	"strings"

	"github.com/wsyangzy/Vane/internal/platform/bodyrewrite"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

type modelRewriteResult struct {
	Body          []byte
	OriginalModel string
	NewModel      string
	Source        string
}

// rewriteModelForEndpoint applies the effective model policy after an endpoint
// (and therefore its credential group) has been selected. The original body is
// always the input so failover never carries a previous group's rewrite.
func rewriteModelForEndpoint(body []byte, endpoint *CodexEndpoint, channel *store.CodexChannelRecord, logger *slog.Logger, requestID string) modelRewriteResult {
	if endpoint == nil {
		return modelRewriteResult{Body: body}
	}
	policy := endpoint.ModelPolicy
	if policy == "" || policy == store.CodexModelPolicyInherit {
		if channel == nil {
			return modelRewriteResult{Body: body}
		}
		return rewriteModelWithMappingDetails(body, channel.ModelMapping, strings.TrimSpace(channel.Model), logger, requestID)
	}
	if policy == store.CodexModelPolicyOverride {
		return rewriteModelWithMappingDetails(body, endpoint.ModelMapping, "", logger, requestID)
	}
	return modelRewriteResult{Body: body}
}

func effectiveCodexModel(endpoint *CodexEndpoint, channel *store.CodexChannelRecord, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if endpoint == nil || endpoint.ModelPolicy == store.CodexModelPolicyPassthrough {
		return requestedModel
	}
	mapping := endpoint.ModelMapping
	fallback := ""
	if endpoint.ModelPolicy == "" || endpoint.ModelPolicy == store.CodexModelPolicyInherit {
		if channel != nil {
			mapping = channel.ModelMapping
			fallback = strings.TrimSpace(channel.Model)
		}
	}
	if mapped := strings.TrimSpace(mapping[requestedModel]); mapped != "" {
		return mapped
	}
	if fallback != "" {
		return fallback
	}
	return requestedModel
}

// endpointSupportsCodexModel 声明项含 * 时大小写不敏感地通配；不含 * 的声明保持精确匹配。
func endpointSupportsCodexModel(endpoint *CodexEndpoint, channel *store.CodexChannelRecord, requestedModel string) bool {
	if endpoint == nil || len(endpoint.SupportedModels) == 0 || strings.TrimSpace(requestedModel) == "" {
		return true
	}
	effective := effectiveCodexModel(endpoint, channel, requestedModel)
	for _, model := range endpoint.SupportedModels {
		if routing.MatchSupportedModel(model, requestedModel) || routing.MatchSupportedModel(model, effective) {
			return true
		}
	}
	return false
}

// endpointDeclaresCodexModel 按分组能力声明核对一个确定的上游模型（不再经过模型映射）；未声明时不过滤。
func endpointDeclaresCodexModel(endpoint *CodexEndpoint, model string) bool {
	if endpoint == nil || len(endpoint.SupportedModels) == 0 {
		return true
	}
	for _, declared := range endpoint.SupportedModels {
		if routing.MatchSupportedModel(declared, model) {
			return true
		}
	}
	return false
}

// rewriteModelForRoute 路由目标声明了上游模型时强制改写 model，
// 优先级高于分组映射，也跳过 channel.Model 兜底改写。
func rewriteModelForRoute(body []byte, upstreamModel string, logger *slog.Logger, requestID string) modelRewriteResult {
	result := modelRewriteResult{Body: body}
	newBody, originalModel, ok := bodyrewrite.ReplaceTopLevelString(body, "model", upstreamModel)
	if !ok || originalModel == upstreamModel {
		return result
	}
	logger.Info("[Codex model 改写]",
		"request_id", requestID,
		"original_model", originalModel,
		"new_model", upstreamModel,
		"rewrite_source", "route",
	)
	return modelRewriteResult{Body: newBody, OriginalModel: originalModel, NewModel: upstreamModel, Source: "route"}
}

// rewriteModelIfNeeded 按 channelModel 决定是否改写 body 中的 model 字段。
// 返回新 body（或原 body 不变）。日志由函数内部负责。
//
// 降级策略——以下场景均返回原 body 不阻断：
//   - channelModel 为空（使用全局默认，不改写）
//   - body 不是合法 JSON 对象
//   - JSON 对象中无 model 字段，或 model 不是字符串
//   - body 中 model 已等于 channelModel
func rewriteModelIfNeeded(body []byte, channelModel string, logger *slog.Logger, requestID string) []byte {
	return rewriteModelWithMappingIfNeeded(body, nil, channelModel, logger, requestID)
}

// rewriteModelWithMappingIfNeeded 按渠道模型映射优先、channelModel 兜底改写 body 中的 model 字段。
// 映射只做精确匹配，避免误伤正常模型；所有异常均原样转发。
func rewriteModelWithMappingIfNeeded(body []byte, modelMapping map[string]string, channelModel string, logger *slog.Logger, requestID string) []byte {
	return rewriteModelWithMappingDetails(body, modelMapping, channelModel, logger, requestID).Body
}

// rewriteModelWithMappingDetails 返回改写后的 body，并携带可写入请求详情的改写元信息。
// 只替换顶层 model 字符串 token 的字节区间，其余字节（键序、空白、转义、数字）保持原样。
func rewriteModelWithMappingDetails(body []byte, modelMapping map[string]string, channelModel string, logger *slog.Logger, requestID string) modelRewriteResult {
	result := modelRewriteResult{Body: body}
	channelModel = strings.TrimSpace(channelModel)
	if channelModel == "" && len(modelMapping) == 0 {
		return result
	}

	// 非 JSON 对象（如二进制、表单上传）、没有 model、model 不是字符串 → 透传
	originalModel, ok := bodyrewrite.LookupTopLevelString(body, "model")
	if !ok {
		return result
	}

	newModel := ""
	rewriteSource := ""
	if len(modelMapping) > 0 {
		if mappedModel := strings.TrimSpace(modelMapping[originalModel]); mappedModel != "" {
			newModel = mappedModel
			rewriteSource = "mapping"
		}
	}
	if newModel == "" && channelModel != "" {
		newModel = channelModel
		rewriteSource = "channel_model"
	}
	if newModel == "" || originalModel == newModel {
		return result
	}

	newBody, _, ok := bodyrewrite.ReplaceTopLevelString(body, "model", newModel)
	if !ok {
		return result
	}

	logger.Info("[Codex model 改写]",
		"request_id", requestID,
		"original_model", originalModel,
		"new_model", newModel,
		"rewrite_source", rewriteSource,
	)
	result.Body = newBody
	result.OriginalModel = originalModel
	result.NewModel = newModel
	result.Source = rewriteSource
	return result
}
