// model_rewriter.go - 请求体 model 字段改写器
// 反代根据当前激活渠道的 model 配置，决定是否改写请求体中的 model 字段。
// 设计原则：改写是增强而非强制，任何异常都降级为原样转发，不阻断请求。

package codexproxy

import (
	"encoding/json"
	"log/slog"
	"strings"
)

type modelRewriteResult struct {
	Body          []byte
	OriginalModel string
	NewModel      string
	Source        string
}

// rewriteModelIfNeeded 按 channelModel 决定是否改写 body 中的 model 字段。
// 返回新 body（或原 body 不变）。日志由函数内部负责。
//
// 降级策略——以下场景均返回原 body 不阻断：
//   - channelModel 为空（使用全局默认，不改写）
//   - body 不是合法 JSON 对象
//   - JSON 对象中无 model 字段
//   - body 中 model 已等于 channelModel
//   - JSON Marshal/Unmarshal 失败
func rewriteModelIfNeeded(body []byte, channelModel string, logger *slog.Logger, requestID string) []byte {
	return rewriteModelWithMappingIfNeeded(body, nil, channelModel, logger, requestID)
}

// rewriteModelWithMappingIfNeeded 按渠道模型映射优先、channelModel 兜底改写 body 中的 model 字段。
// 映射只做精确匹配，避免误伤正常模型；所有异常均原样转发。
func rewriteModelWithMappingIfNeeded(body []byte, modelMapping map[string]string, channelModel string, logger *slog.Logger, requestID string) []byte {
	return rewriteModelWithMappingDetails(body, modelMapping, channelModel, logger, requestID).Body
}

// rewriteModelWithMappingDetails 返回改写后的 body，并携带可写入请求详情的改写元信息。
func rewriteModelWithMappingDetails(body []byte, modelMapping map[string]string, channelModel string, logger *slog.Logger, requestID string) modelRewriteResult {
	result := modelRewriteResult{Body: body}
	channelModel = strings.TrimSpace(channelModel)
	if channelModel == "" && len(modelMapping) == 0 {
		return result
	}

	// 用 RawMessage 解析以保留整数/浮点精度和字段原始 JSON 语法
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		// body 不是 JSON 对象（如二进制、表单上传）→ 透传
		return result
	}

	modelRaw, hasModel := raw["model"]
	if !hasModel {
		// 没有 model 字段（如 GET /v1/models 路径错走 POST）→ 透传
		return result
	}

	// 比对原 model 与 channelModel；相同则跳过改写
	var originalModel string
	originalModelErr := json.Unmarshal(modelRaw, &originalModel)

	newModel := ""
	rewriteSource := ""
	if originalModelErr == nil && len(modelMapping) > 0 {
		if mappedModel := strings.TrimSpace(modelMapping[originalModel]); mappedModel != "" {
			newModel = mappedModel
			rewriteSource = "mapping"
		}
	}
	if newModel == "" && channelModel != "" {
		newModel = channelModel
		rewriteSource = "channel_model"
	}
	if newModel == "" {
		return result
	}
	if originalModelErr == nil && originalModel == newModel {
		return result
	}

	// 构造新的 model JSON 值并替换
	newModelBytes, err := json.Marshal(newModel)
	if err != nil {
		logger.Warn("[Codex model 改写] 新 model 序列化失败，原样转发",
			"request_id", requestID,
			"new_model", newModel,
			"error", err,
		)
		return result
	}
	raw["model"] = newModelBytes

	newBody, err := json.Marshal(raw)
	if err != nil {
		logger.Warn("[Codex model 改写] body 重新序列化失败，原样转发",
			"request_id", requestID,
			"error", err,
		)
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
