import { buildUsageDashboardQuery, getUsageDashboard } from './usage-dashboard.js';

export const USAGE_CACHE_MAX_AGE = 30_000;
export const USAGE_CACHE_NAME = 'overview:queries';
export const USAGE_CACHE_BYTE_BUDGET = 8 * 1024 * 1024;

// A conservative estimate avoids allocating another JSON copy of the result.
// Shared objects are counted once; this is a cache budget, not a JS heap metric.
function estimateBytes(value, ceiling, seen = new WeakSet()) {
  if (typeof value === 'string') return value.length * 2;
  if (typeof value === 'number') return 8;
  if (typeof value === 'boolean') return 4;
  if (!value || typeof value !== 'object' || seen.has(value)) return 0;
  seen.add(value);
  let bytes = Array.isArray(value) ? 24 : 48;
  for (const key in value) {
    if (!Object.hasOwn(value, key)) continue;
    bytes += 16 + key.length * 2 + estimateBytes(value[key], ceiling - bytes, seen);
    if (bytes > ceiling) break;
  }
  return bytes;
}

export function usageDashboardQueryKey(query) {
  const { page_size: pageSize, ...fields } = buildUsageDashboardQuery(query);
  return JSON.stringify({ ...fields, pageSize });
}

// Only aggregate metadata lives here. Keep the cache bounded and in memory;
// manual refresh invalidates the source, including its other filter results.
export function createUsageDashboardCache(fetchDashboard, { now = Date.now, measureNow = () => performance.now(), capacity = 24, byteBudget = USAGE_CACHE_BYTE_BUDGET } = {}) {
  const entries = new Map();
  const pending = new Map();
  const versions = new Map();
  let generation = 0;
  let enabled = true;
  let ttl = USAGE_CACHE_MAX_AGE;
  let limit = capacity;
  let bytes = 0;
  let lastViewedKey = '';
  const maxBytes = Number.isFinite(byteBudget) && byteBudget > 0 ? byteBudget : USAGE_CACHE_BYTE_BUDGET;
  const counters = { hits: 0, misses: 0, expired: 0, evicted: 0, invalidations: 0, loads: 0, lastLoadMs: null, lastUpdatedAt: '', lastInvalidatedAt: '' };
  const version = source => `${generation}:${versions.get(source) || 0}`;
  const remove = key => {
    const entry = entries.get(key);
    if (!entry) return;
    bytes -= entry.bytes;
    entries.delete(key);
  };
  const prune = (keep = lastViewedKey, maxAge = ttl) => {
    for (const [key, entry] of entries) {
      // Preserve the active view's last good result during revalidation/errors.
      if (key !== keep && now() - entry.at >= maxAge) { remove(key); counters.expired++; }
    }
  };
  const enforceBudget = () => {
    while (entries.size && (entries.size > limit || bytes > maxBytes)) { remove(entries.keys().next().value); counters.evicted++; }
  };
  const peek = query => {
    const key = usageDashboardQueryKey(query);
    lastViewedKey = key;
    prune(key);
    return entries.get(key);
  };
  const isFresh = (query, maxAge = ttl) => {
    const entry = peek(query);
    return Boolean(enabled && entry && now() - entry.at < maxAge);
  };
  const load = (query, { force = false, maxAge = ttl } = {}) => {
    const key = usageDashboardQueryKey(query);
    lastViewedKey = key;
    prune(key, maxAge);
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
    const started = measureNow();
    request.promise = Promise.resolve().then(() => fetchDashboard(canonical, { force })).then(data => {
      if (enabled && version(source) === revision) {
        remove(key);
        const size = estimateBytes(data, maxBytes);
        if (size <= maxBytes) {
          entries.set(key, { data, at: now(), source, bytes: size });
          bytes += size;
        } else counters.evicted++;
        counters.lastUpdatedAt = new Date(now()).toISOString();
        enforceBudget();
      }
      return data;
    }).finally(() => {
      counters.loads++;
      counters.lastLoadMs = Math.max(0, measureNow() - started);
      if (pending.get(key) === request) pending.delete(key);
    });
    pending.set(key, request);
    return request.promise;
  };
  const invalidate = source => {
    if (source === undefined) generation++;
    else versions.set(source, (versions.get(source) || 0) + 1);
    let removed = 0;
    for (const [key, entry] of entries) if (source === undefined || entry.source === source) { remove(key); removed++; }
    counters.invalidations++;
    counters.lastInvalidatedAt = new Date(now()).toISOString();
    return removed;
  };
  const configure = (options = {}) => {
    if (options.enabled !== undefined && Boolean(options.enabled) !== enabled) { enabled = Boolean(options.enabled); invalidate(); }
    if (options.maxAge !== undefined) ttl = options.maxAge;
    if (options.capacity > 0) limit = Math.min(capacity, options.capacity);
    prune();
    enforceBudget();
  };
  const stats = () => {
    prune();
    return { name: USAGE_CACHE_NAME, enabled, fixed: false, ttl: `${ttl / 1000}s`, maxEntries: limit, size: entries.size, estimatedBytes: bytes, maxBytes, budgetBytes: maxBytes, ...counters };
  };
  return { peek, isFresh, load, invalidate, configure, stats };
}

export const usageDashboardCache = createUsageDashboardCache(getUsageDashboard);
