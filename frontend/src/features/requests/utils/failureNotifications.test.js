import test from 'node:test';
import assert from 'node:assert/strict';
import { createFailureNotificationAggregator, createFailureNotificationPoller } from './failureNotifications.js';

const failure = (id, fields = {}) => ({ request_id: String(id), provider: 'codex', channel: 'anyrouter', failure_reason: 'recovery_outside_budget',
  http_status_is_upstream: false, route_snapshot: { profile_id: 7, decision: { kind: 'default' }, attempts: 0 }, ...fields });

test('same provider, dispatch scope and cause update one quiet-period notification window', () => {
  const groups = createFailureNotificationAggregator();
  const [first] = groups.consume([failure(1), failure(2)], 1000);
  assert.equal(first.update, false);
  assert.match(first.message.at(-1), /2 个请求/);
  const [second] = groups.consume([failure(3)], 8000);
  assert.equal(second.id, first.id);
  assert.equal(second.update, true);
  assert.match(second.message.at(-1), /3 个请求/);
  const [stillActive] = groups.consume([failure(4)], 31000);
  assert.equal(stillActive.id, first.id);
  assert.match(stillActive.message.at(-1), /4 个请求/);
  const [nextWindow] = groups.consume([failure(5)], 62000);
  assert.notEqual(nextWindow.id, first.id);
  assert.equal(nextWindow.update, false);
  assert.ok(!nextWindow.message.join().includes('5 个请求'));
});

test('provider, profile, rule, route and stop reason boundaries are never merged', () => {
  const groups = createFailureNotificationAggregator();
  const changes = groups.consume([
    failure(1), failure(2, { provider: 'claude' }),
    failure(3, { route_snapshot: { profile_id: 8 } }),
    failure(4, { route_snapshot: { profile_id: 7, decision: { kind: 'model', rule_id: 2 } } }),
    failure(5, { failure_reason: 'retry_attempts_exhausted' }),
    failure(6, { route_snapshot: null, endpoint: 'one' }),
    failure(7, { route_snapshot: null, endpoint: 'two' }),
    failure(8, { failure_reason: 'server_error' }),
  ], 0);
  assert.equal(changes.length, 7);
  assert.equal(new Set(changes.map(item => item.id)).size, 7);
});

test('notification grouping retains a bounded number of independent scopes', () => {
  const groups = createFailureNotificationAggregator({ maxGroups: 2 });
  const [first] = groups.consume([failure(1)], 0);
  groups.consume([failure(2, { channel: 'two', route_snapshot: null }), failure(3, { channel: 'three', route_snapshot: null })], 1);
  const [again] = groups.consume([failure(4)], 2);
  assert.equal(again.update, false);
  assert.notEqual(again.id, first.id);
});

test('different candidate blocker causes do not share one notification', () => {
  const groups = createFailureNotificationAggregator();
  const [cooling, limited] = groups.consume([
    failure(1, { route_snapshot: { profile_id: 7, candidates: [{ action: 'skipped', reason: 'cooldown', candidate: { channel: '41' } }] } }),
    failure(2, { route_snapshot: { profile_id: 7, candidates: [{ action: 'skipped', reason: 'daily_cost_limit_exceeded', candidate: { channel: '38' } }] } }),
  ], 1000);
  assert.notEqual(cooling.id, limited.id);
  assert.ok(cooling.message.some(line => line.includes('健康冷却')));
  assert.ok(limited.message.some(line => line.includes('每日成本限额')));
});

test('poller seeds old records then reads all pages, deduplicating IDs and including long-running requests', async () => {
  let clock = 10000;
  const calls = [];
  const old = failure('old');
  const recent = [old, failure('long', { start_time: '2026-11-16 23:59:59' }), failure('new')];
  let seed = true;
  const poller = createFailureNotificationPoller({ now: () => clock, pageSize: 2, pagesPerPoll: 1,
    fetchRequests: async (params) => {
      calls.push(params);
      const rows = seed ? [old] : recent;
      return { requests: rows.slice((params.page - 1) * 2, params.page * 2) };
    } });
  assert.deepEqual(await poller.poll(), []);
  seed = false;
  clock = 14000;
  assert.deepEqual((await poller.poll()).map(row => row.request_id), ['long']);
  clock = 18000;
  assert.deepEqual((await poller.poll()).map(row => row.request_id), ['new']);
  assert.equal(calls[1].completedBefore, calls[2].completedBefore);
  assert.equal(calls[1].completedSince, calls[2].completedSince);
  assert.equal(calls[1].completedSince, '1970-01-01T00:00:10Z');
  assert.ok(!('startDate' in calls[1]));
  await poller.poll();
  assert.equal(calls[3].completedSince, '1970-01-01T00:00:14Z');
});

test('a failed page preserves earlier results and resumes the same completion window', async () => {
  let clock = 10000;
  let fail = true;
  const calls = [];
  const poller = createFailureNotificationPoller({ now: () => clock, pageSize: 1, pagesPerPoll: 5,
    fetchRequests: async (params) => {
      calls.push(params);
      if (clock === 10000) return { requests: [] };
      if (params.page === 1) return { requests: [failure(1)] };
      if (fail) throw new Error('temporary query failure');
      return { requests: [] };
    } });
  await poller.poll();
  clock = 14000;
  assert.equal((await poller.poll()).length, 1);
  fail = false;
  clock = 18000;
  assert.deepEqual(await poller.poll(), []);
  assert.equal(calls.at(-1).page, 2);
  assert.equal(calls.at(-1).completedBefore, '1970-01-01T00:00:14Z');
});

test('bursts larger than one batch are counted across polls without latest-row loss', async () => {
  let clock = 10000;
  const requests = Array.from({ length: 1251 }, (_, index) => failure(index));
  const poller = createFailureNotificationPoller({ now: () => clock,
    fetchRequests: async ({ page, pageSize }) => ({ requests: clock === 10000 ? [] : requests.slice((page - 1) * pageSize, page * pageSize) }) });
  await poller.poll();
  clock = 20000;
  const delivered = [...await poller.poll(), ...await poller.poll(), ...await poller.poll()];
  assert.equal(delivered.length, requests.length);
  assert.equal(new Set(delivered.map(item => item.request_id)).size, requests.length);
});
