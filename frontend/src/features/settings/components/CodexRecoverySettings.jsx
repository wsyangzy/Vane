import { useId } from 'react';
import { FolderOpen, RotateCcw } from 'lucide-react';
import SettingItem from './SettingItem.jsx';

const summaries = {
  codex_history_persistence: { label: '保存完整恢复状态', description: '加密保存已完成的推理状态，供重启后继续恢复。' },
  codex_encrypted_history_recovery: { label: '允许降级兜底', description: '完整恢复失败后移除加密推理项重试，会丢失隐藏推理。' },
  codex_history_retention_days: { description: '1–90 天，启用或读写时清理到期记录。' },
  codex_history_max_mib: { description: '8–1024 MiB，超出后优先清理最早记录。' },
  codex_history_client_lookup: { description: '启用持久化后，只读补查当前 Codex 会话，不修改原文件。' },
};

export default function CodexRecoverySettings({ section, settings, changes, getValue, onChange, onRevert, disabled }) {
  const pathId = useId();
  const directory = settings.find(setting => setting.key === 'codex_history_directory');
  const hasPathChange = directory && `global.${directory.category}.${directory.key}` in changes;
  const savedPath = directory?.description?.split('当前保存文件：')[1]?.trim();
  const renderItem = key => {
    const setting = settings.find(item => item.key === key);
    return setting && <SettingItem key={key} setting={{ ...setting, ...summaries[key] }} value={getValue(setting)} onChange={onChange} disabled={disabled} hasChange={`global.${setting.category}.${key}` in changes} onRevert={onRevert} />;
  };

  if (section === 'strategy') return <>
    {['codex_history_persistence', 'codex_encrypted_history_recovery'].map(renderItem)}
    <p className="my-2 rounded-lg bg-slate-50 px-2.5 py-2 text-[11px] leading-5 text-slate-500">仅用于 Codex。先原样重试，再尝试完整恢复，仍失败才按开关降级。消息与工具记录保留；含压缩历史或服务端会话引用时不降级，所有尝试仍受调度预算限制。</p>
  </>;

  return <>
    {directory && <div className="min-w-0 space-y-2 border-b border-slate-100 py-2">
      <div className="flex items-center gap-2 text-xs font-medium text-slate-700">
        <FolderOpen size={14} className="shrink-0 text-slate-400" /><label htmlFor={pathId}>恢复状态保存目录</label>
        {hasPathChange && <><span className="h-1.5 w-1.5 rounded-full bg-amber-400" /><button type="button" disabled={disabled} onClick={() => onRevert(directory.category, directory.key)} aria-label="撤销恢复目录修改" className="ml-auto rounded p-1 text-slate-400 hover:bg-slate-100 focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:opacity-50"><RotateCcw size={13} /></button></>}
      </div>
      <input id={pathId} value={getValue(directory) || ''} disabled={disabled} onChange={event => onChange(directory.category, directory.key, event.target.value)} spellCheck={false} autoComplete="off" placeholder="留空使用 Vane 默认数据目录" className="h-8 w-full min-w-0 rounded-lg border border-slate-200 bg-slate-50 px-2.5 font-mono text-xs text-slate-700 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:opacity-50" />
      <p className="text-[11px] leading-5 text-slate-500">填写绝对路径。更换目录后使用新位置，旧文件保留，不自动迁移或合并。</p>
      {savedPath && <p className="text-[11px] leading-5 text-slate-400">{hasPathChange ? '当前已保存的文件位置' : '当前保存文件'}<span className="block font-mono [overflow-wrap:anywhere]">{savedPath}</span></p>}
    </div>}
    {['codex_history_retention_days', 'codex_history_max_mib', 'codex_history_client_lookup'].map(renderItem)}
    <details className="my-2 rounded-lg bg-slate-50 px-2.5 py-2 text-[11px] leading-5 text-slate-500">
      <summary className="cursor-pointer font-medium text-slate-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300">保存与补查说明</summary>
      <div className="mt-2 space-y-2 [overflow-wrap:anywhere]">
        <p>状态由当前 Windows 用户加密保存，开发与正式环境分别存储。关闭持久化后停止磁盘读写，已有文件保留。</p>
        <p>最多保留 4096 项；单项超过 256 KiB 时保存校验信息，完整内容通过客户端会话补查。应用关闭或持久化停用期间不清理。</p>
        <p>补查遵循 CODEX_HOME，未设置时使用用户的 .codex 目录。只有会话、来源与密文哈希匹配才恢复，不重新加入已压缩或撤销的历史。</p>
      </div>
    </details>
  </>;
}
