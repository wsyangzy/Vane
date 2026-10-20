package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/monitor"
	"github.com/wsyangzy/Vane/internal/tracking"
)

// TokenParser 处理SSE事件的解析以提取token使用信息
// 实现TokenParserInterface接口
type TokenParser struct {
	// 用于收集多行JSON数据的缓冲区
	eventBuffer    strings.Builder
	currentEvent   string
	collectingData bool
	// 用于日志记录的请求ID
	requestID string
	// 从message_start事件中提取的模型名称
	modelName string
	// 用于记录token使用和成本的跟踪器
	usageTracker *tracking.UsageTracker
	// 用于计算持续时间的开始时间
	startTime time.Time
	// 用于累积的最终token使用量
	finalUsage *tracking.TokenUsage
	// 用于处理中断的部分使用量
	partialUsage *tracking.TokenUsage

	// 🆕 [流完整性追踪] 2025-12-11
	hasMessageStart      bool // 是否收到 message_start 事件
	hasMessageDeltaUsage bool // 是否收到带 usage 的 message_delta 事件
	hasMessageStop       bool // 是否收到 message_stop 事件

	// 🔇 [日志去重] 增量事件无 token 的提示只打一次（一个流可能有几十~几百个增量事件）
	noTokenLogged bool
}

// fixMalformedEventType 修复格式错误的事件类型
// 处理如 "content_event: message_delta" 这样的格式错误，提取最后一个有效的事件名称
func (tp *TokenParser) fixMalformedEventType(eventType string) string {
	// 🔧 [格式错误修复] 处理格式错误的事件行，如 "event: content_event: message_delta"
	// 从事件类型中提取最后一个有效的事件名称
	if strings.Contains(eventType, ":") {
		parts := strings.Split(eventType, ":")
		// 取最后一个非空部分作为真正的事件类型
		for i := len(parts) - 1; i >= 0; i-- {
			cleanPart := strings.TrimSpace(parts[i])
			if cleanPart != "" {
				if tp.requestID != "" {
					slog.Warn(fmt.Sprintf("⚠️ [格式错误修复] [%s] 检测到格式错误的事件行，修正为: %s", tp.requestID, cleanPart))
				}
				return cleanPart
			}
		}
	}
	return eventType
}

// NewTokenParser 创建新的token解析器实例
func NewTokenParser() *TokenParser {
	return &TokenParser{
		startTime: time.Now(),
	}
}

// NewTokenParserWithRequestID 创建带请求ID的新token解析器实例
func NewTokenParserWithRequestID(requestID string) *TokenParser {
	return &TokenParser{
		requestID: requestID,
		startTime: time.Now(),
	}
}

// NewTokenParserWithUsageTracker 创建带使用跟踪器的新token解析器实例
func NewTokenParserWithUsageTracker(requestID string, usageTracker *tracking.UsageTracker) *TokenParser {
	return &TokenParser{
		requestID:    requestID,
		usageTracker: usageTracker,
		startTime:    time.Now(),
	}
}

// ParseSSEEvent 新版本的SSE解析方法
// 返回 ParseResult 而不是直接调用 usageTracker
func (tp *TokenParser) ParseSSEEvent(line string) *ParseResult {
	line = strings.TrimSpace(line)

	// 处理事件类型行 - 支持 "event: " 和 "event:" 两种格式
	if strings.HasPrefix(line, "event:") {
		var eventType string
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else {
			eventType = strings.TrimPrefix(line, "event:")
		}

		// 使用公共方法修复格式错误的事件类型
		eventType = tp.fixMalformedEventType(eventType)

		tp.currentEvent = eventType

		// 🆕 [流完整性追踪] 检测 message_stop 事件
		if eventType == "message_stop" {
			tp.hasMessageStop = true
		}

		// 为message_start（模型信息）、增量usage事件和error事件收集数据
		tp.collectingData = eventType == "message_delta" || eventType == "content_block_delta" || eventType == "message_start" || eventType == "error"
		tp.eventBuffer.Reset()
		return nil
	}

	// 处理数据行 - 支持 "data: " 和 "data:" 两种格式
	if strings.HasPrefix(line, "data:") && tp.collectingData {
		var dataContent string
		if strings.HasPrefix(line, "data: ") {
			dataContent = strings.TrimPrefix(line, "data: ")
		} else {
			dataContent = strings.TrimPrefix(line, "data:")
		}
		tp.eventBuffer.WriteString(dataContent)
		return nil
	}

	// 处理表示SSE事件结束的空行
	if line == "" && tp.collectingData && tp.eventBuffer.Len() > 0 {
		switch tp.currentEvent {
		case "message_start":
			// 仅解析message_start以获取模型信息（不需要ParseResult）
			tp.parseMessageStart()
			return nil
		case "message_delta", "content_block_delta":
			// 解析带 usage 的增量事件
			return tp.extractMessageDelta()
		case "error":
			// 解析 error 事件
			return tp.extractErrorEvent()
		}
	}

	return nil
}

// ParseSSELine 处理SSE流中的单行数据，如果找到则提取token使用信息
func (tp *TokenParser) ParseSSELine(line string) *monitor.TokenUsage {
	line = strings.TrimSpace(line)

	// 处理事件类型行 - 支持 "event: " 和 "event:" 两种格式
	if strings.HasPrefix(line, "event:") {
		var eventType string
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else {
			eventType = strings.TrimPrefix(line, "event:")
		}

		// 使用公共方法修复格式错误的事件类型
		eventType = tp.fixMalformedEventType(eventType)

		tp.currentEvent = eventType
		// 为message_start（模型信息）、增量usage事件和error事件收集数据
		tp.collectingData = eventType == "message_delta" || eventType == "content_block_delta" || eventType == "message_start" || eventType == "error"
		tp.eventBuffer.Reset()
		return nil
	}

	// 处理数据行 - 支持 "data: " 和 "data:" 两种格式
	if strings.HasPrefix(line, "data:") && tp.collectingData {
		var dataContent string
		if strings.HasPrefix(line, "data: ") {
			dataContent = strings.TrimPrefix(line, "data: ")
		} else {
			dataContent = strings.TrimPrefix(line, "data:")
		}
		tp.eventBuffer.WriteString(dataContent)
		return nil
	}

	// 处理表示SSE事件结束的空行
	if line == "" && tp.collectingData && tp.eventBuffer.Len() > 0 {
		switch tp.currentEvent {
		case "message_start":
			// 解析message_start以获取模型信息和token使用量
			return tp.parseMessageStart()
		case "message_delta", "content_block_delta":
			// 解析带 usage 的增量事件以获取使用信息
			return tp.parseMessageDelta()
		case "error":
			// 解析error事件并记录为API错误
			// 🚫 修复：注释掉违规的直接usageTracker调用，让生命周期管理器处理
			// tp.parseErrorEvent()
			slog.Info(fmt.Sprintf("❌ [错误事件] [%s] 检测到API错误事件", tp.requestID))
			return nil // error事件不返回TokenUsage
		}
	}

	return nil
}

// parseMessageStart 解析收集的message_start JSON数据以仅提取模型信息
func (tp *TokenParser) parseMessageStart() *monitor.TokenUsage {
	defer func() {
		tp.eventBuffer.Reset()
		tp.collectingData = false
		tp.currentEvent = ""
	}()

	// 🆕 [流完整性追踪] 标记收到 message_start 事件
	tp.hasMessageStart = true

	jsonData := tp.eventBuffer.String()
	if jsonData == "" {
		return nil
	}

	// 解析JSON数据
	var messageStart MessageStart
	if err := json.Unmarshal([]byte(jsonData), &messageStart); err != nil {
		return nil
	}

	// 如果可用，提取模型名称
	if messageStart.Message != nil && messageStart.Message.Model != "" {
		tp.modelName = messageStart.Message.Model

		// 记录模型提取 - 始终包含requestID
		slog.Info(fmt.Sprintf("🎯 [模型提取] [%s] 从message_start事件中提取模型信息: %s",
			tp.requestID, tp.modelName))
	}

	// 🆕 [流式Token修复] 从message_start事件中提取usage信息作为初始值
	// 这样确保即使流被中断，也能保存有效的token使用信息
	if messageStart.Message != nil && messageStart.Message.Usage != nil {
		usage := messageStart.Message.Usage

		// 提取分开的 5m/1h 缓存 tokens
		var cache5m, cache1h int64
		if usage.CacheCreation != nil {
			cache5m = usage.CacheCreation.Ephemeral5mInputTokens
			cache1h = usage.CacheCreation.Ephemeral1hInputTokens
		}

		// 总缓存创建 tokens = 5m + 1h（向后兼容）
		cacheCreationTotal := usage.CacheCreationInputTokens
		if cacheCreationTotal == 0 && (cache5m > 0 || cache1h > 0) {
			cacheCreationTotal = cache5m + cache1h
		}

		tp.partialUsage = &tracking.TokenUsage{
			InputTokens:           usage.InputTokens,
			OutputTokens:          usage.OutputTokens,
			CacheCreationTokens:   cacheCreationTotal,
			CacheReadTokens:       usage.CacheReadInputTokens,
			CacheCreation5mTokens: cache5m,
			CacheCreation1hTokens: cache1h,
		}

		slog.Info(fmt.Sprintf("🎯 [Usage初始化] [%s] 从message_start提取token信息: input=%d, output=%d, cache_create=%d (5m=%d, 1h=%d), cache_read=%d",
			tp.requestID, tp.partialUsage.InputTokens, tp.partialUsage.OutputTokens,
			tp.partialUsage.CacheCreationTokens, cache5m, cache1h, tp.partialUsage.CacheReadTokens))
	}

	return nil
}

// extractMessageDelta 解析增量 usage 事件并返回结构化结果
func (tp *TokenParser) extractMessageDelta() *ParseResult {
	defer func() {
		tp.eventBuffer.Reset()
		tp.collectingData = false
		tp.currentEvent = ""
	}()

	jsonData := tp.eventBuffer.String()
	if jsonData == "" {
		return nil
	}

	// 解析JSON数据
	var messageDelta MessageDelta
	if err := json.Unmarshal([]byte(jsonData), &messageDelta); err != nil {
		return nil
	}

	// 检查此增量事件是否包含使用信息
	if messageDelta.Usage == nil {
		// ⚠️ 兼容性处理：对于非Claude端点，增量事件可能不包含usage信息
		// 这种情况下需要fallback机制来标记请求完成
		if tp.requestID != "" {
			// 如果未从message_start提取模型，则使用"default"作为模型名称
			modelName := tp.modelName
			if modelName == "" {
				modelName = "default"
			}

			if !tp.noTokenLogged {
				slog.Info(fmt.Sprintf("🎯 [无Token响应] [%s] 增量事件不包含token信息，标记为完成 - 模型: %s",
					tp.requestID, modelName))
				tp.noTokenLogged = true
			}

			// 返回空Token的完成结果
			return &ParseResult{
				TokenUsage: &tracking.TokenUsage{
					InputTokens:         0,
					OutputTokens:        0,
					CacheCreationTokens: 0,
					CacheReadTokens:     0,
				},
				ModelName:   modelName,
				IsCompleted: true,
				Status:      "non_token_response",
			}
		}
		return nil
	}

	// 🆕 [流完整性追踪] 标记收到带 usage 的增量事件
	tp.hasMessageDeltaUsage = true

	// 🚀 [智能合并] 实现message_start和增量事件的token信息智能合并
	// 策略：
	// 1. input/output tokens: 优先使用增量事件的值（更准确的最终统计）
	// 2. cache tokens: 如果增量事件为0，保留message_start的值（防止缓存信息丢失）

	usage := messageDelta.Usage

	// 获取增量事件中的基础值
	inputTokens := usage.InputTokens
	outputTokens := usage.OutputTokens
	cacheCreationTokens := usage.CacheCreationInputTokens
	cacheReadTokens := usage.CacheReadInputTokens

	// 提取分开的 5m/1h 缓存 tokens
	var cache5m, cache1h int64
	if usage.CacheCreation != nil {
		cache5m = usage.CacheCreation.Ephemeral5mInputTokens
		cache1h = usage.CacheCreation.Ephemeral1hInputTokens
	}

	// 智能合并缓存token：如果增量事件中为0，但message_start中有值，则保留message_start的值
	if cacheCreationTokens == 0 && tp.partialUsage != nil && tp.partialUsage.CacheCreationTokens > 0 {
		cacheCreationTokens = tp.partialUsage.CacheCreationTokens
		slog.Info(fmt.Sprintf("🔄 [缓存合并] [%s] 保留message_start中的cache_creation_tokens: %d",
			tp.requestID, cacheCreationTokens))
	}

	// 智能合并 5m/1h 缓存
	if cache5m == 0 && tp.partialUsage != nil && tp.partialUsage.CacheCreation5mTokens > 0 {
		cache5m = tp.partialUsage.CacheCreation5mTokens
		slog.Info(fmt.Sprintf("🔄 [缓存合并] [%s] 保留message_start中的cache_5m_tokens: %d",
			tp.requestID, cache5m))
	}
	if cache1h == 0 && tp.partialUsage != nil && tp.partialUsage.CacheCreation1hTokens > 0 {
		cache1h = tp.partialUsage.CacheCreation1hTokens
		slog.Info(fmt.Sprintf("🔄 [缓存合并] [%s] 保留message_start中的cache_1h_tokens: %d",
			tp.requestID, cache1h))
	}

	// 如果有分开的 5m/1h 但没有总量，计算总量
	if cacheCreationTokens == 0 && (cache5m > 0 || cache1h > 0) {
		cacheCreationTokens = cache5m + cache1h
	}

	if cacheReadTokens == 0 && tp.partialUsage != nil && tp.partialUsage.CacheReadTokens > 0 {
		cacheReadTokens = tp.partialUsage.CacheReadTokens
		slog.Info(fmt.Sprintf("🔄 [缓存合并] [%s] 保留message_start中的cache_read_tokens: %d",
			tp.requestID, cacheReadTokens))
	}

	// 输入token合并：如果message_delta中为0，但message_start中有值，则保留message_start的值
	if inputTokens == 0 && tp.partialUsage != nil && tp.partialUsage.InputTokens > 0 {
		inputTokens = tp.partialUsage.InputTokens
		slog.Info(fmt.Sprintf("🔄 [输入合并] [%s] 保留message_start中的input_tokens: %d",
			tp.requestID, inputTokens))
	}

	// ✅ 设置finalUsage供GetFinalUsage()方法使用
	tp.finalUsage = &tracking.TokenUsage{
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		CacheCreationTokens:   cacheCreationTokens,
		CacheReadTokens:       cacheReadTokens,
		CacheCreation5mTokens: cache5m,
		CacheCreation1hTokens: cache1h,
	}

	// 如果有分开的缓存信息，记录日志
	if cache5m > 0 || cache1h > 0 {
		slog.Info(fmt.Sprintf("🎯 [缓存分离] [%s] 5m缓存=%d, 1h缓存=%d, 总计=%d",
			tp.requestID, cache5m, cache1h, cacheCreationTokens))
	}

	// 返回解析结果而不是直接记录
	return &ParseResult{
		TokenUsage:  tp.finalUsage,
		ModelName:   tp.modelName,
		IsCompleted: true,
		Status:      "completed",
	}
}

// parseMessageDelta 解析收集的增量 usage 事件以获取完整的token使用信息
func (tp *TokenParser) parseMessageDelta() *monitor.TokenUsage {
	defer func() {
		tp.eventBuffer.Reset()
		tp.collectingData = false
		tp.currentEvent = ""
	}()

	jsonData := tp.eventBuffer.String()
	if jsonData == "" {
		return nil
	}

	// 解析JSON数据
	var messageDelta MessageDelta
	if err := json.Unmarshal([]byte(jsonData), &messageDelta); err != nil {
		return nil
	}

	// 检查此增量事件是否包含使用信息
	if messageDelta.Usage == nil {
		// ⚠️ 兼容性处理：对于非Claude端点，增量事件可能不包含usage信息
		// 这种情况下需要fallback机制来标记请求完成
		if tp.requestID != "" && !tp.noTokenLogged {
			// 如果未从message_start提取模型，则使用"default"作为模型名称
			modelName := tp.modelName
			if modelName == "" {
				modelName = "default"
			}

			slog.Info(fmt.Sprintf("🎯 [无Token响应] [%s] 增量事件不包含token信息，标记为完成 - 模型: %s",
				tp.requestID, modelName))
			tp.noTokenLogged = true
		}
		return nil
	}

	// 转换为我们的TokenUsage格式
	usage := messageDelta.Usage

	// 提取分开的 5m/1h 缓存 tokens
	var cache5m, cache1h int64
	if usage.CacheCreation != nil {
		cache5m = usage.CacheCreation.Ephemeral5mInputTokens
		cache1h = usage.CacheCreation.Ephemeral1hInputTokens
	}

	// 计算总缓存创建 tokens
	cacheCreationTotal := usage.CacheCreationInputTokens
	if cacheCreationTotal == 0 && (cache5m > 0 || cache1h > 0) {
		cacheCreationTotal = cache5m + cache1h
	}

	tokenUsage := &monitor.TokenUsage{
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		CacheCreationTokens: cacheCreationTotal,
		CacheReadTokens:     usage.CacheReadInputTokens,
	}

	// ✅ 设置finalUsage供GetFinalUsage()方法使用
	tp.finalUsage = &tracking.TokenUsage{
		InputTokens:           usage.InputTokens,
		OutputTokens:          usage.OutputTokens,
		CacheCreationTokens:   cacheCreationTotal,
		CacheReadTokens:       usage.CacheReadInputTokens,
		CacheCreation5mTokens: cache5m,
		CacheCreation1hTokens: cache1h,
	}

	// 移除重复的日志记录 - 由StreamProcessor统一处理
	// if tp.requestID != "" {
	//	slog.Info(fmt.Sprintf("🪙 [Token使用统计] [%s] 从message_delta事件中提取完整令牌使用情况 -%s 输入: %d, 输出: %d, 缓存创建: %d, 缓存读取: %d",
	//		tp.requestID, modelInfo, tokenUsage.InputTokens, tokenUsage.OutputTokens, tokenUsage.CacheCreationTokens, tokenUsage.CacheReadTokens))
	// } else {
	//	slog.Info(fmt.Sprintf("🪙 [Token使用统计] 从message_delta事件中提取完整令牌使用情况 -%s 输入: %d, 输出: %d, 缓存创建: %d, 缓存读取: %d",
	//		modelInfo, tokenUsage.InputTokens, tokenUsage.OutputTokens, tokenUsage.CacheCreationTokens, tokenUsage.CacheReadTokens))
	// }

	// ✅ TokenParser只负责解析，不直接调用usage tracker
	// usage tracker的调用由上层（StreamProcessor或Handler）统一管理
	// if tp.usageTracker != nil && tp.requestID != "" {
	//	// 计算创建解析器以来的持续时间
	//	duration := time.Since(tp.startTime)
	//
	//	// 转换monitor.TokenUsage为tracking.TokenUsage
	//	trackingTokens := &tracking.TokenUsage{
	//		InputTokens:         tokenUsage.InputTokens,
	//		OutputTokens:        tokenUsage.OutputTokens,
	//		CacheCreationTokens: tokenUsage.CacheCreationTokens,
	//		CacheReadTokens:     tokenUsage.CacheReadTokens,
	//	}
	//
	//	// 记录完成的token使用和成本信息
	//	tp.usageTracker.RecordRequestComplete(tp.requestID, tp.modelName, trackingTokens, duration)
	// }

	return tokenUsage
}

// Reset 清除解析器状态
func (tp *TokenParser) Reset() {
	tp.eventBuffer.Reset()
	tp.currentEvent = ""
	tp.collectingData = false
	tp.finalUsage = nil
	tp.partialUsage = nil
	tp.startTime = time.Now()
	// 🆕 [流完整性追踪] 重置完整性追踪字段
	tp.hasMessageStart = false
	tp.hasMessageDeltaUsage = false
	tp.hasMessageStop = false
}

// SetModelName 允许直接设置模型名称（用于JSON响应解析）
func (tp *TokenParser) SetModelName(modelName string) {
	tp.modelName = modelName
}

// ParseMessageStart 实现接口方法 - 解析message_start事件提取模型信息
func (tp *TokenParser) ParseMessageStart(line string) *ModelInfo {
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}

	data := line[6:] // 移除 "data: "
	if strings.TrimSpace(data) == "[DONE]" {
		return nil
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil
	}

	if event["type"] == "message_start" {
		if message, ok := event["message"].(map[string]interface{}); ok {
			if model, ok := message["model"].(string); ok {
				tp.SetModel(model)
				return &ModelInfo{Model: model}
			}
		}
	}

	return nil
}

// ParseMessageDelta 实现接口方法 - 解析message_delta事件提取Token使用统计
func (tp *TokenParser) ParseMessageDelta(line string) *tracking.TokenUsage {
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}

	data := line[6:] // 移除 "data: "
	if strings.TrimSpace(data) == "[DONE]" {
		return nil
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil
	}

	if event["type"] == "message_delta" {
		if usage, ok := event["usage"].(map[string]interface{}); ok {
			tokenUsage := &tracking.TokenUsage{}

			if inputTokens, ok := usage["input_tokens"].(float64); ok {
				tokenUsage.InputTokens = int64(inputTokens)
			}
			if outputTokens, ok := usage["output_tokens"].(float64); ok {
				tokenUsage.OutputTokens = int64(outputTokens)
			}
			if cacheCreation, ok := usage["cache_creation_input_tokens"].(float64); ok {
				tokenUsage.CacheCreationTokens = int64(cacheCreation)
			}
			if cacheRead, ok := usage["cache_read_input_tokens"].(float64); ok {
				tokenUsage.CacheReadTokens = int64(cacheRead)
			}

			// 保存最终使用统计
			tp.finalUsage = tokenUsage

			return tokenUsage
		}
	}

	return nil
}

// SetModel 实现接口方法 - 设置模型名称
func (tp *TokenParser) SetModel(modelName string) {
	tp.modelName = modelName
}

// GetFinalUsage 实现接口方法 - 获取最终Token使用统计
// 🆕 [流式Token修复] 支持fallback机制：finalUsage > partialUsage > nil
func (tp *TokenParser) GetFinalUsage() *tracking.TokenUsage {
	// 优先返回finalUsage（来自message_delta）
	if tp.finalUsage != nil {
		return tp.finalUsage
	}

	// Fallback到partialUsage（来自message_start）
	if tp.partialUsage != nil {
		if tp.requestID != "" {
			slog.Info(fmt.Sprintf("🚨 [中断恢复] [%s] 使用message_start中的usage信息作为最终结果", tp.requestID))
		}
		return tp.partialUsage
	}

	return nil
}

// IsFallbackUsed 检查是否使用了fallback机制
// 返回true表示使用了message_start的数据而不是完整的message_delta数据
func (tp *TokenParser) IsFallbackUsed() bool {
	return tp.finalUsage == nil && tp.partialUsage != nil
}
