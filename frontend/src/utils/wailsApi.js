// ============================================
// Wails API 适配层
// 2025-12-04
// 将 Wails Bindings 包装为与 HTTP API 兼容的接口
// ============================================

import { parseMultiplierExpression } from './multiplier.js';

const parseEndpointMultiplier = (value, fallback = 1.0) => {
  const parsed = parseMultiplierExpression(value, fallback);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
};

const normalizeModelMapping = (mapping) => {
  if (!mapping || typeof mapping !== 'object' || Array.isArray(mapping)) {
    return {};
  }
  return Object.entries(mapping).reduce((result, [sourceModel, targetModel]) => {
    const source = String(sourceModel || '').trim();
    const target = String(targetModel || '').trim();
    if (source && target) {
      result[source] = target;
    }
    return result;
  }, {});
};

const normalizeModelMultipliers = (rules) => {
  if (!Array.isArray(rules)) return [];
  return rules
    .map((rule) => ({
      model: String(rule?.model || '').trim(),
      costMultiplier: parseEndpointMultiplier(rule?.costMultiplier),
      inputCostMultiplier: parseEndpointMultiplier(rule?.inputCostMultiplier),
      outputCostMultiplier: parseEndpointMultiplier(rule?.outputCostMultiplier),
      cacheReadCostMultiplier: parseEndpointMultiplier(rule?.cacheReadCostMultiplier)
    }))
    .filter((rule) => rule.model);
};

const hasOwn = (value, key) => Object.prototype.hasOwnProperty.call(value || {}, key);

const getExplicitModelMultipliers = (input, normalizer = normalizeModelMultipliers) => {
  if (hasOwn(input, 'modelMultipliers')) {
    return normalizer(input.modelMultipliers);
  }
  if (hasOwn(input, 'model_multipliers')) {
    return normalizer(input.model_multipliers);
  }
  return undefined;
};

const mapClaudeCredentialGroup = (group = {}) => ({
  id: group.id,
  channel: group.channel || '',
  name: group.name || '',
  token: group.token || '',
  apiKey: group.api_key || group.apiKey || '',
  tokenMasked: group.token_masked || group.tokenMasked || '',
  apiKeyMasked: group.api_key_masked || group.apiKeyMasked || '',
  isDefault: Boolean(group.is_default ?? group.isDefault),
  costMultiplier: group.cost_multiplier ?? group.costMultiplier ?? 1.0,
  inputCostMultiplier: group.input_cost_multiplier ?? group.inputCostMultiplier ?? 1.0,
  outputCostMultiplier: group.output_cost_multiplier ?? group.outputCostMultiplier ?? 1.0,
  cacheCreationCostMultiplier: group.cache_creation_cost_multiplier ?? group.cacheCreationCostMultiplier ?? 1.0,
  cacheCreationCostMultiplier1h: group.cache_creation_cost_multiplier_1h ?? group.cacheCreationCostMultiplier1h ?? 1.0,
  cacheReadCostMultiplier: group.cache_read_cost_multiplier ?? group.cacheReadCostMultiplier ?? 1.0,
  modelMultipliers: normalizeModelMultipliers(group.model_multipliers || group.modelMultipliers),
  endpointCount: group.endpoint_count ?? group.endpointCount ?? 0,
  createdAt: group.created_at || group.createdAt || '',
  updatedAt: group.updated_at || group.updatedAt || ''
});

const buildClaudeCredentialGroupPayload = (input = {}) => {
  const payload = {
    channel: (input.channel || '').trim(),
    name: (input.name || '').trim(),
    token: (input.token || '').trim(),
    api_key: (input.apiKey || input.api_key || '').trim(),
    is_default: Boolean(input.isDefault || input.is_default),
    cost_multiplier: parseEndpointMultiplier(input.costMultiplier),
    input_cost_multiplier: parseEndpointMultiplier(input.inputCostMultiplier),
    output_cost_multiplier: parseEndpointMultiplier(input.outputCostMultiplier),
    cache_creation_cost_multiplier: parseEndpointMultiplier(input.cacheCreationCostMultiplier),
    cache_creation_cost_multiplier_1h: parseEndpointMultiplier(input.cacheCreationCostMultiplier1h),
    cache_read_cost_multiplier: parseEndpointMultiplier(input.cacheReadCostMultiplier)
  };
  const modelMultipliers = getExplicitModelMultipliers(input);
  if (modelMultipliers !== undefined) {
    payload.model_multipliers = modelMultipliers;
  }
  return payload;
};

const normalizePrivacyRule = (rule = {}) => ({
  id: rule.id,
  name: rule.name || '',
  type: rule.type || 'exact',
  category: rule.category || 'custom',
  pattern_or_value: rule.pattern_or_value || '',
  replacement_mode: rule.replacement_mode || rule.replacementMode || 'placeholder',
  replacementMode: rule.replacement_mode || rule.replacementMode || 'placeholder',
  patternMasked: rule.pattern_masked || rule.patternMasked || '',
  pattern_masked: rule.pattern_masked || rule.patternMasked || '',
  valueLength: rule.value_length ?? rule.valueLength ?? 0,
  value_length: rule.value_length ?? rule.valueLength ?? 0,
  enabled: Boolean(rule.enabled),
  priority: Number(rule.priority ?? 100),
  created_at: rule.created_at || rule.createdAt || '',
  updated_at: rule.updated_at || rule.updatedAt || ''
});

const normalizePrivacySyntheticValue = (record = {}) => ({
  raw_hash: record.raw_hash || record.rawHash || '',
  rawHash: record.raw_hash || record.rawHash || '',
  raw_value: record.raw_value || record.rawValue || '',
  rawValue: record.raw_value || record.rawValue || '',
  replacement: record.replacement || record.value || '',
  value: record.replacement || record.value || '',
  kind: record.kind || 'custom',
  created_at: record.created_at || record.createdAt || '',
  createdAt: record.created_at || record.createdAt || '',
  last_seen_at: record.last_seen_at || record.lastSeenAt || '',
  lastSeenAt: record.last_seen_at || record.lastSeenAt || '',
  last_skip_at: record.last_skip_at || record.lastSkipAt || '',
  lastSkipAt: record.last_skip_at || record.lastSkipAt || '',
  reuse_count: Number(record.reuse_count ?? record.reuseCount ?? 0),
  reuseCount: Number(record.reuse_count ?? record.reuseCount ?? 0),
  skip_count: Number(record.skip_count ?? record.skipCount ?? 0),
  skipCount: Number(record.skip_count ?? record.skipCount ?? 0),
  hit_count: Number(record.hit_count ?? record.hitCount ?? 0),
  hitCount: Number(record.hit_count ?? record.hitCount ?? 0)
});

// 检测是否在 Wails 环境中运行
// v3: 使用 window._wails 替代 v2 的 window.go
export const isWailsEnvironment = () => {
  return typeof window !== 'undefined' && (window._wails !== undefined || window.go !== undefined);
};

// 动态导入 Wails 绑定（避免在非 Wails 环境中报错）
let WailsApp = null;
let WailsRuntime = null;

const loadWailsBindings = async () => {
  if (!isWailsEnvironment()) {
    return false;
  }

  try {
    WailsApp = await import('@wailsjs/go/main/App');
    WailsRuntime = await import('@wailsjs/runtime/runtime');
    return true;
  } catch (error) {
    console.warn('Failed to load Wails bindings:', error);
    return false;
  }
};

// 初始化 Wails 绑定
let wailsInitialized = false;
let wailsInitPromise = null;

export const initWails = async () => {
  if (wailsInitialized) return true;
  if (wailsInitPromise) return wailsInitPromise;

  wailsInitPromise = loadWailsBindings().then(result => {
    wailsInitialized = result;
    return result;
  });

  return wailsInitPromise;
};

// 打开外部链接（Wails 环境下优先走 BrowserOpenURL，避免 WebView 内导航）
export const openExternalURL = async (url) => {
  const target = (url || '').trim();
  if (!target) return;

  if (isWailsEnvironment()) {
    await initWails();
    try {
      WailsRuntime?.BrowserOpenURL?.(target);
      return;
    } catch (error) {
      console.warn('Failed to open URL via Wails runtime:', error);
    }
  }

  try {
    if (typeof window !== 'undefined' && typeof window.open === 'function') {
      window.open(target, '_blank', 'noopener,noreferrer');
    }
  } catch (error) {
    console.warn('Failed to open URL via window.open:', error);
  }
};

// ============================================
// Wails Events 适配
// ============================================

// 事件监听器映射
const eventListeners = new Map();

/**
 * 订阅 Wails 事件
 * @param {string} eventName - 事件名称
 * @param {Function} callback - 回调函数
 * @returns {Function} - 取消订阅函数
 */
export const subscribeToEvent = (eventName, callback) => {
  // 异步初始化后订阅
  let unsubscribeFunc = null;
  let isUnsubscribed = false;

  console.log(`📡 [subscribeToEvent] 开始订阅事件: ${eventName}`);

  initWails().then(() => {
    if (isUnsubscribed) {
      console.log(`📡 [subscribeToEvent] 事件 ${eventName} 已取消订阅，跳过注册`);
      return;
    }

    if (!WailsRuntime) {
      console.warn(`📡 [subscribeToEvent] Wails Runtime not loaded, 无法订阅 ${eventName}`);
      return;
    }

    console.log(`📡 [subscribeToEvent] 注册事件监听: ${eventName}`);
    unsubscribeFunc = WailsRuntime.EventsOn(eventName, (data) => {
      console.log(`📡 [subscribeToEvent] 收到事件 ${eventName}:`, data);
      callback(data);
    });

    // 存储监听器以便清理
    if (!eventListeners.has(eventName)) {
      eventListeners.set(eventName, []);
    }
    eventListeners.get(eventName).push({ callback, unsubscribe: unsubscribeFunc });
  });

  // 返回取消订阅函数
  return () => {
    isUnsubscribed = true;
    if (unsubscribeFunc && typeof unsubscribeFunc === 'function') {
      unsubscribeFunc();
    }
  };
};

/**
 * 取消订阅所有事件
 */
export const unsubscribeAll = () => {
  if (!WailsRuntime) return;

  eventListeners.forEach((listeners) => {
    listeners.forEach(({ unsubscribe }) => {
      if (typeof unsubscribe === 'function') {
        unsubscribe();
      }
    });
  });
  eventListeners.clear();
};

// ============================================
// 系统状态 API
// ============================================

export const getSystemStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetSystemStatus();

  // 转换为前端期望的格式
  return {
    status: status.proxy_running ? 'running' : 'stopped',
    version: status.version,
    uptime: status.uptime_seconds,
    start_time: status.start_time, // ISO8601 格式的启动时间
    proxy_running: status.proxy_running,
    proxy_port: status.proxy_port,
    proxy_host: status.proxy_host,
    active_group: status.active_group,
    config_path: status.config_path,
    auth_enabled: status.auth_enabled,
    codex_enabled: status.codex_proxy_enabled
  };
};

/**
 * 获取 Claude Code CLI 版本
 * @returns {Promise<string>} 版本号，如 "1.0.17" 或 "未安装"
 */
export const getClaudeCodeVersion = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.GetClaudeCodeVersion();
};

/**
 * 获取 Codex CLI 版本
 * @returns {Promise<string>} 版本号，如 "0.1.0" 或 "未安装"
 */
export const getCodexVersion = async () => {
  await initWails()
  if (!WailsApp) throw new Error('Wails not available')
  return await WailsApp.GetCodexVersion()
}

export const getConfig = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetConfig();
};

// ============================================
// 端点管理 API
// ============================================

export const getEndpoints = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const endpoints = await WailsApp.GetEndpoints();

  // 转换为前端期望的格式
  const formattedEndpoints = endpoints.map(ep => ({
    name: ep.name,
    url: ep.url,
    channel: ep.channel || '', // 渠道标签
    group: ep.group,
    priority: ep.priority,
    group_priority: ep.group_priority,
    group_is_active: ep.group_is_active,
    healthy: ep.healthy,
    health_state: ep.health_state,         // 健康状态枚举
    skip_health_check: ep.skip_health_check, // 跳过健康检查
    status: ep.healthy ? 'healthy' : 'unhealthy',
    last_check: ep.last_check || ep.lastCheck || ep.LastCheck || '',
    response_time: ep.response_time_ms ?? ep.responseTimeMs ?? ep.ResponseTimeMs,
    createdAt: ep.created_at || ep.createdAt || ep.CreatedAt || '',
    updatedAt: ep.updated_at || ep.updatedAt || ep.UpdatedAt || '',
    consecutive_fail: ep.consecutive_fail,
    never_checked: !(ep.last_check || ep.lastCheck || ep.LastCheck),
    // 错误信息字段
    last_fail_code: ep.last_fail_code || 0,
    last_fail_reason: ep.last_fail_reason || ''
  }));

  const healthy = formattedEndpoints.filter(e => e.healthy).length;

  return {
    endpoints: formattedEndpoints,
    total: formattedEndpoints.length,
    healthy
  };
};

export const setEndpointPriority = async (endpointName, priority) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetEndpointPriority(endpointName, priority);
  return { success: true };
};

export const triggerHealthCheck = async (endpointName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.TriggerHealthCheck(endpointName);
  return { success: true, healthy: true };
};

export const batchHealthCheckAll = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const result = await WailsApp.BatchHealthCheckAll();
  return {
    success: result.success,
    message: result.message,
    total: result.total,
    healthy_count: result.healthy_count,
    unhealthy_count: result.unhealthy_count
  };
};

// ============================================
// Key 管理 API
// ============================================

export const getKeysOverview = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  console.log('🔑 [Wails] 调用 GetKeysOverview...');
  const result = await WailsApp.GetKeysOverview();
  console.log('🔑 [Wails] GetKeysOverview 原始返回:', result);

  // 转换为前端期望的格式
  const endpoints = (result.endpoints || []).map(ep => ({
    endpoint: ep.endpoint,
    tokens: (ep.tokens || []).map(t => ({
      index: t.index,
      name: t.name || `Token ${t.index + 1}`,
      masked: t.value,  // 后端返回的是 value 字段（已脱敏）
      is_active: t.is_active
    })),
    api_keys: (ep.api_keys || []).map(k => ({
      index: k.index,
      name: k.name || `API Key ${k.index + 1}`,
      masked: k.value,  // 后端返回的是 value 字段（已脱敏）
      is_active: k.is_active
    })),
    current_token_index: ep.current_token_index,
    current_api_key_index: ep.current_api_key_index
  }));

  const formatted = {
    endpoints,
    total: endpoints.length,
    timestamp: result.timestamp
  };
  console.log('🔑 [Wails] GetKeysOverview 格式化后:', formatted);
  return formatted;
};

export const switchKey = async (endpointName, keyType, index) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const result = await WailsApp.SwitchKey(endpointName, keyType, index);
  return {
    success: result.success,
    message: result.message,
    endpoint: result.endpoint,
    key_type: result.key_type,
    new_index: result.new_index
  };
};

// ============================================
// 组管理 API
// ============================================

export const getGroups = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 获取组、端点和 Keys 信息
  const [groups, endpointsData, keysData] = await Promise.all([
    WailsApp.GetGroups(),
    WailsApp.GetEndpoints(),
    WailsApp.GetKeysOverview().catch(() => ({ endpoints: [] })) // 容错：获取 keys 失败不影响主流程
  ]);

  // 组名 = 渠道(channel)（后端已在 GetEndpoints 中映射到 ep.group）
  // 计算每个组的健康端点统计
  const groupHealthMap = new Map();
  endpointsData.forEach(ep => {
    const groupName = ep.group || ep.channel || ep.name;
    if (!groupHealthMap.has(groupName)) {
      groupHealthMap.set(groupName, { total: 0, healthy: 0 });
    }
    const stats = groupHealthMap.get(groupName);
    stats.total++;
    if (ep.healthy) {
      stats.healthy++;
    }
  });

  // 构建端点名到 tokens 的映射
  const endpointTokensMap = new Map();
  (keysData.endpoints || []).forEach(ep => {
    const tokens = (ep.tokens || []).map(t => ({
      index: t.index,
      name: t.name || `Token ${t.index + 1}`,
      key: t.value, // 脱敏的 key 值
      is_active: t.is_active,
      endpoint: ep.endpoint, // 关联的端点名
      type: inferTokenType(t.name) // 推断 Token 类型
    }));
    endpointTokensMap.set(ep.endpoint, tokens);
  });

  // 将端点 tokens 聚合到组（渠道）
  const groupTokensMap = new Map();
  endpointsData.forEach(ep => {
    const groupName = ep.group || ep.channel || ep.name;
    const endpointTokens = endpointTokensMap.get(ep.name) || [];
    if (!groupTokensMap.has(groupName)) {
      groupTokensMap.set(groupName, []);
    }
    const existing = groupTokensMap.get(groupName);
    existing.push(...endpointTokens);
  });

  // 推断 Token 类型的辅助函数
  function inferTokenType(name) {
    if (!name) return 'Std';
    const lowerName = name.toLowerCase();
    if (lowerName.includes('pro') || lowerName.includes('特价')) return 'Pro';
    if (lowerName.includes('ent') || lowerName.includes('主号')) return 'Ent';
    if (lowerName.includes('free') || lowerName.includes('测试')) return 'Free';
    return 'Std';
  }

  // 转换为前端期望的格式
  const formattedGroups = groups.map(g => {
    const healthStats = groupHealthMap.get(g.name) || { total: 0, healthy: 0 };
    // 组名 = 渠道(channel)，聚合组内所有端点 tokens
    const tokens = groupTokensMap.get(g.name) || [];

    return {
      name: g.name,
      channel: g.channel,  // 渠道名称
      is_active: g.active,
      paused: g.paused,
      priority: g.priority,
      endpoint_count: g.endpoint_count,
      total_endpoints: healthStats.total,
      healthy_endpoints: healthStats.healthy,
      unhealthy_endpoints: healthStats.total - healthStats.healthy,
      in_cooldown: g.in_cooldown,
      cooldown_remain_ms: g.cooldown_remain_ms,
      tokens: tokens // 添加 tokens 数组
    };
  });

  const activeGroup = formattedGroups.find(g => g.is_active);

  return {
    groups: formattedGroups,
    active_group: activeGroup?.name || null,
    total_suspended_requests: 0
  };
};

// 获取后端原始组信息（轻量：仅 GetGroups，不做额外聚合）
export const getGroupsRaw = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const groups = await WailsApp.GetGroups();
  return Array.isArray(groups) ? groups : [];
};

// 检查是否启用渠道间故障转移
// 用于前端判断是否显示渠道切换器
export const isInterChannelFailoverEnabled = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.IsInterChannelFailoverEnabled();
};

// 获取下一个可用渠道（用于"暂停活跃渠道"前的提示/确认）
export const getNextAvailableChannel = async (excludeChannel) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const name = String(excludeChannel || '').trim();
  return await WailsApp.GetNextAvailableChannel(name);
};

export const activateGroup = async (groupName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ActivateGroup(groupName);
  return { success: true };
};

export const pauseGroup = async (groupName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.PauseGroup(groupName);
  return { success: true };
};

export const resumeGroup = async (groupName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ResumeGroup(groupName);
  return { success: true };
};

// ============================================
// 使用统计 API
// ============================================

/**
 * 获取使用统计（与 HTTP API 格式一致）
 * @param {Object} params - 查询参数
 * @param {string} params.period - 时间周期: "1h", "1d", "7d", "30d", "90d"
 * @param {string} params.start_date - 开始时间（优先于 period）
 * @param {string} params.end_date - 结束时间（优先于 period）
 * @param {string} params.status - 状态筛选
 * @param {string} params.model - 模型筛选
 * @param {string} params.endpoint - 端点筛选
 * @param {string} params.group - 组筛选
 * @returns {Promise<Object>} - 统计数据
 */
export const getUsageStats = async (params = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 构建查询参数对象
  // 注意：前端 buildQueryParams() 返回 start_date/end_date（带下划线）
  const queryParams = {
    period: params.period || '30d',
    start_date: params.start_date || '',
    end_date: params.end_date || '',
    status: params.status || '',
    model: params.model || '',
    channel: params.channel || '',
    endpoint: params.endpoint || '',
    group: params.group || '',
    provider: params.provider || ''
  };

  console.log('📊 [Wails] GetUsageStats 参数:', queryParams);
  const data = await WailsApp.GetUsageStats(queryParams);

  // 返回与 HTTP API 一致的格式
  return {
    period: data.period || queryParams.period,
    total_requests: data.total_requests || 0,
    success_rate: data.success_rate || 0,
    avg_duration_ms: data.avg_duration_ms || 0,
    total_cost_usd: data.total_cost_usd || 0,
    total_tokens: data.total_tokens || 0,
    failed_requests: data.failed_requests || 0
  };
};

export const getUsageSummary = async (startTime = '', endTime = '', provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const summary = await WailsApp.GetUsageSummary(startTime, endTime, provider);

  return {
    total_requests: summary.total_requests || 0,
    all_time_total_requests: summary.all_time_total_requests || 0, // 全部历史请求数
    today_requests: summary.today_requests || 0,            // 今日请求数
    successful_requests: summary.success_requests || 0,
    failed_requests: summary.failed_requests || 0,
    total_input_tokens: summary.total_input_tokens || 0,
    total_output_tokens: summary.total_output_tokens || 0,
    total_cost: summary.total_cost || 0,
    today_cost: summary.today_cost || 0,                    // 今日成本
    all_time_total_cost: summary.all_time_total_cost || 0,  // 全部历史成本
    today_tokens: summary.today_tokens || 0,                // 今日 tokens
    all_time_total_tokens: summary.all_time_total_tokens || 0,  // 全部历史 tokens
    total_tokens: summary.total_tokens || ((summary.total_input_tokens || 0) + (summary.total_output_tokens || 0))
  };
};

export const getRequests = async (params = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 构建 Wails 绑定参数对象
  // 注意：前端 buildQueryParams() 返回 start_date/end_date（带下划线）
  const queryParams = {
    page: parseInt(params.page || 1),
    page_size: parseInt(params.limit || params.pageSize || 50),
    start_date: params.start_date || params.start_time || params.startDate || '',
    end_date: params.end_date || params.end_time || params.endDate || '',
    status: params.status || '',
    model: params.model || '',
    channel: params.channel || '',
    endpoint: params.endpoint || '',
    group: params.group || '',
    provider: params.provider || ''
  };

  console.log('🔍 [Wails] GetRequests 参数:', queryParams);
  const result = await WailsApp.GetRequests(queryParams);
  console.log('🔍 [Wails] GetRequests 返回:', result);

  // 转换请求记录格式
  const requests = (result.requests || []).map(r => ({
    request_id: r.request_id,
    id: r.request_id,
    timestamp: r.timestamp,
    start_time: r.timestamp,
    channel: r.channel || '',
    endpoint_name: r.endpoint,
    endpoint: r.endpoint,
    endpoint_chain: r.endpoint_chain || r.endpointChain || '',
    group_name: r.group,
    group: r.group,
    model_name: r.model,
    model: r.model,
    original_model_name: r.original_model_name || '',
    model_rewrite_source: r.model_rewrite_source || '',
    model_rewrite_target: r.model_rewrite_target || '',
    originalModelName: r.original_model_name || '',
    modelRewriteSource: r.model_rewrite_source || '',
    modelRewriteTarget: r.model_rewrite_target || '',
    privacy_redacted: Boolean(r.privacy_redacted),
    privacy_redaction_summary: r.privacy_redaction_summary || '',
    privacy_redaction_matches: r.privacy_redaction_matches || '',
    privacyRedacted: Boolean(r.privacy_redacted),
    privacyRedactionSummary: r.privacy_redaction_summary || '',
    privacyRedactionMatches: r.privacy_redaction_matches || '',
    provider: r.provider || '',
    status: r.status,
    status_code: r.http_status,
    http_status_code: r.http_status,  // 添加 http_status_code 映射
    retry_count: r.retry_count || 0,  // 添加重试次数
    input_tokens: r.input_tokens,
    output_tokens: r.output_tokens,
    reasoning_tokens: r.reasoning_tokens ?? 0,
    billable_output_tokens: r.billable_output_tokens ?? r.output_tokens ?? 0,
    billing_output_token_source: r.billing_output_token_source || 'output_tokens',
    cache_creation_tokens: r.cache_creation_tokens,
    cache_creation_5m_tokens: r.cache_creation_5m_tokens,
    cache_creation_1h_tokens: r.cache_creation_1h_tokens,
    cache_read_tokens: r.cache_read_tokens,
    duration_ms: r.response_time,
    duration: r.response_time,
    first_byte_ms: r.first_byte_ms ?? r.firstByteMs ?? null,
    is_streaming: r.is_streaming,
    total_cost_usd: r.cost,
    cost: r.cost,
    // 成本明细字段（计费详情展示用）
    input_cost_usd: r.input_cost_usd || 0,
    output_cost_usd: r.output_cost_usd || 0,
    cache_creation_cost_usd: r.cache_creation_cost_usd || 0,
    cache_creation_5m_cost_usd: r.cache_creation_5m_cost_usd || 0,
    cache_creation_1h_cost_usd: r.cache_creation_1h_cost_usd || 0,
    cache_read_cost_usd: r.cache_read_cost_usd || 0,
    // 端点倍率快照（计费详情展示用）
    cost_multiplier: r.cost_multiplier ?? 1.0,
    input_cost_multiplier: r.input_cost_multiplier ?? 1.0,
    output_cost_multiplier: r.output_cost_multiplier ?? 1.0,
    cache_creation_cost_multiplier: r.cache_creation_cost_multiplier ?? 1.0,
    cache_creation_cost_multiplier_1h: r.cache_creation_cost_multiplier_1h ?? 1.0,
    cache_read_cost_multiplier: r.cache_read_cost_multiplier ?? 1.0,
    model_cost_multiplier: r.model_cost_multiplier ?? 1.0,
    model_input_cost_multiplier: r.model_input_cost_multiplier ?? 1.0,
    model_output_cost_multiplier: r.model_output_cost_multiplier ?? 1.0,
    model_cache_read_cost_multiplier: r.model_cache_read_cost_multiplier ?? 1.0,
    // 错误信息字段
    failure_reason: r.failure_reason || '',
    cancel_reason: r.cancel_reason || ''
  }));

  return {
    requests,
    total: result.total,
    page: result.page,
    pageSize: result.page_size,
    totalPages: Math.ceil(result.total / result.page_size)
  };
};

export const getPrivacyConfig = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const config = await WailsApp.GetPrivacyConfig();
  return {
    enabled: Boolean(config?.enabled),
    rules: (config?.rules || []).map(normalizePrivacyRule)
  };
};

export const setPrivacyEnabled = async (enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.SetPrivacyEnabled(Boolean(enabled));
};

export const createPrivacyRule = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const rule = await WailsApp.CreatePrivacyRule(input);
  return normalizePrivacyRule(rule);
};

export const updatePrivacyRule = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const rule = await WailsApp.UpdatePrivacyRule(Number(id), input);
  return normalizePrivacyRule(rule);
};

export const deletePrivacyRule = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.DeletePrivacyRule(Number(id));
};

export const previewPrivacyRedaction = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const result = await WailsApp.PreviewPrivacyRedaction(String(input ?? ''));
  return {
    output: result?.output || '',
    changed: Boolean(result?.changed),
    hits: result?.hits || []
  };
};

export const getPrivacySyntheticValues = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const records = await WailsApp.GetPrivacySyntheticValues();
  return (records || []).map(normalizePrivacySyntheticValue);
};

export const clearPrivacySyntheticValues = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.ClearPrivacySyntheticValues();
};

export const deletePrivacySyntheticValue = async (kind, rawHash) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeletePrivacySyntheticValue(kind, rawHash);
  return { success: true };
};

// ============================================
// 代理信息
// ============================================

export const getProxyURL = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetProxyURL();
};

export const isProxyRunning = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.IsProxyRunning();
};

// ============================================
// 系统操作 API
// ============================================

/**
 * 打开 Claude 配置目录
 */
export const openClaudeConfigDir = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.OpenClaudeConfigDir();
};

/**
 * 打开 Codex 配置目录
 */
export const openCodexConfigDir = async () => {
  await initWails()
  if (!WailsApp) throw new Error('Wails not available')
  return await WailsApp.OpenCodexConfigDir()
}

/**
 * 打开日志目录
 */
export const openLogDir = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.OpenLogDir();
};

/**
 * 请求退出应用
 */
export const requestQuit = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.RequestQuit();
};

/**
 * 复制文本到剪贴板
 */
export const copyToClipboard = async (text) => {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Fallback for older browsers
    const textArea = document.createElement('textarea');
    textArea.value = text;
    document.body.appendChild(textArea);
    textArea.select();
    document.execCommand('copy');
    document.body.removeChild(textArea);
    return true;
  }
};

// ============================================
// 图表数据 API
// ============================================

/**
 * 获取请求趋势图表数据
 * @param {number} minutes - 时间范围（分钟）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 图表数据点数组 [{time, total, success, fail}]
 */
export const getRequestTrendChart = async (minutes = 30, provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  console.log('📊 [Wails] 调用 GetRequestTrendChart, minutes:', minutes, 'provider:', provider);
  const data = await WailsApp.GetRequestTrendChart(minutes, provider);
  console.log('📊 [Wails] GetRequestTrendChart 返回:', data ? `${data.length} 个数据点` : '无数据', data);
  return data || [];
};

/**
 * 获取响应时间图表数据
 * @param {number} minutes - 时间范围（分钟）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 图表数据点数组 [{time, avg, min, max}]
 */
export const getResponseTimeChart = async (minutes = 30, provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetResponseTimeChart(minutes, provider);
  return data || [];
};

/**
 * 获取连接活动图表数据
 * @param {number} minutes - 时间范围（分钟）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 图表数据点数组 [{time, value}]
 */
export const getConnectionActivityChart = async (minutes = 60, provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetConnectionActivityChart(minutes, provider);
  // 转换为前端期望的格式 (connections 字段)
  return (data || []).map(point => ({
    time: point.time,
    timestamp: point.timestamp,
    connections: point.value
  }));
};

// ============================================
// Token 使用统计 API
// ============================================

/**
 * 获取 Token 使用统计（今日 request_logs 数据）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Object>} - Token 使用数据 {input, output, cacheCreation, cacheRead}
 */
export const getTokenUsage = async (provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetTokenUsage(provider);
  return {
    input: data.input_tokens || 0,
    output: data.output_tokens || 0,
    cacheCreation: data.cache_creation_tokens || 0,
    cacheRead: data.cache_read_tokens || 0,
    total: data.total_tokens || 0
  };
};

// ============================================
// 端点健康状态图表 API
// ============================================

/**
 * 获取端点健康状态数据
 * @returns {Promise<Object>} - 健康状态数据 {healthy, unhealthy, skipped, excluded, total}
 */
export const getEndpointHealthChart = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetEndpointHealthChart();
  return {
    healthy: data.healthy || 0,
    unhealthy: data.unhealthy || 0,
    skipped: data.skipped || 0,
    excluded: data.excluded || 0,
    total: data.total || 0
  };
};

/**
 * 获取 Codex 端点健康状态数据（仅当前激活渠道下的端点）
 * @returns {Promise<Object>} - 健康状态数据 {healthy, unhealthy, skipped, excluded, total}
 */
export const getCodexEndpointHealthChart = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetCodexEndpointHealthChart();
  return {
    healthy: data.healthy || 0,
    unhealthy: data.unhealthy || 0,
    skipped: data.skipped || 0,
    excluded: data.excluded || 0,
    total: data.total || 0
  };
};

// ============================================
// 端点成本图表 API
// ============================================

/**
 * 获取当日端点成本数据
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 端点成本数据 [{name, tokens, cost}]
 */
export const getEndpointCosts = async (provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetEndpointCosts(provider);
  // 数据已经是 [{name, tokens, cost}] 格式
  return data || [];
};

// ============================================
// 端点存储管理 API (SQLite)
// ============================================

/**
 * 获取端点存储状态
 * @returns {Promise<Object>} - {enabled, storage_type, total_count, enabled_count}
 */
export const getEndpointStorageStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetEndpointStorageStatus();
  return {
    enabled: status.enabled || false,
    storageType: status.storage_type || 'yaml',
    totalCount: status.total_count || 0,
    enabledCount: status.enabled_count || 0
  };
};

/**
 * 获取所有端点记录（SQLite 存储）
 * @returns {Promise<Array>} - 端点记录数组
 */
export const getEndpointRecords = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const records = await WailsApp.GetEndpointRecords();
  return (records || []).map(r => ({
    id: r.id,
    channel: r.channel,
    credentialGroupId: r.credential_group_id ?? r.credentialGroupId ?? 0,
    credentialGroupName: r.credential_group_name || r.credentialGroupName || '',
    name: r.name,
    url: r.url,
    token: r.token,       // 本地桌面应用，直接返回原始 Token
    apiKey: r.api_key,    // 本地桌面应用，直接返回原始 ApiKey
    tokenMasked: r.token_masked,
    apiKeyMasked: r.api_key_masked,
    headers: r.headers || {},
    priority: r.priority,
    failoverEnabled: r.failover_enabled,
    // 跳过健康检查（部分中转商不支持健康检查端点）
    skip_health_check: r.skip_health_check,
    skipHealthCheck: r.skip_health_check,
    // 健康状态枚举（用于统计/展示）
    health_state: r.health_state,
    healthState: r.health_state,
    never_checked: r.never_checked,
    neverChecked: r.never_checked,
    cooldownSeconds: r.cooldown_seconds,
    timeoutSeconds: r.timeout_seconds,
    supportsCountTokens: r.supports_count_tokens,
    costMultiplier: r.cost_multiplier,
    inputCostMultiplier: r.input_cost_multiplier,
    outputCostMultiplier: r.output_cost_multiplier,
    cacheCreationCostMultiplier: r.cache_creation_cost_multiplier,
    cacheCreationCostMultiplier1h: r.cache_creation_cost_multiplier_1h,
    cacheReadCostMultiplier: r.cache_read_cost_multiplier,
    modelMultipliers: normalizeModelMultipliers(r.model_multipliers),
    enabled: r.enabled,
    createdAt: r.created_at || r.createdAt || r.CreatedAt || '',
    updatedAt: r.updated_at || r.updatedAt || r.UpdatedAt || '',
    healthy: r.healthy,
    lastCheck: r.last_check || r.lastCheck || r.LastCheck || '',
    responseTimeMs: r.response_time_ms ?? r.responseTimeMs ?? r.ResponseTimeMs,
    // 冷却状态（请求级故障转移）
    in_cooldown: r.in_cooldown,
    inCooldown: r.in_cooldown,
    cooldown_until: r.cooldown_until,
    cooldownUntil: r.cooldown_until,
    cooldown_reason: r.cooldown_reason,
    cooldownReason: r.cooldown_reason,
    // 错误信息
    last_fail_code: r.last_fail_code || 0,
    last_fail_reason: r.last_fail_reason || ''
  }));
};

/**
 * 获取单个端点记录
 * @param {string} name - 端点名称
 * @returns {Promise<Object>} - 端点记录
 */
export const getEndpointRecord = async (name) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const r = await WailsApp.GetEndpointRecord(name);
  return {
    id: r.id,
    channel: r.channel,
    credentialGroupId: r.credential_group_id ?? r.credentialGroupId ?? 0,
    credentialGroupName: r.credential_group_name || r.credentialGroupName || '',
    name: r.name,
    url: r.url,
    tokenMasked: r.token_masked,
    apiKeyMasked: r.api_key_masked,
    headers: r.headers || {},
    priority: r.priority,
    failoverEnabled: r.failover_enabled,
    // 跳过健康检查（部分中转商不支持健康检查端点）
    skip_health_check: r.skip_health_check,
    skipHealthCheck: r.skip_health_check,
    // 健康状态枚举（用于统计/展示）
    health_state: r.health_state,
    healthState: r.health_state,
    never_checked: r.never_checked,
    neverChecked: r.never_checked,
    cooldownSeconds: r.cooldown_seconds,
    timeoutSeconds: r.timeout_seconds,
    supportsCountTokens: r.supports_count_tokens,
    costMultiplier: r.cost_multiplier,
    inputCostMultiplier: r.input_cost_multiplier,
    outputCostMultiplier: r.output_cost_multiplier,
    cacheCreationCostMultiplier: r.cache_creation_cost_multiplier,
    cacheCreationCostMultiplier1h: r.cache_creation_cost_multiplier_1h,
    cacheReadCostMultiplier: r.cache_read_cost_multiplier,
    modelMultipliers: normalizeModelMultipliers(r.model_multipliers),
    enabled: r.enabled,
    createdAt: r.created_at || r.createdAt || r.CreatedAt || '',
    updatedAt: r.updated_at || r.updatedAt || r.UpdatedAt || '',
    healthy: r.healthy,
    lastCheck: r.last_check || r.lastCheck || r.LastCheck || '',
    responseTimeMs: r.response_time_ms ?? r.responseTimeMs ?? r.ResponseTimeMs,
    // 错误信息
    last_fail_code: r.last_fail_code || 0,
    last_fail_reason: r.last_fail_reason || ''
  };
};

/**
 * 创建新端点
 * @param {Object} input - 端点配置
 * @returns {Promise<void>}
 */
export const createEndpointRecord = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const skipHealthCheck = input?.skipHealthCheck ?? input?.skip_health_check ?? false;

  // 转换为后端期望的格式
  const record = {
    channel: input.channel || '',
    credential_group_id: Number(input.credentialGroupId ?? input.credential_group_id ?? 0) || 0,
    name: input.name || '',
    url: input.url || '',
    token: input.token || '',
    api_key: input.apiKey || '',
    headers: input.headers || {},
    // 传 0 时让后端自动递增，不再强制转为 1
    priority: Number.isInteger(Number(input.priority)) ? Number(input.priority) : 1,
    failover_enabled: input.failoverEnabled !== false,
    skip_health_check: !!skipHealthCheck,
    cooldown_seconds: input.cooldownSeconds ? parseInt(input.cooldownSeconds) : null,
    timeout_seconds: parseInt(input.timeoutSeconds) || 300,
    supports_count_tokens: input.supportsCountTokens || false,
    cost_multiplier: parseEndpointMultiplier(input.costMultiplier),
    input_cost_multiplier: parseEndpointMultiplier(input.inputCostMultiplier),
    output_cost_multiplier: parseEndpointMultiplier(input.outputCostMultiplier),
    cache_creation_cost_multiplier: parseEndpointMultiplier(input.cacheCreationCostMultiplier),
    cache_creation_cost_multiplier_1h: parseEndpointMultiplier(input.cacheCreationCostMultiplier1h),
    cache_read_cost_multiplier: parseEndpointMultiplier(input.cacheReadCostMultiplier),
    model_multipliers: normalizeModelMultipliers(input.modelMultipliers)
  };

  await WailsApp.CreateEndpointRecord(record);
  return { success: true };
};

/**
 * 更新端点配置
 * @param {string} name - 端点名称
 * @param {Object} input - 更新的配置
 * @returns {Promise<void>}
 */
export const updateEndpointRecord = async (name, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const skipHealthCheck = input?.skipHealthCheck ?? input?.skip_health_check ?? false;

  // 转换为后端期望的格式
  const record = {
    channel: input.channel || '',
    credential_group_id: Number(input.credentialGroupId ?? input.credential_group_id ?? 0) || 0,
    name: input.name || name,
    url: input.url || '',
    token: input.token || '',
    api_key: input.apiKey || '',
    headers: input.headers || {},
    priority: parseInt(input.priority) || 1,
    failover_enabled: input.failoverEnabled !== false,
    skip_health_check: !!skipHealthCheck,
    cooldown_seconds: input.cooldownSeconds ? parseInt(input.cooldownSeconds) : null,
    timeout_seconds: parseInt(input.timeoutSeconds) || 300,
    supports_count_tokens: input.supportsCountTokens || false,
    cost_multiplier: parseEndpointMultiplier(input.costMultiplier),
    input_cost_multiplier: parseEndpointMultiplier(input.inputCostMultiplier),
    output_cost_multiplier: parseEndpointMultiplier(input.outputCostMultiplier),
    cache_creation_cost_multiplier: parseEndpointMultiplier(input.cacheCreationCostMultiplier),
    cache_creation_cost_multiplier_1h: parseEndpointMultiplier(input.cacheCreationCostMultiplier1h),
    cache_read_cost_multiplier: parseEndpointMultiplier(input.cacheReadCostMultiplier)
  };
  if (hasOwn(input, 'modelMultipliers') || hasOwn(input, 'model_multipliers')) {
    record.model_multipliers = normalizeModelMultipliers(input.modelMultipliers || input.model_multipliers);
  }

  await WailsApp.UpdateEndpointRecord(name, record);
  return { success: true };
};

/**
 * 更新端点配置（按ID，避免同名歧义）
 * @param {number} id - 端点ID
 * @param {Object} input - 更新的配置
 * @returns {Promise<void>}
 */
export const updateEndpointRecordByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const skipHealthCheck = input?.skipHealthCheck ?? input?.skip_health_check ?? false;

  const record = {
    channel: input.channel || '',
    credential_group_id: Number(input.credentialGroupId ?? input.credential_group_id ?? 0) || 0,
    name: input.name || '',
    url: input.url || '',
    token: input.token || '',
    api_key: input.apiKey || '',
    headers: input.headers || {},
    priority: parseInt(input.priority) || 1,
    failover_enabled: input.failoverEnabled !== false,
    skip_health_check: !!skipHealthCheck,
    cooldown_seconds: input.cooldownSeconds ? parseInt(input.cooldownSeconds) : null,
    timeout_seconds: parseInt(input.timeoutSeconds) || 300,
    supports_count_tokens: input.supportsCountTokens || false,
    cost_multiplier: parseEndpointMultiplier(input.costMultiplier),
    input_cost_multiplier: parseEndpointMultiplier(input.inputCostMultiplier),
    output_cost_multiplier: parseEndpointMultiplier(input.outputCostMultiplier),
    cache_creation_cost_multiplier: parseEndpointMultiplier(input.cacheCreationCostMultiplier),
    cache_creation_cost_multiplier_1h: parseEndpointMultiplier(input.cacheCreationCostMultiplier1h),
    cache_read_cost_multiplier: parseEndpointMultiplier(input.cacheReadCostMultiplier)
  };
  if (hasOwn(input, 'modelMultipliers') || hasOwn(input, 'model_multipliers')) {
    record.model_multipliers = normalizeModelMultipliers(input.modelMultipliers || input.model_multipliers);
  }

  await WailsApp.UpdateEndpointRecordByID(id, record);
  return { success: true };
};

export const setEndpointFailoverEnabled = async (name, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetEndpointFailoverEnabled(name, !!enabled);
  return { success: true };
};

export const setEndpointFailoverEnabledByID = async (id, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetEndpointFailoverEnabledByID(id, !!enabled);
  return { success: true };
};

// 设置端点是否跳过健康检测
export const setEndpointSkipHealthCheckByID = async (id, skip) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetEndpointSkipHealthCheckByID(id, !!skip);
  return { success: true };
};

/**
 * 删除端点
 * @param {string} name - 端点名称
 * @returns {Promise<void>}
 */
export const deleteEndpointRecord = async (name) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteEndpointRecord(name);
  return { success: true };
};

export const deleteEndpointRecordByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteEndpointRecordByID(id);
  return { success: true };
};

export const createClaudeCredentialGroup = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.CreateClaudeCredentialGroup(buildClaudeCredentialGroupPayload(input));
  return mapClaudeCredentialGroup(group || {});
};

export const updateClaudeCredentialGroupByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.UpdateClaudeCredentialGroupByID(Number(id), buildClaudeCredentialGroupPayload(input));
  return mapClaudeCredentialGroup(group || {});
};

export const deleteClaudeCredentialGroupByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteClaudeCredentialGroupByID(Number(id));
  return { success: true };
};

export const setDefaultClaudeCredentialGroup = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.SetDefaultClaudeCredentialGroup(Number(id));
  return { success: true };
};

/**
 * 切换端点启用状态
 * @param {string} name - 端点名称
 * @param {boolean} enabled - 是否启用
 * @returns {Promise<void>}
 */
export const toggleEndpointRecord = async (name, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ToggleEndpointRecord(name, enabled);
  return { success: true };
};

export const toggleEndpointRecordByID = async (id, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ToggleEndpointRecordByID(id, enabled);
  return { success: true };
};

export const activateEndpointRecordByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ActivateEndpointRecordByID(id);
  return { success: true };
};

/**
 * 获取所有渠道
 * @returns {Promise<Array>} - 渠道列表 [{name, endpointCount}]
 */
export const getChannels = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const channels = await WailsApp.GetChannels();
  return (channels || []).map(c => ({
    name: c.name,
    website: c.website || '',
    priority: c.priority || 1,
    modelMapping: normalizeModelMapping(c.model_mapping || c.modelMapping),
    createdAt: c.created_at || '',
    endpointCount: c.endpoint_count,
    credentialGroups: (c.credential_groups || c.credentialGroups || []).map(mapClaudeCredentialGroup)
  }));
};

export const createChannel = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const priority = parseInt(input?.priority, 10);
  const payload = {
    name: (input?.name || '').trim(),
    website: (input?.website || '').trim(),
    // 传 0 时让后端自动递增
    priority: Number.isInteger(priority) ? priority : 0,
    model_mapping: normalizeModelMapping(input?.modelMapping || input?.model_mapping)
  };

  await WailsApp.CreateChannel(payload);
  return { success: true };
};

export const updateChannel = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const channelName = (input?.name || '').trim();
  if (!channelName) throw new Error('渠道名称不能为空');

  const priority = parseInt(input?.priority, 10);
  const payload = {
    name: channelName,
    website: (input?.website || '').trim(),
    priority: Number.isFinite(priority) && priority > 0 ? priority : 1
  };
  if (hasOwn(input, 'modelMapping') || hasOwn(input, 'model_mapping')) {
    payload.model_mapping = normalizeModelMapping(input?.modelMapping || input?.model_mapping);
  }

  await WailsApp.UpdateChannel(payload);
  return { success: true };
};

export const deleteChannel = async (name, deleteEndpoints = false) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const channelName = (name || '').trim();
  if (!channelName) throw new Error('渠道名称不能为空');

  await WailsApp.DeleteChannel(channelName, !!deleteEndpoints);
  return { success: true };
};

/**
 * 按渠道获取端点
 * @param {string} channel - 渠道名称
 * @returns {Promise<Array>} - 端点记录数组
 */
export const getEndpointsByChannel = async (channel) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const records = await WailsApp.GetEndpointsByChannel(channel);
  return (records || []).map(r => ({
    id: r.id,
    channel: r.channel,
    name: r.name,
    url: r.url,
    tokenMasked: r.token_masked,
    priority: r.priority,
    failoverEnabled: r.failover_enabled,
    enabled: r.enabled,
    healthy: r.healthy,
    responseTimeMs: r.response_time_ms ?? r.responseTimeMs ?? r.ResponseTimeMs,
    createdAt: r.created_at || r.createdAt || r.CreatedAt || '',
    updatedAt: r.updated_at || r.updatedAt || r.UpdatedAt || '',
    lastCheck: r.last_check || r.lastCheck || r.LastCheck || ''
  }));
};

// ============================================
// 模型定价管理 API (SQLite)
// ============================================

/**
 * 获取模型定价存储状态
 * @returns {Promise<Object>} - {enabled, totalCount, hasDefault}
 */
export const getModelPricingStorageStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetModelPricingStorageStatus();
  return {
    enabled: status.enabled || false,
    totalCount: status.total_count || 0,
    hasDefault: status.has_default || false
  };
};

/**
 * 获取所有模型定价
 * @returns {Promise<Array>} - 模型定价数组
 */
export const getModelPricings = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const records = await WailsApp.GetModelPricings();
  return (records || []).map(r => ({
    id: r.id,
    modelName: r.model_name,
    provider: r.provider || 'claude',
    displayName: r.display_name,
    description: r.description,
    inputPrice: r.input_price,
    outputPrice: r.output_price,
    cacheCreationPrice5m: r.cache_creation_price_5m,
    cacheCreationPrice1h: r.cache_creation_price_1h,
    cacheReadPrice: r.cache_read_price,
    isDefault: r.is_default,
    createdAt: r.created_at,
    updatedAt: r.updated_at
  }));
};

/**
 * 按 provider 获取模型定价列表
 * @param {string} provider - 提供商标识 (claude/codex)
 * @returns {Promise<Array>} - 模型定价数组
 */
export const getModelPricingsByProvider = async (provider) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const records = await WailsApp.GetModelPricingsByProvider(provider || 'claude');
  return (records || []).map(r => ({
    id: r.id,
    modelName: r.model_name,
    provider: r.provider || 'claude',
    displayName: r.display_name,
    description: r.description,
    inputPrice: r.input_price,
    outputPrice: r.output_price,
    cacheCreationPrice5m: r.cache_creation_price_5m,
    cacheCreationPrice1h: r.cache_creation_price_1h,
    cacheReadPrice: r.cache_read_price,
    isDefault: r.is_default,
    createdAt: r.created_at,
    updatedAt: r.updated_at
  }));
};

/**
 * 获取单个模型定价
 * @param {string} modelName - 模型名称
 * @returns {Promise<Object>} - 模型定价记录
 */
export const getModelPricing = async (modelName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const r = await WailsApp.GetModelPricing(modelName);
  return {
    id: r.id,
    modelName: r.model_name,
    provider: r.provider || 'claude',
    displayName: r.display_name,
    description: r.description,
    inputPrice: r.input_price,
    outputPrice: r.output_price,
    cacheCreationPrice5m: r.cache_creation_price_5m,
    cacheCreationPrice1h: r.cache_creation_price_1h,
    cacheReadPrice: r.cache_read_price,
    isDefault: r.is_default,
    createdAt: r.created_at,
    updatedAt: r.updated_at
  };
};

/**
 * 创建模型定价
 * @param {Object} input - 定价配置
 * @returns {Promise<Object>} - {success: true}
 */
export const createModelPricing = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 转换为后端期望的格式
  const record = {
    model_name: input.modelName || '',
    provider: input.provider || 'claude',
    display_name: input.displayName || '',
    description: input.description || '',
    input_price: parseFloat(input.inputPrice) || 3.0,
    output_price: parseFloat(input.outputPrice) || 15.0,
    cache_creation_price_5m: parseFloat(input.cacheCreationPrice5m) || 0,
    cache_creation_price_1h: parseFloat(input.cacheCreationPrice1h) || 0,
    cache_read_price: parseFloat(input.cacheReadPrice) || 0,
    is_default: input.isDefault || false
  };

  await WailsApp.CreateModelPricing(record);
  return { success: true };
};

/**
 * 更新模型定价
 * @param {string} modelName - 模型名称
 * @param {Object} input - 更新的配置
 * @returns {Promise<Object>} - {success: true}
 */
export const updateModelPricing = async (modelName, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 转换为后端期望的格式
  const record = {
    model_name: input.modelName || modelName,
    provider: input.provider || 'claude',
    display_name: input.displayName || '',
    description: input.description || '',
    input_price: parseFloat(input.inputPrice) || 3.0,
    output_price: parseFloat(input.outputPrice) || 15.0,
    cache_creation_price_5m: parseFloat(input.cacheCreationPrice5m) || 0,
    cache_creation_price_1h: parseFloat(input.cacheCreationPrice1h) || 0,
    cache_read_price: parseFloat(input.cacheReadPrice) || 0,
    is_default: input.isDefault || false
  };

  await WailsApp.UpdateModelPricing(modelName, record);
  return { success: true };
};

/**
 * 删除模型定价
 * @param {string} modelName - 模型名称
 * @returns {Promise<Object>} - {success: true}
 */
export const deleteModelPricing = async (modelName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteModelPricing(modelName);
  return { success: true };
};

/**
 * 设置默认模型定价
 * @param {string} modelName - 模型名称
 * @returns {Promise<Object>} - {success: true}
 */
export const setDefaultModelPricing = async (modelName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetDefaultModelPricing(modelName);
  return { success: true };
};

// ============================================
// Codex Provider 管理 API
// ============================================

/**
 * 获取 Codex Provider 存储状态
 * @returns {Promise<Object>} - {enabled, totalCount, hasActive, activeProviderId, activeProviderName}
 */
export const getCodexProviderStorageStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetCodexProviderStorageStatus();
  return {
    enabled: status.enabled || false,
    totalCount: status.total_count || 0,
    hasActive: status.has_active || false,
    activeProviderId: status.active_provider_id || 0,
    activeProviderName: status.active_provider_name || ''
  };
};

/**
 * 获取所有 Codex Provider
 * @returns {Promise<Array>} - Provider 列表
 */
export const getCodexProviders = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const providers = await WailsApp.GetCodexProviders();
  return (providers || []).map((provider) => ({
    id: provider.id,
    name: provider.name,
    providerType: provider.provider_type,
    baseURL: provider.base_url,
    apiKey: provider.api_key,
    apiKeyMasked: provider.api_key_masked,
    model: provider.model,
    reasoningEffort: provider.model_reasoning_effort || '',
    enable1MContext: provider.enable_1m_context || false,
    website: provider.website || '',
    isActive: provider.is_active,
    createdAt: provider.created_at,
    updatedAt: provider.updated_at
  }));
};

/**
 * 创建 Codex Provider
 * @param {Object} input - Provider 配置
 * @returns {Promise<Object>} - {success: true}
 */
export const createCodexProvider = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const payload = {
    name: (input?.name || '').trim(),
    provider_type: (input?.providerType || '').trim(),
    base_url: (input?.baseURL || '').trim(),
    api_key: (input?.apiKey || '').trim(),
    model: (input?.model || '').trim(),
    model_reasoning_effort: (input?.reasoningEffort || input?.modelReasoningEffort || '').trim(),
    enable_1m_context: Boolean(input?.enable1MContext || input?.enable_1m_context),
    website: (input?.website || '').trim()
  };

  await WailsApp.CreateCodexProvider(payload);
  return { success: true };
};

/**
 * 更新 Codex Provider
 * @param {number} id - Provider ID
 * @param {Object} input - Provider 配置
 * @returns {Promise<Object>} - {success: true}
 */
export const updateCodexProviderByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const payload = {
    name: (input?.name || '').trim(),
    provider_type: (input?.providerType || '').trim(),
    base_url: (input?.baseURL || '').trim(),
    api_key: (input?.apiKey || '').trim(),
    model: (input?.model || '').trim(),
    model_reasoning_effort: (input?.reasoningEffort || input?.modelReasoningEffort || '').trim(),
    enable_1m_context: Boolean(input?.enable1MContext || input?.enable_1m_context),
    website: (input?.website || '').trim()
  };

  await WailsApp.UpdateCodexProviderByID(id, payload);
  return { success: true };
};

/**
 * 删除 Codex Provider
 * @param {number} id - Provider ID
 * @returns {Promise<Object>} - {success: true}
 */
export const deleteCodexProviderByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteCodexProviderByID(id);
  return { success: true };
};

/**
 * 切换当前 Codex Provider
 * @param {number} id - Provider ID
 * @returns {Promise<Object>} - {success: true}
 */
export const activateCodexProvider = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ActivateCodexProvider(id);
  return { success: true };
};

// ============================================
// Codex 渠道-端点 管理 API（2026-05-13 新增）
// ============================================

const normalizeCodexModelMapping = normalizeModelMapping;
const normalizeCodexModelMultipliers = normalizeModelMultipliers;

const mapCodexEndpoint = (endpoint) => ({
  id: endpoint.id,
  channelId: endpoint.channel_id,
  credentialGroupId: endpoint.credential_group_id || endpoint.credentialGroupId || 0,
  credential_group_id: endpoint.credential_group_id || endpoint.credentialGroupId || 0,
  credentialGroupName: endpoint.credential_group_name || endpoint.credentialGroupName || '',
  credential_group_name: endpoint.credential_group_name || endpoint.credentialGroupName || '',
  apiKey: endpoint.api_key || endpoint.apiKey || '',
  apiKeyMasked: endpoint.api_key_masked || endpoint.apiKeyMasked || '',
  name: endpoint.name || '',
  baseURL: endpoint.base_url,
  isActive: Boolean(endpoint.is_active),
  failoverEnabled: endpoint.failover_enabled !== false,
  skipHealthCheck: Boolean(endpoint.skip_health_check),
  skip_health_check: Boolean(endpoint.skip_health_check),
  healthy: Boolean(endpoint.healthy),
  healthState: endpoint.health_state || 'unchecked',
  health_state: endpoint.health_state || 'unchecked',
  healthInScope: Boolean(endpoint.health_in_scope),
  health_in_scope: Boolean(endpoint.health_in_scope),
  responseTimeMs: endpoint.response_time_ms || 0,
  response_time_ms: endpoint.response_time_ms || 0,
  lastFailCode: endpoint.last_fail_code || 0,
  last_fail_code: endpoint.last_fail_code || 0,
  lastFailReason: endpoint.last_fail_reason || '',
  last_fail_reason: endpoint.last_fail_reason || '',
  priority: endpoint.priority || 0,
  timeoutSeconds: endpoint.timeout_seconds ?? endpoint.timeoutSeconds ?? endpoint.TimeoutSeconds ?? 0,
  timeout_seconds: endpoint.timeout_seconds ?? endpoint.timeoutSeconds ?? endpoint.TimeoutSeconds ?? 0,
  costMultiplier: endpoint.costMultiplier ?? 1.0,
  inputCostMultiplier: endpoint.inputCostMultiplier ?? 1.0,
  outputCostMultiplier: endpoint.outputCostMultiplier ?? 1.0,
  cacheReadCostMultiplier: endpoint.cacheReadCostMultiplier ?? 1.0,
  billingOutputTokenSource: endpoint.billingOutputTokenSource || endpoint.billing_output_token_source || 'output_tokens',
  modelMultipliers: normalizeCodexModelMultipliers(endpoint.modelMultipliers || endpoint.model_multipliers),
  createdAt: endpoint.created_at || endpoint.createdAt || endpoint.CreatedAt || '',
  updatedAt: endpoint.updated_at || endpoint.updatedAt || endpoint.UpdatedAt || '',
  lastCheck: endpoint.last_check || endpoint.lastCheck || endpoint.LastCheck || '',
  last_check: endpoint.last_check || endpoint.lastCheck || endpoint.LastCheck || ''
});

const mapCodexCredentialGroup = (group) => ({
  id: group.id,
  channelId: group.channel_id,
  channel_id: group.channel_id,
  name: group.name || '',
  apiKey: group.api_key || group.apiKey || '',
  apiKeyMasked: group.api_key_masked || group.apiKeyMasked || '',
  isDefault: Boolean(group.is_default ?? group.isDefault),
  is_default: Boolean(group.is_default ?? group.isDefault),
  costMultiplier: group.costMultiplier ?? 1.0,
  inputCostMultiplier: group.inputCostMultiplier ?? 1.0,
  outputCostMultiplier: group.outputCostMultiplier ?? 1.0,
  cacheReadCostMultiplier: group.cacheReadCostMultiplier ?? 1.0,
  billingOutputTokenSource: group.billingOutputTokenSource || group.billing_output_token_source || 'output_tokens',
  modelMultipliers: normalizeCodexModelMultipliers(group.modelMultipliers || group.model_multipliers),
  createdAt: group.created_at || group.createdAt || '',
  updatedAt: group.updated_at || group.updatedAt || ''
});

const mapCodexChannel = (channel) => {
  // 显式按 createdAt + id 排序，确保激活操作不改变端点显示顺序
  // （即便后端因缓存或旧二进制返回了 is_active 优先排序）
  const sortedEndpoints = (channel.endpoints || [])
    .map(mapCodexEndpoint)
    .sort((a, b) => {
      const ta = a.createdAt || '';
      const tb = b.createdAt || '';
      if (ta !== tb) return ta < tb ? -1 : 1;
      return (a.id || 0) - (b.id || 0);
    });
  return {
    id: channel.id,
    name: channel.name,
    providerType: channel.provider_type,
    apiKey: channel.api_key,
    apiKeyMasked: channel.api_key_masked,
    model: channel.model,
    modelMapping: normalizeCodexModelMapping(channel.model_mapping || channel.modelMapping),
    reasoningEffort: channel.model_reasoning_effort || '',
    enable1MContext: Boolean(channel.enable_1m_context),
    website: channel.website || '',
    isActive: Boolean(channel.is_active),
    credentialGroups: (channel.credential_groups || channel.credentialGroups || []).map(mapCodexCredentialGroup),
    endpoints: sortedEndpoints,
    createdAt: channel.created_at || channel.createdAt || channel.CreatedAt || '',
    updatedAt: channel.updated_at || channel.updatedAt || channel.UpdatedAt || ''
  };
};

const buildCodexCredentialGroupPayload = (input) => {
  const payload = {
    channel_id: input?.channelId || input?.channel_id || 0,
    name: (input?.name || '').trim(),
    api_key: (input?.apiKey || input?.api_key || '').trim(),
    is_default: Boolean(input?.isDefault || input?.is_default),
    costMultiplier: parseEndpointMultiplier(input?.costMultiplier),
    inputCostMultiplier: parseEndpointMultiplier(input?.inputCostMultiplier),
    outputCostMultiplier: parseEndpointMultiplier(input?.outputCostMultiplier),
    cacheReadCostMultiplier: parseEndpointMultiplier(input?.cacheReadCostMultiplier),
    billingOutputTokenSource: input?.billingOutputTokenSource || input?.billing_output_token_source || 'output_tokens'
  };
  const modelMultipliers = getExplicitModelMultipliers(input, normalizeCodexModelMultipliers);
  if (modelMultipliers !== undefined) {
    payload.modelMultipliers = modelMultipliers;
  }
  return payload;
};

const buildCodexChannelPayload = (input) => ({
  name: (input?.name || '').trim(),
  provider_type: (input?.providerType || 'openai-compatible').trim(),
  api_key: (input?.apiKey || '').trim(),
  model: (input?.model || '').trim(),
  model_mapping: normalizeCodexModelMapping(input?.modelMapping || input?.model_mapping),
  model_reasoning_effort: (input?.reasoningEffort || input?.modelReasoningEffort || '').trim(),
  enable_1m_context: Boolean(input?.enable1MContext || input?.enable_1m_context),
  website: (input?.website || '').trim()
});

const parseEndpointTimeout = (value) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
};

/**
 * 获取所有 Codex 渠道（含端点列表）
 */
export const getCodexChannels = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const channels = await WailsApp.GetCodexChannels();
  return (channels || []).map(mapCodexChannel);
};

/**
 * 创建 Codex 渠道（不含端点；端点单独添加）
 */
export const createCodexChannel = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.CreateCodexChannel(buildCodexChannelPayload(input));
  return { success: true };
};

/**
 * 更新 Codex 渠道
 */
export const updateCodexChannelByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.UpdateCodexChannelByID(id, buildCodexChannelPayload(input));
  return { success: true };
};

/**
 * 删除 Codex 渠道（级联删除其端点）
 */
export const deleteCodexChannelByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteCodexChannelByID(id);
  return { success: true };
};

/**
 * 激活渠道（自动激活该渠道下的某个端点并写入 config.toml）
 */
export const activateCodexChannel = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.ActivateCodexChannel(id);
  return { success: true };
};

export const createCodexCredentialGroup = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.CreateCodexCredentialGroup(buildCodexCredentialGroupPayload(input));
  return mapCodexCredentialGroup(group || {});
};

export const updateCodexCredentialGroupByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.UpdateCodexCredentialGroupByID(id, buildCodexCredentialGroupPayload(input));
  return mapCodexCredentialGroup(group || {});
};

export const deleteCodexCredentialGroupByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteCodexCredentialGroupByID(id);
  return { success: true };
};

export const setDefaultCodexCredentialGroup = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.SetDefaultCodexCredentialGroup(id);
  return { success: true };
};

/**
 * 在指定渠道/凭证分组下新增端点。端点只负责 base_url 线路配置，API Key 与计费倍率归属凭证分组。
 */
export const createCodexEndpoint = async ({ channelId, credentialGroupId, name, baseURL, failoverEnabled, skipHealthCheck, priority, timeoutSeconds }) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const payload = {
    channel_id: channelId,
    credential_group_id: credentialGroupId || 0,
    name: (name || '').trim(),
    base_url: (baseURL || '').trim()
  };
  if (failoverEnabled !== undefined) payload.failover_enabled = failoverEnabled;
  if (skipHealthCheck !== undefined) payload.skip_health_check = skipHealthCheck;
  if (priority !== undefined) payload.priority = priority;
  if (timeoutSeconds !== undefined) payload.timeout_seconds = parseEndpointTimeout(timeoutSeconds);
  await WailsApp.CreateCodexEndpoint(payload);
  return { success: true };
};

/**
 * 更新端点线路配置。未传的历史端点计费字段由后端保留，运行时不再读取端点级计费。
 */
export const updateCodexEndpointByID = async (id, { name, baseURL, credentialGroupId, failoverEnabled, skipHealthCheck, priority, timeoutSeconds }) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const payload = {
    name: (name || '').trim(),
    base_url: (baseURL || '').trim()
  };
  if (credentialGroupId !== undefined) payload.credential_group_id = credentialGroupId || 0;
  if (failoverEnabled !== undefined) payload.failover_enabled = failoverEnabled;
  if (skipHealthCheck !== undefined) payload.skip_health_check = skipHealthCheck;
  if (priority !== undefined) payload.priority = priority;
  if (timeoutSeconds !== undefined) payload.timeout_seconds = parseEndpointTimeout(timeoutSeconds);
  await WailsApp.UpdateCodexEndpointByID(id, payload);
  return { success: true };
};

/**
 * 删除端点
 */
export const deleteCodexEndpointByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteCodexEndpointByID(id);
  return { success: true };
};

/**
 * 激活端点（仅当所属渠道为当前激活渠道时才同步 config.toml 的 base_url 行）
 */
export const activateCodexEndpoint = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.ActivateCodexEndpoint(id);
  return { success: true };
};

// ============================================
// 系统设置管理 API (SQLite)
// ============================================

/**
 * 获取设置存储状态
 * @returns {Promise<Object>} - {enabled, totalCount, categoryCount, isInitialized}
 */
export const getSettingsStorageStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetSettingsStorageStatus();
  return {
    enabled: status.enabled || false,
    totalCount: status.total_count || 0,
    categoryCount: status.category_count || 0,
    isInitialized: status.is_initialized || false
  };
};

/**
 * 获取所有设置分类
 * @returns {Promise<Array>} - 分类信息数组
 */
export const getSettingCategories = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const categories = await WailsApp.GetSettingCategories();
  return (categories || []).map(c => ({
    name: c.name,
    label: c.label,
    description: c.description,
    icon: c.icon,
    order: c.order
  }));
};

/**
 * 获取所有设置
 * @returns {Promise<Array>} - 设置数组
 */
export const getAllSettings = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const settings = await WailsApp.GetAllSettings();
  return (settings || []).map(s => ({
    id: s.id,
    category: s.category,
    key: s.key,
    value: s.value,
    value_type: s.value_type,
    label: s.label,
    description: s.description,
    display_order: s.display_order,
    requires_restart: s.requires_restart,
    created_at: s.created_at,
    updated_at: s.updated_at
  }));
};

/**
 * 获取指定分类的设置
 * @param {string} category - 分类名称
 * @returns {Promise<Array>} - 设置数组
 */
export const getSettingsByCategory = async (category) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const settings = await WailsApp.GetSettingsByCategory(category);
  return (settings || []).map(s => ({
    id: s.id,
    category: s.category,
    key: s.key,
    value: s.value,
    value_type: s.value_type,
    label: s.label,
    description: s.description,
    display_order: s.display_order,
    requires_restart: s.requires_restart,
    created_at: s.created_at,
    updated_at: s.updated_at
  }));
};

/**
 * 获取单个设置
 * @param {string} category - 分类名称
 * @param {string} key - 设置键
 * @returns {Promise<Object>} - 设置记录
 */
export const getSetting = async (category, key) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const s = await WailsApp.GetSetting(category, key);
  return {
    id: s.id,
    category: s.category,
    key: s.key,
    value: s.value,
    value_type: s.value_type,
    label: s.label,
    description: s.description,
    display_order: s.display_order,
    requires_restart: s.requires_restart,
    created_at: s.created_at,
    updated_at: s.updated_at
  };
};

/**
 * 更新单个设置
 * @param {string} category - 分类名称
 * @param {string} key - 设置键
 * @param {string} value - 新值
 * @returns {Promise<Object>} - {success: true}
 */
export const updateSetting = async (category, key, value) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.UpdateSetting({
    category,
    key,
    value
  });
  return { success: true };
};

/**
 * 批量更新设置
 * @param {Object|Array} input - 设置对象 {settings: [...]} 或设置数组 [{category, key, value}]
 * @returns {Promise<Object>} - {success: true}
 */
export const batchUpdateSettings = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 兼容两种调用方式：对象 {settings: [...]} 或数组 [...]
  const settingsArray = Array.isArray(input) ? input : (input.settings || []);

  await WailsApp.BatchUpdateSettings({
    settings: settingsArray.map(s => ({
      category: s.category,
      key: s.key,
      value: s.value
    }))
  });
  return { success: true };
};

/**
 * 重置分类设置为默认值
 * @param {string} category - 分类名称
 * @returns {Promise<Object>} - {success: true}
 */
export const resetCategorySettings = async (category) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ResetCategorySettings(category);
  return { success: true };
};

/**
 * 获取端口信息
 * @returns {Promise<Object>} - {preferred_port, actual_port, is_default, was_occupied}
 */
export const getPortInfo = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const info = await WailsApp.GetPortInfo();
  return {
    preferred_port: info.preferred_port,
    actual_port: info.actual_port,
    is_default: info.is_default,
    was_occupied: info.was_occupied
  };
};

/**
 * 更新首选端口（需要重启生效）
 * @param {number} port - 新端口号
 * @returns {Promise<Object>} - {success: true}
 */
export const updatePreferredPort = async (port) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.UpdatePreferredPort(port);
  return { success: true };
};

/**
 * 检查端口是否可用
 * @param {number} port - 端口号
 * @returns {Promise<boolean>} - 是否可用
 */
export const checkPortAvailable = async (port) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.CheckPortAvailable(port);
};

// ============================================
// 挂起请求计数 API
// ============================================

/**
 * 获取当前挂起等待端点恢复的请求数量
 * 用于渠道管理页面显示等待提示
 * @returns {Promise<number>} - 挂起请求数量
 */
export const getSuspendedRequestsCount = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetSuspendedRequestsCount();
};

/**
 * 获取当前挂起请求的端点计数
 * @returns {Promise<Object>} - { [endpointName]: count }
 */
export const getSuspendedEndpointCounts = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetSuspendedEndpointCounts();
};

/**
 * 获取等待用户操作的挂起请求数量（达到最大循环轮数）
 * @returns {Promise<number>} - 挂起请求数量
 */
export const getManualRetryRequestsCount = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetManualRetryRequestsCount();
};

/**
 * 获取等待用户操作的渠道计数
 * @returns {Promise<Object>} - { [channelName]: count }
 */
export const getManualRetryChannelCounts = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetManualRetryChannelCounts();
};

// ============================================
// 端口冲突处理 API
// ============================================

/**
 * 获取端口冲突状态
 * @returns {Promise<Object>} - {has_conflict, conflict_port, error_message, proxy_address, proxy_running}
 */
export const getPortConflictStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetPortConflictStatus();
};

/**
 * 解决端口冲突
 * @param {string} action - 处理动作: "retry" (重试首选端口) | "switch" (切换到可用端口)
 * @returns {Promise<Object>} - {success, proxy_address, error_message}
 */
export const resolvePortConflict = async (action) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.ResolvePortConflict(action);
};

// ============================================================================
// Codex 挂起请求相关
// ============================================================================

/**
 * 获取当前 Codex 挂起请求数（用于前端展示警示色与 Toast 提示）
 */
export const getCodexSuspendedCount = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const count = await WailsApp.GetCodexSuspendedCount();
  return Number(count) || 0;
};

// ============================================================================
// Codex 全局配置（~/.codex/config.toml 中的 model / reasoning_effort / 1M）
// base_url 由后端自动同步自「设置 → 服务端口」，前端不可编辑
// ============================================================================

/**
 * 读取 Codex 全局配置
 * @returns {Promise<{model:string, reasoning_effort:string, enable_1m_context:boolean}>}
 */
export const getCodexGlobalConfig = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const info = await WailsApp.GetCodexGlobalConfig();
  return {
    model: info?.model || '',
    reasoning_effort: info?.reasoning_effort || '',
    enable_1m_context: Boolean(info?.enable_1m_context),
    global_timeout_seconds: info?.global_timeout_seconds || info?.globalTimeoutSeconds || info?.GlobalTimeoutSeconds || 300
  };
};

/**
 * 更新 Codex 全局配置（直接写 ~/.codex/config.toml）
 * base_url 由后端自动取 server.host:port + /v1
 * @param {{model:string, reasoning_effort:string, enable_1m_context:boolean}} input
 */
export const updateCodexGlobalConfig = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.UpdateCodexGlobalConfig({
    model: input?.model || '',
    reasoning_effort: input?.reasoning_effort || '',
    enable_1m_context: Boolean(input?.enable_1m_context)
  });
};
