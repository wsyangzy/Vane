import test from 'node:test';
import assert from 'node:assert/strict';
import { clearCacheGroups, mergeCacheStatus } from './cache.js';
import { createUsageDashboardCache, USAGE_CACHE_NAME } from './usage-dashboard-cache.js';

const createCache = () => createUsageDashboardCache(async query => ({ source: query.source }));

test('cache bridge preserves unmeasured metrics, measured zero and byte budgets', () => {
  const status = mergeCacheStatus({ enabled: true, caches: [
    { name: 'empty', enabled: true, estimated_bytes: 0, max_bytes: 4 * 1024 * 1024, last_load_ms: null },
    { name: 'loaded', enabled: true, estimated_bytes: 512, last_load_ms: 1.5, loads: 2 },
    { name: 'unknown', enabled: true }
  ] }, createCache());
  const empty = status.caches.find(cache => cache.name === 'empty');
  assert.equal(empty.estimatedBytes, 0);
  assert.equal(empty.maxBytes, 4 * 1024 * 1024);
  assert.equal(empty.lastLoadMs, null);
  const loaded = status.caches.find(cache => cache.name === 'loaded');
  assert.equal(loaded.estimatedBytes, 512);
  assert.equal(loaded.lastLoadMs, 1.5);
  assert.equal(loaded.loads, 2);
  const unknown = status.caches.find(cache => cache.name === 'unknown');
  assert.equal(unknown.estimatedBytes, null);
  assert.equal(unknown.lastLoadMs, null);
});

test('overview timing counts one coalesced load and includes failed reloads', async () => {
  let clock = 10;
  let resolve;
  const work = new Promise(yes => { resolve = yes; });
  const cache = createUsageDashboardCache(() => work, { measureNow: () => clock });
  assert.equal(cache.stats().lastLoadMs, null);
  const first = cache.load({}), second = cache.load({});
  clock = 27.5;
  resolve({ tokens: 1 });
  await Promise.all([first, second]);
  assert.equal(cache.stats().loads, 1);
  assert.equal(cache.stats().lastLoadMs, 17.5);
  await cache.load({});
  assert.equal(cache.stats().loads, 1);
  const failed = createUsageDashboardCache(async () => { clock = 50; throw new Error('read failed'); }, { measureNow: () => clock });
  await assert.rejects(failed.load({}), /read failed/);
  assert.equal(failed.stats().loads, 1);
  assert.equal(failed.stats().lastLoadMs, 22.5);
});

test('clearing new backend snapshots clears only their dependent frontend source', async () => {
  for (const [name, source] of [['usage:dashboard-local', 'local'], ['usage:dashboard-pricing', 'local'], ['usage:dashboard-proxy', 'proxy']]) {
    const cache = createCache();
    await cache.load({ source: 'local' }); await cache.load({ source: 'proxy' });
    await clearCacheGroups([name], async names => { assert.deepEqual(names, [name]); return { cleared_caches: 1 }; }, cache);
    assert.equal(cache.peek({ source }), undefined);
    const retained = source === 'local' ? 'proxy' : 'local';
    assert.equal(cache.peek({ source: retained }).data.source, retained);
  }
});

test('cache status merges the actual overview cache counters with snake case backend statistics', async () => {
  const cache = createCache();
  await cache.load({ source: 'local' }); await cache.load({ source: 'local' });
  const status = mergeCacheStatus({ enabled: true, caches: [{ name: 'usage:local-index', enabled: true, size: 3, max_entries: 0, last_updated_at: 'time', hits: 8 }] }, cache);
  assert.equal(status.caches.length, 2);
  assert.equal(status.caches[0].name, USAGE_CACHE_NAME);
  assert.equal(status.caches[0].size, 1);
  assert.equal(status.caches[0].hits, 1);
  assert.equal(status.caches[0].misses, 1);
  assert.equal(status.caches[1].lastUpdatedAt, 'time');
});

test('clearing only the frontend group never sends empty names to clear every backend cache', async () => {
  const cache = createCache();
  await cache.load({});
  const result = await clearCacheGroups([USAGE_CACHE_NAME, USAGE_CACHE_NAME], () => { throw new Error('must not clear Go'); }, cache);
  assert.deepEqual(result, { clearedCaches: 1, clearedEntries: 1, unknownNames: [] });
  assert.equal(cache.stats().size, 0);
});

test('clear all combines the backend and frontend results', async () => {
  const cache = createCache();
  await cache.load({});
  const result = await clearCacheGroups([], async names => {
    assert.deepEqual(names, []);
    return { cleared_caches: 10, cleared_entries: 5 };
  }, cache);
  assert.equal(result.clearedCaches, 11);
  assert.equal(result.clearedEntries, 6);
});

test('index clear invalidates dependent local queries and retains proxy queries', async () => {
  const cache = createCache();
  await cache.load({ source: 'local' }); await cache.load({ source: 'proxy' });
  await clearCacheGroups(['usage:local-index'], async names => {
    assert.deepEqual(names, ['usage:local-index']);
    return { cleared_caches: 1, cleared_entries: 4 };
  }, cache);
  assert.equal(cache.peek({ source: 'local' }), undefined);
  assert.equal(cache.peek({ source: 'proxy' }).data.source, 'proxy');
});

test('failed backend clear leaves frontend cache intact and reports the error', async () => {
  const cache = createCache();
  await cache.load({});
  await assert.rejects(clearCacheGroups([], async () => { throw new Error('clear failed'); }, cache), /clear failed/);
  assert.equal(cache.stats().size, 1);
});

test('global cache setting disables result reuse and can be enabled again', async () => {
  let calls = 0;
  const cache = createUsageDashboardCache(async () => ++calls);
  await cache.load({});
  assert.equal(mergeCacheStatus({ enabled: false }, cache).caches[0].enabled, false);
  await cache.load({}); await cache.load({});
  assert.equal(calls, 3);
  assert.equal(cache.stats().size, 0);
  mergeCacheStatus({ enabled: true }, cache);
  await cache.load({}); await cache.load({});
  assert.equal(calls, 4);
});
