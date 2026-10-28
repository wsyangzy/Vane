import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useCallback, useState } from 'react';
import { ExternalLink, KeyRound, Pencil, Server, Trash2 } from 'lucide-react';
import { openExternalURL } from '@/services/wails/index.js';
import ChannelStructureDialog from './ChannelStructureDialog.jsx';
import './channelWorkspace.css';

const iconButton = 'inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 disabled:opacity-40';

function websiteLabel(website) {
  const address = website?.trim();
  if (!address || /\s/.test(address)) return '';
  try {
    const url = new URL(address.includes('://') ? address : `https://${address}`);
    return /^https?:$/.test(url.protocol) ? url.hostname.replace(/^www\./, '') : '';
  } catch {
    return '';
  }
}

// The provider adapters own credentials, forms and endpoint normalization.
export default function ChannelResourceRow({ provider, name, website, groups, endpoints, busy, onEdit, onDelete, onAddGroup, ...resourceActions }) {
  const [structureOpen, setStructureOpen] = useState(false);
  const closeStructure = useCallback(() => setStructureOpen(false), []);
  const subscriptions = groups.filter((group) => group.authMode === 'client_passthrough').length;
  const tone = !groups.length ? 'empty' : subscriptions === groups.length ? 'subscription' : subscriptions ? 'mixed' : 'api';
  const caption = { empty: '待配置', subscription: '订阅账号', mixed: '混合凭证', api: 'API 凭证' }[tone];
  const domain = websiteLabel(website);

  return (
    <section aria-label={`渠道 ${name}`} className="channel-resource-card min-w-0" data-provider={provider} data-channel-tone={tone} onDoubleClick={(event) => {
      if (!event.target.closest('button, a')) setStructureOpen(true);
    }}>
      <div className="channel-card-caption">{caption}</div>
      <div className="channel-card-paper min-w-0">
        <Tooltip asChild content="双击打开渠道画布，也可按 Enter 或空格键"><div role="button" tabIndex={0} aria-label={`打开渠道画布 ${name}`} aria-description="双击打开画布，也可按 Enter 或空格键" aria-haspopup="dialog" className="channel-card-surface min-w-0 outline-none" onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            setStructureOpen(true);
          }
        }}>
          <span className="channel-card-heading">
            <span className="channel-card-name">{name}</span>
            {domain && <Tooltip asChild content={website}><span className="channel-card-domain">{domain}</span></Tooltip>}
          </span>
          <span className="channel-card-groups" aria-label="凭证分组预览">
            {groups.slice(0, 2).map((group) => <span key={group.id} className="channel-card-group"><KeyRound size={10} /><span>{group.name}</span></span>)}
            {groups.length > 2 && <Tooltip asChild content={groups.slice(2).map(group => group.name).join('、')}><span className="channel-card-group-overflow">+{groups.length - 2}</span></Tooltip>}
            {!groups.length && <span className="channel-card-empty">尚未添加凭证分组</span>}
          </span>
        </div></Tooltip>
        <div className="channel-card-footer flex min-w-0 items-center">
          <div className="flex min-w-0 items-center gap-2 text-[10px] text-slate-500">
            <Tooltip asChild content={`${groups.length} 个凭证分组`}><span className="inline-flex items-center gap-1 whitespace-nowrap tabular-nums" aria-label={`${groups.length} 个凭证分组`}><KeyRound size={11} />{groups.length}</span></Tooltip>
            <Tooltip asChild content={`${endpoints.length} 个端点`}><span className="inline-flex items-center gap-1 whitespace-nowrap tabular-nums" aria-label={`${endpoints.length} 个端点`}><Server size={11} />{endpoints.length}</span></Tooltip>
          </div>
          <div className="flex shrink-0 items-center gap-0.5">
            {website && <Tooltip asChild content="打开渠道网站"><button type="button" className={iconButton} aria-label="打开渠道网站" onClick={() => openExternalURL(website)}><ExternalLink size={12} /></button></Tooltip>}
            <Tooltip asChild content="编辑渠道"><button type="button" className={iconButton} aria-label={`编辑渠道 ${name}`} disabled={busy} onClick={onEdit}><Pencil size={12} /></button></Tooltip>
            <Tooltip asChild content="删除渠道"><button type="button" className={`${iconButton} hover:!bg-rose-50 hover:!text-rose-500`} aria-label={`删除渠道 ${name}`} disabled={busy} onClick={onDelete}><Trash2 size={12} /></button></Tooltip>
          </div>
        </div>
      </div>
      {structureOpen && <ChannelStructureDialog name={name} groups={groups} endpoints={endpoints} busy={busy} onClose={closeStructure} onEdit={onEdit} onDelete={onDelete} onAddGroup={onAddGroup} {...resourceActions} />}
    </section>
  );
}
