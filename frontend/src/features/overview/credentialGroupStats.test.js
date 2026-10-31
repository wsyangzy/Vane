import test from 'node:test';
import assert from 'node:assert/strict';
import { prepareGroupCosts, summarizeCredentialGroups } from './credentialGroupStats.js';

test('overview counts each credential group once across endpoints and separates equal names by channel', () => {
  const candidate = { channelId: 1, groupId: 1, enabled: true, protocol: 'responses', health: {} };
  const result = summarizeCredentialGroups({
    channels: [{ id: 1, name: 'A' }, { id: 2, name: 'B' }, { id: 3, name: 'Empty' }],
    accounts: [{ channelId: 1, groupId: 1, groupName: 'same' }, { channelId: 2, groupId: 1, groupName: 'same' }, { channelId: 1, groupId: 1, groupName: 'duplicate' }],
    candidates: [{ ...candidate, endpointId: 'one' }, { ...candidate, endpointId: 'two' }, { ...candidate, endpointId: 'two' }],
  });
  assert.equal(result.channels.length, 3);
  assert.equal(result.total, 2);
  assert.equal(result.usable, 1);
  assert.deepEqual(result.counts, { available: 1, partial: 0, blocked: 0, unconfigured: 1 });
  assert.equal(result.channels[0].groups.length, 1);
  assert.equal(result.channels[2].groups.length, 0);
});

test('one working candidate preserves partial availability and all blocked groups stay separate from unconfigured groups', () => {
  const base = { channelId: 1, enabled: true, health: {} };
  const result = summarizeCredentialGroups({
    accounts: [1, 2, 3, 4, 5].map((groupId) => ({ channelId: 1, groupId, channelName: 'relay' })),
    candidates: [
      { ...base, groupId: 1, endpointId: 'a' },
      { ...base, groupId: 1, endpointId: 'b', health: { coolingDown: true } },
      { ...base, groupId: 2, endpointId: 'a', health: { repairRequired: true } },
      { ...base, groupId: 3, endpointId: 'a', enabled: false },
      { ...base, groupId: 4, endpointId: 'a', health: { coolingDown: true } },
      { ...base, groupId: 99, endpointId: 'orphan' },
    ],
  });
  assert.equal(result.total, 5);
  assert.equal(result.usable, 1);
  assert.deepEqual(result.counts, { available: 0, partial: 1, blocked: 2, unconfigured: 2 });
  assert.equal(result.channels[0].name, 'relay');
  assert.deepEqual(result.channels[0].groups.map((group) => group.state), ['partial', 'blocked', 'unconfigured', 'blocked', 'unconfigured']);
});

test('resource state does not use legacy participation, profile membership, balance or unknown quota as health', () => {
  const result = summarizeCredentialGroups({
    accounts: [{ channelId: 1, groupId: 1, enabled: false }],
    candidates: [{ channelId: 1, groupId: 1, endpointId: 'a', enabled: true, health: {}, balance: { enabled: true, balanceUsd: '0' }, quota: { windows: [] } }],
    profiles: [],
  });
  assert.equal(result.counts.available, 1);
  assert.equal(summarizeCredentialGroups({}).total, 0);
});

test('cost labels identify provider, channel and credential group without exposing endpoint labels', () => {
  const costs = prepareGroupCosts([{ provider: 'claude', channel: 'relay', group: 'key', endpoint: 'east', cost: 2, tokens: 100 }, { provider: 'codex', channel: 'relay', group: 'key', cost: 3 }, { provider: 'claude', channel: '', group: '' }]);
  assert.equal(costs[0].name, 'Claude · relay / key');
  assert.equal(costs[1].name, 'Codex · relay / key');
  assert.equal(costs[2].name, 'Claude · 未记录渠道 / 未记录分组');
  assert.equal(costs[0].tokens, 100);
});
