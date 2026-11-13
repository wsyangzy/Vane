import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { FlaskConical, X } from 'lucide-react';
import { Button, ErrorMessage } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import PreviewPanel from './PreviewPanel.jsx';

export default function DispatchPreviewDialog({ profile, channels, accounts, busy, perform, triggerRef, onClose }) {
  const titleId = useId();
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const [error, setError] = useState('');
  useLayoutEffect(() => {
    const previousOverflow = document.body.style.overflow;
    const previousFocus = triggerRef.current;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus();
    return () => { document.body.style.overflow = previousOverflow; previousFocus?.focus({ preventScroll: true }); };
  }, [triggerRef]);
  const run = operation => perform(async () => {
    setError('');
    try { return await operation(); }
    catch (err) { setError(getErrorMessage(err, '模拟调度失败')); throw err; }
  });
  useEffect(() => {
    const onKeyDown = event => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (!busy) onClose(); return; }
      if (event.key !== 'Tab') return;
      const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), summary, [tabindex="0"]')]
        .filter(element => !element.closest('[inert]') && element.getClientRects().length > 0);
      // Disabling a pending action can move focus to the document body.
      if (!dialogRef.current.contains(document.activeElement) || (event.shiftKey && document.activeElement === controls[0]) || (!event.shiftKey && document.activeElement === controls.at(-1))) {
        event.preventDefault(); (event.shiftKey ? controls.at(-1) : controls[0])?.focus();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [busy, onClose]);
  return renderModalPortal(<div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/40 p-3 backdrop-blur-sm sm:p-4" onClick={event => { if (event.target === event.currentTarget && !busy) onClose(); }}>
    <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} className="dp-preview-dialog flex max-h-[calc(100dvh-2rem)] min-h-0 w-full max-w-4xl flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-2xl">
      <header className="flex min-w-0 shrink-0 items-center gap-3 border-b border-slate-100 px-4 py-3"><span className="shrink-0 rounded-lg bg-indigo-50 p-2 text-indigo-600"><FlaskConical size={18} /></span><div className="min-w-0 flex-1"><h2 id={titleId} className="text-sm font-semibold text-slate-900">模拟调度</h2><p className="mt-1 break-words text-xs text-slate-500 [overflow-wrap:anywhere]">{profile.name}</p></div><span className="dp-badge">只读预览</span><button ref={closeRef} type="button" aria-label="关闭模拟调度" className="dp-icon-button" disabled={busy} onClick={onClose}><X size={18} /></button></header>
      <div className="scrollbar-visible min-h-0 overflow-y-auto overscroll-contain">
        {error && <div className="px-3 pt-3"><ErrorMessage message={error} onClose={() => setError('')} /></div>}
        <PreviewPanel key={`${profile.id}/${profile.revision}`} provider={profile.provider} selected={profile} channels={channels} accounts={accounts} busy={busy} perform={run} embedded />
      </div>
      <footer className="flex shrink-0 justify-end border-t border-slate-100 px-4 py-3"><Button type="button" size="sm" variant="secondary" disabled={busy} onClick={onClose}>关闭</Button></footer>
    </section>
  </div>, { root: true });
}
