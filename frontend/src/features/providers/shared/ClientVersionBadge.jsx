import { Tooltip } from '@/shared/ui/index.js';

export default function ClientVersionBadge({ provider, version }) {
  const label = provider === 'claude' ? 'Claude Code' : 'Codex CLI';
  const checking = !version;
  const display = checking ? '检测中…' : /^\d/.test(version) ? `v${version}` : version;
  return <Tooltip content={`${label} 本机版本：${display}`}>
    <span aria-label={`${label} 版本：${display}`} className="shrink-0 font-mono text-xs text-slate-400">
      {display}
    </span>
  </Tooltip>;
}
