import { Globe, KeyRound, Layers, Pencil, Server } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { DetailMetricCard, DetailPill, ResourceDetailShell } from '../endpoints/detailParts.jsx';

export default function ChannelDetailModal({ provider, name, website, graph, busy, onClose, onEdit }) {
  const groups = graph.nodes.filter((node) => node.kind === 'group');
  const endpointCount = graph.nodes.filter((node) => node.kind === 'endpoint').length;
  return (
    <ResourceDetailShell
      title={name}
      eyebrow={`${provider === 'codex' ? 'Codex' : 'Claude'} · 渠道详情`}
      icon={Layers}
      onClose={onClose}
      actions={onEdit && <Button variant="ghost" size="sm" icon={Pencil} disabled={busy} onClick={onEdit}>编辑</Button>}
      summary={<>
        <DetailPill icon={KeyRound}>{groups.length} 个分组</DetailPill>
        <DetailPill icon={Server}>{endpointCount} 个端点</DetailPill>
      </>}
    >
      <div className="space-y-3 pb-4">
        <DetailMetricCard label="渠道网站" icon={Globe} density="compact">
          <span className={website ? 'text-xs text-slate-700 [overflow-wrap:anywhere]' : 'rd-empty'}>{website || '未设置'}</span>
        </DetailMetricCard>
        <DetailMetricCard label="凭证分组与端点" icon={KeyRound} density="compact">
          {groups.length ? <div className="divide-y divide-slate-100">
            {groups.map((node) => {
              const endpoints = graph.nodes.filter((child) => child.kind === 'endpoint' && child.group === node.group);
              return <div key={node.key} className="min-w-0 py-2 first:pt-0 last:pb-0">
                <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                  <span className="min-w-0 text-xs font-medium text-slate-700 [overflow-wrap:anywhere]">{node.label}</span>
                  <span className="text-[11px] text-slate-400">{node.group.authMode === 'client_passthrough' ? '订阅账号' : 'API 凭证'} · {endpoints.length} 个端点</span>
                </div>
                {endpoints.length ? <ul className="mt-2 space-y-1.5">
                  {endpoints.map((child) => <li key={child.key} className="flex min-w-0 items-start gap-2 text-[11px] text-slate-500">
                    <Server size={12} className="mt-0.5 shrink-0 text-slate-400" />
                    <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">{child.label}<span className="ml-2 text-slate-400">{child.endpoint.url}</span></span>
                    <span className="shrink-0 tabular-nums text-slate-400">P{child.endpoint.priority}</span>
                  </li>)}
                </ul> : <p className="rd-empty mt-1">尚未添加端点</p>}
              </div>;
            })}
          </div> : <span className="rd-empty">尚未添加凭证分组</span>}
        </DetailMetricCard>
      </div>
    </ResourceDetailShell>
  );
}
