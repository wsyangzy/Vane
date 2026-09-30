import test from 'node:test';
import assert from 'node:assert/strict';

import { formatCacheHitRate, getBillableInputTokens, getRequestCacheHitRate } from './tokenDisplay.js';

test('per-request cache hit rate follows the provider token semantics', () => {
  // Claude：800 / (100 + 100 + 800)
  assert.equal(getRequestCacheHitRate({ provider: 'claude', inputTokens: 100, cacheCreationTokens: 100, cacheReadTokens: 800 }), 0.8);
  // Codex：input 已含缓存 → 800 / 1000
  assert.equal(getRequestCacheHitRate({ provider: 'codex', inputTokens: 1000, cacheReadTokens: 800 }), 0.8);
  // Codex 极旧行只存计费输入 → 800 / (200 + 800)
  assert.equal(getRequestCacheHitRate({ provider: 'codex', inputTokens: 200, cacheReadTokens: 800 }), 0.8);
  assert.equal(getRequestCacheHitRate({ provider: 'claude' }), null);
});

test('cache hit rate shows "-" without input usage and keeps a real 0%', () => {
  assert.equal(formatCacheHitRate(null), '-');
  assert.equal(formatCacheHitRate(undefined), '-');
  assert.equal(formatCacheHitRate(0), '0.0%');
  assert.equal(formatCacheHitRate(0.8771), '87.7%');
});

test('Codex billable input excludes cache reads and writes', () => {
  assert.equal(getBillableInputTokens({
    provider: 'codex',
    inputTokens: 1_000_000,
    cacheReadTokens: 200_000,
    cacheCreationTokens: 300_000
  }), 500_000);
});

test('legacy cache-exclusive input remains billable as stored', () => {
  assert.equal(getBillableInputTokens({
    provider: 'codex',
    inputTokens: 1_000,
    cacheReadTokens: 2_000,
    cacheCreationTokens: 0
  }), 1_000);
});

test('Claude input accounting remains unchanged', () => {
  assert.equal(getBillableInputTokens({
    provider: 'claude',
    inputTokens: 1_000,
    cacheReadTokens: 200,
    cacheCreationTokens: 300
  }), 1_000);
});
