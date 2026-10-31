package storage

import (
	"context"
	"fmt"

	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"

	_ "modernc.org/sqlite"
)

// migrateSchema 执行数据库迁移（添加 5m/1h 缓存字段）
func (s *SQLiteAdapter) migrateSchema(ctx context.Context) error {
	if err := s.ensureSettingsProviderScope(ctx); err != nil {
		return err
	}

	// request_logs 迁移：历史上先上线 usage tracking，后续补充缓存字段
	requestLogMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "endpoint_chain",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN endpoint_chain TEXT",
			description: "端点链路字段",
		},
		{
			checkColumn: "cache_creation_5m_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_5m_tokens INTEGER DEFAULT 0",
			description: "5分钟缓存创建tokens字段",
		},
		{
			checkColumn: "cache_creation_1h_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_1h_tokens INTEGER DEFAULT 0",
			description: "1小时缓存创建tokens字段",
		},
		{
			checkColumn: "cache_creation_5m_cost_usd",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_5m_cost_usd REAL DEFAULT 0",
			description: "5分钟缓存创建成本字段",
		},
		{
			checkColumn: "cache_creation_1h_cost_usd",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_1h_cost_usd REAL DEFAULT 0",
			description: "1小时缓存创建成本字段",
		},
		// 🔧 [P5.9 新增] output_sent_at 字段，用于崩溃恢复时区分"已计费"场景
		{
			checkColumn: "output_sent_at",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN output_sent_at DATETIME",
			description: "已发送内容时间戳（P5.9）",
		},
		{
			checkColumn: "provider",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude'",
			description: "提供商标识字段（claude/codex）",
		},
		{
			checkColumn: "environment",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN environment TEXT NOT NULL DEFAULT 'release'",
			description: "运行环境字段（dev/release）",
		},
		{
			checkColumn: "privacy_redacted",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN privacy_redacted BOOLEAN DEFAULT 0",
			description: "隐私脱敏命中标记字段",
		},
		{
			checkColumn: "privacy_redaction_summary",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN privacy_redaction_summary TEXT",
			description: "隐私脱敏摘要字段",
		},
		// 端点倍率快照字段（用于详情页计费过程展示）
		{
			checkColumn: "cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点总成本倍率快照",
		},
		{
			checkColumn: "input_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN input_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点输入成本倍率快照",
		},
		{
			checkColumn: "output_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN output_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点输出成本倍率快照",
		},
		{
			checkColumn: "cache_creation_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点 5m 缓存创建成本倍率快照",
		},
		{
			checkColumn: "cache_creation_cost_multiplier_1h",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_creation_cost_multiplier_1h REAL DEFAULT 1.0",
			description: "请求时端点 1h 缓存创建成本倍率快照",
		},
		{
			checkColumn: "cache_read_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN cache_read_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时端点缓存读取成本倍率快照",
		},
		{
			checkColumn: "model_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加总倍率快照",
		},
		{
			checkColumn: "model_input_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_input_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加输入倍率快照",
		},
		{
			checkColumn: "model_output_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_output_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加输出倍率快照",
		},
		{
			checkColumn: "model_cache_read_cost_multiplier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_cache_read_cost_multiplier REAL DEFAULT 1.0",
			description: "请求时命中模型附加缓存读取倍率快照",
		},
		{
			checkColumn: "reasoning_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN reasoning_tokens INTEGER DEFAULT 0",
			description: "输出推理 tokens 字段",
		},
		{
			checkColumn: "billable_output_tokens",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN billable_output_tokens INTEGER DEFAULT 0",
			description: "输出计费 tokens 字段",
		},
		{
			checkColumn: "billing_output_token_source",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens'",
			description: "输出计费口径字段",
		},
		{
			checkColumn: "billing_rule_snapshot",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN billing_rule_snapshot TEXT",
			description: "组合计费规则命中快照",
		},
		{
			checkColumn: "original_model_name",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN original_model_name TEXT",
			description: "模型改写前原始模型字段",
		},
		{
			checkColumn: "model_rewrite_source",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_rewrite_source TEXT",
			description: "模型改写来源字段",
		},
		{
			checkColumn: "model_rewrite_target",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_rewrite_target TEXT",
			description: "模型改写目标模型字段",
		},
		{
			checkColumn: "model_reasoning_effort",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN model_reasoning_effort TEXT",
			description: "推理强度字段 (Codex reasoning.effort)",
		},
		{
			checkColumn: "service_tier",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN service_tier TEXT",
			description: "服务等级字段 (Codex fast mode)",
		},
		{
			checkColumn: "http_status_is_upstream",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN http_status_is_upstream BOOLEAN NOT NULL DEFAULT 0",
			description: "HTTP状态码来源标记（真实上游响应）",
		},
		{
			checkColumn: "route_snapshot",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN route_snapshot TEXT",
			description: "请求级路由快照字段",
		},
		{
			// 不回填历史行：旧行没有流完整性判定的原始信号，成功率 SQL 对 NULL 回退到旧口径
			checkColumn: "stream_outcome",
			alterSQL:    "ALTER TABLE request_logs ADD COLUMN stream_outcome TEXT",
			description: "请求终态枚举字段",
		},
	}

	// endpoints 迁移：端点存储表迭代新增字段时，需要兼容旧 db（CREATE TABLE IF NOT EXISTS 不会补列）
	endpointsMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "credential_group_id",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN credential_group_id INTEGER REFERENCES claude_credential_groups(id) ON DELETE SET NULL",
			description: "Claude 凭证分组引用字段",
		},
		{
			checkColumn: "timeout_seconds",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN timeout_seconds INTEGER DEFAULT 300",
			description: "端点超时字段",
		},
		{
			checkColumn: "supports_count_tokens",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN supports_count_tokens INTEGER DEFAULT 0",
			description: "端点 supports_count_tokens 字段",
		},
		{
			checkColumn: "cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cost_multiplier REAL DEFAULT 1.0",
			description: "端点总成本倍率字段",
		},
		{
			checkColumn: "input_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN input_cost_multiplier REAL DEFAULT 1.0",
			description: "端点输入成本倍率字段",
		},
		{
			checkColumn: "output_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN output_cost_multiplier REAL DEFAULT 1.0",
			description: "端点输出成本倍率字段",
		},
		{
			checkColumn: "cache_creation_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cache_creation_cost_multiplier REAL DEFAULT 1.0",
			description: "端点 5m 缓存创建成本倍率字段",
		},
		{
			checkColumn: "cache_creation_cost_multiplier_1h",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cache_creation_cost_multiplier_1h REAL DEFAULT 1.0",
			description: "端点 1h 缓存创建成本倍率字段",
		},
		{
			checkColumn: "cache_read_cost_multiplier",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN cache_read_cost_multiplier REAL DEFAULT 1.0",
			description: "端点缓存读取成本倍率字段",
		},
		{
			checkColumn: "skip_health_check",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN skip_health_check INTEGER DEFAULT 0",
			description: "端点跳过健康检查字段 (v6.1)",
		},
		{
			checkColumn: "model_multipliers",
			alterSQL:    "ALTER TABLE endpoints ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'",
			description: "端点模型附加倍率字段",
		},
	}

	// channels 迁移：早期可能只有 name，后续新增 website
	codexEndpointMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "skip_health_check",
			alterSQL:    "ALTER TABLE codex_endpoints ADD COLUMN skip_health_check INTEGER NOT NULL DEFAULT 0",
			description: "Codex 端点跳过健康检查字段",
		},
		{
			checkColumn: "timeout_seconds",
			alterSQL:    "ALTER TABLE codex_endpoints ADD COLUMN timeout_seconds INTEGER NOT NULL DEFAULT 0",
			description: "Codex 端点请求超时字段",
		},
		{
			checkColumn: "model_multipliers",
			alterSQL:    "ALTER TABLE codex_endpoints ADD COLUMN model_multipliers TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 端点模型附加倍率字段",
		},
	}
	codexChannelMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{"priority", "ALTER TABLE codex_channels ADD COLUMN priority INTEGER NOT NULL DEFAULT 0", "Codex 渠道优先级字段"},
		{"failover_enabled", "ALTER TABLE codex_channels ADD COLUMN failover_enabled INTEGER NOT NULL DEFAULT 1", "Codex 渠道故障转移参与字段"},
		{"non_stream_route", "ALTER TABLE codex_channels ADD COLUMN non_stream_route TEXT", "Codex 渠道非流改道字段"},
		{"privacy_level", "ALTER TABLE codex_channels ADD COLUMN privacy_level TEXT", "Codex 渠道隐私级别字段"},
	}
	modelRouteMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		// 旧行补 0：它们原本是全局规则，改为渠道级后不再参与匹配，需用户在渠道下重建
		{"source_channel_id", "ALTER TABLE model_routes ADD COLUMN source_channel_id INTEGER NOT NULL DEFAULT 0", "模型路由规则来源渠道字段"},
	}

	channelMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "website",
			alterSQL:    "ALTER TABLE channels ADD COLUMN website TEXT",
			description: "渠道官网字段",
		},
		{
			checkColumn: "priority",
			alterSQL:    "ALTER TABLE channels ADD COLUMN priority INTEGER DEFAULT 1",
			description: "渠道优先级字段",
		},
		{
			checkColumn: "failover_enabled",
			alterSQL:    "ALTER TABLE channels ADD COLUMN failover_enabled INTEGER DEFAULT 1",
			description: "渠道故障转移开关字段",
		},
		{
			checkColumn: "model_mapping",
			alterSQL:    "ALTER TABLE channels ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'",
			description: "渠道模型映射字段",
		},
		{
			checkColumn: "non_stream_route",
			alterSQL:    "ALTER TABLE channels ADD COLUMN non_stream_route TEXT",
			description: "渠道非流改道字段",
		},
		{
			checkColumn: "privacy_level",
			alterSQL:    "ALTER TABLE channels ADD COLUMN privacy_level TEXT",
			description: "渠道隐私级别字段",
		},
	}

	runMigrations := func(table string, migrations []struct {
		checkColumn string
		alterSQL    string
		description string
	}) error {
		tableExists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}
		if !tableExists {
			return nil
		}

		for _, m := range migrations {
			exists, err := s.columnExists(ctx, table, m.checkColumn)
			if err != nil {
				return fmt.Errorf("failed to check column %s.%s: %w", table, m.checkColumn, err)
			}
			if exists {
				continue
			}

			s.logger.Info(fmt.Sprintf("🔧 [数据库迁移] %s：添加 %s", table, m.description))
			if _, err := s.GetWriteDB().ExecContext(ctx, m.alterSQL); err != nil {
				return fmt.Errorf("failed to add column %s.%s: %w", table, m.checkColumn, err)
			}
			s.logger.Info(fmt.Sprintf("✅ [数据库迁移] %s：%s 添加成功", table, m.description))
		}
		return nil
	}

	if err := runMigrations("request_logs", requestLogMigrations); err != nil {
		return err
	}
	if err := runMigrations("model_routes", modelRouteMigrations); err != nil {
		return err
	}
	// 历史行的 http_status_code 可能是旧版本 Relay 自造的兜底码，但读取侧已按
	// http_status_is_upstream 隐藏；不做不可逆的批量清空，真实上游码也在其中。
	if err := s.purgePrivacyPlaintextMatches(ctx); err != nil {
		return err
	}
	if err := runMigrations("endpoints", endpointsMigrations); err != nil {
		return err
	}
	if err := s.ensureClaudeCredentialGroupsSchema(ctx); err != nil {
		return err
	}
	if err := s.backfillClaudeCredentialGroups(ctx); err != nil {
		return err
	}
	if err := s.repairClaudeCredentialGroupOwnership(ctx); err != nil {
		return err
	}
	if err := s.repairCrossProviderMultiplierCost(ctx); err != nil {
		return err
	}
	if err := s.migrateLongContextPricingDefault(ctx); err != nil {
		return err
	}
	if err := s.migrateClaudeLongContextRemoval(ctx); err != nil {
		return err
	}
	if err := runMigrations("codex_credential_groups", []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{checkColumn: "enabled", alterSQL: "ALTER TABLE codex_credential_groups ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1", description: "账号全局启停"},
		{
			checkColumn: "daily_cost_limit_usd",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN daily_cost_limit_usd REAL NOT NULL DEFAULT 0",
			description: "Codex 凭证分组每日成本限额字段",
		},
		{
			checkColumn: "model_policy",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN model_policy TEXT NOT NULL DEFAULT 'inherit'",
			description: "Codex 凭证分组模型策略字段",
		},
		{
			checkColumn: "model_mapping",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'",
			description: "Codex 凭证分组模型映射字段",
		},
		{
			checkColumn: "supported_models",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN supported_models TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 凭证分组支持模型字段",
		},
		{
			checkColumn: "disable_service_tier_pricing",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1",
			description: "Codex 凭证分组服务等级计费开关",
		},
		{
			checkColumn: "service_tier_multipliers",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN service_tier_multipliers TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 凭证分组四档服务等级倍率",
		},
		{
			checkColumn: "billing_rules",
			alterSQL:    "ALTER TABLE codex_credential_groups ADD COLUMN billing_rules TEXT NOT NULL DEFAULT '[]'",
			description: "Codex 凭证分组组合计费规则",
		},
	}); err != nil {
		return err
	}
	if err := codexstore.EnsureCodexSubscriptionSchema(ctx, s.GetWriteDB()); err != nil {
		return err
	}
	if err := runMigrations("codex_endpoints", codexEndpointMigrations); err != nil {
		return err
	}
	if err := runMigrations("codex_channels", codexChannelMigrations); err != nil {
		return err
	}
	if err := s.normalizeCodexChannelPriorities(ctx); err != nil {
		return err
	}
	// 允许不同凭证分组端点同名（约束从 name/channel 级调整为 credential_group 级）。
	// SQLite 无法直接移除旧 UNIQUE 约束，需要在发现旧约束时重建表。
	if err := s.ensureEndpointsUniqueByCredentialGroup(ctx); err != nil {
		return err
	}
	if err := runMigrations("channels", channelMigrations); err != nil {
		return err
	}

	// usage_summary 迁移
	usageSummaryMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "provider",
			alterSQL:    "ALTER TABLE usage_summary ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude'",
			description: "使用统计提供商标识字段（claude/codex）",
		},
		{
			checkColumn: "environment",
			alterSQL:    "ALTER TABLE usage_summary ADD COLUMN environment TEXT NOT NULL DEFAULT 'release'",
			description: "使用统计运行环境字段（dev/release）",
		},
	}
	if err := runMigrations("usage_summary", usageSummaryMigrations); err != nil {
		return err
	}
	if err := s.ensureUsageSummaryEnvironmentScope(ctx); err != nil {
		return err
	}

	// model_pricing 迁移
	modelPricingMigrations := []struct {
		checkColumn string
		alterSQL    string
		description string
	}{
		{
			checkColumn: "provider",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude'",
			description: "模型定价提供商标识字段（claude/codex）",
		},
		{
			checkColumn: "long_context_threshold",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_threshold INTEGER NOT NULL DEFAULT 0",
			description: "Long context 原始输入 token 阈值",
		},
		{
			checkColumn: "long_context_input_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_input_price REAL NOT NULL DEFAULT 0",
			description: "Long context 输入价格",
		},
		{
			checkColumn: "long_context_cache_read_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_cache_read_price REAL NOT NULL DEFAULT 0",
			description: "Long context 缓存读取价格",
		},
		{
			checkColumn: "long_context_cache_write_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_cache_write_price REAL NOT NULL DEFAULT 0",
			description: "Long context 缓存写入价格",
		},
		{
			checkColumn: "long_context_output_price",
			alterSQL:    "ALTER TABLE model_pricing ADD COLUMN long_context_output_price REAL NOT NULL DEFAULT 0",
			description: "Long context 输出价格",
		},
	}
	if err := runMigrations("model_pricing", modelPricingMigrations); err != nil {
		return err
	}
	if err := s.rebuildModelPricingForProviderTierUnique(ctx); err != nil {
		return err
	}
	if err := s.ensureCodexCacheWritePrices(ctx); err != nil {
		return err
	}

	return nil
}

// purgePrivacyPlaintextMatches 清除历史遗留的 privacy_redaction_matches 列。
//
// 该列曾存放隐私命中的原文与替换值——隐私防护功能反而把客户明文集中落进了
// 本地库。停止写入不足以止血，已存在的明文必须主动清除：
// 先 UPDATE 置空（即便后续 DROP 不被支持，明文也已不可读），再尝试 DROP COLUMN
// （SQLite 3.35+ 才支持，失败不阻断启动）。
func (s *SQLiteAdapter) purgePrivacyPlaintextMatches(ctx context.Context) error {
	exists, err := s.columnExists(ctx, "request_logs", "privacy_redaction_matches")
	if err != nil {
		return fmt.Errorf("failed to check column request_logs.privacy_redaction_matches: %w", err)
	}
	if !exists {
		return nil
	}

	s.logger.Info("🔒 [隐私止血] 清除 request_logs.privacy_redaction_matches 中的历史明文")
	if _, err := s.GetWriteDB().ExecContext(ctx,
		`UPDATE request_logs SET privacy_redaction_matches = NULL WHERE privacy_redaction_matches IS NOT NULL`); err != nil {
		return fmt.Errorf("failed to clear privacy_redaction_matches: %w", err)
	}
	if _, err := s.GetWriteDB().ExecContext(ctx,
		`ALTER TABLE request_logs DROP COLUMN privacy_redaction_matches`); err != nil {
		// 旧版 SQLite 不支持 DROP COLUMN。内容已置空，留列不影响隐私。
		s.logger.Warn("隐私明文列内容已清空，但当前 SQLite 不支持删除该列", "error", err)
		return nil
	}
	s.logger.Info("✅ [隐私止血] privacy_redaction_matches 列已删除")
	return nil
}
