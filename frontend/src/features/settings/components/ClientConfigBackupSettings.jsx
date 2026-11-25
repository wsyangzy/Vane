import { useEffect, useRef, useState } from 'react';
import { Archive, FolderOpen, RotateCcw } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { isWailsEnvironment } from '@/services/wails/connection.js';
import { manageClientConfigBackups } from '@/services/wails/client-config-backups.js';
import BackupRecordsModal from './BackupRecordsModal.jsx';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';

export default function ClientConfigBackupSettings({ setting, value, onChange, disabled, hasChange, onRevert, refreshKey }) {
  const [paths, setPaths] = useState(null), [error, setError] = useState('');
  const [choosing, setChoosing] = useState(false), [open, setOpen] = useState(false);
  const triggerRef = useRef(null);
  const desktop = isWailsEnvironment();
  useEffect(() => {
    if (!desktop) return;
    let current = true;
    manageClientConfigBackups('codex').then(result => { if (current) { setPaths(result); setError(''); } }).catch(err => { if (current) setError(getErrorMessage(err)); });
    return () => { current = false; };
  }, [desktop, refreshKey]);
  const choose = async () => {
    setChoosing(true);
    try {
      const result = await manageClientConfigBackups('', 'choose_directory');
      if (result.directory) onChange(setting.category, setting.key, result.directory);
      setError('');
    } catch (err) { setError(getErrorMessage(err)); }
    finally { setChoosing(false); }
  };
  return <div className="min-w-0 space-y-2 py-2">
    <div className="flex items-center gap-2 text-xs font-medium text-slate-700"><label htmlFor="client-config-backup-directory">客户端配置备份目录</label>{hasChange && <button type="button" aria-label="撤销备份路径修改" disabled={disabled} onClick={() => onRevert(setting.category, setting.key)} className="ml-auto rounded p-1 text-slate-400 hover:bg-slate-100"><RotateCcw size={13} /></button>}</div>
    <div className="flex min-w-0 gap-2"><input id="client-config-backup-directory" value={value} spellCheck={false} placeholder={paths ? paths.defaultDirectory : '%APPDATA%\\Vane\\backup'} disabled={disabled || choosing} onChange={event => onChange(setting.category, setting.key, event.target.value)} className="h-8 min-w-0 flex-1 rounded-lg border border-slate-200 px-2.5 font-mono text-xs outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:opacity-50" />{desktop && <button type="button" aria-label="选择配置备份目录" disabled={disabled || choosing} onClick={choose} className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:border-indigo-300 hover:text-indigo-600 focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:opacity-50"><FolderOpen size={15} /></button>}</div>
    <p className="text-[11px] leading-5 text-slate-500">留空使用默认目录。保存后用于后续备份，原目录的文件保留；查看原记录时可切回原目录。</p>
    {paths && <p className="text-[11px] leading-5 text-slate-400 [overflow-wrap:anywhere]">当前目录：<span className="font-mono">{paths.directory}</span></p>}
    {desktop && <Button ref={triggerRef} size="sm" variant="secondary" icon={Archive} disabled={disabled} onClick={() => setOpen(true)}>查看备份记录</Button>}
    {error && <p role="alert" className="text-xs text-rose-600 [overflow-wrap:anywhere]">{error}</p>}
    {open && <BackupRecordsModal triggerRef={triggerRef} onClose={() => setOpen(false)} />}
  </div>;
}
