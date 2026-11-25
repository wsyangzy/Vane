import { useEffect, useRef, useState } from 'react';
import { RefreshCw, RotateCcw } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { getDispatchRecovery, retryDispatchCandidate } from '@/services/wails/dispatch.js';
import { dispatchRecoveryTarget, retryCurrentDispatchCandidate } from '@/shared/lib/dispatchRecovery.js';
import { blockageLabel, isHealthCooldown } from '../utils/blockage.js';

export default function RequestCandidateRecovery({ target }) {
  const [inspection, setInspection] = useState(null);
  const [busyScope, setBusyScope] = useState(null);
  const [error, setError] = useState(null);
  const [notice, setNotice] = useState(null);
  const request = useRef(0);
  const scopeKey = JSON.stringify([target?.provider, target?.channelId, target?.groupId, target?.upstreamModel, target?.model]);
  useEffect(() => () => { request.current++; }, [scopeKey]);
  const busy = busyScope === scopeKey;
  const states = inspection?.key === scopeKey ? inspection.states : null;
  const visibleError = error?.key === scopeKey ? error.message : '';
  const visibleNotice = notice?.key === scopeKey ? notice.message : '';
  if (!target?.channelId || !target.groupId) return <p className="text-xs leading-5 text-slate-500">此旧记录缺少资源编号，请在渠道资源或“额度与健康”中检查对应分组。</p>;
  const check = async (retry = null) => {
    const token = ++request.current;
    setBusyScope(scopeKey);
    setError(null);
    setNotice(null);
    try {
      const input = dispatchRecoveryTarget(target.provider, retry || { ...target, endpointId: '' });
      const result = retry
        ? await retryCurrentDispatchCandidate(input, { readRecovery: getDispatchRecovery, retryCandidate: retryDispatchCandidate })
        : { states: await getDispatchRecovery(input) };
      if (token !== request.current) return;
      setInspection({ key: scopeKey, states: retry ? (states || [result.state]).map(item => item.blockage?.endpointId === result.state.blockage?.endpointId && item.blockage?.model === result.state.blockage?.model ? result.state : item) : result.states });
      if (retry) setNotice({ key: scopeKey, message: result.released ? '已允许此端点重新尝试，后续请求会验证上游状态。' : '当前状态已变化，仍有其他阻塞需要处理。' });
    } catch (cause) {
      if (token === request.current) setError({ key: scopeKey, message: cause?.message || '读取恢复状态失败，请稍后再试。' });
    } finally {
      if (token === request.current) setBusyScope(null);
    }
  };
  return <div className="space-y-2 pt-1">
    <Button variant="ghost" size="sm" icon={RefreshCw} loading={busy} onClick={() => check()}>{states ? '刷新当前状态' : '查看当前状态'}</Button>
    {visibleError && <p role="alert" className="text-xs leading-5 text-rose-700 [overflow-wrap:anywhere]">{visibleError}</p>}
    {visibleNotice && <p role="status" className="text-xs leading-5 text-slate-700 [overflow-wrap:anywhere]">{visibleNotice}</p>}
    {states && <div className="space-y-1 border-l-2 border-slate-200 pl-2">
      {!states.some(item => item.blockage?.reason) && <p className="text-xs leading-5 text-slate-500">当前检查未发现阻塞。</p>}
      {states.filter(item => item.blockage?.reason).map(({ blockage, canRetry }) => {
        return <div key={`${blockage.endpointId}/${blockage.model}`} className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs leading-5 [overflow-wrap:anywhere]">
          <span className="font-medium text-slate-700">{blockage.endpointName || '端点'}</span>
          <span className="text-slate-500">{blockageLabel(blockage.reason)}</span>
          {canRetry && <Button type="button" variant="secondary" size="sm" icon={RotateCcw} disabled={busy} onClick={() => check(blockage)}>{isHealthCooldown(blockage.reason) ? '解除冷却' : '允许同组重试'}</Button>}
        </div>;
      })}
    </div>}
  </div>;
}
