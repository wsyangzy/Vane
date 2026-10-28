import test from 'node:test';
import assert from 'node:assert/strict';
import { buildUsageDashboardQuery, getUsageDashboard, mapUsageDashboard } from './usage-dashboard.js';

test('usage dashboard maps actual snake case DTO fields across every nested section', () => {
  const metric = { requests: 0, sessions: 2, input_tokens: 30, output_tokens: 20, tokens: 50, cache_read_tokens: 70, cost_usd: .001, unpriced: 1, active_seconds: 120.5, active_days: 2, median_tokens: 25, p90_tokens: 40, cache_hit_rate: .7 };
  const result = mapUsageDashboard({ source: 'local', from: '2026-10-01', to: '2026-10-27', timezone: 'Asia/Shanghai', updated_at: 'now', summary: metric, days: [{ date: '2026-10-27', ...metric }], calendar: [{ date: '2026-10-27', ...metric }], models: [{ key: 'key', name: 'gpt', ...metric }], projects: [], credential_options: [{ key: 'key', channel: 'channel', group: 'group' }], rows: [{ id: 's', first_at: 'first', last_at: 'last', models: ['gpt'], ...metric }], row_count: 2, page_size: 20, unpriced_models: ['gpt'], model_options: ['gpt'], project_options: [] });
  assert.equal(result.summary.cacheRead, 70); assert.equal(result.summary.tokens, 50); assert.equal(result.summary.activeSeconds, 120.5); assert.equal(result.summary.cacheHitRate, .7);
  assert.equal(result.rows[0].firstAt, 'first'); assert.equal(result.rows[0].cost, .001); assert.equal(result.days[0].unpriced, 1); assert.equal(result.calendar[0].date, '2026-10-27'); assert.equal(result.rowCount, 2);
  assert.equal(result.credentialOptions[0].channel, 'channel'); assert.equal(result.models[0].name, 'gpt');
  assert.equal(mapUsageDashboard({}).summary.cacheHitRate, null);
});
test('usage query exposes supported analytics filters only', () => {
  assert.deepEqual(buildUsageDashboardQuery({ source: 'proxy', provider: 'codex', credential: 'group', project: '', page: 2, pageSize: 10, search: ' test ', endpoint: 'must-not-filter', privateBody: 'secret' }), { source: 'proxy', period: '30d', provider: 'codex', model: '', project: '', credential: 'group', day: '', search: 'test', sort: 'recent', page: 2, page_size: 10 });
});
test('usage workspace fails clearly outside the desktop environment', async () => {
  await assert.rejects(getUsageDashboard({}), /桌面应用/);
});

test('session title and project metadata cross the explicit DTO mapping boundary', () => {
  const result = mapUsageDashboard({ projects: [{ key: 'D:/team-a/Vane', name: 'Vane', title: '修复会话明细', last_at: '2026-10-28T01:00:00Z', sessions: 25 }], rows: [{ id: '["codex","one"]', title: '实施统一调度', project: 'D:/team-a/Vane', models: ['model-a'] }] });
  assert.equal(result.rows[0].title, '实施统一调度');
  assert.equal(result.projects[0].lastAt, '2026-10-28T01:00:00Z');
  assert.equal(result.projects[0].title, '修复会话明细');
  assert.equal(result.projects[0].sessions, 25);
  assert.equal(mapUsageDashboard({ rows: [{}] }).rows[0].title, '');
});
