import { useState } from 'react';
import { Boxes, ChevronDown, ChevronUp, FolderOpen, KeyRound } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { count, money, providerLabel } from '../usageDashboard.js';

function Ranking({ title, icon: Icon, items, selected, onSelect, local, kind }) {
  const [expanded, setExpanded] = useState(false);
  const maximum = Math.max(1, ...items.map((item) => item.tokens));
  const visible = expanded ? items : items.slice(0, 6);
  return <section className="usage-surface usage-ranking" aria-label={`${title}用量排行`}><header className="usage-section-heading"><h2><Icon size={16} />{title}</h2><span className="usage-count">{items.length}</span><small>按 Token 排序</small></header>
    <div className="usage-ranking-list">{visible.map((item) => <button type="button" className="usage-ranking-row" data-selected={selected === (kind === 'models' ? item.name : item.key)} key={item.key} onClick={() => onSelect(item)} aria-pressed={selected === (kind === 'models' ? item.name : item.key)}>
      <Tooltip content={kind === 'projects' && item.key ? <div className="space-y-1"><div>项目目录</div><div>{item.key}</div></div> : kind === 'credentials' ? <div className="space-y-1"><div>{providerLabel(item.provider)}</div><div>渠道：{item.channel || '未记录渠道'}</div><div>凭证分组：{item.group || '未记录分组'}</div></div> : null} className="usage-ranking-name flex"><span>{item.name}</span>{kind === 'models' && <small>{providerLabel(item.provider)}</small>}</Tooltip>
      <span className="usage-ranking-bar"><i style={{ width: `${Math.max(1, item.tokens / maximum * 100)}%` }} /></span>
      <span className="usage-ranking-tokens">{count(item.tokens)}</span><span className="usage-ranking-cost">{money(item.cost, item.unpriced, local)}</span>
    </button>)}{!items.length && <div className="usage-empty-small">当前筛选范围暂无用量</div>}</div>
    {items.length > 6 && <button type="button" className="usage-expand" onClick={() => setExpanded(!expanded)}>{expanded ? <ChevronUp size={13} /> : <ChevronDown size={13} />}{expanded ? '收起' : `查看全部 ${items.length} 项`}</button>}
  </section>;
}

export default function UsageRankings({ data, query, onChange }) {
  const local = query.source === 'local';
  return <div className="usage-rankings"><Ranking title={local ? '项目' : '凭证分组'} icon={local ? FolderOpen : KeyRound} items={(local ? data?.projects : data?.credentials) || []} kind={local ? 'projects' : 'credentials'} local={local} selected={local ? query.project : query.credential} onSelect={(item) => onChange(local ? { project: query.project === item.key ? '' : item.key } : { credential: query.credential === item.key ? '' : item.key })} />
    <Ranking title="模型" icon={Boxes} items={data?.models || []} kind="models" local={local} selected={query.model} onSelect={(item) => onChange(query.model === item.name && query.provider === item.provider ? { model: '' } : { provider: item.provider, model: item.name })} />
  </div>;
}
