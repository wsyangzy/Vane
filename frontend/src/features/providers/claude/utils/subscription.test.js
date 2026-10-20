import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash, webcrypto } from 'node:crypto';
import { generateLocalRelayKey, validateSubscription } from './subscription.js';
import { buildClaudeCredentialGroupPayload, mapClaudeCredentialGroup, buildCodexCredentialGroupPayload, mapCodexCredentialGroup, mapSubscriptionAccount } from '../../../../services/wails/index.js';

test('local key stays at generation boundary and only SHA256 enters the bridge', async () => {
  const { key, hash } = await generateLocalRelayKey(webcrypto);
  const next = await generateLocalRelayKey(webcrypto);
  assert.notEqual(key, next.key);
  assert.equal(hash, createHash('sha256').update(key).digest('hex'));
  const form = { authMode: 'client_passthrough', localKeyAcknowledged: true, subscription: { version: 2, enabled: true, keyHash: hash, backupTargets: [{ channelId: 5, groupId: 9, upstreamModel: 'model-y', rowId: 'ui-only' }] } };
  assert.equal(validateSubscription(form), '');
  const payload = buildClaudeCredentialGroupPayload(form);
  assert.equal(payload.subscription.key_hash, hash);
  assert.deepEqual(payload.subscription.backup_targets, [{ channel_id: 5, group_id: 9, upstream_model: 'model-y' }]);
  assert.ok(!JSON.stringify(payload).includes(key));
  assert.ok(!JSON.stringify(payload).includes('ui-only'));
  assert.notEqual(validateSubscription({ ...form, localKeyAcknowledged: false }), '');
});

test('saved subscription maps disabled and unknown reset explicitly', () => {
  const group = mapClaudeCredentialGroup({ auth_mode: 'client_passthrough', local_key_configured: true, subscription: { version: 4, enabled: false, backup_targets: [{ channel_id: 2, group_id: 3, upstream_model: 'y' }] }, subscription_status: [{ model: '*', state: 'due_unverified', reset_known: false, resume_not_before: '2026-10-16T10:00:00Z' }] });
  assert.equal(group.subscription.enabled, false);
  assert.equal(group.subscription.version, 4);
  assert.equal(group.subscriptionStatus[0].resetKnown, false);
  assert.equal(group.subscriptionStatus[0].state, 'due_unverified');
  assert.equal(validateSubscription(group), '');
  const legacy = buildClaudeCredentialGroupPayload({ name: 'old' });
  assert.equal(legacy.auth_mode, '');
  assert.equal(Object.hasOwn(legacy, 'subscription'), false);
});

test('Claude and Codex keep managed account binding and public metadata in parity', () => {
  for (const [map, build] of [[mapClaudeCredentialGroup, buildClaudeCredentialGroupPayload], [mapCodexCredentialGroup, buildCodexCredentialGroupPayload]]) {
    const group = map({ auth_mode: 'client_passthrough', local_key_configured: true, subscription: { account_id: 'managed-account', version: 3, enabled: true, backup_targets: [] } });
    assert.equal(group.subscription.accountId, 'managed-account');
    const payload = build(group);
    assert.equal(payload.subscription.account_id, 'managed-account');
    assert.equal(payload.subscription.version, 3);
    assert.equal(Object.hasOwn(payload.subscription, 'access_token'), false);
  }
  const metadata = mapSubscriptionAccount({ id: 'account', provider: 'codex', enabled: false, access_token: 'must-not-return', refresh_token: 'must-not-return', status: 'sign_in_required' });
  assert.equal(metadata.enabled, false);
  assert.equal(metadata.status, 'sign_in_required');
  assert.equal(JSON.stringify(metadata).includes('must-not-return'), false);
});
