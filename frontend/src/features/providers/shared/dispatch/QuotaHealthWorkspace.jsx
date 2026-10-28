import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useState } from 'react';
import { Activity, CircleAlert, CircleHelp, Clock3, Database, FlaskConical, History, KeyRound, Layers, Pencil, Plus, Power, RefreshCw, Search, Server, Settings2, ShieldCheck, Wallet } from 'lucide-react';
import { Button, SegmentedTabs } from '@/shared/ui/index.js';
import { candidateHealthPresentation, quotaPresentation } from './dispatchState.js';
import { filterQuotaHealthChannels, healthGroupSummary, quotaGroupSummary, quotaHealthChannels } from './quotaHealthState.js';
import HealthCardGrid from './HealthCardGrid.jsx';
import ManualBalanceDialog from './ManualBalanceDialog.jsx';
import { formatUsd } from './manualBalanceState.js';
import './quotaHealth.css';

const number = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 });
const date = (value) => value && new Date(value).getTime() > 0 ? new Date(value).toLocaleString() : '—';

function ResourceCard({ channel, kind, children }) {
  return <article className="qh-channel" aria-label={`${channel.name} ${kind}`}><header className="qh-channel-header"><span className="qh-channel-icon"><Server size={15} /></span><h4>{channel.name}</h4><span>{channel.groups.length} 个分组</span></header><div className="qh-channel-body">{children}</div></article>;
}

function WorkspaceSection({ title, icon: Icon, count, tools, className = '', children }) {
  return <section className={`qh-section qh-overview-section ${className}`} aria-label={title}>
    <header className="qh-section-heading"><div className="qh-section-title"><Icon size={16} /><h3>{title}</h3><span className="qh-count">{count}</span></div>{tools}</header>
    <div className="qh-section-content">{children}</div>
  </section>;
}

function ScopeSwitch({ label, value, onChange, options }) {
  return <div role="group" aria-label={label} className="qh-scope-control"><SegmentedTabs tabs={options.map(([key, title, Icon]) => ({ key, label: <><Icon size={12} />{title}</> }))} activeTab={value} onChange={onChange} className="grid-cols-2 qh-scope-tabs" buttonPaddingClass="px-2" /></div>;
}

const quotaScopes = [['subscriptions', '订阅账号', Database], ['all', '全部资源', Layers]];
const healthScopes = [['issues', '异常', CircleAlert], ['all', '全部', Layers]];

function ResourceGroup({ label, name, summary, tools, compact = false, children }) {
  return <section className={`qh-group qh-resource-group${compact ? ' qh-resource-group-compact' : ''}`} aria-label={label}>
    <div className="qh-resource-group-heading"><div className="qh-group-heading"><KeyRound size={13} /><h5>{name}</h5></div>{summary}{tools && <div className="qh-balance-actions">{tools}</div>}</div>
    {children}
  </section>;
}

function QuotaWindow({ quota, busy, onRefresh }) {
  const display = quotaPresentation(quota);
  return <div className="qh-window" data-stale={display.stale}>
    <div className="qh-window-heading"><span>{quota.windowName || '默认窗口'}</span><Tooltip asChild content="刷新此模型额度"><button type="button" className="qh-icon-button" aria-label={`刷新额度 ${quota.channelId}/${quota.groupId}/${quota.modelScope}/${quota.windowName}`} disabled={busy} onClick={() => onRefresh(quota.channelId, quota.groupId, quota.modelScope)}><RefreshCw size={12} /></button></Tooltip></div>
    <div className="qh-window-value"><strong>{display.label}</strong><span>已使用</span></div>
    <p className="qh-remaining">剩余 <b>{quota.remaining == null ? '—' : number.format(quota.remaining)}</b> / {quota.limit == null ? '—' : number.format(quota.limit)}</p>
    <div className="qh-window-status"><span className="qh-badge" data-tone={display.known ? 'success' : 'muted'}>{display.status}</span></div>
    {quota.error && <p className="qh-query-error">查询失败：{quota.error}</p>}
    <p className="qh-reset"><Clock3 size={11} />重置 {date(quota.resetAt)}</p>
    <details className="qh-snapshot-details"><summary>来源与快照时间</summary><dl><dt>来源</dt><dd>{quota.source || '未知'}</dd><dt>观察</dt><dd>{date(quota.observedAt)}</dd><dt>有效至</dt><dd>{date(quota.expiresAt)}</dd></dl></details>
  </div>;
}

function QuotaGroup({ channel, group, model, busy, onRefresh }) {
  const scopes = [...new Set(group.quotas.map((quota) => quota.modelScope))];
  const summary = quotaGroupSummary(group.quotas);
  return <ResourceGroup label={`${channel.name} / ${group.name} 额度`} name={group.name}
    summary={<span className="qh-group-overview"><span className="qh-summary-value" data-tone={summary.tone}>{summary.label}</span><small>{summary.description}</small>{summary.issue && <span className="qh-summary-warning">{summary.issue}</span>}</span>}
    tools={<Tooltip asChild content={model.trim() ? `查询 ${model.trim()} 的额度` : '查询订阅额度'}><button type="button" className="qh-icon-button" disabled={busy} aria-label={`查询分组额度 ${channel.name} / ${group.name}`} onClick={() => onRefresh(channel.id, group.id, model)}><RefreshCw size={13} /></button></Tooltip>}>
    {scopes.map((scope) => <div className="qh-model-scope" key={scope}><p className="qh-model-name">{scope || '全部模型'}</p><div className="qh-windows">{group.quotas.filter((quota) => quota.modelScope === scope).map((quota) => <QuotaWindow key={`${quota.accountIdHash}/${quota.windowName}`} quota={quota} busy={busy} onRefresh={onRefresh} />)}</div></div>)}
    {!group.quotas.length && <p className="qh-quota-empty"><CircleHelp size={13} /><span>暂未获取订阅额度，查询失败时保留历史快照。</span></p>}
  </ResourceGroup>;
}

function ManualBalanceGroup({ channel, group, busy, onAction }) {
  const balance = group.balance;
  const enabled = balance?.enabled;
  const open = (action) => onAction({ channel, group, action });
  const summary = <span className="qh-group-overview"><span className="qh-group-kind" data-warning={Boolean(balance?.bindingChanged)}>{enabled ? '手动余额' : balance?.bindingChanged ? '凭证变更 · 待校准' : group.passthrough ? '客户端透传' : balance ? '余额跟踪已关闭' : '未提供额度'}</span></span>;
  const tools = !group.passthrough && <><Tooltip asChild content={enabled ? '记录充值' : '设置手动余额 · USD'}><button type="button" className="qh-icon-button qh-balance-setup" aria-label={`${enabled ? '充值' : '设置手动余额'} ${channel.name} / ${group.name}`} disabled={busy} onClick={() => open(enabled ? 'credit' : 'calibrate')}>{enabled ? <Plus size={13} /> : <Wallet size={13} />}</button></Tooltip></>;
  return <ResourceGroup label={`${channel.name} / ${group.name} 余额`} name={group.name} compact={!enabled} summary={summary} tools={tools}>
    {enabled && <><div className="qh-manual-balance"><div className="qh-balance-heading"><Wallet size={14} /><span>估算剩余 · USD</span><span className="qh-badge" data-tone="muted">本地估算</span></div><strong className="qh-balance-amount" data-negative={Number(balance.balanceUsd) <= 0}>{formatUsd(balance.balanceUsd)}</strong><dl className="qh-balance-totals"><div><dt>起点</dt><dd>{formatUsd(balance.baselineUsd)}</dd></div><div><dt>充值</dt><dd>{formatUsd(balance.creditedUsd)}</dd></div><div><dt>已扣减</dt><dd>{formatUsd(balance.spentUsd)}</dd></div></dl><p className="qh-reset">校准 {date(balance.calibratedAt)}</p><p className="qh-balance-policy">{balance.stopWhenExhausted ? '余额耗尽后跳过此分组' : '仅记账，余额耗尽后仍可调度'}</p>{balance.incompleteCount > 0 && <p className="qh-balance-warning">{balance.incompleteCount} 次请求用量或价格不完整，余额可能偏高；请核对平台后校准。</p>}</div><div className="qh-balance-detail-actions"><button type="button" className="qh-small-action" disabled={busy} onClick={() => open('calibrate')}><Pencil size={12} />校准</button><button type="button" className="qh-small-action" disabled={busy} onClick={() => open('settings')}><Settings2 size={12} />设置</button><button type="button" className="qh-small-action" disabled={busy} onClick={() => open('disable')}><Power size={12} />关闭跟踪</button><button type="button" className="qh-small-action" disabled={busy} onClick={() => open('history')}><History size={12} />流水</button></div></>}
    {!enabled && balance?.bindingChanged && <p className="qh-summary-warning">凭证已变更，请重新校准余额。</p>}
    {!enabled && balance && !group.passthrough && <button type="button" className="qh-small-action" disabled={busy} onClick={() => open('history')}><History size={12} />历史流水</button>}
  </ResourceGroup>;
}

function HealthEndpoint({ candidate }) {
  const health = candidateHealthPresentation(candidate);
  return <div className="qh-endpoint"><div className="qh-endpoint-heading"><span className="qh-endpoint-dot" data-tone={health.tone} /><span className="qh-endpoint-name">{candidate.endpointName || (candidate.endpointAddress ? '未命名端点' : '端点信息待同步')}</span><span className="qh-badge" data-tone={health.tone}>{health.label}</span></div>
    {candidate.endpointAddress && <p className="qh-endpoint-address">{candidate.endpointAddress}</p>}
    <div className="qh-endpoint-meta"><span>{candidate.upstreamModel || candidate.model || '沿用请求模型'}</span>{candidate.protocol && <span className="qh-endpoint-protocol">{candidate.protocol === 'messages' ? 'Messages' : candidate.protocol === 'responses' ? 'Responses' : candidate.protocol}</span>}</div>
    {(health.reason || health.until) && <div className="qh-health-reason" data-tone={health.tone}>{health.reason && <p>{health.reason}</p>}{date(health.until) !== '—' && <p>恢复 {date(health.until)}</p>}</div>}
  </div>;
}

function AccountLoad({ load }) {
  return <dl className="qh-load" aria-label="账号近期负载"><div><dt>并发</dt><dd>{load.concurrent}{load.maxConcurrent > 0 && <span> / {load.maxConcurrent}</span>}</dd></div><div><dt>近期 tokens</dt><dd>{number.format(load.decayedTokens)}</dd></div><div><dt>近期请求</dt><dd>{number.format(load.decayedRequests)}</dd></div></dl>;
}

function HealthGroup({ channel, group }) {
  const summary = healthGroupSummary(group.candidates);
  const load = group.candidates[0]?.load || {};
  return <ResourceGroup label={`${channel.name} / ${group.name} 健康`} name={group.name} summary={<span className="qh-group-overview"><span className="qh-badge" data-tone={summary.tone}>{summary.label}</span><small>{group.candidates.length} 个候选</small></span>}>
    <AccountLoad load={load} /><div className="qh-endpoints">{group.candidates.map((candidate) => <HealthEndpoint key={`${candidate.endpointId}/${candidate.model}/${candidate.upstreamModel}/${candidate.protocol}`} candidate={candidate} />)}</div>
  </ResourceGroup>;
}

export default function QuotaHealthWorkspace({ provider, channels, accounts, candidates, quotas, balances = [], endpoints = [], model, onModelChange, busy, onRead, onRefresh, onBalanceUpdate, onPreview }) {
  const [query, setQuery] = useState('');
  const [quotaScope, setQuotaScope] = useState('subscriptions');
  const [healthScope, setHealthScope] = useState('issues');
  const [balanceDialog, setBalanceDialog] = useState(null);
  const resources = quotaHealthChannels(channels, accounts, quotas, candidates, endpoints, balances);
  const quotaChannels = filterQuotaHealthChannels(resources, 'quotas', query, { subscriptionsOnly: quotaScope === 'subscriptions' });
  const healthChannels = filterQuotaHealthChannels(resources, 'candidates', query, { issuesOnly: healthScope === 'issues' });
  const visibleGroups = quotaChannels.flatMap((channel) => channel.groups);
  const managedCount = visibleGroups.filter((group) => group.managed).length;
  const resourceCount = resources.reduce((total, channel) => total + channel.groups.length, 0);
  const issues = candidates.filter((candidate) => candidateHealthPresentation(candidate).tone !== 'success').length;
  return <div className="quota-health-workspace">
    <div className="qh-toolbar"><label className="qh-search"><Search size={14} /><input aria-label="搜索额度与健康" placeholder="搜索渠道、分组、模型或端点" value={query} onChange={(event) => setQuery(event.target.value)} /></label><Button type="button" size="sm" variant="secondary" icon={RefreshCw} disabled={busy} onClick={onRead}>读取快照</Button></div>
    <WorkspaceSection title="额度与余额" icon={Database} className="qh-quota-section" count={`${visibleGroups.length} / ${resourceCount} 个分组`} tools={<ScopeSwitch label="额度展示范围" value={quotaScope} onChange={setQuotaScope} options={quotaScopes} />}>
      {managedCount > 0 && <div className="qh-quota-controls"><label>订阅查询模型<input aria-label="主动查询的模型范围" placeholder="留空查询整体额度" value={model} disabled={busy} onChange={(event) => onModelChange(event.target.value)} /></label></div>}
      {quotaChannels.length > 0 && <p className="qh-section-note">{quotaScope === 'subscriptions' ? '订阅额度来自供应商；查询失败保留历史快照。' : '手动余额按请求成本扣减，仅为本地估算。未提供额度不影响健康检查或调度。'}</p>}
      <HealthCardGrid>{quotaChannels.map((channel) => <ResourceCard key={channel.id} channel={channel} kind="额度">{channel.groups.map((group) => group.managed ? <QuotaGroup key={group.id} channel={channel} group={group} model={model} busy={busy} onRefresh={onRefresh} /> : <ManualBalanceGroup key={group.id} channel={channel} group={group} busy={busy} onAction={setBalanceDialog} />)}</ResourceCard>)}</HealthCardGrid>
      {!quotaChannels.length && <div className="qh-scope-empty"><CircleHelp size={15} /><span>{query ? quotaScope === 'subscriptions' ? '没有匹配的订阅账号。' : '没有匹配的额度或余额资源。' : quotaScope === 'subscriptions' ? '暂无订阅账号，中转余额可在全部资源中查看和设置。' : '暂无凭证分组，请先添加渠道资源。'}</span>{quotaScope === 'subscriptions' && <button type="button" className="qh-small-action" onClick={() => setQuotaScope('all')}><Layers size={12} />查看全部资源</button>}</div>}
    </WorkspaceSection>
    <WorkspaceSection title="健康与冷却" icon={Activity} count={`${candidates.length} 个候选 · ${issues} 个需关注`} tools={<ScopeSwitch label="健康展示范围" value={healthScope} onChange={setHealthScope} options={healthScopes} />}>
      {healthChannels.length > 0 && <p className="qh-section-note">近期负载按 1 小时半衰期衰减，同一账号的端点共享负载。</p>}
      <HealthCardGrid>{healthChannels.map((channel) => <ResourceCard key={channel.id} channel={channel} kind="健康">{channel.groups.map((group) => <HealthGroup key={group.id} channel={channel} group={group} />)}</ResourceCard>)}</HealthCardGrid>
      {!healthChannels.length && <div className="qh-scope-empty" data-tone={!query && candidates.length && !issues ? 'success' : 'muted'}>{!query && candidates.length && !issues ? <ShieldCheck size={15} /> : <CircleHelp size={15} />}<span>{query ? healthScope === 'issues' ? '当前搜索范围内没有异常候选。' : '没有匹配的候选。' : !candidates.length ? '尚无候选健康状态。' : healthScope === 'issues' ? '当前没有异常候选。' : '没有可展示的候选。'}</span></div>}
      {healthChannels.length > 0 && <footer className="qh-health-footer"><p>更新凭证可解除修复状态；具体模型限制可通过候选预览查看。</p><Button type="button" size="sm" variant="ghost" icon={FlaskConical} disabled={busy} onClick={onPreview}>查看候选健康</Button></footer>}
    </WorkspaceSection>
    {balanceDialog && <ManualBalanceDialog provider={provider} {...balanceDialog} onSave={onBalanceUpdate} onClose={() => setBalanceDialog(null)} />}
  </div>;
}
