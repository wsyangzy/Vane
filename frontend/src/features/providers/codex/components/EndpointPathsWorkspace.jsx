import { useEffect, useId, useRef, useState } from 'react';
import { ArrowRight, Check, ChevronDown, KeyRound, ListChecks, Loader2, ScanLine, Server, Square, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { probeCodexEndpoint, updateCodexEndpointPath } from '@/services/wails/codex.js';
import { isSubscriptionGroup } from '@/features/providers/shared/subscriptions/subscriptionSettings.js';
import { getCredentialGroups, getDefaultCredentialGroup } from '../model.js';
import { buildEndpointProbeInput, endpointPathIdentity, getDetectedPathStrategy, getEndpointPathStrategy, pathStrategyLabel, PROBE_RESULT_LABELS } from '../pathPolicy.js';
import { currentPathResult, groupPathEntries, pathEntryStatus } from '../endpointPathsState.js';
import './endpointPaths.css';

const phaseLabels = { detect: '正在检测端点', apply: '正在应用明确结果', refresh: '正在刷新渠道资源', finished: '检测完成', stopped: '已停止', failed: '处理未完成' };

function PathProgress({ run }) {
  const finished = ['finished', 'stopped', 'failed'].includes(run.phase);
  const steps = run.applyMode ? ['检测', '应用', '刷新'] : ['检测'];
  const phaseIndex = { detect: 0, apply: 1, refresh: 2 }[run.phase] ?? (run.phase === 'finished' ? steps.length : run.lastPhase);
  const value = run.phase === 'apply' ? run.applyComplete : run.complete;
  const max = run.phase === 'apply' ? run.applyTotal : run.total;
  return <section className="ep-path-progress" aria-label="批量处理进度" aria-live="polite">
    <div className="ep-path-progress-heading"><strong>{run.stopped && !finished ? '正在停止，等待当前操作结束' : phaseLabels[run.phase]}</strong><span className="tabular-nums">已检测 {run.complete} / {run.total}{run.applyMode && (run.phase === 'detect' ? ' · 检测后应用明确结果' : ` · 已应用 ${run.changed} / ${run.applyTotal}`)}</span></div>
    <ol className="ep-path-steps">{steps.map((label, index) => <li key={label} data-state={index < phaseIndex ? 'complete' : index === phaseIndex ? 'active' : 'pending'}>{index < phaseIndex ? <Check size={13} /> : index === phaseIndex && !finished ? <Loader2 size={13} className="ep-path-spin" /> : <span className="ep-path-step-dot" />}{label}</li>)}</ol>
    <div role="progressbar" aria-label={run.phase === 'apply' ? '路径应用进度' : run.phase === 'refresh' ? '资源刷新中' : '端点检测进度'} aria-valuemin={0} aria-valuemax={max || 1} aria-valuenow={run.phase === 'refresh' ? undefined : value} className="ep-path-track"><span className={run.phase === 'refresh' ? 'ep-path-indeterminate' : ''} style={run.phase === 'refresh' ? undefined : { width: `${max ? Math.min(100, value / max * 100) : 100}%` }} /></div>
    {finished && <p>{run.applyMode ? `已应用 ${run.changed} 个端点的路径。` : '检测结果已保留。'}{run.phase === 'stopped' ? '未开始的检测和应用已停止。' : '未明确确认的端点保留原策略。'}</p>}
  </section>;
}

function EndpointTile({ entry, row, active, phase }) {
  const { endpoint, fixed } = entry;
  const strategy = row?.saved ? row.strategy : getEndpointPathStrategy(endpoint);
  const status = active ? { tone: 'accent', label: phase === 'apply' ? '应用中' : '检测中' } : pathEntryStatus(entry, row);
  return <li className="ep-path-endpoint" aria-label={`端点 ${endpoint.name}`}>
    <div className="ep-path-endpoint-heading"><strong>{endpoint.name}</strong><span className="ep-path-status" data-tone={status.tone}>{active && <Loader2 size={12} className="ep-path-spin" />}{status.label}</span></div>
    <p className="ep-path-address" title={endpoint.baseURL || endpoint.base_url}>{endpoint.baseURL || endpoint.base_url}</p>
    <div className="ep-path-policy" title={pathStrategyLabel(strategy)}><span>{fixed ? '官方固定路径' : strategy === 'legacy' ? '旧版路径' : strategy === 'v1' ? '/v1 路径' : '基础地址路径'}</span>{row?.strategy && row.strategy !== strategy && <><ArrowRight size={12} /><span>{row.strategy === 'v1' ? '/v1 路径' : '基础地址路径'}</span></>}{row?.result && !row.strategy && <span>保留原策略</span>}</div>
    {(row?.result || row?.error) && <details className="ep-path-details"><summary>检测详情<ChevronDown size={12} /></summary><div>{row.error && <p role="alert" className="ep-path-error">{row.error}</p>}{row.result?.candidates?.map((candidate, index) => <div className="ep-path-probe" key={`${candidate.url}/${index}`}><p data-verified={candidate.verified}>{candidate.http_status ? `HTTP ${candidate.http_status} · ` : ''}{PROBE_RESULT_LABELS[candidate.result] || '协议未确认'}</p><code title={candidate.url}>{candidate.url}</code></div>)}</div></details>}
  </li>;
}

export default function EndpointPathsWorkspace({ channels, disabled, onRefresh, onBusyChange, results, onResultsChange, onClose }) {
  const [run, setRun] = useState(null);
  const [error, setError] = useState('');
  const titleId = useId(), descriptionId = useId();
  const dialogRef = useRef(null), closeRef = useRef(null), activeRun = useRef(null);
  const latestChannels = useRef(channels), closeHandler = useRef(onClose);
  useEffect(() => { latestChannels.current = channels; }, [channels]);
  useEffect(() => { closeHandler.current = onClose; }, [onClose]);
  useEffect(() => {
    const previousFocus = document.activeElement, overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden'; closeRef.current?.focus();
    const onKeyDown = event => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (!activeRun.current) closeHandler.current?.(); return; }
      if (event.key !== 'Tab') return;
      const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), summary, [tabindex="0"]')].filter(element => element.getClientRects().length > 0);
      const first = controls[0], last = controls.at(-1);
      if (!dialogRef.current.contains(document.activeElement) || event.shiftKey && document.activeElement === first || !event.shiftKey && document.activeElement === last) { event.preventDefault(); (event.shiftKey ? last : first)?.focus(); }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => { if (activeRun.current) activeRun.current.stopped = true; window.removeEventListener('keydown', onKeyDown); document.body.style.overflow = overflow; previousFocus?.focus({ preventScroll: true }); };
  }, []);

  const entries = channels.flatMap(channel => (channel.endpoints || []).map(endpoint => {
    const groups = getCredentialGroups(channel);
    const group = groups.find(item => Number(item.id) === Number(endpoint.credentialGroupId || endpoint.credential_group_id)) || getDefaultCredentialGroup(channel);
    return { channel, endpoint, group, fixed: isSubscriptionGroup(group) || endpoint.authMode === 'client_passthrough' };
  }));
  const grouped = groupPathEntries(entries), eligible = entries.filter(entry => !entry.fixed);
  const running = run && !['finished', 'stopped', 'failed'].includes(run.phase);
  const start = async (apply) => {
    if (activeRun.current || disabled || !eligible.length) return;
    const job = { stopped: false, phase: 'detect', lastPhase: 0, applyMode: apply, complete: 0, total: eligible.length, applyComplete: 0, applyTotal: 0, changed: 0, active: new Set() };
    const publish = () => setRun({ ...job, active: [...job.active] });
    activeRun.current = job; onBusyChange?.(true);
    publish(); onResultsChange({}); setError('');
    let next = 0;
    const changes = [], record = (id, result) => onResultsChange(previous => ({ ...previous, [id]: result }));
    try {
      await Promise.all(Array.from({ length: Math.min(3, eligible.length) }, async () => {
        while (!job.stopped && next < eligible.length) {
          const { channel, endpoint, group } = eligible[next++];
          const identity = endpointPathIdentity(endpoint), current = getEndpointPathStrategy(endpoint);
          job.active.add(endpoint.id); publish();
          try {
            const result = await probeCodexEndpoint(buildEndpointProbeInput({ channel, credentialGroup: group, endpoint, baseURL: endpoint.baseURL || endpoint.base_url }));
            const strategy = getDetectedPathStrategy(result, current), row = { identity, result, strategy };
            record(endpoint.id, row);
            if (apply && strategy && strategy !== current) changes.push({ channel, endpoint, current, row });
          } catch (err) { record(endpoint.id, { identity, error: getErrorMessage(err, '检测失败') }); }
          finally { job.complete++; job.active.delete(endpoint.id); publish(); }
        }
      }));
      if (apply && !job.stopped) { job.phase = 'apply'; job.lastPhase = 1; }
      job.applyTotal = changes.length; publish();
      for (const { channel, endpoint, current, row } of changes) {
        if (job.stopped) break;
        const latest = latestChannels.current.find(item => item.id === channel.id)?.endpoints?.find(item => item.id === endpoint.id);
        job.active.add(endpoint.id); publish();
        try {
          if (endpointPathIdentity(latest) !== row.identity || getEndpointPathStrategy(latest) !== current) throw new Error('端点配置已更改，请重新检测');
          await updateCodexEndpointPath(endpoint.id, row.strategy);
          job.changed++; record(endpoint.id, { ...row, saved: true });
        } catch (err) { record(endpoint.id, { ...row, error: getErrorMessage(err, '保存路径失败') }); }
        finally { job.applyComplete++; job.active.delete(endpoint.id); publish(); }
      }
      if (job.changed) { job.phase = 'refresh'; job.lastPhase = 2; publish(); await onRefresh?.(); }
      job.phase = job.stopped ? 'stopped' : 'finished';
    } catch (err) { job.phase = 'failed'; setError(getErrorMessage(err, '刷新端点路径失败')); }
    finally { activeRun.current = null; publish(); onBusyChange?.(false); }
  };
  const close = () => { if (!activeRun.current) onClose?.(); };
  return renderModalPortal(<div className="ep-path-backdrop" onClick={event => { if (event.target === event.currentTarget) close(); }}>
    <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descriptionId} className="ep-path-dialog">
      <header className="ep-path-header"><div className="min-w-0"><h2 id={titleId}><ScanLine size={18} />端点路径</h2><p>{grouped.length} 个渠道 · {grouped.reduce((count, channel) => count + channel.groups.length, 0)} 个分组 · {entries.length} 个端点</p></div><button ref={closeRef} type="button" className="ep-path-close" aria-label="关闭端点路径" disabled={Boolean(running)} onClick={close}><X size={18} /></button></header>
      <div className="ep-path-toolbar"><p id={descriptionId}>使用各分组的凭证检测；鉴权失败、限流或结果不明确时保留原策略。</p><div>{running ? <Button type="button" variant="secondary" size="sm" icon={Square} disabled={run.stopped} onClick={() => { activeRun.current.stopped = true; setRun({ ...activeRun.current, active: [...activeRun.current.active] }); }}>{run.stopped ? '正在停止…' : '停止'}</Button> : <><Button type="button" variant="secondary" size="sm" icon={ScanLine} disabled={disabled || !eligible.length} onClick={() => start(false)}>检测全部</Button><Button type="button" size="sm" icon={ListChecks} disabled={disabled || !eligible.length} onClick={() => start(true)}>检测并应用</Button></>}</div></div>
      {run && <PathProgress run={run} />}
      {error && <p role="alert" className="ep-path-error ep-path-top-error">{error}</p>}
      <div className="ep-path-body scrollbar-visible"><div className="ep-path-channels">{grouped.map(({ channel, groups, entries: channelEntries }) => <section key={channel.id} className="ep-path-channel" aria-label={`渠道 ${channel.name}`}><header><h3><Server size={14} />{channel.name}</h3><span>{groups.length} 分组 · {channelEntries.length} 端点</span></header>{groups.map(({ group, entries: groupEntries }) => <section className="ep-path-group" key={group?.id ?? 0} aria-label={`凭证分组 ${group?.name || '默认分组'}`}><div className="ep-path-group-heading"><h4><KeyRound size={12} />{group?.name || '默认分组'}</h4><span>{groupEntries.length} 个端点</span></div><ul className="ep-path-endpoints">{groupEntries.map(entry => <EndpointTile key={entry.endpoint.id} entry={entry} row={currentPathResult(entry.endpoint, results)} active={Boolean(running && run.active.includes(entry.endpoint.id))} phase={run?.phase} />)}</ul></section>)}</section>)}</div>{!entries.length && <p className="ep-path-empty">暂无端点，请先在渠道资源中添加端点。</p>}</div>
      <footer className="ep-path-footer"><span>{eligible.length} 个可检测{entries.length > eligible.length && ` · ${entries.length - eligible.length} 个官方固定路径`}</span><Button type="button" variant="secondary" size="sm" disabled={Boolean(running)} onClick={close}>关闭</Button></footer>
    </section>
  </div>, { root: true });
}
