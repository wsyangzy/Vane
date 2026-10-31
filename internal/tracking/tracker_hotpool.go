package tracking

import (
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"
)

// initHotPool 初始化热池和归档管理器
func (ut *UsageTracker) initHotPool() {
	// 检查是否启用热池模式（默认启用）
	hotPoolEnabled := true
	var hotPoolConfig HotPoolConfig

	if ut.config.HotPool != nil {
		// 使用调用方提供的热池设置
		if !ut.config.HotPool.Enabled {
			hotPoolEnabled = false
		}
		hotPoolConfig = HotPoolConfig{
			MaxAge:           ut.config.HotPool.MaxAge,
			MaxSize:          ut.config.HotPool.MaxSize,
			CleanupInterval:  ut.config.HotPool.CleanupInterval,
			ArchiveOnCleanup: ut.config.HotPool.ArchiveOnCleanup,
		}
	} else {
		// 使用默认配置
		hotPoolConfig = DefaultHotPoolConfig()
	}

	if !hotPoolEnabled {
		slog.Info("🔥 热池模式已禁用，使用传统事件队列模式")
		ut.hotPoolEnabled = false
		return
	}

	// 创建热池
	ut.hotPool = NewHotPool(hotPoolConfig)

	// 创建归档管理器
	archiveConfig := ArchiveManagerConfig{
		ChannelSize:   ut.config.BufferSize,
		BatchSize:     ut.config.BatchSize,
		FlushInterval: ut.config.FlushInterval,
		MaxRetry:      ut.config.MaxRetry,
		Environment:   ut.environment,
	}
	ut.archiveManager = NewArchiveManager(ut.adapter, archiveConfig, ut.pricing, ut.location)
	ut.archiveManager.SetWriteLocker(&ut.writeMu)
	ut.archiveManager.SetArchiveSuccessCallback(ut.notifyUsageDataChanged)

	// 🔧 [P6 新增] 把内存计数器注入 ArchiveManager，归档成功后增量维护 total / by-provider
	if ut.dbCounter != nil {
		ut.archiveManager.SetDBCounter(ut.dbCounter)
	}

	// 设置双向引用：ArchiveManager 需要访问 HotPool 来清理归档缓存
	ut.archiveManager.SetHotPool(ut.hotPool)
	if err := ut.archiveManager.ReplayFailedArchives(); err != nil {
		slog.Warn("📦 启动时回放归档失败记录失败（保留原文件供下次重试）", "error", err)
	}

	// 设置热池归档回调
	ut.hotPool.SetArchiveCallback(func(req *ActiveRequest) {
		if ut.archiveManager != nil {
			if err := ut.archiveManager.Archive(req); err != nil {
				slog.Warn("📦 归档事件发送失败",
					"request_id", req.RequestID,
					"error", err)
			}
		}
	})

	ut.hotPoolEnabled = true
	slog.Info("🔥 热池模式已启用",
		"max_age", hotPoolConfig.MaxAge,
		"max_size", hotPoolConfig.MaxSize)

	// 启动归档超时清理goroutine
	ut.hotPool.StartArchivingCleanup()
}

// GetActiveRequests 获取热池中的活跃请求
// 返回当前正在处理中的请求列表（内存中）
func (ut *UsageTracker) GetActiveRequests() []*ActiveRequest {
	if ut.hotPool == nil {
		return nil
	}
	return ut.hotPool.GetActive()
}

// GetActiveRequestCount 获取活跃请求数量
func (ut *UsageTracker) GetActiveRequestCount() int {
	if ut.hotPool == nil {
		return 0
	}
	return ut.hotPool.GetActiveCount()
}

// GetHotPoolStats 获取热池统计信息
func (ut *UsageTracker) GetHotPoolStats() *HotPoolStats {
	if ut.hotPool == nil {
		return nil
	}
	stats := ut.hotPool.GetStats()
	return &stats
}

// GetArchiveStats 获取归档管理器统计信息
func (ut *UsageTracker) GetArchiveStats() *ArchiveStats {
	if ut.archiveManager == nil {
		return nil
	}
	stats := ut.archiveManager.GetStats()
	return &stats
}

// IsHotPoolEnabled 检查热池模式是否启用
func (ut *UsageTracker) IsHotPoolEnabled() bool {
	return ut.hotPoolEnabled
}

// GetActiveRequest 获取单个活跃请求
func (ut *UsageTracker) GetActiveRequest(requestID string) (*ActiveRequest, bool) {
	if ut.hotPool == nil {
		return nil, false
	}
	return ut.hotPool.Get(requestID)
}

// IsRequestActive 检查请求是否在热池中
func (ut *UsageTracker) IsRequestActive(requestID string) bool {
	if ut.hotPool == nil {
		return false
	}
	return ut.hotPool.Exists(requestID)
}

// UpdateActiveRequestTokens 更新活跃请求的Token（流式请求累积）
func (ut *UsageTracker) UpdateActiveRequestTokens(requestID string, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) error {
	if ut.hotPool == nil {
		return fmt.Errorf("hot pool not enabled")
	}

	return ut.hotPool.Update(requestID, func(req *ActiveRequest) {
		req.InputTokens = inputTokens
		req.OutputTokens = outputTokens
		req.CacheCreationTokens = cacheCreationTokens
		req.CacheReadTokens = cacheReadTokens
	})
}

// UpdateActiveRequestMultiplier 将端点倍率快照写入热池中的活跃请求
// 由生命周期管理器在 SetEndpoint 时调用，确保归档/查询时能展示请求实际使用的倍率
// 倍率值为 0 或负数时回退到 1.0（不影响成本）
func (ut *UsageTracker) UpdateActiveRequestMultiplier(requestID string, mult EndpointMultiplier) error {
	if ut.hotPool == nil {
		// 热池未启用时静默返回，不视为错误（向后兼容传统模式）
		return nil
	}
	if requestID == "" {
		return nil
	}

	return ut.hotPool.Update(requestID, func(req *ActiveRequest) {
		req.BillingRules = append([]BillingRule(nil), mult.BillingRules...)
		req.BillingRulesCaptured = true
		req.ServiceTierSource = NormalizeServiceTierSource(mult.ServiceTierSource)
		if req.Provider == "codex" {
			req.ServiceTierSource = billingServiceTierSource(mult)
		}
		req.CacheWriteTTLPricing = mult.CacheWriteTTLPricing
		resolveActiveRequestServiceTier(req)
		req.BillingRuleSnapshot = ""
		req.BillingGroupMultiplier = nil
		if len(mult.BillingRules) > 0 {
			groupSnapshot := DefaultEndpointMultiplier()
			copyBillingGroupMultipliers(&groupSnapshot, mult)
			req.BillingGroupMultiplier = &groupSnapshot
			billingOutputTokenSource := mult.BillingOutputTokenSource
			mult = DefaultEndpointMultiplier()
			mult.BillingOutputTokenSource = billingOutputTokenSource
		}
		req.CostMultiplier = ensurePositiveMultiplier(mult.CostMultiplier)
		req.InputCostMultiplier = ensurePositiveMultiplier(mult.InputCostMultiplier)
		req.OutputCostMultiplier = ensurePositiveMultiplier(mult.OutputCostMultiplier)
		req.CacheCreationCostMultiplier = ensurePositiveMultiplier(mult.CacheCreationCostMultiplier)
		req.CacheCreationCostMultiplier1h = ensurePositiveMultiplier(mult.CacheCreationCostMultiplier1h)
		req.CacheReadCostMultiplier = ensurePositiveMultiplier(mult.CacheReadCostMultiplier)
		req.ModelCostMultiplier = ensurePositiveMultiplier(mult.ModelCostMultiplier)
		req.ModelInputCostMultiplier = ensurePositiveMultiplier(mult.ModelInputCostMultiplier)
		req.ModelOutputCostMultiplier = ensurePositiveMultiplier(mult.ModelOutputCostMultiplier)
		req.ModelCacheReadCostMultiplier = ensurePositiveMultiplier(mult.ModelCacheReadCostMultiplier)
		req.BillingOutputTokenSource = NormalizeBillingOutputTokenSource(mult.BillingOutputTokenSource)
		req.BillableOutputTokens = BillableOutputTokens(req.OutputTokens, req.ReasoningTokens, req.BillingOutputTokenSource)
	})
}

// ensurePositiveMultiplier 倍率值 <= 0 时回退到 1.0（不影响成本）
func ensurePositiveMultiplier(v float64) float64 {
	if v <= 0 {
		return 1.0
	}
	return v
}

// GetHotPoolAndArchiveOverview 获取热池和归档概览（用于监控）
func (ut *UsageTracker) GetHotPoolAndArchiveOverview() map[string]interface{} {
	overview := map[string]interface{}{
		"hot_pool_enabled": ut.hotPoolEnabled,
	}

	if ut.hotPool != nil {
		stats := ut.hotPool.GetStats()
		overview["hot_pool"] = map[string]interface{}{
			"current_size":   stats.CurrentSize,
			"peak_size":      stats.PeakSize,
			"total_added":    stats.TotalAdded,
			"total_removed":  stats.TotalRemoved,
			"total_archived": stats.TotalArchived,
			"total_expired":  stats.TotalExpired,
			"total_overflow": stats.TotalOverflow,
		}
	}

	if ut.archiveManager != nil {
		stats := ut.archiveManager.GetStats()
		overview["archive_manager"] = map[string]interface{}{
			"total_received":     stats.TotalReceived,
			"total_archived":     stats.TotalArchived,
			"total_failed":       stats.TotalFailed,
			"total_dropped":      stats.TotalDropped,
			"total_batches":      stats.TotalBatches,
			"avg_batch_size":     stats.AvgBatchSize,
			"channel_length":     stats.ChannelLength,
			"last_flush_time":    stats.LastFlushTime,
			"last_flush_latency": stats.LastFlushLatency.String(),
		}
	}

	return overview
}

// ============================================================================
// 🔥 双源查询方法（热池 + 数据库）
// ============================================================================
