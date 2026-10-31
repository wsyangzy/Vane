export const sourceTabs = [{ key: 'local', label: '本地会话' }, { key: 'proxy', label: '代理用量' }];
export const periodTabs = [{ key: 'today', label: '今天' }, { key: '7d', label: '7 天' }, { key: '30d', label: '30 天' }, { key: '90d', label: '90 天' }, { key: 'all', label: '全部' }];
export const providerTabs = [{ key: '', label: '全部' }, { key: 'claude', label: 'Claude Code' }, { key: 'codex', label: 'Codex' }];
export const defaultUsageQuery = { source: 'local', period: '30d', provider: '', model: '', project: '', credential: '', day: '', search: '', sort: 'recent', page: 1, pageSize: 20 };
export const providerLabel = (provider) => ({ claude: 'Claude Code', codex: 'Codex' }[provider] || provider || '未记录');
export function count(value = 0) {
  return new Intl.NumberFormat('en', { notation: value >= 1000 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value);
}
export function money(value = 0, unpriced = 0, estimate = true) {
  return `${estimate ? '≈' : ''}$${new Intl.NumberFormat('en', { maximumFractionDigits: value < 1 ? 4 : 2 }).format(value)}${unpriced ? '+' : ''}`;
}
export function duration(seconds = 0) {
  if (seconds < 0) return '—';
  if (seconds > 0 && seconds < 60) return '<1 分钟';
  const minutes = Math.round(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分` : `${minutes} 分钟`;
}
export const projectName = (value) => value?.replace(/\\/g, '/').split('/').filter(Boolean).at(-1) || '未记录项目';
export const unrecordedProject = '\0';
export const projectParent = (value) => value?.replace(/\\/g, '/').replace(/\/$/, '').split('/').slice(0, -1).join('/') || '';
export function sessionTitle(row) {
  if (row.title?.trim()) return row.title.trim();
  let id = row.id || '';
  try { const parts = JSON.parse(id); if (Array.isArray(parts)) id = parts.at(-1) || ''; } catch { /* Older sources may already use the native ID. */ }
  return `未命名会话${id ? ` · ${id.slice(-8)}` : ''}`;
}
export const usageDate = (value) => value ? new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }) : '—';
export function nextUsageQuery(previous, patch) {
  const next = { ...previous, ...patch, page: patch.page || 1 };
  if (patch.source && patch.source !== previous.source) { next.project = ''; next.credential = ''; next.search = ''; next.day = ''; next.sort = 'recent'; }
  if ('provider' in patch && patch.provider !== previous.provider) { next.model = patch.model || ''; next.project = patch.project || ''; next.credential = patch.credential || ''; }
  if ('period' in patch) next.day = patch.day || '';
  return next;
}
export function usageChartDays(days, metric) {
  const metricValue = (day) => Number(day[metric] || 0);
  if (days.length <= 100) return days.map((day) => ({ ...day, label: day.date.slice(5).replace('-', '/'), endDate: day.date, value: metricValue(day) }));
  const size = Math.ceil(days.length / 100);
  return Array.from({ length: Math.ceil(days.length / size) }, (_, index) => {
    const chunk = days.slice(index * size, (index + 1) * size);
    return { date: chunk[0].date, endDate: chunk.at(-1).date, label: chunk[0].date.slice(0, 7), value: chunk.reduce((sum, day) => sum + metricValue(day), 0), tokens: chunk.reduce((sum, day) => sum + day.tokens, 0), input: chunk.reduce((sum, day) => sum + day.input, 0), output: chunk.reduce((sum, day) => sum + day.output, 0), requests: chunk.reduce((sum, day) => sum + day.requests, 0), sessions: chunk.reduce((sum, day) => sum + day.sessions, 0), cost: chunk.reduce((sum, day) => sum + day.cost, 0), activeSeconds: chunk.reduce((sum, day) => sum + day.activeSeconds, 0), unpriced: chunk.reduce((sum, day) => sum + day.unpriced, 0) };
  });
}
