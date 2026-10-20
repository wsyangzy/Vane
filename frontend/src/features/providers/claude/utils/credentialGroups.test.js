import test from 'node:test';
import assert from 'node:assert/strict';

import { getCredentialGroupCostMultiplier, getCredentialGroupForEndpoint } from './credentialGroups.js';

test('endpoint display uses its credential group multiplier before stale endpoint multiplier', () => {
  const groups = [
    { id: 1, isDefault: true, costMultiplier: 1 },
    { id: 2, name: 'rkkey', costMultiplier: 0.4 },
  ];
  const endpoint = { credentialGroupId: 2, costMultiplier: 2 };

  const group = getCredentialGroupForEndpoint(groups, endpoint);

  assert.equal(group.id, 2);
  assert.equal(getCredentialGroupCostMultiplier(group, endpoint), 0.4);
});

test('default credential group is used for legacy endpoints without a group id', () => {
  const groups = [
    { id: 1, is_default: true, cost_multiplier: 1.25 },
    { id: 2, cost_multiplier: 0.4 },
  ];
  const endpoint = { cost_multiplier: 3 };

  const group = getCredentialGroupForEndpoint(groups, endpoint);

  assert.equal(group.id, 1);
  assert.equal(getCredentialGroupCostMultiplier(group, endpoint), 1.25);
});

test('stale endpoint multiplier remains the fallback when no credential group exists', () => {
  const endpoint = { costMultiplier: 1.5 };

  assert.equal(getCredentialGroupForEndpoint([], endpoint), null);
  assert.equal(getCredentialGroupCostMultiplier(null, endpoint), 1.5);
});
