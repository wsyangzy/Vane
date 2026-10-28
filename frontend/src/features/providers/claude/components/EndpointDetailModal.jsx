import { Activity, Pencil, Trash2, Database, Server, ArrowRight, Calculator, XCircle, Clock, Settings, KeyRound } from 'lucide-react';
import { Button, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import ModelTag from '@/features/requests/components/ModelTag.jsx';
import EndpointAvailabilityBar from '@/features/providers/shared/endpoints/EndpointAvailabilityBar.jsx';
import { EndpointHealthBadge as HealthBadge, EndpointLatencyBadge as LatencyBadge, EndpointRequestFailureBadge as RequestFailedBadge } from '@/features/providers/shared/endpoints/endpointBadges.jsx';
import { DetailChip, DetailMetricCard, DetailPill, DetailTimeoutPill, DetailTimestampFooter, ModelMappingPair } from '@/features/providers/shared/endpoints/detailParts.jsx';
import { formatLastCheck, formatDetailMultiplier, getDetailTimestamp, isDefaultDetailMultiplier } from '@/features/providers/shared/formatters.js';
import { getCredentialGroupCostMultiplier } from '../utils/credentialGroups.js';

// ============================================
// 端点详情弹窗（点击端点卡片弹出）
// ============================================

const EndpointDetailModal = ({
  endpoint,
  credentialGroup,
  isOpen,
  isSqliteMode,
  channelMeta = null,
  globalTimeoutSeconds = 300,
  // failoverDefaultCooldownSeconds 已移除
  onClose,
  onEdit,
  onDelete,
  onHealth
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

  const supportsCountTokens = !!endpoint.supportsCountTokens;
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
              <Button variant="ghost" size="sm" icon={Activity} onClick={() => onHealth?.(endpoint)}>健康检测</Button>
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




            {supportsCountTokens && (
              <Tooltip content="支持 count_tokens">
                <span className="inline-flex">
                  <DetailPill tone="purple" icon={Calculator}>
                    count_tokens
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
                      账号池请求在预算内等待已配置账号恢复。
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

export { EndpointDetailModal };
