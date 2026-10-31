import test from 'node:test';
import assert from 'node:assert/strict';
import { count, duration, money, nextUsageQuery, defaultUsageQuery, projectName, projectParent, sessionTitle, usageChartDays } from './usageDashboard.js';

test('source/provider changes clear incompatible filters and reset pagination', () => {
  const old = { ...defaultUsageQuery, source: 'proxy', provider: 'claude', project: 'old', credential: 'key', search: 'old', model: 'model', page: 3 };
  const local = nextUsageQuery(old, { source: 'local' });
  assert.equal(local.credential, ''); assert.equal(local.project, ''); assert.equal(local.search, ''); assert.equal(local.page, 1);
  const codex = nextUsageQuery(old, { provider: 'codex' });
  assert.equal(codex.model, ''); assert.equal(codex.credential, ''); assert.equal(codex.project, '');
  assert.equal(nextUsageQuery(old, { page: 2 }).page, 2);
  assert.equal(nextUsageQuery({ ...old, day: '2026-10-27' }, { period: '7d' }).day, '');
  assert.equal(nextUsageQuery(old, { period: 'all', day: '2026-10-01' }).day, '2026-10-01');
});

test('session labels distinguish topics and same-name projects without promoting IDs to titles', () => {
  assert.equal(sessionTitle({ title: '  实施统一路由  ', id: '["codex","session"]' }), '实施统一路由');
  assert.equal(sessionTitle({ id: '["codex","long-session-id"]' }), '未命名会话 · ssion-id');
  assert.equal(projectName('D:\\team-a\\Vane'), 'Vane');
  assert.equal(projectParent('D:\\team-a\\Vane'), 'D:/team-a');
  assert.notEqual(projectParent('D:/team-a/Vane'), projectParent('D:/team-b/Vane'));
});
test('long histories retain exact chart sums while bounding the displayed bars', () => {
  const days = Array.from({ length: 366 }, (_, i) => ({ date: `2026-${String(Math.floor(i / 31) + 1).padStart(2, '0')}-${String(i % 31 + 1).padStart(2, '0')}`, input: i, output: 1, tokens: i + 1, requests: 1, sessions: 1, cost: i / 100, activeSeconds: i, unpriced: 0 }));
  const series = usageChartDays(days, 'tokens');
  assert.ok(series.length <= 100);
  assert.equal(series.reduce((sum, day) => sum + day.value, 0), days.reduce((sum, day) => sum + day.tokens, 0));
  assert.equal(series.reduce((sum, day) => sum + day.input + day.output, 0), days.reduce((sum, day) => sum + day.tokens, 0));
  assert.equal(series.at(-1).endDate, days.at(-1).date);
  assert.equal(usageChartDays(days.slice(0, 7), 'cost')[0].date, days[0].date);
});
test('usage formatting preserves small prices, missing prices and path identity', () => {
  assert.equal(money(.0001, 1), '≈$0.0001+');
  assert.equal(money(123.45, 0, false), '$123.45');
  assert.equal(duration(300), '5 分钟'); assert.equal(duration(30), '<1 分钟'); assert.equal(duration(3660), '1 小时 1 分');
  assert.equal(projectName('D:\\work\\Vane'), 'Vane'); assert.equal(projectName(''), '未记录项目'); assert.equal(count(0), '0');
});
