import { useEffect, useId, useRef } from 'react';
import { Trash2, TriangleAlert, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

export default function DeleteProfileConfirmDialog({ profile, busy, error, onCancel, onConfirm }) {
  const titleId = useId();
  const descriptionId = useId();
  const dialogRef = useRef(null);
  const cancelRef = useRef(null);

  useEffect(() => {
    const previousFocus = document.activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    cancelRef.current?.focus();
    return () => {
      document.body.style.overflow = previousOverflow;
      if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
    };
  }, []);

  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      if (!busy) onCancel();
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled)')];
    const first = controls[0], last = controls.at(-1);
    if (!controls.length) { event.preventDefault(); return; }
    if (event.shiftKey && document.activeElement === first || !event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      (event.shiftKey ? last : first)?.focus();
    }
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/30 p-4 backdrop-blur-sm"
      onMouseDown={(event) => { if (!busy && event.target === event.currentTarget) onCancel(); }}>
      <section ref={dialogRef} role="alertdialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descriptionId}
        aria-busy={busy} onKeyDown={onKeyDown}
        className="relative flex max-h-[calc(100dvh-2rem)] w-full max-w-md flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-xl">
        <header className="flex shrink-0 items-center gap-3 border-b border-slate-100 px-5 py-4">
          <span className="rounded-xl bg-rose-100 p-2 text-rose-600"><TriangleAlert size={18} /></span>
          <h2 id={titleId} className="min-w-0 flex-1 text-sm font-semibold text-slate-900">删除调度方案</h2>
          <button type="button" aria-label="关闭" className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 disabled:opacity-40"
            disabled={busy} onClick={onCancel}><X size={16} /></button>
        </header>
        <div className="min-h-0 overflow-y-auto px-5 py-4 text-sm leading-6 text-slate-600">
          <p id={descriptionId} className="wrap-anywhere">确认删除调度方案「<strong className="font-semibold text-slate-800">{profile.name}</strong>」？</p>
          <p className="mt-2 text-xs leading-5 text-slate-500">仅删除该方案的调度配置，渠道、分组、端点和历史请求记录继续保留。方案被其他方案引用时，无法删除。</p>
          {error && <p role="alert" className="mt-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700 wrap-anywhere">{error}</p>}
        </div>
        <footer className="flex shrink-0 justify-end gap-2 border-t border-slate-100 bg-slate-50/50 px-5 py-3">
          <Button ref={cancelRef} type="button" size="sm" variant="secondary" icon={X} disabled={busy} onClick={onCancel}>取消</Button>
          <Button type="button" size="sm" variant="danger" icon={Trash2} loading={busy} onClick={onConfirm}>确认删除</Button>
        </footer>
      </section>
    </div>, { root: true }
  );
}
