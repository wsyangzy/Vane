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
import { Button } from '@/shared/ui/index.js';
import { getCredentialGroupDailyUsage } from '@/services/wails/index.js';
import {
  ResourceDetailShell,
  DetailChip,
  DetailMetricCard,
  DetailPill,
  DetailTimestampFooter
} from '../endpoints/detailParts.jsx';
import { formatDetailMultiplier, getDetailTimestamp } from '../formatters.js';

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
	const billingRules = group?.billingRules || group?.billing_rules || [];
	const combinationBillingEnabled = provider === 'codex' && billingRules.length > 0;
  const longContextEnabled = !Boolean(group?.disableLongContextPricing ?? group?.disable_long_context_pricing);

  useEffect(() => {
    let cancelled = false;
    const loadUsage = async () => {
      setUsageLoading(true);
      setUsageError('');
      setUsage(null);
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

  return (
    <ResourceDetailShell
      title={group.name || '未命名分组'}
      eyebrow={`${provider === 'codex' ? 'Codex' : 'Claude'} · ${resolvedChannelName || '渠道'} · 凭证分组`}
      subtitle={credentialMasked}
      icon={KeyRound}
      tone="group"
      onClose={onClose}
      actions={<Button variant="ghost" size="sm" icon={Pencil} onClick={() => onEdit?.(group)}>编辑</Button>}
      summary={<>
        {isDefault && <DetailPill tone="emerald" variant={variant}>默认分组</DetailPill>}
        <DetailPill icon={Server} tone="slate" variant={variant}>{endpointCount} 个端点</DetailPill>
        <DetailPill icon={WalletCards} tone={usage?.reached ? 'rose' : 'slate'} variant={variant}>{limitUSD > 0 ? `每日限额 ${formatUSD(limitUSD)}` : '每日不限额'}</DetailPill>
        {usage?.reached && <DetailPill icon={Gauge} tone="rose" variant={variant}>已达限额</DetailPill>}
      </>}
    >
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <DetailMetricCard label="每日成本使用" icon={Gauge} density="compact" wide>
              {usageLoading ? (
                <div className="text-sm text-slate-400">正在读取当日用量…</div>
              ) : usageError ? (
                <div className="break-words text-sm text-rose-600">{usageError}</div>
              ) : (
                <div>
                  <dl className="rd-usage-values">
                    <div><dt>今日已用</dt><dd>{formatUSD(usage?.usedUSD)}</dd></div>
                    <div><dt>每日限额</dt><dd>{limitUSD > 0 ? formatUSD(limitUSD) : '不限额'}</dd></div>
                    <div><dt>剩余额度</dt><dd>{limitUSD > 0 ? formatUSD(usage?.remainingUSD) : '不限额'}</dd></div>
                  </dl>
                  <div className="rd-reset"><span>按当前计费配置统计成本</span><span>下次重置 {formatResetAt(usage?.resetAt)}</span></div>
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

			{combinationBillingEnabled ? (
			  <DetailMetricCard label="组合计费规则" icon={Calculator} density="compact" wide>
				<div className="divide-y divide-slate-100">
				  {billingRules.map((rule, index) => {
					const fixed = rule.mode === 'fixed';
					const selector = [rule.model || '全部模型', rule.serviceTier || rule.service_tier || '全部档位', rule.context || '全部上下文'].join(' / ');
					const values = fixed
					  ? `输入 $${rule.inputPrice ?? rule.input_price ?? 0} · 输出 $${rule.outputPrice ?? rule.output_price ?? 0} · 写入 $${rule.cacheCreationPrice ?? rule.cache_creation_price ?? 0} · 读取 $${rule.cacheReadPrice ?? rule.cache_read_price ?? 0}`
					  : `总 ${formatDetailMultiplier(rule.costMultiplier ?? rule.cost_multiplier ?? 1)} · 输入 ${formatDetailMultiplier(rule.inputCostMultiplier ?? rule.input_cost_multiplier ?? 1)} · 输出 ${formatDetailMultiplier(rule.outputCostMultiplier ?? rule.output_cost_multiplier ?? 1)}`;
					return (
					  <div key={`${selector}-${index}`} className="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2 first:pt-0 last:pb-0">
						<span className="min-w-0 break-all text-xs font-medium text-slate-700">{selector}</span>
						<span className="min-w-0 break-all font-mono text-[11px] text-slate-500">{fixed ? '固定单价' : '倍率'} · {values}</span>
					  </div>
					);
				  })}
				</div>
			  </DetailMetricCard>
			) : (
			  <>
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

            {/* Long context 计费为 Codex 独有，Claude 侧不提供该入口 */}
            {provider === 'codex' && (
              <DetailMetricCard label="长上下文计费" icon={Layers3} density="compact">
                {longContextEnabled ? (
                  <div className="flex flex-wrap gap-1.5">
                    {longContextMultiplierItems(group).map(([label, value]) => (
                      <DetailChip key={label} mono tone="indigo" variant={variant}>
                        <span className="mr-1 font-sans text-[10px] font-medium text-slate-500">{label}</span>
                        {formatDetailMultiplier(value ?? 1)}
                      </DetailChip>
                    ))}
                  </div>
                ) : <span className="rd-empty">未启用，沿用普通模型价格。</span>}
              </DetailMetricCard>
            )}

            <DetailMetricCard label="模型专属倍率" icon={KeyRound} density="compact">
              {modelMultipliers.length > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                  {modelMultipliers.map((item, index) => (
                    <DetailChip key={`${item.model || index}-${index}`} mono tone="purple" variant={variant}>
                      <span className="mr-1 min-w-0 font-sans text-[10px] font-medium text-slate-600 [overflow-wrap:anywhere]">{item.model || '-'}</span>
                      {formatDetailMultiplier(item.multiplier ?? item.cost_multiplier ?? 1)}
                    </DetailChip>
                  ))}
                </div>
              ) : <span className="rd-empty">未配置，沿用分组计费倍率。</span>}
            </DetailMetricCard>
			  </>
			)}

            {provider === 'codex' && (
              <DetailMetricCard label="输出计费口径" icon={Clock} density="compact" wide>
                <div className="rd-billing-source">
                  <strong>{{ output_tokens: '按总输出计费', reasoning_tokens: '按推理输出计费', visible_output_tokens: '按可见补全计费' }[group.billingOutputTokenSource || group.billing_output_token_source || 'output_tokens'] || '自定义输出口径'}</strong>
                  <code>{group.billingOutputTokenSource || group.billing_output_token_source || 'output_tokens'}</code>
                </div>
              </DetailMetricCard>
            )}
          </div>
      <DetailTimestampFooter createdAt={createdAt} updatedAt={updatedAt} density="compact" />
    </ResourceDetailShell>
  );
};

export default CredentialGroupDetailModal;
