import test from 'node:test';
import assert from 'node:assert/strict';

import { formatCacheHitRate, getBillableInputTokens } from './tokenDisplay.js';

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
