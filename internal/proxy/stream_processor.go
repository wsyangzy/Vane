package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-relay-forwarder/internal/proxy/response"
	"ai-relay-forwarder/internal/tracking"
	"ai-relay-forwarder/internal/utils"
)

// 缓冲区大小常量
const (
	StreamBufferSize     = 8192 // 8KB主缓冲区
	LineBufferInitSize   = 1024 // 1KB行缓冲区初始大小
	BackgroundBufferSize = 4096 // 4KB后台解析缓冲区
	DebugLineLimit       = 100  // 调试模式下最多保存100行SSE数据
)

// StreamProcessor 流式处理器核心结构体
type StreamProcessor struct {
	// 核心组件
	tokenParser    *TokenParser           // Token解析器，用于提取模型信息和使用统计
	usageTracker   *tracking.UsageTracker // 使用跟踪器，记录请求生命周期
	responseWriter http.ResponseWriter    // HTTP响应写入器
	flusher        http.Flusher           // HTTP刷新器，用于立即发送数据到客户端

	// 错误处理和恢复
	errorRecovery *ErrorRecoveryManager // 错误恢复管理器
	lastAPIError  error                 // 最后一次API错误信息

	// 请求标识信息
	requestID string // 请求唯一标识符
	endpoint  string // 端点名称

	// 流式处理状态
	startTime      time.Time // 处理开始时间
	bytesProcessed int64     // 已处理字节数
	lineBuffer     []byte    // SSE行缓冲区
	partialData    []byte    // 部分数据缓冲区，用于错误恢复

	// 并发控制
	parseMutex sync.Mutex // 保护 partialData、tokenParser 等共享状态

	// 错误处理
	parseErrors    []error // 解析过程中的错误集合
	maxParseErrors int     // 最大允许解析错误数

	// 完成状态跟踪
	completionRecorded bool // 是否已经记录完成状态，防止重复记录

	// 🔍 [调试缓冲区] 轻量级调试数据收集（仅在token解析失败时使用）
	debugLines []string // SSE行数据收集，最多保存DebugLineLimit行

	// 🔥 [重复计费防护] 2025-12-26: 追踪是否已向客户端发送实际模型输出
	hasContentSent   bool       // 是否已发送 content_block_delta 事件
	contentSentMutex sync.Mutex // 保护 hasContentSent 的并发访问
}

// NewStreamProcessor 创建新的流式处理器实例
func NewStreamProcessor(tokenParser *TokenParser, usageTracker *tracking.UsageTracker,
	w http.ResponseWriter, flusher http.Flusher, requestID, endpoint string) *StreamProcessor {

	sp := &StreamProcessor{
		tokenParser:    tokenParser,
		usageTracker:   usageTracker,
		responseWriter: w,
		flusher:        flusher,
		errorRecovery:  NewErrorRecoveryManager(usageTracker),
		requestID:      requestID,
		endpoint:       endpoint,
		startTime:      time.Now(),
		lineBuffer:     make([]byte, 0, LineBufferInitSize),
		partialData:    make([]byte, 0, BackgroundBufferSize),
		maxParseErrors: 10,                                // 最多允许10个解析错误
		debugLines:     make([]string, 0, DebugLineLimit), // 🔍 [调试] 初始化调试缓冲区
	}

	return sp
}

// ProcessStream 实现边接收边转发的8KB缓冲区流式处理
// 这是核心方法，实现真正的流式处理机制
func (sp *StreamProcessor) ProcessStream(ctx context.Context, resp *http.Response) (*tracking.TokenUsage, error) {
	defer resp.Body.Close()
	defer sp.finalizeParsing() // 确保剩余缓冲数据被处理

	// 🔧 [解压缩修复] 创建响应处理器并获取解压缩的流式读取器
	processor := response.NewProcessor()
	decompressedReader, err := processor.DecompressStreamReader(resp)
	if err != nil {
		return nil, fmt.Errorf("🗜️ [解压缩失败] [%s] 端点: %s, 错误: %w", sp.requestID, sp.endpoint, err)
	}
	defer decompressedReader.Close() // 确保解压缩读取器被关闭

	// 记录解压缩状态
	contentEncoding := resp.Header.Get("Content-Encoding")
	if contentEncoding != "" {
		slog.Info(fmt.Sprintf("🗜️ [流式解压] [%s] 端点: %s, 编码: %s", sp.requestID, sp.endpoint, contentEncoding))
	}

	// 初始化8KB缓冲区，使用解压缩后的读取器
	buffer := make([]byte, StreamBufferSize)
	reader := bufio.NewReader(decompressedReader)

	// 记录流处理开始
	slog.Info(fmt.Sprintf("🌊 [流式处理] [%s] 开始流式处理，端点: %s", sp.requestID, sp.endpoint))

	// 主流式处理循环
	for {
		// 检查context取消 - 优先级最高
		select {
		case <-ctx.Done():
			// 客户端取消，进入优雅取消处理
			return sp.handleCancellation(ctx, ctx.Err())
		default:
			// 继续正常处理
		}

		// 1. 从响应中读取数据到8KB缓冲区
		n, err := reader.Read(buffer)

		if n > 0 {
			chunk := buffer[:n]

			// 保存部分数据用于错误恢复
			sp.savePartialData(chunk)

			// 2. 立即转发到客户端 - 这是关键！不等待完整响应
			if writeErr := sp.forwardToClient(chunk); writeErr != nil {
				// 使用错误恢复管理器处理转发错误
				errorCtx := sp.errorRecovery.ClassifyError(writeErr, sp.requestID, sp.endpoint, "", 0)
				sp.errorRecovery.HandleFinalFailure(errorCtx)
				slog.Error(fmt.Sprintf("❌ [流式错误] [%s] 转发到客户端失败: %v", sp.requestID, writeErr))
				return nil, fmt.Errorf("failed to forward to client: %w", writeErr)
			}

			// 3. 同步解析Token信息 - 保证chunk顺序，避免goroutine竞态
			sp.parseTokensInline(chunk)

			// 4. 更新处理状态
			sp.bytesProcessed += int64(n)
		}

		// 处理读取结束和错误
		if err == io.EOF {
			// 处理剩余缓冲数据
			sp.finalizeParsing()

			// 获取最终的 Token 使用信息
			finalTokenUsage := sp.getFinalTokenUsage()

			slog.Info(fmt.Sprintf("✅ [流式完成] [%s] 端点: %s, 流处理正常完成，已处理 %d 字节",
				sp.requestID, sp.endpoint, sp.bytesProcessed))
			return finalTokenUsage, nil
		}

		if err != nil {
			// 网络中断或其他错误，尝试部分数据处理
			return sp.handlePartialStream(err)
		}
	}
}

// forwardToClient 立即转发数据到客户端
func (sp *StreamProcessor) forwardToClient(data []byte) error {
	// 写入数据到响应
	if _, err := sp.responseWriter.Write(data); err != nil {
		return err
	}

	// 立即刷新，确保数据立即发送到客户端
	sp.flusher.Flush()

	return nil
}

// parseTokensInline 同步Token解析，在主循环中内联执行
// 🔧 [竞态修复] 2026-03-14: 原 parseTokensInBackground 为每个 8KB 数据块启动独立
// goroutine，但这些 goroutine 共享 lineBuffer 且 Go 不保证执行顺序。当 goroutine
// 乱序执行时，SSE 行拼接被破坏，导致 message_delta(usage) 事件丢失，最终触发
// "使用了 message_start fallback" 误报和不必要的重试（向提供商发送重复请求）。
// 修复：改为同步执行。SSE 行解析极轻量（逐字节扫描+偶尔 JSON），不影响转发性能。
func (sp *StreamProcessor) parseTokensInline(data []byte) {
	// 逐字节处理，构建SSE行（同步执行，保证chunk顺序）
	for _, b := range data {
		// 构建行缓冲区
		sp.lineBuffer = append(sp.lineBuffer, b)

		// 检测换行符，处理完整的SSE行
		if b == '\n' {
			line := strings.TrimSpace(string(sp.lineBuffer))

			// 处理所有行，包括空行（空行触发SSE事件解析）
			sp.processSSELine(line)

			// 重置行缓冲区，准备下一行
			sp.lineBuffer = sp.lineBuffer[:0]
		}
	}
}

// processSSELine 处理单个SSE行
// 修改版本：仅进行 Token 解析，不再直接记录到 usageTracker
func (sp *StreamProcessor) processSSELine(line string) {
	// 🔥 [重复计费防护] 2025-12-26: 检测是否为实际模型输出事件
	trimmedLine := strings.TrimSpace(line)
	if strings.HasPrefix(trimmedLine, "event:") {
		eventType := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "event:"))
		if eventType == "content_block_delta" {
			// 标记已向客户端发送实际内容
			sp.contentSentMutex.Lock()
			sp.hasContentSent = true
			sp.contentSentMutex.Unlock()
			// 🔧 [日志优化] 2025-12-29: 移除高频Debug日志，减少日志噪音
		}
	}

	// 🔍 [调试数据收集] 轻量级收集SSE行数据（无性能影响）
	if len(sp.debugLines) < DebugLineLimit {
		sp.debugLines = append(sp.debugLines, line)
	}

	// 解析 SSE 事件
	result := sp.tokenParser.ParseSSEEvent(line)

	if result != nil {
		// 检查是否有错误信息
		if result.ErrorInfo != nil {
			slog.Error(fmt.Sprintf("❌ [API错误] [%s] 类型: %s, 消息: %s",
				sp.requestID, result.ErrorInfo.Type, result.ErrorInfo.Message))

			// 将错误信息存储，供上层生命周期管理器处理
			sp.lastAPIError = fmt.Errorf("API错误 %s: %s", result.ErrorInfo.Type, result.ErrorInfo.Message)
			return
		}

		// 处理正常Token信息
		if result.TokenUsage != nil {
			// 直接使用ParseResult，无需类型转换
			trackingTokens := result.TokenUsage
			modelName := result.ModelName

			// 确保模型名称不为空
			if modelName == "" {
				modelName = "default"
			}

			// ✅ 移除completionRecorded限制，每次都更新最新token统计
			slog.Debug(fmt.Sprintf("🔄 [Token实时更新] [%s] 模型: %s, 输入: %d, 输出: %d, 缓存创建: %d, 缓存读取: %d",
				sp.requestID, modelName, trackingTokens.InputTokens, trackingTokens.OutputTokens,
				trackingTokens.CacheCreationTokens, trackingTokens.CacheReadTokens))

			// ✅ 移除或注释掉这个字段，因为不再需要
			// sp.completionRecorded = true
		}
	}
}

// 🔧 [P4.3 清理] 已删除 ensureRequestCompletion DEPRECATED 死方法
// 此方法已被 getFinalTokenUsage() 完全替代，无外部调用者

// attemptUsageRecovery 异步尝试从debug文件恢复完整的usage信息
// 🔧 [Fallback修复] 等待debug文件写入完成后，尝试提取完整的token使用统计并更新数据库
func (sp *StreamProcessor) attemptUsageRecovery() {
	// 等待一段时间确保debug文件写入完成
	time.Sleep(2 * time.Second)

	modelName := sp.tokenParser.GetModelName()
	if modelName == "" {
		modelName = "unknown"
	}

	// 尝试从debug文件恢复usage信息
	err := utils.RecoverAndUpdateUsage(sp.requestID, modelName, sp.usageTracker)
	if err != nil {
		slog.Warn(fmt.Sprintf("🔧 [Recovery失败] [%s] 从debug文件恢复usage失败: %v", sp.requestID, err))
		return
	}

	slog.Info(fmt.Sprintf("🔧 [Recovery成功] [%s] 成功从debug文件恢复完整的usage信息", sp.requestID))
}

// classifyStreamError 智能状态分类方法
// 确保 context canceled 正确归类为 cancelled 而不是 error
func (sp *StreamProcessor) classifyStreamError(err error, tokenUsage *tracking.TokenUsage) string {
	if err == nil {
		return "success"
	}

	errStr := err.Error()

	// ✅ 优先检测取消状态（修复核心问题）
	if errStr == "context canceled" || strings.Contains(errStr, "context canceled") {
		// 检查是否有token信息，用于日志区分
		hasTokens := tokenUsage != nil && (tokenUsage.InputTokens > 0 || tokenUsage.OutputTokens > 0 ||
			tokenUsage.CacheCreationTokens > 0 || tokenUsage.CacheReadTokens > 0)

		if hasTokens {
			slog.Warn(fmt.Sprintf("⚠️ [已计费取消] [%s] 用户取消但已产生token计费", sp.requestID))
		} else {
			slog.Info(fmt.Sprintf("🚫 [早期取消] [%s] 用户早期取消，未产生计费", sp.requestID))
		}

		return "cancelled" // ✅ 使用现有cancelled状态
	}

	// 其他错误保持原有逻辑
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded") {
		return "timeout"
	}

	return "error"
}

// 🔧 [P4.3 清理] 已删除 handlePartialStream DEPRECATED 死方法
// 此方法已被 handlePartialStream() 完全替代，无外部调用者

// ProcessStreamWithRetry 流式处理入口（无重试版本）
// 2025-12-11: 移除无效的重试循环，流式传输阶段不重试以避免重复计费
// 原因：resp.Body 在 ProcessStream 返回后会被关闭，重试无法重新读取
// 返回值：(finalTokenUsage *tracking.TokenUsage, modelName string, err error)
func (sp *StreamProcessor) ProcessStreamWithRetry(ctx context.Context, resp *http.Response) (*tracking.TokenUsage, string, error) {
	// 执行流式处理（单次调用，不重试）
	finalTokenUsage, err := sp.ProcessStream(ctx, resp)

	if err == nil {
		// ✅ 检查是否在处理过程中遇到了API错误（如SSE错误事件）
		if sp.lastAPIError != nil {
			modelName := sp.tokenParser.GetModelName()
			if modelName == "" {
				modelName = "unknown"
			}

			// 智能错误包装：检查API错误是否已被包装，避免重复包装
			if strings.HasPrefix(sp.lastAPIError.Error(), "stream_status:") {
				return finalTokenUsage, modelName, sp.lastAPIError
			}

			// 根据API错误内容智能确定状态
			status := "stream_error"
			errorMsg := sp.lastAPIError.Error()
			if strings.Contains(errorMsg, "rate") || strings.Contains(errorMsg, "429") {
				status = "rate_limited"
			} else if strings.Contains(errorMsg, "timeout") || strings.Contains(errorMsg, "deadline") {
				status = "timeout"
			} else if strings.Contains(errorMsg, "cancel") {
				status = "cancelled"
			} else if strings.Contains(errorMsg, "auth") || strings.Contains(errorMsg, "401") {
				status = "auth_error"
			}

			wrappedErr := fmt.Errorf("stream_status:%s:model:%s: %w", status, modelName, sp.lastAPIError)
			return finalTokenUsage, modelName, wrappedErr
		}

		// 🆕 [流完整性检测] 2025-12-11
		// 检查流是否完整，即使 err == nil 也可能是不完整的流
		completeness := sp.tokenParser.GetStreamCompleteness()
		modelName := sp.tokenParser.GetModelName()
		if modelName == "" {
			modelName = "default"
		}

		if !completeness.IsComplete {
			// 流不完整，返回结构化错误让上层设置 failure_reason
			slog.Warn(fmt.Sprintf("⚠️ [流不完整] [%s] %s", sp.requestID, completeness.Reason))

			// 🔧 [结构化错误] 2025-12-11: 使用 StreamIncompleteError 替代字符串格式
			streamErr := &StreamIncompleteError{
				FailureReason: completeness.FailureReason,
				ModelName:     modelName,
				Reason:        completeness.Reason,
			}
			return finalTokenUsage, modelName, streamErr
		}

		// 处理成功且流完整
		return finalTokenUsage, modelName, nil
	}

	// 流式处理失败，进行错误包装
	tokenUsage := sp.getFinalTokenUsage()
	modelName := sp.tokenParser.GetModelName()
	if modelName == "" {
		modelName = "unknown"
	}

	// 智能错误包装：检查错误是否已被包装，避免重复包装
	if strings.HasPrefix(err.Error(), "stream_status:") {
		return tokenUsage, modelName, err
	}

	// 根据错误内容智能确定状态
	status := "error"
	errStr := err.Error()
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
		status = "timeout"
	} else if strings.Contains(errStr, "cancel") {
		status = "cancelled"
	} else if strings.Contains(errStr, "network") || strings.Contains(errStr, "connection") {
		status = "network_error"
	}

	wrappedErr := fmt.Errorf("stream_status:%s:model:%s: %w", status, modelName, err)
	return tokenUsage, modelName, wrappedErr
}

// finalizeParsing 处理剩余缓冲数据并刷新待处理事件
// 🔧 [竞态修复] 2026-03-14: 原 waitForBackgroundParsing，改为同步解析后不再需要等待 goroutine
func (sp *StreamProcessor) finalizeParsing() {
	// 处理剩余的行缓冲区数据
	if len(sp.lineBuffer) > 0 {
		line := strings.TrimSpace(string(sp.lineBuffer))
		if len(line) > 0 {
			sp.processSSELine(line)
		}
		sp.lineBuffer = sp.lineBuffer[:0]
	}

	// 强制刷新缓存的待处理事件
	// 确保即使上游没有发送终止空行，也能解析完整的 usage 信息
	if result := sp.tokenParser.FlushPendingEvent(); result != nil {
		// 处理刷新后的解析结果
		if result.ErrorInfo != nil {
			slog.Error(fmt.Sprintf("❌ [Flush错误] [%s] 类型: %s, 消息: %s",
				sp.requestID, result.ErrorInfo.Type, result.ErrorInfo.Message))
			sp.lastAPIError = fmt.Errorf("API错误 %s: %s", result.ErrorInfo.Type, result.ErrorInfo.Message)
		} else if result.TokenUsage != nil {
			slog.Debug(fmt.Sprintf("🔄 [Flush成功] [%s] 成功解析待处理事件的Token信息", sp.requestID))
		}
	}
}

// savePartialData 保存部分数据用于错误恢复
func (sp *StreamProcessor) savePartialData(chunk []byte) {
	// 限制部分数据缓冲区大小，防止内存过度使用
	const maxPartialDataSize = 64 * 1024 // 64KB

	sp.parseMutex.Lock()
	defer sp.parseMutex.Unlock()

	// 如果添加新数据会超过限制，则移除旧数据
	if len(sp.partialData)+len(chunk) > maxPartialDataSize {
		// 保留最后的32KB数据，丢弃更早的数据
		keepSize := maxPartialDataSize/2 - len(chunk)
		if keepSize > 0 && keepSize < len(sp.partialData) {
			copy(sp.partialData, sp.partialData[len(sp.partialData)-keepSize:])
			sp.partialData = sp.partialData[:keepSize]
		} else {
			sp.partialData = sp.partialData[:0]
		}
	}

	// 添加新的数据块
	sp.partialData = append(sp.partialData, chunk...)
}

// GetProcessingStats 获取处理统计信息
func (sp *StreamProcessor) GetProcessingStats() map[string]interface{} {
	return map[string]interface{}{
		"request_id":       sp.requestID,
		"endpoint":         sp.endpoint,
		"bytes_processed":  sp.bytesProcessed,
		"processing_time":  time.Since(sp.startTime),
		"parse_errors":     len(sp.parseErrors),
		"max_parse_errors": sp.maxParseErrors,
	}
}

// Reset 重置处理器状态，用于复用
func (sp *StreamProcessor) Reset() {
	sp.parseMutex.Lock()
	defer sp.parseMutex.Unlock()

	sp.startTime = time.Now()
	sp.bytesProcessed = 0
	sp.lineBuffer = sp.lineBuffer[:0]
	sp.partialData = sp.partialData[:0] // 重置部分数据缓冲区
	sp.parseErrors = sp.parseErrors[:0]

	// 重置TokenParser状态
	if sp.tokenParser != nil {
		sp.tokenParser.Reset()
	}

	slog.Info(fmt.Sprintf("🔄 [处理器重置] [%s] 流处理器已重置", sp.requestID))
}

// 🔧 [P4.3 清理] 已删除 handleCancellation 和 collectAvailableInfo 两个 DEPRECATED 死方法
// 它们已被 handleCancellation() 和 collectAvailableInfo() 完全替代，无外部调用者

// getFinalTokenUsage 获取最终的Token使用信息（修复版）
func (sp *StreamProcessor) getFinalTokenUsage() *tracking.TokenUsage {
	sp.parseMutex.Lock()
	defer sp.parseMutex.Unlock()

	// 尝试从TokenParser获取最终使用统计
	finalUsage := sp.tokenParser.GetFinalUsage()

	// 🔍 [Fallback检测] 检查是否使用了fallback机制
	if sp.tokenParser.IsFallbackUsed() {
		slog.Warn(fmt.Sprintf("🚨 [Fallback检测] [%s] 使用了message_start fallback机制，保存完整调试数据", sp.requestID))

		// 🔧 [Fallback修复] fallback时使用完整的响应数据，不受100行限制
		if len(sp.partialData) > 0 {
			// 将完整的原始数据按行分割
			fullLines := strings.Split(string(sp.partialData), "\n")
			slog.Info(fmt.Sprintf("🔧 [完整数据] [%s] 使用%d字节完整数据替代100行限制数据", sp.requestID, len(sp.partialData)))

			// 使用完整数据保存debug文件
			utils.WriteStreamDebugResponse(sp.requestID, sp.endpoint, fullLines, sp.bytesProcessed)
		} else {
			// 兜底：如果没有完整数据，使用现有的debug数据
			utils.WriteStreamDebugResponse(sp.requestID, sp.endpoint, sp.debugLines, sp.bytesProcessed)
		}

		// 🔧 [Fallback修复] 异步尝试从debug文件恢复完整的usage信息
		go sp.attemptUsageRecovery()
	}

	if finalUsage != nil {
		// ✅ 检查是否有真实的Token使用
		hasRealTokens := finalUsage.InputTokens > 0 || finalUsage.OutputTokens > 0 ||
			finalUsage.CacheCreationTokens > 0 || finalUsage.CacheReadTokens > 0

		if hasRealTokens {
			// 有真实token信息，记录详细日志
			modelName := sp.tokenParser.GetModelName()
			if modelName == "" {
				modelName = "default"
			}
			slog.Info(fmt.Sprintf("🪙 [Token最终统计] [%s] 流式处理完成 - 模型: %s, 输入: %d, 输出: %d, 缓存创建: %d, 缓存读取: %d",
				sp.requestID, modelName, finalUsage.InputTokens, finalUsage.OutputTokens, finalUsage.CacheCreationTokens, finalUsage.CacheReadTokens))
			return finalUsage
		} else {
			// 有finalUsage结构但无实际token，返回nil
			slog.Info(fmt.Sprintf("🎯 [无Token完成] [%s] 流式响应包含空Token信息", sp.requestID))

			// 🔍 [调试] 异步保存流式调试数据用于分析Token解析失败
			utils.WriteStreamDebugResponse(sp.requestID, sp.endpoint, sp.debugLines, sp.bytesProcessed)

			return nil
		}
	} else {
		// 没有token信息，返回nil
		slog.Info(fmt.Sprintf("🎯 [无Token完成] [%s] 流式响应不包含token信息", sp.requestID))

		// 🔍 [调试] 异步保存流式调试数据用于分析Token解析失败
		utils.WriteStreamDebugResponse(sp.requestID, sp.endpoint, sp.debugLines, sp.bytesProcessed)

		return nil
	}
}

// handlePartialStream 处理部分数据流中断情况（返回Token信息版本）
// 当网络中断或其他错误发生时，收集已解析的Token信息并返回
func (sp *StreamProcessor) handlePartialStream(err error) (*tracking.TokenUsage, error) {
	// ✅ 处理剩余缓冲数据（包含flush逻辑）
	sp.finalizeParsing()

	// 获取Token信息和模型名称
	finalUsage := sp.tokenParser.GetFinalUsage()
	modelName := sp.tokenParser.GetModelName()
	if modelName == "" {
		modelName = "unknown"
	}

	// ✅ 使用智能状态分类
	status := sp.classifyStreamError(err, finalUsage)

	if finalUsage != nil {
		slog.Info(fmt.Sprintf("💾 [流中断保存] [%s] 状态: %s, 模型: %s, 输入: %d, 输出: %d, 缓存: %d+%d",
			sp.requestID, status, modelName, finalUsage.InputTokens, finalUsage.OutputTokens,
			finalUsage.CacheCreationTokens, finalUsage.CacheReadTokens))
	} else {
		slog.Info(fmt.Sprintf("💾 [流中断保存] [%s] 状态: %s, 模型: %s, 无token信息",
			sp.requestID, status, modelName))
	}

	// 尝试从部分数据中恢复有用信息
	if len(sp.partialData) > 0 {
		sp.errorRecovery.RecoverFromPartialData(sp.requestID, sp.partialData, time.Since(sp.startTime))
	}

	// 🔍 [EOF调试] 流中断时保存 debug 文件，无论是否有 Token 信息，方便调试
	if len(sp.partialData) > 0 {
		fullLines := strings.Split(string(sp.partialData), "\n")
		utils.WriteStreamDebugResponse(sp.requestID, sp.endpoint, fullLines, sp.bytesProcessed)
	} else if len(sp.debugLines) > 0 {
		utils.WriteStreamDebugResponse(sp.requestID, sp.endpoint, sp.debugLines, sp.bytesProcessed)
	}

	// 处理Token信息
	var partialTokenUsage *tracking.TokenUsage
	if finalUsage != nil {
		partialTokenUsage = finalUsage
	} else {
		partialTokenUsage = &tracking.TokenUsage{
			InputTokens: 0, OutputTokens: 0,
			CacheCreationTokens: 0, CacheReadTokens: 0,
		}
	}

	// ✅ 返回包含状态和模型信息的错误，供上层处理
	return partialTokenUsage, fmt.Errorf("stream_status:%s:model:%s: %w", status, modelName, err)
}

// handleCancellation 处理客户端取消请求（返回Token信息版本）
// 🔧 [竞态修复] 2026-03-14: token 解析已改为同步执行，无需再等待后台解析完成。
func (sp *StreamProcessor) handleCancellation(ctx context.Context, cancelErr error) (*tracking.TokenUsage, error) {
	slog.Info(fmt.Sprintf("🚫 [客户端取消] [%s] 检测到客户端取消: %v", sp.requestID, cancelErr))

	return sp.collectAvailableInfo(cancelErr)
}

// collectAvailableInfo 智能信息收集（返回Token信息版本）
func (sp *StreamProcessor) collectAvailableInfo(cancelErr error) (*tracking.TokenUsage, error) {
	sp.parseMutex.Lock()
	defer sp.parseMutex.Unlock()

	// ✅ 尝试刷新待处理事件
	if result := sp.tokenParser.FlushPendingEvent(); result != nil {
		if result.ErrorInfo != nil {
			slog.Error(fmt.Sprintf("❌ [取消Flush错误] [%s] 类型: %s, 消息: %s",
				sp.requestID, result.ErrorInfo.Type, result.ErrorInfo.Message))
		} else if result.TokenUsage != nil {
			slog.Debug(fmt.Sprintf("🔄 [取消Flush成功] [%s] 成功解析取消前的待处理事件", sp.requestID))
		}
	}

	// 获取已解析的信息
	modelName := sp.tokenParser.GetModelName()
	finalUsage := sp.tokenParser.GetFinalUsage()

	// ✅ 使用智能状态分类
	statusClassified := sp.classifyStreamError(cancelErr, finalUsage)

	var tokenUsage *tracking.TokenUsage

	if finalUsage != nil {
		tokenUsage = finalUsage
		slog.Info(fmt.Sprintf("💾 [取消保存] [%s] 状态: %s, 模型: %s, 输入: %d, 输出: %d",
			sp.requestID, statusClassified, modelName, finalUsage.InputTokens, finalUsage.OutputTokens))
	} else {
		tokenUsage = &tracking.TokenUsage{
			InputTokens: 0, OutputTokens: 0,
			CacheCreationTokens: 0, CacheReadTokens: 0,
		}
		slog.Info(fmt.Sprintf("💾 [取消保存] [%s] 状态: %s, 模型: %s, 无token信息",
			sp.requestID, statusClassified, modelName))
	}

	// ✅ 返回包含完整信息的错误
	return tokenUsage, fmt.Errorf("stream_status:%s:model:%s: %w", statusClassified, modelName, cancelErr)
}

// HasSentContent 检查是否已向客户端发送实际模型输出内容
// 🔥 [重复计费防护] 2025-12-26: 用于判断流式请求是否应该重试
func (sp *StreamProcessor) HasSentContent() bool {
	sp.contentSentMutex.Lock()
	defer sp.contentSentMutex.Unlock()
	return sp.hasContentSent
}
