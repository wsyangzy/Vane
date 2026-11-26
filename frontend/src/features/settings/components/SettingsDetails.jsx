import { useId, useLayoutEffect, useRef, useState } from 'react';
import { ChevronRight, SlidersHorizontal, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { SegmentedTabs } from '@/shared/ui/SegmentedTabs.jsx';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

function SettingsDetailsDialog({ title, pages, onClose, triggerRef }) {
  const [activePage, setActivePage] = useState(pages[0].key);
  const titleId = useId(), panelId = useId();
  const dialogRef = useRef(null), closeRef = useRef(null), scrollRef = useRef(null);
  const current = pages.find(page => page.key === activePage) || pages[0];

  useLayoutEffect(() => {
    const previousFocus = triggerRef.current || document.activeElement;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus();
    return () => {
      document.body.style.overflow = overflow;
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [triggerRef]);

  const handleKeyDown = event => {
    if (event.defaultPrevented || dialogRef.current?.inert) return;
    // CustomSelect renders its menu in a separate portal and owns its Escape key.
    if (document.querySelector('[role="listbox"]')) return;
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopPropagation(); onClose();
    }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') onClose();
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), input:not(:disabled), summary, [tabindex="0"]')]
      .filter(element => element.getClientRects().length);
    const first = controls[0], last = controls.at(-1);
    if ((event.shiftKey && document.activeElement === first) || (!event.shiftKey && document.activeElement === last)) {
      event.preventDefault(); (event.shiftKey ? last : first)?.focus();
    }
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/40 p-3 backdrop-blur-sm sm:p-4" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={handleKeyDown} className="flex max-h-[calc(100dvh-2rem)] w-full max-w-xl flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl">
        <div className="flex shrink-0 items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
          <div className="flex min-w-0 items-center gap-2.5"><SlidersHorizontal size={17} className="shrink-0 text-indigo-600" /><h2 id={titleId} className="text-sm font-semibold text-slate-900">{title}</h2></div>
          <button ref={closeRef} type="button" onClick={onClose} aria-label={`关闭${title}`} className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300"><X size={18} /></button>
        </div>
        {pages.length > 1 && <div className="shrink-0 px-4 pt-3" role="group" aria-label="配置分页" aria-controls={panelId}>
          <SegmentedTabs tabs={pages.map(page => ({ key: page.key, label: page.label }))} activeTab={current.key} onChange={key => { setActivePage(key); scrollRef.current?.scrollTo({ top: 0 }); }} className={pages.length === 3 ? 'grid-cols-3' : 'grid-cols-2'} buttonPaddingClass="px-2" />
        </div>}
        <div ref={scrollRef} id={panelId} role="region" aria-label={current.label} className="min-h-0 overflow-y-auto overscroll-contain px-4 py-2">
          {current.content}
          {current.description && <p className="my-2 rounded-lg bg-slate-50 px-2.5 py-2 text-[11px] leading-5 text-slate-500 [overflow-wrap:anywhere]">{current.description}</p>}
        </div>
        <div className="flex shrink-0 items-center justify-between gap-3 border-t border-slate-100 px-4 py-3">
          <p className="text-[11px] leading-5 text-slate-500">修改会保留，点击页面顶部“保存”后生效。</p>
          <Button size="sm" variant="secondary" icon={ChevronRight} onClick={onClose} className="shrink-0">完成配置</Button>
        </div>
      </div>
    </div>,
  );
}

export default function SettingsDetails({ title, summary, pages, changedCount = 0, disabled = false }) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef(null);
  return <>
    <button ref={triggerRef} type="button" disabled={disabled} onClick={() => setOpen(true)} aria-haspopup="dialog" aria-expanded={open} className="my-2 flex w-full min-w-0 items-center gap-2 rounded-lg bg-slate-50 px-2.5 py-2 text-left transition-colors hover:bg-indigo-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:opacity-50">
      <SlidersHorizontal size={14} className="shrink-0 text-slate-500" />
      <span className="min-w-0 flex-1"><span className="flex flex-wrap items-center gap-2 text-xs font-medium text-slate-700">{title}{changedCount > 0 && <span className="text-[10px] text-amber-700">{changedCount} 项待保存</span>}</span>{summary && <span className="mt-0.5 block text-[11px] leading-4 text-slate-500 [overflow-wrap:anywhere]">{summary}</span>}</span>
      <ChevronRight size={14} className="shrink-0 text-slate-400" />
    </button>
    {open && <SettingsDetailsDialog title={title} pages={pages} onClose={() => setOpen(false)} triggerRef={triggerRef} />}
  </>;
}
