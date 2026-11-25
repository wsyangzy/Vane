import test from 'node:test';
import assert from 'node:assert/strict';
import { groupPathEntries, currentPathResult, pathEntryStatus } from './endpointPathsState.js';
import { endpointPathIdentity } from './pathPolicy.js';

test('path inventory groups by channel then credential group, preserving endpoints with repeated names', () => {
  const entries = [
    { channel: { id: 1 }, group: { id: 11 }, endpoint: { id: 1, name: 'main' } },
    { channel: { id: 2 }, group: { id: 11 }, endpoint: { id: 2, name: 'main' } },
    { channel: { id: 1 }, group: { id: 12 }, endpoint: { id: 3, name: 'main' } },
    { channel: { id: 1 }, group: { id: 11 }, endpoint: { id: 4, name: 'Asia' } },
  ];
  const grouped = groupPathEntries(entries);
  assert.deepEqual(grouped.map(item => [item.channel.id, item.groups.map(group => group.entries.map(entry => entry.endpoint.id))]), [[1, [[1, 4], [3]]], [2, [[2]]]]);
  assert.equal(grouped[0].entries.length, 3);
});

test('path summaries retain ambiguity, distinguish saved paths, and discard stale detection evidence', () => {
  const entry = { endpoint: { id: 1, baseURL: 'https://example.invalid', credentialGroupId: 11, pathStrategy: 'legacy' } };
  const row = { identity: endpointPathIdentity(entry.endpoint), result: { candidates: [{ result: 'authentication_failed' }, { result: 'authentication_failed' }] }, strategy: '' };
  assert.equal(pathEntryStatus(entry, row).label, '鉴权失败');
  assert.equal(pathEntryStatus(entry, { ...row, strategy: 'v1' }).label, '可应用');
  assert.equal(pathEntryStatus(entry, { ...row, strategy: 'v1', saved: true }).label, '已应用');
  assert.equal(pathEntryStatus(entry, { ...row, error: 'save failed' }).label, '应用失败');
  assert.equal(pathEntryStatus({ ...entry, fixed: true }, row).label, '官方固定');
  assert.equal(currentPathResult(entry.endpoint, { 1: row }), row);
  assert.equal(currentPathResult({ ...entry.endpoint, credentialGroupId: 12 }, { 1: row }), null);
  assert.equal(currentPathResult({ ...entry.endpoint, baseURL: 'https://changed.invalid' }, { 1: row }), null);
});
