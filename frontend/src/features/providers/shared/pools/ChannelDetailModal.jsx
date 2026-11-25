import { ExternalLink, Globe, Layers, Pencil } from 'lucide-react';
import { Button, Tooltip } from '@/shared/ui/index.js';
import { openExternalURL } from '@/services/wails/index.js';
import { DetailMetricCard, ResourceDetailShell } from '../endpoints/detailParts.jsx';

export default function ChannelDetailModal({ provider, name, website, details, busy, onClose, onEdit }) {
  const websiteText = website?.trim() || '';
  let websiteURL = '';
  try {
    const url = new URL(websiteText.includes('://') ? websiteText : `https://${websiteText}`);
    if (/^https?:$/.test(url.protocol) && !/\s/.test(websiteText)) websiteURL = url.href;
  } catch { /* Keep an invalid saved address readable without making it a link. */ }
  return (
    <ResourceDetailShell
      title={name}
      eyebrow={`${provider === 'codex' ? 'Codex' : 'Claude'} · 渠道详情`}
      icon={Layers}
      maxWidth={680}
      onClose={onClose}
      actions={onEdit && <Button variant="ghost" size="sm" icon={Pencil} disabled={busy} onClick={onEdit}>编辑</Button>}
    >
      <div className="space-y-2.5 pb-3">
        <DetailMetricCard label="官网地址" icon={Globe} density="compact">
          <div className="flex min-w-0 items-start gap-2">
            <span className={`min-w-0 flex-1 ${websiteText ? 'text-xs leading-5 text-slate-700 [overflow-wrap:anywhere]' : 'rd-empty'}`}>{websiteText || '未设置'}</span>
            {websiteURL && <Tooltip asChild content="使用默认浏览器打开">
              <button type="button" aria-label="打开官网" onClick={() => openExternalURL(websiteURL)} className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-500 transition-colors hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-600 focus-visible:outline-2 focus-visible:outline-indigo-500 focus-visible:outline-offset-2">
                <ExternalLink size={13} />
              </button>
            </Tooltip>}
          </div>
        </DetailMetricCard>
        {details}
      </div>
    </ResourceDetailShell>
  );
}
