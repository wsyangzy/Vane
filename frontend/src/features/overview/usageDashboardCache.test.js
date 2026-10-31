import test from 'node:test';
import assert from 'node:assert/strict';
import { createUsageDashboardCache, usageDashboardQueryKey, USAGE_CACHE_MAX_AGE } from '../../services/wails/usage-dashboard-cache.js';
import { defaultUsageQuery } from './usageDashboard.js';

const dayQuery = day => ({ ...defaultUsageQuery, period: 'all', day });
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};

test('usage cache returns revisited days immediately and expires after its freshness window', async () => {
  let at = 0, reads = 0;
  const cache = createUsageDashboardCache(async query => ({ day: query.day, reads: ++reads }), { now: () => at });
  const a = dayQuery('2026-10-26'), b = dayQuery('2026-10-27');
  const first = await cache.load(a);
  await cache.load(b);
  assert.equal(await cache.load(a), first);
  assert.equal(reads, 2);
  at = USAGE_CACHE_MAX_AGE;
  assert.equal(cache.isFresh(a), false);
  assert.equal(cache.peek(a).data, first, 'expired results remain available during revalidation');
  assert.equal((await cache.load(a)).reads, 3);
});

test('usage cache coalesces concurrent desktop requests even for forced refresh', async () => {
  let reads = 0;
  const work = deferred();
  const cache = createUsageDashboardCache(() => { reads++; return work.promise; });
  const query = dayQuery('2026-10-27');
  const a = cache.load(query), b = cache.load(query), c = cache.load(query, { force: true });
  assert.equal(a, b);
  assert.equal(a, c);
  work.resolve({ tokens: 42 });
  assert.deepEqual(await a, { tokens: 42 });
  assert.equal(reads, 1);
});

test('usage cache keeps only the most recently used query results', async () => {
  let reads = 0;
  const cache = createUsageDashboardCache(async () => ++reads, { capacity: 2 });
  const a = dayQuery('2026-10-25'), b = dayQuery('2026-10-26'), c = dayQuery('2026-10-27');
  await cache.load(a); await cache.load(b); await cache.load(a); await cache.load(c);
  assert.equal(cache.peek(b), undefined);
  assert.equal(cache.peek(a).data, 1);
  assert.equal(cache.peek(c).data, 3);
});

test('usage cache isolates source, provider, filters and pagination and normalizes query keys', async () => {
  let reads = 0;
  const cache = createUsageDashboardCache(async query => ({ ...query, reads: ++reads }));
  const base = dayQuery('2026-10-27');
  for (const patch of [{}, { source: 'proxy' }, { provider: 'codex' }, { project: 'project-a' }, { model: 'model-a' }, { credential: 'group-a' }, { search: 'session-a' }, { page: 2 }, { sort: 'tokens' }, { period: '7d' }, { pageSize: 50 }]) {
    await cache.load({ ...base, ...patch });
  }
  assert.equal(reads, 11);
  assert.equal(usageDashboardQueryKey({ ...base, search: ' session-a ', ignored: 'value' }), usageDashboardQueryKey({ ...base, search: 'session-a' }));
  assert.equal((await cache.load({ ...base, search: ' session-a ' })).reads, 7);
});

test('failed usage queries can retry and do not replace the last good snapshot', async () => {
  let fails = false, reads = 0;
  const cache = createUsageDashboardCache(async () => { reads++; if (fails) throw new Error('read failed'); return { reads }; });
  const query = dayQuery('2026-10-27');
  const good = await cache.load(query);
  fails = true;
  await assert.rejects(cache.load(query, { force: true }), /read failed/);
  assert.equal(cache.peek(query).data, good);
  fails = false;
  assert.equal((await cache.load(query, { force: true })).reads, 3);
});

test('manual invalidation refreshes related days without clearing the other source', async () => {
  let reads = 0;
  const cache = createUsageDashboardCache(async () => ++reads);
  const a = dayQuery('2026-10-26'), b = dayQuery('2026-10-27'), proxy = { ...a, source: 'proxy' };
  await cache.load(a); await cache.load(b); await cache.load(proxy);
  cache.invalidate('local');
  assert.equal(cache.peek(a), undefined);
  assert.equal(cache.peek(b), undefined);
  assert.equal(cache.peek(proxy).data, 3);
  assert.equal(await cache.load(a), 4);
});

test('invalidated in-flight usage data cannot overwrite the new refresh', async () => {
  const old = deferred(), fresh = deferred();
  let reads = 0;
  const cache = createUsageDashboardCache(() => ++reads === 1 ? old.promise : fresh.promise);
  const query = dayQuery('2026-10-27');
  const before = cache.load(query);
  await Promise.resolve();
  cache.invalidate('local');
  const after = cache.load(query, { force: true });
  await Promise.resolve();
  fresh.resolve({ tokens: 200 });
  await after;
  old.resolve({ tokens: 100 });
  await before;
  assert.equal(cache.peek(query).data.tokens, 200);
});

test('configured short refresh intervals also shorten cache freshness', async () => {
  let at = 0, reads = 0;
  const cache = createUsageDashboardCache(async () => ++reads, { now: () => at });
  const query = dayQuery('2026-10-27');
  await cache.load(query);
  at = 5000;
  assert.equal(cache.isFresh(query, 5000), false);
  assert.equal(await cache.load(query, { maxAge: 5000 }), 2);
});

test('clear all rejects late in-flight cache writes and reports counters without exposing query contents', async () => {
  const work = deferred();
  const cache = createUsageDashboardCache(() => work.promise);
  const request = cache.load({ search: 'private-project' });
  cache.invalidate();
  work.resolve({ tokens: 10 });
  await request;
  assert.equal(cache.stats().size, 0);
  assert.equal(cache.stats().misses, 1);
  assert.equal(cache.stats().invalidations, 1);
  assert.equal(JSON.stringify(cache.stats()).includes('private-project'), false);
});
