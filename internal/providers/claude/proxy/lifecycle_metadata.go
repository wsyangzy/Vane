package proxy

import (
	"fmt"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking"
)

// SetEndpoint 设置端点信息
// 🎯 [SRP重构] 更新 stateManager 并同步热池
// 立即更新热池中的端点和渠道信息，确保前端实时显示
func (rlm *RequestLifecycleManager) SetEndpoint(endpointName, groupName, channel string) {
	if rlm.stateManager.IsCompleted() {
		rlm.logger.Debug(fmt.Sprintf("⏭️ [端点忽略] [%s] 请求已终态，忽略端点更新: %s",
			rlm.stateManager.GetRequestID(), endpointName))
		return
	}

	// 更新 stateManager
	chain := rlm.stateManager.AppendEndpointChain(channel, groupName, endpointName)
	rlm.stateManager.SetEndpoint(endpointName, groupName, channel)

	// 立即更新热池中的端点和渠道信息
	if rlm.usageTracker != nil && rlm.stateManager.GetRequestID() != "" {
		updateOpts := tracking.UpdateOptions{
			EndpointName: &endpointName,
			GroupName:    &groupName,
			Channel:      &channel,
		}
		if chain != "" {
			updateOpts.EndpointChain = &chain
		}
		rlm.usageTracker.RecordRequestUpdate(rlm.stateManager.GetRequestID(), updateOpts)

		// 把端点倍率快照写入热池，供详情页展示计费过程
		rlm.snapshotEndpointMultiplier(endpointName, groupName, channel)
	}
}

// snapshotEndpointMultiplier 从 UsageTracker 端点倍率缓存中读取当前端点的倍率
// 并写入到 ActiveRequest 的快照字段。这样在归档/查询时可以展示该请求实际使用的倍率。
func (rlm *RequestLifecycleManager) snapshotEndpointMultiplier(endpointName, groupName, channel string) {
	if rlm.usageTracker == nil || endpointName == "" {
		return
	}

	mult := rlm.usageTracker.GetEndpointMultiplierForEndpointWithGroup("claude", channel, groupName, endpointName)

	rlm.usageTracker.UpdateActiveRequestMultiplier(rlm.stateManager.GetRequestID(), mult)
}

func (rlm *RequestLifecycleManager) snapshotEndpointMultiplierForModel(modelName string) {
	if rlm.usageTracker == nil || modelName == "" {
		return
	}
	endpointName := rlm.stateManager.GetEndpointName()
	if endpointName == "" {
		return
	}
	channel := rlm.stateManager.GetChannel()
	groupName := rlm.stateManager.GetGroupName()
	mult := rlm.usageTracker.GetEndpointMultiplierForEndpointAndModelWithGroup("claude", channel, groupName, endpointName, modelName)
	rlm.usageTracker.UpdateActiveRequestMultiplier(rlm.stateManager.GetRequestID(), mult)
}

// SetModel 设置模型名称（线程安全）
// 简单版本，只在模型为空或unknown时设置
func (rlm *RequestLifecycleManager) SetModel(modelName string) {
	rlm.stateMu.Lock()
	updated := false

	// 只在当前模型为空或unknown时设置，避免覆盖更准确的模型信息
	if rlm.modelName == "" || rlm.modelName == "unknown" {
		rlm.modelName = modelName
		updated = true
		rlm.logger.Debug(fmt.Sprintf("🏷️ [模型提取] [%s] 从请求中获取模型名称: %s", rlm.stateManager.GetRequestID(), modelName))
	}
	rlm.stateMu.Unlock()

	if updated {
		rlm.snapshotEndpointMultiplierForModel(modelName)
	}
}

// SetModelWithComparison 设置模型名称并进行对比检查（线程安全）
// 如果已有模型，会进行对比并在不一致时输出警告，最终以新模型为准
func (rlm *RequestLifecycleManager) SetModelWithComparison(newModelName, source string) {
	rlm.stateMu.Lock()
	updated := false

	// 如果新模型为空或unknown，不进行设置
	if newModelName == "" || newModelName == "unknown" {
		rlm.stateMu.Unlock()
		return
	}

	// 如果当前没有模型或为unknown，直接设置
	if rlm.modelName == "" || rlm.modelName == "unknown" {
		rlm.modelName = newModelName
		updated = true
		rlm.logger.Debug(fmt.Sprintf("🏷️ [模型提取] [%s] 从%s设置模型名称: %s", rlm.stateManager.GetRequestID(), source, newModelName))
	} else if rlm.modelName != newModelName {
		rlm.logger.Warn(fmt.Sprintf("⚠️ [模型不一致] [%s] 请求体模型: %s, %s模型: %s - 以%s为准",
			rlm.stateManager.GetRequestID(), rlm.modelName, source, newModelName, source))

		// 以新模型（通常是message_start解析的）为准
		rlm.modelName = newModelName
		updated = true
	} else {
		rlm.logger.Debug(fmt.Sprintf("✅ [模型一致] [%s] 请求体与%s模型一致: %s", rlm.stateManager.GetRequestID(), source, newModelName))
	}
	rlm.stateMu.Unlock()

	if updated {
		rlm.snapshotEndpointMultiplierForModel(newModelName)
	}
}

// SetModelRewrite 设置模型改写元信息，并将成本模型切换到改写后的目标模型。
func (rlm *RequestLifecycleManager) SetModelRewrite(originalModel, newModel, source string) {
	originalModel = strings.TrimSpace(originalModel)
	newModel = strings.TrimSpace(newModel)
	source = strings.TrimSpace(source)
	if newModel == "" || source == "" {
		return
	}

	rlm.SetModelWithComparison(newModel, source)
	if rlm.usageTracker == nil || rlm.stateManager.GetRequestID() == "" {
		return
	}

	opts := tracking.UpdateOptions{
		ModelName:          &newModel,
		ModelRewriteSource: &source,
		ModelRewriteTarget: &newModel,
	}
	if originalModel != "" {
		opts.OriginalModelName = &originalModel
	}
	rlm.usageTracker.RecordRequestUpdate(rlm.stateManager.GetRequestID(), opts)
}

// SetModelName 设置模型名称（兼容性方法，内部调用SetModel）
// 用于在流处理中动态设置正确的模型信息
func (rlm *RequestLifecycleManager) SetModelName(modelName string) {
	rlm.SetModel(modelName)
}

// SetRouteSnapshot 记录请求级路由快照（命中的规则或改道来源，以及当前所处阶段）。
// 快照只含规则标识与阶段序号，不含请求内容。
func (rlm *RequestLifecycleManager) SetRouteSnapshot(snapshotJSON string) {
	snapshotJSON = strings.TrimSpace(snapshotJSON)
	if snapshotJSON == "" || rlm.usageTracker == nil || rlm.stateManager.GetRequestID() == "" {
		return
	}
	rlm.usageTracker.RecordRequestUpdate(rlm.stateManager.GetRequestID(), tracking.UpdateOptions{
		RouteSnapshot: &snapshotJSON,
	})
}

// SetReasoningConfig 将 Claude 的推理配置写入请求追踪
// reasoningJSON 形如 {"type":"adaptive","effort":"high"}：
// type 是扩展思考的开关模式，effort 才是推理强度，两者可各自缺省
func (rlm *RequestLifecycleManager) SetReasoningConfig(reasoningJSON string) {
	if rlm.usageTracker == nil || reasoningJSON == "" {
		return
	}
	requestID := rlm.stateManager.GetRequestID()
	if requestID == "" {
		return
	}
	rlm.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{
		ReasoningEffort: &reasoningJSON,
	})
}

// SetServiceTier 将 Claude service_tier 写入请求追踪
func (rlm *RequestLifecycleManager) SetServiceTier(serviceTier string) {
	if rlm.usageTracker == nil || serviceTier == "" {
		return
	}
	requestID := rlm.stateManager.GetRequestID()
	if requestID == "" {
		return
	}
	rlm.usageTracker.RecordRequestUpdate(requestID, tracking.UpdateOptions{
		ServiceTier: &serviceTier,
	})
}

// GetModelName 获取当前模型名称（线程安全）
func (rlm *RequestLifecycleManager) GetModelName() string {
	rlm.stateMu.RLock()
	defer rlm.stateMu.RUnlock()
	return rlm.modelName
}

// HasModel 检查是否已有有效的模型名称（线程安全）
func (rlm *RequestLifecycleManager) HasModel() bool {
	rlm.stateMu.RLock()
	defer rlm.stateMu.RUnlock()
	return rlm.modelName != "" && rlm.modelName != "unknown"
}

// GetRequestID 获取请求ID
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetRequestID() string {
	return rlm.stateManager.GetRequestID()
}

// GetEndpointName 获取端点名称
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetEndpointName() string {
	return rlm.stateManager.GetEndpointName()
}

// GetGroupName 获取组名称
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetGroupName() string {
	return rlm.stateManager.GetGroupName()
}

// GetChannel 获取渠道标签
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetChannel() string {
	return rlm.stateManager.GetChannel()
}

// GetDuration 获取请求持续时间
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetDuration() time.Duration {
	return rlm.stateManager.GetDuration()
}

// GetLastStatus 获取最后状态
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetLastStatus() string {
	return rlm.stateManager.GetStatus()
}

// GetRetryCount 获取重试次数
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) GetRetryCount() int {
	return rlm.stateManager.GetRetryCount()
}

// IsCompleted 检查请求是否已完成
// 🎯 [SRP重构] 委托给 stateManager
func (rlm *RequestLifecycleManager) IsCompleted() bool {
	return rlm.stateManager.IsCompleted()
}

// GetStats 获取生命周期统计信息
// 🎯 [SRP重构] 委托给 stateManager 并添加错误信息
func (rlm *RequestLifecycleManager) GetStats() map[string]any {
	// 从 stateManager 获取基础统计信息
	stats := rlm.stateManager.GetStats()

	// 如果有错误信息，包含在统计中
	lastError := rlm.stateManager.GetLastError()
	if lastError != nil {
		stats["last_error"] = lastError.Error()

		// 使用错误恢复管理器分析错误类型
		errorCtx := rlm.errorRecovery.ClassifyError(lastError, rlm.stateManager.GetRequestID(),
			rlm.stateManager.GetEndpointName(), rlm.stateManager.GetGroupName(), rlm.stateManager.GetRetryCount())
		stats["error_type"] = rlm.errorRecovery.getErrorTypeName(errorCtx.ErrorType)
		stats["retryable"] = rlm.errorRecovery.ShouldRetry(errorCtx)
	}

	return stats
}
