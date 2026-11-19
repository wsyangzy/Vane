import test from 'node:test';
import assert from 'node:assert/strict';
import { getRequestFailureDetails } from './failureSummary.js';
import { collectRecoveryGroups, readRecoveryCandidates, releasableCandidates, releaseRecoveryCandidates } from './requestRecovery.js';

const target = (endpointId, fields = {}) => ({
  provider: 'codex', channelId: 38, groupId: 54, endpointId, model: 'upstream-model', reason: 'cooldown', ...fields,
});
const entry = (blockage, canRetry = true) => ({ blockage, canRetry });
const snapshot = (events, status = 'failed') => ({ status, provider: 'codex', route_snapshot: { attempts: 0, candidates: events } });
const event = (blockage, reason = blockage.reason) => ({ action: 'skipped', reason, candidate: { endpoint: blockage.endpointId }, blockage });

test('recovery scope deduplicates exact resources, excludes structural skips and uses the effective model', () => {
  const request = snapshot([
    event(target('97')),
    event(target('98')),
    event(target('99'), 'candidate_duplicate'),
    event(target('100', { reason: 'daily_cost_limit_exceeded' })),
    event(target('98', { model: 'mapped-model' })),
  ]);
  const groups = collectRecoveryGroups([request, request, snapshot([event(target('101'))], 'completed')]);
  assert.deepEqual(groups.map(group => [group.model, [...group.endpoints]]), [['upstream-model', ['97', '98']], ['mapped-model', ['98']]]);
  assert.ok(getRequestFailureDetails(request).blockingCandidates.every(item => !item.endpoints.includes('端点 #99')));
  assert.ok(!getRequestFailureDetails(request).blockingCandidates.some(item => item.reason === 'candidate_duplicate'));
});

test('batch rechecks admission, only releases the reviewed endpoints and preserves partial results', async () => {
  const groups = collectRecoveryGroups([snapshot(['1', '2', '3', '4', '5'].map(id => event(target(id))))]);
  let apply = false;
  const ready = new Set();
  const calls = [];
  const getRecovery = async () => [
    ...['1', '2', '3', '4', '5', 'unreviewed'].map(id => ready.has(id)
      ? entry(target(id, { reason: '', recovery: 'ready' }), false)
      : apply && id === '5' ? entry(target(id, { reason: 'quota_exhausted' }), false) : entry(target(id))),
  ];
  const retryCandidate = async ({ endpointId }) => {
    calls.push(endpointId);
    if (endpointId === '1') ['1', '2'].forEach(id => ready.add(id));
    else if (endpointId === '2') throw new Error('shared gate already released');
    else if (endpointId === '3') throw new Error('operation failed');
    else ready.add(endpointId);
  };
  const reviewed = releasableCandidates(await readRecoveryCandidates(groups, getRecovery));
  assert.equal(reviewed.length, 5);
  apply = true;
  const result = await releaseRecoveryCandidates(groups, reviewed, { getRecovery, retryCandidate });
  assert.deepEqual(calls, ['1', '2', '3', '4']);
  assert.equal(result.released, 2);
  assert.deepEqual([...result.failures.values()], ['operation failed']);
  assert.deepEqual(releasableCandidates(result.entries).map(item => item.endpointId), ['3']);
});
