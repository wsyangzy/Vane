import { useState } from 'react';
import { RefreshCw, RotateCcw } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { getDispatchRecovery, retryDispatchCandidate } from '@/services/wails/dispatch.js';
import { describeBlockageRecovery } from '../utils/blockage.js';
import { describeFailureReason } from '../utils/failureSummary.js';

export default function RequestCandidateRecovery({ target }) {
  const [states, setStates] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  if (!target?.channelId || !target.groupId) return <p className="text-xs leading-5 text-slate-500">此旧记录缺少资源编号，请在渠道资源或“额度与健康”中检查对应分组。</p>;
  const check = async (retry = null) => {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const input = { ...target, endpointId: retry?.endpointId || '' };
      const result = await (retry ? retryDispatchCandidate(input) : getDispatchRecovery(input));
      setStates(current => retry && current ? current.map(item => result.find(updated => updated.blockage?.endpointId === item.blockage?.endpointId) || item) : result);
      if (retry) setNotice('已允许同一分组重新尝试。请重新发起业务请求，成功后才会确认恢复；失败时仍会进入冷却或待修复状态。');
    } catch (cause) {
      setError(cause?.message || '读取恢复状态失败，请稍后再试。');
    } finally {
      setBusy(false);
    }
  };
  return <div className="space-y-2 pt-1">
    <Button variant="ghost" size="sm" icon={RefreshCw} loading={busy} onClick={() => check()}>{states ? '刷新当前状态' : '查看当前状态'}</Button>
    {error && <p role="alert" className="text-xs leading-5 text-rose-700 [overflow-wrap:anywhere]">{error}</p>}
    {notice && <p role="status" className="text-xs leading-5 text-slate-700 [overflow-wrap:anywhere]">{notice}</p>}
    {states && <div className="space-y-2 rounded-lg border border-slate-200 bg-white p-3">
      <p className="text-xs font-semibold text-slate-700">当前资源状态</p>
      <p className="text-xs leading-5 text-slate-500">检查此分组的资源准入条件；实际请求还会检查调度规则与会话兼容性。</p>
      {states.map(({ blockage, canRetry }) => {
        const display = describeBlockageRecovery(blockage);
        return <div key={`${blockage.endpointId}/${blockage.model}`} className="space-y-1 border-t border-slate-100 pt-2 text-xs leading-5 [overflow-wrap:anywhere]">
          <p className="font-medium text-slate-700">{blockage.endpointName || '端点'} · {display.label}</p>
          {blockage.model && <p className="text-slate-500">模型：{blockage.model}</p>}
          {display.costs && <p className="text-slate-700">{display.costs}</p>}
          {blockage.cause && <p className="text-slate-600">触发原因：{describeFailureReason(blockage.cause) || blockage.cause}</p>}
          <p className="text-slate-600">{display.recovery}</p>
          {display.action && <p className="text-slate-700">处理：{display.action}</p>}
          {canRetry && <Button variant="secondary" size="sm" icon={RotateCcw} disabled={busy} onClick={() => check(blockage)}>已处理原因，允许同组重试</Button>}
          {blockage.observedAt && <p className="text-slate-400">检查时间：{new Date(blockage.observedAt).toLocaleString('zh-CN', { hour12: false })}</p>}
        </div>;
      })}
    </div>}
  </div>;
}
