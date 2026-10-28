import test from 'node:test';
import assert from 'node:assert/strict';
import { clearCacheGroups, mergeCacheStatus } from './cache.js';
import { createUsageDashboardCache, USAGE_CACHE_NAME } from './usage-dashboard-cache.js';

const createCache = () => createUsageDashboardCache(async query => ({ source: query.source }));

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
