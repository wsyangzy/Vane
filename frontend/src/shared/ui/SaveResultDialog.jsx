import { useEffect, useId, useRef } from 'react';
import { AlertTriangle, X } from 'lucide-react';
import { Button } from './Button.jsx';
import { renderModalPortal } from './modalPortal.js';

export default function SaveResultDialog({ isOpen, result, onClose }) {
  const titleId = useId();
  const errorId = useId();
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const visible = Boolean(isOpen && result && !result.success && result.error !== 'cancelled');

  useEffect(() => {
    if (!visible) return undefined;
    const previousFocus = document.activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus();
    return () => {
      document.body.style.overflow = previousOverflow;
      if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
    };
  }, [visible]);

  if (!visible) return null;

  const handleKeyDown = (event) => {
    if (event.key === 'Escape') { event.preventDefault(); onClose(); }
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled)')];
    const first = controls[0], last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first || !event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      (event.shiftKey ? last : first)?.focus();
    }
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/20 p-4 backdrop-blur-sm" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={errorId} onKeyDown={handleKeyDown}
        className="relative flex max-h-[calc(100dvh-2rem)] w-full max-w-[420px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl">
        <header className="flex shrink-0 items-center gap-2.5 border-b border-slate-100 px-4 py-3">
          <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-rose-50 text-rose-500"><AlertTriangle size={16} /></span>
          <h2 id={titleId} className="min-w-0 flex-1 text-sm font-semibold text-slate-800">日志导出失败</h2>
          <button ref={closeRef} type="button" aria-label="关闭导出结果" onClick={onClose}
            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 focus-visible:outline-2 focus-visible:outline-indigo-500"><X size={15} /></button>
        </header>
        <div className="min-h-0 overflow-y-auto px-4 py-4">
          <p className="mb-3 text-xs leading-5 text-slate-500">日志文件未能保存，请根据错误信息检查后重试。</p>
          <div className="rounded-lg border border-slate-200 bg-slate-50/70 p-3">
            <p className="mb-1.5 text-[10px] font-medium text-slate-400">错误原因</p>
            <pre id={errorId} className="max-h-[min(35dvh,14rem)] overflow-y-auto whitespace-pre-wrap font-mono text-[11px] leading-5 text-slate-600 [overflow-wrap:anywhere]">{result.error || '保存日志时发生未知错误'}</pre>
          </div>
        </div>
        <footer className="flex shrink-0 justify-end border-t border-slate-100 bg-slate-50/50 px-4 py-2.5">
          <Button type="button" size="sm" icon={X} onClick={onClose}>关闭</Button>
        </footer>
      </section>
    </div>
  );
}
