package proxy

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/wsyangzy/Vane/internal/tracking"
)

// GetModelName 获取模型名称
func (tp *TokenParser) GetModelName() string {
	return tp.modelName
}

// StreamCompleteness 流完整性状态
// 🆕 [流完整性追踪] 2025-12-11
type StreamCompleteness struct {
	IsComplete    bool   // 是否完整
	Reason        string // 不完整的原因（用于日志）
	FailureReason string // 数据库 failure_reason 值
}

// StreamIncompleteError 流不完整错误（结构化错误类型）
// 🆕 [流完整性追踪] 2025-12-11
// 用于替代字符串解析，提供类型安全的错误传递
type StreamIncompleteError struct {
	FailureReason string // 数据库 failure_reason 值（incomplete_stream 或 stream_truncated）
	ModelName     string // 模型名称
	Reason        string // 不完整的原因（用于日志）
}

// Error 实现 error 接口
func (e *StreamIncompleteError) Error() string {
	return fmt.Sprintf("stream incomplete: %s (model: %s)", e.Reason, e.ModelName)
}

// GetFailureReason 获取 failure_reason（实现 StreamIncompleteErrorInterface）
func (e *StreamIncompleteError) GetFailureReason() string {
	return e.FailureReason
}

// GetModelName 获取模型名称（实现 StreamIncompleteErrorInterface）
func (e *StreamIncompleteError) GetModelName() string {
	return e.ModelName
}

// GetReason 获取不完整的原因（实现 StreamIncompleteErrorInterface）
func (e *StreamIncompleteError) GetReason() string {
	return e.Reason
}

// IsStreamIncompleteError 检查错误是否为 StreamIncompleteError
func IsStreamIncompleteError(err error) (*StreamIncompleteError, bool) {
	var streamErr *StreamIncompleteError
	if errors.As(err, &streamErr) {
		return streamErr, true
	}
	return nil, false
}

// GetStreamCompleteness 获取流完整性状态
// 🆕 [流完整性追踪] 2025-12-11
// 根据接收到的 SSE 事件判断流是否完整：
// - 完整流：收到 message_start + message_delta(usage) + message_stop
// - 不完整流：缺少 message_start、message_stop 或 message_delta(usage)
func (tp *TokenParser) GetStreamCompleteness() StreamCompleteness {
	// 🔧 [边界条件修复] 2025-12-11
	// 首先检查是否收到 message_start，这是所有有效响应的起点
	if !tp.hasMessageStart {
		return StreamCompleteness{
			IsComplete:    false,
			Reason:        "未收到 message_start 事件",
			FailureReason: "stream_truncated",
		}
	}

	// 检查是否收到必要的结束事件
	if !tp.hasMessageStop {
		// 没有 message_stop，判断是缺少结束事件还是内容截断
		if tp.hasMessageDeltaUsage {
			// 有 usage 但没有 stop，可能是轻微不完整
			return StreamCompleteness{
				IsComplete:    false,
				Reason:        "缺少 message_stop 事件",
				FailureReason: "incomplete_stream",
			}
		}
		// 只有 start，响应被截断
		return StreamCompleteness{
			IsComplete:    false,
			Reason:        "响应被截断，缺少 message_delta 和 message_stop",
			FailureReason: "stream_truncated",
		}
	}

	// 检查是否有完整的 usage 信息
	if !tp.hasMessageDeltaUsage && tp.IsFallbackUsed() {
		return StreamCompleteness{
			IsComplete:    false,
			Reason:        "使用了 message_start fallback，缺少最终 usage",
			FailureReason: "incomplete_stream",
		}
	}

	// 流完整
	return StreamCompleteness{
		IsComplete:    true,
		Reason:        "",
		FailureReason: "",
	}
}

// IsStreamComplete 简单判断流是否完整
// 🆕 [流完整性追踪] 2025-12-11
func (tp *TokenParser) IsStreamComplete() bool {
	return tp.GetStreamCompleteness().IsComplete
}

// GetPartialUsage 获取部分Token使用统计（用于网络中断恢复）
func (tp *TokenParser) GetPartialUsage() *tracking.TokenUsage {
	if tp.partialUsage != nil {
		return tp.partialUsage
	}
	return tp.finalUsage
}

// FlushPendingEvent 强制解析缓存中的待处理事件
// 在流结束或连接中断时调用，确保不会因为缺少终止空行而丢失 usage 信息
func (tp *TokenParser) FlushPendingEvent() *ParseResult {
	// 只有在收集数据且缓存非空时才需要flush
	if !tp.collectingData || tp.eventBuffer.Len() == 0 {
		return nil
	}

	// 根据当前事件类型调用相应的解析方法
	switch tp.currentEvent {
	case "message_delta", "content_block_delta":
		if tp.requestID != "" {
			slog.Info(fmt.Sprintf("🔄 [事件Flush] [%s] 强制解析缓存的增量usage事件(%s)", tp.requestID, tp.currentEvent))
		}
		return tp.extractMessageDelta()
	case "message_start":
		if tp.requestID != "" {
			slog.Info(fmt.Sprintf("🔄 [事件Flush] [%s] 强制解析缓存的message_start事件", tp.requestID))
		}
		tp.parseMessageStart()
		return nil
	case "error":
		if tp.requestID != "" {
			slog.Info(fmt.Sprintf("🔄 [事件Flush] [%s] 强制解析缓存的error事件", tp.requestID))
		}
		return tp.extractErrorEvent()
	default:
		if tp.requestID != "" {
			slog.Info(fmt.Sprintf("⚠️ [事件Flush] [%s] 未知事件类型: %s, 跳过解析", tp.requestID, tp.currentEvent))
		}
		return nil
	}
}
