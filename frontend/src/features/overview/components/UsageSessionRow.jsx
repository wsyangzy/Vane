import { ChevronRight, MessageSquare } from 'lucide-react';
import { CodexLogo } from '@/features/providers/codex/components/CodexLogo.jsx';
import { ClaudeIcon } from '@/features/requests/components/ModelTag.jsx';
import { Tooltip } from '@/shared/ui/index.js';
import { count, duration, money, projectName, providerLabel, sessionTitle, usageDate } from '../usageDashboard.js';

export default function UsageSessionRow({ row, onOpen, disabled, selected, inDialog = false }) {
  const Icon = { codex: CodexLogo, claude: ClaudeIcon }[row.provider] || MessageSquare;
  const title = sessionTitle(row);
  const detail = `输入 ${count(row.input)} · 输出 ${count(row.output)} · 缓存 ${count(row.cacheRead)}`;
  const usageDetail = <div className="space-y-1"><div>输入 {row.input.toLocaleString()} · 输出 {row.output.toLocaleString()}</div><div>缓存读取 {row.cacheRead.toLocaleString()} · 写入 {row.cacheCreation.toLocaleString()}</div><div>{row.requests.toLocaleString()} 次调用 · 活跃 {duration(row.activeSeconds)}</div></div>;
  return <button type="button" className="usage-session-row" onClick={() => onOpen(row)} disabled={disabled} data-selected={selected || undefined} aria-label={`查看会话：${title}`} aria-haspopup={inDialog ? undefined : 'dialog'}>
    <span className="usage-session-icon" data-provider={row.provider}><Tooltip content={providerLabel(row.provider)} className="flex items-center"><Icon className="h-4 w-4 shrink-0" /></Tooltip></span>
    <span className="usage-session-identity"><strong>{title}</strong><span className="usage-session-meta"><Tooltip content={row.project ? <div className="space-y-1"><div>项目目录</div><div>{row.project}</div></div> : null} className="block min-w-0">{projectName(row.project)}</Tooltip><i>·</i><Tooltip content={row.models.length > 1 ? <div className="space-y-1"><div>会话使用的模型</div>{row.models.map(model => <div key={model}>{model}</div>)}</div> : null} className="block min-w-0">{row.models[0] || providerLabel(row.provider)}{row.models.length > 1 ? ` +${row.models.length - 1}` : ''}</Tooltip><i>·</i><time dateTime={row.lastAt}>{usageDate(row.lastAt)}</time></span></span>
    <Tooltip content={usageDetail} className="usage-session-tokens block min-w-0"><strong>{count(row.tokens)}</strong><small>{detail}</small></Tooltip>
    <Tooltip content={row.unpriced ? `${row.unpriced} 次调用尚无定价，当前费用仅包含已计价用量` : '按当前模型定价估算，不代表订阅账单'} className="usage-session-cost block min-w-0">{money(row.cost, row.unpriced, true)}</Tooltip>
    <ChevronRight className="usage-session-chevron" size={15} />
  </button>;
}
