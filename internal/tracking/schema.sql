-- Usage Tracking Database Schema
-- 使用跟踪系统数据库结构
-- 创建时间: 2025-09-04

-- 请求记录主表
CREATE TABLE IF NOT EXISTS request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT UNIQUE NOT NULL,        -- req-xxxxxxxx
    
    -- 请求基本信息
    client_ip TEXT,                         -- 客户端IP
    user_agent TEXT,                        -- 客户端User-Agent
    method TEXT DEFAULT 'POST',             -- HTTP方法
    path TEXT DEFAULT '/v1/messages',       -- 请求路径
    
    -- 时间信息
    start_time DATETIME NOT NULL,           -- 请求开始时间
    end_time DATETIME,                      -- 请求完成时间
    duration_ms INTEGER,                    -- 总耗时(毫秒)
    
    -- 转发信息
    channel TEXT DEFAULT '',                -- 渠道标签（来自端点配置）
    endpoint_name TEXT,                     -- 使用的端点名称
    endpoint_chain TEXT,                    -- 端点尝试链路（简略）
    group_name TEXT,                        -- 所属组名
    model_name TEXT,                        -- Claude模型名称
    original_model_name TEXT,               -- 模型改写前的原始请求模型
    model_rewrite_source TEXT,              -- 模型改写来源：mapping/channel_model
    model_rewrite_target TEXT,              -- 模型改写后的目标模型
    model_reasoning_effort TEXT,             -- 推理强度/thinking 完整 JSON
    service_tier TEXT,                       -- 服务档位: auto/default/batch/flex/priority/fast
    is_streaming BOOLEAN DEFAULT FALSE,     -- 是否为流式请求
    provider TEXT NOT NULL DEFAULT 'claude',   -- 提供商标识: claude/codex
    environment TEXT NOT NULL DEFAULT 'release', -- 运行环境: dev/release（两版共享本库，靠此列隔离追踪视图）
    privacy_redacted BOOLEAN DEFAULT 0,        -- 发送上游前是否命中隐私脱敏
    privacy_redaction_summary TEXT,            -- 隐私脱敏命中摘要（仅规则名与命中数，绝不含原文）

    -- 状态信息 (更新: 生命周期状态与错误原因分离 - 2025-09-28)
    status TEXT NOT NULL DEFAULT 'pending', -- 生命周期状态: pending/forwarding/processing/retry/suspended/completed/failed/cancelled
    http_status_code INTEGER,               -- HTTP状态码
    retry_count INTEGER DEFAULT 0,          -- 重试次数

    -- 失败信息 (状态机重构新增字段 2025-09-28)
    failure_reason TEXT,                    -- 当前失败原因类型: rate_limited/server_error/network_error/timeout/empty_response/invalid_response
    last_failure_reason TEXT,               -- 最后一次失败的详细错误信息

    -- 取消信息 (状态机重构新增字段 2025-09-28)
    cancel_reason TEXT,                     -- 取消原因 (取消时间使用end_time字段)

    -- 🔧 [P5.9 新增] 已发送内容时间戳
    -- 流式响应首次向客户端发送 token 时设置，用于崩溃恢复时区分"已计费但本地缺失"场景
    -- NULL 表示尚未发送任何内容；非 NULL 表示客户端已收到部分响应
    output_sent_at DATETIME,
    
    -- Token统计
    input_tokens INTEGER DEFAULT 0,        -- 输入token数
    output_tokens INTEGER DEFAULT 0,       -- 输出token数（上游原始 output_tokens）
    reasoning_tokens INTEGER DEFAULT 0,    -- 输出中的推理 token（OpenAI/Codex output_tokens_details.reasoning_tokens）
    billable_output_tokens INTEGER DEFAULT 0, -- 实际用于输出计费的 token 数
    billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens', -- 输出计费口径
    billing_rule_snapshot TEXT,             -- 命中的组合计费规则与最终倍率/单价 JSON
    cache_creation_tokens INTEGER DEFAULT 0, -- 缓存创建token数（总计，向后兼容）
    cache_creation_5m_tokens INTEGER DEFAULT 0, -- 5分钟缓存创建token数
    cache_creation_1h_tokens INTEGER DEFAULT 0, -- 1小时缓存创建token数
    cache_read_tokens INTEGER DEFAULT 0,   -- 缓存读取token数

    -- 成本计算（包含缓存）
    input_cost_usd REAL DEFAULT 0,         -- 输入token成本
    output_cost_usd REAL DEFAULT 0,        -- 输出token成本
    cache_creation_cost_usd REAL DEFAULT 0, -- 缓存创建成本（总计，向后兼容）
    cache_creation_5m_cost_usd REAL DEFAULT 0, -- 5分钟缓存创建成本
    cache_creation_1h_cost_usd REAL DEFAULT 0, -- 1小时缓存创建成本
    cache_read_cost_usd REAL DEFAULT 0,    -- 缓存读取成本
    total_cost_usd REAL DEFAULT 0,         -- 总成本

    -- 端点倍率快照（请求处理时端点的倍率配置，用于详情页计费过程展示）
    -- 默认 1.0 = 不影响成本
    cost_multiplier REAL DEFAULT 1.0,                       -- 总成本倍率
    input_cost_multiplier REAL DEFAULT 1.0,                 -- 输入成本倍率
    output_cost_multiplier REAL DEFAULT 1.0,                -- 输出成本倍率
    cache_creation_cost_multiplier REAL DEFAULT 1.0,        -- 5分钟缓存创建倍率
    cache_creation_cost_multiplier_1h REAL DEFAULT 1.0,     -- 1小时缓存创建倍率
    cache_read_cost_multiplier REAL DEFAULT 1.0,            -- 缓存读取倍率
    model_cost_multiplier REAL DEFAULT 1.0,                 -- 命中模型附加总倍率
    model_input_cost_multiplier REAL DEFAULT 1.0,           -- 命中模型附加输入倍率
    model_output_cost_multiplier REAL DEFAULT 1.0,          -- 命中模型附加输出倍率
    model_cache_read_cost_multiplier REAL DEFAULT 1.0,      -- 命中模型附加缓存读取倍率

    -- 审计字段（统一使用带时区格式，微秒精度）
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

-- 索引优化
CREATE INDEX IF NOT EXISTS idx_request_logs_request_id ON request_logs(request_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_start_time ON request_logs(start_time);
CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(status);
CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model_name);
CREATE INDEX IF NOT EXISTS idx_request_logs_channel ON request_logs(channel);
CREATE INDEX IF NOT EXISTS idx_request_logs_endpoint ON request_logs(endpoint_name);
CREATE INDEX IF NOT EXISTS idx_request_logs_group ON request_logs(group_name);
CREATE INDEX IF NOT EXISTS idx_request_logs_failure_reason ON request_logs(failure_reason);
-- dev/release 共享同一个 usage.db，追踪视图查询一律带 environment，故建复合索引
CREATE INDEX IF NOT EXISTS idx_request_logs_environment_start ON request_logs(environment, start_time);

-- 使用统计汇总表 (可选，用于快速查询)
CREATE TABLE IF NOT EXISTS usage_summary (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL,                    -- YYYY-MM-DD
    model_name TEXT NOT NULL,
    endpoint_name TEXT NOT NULL,
    group_name TEXT,
    provider TEXT NOT NULL DEFAULT 'claude',   -- 提供商标识: claude/codex
    environment TEXT NOT NULL DEFAULT 'release', -- 运行环境: dev/release

    request_count INTEGER DEFAULT 0,       -- 请求总数
    success_count INTEGER DEFAULT 0,       -- 成功请求数
    error_count INTEGER DEFAULT 0,         -- 失败请求数
    
    total_input_tokens INTEGER DEFAULT 0,
    total_output_tokens INTEGER DEFAULT 0,
    total_cache_creation_tokens INTEGER DEFAULT 0,
    total_cache_read_tokens INTEGER DEFAULT 0,
    total_cost_usd REAL DEFAULT 0,
    
    avg_duration_ms REAL DEFAULT 0,        -- 平均响应时间
    
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    
    UNIQUE(date, model_name, endpoint_name, group_name, provider, environment)
);

-- 汇总表索引
CREATE INDEX IF NOT EXISTS idx_usage_summary_date ON usage_summary(date);
CREATE INDEX IF NOT EXISTS idx_usage_summary_model ON usage_summary(model_name);
CREATE INDEX IF NOT EXISTS idx_usage_summary_endpoint ON usage_summary(endpoint_name);
CREATE INDEX IF NOT EXISTS idx_usage_summary_group ON usage_summary(group_name);
CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider);
CREATE INDEX IF NOT EXISTS idx_usage_summary_provider ON usage_summary(provider);
CREATE INDEX IF NOT EXISTS idx_usage_summary_environment_date ON usage_summary(environment, date);


-- 触发器：自动更新 updated_at 时间戳（统一使用带时区格式，微秒精度）
CREATE TRIGGER IF NOT EXISTS update_request_logs_timestamp
    AFTER UPDATE ON request_logs
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE request_logs SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS update_usage_summary_timestamp
    AFTER UPDATE ON usage_summary
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE usage_summary SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

-- ============================================================================
-- 隐私防护表
-- 结构由 internal/privacy.Store.EnsureSchema 单独持有，此处刻意不定义。
--
-- 原因有二：
--   1. 隐私表经历过破坏性重构（旧 privacy_rules 需整表改名保留、
--      privacy_synthetic_* 含明文列需删除）。若此处再用
--      CREATE TABLE IF NOT EXISTS 建一份旧结构，全新安装会先建旧表、
--      再被迁移逻辑改名成「旧版规则」，凭空产生一个空的历史清单。
--   2. 两处定义必然漂移，而隐私表的漂移意味着明文列可能被悄悄重建。
-- ============================================================================

-- ============================================================================
-- 端点配置表 (新增 - 2025-12-05)
-- 将端点配置从 config.yaml 迁移到 SQLite，支持动态增删改查
-- ============================================================================
CREATE TABLE IF NOT EXISTS endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- ========== 基本信息 ==========
    channel TEXT NOT NULL,                          -- 渠道标签（用于分组展示）
    credential_group_id INTEGER REFERENCES claude_credential_groups(id) ON DELETE SET NULL,
    name TEXT NOT NULL,                             -- 端点名称（同一凭证分组内唯一）
    url TEXT NOT NULL,                              -- 端点 URL

    -- ========== 认证配置 ==========
    token TEXT,                                     -- Bearer Token
    api_key TEXT,                                   -- API Key (备用)
    headers TEXT,                                   -- 自定义请求头 (JSON格式)

    -- ========== 路由配置 ==========
    priority INTEGER DEFAULT 0,                     -- 端点优先级（0 为最高，数字越小越高）
    failover_enabled INTEGER DEFAULT 1,             -- 是否参与故障转移 (1=是, 0=否)
    cooldown_seconds INTEGER,                       -- 冷却时间（秒，NULL=使用全局配置）
    timeout_seconds INTEGER DEFAULT 300,            -- 请求超时（秒）

    -- ========== 功能支持 ==========
    supports_count_tokens INTEGER DEFAULT 0,        -- 是否支持 count_tokens 端点
    skip_health_check INTEGER DEFAULT 0,            -- 跳过健康检查 (, 1=跳过, 0=正常检查)

    -- ========== 成本倍率 ==========
    cost_multiplier REAL DEFAULT 1.0,               -- 总成本倍率
    input_cost_multiplier REAL DEFAULT 1.0,         -- 输入成本倍率
    output_cost_multiplier REAL DEFAULT 1.0,        -- 输出成本倍率
    cache_creation_cost_multiplier REAL DEFAULT 1.0,-- 5分钟缓存创建成本倍率（默认）
    cache_creation_cost_multiplier_1h REAL DEFAULT 1.0, -- 1小时缓存创建成本倍率
    cache_read_cost_multiplier REAL DEFAULT 1.0,    -- 缓存读取成本倍率
    model_multipliers TEXT NOT NULL DEFAULT '[]',   -- 按模型附加倍率 JSON

    -- ========== 状态 ==========
    enabled INTEGER DEFAULT 1,                      -- 是否启用 (1=启用, 0=禁用)

    -- ========== 审计字段 ==========
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

-- 端点表索引
CREATE INDEX IF NOT EXISTS idx_endpoints_channel ON endpoints(channel);
CREATE INDEX IF NOT EXISTS idx_endpoints_credential_group ON endpoints(credential_group_id);
CREATE INDEX IF NOT EXISTS idx_endpoints_priority ON endpoints(priority);
CREATE INDEX IF NOT EXISTS idx_endpoints_enabled ON endpoints(enabled);
CREATE INDEX IF NOT EXISTS idx_endpoints_failover ON endpoints(failover_enabled);
CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_unique_group_name
    ON endpoints(channel, credential_group_id, name) WHERE credential_group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoints_unique_legacy_name
    ON endpoints(channel, name) WHERE credential_group_id IS NULL;

-- 端点表触发器：自动更新 updated_at
CREATE TRIGGER IF NOT EXISTS update_endpoints_timestamp
    AFTER UPDATE ON endpoints
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE endpoints SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

-- ============================================================================
-- 渠道表 (新增)
-- 支持“先创建渠道，再在渠道下创建端点”的管理流程
-- ============================================================================
CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    name TEXT UNIQUE NOT NULL,                      -- 渠道名称
    website TEXT,                                   -- 渠道官网（可选）
    priority INTEGER DEFAULT 1,                     -- 渠道优先级（数字越小越高，用于渠道间故障转移顺序）
    failover_enabled INTEGER DEFAULT 1,             -- 是否参与渠道间故障转移 (1=是, 0=否)
    model_mapping TEXT NOT NULL DEFAULT '{}',       -- 渠道级请求模型映射 JSON object：source_model -> target_model

    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name);

CREATE TRIGGER IF NOT EXISTS update_channels_timestamp
    AFTER UPDATE ON channels
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE channels SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

CREATE TABLE IF NOT EXISTS claude_credential_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel TEXT NOT NULL,
    name TEXT NOT NULL,
    token TEXT NOT NULL DEFAULT '',
    api_key TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0,
    daily_cost_limit_usd REAL NOT NULL DEFAULT 0,
    cost_multiplier REAL NOT NULL DEFAULT 1.0,
    input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_creation_cost_multiplier_1h REAL NOT NULL DEFAULT 1.0,
    cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    disable_long_context_pricing INTEGER NOT NULL DEFAULT 1,
    disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1,
    service_tier_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    service_tier_input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    service_tier_output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    service_tier_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    UNIQUE(channel, name)
);

CREATE INDEX IF NOT EXISTS idx_claude_credential_groups_channel ON claude_credential_groups(channel);
CREATE UNIQUE INDEX IF NOT EXISTS idx_claude_credential_groups_single_default
    ON claude_credential_groups(channel, is_default) WHERE is_default = 1;

CREATE TRIGGER IF NOT EXISTS update_claude_credential_groups_timestamp
    AFTER UPDATE ON claude_credential_groups
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE claude_credential_groups SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

-- ============================================================================
-- Codex Provider 表 (新增 - 2026-03-10)
-- 独立维护 Codex Provider 配置，并通过单活 is_active 切换当前接管项
-- ============================================================================
CREATE TABLE IF NOT EXISTS codex_providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    name TEXT UNIQUE NOT NULL,
    provider_type TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    model TEXT NOT NULL,
    model_reasoning_effort TEXT NOT NULL DEFAULT 'xhigh',
    enable_1m_context INTEGER NOT NULL DEFAULT 0,
    website TEXT,
    is_active INTEGER DEFAULT 0,

    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

CREATE INDEX IF NOT EXISTS idx_codex_providers_name ON codex_providers(name);
CREATE INDEX IF NOT EXISTS idx_codex_providers_is_active ON codex_providers(is_active);
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_providers_single_active ON codex_providers(is_active) WHERE is_active = 1;

CREATE TRIGGER IF NOT EXISTS update_codex_providers_timestamp
    AFTER UPDATE ON codex_providers
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE codex_providers SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

-- ============================================================================
-- Codex 渠道-端点 两层模型 (新增 - 2026-05-13)
-- 一个渠道承载共用配置（模型/推理强度/1M 上下文/官网/provider_type）
-- 一个渠道下可挂多个凭证分组；端点归属到分组，同一渠道不同分组可复用相同端点名和 base_url
-- 单活语义：全局唯一一个 codex_channels.is_active=1，每个渠道下唯一一个 codex_endpoints.is_active=1
-- 旧 codex_providers 数据通过运行时 ensureCodexChannelsSchema 自动迁移到新表，旧表保留为 codex_providers_legacy
-- ============================================================================
CREATE TABLE IF NOT EXISTS codex_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    name TEXT UNIQUE NOT NULL,                                  -- 渠道名（同时作为 config.toml 中 model_provider 与 [model_providers.<name>] section）
    provider_type TEXT NOT NULL DEFAULT 'openai-compatible',    -- 接入类型元数据（当前不参与分流，保留扩展位）
    api_key TEXT NOT NULL,
    model TEXT NOT NULL,
    model_mapping TEXT NOT NULL DEFAULT '{}',                    -- 渠道级请求模型映射 JSON object：source_model -> target_model
    model_reasoning_effort TEXT NOT NULL DEFAULT 'xhigh',
    enable_1m_context INTEGER NOT NULL DEFAULT 0,
    website TEXT,
    is_active INTEGER NOT NULL DEFAULT 0,
    priority INTEGER NOT NULL DEFAULT 0,
    failover_enabled INTEGER NOT NULL DEFAULT 1,

    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

CREATE INDEX IF NOT EXISTS idx_codex_channels_name ON codex_channels(name);
CREATE INDEX IF NOT EXISTS idx_codex_channels_is_active ON codex_channels(is_active);
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_channels_single_active ON codex_channels(is_active) WHERE is_active = 1;

CREATE TRIGGER IF NOT EXISTS update_codex_channels_timestamp
    AFTER UPDATE ON codex_channels
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE codex_channels SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

CREATE TABLE IF NOT EXISTS codex_credential_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    api_key TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0,
    daily_cost_limit_usd REAL NOT NULL DEFAULT 0,
    cost_multiplier REAL NOT NULL DEFAULT 1.0,
    input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_input_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_output_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_creation_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    long_context_cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0,
    disable_long_context_pricing INTEGER NOT NULL DEFAULT 1,
    disable_service_tier_pricing INTEGER NOT NULL DEFAULT 1,
    service_tier_multipliers TEXT NOT NULL DEFAULT '[]', -- Standard/Batch/Flex/Fast mode 倍率 JSON
    billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens',
    model_multipliers TEXT NOT NULL DEFAULT '[]', -- 按模型附加倍率 JSON
    billing_rules TEXT NOT NULL DEFAULT '[]', -- 模型/档位/上下文组合计费规则 JSON
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    UNIQUE(channel_id, name)
);

CREATE INDEX IF NOT EXISTS idx_codex_credential_groups_channel ON codex_credential_groups(channel_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_credential_groups_single_default
    ON codex_credential_groups(channel_id, is_default) WHERE is_default = 1;

CREATE TRIGGER IF NOT EXISTS update_codex_credential_groups_timestamp
    AFTER UPDATE ON codex_credential_groups
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE codex_credential_groups SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

CREATE TABLE IF NOT EXISTS codex_endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    channel_id INTEGER NOT NULL REFERENCES codex_channels(id) ON DELETE CASCADE,
    credential_group_id INTEGER REFERENCES codex_credential_groups(id) ON DELETE SET NULL,
    api_key TEXT NOT NULL DEFAULT '',                           -- 端点级 API Key 覆盖，通常为空
    name TEXT NOT NULL,                                         -- 端点名（同凭证分组内唯一，仅作为 UI 标识）
    base_url TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 0,
    failover_enabled INTEGER NOT NULL DEFAULT 1,   -- 是否参与故障转移（1=参与）
    skip_health_check INTEGER NOT NULL DEFAULT 0,  -- 跳过健康检查（1=跳过）
    priority INTEGER NOT NULL DEFAULT 0,           -- 转移优先级（越小越优先）
    timeout_seconds INTEGER NOT NULL DEFAULT 0,    -- 请求超时（秒，0=使用全局配置）
    cost_multiplier REAL NOT NULL DEFAULT 1.0,     -- 成本倍率（默认 1.0，无调整）
    input_cost_multiplier REAL NOT NULL DEFAULT 1.0,      -- 输入 token 成本倍率
    output_cost_multiplier REAL NOT NULL DEFAULT 1.0,     -- 输出 token 成本倍率
    cache_read_cost_multiplier REAL NOT NULL DEFAULT 1.0, -- 缓存读取成本倍率
    billing_output_token_source TEXT NOT NULL DEFAULT 'output_tokens', -- 输出计费口径
    model_multipliers TEXT NOT NULL DEFAULT '[]', -- 按模型附加倍率 JSON

    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

CREATE INDEX IF NOT EXISTS idx_codex_endpoints_channel ON codex_endpoints(channel_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_single_active_per_channel
    ON codex_endpoints(channel_id, is_active) WHERE is_active = 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_group_base_url
    ON codex_endpoints(channel_id, credential_group_id, base_url) WHERE credential_group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_legacy_base_url
    ON codex_endpoints(channel_id, base_url) WHERE credential_group_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_group_name
    ON codex_endpoints(channel_id, credential_group_id, name) WHERE credential_group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_codex_endpoints_unique_legacy_name
    ON codex_endpoints(channel_id, name) WHERE credential_group_id IS NULL;

CREATE TRIGGER IF NOT EXISTS update_codex_endpoints_timestamp
    AFTER UPDATE ON codex_endpoints
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE codex_endpoints SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

-- ============================================================================
-- 模型定价表 (新增 - 2025-12-06, 更新于 2025-12-06 支持 5m/1h 缓存)
-- 将模型定价从 config.yaml 迁移到 SQLite，支持动态管理
-- ============================================================================
CREATE TABLE IF NOT EXISTS model_pricing (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- ========== 模型信息 ==========
    model_name TEXT UNIQUE NOT NULL,                -- 模型名称（如 claude-sonnet-4-20250514）
    provider TEXT NOT NULL DEFAULT 'claude',        -- 提供商标识: claude/codex

    -- ========== 定价信息 (USD per 1M tokens) ==========
    input_price REAL NOT NULL DEFAULT 3.0,          -- 输入价格
    output_price REAL NOT NULL DEFAULT 15.0,        -- 输出价格
    cache_creation_price_5m REAL DEFAULT 3.75,      -- 5分钟缓存创建价格 (通常为 input * 1.25)
    cache_creation_price_1h REAL DEFAULT 6.0,       -- 1小时缓存创建价格 (通常为 input * 2.0)
    cache_read_price REAL DEFAULT 0.30,             -- 缓存读取价格 (通常为 input * 0.1)
    long_context_threshold INTEGER NOT NULL DEFAULT 0,         -- Long context 原始输入 token 阈值，0 表示禁用
    long_context_input_price REAL NOT NULL DEFAULT 0,          -- Long context 输入价格
    long_context_cache_read_price REAL NOT NULL DEFAULT 0,     -- Long context 缓存读取价格
    long_context_cache_write_price REAL NOT NULL DEFAULT 0,    -- Long context 缓存写入价格
    long_context_output_price REAL NOT NULL DEFAULT 0,         -- Long context 输出价格

    -- ========== 模型元信息 ==========
    display_name TEXT,                              -- 显示名称（用于前端展示）
    description TEXT,                               -- 模型描述
    is_default INTEGER DEFAULT 0,                   -- 是否为默认定价 (1=是)
    -- ========== 审计字段 ==========
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00')
);

-- 模型定价表索引
CREATE INDEX IF NOT EXISTS idx_model_pricing_model_name ON model_pricing(model_name);
CREATE INDEX IF NOT EXISTS idx_model_pricing_is_default ON model_pricing(is_default);
CREATE INDEX IF NOT EXISTS idx_model_pricing_provider ON model_pricing(provider);

-- 模型定价表触发器：自动更新 updated_at
CREATE TRIGGER IF NOT EXISTS update_model_pricing_timestamp
    AFTER UPDATE ON model_pricing
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE model_pricing SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;

-- ============================================================================
-- 系统设置表 (新增 - 2025-12-08)
-- 将运行时可调配置从 config.yaml 迁移到 SQLite，支持动态管理和热更新
-- ============================================================================
CREATE TABLE IF NOT EXISTS settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- ========== 设置标识 ==========
    provider TEXT NOT NULL CHECK (provider IN ('global', 'claude', 'codex')), -- 配置作用域
    category TEXT NOT NULL,                         -- 分类: strategy, retry, health, failover, request, streaming, auth, token_counting, retention, hot_pool, server
    key TEXT NOT NULL,                              -- 配置键

    -- ========== 设置值 ==========
    value TEXT NOT NULL,                            -- 值 (JSON格式支持复杂类型)
    value_type TEXT DEFAULT 'string',               -- 类型: string, int, float, bool, duration, json

    -- ========== 显示信息 ==========
    label TEXT,                                     -- 显示名称
    description TEXT,                               -- 配置说明
    display_order INTEGER DEFAULT 0,                -- 显示顺序（同分类内排序）

    -- ========== 元信息 ==========
    requires_restart INTEGER DEFAULT 0,             -- 是否需要重启生效 (1=需要, 0=立即生效)

    -- ========== 审计字段 ==========
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00'),

    UNIQUE(provider, category, key)
);

-- 设置表索引
CREATE INDEX IF NOT EXISTS idx_settings_provider_category ON settings(provider, category);
CREATE INDEX IF NOT EXISTS idx_settings_provider_category_key ON settings(provider, category, key);

-- 设置表触发器：自动更新 updated_at
CREATE TRIGGER IF NOT EXISTS update_settings_timestamp
    AFTER UPDATE ON settings
    FOR EACH ROW
    WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE settings SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now', 'localtime') || '+08:00' WHERE id = NEW.id;
END;
