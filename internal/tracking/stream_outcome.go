package tracking

import "strings"

// stream_outcome 请求终态的统一枚举，Claude 与 Codex 共用。
//
// 与 status 的区别：status 描述生命周期（completed/failed/cancelled…），流式请求哪怕
// 缺了结束事件也可能以 completed 收尾（failure_reason=incomplete_stream）；
// stream_outcome 专门回答"客户端是否拿到了完整结果"，是成功率的口径来源。
const (
	StreamOutcomeCompleted    = "completed"     // 流式请求收到结束事件（Claude message_stop / Codex response.completed）
	StreamOutcomeIncomplete   = "incomplete"    // 流式请求上游返回 2xx 但缺结束事件或中途截断
	StreamOutcomeFailed       = "failed"        // 请求最终失败（含非流式）
	StreamOutcomeCanceled     = "canceled"      // 客户端取消（含非流式）
	StreamOutcomeNotStreaming = "not_streaming" // 非流式请求且最终成功
)

// ResolveStreamOutcome 由请求终态推导 stream_outcome。
//
// 失败与取消不区分流式与否：它们对成功率的贡献一样都是"未成功"，而区分它们本身就是这一列的意义。
// 流式成功以 failure_reason 区分完整与否：Claude 的流完整性检测只通过 failure_reason 表达
// （incomplete_stream / stream_truncated），这里不再重复判定。
func ResolveStreamOutcome(status string, isStreaming bool, failureReason string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "canceled":
		return StreamOutcomeCanceled
	case "completed":
		if !isStreaming {
			return StreamOutcomeNotStreaming
		}
		if isIncompleteStreamReason(failureReason) {
			return StreamOutcomeIncomplete
		}
		return StreamOutcomeCompleted
	case "":
		return ""
	default:
		// failed / partial_failed / error / timeout / *_error 等一律视为失败
		return StreamOutcomeFailed
	}
}

func isIncompleteStreamReason(failureReason string) bool {
	switch strings.ToLower(strings.TrimSpace(failureReason)) {
	case "incomplete_stream", "stream_truncated":
		return true
	default:
		return false
	}
}

// successRequestSQLCondition 成功请求的判定条件。
//
// 新口径：stream_outcome='completed'，或非流式且拿到上游 2xx；
// 历史行 stream_outcome 为 NULL，回退到旧口径 status IN ('completed','processing')，
// 保证迁移前后同一时间段的成功率可比。
//
// 注意：流式但 incomplete 的请求在旧口径下算成功、新口径下不算。这是有意为之——
// 客户端拿到的是半截回答。
const successRequestSQLCondition = `(CASE
	WHEN stream_outcome IS NULL THEN status IN ('completed', 'processing')
	WHEN stream_outcome = 'completed' THEN 1
	WHEN stream_outcome = 'not_streaming' THEN (COALESCE(http_status_is_upstream, 0) = 0 OR http_status_code IS NULL OR (http_status_code >= 200 AND http_status_code < 300))
	ELSE 0
END)`

// SuccessRequestSQLCondition 暴露给需要复用同一成功口径的查询
func SuccessRequestSQLCondition() string {
	return successRequestSQLCondition
}

// isSuccessfulOutcome 热池内存聚合用的成功判定，与 successRequestSQLCondition 一致。
// 活跃请求尚未落终态时 outcome 为空，回退到按 status 判定（processing 视为成功中）。
func isSuccessfulOutcome(outcome, status string, httpStatus int, httpStatusIsUpstream bool) bool {
	switch outcome {
	case "":
		return status == "completed" || status == "processing"
	case StreamOutcomeCompleted:
		return true
	case StreamOutcomeNotStreaming:
		return !httpStatusIsUpstream || httpStatus <= 0 || (httpStatus >= 200 && httpStatus < 300)
	default:
		return false
	}
}
