import { WailsApp, initWails, isWailsEnvironment } from './connection.js';

const number = (value) => Number(value) || 0;
export function mapUsageMetrics(value = {}) {
  return {
    requests: number(value.requests), sessions: number(value.sessions), messages: number(value.messages),
    input: number(value.input_tokens), output: number(value.output_tokens), cacheRead: number(value.cache_read_tokens), cacheCreation: number(value.cache_creation_tokens),
    tokens: number(value.tokens), cost: number(value.cost_usd), unpriced: number(value.unpriced),
    activeSeconds: number(value.active_seconds), activeDays: number(value.active_days), projects: number(value.projects), credentialGroups: number(value.credential_groups),
    medianTokens: number(value.median_tokens), p90Tokens: number(value.p90_tokens), successRequests: number(value.success_requests), failedRequests: number(value.failed_requests),
    avgDurationMs: number(value.avg_duration_ms), cacheHitRate: value.cache_hit_rate == null ? null : number(value.cache_hit_rate),
  };
}
const mapGroup = (value) => ({ key: value.key || '', name: value.name || '', title: value.title || '', lastAt: value.last_at || '', provider: value.provider || '', channel: value.channel || '', group: value.group || '', ...mapUsageMetrics(value) });
const mapDay = (value) => ({ date: value.date || '', ...mapUsageMetrics(value) });
export function mapUsageDashboard(value) {
  return {
    source: value.source, from: value.from, to: value.to, timezone: value.timezone, updatedAt: value.updated_at,
    summary: mapUsageMetrics(value.summary), days: (value.days || []).map(mapDay), calendar: (value.calendar || []).map(mapDay),
    projects: (value.projects || []).map(mapGroup), models: (value.models || []).map(mapGroup), providers: (value.providers || []).map(mapGroup), credentials: (value.credentials || []).map(mapGroup),
    modelOptions: value.model_options || [], projectOptions: value.project_options || [], credentialOptions: (value.credential_options || []).map(mapGroup), unpricedModels: value.unpriced_models || [],
    rows: (value.rows || []).map((row) => ({ id: row.id, title: row.title || '', provider: row.provider, project: row.project || '', channel: row.channel || '', group: row.group || '', models: row.models || [], firstAt: row.first_at, lastAt: row.last_at, status: row.status || '', ...mapUsageMetrics(row) })),
    rowCount: number(value.row_count), page: number(value.page) || 1, pageSize: number(value.page_size) || 20,
  };
}
export const buildUsageDashboardQuery = (value) => ({ source: value.source || 'local', period: value.period || '30d', provider: value.provider || '', model: value.model || '', project: value.project || '', credential: value.credential || '', day: value.day || '', search: value.search?.trim() || '', sort: value.sort || 'recent', page: value.page || 1, page_size: value.pageSize || 20 });
export async function getUsageDashboard(query) {
  if (!isWailsEnvironment()) throw new Error('请在 Vane 桌面应用中查看本机用量');
  if (!await initWails()) throw new Error('用量桌面接口加载失败');
  if (typeof WailsApp?.GetUsageDashboard !== 'function') throw new Error('当前运行的应用尚未包含用量工作区，请启动本次构建的桌面应用');
  return mapUsageDashboard(await WailsApp.GetUsageDashboard(buildUsageDashboardQuery(query)));
}
