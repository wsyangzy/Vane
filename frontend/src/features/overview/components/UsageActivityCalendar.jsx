import { useMemo, useState } from 'react';
import { CalendarDays, LoaderCircle, X } from 'lucide-react';
import { SegmentedTabs, Tooltip } from '@/shared/ui/index.js';
import { buildUsageCalendar } from '../usageHeatmap.js';
import { count } from '../usageDashboard.js';

const levels = ['bg-slate-100', 'bg-indigo-100', 'bg-indigo-300', 'bg-indigo-400', 'bg-indigo-600'];
const weekdays = ['周一', '', '周三', '', '周五', '', '周日'];
export default function UsageActivityCalendar({ data, query, onChange, loading, previous }) {
  const [metric, setMetric] = useState('tokens');
  const items = useMemo(() => (data?.calendar || []).map((day) => ({ ...day, total: day.tokens, requestCount: day.requests })), [data?.calendar]);
  const calendar = useMemo(() => buildUsageCalendar(items, metric), [items, metric]);
  return <section className="usage-surface usage-calendar" aria-label="用量活动分布"><header className="usage-section-heading"><h2><CalendarDays size={16} />活动分布</h2><small>最近 53 周 · 每格一天</small>{query.day && <button type="button" className="usage-filter-chip" aria-label="清除活动分布日期筛选" onClick={() => onChange({ day: '' })}>{query.day}<X size={12} /></button>}{loading && <span className="usage-inline-loading" role="status"><LoaderCircle size={12} />{query.day ? `正在加载 ${query.day}` : '正在更新用量'}{previous && ' · 保留上次结果'}</span>}<SegmentedTabs tabs={[{ key: 'tokens', label: 'Token' }, { key: 'requests', label: '调用' }]} activeTab={metric} onChange={setMetric} className="grid-cols-2" buttonPaddingClass="px-2" /></header>
    <div className="usage-calendar-grid" role="group" aria-label="每日用量">{calendar.months.map((month, index) => <span key={`${month.week}-${month.label}`} className="usage-calendar-month" style={{ gridColumn: `${month.week + 2} / ${(calendar.months[index + 1]?.week ?? 53) + 2}`, gridRow: 1 }}>{month.label}</span>)}
      {weekdays.map((day, index) => <span key={index} className="usage-calendar-weekday" style={{ gridColumn: 1, gridRow: index + 2 }}>{day}</span>)}
      {calendar.days.map((day) => <div key={day.date} className="min-w-0" style={{ gridColumn: day.week + 2, gridRow: day.weekday + 2 }}>{day.future ? <span className="block aspect-square" /> : <Tooltip className="block min-w-0" content={<div className="space-y-1 text-xs"><strong>{day.date}</strong><div>{count(day.data?.tokens)} Token · {count(day.data?.requests)} 次调用</div><div>输入 {count(day.data?.input)} · 输出 {count(day.data?.output)}</div><div>缓存读取 {count(day.data?.cacheRead)} · 写入 {count(day.data?.cacheCreation)}</div><div>{query.day === day.date ? '再次点击清除日期筛选' : '点击查看当日'}</div></div>}><button type="button" aria-label={query.day === day.date ? `清除 ${day.date} 日期筛选` : `查看 ${day.date} 用量`} aria-pressed={query.day === day.date} data-date={day.date} className={`usage-calendar-day ${levels[day.level]}`} onClick={() => onChange({ day: query.day === day.date ? '' : day.date })} /></Tooltip>}</div>)}
    </div><footer className="usage-section-note"><span>{calendar.activeDays} 个活跃日 · 日期范围独立于上方周期，沿用提供商与分类筛选</span><span className="usage-calendar-legend">少{levels.map((level) => <i key={level} className={level} />)}多</span></footer>
  </section>;
}
