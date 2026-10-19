import { useEffect, useId, useRef, useState } from 'react';
import { Cable, KeyRound, X } from 'lucide-react';
import { Button } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import SubscriptionAccountsPanel from './SubscriptionAccountsPanel.jsx';
import SubscriptionConnectionPanel from './SubscriptionConnectionPanel.jsx';

function SubscriptionDialog({ provider, channels, onUpdated, onClose }) {
  const [tab, setTab] = useState('accounts');
  const [busy, setBusy] = useState(false);
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const titleId = useId();
  const tabsId = useId();

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
  const tabs = [{ id: 'accounts', label: '订阅账号', icon: KeyRound }, { id: 'connection', label: '客户端接入', icon: Cable }];

  return renderModalPortal(<div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/40 p-3 backdrop-blur-sm sm:p-6" onClick={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={onKeyDown} className="flex max-h-[calc(100dvh-1.5rem)] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl sm:max-h-[calc(100dvh-3rem)]">
      <div className="flex shrink-0 items-center justify-between gap-3 border-b border-slate-100 px-5 py-4">
        <div className="flex items-center gap-3"><div className="rounded-lg bg-indigo-50 p-2 text-indigo-600"><KeyRound size={18} /></div><div><h2 id={titleId} className="text-base font-semibold text-slate-900">{title} 订阅管理</h2><p className="mt-0.5 text-xs text-slate-500">管理订阅账号与本机客户端接入</p></div></div>
        <button ref={closeRef} type="button" aria-label="关闭订阅管理" disabled={busy} onClick={onClose} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700 disabled:opacity-40"><X size={18} /></button>
      </div>
      <div role="tablist" aria-label="订阅管理功能" className="flex shrink-0 gap-1 border-b border-slate-100 px-4 pt-2">
        {tabs.map(({ id, label, icon: Icon }, index) => <button key={id} id={`${tabsId}-${id}-tab`} role="tab" type="button" aria-selected={tab === id} aria-controls={`${tabsId}-${id}-panel`} tabIndex={tab === id ? 0 : -1} disabled={busy} onClick={() => setTab(id)} onKeyDown={(event) => {
          if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
          event.preventDefault();
          const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
          setTab(tabs[next].id);
          document.getElementById(`${tabsId}-${tabs[next].id}-tab`)?.focus();
        }} className={`flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium disabled:opacity-50 ${tab === id ? 'border-indigo-600 text-indigo-600' : 'border-transparent text-slate-500 hover:text-slate-800'}`}><Icon size={15} />{label}</button>)}
      </div>
      <div className="min-h-0 overflow-y-auto overscroll-contain p-1 sm:p-2">
        {tab === 'accounts' ? <div id={`${tabsId}-accounts-panel`} role="tabpanel" aria-labelledby={`${tabsId}-accounts-tab`}><SubscriptionAccountsPanel provider={provider} embedded onBusyChange={setBusy} /></div> : <div id={`${tabsId}-connection-panel`} role="tabpanel" aria-labelledby={`${tabsId}-connection-tab`}><SubscriptionConnectionPanel provider={provider} channels={channels} onUpdated={onUpdated} embedded onBusyChange={setBusy} /></div>}
      </div>
    </div>
  </div>);
}

export default function SubscriptionManagement({ provider, channels = [], onUpdated }) {
  const [open, setOpen] = useState(false);
  return <>
    <Button type="button" variant="secondary" size="sm" icon={KeyRound} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(true)}>订阅管理</Button>
    {open && <SubscriptionDialog provider={provider} channels={channels} onUpdated={onUpdated} onClose={() => setOpen(false)} />}
  </>;
}
