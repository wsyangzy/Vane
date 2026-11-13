import { useEffect, useId, useRef, useState } from 'react';
import { Cable, KeyRound, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import SubscriptionAccountsPanel from './SubscriptionAccountsPanel.jsx';
import SubscriptionConnectionPanel from './SubscriptionConnectionPanel.jsx';

function SubscriptionDialog({ provider, channels, onUpdated, onClose, connection = false }) {
  const [busy, setBusy] = useState(false);
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const titleId = useId();

  useEffect(() => {
    const previousFocus = document.activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus();
    return () => {
      document.body.style.overflow = previousOverflow;
      previousFocus?.focus();
    };
  }, []);

  const onKeyDown = (event) => {
    // A portaled select handles its own keys outside this logical dialog.
    if (!dialogRef.current?.contains(event.target)) return;
    if (event.key === 'Escape') {
      if (dialogRef.current.querySelector('[aria-haspopup="listbox"][aria-expanded="true"]')) return;
      event.stopPropagation();
      if (!busy) onClose();
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]')]
      .filter((element) => element.tabIndex >= 0 && element.getClientRects().length > 0);
    const first = controls[0];
    const last = controls.at(-1);
    if ((event.shiftKey && document.activeElement === first) || (!event.shiftKey && document.activeElement === last)) {
      event.preventDefault();
      (event.shiftKey ? last : first)?.focus();
    }
  };
  const title = provider === 'codex' ? 'Codex' : 'Claude';
  const clientName = provider === 'codex' ? 'Codex' : 'Claude Code';

  return renderModalPortal(<div className={`fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/40 p-3 backdrop-blur-sm ${connection ? 'sm:p-4' : 'sm:p-6'}`} onClick={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={onKeyDown} className={`flex max-h-[calc(100dvh-1.5rem)] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl ${connection ? 'sm:max-h-[calc(100dvh-2rem)]' : 'sm:max-h-[calc(100dvh-3rem)]'}`}>
      <div className={`flex shrink-0 items-center justify-between gap-3 border-b border-slate-100 ${connection ? 'px-4 py-3' : 'px-5 py-4'}`}>
        <div className="flex items-center gap-3"><div className="rounded-lg bg-indigo-50 p-2 text-indigo-600">{connection ? <Cable size={18} /> : <KeyRound size={18} />}</div><div><h2 id={titleId} className="text-base font-semibold text-slate-900">{connection ? `${clientName} 客户端配置` : `${title} 订阅管理`}</h2><p className="mt-0.5 text-xs text-slate-500">{connection ? `将本机 ${clientName} 连接到 Vane，适用于 API 渠道和官方订阅` : '登录、导入和管理由 Vane 托管的订阅账号'}</p></div></div>
        <button ref={closeRef} type="button" aria-label={connection ? '关闭客户端配置' : '关闭订阅管理'} disabled={busy} onClick={onClose} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700 disabled:opacity-40"><X size={18} /></button>
      </div>
      <div className={`min-h-0 overflow-y-auto overscroll-contain p-1 ${connection ? '' : 'sm:p-2'}`}>
        {connection ? <SubscriptionConnectionPanel provider={provider} channels={channels} onUpdated={onUpdated} embedded onBusyChange={setBusy} /> : <SubscriptionAccountsPanel provider={provider} embedded onBusyChange={setBusy} />}
      </div>
    </div>
  </div>);
}

export function ClientConnectionButton({ provider, onUpdated, className = '', disabled = false }) {
  const [open, setOpen] = useState(false);
  return <>
    <Button type="button" variant="secondary" size="sm" icon={Cable} className={className} disabled={disabled} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(true)}>客户端配置</Button>
    {open && <SubscriptionDialog provider={provider} onUpdated={onUpdated} connection onClose={() => setOpen(false)} />}
  </>;
}

export default function SubscriptionManagement({ provider, channels = [], onUpdated, className = '', disabled = false }) {
  const [open, setOpen] = useState(false);
  return <>
    <Button type="button" variant="secondary" size="sm" icon={KeyRound} className={className} disabled={disabled} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(true)}>订阅管理</Button>
    {open && <SubscriptionDialog provider={provider} channels={channels} onUpdated={onUpdated} onClose={() => setOpen(false)} />}
  </>;
}
