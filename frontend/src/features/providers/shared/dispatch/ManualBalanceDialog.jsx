import { useEffect, useId, useRef, useState } from 'react';
import { Check, Clock3, Wallet, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { listManualBalanceEntries } from '@/services/wails/dispatch.js';
import { balanceAmountError, balanceErrorMessage, formatUsd } from './manualBalanceState.js';

const titles = { calibrate: '设置当前余额', credit: '记录充值', settings: '余额调度设置', disable: '关闭余额跟踪', history: '余额流水' };
const entryNames = { calibrate: '余额校准', credit: '充值', debit: '请求扣减', disable: '关闭跟踪', settings: '设置调整' };
const date = (value) => value ? new Date(value).toLocaleString() : '—';

export default function ManualBalanceDialog({ provider, channel, group, action, onSave, onClose }) {
  const balance = group.balance;
  const [amount, setAmount] = useState('');
  const [stop, setStop] = useState(balance?.stopWhenExhausted ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [entries, setEntries] = useState(null);
  const [operationId] = useState(() => crypto.randomUUID());
  const dialog = useRef(null);
  const titleId = useId();
  const descriptionId = useId();
  useEffect(() => {
    const previousFocus = document.activeElement;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    dialog.current?.querySelector('input, button')?.focus();
    return () => { document.body.style.overflow = overflow; previousFocus?.isConnected && previousFocus.focus(); };
  }, []);
  useEffect(() => {
    if (action !== 'history') return;
    let active = true;
    listManualBalanceEntries(provider, channel.id, group.id).then((value) => { if (active) setEntries(value); }, (err) => { if (active) setError(balanceErrorMessage(err)); });
    return () => { active = false; };
  }, [action, provider, channel.id, group.id]);
  const submit = async (event) => {
    event.preventDefault();
    if (busy) return;
    const validation = ['calibrate', 'credit'].includes(action) ? balanceAmountError(amount, action) : '';
    if (validation) { setError(validation); return; }
    setBusy(true); setError('');
    try {
      await onSave({ provider, channelId: channel.id, groupId: group.id, action, amountUsd: amount, stopWhenExhausted: stop, revision: balance?.revision || 0, operationId });
      onClose();
    } catch (err) { setError(balanceErrorMessage(err)); }
    finally { setBusy(false); }
  };
  const keyDown = (event) => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (!busy) onClose(); }
    if (event.key !== 'Tab') return;
    const elements = [...dialog.current.querySelectorAll('button:not(:disabled), input:not(:disabled), [tabindex="0"]')].filter((node) => node.getClientRects().length);
    const first = elements[0], last = elements.at(-1);
    if (event.shiftKey && document.activeElement === first || !event.shiftKey && document.activeElement === last) { event.preventDefault(); (event.shiftKey ? last : first)?.focus(); }
  };
  return renderModalPortal(<div className="qh-balance-overlay" onClick={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
    <form ref={dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={descriptionId} className="qh-balance-dialog" onSubmit={submit} onKeyDown={keyDown}>
      <header><span className="qh-balance-dialog-icon"><Wallet size={19} /></span><div><h2 id={titleId}>{titles[action]}</h2><p id={descriptionId}>{channel.name} / {group.name} · USD</p></div><button type="button" className="qh-icon-button" aria-label="关闭余额弹窗" disabled={busy} onClick={onClose}><X size={18} /></button></header>
      <div className="qh-balance-dialog-body">
        {balance?.enabled && <div className="qh-balance-current"><span>当前估算余额</span><strong>{formatUsd(balance.balanceUsd)}</strong><small>校准于 {date(balance.calibratedAt)}</small></div>}
        {action === 'history' ? <><p className="qh-section-note">本分组最近 100 条流水，包含历史校准。金额来自本地记账，和平台余额可能有差异。</p>{entries === null && !error ? <p className="qh-section-note">读取流水…</p> : <div className="qh-balance-ledger" tabIndex={0}><table><thead><tr><th>操作 / 时间</th><th>变动 USD</th><th>记账后余额</th></tr></thead><tbody>{entries?.map((entry) => <tr key={entry.id}><td>{entryNames[entry.kind] || entry.kind}<small>{date(entry.createdAt)}</small>{entry.estimated && <small className="qh-balance-warning">用量或价格不完整</small>}</td><td>{formatUsd(entry.amountUsd)}</td><td>{formatUsd(entry.balanceUsd)}</td></tr>)}</tbody></table>{entries?.length === 0 && <p className="qh-section-note">暂无流水。</p>}</div>}</> : <>
          {['calibrate', 'credit'].includes(action) && <label className="qh-balance-field">{action === 'credit' ? '充值金额（USD）' : '平台当前余额（USD）'}<input aria-label={action === 'credit' ? '充值金额' : '平台当前余额'} inputMode="decimal" autoComplete="off" placeholder="例如 20.00" value={amount} disabled={busy} onChange={(event) => setAmount(event.target.value)} /></label>}
          {action === 'calibrate' && <p className="qh-section-note">以平台当前余额作为新的起点，仅扣减校准后开始的请求。历史消费不追扣，校准时已在执行的请求也不计入本次起点之后。</p>}
          {action === 'credit' && <p className="qh-section-note">仅记录此次增加的余额，保留已有消费与校准起点。</p>}
          {action !== 'disable' && <label className="qh-balance-setting"><input type="checkbox" checked={stop} disabled={busy} onChange={(event) => setStop(event.target.checked)} /><span>余额耗尽后跳过此分组<small>关闭后仍记录成本；已开始的并发请求可能使余额低于 0。</small></span></label>}
          {action === 'disable' && <p className="qh-balance-warning">确认后停止后续扣减和余额准入检查，保留历史流水。再次开启时需要重新设置当前余额。</p>}
          <p className="qh-section-note">本地估算只覆盖经过 Vane 且能够计费的请求。其他客户端消费、退款和平台赠送需要手动校准。</p>
        </>}
        {error && <p role="alert" className="qh-balance-warning">{error}</p>}
      </div>
      <footer><span><Clock3 size={12} />本地估算 · USD</span><Button type="button" size="sm" variant="secondary" icon={X} disabled={busy} onClick={onClose}>{action === 'history' ? '关闭' : '取消'}</Button>{action !== 'history' && <Button type="submit" size="sm" icon={Check} disabled={busy}>{busy ? '保存中…' : '确认'}</Button>}</footer>
    </form>
  </div>);
}
