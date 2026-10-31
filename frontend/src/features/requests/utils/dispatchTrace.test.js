import test from 'node:test';
import assert from 'node:assert/strict';
import { readRequestDispatchTrace } from './dispatchTrace.js';

const candidate = (provider, channel, group, endpoint) => ({ provider, channel: String(channel), credential: `${channel}/${group}`, endpoint: String(endpoint), model: 'test-model', protocol: provider === 'codex' ? 'responses' : 'messages' });
const resources = [
  { provider: 'codex', id: 1, name: '渠道 A', credentialGroups: [{ id: 11, name: '分组 A' }], endpoints: [{ id: 101, name: '主端点', credentialGroupId: 11 }] },
  { provider: 'codex', id: 2, name: '渠道 B', credentialGroups: [{ id: 21, name: '分组 B' }], endpoints: [{ id: 201, name: '备用端点', credentialGroupId: 21 }] },
  { provider: 'claude', id: 1, name: 'Claude 渠道', credentialGroups: [{ id: 11, name: 'Claude 分组' }] },
];
const trace = (events, extra = {}) => ({ profile_id: 7, profile_name: '请求当时的方案', strategy: 'order', affinity_mode: 'turn', decision: { kind: 'default' }, candidates: events, ...extra });

test('dispatch trace distinguishes actual attempts, repeated retries, skipped resources and terminal events', () => {
  const a = candidate('codex', 1, 11, 101), b = candidate('codex', 2, 21, 201);
  const result = readRequestDispatchTrace({ provider: 'codex', route_snapshot: JSON.stringify(trace([
    { candidate: b, action: 'skipped', reason: 'candidate_busy' },
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed', reason: 'upstream_error' },
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed', reason: 'upstream_error' },
    { candidate: b, action: 'attempt' }, { candidate: b, action: 'response_started' }, { candidate: b, action: 'success' },
  ], { attempts: 3, successful: true, finished_at: '2026-10-28T17:00:00+08:00' })) }, resources);
  assert.equal(result.profileName, '请求当时的方案');
  assert.equal(result.attemptCount, 3);
  assert.deepEqual(result.attempts.map(item => [item.channel, item.credentialGroup, item.endpoint, item.action]), [
    ['渠道 A', '分组 A', '主端点', 'failed'], ['渠道 A', '分组 A', '主端点', 'failed'], ['渠道 B', '分组 B', '备用端点', 'success'],
  ]);
  assert.equal(result.skipped.length, 1);
});

test('Claude resource keys and managed credentials resolve without leaking account identity', () => {
  const c = { ...candidate('claude', 1, 11, 'Claude 渠道::g11::主端点'), credential: 'account:PRIVATE_ACCOUNT_HASH' };
  const result = readRequestDispatchTrace({ provider: 'claude', routeSnapshot: trace([{ candidate: c, action: 'attempt' }]) }, resources);
  assert.equal(result.attempts[0].channel, 'Claude 渠道');
  assert.equal(result.attempts[0].credentialGroup, 'Claude 分组');
  assert.equal(result.attempts[0].endpoint, '主端点');
  assert.ok(!JSON.stringify(result).includes('PRIVATE_ACCOUNT_HASH'));
});

test('historical path names and hop diagnostics survive renamed or deleted resources', () => {
  const c = candidate('codex', 1, 11, 101);
  const request = { provider: 'codex', channel: '历史渠道', endpoint: '历史端点', endpoint_chain: [{ channel: '历史渠道', credential_group: '历史分组', endpoint: '历史端点', duration_ms: 12, status_code: 429 }], route_snapshot: trace([{ candidate: c, action: 'attempt' }, { candidate: c, action: 'failed' }], { attempts: 1 }) };
  const result = readRequestDispatchTrace(request, resources);
  assert.equal(result.attempts[0].channel, '历史渠道');
  assert.equal(result.attempts[0].endpoint, '历史端点');
  assert.equal(result.attempts[0].credentialGroup, '历史分组');
  assert.equal(result.attempts[0].durationMs, 12);
  assert.equal(result.attempts[0].statusCode, 429);
  const deleted = readRequestDispatchTrace({ routeSnapshot: trace([{ candidate: c, action: 'attempt' }]) });
  assert.equal(deleted.attempts[0].channel, '渠道 #1');
  assert.equal(deleted.attempts[0].endpoint, '端点 #101');
});

test('admission rejection has zero upstream attempts and old records never inherit the current profile', () => {
  const c = candidate('codex', 1, 11, 101);
  const result = readRequestDispatchTrace({ routeSnapshot: trace([{ candidate: c, action: 'skipped', reason: 'manual_balance_exhausted' }], { attempts: 0, stop_reason: 'route_unavailable' }) }, resources);
  assert.equal(result.attemptCount, 0);
  assert.equal(result.attempts.length, 0);
  assert.equal(result.skipped.length, 1);
  for (const value of ['', 'broken-json', '[]', null, { kind: 'model_rule', name: '旧规则' }]) {
    assert.equal(readRequestDispatchTrace({ route_snapshot: value }, resources), null);
  }
});

test('historical group identity survives moving an endpoint to another credential group', () => {
  const c = candidate('codex', 1, 11, 101);
  const moved = [{ ...resources[0], credentialGroups: [{ id: 11, name: '历史分组' }, { id: 12, name: '新分组' }], endpoints: [{ id: 101, name: '主端点', credentialGroupId: 12 }] }];
  const result = readRequestDispatchTrace({ provider: 'codex', route_snapshot: trace([{ candidate: c, action: 'attempt' }]) }, moved);
  assert.equal(result.attempts[0].credentialGroup, '历史分组');
});

test('grouped historical hops retain names without assigning aggregate diagnostics to each retry', () => {
  const a = candidate('codex', 1, 11, 101), b = candidate('codex', 2, 21, 201);
  const result = readRequestDispatchTrace({ provider: 'codex', routeSnapshot: trace([
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed' },
    { candidate: b, action: 'attempt' }, { candidate: b, action: 'success' },
  ]), endpoint_chain: [
    { channel: '渠道 A', credential_group: '分组 A', endpoint: '主端点', attempts: 2, duration_ms: 200, status_code: 503 },
    { channel: '渠道 B', credential_group: '分组 B', endpoint: '备用端点', attempts: 1, duration_ms: 300 },
  ] }, resources);
  assert.deepEqual(result.attempts.map(item => item.endpoint), ['主端点', '主端点', '备用端点']);
  assert.ok(result.attempts.slice(0, 2).every(item => item.durationMs == null && !item.statusCode));
  assert.equal(result.attempts[2].durationMs, 300);
});

test('advanced diagnostics retain historical identities, zero state versions and every candidate event across retries', () => {
  const c = { ...candidate('codex', 1, 11, 101), state_version: 0 };
  const next = { ...c, state_version: 9 };
  const result = readRequestDispatchTrace({ provider: 'codex', routeSnapshot: trace([
    { candidate: c, action: 'attempt' }, { candidate: c, action: 'failed', reason: 'network_error', failure_scope: 'endpoint' },
    { candidate: c, action: 'attempt' }, { candidate: c, action: 'success' },
    { candidate: next, action: 'skipped', reason: 'candidate_busy' },
    { candidate: next, action: 'failed', reason: 'route_unavailable' },
  ], { request_id: 'historical-request-id' }) }, [{ ...resources[0], id: 99 }]);
  assert.equal(result.requestId, 'historical-request-id');
  assert.equal(result.attempts.length, 2);
  assert.equal(result.diagnostics.length, 2);
  const [first, second] = result.diagnostics;
  assert.deepEqual([first.channelId, first.groupId, first.endpointId, first.credentialLabel, first.stateVersion], ['1', '11', '101', '1/11', '0']);
  assert.deepEqual(first.events.map(event => event.action), ['attempt', 'failed', 'attempt', 'success']);
  assert.deepEqual(first.events[1], { action: 'failed', reason: 'network_error', scope: 'endpoint' });
  assert.equal(second.stateVersion, '9');
  assert.deepEqual(second.events.map(event => event.action), ['skipped', 'failed']);
});

test('advanced diagnostics hide managed account hashes and unexpected credential values and distinguish missing versions', () => {
  for (const credential of ['account:PRIVATE_ACCOUNT_HASH', 'unexpected-secret-placeholder']) {
    const c = { ...candidate('claude', 1, 11, 'Claude 渠道::g11::主端点'), credential };
    const result = readRequestDispatchTrace({ provider: 'claude', routeSnapshot: trace([{ candidate: c, action: 'skipped', reason: 'candidate_busy' }]) }, resources);
    assert.equal(result.diagnostics[0].groupId, '11');
    assert.equal(result.diagnostics[0].stateVersion, null);
    assert.ok(result.diagnostics[0].credentialLabel.includes('隐藏'));
    assert.ok(!JSON.stringify(result).includes(credential));
  }
});
