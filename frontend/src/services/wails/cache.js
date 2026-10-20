import { WailsApp, initWails } from './connection.js';

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

const getCacheStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetCacheStatus();
  return {
    enabled: Boolean(status?.enabled),
    caches: (status?.caches || []).map(mapCacheStatusItem)
  };
};

const clearCache = async (names = []) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const result = await WailsApp.ClearCache({
    names: Array.isArray(names) ? names : []
  });
  return {
    clearedCaches: Number(result?.cleared_caches ?? result?.clearedCaches ?? 0),
    clearedEntries: Number(result?.cleared_entries ?? result?.clearedEntries ?? 0),
    unknownNames: result?.unknown_names || result?.unknownNames || []
  };
};

export { getCacheStatus, clearCache };
