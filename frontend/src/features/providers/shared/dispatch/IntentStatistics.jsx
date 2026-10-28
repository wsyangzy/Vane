import Tooltip from '@/shared/ui/Tooltip.jsx';
import { ChartNoAxesColumn, ChevronDown, CircleHelp, RefreshCw } from 'lucide-react';
import './profileDetails.css';

export default function IntentStatistics({ statistics, intents, saved, loaded, error, busy, onRefresh }) {
  return <section className="dp-card dp-statistics" aria-label="分类统计">
    <header className="dp-card-header"><div className="dp-heading"><ChartNoAxesColumn size={15} /><h4>分类统计</h4><span>最近 100 次调度</span></div><Tooltip asChild content="刷新分类统计"><button type="button" className="dp-icon-button" aria-label="刷新分类统计" disabled={busy || !saved} onClick={onRefresh}><RefreshCw size={13} /></button></Tooltip></header>
    {!saved ? <p className="dp-statistics-state">保存方案后显示本方案的分类统计。</p> : error ? <p role="alert" className="dp-statistics-state text-amber-700">分类记录读取失败：{error}</p> : !loaded ? <p className="dp-statistics-state">正在读取分类记录…</p> : <>
      <dl className="dp-statistics-values"><div><dt>分类命中</dt><dd>{statistics.hits}<small>次</small></dd></div><div data-warning={statistics.failures > 0}><dt>分类失败</dt><dd>{statistics.failures}<small>次</small></dd></div><div><dt>分类费用</dt><dd className="dp-statistics-cost">${statistics.cost.toFixed(6)}</dd></div></dl>
      {!!intents.length && <div className="dp-intent-hits">{intents.map((intent, index) => <span key={intent.rowKey || `${intent.id}/${index}`}><span>{intent.name || intent.id || '未命名意图'}</span><b>{Object.hasOwn(statistics.intents, intent.id) ? statistics.intents[intent.id] : 0}<small> 次命中</small></b></span>)}</div>}
      <details className="dp-statistics-help"><summary><span>本方案 {statistics.matching} 条记录 · {statistics.calls} 次非缓存分类</span><span><CircleHelp size={12} />统计口径<ChevronDown size={11} /></span></summary><p>来源：当前服务最近 100 次调度记录，已读取 {statistics.sampled} 条，其中本方案 {statistics.matching} 条。仅统计 {statistics.calls} 次非缓存分类结果；缓存、轮次复用及未执行分类的跳过不计数、不重复计费。回退结果计为失败。费用为记录中的分类费用合计，含失败结果已记录的用量；此处不包含分类预览。</p></details>
    </>}
  </section>;
}
