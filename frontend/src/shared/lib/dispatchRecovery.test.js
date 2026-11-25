import test from 'node:test';
import assert from 'node:assert/strict';
import { dispatchRecoveryTarget, readCurrentDispatchCandidate, retryCurrentDispatchCandidate } from './dispatchRecovery.js';

const target = { provider: 'codex', channelId: 1, groupId: 11, endpointId: 'shared', model: 'mapped-model' };
const state = (patch = {}, canRetry = true) => ({ blockage: { ...target, reason: 'cooldown', recovery: 'automatic', ...patch }, canRetry });

test('recovery identifies the effective model and retains the exact provider, group and endpoint', () => {
  assert.deepEqual(dispatchRecoveryTarget('claude', { channelId: '2', groupId: '12', endpointId: 3, model: 'client-alias', upstreamModel: ' mapped-model ' }), {
    provider: 'claude', channelId: 2, groupId: 12, endpointId: '3', model: 'mapped-model',
  });
  assert.equal(dispatchRecoveryTarget('codex', { ...target, upstreamModel: ' ' }).model, 'mapped-model');
});

test('release rechecks live admission and leaves newly blocked or already probing resources unchanged', async () => {
  for (const provider of ['claude', 'codex']) {
    for (const reason of ['quota_exhausted', 'credential_missing', 'concurrency_limit', 'account_probe_in_progress', '']) {
      let retries = 0;
      const current = state({ provider, reason }, false);
      const result = await retryCurrentDispatchCandidate({ ...target, provider }, {
        readRecovery: async () => [current], retryCandidate: async () => { retries++; },
      });
      assert.equal(result.released, false);
      assert.equal(result.state, current);
      assert.equal(retries, 0);
    }
  }
});

test('matching never retries a similarly named endpoint from another group or model', async () => {
  for (const patch of [{ groupId: 12 }, { channelId: 2 }, { provider: 'claude' }, { model: 'other-model' }]) {
    let retries = 0;
    await assert.rejects(retryCurrentDispatchCandidate(target, {
      readRecovery: async () => [state(patch)], retryCandidate: async () => { retries++; },
    }), /当前状态或模型范围已变化/);
    assert.equal(retries, 0);
  }
});

test('a snapshot without a model retries only the model returned by the live check and keeps probing explicit', async () => {
  const calls = [];
  const updated = state({ reason: 'account_probe_in_progress', recovery: 'probing' }, false);
  const result = await retryCurrentDispatchCandidate({ ...target, model: '' }, {
    readRecovery: async () => [state()],
    retryCandidate: async input => { calls.push(input); return [updated]; },
  });
  assert.deepEqual(calls, [target]);
  assert.equal(result.released, true);
  assert.equal(result.state, updated);
  assert.equal(result.state.blockage.recovery, 'probing');
});

test('missing or ambiguous resources and failed mutations keep their failures visible', async () => {
  await assert.rejects(readCurrentDispatchCandidate(target, async () => []), /当前状态/);
  await assert.rejects(readCurrentDispatchCandidate({ ...target, model: '' }, async () => [state(), state({ model: 'other' })]), /模型范围/);
  await assert.rejects(retryCurrentDispatchCandidate(target, {
    readRecovery: async () => [state()], retryCandidate: async () => { throw new Error('上游状态更新失败'); },
  }), /上游状态更新失败/);
});
