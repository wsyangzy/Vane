// ============================================
// Channels 页面 - 渠道管理（渠道内端点故障转移）
// 2025-11-28 (Updated 2025-12-06 for SQLite Storage)
// ============================================

import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import {
  RefreshCw,
  Plus,
  Pencil,
  Trash2,
  Database,
  AlertTriangle,
  Server,
  ArrowRight,
  ArrowRightLeft,
  Calculator,
  Repeat,
  ShieldCheck,
  ShieldOff,
  Slash,
  CheckCircle2,
  XCircle,
  Clock,
  ChevronDown,
  Pause,
  Route,
  RouteOff,
  Globe,
  Settings,
  KeyRound,
  Eye,
  EyeOff,
  Waves
} from 'lucide-react';
import {
  Button,
  KPICard,
  LoadingSpinner,
  ErrorMessage,
  SegmentedTabs,
  Tooltip
} from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import useEndpointsData from '@hooks/useEndpointsData.js';
import {
  EndpointForm,
  DeleteConfirmDialog
} from './components';
import ModelTag from '@pages/requests/components/ModelTag.jsx';
import { ClaudeIcon } from '@pages/requests/components/ModelTag.jsx';
import ModelMultiplierEditor from '@pages/management/shared/ModelMultiplierEditor.jsx';
import CredentialGroupDetailModal from '@pages/management/shared/CredentialGroupDetailModal.jsx';
import CredentialGroupStrip from '@pages/management/shared/CredentialGroupStrip.jsx';
import { buildCredentialGroupStats } from '@pages/management/shared/credentialGroupStats.js';
import EndpointAvailabilityBar from '@pages/management/shared/EndpointAvailabilityBar.jsx';
import {
  EndpointHealthBadge as HealthBadge,
  EndpointLatencyBadge as LatencyBadge,
  EndpointRequestFailureBadge as RequestFailedBadge
} from '@pages/management/shared/endpointBadges.jsx';
import {
  DetailChip,
  DetailMetricCard,
  DetailPill,
  DetailTimeoutPill,
  DetailTimestampFooter,
  ModelMappingPair
} from '@pages/management/shared/detailParts.jsx';
import EndpointCopyNameEditorStep, { EndpointCopySourcePicker } from '@pages/management/shared/EndpointCopyNameEditorStep.jsx';
import {
  formatLastCheck,
  formatDetailMultiplier,
  getDetailTimestamp,
  getErrorMessage,
  getFailoverToggleErrorMessage,
  isDefaultDetailMultiplier
} from '@pages/management/shared/formatters.js';
import { buildInitialModelMappingRows, buildModelMappingPayload } from '@pages/management/shared/modelMapping.js';
import { buildInitialModelMultiplierRows, MULTIPLIER_EXPRESSION_HELP, parseModelMultiplierRows } from '@pages/management/shared/modelMultiplierRows.js';
import { selectPersistedEndpoints } from '@pages/management/shared/endpointActions.js';
import MultiplierSettingsModal from '@pages/management/shared/MultiplierSettingsModal.jsx';
import { getCredentialGroupCostMultiplier, getCredentialGroupForEndpoint } from './utils/credentialGroups.js';
import { buildClaudeEndpointCopyPayload } from './utils/endpointPayload.js';
import {
  getEndpointStorageStatus,
  getEndpointRecords,
  createEndpointRecord,
  updateEndpointRecordByID,
  deleteEndpointRecordByID,
  activateEndpointRecordByID,
  setEndpointFailoverEnabledByID,
  setEndpointSkipHealthCheckByID, // 跳过健康检测
  getChannels,
  createChannel,
  updateChannel,
  deleteChannel,
  getGroupsRaw,
  activateGroup,
  pauseGroup,
  resumeGroup,
  getNextAvailableChannel,
  getConfig,
  getSettingsByCategory,
  isInterChannelFailoverEnabled,
  isWailsEnvironment,
  openExternalURL,
  subscribeToEvent,
  getSuspendedRequestsCount,
  getSuspendedEndpointCounts,
  getManualRetryRequestsCount,
  getManualRetryChannelCounts,
  createClaudeCredentialGroup,
  updateClaudeCredentialGroupByID,
  deleteClaudeCredentialGroupByID,
  setDefaultClaudeCredentialGroup
} from '@utils/wailsApi.js';
import { formatMultiplierInputValue, parseMultiplierExpression } from '@utils/multiplier.js';

const RepeatOffIcon = ({ size = 12 }) => (
  <span className="relative inline-flex flex-shrink-0" style={{ width: size, height: size }}>
    <Repeat size={size} />
    <Slash size={size} strokeWidth={2.5} className="absolute inset-0" />
  </span>
);

const ENDPOINT_STATE_BUTTON_CLASS = 'inline-flex size-5 min-h-5 min-w-5 max-h-5 max-w-5 flex-shrink-0 appearance-none items-center justify-center rounded-md border-0 p-0 leading-none transition-colors duration-150';

const CHANNEL_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'mapping', label: '模型映射' }
];

const CREDENTIAL_GROUP_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
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

const hasClaudeCredential = (group) => [
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
  channel: group?.channel || channelName || '',
  name: group?.name || '',
  token: group?.token || '',
  apiKey: group?.apiKey || group?.api_key || '',
  isDefault: Boolean(group?.isDefault ?? group?.is_default ?? false),
  dailyCostLimitUSD: String(group?.dailyCostLimitUSD ?? group?.daily_cost_limit_usd ?? 0),
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

const DeleteChannelConfirmDialog = ({ channelName, endpointCount = 0, onConfirm, onCancel, loading }) => {
  const confirmDisabled = loading;

  return renderModalPortal(
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-3 bg-rose-100 rounded-full">
            <AlertTriangle className="text-rose-600" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认删除渠道</h3>
            <p className="text-sm text-slate-500">此操作不可撤销</p>
          </div>
        </div>

        <p className="text-slate-700 mb-4">
          确定要删除渠道 <span className="font-semibold">“{channelName}”</span> 吗？
        </p>

        {endpointCount > 0 && (
          <div className="p-3 bg-amber-50 border border-amber-200 rounded-lg text-amber-800 text-sm mb-4">
            <div className="font-medium">该渠道下仍有 {endpointCount} 个端点</div>
            <div className="text-xs text-amber-700 mt-2">确认删除将同时删除该渠道下的所有端点并删除该渠道。</div>
          </div>
        )}

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            variant="danger"
            icon={Trash2}
            onClick={() => onConfirm?.()}
            loading={loading}
            disabled={confirmDisabled}
          >
            确认删除
          </Button>
        </div>
      </div>
    </div>
  );
};

const DeleteCredentialGroupConfirmDialog = ({ channelName, group, onConfirm, onCancel, loading }) => {
  const endpointCount = Number(group?.endpointCount ?? group?.endpoint_count ?? 0);

  return renderModalPortal(
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-3 bg-rose-100 rounded-full">
            <AlertTriangle className="text-rose-600" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认删除凭证分组</h3>
            <p className="text-sm text-slate-500">此操作不可撤销</p>
          </div>
        </div>

        <p className="text-slate-700 mb-4">
          确定要删除渠道 <span className="font-semibold">“{channelName}”</span> 下的凭证分组{' '}
          <span className="font-semibold">“{group?.name || '未命名分组'}”</span> 吗？
        </p>

        <div className="p-3 bg-amber-50 border border-amber-200 rounded-lg text-amber-800 text-sm mb-4">
          <div className="font-medium">
            {endpointCount > 0 ? `该分组仍被 ${endpointCount} 个端点引用` : '仅未被端点引用的非默认分组可以删除'}
          </div>
          <div className="text-xs text-amber-700 mt-2">
            默认分组或仍被端点引用的分组不能删除；如需删除，请先切换端点使用其他分组。
          </div>
        </div>

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            variant="danger"
            icon={Trash2}
            onClick={() => onConfirm?.()}
            loading={loading}
            disabled={loading}
          >
            确认删除
          </Button>
        </div>
      </div>
    </div>
  );
};

const PauseActiveChannelConfirmDialog = ({ channelName, onConfirm, onCancel, loading }) => {
  const confirmDisabled = loading;

  return renderModalPortal(
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-3 bg-amber-100 rounded-full">
            <AlertTriangle className="text-amber-700" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认暂停活跃渠道</h3>
            <p className="text-sm text-slate-500">暂停后将按路由策略切换渠道</p>
          </div>
        </div>

        <p className="text-slate-700 mb-4">
          确定要暂停当前活跃渠道 <span className="font-semibold">“{channelName}”</span> 吗？
          暂停后将自动切换到可用渠道。
        </p>

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            variant="danger"
            icon={Pause}
            onClick={() => onConfirm?.()}
            loading={loading}
            disabled={confirmDisabled}
          >
            确认暂停
          </Button>
        </div>
      </div>
    </div>
  );
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

// ============================================
// 端点精简行（对齐 Codex 管理页的端点列表密度）
// ============================================

const EndpointMiniCard = ({
  endpoint,
  credentialGroup,
  isActiveChannel,
  isSqliteMode,
  isCurrentEndpoint = false,
  healthInScope = true,
  channelFailoverEnabled = true,
  onOpen,
  onActivateEndpoint,
  onToggleFailover,
  onToggleSkipHealthCheck, // 跳过健康检测回调
  onEdit,
  onDelete,
  canDisableFailover = true
}) => {
  if (!endpoint) return null;

  const inScope = healthInScope && endpoint._health_scope !== false;
  const channelPaused = endpoint._channel_paused === true;
  const showPaused = channelFailoverEnabled && channelPaused;
  const responseTime = endpoint.response_time || endpoint.responseTimeMs || 0;
  const isNeverChecked = endpoint.never_checked || endpoint.neverChecked || (!endpoint.lastCheck && !endpoint.last_check);
  const isSkipHealthCheck = endpoint.skipHealthCheck || endpoint.skip_health_check;
  const healthState = endpoint.health_state || endpoint.healthState || '';
  const lastFailCode = endpoint.last_fail_code || endpoint.lastFailCode || 0;
  const lastFailReason = endpoint.last_fail_reason || endpoint.lastFailReason || '';
  const isRequestFailed = healthState === 'request_failed';

  const failoverEnabled = normalizeBool(endpoint.failoverEnabled, true);
  const multiplier = getCredentialGroupCostMultiplier(credentialGroup, endpoint);
  const endpointURL = endpoint.url || endpoint.URL || endpoint.baseURL || endpoint.base_url || '';
  const numericPriority = Number(endpoint.priority);
  const priorityValue = Number.isFinite(numericPriority) ? Math.max(0, Math.floor(numericPriority)) : 0;
  const isChannelActive = typeof isActiveChannel === 'boolean'
    ? isActiveChannel
    : Boolean(endpoint.group_is_active || endpoint.groupIsActive);
  const endpointEnabled = isSqliteMode
    ? (isChannelActive && (isCurrentEndpoint || normalizeBool(endpoint.enabled, false)))
    : isChannelActive;
  const endpointSwitchActive = isSqliteMode
    ? Boolean(isCurrentEndpoint)
    : isChannelActive;
  const endpointSwitchDisabled = !failoverEnabled;
  const failoverToggleBlocked = failoverEnabled && endpointSwitchActive && !canDisableFailover;
  const isUnclaimedEndpoint = !isChannelActive && !channelFailoverEnabled;
  const showHealthBadge = inScope && !isUnclaimedEndpoint && (isSkipHealthCheck || !isRequestFailed);
  const hasRequestFailure = Number(lastFailCode) > 0 || !!lastFailReason || isRequestFailed;
  const showRequestFailedBadge = inScope && !isUnclaimedEndpoint && hasRequestFailure;
  const showLatencyBadge = inScope && !isUnclaimedEndpoint && !showRequestFailedBadge;
  const rowActive = failoverEnabled && isChannelActive && (!isSqliteMode || endpointEnabled);
  const statusBadgeText = isUnclaimedEndpoint
    ? '未接管'
    : (!failoverEnabled
        ? '不参与'
        : (showPaused
        ? '暂停'
        : (!isChannelActive
        ? ''
        : '')));
  const statusBadgeTitle = isUnclaimedEndpoint
    ? '渠道间转移未开启，非当前接管渠道不参与健康检查'
    : (!failoverEnabled
        ? '该端点不参与渠道内故障转移，因此不会被使用/检测'
        : (showPaused
        ? '渠道已暂停：不参与健康检查与故障转移'
        : (!isChannelActive
            ? '备用渠道端点：当前渠道非活跃'
            : '')));

  return (
    <div className="border-b border-slate-100 last:border-b-0">
      <div
        className={`group/row relative flex h-[56px] min-h-[56px] max-h-[56px] items-start gap-3 px-4 py-1.5 text-left transition-colors duration-150 ${
          rowActive ? 'bg-indigo-50/20' : 'hover:bg-slate-50/80 opacity-85'
        }`}
      >
        <div className="flex h-5 w-[74px] flex-shrink-0 self-center items-center gap-3">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              if (!endpointSwitchDisabled && !endpointSwitchActive) {
                onActivateEndpoint?.(endpoint);
              }
            }}
            className={`relative inline-flex h-[18px] w-8 flex-shrink-0 rounded-full transition-colors duration-200 ease-out focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 ${
              endpointSwitchActive
                ? 'bg-emerald-500'
                : endpointSwitchDisabled
                  ? 'cursor-not-allowed bg-slate-100'
                  : 'bg-slate-200 group-hover/row:bg-slate-300'
            }`}
            aria-label={
              endpointSwitchDisabled
                ? '端点未参与故障转移，无法设为当前路由端点'
                : endpointSwitchActive
                  ? '当前路由端点'
                  : '设为当前路由端点'
            }
            aria-pressed={endpointSwitchActive}
            aria-disabled={endpointSwitchDisabled}
          >
            <span
              className={`absolute top-[2px] h-[14px] w-[14px] rounded-full bg-white shadow-[0_1px_2px_rgba(15,23,42,0.15)] transition-all duration-200 ease-out ${
                endpointSwitchActive ? 'left-[16px]' : 'left-[2px]'
              }`}
            />
          </button>

          <span className="inline-flex h-[18px] min-w-[30px] flex-shrink-0 items-center justify-center rounded bg-slate-100 px-1.5 text-[10px] font-medium leading-none text-slate-500">
            P{priorityValue}
          </span>
        </div>

        <div
          role="button"
          tabIndex={0}
          onClick={() => onOpen?.(endpoint)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              onOpen?.(endpoint);
            }
          }}
          className="min-w-0 flex-1 cursor-pointer rounded-md py-0.5 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300"
        >
          <div className="flex h-5 min-w-0 items-center">
            <span className={`relative inline-flex h-5 min-w-0 max-w-full items-center overflow-visible text-xs font-semibold leading-none ${
              endpointSwitchActive ? 'text-slate-900' : 'text-slate-700'
            }`}>
              <Tooltip
                content={endpointURL || endpoint.name}
                className={`min-w-0 ${multiplier && multiplier !== 1.0 ? 'pr-8' : ''}`}
                contentClassName="max-w-[420px] break-all font-mono"
              >
                <span className="block min-w-0 truncate leading-none">
                  {endpoint.name}
                </span>
              </Tooltip>
              {multiplier && multiplier !== 1.0 && (
                <span className="pointer-events-none absolute -top-1 right-0 z-10 inline-flex h-3 w-fit flex-shrink-0 items-center justify-center rounded-full bg-amber-500 px-1 font-mono text-[8px] font-bold leading-none text-white shadow-[0_1px_1px_rgba(217,119,6,0.35)] whitespace-nowrap">
                  {multiplier}x
                </span>
              )}
            </span>
          </div>

          <div className="mt-0.5 flex min-w-0 flex-nowrap items-center gap-1.5 overflow-visible">
            {showHealthBadge && (
              <HealthBadge healthy={endpoint.healthy} neverChecked={isNeverChecked} skipped={isSkipHealthCheck} />
            )}
            {showRequestFailedBadge && (
              <RequestFailedBadge failCode={lastFailCode} failReason={lastFailReason} />
            )}
            {showLatencyBadge && (
              <LatencyBadge ms={responseTime} inScope={inScope} />
            )}
            {statusBadgeText && (
              <span
                className="inline-flex h-5 w-fit flex-shrink-0 items-center justify-center gap-1 rounded-full border border-slate-200 bg-slate-50 px-1.5 text-[10px] font-medium leading-none text-slate-500 whitespace-nowrap"
                aria-label={statusBadgeTitle || statusBadgeText}
              >
                {statusBadgeText === '不参与' && <RepeatOffIcon size={10} />}
                {statusBadgeText}
              </span>
            )}
          </div>
        </div>

        <div className="flex w-[92px] flex-shrink-0 self-center items-center justify-end gap-0.5">
          {isSqliteMode && (
            <>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  if (failoverToggleBlocked) return;
                  onToggleFailover?.(endpoint, !failoverEnabled);
                }}
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
                    ? (endpointSwitchActive
                        ? '点击：不参与渠道内故障转移，并自动切换当前路由端点'
                        : '点击：不参与渠道内故障转移')
                    : '点击：参与渠道内故障转移'
                }
                aria-pressed={failoverEnabled}
              >
                {failoverEnabled ? <Repeat size={12} /> : <RepeatOffIcon size={12} />}
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onToggleSkipHealthCheck?.(endpoint, !isSkipHealthCheck);
                }}
                className={`${ENDPOINT_STATE_BUTTON_CLASS} ${
                  isSkipHealthCheck
                    ? 'text-slate-400 hover:bg-slate-100 hover:text-emerald-600'
                    : 'bg-emerald-50 text-emerald-600 hover:bg-emerald-100'
                }`}
                aria-label={isSkipHealthCheck ? '已跳过健康检测 (点击关闭)' : '跳过健康检测 (点击开启)'}
                aria-pressed={isSkipHealthCheck}
              >
                {isSkipHealthCheck ? <ShieldOff size={12} /> : <ShieldCheck size={12} />}
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onEdit?.(endpoint);
                }}
                className="rounded-md p-1 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-indigo-600"
                aria-label="编辑端点"
              >
                <Pencil size={12} />
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onDelete?.(endpoint);
                }}
                className="rounded-md p-1 text-slate-400 transition-colors duration-150 hover:bg-rose-50 hover:text-rose-600"
                aria-label="删除端点"
              >
                <Trash2 size={12} />
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

// ============================================
// 端点详情弹窗（点击端点卡片弹出）
// ============================================

const EndpointDetailModal = ({
  endpoint,
  credentialGroup,
  isOpen,
  isSqliteMode,
  channelFailoverEnabled = true,
  channelMeta = null,
  globalTimeoutSeconds = 300,
  // failoverDefaultCooldownSeconds 已移除
  onClose,
  onEdit,
  onDelete
}) => {
  if (!isOpen || !endpoint) return null;

  const channel = endpoint.channel || endpoint.group || '-';
  const responseTime = endpoint.response_time || endpoint.responseTimeMs || 0;
  const createdAt = getDetailTimestamp(endpoint.createdAt, endpoint.created_at, endpoint.CreatedAt);
  const updatedAt = getDetailTimestamp(endpoint.updatedAt, endpoint.updated_at, endpoint.UpdatedAt);
  const rawLastCheck = endpoint.lastCheck || endpoint.last_check || endpoint.LastCheck;
  const lastCheck = formatLastCheck(rawLastCheck || updatedAt);
  const inScope = endpoint._health_scope !== false;
  const neverChecked = endpoint.never_checked || endpoint.neverChecked || !rawLastCheck;
  const isSkipHealthCheck = endpoint.skipHealthCheck || endpoint.skip_health_check;
  const healthNeverChecked = !inScope || neverChecked;
  const healthState = endpoint.health_state || endpoint.healthState || '';
  const lastFailCode = endpoint.last_fail_code || endpoint.lastFailCode || 0;
  const lastFailReason = endpoint.last_fail_reason || endpoint.lastFailReason || '';
  const suspendedCountValue = Number.isFinite(Number(endpoint.suspendedCount || endpoint.suspended_count || 0))
    ? Number(endpoint.suspendedCount || endpoint.suspended_count || 0)
    : 0;
  const isSuspended = suspendedCountValue > 0;
  const isRequestFailed = healthState === 'request_failed';
  const showHealthBadge = inScope && (isSkipHealthCheck || !isRequestFailed);
  const hasFailDetail = (Number(lastFailCode) > 0) || !!lastFailReason || isRequestFailed;

  const failoverEnabled = normalizeBool(endpoint.failoverEnabled, true);
  const supportsCountTokens = !!endpoint.supportsCountTokens;
  const streamOnly = !!(endpoint.streamOnly ?? endpoint.stream_only);
  const priority = Number(endpoint.priority ?? 0);
  const timeoutSeconds = Number(endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0);
  const hasCustomTimeout = Number.isFinite(timeoutSeconds) && timeoutSeconds > 0 && timeoutSeconds !== 300;
  const effectiveGlobalTimeoutSeconds = Number(globalTimeoutSeconds) || 300;
  const timeoutLabel = hasCustomTimeout ? `${timeoutSeconds}s` : `${effectiveGlobalTimeoutSeconds}s`;
  const timeoutScope = hasCustomTimeout ? '端点覆盖' : '使用全局超时';
  const credentialGroupName = credentialGroup?.name || endpoint.credentialGroupName || endpoint.credential_group_name || '默认分组';
  const credentialGroupIsDefault = Boolean(credentialGroup?.isDefault || credentialGroup?.is_default);
  const credentialGroupSecretMasked = credentialGroup?.tokenMasked ||
    credentialGroup?.token_masked ||
    credentialGroup?.apiKeyMasked ||
    credentialGroup?.api_key_masked ||
    '使用分组凭证';
  const channelModelMapping = channelMeta?.modelMapping || channelMeta?.model_mapping || {};
  const modelMappingEntries = Object.entries(channelModelMapping)
    .map(([sourceModel, targetModel]) => [String(sourceModel || '').trim(), String(targetModel || '').trim()])
    .filter(([sourceModel, targetModel]) => sourceModel && targetModel);
  const modelMultiplierRules = credentialGroup?.modelMultipliers || credentialGroup?.model_multipliers || endpoint.modelMultipliers || endpoint.model_multipliers || [];
  const multiplierItems = [
    { label: '总', value: getCredentialGroupCostMultiplier(credentialGroup, endpoint), tone: 'indigo' },
    { label: '输入', value: credentialGroup?.inputCostMultiplier ?? credentialGroup?.input_cost_multiplier ?? endpoint.inputCostMultiplier ?? endpoint.input_cost_multiplier ?? 1, tone: 'slate' },
    { label: '输出', value: credentialGroup?.outputCostMultiplier ?? credentialGroup?.output_cost_multiplier ?? endpoint.outputCostMultiplier ?? endpoint.output_cost_multiplier ?? 1, tone: 'sky' },
    { label: '缓存读取', value: credentialGroup?.cacheReadCostMultiplier ?? credentialGroup?.cache_read_cost_multiplier ?? endpoint.cacheReadCostMultiplier ?? endpoint.cache_read_cost_multiplier ?? 1, tone: 'amber' },
    { label: '5分钟写入', value: credentialGroup?.cacheCreationCostMultiplier ?? credentialGroup?.cache_creation_cost_multiplier ?? endpoint.cacheCreationCostMultiplier ?? endpoint.cache_creation_cost_multiplier ?? 1, tone: 'emerald' },
    { label: '1小时写入', value: credentialGroup?.cacheCreationCostMultiplier1h ?? credentialGroup?.cache_creation_cost_multiplier_1h ?? endpoint.cacheCreationCostMultiplier1h ?? endpoint.cache_creation_cost_multiplier_1h ?? 1, tone: 'emerald' }
  ];
  const modelMultiplierValueKeys = [
    ['costMultiplier', 'cost_multiplier'],
    ['inputCostMultiplier', 'input_cost_multiplier'],
    ['outputCostMultiplier', 'output_cost_multiplier'],
    ['cacheReadCostMultiplier', 'cache_read_cost_multiplier']
  ];
  const hasCustomEndpointMultiplier = multiplierItems.some((item) => !isDefaultDetailMultiplier(item.value));
  const hasCustomModelMultiplier = modelMultiplierRules.some((rule) => (
    modelMultiplierValueKeys.some(([camelKey, snakeKey]) => (
      !isDefaultDetailMultiplier(rule?.[camelKey] ?? rule?.[snakeKey] ?? 1)
    ))
  ));
  const hasCustomCostMultiplier = hasCustomEndpointMultiplier || hasCustomModelMultiplier;
  const hasModelMapping = modelMappingEntries.length > 0;

  const rows = [
    {
      label: '渠道',
      icon: Server,
      content: <span className="text-sm font-semibold text-slate-900">{channel}</span>
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
            {credentialGroupSecretMasked}
          </div>
        </div>
      )
    }
  ];

  // cooldown 相关变量已移除

  return renderModalPortal(
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50 animate-fade-in"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) {
          onClose?.();
        }
      }}
    >
      <div
        className="bg-white rounded-2xl shadow-xl w-full max-w-3xl max-h-[calc(100vh-2rem)] flex flex-col overflow-hidden"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="border-b border-slate-100 px-6 py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 flex-wrap">
                <h2 className="text-lg font-semibold text-slate-900 truncate">{endpoint.name}</h2>
                <span className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium bg-blue-50 text-blue-600 border border-blue-100">
                  {channel}
                </span>
              </div>
              <Tooltip content={endpoint.url} className="block min-w-0">
                <p className="text-xs text-slate-500 font-mono mt-1 truncate">
                  {endpoint.url}
                </p>
              </Tooltip>
            </div>

            <div className="flex items-center gap-2 flex-shrink-0">
              {isSqliteMode && (
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    icon={Pencil}
                    onClick={() => onEdit?.(endpoint)}
                  >
                    编辑
                  </Button>
                  <Button
                    variant="danger"
                    size="sm"
                    icon={Trash2}
                    onClick={() => onDelete?.(endpoint)}
                  >
                    删除
                  </Button>
                </>
              )}
              <Button variant="ghost" size="sm" onClick={onClose}>
                关闭
              </Button>
            </div>
        </div>

          <div className="mt-2 flex min-w-0 flex-nowrap items-center gap-1.5 overflow-x-auto pb-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            {showHealthBadge && (
              <HealthBadge healthy={endpoint.healthy} neverChecked={healthNeverChecked} skipped={isSkipHealthCheck} detail />
            )}
            {hasFailDetail && (
              <RequestFailedBadge failCode={lastFailCode} failReason={lastFailReason} detail />
            )}
            {!hasFailDetail && (
              <LatencyBadge ms={responseTime} inScope={inScope} detail />
            )}

            <DetailPill tone="indigo" icon={Settings} mono>
              P{priority}
            </DetailPill>

            <DetailTimeoutPill
              custom={hasCustomTimeout}
              value={timeoutLabel}
              tooltip={`${timeoutScope}: ${timeoutLabel}`}
            />

            <Tooltip content={hasCustomCostMultiplier ? '使用凭证分组配置的计费倍率' : '未配置自定义倍率，使用默认 1x'}>
              <span className="inline-flex">
                <DetailPill tone={hasCustomCostMultiplier ? 'indigo' : 'slate'} icon={Database}>
                  <span className="leading-none">{hasCustomCostMultiplier ? '分组倍率' : '默认倍率'}</span>
                  <span className="font-mono leading-none">
                    {formatDetailMultiplier(getCredentialGroupCostMultiplier(credentialGroup, endpoint))}
                  </span>
                </DetailPill>
              </span>
            </Tooltip>

            <Tooltip content={failoverEnabled ? '参与渠道内故障转移' : '不参与渠道内故障转移'}>
              <span className="inline-flex">
                <DetailPill tone={failoverEnabled ? 'emerald' : 'slate'} icon={failoverEnabled ? Repeat : RepeatOffIcon}>
                  {failoverEnabled ? '参与故障转移' : '不参与转移'}
                </DetailPill>
              </span>
            </Tooltip>

            {supportsCountTokens && (
              <Tooltip content="支持 count_tokens">
                <span className="inline-flex">
                  <DetailPill tone="purple" icon={Calculator}>
                    count_tokens
                  </DetailPill>
                </span>
              </Tooltip>
            )}

            {streamOnly && (
              <Tooltip content="该端点仅支持流式请求，非流式请求不会路由到此端点">
                <span className="inline-flex">
                  <DetailPill tone="amber" icon={Waves}>
                    仅流式
                  </DetailPill>
                </span>
              </Tooltip>
            )}

            <span className="flex-shrink-0 whitespace-nowrap font-mono text-[10px] text-slate-400">
              最后检查 {lastCheck}
            </span>
          </div>
          {!isSuspended && hasFailDetail && (
            <div className="mt-3 rounded-xl border border-rose-200 bg-rose-50 p-4">
              <div className="mb-2 flex items-center gap-2">
                <XCircle size={16} className="text-rose-600" />
                <div className="text-sm font-medium text-rose-800">异常详情</div>
              </div>
              <div className="space-y-2 text-sm">
                {lastFailCode > 0 && (
                  <div className="flex items-start gap-2">
                    <span className="min-w-[70px] font-medium text-rose-600">状态码:</span>
                    <span className="rounded bg-rose-100 px-2 py-0.5 font-mono text-rose-900">
                      {lastFailCode}
                    </span>
                  </div>
                )}
                {lastFailReason && (
                  <div className="flex items-start gap-2">
                    <span className="min-w-[70px] font-medium text-rose-600">失败原因:</span>
                    <span className="break-all text-rose-900">
                      {lastFailReason}
                    </span>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>

        <div className="overflow-y-auto p-6">
          <EndpointAvailabilityBar
            provider="claude"
            channel={endpoint.channel || ''}
            endpointName={endpoint.name || ''}
          />

          <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
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
          </div>

          <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
            <DetailMetricCard label="分组计费倍率" icon={Database} wide>
              <div className="space-y-3">
                <div className="flex flex-wrap gap-2">
                  {multiplierItems.map((item) => (
                    <DetailChip key={item.label} tone={item.tone} mono>
                      <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">{item.label}</span>
                      {formatDetailMultiplier(item.value)}
                    </DetailChip>
                  ))}
                </div>

                {modelMultiplierRules.length > 0 && (
                  <div className="border-t border-slate-200/70 pt-3">
                    <div className="mb-2 text-xs font-medium text-slate-500">模型附加倍率</div>
                    <div className="space-y-2">
                      {modelMultiplierRules.map((rule, index) => (
                        <div
                          key={`${rule.model || 'model'}-${index}`}
                          className="flex flex-wrap items-center gap-2"
                        >
                          <ModelTag model={rule.model} />
                          <DetailChip tone="indigo" mono>
                            <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">总</span>
                            {formatDetailMultiplier(rule.costMultiplier ?? rule.cost_multiplier ?? 1)}
                          </DetailChip>
                          <DetailChip tone="slate" mono>
                            <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">输入</span>
                            {formatDetailMultiplier(rule.inputCostMultiplier ?? rule.input_cost_multiplier ?? 1)}
                          </DetailChip>
                          <DetailChip tone="sky" mono>
                            <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">输出</span>
                            {formatDetailMultiplier(rule.outputCostMultiplier ?? rule.output_cost_multiplier ?? 1)}
                          </DetailChip>
                          <DetailChip tone="amber" mono>
                            <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">缓存</span>
                            {formatDetailMultiplier(rule.cacheReadCostMultiplier ?? rule.cache_read_cost_multiplier ?? 1)}
                          </DetailChip>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            </DetailMetricCard>
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

          {/* 挂起详情（挂起优先展示） */}
          {isSuspended && (
            <div className="mt-4">
              <div className="bg-amber-50 rounded-xl p-4 border border-amber-200">
                <div className="flex items-center gap-2 mb-2">
                  <Clock size={16} className="text-amber-600" />
                  <div className="text-sm font-medium text-amber-800">挂起详情</div>
                </div>
                <div className="space-y-2 text-sm">
                  <div className="flex items-start gap-2">
                    <span className="text-amber-600 font-medium min-w-[70px]">挂起请求:</span>
                    <span className="font-mono text-amber-900 bg-amber-100 px-2 py-0.5 rounded">
                      {suspendedCountValue}
                    </span>
                  </div>
                  <div className="flex items-start gap-2">
                    <span className="text-amber-600 font-medium min-w-[70px]">等待说明:</span>
                    <span className="text-amber-900">
                      {channelFailoverEnabled
                        ? '请求已挂起等待端点恢复或自动切换渠道。'
                        : '请求已挂起，等待用户点击「重试」按钮重新尝试当前渠道。'}
                    </span>
                  </div>
                  {hasFailDetail && (
                    <div className="pt-2 border-t border-amber-200/60">
                      <div className="text-xs text-amber-700 mb-1">最近失败</div>
                      <div className="space-y-2 text-sm">
                        {lastFailCode > 0 && (
                          <div className="flex items-start gap-2">
                            <span className="text-amber-600 font-medium min-w-[70px]">状态码:</span>
                            <span className="font-mono text-amber-900 bg-amber-100 px-2 py-0.5 rounded">
                              {lastFailCode}
                            </span>
                          </div>
                        )}
                        {lastFailReason && (
                          <div className="flex items-start gap-2">
                            <span className="text-amber-600 font-medium min-w-[70px]">失败原因:</span>
                            <span className="text-amber-900 break-all">
                              {lastFailReason}
                            </span>
                          </div>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {endpoint.headers && Object.keys(endpoint.headers).length > 0 && (
            <div className="mt-4">
              <div className="flex items-center justify-between mb-2">
                <div className="text-xs font-medium text-slate-500">Headers</div>
                <button
                  onClick={() => navigator.clipboard.writeText(JSON.stringify(endpoint.headers, null, 2))}
                  className="text-xs text-slate-400 hover:text-indigo-600 transition-colors"
                >
                  复制
                </button>
              </div>
              <pre className="text-xs bg-slate-50 border border-slate-200/60 rounded-xl p-3 overflow-auto">
{JSON.stringify(endpoint.headers, null, 2)}
              </pre>
            </div>
          )}

          <DetailTimestampFooter createdAt={createdAt} updatedAt={updatedAt} />
        </div>
      </div>
    </div>
  );
};

// ============================================
// 新建渠道弹窗（SQLite 模式）
// ============================================

const CreateChannelModal = ({
  open,
  onClose,
  onSubmit,
  loading = false,
  serverError = '',
  mode = 'create',
  initialValue = null,
  channelFailoverEnabled = false
}) => {
  const isEdit = mode === 'edit';
  const showPriority = isEdit || channelFailoverEnabled;
  const [name, setName] = useState(() => (isEdit ? (initialValue?.name || '') : ''));
  const [website, setWebsite] = useState(() => (isEdit ? (initialValue?.website || '') : ''));
  const [priority, setPriority] = useState(() => (isEdit ? String(initialValue?.priority ?? 1) : ''));
  const [modelMappingRows, setModelMappingRows] = useState(() => buildInitialModelMappingRows(initialValue?.modelMapping));
  const [activeTab, setActiveTab] = useState('basic');
  const [error, setError] = useState('');

  if (!open) return null;

  const updateMappingRow = (index, key, value) => {
    setModelMappingRows((prev) =>
      (prev || []).map((row, rowIndex) =>
        rowIndex === index ? { ...row, [key]: value } : row
      )
    );
    if (error) {
      setError('');
    }
  };

  const addMappingRow = () => {
    setModelMappingRows((prev) => [
      ...(prev || []),
      { sourceModel: '', targetModel: '' }
    ]);
  };

  const removeMappingRow = (index) => {
    setModelMappingRows((prev) => {
      const nextRows = (prev || []).filter((_, rowIndex) => rowIndex !== index);
      return nextRows.length > 0 ? nextRows : [{ sourceModel: '', targetModel: '' }];
    });
  };

  return renderModalPortal(
    <div className="fixed inset-0 bg-slate-900/30 backdrop-blur-sm flex items-center justify-center p-4 z-[60]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
        <div className="flex items-start justify-between px-6 py-4 border-b border-slate-100">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑渠道' : '新建渠道'}</h2>
            <p className="text-xs text-slate-500 mt-1">
              {isEdit ? '修改渠道官网与优先级' : '创建渠道后继续配置凭证分组与端点'}
            </p>
          </div>
          <Tooltip content="关闭">
            <button
              onClick={onClose}
              className="p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700 rounded-lg transition-colors"
              disabled={loading}
              aria-label="关闭"
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            const trimmedName = name.trim();
            if (!trimmedName) {
              setActiveTab('basic');
              setError('请输入渠道名称');
              return;
            }
            const priorityValue = priority.trim();
            const hasPriority = priorityValue !== '';
            const parsedPriority = Number(priorityValue);
            if ((isEdit && !hasPriority) || (hasPriority && (!Number.isInteger(parsedPriority) || parsedPriority < 1))) {
              setActiveTab('basic');
              setError('优先级必须是 1 或更大的整数');
              return;
            }
            setError('');
            onSubmit?.({
              name: trimmedName,
              website: website.trim(),
              priority: priorityValue,
              modelMapping: buildModelMappingPayload(modelMappingRows)
            });
          }}
          className="flex flex-1 flex-col overflow-hidden"
          autoComplete="off"
        >
          <div className="border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <SegmentedTabs
              tabs={CHANNEL_FORM_TABS}
              activeTab={activeTab}
              onChange={setActiveTab}
              disabled={loading}
            />
          </div>

          <div className="flex-1 overflow-y-auto p-6">
            {(error || serverError) && (
              <div className="mb-4 p-3 bg-rose-50 border border-rose-200 rounded-lg text-rose-700 text-sm">
                {error || serverError}
              </div>
            )}

            {activeTab === 'basic' && (
          <div className="space-y-4">
            <div className={showPriority ? 'grid grid-cols-1 gap-4 sm:grid-cols-3' : ''}>
              <div className={showPriority ? 'sm:col-span-2' : ''}>
                <label className="block text-sm font-medium text-slate-700">
                  渠道名称 <span className="text-rose-500">*</span>
                </label>
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="例如：official / backup / openai"
                  autoComplete="off"
                  data-form-type="other"
                  className={`mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 ${
                    isEdit ? 'disabled:bg-slate-100 disabled:text-slate-500 disabled:cursor-not-allowed' : ''
                  }`}
                  disabled={loading || isEdit}
                />
                {isEdit && (
                  <p className="text-xs text-slate-400 mt-1">渠道名称用于关联端点，不支持修改</p>
                )}
              </div>

              {showPriority && (
                <div>
                  <label className="block text-sm font-medium text-slate-700">优先级</label>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={priority}
                    onChange={(e) => setPriority(e.target.value)}
                    placeholder={isEdit ? undefined : '留空自动分配'}
                    autoComplete="off"
                    data-form-type="other"
                    className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                    disabled={loading}
                  />
                </div>
              )}
            </div>

            <div>
              <label className="block text-sm font-medium text-slate-700">渠道官网</label>
              <input
                value={website}
                onChange={(e) => setWebsite(e.target.value)}
                placeholder="https://example.com (可选)"
                autoComplete="off"
                data-form-type="other"
                className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                disabled={loading}
              />
              <p className="text-xs text-slate-400 mt-1">用于渠道信息展示，非必填</p>
            </div>

          </div>
            )}

            {activeTab === 'mapping' && (
            <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <label className="block text-sm font-medium text-slate-700">模型映射</label>
                  <p className="mt-0.5 text-xs text-slate-400">请求模型精确命中后替换为该渠道支持的模型。</p>
                </div>
                <button
                  type="button"
                  onClick={addMappingRow}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-600 transition-colors hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-700"
                  disabled={loading}
                >
                  <Plus size={14} />
                  添加
                </button>
              </div>

              <div className="mt-3 space-y-2">
                {(modelMappingRows || []).map((row, index) => (
                  <div
                    key={`channel-model-mapping-${index}`}
                    className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_1fr_auto] sm:items-center"
                  >
                    <input
                      type="text"
                      value={row.sourceModel}
                      onChange={(event) => updateMappingRow(index, 'sourceModel', event.target.value)}
                      placeholder="claude-3-5-haiku-20241022"
                      autoComplete="off"
                      data-form-type="other"
                      className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                      disabled={loading}
                    />
                    <ArrowRight size={16} className="text-slate-400" />
                    <input
                      type="text"
                      value={row.targetModel}
                      onChange={(event) => updateMappingRow(index, 'targetModel', event.target.value)}
                      placeholder="claude-sonnet-4-20250514"
                      autoComplete="off"
                      data-form-type="other"
                      className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                      disabled={loading}
                    />
                    <Tooltip content="删除映射">
                      <button
                        type="button"
                        onClick={() => removeMappingRow(index)}
                        className="rounded-lg bg-white p-2 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600"
                        disabled={loading}
                      >
                        <Trash2 size={16} />
                      </button>
                    </Tooltip>
                  </div>
                ))}
              </div>
            </div>
            )}
          </div>

          <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-white px-6 py-4">
            <Button variant="ghost" type="button" onClick={onClose} disabled={loading}>
              取消
            </Button>
            <Button icon={isEdit ? CheckCircle2 : undefined} type="submit" loading={loading}>
              {isEdit ? '保存修改' : '下一步'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

const ClaudeCredentialGroupFormModal = ({
  open,
  mode = 'create',
  channelName = '',
  group = null,
  continueToEndpoint = false,
  loading = false,
  serverError = '',
  endpointCopyOptions = [],
  onClose,
  onSubmit
}) => {
  const isEdit = mode === 'edit';
  const [formData, setFormData] = useState(() => buildInitialClaudeCredentialGroupForm(group, channelName));
  const [activeTab, setActiveTab] = useState('basic');
  const [standardMultiplierSettingsOpen, setStandardMultiplierSettingsOpen] = useState(false);
  const [error, setError] = useState('');
  const [showToken, setShowToken] = useState(false);
  const [showApiKey, setShowApiKey] = useState(false);
  const [credentialSecretType, setCredentialSecretType] = useState(() => getInitialClaudeCredentialSecretType(group));
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
    ? CREDENTIAL_GROUP_CREATE_FORM_TABS
    : CREDENTIAL_GROUP_FORM_TABS;

  if (!open) return null;

  const updateField = (key, value) => {
    setFormData((prev) => ({ ...prev, [key]: value }));
    if (error) setError('');
  };

  const handleStandardMultiplierSave = (values) => {
    setFormData((prev) => ({ ...prev, ...values }));
    setStandardMultiplierSettingsOpen(false);
    if (error) setError('');
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

  const updateCredentialSecretType = (nextType) => {
    setCredentialSecretType(nextType);
    if (error) setError('');
  };

  const updateModelMultiplierRows = (rows) => {
    setFormData((prev) => ({ ...prev, modelMultiplierRows: rows }));
    if (error) setError('');
  };

  const buildSubmitPayload = () => {
    const name = formData.name.trim();
    const token = credentialSecretType === 'token' ? formData.token.trim() : '';
    const apiKey = credentialSecretType === 'apiKey' ? formData.apiKey.trim() : '';

    if (!name) {
      setError('分组名称不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
    if (credentialSecretType === 'token' && !token) {
      setError('Token 不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
    if (credentialSecretType === 'apiKey' && !apiKey) {
      setError('API Key 不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }

	const dailyCostLimitUSD = Number(formData.dailyCostLimitUSD);
	if (!Number.isFinite(dailyCostLimitUSD) || dailyCostLimitUSD < 0) {
	  setError('每日成本限额必须是大于或等于 0 的数字');
	  setFormStep('details');
	  setActiveTab('billing');
	  return null;
	}

    const multiplierError = validateClaudeCredentialGroupMultipliers(formData);
    if (multiplierError) {
      setError(multiplierError);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }

    const modelMultiplierResult = parseModelMultiplierRows(formData.modelMultiplierRows);
    if (modelMultiplierResult.error) {
      setError(modelMultiplierResult.error);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }

    return {
      ...formData,
      channel: formData.channel || channelName,
      name,
      token,
      apiKey,
	  dailyCostLimitUSD,
      copyEndpointsFromGroupId: formData.copyEndpointsFromGroupId || '',
      modelMultipliers: modelMultiplierResult.payload
    };
  };

  const renderSecretInput = ({ label, field, placeholder, visible, setVisible }) => (
    <div>
      <label className="mb-1.5 block text-sm font-medium text-slate-700">{label}</label>
      <div className="relative">
        <input
          type={visible ? 'text' : 'password'}
          value={formData[field] || ''}
          onChange={(event) => updateField(field, event.target.value)}
          placeholder={placeholder}
          autoComplete="new-password"
          data-form-type="other"
          className="w-full rounded-lg border border-slate-200 px-3 py-2 pr-10 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20 [&::-ms-reveal]:hidden [&::-ms-clear]:hidden [&::-webkit-credentials-auto-fill-button]:hidden"
          disabled={loading}
        />
        <Tooltip content={visible ? '隐藏' : '显示'} className="absolute right-2 top-1/2 -translate-y-1/2">
          <button
            type="button"
            onClick={() => setVisible((prev) => !prev)}
            className="rounded-md p-1 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
            tabIndex={-1}
            aria-label={visible ? '隐藏' : '显示'}
          >
            {visible ? <EyeOff size={16} /> : <Eye size={16} />}
          </button>
        </Tooltip>
      </div>
    </div>
  );

  const handleSubmit = async (event) => {
    event.preventDefault();
    const payload = buildSubmitPayload();
    if (!payload) return;
    if (!isEdit && copyEndpointsEnabled && !selectedCopyOption?.value) {
      setError('请选择复制来源分组');
      setFormStep('details');
      setActiveTab('copy');
      return;
    }
    if (!isEdit && selectedCopyOption?.value && selectedCopyOption.endpoints?.length > 0) {
      setCopyStepEndpoints(selectedCopyOption.endpoints);
      setFormStep('copyNames');
      setError('');
      return;
    }

    await onSubmit?.({ ...payload, copyEndpoints: [] });
  };

  const handleConfirmCopyNames = async (editedEndpoints) => {
    const payload = buildSubmitPayload();
    if (!payload) return;
    await onSubmit?.({ ...payload, copyEndpoints: editedEndpoints });
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center overflow-hidden bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in">
      <div className="flex max-h-[calc(100vh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              <KeyRound size={18} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑凭证分组' : '添加凭证分组'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道：{channelName || formData.channel || '-'}</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
              disabled={loading}
              aria-label="关闭"
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
              onCancel={onClose}
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
            {(error || serverError) && (
              <div className="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {error || serverError}
              </div>
            )}

            {activeTab === 'basic' && (
              <div className="space-y-4">
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">
                    分组名称 <span className="text-rose-500">*</span>
                  </label>
                  <input
                    value={formData.name}
                    onChange={(event) => updateField('name', event.target.value)}
                    placeholder="例如：主账号 / 高倍率组"
                    autoComplete="off"
                    data-form-type="other"
                    className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    disabled={loading}
                    autoFocus
                  />
                </div>

                <div className="space-y-3">
                  <SegmentedTabs
                    tabs={CLAUDE_CREDENTIAL_SECRET_TABS}
                    activeTab={credentialSecretType}
                    onChange={updateCredentialSecretType}
                    disabled={loading}
                    className="grid-cols-2"
                    buttonPaddingClass="px-2"
                  />
                  {credentialSecretType === 'token' && renderSecretInput({
                    label: 'Token',
                    field: 'token',
                    placeholder: 'sk-...',
                    visible: showToken,
                    setVisible: setShowToken
                  })}
                  {credentialSecretType === 'apiKey' && renderSecretInput({
                    label: 'API Key',
                    field: 'apiKey',
                    placeholder: 'sk-ant-...',
                    visible: showApiKey,
                    setVisible: setShowApiKey
                  })}
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
				<div className="rounded-xl border border-indigo-100 bg-indigo-50/40 p-3">
				  <label className="mb-1.5 block text-sm font-medium text-slate-700">每日成本限额（USD）</label>
				  <input
					type="number"
					min="0"
					step="0.01"
					value={formData.dailyCostLimitUSD}
					onChange={(event) => updateField('dailyCostLimitUSD', event.target.value)}
					className="w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
					disabled={loading}
				  />
				  <p className="mt-1 text-xs text-slate-500">0 表示不限额；达到限额后自动切换到其他凭证分组。</p>
				</div>
                <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-medium text-slate-700">普通成本倍率</div>
                      <div className="mt-0.5 text-xs text-slate-500">该分组下 Claude 端点的默认计费倍率</div>
                    </div>
                    <Button type="button" variant="secondary" size="sm" onClick={() => setStandardMultiplierSettingsOpen(true)} disabled={loading}>编辑</Button>
                  </div>
                  <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
                    {STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS.map((field) => (
                      <div key={field.name} className="rounded-lg border border-slate-200 bg-white px-2.5 py-2">
                        <div className="truncate text-[11px] text-slate-400">{field.label.replace(/成本倍率|倍率/, '') || '总'}</div>
                        <div className="mt-0.5 truncate text-sm font-medium text-slate-700">{formData[field.name]}</div>
                      </div>
                    ))}
                  </div>
                  <p className="mt-2 text-xs text-slate-400">成本倍率会作为该分组下 Claude 端点的默认计费倍率，{MULTIPLIER_EXPRESSION_HELP}</p>
                </div>

                {standardMultiplierSettingsOpen && (
                  <MultiplierSettingsModal
                    title="普通成本倍率"
                    description="该分组下 Claude 端点的默认计费倍率。"
                    fields={STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS}
                    values={formData}
                    pricingProvider="claude"
                    disabled={loading}
                    onCancel={() => setStandardMultiplierSettingsOpen(false)}
                    onSave={handleStandardMultiplierSave}
                  />
                )}

                <ModelMultiplierEditor
                  rows={formData.modelMultiplierRows}
                  onChange={updateModelMultiplierRows}
                  disabled={loading}
                />
              </div>
            )}

            <div className="mt-5 flex justify-end gap-3 border-t border-slate-100 pt-4">
              <Button type="button" variant="ghost" onClick={onClose} disabled={loading}>
                取消
              </Button>
              <Button type="submit" variant="primary" loading={loading}>
                {continueToEndpoint ? '下一步' : isEdit ? '保存分组' : copyEndpointsEnabled ? '下一步' : '添加分组'}
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

// ============================================
// 渠道卡片组件
// ============================================

const ChannelCard = ({
  channelName,
  endpoints = [],
  channelWebsite = '',
  channelPriority = null,
  groupInfo = null,
  activeChannelName = '',
  isSqliteMode = false,
  channelFailoverEnabled = true,
  suspendedRequestsCount = 0,
  expanded = false,
  onToggleExpanded,
  onActivate,
  onPause,
  onResume,
  onAddEndpoint,
  onAddCredentialGroup,
	onOpenCredentialGroup,
  onEditCredentialGroup,
  onDeleteCredentialGroup,
  onSetDefaultCredentialGroup,
  onEditChannel,
  onDeleteChannel,
  onOpenEndpoint,
  onActivateEndpoint,
  onToggleEndpointFailover,
  onSetGroupEndpointFailover,
  onSetGroupEndpointSkipHealthCheck,
  onToggleEndpointSkipHealthCheck, // 跳过健康检测回调
  onEditEndpoint,
  onDeleteEndpoint,
  manualRetryCount = 0,
  credentialGroups = [],
  loading = false
}) => {
  const [selectedCredentialGroupID, setSelectedCredentialGroupID] = useState(0);
  const groupActive = groupInfo?.active ?? groupInfo?.is_active;
  const isActive = typeof groupActive === 'boolean'
    ? groupActive
    : activeChannelName === channelName;
  const currentRouteEndpoint = pickCurrentRouteEndpoint(endpoints, isActive);
  const defaultCredentialGroup = getDefaultClaudeCredentialGroup({ credentialGroups });
  const activeEndpointGroupID = Number(
    currentRouteEndpoint?.credentialGroupId ||
    currentRouteEndpoint?.credential_group_id ||
    0
  );
  const preferredCredentialGroupID = activeEndpointGroupID || Number(defaultCredentialGroup?.id || credentialGroups[0]?.id || 0);
  const selectedCredentialGroup = credentialGroups.find((group) => Number(group.id) === Number(selectedCredentialGroupID)) ||
    credentialGroups.find((group) => Number(group.id) === preferredCredentialGroupID) ||
    defaultCredentialGroup ||
    credentialGroups[0] ||
    null;
  const selectedCredentialGroupHasCredential = hasClaudeCredential(selectedCredentialGroup);
  const channelHasCredential = credentialGroups.some((group) => hasClaudeCredential(group)) ||
    endpoints.some((endpoint) => hasClaudeCredential(endpoint));
  const selectedCredentialGroupIDResolved = Number(selectedCredentialGroup?.id || 0);
  const groupEndpointFilter = (endpoint) => {
    if (credentialGroups.length === 0) return true;
    const endpointGroupID = Number(endpoint?.credentialGroupId || endpoint?.credential_group_id || 0);
    if (selectedCredentialGroupIDResolved > 0) {
      if (selectedCredentialGroup?.isDefault || selectedCredentialGroup?.is_default) {
        return endpointGroupID === selectedCredentialGroupIDResolved || endpointGroupID === 0;
      }
      return endpointGroupID === selectedCredentialGroupIDResolved;
    }
    return endpointGroupID === 0;
  };
  const getEndpointPriority = (value, fallback = 999) => {
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
  };
  const groupEndpoints = endpoints.filter(groupEndpointFilter).sort((a, b) => {
    const priorityDiff = getEndpointPriority(a?.priority) - getEndpointPriority(b?.priority);
    if (priorityDiff !== 0) return priorityDiff;
    const leftCreatedAt = a?.createdAt || a?.created_at || '';
    const rightCreatedAt = b?.createdAt || b?.created_at || '';
    const createdAtDiff = leftCreatedAt.localeCompare(rightCreatedAt);
    if (createdAtDiff !== 0) return createdAtDiff;
    return Number(a?.id || 0) - Number(b?.id || 0);
  });
  // 分段条上的转移指示器，按分组统计端点归属（与 groupEndpointFilter 同规则）
  const credentialGroupStats = buildCredentialGroupStats(credentialGroups, endpoints);

  const isPaused = !!groupInfo?.paused;

  // 渠道是否参与路由：
  // - 仅渠道内转移时 → 只有活跃渠道参与
  // - 渠道间转移时 → 未暂停的渠道参与
  const channelParticipating = channelFailoverEnabled ? !isPaused : isActive;
  const channelHealthInScope = channelParticipating;
  const routeEndpoints = endpoints.filter(endpointParticipatesInFailover);
  const scopedEndpoints = channelHealthInScope ? routeEndpoints.filter(e => e._health_scope !== false) : [];
  // 跳过检测的端点数
  // 健康统计：跳过检测的端点视为健康
  const scopedHealthyCount = scopedEndpoints.filter(e => e.healthy || e.skipHealthCheck || e.skip_health_check).length;
  const scopedTotalCount = scopedEndpoints.length;

  const totalCount = endpoints.length;
  const hasEndpoints = totalCount > 0;
  const hasRouteEndpoints = routeEndpoints.length > 0;
  const hasGroupInfo = !!groupInfo;
  const manualRetryCountValue = Number.isFinite(Number(manualRetryCount))
    ? Number(manualRetryCount)
    : 0;
  const hasManualRetry = manualRetryCountValue > 0;

  const computedPriority = Math.min(...endpoints.map(e => getEndpointPriority(e.priority)));
  const priority = Number.isFinite(channelPriority) && channelPriority >= 0
    ? channelPriority
    : (groupInfo?.priority ?? (Number.isFinite(computedPriority) ? computedPriority : 999));

  // 判断是否处于"全部失败"警示状态：渠道间故障转移关闭 + 活跃渠道 + 有端点 + 全部端点失败
  const isAllEndpointsFailed = !channelFailoverEnabled && isActive && hasRouteEndpoints && scopedTotalCount > 0 && scopedHealthyCount === 0;
  const hasSuspendedRequests = suspendedRequestsCount > 0;
  const showRetryButton = isAllEndpointsFailed || (isActive && hasSuspendedRequests) || hasManualRetry;
  const showRetryOnInactive = hasManualRetry;

  // 判断当前活跃渠道是否所有端点都失败（用于边框颜色，不受 channelFailoverEnabled 影响）
  const isActiveChannelAllFailed = isActive && hasRouteEndpoints && scopedTotalCount > 0 && scopedHealthyCount === 0;
  const isActiveChannelInterrupted = isActive && (isActiveChannelAllFailed || hasManualRetry);
  const isActiveChannelSuspended = isActive && hasSuspendedRequests && !isActiveChannelInterrupted;

  const claudeActiveRouteClass = channelFailoverEnabled
    ? 'border-violet-300 ring-1 ring-violet-100'
    : 'border-orange-300 ring-1 ring-orange-100';

  // 活跃渠道边框样式
  // 优先级：彻底中断 > 挂起等待 > 跨渠道转移 > 单渠道活跃
  const activeHighlightClass = isActiveChannelInterrupted
    ? 'border-rose-300 ring-1 ring-rose-100'  // 全部失败 → 红色
    : isActiveChannelSuspended
      ? 'border-amber-300 ring-1 ring-amber-100'
      : claudeActiveRouteClass;
  const idleHighlightClass = channelFailoverEnabled && channelParticipating
    ? 'border-violet-100/80 hover:border-violet-200'
    : 'border-slate-200/70 hover:border-slate-300';
  // 默认折叠态：统一只展示 1 个端点，保证同行渠道卡片基础高度一致
  const collapsedVisibleCount = 1;
  const visibleEndpoints = expanded ? groupEndpoints : groupEndpoints.slice(0, collapsedVisibleCount);
  const hasMore = groupEndpoints.length > collapsedVisibleCount;
  const groupFailoverEndpoints = groupEndpoints.filter(endpointParticipatesInFailover);
  const groupNonFailoverEndpoints = groupEndpoints.filter((endpoint) => !endpointParticipatesInFailover(endpoint));
  const groupSkippedHealthEndpoints = groupEndpoints.filter((endpoint) => endpoint.skipHealthCheck || endpoint.skip_health_check);
  const groupCheckedHealthEndpoints = groupEndpoints.filter((endpoint) => !(endpoint.skipHealthCheck || endpoint.skip_health_check));
  const currentRouteInSelectedGroup = !!currentRouteEndpoint &&
    groupEndpoints.some((endpoint) => isSameEndpoint(endpoint, currentRouteEndpoint));
  const hasFailoverOutsideSelectedGroup = endpoints.some((endpoint) => (
    !groupEndpointFilter(endpoint) && endpointParticipatesInFailover(endpoint)
  ));
  const disableGroupFailoverBlocked = groupFailoverEndpoints.length === 0 ||
    (currentRouteInSelectedGroup && !hasFailoverOutsideSelectedGroup);
  const disableGroupFailoverTargets = currentRouteInSelectedGroup
    ? [
      ...groupFailoverEndpoints.filter((endpoint) => !isSameEndpoint(endpoint, currentRouteEndpoint)),
      currentRouteEndpoint
    ].filter(endpointParticipatesInFailover)
    : groupFailoverEndpoints;
  const disableGroupFailoverTip = groupFailoverEndpoints.length === 0
    ? '当前分组没有参与转移的端点'
    : currentRouteInSelectedGroup && !hasFailoverOutsideSelectedGroup
      ? '当前路由端点在此分组，需先让其他分组至少一个端点参与转移'
      : `将当前分组 ${groupFailoverEndpoints.length} 个端点设为不参与转移`;
  const enableGroupFailoverTip = groupNonFailoverEndpoints.length === 0
    ? '当前分组端点已全部参与转移'
    : `将当前分组 ${groupNonFailoverEndpoints.length} 个端点设为参与转移`;
  const shouldEnableGroupFailover = groupNonFailoverEndpoints.length > 0;
  const groupFailoverActionTargets = shouldEnableGroupFailover ? groupNonFailoverEndpoints : disableGroupFailoverTargets;
  const groupFailoverActionDisabled = shouldEnableGroupFailover
    ? groupNonFailoverEndpoints.length === 0
    : disableGroupFailoverBlocked;
  const groupFailoverActionTip = shouldEnableGroupFailover ? enableGroupFailoverTip : disableGroupFailoverTip;
  const shouldSkipGroupHealthCheck = groupCheckedHealthEndpoints.length > 0;
  const groupHealthActionTargets = shouldSkipGroupHealthCheck ? groupCheckedHealthEndpoints : groupSkippedHealthEndpoints;
  const groupHealthActionTip = shouldSkipGroupHealthCheck
    ? `将当前分组 ${groupCheckedHealthEndpoints.length} 个端点设为跳过检测`
    : groupSkippedHealthEndpoints.length > 0
      ? `恢复当前分组 ${groupSkippedHealthEndpoints.length} 个端点的健康检测`
      : '当前分组没有端点';

  const priorityValue = Number.isFinite(priority) ? Math.max(0, Math.floor(priority)) : null;
  const hasValidPriority = priorityValue != null && priorityValue >= 0 && priorityValue < 999;
  const priorityLabel = hasValidPriority ? `P${priorityValue}` : 'P-';

  return (
    <div className={`
      group flex flex-col self-start overflow-hidden rounded-xl border bg-white
      shadow-[0_1px_2px_rgba(15,23,42,0.04)] transition-all duration-150 ease-out hover:shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08)]
      ${isActive ? activeHighlightClass : idleHighlightClass}
    `} data-channel-card={channelName}>
      {/* 渠道头部 */}
      <div className="px-4 py-3">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <h2 className="min-w-0 truncate text-sm font-semibold tracking-tight text-slate-900">{channelName}</h2>
            {isActive && (
              isActiveChannelInterrupted ? (
                <Tooltip content="当前 Claude 接管渠道的可用端点已全部异常，请检查端点或切换渠道" className="flex-shrink-0">
                  <span className="inline-flex items-center gap-1 rounded-full bg-rose-50 px-1.5 py-0.5 text-[10px] font-medium tracking-tight text-rose-700">
                    <AlertTriangle size={10} className="text-rose-600" />
                    已中断
                  </span>
                </Tooltip>
              ) : isActiveChannelSuspended ? (
                <Tooltip content={`已有 ${suspendedRequestsCount} 个请求挂起，请点击当前端点或其他渠道重新激活`} className="flex-shrink-0">
                  <span className="inline-flex items-center gap-1 rounded-full bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium tracking-tight text-amber-700">
                    <AlertTriangle size={10} className="text-amber-600" />
                    等待重新激活 · {suspendedRequestsCount}
                  </span>
                </Tooltip>
              ) : (
                <span className="inline-flex flex-shrink-0 items-center gap-1 rounded-full bg-emerald-50 px-1.5 py-0.5 text-[10px] font-medium tracking-tight text-emerald-700">
                  <span className="h-1 w-1 rounded-full bg-emerald-500" />
                  当前接管
                </span>
              )
            )}
            {channelFailoverEnabled && (
              <span className="inline-flex h-[18px] min-w-[30px] flex-shrink-0 items-center justify-center rounded bg-slate-100 px-1.5 text-[10px] font-medium leading-none text-slate-500">
                {priorityLabel}
              </span>
            )}
          </div>

          {/* 渠道操作 */}
          <div className="flex flex-shrink-0 items-center gap-0.5">
            {/* 非激活渠道可手动设为当前；渠道间转移暂停态需先恢复 */}
            {!isActive && isSqliteMode && hasEndpoints && channelHasCredential && (
              <Tooltip content={
                channelFailoverEnabled && isPaused
                  ? '请先点击"恢复"按钮取消暂停状态'
                  : (showRetryOnInactive ? '重试此渠道' : '设为当前接管渠道')
              }>
                <button
                  type="button"
                  onClick={() => onActivate?.(channelName)}
                  disabled={loading || (channelFailoverEnabled && isPaused)}
                  className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-indigo-600 transition-colors duration-150 hover:bg-indigo-50 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {showRetryOnInactive ? <RefreshCw size={12} /> : <ArrowRightLeft size={12} />}
                  {showRetryOnInactive ? '重试' : '设为当前'}
                </button>
              </Tooltip>
            )}
            {isActive && isSqliteMode && showRetryButton && channelHasCredential && (
                <Tooltip content="重试当前渠道">
                  <button
                    type="button"
                    onClick={() => onActivate?.(channelName)}
                    disabled={loading}
                    className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-indigo-600 transition-colors duration-150 hover:bg-indigo-50 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <RefreshCw size={12} />
                    重试
                  </button>
                </Tooltip>
            )}
            {/* 暂停/恢复按钮 - 控制渠道是否参与渠道间转移 */}
            {channelFailoverEnabled && hasGroupInfo && channelHasCredential && !isPaused && (
              <Tooltip content="暂停此渠道，使其不参与渠道间转移">
                <button
                  type="button"
                  onClick={() => onPause?.(channelName)}
                  disabled={loading}
                  className="inline-flex size-[26px] flex-shrink-0 items-center justify-center rounded-md bg-indigo-50 text-indigo-600 transition-colors duration-150 hover:bg-indigo-100 disabled:cursor-not-allowed disabled:opacity-50"
                  aria-label="当前渠道参与渠道间转移，点击暂停"
                >
                  <Route size={14} />
                </button>
              </Tooltip>
            )}
            {/* 恢复按钮仅在非活跃且已暂停时显示（活跃渠道一定是未暂停的） */}
            {channelFailoverEnabled && hasGroupInfo && channelHasCredential && isPaused && !isActive && (
              <Tooltip content="恢复此渠道，使其重新参与渠道间转移">
                <button
                  type="button"
                  onClick={() => onResume?.(channelName)}
                  disabled={loading}
                  className="inline-flex size-[26px] flex-shrink-0 items-center justify-center rounded-md text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-indigo-600 disabled:cursor-not-allowed disabled:opacity-50"
                  aria-label="当前渠道已暂停渠道间转移，点击恢复"
                >
                  <RouteOff size={14} />
                </button>
              </Tooltip>
            )}
            {channelWebsite && (
              <Tooltip content="打开渠道官网">
                <button
                  type="button"
                  className="rounded-md p-1.5 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
                  aria-label="打开渠道官网"
                  onClick={() => openExternalURL(channelWebsite)}
                >
                  <Globe size={14} />
                </button>
              </Tooltip>
            )}
            {isSqliteMode && (
              <Tooltip content="编辑渠道">
                <button
                  type="button"
                  onClick={() => onEditChannel?.(channelName)}
                  disabled={loading}
                  className={`rounded-md p-1.5 transition-colors ${
                    loading ? 'text-slate-300 cursor-not-allowed' : 'text-slate-400 hover:bg-slate-100 hover:text-slate-700'
                  }`}
                  aria-label="编辑渠道"
                >
                  <Pencil size={14} />
                </button>
              </Tooltip>
            )}
            {isSqliteMode && (
              <Tooltip content="删除渠道">
                <button
                  type="button"
                  onClick={() => onDeleteChannel?.(channelName)}
                  disabled={loading}
                  className={`rounded-md p-1.5 transition-colors ${
                    loading ? 'text-slate-300 cursor-not-allowed' : 'text-slate-400 hover:bg-rose-50 hover:text-rose-600'
                  }`}
                  aria-label="删除渠道"
                >
                  <Trash2 size={14} />
                </button>
              </Tooltip>
            )}
          </div>
        </div>

      </div>

      {isSqliteMode && (
        <div className="border-t border-slate-100 bg-white px-4 py-2">
          <CredentialGroupStrip
            groups={credentialGroups}
            selectedId={selectedCredentialGroupIDResolved}
            stats={credentialGroupStats}
            label={`Credentials · ${credentialGroups.length}`}
            headerExtra={channelFailoverEnabled && isPaused ? (
              <span className="inline-flex h-4 flex-shrink-0 items-center rounded bg-amber-50 px-1.5 text-[9px] font-medium leading-none text-amber-700">
                已暂停
              </span>
            ) : null}
            onSelect={setSelectedCredentialGroupID}
            onOpen={(group) => onOpenCredentialGroup?.(channelName, group)}
            onSetDefault={(group) => onSetDefaultCredentialGroup?.(channelName, group)}
            onEdit={(group) => onEditCredentialGroup?.(channelName, group)}
            onDelete={(group) => onDeleteCredentialGroup?.(channelName, group)}
            onAdd={() => onAddCredentialGroup?.(channelName)}
            disabled={loading}
            emptyHint="暂无分组，将回退旧端点凭证。"
          />
        </div>
      )}

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
            {hasMore && (
              <button
                type="button"
                onClick={() => onToggleExpanded?.()}
                className="inline-flex items-center gap-0.5 rounded-md px-1 py-0.5 text-[10px] font-medium normal-case tracking-normal text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
              >
                {expanded ? '收起' : `展开 ${groupEndpoints.length - collapsedVisibleCount} 个`}
                <ChevronDown
                  size={11}
                  className={`transition-transform duration-200 ease-out ${expanded ? 'rotate-180' : ''}`}
                />
              </button>
            )}
          </div>
          {isSqliteMode && selectedCredentialGroupHasCredential && (
            <div className="flex flex-shrink-0 items-center gap-1">
              <Tooltip content={groupFailoverActionTip}>
                <button
                  type="button"
                  onClick={() => onSetGroupEndpointFailover?.(groupFailoverActionTargets, shouldEnableGroupFailover)}
                  disabled={loading || groupFailoverActionDisabled}
                  className={`${ENDPOINT_STATE_BUTTON_CLASS} disabled:cursor-not-allowed ${
                    shouldEnableGroupFailover
                      ? 'text-slate-400 hover:bg-slate-100 hover:text-indigo-600'
                      : 'bg-indigo-50 text-indigo-600 hover:bg-indigo-100'
                  }`}
                  aria-label={shouldEnableGroupFailover ? '当前分组存在不参与转移的端点' : '当前分组端点全部参与转移'}
                >
                  {shouldEnableGroupFailover ? <RepeatOffIcon size={12} /> : <Repeat size={12} />}
                </button>
              </Tooltip>
              <Tooltip content={groupHealthActionTip}>
                <button
                  type="button"
                  onClick={() => onSetGroupEndpointSkipHealthCheck?.(groupHealthActionTargets, shouldSkipGroupHealthCheck)}
                  disabled={loading || groupHealthActionTargets.length === 0}
                  className={`${ENDPOINT_STATE_BUTTON_CLASS} disabled:cursor-not-allowed ${
                    shouldSkipGroupHealthCheck
                      ? 'bg-emerald-50 text-emerald-600 hover:bg-emerald-100'
                      : 'text-slate-400 hover:bg-slate-100 hover:text-emerald-600'
                  }`}
                  aria-label={shouldSkipGroupHealthCheck ? '当前分组存在正常检测的端点' : '当前分组端点全部跳过检测'}
                >
                  {shouldSkipGroupHealthCheck ? <ShieldCheck size={12} /> : <ShieldOff size={12} />}
                </button>
              </Tooltip>
              <button
                type="button"
                onClick={() => onAddEndpoint?.(channelName, selectedCredentialGroup)}
                disabled={loading}
                className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Plus size={12} />
                添加端点
              </button>
            </div>
          )}
        </div>

        {visibleEndpoints.length === 0 ? (
          <div className="border-t border-slate-100 bg-white px-4 py-6 text-center text-[11px] text-slate-400">
            {selectedCredentialGroupHasCredential
              ? '当前凭证分组暂无端点'
              : '请先配置凭证分组的 Token 或 API Key，再添加端点。'}
          </div>
        ) : (
          <div className="border-t border-slate-100 bg-white">
            {visibleEndpoints.map((endpoint, index) => (
              <EndpointMiniCard
                key={endpoint.id || endpoint._id || `${channelName}-endpoint-${index}`}
                endpoint={endpoint}
                credentialGroup={getCredentialGroupForEndpoint(credentialGroups, endpoint)}
                isActiveChannel={isActive}
                isSqliteMode={isSqliteMode}
                isCurrentEndpoint={isSameEndpoint(endpoint, currentRouteEndpoint)}
                healthInScope={channelHealthInScope}
                channelFailoverEnabled={channelFailoverEnabled}
                canDisableFailover={
                  !(endpointParticipatesInFailover(endpoint) && isSameEndpoint(endpoint, currentRouteEndpoint)) ||
                  endpoints.some((candidate) => !isSameEndpoint(candidate, endpoint) && endpointParticipatesInFailover(candidate))
                }
                onOpen={onOpenEndpoint}
                onActivateEndpoint={onActivateEndpoint}
                onToggleFailover={onToggleEndpointFailover}
                onToggleSkipHealthCheck={onToggleEndpointSkipHealthCheck}
                onEdit={onEditEndpoint}
                onDelete={onDeleteEndpoint}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

// ============================================
// Channels 页面
// ============================================

const ChannelsPage = () => {
  // 存储模式状态
  const [storageStatus, setStorageStatus] = useState(null);
  const [storageEndpoints, setStorageEndpoints] = useState([]);
  const [storageLoading, setStorageLoading] = useState(false);

  // 端点存储统一使用 SQLite；只检查 storageType，不依赖 enabled。
  const isSqliteMode = storageStatus?.storageType === 'sqlite';

  // SQLite 端点由专用 API 加载；保留旧 Hook 仅供兼容，不自动订阅。
  const {
    endpoints,
    loading,
    error,
    stats,
    refresh,
    performBatchHealthCheckAll,
    lastUpdate
  } = useEndpointsData({ enabled: storageStatus ? !isSqliteMode : false });

  // 渠道（组）状态
  const [groups, setGroups] = useState([]);
  const [channelActionLoading, setChannelActionLoading] = useState(false);
  const [channelFailoverEnabled, setChannelFailoverEnabled] = useState(true);
  const [globalTimeoutSeconds, setGlobalTimeoutSeconds] = useState(300);
  // failoverDefaultCooldownSeconds 已移除
  const [channelsMeta, setChannelsMeta] = useState([]);
  const [channelsLastUpdate, setChannelsLastUpdate] = useState('');

  // 挂起请求计数（用于显示等待提示）
  const [suspendedRequestsCount, setSuspendedRequestsCount] = useState(0);
  const [suspendedEndpointCounts, setSuspendedEndpointCounts] = useState({});
  const [manualRetryRequestsCount, setManualRetryRequestsCount] = useState(0);
  const [manualRetryChannelCounts, setManualRetryChannelCounts] = useState({});

  // 批量检测状态
  const [batchCheckLoading, setBatchCheckLoading] = useState(false);

  // 表单状态
  const [showForm, setShowForm] = useState(false);
  const [showCreateChannel, setShowCreateChannel] = useState(false);
  const [createChannelError, setCreateChannelError] = useState('');
  const [showEditChannel, setShowEditChannel] = useState(false);
  const [editChannelError, setEditChannelError] = useState('');
  const [editingChannel, setEditingChannel] = useState(null);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [defaultChannel, setDefaultChannel] = useState('');
  const [defaultCredentialGroupId, setDefaultCredentialGroupId] = useState(0);
  const [lockChannel, setLockChannel] = useState(false);
  const [formLoading, setFormLoading] = useState(false);
  const [channelFormLoading, setChannelFormLoading] = useState(false);
  const [showCredentialGroupForm, setShowCredentialGroupForm] = useState(false);
  const [credentialGroupFormMode, setCredentialGroupFormMode] = useState('create');
  const [credentialGroupFormChannel, setCredentialGroupFormChannel] = useState('');
  const [editingCredentialGroup, setEditingCredentialGroup] = useState(null);
  const [credentialGroupFormLoading, setCredentialGroupFormLoading] = useState(false);
  const [credentialGroupFormError, setCredentialGroupFormError] = useState('');
  const [initialSetupChannelName, setInitialSetupChannelName] = useState('');
	const [credentialGroupDetailTarget, setCredentialGroupDetailTarget] = useState(null);

  // 删除确认状态
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteChannelTarget, setDeleteChannelTarget] = useState(null);
  const [deleteChannelLoading, setDeleteChannelLoading] = useState(false);
  const [deleteCredentialGroupTarget, setDeleteCredentialGroupTarget] = useState(null);
  const [deleteCredentialGroupLoading, setDeleteCredentialGroupLoading] = useState(false);
  const [pauseActiveChannelTarget, setPauseActiveChannelTarget] = useState(null);
  const [actionError, setActionError] = useState('');

  // 端点详情弹窗
  const [detailTarget, setDetailTarget] = useState(null);
  const [detailOpen, setDetailOpen] = useState(false);

  const storageLoadSeqRef = useRef(0);
  const groupsLoadSeqRef = useRef(0);
  const channelsMetaLoadSeqRef = useRef(0);
  const configLoadSeqRef = useRef(0);

  const openEndpointDetail = useCallback((endpoint) => {
    setDetailTarget(endpoint);
    setDetailOpen(true);
  }, []);

  const closeEndpointDetail = useCallback(() => {
    setDetailOpen(false);
    setDetailTarget(null);
  }, []);

  // 加载存储状态
  const loadStorageStatus = useCallback(async ({ throwOnError = false } = {}) => {
    const seq = ++storageLoadSeqRef.current;
    setStorageLoading(true);
    try {
      const status = await getEndpointStorageStatus();
      if (seq !== storageLoadSeqRef.current) return;
      setStorageStatus(status);

      // SQLite 模式下加载存储的端点。
      if (status.storageType === 'sqlite') {
        const records = await getEndpointRecords();
        if (seq !== storageLoadSeqRef.current) return;
        setStorageEndpoints(records);
      }
    } catch (err) {
      console.error('获取存储状态失败:', err);
      if (seq !== storageLoadSeqRef.current) return;
      // 存储后端固定为 SQLite，即使状态接口暂时失败也不要回退到旧模式。
      setStorageStatus({ enabled: false, storageType: 'sqlite' });
      if (throwOnError) throw err;
    } finally {
      if (seq === storageLoadSeqRef.current) {
        setStorageLoading(false);
      }
    }
  }, []);

  // 初始化加载存储状态
  useEffect(() => {
    loadStorageStatus();
  }, [loadStorageStatus]);

  // 加载渠道（组）状态
  const loadGroups = useCallback(async ({ throwOnError = false } = {}) => {
    const seq = ++groupsLoadSeqRef.current;
    try {
      const data = await getGroupsRaw();
      if (seq !== groupsLoadSeqRef.current) return;
      setGroups(Array.isArray(data) ? data : []);
    } catch (err) {
      console.error('获取渠道状态失败:', err);
      if (seq !== groupsLoadSeqRef.current) return;
      setGroups([]);
      if (throwOnError) throw err;
    }
  }, []);

  const loadChannelsMeta = useCallback(async ({ throwOnError = false } = {}) => {
    const seq = ++channelsMetaLoadSeqRef.current;
    // 只检查 storageType
    const sqliteEnabled = storageStatus?.storageType === 'sqlite';
    if (!sqliteEnabled) {
      if (seq === channelsMetaLoadSeqRef.current) {
        setChannelsMeta([]);
        setChannelsLastUpdate('');
      }
      if (throwOnError) {
        throw new Error('SQLite 渠道存储尚未就绪');
      }
      return [];
    }
    try {
      const list = await getChannels();
      const normalizedList = Array.isArray(list) ? list : [];
      if (seq === channelsMetaLoadSeqRef.current) {
        setChannelsMeta(normalizedList);
        setChannelsLastUpdate(new Date().toLocaleTimeString());
      }
      return normalizedList;
    } catch (err) {
      console.error('获取渠道列表失败:', err);
      // 避免瞬时失败导致 UI “清空”，保留上一次结果
      if (throwOnError) throw err;
      return [];
    }
  }, [storageStatus]);

  const loadConfig = useCallback(async () => {
    const seq = ++configLoadSeqRef.current;
    try {
      const [cfg, interChannelEnabled, failoverSettings] = await Promise.all([
        getConfig(),
        isInterChannelFailoverEnabled().catch(() => undefined),
        getSettingsByCategory('failover').catch(() => [])
      ]);
      if (seq !== configLoadSeqRef.current) return;
      const settingsFailoverEnabled = parseSettingsBool(
        (failoverSettings || []).find(setting => setting?.key === 'enabled')?.value
      );
      const configFailoverEnabled = cfg?.failover_enabled ?? cfg?.failoverEnabled;
      const resolvedFailoverEnabled = typeof settingsFailoverEnabled === 'boolean'
        ? settingsFailoverEnabled
        : (typeof interChannelEnabled === 'boolean'
            ? interChannelEnabled
            : (typeof configFailoverEnabled === 'boolean' ? configFailoverEnabled : false));
      setChannelFailoverEnabled(resolvedFailoverEnabled);
      setGlobalTimeoutSeconds(Number(cfg?.global_timeout_seconds ?? cfg?.globalTimeoutSeconds ?? 300) || 300);
      // cooldownSeconds 加载已移除
    } catch (err) {
      console.error('获取配置失败:', err);
      if (seq !== configLoadSeqRef.current) return;
      setChannelFailoverEnabled(false);
      setGlobalTimeoutSeconds(300);
    }
  }, []);

  // 加载挂起请求计数
  const loadSuspendedCount = useCallback(async () => {
    try {
      const [count, endpointCounts, manualCount, manualChannelCounts] = await Promise.all([
        getSuspendedRequestsCount(),
        getSuspendedEndpointCounts(),
        getManualRetryRequestsCount(),
        getManualRetryChannelCounts()
      ]);
      setSuspendedRequestsCount(count || 0);
      setSuspendedEndpointCounts(
        endpointCounts && !Array.isArray(endpointCounts) ? endpointCounts : {}
      );
      setManualRetryRequestsCount(manualCount || 0);
      setManualRetryChannelCounts(
        manualChannelCounts && !Array.isArray(manualChannelCounts) ? manualChannelCounts : {}
      );
    } catch (err) {
      // 静默处理错误，不影响页面正常使用
      console.debug('获取挂起请求数量失败:', err);
    }
  }, []);

  const handleToggleEndpointFailover = useCallback(async (endpoint, enabled) => {
    if (!endpoint?.id) return;
    try {
      setChannelActionLoading(true);
      await setEndpointFailoverEnabledByID(endpoint.id, enabled);
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('切换故障转移参与状态失败:', err);
      setActionError(getFailoverToggleErrorMessage(err, '操作失败'));
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  const handleSetGroupEndpointFailover = useCallback(async (targetEndpoints = [], enabled) => {
    const targets = selectPersistedEndpoints(targetEndpoints);
    if (targets.length === 0) return;
    try {
      setChannelActionLoading(true);
      setActionError('');
      for (const endpoint of targets) {
        await setEndpointFailoverEnabledByID(endpoint.id, enabled);
      }
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('批量切换分组端点转移失败:', err);
      setActionError(getFailoverToggleErrorMessage(err, '批量操作失败'));
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  const handleSetGroupEndpointSkipHealthCheck = useCallback(async (targetEndpoints = [], skip) => {
    const targets = selectPersistedEndpoints(targetEndpoints);
    if (targets.length === 0) return;
    try {
      setChannelActionLoading(true);
      setActionError('');
      for (const endpoint of targets) {
        await setEndpointSkipHealthCheckByID(endpoint.id, skip);
      }
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('批量切换分组端点跳过检测失败:', err);
      setActionError(`批量操作失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  const handleActivateEndpoint = useCallback(async (endpoint) => {
    if (!endpoint?.id) return;
    try {
      setChannelActionLoading(true);
      setActionError('');
      await activateEndpointRecordByID(endpoint.id);
      await loadStorageStatus();
      await loadGroups();
      await loadChannelsMeta();
    } catch (err) {
      console.error('切换当前路由端点失败:', err);
      setActionError(`切换失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadChannelsMeta, loadGroups, loadStorageStatus]);

  // 跳过健康检测快捷开关处理函数
  const handleToggleEndpointSkipHealthCheck = useCallback(async (endpoint, skip) => {
    if (!endpoint?.id) return;
    try {
      setChannelActionLoading(true);
      await setEndpointSkipHealthCheckByID(endpoint.id, skip);
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('切换跳过健康检测状态失败:', err);
      setActionError(`操作失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  useEffect(() => {
    loadGroups();
  }, [loadGroups]);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  // 定时获取挂起请求数量（页面加载时立即获取，之后每2秒轮询）
  useEffect(() => {
    loadSuspendedCount();
    const interval = setInterval(loadSuspendedCount, 2000);
    return () => clearInterval(interval);
  }, [loadSuspendedCount]);

  useEffect(() => {
    loadChannelsMeta();
  }, [loadChannelsMeta]);

  useEffect(() => {
    if (showCreateChannel) {
      setCreateChannelError('');
    }
  }, [showCreateChannel]);

  useEffect(() => {
    if (showEditChannel) {
      setEditChannelError('');
    }
  }, [showEditChannel]);

  // SQLite 模式下监听 Wails 事件，实时刷新端点数据
  const isSqliteModeRef = useRef(false);
  useEffect(() => {
    // 只检查 storageType
    isSqliteModeRef.current = storageStatus?.storageType === 'sqlite';
  }, [storageStatus]);

  useEffect(() => {
    if (!isWailsEnvironment()) return;

    // 订阅端点更新事件
    const unsubscribe = subscribeToEvent('endpoint:update', () => {
      // 只在 SQLite 模式下刷新数据
      if (isSqliteModeRef.current) {
        console.log('📡 [Channels] 收到端点更新事件，刷新 SQLite 数据');
        loadConfig();
        loadStorageStatus();
        loadGroups();
        loadChannelsMeta();
      }
    });

    return () => {
      if (typeof unsubscribe === 'function') {
        unsubscribe();
      }
    };
  }, [loadChannelsMeta, loadConfig, loadGroups, loadStorageStatus]);

  const handleRefresh = async () => {
    setBatchCheckLoading(true);
    try {
      await performBatchHealthCheckAll();
      if (isSqliteMode) {
        await loadStorageStatus();
      } else {
        await refresh();
      }
      await Promise.all([loadGroups(), loadChannelsMeta()]);
    } catch (err) {
      console.error('批量健康检测失败:', err);
      setActionError(`批量健康检测失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setBatchCheckLoading(false);
    }
  };

  // 获取要显示的端点列表
  const displayEndpoints = isSqliteMode ? storageEndpoints : endpoints;

  // 渠道卡片“显示全部/收起”：只允许展开一个渠道，避免同一行两列联动展开导致误触
  const [expandedChannelName, setExpandedChannelName] = useState('');

  // 点击渠道卡片外部区域时自动收起（排除弹窗区域）
  useEffect(() => {
    if (!expandedChannelName) return;
    if (typeof document === 'undefined') return;

    const onPointerDown = (event) => {
      const target = event?.target;

      // 如果点击在弹窗内（固定定位的遮罩层或其内部），不收缩渠道
      const inModal = target?.closest?.('.fixed.inset-0');
      if (inModal) return;

      const card = target?.closest?.('[data-channel-card]');
      const clickedChannelName = card?.getAttribute?.('data-channel-card') || '';
      if (clickedChannelName !== expandedChannelName) {
        setExpandedChannelName('');
      }
    };

    document.addEventListener('pointerdown', onPointerDown, true);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true);
    };
  }, [expandedChannelName]);

  const activeChannel = useMemo(() => {
    const activeGroup = groups.find(g => g.active);
    if (activeGroup?.name) return activeGroup.name;

    if (isSqliteMode) {
      return storageEndpoints.find(e => e.enabled)?.channel || '';
    }

    // 兜底：从端点数据推断（避免 groups 加载失败时 UI 空白）
    const inferred = displayEndpoints.find(e => e.group_is_active)?.group
      || displayEndpoints.find(e => e.group_is_active)?.channel
      || '';
    return inferred;
  }, [displayEndpoints, groups, isSqliteMode, storageEndpoints]);

  const channelOptions = useMemo(() => {
    const ordered = [];
    const seen = new Set();
    const add = (name) => {
      if (!name) return;
      if (seen.has(name)) return;
      seen.add(name);
      ordered.push(name);
    };
    channelsMeta.forEach((c) => add(c?.name || ''));
    displayEndpoints.forEach((e) => add(e.group || e.channel || ''));
    return ordered;
  }, [channelsMeta, displayEndpoints]);

  const credentialGroupsByChannel = useMemo(() => {
    const map = {};
    channelsMeta.forEach((channel) => {
      const name = channel?.name || '';
      if (name) {
        map[name] = getClaudeCredentialGroups(channel);
      }
    });
    return map;
  }, [channelsMeta]);
	const selectedCredentialGroupDetail = credentialGroupDetailTarget ? {
	  channelName: credentialGroupDetailTarget.channelName,
	  group: (credentialGroupsByChannel[credentialGroupDetailTarget.channelName] || []).find(
		(group) => Number(group.id) === Number(credentialGroupDetailTarget.group?.id)
	  ) || credentialGroupDetailTarget.group
	} : null;
  const detailChannelName = detailTarget?.channel || detailTarget?.group || '';
  const detailChannelMeta = channelsMeta.find((item) => item?.name === detailChannelName) || null;
  const detailCredentialGroup = getCredentialGroupForEndpoint(
    credentialGroupsByChannel[detailChannelName] || [],
    detailTarget
  );

  const groupInfoMap = useMemo(() => {
    const map = new Map();
    groups.forEach(g => {
      if (g?.name) map.set(g.name, g);
    });
    return map;
  }, [groups]);

  const isEndpointInHealthScope = useCallback((ep) => {
    const channel = ep?.group || ep?.channel || ep?.name || 'default';
    const endpointFailoverEnabled = normalizeBool(ep?.failoverEnabled, true);
    if (!endpointFailoverEnabled) return false;

    if (channelFailoverEnabled) {
      const gi = groupInfoMap.get(channel);
      return gi?.paused ? false : true;
    }

    return channel === activeChannel;
  }, [activeChannel, channelFailoverEnabled, groupInfoMap]);

  const scopedDisplayEndpoints = useMemo(() => {
    return (displayEndpoints || []).map((ep) => ({
      ...ep,
      suspendedCount: Number.isFinite(Number(suspendedEndpointCounts?.[buildSuspendedEndpointKey(ep)] ?? 0))
        ? Number(suspendedEndpointCounts?.[buildSuspendedEndpointKey(ep)] ?? 0)
        : 0,
      _health_scope: isEndpointInHealthScope(ep),
      _channel_paused: Boolean(groupInfoMap.get(ep?.group || ep?.channel || ep?.name || 'default')?.paused)
    }));
  }, [displayEndpoints, groupInfoMap, isEndpointInHealthScope, suspendedEndpointCounts]);

  const channelSections = useMemo(() => {
    const getChannelKey = (ep) => ep.group || ep.channel || ep.name || 'default';
    const map = new Map(channelOptions.map((name) => [name, []]));
    scopedDisplayEndpoints.forEach((ep) => {
      const key = getChannelKey(ep);
      if (!map.has(key)) map.set(key, []);
      map.get(key).push(ep);
    });

    const metaMap = new Map();
    channelsMeta.forEach((c) => {
      if (c?.name) metaMap.set(c.name, c);
    });

    const normalizePriority = (p) => {
      const parsed = Number(p);
      return Number.isFinite(parsed) && parsed >= 0 ? parsed : 999;
    };
    const compareDescString = (a, b) => (b || '').localeCompare(a || '');

    const sections = Array.from(map.entries()).map(([name, eps]) => {
      const meta = metaMap.get(name) || null;
      const gi = groupInfoMap.get(name) || null;
      const computedPriority = eps.length > 0 ? Math.min(...eps.map(e => normalizePriority(e.priority))) : 999;
      const sortPriority = normalizePriority(meta?.priority ?? gi?.priority ?? computedPriority);
      const sortCreatedAt = meta?.createdAt || '';

      const sortedEndpoints = [...eps].sort((a, b) => {
        const pa = normalizePriority(a?.priority);
        const pb = normalizePriority(b?.priority);
        if (pa !== pb) return pa - pb;
        const ca = a?.createdAt || '';
        const cb = b?.createdAt || '';
        const byCreated = compareDescString(ca, cb);
        if (byCreated !== 0) return byCreated;
        return (a?.name || '').localeCompare(b?.name || '');
      });

      return {
        name,
        website: meta?.website || '',
        channelPriority: meta?.priority ?? null,
        endpoints: sortedEndpoints,
        groupInfo: gi,
        sortPriority,
        sortCreatedAt
      };
    });

    return sections.sort((a, b) => {
      if (activeChannel) {
        const aIsActive = a.name === activeChannel;
        const bIsActive = b.name === activeChannel;
        if (aIsActive && !bIsActive) return -1;
        if (bIsActive && !aIsActive) return 1;
      }
      if (a.sortPriority !== b.sortPriority) return a.sortPriority - b.sortPriority;
      const byCreated = compareDescString(a.sortCreatedAt, b.sortCreatedAt);
      if (byCreated !== 0) return byCreated;
      return (a.name || '').localeCompare(b.name || '');
    });
  }, [activeChannel, channelOptions, channelsMeta, groupInfoMap, scopedDisplayEndpoints]);

  const headerLastUpdate = isSqliteMode ? channelsLastUpdate : lastUpdate;

  const credentialGroupEndpointCopyOptions = useMemo(() => {
    if (credentialGroupFormMode !== 'create' || !credentialGroupFormChannel) return [];
    const sourceGroups = credentialGroupsByChannel[credentialGroupFormChannel] || [];
    const sourceEndpoints = channelSections.find((section) => section.name === credentialGroupFormChannel)?.endpoints || [];
    return buildClaudeEndpointCopyOptions(sourceGroups, sourceEndpoints);
  }, [channelSections, credentialGroupFormChannel, credentialGroupFormMode, credentialGroupsByChannel]);

  // 渠道列表变化时重置展开状态，避免展开指向已不存在的渠道
  useEffect(() => {
    setExpandedChannelName('');
  }, [channelSections.length]);

  // 计算统计数据
  // 冷却机制已移除
  const isSkippedEndpoint = (e) => e?.skipHealthCheck || e?.skip_health_check;
  const hasLastCheck = (e) => !!(e?.lastCheck || e?.last_check);
  const isNeverCheckedEndpoint = (e) => e?.never_checked || e?.neverChecked || !hasLastCheck(e);

  const participatingChannelCount = useMemo(() => {
    return channelSections.filter(section => {
      const gi = groupInfoMap.get(section.name);
      return !gi?.paused;
    }).length;
  }, [channelSections, groupInfoMap]);

  // 检测范围内的端点数
  const scopedEndpointCount = useMemo(() => {
    return scopedDisplayEndpoints.filter(e => e._health_scope).length;
  }, [scopedDisplayEndpoints]);

  const displayStats = isSqliteMode
    ? {
        total: scopedDisplayEndpoints.length,
        skipped: scopedDisplayEndpoints.filter(e => e._health_scope && isSkippedEndpoint(e)).length,
        // 健康端点包含跳过检测的端点（与概览页面一致）
        healthy: scopedDisplayEndpoints.filter(e => e._health_scope && (isSkippedEndpoint(e) || (e.healthy && !isNeverCheckedEndpoint(e)))).length,
        unhealthy: scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && !e.healthy && hasLastCheck(e) && !isNeverCheckedEndpoint(e)).length,
        unchecked: scopedDisplayEndpoints.filter(e => !e._health_scope || (!isSkippedEndpoint(e) && isNeverCheckedEndpoint(e))).length,
        healthPercentage: (scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && !isNeverCheckedEndpoint(e)).length) > 0
          ? ((scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && e.healthy && !isNeverCheckedEndpoint(e)).length /
              scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && !isNeverCheckedEndpoint(e)).length) * 100).toFixed(1)
          : 0
      }
    : { ...stats, skipped: stats.skipped || 0 };
  const activeChannelSection = channelSections.find((section) => section.name === activeChannel) || null;
  const activeChannelEndpointCount = activeChannelSection?.endpoints?.length || 0;
  const activeChannelCredentialGroupCount = (credentialGroupsByChannel[activeChannel] || []).length;
  const failoverOverviewCards = [
    {
      title: '参与端点',
      value: String(scopedEndpointCount),
      tooltip: String(scopedEndpointCount),
      subText: `端点总数 ${scopedDisplayEndpoints.length} 个`,
      icon: Server,
      statusColor: 'bg-indigo-50 text-indigo-600'
    },
    {
      title: '健康端点',
      value: String(displayStats.healthy),
      tooltip: String(displayStats.healthy),
      subText: '当前通过健康检测',
      icon: CheckCircle2,
      statusColor: 'bg-emerald-50 text-emerald-600'
    },
    {
      title: '跳过检测',
      value: String(displayStats.skipped || 0),
      tooltip: String(displayStats.skipped || 0),
      subText: '已标记跳过健康检查',
      icon: Pause,
      statusColor: 'bg-violet-50 text-violet-600'
    },
    {
      title: '异常端点',
      value: String(displayStats.unhealthy),
      tooltip: String(displayStats.unhealthy),
      subText: '当前判定异常',
      icon: XCircle,
      statusColor: 'bg-rose-50 text-rose-600'
    },
    {
      title: '未参与端点',
      value: String(displayStats.unchecked),
      tooltip: String(displayStats.unchecked),
      subText: '未纳入当前检测范围',
      icon: ShieldOff,
      statusColor: 'bg-slate-100 text-slate-500'
    },
    {
      title: '参与渠道',
      value: String(participatingChannelCount),
      tooltip: String(participatingChannelCount),
      subText: `总渠道 ${channelSections.length} 个`,
      icon: ArrowRightLeft,
      statusColor: 'bg-slate-100 text-slate-700'
    }
  ];
  const activeChannelOverviewCards = [
    {
      title: '凭证分组',
      value: String(activeChannelCredentialGroupCount),
      tooltip: String(activeChannelCredentialGroupCount),
      subText: activeChannel ? `当前渠道 ${activeChannel}` : '暂无活跃渠道',
      icon: KeyRound,
      statusColor: 'bg-sky-50 text-sky-600'
    },
    {
      title: '参与端点',
      value: String(scopedEndpointCount),
      tooltip: String(scopedEndpointCount),
      subText: '当前活跃渠道参与路由',
      icon: Server,
      statusColor: 'bg-indigo-50 text-indigo-600'
    },
    {
      title: '健康端点',
      value: String(displayStats.healthy),
      tooltip: String(displayStats.healthy),
      subText: '当前活跃渠道通过检测',
      icon: CheckCircle2,
      statusColor: 'bg-emerald-50 text-emerald-600'
    },
    {
      title: '跳过检测',
      value: String(displayStats.skipped || 0),
      tooltip: String(displayStats.skipped || 0),
      subText: '当前活跃渠道跳过检测',
      icon: Pause,
      statusColor: 'bg-violet-50 text-violet-600'
    },
    {
      title: '异常端点',
      value: String(displayStats.unhealthy),
      tooltip: String(displayStats.unhealthy),
      subText: '当前活跃渠道判定异常',
      icon: XCircle,
      statusColor: 'bg-rose-50 text-rose-600'
    },
    {
      title: '总端点',
      value: String(activeChannelEndpointCount),
      tooltip: String(activeChannelEndpointCount),
      subText: '当前活跃渠道全部端点',
      icon: Database,
      statusColor: 'bg-slate-100 text-slate-700'
    }
  ];
  const channelOverviewCards = channelFailoverEnabled ? failoverOverviewCards : activeChannelOverviewCards;

  // ============================================
  // CRUD 操作处理
  // ============================================

  const openCreateEndpointForm = useCallback((channelName, credentialGroup = null) => {
    const name = String(channelName || '').trim();
    if (!name || !hasClaudeCredential(credentialGroup)) return false;
    setEditingEndpoint(null);
    setDefaultChannel(name);
    setDefaultCredentialGroupId(Number(credentialGroup?.id || 0) || 0);
    setLockChannel(true);
    setShowForm(true);
    return true;
  }, []);

  const handleCreateChannel = useCallback(async (payload) => {
    let channelCreated = false;
    try {
      setChannelFormLoading(true);
      setCreateChannelError('');

      await createChannel(payload);
      channelCreated = true;

      const refreshedChannels = await loadChannelsMeta({ throwOnError: true });
      const createdChannel = refreshedChannels.find((channel) => channel?.name === payload.name) || null;
      if (!createdChannel) {
        throw new Error('渠道已创建，但刷新后未找到该渠道，请刷新列表后继续配置');
      }
      setShowCreateChannel(false);
      const defaultCredentialGroup = getDefaultClaudeCredentialGroup(createdChannel);
      setInitialSetupChannelName(createdChannel.name);
      setCredentialGroupFormMode(defaultCredentialGroup ? 'edit' : 'create');
      setCredentialGroupFormChannel(createdChannel.name);
      setEditingCredentialGroup(defaultCredentialGroup);
      setCredentialGroupFormError('');
      setShowCredentialGroupForm(true);
    } catch (err) {
      console.error('创建渠道失败:', err);
      const message = getErrorMessage(err, '创建渠道失败');
      if (channelCreated) {
        setShowCreateChannel(false);
        setCreateChannelError('');
        setActionError(`渠道已创建，但刷新列表失败，请关闭此提示后点击页面刷新，再继续配置凭证分组和端点。\n${message}`);
      } else {
        setCreateChannelError(message);
      }
    } finally {
      setChannelFormLoading(false);
    }
  }, [loadChannelsMeta]);

  const handleOpenEditChannel = useCallback((channelName) => {
    const name = (channelName || '').trim();
    if (!name) return;
    const meta = channelsMeta.find((c) => c?.name === name) || null;
    setEditingChannel({
      name,
      website: meta?.website || '',
      priority: meta?.priority ?? 0,
      modelMapping: meta?.modelMapping || {}
    });
    setShowEditChannel(true);
  }, [channelsMeta]);

  const handleUpdateChannel = useCallback(async (payload) => {
    try {
      setChannelFormLoading(true);
      setEditChannelError('');

      await updateChannel(payload);

      setShowEditChannel(false);
      setEditingChannel(null);
      await loadChannelsMeta();
      await loadGroups();
    } catch (err) {
      console.error('更新渠道失败:', err);
      setEditChannelError(getErrorMessage(err, '更新渠道失败'));
    } finally {
      setChannelFormLoading(false);
    }
  }, [loadChannelsMeta, loadGroups]);

  const openCreateCredentialGroupForm = useCallback((channelName) => {
    const name = String(channelName || '').trim();
    if (!name) return;
    setInitialSetupChannelName('');
    setCredentialGroupFormMode('create');
    setCredentialGroupFormChannel(name);
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  }, []);

  const openEditCredentialGroupForm = useCallback((channelName, group) => {
    const name = String(channelName || group?.channel || '').trim();
    if (!name || !group) return;
    setInitialSetupChannelName('');
    setCredentialGroupFormMode('edit');
    setCredentialGroupFormChannel(name);
    setEditingCredentialGroup(group);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  }, []);

  const closeCredentialGroupForm = useCallback(() => {
    setShowCredentialGroupForm(false);
    setCredentialGroupFormChannel('');
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
    setInitialSetupChannelName('');
  }, []);

  const handleSaveCredentialGroup = useCallback(async (payload) => {
    const { copyEndpoints = [], ...groupPayload } = payload || {};
    const shouldContinueInitialSetup = initialSetupChannelName === credentialGroupFormChannel;
    const targetChannelName = credentialGroupFormChannel;
    let savedGroup = null;
    try {
      setCredentialGroupFormLoading(true);
      setCredentialGroupFormError('');
      if (credentialGroupFormMode === 'edit' && editingCredentialGroup?.id) {
        savedGroup = await updateClaudeCredentialGroupByID(editingCredentialGroup.id, groupPayload);
      } else {
        savedGroup = await createClaudeCredentialGroup({
          ...groupPayload,
          channel: credentialGroupFormChannel
        });
        const targetGroupID = Number(savedGroup?.id || 0);
        if (targetGroupID > 0) {
          for (const endpoint of copyEndpoints) {
            await createEndpointRecord(buildClaudeEndpointCopyPayload(endpoint, credentialGroupFormChannel, targetGroupID));
          }
        }
      }
    } catch (err) {
      console.error('保存凭证分组失败:', err);
      setCredentialGroupFormError(getErrorMessage(err, '保存凭证分组失败'));
      return;
    } finally {
      setCredentialGroupFormLoading(false);
    }

    closeCredentialGroupForm();
    if (shouldContinueInitialSetup && savedGroup?.id && copyEndpoints.length === 0) {
      openCreateEndpointForm(targetChannelName, savedGroup);
    }

    try {
      await loadStorageStatus({ throwOnError: true });
      await loadChannelsMeta({ throwOnError: true });
      await loadGroups({ throwOnError: true });
    } catch (err) {
      console.error('刷新凭证分组列表失败:', err);
      setActionError(`凭证分组已保存，但列表刷新失败，请点击页面刷新查看最新数据。\n${getErrorMessage(err, '未知错误')}`);
    }
  }, [
    closeCredentialGroupForm,
    credentialGroupFormChannel,
    credentialGroupFormMode,
    editingCredentialGroup,
    initialSetupChannelName,
    loadChannelsMeta,
    loadGroups,
    loadStorageStatus,
    openCreateEndpointForm
  ]);

  const handleSetDefaultCredentialGroup = useCallback(async (_channelName, group) => {
    if (!group?.id) return;
    try {
      setChannelActionLoading(true);
      await setDefaultClaudeCredentialGroup(group.id);
      await loadStorageStatus();
      await loadChannelsMeta();
      await loadGroups();
    } catch (err) {
      console.error('设置默认凭证分组失败:', err);
      setActionError(`设置默认分组失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadChannelsMeta, loadGroups, loadStorageStatus]);

  const handleDeleteCredentialGroup = useCallback((channelName, group) => {
    if (!group?.id) return;
    setDeleteCredentialGroupTarget({
      channelName,
      group
    });
  }, []);

  const handleConfirmDeleteCredentialGroup = useCallback(async () => {
    const target = deleteCredentialGroupTarget;
    if (!target?.group?.id) return;
    try {
      setDeleteCredentialGroupLoading(true);
      await deleteClaudeCredentialGroupByID(target.group.id);
      setDeleteCredentialGroupTarget(null);
      await loadStorageStatus();
      await loadChannelsMeta();
      await loadGroups();
    } catch (err) {
      console.error('删除凭证分组失败:', err);
      setActionError(`删除凭证分组失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setDeleteCredentialGroupLoading(false);
    }
  }, [deleteCredentialGroupTarget, loadChannelsMeta, loadGroups, loadStorageStatus]);

  // 编辑端点
  const handleEdit = (endpoint) => {
    setEditingEndpoint(endpoint);
    setShowForm(true);
  };

  // 删除端点
  const handleDelete = (endpoint) => {
    setDeleteTarget(endpoint);
  };

  const handleDeleteChannel = useCallback((channelName) => {
    const section = channelSections.find((s) => s.name === channelName);
    setDeleteChannelTarget({
      name: channelName,
      endpointCount: section?.endpoints?.length || 0
    });
  }, [channelSections]);

  // 保存端点
  const handleSave = async (formData) => {
    setFormLoading(true);
    try {
      if (editingEndpoint) {
        // 编辑模式
        await updateEndpointRecordByID(editingEndpoint.id, formData);
      } else {
        // 新建模式
        await createEndpointRecord(formData);
      }
      setShowForm(false);
      setEditingEndpoint(null);
      setDefaultChannel('');
      setDefaultCredentialGroupId(0);
      setLockChannel(false);
      // 刷新列表
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('保存失败:', err);
      throw err;
    } finally {
      setFormLoading(false);
    }
  };

  // 确认删除
  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;

    setDeleteLoading(true);
    try {
      await deleteEndpointRecordByID(deleteTarget.id);
      setDeleteTarget(null);
      // 刷新列表
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('删除失败:', err);
      setActionError(`删除失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setDeleteLoading(false);
    }
  };

  // 错误状态
  if (error && !isSqliteMode) {
    return (
      <ErrorMessage
        title="端点数据加载失败"
        message={error}
        onRetry={refresh}
      />
    );
  }

  // 加载状态
  if (!storageStatus) {
    return <LoadingSpinner text="加载渠道数据..." />;
  }

  if ((storageLoading || (!isSqliteMode && loading)) && channelSections.length === 0) {
    return <LoadingSpinner text="加载渠道数据..." />;
  }

  return (
      <div className="animate-fade-in">
      {/* 页面标题 */}
      <div className="flex justify-between items-end mb-8">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-slate-900 rounded-lg text-white shadow-lg">
            <ClaudeIcon className="w-6 h-6" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-900">Claude 管理</h1>
            <p className="text-slate-500 text-sm mt-1">
              以渠道为单位进行路由与故障转移，渠道内优先在端点之间切换，渠道耗尽后跨渠道切换
              {headerLastUpdate && (
                <span className="ml-2 text-slate-400">· 更新于 {headerLastUpdate}</span>
              )}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {/* 刷新按钮 */}
          <Button
            variant="ghost"
            size="sm"
            icon={RefreshCw}
            onClick={handleRefresh}
            loading={loading || batchCheckLoading}
          >
            刷新
          </Button>

          {/* 新建渠道按钮 (SQLite 模式) */}
          {isSqliteMode && (
            <Button
              icon={Plus}
              onClick={() => {
                setShowCreateChannel(true);
              }}
            >
              添加渠道
            </Button>
          )}
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-6">
        {channelOverviewCards.map((card) => (
          <KPICard
            key={card.title}
            title={card.title}
            value={card.value}
            tooltip={card.tooltip}
            subText={card.subText}
            icon={card.icon}
            statusColor={card.statusColor}
            layout="side"
            size={card.size || 'lg'}
            wrapTitle={card.wrapTitle}
            titleClassName={card.titleClassName}
            iconSize={card.iconSize}
            iconContainerClassName={card.iconContainerClassName}
          />
        ))}
      </div>

      {channelFailoverEnabled && manualRetryRequestsCount > 0 && (
        <div className="mb-6 p-4 bg-amber-50/70 border border-amber-200 rounded-xl flex items-start gap-3">
          <div className="p-2 bg-amber-100 rounded-full flex-shrink-0 animate-pulse">
            <RefreshCw className="w-4 h-4 text-amber-600" />
          </div>
          <div className="flex-1">
            <div className="font-semibold text-amber-900">
              已达到最大循环轮数，等待用户操作 ({manualRetryRequestsCount})
            </div>
            <div className="text-sm text-amber-700 mt-1">
              请在参与失败的渠道上点击「重试」以重新开始转移链路。
            </div>
          </div>
        </div>
      )}
      {channelFailoverEnabled && manualRetryRequestsCount === 0 && suspendedRequestsCount > 0 && (
        <div className="mb-6 p-4 bg-purple-50/70 border border-purple-200 rounded-xl flex items-start gap-3">
          <div className="p-2 bg-purple-100 rounded-full flex-shrink-0 animate-pulse">
            <Clock className="w-4 h-4 text-purple-600" />
          </div>
          <div className="flex-1">
            <div className="font-semibold text-purple-900">
              {suspendedRequestsCount} 个请求正在等待端点恢复
            </div>
            <div className="text-sm text-purple-700 mt-1">
              请求已挂起等待端点恢复或自动切换渠道。
            </div>
          </div>
        </div>
      )}

      {/* 全部端点失败警示 - 仅在启用渠道间故障转移时显示全局警示；未启用时在渠道卡片内显示 */}
      {channelFailoverEnabled && participatingChannelCount > 0 && scopedEndpointCount > 0 && displayStats.healthy === 0 && (
        <div className="mb-6 p-4 bg-rose-50 border border-rose-300 rounded-xl flex items-start gap-3">
          <div className="p-2 bg-rose-100 rounded-full flex-shrink-0">
            <AlertTriangle className="w-5 h-5 text-rose-600" />
          </div>
          <div className="flex-1">
            <div className="font-semibold text-rose-900">
              所有端点均处于不可用状态
            </div>
            <div className="text-sm text-rose-700 mt-1">
              当前 {participatingChannelCount} 个参与渠道共 {scopedEndpointCount} 个端点全部异常，请求将根据恢复策略处理（快速失败/循环重试/智能等待）。
            </div>
            <div className="flex items-center gap-3 mt-3 text-[11px]">
              <span className="text-slate-500">建议操作：</span>
              <span className="px-2 py-0.5 bg-white border border-rose-200 rounded text-rose-600">
                检查端点配置和网络连接
              </span>
              <span className="text-slate-400">或</span>
              <span className="px-2 py-0.5 bg-white border border-indigo-200 rounded text-indigo-600">
                恢复已暂停的渠道
              </span>
              <span className="text-slate-400">或</span>
              <span className="px-2 py-0.5 bg-white border border-emerald-200 rounded text-emerald-600">
                添加新的端点/渠道
              </span>
            </div>
          </div>
        </div>
      )}

      {/* 渠道分块列表 */}
      {channelSections.length === 0 ? (
        <div className="bg-white rounded-2xl border border-slate-200/60 shadow-sm p-10 text-center text-slate-500">
          {isSqliteMode ? (
            <div className="flex flex-col items-center gap-3">
              <Database size={40} className="text-slate-300" />
              <p>暂无渠道配置</p>
              <Button
                icon={Plus}
                onClick={() => {
                  setShowCreateChannel(true);
                }}
              >
                添加第一个渠道
              </Button>
            </div>
          ) : (
            '暂无端点数据'
          )}
        </div>
       ) : (
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {channelSections.map((section) => (
            <ChannelCard
              key={section.name}
              channelName={section.name}
              endpoints={section.endpoints}
              channelWebsite={section.website}
              channelPriority={section.channelPriority}
              groupInfo={section.groupInfo}
              activeChannelName={activeChannel}
              isSqliteMode={isSqliteMode}
              channelFailoverEnabled={channelFailoverEnabled}
              suspendedRequestsCount={suspendedRequestsCount}
              manualRetryCount={manualRetryChannelCounts?.[section.name] ?? 0}
              credentialGroups={credentialGroupsByChannel[section.name] || []}
              expanded={expandedChannelName === section.name}
              onToggleExpanded={() => {
                setExpandedChannelName((prev) => (prev === section.name ? '' : section.name));
              }}
              loading={channelActionLoading}
              onOpenEndpoint={openEndpointDetail}
              onActivateEndpoint={isSqliteMode ? handleActivateEndpoint : undefined}
              onToggleEndpointFailover={isSqliteMode ? handleToggleEndpointFailover : undefined}
              onSetGroupEndpointFailover={isSqliteMode ? handleSetGroupEndpointFailover : undefined}
              onSetGroupEndpointSkipHealthCheck={isSqliteMode ? handleSetGroupEndpointSkipHealthCheck : undefined}
              onToggleEndpointSkipHealthCheck={isSqliteMode ? handleToggleEndpointSkipHealthCheck : undefined}
              onActivate={async (channelName) => {
                if (channelFailoverEnabled && section.groupInfo?.paused) {
                  setActionError(`激活失败：渠道「${channelName}」已暂停，无法激活。\n请先点击「恢复」后再激活该渠道。`);
                  return;
                }
                try {
                  setChannelActionLoading(true);
                  await activateGroup(channelName);
                  if (isSqliteMode) {
                    await loadStorageStatus();
                  } else {
                    await refresh();
                  }
                  await loadGroups();
                } catch (err) {
                  console.error('激活渠道失败:', err);
                  setActionError(`激活失败: ${getErrorMessage(err, '未知错误')}`);
                } finally {
                  setChannelActionLoading(false);
                }
              }}
              onPause={async (channelName) => {
                if (channelFailoverEnabled && section.groupInfo?.active) {
                  try {
                    const next = await getNextAvailableChannel(channelName);
                    if (!next) {
                      setActionError(`暂停失败：渠道「${channelName}」为当前活跃渠道，但没有可切换的渠道。\n请检查是否其他渠道已全部暂停或无可用端点。`);
                      return;
                    }
                    setPauseActiveChannelTarget({ name: channelName, next });
                    return;
                  } catch (err) {
                    console.error('获取下一个可用渠道失败:', err);
                    setActionError(`暂停失败: ${getErrorMessage(err, '无法获取可切换渠道')}`);
                    return;
                  }
                }
                try {
                  setChannelActionLoading(true);
                  await pauseGroup(channelName);
                  await loadGroups();
                } catch (err) {
                  console.error('暂停渠道失败:', err);
                  setActionError(`暂停失败: ${getErrorMessage(err, '未知错误')}`);
                } finally {
                  setChannelActionLoading(false);
                }
              }}
              onResume={async (channelName) => {
                try {
                  setChannelActionLoading(true);
                  await resumeGroup(channelName);
                  await loadGroups();
                } catch (err) {
                  console.error('恢复渠道失败:', err);
                  setActionError(`恢复失败: ${getErrorMessage(err, '未知错误')}`);
                } finally {
                  setChannelActionLoading(false);
                }
              }}
              onAddCredentialGroup={openCreateCredentialGroupForm}
			  onOpenCredentialGroup={(channelName, group) => setCredentialGroupDetailTarget({ channelName, group })}
              onEditCredentialGroup={openEditCredentialGroupForm}
              onDeleteCredentialGroup={handleDeleteCredentialGroup}
              onSetDefaultCredentialGroup={handleSetDefaultCredentialGroup}
              onAddEndpoint={openCreateEndpointForm}
              onEditChannel={handleOpenEditChannel}
              onDeleteChannel={handleDeleteChannel}
              onEditEndpoint={(ep) => {
                closeEndpointDetail();
                setDefaultChannel('');
                setDefaultCredentialGroupId(0);
                setLockChannel(false);
                handleEdit(ep);
              }}
              onDeleteEndpoint={(ep) => {
                closeEndpointDetail();
                handleDelete(ep);
              }}
            />
          ))}
        </div>
      )}

      {/* 端点表单弹窗 */}
      {showForm && (
        <EndpointForm
          endpoint={editingEndpoint}
          channels={channelOptions}
          existingEndpoints={isSqliteMode ? storageEndpoints : []}
          credentialGroupsByChannel={credentialGroupsByChannel}
          defaultChannel={defaultChannel}
          defaultCredentialGroupId={defaultCredentialGroupId}
          lockChannel={lockChannel}
          onSave={handleSave}
          onCancel={() => {
            setShowForm(false);
            setEditingEndpoint(null);
            setDefaultChannel('');
            setDefaultCredentialGroupId(0);
            setLockChannel(false);
          }}
          loading={formLoading}
        />
      )}

      {showCreateChannel && isSqliteMode && (
        <CreateChannelModal
          open
          loading={channelFormLoading}
          serverError={createChannelError}
          channelFailoverEnabled={channelFailoverEnabled}
          onClose={() => setShowCreateChannel(false)}
          onSubmit={handleCreateChannel}
        />
      )}

      {showEditChannel && isSqliteMode && (
        <CreateChannelModal
          open
          loading={channelFormLoading}
          serverError={editChannelError}
          mode="edit"
          initialValue={editingChannel}
          channelFailoverEnabled={channelFailoverEnabled}
          onClose={() => {
            setShowEditChannel(false);
            setEditingChannel(null);
          }}
          onSubmit={handleUpdateChannel}
        />
      )}

      {showCredentialGroupForm && isSqliteMode && (
        <ClaudeCredentialGroupFormModal
          open
          mode={credentialGroupFormMode}
          channelName={credentialGroupFormChannel}
          group={editingCredentialGroup}
          continueToEndpoint={initialSetupChannelName === credentialGroupFormChannel}
          loading={credentialGroupFormLoading}
          serverError={credentialGroupFormError}
          endpointCopyOptions={credentialGroupEndpointCopyOptions}
          onClose={closeCredentialGroupForm}
          onSubmit={handleSaveCredentialGroup}
        />
      )}

	  {selectedCredentialGroupDetail?.group && (
		<CredentialGroupDetailModal
		  provider="claude"
		  channelName={selectedCredentialGroupDetail.channelName}
		  group={selectedCredentialGroupDetail.group}
		  onClose={() => setCredentialGroupDetailTarget(null)}
		  onEdit={(group) => {
			setCredentialGroupDetailTarget(null);
			openEditCredentialGroupForm(selectedCredentialGroupDetail.channelName, group);
		  }}
		/>
	  )}

      {/* 删除确认弹窗 */}
      {deleteTarget && (
        <DeleteConfirmDialog
          endpoint={deleteTarget}
          onConfirm={handleConfirmDelete}
          onCancel={() => setDeleteTarget(null)}
          loading={deleteLoading}
        />
      )}

      {deleteCredentialGroupTarget && (
        <DeleteCredentialGroupConfirmDialog
          channelName={deleteCredentialGroupTarget.channelName}
          group={deleteCredentialGroupTarget.group}
          loading={deleteCredentialGroupLoading}
          onCancel={() => setDeleteCredentialGroupTarget(null)}
          onConfirm={handleConfirmDeleteCredentialGroup}
        />
      )}

      {deleteChannelTarget && (
        <DeleteChannelConfirmDialog
          channelName={deleteChannelTarget.name}
          endpointCount={deleteChannelTarget.endpointCount}
          loading={deleteChannelLoading}
          onCancel={() => setDeleteChannelTarget(null)}
          onConfirm={async () => {
            try {
              setDeleteChannelLoading(true);
              await deleteChannel(deleteChannelTarget.name, true);
              setDeleteChannelTarget(null);
              await loadStorageStatus();
              await loadGroups();
              await loadChannelsMeta();
            } catch (err) {
              console.error('删除渠道失败:', err);
              setActionError(`删除渠道失败: ${getErrorMessage(err, '未知错误')}`);
            } finally {
              setDeleteChannelLoading(false);
            }
          }}
        />
      )}

      {pauseActiveChannelTarget?.name && (
        <PauseActiveChannelConfirmDialog
          channelName={pauseActiveChannelTarget.name}
          nextChannel={pauseActiveChannelTarget.next}
          loading={channelActionLoading}
          onCancel={() => setPauseActiveChannelTarget(null)}
          onConfirm={async () => {
            const name = pauseActiveChannelTarget.name;
            setPauseActiveChannelTarget(null);
            try {
              setChannelActionLoading(true);
              await pauseGroup(name);
              if (isSqliteMode) {
                await loadStorageStatus();
              } else {
                await refresh();
              }
              await loadGroups();
            } catch (err) {
              console.error('暂停活跃渠道失败:', err);
              setActionError(`暂停失败: ${getErrorMessage(err, '未知错误')}`);
            } finally {
              setChannelActionLoading(false);
            }
          }}
        />
      )}

      {actionError && renderModalPortal(
        <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
          <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
            <div className="flex items-center gap-3 mb-4">
              <div className="p-3 bg-rose-100 rounded-full">
                <AlertTriangle className="text-rose-600" size={24} />
              </div>
              <div>
                <h3 className="text-lg font-semibold text-slate-900">操作失败</h3>
                <p className="text-sm text-slate-500">请检查配置或稍后重试</p>
              </div>
            </div>
            <p className="text-slate-700 mb-4 whitespace-pre-wrap">{actionError}</p>
            <div className="flex justify-end gap-3">
              <Button variant="ghost" onClick={() => setActionError('')}>
                关闭
              </Button>
            </div>
          </div>
        </div>
      )}

      <EndpointDetailModal
        endpoint={detailTarget}
        credentialGroup={detailCredentialGroup}
        isOpen={detailOpen}
        isSqliteMode={isSqliteMode}
        channelFailoverEnabled={channelFailoverEnabled}
        globalTimeoutSeconds={globalTimeoutSeconds}
        channelMeta={detailChannelMeta}
        onClose={closeEndpointDetail}
        onEdit={(ep) => {
          closeEndpointDetail();
          setDefaultChannel('');
          setDefaultCredentialGroupId(0);
          setLockChannel(false);
          handleEdit(ep);
        }}
        onDelete={(ep) => {
          closeEndpointDetail();
          handleDelete(ep);
        }}
      />
    </div>
  );
};

export default ChannelsPage;
