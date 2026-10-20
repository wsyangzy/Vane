package codexproxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
)

// handleStreaming 处理流式响应：逐行透传 SSE 并提取 token。
// 如果上游在真正输出前通过 SSE 返回错误事件，则返回错误交给外层端点循环做故障转移。
// served 表示端点是否把这次请求完整服务掉了：已输出后的 SSE 错误 / 读取中断都算 false。
func (h *Handler) handleStreaming(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time, requestModel, endpointName, groupName, channelName string, hopsTried int) (streamErr *UpstreamStreamError, served bool) {
	defer resp.Body.Close()

	var headersWritten bool
	flusher, canFlush := w.(http.Flusher)
	writeHeaders := func() {
		if headersWritten {
			return
		}
		for key, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(resp.StatusCode)
		headersWritten = true
	}

	// 🆕 [详细日志] 流式处理开始（emoji 风格，对齐 Claude 日志详细度）
	h.logger.Info(fmt.Sprintf("🌊 [Codex流式] [%s] 开始流式处理，端点: %s", requestID, endpointName),
		"provider", "codex", "request_id", requestID)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)

	var lastResult *TokenResult
	var bytesProcessed int64
	var modelLogged, usageLogged bool
	var outputSentRecorded bool
	var downstreamStreamErr *UpstreamStreamError
	var streamCompleted bool
	var pendingLines [][]byte
	var pendingBytes int
	currentEvent := ""

	writeLine := func(line []byte) {
		writeHeaders()
		if !outputSentRecorded && len(bytes.TrimSpace(line)) > 0 {
			h.recordOutputSent(requestID)
			outputSentRecorded = true
		}
		w.Write(line)
		w.Write([]byte("\n"))
		if canFlush {
			flusher.Flush()
		}
	}
	flushPending := func() {
		if len(pendingLines) == 0 {
			return
		}
		for _, pending := range pendingLines {
			writeLine(pending)
		}
		pendingLines = nil
		pendingBytes = 0
	}
	bufferLine := func(line []byte) {
		copied := append([]byte(nil), line...)
		pendingLines = append(pendingLines, copied)
		pendingBytes += len(copied) + 1
		if pendingBytes >= maxPreOutputStreamBufferBytes {
			flushPending()
		}
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesProcessed += int64(len(line)) + 1 // +1 for trailing newline

		if eventName, ok := parseSSEEventName(line); ok {
			currentEvent = eventName
		}

		if streamErr := h.tokenParser.ParseStreamingErrorLine(line); streamErr != nil {
			if resp.Request != nil {
				streamErr.Message = subscription.Redact(resp.Request.Context(), streamErr.Message)
			}
			if !headersWritten {
				// The caller may still fail over. Restore the consumed SSE event so
				// the final exhausted response keeps the upstream 200 and raw body.
				raw := make([]byte, 0, pendingBytes+len(line)+1)
				for _, pending := range pendingLines {
					raw = append(raw, pending...)
					raw = append(raw, '\n')
				}
				raw = append(raw, line...)
				raw = append(raw, '\n', '\n')
				resp.Body = io.NopCloser(bytes.NewReader(raw))
				h.logger.Warn("[Codex 流式上游错误] 输出前捕获到可转移错误",
					"provider", "codex",
					"request_id", requestID,
					"endpoint", endpointName,
					"error", streamErr.Error(),
				)
				return streamErr, false
			}
			downstreamStreamErr = streamErr
		}

		// 解析 token（流式最终 chunk 可能包含 usage）
		if result, isDone := h.tokenParser.ParseStreamingLine(line); isDone {
			streamCompleted = true
		} else if result != nil {
			// 🆕 [详细日志] 首次从 response 事件提取到模型时记录
			if !modelLogged && result.Model != "" {
				h.logger.Info(fmt.Sprintf("🎯 [Codex模型提取] [%s] 从response事件提取模型: %s",
					requestID, result.Model),
					"provider", "codex", "request_id", requestID)
				modelLogged = true
			}
			// 🆕 [详细日志] 首次提取到 token 用量时记录（input/output/cached 任一非零即视为获取到）
			if !usageLogged && (result.InputTokens > 0 || result.OutputTokens > 0 || result.CacheReadTokens > 0) {
				h.logger.Info(fmt.Sprintf("🎯 [Codex Usage] [%s] 提取token信息: input=%d, output=%d, cached=%d",
					requestID, result.InputTokens, result.OutputTokens, result.CacheReadTokens),
					"provider", "codex", "request_id", requestID)
				usageLogged = true
			}
			lastResult = result
		}
		if isCodexStreamCompletionLine(currentEvent, line) {
			streamCompleted = true
		}

		if !headersWritten && !isLikelyCodexOutputLine(currentEvent, line) {
			bufferLine(line)
			continue
		}
		flushPending()
		writeLine(line)
	}

	// 🆕 [详细日志] 扫描器错误检测：非 nil 视为流式中断；nil 视为正常完成
	if scanErr := scanner.Err(); scanErr != nil {
		h.logger.Error(fmt.Sprintf("❌ [Codex流式失败] [%s] 端点: %s, 已处理 %d 字节, 错误: %v",
			requestID, endpointName, bytesProcessed, scanErr),
			"provider", "codex", "request_id", requestID)
		if !headersWritten {
			return &UpstreamStreamError{
				Type:    "stream_read_error",
				Message: scanErr.Error(),
			}, false
		}
		if isContextCanceledError(scanErr) {
			if streamCompleted {
				h.logger.Warn("[Codex 流式收尾取消] 已收到完成事件，按成功记录",
					"provider", "codex",
					"request_id", requestID,
					"endpoint", endpointName,
					"bytes_processed", bytesProcessed,
					"error", scanErr,
				)
				h.recordSuccess(requestID, startTime, resp.StatusCode, lastResult, requestModel, endpointName, groupName, channelName)
				return nil, true
			}
			if outputSentRecorded {
				h.logger.Warn("[Codex 流式下游取消] 已输出内容后连接取消，按客户端取消记录",
					"provider", "codex",
					"request_id", requestID,
					"endpoint", endpointName,
					"bytes_processed", bytesProcessed,
					"error", scanErr,
				)
				h.recordCancelled(requestID, startTime, "client_canceled_after_output", lastResult, requestModel, endpointName, groupName, channelName)
				return nil, true
			}
		}
		// 上游 HTTP 层这次是真给过响应的（200），流是在那之后断的，就记它的真实状态码。
		// 原先这里固定写 502，是一个提供商从未返回过的码。
		h.recordUpstreamFailure(requestID, startTime, "network_error", scanErr.Error(), resp.StatusCode, nil, requestModel)
		return nil, false
	} else {
		h.logger.Info(fmt.Sprintf("✅ [Codex流式完成] [%s] 端点: %s, 已处理 %d 字节",
			requestID, endpointName, bytesProcessed),
			"provider", "codex", "request_id", requestID)
	}

	if downstreamStreamErr != nil {
		h.logger.Error("[Codex 流式上游错误] 已输出后收到错误事件，记录为失败",
			"provider", "codex",
			"request_id", requestID,
			"endpoint", endpointName,
			"error", downstreamStreamErr.Error(),
		)
		// 记上游真实给出的状态码（这条响应 HTTP 层就是 200，错误只在 SSE 事件里）。
		// 200 + failed 是准确的：它说明失败发生在流中途，而不是连接或鉴权阶段。
		h.recordUpstreamFailure(requestID, startTime, "server_error", downstreamStreamErr.Error(), resp.StatusCode, nil, requestModel)
		return nil, false
	}

	flushPending()

	// 🆕 [Codex 生命周期] 增强的完成日志（含端点、渠道、token、状态码、耗时、跳数）
	durationMs := time.Since(startTime).Milliseconds()
	modelName := requestModel
	var inputTokens, outputTokens, cacheReadTokens, totalTokens int64
	var reasoningTokens int64
	if lastResult != nil {
		modelName = pickModelName(lastResult.Model, requestModel)
		inputTokens = lastResult.InputTokens
		outputTokens = lastResult.OutputTokens
		reasoningTokens = lastResult.ReasoningTokens
		cacheReadTokens = lastResult.CacheReadTokens
		totalTokens = lastResult.TotalTokens
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
		"streaming", true,
	)

	if lastResult == nil {
		h.logger.Debug("Codex 流式响应未捕获 usage 信息",
			"request_id", requestID,
		)
	}

	// 🆕 [详细日志] Token 最终统计 + 请求完成 + Token 汇总（emoji 风格，对齐 Claude 日志详细度）
	if lastResult != nil {
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

	h.recordSuccess(requestID, startTime, resp.StatusCode, lastResult, requestModel, endpointName, groupName, channelName)
	return nil, true
}

func parseSSEEventName(line []byte) (string, bool) {
	trimmed := bytes.TrimSpace(line)
	const eventPrefix = "event:"
	if !bytes.HasPrefix(trimmed, []byte(eventPrefix)) {
		return "", false
	}
	return strings.TrimSpace(string(trimmed[len(eventPrefix):])), true
}

func isLikelyCodexOutputLine(eventName string, line []byte) bool {
	eventName = strings.ToLower(strings.TrimSpace(eventName))
	if strings.Contains(eventName, "delta") ||
		strings.Contains(eventName, "content") ||
		strings.Contains(eventName, "output_text") {
		return true
	}
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, sseDataPrefix) {
		return false
	}
	payload := bytes.TrimSpace(trimmed[len(sseDataPrefix):])
	if bytes.Equal(payload, sseDoneMarker) {
		return true
	}
	payloadLower := bytes.ToLower(payload)
	return bytes.Contains(payloadLower, []byte(`"choices"`)) ||
		bytes.Contains(payloadLower, []byte(`"delta"`)) ||
		bytes.Contains(payloadLower, []byte(`"content"`)) ||
		bytes.Contains(payloadLower, []byte(`"output_text"`))
}

func isCodexStreamCompletionLine(eventName string, line []byte) bool {
	eventName = strings.ToLower(strings.TrimSpace(eventName))
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, sseDataPrefix) {
		return false
	}
	if eventName == "response.completed" {
		return true
	}
	payload := bytes.ToLower(bytes.TrimSpace(trimmed[len(sseDataPrefix):]))
	return bytes.Contains(payload, []byte(`"response.completed"`))
}

func isContextCanceledError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "context canceled")
}

// ============================================
// 追踪辅助方法
// ============================================
