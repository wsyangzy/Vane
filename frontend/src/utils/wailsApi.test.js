import test from 'node:test';
import assert from 'node:assert/strict';

import { parseEndpointPriority } from './wailsApi.js';

test('parseEndpointPriority preserves zero as the highest priority', () => {
  assert.equal(parseEndpointPriority(0), 0);
  assert.equal(parseEndpointPriority('0'), 0);
});

test('parseEndpointPriority rejects invalid and negative values', () => {
  assert.equal(parseEndpointPriority('', 1), 1);
  assert.equal(parseEndpointPriority('2.5', 1), 1);
  assert.equal(parseEndpointPriority('-1', 1), 1);
});
