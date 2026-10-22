import { LoaderCircle, Terminal } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';

export default function ClientVersionBadge({ provider, version }) {
  const label = provider === 'claude' ? 'Claude Code' : 'Codex CLI';
  const checking = !version;
  const display = checking ? '检测中…' : /^\d/.test(version) ? `v${version}` : version;
  const Icon = checking ? LoaderCircle : Terminal;
  return <Tooltip content={`${label} 本机版本：${display}`}>
    <span aria-label={`${label} 版本：${display}`} className="inline-flex shrink-0 items-baseline gap-1.5 rounded-lg border border-slate-200 bg-white px-2 py-1 text-xs font-medium text-slate-500">
      <Icon size={13} aria-hidden="true" className={checking ? 'animate-spin' : ''} />
      <span>{label}</span><span className="font-mono text-slate-700">{display}</span>
    </span>
  </Tooltip>;
}
