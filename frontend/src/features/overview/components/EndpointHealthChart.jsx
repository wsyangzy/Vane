import { useCallback, useEffect, useRef, useState } from 'react';
import { KeyRound, RefreshCw } from 'lucide-react';
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts';
import { SegmentedTabs, Tooltip as UiTooltip } from '@/shared/ui/index.js';
import { listRoutingProfiles } from '@/services/wails/dispatch.js';
import { groupStateOptions, summarizeCredentialGroups } from '../credentialGroupStats.js';

const providers = [{ key: 'claude', label: 'Claude' }, { key: 'codex', label: 'Codex' }];

export default function CredentialGroupHealthChart() {
  const [provider, setProvider] = useState('claude');
  const [summaries, setSummaries] = useState({});
  const [errors, setErrors] = useState({});
  const [refreshing, setRefreshing] = useState(false);
  const generation = useRef({ value: 0 });
  const loadData = useCallback(async () => {
    const current = ++generation.current.value;
    setRefreshing(true);
    try {
      const workspace = await listRoutingProfiles(provider);
      if (current !== generation.current.value) return;
      setSummaries((previous) => ({ ...previous, [provider]: summarizeCredentialGroups(workspace) }));
      setErrors((previous) => ({ ...previous, [provider]: '' }));
    } catch (error) {
      if (current !== generation.current.value) return;
      setErrors((previous) => ({ ...previous, [provider]: error?.message || '分组状态读取失败' }));
    } finally {
      if (current === generation.current.value) setRefreshing(false);
    }
  }, [provider]);

  useEffect(() => {
    const pending = generation.current;
    const initial = setTimeout(loadData, 0);
    const timer = setInterval(loadData, 30000);
    const update = (event) => {
      if (['endpoint_health', 'endpointHealth', 'credential_group_health'].includes(event.detail?.chart_type)) loadData();
    };
    document.addEventListener('chartUpdate', update);
    return () => {
      ++pending.value;
      clearTimeout(initial);
      clearInterval(timer);
      document.removeEventListener('chartUpdate', update);
    };
  }, [loadData]);

  const summary = summaries[provider];
  const error = errors[provider];
  const chartData = groupStateOptions.map((option) => ({ ...option, value: summary?.counts[option.key] || 0 }));
  return <section aria-label="凭证分组状态" className="flex h-full min-w-0 flex-col rounded-2xl border border-slate-200/60 bg-white p-6 shadow-sm">
    <div className="mb-1 flex flex-wrap items-start justify-between gap-2">
      <div className="flex items-center gap-2"><div className="rounded-md bg-emerald-50 p-1.5 text-emerald-500"><KeyRound size={16} /></div><h3 className="font-semibold text-slate-900">凭证分组状态</h3></div>
      <div className="flex items-center gap-2"><SegmentedTabs tabs={providers} activeTab={provider} onChange={setProvider} className="grid-cols-2" buttonPaddingClass="px-2 py-1 text-xs" />
        <UiTooltip content="刷新分组状态"><button type="button" aria-label="刷新分组状态" disabled={refreshing} onClick={loadData} className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600 disabled:opacity-50"><RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} /></button></UiTooltip>
      </div>
    </div>
    <p className="mb-3 text-xs leading-5 text-slate-500">覆盖全部渠道，每个凭证分组计一次；任一路径可用即计入可用分组。</p>
    {error && <p role="alert" className="mb-2 rounded-lg bg-rose-50 px-3 py-2 text-xs leading-5 text-rose-600 [overflow-wrap:anywhere]">{summary ? '刷新失败，保留上次状态：' : ''}{error}</p>}
    {!summary ? <div className="flex min-h-[220px] flex-1 items-center justify-center text-sm text-slate-400">{refreshing ? '正在读取分组状态…' : '暂无分组状态'}</div> : <>
      <div className="relative h-[190px] shrink-0">
        {summary.total > 0 && <ResponsiveContainer width="100%" height="100%"><PieChart><Pie data={chartData} startAngle={180} endAngle={0} innerRadius={58} outerRadius={80} cx="50%" cy="70%" paddingAngle={2} dataKey="value" nameKey="label" isAnimationActive={false}>{chartData.map((item) => <Cell key={item.key} fill={item.color} stroke="none" />)}</Pie><Tooltip formatter={(value) => `${value} 个凭证分组`} /></PieChart></ResponsiveContainer>}
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center pt-7"><span className="text-3xl font-semibold tabular-nums text-slate-900">{summary.usable}<span className="text-base text-slate-400"> / {summary.total}</span></span><span className="mt-1 text-xs text-slate-500">可用 / 全部分组</span><span className="mt-1 text-[11px] text-slate-400">{summary.channels.length} 个渠道{summary.total === 0 ? ' · 尚无凭证分组' : ''}</span></div>
      </div>
      <div className="grid grid-cols-2 gap-x-5 gap-y-2 border-t border-slate-100 pt-3 min-[1200px]:grid-cols-4">{chartData.map((item) => <div key={item.key} className="flex min-w-0 items-center justify-between gap-2 text-xs"><span className="inline-flex items-center gap-1.5 whitespace-nowrap text-slate-500"><i className="h-2 w-2 rounded-full" style={{ background: item.color }} />{item.label}</span><span className="font-semibold tabular-nums text-slate-700">{item.value}</span></div>)}</div>
      <div className="mt-3 max-h-[180px] overflow-y-auto border-t border-slate-100 pt-1">{summary.channels.map((channel) => <div key={channel.id} className="flex min-w-0 items-center gap-3 border-b border-slate-50 py-2 last:border-0"><span className="min-w-0 flex-1 truncate text-xs font-medium text-slate-600">{channel.name}</span><span className="shrink-0 text-[11px] tabular-nums text-slate-400">{channel.groups.length} 分组</span><span className="inline-flex shrink-0 gap-2" aria-label={`${channel.name}分组状态`}>{groupStateOptions.filter((option) => channel.counts[option.key]).map((option) => <UiTooltip asChild content={`${option.label} ${channel.counts[option.key]} 个分组`} key={option.key}><span key={option.key} className="inline-flex items-center gap-1 text-[11px] tabular-nums text-slate-500"><i className="h-1.5 w-1.5 rounded-full" style={{ background: option.color }} />{channel.counts[option.key]}</span></UiTooltip>)}</span></div>)}</div>
      <p className="mt-3 text-[10px] leading-4 text-slate-400">部分可用：分组内仍有可用路径。暂不可用：冷却、需修复或资源异常。待配置：缺少端点或凭证。此处不统计方案参与、余额或订阅额度。</p>
    </>}
  </section>;
}
