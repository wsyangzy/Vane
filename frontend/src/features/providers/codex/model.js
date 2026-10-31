import { isSubscriptionGroup } from '@/features/providers/shared/subscriptions/subscriptionSettings.js';
import { buildInitialBillingRuleRows } from './components/BillingRuleEditor.jsx';
import { buildInitialModelMappingRows } from '@/features/providers/shared/routing/modelMapping.js';
import { buildInitialModelMultiplierRows } from '@/features/providers/shared/billing/modelMultiplierRows.js';
import { formatMultiplierInputValue, parseMultiplierExpression } from '@/shared/lib/multiplier.js';

const getCredentialGroups = (channel) => {
  const groups = channel?.credentialGroups || channel?.credential_groups || [];
  return Array.isArray(groups) ? groups : [];
};

const hasCodexCredential = (...sources) => sources.some((source) => isSubscriptionGroup(source) || [
  source?.apiKey,
  source?.api_key,
  source?.apiKeyMasked,
  source?.api_key_masked
].some((value) => (typeof value === 'string' ? value.trim() !== '' : Boolean(value))));

const getDefaultCredentialGroup = (channel) => {
  const groups = getCredentialGroups(channel);
  return groups.find((group) => group.isDefault || group.is_default) || groups[0] || null;
};

const sortEndpointsByPriority = (endpoints = []) => (
  [...endpoints].sort((a, b) => {
    const normalizePriority = (value) => {
      const parsed = Number(value);
      return Number.isFinite(parsed) && parsed >= 0 ? parsed : 999;
    };
    const priorityDiff = normalizePriority(a?.priority) - normalizePriority(b?.priority);
    if (priorityDiff !== 0) return priorityDiff;
    const leftCreatedAt = a?.createdAt || a?.created_at || '';
    const rightCreatedAt = b?.createdAt || b?.created_at || '';
    const createdAtDiff = leftCreatedAt.localeCompare(rightCreatedAt);
    if (createdAtDiff !== 0) return createdAtDiff;
    return Number(a?.id || 0) - Number(b?.id || 0);
  })
);

const buildInitialChannelForm = (channel) => ({
  name: channel?.name || '',
  website: channel?.website || '',
  model: channel?.model || '',
  priority: channel ? String(channel.priority ?? 0) : '',
  failoverEnabled: channel?.failoverEnabled !== false,
  modelMappingRows: buildInitialModelMappingRows(channel?.modelMapping)
});

const buildInitialCredentialGroupForm = (group, channel) => ({
  authMode: group?.authMode || 'group_credential',
  localKeyConfigured: Boolean(group?.localKeyConfigured), localKeyAcknowledged: false,
  subscription: group?.subscription ? { ...group.subscription, action: '', backupTargets: (group.subscription.backupTargets || []).map((target) => ({ ...target, rowId: crypto.randomUUID() })) } : { version: 0, enabled: true, accountId: '', backupTargets: [] },
  name: group?.name || '',
  apiKey: group?.apiKey || '',
  isDefault: Boolean(group?.isDefault || group?.is_default || (!group && getCredentialGroups(channel).length === 0)),
  dailyCostLimitUSD: String(group?.dailyCostLimitUSD ?? group?.daily_cost_limit_usd ?? 0),
  modelPolicy: group?.modelPolicy || group?.model_policy || 'inherit',
  modelMappingRows: buildInitialModelMappingRows(group?.modelMapping || group?.model_mapping),
  supportedModelsText: Array.isArray(group?.supportedModels || group?.supported_models)
    ? (group?.supportedModels || group?.supported_models).join(', ')
    : '',
  costMultiplier: formatMultiplierInputValue(group?.costMultiplier ?? 1.0),
  inputCostMultiplier: formatMultiplierInputValue(group?.inputCostMultiplier ?? 1.0),
  outputCostMultiplier: formatMultiplierInputValue(group?.outputCostMultiplier ?? 1.0),
  cacheCreationCostMultiplier: formatMultiplierInputValue(group?.cacheCreationCostMultiplier ?? 1.0),
  cacheReadCostMultiplier: formatMultiplierInputValue(group?.cacheReadCostMultiplier ?? 1.0),
  longContextCostMultiplier: formatMultiplierInputValue(group?.longContextCostMultiplier ?? 1.0),
  longContextInputCostMultiplier: formatMultiplierInputValue(group?.longContextInputCostMultiplier ?? 1.0),
  longContextOutputCostMultiplier: formatMultiplierInputValue(group?.longContextOutputCostMultiplier ?? 1.0),
  longContextCacheCreationCostMultiplier: formatMultiplierInputValue(group?.longContextCacheCreationCostMultiplier ?? 1.0),
  longContextCacheReadCostMultiplier: formatMultiplierInputValue(group?.longContextCacheReadCostMultiplier ?? 1.0),
  serviceTierSource: group?.serviceTierSource || 'response',
  cacheWriteTTLPricing: Boolean(group?.cacheWriteTTLPricing),
  disableLongContextPricing: group ? Boolean(group.disableLongContextPricing) : true,  billingOutputTokenSource: group?.billingOutputTokenSource || 'output_tokens',
	 billingMode: Array.isArray(group?.billingRules || group?.billing_rules) && (group?.billingRules || group?.billing_rules).length > 0 ? 'rules' : 'legacy',
	 billingRuleRows: buildInitialBillingRuleRows(group?.billingRules || group?.billing_rules, group),
  copyEndpointsFromGroupId: '',
  modelMultiplierRows: buildInitialModelMultiplierRows(group?.modelMultipliers || group?.model_multipliers || [])
});

const CODEX_CHANNEL_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'mapping', label: '模型映射' }
];

// 模型路由与非流改道都按渠道 ID 保存，只在编辑已有渠道时提供
const CODEX_CHANNEL_EDIT_FORM_TABS = [
  ...CODEX_CHANNEL_FORM_TABS,
  { key: 'modelRoutes', label: '模型路由' },
  { key: 'nonStream', label: '非流改道' }
];

const CODEX_CREDENTIAL_GROUP_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'model', label: '模型策略' },
  { key: 'billing', label: '成本倍率' }
];

const CODEX_CREDENTIAL_GROUP_CREATE_FORM_TABS = [
  ...CODEX_CREDENTIAL_GROUP_FORM_TABS,
  { key: 'copy', label: '配置端点' }
];

// 计费模式是二选一而非视图切换，故用单选卡片而非 SegmentedTabs——
// 后者与弹窗顶部的视图标签视觉一致，会让用户误以为两种模式的配置都会生效。
const CODEX_BILLING_MODES = [
  { key: 'legacy', label: '传统倍率', description: 'Standard 单价 × 分项倍率，支持 Long context' },
  { key: 'rules', label: '组合规则', description: '按模型 / 档位 / 上下文匹配规则' }
];

const CODEX_ENDPOINT_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'routing', label: '路由配置' }
];

const getCodexEndpointCredentialGroupID = (endpoint) => Number(endpoint?.credentialGroupId || endpoint?.credential_group_id || 0);

const getCodexEndpointsForCredentialGroup = (channel, group = null) => {
  const groupID = Number(group?.id || 0);
  if (!groupID) return [];
  const endpoints = Array.isArray(channel?.endpoints) ? channel.endpoints : [];
  const isDefaultGroup = Boolean(group?.isDefault || group?.is_default);
  return sortEndpointsByPriority(endpoints.filter((endpoint) => {
    const endpointGroupID = getCodexEndpointCredentialGroupID(endpoint);
    return isDefaultGroup
      ? endpointGroupID === groupID || endpointGroupID === 0
      : endpointGroupID === groupID;
  }));
};

const buildCodexEndpointCopyOptions = (channel) => (
  getCredentialGroups(channel)
    .map((group) => {
      const copyEndpoints = getCodexEndpointsForCredentialGroup(channel, group);
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

const buildCodexEndpointCopyPayload = (endpoint, channelId, credentialGroupId) => ({
  channelId,
  credentialGroupId,
  name: endpoint?.name || '',
  baseURL: endpoint?.baseURL || endpoint?.base_url || '',
  failoverEnabled: endpoint?.failoverEnabled ?? endpoint?.failover_enabled ?? true,
  skipHealthCheck: endpoint?.skipHealthCheck ?? endpoint?.skip_health_check ?? false,
  priority: endpoint?.priority,
  timeoutSeconds: endpoint?.timeoutSeconds ?? endpoint?.timeout_seconds ?? 0
});

const getNextCodexEndpointPriority = (channel, credentialGroupID = 0) => {
  const endpoints = Array.isArray(channel?.endpoints) ? channel.endpoints : [];
  const targetCredentialGroupID = Number(credentialGroupID || 0);
  const maxPriority = endpoints.reduce((max, endpoint) => {
    if (getCodexEndpointCredentialGroupID(endpoint) !== targetCredentialGroupID) return max;
    const priority = Number(endpoint?.priority);
    return Number.isFinite(priority) ? Math.max(max, Math.floor(priority)) : max;
  }, -1);
  return maxPriority + 1;
};

const BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS = [
  { value: 'output_tokens', label: '总输出 output_tokens' },
  { value: 'reasoning_tokens', label: '推理 reasoning_tokens' },
  { value: 'visible_output_tokens', label: '可见补全 output - reasoning' }
];

// priceKey 指向模型定价中对应的单价字段，供倍率弹窗展示「基础单价 → 实际单价」对照。
// 无 priceKey 的是总倍率，作用于全部分项。
const STANDARD_ENDPOINT_MULTIPLIER_FIELDS = [
  { label: '总成本倍率', key: 'costMultiplier' },
  { label: '输入成本倍率', key: 'inputCostMultiplier', priceKey: 'inputPrice' },
  { label: '输出成本倍率', key: 'outputCostMultiplier', priceKey: 'outputPrice' },
  { label: '缓存写入倍率', key: 'cacheCreationCostMultiplier', priceKey: 'cacheWritePrice30m' },
  { label: '缓存读取倍率', key: 'cacheReadCostMultiplier', priceKey: 'cacheReadPrice' }
];

const LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS = [
  { label: 'Long context 总倍率', key: 'longContextCostMultiplier' },
  { label: 'Long context 输入倍率', key: 'longContextInputCostMultiplier', priceKey: 'longContextInputPrice' },
  { label: 'Long context 输出倍率', key: 'longContextOutputCostMultiplier', priceKey: 'longContextOutputPrice' },
  { label: 'Long context 缓存写入倍率', key: 'longContextCacheCreationCostMultiplier', priceKey: 'longContextCacheWritePrice' },
  { label: 'Long context 缓存读取倍率', key: 'longContextCacheReadCostMultiplier', priceKey: 'longContextCacheReadPrice' }
];

const ENDPOINT_MULTIPLIER_FIELDS = [
  ...STANDARD_ENDPOINT_MULTIPLIER_FIELDS,
  ...LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS
];

const parseEndpointMultiplierPayload = (values) => {
  const payload = {};
  for (const field of ENDPOINT_MULTIPLIER_FIELDS) {
    const parsed = parseMultiplierExpression(values[field.key], 1.0);
    if (!Number.isFinite(parsed) || parsed < 0) {
      return { error: `${field.label}必须是非负数字或分数，例如 1.25 或 40/30` };
    }
    payload[field.key] = parsed;
  }
  return { payload };
};

const normalizeMultiplierValue = (value) => {
  const parsed = typeof value === 'string'
    ? parseMultiplierExpression(value, Number.NaN)
    : Number(value);
  return Number.isFinite(parsed) ? parsed : 1;
};

const formatEndpointTimeoutInputValue = (value) => {
  const seconds = Number(value ?? 0);
  if (!Number.isFinite(seconds) || seconds <= 0) return '';
  return String(Math.trunc(seconds));
};

const parseEndpointTimeoutPayload = (value) => {
  const raw = String(value ?? '').trim();
  if (raw === '') return { timeoutSeconds: 0 };
  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed < 0) {
    return { error: '超时时间必须是大于等于 0 的整数秒数' };
  }
  return { timeoutSeconds: parsed };
};

const parseEndpointPriorityPayload = (value) => {
  const raw = String(value ?? '').trim();
  if (raw === '') return { priority: undefined };
  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed < 0) {
    return { error: '优先级必须是大于等于 0 的整数' };
  }
  return { priority: parsed };
};

export { buildInitialChannelForm, CODEX_CHANNEL_FORM_TABS, CODEX_CHANNEL_EDIT_FORM_TABS, buildInitialCredentialGroupForm, CODEX_CREDENTIAL_GROUP_FORM_TABS, CODEX_CREDENTIAL_GROUP_CREATE_FORM_TABS, CODEX_BILLING_MODES, BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS, STANDARD_ENDPOINT_MULTIPLIER_FIELDS, LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS, parseEndpointMultiplierPayload, CODEX_ENDPOINT_FORM_TABS, getNextCodexEndpointPriority, formatEndpointTimeoutInputValue, parseEndpointTimeoutPayload, parseEndpointPriorityPayload, getDefaultCredentialGroup, normalizeMultiplierValue, getCredentialGroups, hasCodexCredential, sortEndpointsByPriority, buildCodexEndpointCopyOptions, buildCodexEndpointCopyPayload };

export const ENDPOINT_STATE_BUTTON_CLASS = 'inline-flex size-5 min-h-5 min-w-5 max-h-5 max-w-5 flex-shrink-0 appearance-none items-center justify-center rounded-md border-0 p-0 leading-none transition-colors duration-150';
