import { useEffect, useState } from 'react';
import { Database, FolderOpen, RotateCcw } from 'lucide-react';
import { chooseHistoryArchiveDirectory, getDataStoragePaths } from '@/services/wails/settings.js';
import { isWailsEnvironment } from '@/services/wails/connection.js';

export default function HistoryStorageSettings({ setting, value, onChange, disabled, hasChange, onRevert, refreshKey }) {
  const [paths, setPaths] = useState(null);
  const [error, setError] = useState('');
  const [choosing, setChoosing] = useState(false);
  const desktop = isWailsEnvironment();
  useEffect(() => {
    if (!desktop) return;
    let current = true;
    getDataStoragePaths().then(data => { if (current) { setPaths(data); setError(''); } }).catch(err => { if (current) setError(String(err.message || err)); });
    return () => { current = false; };
  }, [desktop, refreshKey]);
  const choose = async () => {
    setChoosing(true);
    try {
      const path = await chooseHistoryArchiveDirectory();
      if (path) onChange(setting.category, setting.key, path);
      setError('');
    } catch (err) { setError(String(err.message || err)); }
    finally { setChoosing(false); }
  };
  return <div className="min-w-0 space-y-3 border-t border-slate-100 py-3">
    <div className="flex items-center gap-2 text-xs font-medium text-slate-700"><FolderOpen size={14} className="text-slate-400" /><label htmlFor="history-archive-directory">历史归档保存路径</label>{hasChange && <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />}{hasChange && <button type="button" aria-label="撤销归档路径修改" className="ml-auto rounded p-1 text-slate-400 hover:bg-slate-100" onClick={() => onRevert(setting.category, setting.key)} disabled={disabled}><RotateCcw size={13} /></button>}</div>
    <div className="flex min-w-0 gap-2"><input id="history-archive-directory" aria-label="历史归档保存路径" className="h-8 min-w-0 flex-1 rounded-lg border border-slate-200 bg-white px-2.5 font-mono text-xs outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:opacity-50" value={value || ''} placeholder={paths?.defaultArchiveRoot || '留空使用默认目录'} onChange={event => onChange(setting.category, setting.key, event.target.value)} disabled={disabled || choosing} />{desktop && <button type="button" aria-label="选择历史归档目录" title="选择目录" onClick={choose} disabled={disabled || choosing} className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:border-indigo-300 hover:text-indigo-600 disabled:opacity-50"><FolderOpen size={15} /></button>}</div>
    <p className="text-[11px] leading-5 text-slate-500">留空使用默认目录。保存后复制已有归档并切换到新位置，旧目录保留副本；目标的环境子目录需为空。</p>
    {paths?.archiveDirectory && <p className="text-[11px] leading-5 text-slate-400">当前归档目录 <span className="block font-mono [overflow-wrap:anywhere]">{paths.archiveDirectory}</span></p>}
    {paths?.databasePath && <div className="min-w-0 rounded-lg bg-slate-50 p-2.5 text-[11px] leading-5 text-slate-500"><span className="flex items-center gap-1.5 font-medium"><Database size={12} />主数据库位置</span><code className="block [overflow-wrap:anywhere]">{paths.databasePath}</code><span className="text-slate-400">保存渠道、设置和近期请求记录。</span></div>}
    {error && <p role="alert" className="text-[11px] text-rose-600 [overflow-wrap:anywhere]">{error}</p>}
  </div>;
}
