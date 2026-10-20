package service

import (
	"context"
	"log/slog"

	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

// InitDefaults 初始化默认设置
func (s *SettingsService) InitDefaults(ctx context.Context) error {
	defaults := s.GetAllDefaults()

	// 清理废弃的配置项
	if err := s.cleanupDeprecatedSettings(ctx); err != nil {
		slog.Warn("清理废弃配置失败", "error", err)
		// 不阻止初始化流程
	}

	// 始终同步元数据（label、description、value_type等）
	// 这样即使数据库中已有数据，也能更新到最新的元数据
	// 但会保留用户设置的 value 值
	return s.store.SyncMetadata(ctx, defaults)
}

// cleanupDeprecatedSettings 清理废弃的配置项
func (s *SettingsService) cleanupDeprecatedSettings(ctx context.Context) error {
	deprecatedKeys := []struct {
		category string
		key      string
	}{
		// 移除旧的请求挂起配置
		{CategoryRequest, "suspend_enabled"},
		{CategoryRequest, "suspend_timeout"},
		{CategoryRequest, "max_suspended"},
		// 移除旧的故障转移配置
		{CategoryFailover, "allow_revisit_after_exhausted"},
		// 移除旧的恢复策略配置（已简化为 loop_retry）
		{CategoryRequest, "recovery_strategy"},
		{CategoryRequest, "auto_wait_timeout"},
		{CategoryRequest, "auto_wait_max_requests"},
		// loop_retry 和 max_loops 从 request 移到 failover
		{CategoryRequest, "loop_retry"},
		{CategoryRequest, "max_loops"},
		// 移除 failover 的 default_cooldown（loop_retry 模式下与循环重试逻辑冲突）
		{CategoryFailover, "default_cooldown"},
		// interrupt_client_on_exhausted 从 failover 移到 request，并更名为 allow_retry_on_exhausted
		{CategoryFailover, "interrupt_client_on_exhausted"},
		{CategoryRequest, "block_retry_on_exhausted"},
		// allow_retry_on_exhausted 已废弃，始终保持连接挂起
		{CategoryRequest, "allow_retry_on_exhausted"},
		// 移除端口自动切换设置（已改为弹窗让用户选择）
		{CategoryServer, "allow_port_fallback"},
		// 移除旧的 codex_server 分类（Codex 路由已合并到主代理端口）
		{"codex_server", "enabled"},
		{"codex_server", "host"},
		{"codex_server", "port"},
		{"codex_server", "timeout"},
		// 移除已废弃的 server 分类下的 codex 设置项（Codex 代理默认启用）
		{CategoryServer, "codex_enabled"},
		{CategoryServer, "codex_host"},
		{CategoryServer, "codex_port"},
		{CategoryServer, "codex_timeout"},
		// 请求历史固定每日维护并按自然月归档，不再暴露技术参数。
		{CategoryRetention, "cleanup_interval"},
		{CategoryRetention, "max_active_database_size_mb"},
		// 响应头超时从隐藏的 streaming 分类移到可见的 request 分类；旧行连同其 60s 旧值一起丢弃，
		// 新行按 300s 默认值种入（60s 正是导致 Codex 长上下文请求被断流重放的值）。
		{CategoryStreaming, "response_header_timeout"},
		// 统计缓存改为写入时按范围失效，分项 TTL 合并为单一兜底时长 fallback_ttl。
		{CategoryCache, "usage_stats_ttl"},
		{CategoryCache, "usage_summary_ttl"},
		{CategoryCache, "token_usage_ttl"},
		{CategoryCache, "endpoint_cost_today_ttl"},
		{CategoryCache, "endpoint_cost_history_ttl"},
	}

	for _, dep := range deprecatedKeys {
		// 先检查是否存在，避免每次启动都输出无意义的日志
		existing, _ := s.store.Get(ctx, dep.category, dep.key)
		if existing == nil {
			continue
		}
		if err := s.store.Delete(ctx, dep.category, dep.key); err != nil {
			slog.Debug("删除废弃配置失败", "category", dep.category, "key", dep.key, "error", err)
		} else {
			slog.Info("✅ [SettingsService] 已删除废弃配置", "category", dep.category, "key", dep.key)
		}
	}

	return nil
}

// IsInitialized 检查是否已初始化
func (s *SettingsService) IsInitialized(ctx context.Context) (bool, error) {
	return s.store.IsInitialized(ctx)
}

// GetAllDefaults 获取所有默认设置
func (s *SettingsService) GetAllDefaults() []*settingsstore.SettingRecord {
	var defaults []*settingsstore.SettingRecord

	// Server 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryServer)...)

	// Strategy 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryStrategy)...)

	// Retry 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryRetry)...)

	// Health 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryHealth)...)

	// Provider 专属 Failover 设置
	for _, provider := range []SettingsProvider{ProviderClaude, ProviderCodex} {
		for _, record := range s.getDefaultsForCategory(CategoryFailover) {
			copy := *record
			copy.Provider = string(provider)
			if provider == ProviderCodex && copy.Key == "enabled" {
				copy.Value = "false"
			}
			defaults = append(defaults, &copy)
		}
	}

	// Request 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryRequest)...)

	// Streaming 默认值写入 SQLite，运行时可通过设置服务读取。
	defaults = append(defaults, s.getDefaultsForCategory(CategoryStreaming)...)

	// HotPool 由 tracking 层使用代码默认值，暂不作为用户可见设置。

	// Auth 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryAuth)...)

	// TokenCounting 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryTokenCounting)...)

	// Retention 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryRetention)...)

	// Cache 设置
	defaults = append(defaults, s.getDefaultsForCategory(CategoryCache)...)

	// Logging / Timezone 设置（SQLite）
	defaults = append(defaults, s.getDefaultsForCategory(CategoryLogging)...)
	defaults = append(defaults, s.getDefaultsForCategory(CategoryTimezone)...)

	for _, record := range defaults {
		if record.Provider == "" {
			record.Provider = string(ProviderGlobal)
		}
	}
	return defaults
}

// getDefaultsForCategory 获取分类的默认设置
func (s *SettingsService) getDefaultsForCategory(category string) []*settingsstore.SettingRecord {
	switch category {
	case CategoryServer:
		return []*settingsstore.SettingRecord{
			{Category: CategoryServer, Key: "preferred_port", Value: "8087", ValueType: ValueTypeInt, Label: "首选端口", Description: "API 服务首选端口", DisplayOrder: 1, RequiresRestart: true},
		}

	case CategoryStrategy:
		return []*settingsstore.SettingRecord{
			{Category: CategoryStrategy, Key: "type", Value: "priority", ValueType: ValueTypeString, Label: "策略类型", Description: "渠道内端点选择策略：priority（按优先级顺序）或 fastest（选择响应最快的端点）", DisplayOrder: 1},
			{Category: CategoryStrategy, Key: "fast_test_enabled", Value: "true", ValueType: ValueTypeBool, Label: "启用快速测试", Description: "定期测试端点响应速度，仅在 fastest 策略下生效", DisplayOrder: 2},
			{Category: CategoryStrategy, Key: "fast_test_cache_ttl", Value: "3s", ValueType: ValueTypeDuration, Label: "缓存时间", Description: "快速测试结果缓存时间", DisplayOrder: 3},
			{Category: CategoryStrategy, Key: "fast_test_timeout", Value: "1s", ValueType: ValueTypeDuration, Label: "测试超时", Description: "快速测试超时时间", DisplayOrder: 4},
			{Category: CategoryStrategy, Key: "fast_test_path", Value: "/v1/models", ValueType: ValueTypeString, Label: "测试路径", Description: "快速测试请求路径", DisplayOrder: 5},
		}

	case CategoryRetry:
		return []*settingsstore.SettingRecord{
			{Category: CategoryRetry, Key: "max_attempts", Value: "3", ValueType: ValueTypeInt, Label: "最大重试次数", Description: "请求失败后的最大重试次数", DisplayOrder: 1},
			{Category: CategoryRetry, Key: "base_delay", Value: "1s", ValueType: ValueTypeDuration, Label: "基础延迟", Description: "首次重试前的等待时间", DisplayOrder: 2},
			{Category: CategoryRetry, Key: "max_delay", Value: "30s", ValueType: ValueTypeDuration, Label: "最大延迟", Description: "重试延迟的上限", DisplayOrder: 3},
			{Category: CategoryRetry, Key: "multiplier", Value: "2.0", ValueType: ValueTypeFloat, Label: "延迟倍数", Description: "每次重试延迟的倍增系数", DisplayOrder: 4},
		}

	case CategoryHealth:
		return []*settingsstore.SettingRecord{
			{Category: CategoryHealth, Key: "check_interval", Value: "30s", ValueType: ValueTypeDuration, Label: "检查间隔", Description: "健康检查的时间间隔", DisplayOrder: 1},
			{Category: CategoryHealth, Key: "timeout", Value: "5s", ValueType: ValueTypeDuration, Label: "检查超时", Description: "健康检查的超时时间", DisplayOrder: 2},
			{Category: CategoryHealth, Key: "health_path", Value: "/v1/models", ValueType: ValueTypeString, Label: "检查路径", Description: "健康检查请求的 API 路径", DisplayOrder: 3},
		}

	case CategoryFailover:
		// 渠道间故障转移配置（含循环重试）
		// 注意：default_cooldown 已移除，loop_retry 模式下冷却时间与循环重试逻辑冲突
		return []*settingsstore.SettingRecord{
			{Category: CategoryFailover, Key: "enabled", Value: "true", ValueType: ValueTypeBool, Label: "渠道间故障转移", Description: "当前渠道内所有端点均重试耗尽时，自动切换到备用渠道", DisplayOrder: 1},
			{Category: CategoryFailover, Key: "loop_retry", Value: "true", ValueType: ValueTypeBool, Label: "恢复策略", Description: "所有渠道耗尽后，回到最高优先级渠道继续尝试。关闭则直接返回失败。", DisplayOrder: 2},
			{Category: CategoryFailover, Key: "max_loops", Value: "3", ValueType: ValueTypeInt, Label: "最大循环轮数", Description: "循环重试启用时，最多循环的轮数（1-10），超过后返回失败", DisplayOrder: 3},
			{Category: CategoryFailover, Key: "loop_retry_wait_timeout", Value: "120s", ValueType: ValueTypeDuration, Label: "循环终止等待", Description: "达到最大循环轮数后，等待用户操作的最大时间", DisplayOrder: 4},
			{Category: CategoryFailover, Key: "skip_health_check", Value: "false", ValueType: ValueTypeBool, Label: "跳过定时健康检测", Description: "启用后不再进行定时健康检测，端点状态仅通过实际请求更新。可减少日志噪音。", DisplayOrder: 5},
		}

	case CategoryRequest:
		// 请求控制配置
		// 移除 allow_retry_on_exhausted，始终保持连接挂起
		return []*settingsstore.SettingRecord{
			{Category: CategoryRequest, Key: "global_timeout", Value: "300s", ValueType: ValueTypeDuration, Label: "全局超时", Description: "非流式请求的默认超时时间", DisplayOrder: 1},
			{Category: CategoryRequest, Key: "response_header_timeout", Value: "300s", ValueType: ValueTypeDuration, Label: "流式响应头超时", Description: "流式请求等待服务端首次响应头的超时时间（Claude 与 Codex 共用）。长上下文 + 高推理档位首包可能超过 60s；超时后请求不会重放", DisplayOrder: 2},
			{Category: CategoryRequest, Key: "suspend_wait_timeout", Value: "120s", ValueType: ValueTypeDuration, Label: "挂起等待超时", Description: "普通挂起等待端点恢复/渠道切换的最大时间", DisplayOrder: 3},
			{Category: CategoryRequest, Key: "eof_retry_hint", Value: "false", ValueType: ValueTypeBool, Label: "EOF 重试提示", Description: "流式传输中断时发送可重试错误格式，让客户端自动重试", DisplayOrder: 4},
		}

	case CategoryStreaming:
		return []*settingsstore.SettingRecord{
			{Category: CategoryStreaming, Key: "heartbeat_interval", Value: "30s", ValueType: ValueTypeDuration, Label: "心跳间隔", Description: "流式连接心跳间隔", DisplayOrder: 1},
			{Category: CategoryStreaming, Key: "read_timeout", Value: "10s", ValueType: ValueTypeDuration, Label: "读取超时", Description: "流式数据读取超时", DisplayOrder: 2},
			{Category: CategoryStreaming, Key: "max_idle_time", Value: "120s", ValueType: ValueTypeDuration, Label: "最大空闲时间", Description: "流式连接最大空闲时间", DisplayOrder: 3},
		}

	case CategoryAuth:
		return []*settingsstore.SettingRecord{
			{Category: CategoryAuth, Key: "enabled", Value: "false", ValueType: ValueTypeBool, Label: "启用鉴权", Description: "是否启用 API 访问鉴权", DisplayOrder: 1},
			{Category: CategoryAuth, Key: "token", Value: "", ValueType: ValueTypePassword, Label: "鉴权 Token", Description: "Bearer Token 值", DisplayOrder: 2},
		}

	case CategoryTokenCounting:
		return []*settingsstore.SettingRecord{
			{Category: CategoryTokenCounting, Key: "enabled", Value: "false", ValueType: ValueTypeBool, Label: "启用 Token 计数", Description: "是否启用 count_tokens 上游转发；关闭时由 Relay 本地估算", DisplayOrder: 1},
			{Category: CategoryTokenCounting, Key: "estimation_ratio", Value: "4.0", ValueType: ValueTypeFloat, Label: "估算比例", Description: "Token 估算比例 (1 token ≈ N 字符)", DisplayOrder: 2},
		}

	case CategoryRetention:
		return []*settingsstore.SettingRecord{
			{Category: CategoryRetention, Key: "retention_days", Value: "0", ValueType: ValueTypeInt, Label: "数据保留天数", Description: "请求记录保留天数，0 表示永久保留；历史请求自动按自然月归档分库", DisplayOrder: 1},
		}

	case CategoryCache:
		return []*settingsstore.SettingRecord{
			{Category: CategoryCache, Key: "enabled", Value: "true", ValueType: ValueTypeBool, Label: "启用缓存", Description: "启用统计聚合读路径缓存，不影响请求转发和计费事实记录；端口检测、CLI 版本等运行时缓存始终启用", DisplayOrder: 1},
			{Category: CategoryCache, Key: "fallback_ttl", Value: "5m", ValueType: ValueTypeDuration, Label: "兜底刷新时间", Description: "请求记录写入后统计缓存会立即按受影响范围刷新；此值只是兜底，超过该时长无论是否有写入都重新查库", DisplayOrder: 2},
			{Category: CategoryCache, Key: "max_entries", Value: "1000", ValueType: ValueTypeInt, Label: "最大条目数", Description: "每类缓存最多保留的 key 数量", DisplayOrder: 3},
			{Category: CategoryCache, Key: "stale_on_error", Value: "true", ValueType: ValueTypeBool, Label: "错误时使用旧值", Description: "数据库瞬时错误时允许统计聚合展示旧值", DisplayOrder: 4},
		}

	case CategoryHotPool:
		return []*settingsstore.SettingRecord{
			{Category: CategoryHotPool, Key: "enabled", Value: "true", ValueType: ValueTypeBool, Label: "启用热池", Description: "是否启用内存热池模式", DisplayOrder: 1},
			{Category: CategoryHotPool, Key: "max_age", Value: "30m", ValueType: ValueTypeDuration, Label: "最大存活时间", Description: "请求在热池中的最大存活时间", DisplayOrder: 2},
			{Category: CategoryHotPool, Key: "max_size", Value: "10000", ValueType: ValueTypeInt, Label: "最大容量", Description: "热池最大请求数量", DisplayOrder: 3},
			{Category: CategoryHotPool, Key: "cleanup_interval", Value: "1m", ValueType: ValueTypeDuration, Label: "清理间隔", Description: "僵尸请求清理间隔", DisplayOrder: 4},
			{Category: CategoryHotPool, Key: "archive_on_cleanup", Value: "true", ValueType: ValueTypeBool, Label: "清理时归档", Description: "清理时是否将请求归档到数据库", DisplayOrder: 5},
		}

	// 🔧 [P5.5.2 新增] Logging 日志配置
	case CategoryLogging:
		return []*settingsstore.SettingRecord{
			{Category: CategoryLogging, Key: "level", Value: "info", ValueType: ValueTypeEnum, Label: "日志级别", Description: "日志详细程度，debug 用于排查问题", EnumOptions: []settingsstore.EnumOption{
				{Value: "debug", Label: "调试 (Debug)"},
				{Value: "info", Label: "信息 (Info)"},
				{Value: "warn", Label: "警告 (Warn)"},
				{Value: "error", Label: "错误 (Error)"},
			}, DisplayOrder: 1},
			{Category: CategoryLogging, Key: "format", Value: "text", ValueType: ValueTypeEnum, Label: "日志格式", Description: "文本格式适合阅读，JSON 格式适合日志采集系统", EnumOptions: []settingsstore.EnumOption{
				{Value: "text", Label: "文本格式"},
				{Value: "json", Label: "JSON 格式"},
			}, DisplayOrder: 2},
		}

	// 🔧 [P5.5.3 新增] Timezone 全局时区
	case CategoryTimezone:
		return []*settingsstore.SettingRecord{
			{Category: CategoryTimezone, Key: "global", Value: "Asia/Shanghai", ValueType: ValueTypeEnum, Label: "全局时区", Description: "影响所有时间戳显示（数据库、日志、UI）。修改后需重启应用生效。", EnumOptions: []settingsstore.EnumOption{
				{Value: "Asia/Shanghai", Label: "中国（北京时间 UTC+8）"},
				{Value: "Asia/Hong_Kong", Label: "香港（UTC+8）"},
				{Value: "Asia/Taipei", Label: "台北（UTC+8）"},
				{Value: "Asia/Tokyo", Label: "东京（UTC+9）"},
				{Value: "Asia/Singapore", Label: "新加坡（UTC+8）"},
				{Value: "Asia/Seoul", Label: "首尔（UTC+9）"},
				{Value: "UTC", Label: "UTC（协调世界时）"},
				{Value: "Europe/London", Label: "伦敦（UTC+0/+1）"},
				{Value: "Europe/Paris", Label: "巴黎（UTC+1/+2）"},
				{Value: "Europe/Berlin", Label: "柏林（UTC+1/+2）"},
				{Value: "Europe/Moscow", Label: "莫斯科（UTC+3）"},
				{Value: "America/New_York", Label: "纽约（UTC-5/-4）"},
				{Value: "America/Chicago", Label: "芝加哥（UTC-6/-5）"},
				{Value: "America/Denver", Label: "丹佛（UTC-7/-6）"},
				{Value: "America/Los_Angeles", Label: "洛杉矶（UTC-8/-7）"},
				{Value: "America/Sao_Paulo", Label: "圣保罗（UTC-3）"},
				{Value: "Australia/Sydney", Label: "悉尼（UTC+10/+11）"},
				{Value: "Pacific/Auckland", Label: "奥克兰（UTC+12/+13）"},
			}, DisplayOrder: 1, RequiresRestart: true},
		}

	default:
		return nil
	}
}
