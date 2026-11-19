import { useEffect, useRef, useState } from 'react';
import { RefreshCw, RotateCcw, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { getDispatchRecovery, retryDispatchCandidate } from '@/services/wails/dispatch.js';
import { blockageLabel } from '../utils/blockage.js';
import { describeFailureReason } from '../utils/failureSummary.js';
import { readRecoveryCandidates, recoveryCandidateKey, releasableCandidates, releaseRecoveryCandidates } from '../utils/requestRecovery.js';

export default function RequestRecoveryDialog({ groups, onClose }) {
  const dialogRef = useRef(null);
  const [entries, setEntries] = useState([]);
  const [busy, setBusy] = useState('check');
  const [result, setResult] = useState(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    const trigger = document.activeElement;
    dialog.showModal();
    let active = true;
    readRecoveryCandidates(groups, getDispatchRecovery).then(current => {
      if (!active) return;
      setEntries(current);
      setBusy('');
    });
    return () => {
      active = false;
      dialog.close();
      if (trigger?.isConnected) trigger.focus({ preventScroll: true });
    };
  }, [groups]);

  const eligible = releasableCandidates(entries);
  const refresh = async () => {
    setBusy('check');
    setResult(null);
    setEntries(await readRecoveryCandidates(groups, getDispatchRecovery));
    setBusy('');
  };
  const release = async () => {
    setBusy('release');
    setResult(null);
    const updated = await releaseRecoveryCandidates(groups, eligible, { getRecovery: getDispatchRecovery, retryCandidate: retryDispatchCandidate });
    setEntries(updated.entries);
    setResult(updated);
    setBusy('');
  };
  const close = () => {
    if (busy !== 'release') onClose();
  };

  return <dialog ref={dialogRef} aria-labelledby="request-recovery-title"
    onCancel={event => { event.preventDefault(); close(); }}
    onClick={event => { if (event.target === event.currentTarget) close(); }}
    className="fixed inset-0 m-auto w-[calc(100%-2rem)] max-w-[35rem] max-h-[calc(100dvh-2rem)] overflow-hidden rounded-xl border border-slate-200 bg-white p-0 text-slate-800 shadow-xl backdrop:bg-slate-900/25">
    <div className="flex max-h-[calc(100dvh-2rem)] min-w-0 flex-col">
      <header className="flex shrink-0 items-start gap-3 border-b border-slate-200 px-4 py-3">
        <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-amber-50 text-amber-700"><RotateCcw size={16} /></span>
        <div className="min-w-0 flex-1">
          <h2 id="request-recovery-title" className="text-sm font-semibold">恢复端点</h2>
          <p className="mt-0.5 text-xs text-slate-500">当前页失败请求涉及的冷却端点</p>
        </div>
        <button type="button" aria-label="关闭恢复端点" onClick={close} disabled={busy === 'release'}
          className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-500 hover:bg-slate-100 disabled:opacity-50">
          <X size={16} />
        </button>
      </header>
      <div className="min-h-0 overflow-y-auto px-4 py-3 text-xs [overflow-wrap:anywhere]" aria-busy={Boolean(busy)}>
        {busy === 'check' && <p role="status" className="py-1 text-slate-600">正在检查端点状态…</p>}
        {busy === 'release' && <p role="status" className="py-1 text-slate-600">正在解除冷却并检查状态…</p>}
        {result && <p role="status" className="mb-3 rounded-md bg-slate-50 px-3 py-2 leading-5 text-slate-700">
          {result.released ? `已解除 ${result.released} 项端点冷却。此操作不会重放当前请求，后续请求到达时才会再次选择这些端点。` : '本次没有解除冷却。'}
          {result.failures.size > 0 && ` ${result.failures.size} 项处理失败。`}
          {result.missing > 0 && ` ${result.missing} 项未能确认当前状态。`}
        </p>}
        {!busy && eligible.length === 0 && !result && <p className="mb-3 text-slate-600">当前没有可手动解除冷却的端点。</p>}
        <ul className="divide-y divide-slate-200">
          {entries.map(({ group, candidates, error, missing }) => <li key={group.key} className="space-y-2 py-2.5">
            <p className="font-semibold">{group.provider === 'codex' ? 'Codex' : 'Claude'} · {group.label}<span className="ml-2 font-normal text-slate-500">{group.model}</span></p>
            {error && <p className="text-rose-700">{error}</p>}
            {missing > 0 && <p className="text-slate-500">{missing} 个历史端点当前不可查询。</p>}
            {candidates.map(({ blockage, canRetry }) => {
              const key = recoveryCandidateKey(blockage);
              const failure = result?.failures.get(key);
              const status = blockage.recovery === 'probing' ? '正在验证' : !blockage.reason ? '可发送' : blockageLabel(blockage.reason);
              const recoveryAt = new Date(blockage.recoverAt);
              const retryTime = blockage.recoverAt && Number.isFinite(recoveryAt.getTime()) && recoveryAt.toLocaleString('zh-CN', { hour12: false });
              return <div key={key} className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 gap-y-1 pl-2">
                <span>{blockage.endpointName || `端点 #${blockage.endpointId}`}</span>
                <span className={canRetry && blockage.recovery !== 'probing' ? 'text-amber-800' : 'text-slate-600'}>{status}</span>
                {blockage.cause && <p className="col-span-2 text-slate-500">{describeFailureReason(blockage.cause) || blockage.cause}</p>}
                {retryTime && <time className="col-span-2 text-slate-500 tabular-nums" dateTime={recoveryAt.toISOString()}>{blockage.reason === 'daily_cost_limit_exceeded' ? '限额重置' : '可重试'} {retryTime}</time>}
                {failure && <p className="col-span-2 text-rose-700">{failure}</p>}
              </div>;
            })}
          </li>)}
        </ul>
      </div>
      <footer className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t border-slate-200 bg-slate-50 px-4 py-3">
        <Button type="button" variant="ghost" size="sm" icon={RefreshCw} loading={busy === 'check'} disabled={Boolean(busy)} onClick={refresh}>刷新状态</Button>
        <Button type="button" size="sm" icon={RotateCcw} disabled={Boolean(busy) || eligible.length === 0} onClick={release}>
          批量允许重试{eligible.length > 0 && `（${eligible.length} 项）`}
        </Button>
      </footer>
    </div>
  </dialog>;
}
