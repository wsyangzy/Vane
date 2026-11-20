import test from 'node:test';
import assert from 'node:assert/strict';
import { buildCostSteps, accumulateWaterfall, buildFallbackAttempts, buildContextMap, buildContextCostEstimate, contextTileRows, formatRequestInsightCost, layoutContextTiles, summarizeTavilyUsage } from './requestInsights.js';

test('Codex cache-inclusive input and reasoning are not counted twice', () => {
  const data = buildCostSteps({ provider: 'codex', inputTokens: 1000, cacheReadTokens: 800, outputTokens: 100, reasoningTokens: 80, inputCost: .002, cacheReadCost: .001, outputCost: .003, cost: .006 });
  assert.equal(data.tokens, 1100);
  assert.deepEqual([data.input, data.output], [1000, 100]);
  assert.deepEqual(data.rows.map(({ input, output }) => [input, output]), [[200, null], [800, null], [null, 20], [null, 80]]);
  assert.ok(Math.abs(data.rows.at(-1).cost - .0024) < 1e-12);
  assert.equal(accumulateWaterfall(data.rows, 'cost').steps.at(-1).end, .006);
  assert.equal(accumulateWaterfall(data.rows, 'tokens').steps.at(-1).end, 1100);
});

test('output billing follows the recorded source, including zero visible output', () => {
  const request = { provider: 'codex', outputTokens: 100, reasoningTokens: 80, outputCost: .003, cost: .003 };
  for (const [source, billable, id] of [['reasoning_tokens', 80, 'output-reasoning'], ['visible_output_tokens', 20, 'output-visible']]) {
    const data = buildCostSteps({ ...request, billingOutputTokenSource: source, billableOutputTokens: billable });
    assert.deepEqual(data.rows.map(row => [row.id, row.output, row.cost]), [[id, billable, .003]]);
  }
  const visible = buildCostSteps({ ...request, billingOutputTokenSource: 'visible_output_tokens', reasoningTokens: 100, billableOutputTokens: 0, outputCost: 0, cost: 0 });
  assert.equal(visible.output, 0);
  assert.deepEqual(visible.rows, []);
  const legacy = buildCostSteps({ ...request, reasoningTokens: 120 });
  assert.deepEqual(legacy.rows.map(row => [row.id, row.output, row.cost]), [['output', 100, .003]]);
});

test('context attribution reconciles to the known input side and requires a complete summary', () => {
  const contextSummary = { version: 2, parts: {
    system: { tokens: 3, breakdown: [{ kind: 'prompt', tokens: 1 }, { kind: 'developer', tokens: 2 }] },
    chat: { tokens: 1, breakdown: [{ kind: 'turn', turn: 1, tokens: 1 }] },
  } };
  const request = { provider: 'codex', inputTokens: 100, cacheReadTokens: 50, cacheCreationTokens: 20,
    inputCost: .3, cacheReadCost: .1, cacheCreationCost: .4, outputTokens: 10, outputCost: .2, cost: 1, contextSummary };
  const estimate = buildContextCostEstimate(request, buildCostSteps(request));
  assert.equal(estimate.cost, .8);
  assert.deepEqual(estimate.rows.map(row => [row.id, row.tokens]), [['system', 75], ['chat', 25]]);
  assert.deepEqual(estimate.rows[0].entries.map(row => [row.label, row.tokens, row.cost]), [['开发者指令', 50, .4], ['Agent 提示词', 25, .2]]);
  assert.equal(buildContextCostEstimate({ ...request, contextSummary: { ...contextSummary, partial: true } }, buildCostSteps(request)), null);
  assert.equal(buildContextCostEstimate(request, buildCostSteps({ ...request, cost: 2 })), null);
});

test('live context rows and their expanded entries occupy the input portion of the waterfall', () => {
  const request = {
    provider: 'codex', inputTokens: 40, outputTokens: 10, inputCost: .04, outputCost: .02, cost: .06,
    billingRuleSnapshot: { live_input_estimated: true, live_output_estimated: true },
    contextSummary: { version: 2, parts: {
      system: { tokens: 30, breakdown: [{ kind: 'prompt', tokens: 30 }] },
      tools: { tokens: 10, breakdown: [{ kind: 'tool', name: 'sample', tokens: 10 }] },
    } },
  };
  const context = buildContextMap(request);
  assert.equal(context.total, 40);
  assert.equal(context.calibrated, false);
  const billing = buildCostSteps(request);
  const estimate = buildContextCostEstimate(request, billing);
  const rows = [...estimate.rows, ...billing.rows.filter(row => row.input == null)];
  for (const [mode, total] of [['tokens', 50], ['cost', .06]]) {
    const { steps } = accumulateWaterfall(rows, mode);
    const tools = steps[1];
    const [entry] = accumulateWaterfall(tools.entries, mode, tools.start).steps;
    assert.equal(entry.start, tools.start);
    assert.equal(entry.end, tools.end);
    assert.equal(steps.at(-1).start, tools.end);
    assert.ok(Math.abs(steps.at(-1).end - total) < 1e-12);
  }
});

test('split Claude cache creation replaces aggregate cache rows', () => {
  const data = buildCostSteps({ provider: 'claude', inputTokens: 10, cacheReadTokens: 30, cacheCreationTokens: 50, cacheCreation5mTokens: 20, cacheCreation1hTokens: 30, outputTokens: 5, cacheCreationCost: 9, cacheCreation5mCost: 2, cacheCreation1hCost: 3, cost: 5 });
  assert.equal(data.tokens, 95);
  assert.equal(data.rows.reduce((sum, row) => sum + row.cost, 0), 5);
  assert.ok(!data.rows.some(row => row.id === 'write' || row.residual));
});

test('Codex TTL rows use persisted costs and show each TTL price with its applied multipliers', () => {
  const data = buildCostSteps({
    provider: 'codex', inputTokens: 100, cacheCreationTokens: 60, cost: .09, cacheCreationCost: .09,
    billingRuleSnapshot: {
      cache_write_pricing: 'explicit', cache_write_30m_tokens: 40, cache_write_12h_tokens: 20,
      cache_write_30m_cost: .03, cache_write_12h_cost: .06, cache_write_30m_price: 2, cache_write_12h_price: 8,
      cache_creation_cost_multiplier: 1.5, total_cost_multiplier: .5,
    },
  });
  assert.equal(data.tokens, 100);
  const writes = data.rows.filter(row => row.id.startsWith('write'));
  assert.deepEqual(writes.map(row => row.cost), [.03, .06]);
  assert.deepEqual(writes.map(row => row.pricing), [
    { unit: 2, multiplier: 1.5, totalMultiplier: .5 },
    { unit: 8, multiplier: 1.5, totalMultiplier: .5 },
  ]);
});

test('historical differences remain visible, including a negative correction', () => {
  const data = buildCostSteps({ inputTokens: 1, inputCost: 2, cost: 1 });
  assert.equal(data.rows.at(-1).cost, -1);
  const { steps, scale } = accumulateWaterfall(data.rows, 'cost');
  assert.equal(scale, 2);
  assert.equal(steps.at(-1).end, 1);
});

test('a missing price is distinguishable from a zero-priced request', () => {
  assert.equal(buildCostSteps({ billingRuleSnapshot: { pricing_fallback: 'missing_price' } }).unpriced, true);
  assert.equal(buildCostSteps({ cost: 0 }).unpriced, false);
  assert.equal(formatRequestInsightCost(0), '$0.00');
  assert.equal(formatRequestInsightCost(4e-8), '$0.00000004');
  assert.equal(formatRequestInsightCost(4e-9), '<$0.00000001');
});

test('aggregated legacy retry timing stays on a single node', () => {
  const rows = buildFallbackAttempts({ status: 'completed', endpointChain: [{ endpoint: 'a', attempts: 3, duration_ms: 900, status_code: 429 }, { endpoint: 'b', attempts: 1, duration_ms: 100 }] });
  assert.equal(rows.length, 2);
  assert.equal(rows[0].attempts, 3);
  assert.equal(rows[0].duration, 900);
  assert.equal(rows[1].state, 'answered');
});

test('dispatch per-send events take priority over grouped chain diagnostics', () => {
  const rows = buildFallbackAttempts({ status: 'failed', endpointChain: [{ endpoint: 'a', attempts: 2, duration_ms: 900 }], dispatchTrace: { attempts: [{ endpoint: 'a', action: 'failed' }, { endpoint: 'a', action: 'failed' }] } });
  assert.equal(rows.length, 2);
  assert.ok(rows.every(row => row.duration === null && row.state === 'failed'));
  assert.deepEqual(buildFallbackAttempts({ endpoint: 'never sent', dispatchTrace: { attempts: [], attemptCount: 0 } }), []);
});

test('Codex search details show recorded Tavily calls and preserve reported zero credits', () => {
  const attempts = buildFallbackAttempts({
    provider: 'codex', path: '/v1/alpha/search', status: 'completed',
    dispatchTrace: { attempts: [{ endpoint: 'ZeroCat', action: 'failed', status_code: 400 }] },
    endpointChain: [
      { channel: '搜索服务', endpoint: 'Tavily', status_code: 200, duration_ms: 12, usage_credits: 0 },
      { channel: '搜索服务', endpoint: 'Tavily', status_code: 200, duration_ms: 15, usage_credits: 2 },
    ],
  });

  assert.equal(attempts.length, 2);
  assert.deepEqual(attempts.map(item => item.state), ['answered', 'answered']);
  assert.deepEqual(attempts.map(item => item.usageCredits), [0, 2]);
  assert.deepEqual(summarizeTavilyUsage(attempts), { calls: 2, reportedCalls: 2, credits: 2 });
});

test('legacy Tavily calls without saved credits remain explicitly unrecorded', () => {
  const attempts = buildFallbackAttempts({
    provider: 'codex', path: '/v1/alpha/search', status: 'completed',
    endpointChain: [{ channel: '搜索服务', endpoint: 'Tavily', status_code: 200 }],
  });

  assert.equal(attempts[0].usageCredits, null);
  assert.deepEqual(summarizeTavilyUsage(attempts), { calls: 1, reportedCalls: 0, credits: null });
});

test('context breakdown uses recorded counts without treating them as provider token totals', () => {
  const map = buildContextMap({ provider: 'codex', inputTokens: 32000, cacheReadTokens: 20000, contextSummary: {
    version: 1, partial: true, parts: { system: { tokens: 1000, items: 2 }, chat: { tokens: 2000, items: 1 } },
  } });
  assert.equal(map.input, 32000);
  assert.equal(map.total, 3000);
  assert.equal(map.partial, true);
  const tiles = layoutContextTiles(map.rows, map.total);
  assert.equal(tiles[0].id, 'chat');
  for (const tile of tiles) {
    assert.ok(Math.abs(tile.width * tile.height / 10000 - tile.tokens / map.total) < 1e-9);
  }
});

test('context map falls back to input accounting for legacy records', () => {
  for (const [provider, inputTokens] of [['codex', 1000], ['claude', 200]]) {
    const map = buildContextMap({ provider, inputTokens, cacheReadTokens: 700, cacheCreationTokens: 100 });
    assert.equal(map.input, 1000);
    assert.equal(map.total, 1000);
    assert.equal(map.estimated, false);
  }
});

test('named context entries are proportionally calibrated and keep their group totals', () => {
  const summary = {
    version: 2, turns: 2,
    parts: {
      tools: { tokens: 12, items: 3, breakdown: [
        { name: 'exec', kind: 'tool', tokens: 7, count: 1 },
        { name: 'collaboration', kind: 'namespace', tokens: 5, count: 2 },
      ] },
      chat: { tokens: 5, items: 2, breakdown: [
        { kind: 'turn', turn: 2, tokens: 2, count: 1 },
        { kind: 'turn', turn: 1, tokens: 3, count: 1 },
      ] },
    },
  };
  for (const partial of [false, true]) {
    const data = buildContextMap({ provider: 'codex', inputTokens: 101, contextSummary: { ...summary, partial } });
    assert.equal(data.calibrated, !partial);
    assert.equal(data.total, partial ? 17 : 101);
    assert.equal(data.turns, 2);
    assert.equal(data.rows[1].entries[0].label, '第 2 轮');
    assert.deepEqual(data.rows[0].entries.map(({ label, items }) => [label, items]), [['exec', 1], ['collaboration', 2]]);
    for (const row of data.rows) assert.equal(row.entries.reduce((sum, item) => sum + item.tokens, 0), row.tokens);
    assert.equal(contextTileRows(data.rows).reduce((sum, tile) => sum + tile.tokens, 0), data.total);
  }
});
