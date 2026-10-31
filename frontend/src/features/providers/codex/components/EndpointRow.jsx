import { Loader2, Pencil, Repeat, ShieldCheck, ShieldOff, Trash2 } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { EndpointHealthBadge as CodexHealthBadge, EndpointLatencyBadge as CodexLatencyBadge, EndpointRequestFailureBadge as CodexRequestFailureBadge } from '@/features/providers/shared/endpoints/endpointBadges.jsx';
import { formatMultiplierExpression } from '@/shared/lib/multiplier.js';
import { RepeatOffIcon } from './endpointState.jsx';
import { ENDPOINT_STATE_BUTTON_CLASS } from '../model.js';
import { normalizeMultiplierValue } from '../model.js';

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
          <Tooltip asChild content={failoverToggleBlocked ? '至少保留一个参与转移的备用端点' : undefined}><button
            type="button"
            onClick={handleFailoverToggle}
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
                ? (endpoint.isActive
                    ? '点击：不参与故障转移，并自动切换当前端点'
                    : '点击：不参与故障转移')
                : '点击：参与故障转移'
            }
            aria-pressed={failoverEnabled}
          >
            {failoverEnabled ? <Repeat size={12} /> : <RepeatOffIcon size={12} />}
          </button></Tooltip>
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

export { EndpointRow };
