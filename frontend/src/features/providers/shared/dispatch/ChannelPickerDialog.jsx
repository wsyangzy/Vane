import { useEffect, useId, useRef, useState } from 'react';
import { Check, Layers, Plus, Search, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

export default function ChannelPickerDialog({ channels, accounts, selectedIds, busy, onConfirm, onClose, returnFocusRef }) {
  const [query, setQuery] = useState('');
  const [draftIds, setDraftIds] = useState(() => [...selectedIds]);
  const titleId = useId();
  const descriptionId = useId();
  const dialog = useRef(null);
  const search = useRef(null);
  useEffect(() => {
    const fallbackFocus = returnFocusRef.current;
    const activeElement = document.activeElement;
    const previousFocus = dialog.current?.contains(activeElement) ? fallbackFocus : activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    search.current?.focus();
    return () => {
      document.body.style.overflow = previousOverflow;
      const target = previousFocus?.isConnected ? previousFocus : fallbackFocus;
      target?.focus();
    };
  }, [returnFocusRef]);
  const matches = channels.filter((channel) => channel.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      if (!busy) onClose();
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialog.current.querySelectorAll('button:not(:disabled), input:not(:disabled)')].filter((element) => element.getClientRects().length);
    const first = controls[0];
    const last = controls.at(-1);
    if (!controls.length) { event.preventDefault(); return; }
    if (event.shiftKey && document.activeElement === first || !event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      (event.shiftKey ? last : first).focus();
    }
  };
  return renderModalPortal(<div className="profile-picker-overlay" onClick={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
    <div ref={dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descriptionId} onKeyDown={onKeyDown} className="profile-picker-dialog">
      <header className="profile-picker-dialog-header"><span className="profile-picker-dialog-icon"><Layers size={19} /></span><div><h2 id={titleId}>选择渠道</h2><p id={descriptionId}>点击卡片选中或取消，确认后更新方案编排。</p></div><button type="button" className="profile-picker-close" aria-label="关闭渠道选择" disabled={busy} onClick={onClose}><X size={18} /></button></header>
      <div className="profile-picker-toolbar"><label className="profile-channel-search"><Search size={15} /><input ref={search} aria-label="搜索可添加的渠道" placeholder="搜索渠道名称" value={query} disabled={busy} onChange={(event) => setQuery(event.target.value)} />{query && <button type="button" aria-label="清空渠道搜索" disabled={busy} onClick={() => { setQuery(''); search.current?.focus(); }}><X size={14} /></button>}</label><span>{channels.length} 个渠道 · 已选 {draftIds.length} 个</span></div>
      <div className="profile-picker-dialog-body"><div className="profile-channel-options">{matches.map((channel) => {
        const groups = accounts.filter((account) => account.channelId === channel.id);
        const selected = draftIds.includes(channel.id);
        return <button type="button" key={channel.id} className="profile-channel-option" data-selected={selected} aria-label={`选择渠道 ${channel.name}`} aria-pressed={selected} disabled={busy} onClick={() => setDraftIds((previous) => previous.includes(channel.id) ? previous.filter((id) => id !== channel.id) : [...previous, channel.id])}><span className="profile-option-icon">{selected ? <Check size={15} /> : <Plus size={15} />}</span><span className="profile-option-copy"><span>{channel.name}</span><small>{groups.length ? `${groups.length} 个凭证分组` : '尚无凭证分组'}</small></span><span className="profile-option-status">{selected ? '已选' : '选择'}</span></button>;
      })}</div>{!matches.length && <div className="profile-picker-empty"><Search size={24} /><p>{channels.length ? '没有匹配的渠道' : '暂无渠道'}</p><span>{channels.length ? '试试其他名称，或清空搜索。' : '请先在渠道资源中添加渠道。'}</span></div>}</div>
      <footer className="profile-picker-dialog-footer"><span role="status" aria-live="polite">已选 {draftIds.length} 个渠道</span><div className="profile-picker-footer-actions"><Button type="button" size="sm" variant="secondary" icon={X} disabled={busy} onClick={onClose}>取消</Button><Button type="button" size="sm" icon={Check} disabled={busy} onClick={() => onConfirm(draftIds)}>确认选择</Button></div></footer>
    </div>
  </div>);
}
