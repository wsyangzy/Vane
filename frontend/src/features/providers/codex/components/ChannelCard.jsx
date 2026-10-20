import { useEffect, useRef, useState } from 'react';
import { ArrowRightLeft, ChevronDown, Globe, KeyRound, Loader2, Pencil, Plus, Repeat, Route, RouteOff, ShieldCheck, ShieldOff, Trash2, TriangleAlert } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { SubscriptionStatus } from '@/features/providers/shared/subscriptions/SubscriptionSettings.jsx';
import { isSubscriptionGroup } from '@/features/providers/shared/subscriptions/subscriptionSettings.js';
import CredentialGroupStrip from '@/features/providers/shared/credentials/CredentialGroupStrip.jsx';
import { buildCredentialGroupStats } from '@/features/providers/shared/credentials/credentialGroupStats.js';
import { openExternalURL } from '@/services/wails/index.js';
import { RepeatOffIcon } from './endpointState.jsx';
import { ENDPOINT_STATE_BUTTON_CLASS } from '../model.js';
import { getCredentialGroups, hasCodexCredential, getDefaultCredentialGroup, sortEndpointsByPriority } from '../model.js';
import { EndpointRow } from './EndpointRow.jsx';

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
	onOpenCredentialGroup,
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
  // actionLoading 形如 default-credential-group-<id>，仅在「设为默认」进行中出现
  const defaultingCredentialGroupId = /^default-credential-group-(\d+)$/.exec(actionLoading || '')?.[1] ?? null;
  const channelCardRef = useRef(null);
  const isSwitchingChannel = actionLoading === `activate-channel-${channel.id}`;
  const isDeletingChannel = actionLoading === `delete-channel-${channel.id}`;
  const isTogglingChannelFailover = actionLoading === `toggle-channel-failover-${channel.id}`;
  const channelActivationDisabled = channelFailoverEnabled && channel.failoverEnabled === false;
  const endpoints = channel.endpoints || [];
  const credentialGroups = getCredentialGroups(channel);
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
  // 分段条上的转移指示器，按分组统计端点归属（与 groupEndpointFilter 同规则）
  const credentialGroupStats = buildCredentialGroupStats(credentialGroups, endpoints);
  const selectedCredentialHasCredential = Boolean(selectedCredentialGroup) &&
    hasCodexCredential(selectedCredentialGroup, channel);
  const channelHasCredential = hasCodexCredential(channel) ||
    credentialGroups.some((group) => hasCodexCredential(group)) ||
    endpoints.some((endpoint) => hasCodexCredential(endpoint));
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
          {channelFailoverEnabled && channelHasCredential && (
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
      <div className="border-t border-slate-100 bg-white px-4 py-2">
        <CredentialGroupStrip
          groups={credentialGroups}
          selectedId={selectedCredentialGroupIDResolved}
          stats={credentialGroupStats}
          label={`Credentials · ${credentialGroups.length}`}
          onSelect={setSelectedCredentialGroupID}
          onOpen={(group) => onOpenCredentialGroup?.(channel, group)}
          onSetDefault={(group) => onSetDefaultCredentialGroup?.(channel, group)}
          onEdit={(group) => onEditCredentialGroup?.(channel, group)}
          onDelete={(group) => onDeleteCredentialGroup?.(channel, group)}
          onAdd={() => onAddCredentialGroup?.(channel)}
          loadingGroupId={defaultingCredentialGroupId}
          emptyHint="暂无分组，将回退旧渠道 Key。"
        />
      </div>

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
          <div hidden={!selectedCredentialHasCredential} className="flex flex-shrink-0 items-center gap-1">
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
             {selectedCredentialHasCredential
               ? '当前分组暂无端点。请为该凭证分组添加至少一个 base_url。'
              : '请先添加凭证分组并配置 API Key，再添加端点。'}
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

export { ChannelCard };
