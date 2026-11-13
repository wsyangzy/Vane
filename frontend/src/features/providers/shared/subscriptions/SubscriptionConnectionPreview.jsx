import { useId, useLayoutEffect, useRef } from 'react';
import { Copy, FileCode2, RefreshCw, Save, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

export default function SubscriptionConnectionPreview({ provider, preview, confirmed, onConfirmedChange, busy, stale, canApply, error, notice, onCopy, onRegenerate, onApply, onClose, triggerRef }) {
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const focused = useRef(false);
  const titleId = useId();
  const title = provider === 'codex' ? 'Codex' : 'Claude Code';

  useLayoutEffect(() => {
    const previousFocus = triggerRef.current || document.activeElement;
    const parentDialog = previousFocus?.closest('[role="dialog"]');
    const previousInert = parentDialog?.inert;
    const previousHidden = parentDialog?.getAttribute('aria-hidden');
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    if (parentDialog) { parentDialog.inert = true; parentDialog.setAttribute('aria-hidden', 'true'); }
    return () => {
      if (parentDialog) {
        parentDialog.inert = previousInert;
        if (previousHidden == null) parentDialog.removeAttribute('aria-hidden');
        else parentDialog.setAttribute('aria-hidden', previousHidden);
      }
      document.body.style.overflow = previousOverflow;
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [triggerRef]);

  useLayoutEffect(() => {
    // The preview can mount before its generating request has cleared busy.
    // A disabled close button cannot receive focus until that request finishes.
    if (!busy && (!focused.current || !dialogRef.current?.contains(document.activeElement))) {
      closeRef.current?.focus(); focused.current = true;
    }
  }, [busy]);

  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopPropagation();
      if (!busy) onClose();
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), input:not(:disabled), [tabindex="0"]')]
      .filter((element) => element.getClientRects().length > 0);
    const first = controls[0], last = controls.at(-1);
    if ((event.shiftKey && document.activeElement === first) || (!event.shiftKey && document.activeElement === last)) {
      event.preventDefault(); (event.shiftKey ? last : first)?.focus();
    }
  };

  return renderModalPortal(<div className="fixed inset-0 z-[11000] flex items-center justify-center bg-slate-900/40 p-3 backdrop-blur-sm sm:p-4" onClick={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={onKeyDown} className="flex h-[min(46rem,calc(100dvh-2rem))] min-h-0 w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl">
      <div className="flex shrink-0 items-start justify-between gap-3 border-b border-slate-100 px-4 py-3">
        <div className="flex min-w-0 items-start gap-3">
          <div className="shrink-0 rounded-lg bg-indigo-50 p-2 text-indigo-600"><FileCode2 size={18} /></div>
          <div className="min-w-0"><h2 id={titleId} className="text-sm font-semibold text-slate-900">{title} 配置预览</h2><p className="mt-1 break-all text-xs leading-5 text-slate-500">目标文件：{preview.configPath}</p></div>
        </div>
        <button ref={closeRef} type="button" aria-label="关闭配置预览" disabled={Boolean(busy)} onClick={onClose} className="shrink-0 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700 disabled:opacity-40"><X size={18} /></button>
      </div>
      <div className="flex shrink-0 items-center justify-between gap-2 bg-slate-50 px-4 py-2">
        <span className="text-xs font-medium text-slate-600">{provider === 'codex' ? 'config.toml 接入与模型配置' : 'settings.json 接入字段'}</span>
        <Button type="button" size="sm" variant="ghost" className="gap-1.5" onClick={() => onCopy(preview.snippet)}><Copy size={13} />复制配置</Button>
      </div>
      <div tabIndex={0} aria-label="配置内容" className="scrollbar-visible min-h-0 flex-1 overflow-y-auto overscroll-contain border-y border-slate-100 px-4 py-3 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-indigo-200">
        <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-5 text-slate-800">{preview.snippet}</pre>
        {provider === 'claude' && <div className="mt-4 border-t border-slate-100 pt-3">
          <div className="mb-2 flex items-center justify-between gap-2"><span className="text-xs font-medium text-slate-600">临时启动（当前 PowerShell 窗口）</span><Button type="button" size="sm" variant="ghost" className="gap-1.5" onClick={() => onCopy(preview.command)}><Copy size={13} />复制命令</Button></div>
          <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-5 text-slate-800">{preview.command}</pre>
        </div>}
      </div>
      <div className="shrink-0 space-y-2 px-4 py-3">
        <p className="text-xs leading-5 text-slate-500">应用前加密备份原配置，重启 {title} 后生效。{provider === 'codex' && (preview.modelCatalogMode === 'local' ? '模型目录使用所选本地 JSON。' : '模型目录通过 Vane 获取。')}</p>
        <label className="flex items-start gap-2 text-xs leading-5 text-slate-700"><input type="checkbox" checked={confirmed} onChange={(event) => onConfirmedChange(event.target.checked)} disabled={Boolean(busy)} className="mt-0.5 accent-indigo-600" />我已核对目标文件和接入字段，允许将配置写入本机 {title}。</label>
        {stale && <p role="alert" className="text-xs leading-5 text-amber-700">接入设置或配置文件已变化，请重新生成预览。</p>}
        {error && <p role="alert" className="break-words text-xs text-rose-600 [overflow-wrap:anywhere]">{error}</p>}
        {notice && <p role="status" className="text-xs text-emerald-700">{notice}</p>}
        <div className="flex flex-wrap items-center justify-between gap-2 pt-1">
          <Button type="button" size="sm" variant="secondary" onClick={onClose} disabled={Boolean(busy)}>返回设置</Button>
          <div className="flex flex-wrap gap-2">{stale && <Button type="button" size="sm" variant="secondary" className="gap-1.5" onClick={onRegenerate} disabled={Boolean(busy)}><RefreshCw size={14} />重新生成</Button>}<Button type="button" size="sm" className="gap-1.5" onClick={onApply} disabled={Boolean(busy) || !confirmed || !canApply || stale}><Save size={14} />{busy === 'apply' ? '正在应用…' : '备份并应用配置'}</Button></div>
        </div>
      </div>
    </div>
  </div>, { root: true });
}
