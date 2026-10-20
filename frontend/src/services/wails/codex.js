import { parseEndpointMultiplier, parseNonNegativeNumber, normalizeModelMapping, normalizeModelMultipliers, hasOwn, getExplicitModelMultipliers, mapSubscription } from './mapping.js';
import { normalizeChannelPrivacyLevel } from './privacy.js';
import { WailsApp, initWails } from './connection.js';
import { mapNonStreamRoute } from './routing.js';

// ============================================
// Codex Provider 管理 API
// ============================================

/**
 * 获取 Codex Provider 存储状态
 * @returns {Promise<Object>} - {enabled, totalCount, hasActive, activeProviderId, activeProviderName}
 */
const getCodexProviderStorageStatus = async () => {
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
const getCodexProviders = async () => {
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
const createCodexProvider = async (input) => {
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
const updateCodexProviderByID = async (id, input) => {
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
const deleteCodexProviderByID = async (id) => {
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
const activateCodexProvider = async (id) => {
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


const normalizeCodexServiceTier = (value) => {
  const tier = String(value || '').trim().toLowerCase();
  if (tier === 'priority') return 'fast';
  if (tier === 'default' || tier === 'standard_only') return 'standard';
  return tier;
};

const normalizeCodexBillingRules = (records) => (Array.isArray(records) ? records : []).map((record) => {
  const multiplier = (camel, snake) => {
    const value = parseEndpointMultiplier(record?.[camel] ?? record?.[snake], 1);
    return value > 0 ? value : 1;
  };
  return {
    model: String(record?.model || '').trim(),
    serviceTier: normalizeCodexServiceTier(record?.serviceTier ?? record?.service_tier),
    context: String(record?.context || '').trim().toLowerCase(),
    mode: record?.mode === 'fixed' ? 'fixed' : 'multiplier',
    costMultiplier: multiplier('costMultiplier', 'cost_multiplier'),
    inputCostMultiplier: multiplier('inputCostMultiplier', 'input_cost_multiplier'),
    outputCostMultiplier: multiplier('outputCostMultiplier', 'output_cost_multiplier'),
    cacheCreationCostMultiplier: multiplier('cacheCreationCostMultiplier', 'cache_creation_cost_multiplier'),
    cacheReadCostMultiplier: multiplier('cacheReadCostMultiplier', 'cache_read_cost_multiplier'),
    inputPrice: parseNonNegativeNumber(record?.inputPrice ?? record?.input_price),
    outputPrice: parseNonNegativeNumber(record?.outputPrice ?? record?.output_price),
    cacheCreationPrice: parseNonNegativeNumber(record?.cacheCreationPrice ?? record?.cache_creation_price),
    cacheReadPrice: parseNonNegativeNumber(record?.cacheReadPrice ?? record?.cache_read_price)
  };
});

const buildCodexBillingRulePayload = (records) => normalizeCodexBillingRules(records).map((record) => ({
  model: record.model,
  service_tier: record.serviceTier,
  context: record.context,
  mode: record.mode,
  cost_multiplier: record.costMultiplier,
  input_cost_multiplier: record.inputCostMultiplier,
  output_cost_multiplier: record.outputCostMultiplier,
  cache_creation_cost_multiplier: record.cacheCreationCostMultiplier,
  cache_read_cost_multiplier: record.cacheReadCostMultiplier,
  input_price: record.inputPrice,
  output_price: record.outputPrice,
  cache_creation_price: record.cacheCreationPrice,
  cache_read_price: record.cacheReadPrice
}));

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
  authMode: group.auth_mode || group.authMode || 'group_credential',
  subscription: mapSubscription(group.subscription),
  localKeyConfigured: Boolean(group.local_key_configured ?? group.localKeyConfigured),
  officialVerification: group.official_verification || group.officialVerification || 'not_verified',
  subscriptionStatus: (group.subscription_status || group.subscriptionStatus || []).map((pause) => ({
    model: pause.model, state: pause.state, reason: pause.reason, source: pause.source,
    observedAt: pause.observed_at ?? pause.observedAt, resumeNotBefore: pause.resume_not_before ?? pause.resumeNotBefore,
    resetKnown: Boolean(pause.reset_known ?? pause.resetKnown)
  })),
  id: group.id,
  channelId: group.channel_id,
  channel_id: group.channel_id,
  name: group.name || '',
  apiKey: group.api_key || group.apiKey || '',
  apiKeyMasked: group.api_key_masked || group.apiKeyMasked || '',
  isDefault: Boolean(group.is_default ?? group.isDefault),
  is_default: Boolean(group.is_default ?? group.isDefault),
  dailyCostLimitUSD: parseNonNegativeNumber(group.daily_cost_limit_usd ?? group.dailyCostLimitUSD),
  modelPolicy: group.modelPolicy || group.model_policy || 'inherit',
  modelMapping: normalizeCodexModelMapping(group.modelMapping || group.model_mapping),
  supportedModels: Array.isArray(group.supportedModels || group.supported_models)
    ? (group.supportedModels || group.supported_models).map((model) => String(model || '').trim()).filter(Boolean)
    : [],
  costMultiplier: group.costMultiplier ?? 1.0,
  inputCostMultiplier: group.inputCostMultiplier ?? 1.0,
  outputCostMultiplier: group.outputCostMultiplier ?? 1.0,
  cacheCreationCostMultiplier: group.cacheCreationCostMultiplier ?? 1.0,
  cacheReadCostMultiplier: group.cacheReadCostMultiplier ?? 1.0,
  longContextCostMultiplier: group.longContextCostMultiplier ?? 1.0,
  longContextInputCostMultiplier: group.longContextInputCostMultiplier ?? 1.0,
  longContextOutputCostMultiplier: group.longContextOutputCostMultiplier ?? 1.0,
  longContextCacheCreationCostMultiplier: group.longContextCacheCreationCostMultiplier ?? 1.0,
  longContextCacheReadCostMultiplier: group.longContextCacheReadCostMultiplier ?? 1.0,
  disableLongContextPricing: Boolean(group.disableLongContextPricing),
  billingOutputTokenSource: group.billingOutputTokenSource || group.billing_output_token_source || 'output_tokens',
  modelMultipliers: normalizeCodexModelMultipliers(group.modelMultipliers || group.model_multipliers),
	 billingRules: normalizeCodexBillingRules(group.billingRules || group.billing_rules),
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
    priority: channel.priority ?? 0,
    failoverEnabled: channel.failover_enabled !== false,
    isActive: Boolean(channel.is_active),
    nonStreamRoute: mapNonStreamRoute(channel.non_stream_route || channel.nonStreamRoute),
    privacyLevel: normalizeChannelPrivacyLevel(channel.privacy_level ?? channel.privacyLevel),
    credentialGroups: (channel.credential_groups || channel.credentialGroups || []).map(mapCodexCredentialGroup),
    endpoints: sortedEndpoints,
    createdAt: channel.created_at || channel.createdAt || channel.CreatedAt || '',
    updatedAt: channel.updated_at || channel.updatedAt || channel.UpdatedAt || ''
  };
};

const buildCodexCredentialGroupPayload = (input) => {
  const payload = {
    auth_mode: input?.authMode || input?.auth_mode || '',
    channel_id: input?.channelId || input?.channel_id || 0,
    name: (input?.name || '').trim(),
    api_key: (input?.apiKey || input?.api_key || '').trim(),
    is_default: Boolean(input?.isDefault || input?.is_default),
    daily_cost_limit_usd: parseNonNegativeNumber(input?.dailyCostLimitUSD ?? input?.daily_cost_limit_usd),
    modelPolicy: input?.modelPolicy || input?.model_policy || 'inherit',
    modelMapping: normalizeCodexModelMapping(input?.modelMapping || input?.model_mapping),
    supportedModels: Array.isArray(input?.supportedModels || input?.supported_models)
      ? (input?.supportedModels || input?.supported_models).map((model) => String(model || '').trim()).filter(Boolean)
      : String(input?.supportedModelsText || '').split(',').map((model) => model.trim()).filter(Boolean),
    costMultiplier: parseEndpointMultiplier(input?.costMultiplier),
    inputCostMultiplier: parseEndpointMultiplier(input?.inputCostMultiplier),
    outputCostMultiplier: parseEndpointMultiplier(input?.outputCostMultiplier),
    cacheCreationCostMultiplier: parseEndpointMultiplier(input?.cacheCreationCostMultiplier),
    cacheReadCostMultiplier: parseEndpointMultiplier(input?.cacheReadCostMultiplier),
    longContextCostMultiplier: parseEndpointMultiplier(input?.longContextCostMultiplier),
    longContextInputCostMultiplier: parseEndpointMultiplier(input?.longContextInputCostMultiplier),
    longContextOutputCostMultiplier: parseEndpointMultiplier(input?.longContextOutputCostMultiplier),
    longContextCacheCreationCostMultiplier: parseEndpointMultiplier(input?.longContextCacheCreationCostMultiplier),
    longContextCacheReadCostMultiplier: parseEndpointMultiplier(input?.longContextCacheReadCostMultiplier),
    disableLongContextPricing: Boolean(input?.disableLongContextPricing),
    billingOutputTokenSource: input?.billingOutputTokenSource || input?.billing_output_token_source || 'output_tokens'
  };
  if (input?.subscription) {
    const binding = input.subscription;
    payload.subscription = {
      account_id: binding.accountId || '', version: Number(binding.version || 0), enabled: Boolean(binding.enabled),
      key_hash: binding.keyHash || '', action: binding.action || '',
      backup_targets: (binding.backupTargets || []).map((target) => ({
        channel_id: Number(target.channelId), group_id: Number(target.groupId), upstream_model: String(target.upstreamModel || '').trim()
      }))
    };
  }
  const modelMultipliers = getExplicitModelMultipliers(input, normalizeCodexModelMultipliers);
  if (modelMultipliers !== undefined) {
    payload.modelMultipliers = modelMultipliers;
  }
	if (hasOwn(input, 'billingRules') || hasOwn(input, 'billing_rules')) {
	  payload.billingRules = buildCodexBillingRulePayload(input?.billingRules || input?.billing_rules);
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
  website: (input?.website || '').trim(),
  priority: Number.parseInt(input?.priority, 10) || 0,
  failover_enabled: input?.failoverEnabled !== false
});

const parseEndpointTimeout = (value) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
};

/**
 * 获取所有 Codex 渠道（含端点列表）
 */
const getCodexChannels = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const channels = await WailsApp.GetCodexChannels();
  return (channels || []).map(mapCodexChannel);
};

const getNextAvailableCodexChannel = async (excludeId) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const channel = await WailsApp.GetNextAvailableCodexChannel(Number(excludeId));
  return {
    id: Number(channel?.id ?? channel?.ID ?? 0),
    name: channel?.name ?? channel?.Name ?? ''
  };
};

/**
 * 创建 Codex 渠道（不含端点；端点单独添加）
 */
const createCodexChannel = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.CreateCodexChannel(buildCodexChannelPayload(input));
  return { success: true };
};

/**
 * 更新 Codex 渠道
 */
const updateCodexChannelByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.UpdateCodexChannelByID(id, buildCodexChannelPayload(input));
  return { success: true };
};

const setCodexChannelFailoverEnabled = async (id, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.SetCodexChannelFailoverEnabled(id, Boolean(enabled));
  return { success: true };
};

/**
 * 删除 Codex 渠道（级联删除其端点）
 */
const deleteCodexChannelByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteCodexChannelByID(id);
  return { success: true };
};

/**
 * 激活渠道（自动激活该渠道下的某个端点并写入 config.toml）
 */
const activateCodexChannel = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.ActivateCodexChannel(id);
  return { success: true };
};

const createCodexCredentialGroup = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.CreateCodexCredentialGroup(buildCodexCredentialGroupPayload(input));
  return mapCodexCredentialGroup(group || {});
};

const updateCodexCredentialGroupByID = async (id, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const group = await WailsApp.UpdateCodexCredentialGroupByID(id, buildCodexCredentialGroupPayload(input));
  return mapCodexCredentialGroup(group || {});
};

const deleteCodexCredentialGroupByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteCodexCredentialGroupByID(id);
  return { success: true };
};

const setDefaultCodexCredentialGroup = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.SetDefaultCodexCredentialGroup(id);
  return { success: true };
};

/**
 * 在指定渠道/凭证分组下新增端点。端点只负责 base_url 线路配置，API Key 与计费倍率归属凭证分组。
 */
const createCodexEndpoint = async ({ channelId, credentialGroupId, name, baseURL, failoverEnabled, skipHealthCheck, priority, timeoutSeconds }) => {
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
const updateCodexEndpointByID = async (id, { name, baseURL, credentialGroupId, failoverEnabled, skipHealthCheck, priority, timeoutSeconds }) => {
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
const deleteCodexEndpointByID = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteCodexEndpointByID(id);
  return { success: true };
};

/**
 * 激活端点（仅当所属渠道为当前激活渠道时才同步 config.toml 的 base_url 行）
 */
const activateCodexEndpoint = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.ActivateCodexEndpoint(id);
  return { success: true };
};

// ============================================================================
// Codex 挂起请求相关
// ============================================================================

/**
 * 获取当前 Codex 挂起请求数（用于前端展示警示色与 Toast 提示）
 */
const getCodexSuspendedCount = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const count = await WailsApp.GetCodexSuspendedCount();
  return Number(count) || 0;
};

// ============================================================================
// Codex 全局配置（Relay 更新 model / 1M；reasoning_effort 仅只读兼容）
// base_url 由后端自动同步自「设置 → 服务端口」，前端不可编辑
// ============================================================================

/**
 * 读取 Codex 全局配置
 * @returns {Promise<{model:string, reasoning_effort:string, enable_1m_context:boolean}>} reasoning_effort 仅兼容读取
 */
const getCodexGlobalConfig = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const info = await WailsApp.GetCodexGlobalConfig();
  return {
    model: info?.model || '',
    reasoning_effort: info?.reasoning_effort || info?.reasoningEffort || '',
    enable_1m_context: Boolean(info?.enable_1m_context ?? info?.enable1MContext),
    effective_model: info?.effective_model || info?.effectiveModel || info?.model || '',
    effective_reasoning_effort: info?.effective_reasoning_effort || info?.effectiveReasoningEffort || info?.reasoning_effort || info?.reasoningEffort || '',
    effective_enable_1m_context: Boolean(info?.effective_enable_1m_context ?? info?.effectiveEnable1MContext ?? info?.enable_1m_context ?? info?.enable1MContext),
    global_timeout_seconds: info?.global_timeout_seconds || info?.globalTimeoutSeconds || info?.GlobalTimeoutSeconds || 300
  };
};

/**
 * 更新 Codex 全局配置（直接写 ~/.codex/config.toml；后端忽略 reasoning_effort）
 * base_url 由后端自动取 server.host:port + /v1
 * @param {{model:string, reasoning_effort?:string, enable_1m_context:boolean}} input reasoning_effort 为兼容字段
 */
const updateCodexGlobalConfig = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.UpdateCodexGlobalConfig({
    model: input?.model || '',
    reasoning_effort: input?.reasoning_effort || input?.reasoningEffort || '',
    enable_1m_context: Boolean(input?.enable_1m_context ?? input?.enable1MContext)
  });
};

export { getCodexProviderStorageStatus, getCodexProviders, createCodexProvider, updateCodexProviderByID, deleteCodexProviderByID, activateCodexProvider, mapCodexCredentialGroup, buildCodexCredentialGroupPayload, getCodexChannels, getNextAvailableCodexChannel, createCodexChannel, updateCodexChannelByID, setCodexChannelFailoverEnabled, deleteCodexChannelByID, activateCodexChannel, createCodexCredentialGroup, updateCodexCredentialGroupByID, deleteCodexCredentialGroupByID, setDefaultCodexCredentialGroup, createCodexEndpoint, updateCodexEndpointByID, deleteCodexEndpointByID, activateCodexEndpoint, getCodexSuspendedCount, getCodexGlobalConfig, updateCodexGlobalConfig };
