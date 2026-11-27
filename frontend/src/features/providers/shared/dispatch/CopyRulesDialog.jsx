import { useEffect, useId, useRef, useState } from 'react';
import { Check, Copy, X } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { ruleKindLabel, ruleTargetLabels } from './dispatchState.js';
import { ruleCopyIssue } from './profileEditingState.js';
import './profileMembers.css';

export default function CopyRulesDialog({ profile, profiles, channels, accounts, busy, onConfirm, onClose, returnFocusRef }) {
  const sources = profiles.filter(item => item.provider === profile.provider && item.id !== profile.id && item.rules.length);
  const resources = { profiles, channels, accounts };
  const [sourceId, setSourceId] = useState(sources[0]?.id);
  const source = sources.find(item => item.id === sourceId);
  const available = (source?.rules || []).filter(rule => !ruleCopyIssue(profile, source, rule, resources));
  const [selected, setSelected] = useState(() => available.map(rule => rule.id));
  const [error, setError] = useState('');
  const dialog = useRef(null);
  const titleId = useId();
  const descriptionId = useId();
  useEffect(() => {
    const previousFocus = returnFocusRef.current;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    dialog.current?.querySelector('button:not(:disabled)')?.focus();
    return () => { document.body.style.overflow = previousOverflow; previousFocus?.focus(); };
  }, [returnFocusRef]);
  const chooseSource = id => {
    const next = sources.find(item => item.id === id);
    setSourceId(id);
    setSelected(next.rules.filter(rule => !ruleCopyIssue(profile, next, rule, resources)).map(rule => rule.id));
    setError('');
  };
  const handleKeys = event => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (!busy) onClose(); }
    if (event.key !== 'Tab') return;
    const controls = [...dialog.current.querySelectorAll('button:not(:disabled), input:not(:disabled)')].filter(element => element.getClientRects().length);
    const first = controls[0], last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first || !event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      (event.shiftKey ? last : first)?.focus();
    }
  };
  return renderModalPortal(<div className="profile-picker-overlay" onClick={event => { if (event.target === event.currentTarget && !busy) onClose(); }}>
    <div ref={dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descriptionId} onKeyDown={handleKeys} className="profile-picker-dialog">
      <header className="profile-picker-dialog-header"><span className="profile-picker-dialog-icon"><Copy size={19} /></span><div><h2 id={titleId}>从其他方案复制规则</h2><p id={descriptionId}>勾选后追加到当前草稿，保存方案后生效。</p></div><button type="button" className="profile-picker-close" aria-label="关闭规则复制" disabled={busy} onClick={onClose}><X size={18} /></button></header>
      <div className="profile-picker-toolbar"><div className="min-w-0 flex-1"><CustomSelect ariaLabel="来源方案" fullWidth wrapOptions disabled={busy} value={sourceId} options={sources.map(item => ({ value: item.id, label: `${item.name} · ${item.rules.length} 条规则${item.enabled ? '' : '（已停用）'}` }))} onChange={chooseSource} /></div><Button type="button" size="sm" variant="ghost" icon={Check} disabled={busy || !available.length} onClick={() => setSelected(selected.length === available.length ? [] : available.map(rule => rule.id))}>{selected.length === available.length ? '取消全选' : '全选可复制规则'}</Button></div>
      <div className="profile-picker-dialog-body space-y-2">
        {(source?.rules || []).map((rule, index) => {
          const issue = ruleCopyIssue(profile, source, rule, resources);
          const condition = [rule.matchModel, rule.kind === 'non_stream' ? '仅非流式请求' : rule.requireStream == null ? '' : rule.requireStream ? '仅流式请求' : '仅非流式请求'].filter(Boolean).join(' · ');
          return <button type="button" key={rule.id} className="profile-channel-option w-full disabled:!opacity-100" data-selected={selected.includes(rule.id)} aria-pressed={selected.includes(rule.id)} aria-label={`复制规则 ${index + 1}：${condition || ruleKindLabel(rule.kind)}`} disabled={busy || Boolean(issue)} onClick={() => setSelected(previous => previous.includes(rule.id) ? previous.filter(id => id !== rule.id) : [...previous, rule.id])}>
            <span className="profile-option-icon">{selected.includes(rule.id) ? <Check size={15} /> : <Copy size={14} />}</span><span className="profile-option-copy"><span>{ruleKindLabel(rule.kind)} · {condition}</span><small className="wrap-anywhere">{ruleTargetLabels(rule.targets, resources).join('；')}</small>{issue && <small className="!text-amber-700">{issue}</small>}</span><span className="profile-option-status">{!rule.enabled ? '已停用' : `优先级 ${rule.priority}`}</span>
          </button>;
        })}
        {!source && <p className="profile-picker-empty">暂无其他包含规则的方案</p>}
        {error && <p role="alert" className="wrap-anywhere text-xs text-rose-600">{error}</p>}
      </div>
      <footer className="profile-picker-dialog-footer"><span role="status" aria-live="polite">已选 {selected.length} / {source?.rules.length || 0} 条规则</span><div className="profile-picker-footer-actions"><Button type="button" size="sm" variant="secondary" icon={X} disabled={busy} onClick={onClose}>取消</Button><Button type="button" size="sm" icon={Copy} disabled={busy || !selected.length} onClick={() => { try { onConfirm(source, selected); } catch (error) { setError(error.message); } }}>追加所选规则</Button></div></footer>
    </div>
  </div>);
}
