import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { Archive, Check, FolderOpen, Pencil, RefreshCw, RotateCcw, Save, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { manageClientConfigBackups } from '@/services/wails/client-config-backups.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';

const operationLabels = { connection_apply: '应用接入配置前', model_settings: '保存模型设置前', before_restore: '手动恢复前' };
const formatTime = value => new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false });

export default function BackupRecordsModal({ provider: initialProvider = 'codex', onClose, onRestored, triggerRef }) {
  const [provider, setProvider] = useState(initialProvider);
  const [data, setData] = useState(null);
  const [busy, setBusy] = useState('loading');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [selected, setSelected] = useState(null);
  const [editing, setEditing] = useState(null);
  const [name, setName] = useState('');
  const generation = useRef(0);
  const dialogRef = useRef(null), closeRef = useRef(null);
  const titleId = useId();
  const load = useCallback(async () => {
    const version = ++generation.current;
    setBusy('loading'); setError(''); setSelected(null); setEditing(null);
    try {
      const result = await manageClientConfigBackups(provider);
      if (version === generation.current) setData(result);
    } catch (err) { if (version === generation.current) { setData(null); setError(getErrorMessage(err)); } }
    finally { if (version === generation.current) setBusy(''); }
  }, [provider]);
  useEffect(() => { load(); return () => { generation.current += 1; }; }, [load]);
  useLayoutEffect(() => {
    const previousFocus = triggerRef?.current || document.activeElement;
    const parent = previousFocus?.closest('[role="dialog"]');
    const inert = parent?.inert, hidden = parent?.getAttribute('aria-hidden');
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    if (parent) { parent.inert = true; parent.setAttribute('aria-hidden', 'true'); }
    closeRef.current?.focus();
    return () => {
      document.body.style.overflow = overflow;
      if (parent) {
        parent.inert = inert;
        if (hidden == null) parent.removeAttribute('aria-hidden'); else parent.setAttribute('aria-hidden', hidden);
      }
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [triggerRef]);
  useLayoutEffect(() => {
    if (!busy && !dialogRef.current?.contains(document.activeElement)) closeRef.current?.focus();
  }, [busy, selected, editing]);
  const restore = async () => {
    setBusy('restore'); setError(''); setNotice('');
    try {
      const result = await manageClientConfigBackups(provider, 'restore', { id: selected.id, revision: data.revision });
      setData(result); setSelected(null);
      setNotice('已恢复所选版本，恢复前的配置也已备份。请重新启动客户端使配置生效。');
      onRestored?.();
    } catch (err) { setError(getErrorMessage(err)); setSelected(null); await load(); setError(getErrorMessage(err)); }
    finally { setBusy(''); }
  };
  const rename = async () => {
    setBusy('rename'); setError(''); setNotice('');
    try {
      const result = await manageClientConfigBackups(provider, 'rename', { id: editing, name: name.trim() });
      setData(result); setEditing(null); setNotice('备份名称已保存。');
    } catch (err) { setError(getErrorMessage(err)); }
    finally { setBusy(''); }
  };
  const openDirectory = async (id = '') => {
    setBusy('open_directory'); setError('');
    try { await manageClientConfigBackups(provider, 'open_directory', { id }); }
    catch (err) { setError(getErrorMessage(err)); }
    finally { setBusy(''); }
  };
  const keyDown = event => {
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopPropagation();
      if (!busy) { if (editing) setEditing(null); else if (selected) setSelected(null); else onClose(); }
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), input:not(:disabled), [tabindex="0"]')].filter(el => el.getClientRects().length);
    const first = controls[0], last = controls.at(-1);
    if ((event.shiftKey && document.activeElement === first) || (!event.shiftKey && document.activeElement === last)) {
      event.preventDefault(); (event.shiftKey ? last : first)?.focus();
    }
  };
  return renderModalPortal(<div className="fixed inset-0 z-[11000] flex items-center justify-center bg-slate-900/40 p-3 backdrop-blur-sm sm:p-4" onClick={event => { if (event.target === event.currentTarget && busy !== 'restore' && busy !== 'rename') onClose(); }}>
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={keyDown} className="flex max-h-[calc(100dvh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl">
      <div className="flex shrink-0 items-start justify-between gap-3 border-b border-slate-100 px-4 py-3">
        <div className="flex min-w-0 gap-2.5"><Archive size={18} className="mt-0.5 shrink-0 text-indigo-600" /><div className="min-w-0"><h2 id={titleId} className="text-sm font-semibold text-slate-900">客户端配置备份记录</h2><p className="mt-1 text-xs leading-5 text-slate-500">选择写入前的版本恢复；此前自行创建的备份保留在原处。</p></div></div>
        <button ref={closeRef} type="button" aria-label="关闭备份记录" onClick={onClose} disabled={busy === 'restore' || busy === 'rename'} className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:opacity-40"><X size={18} /></button>
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 px-4 py-2">
        <div className="flex gap-1 rounded-lg bg-slate-100 p-1">{['codex', 'claude'].map(value => <button key={value} type="button" aria-pressed={provider === value} disabled={Boolean(busy)} onClick={() => { setProvider(value); setNotice(''); setData(null); }} className={`rounded-md px-3 py-1.5 text-xs font-medium focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:opacity-50 ${provider === value ? 'bg-white text-indigo-700 shadow-sm' : 'text-slate-500 hover:text-slate-800'}`}>{value === 'codex' ? 'Codex' : 'Claude Code'}</button>)}</div>
        <div className="flex flex-wrap justify-end gap-2"><Button size="sm" variant="secondary" icon={FolderOpen} className="whitespace-nowrap" disabled={Boolean(busy)} onClick={() => openDirectory()}>打开目录</Button><Button size="sm" variant="secondary" icon={RefreshCw} disabled={Boolean(busy)} onClick={load}>刷新</Button></div>
      </div>
      {data && <dl className="shrink-0 space-y-1.5 border-y border-slate-100 bg-slate-50 px-4 py-2.5 text-[11px] leading-5"><div className="flex min-w-0 gap-3"><dt className="shrink-0 text-slate-400">备份目录</dt><dd className="min-w-0 font-mono text-slate-600 [overflow-wrap:anywhere]">{data.directory}</dd></div><div className="flex min-w-0 gap-3"><dt className="shrink-0 text-slate-400">恢复目标</dt><dd className="min-w-0 font-mono text-slate-600 [overflow-wrap:anywhere]">{data.configPath}</dd></div></dl>}
      <div className="scrollbar-visible min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-2">
        {busy === 'loading' ? <p role="status" className="py-8 text-center text-xs text-slate-500">正在读取备份记录…</p> : data?.records.length === 0 ? <div className="py-8 text-center text-xs leading-6 text-slate-500">暂无 Vane 管理的备份记录。<br />首次应用配置或保存模型设置时，会先备份当前文件。</div> : data?.records.map(record => <div key={record.id} className="flex min-w-0 items-start justify-between gap-3 border-b border-slate-100 py-3 last:border-0">
          <div className="min-w-0 flex-1">
            {editing === record.id ? <form className="flex min-w-0 flex-wrap items-center gap-1.5" onSubmit={event => { event.preventDefault(); rename(); }}><input autoFocus aria-label="备份名称" value={name} maxLength={80} disabled={Boolean(busy)} onChange={event => setName(event.target.value)} placeholder="例如：首次接入、调整压缩方式前" className="h-8 min-w-0 flex-1 basis-40 rounded-lg border border-slate-200 px-2.5 text-xs outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100" /><button type="submit" aria-label="保存备份名称" disabled={Boolean(busy)} className="rounded-lg p-1.5 text-indigo-600 hover:bg-indigo-50 disabled:opacity-50"><Save size={15} /></button><button type="button" aria-label="取消名称编辑" disabled={Boolean(busy)} onClick={() => setEditing(null)} className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100"><X size={15} /></button></form> : <div className="flex min-w-0 items-start gap-1.5"><p className="min-w-0 text-xs font-medium text-slate-800 [overflow-wrap:anywhere]">{record.name || formatTime(record.createdAt)}</p><button type="button" aria-label={`编辑备份名称：${record.name || formatTime(record.createdAt)}`} disabled={Boolean(busy)} onClick={() => { setSelected(null); setEditing(record.id); setName(record.name || ''); setError(''); }} className="shrink-0 rounded p-1 text-slate-400 hover:bg-slate-100 hover:text-indigo-600 focus-visible:ring-2 focus-visible:ring-indigo-300"><Pencil size={13} /></button></div>}
            <p className="mt-1 text-[11px] text-slate-500">{formatTime(record.createdAt)} · {operationLabels[record.operation] || record.operation} · {record.exists ? `${(record.size / 1024).toFixed(1)} KB` : '当时未创建配置文件'}</p><p className="mt-1 font-mono text-[11px] text-slate-400 [overflow-wrap:anywhere]">{record.configPath}</p>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1.5 sm:flex-row"><button type="button" aria-label={`打开记录目录：${record.name || formatTime(record.createdAt)}`} disabled={Boolean(busy) || Boolean(editing)} onClick={() => openDirectory(record.id)} className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 hover:text-indigo-600 focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:opacity-50"><FolderOpen size={15} /></button><Button size="sm" variant="secondary" icon={RotateCcw} className="shrink-0 whitespace-nowrap" disabled={Boolean(busy) || Boolean(editing)} onClick={() => { setSelected(record); setNotice(''); }}>恢复</Button></div>
        </div>)}
      </div>
      <div className="shrink-0 space-y-2 border-t border-slate-100 px-4 py-3">
        {selected && <div className="space-y-2 rounded-lg border border-amber-200 bg-amber-50 p-3"><p className="text-xs leading-5 text-amber-800 [overflow-wrap:anywhere]">恢复「{selected.name || formatTime(selected.createdAt)}」的版本？{selected.exists ? '将替换当前客户端配置。' : '该版本没有配置文件，恢复会移除当前文件。'}恢复前会先备份当前文件。</p><div className="flex justify-end gap-2"><Button size="sm" variant="secondary" disabled={Boolean(busy)} onClick={() => setSelected(null)}>取消</Button><Button size="sm" icon={RotateCcw} disabled={Boolean(busy)} onClick={restore}>{busy === 'restore' ? '正在恢复…' : '备份当前配置并恢复'}</Button></div></div>}
        {error && <p role="alert" className="text-xs text-rose-600 [overflow-wrap:anywhere]">{error}</p>}
        {notice && <p role="status" className="flex items-start gap-1.5 text-xs leading-5 text-emerald-700"><Check size={14} className="mt-0.5 shrink-0" />{notice}</p>}
        <p className="text-[11px] leading-5 text-slate-400">配置内容加密保存，仅由创建备份的 Windows 用户恢复。</p>
      </div>
    </div>
  </div>);
}
