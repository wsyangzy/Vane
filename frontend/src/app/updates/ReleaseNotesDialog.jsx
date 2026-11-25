import { useEffect, useId, useRef } from 'react';
import { FileText, X } from 'lucide-react';
import { Button } from '@/shared/ui/Button.jsx';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

export default function ReleaseNotesDialog({ announcement, onClose, returnFocusRef }) {
  const titleId = useId();
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const visible = Boolean(announcement);

  useEffect(() => {
    if (!visible) return undefined;
    const previousFocus = returnFocusRef?.current || document.activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus({ preventScroll: true });
    return () => {
      document.body.style.overflow = previousOverflow;
      if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
    };
  }, [visible, returnFocusRef]);

  if (!visible) return null;

  const sections = announcement.sections || [];
  const itemCount = sections.reduce((count, section) => count + section.items.length, 0);
  const handleKeyDown = event => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onClose();
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), [tabindex="0"]')];
    const first = controls[0], last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first || !event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      (event.shiftKey ? last : first)?.focus();
    }
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/20 p-4 backdrop-blur-sm"
      onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={handleKeyDown}
        className="relative flex max-h-[min(80dvh,760px)] w-full max-w-[720px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl">
        <header className="flex shrink-0 items-center gap-3 border-b border-slate-100 px-5 py-4">
          <span className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600"><FileText size={18} aria-hidden="true" /></span>
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="text-sm font-semibold text-slate-900">更新内容</h2>
            <p className="mt-0.5 text-xs text-slate-500">Vane <span className="font-medium text-indigo-600">v{announcement.version}</span></p>
          </div>
          <button ref={closeRef} type="button" aria-label="关闭更新内容" onClick={onClose}
            className="motion-control inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 focus-visible:outline-2 focus-visible:outline-indigo-500"><X size={17} /></button>
        </header>
        <div role="region" aria-label="版本更新内容" tabIndex={0}
          className="scrollbar-visible min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 py-5 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-indigo-500">
          {sections.length ? <div className="space-y-5">
            {sections.map((section, sectionIndex) => <section key={`${section.title}/${sectionIndex}`}>
              <h3 className="mb-2 text-[13px] font-semibold text-slate-800">{section.title}</h3>
              <ul className="list-disc space-y-1.5 pl-4 text-xs leading-6 text-slate-600 marker:text-indigo-400">
                {section.items.map((item, index) => <li key={index} className="pl-0.5 [overflow-wrap:anywhere]">{item}</li>)}
              </ul>
            </section>)}
          </div> : <p className="py-6 text-center text-sm text-slate-500">当前版本暂无更新说明。</p>}
        </div>
        <footer className="flex shrink-0 items-center justify-between gap-3 border-t border-slate-100 bg-slate-50/50 px-5 py-3">
          <span className="text-[11px] text-slate-400">{itemCount > 0 ? `${sections.length} 类 · ${itemCount} 项更新` : `v${announcement.version}`}</span>
          <Button type="button" size="sm" icon={X} onClick={onClose}>关闭</Button>
        </footer>
      </section>
    </div>
  );
}
