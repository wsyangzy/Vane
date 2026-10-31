import { WailsApp, initWails } from './connection.js';
import { usageDashboardCache, USAGE_CACHE_NAME } from './usage-dashboard-cache.js';

const mapCacheStatusItem = (item = {}) => {
  const maxEntries = Number(item.max_entries ?? item.maxEntries ?? 0);
  const lastUpdatedAt = item.last_updated_at || item.lastUpdatedAt || '';
  const lastInvalidatedAt = item.last_invalidated_at || item.lastInvalidatedAt || '';
  return {
    name: item.name || '',
    enabled: Boolean(item.enabled),
    ttl: item.ttl || '',
    maxEntries,
    max_entries: maxEntries,
    size: Number(item.size ?? 0),
    hits: Number(item.hits ?? 0),
    misses: Number(item.misses ?? 0),
    fixed: Boolean(item.fixed),
    expired: Number(item.expired ?? 0),
    evicted: Number(item.evicted ?? 0),
    invalidations: Number(item.invalidations ?? 0),
    staleServed: Number(item.stale_served ?? item.staleServed ?? 0),
    lastUpdatedAt,
    last_updated_at: lastUpdatedAt,
    lastInvalidatedAt,
    last_invalidated_at: lastInvalidatedAt
  };
};

// ============================================
// 缓存状态 API
// ============================================

export const mergeCacheStatus = (status, cache = usageDashboardCache) => {
  cache.configure({ enabled: Boolean(status?.enabled) });
  return {
    enabled: Boolean(status?.enabled),
    caches: [...(status?.caches || []).map(mapCacheStatusItem), cache.stats()].sort((a, b) => a.name.localeCompare(b.name))
  };
};

let settingsPromise;
export const ensureUsageCacheSettings = () => {
  if (!settingsPromise) settingsPromise = getCacheStatus().catch(error => { settingsPromise = null; throw error; });
  return settingsPromise;
};

const getCacheStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetCacheStatus();
  return mergeCacheStatus(status);
};

export const clearCacheGroups = async (names, clearBackend, cache = usageDashboardCache) => {
  const selected = [...new Set(Array.isArray(names) ? names : [])];
  const all = selected.length === 0;
  const backend = selected.filter(name => name !== USAGE_CACHE_NAME);
  // An empty backend selection means "clear all" to Go; never send it for a
  // frontend-only selection. Clear local values only after Go succeeds.
  const result = all || backend.length ? await clearBackend(all ? [] : backend) : {};
  let clearedCaches = Number(result?.cleared_caches ?? result?.clearedCaches ?? 0);
  let clearedEntries = Number(result?.cleared_entries ?? result?.clearedEntries ?? 0);
  if (all || selected.includes(USAGE_CACHE_NAME)) {
    clearedCaches++;
    clearedEntries += cache.invalidate();
  } else if (backend.includes('usage:local-index')) {
    const removed = cache.invalidate('local');
    if (removed) { clearedCaches++; clearedEntries += removed; }
  }
  return { clearedCaches, clearedEntries, unknownNames: result?.unknown_names || result?.unknownNames || [] };
};

const clearCache = async (names = []) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return clearCacheGroups(names, selected => WailsApp.ClearCache({ names: selected }));
};

export { getCacheStatus, clearCache };
