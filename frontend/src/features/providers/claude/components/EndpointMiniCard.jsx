import { Pencil, Trash2, Repeat, ShieldCheck, ShieldOff } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { EndpointHealthBadge as HealthBadge, EndpointLatencyBadge as LatencyBadge, EndpointRequestFailureBadge as RequestFailedBadge } from '@/features/providers/shared/endpoints/endpointBadges.jsx';
import { getCredentialGroupCostMultiplier } from '../utils/credentialGroups.js';
import { RepeatOffIcon } from './endpointState.jsx';
import { ENDPOINT_STATE_BUTTON_CLASS } from '../model.js';
import { normalizeBool } from '../model.js';

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
              <Tooltip asChild content={failoverToggleBlocked ? '至少保留一个参与转移的备用端点' : undefined}><button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  if (failoverToggleBlocked) return;
                  onToggleFailover?.(endpoint, !failoverEnabled);
                }}
                disabled={failoverToggleBlocked}
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
              </button></Tooltip>
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

export { EndpointMiniCard };
