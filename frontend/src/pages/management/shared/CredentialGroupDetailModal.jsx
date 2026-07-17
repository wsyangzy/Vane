import { useEffect, useMemo, useState } from 'react';
import {
  Calculator,
  Clock,
  Gauge,
  KeyRound,
  Layers3,
  Pencil,
  Server,
  WalletCards
} from 'lucide-react';
import { Button } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import { getCredentialGroupDailyUsage } from '@utils/wailsApi.js';
import {
  DetailChip,
  DetailMetricCard,
  DetailPill,
  DetailTimestampFooter
} from './detailParts.jsx';
import { formatDetailMultiplier, getDetailTimestamp } from './formatters.js';

const formatUSD = (value) => `$${Number(value || 0).toFixed(4)}`;

const formatResetAt = (value) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};

const getEndpointCount = (channel, group) => {
  const explicit = Number(group?.endpointCount ?? group?.endpoint_count);
  if (Number.isFinite(explicit)) return explicit;
  const groupID = Number(group?.id || 0);
  if (!groupID || !Array.isArray(channel?.endpoints)) return 0;
  const isDefault = Boolean(group?.isDefault ?? group?.is_default);
  return channel.endpoints.filter((endpoint) => {
    const endpointGroupID = Number(endpoint?.credentialGroupId ?? endpoint?.credential_group_id ?? 0);
    return endpointGroupID === groupID || (isDefault && endpointGroupID === 0);
  }).length;
};

const standardMultiplierItems = (provider, group) => {
  const items = [
    ['总', group?.costMultiplier ?? group?.cost_multiplier],
    ['输入', group?.inputCostMultiplier ?? group?.input_cost_multiplier],
    ['输出', group?.outputCostMultiplier ?? group?.output_cost_multiplier],
    ['缓存读', group?.cacheReadCostMultiplier ?? group?.cache_read_cost_multiplier],
    ['缓存创建', group?.cacheCreationCostMultiplier ?? group?.cache_creation_cost_multiplier]
  ];
  if (provider === 'claude') {
    items.push(['1h 缓存创建', group?.cacheCreationCostMultiplier1h ?? group?.cache_creation_cost_multiplier_1h]);
  }
  return items;
};

const longContextMultiplierItems = (group) => [
  ['总', group?.longContextCostMultiplier ?? group?.long_context_cost_multiplier],
  ['输入', group?.longContextInputCostMultiplier ?? group?.long_context_input_cost_multiplier],
  ['输出', group?.longContextOutputCostMultiplier ?? group?.long_context_output_cost_multiplier],
  ['缓存创建', group?.longContextCacheCreationCostMultiplier ?? group?.long_context_cache_creation_cost_multiplier],
  ['缓存读取', group?.longContextCacheReadCostMultiplier ?? group?.long_context_cache_read_cost_multiplier]
];

const CredentialGroupDetailModal = ({ provider, channel, channelName, group, onClose, onEdit }) => {
  const [usage, setUsage] = useState(null);
  const [usageError, setUsageError] = useState('');
  const [usageLoading, setUsageLoading] = useState(false);
  const resolvedChannelName = channel?.name || channelName || group?.channel || '';
  const limitUSD = Number(group?.dailyCostLimitUSD ?? group?.daily_cost_limit_usd ?? 0);
  const variant = provider === 'codex' ? 'codex' : 'default';
  const isDefault = Boolean(group?.isDefault ?? group?.is_default);
  const endpointCount = getEndpointCount(channel, group);
  const credentialMasked = group?.tokenMasked || group?.token_masked || group?.apiKeyMasked || group?.api_key_masked || '未设置凭证';
  const createdAt = getDetailTimestamp(group?.createdAt, group?.created_at, group?.CreatedAt);
  const updatedAt = getDetailTimestamp(group?.updatedAt, group?.updated_at, group?.UpdatedAt);
  const modelMultipliers = group?.modelMultipliers || group?.model_multipliers || [];
  const longContextEnabled = !Boolean(group?.disableLongContextPricing ?? group?.disable_long_context_pricing);

  useEffect(() => {
    let cancelled = false;
    const loadUsage = async () => {
      setUsageLoading(true);
      setUsageError('');
      try {
        const result = await getCredentialGroupDailyUsage(provider, resolvedChannelName, group?.name || '', limitUSD);
        if (!cancelled) setUsage(result);
      } catch (error) {
        if (!cancelled) setUsageError(error?.message || String(error));
      } finally {
        if (!cancelled) setUsageLoading(false);
      }
    };
    if (provider && resolvedChannelName && group?.name) loadUsage();
    return () => {
      cancelled = true;
    };
  }, [group?.id, group?.name, limitUSD, provider, resolvedChannelName]);

	const usagePercent = useMemo(() => {
    if (!(limitUSD > 0)) return 0;
	return (Number(usage?.usedUSD || 0) / limitUSD) * 100;
  }, [limitUSD, usage?.usedUSD]);
	const progress = Math.min(usagePercent, 100);
	const progressTone = usagePercent >= 100 ? 'bg-rose-500' : usagePercent >= 80 ? 'bg-amber-500' : 'bg-indigo-500';

  if (!group) return null;

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm animate-fade-in"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose?.();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="credential-group-detail-title"
        className="flex max-h-[calc(100vh-2rem)] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="border-b border-slate-100 px-6 py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <h2 id="credential-group-detail-title" className="truncate text-lg font-semibold text-slate-900">
                  {group.name || '未命名分组'}
                </h2>
                <span className="inline-flex items-center rounded border border-slate-200 bg-slate-50 px-2 py-0.5 text-[10px] font-medium text-slate-500">
                  {provider === 'codex' ? 'Codex' : 'Claude'} · {resolvedChannelName || '-'}
                </span>
                {isDefault && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-medium text-emerald-700">
                    <span className="h-1 w-1 rounded-full bg-emerald-500" />
                    默认分组
                  </span>
                )}
              </div>
              <p className="mt-1 truncate font-mono text-xs text-slate-500">{credentialMasked}</p>
            </div>
            <div className="flex flex-shrink-0 items-center gap-2">
			  <Button variant="ghost" size="sm" icon={Pencil} onClick={() => onEdit?.(group)}>编辑</Button>
              <Button variant="ghost" size="sm" onClick={onClose}>关闭</Button>
            </div>
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            <DetailPill icon={WalletCards} tone={limitUSD > 0 ? (usage?.reached ? 'rose' : 'indigo') : 'slate'} variant={variant} className="min-w-32">
              {limitUSD > 0 ? `每日限额 ${formatUSD(limitUSD)}` : '每日限额 不限额'}
            </DetailPill>
            <DetailPill icon={Server} tone="sky" variant={variant}>{endpointCount} 个端点</DetailPill>
            {usage?.reached && <DetailPill icon={Gauge} tone="rose" variant={variant}>已达限额</DetailPill>}
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <DetailMetricCard label="每日成本使用" icon={Gauge} density="compact" wide>
              {usageLoading ? (
                <div className="text-sm text-slate-400">正在读取当日用量…</div>
              ) : usageError ? (
                <div className="break-words text-sm text-rose-600">{usageError}</div>
              ) : (
                <div>
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                    <div><div className="text-[11px] text-slate-400">已使用</div><div className="mt-0.5 font-mono text-sm font-semibold text-slate-800">{formatUSD(usage?.usedUSD)}</div></div>
                    <div><div className="text-[11px] text-slate-400">限额</div><div className="mt-0.5 font-mono text-sm font-semibold text-slate-800">{limitUSD > 0 ? formatUSD(limitUSD) : '不限额'}</div></div>
                    <div><div className="text-[11px] text-slate-400">剩余</div><div className="mt-0.5 font-mono text-sm font-semibold text-slate-800">{limitUSD > 0 ? formatUSD(usage?.remainingUSD) : '不限额'}</div></div>
                    <div><div className="text-[11px] text-slate-400">重置时间</div><div className="mt-0.5 text-xs font-medium text-slate-700">{formatResetAt(usage?.resetAt)}</div></div>
                  </div>
                  {limitUSD > 0 && (
                    <div className="mt-3">
                      <div className="mb-1 flex items-center justify-between text-[11px] text-slate-400">
						<span>当日额度进度</span><span>{usagePercent.toFixed(1)}%</span>
                      </div>
                      <div className="h-2 overflow-hidden rounded-full bg-slate-200">
                        <div className={`h-full rounded-full transition-all ${progressTone}`} style={{ width: `${progress}%` }} />
                      </div>
                    </div>
                  )}
                </div>
              )}
            </DetailMetricCard>

            <DetailMetricCard label="普通计费倍率" icon={Calculator} density="compact" wide>
              <div className="flex flex-wrap gap-1.5">
                {standardMultiplierItems(provider, group).map(([label, value]) => (
                  <DetailChip key={label} mono variant={variant}>
                    <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">{label}</span>
                    {formatDetailMultiplier(value ?? 1)}
                  </DetailChip>
                ))}
              </div>
            </DetailMetricCard>

            <DetailMetricCard label="Long context 倍率" icon={Layers3} density="compact" wide>
              {longContextEnabled ? (
                <div className="flex flex-wrap gap-1.5">
                  {longContextMultiplierItems(group).map(([label, value]) => (
                    <DetailChip key={label} mono tone="indigo" variant={variant}>
                      <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">{label}</span>
                      {formatDetailMultiplier(value ?? 1)}
                    </DetailChip>
                  ))}
                </div>
              ) : <span className="text-sm text-slate-500">未启用，始终使用普通模型价格。</span>}
            </DetailMetricCard>

            <DetailMetricCard label="模型倍率" icon={KeyRound} density="compact" wide>
              {modelMultipliers.length > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                  {modelMultipliers.map((item, index) => (
                    <DetailChip key={`${item.model || index}-${index}`} mono tone="purple" variant={variant}>
                      <span className="mr-1 max-w-[18rem] truncate font-sans text-[10px] font-medium text-slate-600">{item.model || '-'}</span>
                      {formatDetailMultiplier(item.multiplier ?? item.cost_multiplier ?? 1)}
                    </DetailChip>
                  ))}
                </div>
              ) : <span className="text-sm text-slate-500">未配置模型专属倍率。</span>}
            </DetailMetricCard>

            {provider === 'codex' && (
              <DetailMetricCard label="输出计费口径" icon={Clock} density="compact" wide>
                <span className="font-mono text-sm font-semibold text-slate-800">
                  {group.billingOutputTokenSource || group.billing_output_token_source || 'output_tokens'}
                </span>
              </DetailMetricCard>
            )}
          </div>
          <DetailTimestampFooter createdAt={createdAt} updatedAt={updatedAt} density="compact" />
        </div>
      </div>
    </div>
  );
};

export default CredentialGroupDetailModal;
