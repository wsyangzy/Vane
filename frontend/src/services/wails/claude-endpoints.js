import { parseEndpointMultiplier, parseNonNegativeNumber, parseEndpointPriority, normalizeModelMapping, normalizeModelMultipliers, hasOwn, getExplicitModelMultipliers, mapSubscription } from './mapping.js';
import { WailsApp, initWails } from './connection.js';

const mapClaudeCredentialGroup = (group = {}) => ({
  enabled: group.enabled !== false,
  authMode: group.auth_mode || group.authMode || 'group_credential',
  subscription: mapSubscription(group.subscription),
  localKeyConfigured: Boolean(group.local_key_configured ?? group.localKeyConfigured),
  officialVerification: group.official_verification || group.officialVerification || 'not_verified',
  subscriptionStatus: (group.subscription_status || group.subscriptionStatus || []).map((pause) => ({
    model: pause.model, state: pause.state, reason: pause.reason, source: pause.source,
    observedAt: pause.observed_at ?? pause.observedAt,
    resumeNotBefore: pause.resume_not_before ?? pause.resumeNotBefore,
    resetKnown: Boolean(pause.reset_known ?? pause.resetKnown)
  })),
  id: group.id,
  channel: group.channel || '',
  name: group.name || '',
  token: group.token || '',
  apiKey: group.api_key || group.apiKey || '',
  tokenMasked: group.token_masked || group.tokenMasked || '',
  apiKeyMasked: group.api_key_masked || group.apiKeyMasked || '',
  isDefault: Boolean(group.is_default ?? group.isDefault),
  dailyCostLimitUSD: parseNonNegativeNumber(group.daily_cost_limit_usd ?? group.dailyCostLimitUSD),
  modelPolicy: group.model_policy || group.modelPolicy || 'inherit',
  modelMapping: normalizeModelMapping(group.model_mapping || group.modelMapping),
  supportedModels: Array.isArray(group.supported_models || group.supportedModels)
    ? (group.supported_models || group.supportedModels).map((model) => String(model || '').trim()).filter(Boolean)
    : [],
  costMultiplier: group.cost_multiplier ?? group.costMultiplier ?? 1.0,
  inputCostMultiplier: group.input_cost_multiplier ?? group.inputCostMultiplier ?? 1.0,
  outputCostMultiplier: group.output_cost_multiplier ?? group.outputCostMultiplier ?? 1.0,
  cacheCreationCostMultiplier: group.cache_creation_cost_multiplier ?? group.cacheCreationCostMultiplier ?? 1.0,
  cacheCreationCostMultiplier1h: group.cache_creation_cost_multiplier_1h ?? group.cacheCreationCostMultiplier1h ?? 1.0,
  cacheReadCostMultiplier: group.cache_read_cost_multiplier ?? group.cacheReadCostMultiplier ?? 1.0,
  longContextCostMultiplier: group.long_context_cost_multiplier ?? group.longContextCostMultiplier ?? 1.0,
  longContextInputCostMultiplier: group.long_context_input_cost_multiplier ?? group.longContextInputCostMultiplier ?? 1.0,
  longContextOutputCostMultiplier: group.long_context_output_cost_multiplier ?? group.longContextOutputCostMultiplier ?? 1.0,
  longContextCacheCreationCostMultiplier: group.long_context_cache_creation_cost_multiplier ?? group.longContextCacheCreationCostMultiplier ?? 1.0,
  longContextCacheReadCostMultiplier: group.long_context_cache_read_cost_multiplier ?? group.longContextCacheReadCostMultiplier ?? 1.0,
  disableLongContextPricing: Boolean(group.disable_long_context_pricing ?? group.disableLongContextPricing),
  modelMultipliers: normalizeModelMultipliers(group.model_multipliers || group.modelMultipliers),
  endpointCount: group.endpoint_count ?? group.endpointCount ?? 0,
  createdAt: group.created_at || group.createdAt || '',
  updatedAt: group.updated_at || group.updatedAt || ''
});

const buildClaudeCredentialGroupPayload = (input = {}) => {
  const payload = {
    auth_mode: input.authMode || input.auth_mode || '',
    channel: (input.channel || '').trim(),
    name: (input.name || '').trim(),
    token: (input.token || '').trim(),
    api_key: (input.apiKey || input.api_key || '').trim(),
    is_default: Boolean(input.isDefault || input.is_default),
    daily_cost_limit_usd: parseNonNegativeNumber(input.dailyCostLimitUSD ?? input.daily_cost_limit_usd),
    model_policy: input.modelPolicy || input.model_policy || 'inherit',
    model_mapping: normalizeModelMapping(input.modelMapping || input.model_mapping),
    supported_models: Array.isArray(input.supportedModels || input.supported_models)
      ? (input.supportedModels || input.supported_models).map((model) => String(model || '').trim()).filter(Boolean)
      : String(input.supportedModelsText || '').split(',').map((model) => model.trim()).filter(Boolean),
    cost_multiplier: parseEndpointMultiplier(input.costMultiplier),
    input_cost_multiplier: parseEndpointMultiplier(input.inputCostMultiplier),
    output_cost_multiplier: parseEndpointMultiplier(input.outputCostMultiplier),
    cache_creation_cost_multiplier: parseEndpointMultiplier(input.cacheCreationCostMultiplier),
    cache_creation_cost_multiplier_1h: parseEndpointMultiplier(input.cacheCreationCostMultiplier1h),
    cache_read_cost_multiplier: parseEndpointMultiplier(input.cacheReadCostMultiplier),
    long_context_cost_multiplier: parseEndpointMultiplier(input.longContextCostMultiplier),
    long_context_input_cost_multiplier: parseEndpointMultiplier(input.longContextInputCostMultiplier),
    long_context_output_cost_multiplier: parseEndpointMultiplier(input.longContextOutputCostMultiplier),
    long_context_cache_creation_cost_multiplier: parseEndpointMultiplier(input.longContextCacheCreationCostMultiplier),
    long_context_cache_read_cost_multiplier: parseEndpointMultiplier(input.longContextCacheReadCostMultiplier),
    disable_long_context_pricing: Boolean(input.disableLongContextPricing)
  };
  if (input.subscription) {
    const binding = input.subscription;
    payload.subscription = {
      account_id: binding.accountId || '',
      version: Number(binding.version || 0), enabled: Boolean(binding.enabled),
      key_hash: binding.keyHash || '', action: binding.action || '',
      backup_targets: (binding.backupTargets || []).map((target) => ({
        channel_id: Number(target.channelId), group_id: Number(target.groupId),
        upstream_model: String(target.upstreamModel || '').trim()
      }))
    };
  }
  const modelMultipliers = getExplicitModelMultipliers(input);
  if (modelMultipliers !== undefined) {
    payload.model_multipliers = modelMultipliers;
  }
  return payload;
};

// ============================================
// 端点管理 API
// ============================================

const getEndpoints = async () => {
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

const setEndpointPriority = async (endpointName, priority) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetEndpointPriority(endpointName, priority);
  return { success: true };
};

const triggerHealthCheck = async (endpointName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.TriggerHealthCheck(endpointName);
  return { success: true, healthy: true };
};

const batchHealthCheckAll = async () => {
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

const getKeysOverview = async () => {
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

const switchKey = async (endpointName, keyType, index) => {
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
// 端点存储管理 API (SQLite)
// ============================================

/**
 * 获取端点存储状态
 * @returns {Promise<Object>} - {enabled, storage_type, total_count, enabled_count}
 */
const getEndpointStorageStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetEndpointStorageStatus();
  return {
    enabled: status.enabled || false,
    storageType: status.storage_type || 'sqlite',
    totalCount: status.total_count || 0,
    enabledCount: status.enabled_count || 0
  };
};

/**
 * 获取所有端点记录（SQLite 存储）
 * @returns {Promise<Array>} - 端点记录数组
 */
const getEndpointRecords = async () => {
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
const getEndpointRecord = async (name) => {
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
const createEndpointRecord = async (input) => {
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
    // 未传时让后端自动递增；显式 0 是最高优先级
    priority: parseEndpointPriority(input.priority, -1),
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
const updateEndpointRecord = async (name, input) => {
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
    priority: parseEndpointPriority(input.priority),
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
const updateEndpointRecordByID = async (id, input) => {
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
    priority: parseEndpointPriority(input.priority),
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

const setEndpointFailoverEnabled = async (name, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetEndpointFailoverEnabled(name, !!enabled);
  return { success: true };
};

const setEndpointFailoverEnabledByID = async (id, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetEndpointFailoverEnabledByID(id, !!enabled);
  return { success: true };
};

// 设置端点是否跳过健康检测
const setEndpointSkipHealthCheckByID = async (id, skip) => {
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
const deleteEndpointRecord = async (name) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteEndpointRecord(name);
  return { success: true };
};

const deleteEndpointRecordByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteEndpointRecordByID(id);
  return { success: true };
};

const createClaudeCredentialGroup = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.CreateClaudeCredentialGroup(buildClaudeCredentialGroupPayload(input));
  return mapClaudeCredentialGroup(group || {});
};

const updateClaudeCredentialGroupByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.UpdateClaudeCredentialGroupByID(Number(id), buildClaudeCredentialGroupPayload(input));
  return mapClaudeCredentialGroup(group || {});
};

const deleteClaudeCredentialGroupByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteClaudeCredentialGroupByID(Number(id));
  return { success: true };
};

const setDefaultClaudeCredentialGroup = async (id) => {
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
const toggleEndpointRecord = async (name, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ToggleEndpointRecord(name, enabled);
  return { success: true };
};

const toggleEndpointRecordByID = async (id, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ToggleEndpointRecordByID(id, enabled);
  return { success: true };
};

const activateEndpointRecordByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ActivateEndpointRecordByID(id);
  return { success: true };
};

export { mapClaudeCredentialGroup, buildClaudeCredentialGroupPayload, getEndpoints, setEndpointPriority, triggerHealthCheck, batchHealthCheckAll, getKeysOverview, switchKey, getEndpointStorageStatus, getEndpointRecords, getEndpointRecord, createEndpointRecord, updateEndpointRecord, updateEndpointRecordByID, setEndpointFailoverEnabled, setEndpointFailoverEnabledByID, setEndpointSkipHealthCheckByID, deleteEndpointRecord, deleteEndpointRecordByID, createClaudeCredentialGroup, updateClaudeCredentialGroupByID, deleteClaudeCredentialGroupByID, setDefaultClaudeCredentialGroup, toggleEndpointRecord, toggleEndpointRecordByID, activateEndpointRecordByID };
