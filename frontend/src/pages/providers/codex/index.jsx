// ============================================
// Codex 页面 - 渠道-端点 两层管理
// 2026-05-13: 引入「渠道（共用配置）+ 端点（仅 base_url）」两层模型
// 切换激活端点时，仅 ~/.codex/config.toml 中所属渠道 section 的 base_url 行变化
// ============================================

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertCircle,
  ArrowRight,
  ArrowRightLeft,
  CheckCircle2,
  ChevronDown,
  Database,
  Eye,
  EyeOff,
  Globe,
  KeyRound,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  Repeat,
  Route,
  RouteOff,
  Save,
  Server,
  Settings,
  ShieldCheck,
  ShieldOff,
  Slash,
  Trash2,
  TriangleAlert,
  X,
  XCircle
} from 'lucide-react';
import {
  Button,
  CustomSelect,
  EmptyState,
  ErrorMessage,
  Input,
  KPICard,
  LoadingSpinner,
  SegmentedTabs,
  Tooltip,
  useToast
} from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import useHorizontalScroll from '@hooks/useHorizontalScroll.js';
import ModelTag from '@pages/requests/components/ModelTag.jsx';
import {
  DetailChip as BaseDetailChip,
  DetailMetricCard as BaseDetailMetricCard,
  DetailPill as BaseDetailPill,
  DetailTimeoutPill as BaseDetailTimeoutPill,
  DetailTimestampFooter as BaseDetailTimestampFooter,
  ModelMappingPair
} from '@pages/management/shared/detailParts.jsx';
import EndpointCopyNameEditorStep, { EndpointCopySourcePicker } from '@pages/management/shared/EndpointCopyNameEditorStep.jsx';
import {
  formatLastCheck,
  formatDetailMultiplier,
  getDetailTimestamp,
  getErrorMessage,
  getFailoverToggleErrorMessage
} from '@pages/management/shared/formatters.js';
import ModelMultiplierEditor from '@pages/management/shared/ModelMultiplierEditor.jsx';
import LongContextMultiplierSettings from '@pages/management/shared/LongContextMultiplierSettings.jsx';
import {
  EndpointHealthBadge as CodexHealthBadge,
  EndpointLatencyBadge as CodexLatencyBadge,
  EndpointRequestFailureBadge as CodexRequestFailureBadge
} from '@pages/management/shared/endpointBadges.jsx';
import { buildInitialModelMappingRows, buildModelMappingPayload } from '@pages/management/shared/modelMapping.js';
import { buildInitialModelMultiplierRows, MULTIPLIER_EXPRESSION_HELP, parseModelMultiplierRows } from '@pages/management/shared/modelMultiplierRows.js';
import {
  activateCodexChannel,
  activateCodexEndpoint,
  createCodexChannel,
  createCodexCredentialGroup,
  createCodexEndpoint,
  deleteCodexChannelByID,
  deleteCodexCredentialGroupByID,
  deleteCodexEndpointByID,
  getCodexChannels,
  getCodexGlobalConfig,
  getNextAvailableCodexChannel,
  getSettingsByScope,
  getCodexSuspendedCount,
  isWailsEnvironment,
  openExternalURL,
  subscribeToEvent,
  setDefaultCodexCredentialGroup,
  setCodexChannelFailoverEnabled,
  updateCodexChannelByID,
  updateCodexCredentialGroupByID,
  updateCodexEndpointByID,
  updateCodexGlobalConfig
} from '@utils/wailsApi.js';
import { formatMultiplierExpression, formatMultiplierInputValue, parseMultiplierExpression } from '@utils/multiplier.js';

const DetailMetricCard = (props) => <BaseDetailMetricCard {...props} density="compact" />;
const DetailChip = (props) => <BaseDetailChip {...props} variant="codex" />;
const DetailPill = (props) => <BaseDetailPill {...props} variant="codex" />;
const DetailTimeoutPill = (props) => <BaseDetailTimeoutPill {...props} variant="codex" />;
const DetailTimestampFooter = (props) => <BaseDetailTimestampFooter {...props} density="compact" />;
const RepeatOffIcon = ({ size = 12 }) => (
  <span className="relative inline-flex flex-shrink-0" style={{ width: size, height: size }}>
    <Repeat size={size} />
    <Slash size={size} strokeWidth={2.5} className="absolute inset-0" />
  </span>
);

const ENDPOINT_STATE_BUTTON_CLASS = 'inline-flex size-5 min-h-5 min-w-5 max-h-5 max-w-5 flex-shrink-0 appearance-none items-center justify-center rounded-md border-0 p-0 leading-none transition-colors duration-150';

const getCredentialGroups = (channel) => {
  const groups = channel?.credentialGroups || channel?.credential_groups || [];
  return Array.isArray(groups) ? groups : [];
};

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

const buildChannelCredentialGroupFields = (group, channel) => ({
  credentialGroupId: group?.id || 0,
  credentialGroupName: group?.name || '默认分组',
  credentialGroupApiKey: group?.apiKey || (!group ? channel?.apiKey || '' : ''),
  credentialGroupCostMultiplier: formatMultiplierInputValue(group?.costMultiplier ?? 1.0),
  credentialGroupInputCostMultiplier: formatMultiplierInputValue(group?.inputCostMultiplier ?? 1.0),
  credentialGroupOutputCostMultiplier: formatMultiplierInputValue(group?.outputCostMultiplier ?? 1.0),
  credentialGroupCacheCreationCostMultiplier: formatMultiplierInputValue(group?.cacheCreationCostMultiplier ?? 1.0),
  credentialGroupCacheReadCostMultiplier: formatMultiplierInputValue(group?.cacheReadCostMultiplier ?? 1.0),
  credentialGroupLongContextCostMultiplier: formatMultiplierInputValue(group?.longContextCostMultiplier ?? 1.0),
  credentialGroupLongContextInputCostMultiplier: formatMultiplierInputValue(group?.longContextInputCostMultiplier ?? 1.0),
  credentialGroupLongContextOutputCostMultiplier: formatMultiplierInputValue(group?.longContextOutputCostMultiplier ?? 1.0),
  credentialGroupLongContextCacheCreationCostMultiplier: formatMultiplierInputValue(group?.longContextCacheCreationCostMultiplier ?? 1.0),
  credentialGroupLongContextCacheReadCostMultiplier: formatMultiplierInputValue(group?.longContextCacheReadCostMultiplier ?? 1.0),
  credentialGroupDisableLongContextPricing: group ? Boolean(group.disableLongContextPricing) : true,
  credentialGroupBillingOutputTokenSource: group?.billingOutputTokenSource || 'output_tokens'
});

const buildInitialChannelForm = (channel) => {
  const defaultGroup = getDefaultCredentialGroup(channel);
  return {
    name: channel?.name || '',
    website: channel?.website || '',
    model: channel?.model || '',
    priority: channel ? String(channel.priority ?? 0) : '',
    failoverEnabled: channel?.failoverEnabled !== false,
    ...buildChannelCredentialGroupFields(defaultGroup, channel),
    modelMappingRows: buildInitialModelMappingRows(channel?.modelMapping)
  };
};

const buildInitialCredentialGroupForm = (group, channel) => ({
  name: group?.name || '',
  apiKey: group?.apiKey || '',
  isDefault: Boolean(group?.isDefault || group?.is_default || (!group && getCredentialGroups(channel).length === 0)),
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
  disableLongContextPricing: group ? Boolean(group.disableLongContextPricing) : true,
  billingOutputTokenSource: group?.billingOutputTokenSource || 'output_tokens',
  copyEndpointsFromGroupId: '',
  modelMultiplierRows: buildInitialModelMultiplierRows(group?.modelMultipliers || group?.model_multipliers || [])
});

const CODEX_CHANNEL_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'credential', label: '凭证分组' },
  { key: 'mapping', label: '模型映射' }
];

const CODEX_CREDENTIAL_GROUP_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'billing', label: '成本倍率' }
];

const CODEX_CREDENTIAL_GROUP_CREATE_FORM_TABS = [
  ...CODEX_CREDENTIAL_GROUP_FORM_TABS,
  { key: 'copy', label: '配置端点' }
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

const STANDARD_ENDPOINT_MULTIPLIER_FIELDS = [
  { label: '总成本倍率', key: 'costMultiplier' },
  { label: '输入成本倍率', key: 'inputCostMultiplier' },
  { label: '输出成本倍率', key: 'outputCostMultiplier' },
  { label: '缓存创建倍率', key: 'cacheCreationCostMultiplier' },
  { label: '缓存读取倍率', key: 'cacheReadCostMultiplier' }
];

const LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS = [
  { label: 'Long context 总倍率', key: 'longContextCostMultiplier' },
  { label: 'Long context 输入倍率', key: 'longContextInputCostMultiplier' },
  { label: 'Long context 输出倍率', key: 'longContextOutputCostMultiplier' },
  { label: 'Long context 缓存创建倍率', key: 'longContextCacheCreationCostMultiplier' },
  { label: 'Long context 缓存读取倍率', key: 'longContextCacheReadCostMultiplier' }
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
  if (!Number.isFinite(seconds) || seconds <= 0) return '0';
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

// 注：model / enable1MContext 已迁移到「设置」页全局配置；推理强度由 Codex 客户端按会话管理
// 在代理模式下，这些字段不再由渠道级管理

// Codex 官方品牌图标（来源：lobe-icons / OpenAI Codex）
// 使用 currentColor 填充，可被父级 text-white 等颜色 token 控制
const CodexLogo = ({ className = 'w-6 h-6' }) => (
  <svg
    className={className}
    viewBox="0 0 24 24"
    fill="currentColor"
    fillRule="evenodd"
    aria-hidden="true"
    focusable="false"
  >
    <title>Codex</title>
    <path
      clipRule="evenodd"
      d="M8.086.457a6.105 6.105 0 013.046-.415c1.333.153 2.521.72 3.564 1.7a.117.117 0 00.107.029c1.408-.346 2.762-.224 4.061.366l.063.03.154.076c1.357.703 2.33 1.77 2.918 3.198.278.679.418 1.388.421 2.126a5.655 5.655 0 01-.18 1.631.167.167 0 00.04.155 5.982 5.982 0 011.578 2.891c.385 1.901-.01 3.615-1.183 5.14l-.182.22a6.063 6.063 0 01-2.934 1.851.162.162 0 00-.108.102c-.255.736-.511 1.364-.987 1.992-1.199 1.582-2.962 2.462-4.948 2.451-1.583-.008-2.986-.587-4.21-1.736a.145.145 0 00-.14-.032c-.518.167-1.04.191-1.604.185a5.924 5.924 0 01-2.595-.622 6.058 6.058 0 01-2.146-1.781c-.203-.269-.404-.522-.551-.821a7.74 7.74 0 01-.495-1.283 6.11 6.11 0 01-.017-3.064.166.166 0 00.008-.074.115.115 0 00-.037-.064 5.958 5.958 0 01-1.38-2.202 5.196 5.196 0 01-.333-1.589 6.915 6.915 0 01.188-2.132c.45-1.484 1.309-2.648 2.577-3.493.282-.188.55-.334.802-.438.286-.12.573-.22.861-.304a.129.129 0 00.087-.087A6.016 6.016 0 015.635 2.31C6.315 1.464 7.132.846 8.086.457zm-.804 7.85a.848.848 0 00-1.473.842l1.694 2.965-1.688 2.848a.849.849 0 001.46.864l1.94-3.272a.849.849 0 00.007-.854l-1.94-3.393zm5.446 6.24a.849.849 0 000 1.695h4.848a.849.849 0 000-1.696h-4.848z"
    />
  </svg>
);

// ============================================================================
// 渠道编辑弹窗（渠道信息 + 创建时的默认凭证分组；model/reasoning/1M 已迁移到全局设置）
// ============================================================================
const ChannelFormModal = ({ mode, channel, channelFailoverEnabled, loading, serverError, onCancel, onSave }) => {
  const [formData, setFormData] = useState(() => buildInitialChannelForm(channel));
  const [activeTab, setActiveTab] = useState('basic');
  const [fieldError, setFieldError] = useState('');
  const [showApiKey, setShowApiKey] = useState(false);
  const isWails = isWailsEnvironment();
  const isEdit = mode === 'edit';
  const channelFormTabs = isEdit
    ? CODEX_CHANNEL_FORM_TABS.filter((tab) => tab.key !== 'credential')
    : CODEX_CHANNEL_FORM_TABS;
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;

  const updateField = (key, value) => {
    setFormData((prev) => ({ ...prev, [key]: value }));
    if (fieldError) {
      setFieldError('');
    }
  };

  const updateMappingRow = (index, key, value) => {
    setFormData((prev) => ({
      ...prev,
      modelMappingRows: (prev.modelMappingRows || []).map((row, rowIndex) =>
        rowIndex === index ? { ...row, [key]: value } : row
      )
    }));
    if (fieldError) {
      setFieldError('');
    }
  };

  const addMappingRow = () => {
    setFormData((prev) => ({
      ...prev,
      modelMappingRows: [
        ...(prev.modelMappingRows || []),
        { sourceModel: '', targetModel: '' }
      ]
    }));
  };

  const removeMappingRow = (index) => {
    setFormData((prev) => {
      const nextRows = (prev.modelMappingRows || []).filter((_, rowIndex) => rowIndex !== index);
      return {
        ...prev,
        modelMappingRows: nextRows.length > 0 ? nextRows : [{ sourceModel: '', targetModel: '' }]
      };
    });
  };

  const buildCredentialGroupPayload = () => {
    const multiplierResult = parseEndpointMultiplierPayload({
      costMultiplier: formData.credentialGroupCostMultiplier,
      inputCostMultiplier: formData.credentialGroupInputCostMultiplier,
      outputCostMultiplier: formData.credentialGroupOutputCostMultiplier,
      cacheCreationCostMultiplier: formData.credentialGroupCacheCreationCostMultiplier,
      cacheReadCostMultiplier: formData.credentialGroupCacheReadCostMultiplier,
      longContextCostMultiplier: formData.credentialGroupLongContextCostMultiplier,
      longContextInputCostMultiplier: formData.credentialGroupLongContextInputCostMultiplier,
      longContextOutputCostMultiplier: formData.credentialGroupLongContextOutputCostMultiplier,
      longContextCacheCreationCostMultiplier: formData.credentialGroupLongContextCacheCreationCostMultiplier,
      longContextCacheReadCostMultiplier: formData.credentialGroupLongContextCacheReadCostMultiplier
    });
    if (multiplierResult.error) {
      return { error: multiplierResult.error };
    }
    return {
      payload: {
        id: formData.credentialGroupId || 0,
        name: formData.credentialGroupName.trim(),
        apiKey: formData.credentialGroupApiKey.trim(),
        isDefault: true,
        ...multiplierResult.payload,
        disableLongContextPricing: formData.credentialGroupDisableLongContextPricing,
        billingOutputTokenSource: formData.credentialGroupBillingOutputTokenSource
      }
    };
  };

  const handleSubmit = async (event) => {
    event.preventDefault();

    const payload = {
      name: formData.name.trim(),
      apiKey: isEdit ? '' : formData.credentialGroupApiKey.trim(),
      website: formData.website.trim(),
      model: formData.model.trim(),
      modelMapping: buildModelMappingPayload(formData.modelMappingRows),
      priority: isEdit ? Math.max(0, Number.parseInt(formData.priority, 10) || 0) : -1,
      failoverEnabled: formData.failoverEnabled,
      credentialGroup: null
    };

    if (!payload.name) {
      setActiveTab('basic');
      setFieldError('请填写渠道名');
      return;
    }

    if (!isEdit) {
      const credentialGroupResult = buildCredentialGroupPayload();
      if (credentialGroupResult.error) {
        setActiveTab('credential');
        setFieldError(credentialGroupResult.error);
        return;
      }
      payload.credentialGroup = credentialGroupResult.payload;
      if (!payload.credentialGroup.name || !payload.credentialGroup.apiKey) {
        setActiveTab('credential');
        setFieldError('请填写默认凭证分组名称和 API Key');
        return;
      }
    }

    await onSave?.(payload);
  };

  const apiKeyInputType = showApiKey ? 'text' : 'password';

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center overflow-hidden bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in"
    >
      <div
        className="flex max-h-[calc(100vh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              {isEdit ? <Pencil size={18} /> : <Plus size={18} />}
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑渠道' : '添加渠道'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道保存基础信息；API Key 在凭证分组中维护，模型/推理/1M 在「Codex 配置」中全局调整</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onCancel}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
              disabled={loading}
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={handleSubmit} autoComplete="off">
          <div className="flex-shrink-0 border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <SegmentedTabs
              tabs={channelFormTabs}
              activeTab={activeTab}
              onChange={setActiveTab}
              disabled={loading}
              className={isEdit ? 'grid-cols-2' : 'grid-cols-3'}
              buttonPaddingClass="px-2"
            />
          </div>
          <div className="space-y-3.5 overflow-y-auto px-6 py-5">
            {(fieldError || serverError) && (
              <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            {activeTab === 'basic' ? (
              <>
            <div className="grid grid-cols-2 gap-4">
              <Input
                label="渠道名"
                value={formData.name}
                onChange={(event) => updateField('name', event.target.value)}
                placeholder="例如：主渠道"
                autoFocus
              />
              <Input
                label="官网地址（可选）"
                value={formData.website}
                onChange={(event) => updateField('website', event.target.value)}
                placeholder="https://platform.openai.com"
              />
            </div>

            {channelFailoverEnabled && isEdit && (
              <div className="grid grid-cols-2 gap-4">
                <div className="block">
                  <span className="mb-1.5 block text-sm font-medium text-slate-700">参与渠道间故障转移</span>
                  <div className="flex h-[42px] min-w-0 items-center justify-between gap-4 rounded-lg border border-slate-200 px-3">
                    <span className="truncate text-xs text-slate-400">
                      {channel?.isActive ? '活跃渠道请在卡片中暂停' : '关闭后自动转移会跳过此渠道'}
                    </span>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={formData.failoverEnabled}
                      onClick={() => updateField('failoverEnabled', !formData.failoverEnabled)}
                      disabled={Boolean(channel?.isActive)}
                      className={`relative inline-flex h-6 w-11 flex-shrink-0 rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${formData.failoverEnabled ? 'bg-indigo-600' : 'bg-slate-200'}`}
                    >
                      <span className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform ${formData.failoverEnabled ? 'translate-x-5' : 'translate-x-0'}`} />
                    </button>
                  </div>
                </div>
                <label className="block">
                  <span className="mb-1.5 block text-sm font-medium text-slate-700">优先级</span>
                  <input
                    type="number"
                    min="0"
                    value={formData.priority}
                    onChange={(event) => updateField('priority', event.target.value)}
                    className="h-[42px] w-full rounded-lg border border-slate-200 px-3 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                  <p className="mt-1 text-xs text-slate-400">数字越小越优先，相同则交换</p>
                </label>
              </div>
            )}

            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-700">模型（可选）</label>
              <input
                type="text"
                value={formData.model}
                onChange={(event) => updateField('model', event.target.value)}
                placeholder="留空 = 使用 Codex 全局配置中的默认模型"
                className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
              <p className="mt-1 text-xs text-slate-400">
                填入后该渠道将覆盖全局 model；不同渠道支持的模型可能不同
              </p>
            </div>
              </>
            ) : activeTab === 'credential' ? (
              <div className="space-y-4">
                <div>
                  <h3 className="text-sm font-semibold text-slate-900">默认凭证分组</h3>
                  <p className="mt-0.5 text-xs text-slate-500">创建渠道时会同时创建默认分组；多个服务商分组可在渠道卡片中继续添加和编辑。</p>
                </div>
                <Input
                  label="默认凭证分组名称"
                  value={formData.credentialGroupName}
                  onChange={(event) => updateField('credentialGroupName', event.target.value)}
                  placeholder="例如：默认分组 / 高倍率组"
                  disabled={loading}
                />
            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-700">默认凭证分组 API Key</label>
              <div className="relative">
                <input
                  type={apiKeyInputType}
                  value={formData.credentialGroupApiKey}
                  onChange={(event) => updateField('credentialGroupApiKey', event.target.value)}
                  placeholder="sk-..."
                  autoComplete="new-password"
                  data-form-type="other"
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 pr-11 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 [&::-ms-reveal]:hidden [&::-ms-clear]:hidden [&::-webkit-credentials-auto-fill-button]:hidden"
                />
                <Tooltip
                  content={showApiKey ? '隐藏 API Key' : '显示 API Key'}
                  className="absolute right-2 top-1/2 -translate-y-1/2"
                >
                  <button
                    type="button"
                    onClick={() => setShowApiKey((prev) => !prev)}
                    className="rounded-md p-1 text-slate-400 transition-colors hover:text-slate-600"
                  >
                    {showApiKey ? <EyeOff size={18} /> : <Eye size={18} />}
                  </button>
                </Tooltip>
              </div>
            </div>
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">成本倍率</label>
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                    {[
                      ['credentialGroupCostMultiplier', '总倍率'],
                      ['credentialGroupInputCostMultiplier', '输入'],
                      ['credentialGroupOutputCostMultiplier', '输出'],
                      ['credentialGroupCacheReadCostMultiplier', '缓存']
                    ].map(([key, label]) => (
                      <div key={key}>
                        <label className="mb-1 block text-xs text-slate-500">{label}</label>
                        <input
                          type="text"
                          inputMode="decimal"
                          value={formData[key]}
                          onChange={(event) => updateField(key, event.target.value)}
                          className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                          placeholder="1.0"
                          disabled={loading}
                        />
                      </div>
                    ))}
                  </div>
                  <p className="mt-1 text-xs text-slate-400">成本倍率直接作为该分组下 Codex 端点的计费倍率，{MULTIPLIER_EXPRESSION_HELP}</p>
                </div>
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">输出计费口径</label>
                  <CustomSelect
                    options={BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS}
                    value={formData.credentialGroupBillingOutputTokenSource}
                    onChange={(value) => updateField('credentialGroupBillingOutputTokenSource', value)}
                    size="md"
                    fullWidth
                  />
                </div>
              </div>
            ) : (
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h3 className="text-sm font-semibold text-slate-900">模型映射</h3>
                    <p className="mt-0.5 text-xs text-slate-500">请求模型精确命中后替换为该渠道支持的模型。</p>
                  </div>
                  <button
                    type="button"
                    onClick={addMappingRow}
                    className="inline-flex items-center gap-1 rounded-lg border border-slate-200 px-3 py-1.5 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50"
                  >
                    <Plus size={14} />
                    添加
                  </button>
                </div>

                <div className="space-y-2">
                  {(formData.modelMappingRows || []).map((row, index) => (
                    <div
                      key={`codex-model-mapping-${index}`}
                      className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_1fr_auto] sm:items-center"
                    >
                      <input
                        type="text"
                        value={row.sourceModel}
                        onChange={(event) => updateMappingRow(index, 'sourceModel', event.target.value)}
                        placeholder="gpt-5.4-mini"
                        className="min-w-0 rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                      />
                      <ArrowRight size={16} className="text-slate-400" />
                      <input
                        type="text"
                        value={row.targetModel}
                        onChange={(event) => updateMappingRow(index, 'targetModel', event.target.value)}
                        placeholder="gpt-5-mini"
                        className="min-w-0 rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                      />
                      <Tooltip content="删除映射">
                        <button
                          type="button"
                          onClick={() => removeMappingRow(index)}
                          className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600"
                        >
                          <Trash2 size={16} />
                        </button>
                      </Tooltip>
                    </div>
                  ))}
                </div>
                <p className="text-xs text-slate-400">未命中映射时，仍按基础信息中的渠道模型覆盖规则处理。</p>
              </div>
            )}
          </div>

          <div className="flex flex-shrink-0 items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
            <Button variant="secondary" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button variant="primary" icon={isEdit ? Pencil : Plus} loading={loading} type="submit">
              {isEdit ? '保存修改' : '创建渠道'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

// ============================================================================
// 凭证分组编辑弹窗
// ============================================================================
const CredentialGroupFormModal = ({ mode, channel, group, loading, serverError, endpointCopyOptions = [], onCancel, onSave }) => {
  const [formData, setFormData] = useState(() => buildInitialCredentialGroupForm(group, channel));
  const [longContextSettingsOpen, setLongContextSettingsOpen] = useState(false);
  const [activeTab, setActiveTab] = useState('basic');
  const [fieldError, setFieldError] = useState('');
  const [showApiKey, setShowApiKey] = useState(false);
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;
  const isEdit = mode === 'edit';
  const [formStep, setFormStep] = useState('details');
  const [copyStepEndpoints, setCopyStepEndpoints] = useState([]);
  const [copyEndpointsEnabled, setCopyEndpointsEnabled] = useState(false);
  const copyChoices = [
    { value: '', label: '不复制', name: '不复制', endpointCount: 0, endpoints: [] },
    ...endpointCopyOptions
  ];
  const selectedCopyOption = copyChoices.find((option) => option.value === (formData.copyEndpointsFromGroupId || '')) || copyChoices[0];
  const canCopyEndpoints = !isEdit && copyChoices.length > 1;
  const credentialGroupTabs = canCopyEndpoints && copyEndpointsEnabled
    ? CODEX_CREDENTIAL_GROUP_CREATE_FORM_TABS
    : CODEX_CREDENTIAL_GROUP_FORM_TABS;

  const updateField = (key, value) => {
    setFormData((prev) => ({ ...prev, [key]: value }));
    if (fieldError) setFieldError('');
  };

  const handleLongContextToggle = () => {
    if (loading) return;
    if (formData.disableLongContextPricing) {
      setLongContextSettingsOpen(true);
      return;
    }
    updateField('disableLongContextPricing', true);
  };

  const handleLongContextSave = (values) => {
    setFormData((current) => ({ ...current, ...values, disableLongContextPricing: false }));
    setLongContextSettingsOpen(false);
    if (fieldError) setFieldError('');
  };

  const updateCopyEndpointsEnabled = (enabled) => {
    setCopyEndpointsEnabled(enabled);
    if (enabled) {
      setActiveTab('copy');
      return;
    }
    updateField('copyEndpointsFromGroupId', '');
    if (activeTab === 'copy') setActiveTab('basic');
  };

  const updateCopySource = (value) => {
    updateField('copyEndpointsFromGroupId', value);
    if (!value) updateCopyEndpointsEnabled(false);
  };

  const updateModelMultiplierRows = (rows) => {
    setFormData((prev) => ({ ...prev, modelMultiplierRows: rows }));
    if (fieldError) setFieldError('');
  };

  const buildSubmitPayload = () => {
    const name = formData.name.trim();
    const apiKey = formData.apiKey.trim();
    if (!name) {
      setFieldError('分组名称不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
    if (!apiKey) {
      setFieldError('API Key 不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
    const multiplierResult = parseEndpointMultiplierPayload({
      costMultiplier: formData.costMultiplier,
      inputCostMultiplier: formData.inputCostMultiplier,
      outputCostMultiplier: formData.outputCostMultiplier,
      cacheCreationCostMultiplier: formData.cacheCreationCostMultiplier,
      cacheReadCostMultiplier: formData.cacheReadCostMultiplier,
      longContextCostMultiplier: formData.longContextCostMultiplier,
      longContextInputCostMultiplier: formData.longContextInputCostMultiplier,
      longContextOutputCostMultiplier: formData.longContextOutputCostMultiplier,
      longContextCacheCreationCostMultiplier: formData.longContextCacheCreationCostMultiplier,
      longContextCacheReadCostMultiplier: formData.longContextCacheReadCostMultiplier
    });
    if (multiplierResult.error) {
      setFieldError(multiplierResult.error);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }
    const modelMultiplierResult = parseModelMultiplierRows(formData.modelMultiplierRows);
    if (modelMultiplierResult.error) {
      setFieldError(modelMultiplierResult.error);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }
    return {
      name,
      apiKey,
      isDefault: formData.isDefault,
      copyEndpointsFromGroupId: formData.copyEndpointsFromGroupId || '',
      ...multiplierResult.payload,
      disableLongContextPricing: formData.disableLongContextPricing,
      billingOutputTokenSource: formData.billingOutputTokenSource,
      modelMultipliers: modelMultiplierResult.payload
    };
  };

  const handleSubmit = async (event) => {
    event.preventDefault();
    const payload = buildSubmitPayload();
    if (!payload) return;
    if (!isEdit && copyEndpointsEnabled && !selectedCopyOption?.value) {
      setFieldError('请选择复制来源分组');
      setFormStep('details');
      setActiveTab('copy');
      return;
    }
    if (!isEdit && selectedCopyOption?.value && selectedCopyOption.endpoints?.length > 0) {
      setCopyStepEndpoints(selectedCopyOption.endpoints);
      setFormStep('copyNames');
      setFieldError('');
      return;
    }
    await onSave?.({ ...payload, copyEndpoints: [] });
  };

  const handleConfirmCopyNames = async (editedEndpoints) => {
    const payload = buildSubmitPayload();
    if (!payload) return;
    await onSave?.({ ...payload, copyEndpoints: editedEndpoints });
  };

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center overflow-hidden bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in"
    >
      <div
        className="flex max-h-[calc(100vh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              <KeyRound size={18} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑凭证分组' : '添加凭证分组'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道：{channel?.name}</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onCancel}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
              disabled={loading}
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <form className="min-h-0 flex-1 overflow-y-auto" onSubmit={handleSubmit} autoComplete="off">
          {formStep === 'copyNames' ? (
            <EndpointCopyNameEditorStep
              endpoints={copyStepEndpoints}
              description={`确认后将创建分组「${formData.name.trim() || '新分组'}」并复制 ${copyStepEndpoints.length} 个端点`}
              loading={loading}
              error={serverError}
              onCancel={onCancel}
              onBack={() => {
                setFormStep('details');
                setActiveTab('copy');
              }}
              onConfirm={handleConfirmCopyNames}
            />
          ) : (
            <>
              <div className="border-b border-slate-100 bg-slate-50/70 px-6 py-3">
                <SegmentedTabs
                  tabs={credentialGroupTabs}
                  activeTab={activeTab}
                  onChange={setActiveTab}
                  disabled={loading}
                  className={credentialGroupTabs.length === 3 ? 'grid-cols-3' : 'grid-cols-2'}
                  buttonPaddingClass="px-2"
                />
              </div>

              <div className="px-6 py-5">
            {(fieldError || serverError) && (
              <div className="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            {activeTab === 'basic' && (
              <div className="space-y-4">
                <Input
                  label="分组名称"
                  value={formData.name}
                  onChange={(event) => updateField('name', event.target.value)}
                  placeholder="例如：主账号 / 高倍率组"
                  autoFocus
                  disabled={loading}
                />

                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">API Key</label>
                  <div className="relative">
                    <input
                      type={showApiKey ? 'text' : 'password'}
                      value={formData.apiKey}
                      onChange={(event) => updateField('apiKey', event.target.value)}
                      placeholder={isEdit && group?.apiKeyMasked ? group.apiKeyMasked : 'sk-...'}
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 pr-10 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 [&::-ms-reveal]:hidden [&::-ms-clear]:hidden [&::-webkit-credentials-auto-fill-button]:hidden"
                      disabled={loading}
                    />
                    <button
                      type="button"
                      onClick={() => setShowApiKey((prev) => !prev)}
                      className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                      tabIndex={-1}
                    >
                      {showApiKey ? <EyeOff size={16} /> : <Eye size={16} />}
                    </button>
                  </div>
                </div>

                <div className={`grid gap-3 ${canCopyEndpoints ? 'md:grid-cols-2' : ''}`}>
                  <button
                    type="button"
                    onClick={() => !loading && updateField('isDefault', !formData.isDefault)}
                    className={`flex min-h-[68px] w-full items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 text-left transition-all duration-200 ${
                      loading ? 'cursor-not-allowed opacity-70' : 'hover:border-slate-300'
                    }`}
                    disabled={loading}
                  >
                    <span className={`text-sm font-medium ${formData.isDefault ? 'text-indigo-700' : 'text-slate-600'}`}>
                      设为渠道默认分组
                    </span>
                    <div className={`relative h-5 w-10 rounded-full transition-colors duration-200 ${
                      formData.isDefault ? 'bg-indigo-500' : 'bg-slate-300'
                    }`}>
                      <div className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                      formData.isDefault ? 'translate-x-5' : 'translate-x-0'
                      }`} />
                    </div>
                  </button>

                  {canCopyEndpoints && (
                    <button
                      type="button"
                      onClick={() => !loading && updateCopyEndpointsEnabled(!copyEndpointsEnabled)}
                      className={`flex min-h-[68px] w-full items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 text-left transition-all duration-200 ${
                        loading ? 'cursor-not-allowed opacity-70' : 'hover:border-slate-300'
                      }`}
                      disabled={loading}
                    >
                      <span className={`text-sm font-medium ${copyEndpointsEnabled ? 'text-indigo-700' : 'text-slate-600'}`}>
                        复制其他分组端点
                      </span>
                      <div className={`relative h-5 w-10 rounded-full transition-colors duration-200 ${
                        copyEndpointsEnabled ? 'bg-indigo-500' : 'bg-slate-300'
                      }`}>
                        <div className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                          copyEndpointsEnabled ? 'translate-x-5' : 'translate-x-0'
                        }`} />
                      </div>
                    </button>
                  )}
                </div>
              </div>
            )}

            {activeTab === 'copy' && canCopyEndpoints && copyEndpointsEnabled && (
              <div className="space-y-4">
                <div className={`rounded-xl border border-slate-200 bg-slate-50 p-3 transition-all duration-200 ${
                  loading ? 'opacity-70' : 'hover:border-slate-300'
                }`}>
                  <div className="flex items-center gap-2">
                    <span className="shrink-0 text-xs font-medium text-slate-500">复制来源</span>
                    <div className="min-w-0 flex-1">
                      <EndpointCopySourcePicker
                        options={copyChoices}
                        value={formData.copyEndpointsFromGroupId || ''}
                        onChange={updateCopySource}
                        disabled={loading}
                      />
                    </div>
                  </div>
                </div>
              </div>
            )}

            {activeTab === 'billing' && (
              <div className="space-y-4">
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">普通成本倍率</label>
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                    {STANDARD_ENDPOINT_MULTIPLIER_FIELDS.map(({ key, label }) => (
                      <div key={key}>
                        <label className="mb-1 block text-xs text-slate-500">{label}</label>
                        <input
                          type="text"
                          inputMode="decimal"
                          value={formData[key]}
                          onChange={(event) => updateField(key, event.target.value)}
                          className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                          placeholder="1.0"
                          disabled={loading}
                        />
                      </div>
                    ))}
                  </div>
                  <p className="mt-1 text-xs text-slate-400">成本倍率直接作为该分组下 Codex 端点的计费倍率，{MULTIPLIER_EXPRESSION_HELP}</p>
                </div>

                <button
                  type="button"
                  role="switch"
                  aria-checked={!formData.disableLongContextPricing}
                  onClick={handleLongContextToggle}
                  className={`flex w-full items-center justify-between gap-3 rounded-xl border p-3 text-left transition-colors ${
                    formData.disableLongContextPricing
                      ? 'border-slate-200 bg-slate-50'
                      : 'border-indigo-200 bg-indigo-50/60'
                  } ${loading ? 'cursor-not-allowed opacity-70' : 'hover:border-indigo-300'}`}
                  disabled={loading}
                >
                  <div>
                    <div className="text-sm font-medium text-slate-700">Long context 计费</div>
                    <div className="mt-0.5 text-xs text-slate-500">
                      {formData.disableLongContextPricing ? '不启用，始终使用普通模型价格' : '跟随模型定价，命中阈值后应用 Long context 倍率'}
                    </div>
                  </div>
                  <span className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                    formData.disableLongContextPricing ? 'bg-slate-300' : 'bg-indigo-600'
                  }`}>
                    <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform ${
                      formData.disableLongContextPricing ? 'translate-x-0.5' : 'translate-x-[1.375rem]'
                    }`} />
                  </span>
                </button>

                {!formData.disableLongContextPricing && (
                  <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <div className="text-sm font-medium text-slate-700">Long context 倍率已配置</div>
                        <div className="mt-0.5 text-xs text-slate-500">命中阈值后使用以下倍率</div>
                      </div>
                      <Button type="button" variant="secondary" size="sm" onClick={() => setLongContextSettingsOpen(true)} disabled={loading}>编辑</Button>
                    </div>
                    <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-5">
                      {LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS.map(({ key, label }) => (
                        <div key={key} className="rounded-lg border border-slate-200 bg-white px-2.5 py-2">
                          <div className="truncate text-[11px] text-slate-400">{label.replace('Long context ', '')}</div>
                          <div className="mt-0.5 truncate text-sm font-medium text-slate-700">{formData[key]}</div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {longContextSettingsOpen && (
                  <LongContextMultiplierSettings
                    fields={LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS}
                    values={formData}
                    disabled={loading}
                    onCancel={() => setLongContextSettingsOpen(false)}
                    onSave={handleLongContextSave}
                  />
                )}

                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">默认输出计费口径</label>
                  <CustomSelect
                    options={BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS}
                    value={formData.billingOutputTokenSource}
                    onChange={(value) => updateField('billingOutputTokenSource', value)}
                    size="md"
                    fullWidth
                  />
                </div>

                <ModelMultiplierEditor
                  rows={formData.modelMultiplierRows}
                  onChange={updateModelMultiplierRows}
                  disabled={loading}
                />
              </div>
            )}

            <div className="mt-5 flex justify-end gap-3 border-t border-slate-100 pt-4">
              <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>取消</Button>
              <Button type="submit" variant="primary" loading={loading}>
                {isEdit ? '保存分组' : copyEndpointsEnabled ? '下一步' : '添加分组'}
              </Button>
            </div>
              </div>
            </>
          )}
        </form>
      </div>
    </div>
  );
};

// ============================================================================
// 端点编辑弹窗（name + base_url）
// ============================================================================
const EndpointToggleTile = ({ label, checked, onChange, disabled }) => {
  const toggle = () => {
    if (!disabled) onChange?.(!checked);
  };

  return (
    <div
      onClick={toggle}
      className={`flex cursor-pointer items-center justify-between rounded-xl border p-3 transition-colors ${
        checked ? 'border-emerald-200 bg-white' : 'border-slate-200 bg-white hover:bg-slate-50/60'
      } ${disabled ? 'cursor-not-allowed opacity-70' : ''}`}
      role="button"
      tabIndex={disabled ? -1 : 0}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          toggle();
        }
      }}
    >
      <span className="min-w-0 truncate text-xs font-semibold text-slate-800">{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        onClick={(event) => {
          event.stopPropagation();
          toggle();
        }}
        className={`relative inline-flex h-5 w-9 flex-shrink-0 items-center rounded-full border transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500/30 ${
          checked ? 'border-emerald-500 bg-emerald-500' : 'border-slate-200 bg-slate-200'
        } ${disabled ? 'cursor-not-allowed' : 'cursor-pointer'}`}
      >
        <span
          className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </button>
    </div>
  );
};

const EndpointFormModal = ({ mode, channel, endpoint, credentialGroup, loading, serverError, onCancel, onSave }) => {
  const [name, setName] = useState(endpoint?.name || '');
  const [baseURL, setBaseURL] = useState(endpoint?.baseURL || endpoint?.base_url || '');
  const [activeTab, setActiveTab] = useState('basic');
  const credentialGroups = channel?.credentialGroups || [];
  const defaultCredentialGroup =
    credentialGroups.find((group) => group.isDefault || group.is_default) || credentialGroups[0] || null;
  const initialCredentialGroupId =
    endpoint?.credentialGroupId || endpoint?.credential_group_id || credentialGroup?.id || defaultCredentialGroup?.id || 0;
  const [credentialGroupId, setCredentialGroupId] = useState(initialCredentialGroupId);
  const selectedCredentialGroup =
    credentialGroups.find((group) => Number(group.id) === Number(credentialGroupId)) ||
    credentialGroup ||
    defaultCredentialGroup;
  const selectedCredentialGroupId = Number(selectedCredentialGroup?.id || credentialGroupId || 0);
  const credentialGroupOptions = [
    ...(!credentialGroups.some((group) => Number(group.id) === selectedCredentialGroupId) && selectedCredentialGroupId > 0
      ? [{ value: String(selectedCredentialGroupId), label: `${selectedCredentialGroup?.name || '当前分组'} (当前)` }]
      : []),
    ...credentialGroups.map((group) => ({
      value: String(group.id),
      label: `${group.name || '默认分组'}${group.isDefault || group.is_default ? ' · 默认' : ''}`
    }))
  ];
  const isEdit = mode === 'edit';
  const initialPriority = getNextCodexEndpointPriority(channel, initialCredentialGroupId);
  const [priority, setPriority] = useState(() => (isEdit ? String(endpoint?.priority ?? initialPriority) : ''));
  const [timeoutSeconds, setTimeoutSeconds] = useState(formatEndpointTimeoutInputValue(endpoint?.timeoutSeconds ?? endpoint?.timeout_seconds ?? 0));
  const [failoverEnabled, setFailoverEnabled] = useState(endpoint?.failoverEnabled !== false);
  const [skipHealthCheck, setSkipHealthCheck] = useState(Boolean(endpoint?.skipHealthCheck || endpoint?.skip_health_check));
  const [fieldError, setFieldError] = useState('');
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;
  const groupMultiplierItems = [
    ['总', selectedCredentialGroup?.costMultiplier ?? 1],
    ['输入', selectedCredentialGroup?.inputCostMultiplier ?? 1],
    ['输出', selectedCredentialGroup?.outputCostMultiplier ?? 1],
    ['缓存读取', selectedCredentialGroup?.cacheReadCostMultiplier ?? 1],
    ['缓存创建', selectedCredentialGroup?.cacheCreationCostMultiplier ?? 1],
    ['Long context', selectedCredentialGroup?.longContextCostMultiplier ?? 1]
  ];
  const groupBillingSource = selectedCredentialGroup?.billingOutputTokenSource || 'output_tokens';
  const groupBillingLabel = BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS.find((option) => option.value === groupBillingSource)?.label || groupBillingSource;

  const handleSubmit = async (event) => {
    event.preventDefault();
    const trimmedName = name.trim();
    const trimmed = baseURL.trim();
    if (!trimmedName) {
      setActiveTab('basic');
      setFieldError('端点名称不能为空');
      return;
    }
    if (!trimmed) {
      setActiveTab('basic');
      setFieldError('base_url 不能为空');
      return;
    }
    if (!/^https?:\/\//i.test(trimmed)) {
      setActiveTab('basic');
      setFieldError('base_url 必须以 http:// 或 https:// 开头');
      return;
    }
    const priorityResult = parseEndpointPriorityPayload(priority);
    if (priorityResult.error) {
      setActiveTab('routing');
      setFieldError(priorityResult.error);
      return;
    }
    const timeoutResult = parseEndpointTimeoutPayload(timeoutSeconds);
    if (timeoutResult.error) {
      setActiveTab('routing');
      setFieldError(timeoutResult.error);
      return;
    }
    await onSave?.({
      name: trimmedName,
      baseURL: trimmed,
      credentialGroupId: selectedCredentialGroupId,
      failoverEnabled,
      skipHealthCheck,
      priority: priorityResult.priority,
      timeoutSeconds: timeoutResult.timeoutSeconds
    });
  };

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div
        className="flex w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-center justify-between border-b border-slate-100 px-6 py-4">
          <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑端点' : '新建端点'}</h2>
          <button
            type="button"
            onClick={onCancel}
            className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
            disabled={loading}
            aria-label="关闭"
          >
            <X size={20} />
          </button>
        </div>

        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={handleSubmit} autoComplete="off">
          <div className="flex-shrink-0 border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <SegmentedTabs
              tabs={CODEX_ENDPOINT_FORM_TABS}
              activeTab={activeTab}
              onChange={setActiveTab}
              disabled={loading}
              className="grid-cols-2"
              buttonPaddingClass="px-2 sm:px-3"
            />
          </div>
          <div className="space-y-4 overflow-y-auto px-6 py-6">
            {(fieldError || serverError) && (
              <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            <div className="rounded-xl border border-slate-200 bg-slate-50/70 p-3">
              <div className="flex items-center gap-2 text-xs font-medium text-slate-700">
                <KeyRound size={14} />
                归属凭证分组
              </div>
              <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2 text-xs">
                <span className="inline-flex max-w-full items-center gap-1 rounded-md bg-white px-2.5 py-1.5 font-medium text-slate-700">
                  <span className="truncate">{selectedCredentialGroup?.name || '默认分组'}</span>
                  {(selectedCredentialGroup?.isDefault || selectedCredentialGroup?.is_default) && (
                    <span className="rounded bg-emerald-50 px-1 text-[10px] font-semibold text-emerald-600">默认</span>
                  )}
                </span>
                {groupMultiplierItems.map(([label, value]) => (
                  <span key={label} className="inline-flex items-center gap-1 rounded-md bg-white px-2 py-1 font-mono text-[11px] font-semibold text-slate-600">
                    <span className="font-sans text-[10px] font-medium text-slate-400">{label}</span>
                    {formatMultiplierExpression(Number(value) || 1, 3)}x
                  </span>
                ))}
                <span className="inline-flex min-w-0 items-center gap-1 rounded-md bg-white px-2 py-1 text-[11px] font-medium text-slate-500">
                  <Database size={11} />
                  <span className="truncate">{groupBillingLabel}</span>
                </span>
              </div>
            </div>

            {activeTab === 'basic' ? (
              <div className="space-y-4">
                <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-500">基本信息</h3>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">
                      渠道 <span className="text-rose-500">*</span>
                    </label>
                    <input
                      type="text"
                      value={channel?.name || ''}
                      disabled
                      className="w-full cursor-not-allowed rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-400"
                    />
                    <p className="mt-1 text-xs text-slate-400">用于分块展示端点，并作为故障转移的第一层边界</p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">凭证分组</label>
                    <CustomSelect
                      options={credentialGroupOptions}
                      value={String(selectedCredentialGroupId || '')}
                      onChange={(value) => {
                        const nextCredentialGroupId = Number(value) || 0;
                        setCredentialGroupId(nextCredentialGroupId);
                        if (isEdit) {
                          if (nextCredentialGroupId !== Number(initialCredentialGroupId || 0)) {
                            setPriority(String(getNextCodexEndpointPriority(channel, nextCredentialGroupId)));
                          } else {
                            setPriority(String(endpoint?.priority ?? getNextCodexEndpointPriority(channel, nextCredentialGroupId)));
                          }
                        }
                        if (fieldError) setFieldError('');
                      }}
                      disabled={loading || credentialGroupOptions.length === 0}
                      size="md"
                      fullWidth
                      placeholder="请选择凭证分组"
                    />
                    <p className="mt-1 text-xs text-slate-400">
                      端点归属到该分组后复用其中的 API Key 与默认倍率
                    </p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">
                      端点名称 <span className="text-rose-500">*</span>
                    </label>
                    <input
                      type="text"
                      autoFocus
                      value={name}
                      onChange={(event) => {
                        setName(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      placeholder="e.g. api-primary"
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    />
                    <p className="mt-1 text-xs text-slate-400">同一凭证分组下端点名称唯一，仅作 UI 标识</p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">
                      URL <span className="text-rose-500">*</span>
                    </label>
                    <input
                      type="text"
                      value={baseURL}
                      onChange={(event) => {
                        setBaseURL(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      placeholder="https://api.example.com"
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    />
                    <p className="mt-1 text-xs text-slate-400">
                      端点只保存线路信息；API Key 与计费倍率来自上方凭证分组
                    </p>
                  </div>
                </div>
              </div>
            ) : (
              <div className="space-y-4">
                <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-500">路由配置</h3>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">优先级</label>
                    <input
                      type="number"
                      min="0"
                      value={priority}
                      onChange={(event) => {
                        setPriority(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      disabled={loading}
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20 disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-400"
                      placeholder={isEdit ? '0 = 最高优先级' : `留空自动分配 P${getNextCodexEndpointPriority(channel, selectedCredentialGroupId)}`}
                    />
                    <p className="mt-1 text-xs text-slate-400">
                      {isEdit ? '0 = 最高优先级，数字越小越优先，同分组相同则交换' : `留空自动分配为 P${getNextCodexEndpointPriority(channel, selectedCredentialGroupId)}；填写后按填写值保存，重复则替换`}
                    </p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">超时时间 (秒)</label>
                    <input
                      type="number"
                      min="0"
                      step="1"
                      value={timeoutSeconds}
                      onChange={(event) => {
                        setTimeoutSeconds(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                      placeholder="默认: 300 (全局配置)"
                    />
                    <p className="mt-1 text-xs text-slate-400">单次请求的最大等待时间，0 使用全局配置</p>
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Tooltip content="关闭后该端点不会参与同一渠道内的端点故障转移" className="block">
                    <EndpointToggleTile
                      label="参与渠道内故障转移"
                      checked={failoverEnabled}
                      onChange={setFailoverEnabled}
                      disabled={loading}
                    />
                  </Tooltip>
                  <Tooltip content="部分中转商不支持健康检查端点，开启后将跳过定时健康检测" className="block">
                    <EndpointToggleTile
                      label="跳过健康检查"
                      checked={skipHealthCheck}
                      onChange={setSkipHealthCheck}
                      disabled={loading}
                    />
                  </Tooltip>
                </div>
              </div>
            )}
          </div>

          <div className="flex flex-shrink-0 items-center justify-end gap-3 border-t border-slate-100 bg-white px-6 py-4">
            <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button variant="primary" icon={isEdit ? Pencil : Save} loading={loading} type="submit">
              {isEdit ? '保存修改' : '创建端点'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

// ============================================================================
// 删除确认弹窗（渠道 / 端点 共用）
// ============================================================================
const DeleteConfirmDialog = ({ title, description, hint, loading, onCancel, onConfirm }) => {
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div className="flex w-full max-w-md flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <div className="flex items-start gap-3 border-b border-slate-100 px-6 py-4">
          <div className="rounded-xl bg-rose-100 p-2 text-rose-600 shadow-sm">
            <TriangleAlert size={18} />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-slate-900">{title}</h2>
            {description && <p className="mt-1 text-sm text-slate-500">{description}</p>}
          </div>
        </div>
        {hint && <div className="px-6 py-4 text-sm text-slate-600">{hint}</div>}
        <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
          <Button variant="secondary" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button variant="danger" icon={Trash2} loading={loading} onClick={onConfirm}>
            确认删除
          </Button>
        </div>
      </div>
    </div>
  );
};

const ActiveChannelPauseDialog = ({ channelName, nextChannelName, loading, onCancel, onConfirm }) => renderModalPortal(
  <div className="fixed inset-0 z-[100] flex items-start justify-center bg-slate-900/30 px-4 pt-[20vh] backdrop-blur-sm animate-fade-in">
    <div className="w-full max-w-md overflow-hidden rounded-2xl bg-white shadow-xl">
      <div className="flex items-start gap-3 px-6 py-5">
        <div className="rounded-xl bg-amber-100 p-2 text-amber-700"><TriangleAlert size={18} /></div>
        <div>
          <h2 className="text-lg font-semibold text-slate-900">确认暂停活跃渠道</h2>
          <p className="mt-1 text-sm leading-6 text-slate-500">
            确定要暂停当前接管渠道“{channelName}”吗？暂停后将自动切换到“{nextChannelName}”。
          </p>
        </div>
      </div>
      <div className="flex justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
        <Button variant="secondary" onClick={onCancel} disabled={loading}>取消</Button>
        <Button variant="danger" loading={loading} onClick={onConfirm}>确认暂停</Button>
      </div>
    </div>
  </div>
);

// ============================================================================
// Codex 全局配置弹窗（直接读写 ~/.codex/config.toml）
// 字段：model / enable_1m_context（base_url 由后端自动同步）
// 显式保存模式：点击「保存」才写入文件
// ============================================================================
const DEFAULT_CONFIG = {
  model: '',
  enable_1m_context: false
};

const CodexGlobalConfigModal = ({ onClose, onSaved }) => {
  const [config, setConfig] = useState(DEFAULT_CONFIG);
  const [initialConfig, setInitialConfig] = useState(DEFAULT_CONFIG);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;

  useEffect(() => {
    let canceled = false;
    setLoading(true);
    getCodexGlobalConfig()
      .then((data) => {
        if (canceled) return;
        const loaded = {
          model: data.model || '',
          enable_1m_context: Boolean(data.enable_1m_context)
        };
        setConfig(loaded);
        setInitialConfig(loaded);
      })
      .catch((err) => {
        if (canceled) return;
        setError(getErrorMessage(err, '加载 Codex 全局配置失败'));
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, []);

  const updateField = useCallback((key, value) => {
    setConfig((prev) => ({ ...prev, [key]: value }));
    setError('');
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    try {
      await updateCodexGlobalConfig({
        model: config.model,
        enable_1m_context: config.enable_1m_context
      });
      await onSaved?.();
      onClose?.();
    } catch (err) {
      setError(getErrorMessage(err, '保存失败'));
    } finally {
      setSaving(false);
    }
  };

  const hasChanges =
    config.model !== initialConfig.model ||
    config.enable_1m_context !== initialConfig.enable_1m_context;

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div
        className="flex w-full max-w-xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              <Settings size={18} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">Codex 全局配置</h2>
              <p className="mt-0.5 text-xs text-slate-500">直接读写 ~/.codex/config.toml；代理地址自动同步自服务端口</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onClose}
              disabled={saving}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 disabled:opacity-50"
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-6 py-6">
          {loading ? (
            <div className="flex items-center gap-2 px-2 py-3 text-sm text-slate-500">
              <Loader2 size={14} className="animate-spin" />
              正在加载 config.toml...
            </div>
          ) : (
            <div className="space-y-5">
              {error && (
                <div className="flex items-start gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
                  <AlertCircle size={14} className="mt-0.5 flex-shrink-0" />
                  <span>{error}</span>
                </div>
              )}

              <div>
                <label className="mb-1.5 block text-sm font-medium text-slate-700">默认模型</label>
                <input
                  type="text"
                  value={config.model}
                  onChange={(e) => updateField('model', e.target.value)}
                  placeholder="例如：gpt-5.4"
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                />
                <p className="mt-1 text-xs text-slate-400">作为所有渠道的默认值；某个渠道可在其编辑弹窗里单独覆盖</p>
              </div>

              <div className="flex items-start justify-between gap-4 rounded-2xl border border-slate-200 bg-slate-50/70 px-4 py-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <label className="text-sm font-medium text-slate-700">1M 上下文窗口</label>
                    <span
                      className={`rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] ${
                        config.enable_1m_context
                          ? 'bg-emerald-100 text-emerald-700'
                          : 'bg-slate-200 text-slate-500'
                      }`}
                    >
                      {config.enable_1m_context ? '1M' : '默认'}
                    </span>
                  </div>
                  <p className="mt-1 text-xs leading-5 text-slate-500">
                    开启后写入 model_context_window = 1000000；关闭则移除该字段。
                  </p>
                </div>
                <Tooltip content={config.enable_1m_context ? '关闭 1M 上下文窗口' : '开启 1M 上下文窗口'}>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={config.enable_1m_context}
                    onClick={() => updateField('enable_1m_context', !config.enable_1m_context)}
                    className={`relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500 focus-visible:ring-offset-2 ${
                      config.enable_1m_context ? 'bg-emerald-500' : 'bg-slate-300 hover:bg-slate-400'
                    }`}
                  >
                    <span
                      className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform ${
                        config.enable_1m_context ? 'translate-x-5' : 'translate-x-0'
                      }`}
                    />
                  </button>
                </Tooltip>
              </div>
            </div>
          )}
        </div>

        <div className="flex flex-shrink-0 items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
          <Button variant="secondary" onClick={onClose} disabled={saving}>
            取消
          </Button>
          <Button
            variant="primary"
            onClick={handleSave}
            disabled={loading || saving || !hasChanges}
            loading={saving}
          >
            保存
          </Button>
        </div>
      </div>
    </div>
  );
};

// ============================================================================
// 渠道卡片（卡片头 + 端点子列表）
// ============================================================================
// ============================================================================
// 端点行（紧凑表格风，延续渠道管理页密度）：
// 左侧 toggle = 激活；行整体点击展开内嵌编辑（base_url 直改即保存）
// ============================================================================
const CodexEndpointDetailModal = ({ endpoint, channel, globalConfig, onClose, onEdit }) => {
  if (!endpoint) return null;

  const healthState = endpoint.healthState || endpoint.health_state || 'unchecked';
  const responseTimeMs = endpoint.responseTimeMs || endpoint.response_time_ms || 0;
  const lastFailCode = Number(endpoint.lastFailCode || endpoint.last_fail_code || 0);
  const lastFailReason = endpoint.lastFailReason || endpoint.last_fail_reason || '';
  const hasFailDetail = (Number.isFinite(lastFailCode) && lastFailCode > 0) || !!lastFailReason;
  const failoverEnabled = endpoint.failoverEnabled !== false;
  const endpointGroupID = Number(endpoint.credentialGroupId || endpoint.credential_group_id || 0);
  const credentialGroup = (channel?.credentialGroups || []).find((group) => Number(group.id) === endpointGroupID) ||
    getDefaultCredentialGroup(channel);
  const credentialGroupName = credentialGroup?.name || endpoint.credentialGroupName || endpoint.credential_group_name || '默认分组';
  const credentialGroupIsDefault = Boolean(credentialGroup?.isDefault || credentialGroup?.is_default);
  const credentialGroupKeyMasked = credentialGroup?.apiKeyMasked || '';
  const billingSource = credentialGroup?.billingOutputTokenSource || 'output_tokens';
  const billingOption = BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS.find(
    (option) => option.value === billingSource
  );
  const billingNameMap = {
    output_tokens: '总输出',
    reasoning_tokens: '推理输出',
    visible_output_tokens: '可见补全'
  };
  const billingName = billingNameMap[billingSource] || billingOption?.label || billingSource;
  const multiplierItems = [
    { label: '总', value: credentialGroup?.costMultiplier ?? 1, tone: 'indigo' },
    { label: '输入', value: credentialGroup?.inputCostMultiplier ?? 1, tone: 'slate' },
    { label: '输出', value: credentialGroup?.outputCostMultiplier ?? 1, tone: 'sky' },
    { label: '缓存', value: credentialGroup?.cacheReadCostMultiplier ?? 1, tone: 'amber' }
  ];
  const longContextPricingEnabled = Boolean(credentialGroup) && !Boolean(
    credentialGroup.disableLongContextPricing ?? credentialGroup.disable_long_context_pricing
  );
  const longContextMultiplierItems = [
    { label: '总', value: credentialGroup?.longContextCostMultiplier ?? credentialGroup?.long_context_cost_multiplier ?? 1, tone: 'indigo' },
    { label: '输入', value: credentialGroup?.longContextInputCostMultiplier ?? credentialGroup?.long_context_input_cost_multiplier ?? 1, tone: 'slate' },
    { label: '输出', value: credentialGroup?.longContextOutputCostMultiplier ?? credentialGroup?.long_context_output_cost_multiplier ?? 1, tone: 'sky' },
    { label: '缓存创建', value: credentialGroup?.longContextCacheCreationCostMultiplier ?? credentialGroup?.long_context_cache_creation_cost_multiplier ?? 1, tone: 'emerald' },
    { label: '缓存读取', value: credentialGroup?.longContextCacheReadCostMultiplier ?? credentialGroup?.long_context_cache_read_cost_multiplier ?? 1, tone: 'amber' }
  ];
  const priority = Number(endpoint.priority || 0);
  const effectiveModel = channel?.model || globalConfig?.model || '未设置';
  const endpointTimeoutSeconds = Number(endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0);
  const globalTimeoutSeconds = Number(globalConfig?.global_timeout_seconds ?? globalConfig?.globalTimeoutSeconds ?? 300) || 300;
  const hasCustomTimeout = endpointTimeoutSeconds > 0;
  const timeoutLabel = hasCustomTimeout ? `${endpointTimeoutSeconds}s` : `${globalTimeoutSeconds}s`;
  const timeoutScope = endpointTimeoutSeconds > 0 ? '端点覆盖' : '使用全局超时';
  const createdAt = getDetailTimestamp(endpoint.createdAt, endpoint.created_at, endpoint.CreatedAt);
  const updatedAt = getDetailTimestamp(endpoint.updatedAt, endpoint.updated_at, endpoint.UpdatedAt);
  const rawLastCheck = endpoint.lastCheck || endpoint.last_check || endpoint.LastCheck;
  const lastCheck = formatLastCheck(rawLastCheck || updatedAt);
  const modelMappingEntries = Object.entries(channel?.modelMapping || channel?.model_mapping || {})
    .map(([sourceModel, targetModel]) => [String(sourceModel || '').trim(), String(targetModel || '').trim()])
    .filter(([sourceModel, targetModel]) => sourceModel && targetModel);
  const hasModelMapping = modelMappingEntries.length > 0;

  const rows = [
    {
      label: '渠道',
      icon: Server,
      content: (
        <span className="text-sm font-semibold text-slate-900">{channel?.name || '-'}</span>
      )
    },
    {
      label: '模型',
      icon: Server,
      content: (
        <ModelTag model={effectiveModel} />
      )
    },
    {
      label: '凭证分组',
      icon: KeyRound,
      content: (
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-sm font-semibold text-slate-900">{credentialGroupName}</span>
            {credentialGroupIsDefault && (
              <span className="flex-shrink-0 rounded bg-emerald-50 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-600">
                默认
              </span>
            )}
          </div>
          <div className="mt-0.5 truncate font-mono text-[11px] text-slate-400">
            {credentialGroupKeyMasked || '使用分组凭证'}
          </div>
        </div>
      )
    },
    {
      label: '分组计费倍率',
      icon: Database,
      content: (
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          {multiplierItems.map((item) => (
            <DetailChip key={item.label} tone={item.tone} mono>
              <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">{item.label}</span>
              {formatDetailMultiplier(item.value)}
            </DetailChip>
          ))}
          <DetailChip tone="slate">
            <Database size={11} className="mr-1 flex-shrink-0 text-slate-400" />
            <span className="mr-1 text-[10px] text-slate-500">计费</span>
            <span className="min-w-0 truncate font-medium text-slate-700">{billingName}</span>
            <span className="ml-1 font-mono text-[9px] text-slate-400">{billingSource}</span>
          </DetailChip>
        </div>
      )
    }
  ];

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm animate-fade-in"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose?.();
      }}
    >
      <div
        className="flex max-h-[calc(100vh-2rem)] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="border-b border-slate-100 px-6 py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <h2 className="truncate text-lg font-semibold text-slate-900">{endpoint.name || '未命名端点'}</h2>
                <span className="inline-flex items-center rounded border border-slate-200 bg-slate-50 px-2 py-0.5 text-[10px] font-medium text-slate-500">
                  {channel?.name || 'Codex'}
                </span>
                {endpoint.isActive && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-medium text-emerald-700">
                    <span className="h-1 w-1 rounded-full bg-emerald-500" />
                    当前端点
                  </span>
                )}
              </div>
              <p className="mt-1 truncate font-mono text-xs text-slate-500">{endpoint.baseURL || '<未设置 base_url>'}</p>
            </div>

            <div className="flex flex-shrink-0 items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                icon={Pencil}
                onClick={() => onEdit?.(endpoint)}
              >
                编辑
              </Button>
              <Button variant="ghost" size="sm" onClick={onClose}>
                关闭
              </Button>
            </div>
        </div>

          <div className="mt-2 flex min-w-0 flex-nowrap items-center gap-1.5 overflow-x-auto pb-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            <CodexHealthBadge state={healthState} detail />
            {!hasFailDetail && <CodexLatencyBadge ms={responseTimeMs} detail />}
            <DetailPill tone="indigo" icon={Settings} mono>
              P{priority}
            </DetailPill>
            <DetailTimeoutPill
              custom={hasCustomTimeout}
              value={timeoutLabel}
              tooltip={`${timeoutScope}: ${timeoutLabel}`}
            />
            <DetailPill tone="indigo" icon={Database}>
              <span className="leading-none">分组倍率</span>
              <span className="font-mono leading-none">{formatDetailMultiplier(credentialGroup?.costMultiplier ?? 1)}</span>
            </DetailPill>
            <DetailPill tone={failoverEnabled ? 'emerald' : 'slate'} icon={failoverEnabled ? Repeat : RepeatOffIcon}>
              {failoverEnabled ? '参与故障转移' : '不参与转移'}
            </DetailPill>
            <DetailPill tone="sky" icon={Database}>
              <span className="leading-none">{billingName}</span>
              <span className="font-mono text-[9px] leading-none opacity-75">{billingSource}</span>
            </DetailPill>
            <CodexRequestFailureBadge failCode={lastFailCode} failReason={lastFailReason} detail hideWhenEmpty />
            <span className="flex-shrink-0 whitespace-nowrap font-mono text-[10px] text-slate-400">
              最后检查 {lastCheck}
            </span>
          </div>
          {hasFailDetail && (
            <div className="mt-3 rounded-xl border border-rose-200 bg-rose-50 p-4">
              <div className="mb-2 flex items-center gap-2">
                <XCircle size={16} className="text-rose-600" />
                <div className="text-sm font-medium text-rose-800">异常详情</div>
              </div>
              <div className="space-y-2 text-sm">
                {lastFailCode > 0 && (
                  <div className="flex items-start gap-2">
                    <span className="min-w-[70px] font-medium text-rose-600">状态码:</span>
                    <span className="rounded bg-rose-100 px-2 py-0.5 font-mono text-rose-900">{lastFailCode}</span>
                  </div>
                )}
                {lastFailReason && (
                  <div className="flex items-start gap-2">
                    <span className="min-w-[70px] font-medium text-rose-600">失败原因:</span>
                    <span className="break-all text-rose-900">{lastFailReason}</span>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>

        <div className="overflow-y-auto p-6">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {rows.map((row) => (
              <DetailMetricCard
                key={row.label}
                label={row.label}
                icon={row.icon}
                wide={row.wide}
              >
                {row.content}
              </DetailMetricCard>
            ))}
            {longContextPricingEnabled && (
              <DetailMetricCard label="Long context 分组倍率" icon={Database} wide>
                <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                  {longContextMultiplierItems.map((item) => (
                    <DetailChip key={item.label} tone={item.tone} mono>
                      <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">{item.label}</span>
                      {formatDetailMultiplier(item.value)}
                    </DetailChip>
                  ))}
                </div>
              </DetailMetricCard>
            )}
          </div>

          {hasModelMapping && (
            <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
              <DetailMetricCard label="渠道模型映射" icon={ArrowRight} wide>
                <div className="space-y-2">
                  {modelMappingEntries.map(([sourceModel, targetModel]) => (
                    <ModelMappingPair
                      key={`${sourceModel}->${targetModel}`}
                      sourceModel={sourceModel}
                      targetModel={targetModel}
                    />
                  ))}
                </div>
              </DetailMetricCard>
            </div>
          )}

          <DetailTimestampFooter createdAt={createdAt} updatedAt={updatedAt} />
        </div>
      </div>
    </div>
  );
};

// ============================================================================
// 端点行（精简表格风）：
//   左侧 toggle = 激活；右侧 hover 浮现编辑/删除按钮（编辑走弹窗，不再使用行内展开）
// ============================================================================
const EndpointRow = ({
  endpoint,
  credentialGroup,
  suspendedCount = 0,
  onActivate,
  onEdit,
  onDelete,
  onOpen,
  onToggleFailover,
  onToggleSkipHealthCheck,
  actionLoading,
  canDisableFailover = true,
  channelPaused = false
}) => {
  const isActivating = actionLoading === `activate-endpoint-${endpoint.id}`;
  const isDeleting = actionLoading === `delete-endpoint-${endpoint.id}`;
  const isTogglingSkipHealthCheck = actionLoading === `skip-health-endpoint-${endpoint.id}`;
  const displayName = endpoint.name || '未命名端点';
  const endpointURL = endpoint.baseURL || endpoint.base_url || '';
  const failoverEnabled = endpoint.failoverEnabled !== false;
  const skipHealthCheck = Boolean(endpoint.skipHealthCheck || endpoint.skip_health_check);
  const priority = endpoint.priority || 0;
  const healthState = endpoint.healthState || endpoint.health_state || 'unchecked';
  const responseTimeMs = endpoint.responseTimeMs || endpoint.response_time_ms || 0;
  const lastFailCode = endpoint.lastFailCode || endpoint.last_fail_code || 0;
  const lastFailReason = endpoint.lastFailReason || endpoint.last_fail_reason || '';
  const groupMultiplier = credentialGroup?.costMultiplier ?? endpoint.costMultiplier ?? 1;
  const groupMultiplierValue = normalizeMultiplierValue(groupMultiplier);
  const groupMultiplierLabel = `${formatMultiplierExpression(groupMultiplierValue, 3)}x`;
  const isCustomGroupMultiplier = Math.abs(groupMultiplierValue - 1) > 0.000001;

  // 挂起态：当请求挂起且当前端点已激活时，toggle 变 amber，允许重复点击触发恢复
  const isSuspended = suspendedCount > 0;
  const showAmber = endpoint.isActive && isSuspended;
  const canRepeatActivate = endpoint.isActive && isSuspended;
  const endpointSwitchDisabled = !failoverEnabled;
  const failoverToggleBlocked = failoverEnabled && endpoint.isActive && !canDisableFailover;
  const hasRequestFailure = Number(lastFailCode) > 0 || !!lastFailReason;
  const showChannelPaused = channelPaused && failoverEnabled;

  const handleToggleActivate = (event) => {
    event.stopPropagation();
    if (isActivating) return;
    if (endpointSwitchDisabled) return;
    // 正常态：已激活端点不可重复激活；挂起态：允许重复点击作为运维干预信号
    if (endpoint.isActive && !canRepeatActivate) return;
    onActivate?.(endpoint);
  };

  const handleEdit = (event) => {
    event.stopPropagation();
    onEdit?.(endpoint);
  };

  const handleDelete = (event) => {
    event.stopPropagation();
    if (isDeleting) return;
    onDelete?.(endpoint);
  };

  const handleFailoverToggle = (event) => {
    event.stopPropagation();
    if (failoverToggleBlocked) return;
    onToggleFailover?.(endpoint, !failoverEnabled);
  };

  const handleSkipHealthCheckToggle = (event) => {
    event.stopPropagation();
    if (isTogglingSkipHealthCheck) return;
    onToggleSkipHealthCheck?.(endpoint, !skipHealthCheck);
  };

  return (
    <div className="border-b border-slate-100 last:border-b-0">
      <div
        className={`group/row flex h-[56px] min-h-[56px] max-h-[56px] items-start gap-3 px-4 py-1.5 text-left transition-colors duration-150 ${
          endpoint.isActive ? 'bg-emerald-50/30' : 'hover:bg-slate-50/80'
        }`}
      >
        <div className="flex h-5 w-[74px] flex-shrink-0 self-center items-center gap-3">
          {/* 激活 Toggle（Apple HIG 风格） */}
          <button
            type="button"
            onClick={handleToggleActivate}
            className={`${
              endpointSwitchDisabled
                ? 'cursor-not-allowed'
                : canRepeatActivate
                ? 'cursor-pointer'
                : endpoint.isActive
                  ? 'cursor-default'
                  : 'cursor-pointer'
            }`}
            aria-label={
              endpointSwitchDisabled
                ? '端点未参与故障转移，无法设为当前端点'
                : endpoint.isActive
                  ? '当前激活端点'
                  : '设为当前端点'
            }
            aria-pressed={endpoint.isActive}
            aria-disabled={endpointSwitchDisabled}
          >
            <div
              className={`relative h-[18px] w-8 rounded-full transition-colors duration-200 ease-out ${
                showAmber
                  ? 'bg-amber-500 hover:bg-amber-600'
                  : endpoint.isActive
                    ? 'bg-emerald-500'
                    : endpointSwitchDisabled
                      ? 'bg-slate-100'
                    : isActivating
                      ? 'bg-emerald-300'
                      : 'bg-slate-200 group-hover/row:bg-slate-300'
              }`}
            >
              <div
                className={`absolute top-[2px] h-[14px] w-[14px] rounded-full bg-white shadow-[0_1px_2px_rgba(15,23,42,0.15)] transition-all duration-200 ease-out ${
                  endpoint.isActive ? 'left-[16px]' : 'left-[2px]'
                } ${isActivating ? 'animate-pulse' : ''}`}
              />
            </div>
          </button>

          {/* 优先级标签 */}
          <span className="inline-flex h-[18px] min-w-[30px] flex-shrink-0 items-center justify-center rounded bg-slate-100 px-1.5 text-[10px] font-medium leading-none text-slate-500">
            P{priority}
          </span>
        </div>

        {/* 端点摘要：名称独占第一行，状态标签放第二行避免挤压名称 */}
        <div
          role="button"
          tabIndex={0}
          onClick={() => onOpen?.(endpoint)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault();
              onOpen?.(endpoint);
            }
          }}
          className="min-w-0 flex-1 cursor-pointer rounded-md py-0.5 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300"
        >
          <div className="flex h-5 min-w-0 items-center">
            <span
              className={`relative inline-flex h-5 min-w-0 max-w-full items-center overflow-visible text-xs font-semibold leading-none ${
                endpoint.isActive ? 'text-slate-900' : 'text-slate-700'
              }`}
            >
              <Tooltip
                content={endpointURL || displayName}
                className={`min-w-0 ${isCustomGroupMultiplier ? 'pr-8' : ''}`}
                contentClassName="max-w-[420px] break-all font-mono"
              >
                <span className="block min-w-0 truncate leading-none">
                  {displayName}
                </span>
              </Tooltip>
              {isCustomGroupMultiplier && (
                <span className="pointer-events-none absolute -top-1 right-0 z-10 inline-flex h-3 w-fit flex-shrink-0 items-center justify-center rounded-full bg-amber-500 px-1 font-mono text-[8px] font-bold leading-none text-white shadow-[0_1px_1px_rgba(217,119,6,0.35)] whitespace-nowrap">
                  {groupMultiplierLabel}
                </span>
              )}
            </span>
          </div>
          <div className="mt-0.5 flex min-w-0 flex-nowrap items-center gap-1.5 overflow-visible">
            {showChannelPaused ? (
              <span
                className="inline-flex h-5 w-fit flex-shrink-0 items-center justify-center rounded-full border border-slate-200 bg-slate-50 px-1.5 text-[10px] font-medium leading-none text-slate-500 whitespace-nowrap"
                aria-label="渠道已暂停：不参与健康检查与故障转移"
              >
                暂停
              </span>
            ) : (
              <>
                <CodexHealthBadge state={healthState} />
                {hasRequestFailure ? (
                  <CodexRequestFailureBadge failCode={lastFailCode} failReason={lastFailReason} />
                ) : (
                  <CodexLatencyBadge ms={responseTimeMs} />
                )}
              </>
            )}
          </div>
        </div>

        {/* 操作按钮（常驻显示） */}
        <div className="flex flex-shrink-0 self-center items-center gap-0.5">
          <button
            type="button"
            onClick={handleFailoverToggle}
            disabled={failoverToggleBlocked}
            title={failoverToggleBlocked ? '至少保留一个参与转移的备用端点' : undefined}
            className={`${ENDPOINT_STATE_BUTTON_CLASS} ${
              failoverToggleBlocked
                ? 'cursor-not-allowed text-indigo-300 opacity-60'
                : failoverEnabled
                ? 'bg-indigo-50 text-indigo-600 hover:bg-indigo-100'
                : 'text-slate-400 hover:bg-slate-100 hover:text-indigo-600'
            }`}
            aria-label={
              failoverToggleBlocked
                ? '至少保留一个参与转移的备用端点'
                : failoverEnabled
                ? (endpoint.isActive
                    ? '点击：不参与故障转移，并自动切换当前端点'
                    : '点击：不参与故障转移')
                : '点击：参与故障转移'
            }
            aria-pressed={failoverEnabled}
          >
            {failoverEnabled ? <Repeat size={12} /> : <RepeatOffIcon size={12} />}
          </button>
          <button
            type="button"
            onClick={handleSkipHealthCheckToggle}
            disabled={isTogglingSkipHealthCheck}
            className={`${ENDPOINT_STATE_BUTTON_CLASS} ${
              isTogglingSkipHealthCheck
                ? 'cursor-wait text-emerald-400 opacity-70'
                : skipHealthCheck
                  ? 'text-slate-400 hover:bg-slate-100 hover:text-emerald-600'
                  : 'bg-emerald-50 text-emerald-600 hover:bg-emerald-100'
            }`}
            aria-label={skipHealthCheck ? '已跳过健康检测 (点击关闭)' : '跳过健康检测 (点击开启)'}
            aria-pressed={skipHealthCheck}
          >
            {isTogglingSkipHealthCheck ? (
              <Loader2 size={12} className="animate-spin" />
            ) : skipHealthCheck ? <ShieldOff size={12} /> : <ShieldCheck size={12} />}
          </button>
          <button
            type="button"
            onClick={handleEdit}
            className="rounded-md p-1 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-indigo-600"
            aria-label="编辑端点"
          >
            <Pencil size={12} />
          </button>
          <button
            type="button"
            onClick={handleDelete}
            disabled={isDeleting}
            className="rounded-md p-1 text-slate-400 transition-colors duration-150 hover:bg-rose-50 hover:text-rose-600 disabled:opacity-50"
            aria-label="删除端点"
          >
            {isDeleting ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
          </button>
        </div>
      </div>
    </div>
  );
};

// ============================================================================
// 渠道卡片（紧凑小卡片：仅展示渠道配置，不显示具体端点 URL）
// ============================================================================
const ChannelCard = ({
  channel,
  actionLoading,
  suspendedCount = 0,
  globalConfig = { model: '', enable_1m_context: false },
  channelFailoverEnabled = false,
  onActivateChannel,
  onToggleChannelFailover,
  onEditChannel,
  onDeleteChannel,
  onAddCredentialGroup,
  onEditCredentialGroup,
  onSetDefaultCredentialGroup,
  onDeleteCredentialGroup,
  onAddEndpoint,
  onActivateEndpoint,
  onToggleEndpointFailover,
  onSetGroupEndpointFailover,
  onSetGroupEndpointSkipHealthCheck,
  onToggleEndpointSkipHealthCheck,
  onEditEndpoint,
  onOpenEndpoint,
  onDeleteEndpoint
}) => {
  const [isEndpointListExpanded, setIsEndpointListExpanded] = useState(false);
  const {
    scrollRef: credentialGroupScrollRef,
    isDragging: isCredentialGroupDragging,
    scrollHandlers: credentialGroupScrollHandlers
  } = useHorizontalScroll();
  const channelCardRef = useRef(null);
  const isSwitchingChannel = actionLoading === `activate-channel-${channel.id}`;
  const isDeletingChannel = actionLoading === `delete-channel-${channel.id}`;
  const isTogglingChannelFailover = actionLoading === `toggle-channel-failover-${channel.id}`;
  const channelActivationDisabled = channelFailoverEnabled && channel.failoverEnabled === false;
  const endpoints = channel.endpoints || [];
  const credentialGroups = channel.credentialGroups || [];
  const defaultCredentialGroup = getDefaultCredentialGroup(channel);
  const activeEndpointInChannel = endpoints.find((ep) => ep.isActive) || null;
  const activeEndpointGroupID = Number(activeEndpointInChannel?.credentialGroupId || activeEndpointInChannel?.credential_group_id || 0);
  const preferredCredentialGroupID = activeEndpointGroupID || Number(defaultCredentialGroup?.id || credentialGroups[0]?.id || 0);
  const [selectedCredentialGroupID, setSelectedCredentialGroupID] = useState(preferredCredentialGroupID);
  const selectedCredentialGroup = credentialGroups.find((group) => Number(group.id) === Number(selectedCredentialGroupID)) ||
    credentialGroups.find((group) => Number(group.id) === preferredCredentialGroupID) ||
    defaultCredentialGroup ||
    credentialGroups[0] ||
    null;
  const selectedCredentialGroupIDResolved = Number(selectedCredentialGroup?.id || 0);
  const groupEndpointFilter = (endpoint) => {
    const endpointGroupID = Number(endpoint.credentialGroupId || endpoint.credential_group_id || 0);
    if (selectedCredentialGroupIDResolved > 0) {
      if (selectedCredentialGroup?.isDefault || selectedCredentialGroup?.is_default) {
        return endpointGroupID === selectedCredentialGroupIDResolved || endpointGroupID === 0;
      }
      return endpointGroupID === selectedCredentialGroupIDResolved;
    }
    return endpointGroupID === 0;
  };
  const getCredentialGroupForEndpoint = (endpoint) => {
    const endpointGroupID = Number(endpoint.credentialGroupId || endpoint.credential_group_id || 0);
    if (endpointGroupID > 0) {
      return credentialGroups.find((group) => Number(group.id) === endpointGroupID) || defaultCredentialGroup || null;
    }
    return defaultCredentialGroup || selectedCredentialGroup || null;
  };
  const groupEndpoints = sortEndpointsByPriority(endpoints.filter(groupEndpointFilter));
  const fallbackApiKeyMasked = defaultCredentialGroup?.apiKeyMasked || channel.apiKeyMasked || '';
  const extraCredentialGroupCount = Math.max(
    credentialGroups.length - (defaultCredentialGroup ? 1 : 0),
    0
  );
  const hasMultiple = groupEndpoints.length > 1;
  const activeSuspendedCount = channel.isActive ? suspendedCount : 0;
  const routableEndpoints = endpoints.filter((ep) => ep.failoverEnabled !== false);
  const hasCodexHealthVerdict = routableEndpoints.some((ep) => {
    const state = ep.healthState || ep.health_state || 'unchecked';
    return state === 'healthy' || state === 'unhealthy';
  });
  const isActiveChannelInterrupted = channel.isActive
    && routableEndpoints.length > 0
    && hasCodexHealthVerdict
    && routableEndpoints.every((ep) => {
      const state = ep.healthState || ep.health_state || 'unchecked';
      return state === 'unhealthy';
    });
  const isActiveChannelSuspended = channel.isActive && activeSuspendedCount > 0 && !isActiveChannelInterrupted;
  const activeCardClass = isActiveChannelInterrupted
    ? 'border-rose-300 ring-1 ring-rose-100'
    : isActiveChannelSuspended
      ? 'border-amber-300 ring-1 ring-amber-100'
      : 'border-emerald-200 ring-1 ring-emerald-100';

  // 折叠时也按优先级展示第一条，展开后展示完整优先级排序列表。
  const displayedEndpoints = isEndpointListExpanded || !hasMultiple
    ? groupEndpoints
    : groupEndpoints.slice(0, 1);
  const hiddenCount = hasMultiple && !isEndpointListExpanded ? groupEndpoints.length - displayedEndpoints.length : 0;
  const groupFailoverEndpoints = groupEndpoints.filter((ep) => ep.failoverEnabled !== false);
  const groupNonFailoverEndpoints = groupEndpoints.filter((ep) => ep.failoverEnabled === false);
  const groupSkippedHealthEndpoints = groupEndpoints.filter((ep) => ep.skipHealthCheck || ep.skip_health_check);
  const groupCheckedHealthEndpoints = groupEndpoints.filter((ep) => !(ep.skipHealthCheck || ep.skip_health_check));
  const activeEndpointInSelectedGroup = !!activeEndpointInChannel &&
    groupEndpoints.some((ep) => ep.id === activeEndpointInChannel.id);
  const hasFailoverOutsideSelectedGroup = endpoints.some((ep) => (
    !groupEndpointFilter(ep) && ep.failoverEnabled !== false
  ));
  const disableGroupFailoverBlocked = groupFailoverEndpoints.length === 0 ||
    (activeEndpointInSelectedGroup && !hasFailoverOutsideSelectedGroup);
  const disableGroupFailoverTargets = activeEndpointInSelectedGroup
    ? [
      ...groupFailoverEndpoints.filter((ep) => ep.id !== activeEndpointInChannel.id),
      activeEndpointInChannel
    ].filter((ep) => ep?.failoverEnabled !== false)
    : groupFailoverEndpoints;
  const disableGroupFailoverActionKey = `disable-group-failover-${channel.id}-${selectedCredentialGroupIDResolved || 'legacy'}`;
  const enableGroupFailoverActionKey = `enable-group-failover-${channel.id}-${selectedCredentialGroupIDResolved || 'legacy'}`;
  const isDisablingGroupFailover = actionLoading === disableGroupFailoverActionKey;
  const isEnablingGroupFailover = actionLoading === enableGroupFailoverActionKey;
  const disableGroupFailoverTip = groupFailoverEndpoints.length === 0
    ? '当前分组没有参与转移的端点'
    : activeEndpointInSelectedGroup && !hasFailoverOutsideSelectedGroup
      ? '当前路由端点在此分组，需先让其他分组至少一个端点参与转移'
      : `将当前分组 ${groupFailoverEndpoints.length} 个端点设为不参与转移`;
  const enableGroupFailoverTip = groupNonFailoverEndpoints.length === 0
    ? '当前分组端点已全部参与转移'
    : `将当前分组 ${groupNonFailoverEndpoints.length} 个端点设为参与转移`;
  const shouldEnableGroupFailover = groupNonFailoverEndpoints.length > 0;
  const groupFailoverActionTargets = shouldEnableGroupFailover ? groupNonFailoverEndpoints : disableGroupFailoverTargets;
  const groupFailoverActionKey = shouldEnableGroupFailover ? enableGroupFailoverActionKey : disableGroupFailoverActionKey;
  const groupFailoverActionLoading = shouldEnableGroupFailover ? isEnablingGroupFailover : isDisablingGroupFailover;
  const groupFailoverActionDisabled = shouldEnableGroupFailover
    ? groupNonFailoverEndpoints.length === 0
    : disableGroupFailoverBlocked;
  const groupFailoverActionTip = shouldEnableGroupFailover ? enableGroupFailoverTip : disableGroupFailoverTip;
  const shouldSkipGroupHealthCheck = groupCheckedHealthEndpoints.length > 0;
  const groupHealthActionTargets = shouldSkipGroupHealthCheck ? groupCheckedHealthEndpoints : groupSkippedHealthEndpoints;
  const groupHealthActionKey = `${shouldSkipGroupHealthCheck ? 'skip' : 'restore'}-group-health-${channel.id}-${selectedCredentialGroupIDResolved || 'legacy'}`;
  const groupHealthActionLoading = actionLoading === groupHealthActionKey;
  const groupHealthActionDisabled = groupHealthActionTargets.length === 0;
  const groupHealthActionTip = shouldSkipGroupHealthCheck
    ? `将当前分组 ${groupCheckedHealthEndpoints.length} 个端点设为跳过检测`
    : groupSkippedHealthEndpoints.length > 0
      ? `恢复当前分组 ${groupSkippedHealthEndpoints.length} 个端点的健康检测`
      : '当前分组没有端点';

  // 外部点击收起
  useEffect(() => {
    if (!isEndpointListExpanded) return undefined;
    const handlePointerDown = (event) => {
      if (channelCardRef.current && !channelCardRef.current.contains(event.target)) {
        setIsEndpointListExpanded(false);
      }
    };
    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('touchstart', handlePointerDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('touchstart', handlePointerDown);
    };
  }, [isEndpointListExpanded]);

  return (
    <div
      ref={channelCardRef}
      className={`group flex flex-col overflow-hidden rounded-xl border bg-white transition-all duration-150 ease-out shadow-[0_1px_2px_rgba(15,23,42,0.04)] hover:shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08)] ${
        channel.isActive
          ? activeCardClass
          : 'border-slate-200/70 hover:border-slate-300'
      }`}
    >
      {/* 卡片头部：渠道名 + 标签 + 操作 */}
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <h3 className="truncate text-sm font-semibold tracking-tight text-slate-900">{channel.name}</h3>
          {!channel.isActive && channelFailoverEnabled && (
            <span className="inline-flex h-[18px] min-w-[30px] flex-shrink-0 items-center justify-center rounded bg-slate-100 px-1.5 text-[10px] font-medium leading-none text-slate-500">P{channel.priority ?? 0}</span>
          )}
          {channel.isActive && (
            isActiveChannelInterrupted ? (
              <Tooltip content="当前 Codex 接管渠道的可用端点已全部异常，请检查端点或切换渠道" className="flex-shrink-0">
                <span
                  className="inline-flex items-center gap-1 rounded-full bg-rose-50 px-1.5 py-0.5 text-[10px] font-medium tracking-tight text-rose-700"
                >
                  <TriangleAlert size={10} className="text-rose-600" />
                  已中断
                </span>
              </Tooltip>
            ) : isActiveChannelSuspended ? (
              <Tooltip content={`已有 ${activeSuspendedCount} 个请求挂起，请点击下方当前端点或其他渠道重新激活`} className="flex-shrink-0">
                <span
                  className="inline-flex items-center gap-1 rounded-full bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium tracking-tight text-amber-700"
                >
                  <TriangleAlert size={10} className="text-amber-600" />
                  等待重新激活 · {activeSuspendedCount}
                </span>
              </Tooltip>
            ) : (
              <span className="inline-flex flex-shrink-0 items-center gap-1 rounded-full bg-emerald-50 px-1.5 py-0.5 text-[10px] font-medium tracking-tight text-emerald-700">
                <span className="h-1 w-1 rounded-full bg-emerald-500" />
                当前接管
              </span>
            )
          )}
          {channel.isActive && channelFailoverEnabled && (
            <span className="inline-flex h-[18px] min-w-[30px] flex-shrink-0 items-center justify-center rounded bg-slate-100 px-1.5 text-[10px] font-medium leading-none text-slate-500">P{channel.priority ?? 0}</span>
          )}
        </div>

        <div className="flex flex-shrink-0 items-center gap-0.5">
          {!channel.isActive && (
            <Tooltip content={channelActivationDisabled ? '请先恢复此渠道，使其重新参与渠道间转移' : '设为当前接管渠道'}>
              <button
                type="button"
                onClick={() => onActivateChannel?.(channel)}
                disabled={isSwitchingChannel || channelActivationDisabled}
                className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-indigo-600 transition-colors duration-150 hover:bg-indigo-50 disabled:opacity-50"
              >
                {isSwitchingChannel ? <Loader2 size={12} className="animate-spin" /> : <ArrowRightLeft size={12} />}
                设为当前
              </button>
            </Tooltip>
          )}
          {channelFailoverEnabled && (
            <Tooltip content={channel.failoverEnabled !== false ? '暂停此渠道，使其不参与渠道间转移' : '恢复此渠道，使其重新参与渠道间转移'}>
              <button type="button" onClick={() => onToggleChannelFailover?.(channel, channel.failoverEnabled === false)} disabled={isTogglingChannelFailover} className={`inline-flex size-[26px] flex-shrink-0 items-center justify-center rounded-md transition-colors disabled:cursor-wait disabled:text-indigo-400 disabled:opacity-70 ${channel.failoverEnabled !== false ? 'bg-indigo-50 text-indigo-600 hover:bg-indigo-100' : 'text-slate-400 hover:bg-slate-100 hover:text-indigo-600'}`}>
                {isTogglingChannelFailover ? <Loader2 size={14} className="animate-spin" /> : channel.failoverEnabled !== false ? <Route size={14} /> : <RouteOff size={14} />}
              </button>
            </Tooltip>
          )}
          {channel.website && (
            <Tooltip content="打开官网">
              <button
                type="button"
                onClick={() => openExternalURL(channel.website)}
                className="rounded-md p-1.5 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
              >
                <Globe size={14} />
              </button>
            </Tooltip>
          )}
          <Tooltip content="编辑渠道">
            <button
              type="button"
              onClick={() => onEditChannel?.(channel)}
              className="rounded-md p-1.5 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-indigo-600"
            >
              <Pencil size={14} />
            </button>
          </Tooltip>
          <Tooltip content="删除渠道">
            <button
              type="button"
              onClick={() => onDeleteChannel?.(channel)}
              disabled={isDeletingChannel}
              className="rounded-md p-1.5 text-slate-400 transition-colors duration-150 hover:bg-rose-50 hover:text-rose-600 disabled:opacity-50"
            >
              {isDeletingChannel ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
            </button>
          </Tooltip>
        </div>
      </div>

      {/* 元数据条：模型 · 1M · 默认凭证
          model 字段：渠道有自己的值则覆盖；否则用全局默认（来自 ~/.codex/config.toml）
          1M：所有渠道共用全局配置 */}
      <div className="flex flex-nowrap items-center gap-x-3 gap-y-1.5 overflow-hidden border-t border-slate-100 px-4 py-2 text-[11px]">
        <span className="inline-flex flex-shrink-0 items-center gap-1 text-slate-600">
          <ShieldCheck size={11} className="text-emerald-500" />
          <span className="font-medium">{channel.model || globalConfig.model || '未设置'}</span>
          <Tooltip content={channel.model ? '该渠道使用专属 model（覆盖全局）' : '该渠道使用全局默认 model'}>
            <span
              className={
                channel.model
                  ? 'rounded-full bg-indigo-50 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wider text-indigo-600'
                  : 'rounded-full bg-slate-100 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wider text-slate-400'
              }
            >
              {channel.model ? '渠道' : '全局'}
            </span>
          </Tooltip>
        </span>
        {globalConfig.enable_1m_context && (
          <>
            <span className="flex-shrink-0 text-slate-300">·</span>
            <span className="flex-shrink-0 font-medium text-sky-600">1M</span>
          </>
        )}
        <span className="flex-shrink-0 text-slate-300">·</span>
        <span className="inline-flex min-w-0 flex-shrink items-center gap-1 text-slate-500">
          <KeyRound size={11} className="flex-shrink-0 text-slate-400" />
          <span className="truncate">{defaultCredentialGroup?.name || (fallbackApiKeyMasked ? '旧渠道 Key' : '未配置分组')}</span>
          {extraCredentialGroupCount > 0 && (
            <span className="flex-shrink-0 rounded bg-slate-100 px-1.5 py-0.5 text-[9px] font-semibold text-slate-500">
              +{extraCredentialGroupCount}
            </span>
          )}
        </span>
        {channelFailoverEnabled && channel.failoverEnabled === false && (
          <>
            <span className="flex-shrink-0 text-slate-300">·</span>
            <span className="inline-flex h-[18px] flex-shrink-0 items-center rounded bg-amber-50 px-1.5 text-[10px] font-medium leading-none text-amber-700">已暂停</span>
          </>
        )}
      </div>

      {/* 凭证分组 */}
      <div className="flex h-[64px] flex-col gap-1.5 border-t border-slate-100 bg-white px-4 py-2">
        <div className="flex h-4 items-center">
          <span className="flex-shrink-0 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-400">
            Credentials · {credentialGroups.length}
          </span>
        </div>
        <div className="flex min-w-0 flex-1 items-center justify-between gap-2">
          <div className="relative min-w-0 flex-1">
            {credentialGroups.length > 1 && (
              <div className="pointer-events-none absolute inset-y-0 right-0 z-10 w-5 bg-gradient-to-l from-white to-transparent" />
            )}
            <div
              ref={credentialGroupScrollRef}
              {...credentialGroupScrollHandlers}
              className={`flex min-w-0 items-center gap-1.5 overflow-x-auto overflow-y-hidden pl-0.5 pr-4 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden ${
                isCredentialGroupDragging ? 'cursor-grabbing select-none' : 'cursor-grab'
              }`}
            >
              {credentialGroups.length === 0 ? (
                <span className="truncate text-[11px] text-slate-400">暂无分组，将回退旧渠道 Key。</span>
              ) : credentialGroups.map((group) => {
                const groupIsDefault = group.isDefault || group.is_default;
                const groupIsSelected = Number(group.id) === selectedCredentialGroupIDResolved;
                const isDefaultingGroup = actionLoading === `default-credential-group-${group.id}`;
                return (
                  <span
                    key={group.id}
                    className={`inline-flex h-6 max-w-[13rem] flex-shrink-0 items-center gap-1 rounded-md border px-1.5 text-[11px] transition-colors ${
                      groupIsSelected
                        ? 'border-indigo-200 bg-indigo-50 text-indigo-700'
                        : 'border-slate-200 bg-slate-50 text-slate-600 hover:border-slate-300'
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => setSelectedCredentialGroupID(Number(group.id) || 0)}
                      title={group.name}
                      className="inline-flex min-w-0 items-center gap-1 rounded-sm px-0.5 text-left focus:outline-none focus-visible:outline-none"
                    >
                      <KeyRound
                        size={11}
                        className={
                          groupIsSelected
                            ? 'flex-shrink-0 text-indigo-500'
                            : groupIsDefault
                              ? 'flex-shrink-0 text-emerald-500'
                              : 'flex-shrink-0 text-slate-400'
                        }
                      />
                      <span className="max-w-[8rem] truncate font-medium">{group.name}</span>
                      {groupIsDefault && <span className="flex-shrink-0 text-[10px] text-emerald-600">默认</span>}
                    </button>
                    {!groupIsDefault && (
                      <Tooltip content="设为默认分组">
                        <button
                          type="button"
                          onClick={() => onSetDefaultCredentialGroup?.(channel, group)}
                          disabled={isDefaultingGroup}
                          className="flex-shrink-0 rounded p-0.5 text-slate-400 hover:bg-white hover:text-emerald-600 focus:outline-none focus-visible:outline-none disabled:cursor-wait disabled:opacity-60"
                          aria-label="设为默认分组"
                        >
                          {isDefaultingGroup ? <Loader2 size={10} className="animate-spin" /> : <CheckCircle2 size={10} />}
                        </button>
                      </Tooltip>
                    )}
                    <button
                      type="button"
                      onClick={() => onEditCredentialGroup?.(channel, group)}
                      className="flex-shrink-0 rounded p-0.5 text-slate-400 hover:bg-white hover:text-indigo-600 focus:outline-none focus-visible:outline-none"
                    >
                      <Pencil size={10} />
                    </button>
                    {!groupIsDefault && (
                      <button
                        type="button"
                        onClick={() => onDeleteCredentialGroup?.(channel, group)}
                        className="flex-shrink-0 rounded p-0.5 text-slate-400 hover:bg-white hover:text-rose-600 focus:outline-none focus-visible:outline-none"
                      >
                        <Trash2 size={10} />
                      </button>
                    )}
                  </span>
                );
              })}
            </div>
          </div>
          <div className="h-5 w-px flex-shrink-0 bg-slate-200/80" />
          <button
            type="button"
            onClick={() => onAddCredentialGroup?.(channel)}
            className="inline-flex h-6 flex-shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus-visible:outline-none"
          >
            <Plus size={12} />
            添加分组
          </button>
        </div>
      </div>

      {/* 端点子列表 */}
      <div className="border-t border-slate-100 bg-slate-50/30">
        <div className="flex h-9 items-center justify-between px-4">
          <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-400">
            <span>Endpoints · {groupEndpoints.length}</span>
            {selectedCredentialGroup && (
              <span className="max-w-[9rem] truncate normal-case tracking-normal text-slate-500">
                {selectedCredentialGroup.name}
              </span>
            )}
            {hasMultiple && (
              <button
                type="button"
                onClick={() => setIsEndpointListExpanded((prev) => !prev)}
                className="inline-flex items-center gap-0.5 rounded-md px-1 py-0.5 text-[10px] font-medium normal-case tracking-normal text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
              >
                {isEndpointListExpanded ? '收起' : `展开 ${hiddenCount} 个`}
                <ChevronDown
                  size={11}
                  className={`transition-transform duration-200 ease-out ${
                    isEndpointListExpanded ? 'rotate-180' : ''
                  }`}
                />
              </button>
            )}
          </div>
          <div className="flex flex-shrink-0 items-center gap-1">
            <Tooltip content={groupFailoverActionTip}>
              <button
                type="button"
                onClick={() => onSetGroupEndpointFailover?.(groupFailoverActionTargets, shouldEnableGroupFailover, groupFailoverActionKey)}
                disabled={groupFailoverActionLoading || groupFailoverActionDisabled}
                className={`${ENDPOINT_STATE_BUTTON_CLASS} disabled:cursor-not-allowed ${
                  shouldEnableGroupFailover
                    ? 'text-slate-400 hover:bg-slate-100 hover:text-indigo-600'
                    : 'bg-indigo-50 text-indigo-600 hover:bg-indigo-100'
                }`}
                aria-label={shouldEnableGroupFailover ? '当前分组存在不参与转移的端点' : '当前分组端点全部参与转移'}
              >
                {groupFailoverActionLoading ? (
                  <Loader2 size={12} className="animate-spin" />
                ) : shouldEnableGroupFailover ? <RepeatOffIcon size={12} /> : <Repeat size={12} />}
              </button>
            </Tooltip>
            <Tooltip content={groupHealthActionTip}>
              <button
                type="button"
                onClick={() => onSetGroupEndpointSkipHealthCheck?.(groupHealthActionTargets, shouldSkipGroupHealthCheck, groupHealthActionKey)}
                disabled={groupHealthActionLoading || groupHealthActionDisabled}
                className={`${ENDPOINT_STATE_BUTTON_CLASS} disabled:cursor-not-allowed ${
                  shouldSkipGroupHealthCheck
                    ? 'bg-emerald-50 text-emerald-600 hover:bg-emerald-100'
                    : 'text-slate-400 hover:bg-slate-100 hover:text-emerald-600'
                }`}
                aria-label={shouldSkipGroupHealthCheck ? '当前分组存在正常检测的端点' : '当前分组端点全部跳过检测'}
              >
                {groupHealthActionLoading ? (
                  <Loader2 size={12} className="animate-spin" />
                ) : shouldSkipGroupHealthCheck ? <ShieldCheck size={12} /> : <ShieldOff size={12} />}
              </button>
            </Tooltip>
            <button
              type="button"
              onClick={() => onAddEndpoint?.(channel, selectedCredentialGroup)}
              className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
            >
              <Plus size={12} />
              添加端点
            </button>
          </div>
        </div>

        {groupEndpoints.length === 0 ? (
          <div className="border-t border-slate-100 bg-white px-4 py-6 text-center text-[11px] text-slate-400">
            当前分组暂无端点。请为该凭证分组添加至少一个 base_url。
          </div>
        ) : (
          <div className="border-t border-slate-100 bg-white">
            {displayedEndpoints.map((endpoint) => (
              <EndpointRow
                key={endpoint.id}
                endpoint={endpoint}
                credentialGroup={getCredentialGroupForEndpoint(endpoint)}
                suspendedCount={activeSuspendedCount}
                channelPaused={channelFailoverEnabled && channel.failoverEnabled === false}
                canDisableFailover={
                  !(endpoint.isActive && endpoint.failoverEnabled !== false) ||
                  endpoints.some((candidate) => candidate.id !== endpoint.id && candidate.failoverEnabled !== false)
                }
                onActivate={onActivateEndpoint}
                onToggleFailover={onToggleEndpointFailover}
                onToggleSkipHealthCheck={onToggleEndpointSkipHealthCheck}
                onEdit={onEditEndpoint}
                onOpen={(ep) => onOpenEndpoint?.(ep, channel)}
                onDelete={(ep) => onDeleteEndpoint?.(ep, channel)}
                actionLoading={actionLoading}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

// ============================================================================
// 主页面
// ============================================================================
const CodexPage = () => {
  const toast = useToast();
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState('');
  const [error, setError] = useState('');
  const [lastUpdate, setLastUpdate] = useState('');
  // Codex 挂起请求数（>0 时触发渠道卡片/端点 toggle 的 amber 警示色）
  const [suspendedCount, setSuspendedCount] = useState(0);
  const [channelFailoverEnabled, setChannelFailoverEnabled] = useState(false);
  // Codex 全局配置弹窗
  const [showGlobalConfig, setShowGlobalConfig] = useState(false);
  // Codex 全局配置（来自 ~/.codex/config.toml，所有渠道共用）
  const [globalConfig, setGlobalConfig] = useState({
    model: '',
    enable_1m_context: false,
    effective_model: '',
    effective_enable_1m_context: false,
    global_timeout_seconds: 300
  });

  // 刷新全局配置（在 page mount + 弹窗保存成功后调用）
  const loadGlobalConfig = useCallback(async () => {
    try {
      const data = await getCodexGlobalConfig();
      setGlobalConfig({
        model: data.model || '',
        enable_1m_context: Boolean(data.enable_1m_context),
        effective_model: data.effective_model || data.model || '',
        effective_enable_1m_context: Boolean(data.effective_enable_1m_context ?? data.enable_1m_context),
        global_timeout_seconds: data.global_timeout_seconds || data.globalTimeoutSeconds || 300
      });
    } catch {
      // 静默失败：全局配置加载失败仅影响展示，不影响渠道管理
    }
  }, []);

  // 渠道弹窗
  const [showChannelForm, setShowChannelForm] = useState(false);
  const [channelFormMode, setChannelFormMode] = useState('create');
  const [editingChannel, setEditingChannel] = useState(null);
  const [channelFormError, setChannelFormError] = useState('');
  const [deleteChannelTarget, setDeleteChannelTarget] = useState(null);
  const [activeChannelPauseTarget, setActiveChannelPauseTarget] = useState(null);

  // 凭证分组弹窗
  const [showCredentialGroupForm, setShowCredentialGroupForm] = useState(false);
  const [credentialGroupFormMode, setCredentialGroupFormMode] = useState('create');
  const [credentialGroupFormChannel, setCredentialGroupFormChannel] = useState(null);
  const [editingCredentialGroup, setEditingCredentialGroup] = useState(null);
  const [credentialGroupFormError, setCredentialGroupFormError] = useState('');
  const [deleteCredentialGroupTarget, setDeleteCredentialGroupTarget] = useState(null);

  // 端点弹窗（创建 + 编辑共用 EndpointFormModal）
  const [showEndpointForm, setShowEndpointForm] = useState(false);
  const [endpointFormMode, setEndpointFormMode] = useState('create'); // 'create' | 'edit'
  const [endpointFormChannel, setEndpointFormChannel] = useState(null);
  const [endpointFormCredentialGroup, setEndpointFormCredentialGroup] = useState(null);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointFormError, setEndpointFormError] = useState('');
  const [deleteEndpointTarget, setDeleteEndpointTarget] = useState(null);
  const [endpointDetailTarget, setEndpointDetailTarget] = useState(null);

  const activeChannel = channels.find((channel) => channel.isActive) || null;
  const selectedEndpointDetail = endpointDetailTarget ? (() => {
    const latestChannel = channels.find((channel) => channel.id === endpointDetailTarget.channelOwner?.id) ||
      endpointDetailTarget.channelOwner;
    const latestEndpoint = latestChannel?.endpoints?.find((endpoint) => endpoint.id === endpointDetailTarget.endpoint?.id) ||
      endpointDetailTarget.endpoint;
    return { channelOwner: latestChannel, endpoint: latestEndpoint };
  })() : null;
  const allEndpoints = channels.flatMap((channel) =>
    (channel.endpoints || []).map((endpoint) => ({ channel, endpoint }))
  );
  const endpointInScope = ({ channel, endpoint }) => {
    if (endpoint.failoverEnabled === false) return false;
    return channelFailoverEnabled ? channel.failoverEnabled !== false : channel.isActive;
  };
  const scopedEndpoints = allEndpoints.filter(endpointInScope);
  const endpointHealthState = ({ endpoint }) => endpoint.healthState || endpoint.health_state || 'unchecked';
  const skippedEndpointCount = scopedEndpoints.filter((item) => endpointHealthState(item) === 'skipped').length;
  const healthyEndpointCount = scopedEndpoints.filter((item) => {
    const state = endpointHealthState(item);
    return state === 'healthy' || state === 'skipped';
  }).length;
  const unhealthyEndpointCount = scopedEndpoints.filter((item) => endpointHealthState(item) === 'unhealthy').length;
  const uncheckedEndpointCount = allEndpoints.filter((item) => {
    if (!endpointInScope(item)) return true;
    const state = endpointHealthState(item);
    return state === 'unchecked' || state === 'inactive' || state === 'excluded';
  }).length;
  const participatingChannelCount = channels.filter((channel) => channel.failoverEnabled !== false).length;
  const activeChannelEndpoints = activeChannel?.endpoints || [];
  const activeChannelCredentialGroupCount = activeChannel?.credentialGroups?.length || 0;
  const failoverOverviewCards = [
    {
      title: '参与端点',
      value: String(scopedEndpoints.length),
      subText: `端点总数 ${allEndpoints.length} 个`,
      icon: Server,
      statusColor: 'bg-indigo-50 text-indigo-600'
    },
    {
      title: '健康端点',
      value: String(healthyEndpointCount),
      subText: '当前通过健康检测',
      icon: CheckCircle2,
      statusColor: 'bg-emerald-50 text-emerald-600'
    },
    {
      title: '跳过检测',
      value: String(skippedEndpointCount),
      subText: '已标记跳过健康检查',
      icon: ShieldOff,
      statusColor: 'bg-violet-50 text-violet-600'
    },
    {
      title: '异常端点',
      value: String(unhealthyEndpointCount),
      subText: '当前判定异常',
      icon: XCircle,
      statusColor: 'bg-rose-50 text-rose-600'
    },
    {
      title: '未参与端点',
      value: String(uncheckedEndpointCount),
      subText: '未纳入当前检测范围',
      icon: ShieldOff,
      statusColor: 'bg-slate-100 text-slate-500'
    },
    {
      title: '参与渠道',
      value: String(participatingChannelCount),
      subText: `总渠道 ${channels.length} 个`,
      icon: ArrowRightLeft,
      statusColor: 'bg-slate-100 text-slate-700'
    }
  ];
  const activeChannelOverviewCards = [
    {
      title: '凭证分组',
      value: String(activeChannelCredentialGroupCount),
      subText: activeChannel ? `当前渠道 ${activeChannel.name}` : '暂无活跃渠道',
      icon: KeyRound,
      statusColor: 'bg-sky-50 text-sky-600'
    },
    {
      title: '参与端点',
      value: String(scopedEndpoints.length),
      subText: '当前活跃渠道参与路由',
      icon: Server,
      statusColor: 'bg-indigo-50 text-indigo-600'
    },
    {
      title: '健康端点',
      value: String(healthyEndpointCount),
      subText: '当前活跃渠道通过检测',
      icon: CheckCircle2,
      statusColor: 'bg-emerald-50 text-emerald-600'
    },
    {
      title: '跳过检测',
      value: String(skippedEndpointCount),
      subText: '当前活跃渠道跳过检测',
      icon: ShieldOff,
      statusColor: 'bg-violet-50 text-violet-600'
    },
    {
      title: '异常端点',
      value: String(unhealthyEndpointCount),
      subText: '当前活跃渠道判定异常',
      icon: XCircle,
      statusColor: 'bg-rose-50 text-rose-600'
    },
    {
      title: '总端点',
      value: String(activeChannelEndpoints.length),
      subText: '当前活跃渠道全部端点',
      icon: Database,
      statusColor: 'bg-slate-100 text-slate-700'
    }
  ];
  const overviewCards = channelFailoverEnabled ? failoverOverviewCards : activeChannelOverviewCards;

  const fetchData = useCallback(async ({ silent = false } = {}) => {
    if (!silent) setLoading(true);
    setError('');
    try {
      const [list, failoverSettings] = await Promise.all([
        getCodexChannels(),
        getSettingsByScope('codex', 'failover').catch(() => [])
      ]);
      setChannels(list);
      setChannelFailoverEnabled((failoverSettings || []).find((setting) => setting.key === 'enabled')?.value === 'true');
      setLastUpdate(new Date().toLocaleTimeString());
    } catch (err) {
      setError(getErrorMessage(err, '加载 Codex 渠道失败'));
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
    // 初始拉取一次挂起数
    getCodexSuspendedCount().then(setSuspendedCount).catch(() => {});
    // 初始拉取一次全局配置
    loadGlobalConfig();

    const unsubscribe = subscribeToEvent('codex-provider:update', () => {
      fetchData({ silent: true });
    });
    // 订阅挂起数变化事件（后端 SuspensionManager 在挂起开始/结束时推送）
    const unsubscribeSuspension = subscribeToEvent('codex-provider:suspension', (data) => {
      const count = Number(data?.suspended_count) || 0;
      setSuspendedCount(count);
    });
    const refreshTimer = window.setInterval(() => {
      fetchData({ silent: true });
    }, 30000);
    return () => {
      window.clearInterval(refreshTimer);
      if (typeof unsubscribe === 'function') unsubscribe();
      if (typeof unsubscribeSuspension === 'function') unsubscribeSuspension();
    };
  }, [fetchData, loadGlobalConfig]);

  // ===== 渠道动作 =====
  const handleActivateChannel = async (channel) => {
    if (channel.isActive) return;
    if (channelFailoverEnabled && channel.failoverEnabled === false) {
      toast.error('请先恢复此渠道，使其重新参与渠道间转移', '无法设为当前');
      return;
    }
    setActionLoading(`activate-channel-${channel.id}`);
    try {
      await activateCodexChannel(channel.id);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getErrorMessage(err, '切换渠道失败'), '切换失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleToggleChannelFailover = async (channel, enabled) => {
    if (channel.isActive && !enabled) {
      setActionLoading(`prepare-channel-pause-${channel.id}`);
      try {
        const nextChannel = await getNextAvailableCodexChannel(channel.id);
        setActiveChannelPauseTarget({ channel, nextChannel });
      } catch (err) {
        toast.error(getFailoverToggleErrorMessage(err, '没有其他可用于接管的 Codex 渠道'), '无法暂停');
      } finally {
        setActionLoading('');
      }
      return;
    }
    setActionLoading(`toggle-channel-failover-${channel.id}`);
    try {
      await setCodexChannelFailoverEnabled(channel.id, enabled);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err, '更新渠道故障转移状态失败'), '操作失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleConfirmActiveChannelPause = async () => {
    const channel = activeChannelPauseTarget?.channel;
    if (!channel) return;
    setActionLoading(`toggle-channel-failover-${channel.id}`);
    try {
      await setCodexChannelFailoverEnabled(channel.id, false);
      setActiveChannelPauseTarget(null);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err, '暂停当前接管渠道失败'), '操作失败');
    } finally {
      setActionLoading('');
    }
  };

  const openCreateChannelForm = () => {
    setChannelFormMode('create');
    setEditingChannel(null);
    setChannelFormError('');
    setShowChannelForm(true);
  };

  const openEditChannelForm = (channel) => {
    setChannelFormMode('edit');
    setEditingChannel(channel);
    setChannelFormError('');
    setShowChannelForm(true);
  };

  const closeChannelForm = () => {
    setShowChannelForm(false);
    setEditingChannel(null);
    setChannelFormError('');
  };

  const handleSaveChannel = async (payload) => {
    setActionLoading(channelFormMode === 'edit' ? 'update-channel' : 'create-channel');
    setChannelFormError('');
    try {
      const { credentialGroup, ...channelPayload } = payload;
      if (channelFormMode === 'edit' && editingChannel) {
        await updateCodexChannelByID(editingChannel.id, channelPayload);
        if (credentialGroup?.id) {
          await updateCodexCredentialGroupByID(credentialGroup.id, credentialGroup);
        } else if (credentialGroup) {
          await createCodexCredentialGroup({
            ...credentialGroup,
            channelId: editingChannel.id
          });
        }
      } else {
        await createCodexChannel(channelPayload);
        if (credentialGroup) {
          const latestChannels = await getCodexChannels();
          const createdChannel = latestChannels.find((channel) => channel.name === channelPayload.name);
          const defaultGroup = getDefaultCredentialGroup(createdChannel);
          if (defaultGroup?.id) {
            await updateCodexCredentialGroupByID(defaultGroup.id, credentialGroup);
          } else if (createdChannel?.id) {
            await createCodexCredentialGroup({
              ...credentialGroup,
              channelId: createdChannel.id
            });
          }
        }
      }
      await fetchData({ silent: true });
      closeChannelForm();
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err, '保存渠道失败'), '保存失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleDeleteChannel = async () => {
    if (!deleteChannelTarget) return;
    setActionLoading(`delete-channel-${deleteChannelTarget.id}`);
    try {
      await deleteCodexChannelByID(deleteChannelTarget.id);
      await fetchData({ silent: true });
      setDeleteChannelTarget(null);
    } catch (err) {
      setError(getErrorMessage(err, '删除渠道失败'));
    } finally {
      setActionLoading('');
    }
  };

  // ===== 凭证分组动作 =====
  const openCreateCredentialGroupForm = (channel) => {
    setCredentialGroupFormMode('create');
    setCredentialGroupFormChannel(channel);
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  };

  const openEditCredentialGroupForm = (channel, group) => {
    setCredentialGroupFormMode('edit');
    setCredentialGroupFormChannel(channel);
    setEditingCredentialGroup(group);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  };

  const closeCredentialGroupForm = () => {
    setShowCredentialGroupForm(false);
    setCredentialGroupFormChannel(null);
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
  };

  const handleSaveCredentialGroup = async (payload) => {
    if (!credentialGroupFormChannel) return;
    const { copyEndpoints = [], ...groupPayload } = payload || {};
    delete groupPayload.copyEndpointsFromGroupId;
    const loadingKey = credentialGroupFormMode === 'edit'
      ? `save-credential-group-${editingCredentialGroup?.id}`
      : 'create-credential-group';
    setActionLoading(loadingKey);
    setCredentialGroupFormError('');
    try {
      if (credentialGroupFormMode === 'edit' && editingCredentialGroup) {
        await updateCodexCredentialGroupByID(editingCredentialGroup.id, groupPayload);
      } else {
        const createdGroup = await createCodexCredentialGroup({
          ...groupPayload,
          channelId: credentialGroupFormChannel.id
        });
        const targetGroupID = Number(createdGroup?.id || 0);
        if (targetGroupID > 0) {
          for (const endpoint of copyEndpoints) {
            await createCodexEndpoint(buildCodexEndpointCopyPayload(
              endpoint,
              credentialGroupFormChannel.id,
              targetGroupID
            ));
          }
        }
      }
      await fetchData({ silent: true });
      closeCredentialGroupForm();
    } catch (err) {
      setCredentialGroupFormError(getErrorMessage(err, '保存凭证分组失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleDeleteCredentialGroup = async () => {
    const target = deleteCredentialGroupTarget;
    if (!target?.group) return;
    setActionLoading(`delete-credential-group-${target.group.id}`);
    try {
      await deleteCodexCredentialGroupByID(target.group.id);
      await fetchData({ silent: true });
      setDeleteCredentialGroupTarget(null);
    } catch (err) {
      setError(getErrorMessage(err, '删除凭证分组失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleSetDefaultCredentialGroup = async (_channel, group) => {
    if (!group?.id) return;
    setActionLoading(`default-credential-group-${group.id}`);
    try {
      await setDefaultCodexCredentialGroup(group.id);
      await fetchData({ silent: true });
    } catch (err) {
      setError(getErrorMessage(err, '设置默认分组失败'));
    } finally {
      setActionLoading('');
    }
  };

  // ===== 端点动作 =====
  const openCreateEndpointForm = (channel, credentialGroup = null) => {
    setEndpointFormMode('create');
    setEndpointFormChannel(channel);
    setEndpointFormCredentialGroup(credentialGroup || getDefaultCredentialGroup(channel));
    setEditingEndpoint(null);
    setEndpointFormError('');
    setShowEndpointForm(true);
  };

  const openEditEndpointForm = (endpoint) => {
    const channel = channels.find((c) => c.id === endpoint.channelId) || null;
    const endpointGroupID = endpoint.credentialGroupId || endpoint.credential_group_id || 0;
    const endpointGroup = (channel?.credentialGroups || []).find((group) => Number(group.id) === Number(endpointGroupID)) ||
      getDefaultCredentialGroup(channel);
    setEndpointFormMode('edit');
    setEndpointFormChannel(channel);
    setEndpointFormCredentialGroup(endpointGroup);
    setEditingEndpoint(endpoint);
    setEndpointFormError('');
    setShowEndpointForm(true);
  };

  const closeEndpointForm = () => {
    setShowEndpointForm(false);
    setEndpointFormChannel(null);
    setEndpointFormCredentialGroup(null);
    setEditingEndpoint(null);
    setEndpointFormError('');
  };

  const handleSaveEndpoint = async ({ name, baseURL, credentialGroupId, failoverEnabled, skipHealthCheck, priority, timeoutSeconds }) => {
    if (endpointFormMode === 'edit') {
      if (!editingEndpoint) return;
      setActionLoading(`save-endpoint-${editingEndpoint.id}`);
      setEndpointFormError('');
      try {
        await updateCodexEndpointByID(editingEndpoint.id, {
          name,
          baseURL,
          credentialGroupId,
          failoverEnabled,
          skipHealthCheck,
          priority,
          timeoutSeconds
        });
        await fetchData({ silent: true });
        closeEndpointForm();
      } catch (err) {
        setEndpointFormError(getErrorMessage(err, '更新端点失败'));
      } finally {
        setActionLoading('');
      }
      return;
    }

    if (!endpointFormChannel) return;
    setActionLoading('create-endpoint');
    setEndpointFormError('');
    try {
      await createCodexEndpoint({
        channelId: endpointFormChannel.id,
        credentialGroupId,
        name,
        baseURL,
        failoverEnabled,
        skipHealthCheck,
        priority,
        timeoutSeconds
      });
      await fetchData({ silent: true });
      closeEndpointForm();
    } catch (err) {
      setEndpointFormError(getErrorMessage(err, '保存端点失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleActivateEndpoint = async (endpoint) => {
    if (endpoint.isActive) return;
    setActionLoading(`activate-endpoint-${endpoint.id}`);
    try {
      await activateCodexEndpoint(endpoint.id);
      await fetchData({ silent: true });
    } catch (err) {
      setError(getErrorMessage(err, '激活端点失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleToggleEndpointFailover = async (endpoint, enabled) => {
    try {
      await updateCodexEndpointByID(endpoint.id, {
        name: endpoint.name,
        baseURL: endpoint.baseURL || endpoint.base_url,
        credentialGroupId: endpoint.credentialGroupId || endpoint.credential_group_id || 0,
        failoverEnabled: enabled,
        priority: endpoint.priority,
        timeoutSeconds: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0
      });
      await fetchData({ silent: true });
    } catch (err) {
      setError(getFailoverToggleErrorMessage(err));
    }
  };

  const handleSetGroupEndpointFailover = async (targetEndpoints = [], enabled, loadingKey = 'set-group-failover') => {
    const targets = targetEndpoints.filter((endpoint) => endpoint?.id);
    if (targets.length === 0) return;
    setError('');
    setActionLoading(loadingKey);
    try {
      for (const endpoint of targets) {
        await updateCodexEndpointByID(endpoint.id, {
          name: endpoint.name,
          baseURL: endpoint.baseURL || endpoint.base_url,
          credentialGroupId: endpoint.credentialGroupId || endpoint.credential_group_id || 0,
          failoverEnabled: enabled,
          priority: endpoint.priority,
          timeoutSeconds: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0
        });
      }
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err, '批量操作失败'), '批量操作失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleSetGroupEndpointSkipHealthCheck = async (targetEndpoints = [], skipHealthCheck, loadingKey = 'set-group-health') => {
    const targets = targetEndpoints.filter((endpoint) => endpoint?.id);
    if (targets.length === 0) return;
    setActionLoading(loadingKey);
    try {
      for (const endpoint of targets) {
        await updateCodexEndpointByID(endpoint.id, {
          name: endpoint.name,
          baseURL: endpoint.baseURL || endpoint.base_url,
          credentialGroupId: endpoint.credentialGroupId || endpoint.credential_group_id || 0,
          skipHealthCheck,
          priority: endpoint.priority,
          timeoutSeconds: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0
        });
      }
      await fetchData({ silent: true });
    } catch (err) {
      setError(getErrorMessage(err, '批量切换跳过检测失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleToggleEndpointSkipHealthCheck = async (endpoint, skipHealthCheck) => {
    setActionLoading(`skip-health-endpoint-${endpoint.id}`);
    try {
      await updateCodexEndpointByID(endpoint.id, {
        name: endpoint.name,
        baseURL: endpoint.baseURL || endpoint.base_url,
        credentialGroupId: endpoint.credentialGroupId || endpoint.credential_group_id || 0,
        skipHealthCheck,
        priority: endpoint.priority,
        timeoutSeconds: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0
      });
      await fetchData({ silent: true });
    } catch (err) {
      setError(getErrorMessage(err, '切换跳过检测失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleDeleteEndpoint = async () => {
    if (!deleteEndpointTarget) return;
    const epId = deleteEndpointTarget.endpoint.id;
    setActionLoading(`delete-endpoint-${epId}`);
    try {
      await deleteCodexEndpointByID(epId);
      await fetchData({ silent: true });
      setDeleteEndpointTarget(null);
    } catch (err) {
      setError(getErrorMessage(err, '删除端点失败'));
    } finally {
      setActionLoading('');
    }
  };

  const credentialGroupEndpointCopyOptions =
    credentialGroupFormMode === 'create' && credentialGroupFormChannel
      ? buildCodexEndpointCopyOptions(credentialGroupFormChannel)
      : [];

  return (
    <div className="animate-fade-in">
      {/* 页面标题 */}
      <div className="flex justify-between items-end mb-8">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-slate-900 rounded-lg text-white shadow-lg">
            <CodexLogo className="w-6 h-6" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-900">Codex 管理</h1>
            <p className="text-slate-500 text-sm mt-1">
              管理 Codex 渠道、端点与模型映射；请求通过主代理端口按当前接管渠道转发
              {lastUpdate && (
                <span className="ml-2 text-slate-400">· 更新于 {lastUpdate}</span>
              )}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="sm"
            icon={RefreshCw}
            onClick={() => fetchData()}
            loading={loading}
          >
            刷新
          </Button>
          <Button
            variant="ghost"
            size="sm"
            icon={Settings}
            onClick={() => setShowGlobalConfig(true)}
          >
            Codex 配置
          </Button>
          <Button icon={Plus} onClick={openCreateChannelForm}>
            添加渠道
          </Button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-6">
        {overviewCards.map((card) => (
          <KPICard
            key={card.title}
            title={card.title}
            value={card.value}
            subText={card.subText}
            icon={card.icon}
            statusColor={card.statusColor}
            layout="side"
            size="lg"
          />
        ))}
      </div>

      {error && (
        <div className="mb-4">
          <ErrorMessage message={error} onClose={() => setError('')} />
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center py-16">
          <LoadingSpinner />
        </div>
      ) : channels.length === 0 ? (
        <EmptyState
          icon={Database}
          title="暂无 Codex 渠道"
          description="请先创建一个渠道并为其添加至少一个端点（base_url），随后即可在管理界面中切换激活。"
          action={
            <Button variant="primary" icon={Plus} onClick={openCreateChannelForm}>
              添加渠道
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2 xl:grid-cols-3">
          {channels.map((channel) => (
            <ChannelCard
              key={channel.id}
              channel={channel}
              actionLoading={actionLoading}
              suspendedCount={suspendedCount}
              globalConfig={globalConfig}
              channelFailoverEnabled={channelFailoverEnabled}
              onActivateChannel={handleActivateChannel}
              onToggleChannelFailover={handleToggleChannelFailover}
              onEditChannel={openEditChannelForm}
              onDeleteChannel={(c) => setDeleteChannelTarget(c)}
              onAddCredentialGroup={openCreateCredentialGroupForm}
              onEditCredentialGroup={openEditCredentialGroupForm}
              onSetDefaultCredentialGroup={handleSetDefaultCredentialGroup}
              onDeleteCredentialGroup={(channelOwner, group) =>
                setDeleteCredentialGroupTarget({ channel: channelOwner, group })
              }
              onAddEndpoint={openCreateEndpointForm}
              onActivateEndpoint={handleActivateEndpoint}
              onToggleEndpointFailover={handleToggleEndpointFailover}
              onSetGroupEndpointFailover={handleSetGroupEndpointFailover}
              onSetGroupEndpointSkipHealthCheck={handleSetGroupEndpointSkipHealthCheck}
              onToggleEndpointSkipHealthCheck={handleToggleEndpointSkipHealthCheck}
              onEditEndpoint={openEditEndpointForm}
              onOpenEndpoint={(endpoint, channelOwner) =>
                setEndpointDetailTarget({ endpoint, channelOwner })
              }
              onDeleteEndpoint={(endpoint, channelOwner) =>
                setDeleteEndpointTarget({ endpoint, channelOwner })
              }
            />
          ))}
        </div>
      )}

      {showChannelForm && (
        <ChannelFormModal
          mode={channelFormMode}
          channel={editingChannel}
          channelFailoverEnabled={channelFailoverEnabled}
          loading={actionLoading === 'create-channel' || actionLoading === 'update-channel'}
          serverError={channelFormError}
          onCancel={closeChannelForm}
          onSave={handleSaveChannel}
        />
      )}

      {showCredentialGroupForm && (
        <CredentialGroupFormModal
          mode={credentialGroupFormMode}
          channel={credentialGroupFormChannel}
          group={editingCredentialGroup}
          loading={
            credentialGroupFormMode === 'edit'
              ? actionLoading === `save-credential-group-${editingCredentialGroup?.id}`
              : actionLoading === 'create-credential-group'
          }
          serverError={credentialGroupFormError}
          endpointCopyOptions={credentialGroupEndpointCopyOptions}
          onCancel={closeCredentialGroupForm}
          onSave={handleSaveCredentialGroup}
        />
      )}

      {showEndpointForm && (
        <EndpointFormModal
          mode={endpointFormMode}
          channel={endpointFormChannel}
          endpoint={editingEndpoint}
          credentialGroup={endpointFormCredentialGroup}
          loading={
            endpointFormMode === 'edit'
              ? actionLoading === `save-endpoint-${editingEndpoint?.id}`
              : actionLoading === 'create-endpoint'
          }
          serverError={endpointFormError}
          onCancel={closeEndpointForm}
          onSave={handleSaveEndpoint}
        />
      )}

      {selectedEndpointDetail?.endpoint && (
        <CodexEndpointDetailModal
          endpoint={selectedEndpointDetail.endpoint}
          channel={selectedEndpointDetail.channelOwner}
          globalConfig={globalConfig}
          onClose={() => setEndpointDetailTarget(null)}
          onEdit={(endpoint) => {
            setEndpointDetailTarget(null);
            openEditEndpointForm(endpoint);
          }}
        />
      )}

      {deleteChannelTarget && (
        <DeleteConfirmDialog
          title={`删除渠道「${deleteChannelTarget.name}」`}
          description="该操作会级联删除其下所有端点，且无法撤销。"
          hint={
            deleteChannelTarget.isActive
              ? '该渠道是当前接管渠道，删除后系统会尝试激活下一个渠道并同步 config.toml。'
              : '删除后端点也会一并消失。若需保留端点请先迁移到其他渠道。'
          }
          loading={actionLoading === `delete-channel-${deleteChannelTarget.id}`}
          onCancel={() => setDeleteChannelTarget(null)}
          onConfirm={handleDeleteChannel}
        />
      )}

      {activeChannelPauseTarget && (
        <ActiveChannelPauseDialog
          channelName={activeChannelPauseTarget.channel?.name}
          nextChannelName={activeChannelPauseTarget.nextChannel?.name}
          loading={actionLoading === `toggle-channel-failover-${activeChannelPauseTarget.channel?.id}`}
          onCancel={() => setActiveChannelPauseTarget(null)}
          onConfirm={handleConfirmActiveChannelPause}
        />
      )}

      {deleteCredentialGroupTarget && (
        <DeleteConfirmDialog
          title="删除凭证分组"
          description={`即将从渠道「${deleteCredentialGroupTarget.channel?.name}」中删除凭证分组「${deleteCredentialGroupTarget.group?.name}」。`}
          hint="默认分组或仍被端点引用的分组不能删除；如需删除，请先切换端点使用其他分组。"
          loading={actionLoading === `delete-credential-group-${deleteCredentialGroupTarget.group.id}`}
          onCancel={() => setDeleteCredentialGroupTarget(null)}
          onConfirm={handleDeleteCredentialGroup}
        />
      )}

      {deleteEndpointTarget && (
        <DeleteConfirmDialog
          title="删除端点"
          description={`即将从渠道「${deleteEndpointTarget.channelOwner?.name}」中删除端点「${deleteEndpointTarget.endpoint?.name || deleteEndpointTarget.endpoint?.baseURL || '未命名端点'}」。`}
          hint={
            deleteEndpointTarget.endpoint.isActive
              ? '这是该渠道的当前活跃端点；删除后系统会尝试切换到同渠道下一个可用端点。'
              : '该端点未激活，删除后只会从该渠道的反代候选中移除。'
          }
          loading={actionLoading === `delete-endpoint-${deleteEndpointTarget.endpoint.id}`}
          onCancel={() => setDeleteEndpointTarget(null)}
          onConfirm={handleDeleteEndpoint}
        />
      )}

      {showGlobalConfig && (
        <CodexGlobalConfigModal
          onClose={() => setShowGlobalConfig(false)}
          onSaved={loadGlobalConfig}
        />
      )}
    </div>
  );
};

export default CodexPage;
