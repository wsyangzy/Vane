import test from 'node:test';
import assert from 'node:assert/strict';
import { normalizeModelSelection, requestModelOptions } from './modelFilter.js';

test('model options collapse provider and pricing tier variants into a single model name', () => {
  assert.deepEqual(requestModelOptions([
    { model_name: 'gpt-6-astra', provider: 'codex', tier: 'standard' },
    { model_name: 'gpt-6-astra', provider: 'codex', tier: 'priority' },
    { modelName: 'gpt-6-astra', provider: 'claude', tier: 'standard' },
    { name: 'gpt-6-luna' }, 'gpt-6-luna', null, { model_name: ' ' }
  ]), ['gpt-6-astra', 'gpt-6-luna']);
});

test('selected models remain available after the pricing catalog changes', () => {
  assert.deepEqual(requestModelOptions([{ name: 'listed' }], ['retired', 'listed']), ['listed', 'retired']);
  assert.deepEqual(normalizeModelSelection([' gpt ', '', 'gpt', null, 1, 'other']), ['gpt', 'other']);
  assert.deepEqual(normalizeModelSelection('legacy'), ['legacy']);
  assert.deepEqual(normalizeModelSelection([]), []);
});
