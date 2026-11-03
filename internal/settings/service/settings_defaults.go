package service

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/wsyangzy/Vane/internal/routing"

	settingsstore "github.com/wsyangzy/Vane/internal/settings/store"
)

// InitDefaults 初始化默认设置
func (s *SettingsService) InitDefaults(ctx context.Context) error {
	defaults := s.GetAllDefaults()
	// Seed the unified preference once from the former model routing default.
	// SyncMetadata preserves an existing unified value and explicit rule overrides.
	ruleFallback, err := s.store.Get(ctx, CategoryRetry, "rule_fallback_to_default")
	if err != nil {
		return err
	}
	if ruleFallback == nil {
		legacy, err := s.store.Get(ctx, CategoryRetry, "model_rule_fallback")
		if err != nil {
			return err
		}
		if legacy != nil && (legacy.Value == "true" || legacy.Value == "false") {
			for _, record := range defaults {
				if record.Category == CategoryRetry && record.Key == "rule_fallback_to_default" {
					record.Value = legacy.Value
				}
			}
		}
	}
	// A new dev override starts with the existing port preference. Metadata
	// synchronization must not replace an upgraded user's choice with 8087.
	devPort, err := s.store.Get(ctx, CategoryServer, "preferred_port_dev")
	if err != nil {
		return err
	}
	if devPort == nil {
		basePort, err := s.store.Get(ctx, CategoryServer, "preferred_port")
		if err != nil {
			return err
		}
		if basePort != nil {
			for _, record := range defaults {
				if record.Category == CategoryServer && record.Key == "preferred_port_dev" {
					record.Value = basePort.Value
				}
			}
		}
	}

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

	// Logging 设置（SQLite）
	defaults = append(defaults, s.getDefaultsForCategory(CategoryLogging)...)

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
			{Category: CategoryServer, Key: "preferred_port", Value: "8087", ValueType: ValueTypeInt, Label: "首选端口", Description: "Vane 代理服务监听的端口。", DisplayOrder: 1, RequiresRestart: true},
			{Category: CategoryServer, Key: "preferred_port_dev", Value: "8087", ValueType: ValueTypeInt, Label: "首选端口", Description: "开发环境代理服务监听的端口。", DisplayOrder: 1, RequiresRestart: true},
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
		cooldown, rules := routing.DefaultCooldownPolicy(), routing.DefaultRuleDefaults()
		return []*settingsstore.SettingRecord{
			{Category: CategoryRetry, Key: "max_total_attempts", Value: "10", ValueType: ValueTypeInt, Label: "总尝试次数上限", Description: "单次请求向上游发送的总次数，含首次发送。", DisplayOrder: 1},
			{Category: CategoryRetry, Key: "max_total_duration", Value: "60s", ValueType: ValueTypeDuration, Label: "调度等待时限", Description: "单次请求可用于尝试与等待恢复的总时长。", DisplayOrder: 2},
			{Category: CategoryRetry, Key: "wait_for_recovery", Value: "true", ValueType: ValueTypeBool, Label: "端点冷却时等待重试", Description: "所有候选暂时不可用时，等待冷却结束再重试。", DisplayOrder: 3},
			{Category: CategoryRetry, Key: "cooldown_initial", Value: strconv.FormatFloat(cooldown.Initial.Seconds(), 'f', -1, 64) + "s", ValueType: ValueTypeDuration, Label: "首次冷却时间", Description: "临时故障后，资源首次恢复尝试前的等待时间。", DisplayOrder: 4},
			{Category: CategoryRetry, Key: "cooldown_multiplier", Value: strconv.FormatFloat(cooldown.Multiplier, 'f', -1, 64), ValueType: ValueTypeFloat, Label: "连续失败递增倍数", Description: "连续失败时，冷却时间每次递增的倍数。", DisplayOrder: 5},
			{Category: CategoryRetry, Key: "cooldown_max", Value: strconv.FormatFloat(cooldown.Maximum.Seconds(), 'f', -1, 64) + "s", ValueType: ValueTypeDuration, Label: "最长冷却时间", Description: "连续失败时，本地冷却时间的上限。", DisplayOrder: 6},
			{Category: CategoryRetry, Key: "rule_fallback_to_default", Value: strconv.FormatBool(rules.FallbackToDefault), ValueType: ValueTypeBool, Label: "规则目标失败后回到默认编排", Description: "规则目标无法完成请求时，使用当前方案的默认编排继续尝试。", DisplayOrder: 7},
		}

	case CategoryHealth:
		return []*settingsstore.SettingRecord{
			{Category: CategoryHealth, Key: "active_probes_enabled", Value: "true", ValueType: ValueTypeBool, Label: "主动健康检查", Description: "定期探测默认方案中可检测端点的连通性。", DisplayOrder: 0},
			{Category: CategoryHealth, Key: "check_interval", Value: "30s", ValueType: ValueTypeDuration, Label: "检查间隔", Description: "两次定时健康探测之间的间隔。", DisplayOrder: 1},
			{Category: CategoryHealth, Key: "timeout", Value: "5s", ValueType: ValueTypeDuration, Label: "检查超时", Description: "单次健康探测的最长等待时间。", DisplayOrder: 2},
			{Category: CategoryHealth, Key: "health_path", Value: "/v1/models", ValueType: ValueTypeString, Label: "检查路径", Description: "健康探测访问的 API 路径。", DisplayOrder: 3},
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
			{Category: CategoryRequest, Key: "global_timeout", Value: "300s", ValueType: ValueTypeDuration, Label: "非流式默认超时", Description: "非流式请求从发送到完成的默认时限。", DisplayOrder: 1},
			{Category: CategoryRequest, Key: "response_header_timeout", Value: "300s", ValueType: ValueTypeDuration, Label: "流式响应头超时", Description: "流式请求等待上游首次响应头的最长时间。", DisplayOrder: 2},
			{Category: CategoryRequest, Key: "default_response_header_timeout", Value: "60s", ValueType: ValueTypeDuration, Label: "非流式响应头超时", Description: "非流式请求等待上游首次响应头的最长时间。", DisplayOrder: 3},
			{Category: CategoryRequest, Key: "suspend_wait_timeout", Value: "120s", ValueType: ValueTypeDuration, Label: "挂起等待超时", Description: "普通挂起等待端点恢复/渠道切换的最大时间", DisplayOrder: 3},
		}

	case CategoryStreaming:
		return []*settingsstore.SettingRecord{
			{Category: CategoryStreaming, Key: "heartbeat_interval", Value: "30s", ValueType: ValueTypeDuration, Label: "心跳间隔", Description: "流式连接心跳间隔", DisplayOrder: 1},
			{Category: CategoryStreaming, Key: "read_timeout", Value: "10s", ValueType: ValueTypeDuration, Label: "读取超时", Description: "流式数据读取超时", DisplayOrder: 2},
			{Category: CategoryStreaming, Key: "max_idle_time", Value: "120s", ValueType: ValueTypeDuration, Label: "最大空闲时间", Description: "流式连接最大空闲时间", DisplayOrder: 3},
		}

	case CategoryAuth:
		return []*settingsstore.SettingRecord{
			{Category: CategoryAuth, Key: "enabled", Value: "false", ValueType: ValueTypeBool, Label: "启用访问鉴权", Description: "校验接入 Vane 的客户端是否持有应用访问令牌。", DisplayOrder: 1},
			{Category: CategoryAuth, Key: "token", Value: "", ValueType: ValueTypePassword, Label: "应用访问令牌", Description: "客户端接入 Vane 时使用的身份凭证。", DisplayOrder: 2},
		}

	case CategoryTokenCounting:
		return []*settingsstore.SettingRecord{
			{Category: CategoryTokenCounting, Key: "enabled", Value: "false", ValueType: ValueTypeBool, Label: "转发 Claude Token 预估", Description: "将 Claude 的 Token 预估请求交由上游处理。", DisplayOrder: 1},
			{Category: CategoryTokenCounting, Key: "estimation_ratio", Value: "4.0", ValueType: ValueTypeFloat, Label: "本地估算比例", Description: "本地预估时，每个 Token 对应的平均字符数。", DisplayOrder: 2},
		}

	case CategoryRetention:
		return []*settingsstore.SettingRecord{
			{Category: CategoryRetention, Key: "retention_days", Value: "0", ValueType: ValueTypeInt, Label: "数据保留天数", Description: "请求历史的保留时长，0 表示永久保留。", DisplayOrder: 1},
		}

	case CategoryCache:
		return []*settingsstore.SettingRecord{
			{Category: CategoryCache, Key: "enabled", Value: "true", ValueType: ValueTypeBool, Label: "启用缓存", Description: "复用统计查询结果，减少重复读取数据库。", DisplayOrder: 1},
			{Category: CategoryCache, Key: "fallback_ttl", Value: "5m", ValueType: ValueTypeDuration, Label: "兜底刷新时间", Description: "统计缓存的最长有效时间。", DisplayOrder: 2},
			{Category: CategoryCache, Key: "max_entries", Value: "1000", ValueType: ValueTypeInt, Label: "最大条目数", Description: "每类缓存最多保留的条目数。", DisplayOrder: 3},
			{Category: CategoryCache, Key: "stale_on_error", Value: "true", ValueType: ValueTypeBool, Label: "错误时使用旧值", Description: "数据库读取失败时，使用已有缓存维持统计展示。", DisplayOrder: 4},
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
			{Category: CategoryLogging, Key: "level", Value: "info", ValueType: ValueTypeEnum, Label: "日志级别", Description: "记录日志的最低严重级别。", EnumOptions: []settingsstore.EnumOption{
				{Value: "debug", Label: "调试 (Debug)"},
				{Value: "info", Label: "信息 (Info)"},
				{Value: "warn", Label: "警告 (Warn)"},
				{Value: "error", Label: "错误 (Error)"},
			}, DisplayOrder: 1},
			{Category: CategoryLogging, Key: "format", Value: "text", ValueType: ValueTypeEnum, Label: "日志格式", Description: "日志内容的输出格式。", EnumOptions: []settingsstore.EnumOption{
				{Value: "text", Label: "文本格式"},
				{Value: "json", Label: "JSON 格式"},
			}, DisplayOrder: 2},
			{Category: CategoryLogging, Key: "file_enabled", Value: "true", ValueType: ValueTypeBool, Label: "启用文件日志", Description: "将运行日志保存到本机文件。", DisplayOrder: 3},
			{Category: CategoryLogging, Key: "max_file_size", Value: "100MB", ValueType: ValueTypeString, Label: "单文件轮转大小", Description: "单个日志文件触发归档的大小。", DisplayOrder: 4},
			{Category: CategoryLogging, Key: "max_files", Value: "10", ValueType: ValueTypeInt, Label: "归档保留数量", Description: "每类日志保留的归档文件数量。", DisplayOrder: 5},
			{Category: CategoryLogging, Key: "compress_rotated", Value: "true", ValueType: ValueTypeBool, Label: "压缩归档日志", Description: "压缩归档日志，减少磁盘占用。", DisplayOrder: 6},
		}

	default:
		return nil
	}
}
