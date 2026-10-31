import { useCallback, useState } from 'react';
import { ChevronLeft, ChevronRight, FolderOpen, ListFilter, LoaderCircle } from 'lucide-react';
import { CustomSelect, Tooltip } from '@/shared/ui/index.js';
import { count, money, usageDate } from '../usageDashboard.js';
import UsageSessionRow from './UsageSessionRow.jsx';
import UsageProjectDialog from './UsageProjectDialog.jsx';

const sorts = [{ value: 'recent', label: '最近活跃' }, { value: 'tokens', label: 'Token 最多' }, { value: 'cost', label: '费用最高' }, { value: 'active', label: '活跃最长' }];
export default function UsageRows({ data, query, onChange, dataKey, loading, previous }) {
  const [browser, setBrowser] = useState(null);
  const closeBrowser = useCallback(() => setBrowser(null), []);
  const local = query.source === 'local';
  const pages = Math.max(1, Math.ceil((data?.rowCount || 0) / query.pageSize));
  const page = data?.page || 1;
  return <section className="usage-surface usage-rows" aria-label={local ? '会话用量明细' : '代理用量明细'}><header className="usage-section-heading"><h2><ListFilter size={16} />{local ? '会话明细' : '调用明细'}</h2><span className="usage-count">{data?.rowCount || 0}</span>{query.day && <small>{query.day}</small>}{(loading || previous) && <span className="usage-inline-loading" role="status">{loading && <LoaderCircle size={12} />}{loading ? '正在加载明细' : '当前显示上次结果'}{loading && previous && ' · 保留上次结果'}</span>}<div className="usage-row-actions">{local && <button type="button" className="usage-project-open" onClick={() => setBrowser({ session: null })} disabled={loading || previous || !data?.projects.length} aria-haspopup="dialog"><FolderOpen size={14} />按项目查看</button>}<CustomSelect ariaLabel="明细排序" value={query.sort} options={sorts.filter((option) => local || option.value !== 'active')} onChange={(sort) => onChange({ sort })} /></div></header>
    <div className="usage-rows-scroll" data-source={query.source} key={dataKey} role="region" aria-label={local ? '会话明细列表' : '调用明细列表'} tabIndex={0}>
    {local ? (data?.rows || []).map(row => <UsageSessionRow key={row.id} row={row} onOpen={session => setBrowser({ session })} disabled={loading || previous} />) : <><div className="usage-row-head"><span>渠道 / 凭证分组</span><span>模型</span><span>Token</span><span>响应 / 状态</span><span>费用</span><span>最近时间</span></div>
    {(data?.rows || []).map((row) => <div className="usage-detail-row" key={row.id}>
      <div className="usage-row-identity"><span className="usage-provider-dot" data-provider={row.provider} /><div><strong>{row.channel || '未记录渠道'}</strong><Tooltip content={row.id ? <div className="space-y-1"><div>请求 ID</div><div>{row.id}</div></div> : null} className="block min-w-0"><small>{row.group || '未记录分组'}</small></Tooltip></div></div>
      <div className="usage-row-model"><Tooltip content={row.models.length > 1 ? <div className="space-y-1"><div>本次调用使用的模型</div>{row.models.map(model => <div key={model}>{model}</div>)}</div> : null} className="block min-w-0">{row.models[0] || '未记录模型'}</Tooltip>{row.models.length > 1 && <small>另 {row.models.length - 1} 个模型</small>}</div>
      <Tooltip content={<div className="space-y-1"><div>输入 {row.input.toLocaleString()} · 输出 {row.output.toLocaleString()}</div><div>缓存读取 {row.cacheRead.toLocaleString()} · 写入 {row.cacheCreation.toLocaleString()}</div></div>} className="usage-row-number block min-w-0"><strong>{count(row.tokens)}</strong><small>缓存 {count(row.cacheRead)}</small></Tooltip>
      <div className="usage-row-number"><span>{(row.avgDurationMs / 1000).toFixed(1)} 秒</span><small>{row.status === 'completed' ? '已完成' : row.status || '未记录'}</small></div>
      <Tooltip content={row.unpriced ? `${row.unpriced} 次调用尚无定价，当前费用仅包含已计价用量` : null} className="usage-row-cost block min-w-0">{money(row.cost, row.unpriced, false)}</Tooltip><time dateTime={row.lastAt}>{usageDate(row.lastAt)}</time>
    </div>)}</>}
    {!data?.rows.length && <div className="usage-empty-small">当前筛选范围暂无{local ? '会话' : '调用'}记录</div>}
    </div>
    <footer className="usage-pagination"><span>共 {data?.rowCount || 0} 条 · 每页 {query.pageSize} 条</span><div><button type="button" disabled={loading || previous || page <= 1} onClick={() => onChange({ page: page - 1 })} aria-label="上一页"><ChevronLeft size={15} /></button><span>{page} / {pages}</span><button type="button" disabled={loading || previous || page >= pages} onClick={() => onChange({ page: page + 1 })} aria-label="下一页"><ChevronRight size={15} /></button></div></footer>
    {local && browser && <UsageProjectDialog data={data} query={query} initialSession={browser.session} onClose={closeBrowser} />}
  </section>;
}
