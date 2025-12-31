// ============================================
// Channels 页面 - 渠道管理（渠道内端点故障转移）
// 2025-11-28 (Updated 2025-12-06 for v5.0 SQLite Storage)
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
  ArrowRightLeft,
  Calculator,
  ShieldCheck,
  ShieldOff,
  CheckCircle2,
  XCircle,
  Clock,
  ChevronDown,
  ChevronUp,
  Pause,
  Play,
  Power,
  Globe
} from 'lucide-react';
import {
  Button,
  LoadingSpinner,
  ErrorMessage
} from '@components/ui';
import useEndpointsData from '@hooks/useEndpointsData.js';
import {
  EndpointForm,
  DeleteConfirmDialog,
  PriorityBadge
} from './components';
import {
  getEndpointStorageStatus,
  getEndpointRecords,
  createEndpointRecord,
  updateEndpointRecordByID,
  deleteEndpointRecordByID,
  toggleEndpointRecordByID,
  setEndpointFailoverEnabledByID,
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
  subscribeToEvent
} from '@utils/wailsApi.js';

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

  return (
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

  return (
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

  return (
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
// 端点表格行组件 (v5.0 增强版 - 参考 test.jsx 设计)
// ============================================

// 健康状态徽章（健康/异常/未检测/跳过检测）
const HealthBadge = ({ healthy, neverChecked, skipped }) => {
  if (skipped) {
    return (
      <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-purple-50 text-purple-600 border border-purple-200 whitespace-nowrap flex-shrink-0">
        <ShieldOff size={10} className="mr-1" />
        跳过检测
      </div>
    );
  }
  if (neverChecked) {
    return (
      <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-slate-50 text-slate-400 border border-slate-200">
        <Clock size={10} className="mr-1" />
        未检测
      </div>
    );
  }

  return healthy ? (
    <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-emerald-50 text-emerald-600 border border-emerald-100">
      <CheckCircle2 size={10} className="mr-1" />
      健康
    </div>
  ) : (
    <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-rose-50 text-rose-600 border border-rose-100">
      <XCircle size={10} className="mr-1" />
      异常
    </div>
  );
};

// 延迟指示器
const LatencyBadge = ({ ms, inScope = true }) => {
  if (!inScope) {
    return null;
  }

  const msValue = Number(ms);
  if (!Number.isFinite(msValue) || msValue <= 0) {
    return (
      <span className="font-mono text-xs font-medium px-2 py-0.5 rounded border whitespace-nowrap flex-shrink-0 bg-slate-50 text-slate-300 border-slate-200">
        -
      </span>
    );
  }

  const msInt = Math.max(1, Math.round(msValue));

  let colorClass = 'text-emerald-600 bg-emerald-50 border-emerald-100';
  if (msInt > 500) colorClass = 'text-amber-600 bg-amber-50 border-amber-100';
  if (msInt > 1000) colorClass = 'text-rose-600 bg-rose-50 border-rose-100';

  return (
    <span className={`font-mono text-xs font-medium px-2 py-0.5 rounded border whitespace-nowrap flex-shrink-0 ${colorClass}`}>
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

// ============================================
// 端点精简卡片（参考基础定价卡片）
// ============================================

const EndpointMiniCard = ({
  endpoint,
  isActiveChannel,
  isSqliteMode,
  channelFailoverEnabled = true,
  onOpen,
  onToggleFailover,
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
  const lastCheck = formatLastCheck(endpoint.lastCheck || endpoint.last_check || endpoint.updatedAt);

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
      className={`
        group w-full text-left bg-white rounded-xl border shadow-sm transition-all
        hover:shadow-md hover:border-slate-300
        ${rowActive ? 'border-slate-200/60' : 'border-slate-200/60 opacity-80'}
      `}
    >
      <div className="px-4 py-3 border-b border-slate-100">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-nowrap overflow-hidden min-w-0">
              <h3 className="font-bold text-slate-900 truncate min-w-0 flex-initial text-sm leading-5">{endpoint.name}</h3>
              <PriorityBadge priority={endpoint.priority || 1} className="flex-shrink-0" />
              <LatencyBadge ms={responseTime} inScope={inScope} />
              {statusBadgeText && (
                <span
                  className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-semibold bg-slate-50 text-slate-500 border border-slate-200 whitespace-nowrap flex-shrink-0"
                  title={statusBadgeTitle || undefined}
                >
                  {statusBadgeText}
                </span>
              )}
            </div>
            <div className="flex items-center gap-2 mt-1 min-w-0">
              <Globe size={12} className="text-slate-400 flex-shrink-0" />
              <span className="text-xs text-slate-500 font-mono truncate" title={endpoint.url}>
                {endpoint.url}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-1 flex-shrink-0">
            {isSqliteMode && (
              <>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onToggleFailover?.(endpoint, !failoverEnabled);
                  }}
                  className={`p-1.5 rounded-md transition-colors ${
                    failoverEnabled
                      ? 'text-indigo-600 hover:bg-indigo-50'
                      : 'text-slate-400 hover:bg-slate-100'
                  }`}
                  title={failoverEnabled ? '点击：不参与渠道内故障转移' : '点击：参与渠道内故障转移'}
                  aria-pressed={failoverEnabled}
                >
                  <ArrowRightLeft size={14} />
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onEdit?.(endpoint);
                  }}
                  className="p-1.5 text-slate-400 hover:bg-slate-100 hover:text-indigo-600 rounded-md transition-colors"
                  title="编辑"
                >
                  <Pencil size={14} />
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete?.(endpoint);
                  }}
                  className="p-1.5 text-slate-400 hover:bg-rose-50 hover:text-rose-600 rounded-md transition-colors"
                  title="删除"
                >
                  <Trash2 size={14} />
                </button>
              </>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2 mt-2 flex-wrap">
          {inScope && (
            <HealthBadge healthy={endpoint.healthy} neverChecked={isNeverChecked} skipped={isSkipHealthCheck} />
          )}
          {authType && (
            <span className="inline-flex items-center text-[10px] text-slate-500 bg-slate-50 px-2 py-0.5 rounded border border-slate-200">
              <ShieldCheck size={10} className="mr-1 text-amber-500" />
              {authType}
            </span>
          )}
          {multiplier && multiplier !== 1.0 && (
            <span className="inline-flex items-center text-[10px] font-mono text-orange-600 bg-orange-50 px-2 py-0.5 rounded border border-orange-100">
              {multiplier}x
            </span>
          )}
          <span className="text-[10px] text-slate-400 font-mono">
            最后检查 {lastCheck}
          </span>
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
  isOpen,
  isSqliteMode,
  failoverDefaultCooldownSeconds = 600,
  onClose,
  onEdit,
  onDelete
}) => {
  if (!isOpen || !endpoint) return null;

  const channel = endpoint.channel || endpoint.group || '-';
  const responseTime = endpoint.response_time || endpoint.responseTimeMs || 0;
  const lastCheck = formatLastCheck(endpoint.lastCheck || endpoint.last_check || endpoint.updatedAt);
  const inScope = endpoint._health_scope !== false;
  const neverChecked = endpoint.never_checked || endpoint.neverChecked || (!endpoint.lastCheck && !endpoint.last_check);
  const isSkipHealthCheck = endpoint.skipHealthCheck || endpoint.skip_health_check;
  const healthNeverChecked = !inScope || neverChecked;

  const failoverEnabled = normalizeBool(endpoint.failoverEnabled, true);
  const supportsCountTokens = !!endpoint.supportsCountTokens;
  const multiplier = endpoint.costMultiplier || 1.0;

  const tokenRaw = endpoint.token || '';
  const apiKeyRaw = endpoint.apiKey || endpoint.api_key || '';
  const tokenMasked = endpoint.tokenMasked || endpoint.token_masked || '';
  const apiKeyMasked = endpoint.apiKeyMasked || endpoint.api_key_masked || '';

  const maskSecret = (secret) => {
    if (!secret) return '';
    const s = String(secret);
    if (s.length <= 8) return '********';
    return `${s.slice(0, 6)}...${s.slice(-4)}`;
  };

  const rows = [
    { label: '优先级', value: endpoint.priority ?? '-' },
    { label: '超时(s)', value: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? '-' },
  ];

  const endpointCooldownSeconds = endpoint.cooldownSeconds ?? endpoint.cooldown_seconds;
  const cooldownUsesGlobal = endpointCooldownSeconds == null;
  const cooldownSeconds = endpointCooldownSeconds ?? failoverDefaultCooldownSeconds ?? '-';
  const hasToken = !!(tokenRaw || tokenMasked);
  const hasApiKey = !!(apiKeyRaw || apiKeyMasked);

  return (
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
        <div className="flex items-start justify-between px-6 py-4 border-b border-slate-100">
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h2 className="text-lg font-semibold text-slate-900 truncate">{endpoint.name}</h2>
              <span className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium bg-blue-50 text-blue-600 border border-blue-100">
                {channel}
              </span>
            </div>
            <p className="text-xs text-slate-500 font-mono mt-1 truncate" title={endpoint.url}>
              {endpoint.url}
            </p>

            <div className="flex items-center gap-1.5 mt-2 flex-nowrap overflow-hidden min-w-0">
              <HealthBadge healthy={endpoint.healthy} neverChecked={healthNeverChecked} skipped={isSkipHealthCheck} />
              <LatencyBadge ms={responseTime} inScope={inScope} />

              <span
                className={`inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium border whitespace-nowrap min-w-0 truncate max-w-[160px] ${
                  failoverEnabled
                    ? 'bg-indigo-50 text-indigo-700 border-indigo-100'
                    : 'bg-slate-50 text-slate-400 border-slate-200'
                }`}
                title={failoverEnabled ? '参与渠道内故障转移' : '不参与渠道内故障转移'}
              >
                <ArrowRightLeft size={10} className="mr-1" />
                {failoverEnabled ? '渠道内故障转移' : '不参与转移'}
              </span>

              <span
                className={`inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium border whitespace-nowrap min-w-0 truncate max-w-[150px] ${
                  supportsCountTokens
                    ? 'bg-purple-50 text-purple-700 border-purple-100'
                    : 'bg-slate-50 text-slate-400 border-slate-200'
                }`}
                title={supportsCountTokens ? '支持 count_tokens' : '不支持 count_tokens'}
              >
                <Calculator size={10} className="mr-1" />
                {supportsCountTokens ? 'count_tokens' : '无 count_tokens'}
              </span>

              {multiplier !== 1.0 && (
                <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium border bg-orange-50 text-orange-700 border-orange-100 whitespace-nowrap flex-shrink-0">
                  {multiplier}x
                </span>
              )}

              <span className="text-[10px] text-slate-400 font-mono whitespace-nowrap flex-shrink-0">
                最后检查 {lastCheck}
              </span>
            </div>
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

        <div className="p-6 overflow-y-auto">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {rows.map((r, idx) => (
              <div
                key={r.label}
                className={`bg-slate-50 rounded-xl p-3 border border-slate-200/60 ${
                  rows.length % 2 === 1 && idx === rows.length - 1 ? 'md:col-span-2' : ''
                }`}
              >
                <div className="text-xs text-slate-500 mb-1">{r.label}</div>
                <div className="text-sm font-semibold text-slate-900 break-all">
                  {String(r.value)}
                </div>
              </div>
            ))}
          </div>

          <div className="mt-4 grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="bg-slate-50 rounded-xl p-3 border border-slate-200/60">
              <div className="flex items-center justify-between mb-1">
                <div className="text-xs text-slate-500">冷却(s)</div>
                {cooldownUsesGlobal && (
                  <span className="text-[10px] text-slate-400 bg-white/70 px-2 py-0.5 rounded border border-slate-200">
                    全局默认
                  </span>
                )}
              </div>
              <div className="text-sm font-semibold text-slate-900 break-all">
                {String(cooldownSeconds)}
              </div>
            </div>

            <div className="bg-slate-50 rounded-xl p-3 border border-slate-200/60">
              <div className="flex items-center justify-between mb-1">
                <div className="text-xs text-slate-500">Token（脱敏）</div>
                <button
                  onClick={() => {
                    if (tokenRaw) {
                      navigator.clipboard.writeText(tokenRaw);
                    }
                  }}
                  disabled={!tokenRaw}
                  className={`inline-flex items-center justify-center p-1 rounded-md transition-colors ${
                    tokenRaw
                      ? 'text-slate-400 hover:text-indigo-600 hover:bg-white/70'
                      : 'text-slate-300 cursor-not-allowed'
                  }`}
                  title={tokenRaw ? '复制原始 Token' : '无原始 Token（仅 SQLite 记录可复制）'}
                >
                  <Copy size={12} />
                </button>
              </div>
              <div className="text-sm font-mono text-slate-900 break-all">
                {hasToken ? (tokenMasked || maskSecret(tokenRaw)) : '-'}
              </div>
            </div>
          </div>

          {hasApiKey && (
            <div className="mt-3 bg-slate-50 rounded-xl p-3 border border-slate-200/60">
              <div className="flex items-center justify-between mb-1">
                <div className="text-xs text-slate-500">API Key（脱敏）</div>
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
                  title={apiKeyRaw ? '复制原始 API Key' : '无原始 API Key（仅 SQLite 记录可复制）'}
                >
                  <Copy size={12} />
                </button>
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
  const [priority, setPriority] = useState(() => String(isEdit ? (initialValue?.priority || 1) : 1));
  const [error, setError] = useState('');

  if (!open) return null;

  return (
    <div className="fixed inset-0 bg-slate-900/30 backdrop-blur-sm flex items-center justify-center p-4 z-[60]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-xl max-h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
        <div className="flex items-start justify-between px-6 py-4 border-b border-slate-100">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑渠道' : '新建渠道'}</h2>
            <p className="text-xs text-slate-500 mt-1">
              {isEdit ? '修改渠道官网与优先级' : '先创建渠道，再在渠道卡片里添加端点'}
            </p>
          </div>
          <button
            onClick={onClose}
            className="p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700 rounded-lg transition-colors"
            title="关闭"
            disabled={loading}
          >
            <XCircle size={18} />
          </button>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            const trimmedName = name.trim();
            if (!trimmedName) {
              setError('请输入渠道名称');
              return;
            }
            setError('');
            onSubmit?.({ name: trimmedName, website: website.trim(), priority });
          }}
          className="p-6 space-y-4"
        >
          {(error || serverError) && (
            <div className="p-3 bg-rose-50 border border-rose-200 rounded-lg text-rose-700 text-sm">
              {error || serverError}
            </div>
          )}

          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div className="sm:col-span-2">
                <label className="block text-sm font-medium text-slate-700">
                  渠道名称 <span className="text-rose-500">*</span>
                </label>
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="例如：official / backup / openai"
                  className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                  disabled={loading}
                  readOnly={isEdit}
                />
                {isEdit && (
                  <p className="text-xs text-slate-400 mt-1">渠道名称用于关联端点，不支持修改</p>
                )}
              </div>

              <div>
                <label className="block text-sm font-medium text-slate-700">优先级</label>
                <input
                  type="number"
                  min={1}
                  value={priority}
                  onChange={(e) => setPriority(e.target.value)}
                  className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                  disabled={loading}
                />
                <p className="text-xs text-slate-400 mt-1">仅在启用渠道间故障转移时生效，数字越小越优先</p>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-slate-700">渠道官网</label>
              <input
                value={website}
                onChange={(e) => setWebsite(e.target.value)}
                placeholder="https://example.com (可选)"
                className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                disabled={loading}
              />
              <p className="text-xs text-slate-400 mt-1">用于渠道信息展示，非必填</p>
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 pt-2">
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
  onEditEndpoint,
  onDeleteEndpoint,
  loading = false
}) => {
  const scopedEndpoints = endpoints.filter(e => e._health_scope !== false);
  const scopedHealthyCount = scopedEndpoints.filter(e => e.healthy).length;
  const scopedTotalCount = scopedEndpoints.length;

  const totalCount = endpoints.length;
  const hasEndpoints = totalCount > 0;
  const hasGroupInfo = !!groupInfo;

  const isActive = groupInfo?.active
    ?? (isSqliteMode ? endpoints.some(e => e.enabled) : (activeChannelName === channelName));

  const isPaused = !!groupInfo?.paused;
  const computedPriority = Math.min(...endpoints.map(e => e.priority || 999));
  const priority = Number.isFinite(channelPriority) && channelPriority > 0
    ? channelPriority
    : (groupInfo?.priority ?? (Number.isFinite(computedPriority) ? computedPriority : 999));

  const activeHighlightClass = channelFailoverEnabled
    ? 'border-[#FF9933] ring-2 ring-[#FF9933]/20'
    : 'border-indigo-300 ring-2 ring-indigo-100';

  // 默认折叠态：统一只展示 1 个端点，保证同行渠道卡片基础高度一致
  const collapsedVisibleCount = 1;
  const visibleEndpoints = expanded ? endpoints : endpoints.slice(0, collapsedVisibleCount);
  const hasMore = endpoints.length > collapsedVisibleCount;
  const pauseDisabled = loading || !channelFailoverEnabled;

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

  const healthTone = !hasEndpoints || scopedTotalCount === 0
    ? 'bg-slate-50 text-slate-500 border-slate-200'
    : scopedHealthyCount === scopedTotalCount
      ? 'bg-emerald-50 text-emerald-700 border-emerald-200'
      : scopedHealthyCount > 0
        ? 'bg-amber-50 text-amber-800 border-amber-200'
        : 'bg-rose-50 text-rose-700 border-rose-200';

  return (
    <div className={`
      bg-white rounded-2xl border shadow-sm overflow-hidden flex flex-col self-start
      ${isActive ? activeHighlightClass : 'border-slate-200/60'}
    `} data-channel-card={channelName}>
      {/* 渠道头部 */}
      <div className="px-6 py-4 border-b border-slate-100">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2 min-w-0">
              {channelWebsite && (
                <button
                  type="button"
                  className="inline-flex items-center justify-center w-6 h-6 rounded-md text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors flex-shrink-0"
                  title={channelWebsite}
                  aria-label="打开渠道官网"
                  onClick={() => openExternalURL(channelWebsite)}
                >
                  <Globe size={14} />
                </button>
              )}
              <h2 className="font-bold text-slate-900 truncate min-w-0">{channelName}</h2>
              <span className={`inline-flex items-center justify-center min-w-9 h-6 px-2 rounded-full text-[11px] font-semibold border whitespace-nowrap leading-none flex-shrink-0 ${priorityTone}`}>
                {priorityLabel}
              </span>
            </div>
          </div>

          {/* 渠道操作 */}
          <div className="flex items-center gap-2 flex-shrink-0">
          {!isActive && isSqliteMode && hasEndpoints && (
            <Button
              size="sm"
              icon={Power}
              className="px-2.5 py-1 rounded-full text-[11px] font-semibold border border-transparent hover:translate-y-0"
              onClick={() => onActivate?.(channelName)}
              disabled={loading}
            >
              激活
            </Button>
          )}
          {isActive && isSqliteMode && (
            <Button
              size="sm"
              variant="dangerSoft"
              className="px-2.5 py-1 rounded-full text-[11px] font-semibold hover:translate-y-0"
              icon={Power}
              onClick={() => onDeactivate?.(channelName)}
              disabled={loading}
            >
              停用
            </Button>
          )}
          {hasGroupInfo && !isPaused ? (
            <Button
              size="sm"
              variant="ghost"
              icon={Pause}
              onClick={() => onPause?.(channelName)}
              disabled={pauseDisabled}
              title={!channelFailoverEnabled ? '已关闭渠道间故障转移：不可暂停渠道' : undefined}
            >
              暂停
            </Button>
          ) : hasGroupInfo ? (
            <Button
              size="sm"
              variant="ghost"
              icon={Play}
              onClick={() => onResume?.(channelName)}
              disabled={pauseDisabled}
              title={!channelFailoverEnabled ? '已关闭渠道间故障转移：不可恢复渠道' : undefined}
            >
              恢复
            </Button>
          ) : null}
          {isSqliteMode && (
            <Button
              size="sm"
              variant="ghost"
              icon={Server}
              onClick={() => onAddEndpoint?.(channelName)}
              disabled={loading}
            >
              添加端点
            </Button>
          )}
          {isSqliteMode && (
            <button
              onClick={() => onEditChannel?.(channelName)}
              disabled={loading}
              className={`p-2 rounded-lg transition-colors ${
                loading ? 'text-slate-300 cursor-not-allowed' : 'text-slate-400 hover:bg-slate-100 hover:text-slate-700'
              }`}
              title="编辑渠道"
            >
              <Pencil size={16} />
            </button>
          )}
          {isSqliteMode && (
            <button
              onClick={() => onDeleteChannel?.(channelName)}
              disabled={loading}
              className={`p-2 rounded-lg transition-colors ${
                loading ? 'text-slate-300 cursor-not-allowed' : 'text-slate-400 hover:bg-rose-50 hover:text-rose-600'
              }`}
              title="删除渠道"
            >
              <Trash2 size={16} />
            </button>
          )}
          </div>
        </div>

        <div className="mt-2 flex items-center gap-1.5 text-[11px] leading-4 flex-nowrap whitespace-nowrap min-w-0">
          {isActive ? (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[11px] font-semibold bg-emerald-50 text-emerald-600 border border-emerald-100 whitespace-nowrap">
              活跃
            </span>
          ) : (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[11px] font-semibold bg-slate-50 text-slate-500 border border-slate-200 whitespace-nowrap">
              备用
            </span>
          )}
          {isPaused && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[11px] font-semibold bg-amber-50 text-amber-700 border border-amber-200 whitespace-nowrap">
              已暂停
            </span>
          )}
          {groupInfo?.in_cooldown && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[11px] font-semibold bg-amber-50 text-amber-700 border border-amber-200 whitespace-nowrap">
              冷却中
            </span>
          )}

          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md bg-slate-50 text-slate-600 border border-slate-200 whitespace-nowrap">
            端点 <span className="font-semibold text-slate-800">{totalCount}</span>
          </span>
          <span
            className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md border ${healthTone} whitespace-nowrap`}
            title={hasEndpoints && scopedTotalCount === 0 ? '当前渠道不在健康检查范围（未参与故障转移/非活跃渠道）' : undefined}
          >
            {hasEndpoints && scopedTotalCount === 0 ? '检测' : '健康'}{' '}
            <span className="font-semibold">
              {hasEndpoints ? (scopedTotalCount > 0 ? `${scopedHealthyCount}/${scopedTotalCount}` : '关闭') : '-'}
            </span>
          </span>
        </div>
      </div>

      {/* 端点卡片列表（精简展示） */}
      <div className="p-4 space-y-3 flex-1">
        {visibleEndpoints.length === 0 ? (
          <div className="text-sm text-slate-500 text-center py-8">
            暂无端点
          </div>
        ) : (
          visibleEndpoints.map((endpoint, index) => (
            <EndpointMiniCard
              key={endpoint.name || index}
              endpoint={endpoint}
              isActiveChannel={isActive}
              isSqliteMode={isSqliteMode}
              channelFailoverEnabled={channelFailoverEnabled}
              onOpen={onOpenEndpoint}
              onToggleFailover={onToggleEndpointFailover}
              onEdit={onEditEndpoint}
              onDelete={onDeleteEndpoint}
            />
          ))
        )}
      </div>

      {/* 底部区域：用于“显示全部/收起”按钮；无更多端点时显示虚线占位，保证同行卡片底边对齐 */}
      <div className="px-4 py-1 border-t border-slate-100 bg-slate-50/40">
        {hasMore ? (
          <button
            onClick={() => onToggleExpanded?.()}
            className="w-full h-6 flex items-center justify-center gap-2 text-[13px] leading-none text-slate-600 hover:text-indigo-600 transition-colors"
          >
            {expanded ? (
              <>
                收起
                <ChevronUp size={16} />
              </>
            ) : (
              <>
                显示全部 ({endpoints.length})
                <ChevronDown size={16} />
              </>
            )}
          </button>
        ) : (
          <div
            className="w-full h-6 rounded-lg border border-dashed border-slate-200/80 bg-white/60"
            aria-hidden="true"
          />
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
  const isSqliteMode = storageStatus?.storageType === 'sqlite' && storageStatus?.enabled;

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
  const [failoverDefaultCooldownSeconds, setFailoverDefaultCooldownSeconds] = useState(600);
  const [channelsMeta, setChannelsMeta] = useState([]);

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
      if (status.storageType === 'sqlite' && status.enabled) {
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
    const sqliteEnabled = storageStatus?.storageType === 'sqlite' && storageStatus?.enabled;
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
      const cooldownSeconds = Number(cfg?.failover_default_cooldown_seconds);
      setFailoverDefaultCooldownSeconds(
        Number.isFinite(cooldownSeconds) && cooldownSeconds > 0 ? cooldownSeconds : 600
      );
    } catch (err) {
      console.error('获取配置失败:', err);
      if (seq !== configLoadSeqRef.current) return;
      setChannelFailoverEnabled(true);
      setFailoverDefaultCooldownSeconds(600);
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

  useEffect(() => {
    loadGroups();
  }, [loadGroups]);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

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
    isSqliteModeRef.current = storageStatus?.storageType === 'sqlite' && storageStatus?.enabled;
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

  // 点击渠道卡片外部区域时自动收起
  useEffect(() => {
    if (!expandedChannelName) return;
    if (typeof document === 'undefined') return;

    const onPointerDown = (event) => {
      const target = event?.target;
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

  // v6.0: SQLite 模式下“enabled”语义为“激活渠道”，会同时启用该渠道下所有端点
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
      _health_scope: isEndpointInHealthScope(ep),
      _channel_paused: Boolean(groupInfoMap.get(ep?.group || ep?.channel || ep?.name || 'default')?.paused)
    }));
  }, [displayEndpoints, groupInfoMap, isEndpointInHealthScope]);

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
  const isSkippedEndpoint = (e) => e?.skipHealthCheck || e?.skip_health_check;
  const hasLastCheck = (e) => !!(e?.lastCheck || e?.last_check);
  const isNeverCheckedEndpoint = (e) => e?.never_checked || e?.neverChecked || !hasLastCheck(e);
  const cooldownChannelCount = groups.filter(g => g?.in_cooldown).length;

  const displayStats = isSqliteMode
    ? {
        total: scopedDisplayEndpoints.length,
        skipped: scopedDisplayEndpoints.filter(e => e._health_scope && isSkippedEndpoint(e)).length,
        healthy: scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && e.healthy && !isNeverCheckedEndpoint(e)).length,
        unhealthy: scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && !e.healthy && hasLastCheck(e) && !isNeverCheckedEndpoint(e)).length,
        unchecked: scopedDisplayEndpoints.filter(e => !e._health_scope || (!isSkippedEndpoint(e) && isNeverCheckedEndpoint(e))).length,
        cooldown: cooldownChannelCount,
        healthPercentage: (scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && !isNeverCheckedEndpoint(e)).length) > 0
          ? ((scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && e.healthy && !isNeverCheckedEndpoint(e)).length /
              scopedDisplayEndpoints.filter(e => e._health_scope && !isSkippedEndpoint(e) && !isNeverCheckedEndpoint(e)).length) * 100).toFixed(1)
          : 0
      }
    : { ...stats, skipped: stats.skipped || 0, cooldown: cooldownChannelCount };

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
      priority: meta?.priority || 1
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
            <h1 className="text-2xl font-bold text-slate-900">渠道管理</h1>
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
      <div className={displayStats.cooldown > 0 ? 'grid grid-cols-7 gap-4 mb-6' : 'grid grid-cols-6 gap-4 mb-6'}>
        <div className="bg-white rounded-xl border border-slate-200/60 p-4 shadow-sm">
          <div className="text-2xl font-bold text-slate-900">{channelSections.length}</div>
          <div className="text-sm text-slate-500">总渠道数</div>
        </div>
        <div className="bg-white rounded-xl border border-indigo-200/60 p-4 shadow-sm">
          <div className="text-2xl font-bold text-indigo-600">
            {activeChannel ? 1 : 0}
          </div>
          <div className="text-sm text-slate-500">
            当前激活
            {activeChannel && (
              <div className="text-xs text-indigo-500 mt-1 truncate">
                {activeChannel}
              </div>
            )}
          </div>
        </div>
        <div className="bg-white rounded-xl border border-emerald-200/60 p-4 shadow-sm">
          <div className="text-2xl font-bold text-emerald-600">{displayStats.healthy}</div>
          <div className="text-sm text-slate-500">健康端点</div>
        </div>
        <div className="bg-white rounded-xl border border-purple-200/60 p-4 shadow-sm">
          <div className="text-2xl font-bold text-purple-600">{displayStats.skipped || 0}</div>
          <div className="text-sm text-slate-500">跳过检测</div>
        </div>
        <div className="bg-white rounded-xl border border-rose-200/60 p-4 shadow-sm">
          <div className="text-2xl font-bold text-rose-600">{displayStats.unhealthy}</div>
          <div className="text-sm text-slate-500">异常端点</div>
        </div>
        {/* 冷却中端点卡片 - 仅在有冷却端点时显示 */}
        {displayStats.cooldown > 0 && (
          <div className="bg-white rounded-xl border border-amber-200/60 p-4 shadow-sm">
            <div className="text-2xl font-bold text-amber-600">{displayStats.cooldown}</div>
            <div className="text-sm text-slate-500">冷却中</div>
          </div>
        )}
        <div className="bg-white rounded-xl border border-slate-200/60 p-4 shadow-sm">
          <div className="text-2xl font-bold text-slate-400">{displayStats.unchecked}</div>
          <div className="text-sm text-slate-500">未检测端点</div>
        </div>
      </div>

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
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 items-start">
          {channelSections.map((section, idx) => (
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
              expanded={expandedChannelName === section.name}
              onToggleExpanded={() => {
                setExpandedChannelName((prev) => (prev === section.name ? '' : section.name));
              }}
              loading={channelActionLoading || deactivateChannelLoading}
              onOpenEndpoint={openEndpointDetail}
              onToggleEndpointFailover={isSqliteMode ? handleToggleEndpointFailover : undefined}
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

      {actionError && (
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
        failoverDefaultCooldownSeconds={failoverDefaultCooldownSeconds}
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
