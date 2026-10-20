import { useState } from 'react';
import { RefreshCw, Plus, Pencil, Trash2, AlertTriangle, ArrowRightLeft, Repeat, ShieldCheck, ShieldOff, ChevronDown, Route, RouteOff, Globe } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { SubscriptionStatus } from './SubscriptionSettings.jsx';
import { isSubscriptionGroup } from '../utils/subscription.js';
import CredentialGroupStrip from '@/features/providers/shared/credentials/CredentialGroupStrip.jsx';
import { buildCredentialGroupStats } from '@/features/providers/shared/credentials/credentialGroupStats.js';
import { getCredentialGroupForEndpoint } from '../utils/credentialGroups.js';
import { openExternalURL } from '@/services/wails/index.js';
import { RepeatOffIcon } from './endpointState.jsx';
import { ENDPOINT_STATE_BUTTON_CLASS } from '../model.js';
import { hasClaudeCredential, getDefaultClaudeCredentialGroup, isSameEndpoint, endpointParticipatesInFailover, pickCurrentRouteEndpoint } from '../model.js';
import { EndpointMiniCard } from './EndpointMiniCard.jsx';

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

      {isSubscriptionGroup(selectedCredentialGroup) && <div className="border-t border-slate-100 px-4 py-2"><SubscriptionStatus group={selectedCredentialGroup} /></div>}

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

export { ChannelCard };
