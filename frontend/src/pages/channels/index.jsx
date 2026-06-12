// ============================================
// Channels 页面 - 渠道管理（渠道内端点故障转移）
// 2025-11-28 (Updated 2025-12-06 for SQLite Storage)
// ============================================

import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import {
  Activity,
  RefreshCw,
  Plus,
  Pencil,
  Trash2,
  Database,
  AlertTriangle,
  Server,
  Copy,
  ArrowRight,
  ArrowRightLeft,
  Calculator,
  ShieldCheck,
  ShieldOff,
  CheckCircle2,
  XCircle,
  Clock,
  ChevronDown,
  Pause,
  Play,
  Power,
  Globe,
  Settings
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
  DeleteConfirmDialog,
  PriorityBadge
} from './components';
import ModelTag from '../requests/components/ModelTag.jsx';
import {
  getEndpointStorageStatus,
  getEndpointRecords,
  createEndpointRecord,
  updateEndpointRecordByID,
  deleteEndpointRecordByID,
  toggleEndpointRecordByID,
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
  isWailsEnvironment,
  openExternalURL,
  subscribeToEvent,
  getSuspendedRequestsCount,
  getSuspendedEndpointCounts,
  getManualRetryRequestsCount,
  getManualRetryChannelCounts
} from '@utils/wailsApi.js';
import { formatMultiplierExpression, parseMultiplierExpression } from '@utils/multiplier.js';

const buildInitialModelMappingRows = (modelMapping) => {
  const entries = Object.entries(modelMapping || {})
    .map(([sourceModel, targetModel]) => ({
      sourceModel: String(sourceModel || ''),
      targetModel: String(targetModel || '')
    }))
    .filter((row) => row.sourceModel.trim() || row.targetModel.trim());

  return entries.length > 0 ? entries : [{ sourceModel: '', targetModel: '' }];
};

const buildModelMappingPayload = (rows) => {
  return (rows || []).reduce((mapping, row) => {
    const sourceModel = String(row?.sourceModel || '').trim();
    const targetModel = String(row?.targetModel || '').trim();
    if (sourceModel && targetModel) {
      mapping[sourceModel] = targetModel;
    }
    return mapping;
  }, {});
};

const CHANNEL_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'mapping', label: '模型映射' }
];

const getErrorMessage = (error, fallback = '操作失败') => {
  if (!error) return fallback;
  if (typeof error === 'string') return error || fallback;
  if (error instanceof Error) return error.message || fallback;
  if (typeof error?.message === 'string' && error.message) return error.message;
  try {
    return JSON.stringify(error);
  } catch {
    return String(error);
  }
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

const DeactivateChannelConfirmDialog = ({ channelName, onConfirm, onCancel, loading }) => {
  const confirmDisabled = loading;

  return renderModalPortal(
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-3 bg-amber-100 rounded-full">
            <AlertTriangle className="text-amber-700" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认停用渠道</h3>
            <p className="text-sm text-slate-500">停用后将没有激活渠道，请谨慎操作</p>
          </div>
        </div>

        <p className="text-slate-700 mb-4">
          确定要停用渠道 <span className="font-semibold">“{channelName}”</span> 吗？
          停用后将没有激活渠道，所有请求会失败直到再次激活。
        </p>

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            variant="danger"
            icon={Power}
            onClick={() => onConfirm?.()}
            loading={loading}
            disabled={confirmDisabled}
          >
            确认停用
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

// ============================================
// 端点表格行组件 (增强版 - 参考 test.jsx 设计)
// ============================================

// 健康状态徽章（健康/异常/未检测/跳过检测）
const HealthBadge = ({ healthy, neverChecked, skipped, detail = false }) => {
  const baseClass = `inline-flex ${detail ? 'h-6' : 'h-5'} min-w-[64px] flex-shrink-0 items-center justify-center rounded-full border px-1.5 text-[10px] font-medium leading-none whitespace-nowrap`;

  if (skipped) {
    return (
      <div className={`${baseClass} bg-purple-50 text-purple-600 border-purple-200`}>
        <ShieldOff size={10} className="mr-1" />
        跳过检测
      </div>
    );
  }
  if (neverChecked) {
    return (
      <div className={`${baseClass} bg-slate-50 text-slate-400 border-slate-200`}>
        <Clock size={10} className="mr-1" />
        未检测
      </div>
    );
  }

  return healthy ? (
    <div className={`${baseClass} bg-emerald-50 text-emerald-600 border-emerald-100`}>
      <CheckCircle2 size={10} className="mr-1" />
      健康
    </div>
  ) : (
    <div className={`${baseClass} bg-rose-50 text-rose-600 border-rose-100`}>
      <XCircle size={10} className="mr-1" />
      异常
    </div>
  );
};

// 请求失败徽章（与健康异常区分）
const RequestFailedBadge = ({ failCode, failReason, detail = false }) => {
  const hasFailCode = Number.isFinite(Number(failCode)) && Number(failCode) > 0;
  const hasFailReason = !!failReason;
  const tooltipParts = [];
  if (hasFailCode) tooltipParts.push(`状态码: ${failCode}`);
  if (hasFailReason) tooltipParts.push(`原因: ${failReason}`);
  const tooltip = tooltipParts.length > 0 ? tooltipParts.join('\n') : '请求失败';

  return (
    <Tooltip content={tooltip}>
      <div className={`inline-flex ${detail ? 'h-6' : 'h-5'} min-w-[72px] flex-shrink-0 items-center justify-center rounded-full border border-orange-200 bg-orange-50 px-1.5 text-[10px] font-medium leading-none text-orange-600 whitespace-nowrap`}>
        <XCircle size={10} className="mr-1" />
        请求失败
        {hasFailCode && (
          <span className="ml-1 font-mono">({failCode})</span>
        )}
      </div>
    </Tooltip>
  );
};

// 延迟指示器
const LatencyBadge = ({ ms, inScope = true, detail = false }) => {
  if (!inScope) {
    return null;
  }

  const baseClass = `inline-flex ${detail ? 'h-6' : 'h-5'} min-w-[54px] flex-shrink-0 items-center justify-center rounded-full border px-1.5 font-mono text-[10px] font-medium leading-none whitespace-nowrap`;

  const msValue = Number(ms);
  if (!Number.isFinite(msValue) || msValue <= 0) {
    return (
      <span className={`${baseClass} bg-slate-50 text-slate-300 border-slate-200`}>
        -
      </span>
    );
  }

  const msInt = Math.max(1, Math.round(msValue));

  let colorClass = 'text-emerald-600 bg-emerald-50 border-emerald-100';
  if (msInt > 500) colorClass = 'text-amber-600 bg-amber-50 border-amber-100';
  if (msInt > 1000) colorClass = 'text-rose-600 bg-rose-50 border-rose-100';

  return (
    <span className={`${baseClass} ${colorClass}`}>
      {msInt}ms
    </span>
  );
};

const formatLastCheck = (time) => {
  if (!time || time === '-') return '-';
  try {
    const date = new Date(time);
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    });
  } catch {
    return String(time);
  }
};

const getAuthType = (endpoint) => {
  if (!endpoint) return null;
  if (endpoint.token || endpoint.tokenMasked) return 'Token';
  if (endpoint.apiKey || endpoint.apiKeyMasked) return 'API Key';
  return null;
};

const normalizeBool = (value, defaultValue = true) => {
  if (value === undefined || value === null) return defaultValue;
  if (value === false || value === 0 || value === '0') return false;
  return Boolean(value);
};

const buildSuspendedEndpointKey = (endpoint) => {
  const channel = endpoint?.channel || '';
  const name = endpoint?.name || '';
  if (!channel) return name || '';
  if (!name) return channel;
  return `${channel}::${name}`;
};

// ============================================
// 端点精简行（对齐 Codex 管理页的端点列表密度）
// ============================================

const EndpointMiniCard = ({
  endpoint,
  isActiveChannel,
  isSqliteMode,
  channelFailoverEnabled = true,
  onOpen,
  onToggleFailover,
  onToggleSkipHealthCheck, // 跳过健康检测回调
  onEdit,
  onDelete
}) => {
  if (!endpoint) return null;

  const inScope = endpoint._health_scope !== false;
  const channelPaused = endpoint._channel_paused === true;
  const showPaused = channelFailoverEnabled && channelPaused;
  const responseTime = endpoint.response_time || endpoint.responseTimeMs || 0;
  const isNeverChecked = endpoint.never_checked || endpoint.neverChecked || (!endpoint.lastCheck && !endpoint.last_check);
  const isSkipHealthCheck = endpoint.skipHealthCheck || endpoint.skip_health_check;
  const healthState = endpoint.health_state || endpoint.healthState || '';
  const lastFailCode = endpoint.last_fail_code || endpoint.lastFailCode || 0;
  const lastFailReason = endpoint.last_fail_reason || endpoint.lastFailReason || '';
  const isRequestFailed = healthState === 'request_failed';
  const showHealthBadge = inScope && (isSkipHealthCheck || !isRequestFailed);

  const authType = getAuthType(endpoint);
  const failoverEnabled = normalizeBool(endpoint.failoverEnabled, true);
  const multiplier = endpoint.costMultiplier || 1.0;
  const isChannelActive = typeof isActiveChannel === 'boolean'
    ? isActiveChannel
    : Boolean(endpoint.group_is_active || endpoint.groupIsActive);
  const endpointEnabled = isSqliteMode ? normalizeBool(endpoint.enabled, true) : isChannelActive;
  const rowActive = failoverEnabled && isChannelActive && (!isSqliteMode || endpointEnabled);
  const statusBadgeText = !failoverEnabled
    ? '未启用'
    : (showPaused
        ? '暂停'
        : (!isChannelActive
        ? (channelFailoverEnabled ? '等待' : '未启用')
        : (!endpointEnabled ? '未启用' : '')));
  const statusBadgeTitle = !failoverEnabled
    ? '该端点不参与渠道内故障转移，因此不会被使用/检测'
    : (showPaused
        ? '渠道已暂停：不参与健康检查与故障转移'
        : (!isChannelActive
            ? (channelFailoverEnabled ? '备用渠道端点：当前渠道非活跃' : '未启用渠道端点：渠道间故障转移未启用')
            : (!endpointEnabled ? '端点未启用' : '')));

  return (
    <div className="border-b border-slate-100 last:border-b-0">
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
        className={`group/row relative flex min-h-[72px] items-center gap-3 px-4 py-2.5 text-left transition-colors duration-150 ${
          rowActive ? 'bg-indigo-50/20' : 'hover:bg-slate-50/80 opacity-85'
        }`}
      >
        <div className="flex w-8 flex-shrink-0 items-center justify-start">
          <PriorityBadge priority={endpoint.priority || 1} size="xs" className="flex-shrink-0" />
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5 overflow-hidden">
            <span className="block min-w-0 max-w-[180px] flex-shrink truncate text-xs font-semibold text-slate-900">
              {endpoint.name}
            </span>
            <LatencyBadge ms={responseTime} inScope={inScope} />
            {statusBadgeText && (
              <Tooltip content={statusBadgeTitle} disabled={!statusBadgeTitle}>
                <span className="inline-flex h-5 min-w-[54px] flex-shrink-0 items-center justify-center rounded-full border border-slate-200 bg-slate-50 px-1.5 text-[10px] font-medium leading-none text-slate-500 whitespace-nowrap">
                  {statusBadgeText}
                </span>
              </Tooltip>
            )}
            {showHealthBadge && (
              <HealthBadge healthy={endpoint.healthy} neverChecked={isNeverChecked} skipped={isSkipHealthCheck} />
            )}
            {isRequestFailed && (
              <RequestFailedBadge failCode={lastFailCode} failReason={lastFailReason} />
            )}
          </div>

          <div className="mt-1 flex min-w-0 items-center gap-1.5">
            <Tooltip content={endpoint.url || '未设置端点地址'}>
              <span
                className="inline-flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-md text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
                onClick={(e) => e.stopPropagation()}
              >
                <Globe size={11} />
              </span>
            </Tooltip>
            {authType && (
              <span className="inline-flex h-5 min-w-[54px] flex-shrink-0 items-center justify-center rounded-full border border-slate-200 bg-slate-50 px-1.5 text-[10px] font-medium leading-none text-slate-500">
                <ShieldCheck size={10} className="mr-1 text-amber-500" />
                {authType}
              </span>
            )}
            {multiplier && multiplier !== 1.0 && (
              <span className="inline-flex h-5 min-w-[42px] flex-shrink-0 items-center justify-center rounded-full border border-orange-100 bg-orange-50 px-1.5 font-mono text-[10px] font-medium leading-none text-orange-600">
                {multiplier}x
              </span>
            )}
          </div>
        </div>

        <div className="flex w-20 flex-shrink-0 items-center justify-end gap-0.5">
          {isSqliteMode && (
            <>
              <Tooltip content={failoverEnabled ? '点击：不参与渠道内故障转移' : '点击：参与渠道内故障转移'}>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onToggleFailover?.(endpoint, !failoverEnabled);
                  }}
                  className={`rounded-md p-1 transition-colors duration-150 ${
                    failoverEnabled
                      ? 'text-indigo-600 hover:bg-indigo-50'
                      : 'text-slate-400 hover:bg-slate-100'
                  }`}
                  aria-label={failoverEnabled ? '点击：不参与渠道内故障转移' : '点击：参与渠道内故障转移'}
                  aria-pressed={failoverEnabled}
                >
                  <ArrowRightLeft size={12} />
                </button>
              </Tooltip>
              <Tooltip content={isSkipHealthCheck ? '已跳过健康检测 (点击关闭)' : '跳过健康检测 (点击开启)'}>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onToggleSkipHealthCheck?.(endpoint, !isSkipHealthCheck);
                  }}
                  className={`rounded-md p-1 transition-colors duration-150 ${
                    isSkipHealthCheck
                      ? 'bg-purple-50 text-purple-600 hover:bg-purple-100'
                      : 'text-slate-400 hover:bg-slate-100 hover:text-purple-600'
                  }`}
                  aria-label={isSkipHealthCheck ? '已跳过健康检测 (点击关闭)' : '跳过健康检测 (点击开启)'}
                  aria-pressed={isSkipHealthCheck}
                >
                  <ShieldOff size={12} />
                </button>
              </Tooltip>
              <Tooltip content="编辑端点">
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
              </Tooltip>
              <Tooltip content="删除端点">
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
              </Tooltip>
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

const DETAIL_CHIP_TONES = {
  slate: 'border-slate-200 bg-slate-50 text-slate-500',
  emerald: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  amber: 'border-amber-200 bg-amber-50 text-amber-700',
  rose: 'border-rose-200 bg-rose-50 text-rose-700',
  purple: 'border-purple-200 bg-purple-50 text-purple-700',
  indigo: 'border-indigo-200 bg-indigo-50 text-indigo-700',
  sky: 'border-sky-200 bg-sky-50 text-sky-700'
};

const DetailMetricCard = ({ label, icon: Icon, wide = false, children }) => (
  <div
    className={`min-w-0 rounded-xl border border-slate-200/70 bg-slate-50 p-3.5 ${
      wide ? 'md:col-span-2' : ''
    }`}
  >
    <div className="mb-2 flex items-center gap-2">
      {Icon && (
        <span className="inline-flex h-6 w-6 items-center justify-center rounded-lg bg-slate-100 text-slate-500">
          <Icon size={13} />
        </span>
      )}
      <span className="text-xs font-medium text-slate-500">{label}</span>
    </div>
    <div className="min-w-0">{children}</div>
  </div>
);

const DetailChip = ({ children, tone = 'slate', mono = false }) => (
  <span
    className={`inline-flex min-h-7 items-center justify-center rounded-lg border px-2.5 text-xs font-semibold leading-none ${
      mono ? 'font-mono' : ''
    } ${DETAIL_CHIP_TONES[tone] || DETAIL_CHIP_TONES.slate}`}
  >
    {children}
  </span>
);

const DetailPill = ({ children, icon: Icon, tone = 'slate', mono = false }) => (
  <span
    className={`inline-flex h-6 max-w-full items-center justify-center gap-1 rounded-full border px-1.5 text-[10px] font-medium leading-none whitespace-nowrap align-middle ${
      mono ? 'font-mono' : ''
    } ${DETAIL_CHIP_TONES[tone] || DETAIL_CHIP_TONES.slate}`}
  >
    {Icon && <Icon size={10} className="shrink-0" />}
    <span className="inline-flex min-w-0 items-center justify-center gap-1 leading-none">
      {children}
    </span>
  </span>
);

const getDetailTimestamp = (...values) => {
  for (const value of values) {
    const text = String(value ?? '').trim();
    if (text && text !== '-' && !text.startsWith('0001-01-01')) {
      return text;
    }
  }
  return '-';
};

const DetailTimeoutPill = ({ custom, value, tooltip }) => (
  <Tooltip content={tooltip}>
    <span className="inline-flex">
      <DetailPill tone={custom ? 'amber' : 'sky'} icon={Clock}>
        <span className="leading-none">{custom ? '超时' : '默认'}</span>
        <span className="font-mono leading-none">{value}</span>
      </DetailPill>
    </span>
  </Tooltip>
);

const MODEL_MAPPING_TAG_CLASS = 'max-w-full [&>span]:whitespace-normal [&>span]:break-all';

const ModelMappingPair = ({ sourceModel, targetModel }) => (
  <div className="flex min-h-7 min-w-0 flex-wrap items-center justify-start gap-2 py-0.5">
    <ModelTag model={sourceModel} className={MODEL_MAPPING_TAG_CLASS} />
    <ArrowRight size={14} className="shrink-0 text-slate-400" />
    <ModelTag model={targetModel} className={MODEL_MAPPING_TAG_CLASS} />
  </div>
);

const EndpointDetailModal = ({
  endpoint,
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
  const hasFailDetail = (Number(lastFailCode) > 0) || !!lastFailReason;

  const failoverEnabled = normalizeBool(endpoint.failoverEnabled, true);
  const supportsCountTokens = !!endpoint.supportsCountTokens;
  const priority = Number(endpoint.priority ?? 0);
  const timeoutSeconds = Number(endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0);
  const hasCustomTimeout = Number.isFinite(timeoutSeconds) && timeoutSeconds > 0 && timeoutSeconds !== 300;
  const effectiveGlobalTimeoutSeconds = Number(globalTimeoutSeconds) || 300;
  const timeoutLabel = hasCustomTimeout ? `${timeoutSeconds}s` : `${effectiveGlobalTimeoutSeconds}s`;
  const timeoutScope = hasCustomTimeout ? '端点覆盖' : '使用全局超时';
  const channelModelMapping = channelMeta?.modelMapping || channelMeta?.model_mapping || {};
  const modelMappingEntries = Object.entries(channelModelMapping)
    .map(([sourceModel, targetModel]) => [String(sourceModel || '').trim(), String(targetModel || '').trim()])
    .filter(([sourceModel, targetModel]) => sourceModel && targetModel);
  const modelMultiplierRules = endpoint.modelMultipliers || endpoint.model_multipliers || [];
  const normalizeDetailMultiplier = (value) => {
    const parsed = typeof value === 'string'
      ? parseMultiplierExpression(value, Number.NaN)
      : Number(value);
    return Number.isFinite(parsed) ? parsed : 1;
  };
  const isDefaultDetailMultiplier = (value) => Math.abs(normalizeDetailMultiplier(value) - 1) <= 0.000001;
  const formatDetailMultiplier = (value) => `${formatMultiplierExpression(normalizeDetailMultiplier(value), 3)}x`;
  const multiplierItems = [
    { label: '总', value: endpoint.costMultiplier ?? endpoint.cost_multiplier ?? 1, tone: 'indigo' },
    { label: '输入', value: endpoint.inputCostMultiplier ?? endpoint.input_cost_multiplier ?? 1, tone: 'slate' },
    { label: '输出', value: endpoint.outputCostMultiplier ?? endpoint.output_cost_multiplier ?? 1, tone: 'sky' },
    { label: '缓存读取', value: endpoint.cacheReadCostMultiplier ?? endpoint.cache_read_cost_multiplier ?? 1, tone: 'amber' },
    { label: '5分钟写入', value: endpoint.cacheCreationCostMultiplier ?? endpoint.cache_creation_cost_multiplier ?? 1, tone: 'emerald' },
    { label: '1小时写入', value: endpoint.cacheCreationCostMultiplier1h ?? endpoint.cache_creation_cost_multiplier_1h ?? 1, tone: 'emerald' }
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

  const tokenRaw = endpoint.token || '';
  const apiKeyRaw = endpoint.apiKey || endpoint.api_key || '';
  const tokenMasked = endpoint.tokenMasked || endpoint.token_masked || '';
  const apiKeyMasked = endpoint.apiKeyMasked || endpoint.api_key_masked || '';
  const hasToken = !!(tokenRaw || tokenMasked);
  const hasApiKey = !!(apiKeyRaw || apiKeyMasked);
  const maskSecret = (secret) => {
    if (!secret) return '';
    const s = String(secret);
    if (s.length <= 8) return '********';
    return `${s.slice(0, 6)}...${s.slice(-4)}`;
  };

  const rows = [
    {
      label: '渠道',
      icon: Server,
      content: <span className="text-sm font-semibold text-slate-900">{channel}</span>
    },
    {
      label: 'Token（脱敏）',
      icon: ShieldCheck,
      content: (
        <div className="flex min-w-0 items-center justify-between gap-3">
          <span className="min-w-0 break-all font-mono text-sm font-semibold text-slate-900">
            {hasToken ? (tokenMasked || maskSecret(tokenRaw)) : '-'}
          </span>
          <Tooltip content={tokenRaw ? '复制原始 Token' : '无原始 Token（仅 SQLite 记录可复制）'}>
            <button
              onClick={() => {
                if (tokenRaw) {
                  navigator.clipboard.writeText(tokenRaw);
                }
              }}
              disabled={!tokenRaw}
              className={`inline-flex flex-shrink-0 items-center justify-center rounded-md p-1 transition-colors ${
                tokenRaw
                  ? 'text-slate-400 hover:bg-white/70 hover:text-indigo-600'
                  : 'cursor-not-allowed text-slate-300'
              }`}
              aria-label={tokenRaw ? '复制原始 Token' : '无原始 Token（仅 SQLite 记录可复制）'}
            >
              <Copy size={12} />
            </button>
          </Tooltip>
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
            {isRequestFailed && (
              <RequestFailedBadge failCode={lastFailCode} failReason={lastFailReason} detail />
            )}
            <LatencyBadge ms={responseTime} inScope={inScope} detail />

            <DetailPill tone="indigo" icon={Settings} mono>
              P{priority}
            </DetailPill>

            <DetailTimeoutPill
              custom={hasCustomTimeout}
              value={timeoutLabel}
              tooltip={`${timeoutScope}: ${timeoutLabel}`}
            />

            {!hasCustomCostMultiplier && (
              <Tooltip content="未配置自定义倍率，使用默认 1x">
                <span className="inline-flex">
                  <DetailPill tone="slate" icon={Database}>
                    <span className="leading-none">默认倍率</span>
                    <span className="font-mono leading-none">1x</span>
                  </DetailPill>
                </span>
              </Tooltip>
            )}

            <Tooltip content={failoverEnabled ? '参与渠道内故障转移' : '不参与渠道内故障转移'}>
              <span className="inline-flex">
                <DetailPill tone={failoverEnabled ? 'emerald' : 'slate'} icon={ArrowRightLeft}>
                  {failoverEnabled ? '渠道内故障转移' : '不参与转移'}
                </DetailPill>
              </span>
            </Tooltip>

            <Tooltip content={supportsCountTokens ? '支持 count_tokens' : '不支持 count_tokens'}>
              <span className="inline-flex">
                <DetailPill tone={supportsCountTokens ? 'purple' : 'slate'} icon={Calculator}>
                  {supportsCountTokens ? 'count_tokens' : '无 count_tokens'}
                </DetailPill>
              </span>
            </Tooltip>

            <span className="flex-shrink-0 whitespace-nowrap font-mono text-[10px] text-slate-400">
              最后检查 {lastCheck}
            </span>
          </div>
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
          </div>

          {(hasCustomCostMultiplier || hasModelMapping) && (
            <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
              {hasCustomCostMultiplier && (
                <DetailMetricCard label="成本倍率" icon={Database} wide>
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
              )}

              {hasModelMapping && (
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
              )}
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

          {/* 异常详情显示 - 有错误信息时显示 */}
          {!isSuspended && hasFailDetail && (
            <div className="mt-4">
              <div className="bg-rose-50 rounded-xl p-4 border border-rose-200">
                <div className="flex items-center gap-2 mb-2">
                  <XCircle size={16} className="text-rose-600" />
                  <div className="text-sm font-medium text-rose-800">异常详情</div>
                </div>
                <div className="space-y-2 text-sm">
                  {lastFailCode > 0 && (
                    <div className="flex items-start gap-2">
                      <span className="text-rose-600 font-medium min-w-[70px]">状态码:</span>
                      <span className="font-mono text-rose-900 bg-rose-100 px-2 py-0.5 rounded">
                        {lastFailCode}
                      </span>
                    </div>
                  )}
                  {lastFailReason && (
                    <div className="flex items-start gap-2">
                      <span className="text-rose-600 font-medium min-w-[70px]">失败原因:</span>
                      <span className="text-rose-900 break-all">
                        {lastFailReason}
                      </span>
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {hasApiKey && (
            <div className="mt-3 bg-slate-50 rounded-xl p-3 border border-slate-200/60">
              <div className="flex items-center justify-between mb-1">
                <div className="text-xs text-slate-500">API Key（脱敏）</div>
                <Tooltip content={apiKeyRaw ? '复制原始 API Key' : '无原始 API Key（仅 SQLite 记录可复制）'}>
                  <button
                    onClick={() => {
                      if (apiKeyRaw) {
                        navigator.clipboard.writeText(apiKeyRaw);
                      }
                    }}
                    disabled={!apiKeyRaw}
                    className={`inline-flex items-center justify-center p-1 rounded-md transition-colors ${
                      apiKeyRaw
                        ? 'text-slate-400 hover:text-indigo-600 hover:bg-white/70'
                        : 'text-slate-300 cursor-not-allowed'
                    }`}
                    aria-label={apiKeyRaw ? '复制原始 API Key' : '无原始 API Key（仅 SQLite 记录可复制）'}
                  >
                    <Copy size={12} />
                  </button>
                </Tooltip>
              </div>
              <div className="text-sm font-mono text-slate-900 break-all">
                {apiKeyMasked || maskSecret(apiKeyRaw)}
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

          <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-2">
            <DetailMetricCard label="创建时间" icon={Clock}>
              <span className="block truncate font-mono text-sm font-medium text-slate-700">
                {createdAt}
              </span>
            </DetailMetricCard>
            <DetailMetricCard label="更新时间" icon={RefreshCw}>
              <span className="block truncate font-mono text-sm font-medium text-slate-700">
                {updatedAt}
              </span>
            </DetailMetricCard>
          </div>
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
  initialValue = null
}) => {
  const isEdit = mode === 'edit';
  const [name, setName] = useState(() => (isEdit ? (initialValue?.name || '') : ''));
  const [website, setWebsite] = useState(() => (isEdit ? (initialValue?.website || '') : ''));
  // 新建时传 0，让后端自动递增；编辑时使用原有优先级
  const [priority, setPriority] = useState(() => String(isEdit ? (initialValue?.priority || 1) : 0));
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
              {isEdit ? '修改渠道官网与优先级' : '先创建渠道，再在渠道卡片里添加端点'}
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
            setError('');
            onSubmit?.({
              name: trimmedName,
              website: website.trim(),
              priority,
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
            <div className={isEdit ? 'grid grid-cols-1 sm:grid-cols-3 gap-4' : ''}>
              <div className={isEdit ? 'sm:col-span-2' : ''}>
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

              {/* 优先级字段仅在编辑模式显示，新建时自动递增 */}
              {isEdit && (
                <div>
                  <label className="block text-sm font-medium text-slate-700">优先级</label>
                  <input
                    type="number"
                    min={1}
                    value={priority}
                    onChange={(e) => setPriority(e.target.value)}
                    autoComplete="off"
                    data-form-type="other"
                    className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                    disabled={loading}
                  />
                  <p className="text-xs text-slate-400 mt-1">数字越小越优先，相同则交换</p>
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
            <Button icon={CheckCircle2} type="submit" loading={loading}>
              {isEdit ? '保存修改' : '创建渠道'}
            </Button>
          </div>
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
  onDeactivate,
  onPause,
  onResume,
  onAddEndpoint,
  onEditChannel,
  onDeleteChannel,
  onOpenEndpoint,
  onToggleEndpointFailover,
  onToggleEndpointSkipHealthCheck, // 跳过健康检测回调
  onEditEndpoint,
  onDeleteEndpoint,
  manualRetryCount = 0,
  loading = false
}) => {
  const scopedEndpoints = endpoints.filter(e => e._health_scope !== false);
  // 跳过检测的端点数
  const scopedSkippedCount = scopedEndpoints.filter(e => e.skipHealthCheck || e.skip_health_check).length;
  // 健康统计：跳过检测的端点视为健康
  const scopedHealthyCount = scopedEndpoints.filter(e => e.healthy || e.skipHealthCheck || e.skip_health_check).length;
  const scopedTotalCount = scopedEndpoints.length;

  const totalCount = endpoints.length;
  const hasEndpoints = totalCount > 0;
  // 参与故障转移的端点数
  const failoverEnabledCount = endpoints.filter(e => {
    const isEnabled = isSqliteMode ? e.enabled : true;
    return isEnabled && normalizeBool(e.failoverEnabled, true);
  }).length;

  // 全部健康（含跳过检测视为健康）
  const allHealthy = hasEndpoints && scopedTotalCount > 0 && scopedHealthyCount === scopedTotalCount;
  const hasGroupInfo = !!groupInfo;
  const suspendedEndpointCount = endpoints.filter((endpoint) => {
    const count = endpoint?.suspendedCount || endpoint?.suspended_count || 0;
    return Number(count) > 0;
  }).length;
  const manualRetryCountValue = Number.isFinite(Number(manualRetryCount))
    ? Number(manualRetryCount)
    : 0;
  const hasManualRetry = manualRetryCountValue > 0;

  const isActive = groupInfo?.active
    ?? (isSqliteMode ? endpoints.some(e => e.enabled) : (activeChannelName === channelName));

  const isPaused = !!groupInfo?.paused;

  // 渠道是否参与路由：
  // - 仅渠道内转移时 → 只有活跃渠道参与
  // - 渠道间转移时 → 未暂停的渠道参与
  const channelParticipating = channelFailoverEnabled ? !isPaused : isActive;

  // 端点标签显示逻辑：
  // - 渠道不参与路由 → 仅显示总数（灰色）
  // - 渠道参与路由且全部端点开启转移 → 仅显示总数（绿色）
  // - 渠道参与路由但部分端点关闭转移 → 显示 N/M（灰色）
  const showEndpointRatio = channelParticipating && failoverEnabledCount !== totalCount;
  const endpointBadgeActive = channelParticipating && failoverEnabledCount === totalCount;

  const computedPriority = Math.min(...endpoints.map(e => e.priority || 999));
  const priority = Number.isFinite(channelPriority) && channelPriority > 0
    ? channelPriority
    : (groupInfo?.priority ?? (Number.isFinite(computedPriority) ? computedPriority : 999));

  // 判断是否处于"全部失败"警示状态（用于渠道卡片内的提示框）：渠道间故障转移关闭 + 活跃渠道 + 有端点 + 全部端点失败
  const isAllEndpointsFailed = !channelFailoverEnabled && isActive && hasEndpoints && scopedTotalCount > 0 && scopedHealthyCount === 0;
  const hasSuspendedRequests = suspendedRequestsCount > 0;
  const showSuspendedPrompt = !channelFailoverEnabled && isActive && hasSuspendedRequests;
  const showRetryButton = isAllEndpointsFailed || (isActive && hasSuspendedRequests) || hasManualRetry;
  const showRetryOnInactive = hasManualRetry;

  // 判断当前活跃渠道是否所有端点都失败（用于边框颜色，不受 channelFailoverEnabled 影响）
  const isActiveChannelAllFailed = isActive && hasEndpoints && scopedTotalCount > 0 && scopedHealthyCount === 0;
  const isActiveChannelInterrupted = isActive && (isActiveChannelAllFailed || hasManualRetry);
  const isActiveChannelSuspended = isActive && hasSuspendedRequests && !isActiveChannelInterrupted;

  // 活跃渠道边框样式
  // 优先级：彻底中断 > 挂起等待 > 正常活跃
  const activeHighlightClass = isActiveChannelInterrupted
    ? 'border-rose-300 ring-1 ring-rose-100'  // 全部失败 → 红色
    : isActiveChannelSuspended
      ? 'border-amber-300 ring-1 ring-amber-100'
      : 'border-indigo-300 ring-1 ring-indigo-100';  // Claude 正常活跃 → 与 Codex 区分

  // 默认折叠态：统一只展示 1 个端点，保证同行渠道卡片基础高度一致
  const collapsedVisibleCount = 1;
  const visibleEndpoints = expanded ? endpoints : endpoints.slice(0, collapsedVisibleCount);
  const hasMore = endpoints.length > collapsedVisibleCount;

  const priorityValue = Number.isFinite(priority) ? Math.max(1, Math.floor(priority)) : null;
  const hasValidPriority = priorityValue != null && priorityValue > 0 && priorityValue < 999;
  const priorityLabel = hasValidPriority ? `P${priorityValue}` : 'P-';
  const priorityTone = !hasValidPriority
    ? 'bg-slate-50 text-slate-500 border-slate-200'
    : priorityValue <= 1
      ? 'bg-emerald-50 text-emerald-700 border-emerald-200'
      : priorityValue <= 3
        ? 'bg-indigo-50 text-indigo-700 border-indigo-200'
        : priorityValue <= 10
          ? 'bg-slate-50 text-slate-700 border-slate-200'
          : 'bg-slate-50 text-slate-500 border-slate-200';

  // 健康标签颜色判断
  // - 无端点/不在检查范围 → 灰色
  // - 全部健康（无跳过贡献）→ 绿色
  // - 全部健康（有跳过贡献）→ 紫色
  // - 部分健康 → 橙色
  // - 全部失败 → 红色
  const healthTone = !hasEndpoints || scopedTotalCount === 0
    ? 'bg-slate-50 text-slate-500 border-slate-200'
    : scopedHealthyCount === scopedTotalCount
      ? scopedSkippedCount > 0
        ? 'bg-purple-50 text-purple-700 border-purple-200'  // 全健康但有跳过 → 紫色
        : 'bg-emerald-50 text-emerald-700 border-emerald-200'  // 全健康无跳过 → 绿色
      : scopedHealthyCount > 0
        ? 'bg-amber-50 text-amber-800 border-amber-200'  // 部分健康 → 橙色
        : 'bg-rose-50 text-rose-700 border-rose-200';  // 全部失败 → 红色

  return (
    <div className={`
      group flex flex-col self-start overflow-hidden rounded-xl border bg-white
      shadow-[0_1px_2px_rgba(15,23,42,0.04)] transition-all duration-150 ease-out hover:shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08)]
      ${isActive ? activeHighlightClass : 'border-slate-200/70 hover:border-slate-300'}
    `} data-channel-card={channelName}>
      {/* 渠道头部 */}
      <div className="px-4 py-3 border-b border-slate-100">
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 min-w-0">
              {channelWebsite && (
                <Tooltip content={channelWebsite}>
                  <button
                    type="button"
                    className="inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
                    aria-label="打开渠道官网"
                    onClick={() => openExternalURL(channelWebsite)}
                  >
                    <Globe size={14} />
                  </button>
                </Tooltip>
              )}
              <h2 className="truncate text-sm font-semibold tracking-tight text-slate-900 min-w-0">{channelName}</h2>
              <span className={`inline-flex h-5 min-w-8 flex-shrink-0 items-center justify-center rounded-full border px-1.5 text-[10px] font-semibold leading-none whitespace-nowrap ${priorityTone}`}>
                {priorityLabel}
              </span>
            </div>
          </div>

          {/* 渠道操作 */}
          <div className="flex items-center gap-0.5 flex-shrink-0">
          {/* 非激活渠道按钮逻辑
              - 开启渠道间转移 + 渠道已暂停：显示"激活"（仅激活，需先点恢复）
              - 其他情况：显示"激活"
          */}
          {!isActive && isSqliteMode && hasEndpoints && (
            <Tooltip content={
              channelFailoverEnabled && isPaused
                ? '请先点击"恢复"按钮取消暂停状态'
                : (showRetryOnInactive ? '重试此渠道' : '激活此渠道')
            }>
              <button
                type="button"
                onClick={() => onActivate?.(channelName)}
                disabled={loading || (channelFailoverEnabled && isPaused)}
                className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-indigo-600 transition-colors duration-150 hover:bg-indigo-50 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {showRetryOnInactive ? <RefreshCw size={12} /> : <Power size={12} />}
                {showRetryOnInactive ? '重试' : '激活'}
              </button>
            </Tooltip>
          )}
          {isActive && isSqliteMode && (
            showRetryButton ? (
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
            ) : (
              <Tooltip content="停用当前渠道">
                <button
                  type="button"
                  onClick={() => onDeactivate?.(channelName)}
                  disabled={loading}
                  className="inline-flex items-center gap-1 rounded-md border border-rose-200 bg-rose-50 px-2 py-1 text-[11px] font-medium text-rose-600 transition-colors duration-150 hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Power size={12} />
                  停用
                </button>
              </Tooltip>
            )
          )}
          {/* 暂停/恢复按钮 - 控制渠道是否参与渠道间转移 */}
          {channelFailoverEnabled && hasGroupInfo && !isPaused && (
            <Tooltip content="暂停此渠道，使其不参与渠道间转移">
              <button
                type="button"
                onClick={() => onPause?.(channelName)}
                disabled={loading}
                className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Pause size={12} />
                暂停
              </button>
            </Tooltip>
          )}
          {/* 恢复按钮仅在非活跃且已暂停时显示（活跃渠道一定是未暂停的） */}
          {channelFailoverEnabled && hasGroupInfo && isPaused && !isActive && (
            <Tooltip content="恢复此渠道，使其重新参与渠道间转移">
              <button
                type="button"
                onClick={() => onResume?.(channelName)}
                disabled={loading}
                className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Play size={12} />
                恢复
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

        <div className="mt-2 flex min-w-0 flex-wrap items-center gap-1.5 text-[11px] leading-4">
          {isActive ? (
            <span className={`inline-flex h-5 items-center rounded-full border px-2 text-[10px] font-medium leading-none whitespace-nowrap ${
              isActiveChannelInterrupted
                ? 'bg-rose-50 text-rose-600 border-rose-100'
                : isActiveChannelSuspended
                  ? 'bg-amber-50 text-amber-700 border-amber-200'
                  : 'bg-indigo-50 text-indigo-700 border-indigo-200'
            }`}>
              {isActiveChannelInterrupted ? '已中断' : isActiveChannelSuspended ? '挂起' : '活跃'}
            </span>
          ) : (
            <span className="inline-flex h-5 items-center rounded-full border border-slate-200 bg-slate-50 px-2 text-[10px] font-medium leading-none text-slate-500 whitespace-nowrap">
              备用
            </span>
          )}
          {isPaused && channelFailoverEnabled && (
            <span className="inline-flex h-5 items-center rounded-full border border-amber-200 bg-amber-50 px-2 text-[10px] font-medium leading-none text-amber-700 whitespace-nowrap">
              已暂停
            </span>
          )}
            {/* 冷却状态显示已移除 */}
          {isAllEndpointsFailed && (
            <span className="inline-flex h-5 items-center rounded-full border border-rose-200 bg-rose-50 px-2 text-[10px] font-medium leading-none text-rose-600 whitespace-nowrap">
              全部失败
            </span>
          )}
          {hasManualRetry && (
            <span className="inline-flex h-5 items-center gap-1 rounded-full border border-amber-200 bg-amber-50 px-2 text-[10px] font-medium leading-none text-amber-700 whitespace-nowrap">
              <RefreshCw size={10} />
              待重试 <span className="font-medium">{manualRetryCountValue}</span>
            </span>
          )}
          {!hasManualRetry && suspendedEndpointCount > 0 && (
            <span className="inline-flex h-5 items-center gap-1 rounded-full border border-amber-200 bg-amber-50 px-2 text-[10px] font-medium leading-none text-amber-700 whitespace-nowrap">
              <Clock size={10} />
              挂起 <span className="font-medium">{suspendedEndpointCount}</span>
            </span>
          )}

          <span className={`inline-flex h-5 items-center rounded-full border px-2 text-[10px] font-medium leading-none whitespace-nowrap ${
            endpointBadgeActive
              ? 'bg-emerald-50 text-emerald-700 border-emerald-200'
              : 'bg-slate-50 text-slate-600 border-slate-200'
          }`}>
            端点 {showEndpointRatio ? `${failoverEnabledCount}/${totalCount}` : totalCount}
          </span>
          <Tooltip
            content={hasEndpoints && scopedTotalCount === 0 ? '当前渠道不在健康检查范围（未参与故障转移/非活跃渠道）' : undefined}
            disabled={!(hasEndpoints && scopedTotalCount === 0)}
          >
            <span className={`inline-flex h-5 items-center gap-1 rounded-full border px-2 text-[10px] font-medium leading-none ${healthTone} whitespace-nowrap`}>
              {hasEndpoints && scopedTotalCount === 0 ? '检测' : '健康'}{' '}
              <span className="font-medium">
                {!hasEndpoints
                  ? '-'
                  : scopedTotalCount === 0
                    ? '关闭'
                    : allHealthy
                      ? ''
                      : `${scopedHealthyCount}/${scopedTotalCount}`
                }
              </span>
            </span>
          </Tooltip>
        </div>

        {/* 挂起等待提示（仅在活跃渠道显示） */}
        {showSuspendedPrompt && (
          <div className="mt-3 p-2.5 bg-purple-50/70 border border-purple-200 rounded-lg flex items-start gap-2">
            <div className="p-1.5 bg-purple-100 rounded-full flex-shrink-0 animate-pulse">
              <Clock className="w-3.5 h-3.5 text-purple-600" />
            </div>
            <div className="flex-1 text-xs text-purple-800">
              <div className="font-medium">
                {suspendedRequestsCount} 个请求正在等待端点恢复
              </div>
              <div className="text-[11px] text-purple-700 mt-0.5">
                请求已挂起，点击「重试」按钮重新尝试当前渠道或激活其他渠道。
              </div>
            </div>
          </div>
        )}

        {/* 全部失败时的操作提示 */}
        {isAllEndpointsFailed && (
          <div className="mt-3 p-2.5 bg-rose-50/50 border border-rose-100 rounded-lg">
            <p className="text-xs text-rose-600 mb-2">
              当前渠道所有端点均不可用，请求将快速失败
            </p>
            <div className="flex items-center gap-2 text-[11px]">
              <span className="text-slate-500">可选操作：</span>
              <span className="px-2 py-0.5 bg-white border border-rose-200 rounded text-rose-600">
                继续重试当前渠道
              </span>
              <span className="text-slate-400">或</span>
              <span className="px-2 py-0.5 bg-white border border-indigo-200 rounded text-indigo-600">
                手动切换其他渠道
              </span>
            </div>
          </div>
        )}
      </div>

      {/* 端点子列表 */}
      <div className="border-t border-slate-100 bg-slate-50/30">
        <div className="flex items-center justify-between px-4 py-1.5">
          <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-400">
            <span>Endpoints · {endpoints.length}</span>
            {hasMore && (
              <Tooltip content={expanded ? '收起端点列表' : '展开全部端点'}>
                <button
                  type="button"
                  onClick={() => onToggleExpanded?.()}
                  className="inline-flex items-center gap-0.5 rounded-md px-1 py-0.5 text-[10px] font-medium normal-case tracking-normal text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
                >
                  {expanded ? '收起' : `展开 ${endpoints.length - collapsedVisibleCount} 个`}
                  <ChevronDown
                    size={11}
                    className={`transition-transform duration-200 ease-out ${expanded ? 'rotate-180' : ''}`}
                  />
                </button>
              </Tooltip>
            )}
          </div>
          {isSqliteMode && (
            <button
              type="button"
              onClick={() => onAddEndpoint?.(channelName)}
              disabled={loading}
              className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Plus size={12} />
              添加端点
            </button>
          )}
        </div>

        {visibleEndpoints.length === 0 ? (
          <div className="border-t border-slate-100 bg-white px-4 py-6 text-center text-[11px] text-slate-400">
            暂无端点
          </div>
        ) : (
          <div className="border-t border-slate-100 bg-white">
            {visibleEndpoints.map((endpoint, index) => (
              <EndpointMiniCard
                key={endpoint.name || index}
                endpoint={endpoint}
                isActiveChannel={isActive}
                isSqliteMode={isSqliteMode}
                channelFailoverEnabled={channelFailoverEnabled}
                onOpen={onOpenEndpoint}
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

  // 判断存储模式（用于控制 Hook 行为）
  // 只检查 storageType，不再依赖 enabled（enabled 表示服务是否初始化，配置重载期间可能暂时为 false）
  const isSqliteMode = storageStatus?.storageType === 'sqlite';

  // 使用端点数据 Hook（YAML 模式需要；SQLite 模式仅保留操作能力，避免后台自动拉取/订阅导致卡住）
  const {
    endpoints,
    loading,
    error,
    stats,
    refresh,
    performBatchHealthCheckAll,
    sseConnectionStatus,
    lastUpdate
  } = useEndpointsData({ enabled: storageStatus ? !isSqliteMode : false });

  // 渠道（组）状态
  const [groups, setGroups] = useState([]);
  const [channelActionLoading, setChannelActionLoading] = useState(false);
  const [channelFailoverEnabled, setChannelFailoverEnabled] = useState(true);
  const [globalTimeoutSeconds, setGlobalTimeoutSeconds] = useState(300);
  // failoverDefaultCooldownSeconds 已移除
  const [channelsMeta, setChannelsMeta] = useState([]);

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
  const [lockChannel, setLockChannel] = useState(false);
  const [formLoading, setFormLoading] = useState(false);
  const [channelFormLoading, setChannelFormLoading] = useState(false);

  // 删除确认状态
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteChannelTarget, setDeleteChannelTarget] = useState(null);
  const [deleteChannelLoading, setDeleteChannelLoading] = useState(false);
  const [deactivateChannelTarget, setDeactivateChannelTarget] = useState(null);
  const [deactivateChannelLoading, setDeactivateChannelLoading] = useState(false);
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
  const loadStorageStatus = useCallback(async () => {
    const seq = ++storageLoadSeqRef.current;
    setStorageLoading(true);
    try {
      const status = await getEndpointStorageStatus();
      if (seq !== storageLoadSeqRef.current) return;
      setStorageStatus(status);

      // 如果是 SQLite 模式，加载存储的端点
      // 只检查 storageType，不再依赖 enabled
      if (status.storageType === 'sqlite') {
        const records = await getEndpointRecords();
        if (seq !== storageLoadSeqRef.current) return;
        setStorageEndpoints(records);
      }
    } catch (err) {
      console.error('获取存储状态失败:', err);
      if (seq !== storageLoadSeqRef.current) return;
      // 默认使用 YAML 模式
      setStorageStatus({ enabled: false, storageType: 'yaml' });
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
  const loadGroups = useCallback(async () => {
    const seq = ++groupsLoadSeqRef.current;
    try {
      const data = await getGroupsRaw();
      if (seq !== groupsLoadSeqRef.current) return;
      setGroups(Array.isArray(data) ? data : []);
    } catch (err) {
      console.error('获取渠道状态失败:', err);
      if (seq !== groupsLoadSeqRef.current) return;
      setGroups([]);
    }
  }, []);

  const loadChannelsMeta = useCallback(async () => {
    const seq = ++channelsMetaLoadSeqRef.current;
    // 只检查 storageType
    const sqliteEnabled = storageStatus?.storageType === 'sqlite';
    if (!sqliteEnabled) {
      if (seq === channelsMetaLoadSeqRef.current) {
        setChannelsMeta([]);
      }
      return;
    }
    try {
      const list = await getChannels();
      if (seq !== channelsMetaLoadSeqRef.current) return;
      setChannelsMeta(Array.isArray(list) ? list : []);
    } catch (err) {
      console.error('获取渠道列表失败:', err);
      // 避免瞬时失败导致 UI “清空”，保留上一次结果
    }
  }, [storageStatus]);

  const loadConfig = useCallback(async () => {
    const seq = ++configLoadSeqRef.current;
    try {
      const cfg = await getConfig();
      if (seq !== configLoadSeqRef.current) return;
      setChannelFailoverEnabled(cfg?.failover_enabled !== false);
      setGlobalTimeoutSeconds(Number(cfg?.global_timeout_seconds ?? cfg?.globalTimeoutSeconds ?? 300) || 300);
      // cooldownSeconds 加载已移除
    } catch (err) {
      console.error('获取配置失败:', err);
      if (seq !== configLoadSeqRef.current) return;
      setChannelFailoverEnabled(true);
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
      setActionError(`操作失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

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
  }, [loadChannelsMeta, loadGroups, loadStorageStatus]);

  // 批量健康检测处理
  const handleBatchHealthCheck = async () => {
    setBatchCheckLoading(true);
    try {
      await performBatchHealthCheckAll();
      // 刷新数据以获取最新的健康状态、响应时间等
      if (isSqliteMode) {
        await loadStorageStatus();
        await loadGroups();
      } else {
        await loadGroups();
      }
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

  // SQLite 模式下“enabled”语义为“激活渠道”，会同时启用该渠道下所有端点
  const activeChannel = useMemo(() => {
    if (isSqliteMode) {
      return storageEndpoints.find(e => e.enabled)?.channel || '';
    }
    const activeGroup = groups.find(g => g.active);
    if (activeGroup?.name) return activeGroup.name;
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

    const normalizePriority = (p) => (Number.isFinite(p) && p > 0 ? p : 999);
    const compareDescString = (a, b) => (b || '').localeCompare(a || '');

    const sections = Array.from(map.entries()).map(([name, eps]) => {
      const meta = metaMap.get(name) || null;
      const gi = groupInfoMap.get(name) || null;
      const computedPriority = eps.length > 0 ? Math.min(...eps.map(e => e.priority || 999)) : 999;
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

  // 渠道列表变化时重置展开状态，避免展开指向已不存在的渠道
  useEffect(() => {
    setExpandedChannelName('');
  }, [channelSections.length]);

  // 计算统计数据
  // 冷却机制已移除
  const isSkippedEndpoint = (e) => e?.skipHealthCheck || e?.skip_health_check;
  const hasLastCheck = (e) => !!(e?.lastCheck || e?.last_check);
  const isNeverCheckedEndpoint = (e) => e?.never_checked || e?.neverChecked || !hasLastCheck(e);

  // 计算检测范围内的渠道数
  const scopedChannelCount = useMemo(() => {
    if (!channelFailoverEnabled) {
      // 关闭渠道间转移：仅活跃渠道
      return activeChannel ? 1 : 0;
    }
    // 开启渠道间转移：参与的渠道数（非暂停）
    return channelSections.filter(section => {
      const gi = groupInfoMap.get(section.name);
      return !gi?.paused;
    }).length;
  }, [channelFailoverEnabled, activeChannel, channelSections, groupInfoMap]);

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
  const channelOverviewCards = [
    {
      title: channelFailoverEnabled ? '参与渠道' : '活跃渠道',
      value: String(scopedChannelCount),
      tooltip: String(scopedChannelCount),
      subText: `总渠道 ${channelSections.length} 个`,
      icon: ArrowRightLeft,
      statusColor: 'bg-slate-100 text-slate-700'
    },
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
      statusColor: 'bg-slate-100 text-slate-500',
      size: 'lg',
      iconSize: 15,
      iconContainerClassName: 'rounded-md p-1.5',
      titleClassName: 'normal-case whitespace-nowrap tracking-[-0.02em] text-[10px]'
    }
  ];

  // ============================================
  // CRUD 操作处理
  // ============================================

  // 新建端点
  const handleCreate = () => {
    setEditingEndpoint(null);
    setShowForm(true);
  };

  const handleCreateChannel = useCallback(async (payload) => {
    try {
      setChannelFormLoading(true);
      setCreateChannelError('');

      await createChannel(payload);

      setShowCreateChannel(false);
      await loadChannelsMeta();
    } catch (err) {
      console.error('创建渠道失败:', err);
      setCreateChannelError(getErrorMessage(err, '创建渠道失败'));
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
      priority: meta?.priority || 1,
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

  const performDeactivateChannel = useCallback(async (channelName) => {
    if (!isSqliteMode) return;

    try {
      setDeactivateChannelLoading(true);
      setActionError('');

      const representativeId = storageEndpoints.find(e => e.channel === channelName)?.id;
      if (!representativeId) throw new Error('未找到可用于停用的端点记录');

      await toggleEndpointRecordByID(representativeId, false);
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('停用渠道失败:', err);
      setActionError(`停用失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setDeactivateChannelLoading(false);
    }
  }, [isSqliteMode, loadGroups, loadStorageStatus, storageEndpoints]);

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
            <ArrowRightLeft className="w-6 h-6" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-900">Claude 管理</h1>
            <p className="text-slate-500 text-sm mt-1">
              以渠道为单位进行路由与故障转移，渠道内优先在端点之间切换，渠道耗尽后跨渠道切换
              {lastUpdate && (
                <span className="ml-2 text-slate-400">· 更新于 {lastUpdate}</span>
              )}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {/* SSE 状态指示器 */}
          <div className="flex items-center gap-1.5 text-xs text-slate-500">
            <span className={`w-2 h-2 rounded-full ${
              sseConnectionStatus === 'connected' ? 'bg-emerald-400' :
              sseConnectionStatus === 'connecting' ? 'bg-amber-400 animate-pulse' :
              'bg-slate-300'
            }`} />
            {sseConnectionStatus === 'connected' ? '实时' : '离线'}
          </div>

          {/* 刷新按钮 */}
          <Button
            variant="ghost"
            size="sm"
            icon={RefreshCw}
            onClick={async () => {
              if (isSqliteMode) {
                await loadStorageStatus();
              } else {
                await refresh();
              }
              await loadGroups();
            }}
            loading={loading}
          >
            刷新
          </Button>

          {/* 批量检测按钮 */}
          <Button
            icon={Activity}
            loading={batchCheckLoading}
            onClick={handleBatchHealthCheck}
          >
            检测全部
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
      {channelFailoverEnabled && scopedChannelCount > 0 && scopedEndpointCount > 0 && displayStats.healthy === 0 && (
        <div className="mb-6 p-4 bg-rose-50 border border-rose-300 rounded-xl flex items-start gap-3">
          <div className="p-2 bg-rose-100 rounded-full flex-shrink-0">
            <AlertTriangle className="w-5 h-5 text-rose-600" />
          </div>
          <div className="flex-1">
            <div className="font-semibold text-rose-900">
              所有端点均处于不可用状态
            </div>
            <div className="text-sm text-rose-700 mt-1">
              当前 {scopedChannelCount} 个参与渠道共 {scopedEndpointCount} 个端点全部异常，请求将根据恢复策略处理（快速失败/循环重试/智能等待）。
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
              expanded={expandedChannelName === section.name}
              onToggleExpanded={() => {
                setExpandedChannelName((prev) => (prev === section.name ? '' : section.name));
              }}
              loading={channelActionLoading || deactivateChannelLoading}
              onOpenEndpoint={openEndpointDetail}
              onToggleEndpointFailover={isSqliteMode ? handleToggleEndpointFailover : undefined}
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
              onDeactivate={async (channelName) => {
                if (!isSqliteMode) return;

                const needConfirm = !channelFailoverEnabled || channelSections.length <= 1;
                if (needConfirm) {
                  setDeactivateChannelTarget({ name: channelName });
                  return;
                }
                await performDeactivateChannel(channelName);
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
              onAddEndpoint={(channelName) => {
                setDefaultChannel(channelName);
                setLockChannel(true);
                handleCreate();
              }}
              onEditChannel={handleOpenEditChannel}
              onDeleteChannel={handleDeleteChannel}
              onEditEndpoint={(ep) => {
                closeEndpointDetail();
                setDefaultChannel('');
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
          defaultChannel={defaultChannel}
          lockChannel={lockChannel}
          onSave={handleSave}
          onCancel={() => {
            setShowForm(false);
            setEditingEndpoint(null);
            setDefaultChannel('');
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
          onClose={() => {
            setShowEditChannel(false);
            setEditingChannel(null);
          }}
          onSubmit={handleUpdateChannel}
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

      {deactivateChannelTarget?.name && (
        <DeactivateChannelConfirmDialog
          channelName={deactivateChannelTarget.name}
          loading={deactivateChannelLoading}
          onCancel={() => setDeactivateChannelTarget(null)}
          onConfirm={async () => {
            const name = deactivateChannelTarget.name;
            setDeactivateChannelTarget(null);
            await performDeactivateChannel(name);
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
        isOpen={detailOpen}
        isSqliteMode={isSqliteMode}
        channelFailoverEnabled={channelFailoverEnabled}
        globalTimeoutSeconds={globalTimeoutSeconds}
        channelMeta={
          channelsMeta.find((item) => item?.name === (detailTarget?.channel || detailTarget?.group)) || null
        }
        onClose={closeEndpointDetail}
        onEdit={(ep) => {
          closeEndpointDetail();
          setDefaultChannel('');
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
