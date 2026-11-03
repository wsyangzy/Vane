import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ArrowUpRight, Route, X } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { getEndpointChainItems, parseChainNode } from '../utils/endpointChain.js';
import RequestDispatchDetails from './RequestDispatchDetails.jsx';

export default function RequestRouteCell({ request, onViewDetails, compact = false }) {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ left: 8, top: 8, maxHeight: 320 });
  const trigger = useRef(null);
  const panel = useRef(null);
  const keyboardOpen = useRef(false);
  const panelId = useId();
  const trace = request.dispatchTrace;
  const chain = getEndpointChainItems(request).map(parseChainNode);
  const legacyEndpoint = chain.at(-1)?.endpoint || request.endpoint;
  const close = useCallback((restoreFocus = false) => {
    setOpen(false);
    if (restoreFocus) trigger.current?.focus();
  }, []);
  useLayoutEffect(() => {
    if (!open) return;
    const rect = trigger.current.getBoundingClientRect();
    const above = rect.top > innerHeight - rect.bottom;
    const width = Math.min(660, innerWidth - 16);
    setPosition({ left: Math.max(8, Math.min(rect.left + rect.width / 2 - width / 2, innerWidth - width - 8)), width, maxHeight: Math.max(120, (above ? rect.top : innerHeight - rect.bottom) - 16), ...(above ? { bottom: innerHeight - rect.top + 6 } : { top: rect.bottom + 6 }) });
    if (keyboardOpen.current) panel.current?.querySelector('button')?.focus();
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const outside = event => { if (!trigger.current?.contains(event.target) && !panel.current?.contains(event.target)) setOpen(false); };
    const escape = event => { if (event.key === 'Escape' && !event.defaultPrevented) { event.preventDefault(); close(true); } };
    const scroll = event => { if (!panel.current?.contains(event.target)) setOpen(false); };
    const resize = () => setOpen(false);
    document.addEventListener('pointerdown', outside, true);
    document.addEventListener('focusin', outside);
    document.addEventListener('keydown', escape);
    window.addEventListener('scroll', scroll, true);
    window.addEventListener('resize', resize);
    return () => { document.removeEventListener('pointerdown', outside, true); document.removeEventListener('focusin', outside); document.removeEventListener('keydown', escape); window.removeEventListener('scroll', scroll, true); window.removeEventListener('resize', resize); };
  }, [open, close]);
  if (!trace && !legacyEndpoint) return <span className="text-[11px] text-slate-400">未记录</span>;
  return <>
    <Tooltip asChild content={open ? '' : trace ? `调度方案：${trace.profileName}；${trace.decisionLabel}；${trace.attemptCount} 次上游尝试` : `历史端点：${legacyEndpoint}`}>
    <button ref={trigger} type="button" className="request-route-cell" data-compact={compact} aria-label={`查看请求调度 ${trace?.profileName || legacyEndpoint}${trace ? `，${trace.decisionLabel}，${trace.attemptCount} 次上游尝试` : '，历史链路'}`} aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? panelId : undefined}
      onClick={event => { event.stopPropagation(); keyboardOpen.current = event.detail === 0; setOpen(previous => !previous); }} onDoubleClick={event => event.stopPropagation()}>
      <Route className="request-route-icon" size={12} aria-hidden="true" /><span className="request-route-name">{trace?.profileName || legacyEndpoint}</span>
      {trace && (trace.attemptCount > 1 || trace.attemptCount === 0) && <span className="request-route-count" data-empty={trace.attemptCount === 0}>{trace.attemptCount === 0 ? trace.finished || trace.stopReason ? '未发送' : '待调度' : `${trace.attemptCount}次`}</span>}
      {!trace && <span className="request-route-history">历史</span>}
    </button>
    </Tooltip>
    {open && createPortal(<div ref={panel} id={panelId} role="dialog" aria-label="请求调度" className="request-route-popover" style={position} onClick={event => event.stopPropagation()} onDoubleClick={event => event.stopPropagation()}>
      <div className="request-route-popover-header"><span><Route size={13} aria-hidden="true" />请求调度</span><button type="button" aria-label="关闭请求调度" onClick={() => close(true)}><X size={14} /></button></div>
      <div className="request-route-popover-body">
      {trace ? <RequestDispatchDetails trace={trace} /> : <section className="request-dispatch-details" data-expanded="false"><header className="request-dispatch-heading"><Route size={14} /><strong>历史调用链路</strong></header><ol className="request-dispatch-legacy">{(chain.length ? chain : [{ channel: request.channel, credentialGroup: request.group, endpoint: legacyEndpoint }]).map((node, index) => <li key={index}><span className="request-dispatch-sequence" aria-label={`第 ${index + 1} 个链路节点`}>{String(index + 1).padStart(2, '0')}</span><div className="request-dispatch-resource"><strong>{node.channel || '渠道未记录'}</strong><span>{[node.credentialGroup, node.endpoint].filter(Boolean).join(' / ')}</span></div></li>)}</ol><p className="request-dispatch-footnote">此记录未保存调度方案快照，按原有端点链路显示。</p></section>}
      </div>
      {onViewDetails && <div className="request-route-popover-footer"><button type="button" onClick={() => { close(true); onViewDetails(); }}>查看完整调度<ArrowUpRight size={13} aria-hidden="true" /></button></div>}
    </div>, document.body)}
  </>;
}
