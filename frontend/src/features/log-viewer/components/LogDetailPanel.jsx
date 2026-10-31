import { Check, Copy, FileText, ListTree, X } from 'lucide-react';
import { useEffect, useRef } from 'react';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { formatLogTime, formatLogValue } from '../logPresentation.js';

export default function LogDetailPanel({ log, copied, onCopy, onClose }) {
  const panelRef = useRef(null);
  const closeRef = useRef(null);
  const fields = Object.entries(log.attrs || {});
  useEffect(() => {
    const previous = document.activeElement;
    closeRef.current?.focus({ preventScroll: true });
    return () => { if (previous?.isConnected) previous.focus({ preventScroll: true }); };
  }, []);
  useEffect(() => { panelRef.current.scrollTop = 0; }, [log]);

  return <aside className="system-log-detail" aria-label="日志详情" onKeyDown={(event) => { if (event.key === 'Escape') { event.stopPropagation(); onClose(); } }}>
    <header className="system-log-detail-header"><h2><FileText size={14} />日志详情</h2><div>
      <Tooltip asChild content={copied ? '已复制' : '复制完整日志'}><button type="button" className="system-log-icon" aria-label="复制完整日志" onClick={() => onCopy(log)}>{copied ? <Check size={14} className="text-emerald-600" /> : <Copy size={14} />}</button></Tooltip>
      <button ref={closeRef} type="button" className="system-log-icon" aria-label="关闭日志详情" onClick={onClose}><X size={15} /></button>
    </div></header>
    <div className="system-log-detail-scroll" ref={panelRef}>
      <div className="system-log-detail-meta"><span className="system-log-level-badge" data-level={log.level}>{log.level || 'INFO'}</span><time>{formatLogTime(log.timestamp, true)}</time></div>
      <section className="system-log-detail-section"><h3><FileText size={12} />日志消息</h3><pre className="system-log-detail-message">{log.message}</pre></section>
      <section className="system-log-detail-section"><h3><ListTree size={12} />附加字段<span>{fields.length}</span></h3>
        {fields.length ? <dl className="system-log-detail-fields">{fields.map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{formatLogValue(value)}</dd></div>)}</dl> : <p className="system-log-detail-empty">此日志没有附加字段</p>}
      </section>
    </div>
  </aside>;
}
