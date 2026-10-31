import { ArrowRight, Database, KeyRound, Pencil, Server, Settings, XCircle } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import EndpointAvailabilityBar from '@/features/providers/shared/endpoints/EndpointAvailabilityBar.jsx';
import ModelTag from '@/features/requests/components/ModelTag.jsx';
import { EndpointHealthSection, ResourceDetailShell, ModelMappingPair } from '@/features/providers/shared/endpoints/detailParts.jsx';
import { formatLastCheck, formatDetailMultiplier, getDetailTimestamp } from '@/features/providers/shared/formatters.js';
import { EndpointHealthBadge as CodexHealthBadge, EndpointRequestFailureBadge as CodexRequestFailureBadge } from '@/features/providers/shared/endpoints/endpointBadges.jsx';
import { DetailMetricCard, DetailChip, DetailPill, DetailTimeoutPill, DetailTimestampFooter } from './detailParts.jsx';
import { getDefaultCredentialGroup, BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS } from '../model.js';

// ============================================================================
// 渠道卡片（卡片头 + 端点子列表）
// ============================================================================
// ============================================================================
// 端点行（紧凑表格风，延续渠道管理页密度）：
// 左侧 toggle = 激活；行整体点击展开内嵌编辑（base_url 直改即保存）
// ============================================================================
const CodexEndpointDetailModal = ({ endpoint, channel, globalConfig, onClose, onEdit, onHealth, onToggleHealth, busy = false }) => {
  if (!endpoint) return null;

  const healthState = endpoint.healthState || endpoint.health_state || 'unchecked';
  const responseTimeMs = endpoint.responseTimeMs || endpoint.response_time_ms || 0;
  const lastFailCode = Number(endpoint.lastFailCode || endpoint.last_fail_code || 0);
  const lastFailReason = endpoint.lastFailReason || endpoint.last_fail_reason || '';
  const hasFailDetail = (Number.isFinite(lastFailCode) && lastFailCode > 0) || !!lastFailReason;
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
  const lastCheck = formatLastCheck(rawLastCheck);
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

  return (
    <ResourceDetailShell
      title={endpoint.name || '未命名端点'}
      eyebrow={`Codex · ${channel?.name || '渠道'} · 端点详情`}
      subtitle={endpoint.baseURL || endpoint.base_url || '未设置地址'}
      icon={Server}
      onClose={onClose}
      actions={<Button variant="ghost" size="sm" icon={Pencil} onClick={() => onEdit?.(endpoint)}>编辑</Button>}
      summary={<>
        <CodexHealthBadge state={healthState} detail />
        <DetailPill tone="slate" icon={Settings} mono>P{priority}</DetailPill>
        <DetailTimeoutPill custom={hasCustomTimeout} value={timeoutLabel} tooltip={`${timeoutScope}: ${timeoutLabel}`} />
        <CodexRequestFailureBadge failCode={lastFailCode} failReason={lastFailReason} detail hideWhenEmpty />
      </>}
    >
      <EndpointHealthSection
        inScope={endpoint.healthInScope !== false && healthState !== 'disabled'}
        skipped={endpoint.skipHealthCheck ?? endpoint.skip_health_check}
        subscription={credentialGroup?.authMode === 'client_passthrough' || endpoint.authMode === 'client_passthrough'}
        ms={responseTimeMs}
        lastCheck={lastCheck}
        onCheck={onHealth ? () => onHealth(endpoint) : undefined}
        onToggle={onToggleHealth ? (skip) => onToggleHealth(endpoint, skip) : undefined}
        disabled={busy}
      />
      {hasFailDetail && <DetailMetricCard label="最近异常" icon={XCircle}>
        <p className="text-xs leading-relaxed text-rose-600 [overflow-wrap:anywhere]">{lastFailCode > 0 && <span className="mr-2 font-mono">HTTP {lastFailCode}</span>}{lastFailReason || '最近一次请求失败'}</p>
      </DetailMetricCard>}
          <EndpointAvailabilityBar
            provider="codex"
            channel={channel?.name || ''}
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
    </ResourceDetailShell>
  );
};

export { CodexEndpointDetailModal };
