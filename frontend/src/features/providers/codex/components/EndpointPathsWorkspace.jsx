import { useEffect, useRef, useState } from 'react';
import { ArrowRight, ListChecks, ScanLine, Square } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { probeCodexEndpoint, updateCodexEndpointPath } from '@/services/wails/codex.js';
import { isSubscriptionGroup } from '@/features/providers/shared/subscriptions/subscriptionSettings.js';
import { getCredentialGroups, getDefaultCredentialGroup } from '../model.js';
import { buildEndpointProbeInput, endpointPathIdentity, getDetectedPathStrategy, getEndpointPathStrategy, pathStrategyLabel, PROBE_RESULT_LABELS } from '../pathPolicy.js';

const concurrency = 3;

export default function EndpointPathsWorkspace({ channels, disabled, onRefresh, onBusyChange, results, onResultsChange }) {
  const [run, setRun] = useState(null);
  const [error, setError] = useState('');
  const activeRun = useRef(null);
  const latestChannels = useRef(channels);
  useEffect(() => { latestChannels.current = channels; }, [channels]);
  useEffect(() => () => { if (activeRun.current) activeRun.current.stopped = true; }, []);

  const entries = channels.flatMap(channel => (channel.endpoints || []).map(endpoint => {
    const groups = getCredentialGroups(channel);
    const group = groups.find(item => Number(item.id) === Number(endpoint.credentialGroupId || endpoint.credential_group_id)) || getDefaultCredentialGroup(channel);
    return { channel, endpoint, group, fixed: isSubscriptionGroup(group) || endpoint.authMode === 'client_passthrough' };
  }));
  const eligible = entries.filter(entry => !entry.fixed);
  const start = async (apply) => {
    if (activeRun.current || !eligible.length) return;
    const job = { stopped: false, complete: 0, total: eligible.length };
    activeRun.current = job;
    onBusyChange?.(true);
    setRun({ ...job });
    onResultsChange({});
    setError('');
    let next = 0;
    let changed = false;
    const changes = [];
    const record = (id, result) => onResultsChange(previous => ({ ...previous, [id]: result }));
    try {
      await Promise.all(Array.from({ length: Math.min(concurrency, eligible.length) }, async () => {
        while (!job.stopped && next < eligible.length) {
          const { channel, endpoint, group } = eligible[next++];
          const identity = endpointPathIdentity(endpoint);
          const current = getEndpointPathStrategy(endpoint);
          try {
            const result = await probeCodexEndpoint(buildEndpointProbeInput({ channel, credentialGroup: group, endpoint, baseURL: endpoint.baseURL || endpoint.base_url }));
            const strategy = getDetectedPathStrategy(result, current);
            const row = { identity, result, strategy };
            record(endpoint.id, row);
            if (apply && strategy && strategy !== current) changes.push({ channel, endpoint, current, row });
          } catch (err) {
            record(endpoint.id, { identity, error: getErrorMessage(err, '检测失败') });
          } finally {
            job.complete++;
            setRun({ ...job });
          }
        }
      }));
      for (const { channel, endpoint, current, row } of changes) {
        if (job.stopped) break;
        const latest = latestChannels.current.find(item => item.id === channel.id)?.endpoints?.find(item => item.id === endpoint.id);
        try {
          if (endpointPathIdentity(latest) !== row.identity || getEndpointPathStrategy(latest) !== current) {
            throw new Error('端点配置已更改，请重新检测');
          }
          await updateCodexEndpointPath(endpoint.id, row.strategy);
          changed = true;
          record(endpoint.id, { ...row, saved: true });
        } catch (err) {
          record(endpoint.id, { ...row, error: getErrorMessage(err, '保存路径失败') });
        }
      }
      if (changed) await onRefresh?.();
    } catch (err) {
      setError(getErrorMessage(err, '刷新端点路径失败'));
    } finally {
      activeRun.current = null;
      setRun(null);
      onBusyChange?.(false);
    }
  };

  return <section aria-label="端点路径检测" className="min-w-0 space-y-3">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 className="text-sm font-semibold text-slate-800">端点路径</h2>
        <p className="mt-1 text-xs text-slate-500">{entries.length} 个端点 · {eligible.length} 个可检测</p>
      </div>
      <div className="flex flex-wrap gap-2">
        {run ? <Button type="button" variant="secondary" size="sm" icon={Square} disabled={run.stopped} onClick={() => {
          activeRun.current.stopped = true;
          setRun({ ...activeRun.current });
        }}>{run.stopped ? '正在停止…' : '停止'}</Button> : <>
          <Button type="button" variant="secondary" size="sm" icon={ScanLine} disabled={disabled || !eligible.length} onClick={() => start(false)}>检测全部</Button>
          <Button type="button" size="sm" icon={ListChecks} disabled={disabled || !eligible.length} onClick={() => start(true)}>检测并应用明确结果</Button>
        </>}
      </div>
    </div>
    {run && <div role="status" className="flex items-center gap-3 text-xs text-slate-600">
      <progress value={run.complete} max={run.total} className="h-1.5 min-w-0 flex-1 accent-indigo-600" aria-label="端点检测进度" />
      <span className="shrink-0 tabular-nums">{run.complete} / {run.total}{run.complete === run.total && !run.stopped ? ' · 处理结果中' : ''}</span>
    </div>}
    {error && <p role="alert" className="text-xs text-rose-700 wrap-anywhere">{error}</p>}
    <ul className="divide-y divide-slate-100 rounded-lg border border-slate-200 bg-white">
      {entries.map(({ channel, endpoint, group, fixed }) => {
        const savedResult = results[endpoint.id];
        const row = savedResult?.identity === endpointPathIdentity(endpoint) ? savedResult : null;
        const strategy = row?.saved ? row.strategy : getEndpointPathStrategy(endpoint);
        return <li key={endpoint.id} className="min-w-0 px-4 py-3">
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
            <div className="min-w-0">
              <h3 className="text-sm font-medium text-slate-800 wrap-anywhere">{endpoint.name}</h3>
              <p className="text-xs text-slate-500 wrap-anywhere">{channel.name}{group?.name && ` · ${group.name}`}</p>
              <p className="mt-1 font-mono text-xs text-slate-400 wrap-anywhere">{endpoint.baseURL || endpoint.base_url}</p>
            </div>
            <div className="flex flex-wrap items-center gap-1.5 text-xs text-slate-600">
              {fixed ? '官方固定路径' : pathStrategyLabel(strategy)}
              {row?.strategy && row.strategy !== strategy && <><ArrowRight size={12} /><span className="text-emerald-700">{pathStrategyLabel(row.strategy)}</span></>}
              {row?.saved && <span className="text-emerald-700">已应用</span>}
            </div>
          </div>
          {row?.error && <p className="mt-2 text-xs text-rose-700 wrap-anywhere">{row.error}</p>}
          {!row && !fixed && <p className="mt-2 text-xs text-slate-400">未检测</p>}
          {row?.result?.candidates?.map(candidate => <div key={candidate.url} className="mt-2 flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-xs">
            <span className={candidate.verified ? 'text-emerald-700' : 'text-amber-700'}>{candidate.http_status ? `HTTP ${candidate.http_status} · ` : ''}{PROBE_RESULT_LABELS[candidate.result] || '协议未确认'}</span>
            <span className="min-w-0 font-mono text-slate-500 wrap-anywhere">{candidate.url}</span>
          </div>)}
          {row?.result && !row.strategy && <p className="mt-2 text-xs text-slate-500">保留当前策略</p>}
        </li>;
      })}
      {!entries.length && <li className="px-4 py-6 text-center text-sm text-slate-500">暂无端点</li>}
    </ul>
  </section>;
}
