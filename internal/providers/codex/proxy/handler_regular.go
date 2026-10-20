package codexproxy

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// handleNonStreaming 处理非流式响应：读取完整 body，解析 token，写回客户端。
// 返回 false 表示上游虽给了 2xx 但正文没读完整，端点不应被记为成功。
func (h *Handler) handleNonStreaming(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time, requestModel, endpointName, groupName, channelName string, hopsTried int) bool {
	defer resp.Body.Close()

	// 🆕 [详细日志] 非流式处理开始（emoji 风格，对齐 Claude 日志详细度）
	h.logger.Info(fmt.Sprintf("📥 [Codex非流式] [%s] 开始非流式处理，端点: %s", requestID, endpointName),
		"provider", "codex", "request_id", requestID)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("读取上游响应失败", "request_id", requestID, "error", err)
		// 🆕 [Codex 生命周期] 请求最终失败日志
		h.logger.Error("[Codex 请求失败]",
			"provider", "codex",
			"request_id", requestID,
			"failure_reason", "network_error",
			"detail", "读取上游响应失败",
			"duration_ms", time.Since(startTime).Milliseconds(),
			"endpoints_tried", hopsTried,
			"channel", channelName,
		)
		detail := err.Error()
		// 上游 HTTP 层已经响应过（2xx），只是正文没读完：追踪记它的真实状态码，客户端才收本地 502。
		h.recordUpstreamFailure(requestID, startTime, "network_error", detail, resp.StatusCode, nil, requestModel)
		h.writeError(w, http.StatusBadGateway, detail)
		return false
	}

	// 解析 token 用量并记录成功
	tokenResult := h.tokenParser.ParseNonStreaming(respBody)

	// 🆕 [详细日志] 模型 / Usage 提取（emoji 风格，对齐 Claude 日志详细度）
	if tokenResult != nil {
		if tokenResult.Model != "" {
			h.logger.Info(fmt.Sprintf("🎯 [Codex模型提取] [%s] 从response提取模型: %s",
				requestID, tokenResult.Model),
				"provider", "codex", "request_id", requestID)
		}
		if tokenResult.InputTokens > 0 || tokenResult.OutputTokens > 0 || tokenResult.CacheReadTokens > 0 {
			h.logger.Info(fmt.Sprintf("🎯 [Codex Usage] [%s] 提取token信息: input=%d, output=%d, cached=%d",
				requestID, tokenResult.InputTokens, tokenResult.OutputTokens, tokenResult.CacheReadTokens),
				"provider", "codex", "request_id", requestID)
		}
	}

	// 🆕 [Codex 生命周期] 增强的完成日志（含端点、渠道、token、状态码、耗时、跳数）
	durationMs := time.Since(startTime).Milliseconds()
	modelName := requestModel
	var inputTokens, outputTokens, cacheReadTokens, totalTokens int64
	var reasoningTokens int64
	if tokenResult != nil {
		modelName = pickModelName(tokenResult.Model, requestModel)
		inputTokens = tokenResult.InputTokens
		outputTokens = tokenResult.OutputTokens
		reasoningTokens = tokenResult.ReasoningTokens
		cacheReadTokens = tokenResult.CacheReadTokens
		totalTokens = tokenResult.TotalTokens
	}
	h.logger.Info("[Codex 请求完成]",
		"provider", "codex",
		"request_id", requestID,
		"endpoint", endpointName,
		"channel", channelName,
		"model", modelName,
		"duration_ms", durationMs,
		"hops_tried", hopsTried,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"reasoning_tokens", reasoningTokens,
		"cache_read_tokens", cacheReadTokens,
		"total_tokens", totalTokens,
		"http_status", resp.StatusCode,
	)

	if tokenResult == nil {
		h.logger.Debug("Codex 非流式响应未携带 usage 字段",
			"request_id", requestID,
			"body_size", len(respBody),
		)
	}

	// 🆕 [详细日志] Token 最终统计 + 请求完成 + Token 汇总（emoji 风格，对齐 Claude 日志详细度）
	if tokenResult != nil {
		h.logger.Info(fmt.Sprintf("🪙 [Codex Token最终] [%s] 模型: %s, 输入: %d, 输出: %d, 缓存读取: %d",
			requestID, modelName, inputTokens, outputTokens, cacheReadTokens),
			"provider", "codex", "request_id", requestID)
	}
	h.logger.Info(fmt.Sprintf("✅ [Codex请求完成] [%s] 端点: %s (总尝试 %d 个端点)",
		requestID, endpointName, hopsTried),
		"provider", "codex", "request_id", requestID)
	h.logger.Info(fmt.Sprintf("📊 [Codex Token统计] [%s] 模型: %s, 输入[%d] 输出[%d] 总计[%d] 缓存[%d], 耗时: %dms",
		requestID, modelName, inputTokens, outputTokens, inputTokens+outputTokens, cacheReadTokens, durationMs),
		"provider", "codex", "request_id", requestID)

	h.recordOutputSent(requestID)
	h.recordSuccess(requestID, startTime, resp.StatusCode, tokenResult, requestModel, endpointName, groupName, channelName)

	// 复制响应头
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
	return true
}
