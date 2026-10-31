import { isSubscriptionGroup } from './utils/subscription.js';
import { buildInitialModelMappingRows } from '@/features/providers/shared/routing/modelMapping.js';
import { buildInitialModelMultiplierRows } from '@/features/providers/shared/billing/modelMultiplierRows.js';
import { formatMultiplierInputValue, parseMultiplierExpression } from '@/shared/lib/multiplier.js';

const CHANNEL_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'mapping', label: '模型映射' }
];

// 模型路由与非流改道都按渠道 ID 保存，只在编辑已入库的渠道时提供
const CHANNEL_EDIT_FORM_TABS = [
  ...CHANNEL_FORM_TABS,
  { key: 'modelRoutes', label: '模型路由' },
  { key: 'nonStream', label: '非流改道' }
];

const CREDENTIAL_GROUP_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'model', label: '模型策略' },
  { key: 'billing', label: '成本倍率' }
];

const CREDENTIAL_GROUP_CREATE_FORM_TABS = [
  ...CREDENTIAL_GROUP_FORM_TABS,
  { key: 'copy', label: '配置端点' }
];

const CLAUDE_CREDENTIAL_SECRET_TABS = [
  { key: 'token', label: 'Token' },
  { key: 'apiKey', label: 'API Key' }
];

const getInitialClaudeCredentialSecretType = (group) => {
  const hasToken = Boolean(group?.token || group?.tokenMasked || group?.token_masked);
  const hasApiKey = Boolean(group?.apiKey || group?.api_key || group?.apiKeyMasked || group?.api_key_masked);
  return !hasToken && hasApiKey ? 'apiKey' : 'token';
};

const hasClaudeCredential = (group) => (isSubscriptionGroup(group) && Boolean(group.subscription)) || [
  group?.token,
  group?.tokenMasked,
  group?.token_masked,
  group?.apiKey,
  group?.api_key,
  group?.apiKeyMasked,
  group?.api_key_masked
].some((value) => (typeof value === 'string' ? value.trim() !== '' : Boolean(value)));

// priceKey 指向模型定价中对应的单价字段，供倍率弹窗展示「基础单价 → 实际单价」对照。
// 无 priceKey 的是总倍率，作用于全部分项。
const STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS = [
  { name: 'costMultiplier', label: '总倍率' },
  { name: 'inputCostMultiplier', label: '输入倍率', priceKey: 'inputPrice' },
  { name: 'outputCostMultiplier', label: '输出倍率', priceKey: 'outputPrice' },
  { name: 'cacheReadCostMultiplier', label: '缓存读取倍率', priceKey: 'cacheReadPrice' },
  { name: 'cacheCreationCostMultiplier', label: '5分钟缓存创建倍率', priceKey: 'cacheCreationPrice5m' },
  { name: 'cacheCreationCostMultiplier1h', label: '1小时缓存创建倍率', priceKey: 'cacheCreationPrice1h' }
];

// Claude 侧不提供 Long context 计费：分组倍率仅覆盖普通定价。
// 后端字段与历史值仍保留（见 createFormState 中的原样回传），仅关闭入口与计费闸门。
const CREDENTIAL_GROUP_MULTIPLIER_FIELDS = [
  ...STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS
];

const isValidMultiplierExpression = (value) => {
  const parsed = parseMultiplierExpression(value, Number.NaN);
  return Number.isFinite(parsed) && parsed >= 0;
};

const getClaudeCredentialGroups = (channelMeta) => (
  Array.isArray(channelMeta?.credentialGroups)
    ? channelMeta.credentialGroups
    : Array.isArray(channelMeta?.credential_groups)
      ? channelMeta.credential_groups
      : []
);

const getDefaultClaudeCredentialGroup = (channelMeta) => {
  const groups = getClaudeCredentialGroups(channelMeta);
  return groups.find((group) => group.isDefault || group.is_default) || groups[0] || null;
};

const getClaudeEndpointCredentialGroupID = (endpoint) => Number(endpoint?.credentialGroupId || endpoint?.credential_group_id || 0);

const getClaudeEndpointsForCredentialGroup = (endpoints = [], group = null) => {
  const groupID = Number(group?.id || 0);
  if (!groupID) return [];
  const isDefaultGroup = Boolean(group?.isDefault || group?.is_default);
  return endpoints.filter((endpoint) => {
    const endpointGroupID = getClaudeEndpointCredentialGroupID(endpoint);
    return isDefaultGroup
      ? endpointGroupID === groupID || endpointGroupID === 0
      : endpointGroupID === groupID;
  });
};

const buildClaudeEndpointCopyOptions = (credentialGroups = [], endpoints = []) => (
  credentialGroups
    .map((group) => {
      const copyEndpoints = getClaudeEndpointsForCredentialGroup(endpoints, group);
      return {
        value: String(group?.id || ''),
        label: `${group?.name || '未命名分组'} · ${copyEndpoints.length} 个端点`,
        name: group?.name || '未命名分组',
        endpointCount: copyEndpoints.length,
        endpoints: copyEndpoints
      };
    })
    .filter((option) => option.value && option.endpointCount > 0)
);

const buildInitialClaudeCredentialGroupForm = (group, channelName = '') => ({
  authMode: group?.authMode || 'group_credential',
  localKeyConfigured: Boolean(group?.localKeyConfigured),
  localKeyAcknowledged: false,
  subscription: group?.subscription ? { ...group.subscription, action: '', backupTargets: (group.subscription.backupTargets || []).map((target) => ({ ...target, rowId: crypto.randomUUID() })) } : { version: 0, enabled: true, backupTargets: [] },
  channel: group?.channel || channelName || '',
  name: group?.name || '',
  token: group?.token || '',
  apiKey: group?.apiKey || group?.api_key || '',
  isDefault: Boolean(group?.isDefault ?? group?.is_default ?? false),
  dailyCostLimitUSD: String(group?.dailyCostLimitUSD ?? group?.daily_cost_limit_usd ?? 0),
  modelPolicy: group?.modelPolicy || group?.model_policy || 'inherit',
  modelMappingRows: buildInitialModelMappingRows(group?.modelMapping || group?.model_mapping),
  supportedModelsText: Array.isArray(group?.supportedModels || group?.supported_models)
    ? (group?.supportedModels || group?.supported_models).join(', ')
    : '',
  costMultiplier: formatMultiplierInputValue(group?.costMultiplier ?? group?.cost_multiplier ?? 1.0),
  inputCostMultiplier: formatMultiplierInputValue(group?.inputCostMultiplier ?? group?.input_cost_multiplier ?? 1.0),
  outputCostMultiplier: formatMultiplierInputValue(group?.outputCostMultiplier ?? group?.output_cost_multiplier ?? 1.0),
  cacheCreationCostMultiplier: formatMultiplierInputValue(group?.cacheCreationCostMultiplier ?? group?.cache_creation_cost_multiplier ?? 1.0),
  cacheCreationCostMultiplier1h: formatMultiplierInputValue(group?.cacheCreationCostMultiplier1h ?? group?.cache_creation_cost_multiplier_1h ?? 1.0),
  cacheReadCostMultiplier: formatMultiplierInputValue(group?.cacheReadCostMultiplier ?? group?.cache_read_cost_multiplier ?? 1.0),
  // Claude 已移除 Long context 计费入口：倍率原样回传避免抹掉历史值，
  // 但 disableLongContextPricing 恒为 true，确保 tracker.go 的计费闸门始终关闭。
  longContextCostMultiplier: formatMultiplierInputValue(group?.longContextCostMultiplier ?? group?.long_context_cost_multiplier ?? 1.0),
  longContextInputCostMultiplier: formatMultiplierInputValue(group?.longContextInputCostMultiplier ?? group?.long_context_input_cost_multiplier ?? 1.0),
  longContextOutputCostMultiplier: formatMultiplierInputValue(group?.longContextOutputCostMultiplier ?? group?.long_context_output_cost_multiplier ?? 1.0),
  longContextCacheCreationCostMultiplier: formatMultiplierInputValue(group?.longContextCacheCreationCostMultiplier ?? group?.long_context_cache_creation_cost_multiplier ?? 1.0),
  longContextCacheReadCostMultiplier: formatMultiplierInputValue(group?.longContextCacheReadCostMultiplier ?? group?.long_context_cache_read_cost_multiplier ?? 1.0),
  disableLongContextPricing: true,
  // Claude 已移除服务等级计费入口：该能力从未接入计费链路
  //（app.go 构造 EndpointMultiplier 时未填 ServiceTierMultipliers，tracker.go:325 恒 early return），
  // 且 Anthropic 请求体的 service_tier 是意图（auto / standard_only）而非实际档位，与 Codex 四档不同构。
  // 倍率原样回传避免抹掉历史值，禁用标志固定为 true。
  disableServiceTierPricing: true,
  serviceTierCostMultiplier: formatMultiplierInputValue(group?.serviceTierCostMultiplier ?? group?.service_tier_cost_multiplier ?? 1.0),
  serviceTierInputCostMultiplier: formatMultiplierInputValue(group?.serviceTierInputCostMultiplier ?? group?.service_tier_input_cost_multiplier ?? 1.0),
  serviceTierOutputCostMultiplier: formatMultiplierInputValue(group?.serviceTierOutputCostMultiplier ?? group?.service_tier_output_cost_multiplier ?? 1.0),
  serviceTierCacheReadCostMultiplier: formatMultiplierInputValue(group?.serviceTierCacheReadCostMultiplier ?? group?.service_tier_cache_read_cost_multiplier ?? 1.0),
  copyEndpointsFromGroupId: '',
  modelMultiplierRows: buildInitialModelMultiplierRows(group?.modelMultipliers || group?.model_multipliers || [])
});

const validateClaudeCredentialGroupMultipliers = (formData) => {
  for (const field of CREDENTIAL_GROUP_MULTIPLIER_FIELDS) {
    if (!isValidMultiplierExpression(formData[field.name])) {
      return `${field.label}必须是非负数字或分数，例如 1.25 或 40/30`;
    }
  }
  return '';
};

const normalizeBool = (value, defaultValue = true) => {
  if (value === undefined || value === null) return defaultValue;
  if (value === false || value === 0 || value === '0') return false;
  return Boolean(value);
};

const parseSettingsBool = (value) => {
  if (value === true || value === false) return value;
  const normalized = String(value ?? '').trim().toLowerCase();
  if (['true', '1', 'yes', 'on'].includes(normalized)) return true;
  if (['false', '0', 'no', 'off'].includes(normalized)) return false;
  return undefined;
};

const buildSuspendedEndpointKey = (endpoint) => {
  const channel = endpoint?.channel || '';
  const name = endpoint?.name || '';
  if (!channel) return name || '';
  if (!name) return channel;
  return `${channel}::${name}`;
};

const getEndpointIdentity = (endpoint) => (
  endpoint?.id ? `id:${endpoint.id}` : buildSuspendedEndpointKey(endpoint)
);

const isSameEndpoint = (left, right) => (
  !!left && !!right && getEndpointIdentity(left) === getEndpointIdentity(right)
);

const endpointParticipatesInFailover = (endpoint) => normalizeBool(
  endpoint?.failoverEnabled ?? endpoint?.failover_enabled,
  true
);

const pickCurrentRouteEndpoint = (endpoints, isActiveChannel) => {
  if (!isActiveChannel || endpoints.length === 0) return null;

  const explicitActive = endpoints.find((endpoint) =>
    normalizeBool(endpoint?.isActive ?? endpoint?.is_active, false)
  );
  if (explicitActive) return explicitActive;

  return endpoints.find((endpoint) => normalizeBool(endpoint?.enabled, false)) || null;
};

export { normalizeBool, CHANNEL_FORM_TABS, CHANNEL_EDIT_FORM_TABS, CREDENTIAL_GROUP_FORM_TABS, CREDENTIAL_GROUP_CREATE_FORM_TABS, CLAUDE_CREDENTIAL_SECRET_TABS, getInitialClaudeCredentialSecretType, STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS, buildInitialClaudeCredentialGroupForm, validateClaudeCredentialGroupMultipliers, hasClaudeCredential, getDefaultClaudeCredentialGroup, isSameEndpoint, endpointParticipatesInFailover, pickCurrentRouteEndpoint, getClaudeCredentialGroups, buildClaudeEndpointCopyOptions, parseSettingsBool, buildSuspendedEndpointKey };

export const ENDPOINT_STATE_BUTTON_CLASS = 'inline-flex size-5 min-h-5 min-w-5 max-h-5 max-w-5 flex-shrink-0 appearance-none items-center justify-center rounded-md border-0 p-0 leading-none transition-colors duration-150';
