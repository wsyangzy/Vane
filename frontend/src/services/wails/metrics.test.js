import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('./metrics.js', import.meta.url), 'utf8');
let sequence = 0;
async function bridge(service) {
  const key = `__usageBridgeTest${++sequence}`;
  globalThis[key] = service;
  const isolated = source.replace("import { WailsApp, initWails } from './connection.js';", `const WailsApp=globalThis[${JSON.stringify(key)}];const initWails=async()=>true;`);
  try { return await import(`data:text/javascript;base64,${Buffer.from(isolated).toString('base64')}`); }
  finally { delete globalThis[key]; }
}

test('local heatmap reuses the generated API and excludes proxy-ledger filters', async () => {
  let called;
  const api = await bridge({ GetTokenUsageHeatmap: async (params) => {
    called = params;
    return [{ date: '2026-10-27', bucket: 'day', request_count: 2, input_tokens: 30, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 70, total_tokens: 120, intensity: 0.5 }];
  } });
  assert.deepEqual(await api.getTokenUsageHeatmap({ provider: 'codex', model: 'gpt', channel: 'relay', status: 'failed', start_date: '2000-01-01' }), [
    { date: '2026-10-27', bucket: 'day', requestCount: 2, input: 30, output: 20, cacheCreation: 0, cacheRead: 70, total: 120, intensity: 0.5 }
  ]);
  assert.deepEqual(called, { page: 0, page_size: 0, start_date: '', end_date: '', status: '', model: 'gpt', channel: '', endpoint: '', group: '', provider: 'codex' });
});

test('native heatmap preserves zero activity days', async () => {
  const api = await bridge({ GetTokenUsageHeatmap: async () => [{ date: '2026-10-27', request_count: 0, total_tokens: 0 }] });
  const result = await api.getTokenUsageHeatmap();
  assert.equal(result[0].requestCount, 0);
  assert.equal(result[0].total, 0);
});

test('native heatmap read errors reach the panel', async () => {
  const failing = await bridge({ GetTokenUsageHeatmap: async () => { throw new Error('read failed'); } });
  await assert.rejects(failing.getTokenUsageHeatmap(), /read failed/);
});

test('request lists and usage stats forward the credential group with its channel and provider', async () => {
  const calls = [];
  const api = await bridge({
    GetRequests: async params => { calls.push(params); return { requests: [], total: 0 }; },
    GetUsageStats: async params => { calls.push(params); return { total_requests: 0 }; }
  });
  const scope = { provider: 'codex', channel: '渠道 A', group: '凭证分组 B', endpoint: '端点 C' };
  await api.getRequests(scope);
  await api.getUsageStats(scope);
  for (const params of calls) {
    assert.equal(params.provider, scope.provider);
    assert.equal(params.channel, scope.channel);
    assert.equal(params.group, scope.group);
    assert.equal(params.endpoint, scope.endpoint);
  }
});
