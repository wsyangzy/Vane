import { buildUsageDashboardQuery, getUsageDashboard } from './usage-dashboard.js';

export const USAGE_CACHE_MAX_AGE = 30_000;
export const USAGE_CACHE_NAME = 'overview:queries';

export function usageDashboardQueryKey(query) {
  const { page_size: pageSize, ...fields } = buildUsageDashboardQuery(query);
  return JSON.stringify({ ...fields, pageSize });
}

// Only aggregate metadata lives here. Keep the cache bounded and in memory;
// manual refresh invalidates the source, including its other filter results.
export function createUsageDashboardCache(fetchDashboard, { now = Date.now, capacity = 24 } = {}) {
  const entries = new Map();
  const pending = new Map();
  const versions = new Map();
  let generation = 0;
  let enabled = true;
  let ttl = USAGE_CACHE_MAX_AGE;
  let limit = capacity;
  const counters = { hits: 0, misses: 0, expired: 0, evicted: 0, invalidations: 0, lastUpdatedAt: '', lastInvalidatedAt: '' };
  const version = source => `${generation}:${versions.get(source) || 0}`;
  const peek = query => entries.get(usageDashboardQueryKey(query));
  const isFresh = (query, maxAge = ttl) => {
    const entry = peek(query);
    return Boolean(enabled && entry && now() - entry.at < maxAge);
  };
  const load = (query, { force = false, maxAge = ttl } = {}) => {
    const key = usageDashboardQueryKey(query);
    const canonical = JSON.parse(key);
    const source = canonical.source;
    const cached = entries.get(key);
    if (!force && isFresh(query, maxAge)) {
      counters.hits++;
      entries.delete(key);
      entries.set(key, cached);
      return Promise.resolve(cached.data);
    }
    const revision = version(source);
    const active = pending.get(key);
    if (active?.revision === revision) { counters.hits++; return active.promise; }
    counters.misses++;
    if (cached && now() - cached.at >= maxAge) counters.expired++;
    const request = { revision };
    request.promise = Promise.resolve().then(() => fetchDashboard(canonical)).then(data => {
      if (enabled && version(source) === revision) {
        entries.delete(key);
        entries.set(key, { data, at: now(), source });
        counters.lastUpdatedAt = new Date(now()).toISOString();
        while (entries.size > limit) { entries.delete(entries.keys().next().value); counters.evicted++; }
      }
      return data;
    }).finally(() => {
      if (pending.get(key) === request) pending.delete(key);
    });
    pending.set(key, request);
    return request.promise;
  };
  const invalidate = source => {
    if (source === undefined) generation++;
    else versions.set(source, (versions.get(source) || 0) + 1);
    let removed = 0;
    for (const [key, entry] of entries) if (source === undefined || entry.source === source) { entries.delete(key); removed++; }
    counters.invalidations++;
    counters.lastInvalidatedAt = new Date(now()).toISOString();
    return removed;
  };
  const configure = (options = {}) => {
    if (options.enabled !== undefined && Boolean(options.enabled) !== enabled) { enabled = Boolean(options.enabled); invalidate(); }
    if (options.maxAge !== undefined) ttl = options.maxAge;
    if (options.capacity > 0) limit = Math.min(capacity, options.capacity);
    while (entries.size > limit) { entries.delete(entries.keys().next().value); counters.evicted++; }
  };
  const stats = () => ({ name: USAGE_CACHE_NAME, enabled, fixed: false, ttl: `${ttl / 1000}s`, maxEntries: limit, size: entries.size, ...counters });
  return { peek, isFresh, load, invalidate, configure, stats };
}

export const usageDashboardCache = createUsageDashboardCache(getUsageDashboard);
