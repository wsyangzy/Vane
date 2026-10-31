import { useMemo, useState } from 'react';
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { ChartNoAxesColumnIncreasing, X } from 'lucide-react';
import { SegmentedTabs } from '@/shared/ui/index.js';
import { count, duration, money, usageChartDays } from '../usageDashboard.js';

const metrics = [{ key: 'tokens', label: 'Token' }, { key: 'output', label: '输出' }, { key: 'requests', label: '调用' }, { key: 'sessions', label: '会话' }, { key: 'cost', label: '费用' }, { key: 'activeSeconds', label: '活跃' }];

function DayTooltip({ active, payload, local }) {
  const day = payload?.[0]?.payload;
  if (!active || !day) return null;
  return <div className="usage-chart-tooltip"><strong>{day.date}{day.endDate !== day.date ? ` 至 ${day.endDate}` : ''}</strong>
    <span>{count(day.tokens)} Token · {money(day.cost, day.unpriced, local)}</span>
    <span>{count(day.requests)} 次调用{local ? ` · ${count(day.sessions)} 个会话 · ${duration(day.activeSeconds)}` : ''}</span>
    {day.date === day.endDate && <small>点击查看当日用量</small>}
  </div>;
}

export default function UsageTrend({ data, query, onChange }) {
  const [metric, setMetric] = useState('tokens');
  const local = query.source === 'local';
  const selectedMetric = !local && ['sessions', 'activeSeconds'].includes(metric) ? 'tokens' : metric;
  const days = useMemo(() => usageChartDays(data?.days || [], selectedMetric), [data?.days, selectedMetric]);
  const tick = (value) => selectedMetric === 'cost' ? `$${count(value)}` : selectedMetric === 'activeSeconds' ? `${Math.round(value / 3600)}h` : count(value);
  return <section className="usage-surface usage-trend" aria-label="每日趋势">
    <header className="usage-section-heading"><h2><ChartNoAxesColumnIncreasing size={16} />每日趋势</h2><SegmentedTabs className={local ? 'grid-cols-6' : 'grid-cols-4'} buttonPaddingClass="px-2" tabs={metrics.filter(({ key }) => local || !['sessions', 'activeSeconds'].includes(key))} activeTab={selectedMetric} onChange={setMetric} />
      {query.day && <button type="button" className="usage-filter-chip" onClick={() => onChange({ day: '' })}>{query.day}<X size={12} /><span className="sr-only">清除日期筛选</span></button>}
    </header>
    <div className="usage-trend-chart"><ResponsiveContainer width="100%" height="100%"><BarChart data={days} margin={{ top: 12, right: 6, left: -18, bottom: 0 }} onClick={(event) => { const day = event?.activePayload?.[0]?.payload || event?.activeTooltipIndex != null && days[event.activeTooltipIndex]; if (day?.date === day?.endDate && day) onChange({ day: query.day === day.date ? '' : day.date }); }}>
      <CartesianGrid vertical={false} stroke="#e2e8f0" strokeDasharray="3 4" />
      <XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: '#94a3b8' }} minTickGap={42} />
      <YAxis tickFormatter={tick} axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: '#94a3b8' }} width={62} />
      <Tooltip cursor={{ fill: '#f1f5f9', radius: 4 }} content={<DayTooltip local={local} />} />
      {selectedMetric === 'tokens' ? <><Bar dataKey="input" stackId="usage" fill="#c7d2fe" maxBarSize={42} isAnimationActive={false} /><Bar dataKey="output" stackId="usage" fill="#6366f1" radius={[4, 4, 0, 0]} maxBarSize={42} isAnimationActive={false} /></> : <Bar dataKey="value" fill="#818cf8" radius={[4, 4, 0, 0]} maxBarSize={42} isAnimationActive={false} />}
    </BarChart></ResponsiveContainer></div>
    <footer className="usage-section-note"><span>{data ? `${data.from} — ${data.to}` : '正在读取统计周期'}{days.length < (data?.days.length || 0) ? ' · 较长周期已按连续日期合并展示' : ''}</span><span>{selectedMetric === 'tokens' ? '浅色为输入 · 深色为输出 · 缓存单独统计' : local ? '按会话用量发生日期统计' : '按代理请求开始日期统计'}</span></footer>
  </section>;
}
