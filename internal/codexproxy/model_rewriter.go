// model_rewriter.go - 请求体 model 字段改写器
// 反代根据当前激活渠道的 model 配置，决定是否改写请求体中的 model 字段。
// 设计原则：改写是增强而非强制，任何异常都降级为原样转发，不阻断请求。

package codexproxy

import (
	"encoding/json"
	"log/slog"
)

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
	if channelModel == "" {
		return body
	}

	// 用 RawMessage 解析以保留整数/浮点精度和字段原始 JSON 语法
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		// body 不是 JSON 对象（如二进制、表单上传）→ 透传
		return body
	}

	modelRaw, hasModel := raw["model"]
	if !hasModel {
		// 没有 model 字段（如 GET /v1/models 路径错走 POST）→ 透传
		return body
	}

	// 比对原 model 与 channelModel；相同则跳过改写
	var originalModel string
	if err := json.Unmarshal(modelRaw, &originalModel); err == nil && originalModel == channelModel {
		return body
	}

	// 构造新的 model JSON 值并替换
	newModelBytes, err := json.Marshal(channelModel)
	if err != nil {
		logger.Warn("[Codex model 改写] 新 model 序列化失败，原样转发",
			"request_id", requestID,
			"channel_model", channelModel,
			"error", err,
		)
		return body
	}
	raw["model"] = newModelBytes

	newBody, err := json.Marshal(raw)
	if err != nil {
		logger.Warn("[Codex model 改写] body 重新序列化失败，原样转发",
			"request_id", requestID,
			"error", err,
		)
		return body
	}

	logger.Info("[Codex model 改写]",
		"request_id", requestID,
		"original_model", originalModel,
		"new_model", channelModel,
	)
	return newBody
}
