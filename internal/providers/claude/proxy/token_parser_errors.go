package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/wsyangzy/Vane/internal/tracking"
)

// extractErrorEvent 解析 SSE 错误事件并返回结构化结果
func (tp *TokenParser) extractErrorEvent() *ParseResult {
	defer func() {
		tp.eventBuffer.Reset()
		tp.collectingData = false
		tp.currentEvent = ""
	}()

	jsonData := tp.eventBuffer.String()
	if jsonData == "" {
		return nil
	}

	// 解析错误JSON数据
	var errorData SSEErrorData
	if err := json.Unmarshal([]byte(jsonData), &errorData); err != nil {
		if tp.requestID != "" {
			slog.Info(fmt.Sprintf("⚠️ [SSE错误解析] [%s] 无法解析错误数据: %s", tp.requestID, jsonData))
		}
		return nil
	}

	// 提取错误类型和消息
	errorType := errorData.Error.Type
	errorMessage := errorData.Error.Message
	if errorType == "" {
		errorType = "unknown_error"
	}
	if errorMessage == "" {
		errorMessage = "Unknown error"
	}

	// 记录API错误
	if tp.requestID != "" {
		slog.Info(fmt.Sprintf("❌ [API错误] [%s] 错误类型: %s, 错误信息: %s",
			tp.requestID, errorType, errorMessage))
	} else {
		slog.Info(fmt.Sprintf("❌ [API错误] 错误类型: %s, 错误信息: %s",
			errorType, errorMessage))
	}

	// 错误类型属于响应元数据，不是模型名称；把它伪装成模型会覆盖请求中的真实模型。
	return &ParseResult{
		TokenUsage:  nil,
		ModelName:   "",
		ErrorInfo:   &ErrorInfo{Type: errorType, Message: errorMessage},
		RawError:    jsonData,
		IsCompleted: true,
		Status:      StatusErrorAPI,
	}
}

// parseErrorEvent 解析SSE错误事件并将其记录为API错误
func (tp *TokenParser) parseErrorEvent() {
	defer func() {
		tp.eventBuffer.Reset()
		tp.collectingData = false
		tp.currentEvent = ""
	}()

	jsonData := tp.eventBuffer.String()
	if jsonData == "" {
		return
	}

	// 解析错误JSON数据
	var errorData SSEErrorData
	if err := json.Unmarshal([]byte(jsonData), &errorData); err != nil {
		if tp.requestID != "" {
			slog.Info(fmt.Sprintf("⚠️ [SSE错误解析] [%s] 无法解析错误数据: %s", tp.requestID, jsonData))
		}
		return
	}

	// 提取错误类型和消息
	errorType := errorData.Error.Type
	errorMessage := errorData.Error.Message
	if errorType == "" {
		errorType = "unknown_error"
	}
	if errorMessage == "" {
		errorMessage = "Unknown error"
	}

	// 记录API错误
	if tp.requestID != "" {
		slog.Info(fmt.Sprintf("❌ [API错误] [%s] 错误类型: %s, 错误信息: %s",
			tp.requestID, errorType, errorMessage))
	} else {
		slog.Info(fmt.Sprintf("❌ [API错误] 错误类型: %s, 错误信息: %s",
			errorType, errorMessage))
	}

	// ℹ️ 返回错误信息，由Handler通过生命周期管理器记录
	// 不再直接调用usageTracker，遵循架构原则
	// TokenParser只负责解析和返回结果，不直接记录到数据库
	if tp.requestID != "" {
		slog.Info(fmt.Sprintf("🏁 [API错误解析] [%s] 错误信息已解析: %s - %s", tp.requestID, errorType, errorMessage))
	}

	// 更新内部状态，由上层通过生命周期管理器记录完成状态
	tp.finalUsage = &tracking.TokenUsage{
		InputTokens:         0,
		OutputTokens:        0,
		CacheCreationTokens: 0,
		CacheReadTokens:     0,
	}
	tp.modelName = fmt.Sprintf("error:%s", errorType)
	// TokenParser不需要维护status字段，由上层处理
}
