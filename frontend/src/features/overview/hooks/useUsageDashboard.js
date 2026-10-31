import { startTransition, useCallback, useEffect, useRef, useState } from 'react';
import { usageDashboardCache as cache, usageDashboardQueryKey, USAGE_CACHE_MAX_AGE } from '@/services/wails/usage-dashboard-cache.js';
import { ensureUsageCacheSettings } from '@/services/wails/cache.js';

export default function useUsageDashboard(query, interval) {
  const [result, setResult] = useState(null);
  const [failure, setFailure] = useState(null);
  const [request, setRequest] = useState(null);
  const sequence = useRef({ value: 0 });
  const key = usageDashboardQueryKey(query);
  const scope = JSON.stringify([query.source, query.provider]);
  const maxAge = interval > 0 ? Math.min(interval * 1000, USAGE_CACHE_MAX_AGE) : USAGE_CACHE_MAX_AGE;
  const load = useCallback(async ({ force = false } = {}) => {
    const id = ++sequence.current.value;
    const selected = JSON.parse(key);
    setRequest({ key, loading: force || !cache.isFresh(selected, maxAge) });
    setFailure(null);
    try {
      await ensureUsageCacheSettings();
      cache.configure({ maxAge });
      const data = await cache.load(selected, { force, maxAge });
      if (id !== sequence.current.value) return;
      startTransition(() => {
        setResult({ key, scope, data });
        setRequest({ key, loading: false });
      });
    } catch (failure) {
      if (id === sequence.current.value) {
        setFailure({ key, message: failure?.message || '用量加载失败' });
        setRequest({ key, loading: false });
      }
    }
  }, [key, scope, maxAge]);
  const refresh = useCallback(() => {
    cache.invalidate(JSON.parse(key).source);
    return load({ force: true });
  }, [key, load]);
  useEffect(() => {
    const pending = sequence.current;
    // A short delay combines rapid date clicks before starting desktop work.
    const initial = setTimeout(load, query.search ? 250 : query.day ? 80 : 0);
    const timer = interval > 0 ? setInterval(() => { if (!document.hidden) load({ force: true }); }, interval * 1000) : null;
    return () => { ++pending.value; clearTimeout(initial); clearInterval(timer); };
  }, [load, interval, query.search, query.day]);
  const cached = cache.peek(JSON.parse(key));
  const current = result?.key === key ? result : cached && { key, data: cached.data };
  // Keep the last view while its filters change, but never carry another
  // source/provider's values across that boundary. Label retained data in UI.
  const displayed = current || (result?.scope === scope ? result : null);
  return {
    data: displayed?.data || null,
    dataKey: displayed?.key || key,
    previous: Boolean(displayed && displayed.key !== key),
    error: failure?.key === key ? failure.message : '',
    loading: request?.key === key ? request.loading : !cache.isFresh(JSON.parse(key), maxAge),
    refresh,
  };
}
