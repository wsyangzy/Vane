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
func (h *Handler) handleStreaming(w http.ResponseWriter, resp *http.Response, requestID string, startTime time.Time, requestModel, endpointName, groupName, channelName string, hopsTried int, beforeArchive ...func(*UpstreamStreamError, bool)) (streamErr *UpstreamStreamError, served bool) {
	defer resp.Body.Close()
	prepareArchive := func(streamErr *UpstreamStreamError, served bool) {
		for _, callback := range beforeArchive {
			callback(streamErr, served)
		}
	}

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
	scanner.Split(splitCodexSSEWireLine)

	var lastResult *TokenResult
	var bytesProcessed int64
	var modelLogged, usageLogged bool
	var outputSentRecorded bool
	var downstreamStreamErr *UpstreamStreamError
	var streamCompleted bool
	var pending []byte
	var decoder codexSSEDecoder
	writeBytes := func(data []byte) {
		writeHeaders()
		if !outputSentRecorded && len(bytes.TrimSpace(data)) > 0 {
			h.recordOutputSent(requestID)
			outputSentRecorded = true
		}
		_, _ = w.Write(data)
		if canFlush {
			flusher.Flush()
		}
	}
	flushPending := func() {
		if len(pending) > 0 {
			writeBytes(pending)
			pending = nil
		}
	}
	restoreFailure := func(failure *UpstreamStreamError) (*UpstreamStreamError, bool) {
		if resp.Request != nil {
			failure.Message = subscription.Redact(resp.Request.Context(), failure.Message)
		}
		// Only the bounded, consumed prefix is retained for provenance. Never
		// send unvalidated bytes just to free the pre-output buffer.
		resp.Body = io.NopCloser(bytes.NewReader(append([]byte(nil), pending...)))
		return failure, false
	}
	processEvent := func(event *codexSSEEvent) *UpstreamStreamError {
		if event == nil {
			return nil
		}
		if result, _ := h.tokenParser.ParseStreamingLine(append([]byte("data: "), event.data...)); result != nil {
			lastResult = result
			if !modelLogged && result.Model != "" {
				h.logger.Info("[Codex 模型提取]", "request_id", requestID, "model", result.Model)
				modelLogged = true
			}
			if !usageLogged && (result.InputTokens > 0 || result.OutputTokens > 0 || result.CacheReadTokens > 0) {
				h.logger.Info("[Codex Usage]", "request_id", requestID, "input_tokens", result.InputTokens, "output_tokens", result.OutputTokens, "cache_read_tokens", result.CacheReadTokens)
				usageLogged = true
			}
		}
		if !headersWritten {
			pending = append(pending, event.raw...)
		}
		if event.err != nil {
			if resp.Request != nil {
				event.err.Message = subscription.Redact(resp.Request.Context(), event.err.Message)
			}
			if !headersWritten {
				return event.err
			}
			downstreamStreamErr = event.err
		}
		streamCompleted = streamCompleted || event.completed
		if headersWritten {
			writeBytes(event.raw)
		} else if event.err == nil && (event.output || event.completed) {
			flushPending()
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		bytesProcessed += int64(len(line))
		if err := processEvent(decoder.wireLine(line)); err != nil {
			return restoreFailure(err)
		}
		if !headersWritten && len(pending)+len(decoder.raw) >= maxPreOutputStreamBufferBytes {
			pending = append(pending, decoder.raw...)
			return restoreFailure(invalidCodexStream("stream_prelude_too_large", "上游响应在有效输出前超过缓冲上限"))
		}
	}
	// Several compatible servers omit the final blank line. Preserve this
	// accepted framing without accepting an arbitrary JSON body as SSE.
	if err := processEvent(decoder.end()); err != nil {
		return restoreFailure(err)
	}

	// 🆕 [详细日志] 扫描器错误检测：非 nil 视为流式中断；nil 视为正常完成
	if scanErr := scanner.Err(); scanErr != nil {
		h.logger.Error(fmt.Sprintf("❌ [Codex流式失败] [%s] 端点: %s, 已处理 %d 字节, 错误: %v",
			requestID, endpointName, bytesProcessed, scanErr),
			"provider", "codex", "request_id", requestID)
		if streamCompleted && downstreamStreamErr == nil {
			flushPending()
			h.logger.Warn("[Codex 流式收尾异常] 已收到完成事件，按成功记录",
				"provider", "codex",
				"request_id", requestID,
				"endpoint", endpointName,
				"bytes_processed", bytesProcessed,
				"error", scanErr,
			)
			prepareArchive(nil, true)
			h.recordSuccess(requestID, startTime, resp.StatusCode, lastResult, requestModel, endpointName, groupName, channelName)
			return nil, true
		}
		if !headersWritten {
			return restoreFailure(&UpstreamStreamError{Type: "stream_read_error", Code: "stream_read_error", Message: scanErr.Error()})
		}
		if isContextCanceledError(scanErr) {
			if outputSentRecorded {
				h.logger.Warn("[Codex 流式下游取消] 已输出内容后连接取消，按客户端取消记录",
					"provider", "codex",
					"request_id", requestID,
					"endpoint", endpointName,
					"bytes_processed", bytesProcessed,
					"error", scanErr,
				)
				prepareArchive(nil, true)
				h.recordCancelled(requestID, startTime, "client_canceled_after_output", lastResult, requestModel, endpointName, groupName, channelName)
				return nil, true
			}
		}
		// 上游 HTTP 层这次是真给过响应的（200），流是在那之后断的，就记它的真实状态码。
		// 原先这里固定写 502，是一个提供商从未返回过的码。
		prepareArchive(nil, false)
		h.recordUpstreamFailure(requestID, startTime, "network_error", scanErr.Error(), resp.StatusCode, lastResult, requestModel)
		return nil, false
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
		prepareArchive(downstreamStreamErr, false)
		h.recordUpstreamFailure(requestID, startTime, "server_error", downstreamStreamErr.Error(), resp.StatusCode, lastResult, requestModel)
		return nil, false
	}

	if !headersWritten && !streamCompleted {
		return restoreFailure(codexStreamPreludeError(pending))
	}
	flushPending()
	if streamCompleted {
		h.logger.Info("[Codex 流式完成]", "request_id", requestID, "endpoint", endpointName, "bytes_processed", bytesProcessed)
	} else {
		h.logger.Warn("[Codex 流式响应未完整结束]", "request_id", requestID, "endpoint", endpointName, "bytes_processed", bytesProcessed)
	}

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

	qualityReason := ""
	if !streamCompleted {
		qualityReason = "incomplete_stream"
	}
	prepareArchive(nil, streamCompleted)
	h.recordSuccess(requestID, startTime, resp.StatusCode, lastResult, requestModel, endpointName, groupName, channelName, qualityReason)
	return nil, true
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
