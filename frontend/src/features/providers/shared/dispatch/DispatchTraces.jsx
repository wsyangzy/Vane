import Tooltip from '@/shared/ui/Tooltip.jsx';
import { ChevronDown, Fingerprint, RefreshCw } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { affinityOptions, dispatchReason, ruleKindLabel, strategyOptions } from './dispatchState.js';
import './dispatchTraces.css';

const actionLabels = { attempt: '尝试调用', response_started: '响应开始', success: '调用成功', failed: '调用失败', skipped: '已跳过' };
const scopeLabels = { candidate: '当前候选', credential: '凭证', endpoint: '端点', model: '同账号模型', request: '当前请求' };
const protocolLabels = { responses: 'Responses', messages: 'Messages' };
const optionLabel = (options, value) => options.find(option => option.value === value)?.label || value;
const dateLabel = date => date && new Date(date).getTime() > 0 ? new Date(date).toLocaleString() : '—';
const toneFor = action => ({ success: 'success', failed: 'danger', skipped: 'muted' }[action] || 'accent');

function candidateEvents(entries) {
  const grouped = new Map();
  for (const entry of entries) {
    const c = entry.candidate;
    const key = JSON.stringify([c.provider, c.channel, c.credential, c.endpoint, c.model, c.protocol, c.stateVersion]);
    if (!grouped.has(key)) grouped.set(key, { key, candidate: c, events: [] });
    grouped.get(key).events.push(entry);
  }
  return [...grouped.values()];
}

function CandidateRecord({ record, index, channels, accounts, endpoints, commonReason }) {
  const { candidate, events } = record;
  const result = events.findLast(event => event.action !== 'skipped') || events.at(-1);
  const endpoint = endpoints.find(item => String(item.channelId) === candidate.channel && String(item.endpointId) === candidate.endpoint);
  const channel = channels.find(item => String(item.id) === candidate.channel);
  const credentialGroup = candidate.credential.startsWith('account:') ? null : candidate.credential.split('/')[1];
  const groupId = endpoint?.groupId || credentialGroup;
  const account = accounts.find(item => String(item.channelId) === candidate.channel && String(item.groupId) === String(groupId));
  const endpointName = endpoint?.name || (candidate.endpoint.includes('::') ? candidate.endpoint.split('::').at(-1) : candidate.endpoint ? `端点 #${candidate.endpoint}` : '未选定端点');
  const reason = result.reason;
  const resultNote = reason && reason === commonReason ? result.action === 'skipped' ? '未发起上游调用' : '原因见上方' : reason ? dispatchReason(reason) : result.action === 'response_started' ? '等待最终结果' : '';
  return <li className="dt-candidate">
    <span className="dt-sequence">{String(index + 1).padStart(2, '0')}</span>
    <div className="dt-resource"><div><strong>{channel?.name || (candidate.channel ? `渠道 #${candidate.channel}` : '未选定渠道')}</strong><span>{account?.groupName || (groupId ? `分组 #${groupId}` : '历史凭证')}</span></div><p>{endpointName}</p></div>
    <div className="dt-model"><strong>{candidate.model || '未指定模型'}</strong><span>{protocolLabels[candidate.protocol] || candidate.protocol || '未知协议'}</span></div>
    <div className="dt-result"><span className="dt-badge" data-tone={toneFor(result.action)}>{actionLabels[result.action] || result.action}</span>{resultNote && <Tooltip asChild content={dispatchReason(reason)}><small>{resultNote}</small></Tooltip>}{result.failureScope && <small>作用域：{scopeLabels[result.failureScope] || result.failureScope}</small>}</div>
    <details className="dt-identity"><summary><Fingerprint size={12} />身份与事件<span>{events.length} 项</span></summary><div className="dt-identity-content"><dl><div><dt>渠道 ID</dt><dd>{candidate.channel || '—'}</dd></div><div><dt>凭证标识</dt><dd>{candidate.credential || '—'}</dd></div><div><dt>端点 ID</dt><dd>{candidate.endpoint || '—'}</dd></div><div><dt>状态版本</dt><dd>{candidate.stateVersion}</dd></div></dl><ol>{events.map((event, at) => <li key={at}><span>{actionLabels[event.action] || event.action}</span>{event.reason && <span>{dispatchReason(event.reason)}</span>}{event.failureScope && <span>作用域：{scopeLabels[event.failureScope] || event.failureScope}</span>}</li>)}</ol></div></details>
  </li>;
}

function TraceRecord({ trace, channels, accounts, endpoints }) {
  const records = candidateEvents(trace.candidates);
  const reasons = [...new Set(trace.candidates.map(event => event.reason).filter(Boolean))];
  const commonReason = reasons.length === 1 ? reasons[0] : '';
  const reason = trace.stopReason || commonReason;
  const pending = !trace.finishedAt && !trace.stopReason && !trace.successful;
  const status = trace.successful ? '调用成功' : pending ? '处理中' : !trace.attempts ? '未发送' : trace.responseStarted ? '响应未完成' : '调用失败';
  return <details className="dt-trace">
    <summary className="dt-summary"><ChevronDown size={14} className="dt-chevron" /><span className="dt-badge" data-tone={trace.successful ? 'success' : pending ? 'accent' : !trace.attempts ? 'warning' : 'danger'}>{status}</span><strong>{trace.profileName || `方案 #${trace.profileId}`}</strong><span className="dt-request-id">{trace.requestId || '未生成请求 ID'}</span><time>{dateLabel(trace.createdAt)}</time><span className="dt-attempts">{trace.attempts} 次上游尝试</span></summary>
    <div className="dt-body">
      <div className="dt-context"><span>{optionLabel(strategyOptions, trace.strategy)}</span><span>{optionLabel(affinityOptions, trace.affinityMode)}</span><span>{ruleKindLabel(trace.decision.kind) || '默认编排'}{trace.decision.ruleId ? ` #${trace.decision.ruleId}` : ''}</span>{trace.decision.intentId && <span>意图 {trace.decision.intentId}</span>}<span>等待 {trace.waitMs}ms</span><span>响应{trace.responseStarted ? '已开始' : '未开始'} · 最终{trace.successful ? '成功' : pending ? '待完成' : '未成功'}</span></div>
      {reason && <div className="dt-reason" data-tone={trace.successful ? 'success' : 'warning'}><strong>{!trace.attempts ? '未发送原因' : '结束原因'}</strong><p>{dispatchReason(reason)}</p></div>}
      <div className="dt-events-heading"><span>候选执行记录</span><small>{records.length} 个候选 · {trace.candidates.filter(event => event.action === 'skipped').length} 条跳过</small></div>
      {records.length ? <><div className="dt-columns" aria-hidden="true"><span /><span>处理资源</span><span>目标模型 / 协议</span><span>执行结果</span></div><ol className="dt-candidates" aria-label="候选执行记录">{records.map((record, index) => <CandidateRecord key={record.key} record={record} index={index} channels={channels} accounts={accounts} endpoints={endpoints} commonReason={commonReason} />)}</ol><p className="dt-footnote">资源名称按当前配置显示；历史身份与完整事件可展开查看。</p></> : <p className="dt-empty">没有候选事件。</p>}
    </div>
  </details>;
}

export default function DispatchTraces({ traces, channels, accounts, endpoints, busy, onReload }) {
  return <section className="dt-workspace" aria-label="调度记录"><div className="dt-toolbar"><p>最近 100 次调度 · 查看处理资源、执行结果与失败原因</p><Button type="button" size="sm" variant="secondary" icon={RefreshCw} disabled={busy} onClick={onReload}>刷新记录</Button></div><div className="dt-traces">{traces.map((trace, index) => <TraceRecord key={`${trace.requestId}/${trace.createdAt}/${index}`} trace={trace} channels={channels} accounts={accounts} endpoints={endpoints} />)}{!traces.length && <p className="dt-empty">尚无调度记录。</p>}</div></section>;
}
