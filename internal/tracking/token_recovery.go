package tracking

import (
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

// RecordFailedRequestTokens 记录失败请求的Token使用
// 只记录Token统计，不影响请求状态
//
// 🔧 [P1.4 防御性增强] 标注 token_recorded_by_failed_path 防止后续 CompleteAndArchive 覆盖：
//   - 设置 ActiveRequest.TokenRecordedByFailedPath = true
//   - CompleteAndArchive 检测此标记后，仅在新传入 tokens 非零时才覆盖
//   - 这样可避免"先记录失败 token → 后 complete 时被零值覆盖"的数据丢失
func (ut *UsageTracker) RecordFailedRequestTokens(requestID, modelName string, tokens *TokenUsage, duration time.Duration, failureReason string) {
	if ut.config == nil || !ut.config.Enabled || tokens == nil {
		return
	}

	// 🔥 热池模式：直接更新内存中的请求数据
	// 这样后续的 CompleteAndArchive 会将正确的 token 信息写入数据库
	if ut.hotPoolEnabled && ut.hotPool != nil {
		err := ut.hotPool.Update(requestID, func(req *ActiveRequest) {
			// 更新 Token 信息
			applyTokenUsageToActiveRequest(req, tokens)
			// 更新模型名（如果有）
			if modelName != "" && modelName != "unknown" {
				req.ModelName = modelName
			}
			// 更新持续时间
			if duration > 0 {
				req.DurationMs = duration.Milliseconds()
			}
			// 记录失败原因（不改变状态）
			if failureReason != "" && req.FailureReason == "" {
				req.FailureReason = failureReason
			}
			// 🔧 [P1.4] 标注此请求的 token 已由失败路径记录，保护后续不被零值覆盖
			req.TokenRecordedByFailedPath = true
		})
		if err == nil {
			slog.Debug(fmt.Sprintf("🔥 [热池Token更新] [%s] 原因: %s, 模型: %s, 输入: %d, 输出: %d",
				requestID, failureReason, modelName, tokens.InputTokens, tokens.OutputTokens))
			return
		}
		// 请求不在热池中（可能已归档），降级到传统模式
		slog.Debug(fmt.Sprintf("🔥 [热池Token更新失败] [%s] 降级到事件队列模式, 错误: %v",
			requestID, err))
	}

	// 传统模式：发送事件到队列（UPDATE 已存在的数据库记录）
	event := RequestEvent{
		Type:      "failed_request_tokens",
		RequestID: requestID,
		Timestamp: ut.now(),
		Data: RequestCompleteData{
			ModelName:                modelName,
			InputTokens:              tokens.InputTokens,
			OutputTokens:             tokens.OutputTokens,
			ReasoningTokens:          tokens.ReasoningTokens,
			BillableOutputTokens:     ResolveBillableOutputTokens(tokens),
			BillingOutputTokenSource: tokens.BillingOutputTokenSource,
			CacheCreationTokens:      tokens.CacheCreationTokens,
			CacheCreation5mTokens:    tokens.CacheCreation5mTokens,
			CacheCreation1hTokens:    tokens.CacheCreation1hTokens,
			CacheReadTokens:          tokens.CacheReadTokens,
			Duration:                 duration,
			FailureReason:            failureReason,
			InputIncludesCacheRead:   tokens.InputIncludesCacheRead,
		},
	}

	select {
	case ut.eventChan <- event:
		slog.Debug(fmt.Sprintf("💾 [失败Token事件] [%s] 原因: %s, 模型: %s", requestID, failureReason, modelName))
	default:
		slog.Warn("Usage tracking event buffer full, dropping failed request tokens event",
			"request_id", requestID)
	}
}

// RecordPartialFailure 记录部分失败(流截断)的计费数据
// 🚀 [Plan D实现] 2025-12-26: 双轨制 - 记录已消耗Token + 保持请求活跃以便重试
// 此方法会创建独立的数据库记录，但不从热池移除请求
func (ut *UsageTracker) RecordPartialFailure(requestID, modelName string, tokens *TokenUsage, duration time.Duration, failureReason, endpointName string) {
	if ut.config == nil || !ut.config.Enabled {
		return
	}

	// 🔥 热池模式：从热池获取请求信息，创建独立的部分失败记录
	if ut.hotPoolEnabled && ut.hotPool != nil && ut.archiveManager != nil {
		activeReq, exists := ut.hotPool.Get(requestID)
		if exists {
			// 创建部分失败记录的副本（不影响热池中的原请求）
			partialReq := &ActiveRequest{
				RequestID:                requestID + "-partial-" + ut.now().Format("20060102150405.000"),
				ClientIP:                 activeReq.ClientIP,
				UserAgent:                activeReq.UserAgent,
				Method:                   activeReq.Method,
				Path:                     activeReq.Path,
				StartTime:                activeReq.StartTime,
				EndpointName:             endpointName,
				Channel:                  activeReq.Channel,
				GroupName:                activeReq.GroupName,
				ModelName:                modelName,
				IsStreaming:              activeReq.IsStreaming,
				Status:                   "partial_failed", // 特殊状态标记
				HTTPStatus:               activeReq.HTTPStatus,
				HTTPStatusIsUpstream:     activeReq.HTTPStatusIsUpstream,
				RetryCount:               activeReq.RetryCount,
				FailureReason:            failureReason,
				InputTokens:              0,
				OutputTokens:             0,
				ReasoningTokens:          0,
				BillableOutputTokens:     0,
				BillingOutputTokenSource: activeReq.BillingOutputTokenSource,
				CacheCreationTokens:      0,
				CacheCreation5mTokens:    0,
				CacheCreation1hTokens:    0,
				CacheReadTokens:          0,
				InputIncludesCacheRead:   false,
				DurationMs:               duration.Milliseconds(),
			}

			// 设置Token信息（如果提供）
			if tokens != nil {
				applyTokenUsageToActiveRequest(partialReq, tokens)
			}

			// 设置结束时间
			now := ut.now()
			partialReq.EndTime = &now

			// 直接归档（写入数据库），不从热池移除原请求
			ut.archiveManager.Archive(partialReq)

			slog.Info(fmt.Sprintf("📊 [部分失败记录] [%s] 端点: %s, Token: %d+%d, 原因: %s",
				requestID, endpointName, partialReq.InputTokens, partialReq.OutputTokens, failureReason))
			return
		}
		// 请求不在热池中，记录警告
		slog.Warn(fmt.Sprintf("🔥 [部分失败记录失败] [%s] 请求不在热池中", requestID))
	}

	// 传统模式或热池不可用：记录日志
	slog.Info(fmt.Sprintf("📊 [部分失败(传统模式)] [%s] 端点: %s, 模型: %s, 原因: %s",
		requestID, endpointName, modelName, failureReason))
}

// RecoverRequestTokens 恢复请求的Token使用统计
// 🔧 [Fallback修复] 专用于debug文件恢复场景，仅更新Token字段，不触发状态变更
func (ut *UsageTracker) RecoverRequestTokens(requestID, modelName string, tokens *TokenUsage) {
	if ut.config == nil || !ut.config.Enabled || tokens == nil {
		return
	}

	// 🔥 热池模式：如果请求还在热池中，先更新热池
	if ut.hotPoolEnabled && ut.hotPool != nil {
		err := ut.hotPool.Update(requestID, func(req *ActiveRequest) {
			// 更新 Token 信息
			applyTokenUsageToActiveRequest(req, tokens)
			// 更新模型名（如果有）
			if modelName != "" && modelName != "unknown" {
				req.ModelName = modelName
			}
		})
		if err == nil {
			slog.Info(fmt.Sprintf("🔥 [热池Token恢复] [%s] 模型: %s, 输入: %d, 输出: %d",
				requestID, modelName, tokens.InputTokens, tokens.OutputTokens))
			return
		}
		// 请求不在热池中（已归档），继续使用传统模式更新数据库
		slog.Debug(fmt.Sprintf("🔥 [热池Token恢复失败] [%s] 请求已归档，使用UPDATE更新数据库",
			requestID))
	}

	// 传统模式：发送事件到队列（UPDATE 已存在的数据库记录）
	event := RequestEvent{
		Type:      "token_recovery",
		RequestID: requestID,
		Timestamp: ut.now(),
		Data: RequestCompleteData{
			ModelName:                modelName,
			InputTokens:              tokens.InputTokens,
			OutputTokens:             tokens.OutputTokens,
			ReasoningTokens:          tokens.ReasoningTokens,
			BillableOutputTokens:     ResolveBillableOutputTokens(tokens),
			BillingOutputTokenSource: tokens.BillingOutputTokenSource,
			CacheCreationTokens:      tokens.CacheCreationTokens,
			CacheCreation5mTokens:    tokens.CacheCreation5mTokens,
			CacheCreation1hTokens:    tokens.CacheCreation1hTokens,
			CacheReadTokens:          tokens.CacheReadTokens,
			Duration:                 0, // 不更新时间相关字段
			InputIncludesCacheRead:   tokens.InputIncludesCacheRead,
		},
	}

	select {
	case ut.eventChan <- event:
		slog.Info(fmt.Sprintf("🔧 [Token恢复事件] [%s] 模型: %s, 输入: %d, 输出: %d",
			requestID, modelName, tokens.InputTokens, tokens.OutputTokens))
	default:
		slog.Warn("Usage tracking event buffer full, dropping token recovery event",
			"request_id", requestID)
	}
}
