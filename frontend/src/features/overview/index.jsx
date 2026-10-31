import { useState } from 'react';
import { AlertCircle, ChartNoAxesCombined, Search, X } from 'lucide-react';
import { CustomSelect, SegmentedTabs } from '@/shared/ui/index.js';
import AutoRefreshControl from '@/features/requests/components/AutoRefreshControl.jsx';
import '@/features/requests/components/toolbar.css';
import useUsageDashboard from './hooks/useUsageDashboard.js';
import { count, defaultUsageQuery, money, nextUsageQuery, periodTabs, projectName, providerLabel, providerTabs, sourceTabs } from './usageDashboard.js';
import UsageSummary from './components/UsageSummary.jsx';
import UsageTrend from './components/UsageTrend.jsx';
import UsageRankings from './components/UsageRankings.jsx';
import UsageActivityCalendar from './components/UsageActivityCalendar.jsx';
import UsageRows from './components/UsageRows.jsx';
import UsageLoading from './components/UsageLoading.jsx';
import './usageDashboard.css';

export default function OverviewPage() {
  const [query, setQuery] = useState(defaultUsageQuery);
  const [interval, setIntervalSeconds] = useState(30);
  const { data, dataKey, previous, error, loading, refresh } = useUsageDashboard(query, interval);
  const local = query.source === 'local';
  const change = (patch) => setQuery((previous) => nextUsageQuery(previous, patch));
  const filtered = query.model || query.project || query.credential || query.day || query.search;
  const options = (values, value) => [...new Set([value, ...values].filter(Boolean))];
  return <div className="usage-workspace animate-fade-in">
    <header className="usage-page-heading"><div className="usage-page-title"><span><ChartNoAxesCombined size={20} /></span><h1>用量概览</h1><p>会话与调用统计</p></div><div className="usage-page-update">{data && <span>更新于 {new Date(data.updatedAt).toLocaleTimeString('zh-CN', { hour12: false })}</span>}{loading && <span role="status">{previous ? '正在加载筛选结果…' : data ? '刷新中…' : '正在索引用量…'}</span>}</div></header>
    <div className="usage-toolbar"><SegmentedTabs tabs={sourceTabs} activeTab={query.source} onChange={(source) => change({ source })} className="usage-segments grid-cols-2" /><SegmentedTabs tabs={periodTabs} activeTab={query.period} onChange={(period) => change({ period })} className="usage-segments grid-cols-5" />
      <div className="usage-refresh"><AutoRefreshControl interval={interval} onIntervalChange={setIntervalSeconds} onManualRefresh={refresh} loading={loading} /></div>
    </div>
    <div className="usage-filterbar"><SegmentedTabs tabs={providerTabs} activeTab={query.provider} onChange={(provider) => change({ provider })} className="usage-segments grid-cols-3" />
      <CustomSelect ariaLabel="筛选用量模型" options={[{ value: '', label: '全部模型' }, ...options(data?.modelOptions || [], query.model).map((model) => ({ value: model, label: model }))]} value={query.model} onChange={(model) => change({ model })} wrapOptions />
      {local ? <CustomSelect ariaLabel="筛选项目文件夹" options={[{ value: '', label: '全部文件夹' }, ...options(data?.projectOptions || [], query.project).map((project) => ({ value: project, label: projectName(project), description: project }))]} value={query.project} onChange={(project) => change({ project })} wrapOptions /> : <CustomSelect ariaLabel="筛选凭证分组" options={[{ value: '', label: '全部凭证分组' }, ...(data?.credentialOptions || []).map((item) => ({ value: item.key, label: item.name, description: providerLabel(item.provider) })), ...(query.credential && !data?.credentialOptions.some((item) => item.key === query.credential) ? [{ value: query.credential, label: '已选凭证分组' }] : [])]} value={query.credential} onChange={(credential) => change({ credential })} wrapOptions />}
      <label className="usage-search"><Search size={14} /><input aria-label="搜索用量" placeholder={local ? '搜索会话标题、项目、模型' : '搜索渠道、分组、模型'} value={query.search} onChange={(event) => change({ search: event.target.value })} /></label>
      {filtered && <button className="usage-clear" type="button" onClick={() => change({ model: '', project: '', credential: '', day: '', search: '' })}><X size={13} />清除筛选</button>}
    </div>
    <p className="usage-source-note">{local ? '本地会话：读取 Claude Code / Codex 保存的用量，包含未经过 Vane 的调用；费用按当前模型定价估算，不代表订阅账单。' : '代理用量：仅统计当前应用环境中经过 Vane 的请求，包含历史归档；同一凭证分组下的全部端点合并统计。'}</p>
    {error && <div role="alert" className="usage-error"><AlertCircle size={16} /><span>{data ? '加载失败，保留上次结果：' : ''}{error}</span><button type="button" onClick={refresh} disabled={loading}>重试</button></div>}
    <div aria-busy={loading} className="usage-content" data-ready={Boolean(data)} data-previous={previous} data-updating={loading && Boolean(data)}>
      {data ? <>
      <UsageSummary data={data} source={query.source} />
      {!!data?.unpricedModels.length && <p className="usage-unpriced"><AlertCircle size={13} /><span>未计价模型：{data.unpricedModels.join('、')}。费用仅累加已知定价，标记“+”表示仍有未计价用量。</span></p>}
      <UsageTrend data={data} query={query} onChange={change} />
      <UsageRankings data={data} query={query} onChange={change} />
      {!!data?.providers.length && <div className="usage-provider-shares" aria-label="客户端用量分布">{data.providers.map((item) => <button type="button" key={item.key} onClick={() => change({ provider: query.provider === item.provider ? '' : item.provider })}><i className="usage-provider-dot" data-provider={item.provider} /><strong>{providerLabel(item.provider)}</strong><span>{count(item.tokens)} Token</span><span>{money(item.cost, item.unpriced, local)}</span></button>)}</div>}
      <UsageActivityCalendar data={data} query={query} onChange={change} loading={loading} previous={previous} />
      <UsageRows data={data} query={query} onChange={change} dataKey={dataKey} loading={loading} previous={previous} />
      </> : loading ? <UsageLoading source={query.source} /> : null}
    </div>
    <p className="usage-footnote">Token 为输入与输出之和，缓存读取和写入单独展示。{local ? '活跃时长累计同一会话内相邻记录不足 5 分钟的间隔；并行会话分别累计。' : '历史记录缺少归属时显示“未记录渠道 / 未记录分组”，不推测凭证归属。'}{data ? ` 统计时区：${data.timezone}。` : ''}</p>
  </div>;
}
