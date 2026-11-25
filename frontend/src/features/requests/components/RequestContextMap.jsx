import { memo, useMemo, useState } from 'react';
import { ChevronDown, PanelsTopLeft } from 'lucide-react';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { buildContextMap, contextTileRows, layoutContextTiles } from '../utils/requestInsights.js';
import { getRequestCacheHitRate } from '../utils/tokenDisplay.js';
import { isRequestInProgress } from '../utils/requestOutcome.js';
import { revealInsight, useInsightPlayback } from './revealInsight.js';
import AnimatedMetric from './AnimatedMetric.jsx';
import './requestInsights.css';

const compactTokens = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 });
const previewRows = 12;

function ContextDetails({ rows, selected, onSelect, live }) {
  const [expanded, setExpanded] = useState(false);
  const details = useMemo(() => rows.flatMap(row => row.entries?.length ? row.entries : [row])
    .sort((a, b) => b.tokens - a.tokens), [rows]);
  const filtered = selected ? details.filter(row => (row.group || row.id) === selected) : details;
  const visible = expanded ? filtered : filtered.slice(0, previewRows);
  const scale = Math.max(1, filtered[0]?.tokens || 0);
  return <>
    {rows.some(row => row.entries?.length) && <div className="request-context-filter">
      <strong>上下文明细</strong>
      <div role="group" aria-label="筛选上下文明细">
        <button type="button" aria-pressed={selected === null} onClick={() => onSelect(null)}>全部</button>
        {rows.map(row => <button key={row.id} type="button" aria-pressed={selected === row.id} onClick={() => onSelect(row.id)}>{row.label}</button>)}
      </div>
    </div>}
    <ul className="request-context-details" aria-label="上下文条目">
      {visible.map(row => <li className="request-context-row" data-tone={row.tone} key={row.id}>
        <span><i aria-hidden="true" /><span>{row.label}</span>{row.items > (row.kind ? 1 : 0) && <small>{row.kind ? `×${row.items}` : `${row.items} 项`}</small>}</span>
        <span className="request-context-meter" aria-hidden="true"><i style={{ width: `${row.tokens / scale * 100}%` }} /></span>
        <strong><AnimatedMetric value={row.tokens} live={live} /></strong>
      </li>)}
    </ul>
    {filtered.length > previewRows && <button type="button" className="request-insight-more" aria-expanded={expanded} onClick={() => setExpanded(value => !value)}>
      <ChevronDown size={13} className={expanded ? 'rotate-180' : ''} />{expanded ? '收起明细' : `查看全部 ${filtered.length} 项`}
    </button>}
  </>;
}

function ContextTiles({ data, selected, onSelect, capacity, animate, waiting }) {
  const tiles = useMemo(() => layoutContextTiles(contextTileRows(data.rows), data.total), [data.rows, data.total]);
  const used = data.input || data.total;
  const free = Math.max(0, capacity - used);
  const usedPercent = free > 0 ? used / capacity * 100 : 100;
  const mapWidth = free > 0 ? Math.max(30, usedPercent) : 100;
  return <div className="request-context-contents">
    <div className="request-context-canvas" data-live={Boolean(animate)} ref={revealInsight}>
      {tiles.length ? <div className="request-context-map" role="group" aria-label="输入上下文组成" style={{ width: `${mapWidth}%` }}>
        {tiles.map((tile, index) => {
          const label = `${tile.label} · ${tile.tokens.toLocaleString()} token`;
          return <Tooltip key={tile.id} asChild content={<div className="space-y-1 text-left">
            <div className="text-slate-300">{tile.label}</div>
            <div className="tabular-nums"><strong className="text-sm font-semibold">{tile.tokens.toLocaleString()}</strong><span className="ml-1 text-slate-300">token</span></div>
          </div>}>
          <button
            type="button"
            data-tone={tile.tone}
            aria-label={label}
            aria-pressed={selected === (tile.group || tile.id)}
            style={{ left: `${tile.x}%`, top: `${tile.y}%`, width: `${tile.width}%`, height: `${tile.height}%`, '--insight-step': index }}
            onClick={() => onSelect(tile.group || tile.id)}
          >
            <span className="request-context-tile-label" aria-hidden="true">
              <strong>{tile.label}</strong>
              <span><AnimatedMetric value={tile.tokens} live={animate} format="compact" /></span>
            </span>
          </button>
          </Tooltip>;
        })}
      </div> : waiting && <div className="request-context-waiting">正在等待输入上下文</div>}
      {free > 0 && <Tooltip asChild content={<div className="space-y-1 text-left">
        <div className="text-slate-300">剩余空间</div>
        <div className="tabular-nums"><strong className="text-sm font-semibold">{free.toLocaleString()}</strong><span className="ml-1 text-slate-300">token · {(100 - usedPercent).toFixed(1)}% 可用</span></div>
      </div>}>
      <div className="request-context-free" tabIndex={0} aria-label={`剩余 ${free.toLocaleString()} token`}>
        <span className="request-context-tile-label">
          <strong>剩余空间</strong>
          <span>{compactTokens.format(free)} · {(100 - usedPercent).toFixed(1)}%</span>
        </span>
      </div>
      </Tooltip>}
    </div>
    {tiles.length > 0 && <ul className="request-context-legend" aria-label="上下文分类">
      {data.rows.map(row => <li key={row.id}>
        <button type="button" data-tone={row.tone} aria-pressed={selected === row.id} onClick={() => onSelect(row.id)}>
          <i />{row.label}<span><AnimatedMetric value={row.tokens} live={animate} format="compact" /></span>
        </button>
      </li>)}
      {free > 0 && <li><span><i />剩余空间<span>{compactTokens.format(free)}</span></span></li>}
    </ul>}
    {tiles.length > 0 && <ContextDetails rows={data.rows} selected={selected} onSelect={onSelect} live={animate} />}
  </div>;
}

export default memo(function RequestContextMap({ request, animate: animateOnOpen }) {
  const [playbackRef, animate] = useInsightPlayback(animateOnOpen);
  const [open, setOpen] = useState(true);
  const [selected, setSelected] = useState(null);
  const [capacityInput, setCapacityInput] = useState('');
  const data = useMemo(() => buildContextMap(request), [request]);
  const onSelect = id => setSelected(current => current === id ? null : id);
  const displayTotal = data.input || data.total;
  const live = isRequestInProgress(request);
  const source = data.calibrated ? '按上游用量校准' : data.estimated ? '组成估算' : data.input ? '已收到上游用量' : live ? '等待上下文' : '未记录';
  const cacheHitRate = request.billingRuleSnapshot?.live_input_estimated ? null : getRequestCacheHitRate(request);
  const cachedTokens = Math.max(0, Number(request.cacheReadTokens) || 0);
  const capacity = Number(capacityInput);
  const windowSize = Number.isSafeInteger(capacity) && capacity > 0 && (!data.partial || data.input > 0) ? capacity : 0;
  return <section className="request-insight" aria-label="上下文窗口">
    <button type="button" className="request-insight-heading request-context-toggle" aria-expanded={open} onClick={() => setOpen(value => !value)}>
      <ChevronDown size={14} /><PanelsTopLeft size={15} /><span className="request-context-title">上下文窗口</span>
      <span className="request-context-source">{source}</span>
      {(live || displayTotal > 0) && <strong><AnimatedMetric value={displayTotal} live={animate} format="compact" /> token</strong>}
    </button>
    {open && (live || data.rows.length ? <>
      <div className="request-context-summary" data-cache={cacheHitRate != null} ref={playbackRef}>
        <div>
          <span>输入上下文</span>
          <p><strong><AnimatedMetric value={displayTotal} live={animate} /></strong>{windowSize > 0 ? ` / ${windowSize.toLocaleString()}` : ''} token</p>
          <small>{data.input ? '上游报告用量' : data.rows.length ? '请求体估算' : '等待输入用量'} · {windowSize > 0 ? `已用 ${(displayTotal / windowSize * 100).toFixed(1)}%` : '窗口容量未记录'}</small>
        </div>
        {cacheHitRate != null && <div className="request-context-cache">
          <span>提示词缓存</span>
          <p><strong><AnimatedMetric value={cacheHitRate} live={animate} format="percent" /></strong> 命中</p>
          <div className="request-context-meter" aria-hidden="true"><i style={{ width: `${Math.min(cacheHitRate * 100, 100)}%` }} /></div>
          <small><AnimatedMetric value={cachedTokens} live={animate} /> 缓存读取 · <AnimatedMetric value={Math.max(0, data.input - cachedTokens)} live={animate} /> 未命中</small>
        </div>}
        {(!data.partial || data.input > 0) && <label>参考窗口容量<input inputMode="numeric" maxLength={12} value={capacityInput} placeholder="未设置" onChange={event => setCapacityInput(event.target.value.replace(/[^0-9]/g, ''))} /></label>}
      </div>
      <ContextTiles data={data} selected={selected} onSelect={onSelect} capacity={windowSize} animate={animate} waiting={live} />
      <p className="request-insight-note">
        {data.calibrated ? '条目占比按请求体估算，总量与上游报告一致。' : data.rows.length > 0 && (data.estimated ? '条目用量按请求体估算。' : '此记录仅包含输入与缓存用量。')}
        {data.partial && '图中只包含此次发送的会话内容。'}
        {data.turns > 0 && `本次请求体包含 ${data.turns} 轮会话。`}
        {windowSize > 0 && displayTotal / windowSize < .3 && '已用区适当放大，占用率以上方数值为准。'}
      </p>
    </> : <p className="request-insight-empty">{data.partial ? '此请求引用上游会话，当前请求体无可分类的上下文。' : '此请求暂无可展示的输入上下文。'}</p>)}
  </section>;
});
